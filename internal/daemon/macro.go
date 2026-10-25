package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/rajeshshrirao/chrome-ctl/internal/config"
	"github.com/rajeshshrirao/chrome-ctl/internal/macro"
	"github.com/rajeshshrirao/chrome-ctl/internal/protocol"
)

const (
	macroResolveTimeoutMs = 3000
	macroRetryAttempts    = 3
)

// macroRecorder holds the in-progress recording. The daemon records mirrored
// actions as they are forwarded to the extension; nothing runs in the
// background.
type macroRecorder struct {
	mu     sync.Mutex
	active bool
	name   string
	steps  []protocol.MacroStep
}

// pendingMacroStep carries the describe result gathered before an action runs
// so macroAfterForward can build the step from the pre-action DOM state.
type pendingMacroStep struct {
	method string
	params json.RawMessage
	target *protocol.MacroTarget
}

var conditionPattern = regexp.MustCompile(`(?s)^(.*?)\s*(==|!=)\s*''\s*$`)

func (d *daemon) macroActive() bool {
	d.macro.mu.Lock()
	defer d.macro.mu.Unlock()
	return d.macro.active
}

func (d *daemon) macroDir() string {
	if d.cfg.Paths.MacrosDir != "" {
		return d.cfg.Paths.MacrosDir
	}
	return config.DefaultPaths().MacrosDir
}

func (d *daemon) handleMacroStart(_ context.Context, req protocol.Request) protocol.Response {
	var params protocol.MacroStartParams
	if len(req.Params) > 0 {
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return protocol.Failure(req.ID, protocol.ErrInvalidRequest, "invalid macro.start params")
		}
	}
	if !macro.ValidName(params.Name) {
		return protocol.Failure(req.ID, protocol.ErrInvalidRequest, fmt.Sprintf("invalid macro name %q", params.Name))
	}

	d.macro.mu.Lock()
	defer d.macro.mu.Unlock()
	if d.macro.active {
		return protocol.Failure(req.ID, protocol.ErrInvalidRequest, "macro recording already active; save the current macro first")
	}
	d.macro.active = true
	d.macro.name = params.Name
	d.macro.steps = nil

	return protocol.Success(req.ID, protocol.MacroStartResult{
		Name:    params.Name,
		Started: true,
		Steps:   0,
	})
}

// macroBeforeForward captures the element descriptor before an action mutates
// the DOM. Recording failures never block the underlying action.
func (d *daemon) macroBeforeForward(ctx context.Context, req protocol.Request) *pendingMacroStep {
	switch req.Method {
	case protocol.MethodTabOpen,
		protocol.MethodActionClick,
		protocol.MethodActionType,
		protocol.MethodActionUpload:
	default:
		return nil
	}
	if !d.macroActive() {
		return nil
	}

	pending := &pendingMacroStep{method: req.Method, params: req.Params}
	var tabID *int
	var id int
	switch req.Method {
	case protocol.MethodActionClick:
		var params protocol.ActionClickParams
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return nil
		}
		tabID, id = params.TabID, params.ID
	case protocol.MethodActionType:
		var params protocol.ActionTypeParams
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return nil
		}
		tabID, id = params.TabID, params.ID
	case protocol.MethodActionUpload:
		var params protocol.ActionUploadParams
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return nil
		}
		tabID, id = params.TabID, params.ID
	default:
		return pending
	}

	// Fall back to an empty descriptor: the action still runs and the step is
	// recorded with whatever tiers are available.
	pending.target = &protocol.MacroTarget{}
	raw, err := json.Marshal(protocol.PageDescribeParams{TabID: tabID, ID: id})
	if err != nil {
		return pending
	}
	describeCtx, cancel := context.WithTimeout(ctx, pageDescribeTimeout)
	defer cancel()
	result, rpcErr := d.forward(describeCtx, protocol.Request{
		JSONRPC: protocol.JSONRPC,
		Method:  protocol.MethodPageDescribe,
		Params:  raw,
	})
	if rpcErr != nil {
		return pending
	}
	var described protocol.PageDescribeResult
	if err := json.Unmarshal(result, &described); err != nil {
		return pending
	}
	pending.target = &described.Target
	return pending
}

// macroAfterForward appends a step for a successfully forwarded recording
// target. It is a no-op when recording stopped while the action was in flight.
func (d *daemon) macroAfterForward(req protocol.Request, resp protocol.Response, pending *pendingMacroStep) {
	if resp.Error != nil {
		return
	}
	if pending == nil {
		// press and scroll carry no element descriptor, so they need no
		// describe pass; record them straight from the forwarded request.
		switch req.Method {
		case protocol.MethodActionPress, protocol.MethodActionScroll:
			pending = &pendingMacroStep{method: req.Method, params: req.Params}
		default:
			return
		}
	}
	step, ok := buildMacroStep(pending, resp.Result)
	if !ok {
		return
	}

	d.macro.mu.Lock()
	defer d.macro.mu.Unlock()
	if !d.macro.active {
		return
	}
	step.StepID = len(d.macro.steps) + 1
	d.macro.steps = append(d.macro.steps, step)
}

