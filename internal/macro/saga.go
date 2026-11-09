package macro

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/rajeshshrirao/chrome-ctl/internal/protocol"
)

// Error classifications used by the saga and reported to callers.
const (
	ClassTransient = "transient"
	ClassPermanent = "permanent"
	ClassAmbiguous = "ambiguous"
)

// Runner is the browser-facing half of a saga. The daemon implements it with
// forwarded JSON-RPC; tests implement it in memory.
type Runner interface {
	Resolve(ctx context.Context, target protocol.MacroTarget) (protocol.PageResolveResult, *protocol.RPCError)
	Execute(ctx context.Context, method string, params any) *protocol.RPCError
	Observe(ctx context.Context, resolvedID int, target *protocol.MacroTarget) (protocol.MacroEffect, error)
	Snapshot(ctx context.Context) string
}

// RunOptions selects a fresh run or resumes an existing checkpoint file.
type RunOptions struct {
	RunID  string
	Resume bool
}

// RunResult describes a successful run.
type RunResult struct {
	RunID       string
	StepsRun    int
	ResumedFrom int
	Checkpoint  string
	Compensated []int
}

// RunFailure describes where and how a run stopped.
type RunFailure struct {
	StepID         int
	Action         string
	Reason         string
	Classification string
	Attempts       int
	CircuitOpen    bool
	DeadLetter     string
	Compensated    []int
	RemainingSteps int
	Snapshot       string
}

// Saga executes a macro file with preconditions, postconditions, retries,
// effect-signature verification, checkpoints, and reverse compensation.
type Saga struct {
	Runner     Runner
	Journal    Journal
	Breakers   *Breakers
	Now        func() time.Time
	Sleep      func(context.Context, time.Duration) error
	Rand       func() float64
	EffectWait time.Duration
}

func (s *Saga) sleep(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		return nil
	}
	if s.Sleep != nil {
		return s.Sleep(ctx, delay)
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *Saga) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *Saga) rand() float64 {
	if s.Rand != nil {
		return s.Rand()
	}
	return 0.5
}

// Run executes file. It returns either a result or a failure, never both.
func (s *Saga) Run(ctx context.Context, file protocol.MacroFile, vars map[string]string, opts RunOptions) (RunResult, *RunFailure) {
	result := RunResult{RunID: opts.RunID}
	if opts.RunID == "" {
		return result, &RunFailure{
			Reason:         "run id is required",
			Classification: ClassPermanent,
		}
	}
	if !ValidName(opts.RunID) {
		return result, &RunFailure{
			Reason:         fmt.Sprintf("invalid run id %q", opts.RunID),
			Classification: ClassPermanent,
		}
	}
	if s.Journal != nil {
		result.Checkpoint = fileCheckpointPath(s.Journal, opts.RunID)
	}

	resumed := map[int]Checkpoint{}
	if opts.Resume && s.Journal != nil {
		records, err := s.Journal.Completed(opts.RunID)
		if err != nil {
			return result, &RunFailure{
				Reason:         "load checkpoint: " + err.Error(),
				Classification: ClassPermanent,
			}
		}
		for _, record := range records {
			resumed[record.StepID] = record
			if record.StepID > result.ResumedFrom {
				result.ResumedFrom = record.StepID
			}
		}
	}

	completed := make([]Checkpoint, 0, len(resumed))
	for index := range file.Steps {
		step := file.Steps[index]
		stepID := macroStepID(step, index)

		if record, ok := resumed[stepID]; ok {
			completed = append(completed, record)
			continue
		}

		allowed, err := macroConditionAllows(step.Condition, vars, file.Parameters)
		if err != nil {
			return result, s.fail(ctx, file, opts.RunID, vars, step, index, ClassPermanent, err.Error(), 0, false, completed)
		}
		if !allowed {
			record := s.checkpoint(file.Name, opts.RunID, stepID, step, StatusSkipped, 0, nil, "")
			if err := s.append(record); err != nil {
				return result, s.fail(ctx, file, opts.RunID, vars, step, index, ClassPermanent, "write checkpoint: "+err.Error(), 0, false, completed)
			}
			continue
		}

		url, value, err := interpolateStep(step, vars)
		if err != nil {
			return result, s.fail(ctx, file, opts.RunID, vars, step, index, ClassPermanent, err.Error(), 0, false, completed)
		}

		breakerKey := breakerKey(file.Name, stepID)
		if !s.Breakers.Allow(breakerKey) {
			return result, s.fail(ctx, file, opts.RunID, vars, step, index, ClassTransient, "circuit breaker open after repeated failures", 0, true, completed)
		}

		resolved, rpcErr, attempts := s.resolveStep(ctx, step)
		if rpcErr != nil {
			s.Breakers.Failure(breakerKey)
			return result, s.fail(ctx, file, opts.RunID, vars, step, index, ClassifyRPCError(rpcErr), resolveReason(step, rpcErr), attempts, false, completed)
		}

		before, haveBefore := protocol.MacroEffect{}, false
		if needsEffectSignature(step) {
			if observed, observeErr := s.Runner.Observe(ctx, resolved.ID, step.Target); observeErr == nil {
				before, haveBefore = observed, true
			}
			if step.Precondition != nil && !Matches(*step.Precondition, before) {
				s.Breakers.Failure(breakerKey)
				return result, s.fail(ctx, file, opts.RunID, vars, step, index, ClassPermanent, "precondition not met", attempts, false, completed)
			}
		}

		effect, attempts, rpcErr, classification := s.executeStep(ctx, file.Name, step, resolved, url, value, before, haveBefore, stepID, opts.RunID)
		if rpcErr != nil {
			s.Breakers.Failure(breakerKey)
			return result, s.fail(ctx, file, opts.RunID, vars, step, index, classification, rpcErr.Message, attempts, false, completed)
		}
		s.Breakers.Success(breakerKey)

		record := s.checkpoint(file.Name, opts.RunID, stepID, step, StatusCompleted, attempts, &effect, "")
		if err := s.append(record); err != nil {
			return result, s.fail(ctx, file, opts.RunID, vars, step, index, ClassPermanent, "write checkpoint: "+err.Error(), attempts, false, completed)
		}
		completed = append(completed, record)
		result.StepsRun++
	}

	return result, nil
}

