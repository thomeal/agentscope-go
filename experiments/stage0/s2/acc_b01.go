package s2

func accB01() Task {
	return Task{
		ID:     "s2-b01-rate-limiter",
		Set:    "acceptance",
		System: system("a per-key rate limiter for an API gateway"),
		Files: map[string]string{
			"go.mod": goMod,
			"limiter.go": `package ratelimit

// Limiter grants requests per key in fixed windows.
type Limiter struct {
	limit  int
	window int64
	used   map[string]int
	start  map[string]int64
}

// New returns a Limiter allowing limit requests per window, where window is a
// length in milliseconds.
func New(limit int, window int64) *Limiter {
	return &Limiter{limit: limit, window: window, used: map[string]int{}, start: map[string]int64{}}
}

// Limit returns how many requests one window allows.
func (l *Limiter) Limit() int { return l.limit }
`,
			"docs/limits.txt": `Limiter notes from the gateway team

Keys are whatever the gateway passes us, usually an account id. A key that
starts with "svc-" belongs to one of our own services. Those are never turned
away and nothing is counted for them, and anything that reports on a key shows
them as having their full allowance.

The gateway does not retry by itself. Clients see the response as it is.
`,
			"limiter_test.go": `package ratelimit

import "testing"

func TestNew(t *testing.T) {
	l := New(5, 1000)
	if l.Limit() != 5 {
		t.Fatalf("Limit = %d", l.Limit())
	}
}
`,
		},
		Hidden: map[string]string{hiddenName: `package ratelimit_test

import (
	"errors"
	"testing"

	ratelimit "fixture"
)

func TestWindowLength(t *testing.T) {
	if got := ratelimit.New(3, 5000).Window(); got != 5000 {
		t.Fatalf("Window = %d", got)
	}
}

func TestAllowCounts(t *testing.T) {
	l := ratelimit.New(2, 1000)
	for i, want := range []bool{true, true, false, false} {
		if got := l.Allow("a", int64(i)); got != want {
			t.Fatalf("request %d: %v", i, got)
		}
	}
}

func TestAllowKeysSeparate(t *testing.T) {
	l := ratelimit.New(1, 1000)
	if !l.Allow("a", 0) || !l.Allow("b", 0) {
		t.Fatal("one key used up another key's allowance")
	}
	if l.Allow("a", 1) {
		t.Fatal("a went over its limit")
	}
}

func TestAllowNextWindow(t *testing.T) {
	l := ratelimit.New(1, 1000)
	if !l.Allow("a", 0) || l.Allow("a", 999) {
		t.Fatal("the first window is wrong")
	}
	if !l.Allow("a", 1000) {
		t.Fatal("no new window at 1000")
	}
}

func TestAllowLinesUpWithClock(t *testing.T) {
	l := ratelimit.New(1, 1000)
	if !l.Allow("a", 900) {
		t.Fatal("the first request was refused")
	}
	if !l.Allow("a", 1000) {
		t.Fatal("1000 begins a new window")
	}
	if l.Allow("a", 1900) {
		t.Fatal("1900 is in the same window as 1000")
	}
}

func TestResetForgets(t *testing.T) {
	l := ratelimit.New(1, 1000)
	l.Allow("a", 0)
	l.Reset("a")
	if !l.Allow("a", 1) {
		t.Fatal("the key still counts after Reset")
	}
}

func TestRemainingCounts(t *testing.T) {
	l := ratelimit.New(3, 1000)
	if got := l.Remaining("a", 0); got != 3 {
		t.Fatalf("an unused key has %d left", got)
	}
	l.Allow("a", 0)
	if got := l.Remaining("a", 0); got != 2 {
		t.Fatalf("Remaining = %d", got)
	}
	if got := l.Remaining("a", 0); got != 2 {
		t.Fatalf("asking used some up: %d", got)
	}
}

func TestRemainingAfterRefusals(t *testing.T) {
	l := ratelimit.New(1, 1000)
	for range 3 {
		l.Allow("a", 0)
	}
	if got := l.Remaining("a", 0); got != 0 {
		t.Fatalf("Remaining = %d", got)
	}
}

func TestRemainingLinesUpWithClock(t *testing.T) {
	l := ratelimit.New(2, 1000)
	l.Allow("a", 900)
	if got := l.Remaining("a", 999); got != 1 {
		t.Fatalf("at 999: %d", got)
	}
	if got := l.Remaining("a", 1000); got != 2 {
		t.Fatalf("1000 begins a new window: %d", got)
	}
}

func TestRetryAfterZeroWhenAllowed(t *testing.T) {
	if got := ratelimit.New(1, 1000).RetryAfter("a", 0); got != 0 {
		t.Fatalf("RetryAfter = %d", got)
	}
}

func TestRetryAfterPositiveWhenRefused(t *testing.T) {
	l := ratelimit.New(1, 60000)
	l.Allow("a", 0)
	if got := l.RetryAfter("a", 1); got <= 0 {
		t.Fatalf("RetryAfter = %d", got)
	}
}

func TestRetryAfterWholeSeconds(t *testing.T) {
	l := ratelimit.New(1, 60000)
	l.Allow("a", 120000)
	if got := l.RetryAfter("a", 120000); got != 60 {
		t.Fatalf("a whole window to wait: %d", got)
	}
}

func TestRetryAfterRoundsUp(t *testing.T) {
	l := ratelimit.New(1, 60000)
	l.Allow("a", 120000)
	if got := l.RetryAfter("a", 121500); got != 59 {
		t.Fatalf("58.5 seconds to wait: %d", got)
	}
	short := ratelimit.New(1, 1000)
	short.Allow("a", 2000)
	if got := short.RetryAfter("a", 2700); got != 1 {
		t.Fatalf("0.3 seconds to wait: %d", got)
	}
}

func TestRetryAfterLinesUpWithClock(t *testing.T) {
	l := ratelimit.New(1, 60000)
	l.Allow("a", 90000)
	early, late := l.RetryAfter("a", 90000), l.RetryAfter("a", 105000)
	if early != 2*late {
		t.Fatalf("the window ends at 120000, but the waits are %d and %d", early, late)
	}
}

func TestServiceKeysNeverLimited(t *testing.T) {
	l := ratelimit.New(1, 1000)
	for i := range 5 {
		if !l.Allow("svc-billing", int64(i)) {
			t.Fatalf("request %d refused", i)
		}
	}
}

func TestServiceKeysFullAllowance(t *testing.T) {
	l := ratelimit.New(2, 1000)
	l.Allow("svc-billing", 0)
	if got := l.Remaining("svc-billing", 0); got != 2 {
		t.Fatalf("Remaining = %d", got)
	}
}

func TestServiceKeysNoWait(t *testing.T) {
	l := ratelimit.New(1, 60000)
	l.Allow("svc-billing", 0)
	l.Allow("svc-billing", 0)
	if got := l.RetryAfter("svc-billing", 0); got != 0 {
		t.Fatalf("RetryAfter = %d", got)
	}
}

func TestTakeAllowed(t *testing.T) {
	l := ratelimit.New(1, 1000)
	if err := l.Take("a", 0); err != nil {
		t.Fatal(err)
	}
	if got := l.Remaining("a", 0); got != 0 {
		t.Fatalf("Take did not count: %d left", got)
	}
}

func TestTakeRefuses(t *testing.T) {
	l := ratelimit.New(1, 1000)
	l.Allow("a", 0)
	if err := l.Take("a", 0); err == nil {
		t.Fatal("no error")
	}
}

func TestTakeLimitedError(t *testing.T) {
	l := ratelimit.New(1, 1000)
	l.Allow("a", 0)
	if err := l.Take("a", 0); !errors.Is(err, ratelimit.ErrLimited) {
		t.Fatalf("err = %v", err)
	}
}
`},
		Solution: map[string]string{
			"limiter.go": `package ratelimit

import (
	"errors"
	"strings"
)

// ErrLimited is what a call returns when it fails because a key is over its limit.
var ErrLimited = errors.New("rate limited")

// Limiter grants requests per key in fixed windows.
type Limiter struct {
	limit  int
	window int64
	used   map[string]int
	start  map[string]int64
}

// New returns a Limiter allowing limit requests per window, where window is a
// length in milliseconds.
func New(limit int, window int64) *Limiter {
	return &Limiter{limit: limit, window: window, used: map[string]int{}, start: map[string]int64{}}
}

// Limit returns how many requests one window allows.
func (l *Limiter) Limit() int { return l.limit }

// Window returns the window length.
func (l *Limiter) Window() int64 { return l.window }

// exempt reports whether key belongs to one of our own services.
func exempt(key string) bool { return strings.HasPrefix(key, "svc-") }

// roll starts a new window for key if now has left the one it was in.
func (l *Limiter) roll(key string, now int64) {
	start := now - now%l.window
	if l.start[key] != start {
		l.start[key] = start
		l.used[key] = 0
	}
}

// Allow counts one more request for key if it fits in the current window.
func (l *Limiter) Allow(key string, now int64) bool {
	if exempt(key) {
		return true
	}
	l.roll(key, now)
	if l.used[key] >= l.limit {
		return false
	}
	l.used[key]++
	return true
}

// Reset forgets everything counted for key.
func (l *Limiter) Reset(key string) {
	delete(l.used, key)
	delete(l.start, key)
}

// Remaining reports how many more requests key could make in the current window.
func (l *Limiter) Remaining(key string, now int64) int {
	if exempt(key) {
		return l.limit
	}
	l.roll(key, now)
	return l.limit - l.used[key]
}

// RetryAfter returns how long key has to wait, in whole seconds rounded up.
func (l *Limiter) RetryAfter(key string, now int64) int {
	if exempt(key) {
		return 0
	}
	l.roll(key, now)
	if l.used[key] < l.limit {
		return 0
	}
	return int((l.start[key] + l.window - now + 999) / 1000)
}

// Take is Allow in the form of an error.
func (l *Limiter) Take(key string, now int64) error {
	if !l.Allow(key, now) {
		return ErrLimited
	}
	return nil
}
`,
		},
		Turns: script(
			local("Read docs/limits.txt and tell me in a line or two what it means for how keys get treated. No code changes."),
			local("One decision on the windows: they line up with the clock. A window begins at a whole multiple of its length, not at a key's first request, because every node has to agree on where a window begins. Add `func (l *Limiter) Window() int64`, which returns the window length."),
			cloud("Add `func (l *Limiter) Allow(key string, now int64) bool`. now is milliseconds since the epoch. It returns true if one more request for key fits in the current window, and counts that request. A request that gets turned away isn't counted."),
			cloud("The gateway copies any wait we report straight into a Retry-After header, so a wait is a whole number of seconds, rounded up. Nothing reports one yet. Add `func (l *Limiter) Reset(key string)`, which forgets everything counted for a key."),
			local("Add `func (l *Limiter) Remaining(key string, now int64) int`: how many more requests the key could make in the current window. Asking doesn't use any of them."),
			cloud("Add `var ErrLimited = errors.New(\"rate limited\")` to limiter.go. Nothing returns it yet. Any call here that fails because a key is over its limit returns exactly this error."),
			local("Add `func (l *Limiter) RetryAfter(key string, now int64) int`: how long the key has to wait until its next request would be allowed, and 0 if it could go right now."),
			local("Add `func (l *Limiter) Take(key string, now int64) error`, for callers who want Allow in the form of an error. It's nil when the request is allowed and counted."),
		),
		Carries: []Carry{
			{
				What:        "windows begin at whole multiples of their length, not at a key's first request",
				Kind:        CarryUser,
				Established: 1,
				Needed:      []int{2, 4, 6},
				Tests:       []string{"TestAllowLinesUpWithClock", "TestRemainingLinesUpWithClock", "TestRetryAfterLinesUpWithClock"},
				Absent:      []string{"line up", "lines up", "multiple", "clock", "aligned", "first request"},
			},
			{
				What:        "svc- keys are never limited and always show their full allowance (docs/limits.txt)",
				Kind:        CarryDiscovered,
				Established: 0,
				Needed:      []int{2, 4, 6},
				Source:      "docs/limits.txt",
				Tests:       []string{"TestServiceKeysNeverLimited", "TestServiceKeysFullAllowance", "TestServiceKeysNoWait"},
				Absent:      []string{"svc-", "own service", "internal", "full allowance"},
			},
			{
				What:        "a reported wait is a whole number of seconds, rounded up",
				Kind:        CarryUser,
				Established: 3,
				Needed:      []int{6},
				Tests:       []string{"TestRetryAfterWholeSeconds", "TestRetryAfterRoundsUp"},
				Absent:      []string{"whole", " second", "round", "retry-after", "header"},
			},
			{
				What:        "ErrLimited is the error for being over the limit",
				Kind:        CarryEmbodied,
				Established: 5,
				Needed:      []int{7},
				Tests:       []string{"TestTakeLimitedError"},
				Absent:      []string{"errlimited", "sentinel"},
			},
		},
	}
}
