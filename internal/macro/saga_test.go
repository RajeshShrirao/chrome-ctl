package macro

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rajeshshrirao/chrome-ctl/internal/protocol"
)

type fakeRunner struct {
	resolveID  int
	resolveErr *protocol.RPCError
	effect     protocol.MacroEffect
	execute    func(ctx context.Context, method string, params any) *protocol.RPCError
	executed   []string
}

func (f *fakeRunner) Resolve(_ context.Context, _ protocol.MacroTarget) (protocol.PageResolveResult, *protocol.RPCError) {
	if f.resolveErr != nil {
		return protocol.PageResolveResult{}, f.resolveErr
	}
	return protocol.PageResolveResult{ID: f.resolveID, Label: "target"}, nil
}

func (f *fakeRunner) Execute(ctx context.Context, method string, params any) *protocol.RPCError {
	f.executed = append(f.executed, actionValue(params))
	if f.execute != nil {
		return f.execute(ctx, method, params)
	}
	return nil
}

func (f *fakeRunner) Observe(_ context.Context, _ int, _ *protocol.MacroTarget) (protocol.MacroEffect, error) {
	return f.effect, nil
}

func (f *fakeRunner) Snapshot(_ context.Context) string {
	return "SNAP"
}

func actionValue(params any) string {
	switch typed := params.(type) {
	case protocol.ActionPressParams:
		return typed.Key
	case protocol.ActionTypeParams:
		return typed.Text
	case protocol.ActionClickParams:
		return "click"
	case protocol.ActionScrollParams:
		return "scroll"
	case protocol.TabOpenParams:
		return typed.URL
	case protocol.ActionUploadParams:
		return typed.FilePath
	default:
		return "?"
	}
}

func testSaga(runner *fakeRunner, journal Journal) *Saga {
	return &Saga{
		Runner:     runner,
		Journal:    journal,
		Breakers:   NewBreakers(3, time.Minute, nil),
		EffectWait: 20 * time.Millisecond,
		Rand:       func() float64 { return 1 },
	}
}

func pressStep(id int, key string) protocol.MacroStep {
	return protocol.MacroStep{StepID: id, Action: "press", Value: key, Options: &protocol.MacroStepOptions{}}
}

func TestSagaAmbiguousLandedEffectIsVerifiedNotRetried(t *testing.T) {
	runner := &fakeRunner{effect: protocol.MacroEffect{Value: "done"}}
	runner.execute = func(context.Context, string, any) *protocol.RPCError {
		// The dispatch timed out, but the effect landed.
		return &protocol.RPCError{Code: protocol.ErrDispatchedUnconfirmed, Message: "no ack"}
	}
	step := pressStep(1, "hello")
	step.Retry = &protocol.MacroRetry{Max: 3, BaseMs: 1, CapMs: 1}
	step.Postcondition = &protocol.MacroEffect{Value: "done"}

	file := protocol.MacroFile{Name: "landed", Steps: []protocol.MacroStep{step}}
	result, failure := testSaga(runner, nil).Run(context.Background(), file, map[string]string{}, RunOptions{RunID: "run-landed"})
	if failure != nil {
		t.Fatalf("unexpected failure: %+v", failure)
	}
	if result.StepsRun != 1 {
		t.Fatalf("stepsRun = %d, want 1", result.StepsRun)
	}
	if len(runner.executed) != 1 {
		t.Fatalf("execute calls = %d, want 1 (effect landed, must not retry)", len(runner.executed))
	}
}

func TestSagaAmbiguousMissingEffectRetriesExactlyOnce(t *testing.T) {
	runner := &fakeRunner{effect: protocol.MacroEffect{Value: ""}}
	calls := 0
	runner.execute = func(context.Context, string, any) *protocol.RPCError {
		calls++
		if calls == 1 {
			return &protocol.RPCError{Code: protocol.ErrDispatchedUnconfirmed, Message: "no ack"}
		}
		runner.effect = protocol.MacroEffect{Value: "done"}
		return nil
	}
	step := pressStep(1, "hello")
	step.Retry = &protocol.MacroRetry{Max: 3, BaseMs: 1, CapMs: 1}
	step.Postcondition = &protocol.MacroEffect{Value: "done"}

	file := protocol.MacroFile{Name: "missing", Steps: []protocol.MacroStep{step}}
	result, failure := testSaga(runner, nil).Run(context.Background(), file, map[string]string{}, RunOptions{RunID: "run-missing"})
	if failure != nil {
		t.Fatalf("unexpected failure: %+v", failure)
	}
	if result.StepsRun != 1 {
		t.Fatalf("stepsRun = %d, want 1", result.StepsRun)
	}
	if len(runner.executed) != 2 {
		t.Fatalf("execute calls = %d, want exactly 2 (initial + one retry)", len(runner.executed))
	}
}