func (s *Saga) append(record Checkpoint) error {
	if s.Journal == nil {
		return nil
	}
	return s.Journal.Append(record)
}

func (s *Saga) checkpoint(macroName, runID string, stepID int, step protocol.MacroStep, status string, attempts int, effect *protocol.MacroEffect, reason string) Checkpoint {
	return Checkpoint{
		RunID:          runID,
		Macro:          macroName,
		StepID:         stepID,
		Action:         step.Action,
		Status:         status,
		Attempts:       attempts,
		Effect:         effect,
		IdempotencyKey: step.IdempotencyKey,
		Reason:         reason,
		At:             s.now().UTC().Format(time.RFC3339Nano),
	}
}

// fail compensates completed steps in reverse order, writes a dead letter, and
// returns the structured failure.
func (s *Saga) fail(ctx context.Context, file protocol.MacroFile, runID string, vars map[string]string, step protocol.MacroStep, index int, classification, reason string, attempts int, circuitOpen bool, completed []Checkpoint) *RunFailure {
	failure := &RunFailure{
		StepID:         macroStepID(step, index),
		Action:         step.Action,
		Reason:         reason,
		Classification: classification,
		Attempts:       attempts,
		CircuitOpen:    circuitOpen,
		RemainingSteps: len(file.Steps) - index - 1,
	}

	stepByID := make(map[int]protocol.MacroStep, len(file.Steps))
	for position, candidate := range file.Steps {
		stepByID[macroStepID(candidate, position)] = candidate
	}

	for i := len(completed) - 1; i >= 0; i-- {
		record := completed[i]
		candidate, ok := stepByID[record.StepID]
		if !ok || candidate.Compensate == nil {
			continue
		}
		compensated, err := s.compensate(ctx, file, vars, runID, candidate, record.StepID)
		if err != nil {
			_ = s.append(Checkpoint{
				RunID:          record.RunID,
				Macro:          file.Name,
				StepID:         record.StepID,
				Action:         candidate.Compensate.Action,
				Status:         StatusFailed,
				Reason:         err.Error(),
				Classification: ClassPermanent,
				CompensationOf: record.StepID,
				At:             s.now().UTC().Format(time.RFC3339Nano),
			})
			continue
		}
		if compensated {
			failure.Compensated = append(failure.Compensated, record.StepID)
		}
	}

	failure.Snapshot = s.Runner.Snapshot(ctx)
	if writer, ok := s.Journal.(DeadLetterWriter); ok && s.Journal != nil {
		path, err := writer.WriteDeadLetter(DeadLetter{
			RunID:          runID,
			Macro:          file.Name,
			StepID:         failure.StepID,
			Action:         failure.Action,
			Classification: classification,
			Reason:         reason,
			Attempts:       attempts,
			CircuitOpen:    circuitOpen,
			Snapshot:       failure.Snapshot,
			Compensated:    failure.Compensated,
			At:             s.now().UTC().Format(time.RFC3339Nano),
		})
		if err == nil {
			failure.DeadLetter = path
		}
	}
	return failure
}

