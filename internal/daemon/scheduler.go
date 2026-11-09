package daemon

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/rajeshshrirao/chrome-ctl/internal/protocol"
)

const (
	// A full per-tab queue means the caller should retry rather than grow memory.
	schedulerQueueLimit = 128
	// Completed read results are reusable for a short window so bursts of
	// identical polls (an agent loop) share one extension round-trip.
	singleFlightTTL = 200 * time.Millisecond
	// A mutation retried with the same idempotency key must not run twice.
	idempotencyTTL = 30 * time.Second
)

// methodSchedule is the scheduling plan for one RPC.
type methodSchedule struct {
	key          string
	write        bool
	singleFlight bool
}

// isMutating reports whether a method must not interleave with any other work
// on the same tab. Unknown methods are treated as mutations for safety.
func isMutating(method string) bool {
	if strings.HasPrefix(method, "action.") {
		return true
	}
	switch method {
	case protocol.MethodTabOpen,
		protocol.MethodTabSwitch,
		protocol.MethodWindowCreate,
		protocol.MethodWindowEnsure,
		protocol.MethodWindowUse,
		protocol.MethodWindowClose,
		protocol.MethodPageEval,
		protocol.MethodAuthSSO,
		protocol.MethodMediaRecStart,
		protocol.MethodMediaRecStop,
		protocol.MethodMacroStart,
		protocol.MethodMacroSave,
		protocol.MethodMacroRun:
		return true
	}
	switch method {
	case protocol.MethodSystemReady,
		protocol.MethodSystemPing,
		protocol.MethodSessionStatus,
		protocol.MethodWindowList,
		protocol.MethodTabList,
		protocol.MethodPageSnap,
		protocol.MethodPageRead,
		protocol.MethodPageWait,
		protocol.MethodPageAttr,
		protocol.MethodPageNet,
		protocol.MethodPageFormState,
		protocol.MethodPageDescribe,
		protocol.MethodPageResolve,
		protocol.MethodMediaShot,
		protocol.MethodMediaCapture,
		protocol.MethodMacroList:
		return false
	}
	return true
}

func scheduleFor(method string, params json.RawMessage) methodSchedule {
	var probe struct {
		TabID *int `json:"tabId"`
	}
	if len(params) > 0 {
		_ = json.Unmarshal(params, &probe)
	}
	key := "global"
	if probe.TabID != nil {
		key = fmt.Sprintf("tab:%d", *probe.TabID)
	}
	return methodSchedule{
		key:   key,
		write: isMutating(method),
		singleFlight: method == protocol.MethodPageSnap ||
			method == protocol.MethodPageRead,
	}
}

// gateWaiter is one queued acquisition. `granted` closes `ready` exactly once.
type gateWaiter struct {
	write   bool
	granted bool
	ready   chan struct{}
}

// tabGate is a FIFO reader/writer gate: reads run together, a mutation is
// exclusive, and neither class can starve the other.
type tabGate struct {
	mu      sync.Mutex
	readers int
	writer  bool
	queue   []*gateWaiter
}

func (g *tabGate) acquire(ctx context.Context, write bool, limit int) (func(), *protocol.RPCError) {
	g.mu.Lock()
	if len(g.queue) >= limit {
		g.mu.Unlock()
		return nil, &protocol.RPCError{
			Code:    protocol.ErrBusy,
			Message: "tab is busy: too many queued requests; retry shortly",
		}
	}
	waiter := &gateWaiter{write: write, ready: make(chan struct{})}
	g.queue = append(g.queue, waiter)
	g.grantNextLocked()
	g.mu.Unlock()

	select {
	case <-waiter.ready:
		return func() { g.release(write) }, nil
	case <-ctx.Done():
		g.mu.Lock()
		if waiter.granted {
			g.releaseLocked(write)
		} else {
			g.removeLocked(waiter)
		}
		g.mu.Unlock()
		return nil, &protocol.RPCError{
			Code:    protocol.ErrInternal,
			Message: "request cancelled while waiting for the tab",
		}
	}
}

func (g *tabGate) release(write bool) {
	g.mu.Lock()
	g.releaseLocked(write)
	g.mu.Unlock()
}

func (g *tabGate) releaseLocked(write bool) {
	if write {
		g.writer = false
	} else if g.readers > 0 {
		g.readers -= 1
	}
	g.grantNextLocked()
}

func (g *tabGate) removeLocked(waiter *gateWaiter) {
	for index, queued := range g.queue {
		if queued == waiter {
			g.queue = append(g.queue[:index], g.queue[index+1:]...)
			return
		}
	}
}

