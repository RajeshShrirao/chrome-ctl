package bulk

import (
	"testing"
	"time"

	"github.com/rajeshshrirao/chrome-ctl/internal/protocol"
)

func TestRateControllerAIMD(t *testing.T) {
	rate := NewRateController(0.5, 4, 2, 0.5)
	if got := rate.Rate(); got != 2 {
		t.Fatalf("start rate = %v, want 2", got)
	}
	if got := rate.Delay(); got != 500*time.Millisecond {
		t.Fatalf("delay = %v, want 500ms", got)
	}

	for index := 0; index < 10; index += 1 {
		rate.OnSuccess()
	}
	if got := rate.Rate(); got != 4 {
		t.Fatalf("rate after successes = %v, want 4", got)
	}

	rate.OnFailure()
	if got := rate.Rate(); got != 2 {
		t.Fatalf("rate after one failure = %v, want 2", got)
	}
	rate.OnFailure()
	if got := rate.Rate(); got != 1 {
		t.Fatalf("rate after two failures = %v, want 1", got)
	}
	rate.OnFailure()
	rate.OnFailure()
	if got := rate.Rate(); got != 0.5 {
		t.Fatalf("rate floor = %v, want 0.5", got)
	}
}

func TestBreakerHalfOpenProbe(t *testing.T) {
	breaker := NewBreaker(2, 100*time.Millisecond)
	now := time.Unix(0, 0)

	if !breaker.Allow(now) {
		t.Fatal("closed breaker should allow")
	}
	breaker.OnFailure(now)
	if !breaker.Allow(now) {
		t.Fatal("one failure should not open")
	}
	breaker.OnFailure(now)
	if breaker.Allow(now) {
		t.Fatal("breaker should be open")
	}
	if got := breaker.RetryAt(now); !got.Equal(now.Add(100 * time.Millisecond)) {
		t.Fatalf("retryAt = %v", got)
	}

	probeTime := now.Add(150 * time.Millisecond)
	if !breaker.Allow(probeTime) {
		t.Fatal("cooldown should allow one probe")
	}
	if breaker.Allow(probeTime) {
		t.Fatal("only one half-open probe may run")
	}
	breaker.OnFailure(probeTime)
	if breaker.Allow(probeTime) {
		t.Fatal("failed probe should reopen")
	}

	reopen := probeTime.Add(150 * time.Millisecond)
	if !breaker.Allow(reopen) {
		t.Fatal("second cooldown should allow a probe")
	}
	breaker.OnSuccess()
	if !breaker.Allow(reopen) {
		t.Fatal("success should close the breaker")
	}
}

func TestRetryPolicyFullJitterAndRetryAfter(t *testing.T) {
	policy := RetryPolicy{Max: 5, Base: 100 * time.Millisecond, Cap: time.Second}

	cases := []struct {
		attempt    int
		rand       float64
		retryAfter time.Duration
		want       time.Duration
	}{
		{0, 0.5, 0, 50 * time.Millisecond},
		{1, 0.5, 0, 100 * time.Millisecond},
		{3, 0.5, 0, 400 * time.Millisecond},
		{10, 0.5, 0, 500 * time.Millisecond},
		{2, 0.0, 0, 0},
		{1, 0.5, 2 * time.Second, 2 * time.Second},
	}
	for _, testCase := range cases {
		got := policy.Delay(testCase.attempt, testCase.rand, testCase.retryAfter)
		if got != testCase.want {
			t.Errorf("attempt %d rand %v retryAfter %v: got %v, want %v",
				testCase.attempt, testCase.rand, testCase.retryAfter, got, testCase.want)
		}
	}
}

func TestIsTransientCode(t *testing.T) {
	if !IsTransientCode(protocol.ErrConnection) {
		t.Fatal("connection errors are transient")
	}
	if !IsTransientCode(protocol.ErrNavigationFailed) {
		t.Fatal("navigation failures are transient")
	}
	if IsTransientCode(protocol.ErrElementNotFound) {
		t.Fatal("element-not-found is permanent")
	}
	if !IsTransientCode(protocol.ErrBusy) {
		t.Fatal("a busy tab should be retried")
	}
	if IsTransientCode(protocol.ErrInternal) {
		t.Fatal("internal errors should not be auto-retried")
	}
}

func TestSimHashStableAcrossFormatting(t *testing.T) {
	left := SimHash64("Hello, world! This is a test.\nSecond line.")
	right := SimHash64("hello world this   is a test second line")
	if left != right {
		t.Fatalf("formatting changed the simhash: %x vs %x", left, right)
	}
	unrelated := SimHash64("Completely different content about cooking pasta and tomatoes")
	if Hamming(left, unrelated) < 8 {
		t.Fatalf("unrelated pages are too close: hamming=%d", Hamming(left, unrelated))
	}
	if SimHash64("") != 0 {
		t.Fatal("empty text should hash to zero")
	}
}

func TestNearDuplicates(t *testing.T) {
	index := NewNearDuplicates(3)
	hash := SimHash64("the quick brown fox jumps over the lazy dog every day")

	if index.Seen(hash) {
		t.Fatal("empty index should not report a duplicate")
	}
	index.Add(hash)
	if !index.Seen(hash) {
		t.Fatal("exact hash should match")
	}
	if !index.Seen(hash ^ 1) {
		t.Fatal("one-bit flip should be a near duplicate")
	}
	if index.Seen(hash ^ 0xffffffffffffffff) {
		t.Fatal("64-bit difference should not match")
	}
}

func TestSchedulerPacesAndTripsBreaker(t *testing.T) {
	scheduler := NewScheduler(SchedulerConfig{
		MinRate:          1,
		MaxRate:          4,
		StartRate:        2,
		Alpha:            0.5,
		FailureThreshold: 2,
		Cooldown:         100 * time.Millisecond,
	})
	now := time.Unix(0, 0)
	scheduler.SetClock(func() time.Time { return now })

	if wait, ok := scheduler.Admit("example.test"); !ok || wait != 0 {
		t.Fatalf("first admit = %v, %v", wait, ok)
	}
	scheduler.Done("example.test", true)
	now = now.Add(100 * time.Millisecond)
	if wait, ok := scheduler.Admit("example.test"); !ok || wait <= 0 {
		t.Fatalf("second admit should wait, got %v, %v", wait, ok)
	}

	now = now.Add(time.Second)
	if wait, ok := scheduler.Admit("example.test"); !ok || wait != 0 {
		t.Fatalf("after the slot, admit = %v, %v", wait, ok)
	}
	scheduler.Done("example.test", false)
	now = now.Add(time.Second)
	if wait, ok := scheduler.Admit("example.test"); !ok || wait != 0 {
		t.Fatalf("one failure should not trip, got %v, %v", wait, ok)
	}
	scheduler.Done("example.test", false)
	wait, ok := scheduler.Admit("example.test")
	if ok {
		t.Fatal("breaker should be open after two consecutive failures")
	}
	if wait <= 0 {
		t.Fatalf("open breaker should report a cooldown, got %v", wait)
	}
}