func buildMacroStep(pending *pendingMacroStep, result json.RawMessage) (protocol.MacroStep, bool) {
	switch pending.method {
	case protocol.MethodTabOpen:
		var params protocol.TabOpenParams
		if err := json.Unmarshal(pending.params, &params); err != nil {
			return protocol.MacroStep{}, false
		}
		return protocol.MacroStep{
			Action:      "open",
			URL:         params.URL,
			Description: "Navigate to " + params.URL,
		}, true

	case protocol.MethodActionClick:
		var params protocol.ActionClickParams
		if err := json.Unmarshal(pending.params, &params); err != nil {
			return protocol.MacroStep{}, false
		}
		label := macroTargetLabel(pending.target)
		var clickResult protocol.ActionClickResult
		if len(result) > 0 && json.Unmarshal(result, &clickResult) == nil && clickResult.Target != nil && clickResult.Target.Label != "" {
			label = clickResult.Target.Label
		}
		step := protocol.MacroStep{
			Action:      "click",
			Target:      pending.target,
			Description: fmt.Sprintf("Click %q", label),
		}
		if params.DoubleClick {
			step.Options = &protocol.MacroStepOptions{Double: true}
		}
		return step, true

	case protocol.MethodActionType:
		var params protocol.ActionTypeParams
		if err := json.Unmarshal(pending.params, &params); err != nil {
			return protocol.MacroStep{}, false
		}
		return protocol.MacroStep{
			Action:      "type",
			Target:      pending.target,
			Value:       params.Text,
			Options:     &protocol.MacroStepOptions{Enter: params.Enter},
			Description: fmt.Sprintf("Type into %q", macroTargetLabel(pending.target)),
		}, true

	case protocol.MethodActionUpload:
		var params protocol.ActionUploadParams
		if err := json.Unmarshal(pending.params, &params); err != nil {
			return protocol.MacroStep{}, false
		}
		return protocol.MacroStep{
			Action: "upload",
			Target: pending.target,
			Value:  params.FilePath,
		}, true

	case protocol.MethodActionPress:
		var params protocol.ActionPressParams
		if err := json.Unmarshal(pending.params, &params); err != nil {
			return protocol.MacroStep{}, false
		}
		step := protocol.MacroStep{Action: "press", Value: params.Key}
		if len(params.Modifiers) > 0 {
			step.Options = &protocol.MacroStepOptions{Modifiers: params.Modifiers}
		}
		return step, true

	case protocol.MethodActionScroll:
		var params protocol.ActionScrollParams
		if err := json.Unmarshal(pending.params, &params); err != nil {
			return protocol.MacroStep{}, false
		}
		return protocol.MacroStep{
			Action:  "scroll",
			Options: &protocol.MacroStepOptions{Direction: params.Direction, Amount: params.Amount},
		}, true
	}
	return protocol.MacroStep{}, false
}

func macroTargetLabel(target *protocol.MacroTarget) string {
	if target == nil {
		return ""
	}
	for _, candidate := range []string{target.AriaLabel, target.Text, target.TestID} {
		if candidate != "" {
			return candidate
		}
	}
	return ""
}

func (d *daemon) handleMacroSave(_ context.Context, req protocol.Request) protocol.Response {
	var params protocol.MacroSaveParams
	if len(req.Params) > 0 {
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return protocol.Failure(req.ID, protocol.ErrInvalidRequest, "invalid macro.save params")
		}
	}

	d.macro.mu.Lock()
	if !d.macro.active {
		d.macro.mu.Unlock()
		return protocol.Failure(req.ID, protocol.ErrInvalidRequest, "no macro recording in progress; run macro.start first")
	}
	name := d.macro.name
	steps := append([]protocol.MacroStep(nil), d.macro.steps...)
	d.macro.mu.Unlock()

	file := protocol.MacroFile{
		Name:        name,
		Description: params.Description,
		Steps:       steps,
	}
	if err := macro.Parameterize(&file, params.Params); err != nil {
		return protocol.Failure(req.ID, protocol.ErrInvalidRequest, err.Error())
	}
	path, err := macro.Save(d.macroDir(), file)
	if err != nil {
		return protocol.Failure(req.ID, protocol.ErrInternal, "save macro: "+err.Error())
	}

	d.macro.mu.Lock()
	if d.macro.name == name {
		d.macro.active = false
		d.macro.name = ""
		d.macro.steps = nil
	}
	d.macro.mu.Unlock()

	names := make([]string, 0, len(file.Parameters))
	for _, param := range file.Parameters {
		names = append(names, param.Name)
	}
	return protocol.Success(req.ID, protocol.MacroSaveResult{
		Path:       path,
		Name:       name,
		Steps:      len(steps),
		Parameters: names,
	})
}