func (s *Saga) compensate(ctx context.Context, file protocol.MacroFile, vars map[string]string, runID string, step protocol.MacroStep, originalID int) (bool, error) {
	comp := step.Compensate
	if comp == nil {
		return false, nil
	}
	url, value, err := interpolateStep(*comp, vars)
	if err != nil {
		return false, err
	}
	resolved := protocol.PageResolveResult{}
	if comp.Target != nil {
		result, rpcErr, _ := s.resolveStepWithAttempts(ctx, *comp, 1)
		if rpcErr != nil {
			return false, errors.New(rpcErr.Message)
		}
		resolved = result
	}
	method, payload, ok := BuildAction(*comp, resolved, url, value)
	if !ok {
		return false, fmt.Errorf("unsupported compensation action %q", comp.Action)
	}

	execCtx := ctx
	cancel := func() {}
	if comp.TimeoutMs > 0 {
		execCtx, cancel = context.WithTimeout(ctx, time.Duration(comp.TimeoutMs)*time.Millisecond)
	}
	defer cancel()
	execCtx = WithIdempotencyKey(execCtx, compensationKey(file.Name, originalID))
	if rpcErr := s.Runner.Execute(execCtx, method, payload); rpcErr != nil {
		return false, errors.New(rpcErr.Message)
	}
	_ = s.append(Checkpoint{
		RunID:          runID,
		Macro:          file.Name,
		StepID:         originalID,
		Action:         comp.Action,
		Status:         StatusCompensated,
		CompensationOf: originalID,
		At:             s.now().UTC().Format(time.RFC3339Nano),
	})
	return true, nil
}

func fileCheckpointPath(journal Journal, runID string) string {
	if fileJournal, ok := journal.(*FileJournal); ok {
		return fileJournal.checkpointPath(runID)
	}
	return ""
}

func breakerKey(macroName string, stepID int) string {
	return fmt.Sprintf("%s#%d", macroName, stepID)
}

func compensationKey(macroName string, stepID int) string {
	return fmt.Sprintf("%s#%d#compensate", macroName, stepID)
}

func interpolateStep(step protocol.MacroStep, vars map[string]string) (string, string, error) {
	url, err := Interpolate(step.URL, vars)
	if err != nil {
		return "", "", fmt.Errorf("macro step %d: %w", step.StepID, err)
	}
	value, err := Interpolate(step.Value, vars)
	if err != nil {
		return "", "", fmt.Errorf("macro step %d: %w", step.StepID, err)
	}
	return url, value, nil
}

func resolveReason(step protocol.MacroStep, rpcErr *protocol.RPCError) string {
	if rpcErr.Code == protocol.ErrElementNotFound {
		if label := macroTargetLabel(step.Target); label != "" {
			return fmt.Sprintf("element %q not found: %s", label, rpcErr.Message)
		}
	}
	return rpcErr.Message
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

// ClassifyRPCError maps a wire error to a saga retry class. It never inspects
// the human-readable message.
func ClassifyRPCError(rpcErr *protocol.RPCError) string {
	if rpcErr == nil {
		return ""
	}
	switch rpcErr.Code {
	case protocol.ErrDispatchedUnconfirmed:
		return ClassAmbiguous
	case protocol.ErrConnection,
		protocol.ErrBusy,
		protocol.ErrDebuggerDetached,
		protocol.ErrSnapshotStale,
		protocol.ErrNavigationFailed,
		protocol.ErrInternal:
		return ClassTransient
	case protocol.ErrElementNotFound,
		protocol.ErrElementNotInteractable,
		protocol.ErrTabNotFound,
		protocol.ErrSSOTimeout,
		protocol.ErrUnauthorized,
		protocol.ErrFeatureDisabled,
		protocol.ErrInvalidRequest,
		protocol.ErrMethodNotFound:
		return ClassPermanent
	default:
		return ClassPermanent
	}
}

// idempotencyKeyKey carries a stable retry key through context without changing
// the wire parameter shape. The daemon's scheduler reads it.
type idempotencyKeyKey struct{}

func WithIdempotencyKey(ctx context.Context, key string) context.Context {
	if key == "" {
		return ctx
	}
	return context.WithValue(ctx, idempotencyKeyKey{}, key)
}

func IdempotencyKey(ctx context.Context) string {
	key, _ := ctx.Value(idempotencyKeyKey{}).(string)
	return key
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

	expanded, err := Interpolate(condition, scope)
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

var conditionPattern = regexp.MustCompile(`(?s)^(.*?)\s*(==|!=)\s*''\s*$`)

// macroStepID returns the explicit step id or its 1-based position.
func macroStepID(step protocol.MacroStep, index int) int {
	if step.StepID > 0 {
		return step.StepID
	}
	return index + 1
}
