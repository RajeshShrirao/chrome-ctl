package macro

import (
	"sync"
	"time"
)

// Breakers is a per-step circuit breaker shared across runs in one daemon.
type Breakers struct {
	mu        sync.Mutex
	entries   map[string]breakerEntry
	threshold int
	cooldown  time.Duration
	now       func() time.Time
}

type breakerEntry struct {
	failures  int
	openUntil time.Time
	probing   bool
}

// NewBreakers creates a breaker set. A zero threshold or cooldown selects the
// defaults (3 consecutive failures, 30s cooldown).
func NewBreakers(threshold int, cooldown time.Duration, now func() time.Time) *Breakers {
	if threshold <= 0 {
		threshold = 3
	}
	if cooldown <= 0 {
		cooldown = 30 * time.Second
	}
	if now == nil {
		now = time.Now
	}
	return &Breakers{
		entries:   make(map[string]breakerEntry),
		threshold: threshold,
		cooldown:  cooldown,
		now:       now,
	}
}

// Allow reports whether a step may run. After the cooldown it grants exactly
// one half-open probe.
func (b *Breakers) Allow(key string) bool {
	if b == nil {
		return true
	}
	b.mu.Lock()
	defer b.mu.Unlock()

	entry, ok := b.entries[key]
	if !ok || entry.openUntil.IsZero() {
		return true
	}
	now := b.now()
	if now.Before(entry.openUntil) {
		return false
	}
	if entry.probing {
		return false
	}
	entry.probing = true
	b.entries[key] = entry
	return true
}

// Success resets a step's consecutive-failure count.
func (b *Breakers) Success(key string) {
	if b == nil {
		return
	}
	b.mu.Lock()
	delete(b.entries, key)
	b.mu.Unlock()
}

// Failure records one failed attempt and opens the breaker at the threshold.
func (b *Breakers) Failure(key string) {
	if b == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()

	entry := b.entries[key]
	entry.failures++
	entry.probing = false
	if entry.failures >= b.threshold || !entry.openUntil.IsZero() {
		entry.openUntil = b.now().Add(b.cooldown)
	}
	b.entries[key] = entry
}
