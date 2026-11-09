// Package bulk provides the politeness, retry, and dedupe primitives behind
// `chrome-ctl bulk`. Everything here is pure and injectable so the crawler's
// failure modes can be tested without a browser.
package bulk

import (
	"math"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/rajeshshrirao/chrome-ctl/internal/protocol"
)

// RateController is AIMD pacing for one host: add `Alpha` on success, halve on
// failure, clamped to [MinRate, MaxRate] requests per second.
type RateController struct {
	MinRate float64
	MaxRate float64
	Alpha   float64

	mu   sync.Mutex
	rate float64
}

func NewRateController(minRate, maxRate, startRate, alpha float64) *RateController {
	if minRate <= 0 {
		minRate = 0.1
	}
	if maxRate < minRate {
		maxRate = minRate
	}
	if startRate < minRate || startRate > maxRate {
		startRate = maxRate
	}
	if alpha <= 0 {
		alpha = 0.25
	}
	return &RateController{
		MinRate: minRate,
		MaxRate: maxRate,
		Alpha:   alpha,
		rate:    startRate,
	}
}

func (r *RateController) Rate() float64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.rate
}

func (r *RateController) OnSuccess() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.rate = math.Min(r.MaxRate, r.rate+r.Alpha)
}

func (r *RateController) OnFailure() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.rate = math.Max(r.MinRate, r.rate*0.5)
}

// Delay is the minimum spacing between two requests to this host.
func (r *RateController) Delay() time.Duration {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.rate <= 0 {
		return 0
	}
	return time.Duration(float64(time.Second) / r.rate)
}

// Breaker opens after Threshold consecutive failures, waits Cooldown, then lets
// exactly one half-open probe through before deciding.
type Breaker struct {
	Threshold int
	Cooldown  time.Duration

	mu        sync.Mutex
	failures  int
	openUntil time.Time
	probing   bool
}

func NewBreaker(threshold int, cooldown time.Duration) *Breaker {
	if threshold < 1 {
		threshold = 1
	}
	if cooldown <= 0 {
		cooldown = 10 * time.Second
	}
	return &Breaker{Threshold: threshold, Cooldown: cooldown}
}

// Allow reports whether a request may start. After the cooldown it grants one
// probe and blocks other callers until Done records the probe's outcome.
func (b *Breaker) Allow(now time.Time) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.openUntil.IsZero() {
		return true
	}
	if now.Before(b.openUntil) || b.probing {
		return false
	}
	b.probing = true
	return true
}

// RetryAt is when the open breaker may next be probed.
func (b *Breaker) RetryAt(now time.Time) time.Time {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.openUntil.IsZero() || !now.Before(b.openUntil) {
		return now
	}
	return b.openUntil
}

func (b *Breaker) Open(now time.Time) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return !b.openUntil.IsZero() && now.Before(b.openUntil)
}

func (b *Breaker) OnSuccess() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.failures = 0
	b.openUntil = time.Time{}
	b.probing = false
}

func (b *Breaker) OnFailure(now time.Time) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.probing {
		b.probing = false
		b.failures = b.Threshold
		b.openUntil = now.Add(b.Cooldown)
		return
	}
	b.failures += 1
	if b.failures >= b.Threshold {
		b.openUntil = now.Add(b.Cooldown)
	}
}

// RetryPolicy is exponential backoff with full jitter. RetryAfter (from the
// server) is honoured as a floor.
type RetryPolicy struct {
	Max  int
	Base time.Duration
	Cap  time.Duration
}

// Delay returns the wait before retry number `attempt` (0-based). `rand` must be
// in [0, 1); full jitter is `rand * min(cap, base*2^attempt)`.
func (p RetryPolicy) Delay(attempt int, rand float64, retryAfter time.Duration) time.Duration {
	if attempt < 0 {
		attempt = 0
	}
	base := p.Base
	if base <= 0 {
		base = 100 * time.Millisecond
	}
	capDelay := p.Cap
	if capDelay <= 0 {
		capDelay = 10 * time.Second
	}
	delay := capDelay
	if attempt < 32 {
		if scaled := base << uint(attempt); scaled > 0 && scaled < capDelay {
			delay = scaled
		}
	}
	if rand < 0 {
		rand = 0
	}
	if rand > 1 {
		rand = 1
	}
	jittered := time.Duration(float64(delay) * rand)
	if retryAfter > jittered {
		return retryAfter
	}
	return jittered
}

// IsTransientCode reports whether an RPC error is worth one more attempt.
func IsTransientCode(code int) bool {
	switch code {
	case protocol.ErrConnection,
		protocol.ErrDispatchedUnconfirmed,
		protocol.ErrNavigationFailed,
		protocol.ErrDebuggerDetached,
		protocol.ErrBusy:
		return true
	}
	return false
}

// SchedulerConfig tunes per-host pacing and the circuit breaker.
type SchedulerConfig struct {
	MinRate          float64
	MaxRate          float64
	StartRate        float64
	Alpha            float64
	FailureThreshold int
	Cooldown         time.Duration
}

type hostState struct {
	rate    *RateController
	breaker *Breaker

	mu     sync.Mutex
	nextAt time.Time
}

// Scheduler owns one AIMD controller and breaker per host.
type Scheduler struct {
	mu    sync.Mutex
	hosts map[string]*hostState
	cfg   SchedulerConfig
	now   func() time.Time
}

func NewScheduler(cfg SchedulerConfig) *Scheduler {
	if cfg.StartRate == 0 {
		cfg.StartRate = cfg.MaxRate
	}
	return &Scheduler{
		hosts: make(map[string]*hostState),
		cfg:   cfg,
		now:   time.Now,
	}
}