func (d *daemon) handleMacroList(_ context.Context, req protocol.Request) protocol.Response {
	macros, err := macro.List(d.macroDir())
	if err != nil {
		return protocol.Failure(req.ID, protocol.ErrInternal, "list macros: "+err.Error())
	}
	if macros == nil {
		macros = []protocol.MacroSummary{}
	}
	return protocol.Success(req.ID, protocol.MacroListResult{Macros: macros})
}

func (d *daemon) handleMacroRun(ctx context.Context, req protocol.Request) protocol.Response {
	var params protocol.MacroRunParams
	if len(req.Params) > 0 {
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return protocol.Failure(req.ID, protocol.ErrInvalidRequest, "invalid macro.run params")
		}
	}

	file, err := macro.Load(d.macroDir(), params.Name)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return protocol.Failure(req.ID, protocol.ErrInvalidRequest, fmt.Sprintf("unknown macro %q", params.Name))
		}
		return protocol.Failure(req.ID, protocol.ErrInternal, "load macro: "+err.Error())
	}

	vars := params.Vars
	if vars == nil {
		vars = map[string]string{}
	}
	for _, param := range file.Parameters {
		if !param.Required || param.Default != "" {
			continue
		}
		if _, ok := vars[param.Name]; !ok {
			return protocol.Failure(req.ID, protocol.ErrInvalidRequest, fmt.Sprintf("missing variable %q", param.Name))
		}
	}

	start := time.Now()
	stepsRun := 0
	for i := range file.Steps {
		step := file.Steps[i]

		run, err := macroConditionAllows(step.Condition, vars, file.Parameters)
		if err != nil {
			return protocol.Failure(req.ID, protocol.ErrInvalidRequest, err.Error())
		}
		if !run {
			continue
		}

		url, err := macro.Interpolate(step.URL, vars)
		if err != nil {
			return protocol.Failure(req.ID, protocol.ErrInvalidRequest, fmt.Sprintf("macro step %d: %v", macroStepID(step, i), err))
		}
		value, err := macro.Interpolate(step.Value, vars)
		if err != nil {
			return protocol.Failure(req.ID, protocol.ErrInvalidRequest, fmt.Sprintf("macro step %d: %v", macroStepID(step, i), err))
		}

		var resolved protocol.PageResolveResult
		if step.Target != nil {
			attempts := 1
			if step.OnFailure == "retry_3x" {
				attempts = macroRetryAttempts
			}
			reason := ""
			found := false
			for attempt := 0; attempt < attempts; attempt++ {
				result, rpcErr := d.resolveMacroTarget(ctx, *step.Target)
				if rpcErr == nil && result.ID > 0 {
					resolved = result
					found = true
					break
				}
				if rpcErr != nil {
					reason = rpcErr.Message
				} else {
					reason = fmt.Sprintf("element %q not found", macroTargetLabel(step.Target))
				}
			}
			if !found {
				label := macroTargetLabel(step.Target)
				if label != "" {
					reason = fmt.Sprintf("element %q not found", label)
				} else if reason == "" {
					reason = "target not found"
				}
				return d.macroStepFailure(ctx, req.ID, file.Name, step, i, len(file.Steps), reason)
			}
		}

		method, payload, ok := macroActionRequest(step, resolved, url, value)
		if !ok {
			return protocol.Failure(req.ID, protocol.ErrInvalidRequest, fmt.Sprintf("unsupported action %q", step.Action))
		}
		raw, err := json.Marshal(payload)
		if err != nil {
			return protocol.Failure(req.ID, protocol.ErrInternal, "encode macro step params")
		}
		if _, rpcErr := d.forward(ctx, protocol.Request{
			JSONRPC: protocol.JSONRPC,
			Method:  method,
			Params:  raw,
		}); rpcErr != nil {
			return d.macroStepFailure(ctx, req.ID, file.Name, step, i, len(file.Steps), rpcErr.Message)
		}
		stepsRun++
	}

	return protocol.Success(req.ID, protocol.MacroRunResult{
		Macro:      file.Name,
		StepsRun:   stepsRun,
		DurationMs: time.Since(start).Milliseconds(),
		Completed:  true,
	})
}

