package macro

import (
	"context"
	"fmt"
	"time"

	"github.com/rajeshshrirao/chrome-ctl/internal/protocol"
)

func needsEffectSignature(step protocol.MacroStep) bool {
	return step.Precondition != nil || step.Postcondition != nil || step.Retry != nil || step.Compensate != nil
}

func resolveAttempts(step protocol.MacroStep) int {
	attempts := 1
	if step.OnFailure == "retry_3x" {
		attempts = 3
	}
	if step.Retry != nil && step.Retry.Max > attempts {
		attempts = step.Retry.Max
	}
	return attempts
}

func (s *Saga) resolveStep(ctx context.Context, step protocol.MacroStep) (protocol.PageResolveResult, *protocol.RPCError, int) {
	attempts := resolveAttempts(step)
	result, rpcErr, used := s.resolveStepWithAttempts(ctx, step, attempts)
	return result, rpcErr, used
}

func (s *Saga) resolveStepWithAttempts(ctx context.Context, step protocol.MacroStep, attempts int) (protocol.PageResolveResult, *protocol.RPCError, int) {
	if step.Target == nil {
		return protocol.PageResolveResult{}, nil, 0
	}
	if attempts < 1 {
		attempts = 1
	}
	var last *protocol.RPCError
	used := 0
	for attempt := 1; attempt <= attempts; attempt++ {
		used = attempt
		result, rpcErr := s.Runner.Resolve(ctx, *step.Target)
		if rpcErr == nil && result.ID > 0 {
			return result, nil, attempt
		}
		last = rpcErr
		if last == nil {
			last = &protocol.RPCError{
				Code:    protocol.ErrElementNotFound,
				Message: fmt.Sprintf("element %q not found", macroTargetLabel(step.Target)),
			}
		}
		if step.OnFailure != "retry_3x" && ClassifyRPCError(last) != ClassTransient {
			break
		}
		if attempt < attempts {
			if err := s.sleep(ctx, s.retryDelay(attempt, step.Retry)); err != nil {
				break
			}
		}
	}
	return protocol.PageResolveResult{}, last, used
}

func (s *Saga) executeStep(
	ctx context.Context,
	macroName string,
	step protocol.MacroStep,
	resolved protocol.PageResolveResult,
	url, value string,
	before protocol.MacroEffect,
	haveBefore bool,
	stepID int,
	runID string,
) (protocol.MacroEffect, int, *protocol.RPCError, string) {
	method, payload, ok := BuildAction(step, resolved, url, value)
	if !ok {
		return protocol.MacroEffect{}, 0, &protocol.RPCError{
			Code:    protocol.ErrInvalidRequest,
			Message: fmt.Sprintf("unsupported action %q", step.Action),
		}, ClassPermanent
	}

	maxAttempts := resolveAttempts(step)
	retryOn := retryClasses(step.Retry)
	key := step.IdempotencyKey
	if key == "" {
		key = fmt.Sprintf("%s:%d:%s", macroName, stepID, runID)
	}

	lastEffect := protocol.MacroEffect{}
	for attempt := 1; ; attempt++ {
		execCtx := ctx
		cancel := func() {}
		if step.TimeoutMs > 0 {
			execCtx, cancel = context.WithTimeout(ctx, time.Duration(step.TimeoutMs)*time.Millisecond)
		}
		execCtx = WithIdempotencyKey(execCtx, key)
		rpcErr := s.Runner.Execute(execCtx, method, payload)
		cancel()

		if rpcErr == nil {
			if step.Postcondition != nil {
				observed, matched := s.waitForEffect(ctx, step, resolved, before, haveBefore, step.Postcondition)
				if !matched {
					return observed, attempt, &protocol.RPCError{
						Code:    protocol.ErrDispatchedUnconfirmed,
						Message: "postcondition not met after dispatch",
					}, ClassAmbiguous
				}
				return observed, attempt, nil, ""
			}
			if needsEffectSignature(step) {
				if observed, observeErr := s.Runner.Observe(ctx, resolved.ID, step.Target); observeErr == nil {
					lastEffect = observed
				}
			}
			return lastEffect, attempt, nil, ""
		}

		classification := ClassifyRPCError(rpcErr)
		if classification == ClassAmbiguous {
			// The effect may have landed. Read the signature before retrying.
			observed, landed := s.waitForEffect(ctx, step, resolved, before, haveBefore, step.Postcondition)
			lastEffect = observed
			if landed {
				return observed, attempt, nil, ""
			}
			if !haveBefore && step.Postcondition == nil {
				// Nothing observable to verify: retrying could duplicate the effect.
				return observed, attempt, rpcErr, classification
			}
			if attempt >= maxInt(maxAttempts, 2) {
				return observed, attempt, rpcErr, classification
			}
			if err := s.sleep(ctx, s.retryDelay(attempt, step.Retry)); err != nil {
				return observed, attempt, rpcErr, classification
			}
			continue
		}

		if classification == ClassPermanent || !retryOn[classification] || attempt >= maxAttempts {
			return lastEffect, attempt, rpcErr, classification
		}
		if err := s.sleep(ctx, s.retryDelay(attempt, step.Retry)); err != nil {
			return lastEffect, attempt, rpcErr, classification
		}
	}
}

