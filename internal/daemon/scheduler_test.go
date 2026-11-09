package daemon

import (
	"context"
	"encoding/json"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rajeshshrirao/chrome-ctl/internal/protocol"
)

func waitQueueLen(t *testing.T, gate *tabGate, want int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		gate.mu.Lock()
		size := len(gate.queue)
		gate.mu.Unlock()
		if size >= want {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("gate queue did not reach %d waiters", want)
}

func TestMutatingRequestsSerializePerTab(t *testing.T) {
	s := newScheduler()
	ctx := context.Background()

	var active int32
	var overlapped atomic.Bool
	var wg sync.WaitGroup
	for index := 0; index < 8; index += 1 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			release, rpcErr := s.acquire(ctx, "tab:1", true)
			if rpcErr != nil {
				t.Errorf("acquire: %s", rpcErr.Message)
				return
			}
			if atomic.AddInt32(&active, 1) > 1 {
				overlapped.Store(true)
			}
			time.Sleep(2 * time.Millisecond)
			atomic.AddInt32(&active, -1)
			release()
		}()
	}
	wg.Wait()
	if overlapped.Load() {
		t.Fatal("mutating requests overlapped on the same tab")
	}
}

func TestReadsRunConcurrently(t *testing.T) {
	s := newScheduler()
	ctx := context.Background()

	first, rpcErr := s.acquire(ctx, "tab:1", false)
	if rpcErr != nil {
		t.Fatalf("first read: %s", rpcErr.Message)
	}
	defer first()

	done := make(chan struct{})
	go func() {
		release, err := s.acquire(ctx, "tab:1", false)
		if err == nil {
			release()
		}
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("a second read did not run concurrently")
	}
}

func TestMutationDoesNotJumpQueuedReads(t *testing.T) {
	s := newScheduler()
	ctx := context.Background()
	key := "tab:7"
	gate := s.gate(key)

	readRelease, rpcErr := s.acquire(ctx, key, false)
	if rpcErr != nil {
		t.Fatalf("read: %s", rpcErr.Message)
	}

	writeAcquired := make(chan struct{})
	writeRelease := make(chan struct{})
	go func() {
		release, err := s.acquire(ctx, key, true)
		if err != nil {
			close(writeAcquired)
			return
		}
		close(writeAcquired)
		<-writeRelease
		release()
	}()
	waitQueueLen(t, gate, 1)

	read2Acquired := make(chan struct{})
	go func() {
		release, err := s.acquire(ctx, key, false)
		if err == nil {
			defer release()
		}
		close(read2Acquired)
		<-writeRelease
	}()
	waitQueueLen(t, gate, 2)

	select {
	case <-read2Acquired:
		t.Fatal("a read overtook the queued mutation")
	default:
	}

	readRelease()
	select {
	case <-writeAcquired:
	case <-time.After(time.Second):
		t.Fatal("mutation never acquired after the reader released")
	}

	select {
	case <-read2Acquired:
		t.Fatal("a read acquired while the mutation was active")
	default:
	}

	close(writeRelease)
	select {
	case <-read2Acquired:
	case <-time.After(time.Second):
		t.Fatal("queued read never acquired after the mutation released")
	}
}

func TestQueueLimitReturnsBusy(t *testing.T) {
	s := newScheduler()
	ctx := context.Background()
	key := "tab:busy"

	hold, rpcErr := s.acquire(ctx, key, true)
	if rpcErr != nil {
		t.Fatalf("hold: %s", rpcErr.Message)
	}
	defer hold()

	release := make(chan struct{})
	defer close(release)
	for index := 0; index < schedulerQueueLimit; index += 1 {
		go func() {
			got, err := s.acquire(ctx, key, false)
			if err == nil {
				defer got()
			}
			<-release
		}()
	}
	waitQueueLen(t, s.gate(key), schedulerQueueLimit)

	_, rpcErr = s.acquire(ctx, key, false)
	if rpcErr == nil {
		t.Fatal("expected ErrBusy when the queue is full")
	}
	if rpcErr.Code != protocol.ErrBusy {
		t.Fatalf("code = %d, want %d", rpcErr.Code, protocol.ErrBusy)
	}
}

