package daemon

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/rajeshshrirao/chrome-ctl/internal/config"
	"github.com/rajeshshrirao/chrome-ctl/internal/macro"
	"github.com/rajeshshrirao/chrome-ctl/internal/protocol"
)

const (
	macroResolveTimeoutMs = 3000
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

	runID := params.RunID
	if runID == "" {
		generated, err := newMacroRunID()
		if err != nil {
			return protocol.Failure(req.ID, protocol.ErrInternal, "generate run id: "+err.Error())
		}
		runID = generated
	}

	saga := macro.Saga{
		Runner:   &macroRunner{d: d},
		Journal:  macro.NewFileJournal(filepath.Join(d.macroDir(), ".runs")),
		Breakers: d.macroBreakers,
	}
	start := time.Now()
	result, failure := saga.Run(ctx, file, vars, macro.RunOptions{RunID: runID, Resume: params.Resume})
	if failure != nil {
		return d.macroRunFailure(req.ID, file.Name, runID, failure)
	}

	return protocol.Success(req.ID, protocol.MacroRunResult{
		Macro:       file.Name,
		RunID:       runID,
		StepsRun:    result.StepsRun,
		DurationMs:  time.Since(start).Milliseconds(),
		Completed:   true,
		ResumedFrom: result.ResumedFrom,
		Checkpoint:  result.Checkpoint,
		Compensated: result.Compensated,
	})
}

func newMacroRunID() (string, error) {
	var raw [8]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw[:]), nil
}

// macroRunner adapts the daemon's forwarded RPCs to the saga contract.
type macroRunner struct {
	d *daemon
}

func (r *macroRunner) Resolve(ctx context.Context, target protocol.MacroTarget) (protocol.PageResolveResult, *protocol.RPCError) {
	return r.d.resolveMacroTarget(ctx, target)
}

func (r *macroRunner) Execute(ctx context.Context, method string, params any) *protocol.RPCError {
	raw, err := json.Marshal(params)
	if err != nil {
		return &protocol.RPCError{Code: protocol.ErrInternal, Message: "encode macro step params"}
	}
	_, rpcErr := r.d.forward(ctx, protocol.Request{
		JSONRPC: protocol.JSONRPC,
		Method:  method,
		Params:  raw,
	})
	return rpcErr
}

// Observe reads the effect signature: URL, matching target count, and the
// resolved element's value/checked/disabled state.
func (r *macroRunner) Observe(ctx context.Context, resolvedID int, target *protocol.MacroTarget) (protocol.MacroEffect, error) {
	snapCtx, cancel := context.WithTimeout(ctx, pageResolveTimeout)
	defer cancel()
	result, rpcErr := r.d.forward(snapCtx, protocol.Request{
		JSONRPC: protocol.JSONRPC,
		Method:  protocol.MethodPageSnap,
		Params:  json.RawMessage(`{}`),
	})
	if rpcErr != nil {
		return protocol.MacroEffect{}, errors.New(rpcErr.Message)
	}
	var snap protocol.SnapResult
	if err := json.Unmarshal(result, &snap); err != nil {
		return protocol.MacroEffect{}, err
	}

	effect := protocol.MacroEffect{URL: snap.URL}
	if target != nil {
		count := 0
		for _, element := range snap.Elements {
			if snapshotElementMatches(element, target) {
				count++
			}
		}
		effect.TargetCount = &count
	}
	for _, element := range snap.Elements {
		if element.ID != resolvedID {
			continue
		}
		effect.Value = element.Value
		checked := strings.Contains(element.State, "checked")
		disabled := strings.Contains(element.State, "disabled")
		effect.Checked = &checked
		effect.Disabled = &disabled
	}

	r.refineFromFormState(ctx, &effect, target)
	return effect, nil
}

func (r *macroRunner) refineFromFormState(ctx context.Context, effect *protocol.MacroEffect, target *protocol.MacroTarget) {
	if target == nil {
		return
	}
	formCtx, cancel := context.WithTimeout(ctx, pageResolveTimeout)
	defer cancel()
	result, rpcErr := r.d.forward(formCtx, protocol.Request{
		JSONRPC: protocol.JSONRPC,
		Method:  protocol.MethodPageFormState,
		Params:  json.RawMessage(`{}`),
	})
	if rpcErr != nil {
		return
	}
	var form protocol.PageFormStateResult
	if err := json.Unmarshal(result, &form); err != nil {
		return
	}
	label := macroTargetLabel(target)
	for _, control := range form.Controls {
		if label == "" || control.Label != label {
			continue
		}
		effect.Value = control.Value
		checked := control.Checked
		disabled := control.Disabled
		effect.Checked = &checked
		effect.Disabled = &disabled
		return
	}
}

func snapshotElementMatches(element protocol.SnapElement, target *protocol.MacroTarget) bool {
	if target.Tag != "" && !strings.EqualFold(element.Tag, target.Tag) {
		return false
	}
	if target.AriaLabel != "" && element.Label == target.AriaLabel {
		return true
	}
	if target.Text != "" && element.Label == target.Text {
		return true
	}
	return false
}

func (r *macroRunner) Snapshot(ctx context.Context) string {
	return r.d.macroSnapshot(ctx)
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

func (d *daemon) macroRunFailure(id *string, macroName, runID string, failure *macro.RunFailure) protocol.Response {
	message := "macro run failed"
	if failure.StepID > 0 {
		message = fmt.Sprintf("macro step %d failed", failure.StepID)
	}
	return protocol.Response{
		JSONRPC: protocol.JSONRPC,
		ID:      id,
		Error: &protocol.RPCError{
			Code:    protocol.ErrMacroStepFailed,
			Message: message,
			Data: protocol.MacroStepFailure{
				Status:          "macro_step_failed",
				Macro:           macroName,
				RunID:           runID,
				FailedStepID:    failure.StepID,
				Reason:          failure.Reason,
				Classification:  failure.Classification,
				Attempts:        failure.Attempts,
				CircuitOpen:     failure.CircuitOpen,
				DeadLetter:      failure.DeadLetter,
				Compensated:     failure.Compensated,
				CurrentSnapshot: failure.Snapshot,
				RemainingSteps:  failure.RemainingSteps,
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