func (s *Saga) waitForEffect(
	ctx context.Context,
	step protocol.MacroStep,
	resolved protocol.PageResolveResult,
	before protocol.MacroEffect,
	haveBefore bool,
	want *protocol.MacroEffect,
) (protocol.MacroEffect, bool) {
	timeout := s.EffectWait
	if timeout <= 0 {
		timeout = 2 * time.Second
	}
	deadline := s.now().Add(timeout)
	poll := 150 * time.Millisecond
	last := protocol.MacroEffect{}

	for {
		observed, err := s.Runner.Observe(ctx, resolved.ID, step.Target)
		if err == nil {
			last = observed
			if want != nil {
				if Matches(*want, observed) {
					return observed, true
				}
			} else if haveBefore && Changed(before, observed) {
				return observed, true
			}
		}

		remaining := deadline.Sub(s.now())
		if remaining <= 0 {
			return last, false
		}
		delay := poll
		if remaining < delay {
			delay = remaining
		}
		if err := s.sleep(ctx, delay); err != nil {
			return last, false
		}
	}
}

func retryClasses(retry *protocol.MacroRetry) map[string]bool {
	classes := map[string]bool{ClassTransient: true}
	if retry == nil {
		return classes
	}
	for _, name := range retry.RetryOn {
		switch name {
		case ClassTransient:
			classes[ClassTransient] = true
		case ClassAmbiguous:
			classes[ClassAmbiguous] = true
		case ClassPermanent:
			classes[ClassPermanent] = true
		}
	}
	return classes
}

func (s *Saga) retryDelay(attempt int, retry *protocol.MacroRetry) time.Duration {
	base := 150 * time.Millisecond
	capDelay := 2 * time.Second
	if retry != nil {
		if retry.BaseMs > 0 {
			base = time.Duration(retry.BaseMs) * time.Millisecond
		}
		if retry.CapMs > 0 {
			capDelay = time.Duration(retry.CapMs) * time.Millisecond
		}
	}
	delay := base
	for i := 1; i < attempt; i++ {
		delay *= 2
		if delay >= capDelay {
			delay = capDelay
			break
		}
	}
	if delay > capDelay {
		delay = capDelay
	}
	return time.Duration(s.rand() * float64(delay))
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// BuildAction maps a macro step to the wire request it replays.
func BuildAction(step protocol.MacroStep, resolved protocol.PageResolveResult, url, value string) (string, any, bool) {
	options := step.Options
	seed := ""
	if options != nil {
		seed = options.Seed
	}
	switch step.Action {
	case "open":
		return protocol.MethodTabOpen, protocol.TabOpenParams{URL: url}, true
	case "click":
		return protocol.MethodActionClick, protocol.ActionClickParams{
			ID:          resolved.ID,
			DoubleClick: options != nil && options.Double,
			Seed:        seed,
		}, true
	case "type":
		return protocol.MethodActionType, protocol.ActionTypeParams{
			ID:    resolved.ID,
			Text:  value,
			Enter: options != nil && options.Enter,
			Seed:  seed,
		}, true
	case "press":
		var modifiers []string
		if options != nil {
			modifiers = options.Modifiers
		}
		return protocol.MethodActionPress, protocol.ActionPressParams{Key: value, Modifiers: modifiers, Seed: seed}, true
	case "scroll":
		var direction string
		var amount float64
		if options != nil {
			direction = options.Direction
			amount = options.Amount
		}
		return protocol.MethodActionScroll, protocol.ActionScrollParams{Direction: direction, Amount: amount, Seed: seed}, true
	case "upload":
		return protocol.MethodActionUpload, protocol.ActionUploadParams{ID: resolved.ID, FilePath: value}, true
	default:
		return "", nil, false
	}
}