func TestQueuedAcquireHonoursCancellation(t *testing.T) {
	s := newScheduler()
	key := "tab:cancel"

	hold, rpcErr := s.acquire(context.Background(), key, true)
	if rpcErr != nil {
		t.Fatalf("hold: %s", rpcErr.Message)
	}
	defer hold()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan *protocol.RPCError, 1)
	go func() {
		_, err := s.acquire(ctx, key, false)
		done <- err
	}()
	waitQueueLen(t, s.gate(key), 1)
	cancel()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("cancelled acquire returned success")
		}
	case <-time.After(time.Second):
		t.Fatal("cancelled acquire never returned")
	}
}

func TestSingleFlightSharesOneCall(t *testing.T) {
	s := newScheduler()
	var calls int32
	gate := make(chan struct{})
	fn := func() (json.RawMessage, *protocol.RPCError) {
		atomic.AddInt32(&calls, 1)
		<-gate
		return json.RawMessage(`{"value":1}`), nil
	}

	results := make(chan json.RawMessage, 2)
	for index := 0; index < 2; index += 1 {
		go func() {
			result, _ := s.singleFlight(context.Background(), "snap:1", fn)
			results <- result
		}()
	}

	deadline := time.Now().Add(time.Second)
	for atomic.LoadInt32(&calls) == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	close(gate)

	for index := 0; index < 2; index += 1 {
		select {
		case result := <-results:
			if string(result) != `{"value":1}` {
				t.Fatalf("result = %s", result)
			}
		case <-time.After(time.Second):
			t.Fatal("waiter never received the shared result")
		}
	}
	if atomic.LoadInt32(&calls) != 1 {
		t.Fatalf("fn called %d times, want 1", calls)
	}
}

func TestMutationClearsCachedReads(t *testing.T) {
	s := newScheduler()
	var calls int32
	fn := func() (json.RawMessage, *protocol.RPCError) {
		atomic.AddInt32(&calls, 1)
		return json.RawMessage(`{"value":1}`), nil
	}

	if _, rpcErr := s.singleFlight(context.Background(), "tab:1\x00page.snap", fn); rpcErr != nil {
		t.Fatalf("first snap: %s", rpcErr.Message)
	}
	s.clearCache()
	if _, rpcErr := s.singleFlight(context.Background(), "tab:1\x00page.snap", fn); rpcErr != nil {
		t.Fatalf("snap after mutation: %s", rpcErr.Message)
	}
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Fatalf("fn called %d times, want 2 after clearing the cache", got)
	}
}

func TestSingleFlightCacheExpires(t *testing.T) {
	s := newScheduler()
	now := time.Unix(0, 0)
	s.now = func() time.Time { return now }

	var calls int32
	fn := func() (json.RawMessage, *protocol.RPCError) {
		atomic.AddInt32(&calls, 1)
		return json.RawMessage(`{"value":1}`), nil
	}

	if _, rpcErr := s.singleFlight(context.Background(), "read:1", fn); rpcErr != nil {
		t.Fatalf("first call: %s", rpcErr.Message)
	}
	if _, rpcErr := s.singleFlight(context.Background(), "read:1", fn); rpcErr != nil {
		t.Fatalf("cached call: %s", rpcErr.Message)
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("fn called %d times within TTL, want 1", got)
	}

	now = now.Add(s.ttl + time.Millisecond)
	if _, rpcErr := s.singleFlight(context.Background(), "read:1", fn); rpcErr != nil {
		t.Fatalf("expired call: %s", rpcErr.Message)
	}
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Fatalf("fn called %d times after TTL, want 2", got)
	}
}