// grantNextLocked wakes the longest-waiting compatible waiters.
func (g *tabGate) grantNextLocked() {
	for len(g.queue) > 0 {
		head := g.queue[0]
		if head.write {
			if g.writer || g.readers > 0 {
				return
			}
			g.queue = g.queue[1:]
			head.granted = true
			g.writer = true
			close(head.ready)
			return
		}
		if g.writer {
			return
		}
		// A read at the head lets every queued read ahead of the next mutation
		// through together.
		for len(g.queue) > 0 && !g.queue[0].write {
			reader := g.queue[0]
			g.queue = g.queue[1:]
			reader.granted = true
			g.readers += 1
			close(reader.ready)
		}
		return
	}
}

type cachedResult struct {
	result  json.RawMessage
	expires time.Time
}

type flightCall struct {
	done   chan struct{}
	result json.RawMessage
	err    *protocol.RPCError
}

// scheduler owns the per-tab gates and the single-flight result cache.
type scheduler struct {
	mu              sync.Mutex
	gates           map[string]*tabGate
	flights         map[string]*flightCall
	cache           map[string]cachedResult
	idempotentCache map[string]cachedResult
	now             func() time.Time
	ttl             time.Duration
}

func newScheduler() *scheduler {
	return &scheduler{
		gates:           make(map[string]*tabGate),
		flights:         make(map[string]*flightCall),
		cache:           make(map[string]cachedResult),
		idempotentCache: make(map[string]cachedResult),
		now:             time.Now,
		ttl:             singleFlightTTL,
	}
}

func (s *scheduler) gate(key string) *tabGate {
	s.mu.Lock()
	defer s.mu.Unlock()
	gate := s.gates[key]
	if gate == nil {
		gate = &tabGate{}
		s.gates[key] = gate
	}
	return gate
}

// acquire takes the per-tab gate for the duration of one extension round-trip.
func (s *scheduler) acquire(ctx context.Context, key string, write bool) (func(), *protocol.RPCError) {
	return s.gate(key).acquire(ctx, write, schedulerQueueLimit)
}

// singleFlight runs fn once for identical concurrent keys and reuses the result
// for a short TTL. Waiters share the leader's raw result.
func (s *scheduler) singleFlight(
	ctx context.Context,
	key string,
	fn func() (json.RawMessage, *protocol.RPCError),
) (json.RawMessage, *protocol.RPCError) {
	s.mu.Lock()
	if cached, ok := s.cache[key]; ok && s.now().Before(cached.expires) {
		s.mu.Unlock()
		return cached.result, nil
	}
	if call, ok := s.flights[key]; ok {
		s.mu.Unlock()
		select {
		case <-call.done:
			return call.result, call.err
		case <-ctx.Done():
			return nil, &protocol.RPCError{
				Code:    protocol.ErrInternal,
				Message: "request cancelled while waiting for an in-flight result",
			}
		}
	}
	call := &flightCall{done: make(chan struct{})}
	s.flights[key] = call
	s.mu.Unlock()

	result, rpcErr := fn()

	s.mu.Lock()
	call.result = result
	call.err = rpcErr
	delete(s.flights, key)
	if rpcErr == nil {
		s.cache[key] = cachedResult{result: result, expires: s.now().Add(s.ttl)}
		s.pruneLocked()
	}
	s.mu.Unlock()
	close(call.done)

	return result, rpcErr
}

// idempotent runs a mutation once per key. A retry that arrives after the
// first call completed receives the cached result instead of repeating the
// effect. Collisions are avoided by including the method in the key.
func (s *scheduler) idempotent(
	key string,
	fn func() (json.RawMessage, *protocol.RPCError),
) (json.RawMessage, *protocol.RPCError) {
	s.mu.Lock()
	if cached, ok := s.idempotentCache[key]; ok && s.now().Before(cached.expires) {
		s.mu.Unlock()
		return cached.result, nil
	}
	s.mu.Unlock()

	result, rpcErr := fn()
	if rpcErr != nil {
		return result, rpcErr
	}

	s.mu.Lock()
	s.idempotentCache[key] = cachedResult{result: result, expires: s.now().Add(idempotencyTTL)}
	if len(s.idempotentCache) > 256 {
		now := s.now()
		for candidate, cached := range s.idempotentCache {
			if !now.Before(cached.expires) {
				delete(s.idempotentCache, candidate)
			}
		}
	}
	s.mu.Unlock()
	return result, nil
}

// clearCache drops every cached read result. It is called after a mutation so
// a snapshot taken before a click cannot be served after it.
func (s *scheduler) clearCache() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.cache) > 0 {
		s.cache = make(map[string]cachedResult)
	}
}

func (s *scheduler) pruneLocked() {
	if len(s.cache) <= 256 {
		return
	}
	now := s.now()
	for key, cached := range s.cache {
		if !now.Before(cached.expires) {
			delete(s.cache, key)
		}
	}
}

// singleFlightKey identifies one logical read: tab, method, and exact params.
func singleFlightKey(plan methodSchedule, req protocol.Request) string {
	sum := sha256.Sum256(req.Params)
	return plan.key + "\x00" + req.Method + "\x00" + hex.EncodeToString(sum[:])
}