// SetClock is for tests.
func (s *Scheduler) SetClock(now func() time.Time) {
	s.now = now
}

func (s *Scheduler) host(name string) *hostState {
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.hosts[name]
	if state == nil {
		state = &hostState{
			rate: NewRateController(
				s.cfg.MinRate,
				s.cfg.MaxRate,
				s.cfg.StartRate,
				s.cfg.Alpha,
			),
			breaker: NewBreaker(s.cfg.FailureThreshold, s.cfg.Cooldown),
		}
		s.hosts[name] = state
	}
	return state
}

// Admit returns how long to wait before hitting `host`. ok=false means the
// breaker is open; the caller should wait RetryAt or stop.
func (s *Scheduler) Admit(host string) (time.Duration, bool) {
	state := s.host(host)
	now := s.now()
	if !state.breaker.Allow(now) {
		return state.breaker.RetryAt(now).Sub(now), false
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.nextAt.After(now) {
		return state.nextAt.Sub(now), true
	}
	return 0, true
}

// Done records the outcome of one attempt and schedules the next slot.
func (s *Scheduler) Done(host string, success bool) {
	state := s.host(host)
	now := s.now()
	if success {
		state.rate.OnSuccess()
		state.breaker.OnSuccess()
	} else {
		state.rate.OnFailure()
		state.breaker.OnFailure(now)
	}
	state.mu.Lock()
	state.nextAt = now.Add(state.rate.Delay())
	state.mu.Unlock()
}

// RateOf exposes the current per-host rate for logging and tests.
func (s *Scheduler) RateOf(host string) float64 {
	return s.host(host).rate.Rate()
}

const (
	simhashBits = 64
	bandCount   = 4
	bandBits    = 16
)

func fnv1a64(input string) uint64 {
	var hash uint64 = 0xcbf29ce484222325
	for index := 0; index < len(input); index += 1 {
		hash ^= uint64(input[index])
		hash *= 0x100000001b3
	}
	return hash
}

func tokens(text string) []string {
	words := make([]string, 0, 64)
	var current strings.Builder
	flush := func() {
		if current.Len() > 0 {
			words = append(words, current.String())
			current.Reset()
		}
	}
	for _, r := range strings.ToLower(text) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			current.WriteRune(r)
		} else {
			flush()
		}
	}
	flush()
	return words
}

func shingles(words []string) []string {
	if len(words) == 0 {
		return nil
	}
	if len(words) < 3 {
		return words
	}
	parts := make([]string, 0, len(words)-2)
	for index := 0; index+2 < len(words); index += 1 {
		parts = append(parts, words[index]+" "+words[index+1]+" "+words[index+2])
	}
	return parts
}

// SimHash64 is a 64-bit SimHash over word 3-shingles. Whitespace and punctuation
// changes leave it stable; unrelated pages differ in many bits.
func SimHash64(text string) uint64 {
	shingles := shingles(tokens(text))
	if len(shingles) == 0 {
		return 0
	}
	var scores [simhashBits]int
	for _, shingle := range shingles {
		hash := fnv1a64(shingle)
		for bit := 0; bit < simhashBits; bit += 1 {
			if hash&(uint64(1)<<uint(bit)) != 0 {
				scores[bit] += 1
			} else {
				scores[bit] -= 1
			}
		}
	}
	var result uint64
	for bit := 0; bit < simhashBits; bit += 1 {
		if scores[bit] > 0 {
			result |= uint64(1) << uint(bit)
		}
	}
	return result
}

// Hamming returns the number of differing bits.
func Hamming(left, right uint64) int {
	return bitsOnes(left ^ right)
}

func bitsOnes(value uint64) int {
	count := 0
	for value != 0 {
		value &= value - 1
		count += 1
	}
	return count
}

// NearDuplicates is an LSH index: 4 bands of 16 bits give average O(1) lookups
// for Hamming distance <= MaxDistance. Pairs that differ in every band can be
// missed by design; exact hashes are always caught. Not safe for concurrent
// use; add one per crawler loop.
type NearDuplicates struct {
	maxDistance int
	exact       map[uint64]struct{}
	bands       [bandCount]map[uint16][]uint64
}

func NewNearDuplicates(maxDistance int) *NearDuplicates {
	if maxDistance < 0 {
		maxDistance = 0
	}
	index := &NearDuplicates{
		maxDistance: maxDistance,
		exact:       make(map[uint64]struct{}),
	}
	for band := 0; band < bandCount; band += 1 {
		index.bands[band] = make(map[uint16][]uint64)
	}
	return index
}

// Seen reports whether an exact or near-duplicate hash is already indexed.
func (n *NearDuplicates) Seen(hash uint64) bool {
	if _, ok := n.exact[hash]; ok {
		return true
	}
	for band := 0; band < bandCount; band += 1 {
		key := uint16((hash >> uint(band*bandBits)) & 0xffff)
		for _, candidate := range n.bands[band][key] {
			if Hamming(candidate, hash) <= n.maxDistance {
				return true
			}
		}
	}
	return false
}

func (n *NearDuplicates) Add(hash uint64) {
	if _, ok := n.exact[hash]; ok {
		return
	}
	n.exact[hash] = struct{}{}
	for band := 0; band < bandCount; band += 1 {
		key := uint16((hash >> uint(band*bandBits)) & 0xffff)
		n.bands[band][key] = append(n.bands[band][key], hash)
	}
}