func TestIdempotentRunsMutationOncePerKey(t *testing.T) {
	s := newScheduler()
	var calls int32
	fn := func() (json.RawMessage, *protocol.RPCError) {
		atomic.AddInt32(&calls, 1)
		return json.RawMessage(`{"ok":true}`), nil
	}

	first, rpcErr := s.idempotent("tab:1\x00action.click\x00key-1", fn)
	if rpcErr != nil {
		t.Fatalf("first call: %s", rpcErr.Message)
	}
	second, rpcErr := s.idempotent("tab:1\x00action.click\x00key-1", fn)
	if rpcErr != nil {
		t.Fatalf("second call: %s", rpcErr.Message)
	}
	if string(first) != `{"ok":true}` || string(second) != `{"ok":true}` {
		t.Fatalf("results = %s / %s", first, second)
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("fn called %d times, want 1 for a repeated idempotency key", got)
	}

	// A different key must run again.
	if _, rpcErr := s.idempotent("tab:1\x00action.click\x00key-2", fn); rpcErr != nil {
		t.Fatalf("different key: %s", rpcErr.Message)
	}
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Fatalf("fn called %d times, want 2 after a new key", got)
	}
}

func TestIdempotentExpiresAfterTTL(t *testing.T) {
	s := newScheduler()
	now := time.Unix(0, 0)
	s.now = func() time.Time { return now }

	var calls int32
	fn := func() (json.RawMessage, *protocol.RPCError) {
		atomic.AddInt32(&calls, 1)
		return json.RawMessage(`{"ok":true}`), nil
	}

	if _, rpcErr := s.idempotent("tab:1\x00action.type\x00key", fn); rpcErr != nil {
		t.Fatalf("first call: %s", rpcErr.Message)
	}
	now = now.Add(idempotencyTTL + time.Millisecond)
	if _, rpcErr := s.idempotent("tab:1\x00action.type\x00key", fn); rpcErr != nil {
		t.Fatalf("call after TTL: %s", rpcErr.Message)
	}
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Fatalf("fn called %d times, want 2 after the idempotency window", got)
	}
}

func TestIdempotentDoesNotCacheFailures(t *testing.T) {
	s := newScheduler()
	var calls int32
	fn := func() (json.RawMessage, *protocol.RPCError) {
		if atomic.AddInt32(&calls, 1) == 1 {
			return nil, &protocol.RPCError{Code: protocol.ErrBusy, Message: "busy"}
		}
		return json.RawMessage(`{"ok":true}`), nil
	}

	if _, rpcErr := s.idempotent("tab:1\x00action.click\x00key", fn); rpcErr == nil {
		t.Fatal("first call should fail")
	}
	result, rpcErr := s.idempotent("tab:1\x00action.click\x00key", fn)
	if rpcErr != nil {
		t.Fatalf("retry should succeed: %s", rpcErr.Message)
	}
	if string(result) != `{"ok":true}` {
		t.Fatalf("result = %s", result)
	}
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Fatalf("fn called %d times, want 2 (errors must not be cached)", got)
	}
}

func TestScheduleForClassifiesMethods(t *testing.T) {
	cases := []struct {
		method string
		params string
		key    string
		write  bool
		single bool
	}{
		{protocol.MethodPageSnap, `{"tabId":5}`, "tab:5", false, true},
		{protocol.MethodPageRead, `{"tabId":5}`, "tab:5", false, true},
		{protocol.MethodPageDescribe, `{"tabId":5}`, "tab:5", false, false},
		{protocol.MethodActionClick, `{"tabId":5}`, "tab:5", true, false},
		{protocol.MethodTabOpen, `{"url":"https://example.test"}`, "global", true, false},
		{"custom.method", `{}`, "global", true, false},
	}
	for _, testCase := range cases {
		plan := scheduleFor(testCase.method, json.RawMessage(testCase.params))
		if plan.key != testCase.key || plan.write != testCase.write || plan.singleFlight != testCase.single {
			t.Errorf("%s: got %+v, want key=%s write=%v single=%v",
				testCase.method, plan, testCase.key, testCase.write, testCase.single)
		}
	}
}