func TestSagaCompensatesCompletedStepsInReverse(t *testing.T) {
	runner := &fakeRunner{resolveID: 1}
	runner.execute = func(_ context.Context, _ string, params any) *protocol.RPCError {
		if press, ok := params.(protocol.ActionPressParams); ok && press.Key == "c" {
			return &protocol.RPCError{Code: protocol.ErrElementNotFound, Message: "gone"}
		}
		return nil
	}
	first := pressStep(1, "a")
	first.Compensate = &protocol.MacroStep{Action: "press", Value: "z"}
	second := pressStep(2, "b")
	second.Compensate = &protocol.MacroStep{Action: "press", Value: "y"}
	third := pressStep(3, "c")

	file := protocol.MacroFile{Name: "comp", Steps: []protocol.MacroStep{first, second, third}}
	_, failure := testSaga(runner, nil).Run(context.Background(), file, map[string]string{}, RunOptions{RunID: "run-comp"})
	if failure == nil {
		t.Fatal("expected failure on step 3")
	}
	want := []string{"a", "b", "c", "y", "z"}
	if len(runner.executed) != len(want) {
		t.Fatalf("executed = %v, want %v", runner.executed, want)
	}
	for i := range want {
		if runner.executed[i] != want[i] {
			t.Fatalf("executed = %v, want %v", runner.executed, want)
		}
	}
	if len(failure.Compensated) != 2 || failure.Compensated[0] != 2 || failure.Compensated[1] != 1 {
		t.Fatalf("compensated = %v, want [2 1]", failure.Compensated)
	}
	if failure.Snapshot != "SNAP" {
		t.Fatalf("snapshot = %q, want SNAP", failure.Snapshot)
	}
}

func TestSagaResumesByCompletedStepID(t *testing.T) {
	journal := NewFileJournal(t.TempDir())
	runner := &fakeRunner{resolveID: 1}
	failOnB := true
	runner.execute = func(_ context.Context, _ string, params any) *protocol.RPCError {
		press := params.(protocol.ActionPressParams)
		if press.Key == "b" && failOnB {
			return &protocol.RPCError{Code: protocol.ErrElementNotFound, Message: "not ready"}
		}
		return nil
	}
	file := protocol.MacroFile{Name: "resume", Steps: []protocol.MacroStep{
		pressStep(1, "a"), pressStep(2, "b"), pressStep(3, "c"),
	}}

	if _, failure := testSaga(runner, journal).Run(context.Background(), file, map[string]string{}, RunOptions{RunID: "run-resume"}); failure == nil {
		t.Fatal("first run should fail on b")
	}
	if len(runner.executed) != 2 || runner.executed[0] != "a" || runner.executed[1] != "b" {
		t.Fatalf("first run executed = %v, want [a b]", runner.executed)
	}

	runner.executed = nil
	failOnB = false
	result, failure := testSaga(runner, journal).Run(context.Background(), file, map[string]string{}, RunOptions{RunID: "run-resume", Resume: true})
	if failure != nil {
		t.Fatalf("resume failed: %+v", failure)
	}
	if result.ResumedFrom != 1 {
		t.Fatalf("resumedFrom = %d, want 1", result.ResumedFrom)
	}
	if result.StepsRun != 2 {
		t.Fatalf("stepsRun = %d, want 2", result.StepsRun)
	}
	if len(runner.executed) != 2 || runner.executed[0] != "b" || runner.executed[1] != "c" {
		t.Fatalf("resumed executed = %v, want [b c]", runner.executed)
	}
}

func TestSagaWritesCheckpointAndDeadLetter(t *testing.T) {
	dir := t.TempDir()
	journal := NewFileJournal(dir)
	runner := &fakeRunner{resolveID: 1}
	runner.execute = func(_ context.Context, _ string, params any) *protocol.RPCError {
		if params.(protocol.ActionPressParams).Key == "boom" {
			return &protocol.RPCError{Code: protocol.ErrElementNotFound, Message: "gone"}
		}
		return nil
	}
	file := protocol.MacroFile{Name: "journal", Steps: []protocol.MacroStep{pressStep(1, "ok"), pressStep(2, "boom")}}
	_, failure := testSaga(runner, journal).Run(context.Background(), file, map[string]string{}, RunOptions{RunID: "run-journal"})
	if failure == nil {
		t.Fatal("expected failure")
	}
	if failure.DeadLetter == "" {
		t.Fatal("dead letter path is empty")
	}
	if _, err := os.Stat(failure.DeadLetter); err != nil {
		t.Fatalf("stat dead letter: %v", err)
	}
	records, err := journal.Completed("run-journal")
	if err != nil {
		t.Fatalf("read checkpoint: %v", err)
	}
	if len(records) != 1 || records[0].StepID != 1 || records[0].Status != StatusCompleted {
		t.Fatalf("checkpoint records = %+v, want step 1 completed", records)
	}
	if _, err := os.Stat(filepath.Join(dir, "run-journal.jsonl")); err != nil {
		t.Fatalf("stat checkpoint jsonl: %v", err)
	}
}

func TestClassifyRPCError(t *testing.T) {
	cases := map[int]string{
		protocol.ErrConnection:             ClassTransient,
		protocol.ErrBusy:                   ClassTransient,
		protocol.ErrDispatchedUnconfirmed:  ClassAmbiguous,
		protocol.ErrElementNotFound:        ClassPermanent,
		protocol.ErrElementNotInteractable: ClassPermanent,
	}
	for code, want := range cases {
		if got := ClassifyRPCError(&protocol.RPCError{Code: code}); got != want {
			t.Errorf("ClassifyRPCError(%d) = %q, want %q", code, got, want)
		}
	}
}