func macroStepID(step protocol.MacroStep, index int) int {
	if step.StepID > 0 {
		return step.StepID
	}
	return index + 1
}

func macroConditionAllows(condition string, vars map[string]string, params []protocol.MacroParameter) (bool, error) {
	condition = strings.TrimSpace(condition)
	if condition == "" {
		return true, nil
	}

	scope := make(map[string]string, len(vars)+len(params))
	for name, value := range vars {
		scope[name] = value
	}
	for _, param := range params {
		if _, ok := scope[param.Name]; !ok {
			scope[param.Name] = ""
		}
	}

	expanded, err := macro.Interpolate(condition, scope)
	if err != nil {
		return false, fmt.Errorf("unsupported condition %q", condition)
	}
	match := conditionPattern.FindStringSubmatch(strings.TrimSpace(expanded))
	if match == nil {
		return false, fmt.Errorf("unsupported condition %q", condition)
	}
	lhs := strings.TrimSpace(match[1])
	if match[2] == "==" {
		return lhs == "", nil
	}
	return lhs != "", nil
}

func (d *daemon) resolveMacroTarget(ctx context.Context, target protocol.MacroTarget) (protocol.PageResolveResult, *protocol.RPCError) {
	var result protocol.PageResolveResult
	raw, err := json.Marshal(protocol.PageResolveParams{
		Target:    target,
		TimeoutMs: macroResolveTimeoutMs,
	})
	if err != nil {
		return result, &protocol.RPCError{Code: protocol.ErrInternal, Message: "encode page.resolve params"}
	}

	resolveCtx, cancel := context.WithTimeout(ctx, pageResolveTimeout)
	defer cancel()
	data, rpcErr := d.forward(resolveCtx, protocol.Request{
		JSONRPC: protocol.JSONRPC,
		Method:  protocol.MethodPageResolve,
		Params:  raw,
	})
	if rpcErr != nil {
		return result, rpcErr
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return result, &protocol.RPCError{Code: protocol.ErrInternal, Message: "invalid page.resolve response"}
	}
	return result, nil
}

func macroActionRequest(step protocol.MacroStep, resolved protocol.PageResolveResult, url, value string) (string, any, bool) {
	options := step.Options
	switch step.Action {
	case "open":
		return protocol.MethodTabOpen, protocol.TabOpenParams{URL: url}, true
	case "click":
		return protocol.MethodActionClick, protocol.ActionClickParams{
			ID:          resolved.ID,
			DoubleClick: options != nil && options.Double,
		}, true
	case "type":
		return protocol.MethodActionType, protocol.ActionTypeParams{
			ID:    resolved.ID,
			Text:  value,
			Enter: options != nil && options.Enter,
		}, true
	case "press":
		var modifiers []string
		if options != nil {
			modifiers = options.Modifiers
		}
		return protocol.MethodActionPress, protocol.ActionPressParams{Key: value, Modifiers: modifiers}, true
	case "scroll":
		var direction string
		var amount float64
		if options != nil {
			direction = options.Direction
			amount = options.Amount
		}
		return protocol.MethodActionScroll, protocol.ActionScrollParams{Direction: direction, Amount: amount}, true
	case "upload":
		return protocol.MethodActionUpload, protocol.ActionUploadParams{ID: resolved.ID, FilePath: value}, true
	default:
		return "", nil, false
	}
}

func (d *daemon) macroStepFailure(ctx context.Context, id *string, macroName string, step protocol.MacroStep, index, total int, reason string) protocol.Response {
	stepID := macroStepID(step, index)
	return protocol.Response{
		JSONRPC: protocol.JSONRPC,
		ID:      id,
		Error: &protocol.RPCError{
			Code:    protocol.ErrMacroStepFailed,
			Message: fmt.Sprintf("macro step %d failed", stepID),
			Data: protocol.MacroStepFailure{
				Status:          "macro_step_failed",
				Macro:           macroName,
				FailedStepID:    stepID,
				Reason:          reason,
				CurrentSnapshot: d.macroSnapshot(ctx),
				RemainingSteps:  total - index - 1,
			},
		},
	}
}

func (d *daemon) macroSnapshot(ctx context.Context) string {
	snapCtx, cancel := context.WithTimeout(ctx, pageResolveTimeout)
	defer cancel()
	result, rpcErr := d.forward(snapCtx, protocol.Request{
		JSONRPC: protocol.JSONRPC,
		Method:  protocol.MethodPageSnap,
		Params:  json.RawMessage(`{}`),
	})
	if rpcErr != nil {
		return ""
	}
	var snap protocol.SnapResult
	if err := json.Unmarshal(result, &snap); err != nil {
		return ""
	}
	return snap.FormattedText
}
