package s2

func accB09() Task {
	return Task{
		ID:     "s2-b09-circuit-breaker",
		Set:    "acceptance",
		System: system("a circuit breaker for calls to a flaky dependency"),
		Files: map[string]string{
			"go.mod": goMod,
			"breaker.go": `package breaker

// State is the breaker's mode.
type State int

// The modes a Breaker can be in.
const (
	Closed State = iota
	Open
	HalfOpen
)

// Breaker guards calls to a flaky dependency. Times are milliseconds on a clock
// the caller supplies.
type Breaker struct {
	Threshold int   // the failure limit
	Cooldown  int64 // how long to stay open
	state     State
	failures  int
	openedAt  int64
}

// State returns the current mode.
func (b *Breaker) State() State { return b.state }
`,
			"docs/oncall.txt": `On-call notes

A cancelled call says nothing about the dependency. An error matching
context.Canceled (use errors.Is) does not count as a failure and does not clear
the failure count either. If it was the trial call, the breaker stays half-open
and the next call becomes the trial. A timeout (context.DeadlineExceeded) is a
real failure.

Dashboards show the state by its String form, which has to be one of closed,
open and half-open.
`,
			"breaker_test.go": `package breaker

import "testing"

func TestStartsClosed(t *testing.T) {
	var b Breaker
	if b.State() != Closed {
		t.Fatalf("State = %v", b.State())
	}
}
`,
		},
		Hidden: map[string]string{hiddenName: `package breaker_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	breaker "fixture"
)

var errBoom = errors.New("boom")

// tripped returns a breaker that opened at time now, however many failures that takes.
func tripped(t *testing.T, threshold int, cooldown, now int64) *breaker.Breaker {
	t.Helper()
	b := &breaker.Breaker{Threshold: threshold, Cooldown: cooldown}
	for i := 0; i < 64 && b.State() != breaker.Open; i++ {
		b.Record(now, errBoom)
	}
	if b.State() != breaker.Open {
		t.Fatal("the breaker never opened")
	}
	return b
}

func fail(b *breaker.Breaker, n int) {
	for range n {
		b.Record(0, errBoom)
	}
}

func TestFailuresCount(t *testing.T) {
	b := &breaker.Breaker{Threshold: 5, Cooldown: 1000}
	fail(b, 2)
	if b.Failures() != 2 {
		t.Fatalf("Failures = %d", b.Failures())
	}
}

func TestSuccessClearsCount(t *testing.T) {
	b := &breaker.Breaker{Threshold: 5, Cooldown: 1000}
	fail(b, 2)
	b.Record(0, nil)
	if b.Failures() != 0 {
		t.Fatalf("Failures = %d", b.Failures())
	}
}

func TestStaysClosedBelowThreshold(t *testing.T) {
	b := &breaker.Breaker{Threshold: 3, Cooldown: 1000}
	fail(b, 2)
	if b.State() != breaker.Closed {
		t.Fatalf("State = %v", b.State())
	}
}

func TestOpensEventually(t *testing.T) {
	b := &breaker.Breaker{Threshold: 3, Cooldown: 1000}
	fail(b, 5)
	if b.State() != breaker.Open {
		t.Fatalf("State = %v", b.State())
	}
}

func TestThresholdFailuresTolerated(t *testing.T) {
	b := &breaker.Breaker{Threshold: 3, Cooldown: 1000}
	fail(b, 3)
	if b.State() != breaker.Closed {
		t.Fatalf("after 3 failures: State = %v", b.State())
	}
	fail(b, 1)
	if b.State() != breaker.Open {
		t.Fatalf("after 4 failures: State = %v", b.State())
	}
}

func TestToleranceAfterSuccess(t *testing.T) {
	b := &breaker.Breaker{Threshold: 2, Cooldown: 1000}
	fail(b, 2)
	b.Record(0, nil)
	fail(b, 2)
	if b.State() != breaker.Closed {
		t.Fatalf("State = %v", b.State())
	}
}

func TestAllowClosed(t *testing.T) {
	b := &breaker.Breaker{Threshold: 1, Cooldown: 1000}
	if err := b.Allow(0); err != nil {
		t.Fatal(err)
	}
}

func TestAllowOpenTurnsAway(t *testing.T) {
	b := tripped(t, 1, 1000, 0)
	if err := b.Allow(1); err == nil {
		t.Fatal("no error")
	}
}

func TestAllowOpenError(t *testing.T) {
	b := tripped(t, 1, 1000, 0)
	if err := b.Allow(1); !errors.Is(err, breaker.ErrOpen) {
		t.Fatalf("err = %v", err)
	}
}

func TestCooldownThenTrial(t *testing.T) {
	b := tripped(t, 1, 1000, 500)
	if err := b.Allow(1499); err == nil {
		t.Fatal("the wait is not over at 1499")
	}
	if err := b.Allow(1500); err != nil {
		t.Fatalf("the wait is over at 1500: %v", err)
	}
	if b.State() != breaker.HalfOpen {
		t.Fatalf("State = %v", b.State())
	}
}

func TestOneTrialAtATime(t *testing.T) {
	b := tripped(t, 1, 1000, 0)
	if err := b.Allow(1000); err != nil {
		t.Fatal(err)
	}
	if err := b.Allow(1001); err == nil {
		t.Fatal("a second call went through while half-open")
	}
}

func TestHalfOpenError(t *testing.T) {
	b := tripped(t, 1, 1000, 0)
	if err := b.Allow(1000); err != nil {
		t.Fatal(err)
	}
	if err := b.Allow(1001); !errors.Is(err, breaker.ErrOpen) {
		t.Fatalf("err = %v", err)
	}
}

func TestTrialSuccessCloses(t *testing.T) {
	b := tripped(t, 1, 1000, 0)
	if err := b.Allow(1000); err != nil {
		t.Fatal(err)
	}
	b.Record(1000, nil)
	if b.State() != breaker.Closed {
		t.Fatalf("State = %v", b.State())
	}
	if err := b.Allow(1001); err != nil {
		t.Fatal(err)
	}
}

func TestTrialFailureReopens(t *testing.T) {
	b := tripped(t, 1, 1000, 0)
	if err := b.Allow(1000); err != nil {
		t.Fatal(err)
	}
	b.Record(1000, errBoom)
	if b.State() != breaker.Open {
		t.Fatalf("State = %v", b.State())
	}
}

func TestFailedTrialDoubles(t *testing.T) {
	b := tripped(t, 1, 1000, 0)
	if err := b.Allow(1000); err != nil {
		t.Fatal(err)
	}
	b.Record(1000, errBoom)
	if err := b.Allow(2999); err == nil {
		t.Fatal("two seconds have not gone by at 2999")
	}
	if err := b.Allow(3000); err != nil {
		t.Fatalf("two seconds have gone by at 3000: %v", err)
	}
}

func TestDoublingStopsAtEightTimes(t *testing.T) {
	b := tripped(t, 1, 1000, 0)
	now := int64(0)
	for _, wait := range []int64{1000, 2000, 4000, 8000, 8000} {
		if err := b.Allow(now + wait - 1); err == nil {
			t.Fatalf("wait %d: went through early", wait)
		}
		now += wait
		if err := b.Allow(now); err != nil {
			t.Fatalf("wait %d: %v", wait, err)
		}
		b.Record(now, errBoom)
	}
}

func TestCloseRestoresCooldown(t *testing.T) {
	b := tripped(t, 0, 1000, 0)
	if err := b.Allow(1000); err != nil {
		t.Fatal(err)
	}
	b.Record(1000, errBoom)
	if err := b.Allow(3000); err != nil {
		t.Fatal(err)
	}
	b.Record(3000, nil)
	b.Record(5000, errBoom)
	if b.State() != breaker.Open {
		t.Fatalf("State = %v", b.State())
	}
	if err := b.Allow(6000); err != nil {
		t.Fatalf("the wait should be back to Cooldown: %v", err)
	}
}

func TestCancelledNotAFailure(t *testing.T) {
	b := &breaker.Breaker{Threshold: 1, Cooldown: 1000}
	for range 5 {
		b.Record(0, context.Canceled)
	}
	if b.State() != breaker.Closed || b.Failures() != 0 {
		t.Fatalf("State = %v, Failures = %d", b.State(), b.Failures())
	}
}

func TestCancelledKeepsCount(t *testing.T) {
	b := &breaker.Breaker{Threshold: 5, Cooldown: 1000}
	fail(b, 2)
	b.Record(0, context.Canceled)
	if b.Failures() != 2 {
		t.Fatalf("Failures = %d", b.Failures())
	}
}

func TestWrappedCancelled(t *testing.T) {
	b := &breaker.Breaker{Threshold: 5, Cooldown: 1000}
	fail(b, 1)
	b.Record(0, fmt.Errorf("call: %w", context.Canceled))
	if b.Failures() != 1 {
		t.Fatalf("Failures = %d", b.Failures())
	}
}

func TestCancelledTrial(t *testing.T) {
	b := tripped(t, 1, 1000, 0)
	if err := b.Allow(1000); err != nil {
		t.Fatal(err)
	}
	b.Record(1000, context.Canceled)
	if b.State() != breaker.HalfOpen {
		t.Fatalf("State = %v", b.State())
	}
	if err := b.Allow(1001); err != nil {
		t.Fatalf("the next call should be the trial: %v", err)
	}
}

func TestTimeoutIsAFailure(t *testing.T) {
	b := &breaker.Breaker{Threshold: 0, Cooldown: 1000}
	b.Record(0, context.DeadlineExceeded)
	if b.State() != breaker.Open {
		t.Fatalf("State = %v", b.State())
	}
}

func TestStateNames(t *testing.T) {
	for state, want := range map[breaker.State]string{breaker.Closed: "closed", breaker.Open: "open", breaker.HalfOpen: "half-open"} {
		if got := state.String(); got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	}
}

func TestDoRunsAndRecords(t *testing.T) {
	b := &breaker.Breaker{Threshold: 5, Cooldown: 1000}
	ran := false
	err := b.Do(0, func() error { ran = true; return errBoom })
	if !ran || !errors.Is(err, errBoom) {
		t.Fatalf("ran = %v, err = %v", ran, err)
	}
	if b.Failures() != 1 {
		t.Fatalf("Failures = %d", b.Failures())
	}
}

func TestDoTurnedAway(t *testing.T) {
	b := tripped(t, 1, 1000, 0)
	ran := false
	if err := b.Do(1, func() error { ran = true; return nil }); err == nil || ran {
		t.Fatalf("ran = %v, err = %v", ran, err)
	}
}

func TestDoTurnedAwayError(t *testing.T) {
	b := tripped(t, 1, 1000, 0)
	if err := b.Do(1, func() error { return nil }); !errors.Is(err, breaker.ErrOpen) {
		t.Fatalf("err = %v", err)
	}
}

func TestRetryAt(t *testing.T) {
	if got := tripped(t, 1, 1000, 500).RetryAt(); got != 1500 {
		t.Fatalf("RetryAt = %d", got)
	}
}

func TestRetryAtAfterFailedTrial(t *testing.T) {
	b := tripped(t, 1, 1000, 500)
	if err := b.Allow(1500); err != nil {
		t.Fatal(err)
	}
	b.Record(1500, errBoom)
	if got := b.RetryAt(); got != 3500 {
		t.Fatalf("RetryAt = %d", got)
	}
}
`},
		Solution: map[string]string{
			"breaker.go": `package breaker

import (
	"context"
	"errors"
)

// ErrOpen is what any call the breaker turns away gets.
var ErrOpen = errors.New("circuit open")

// State is the breaker's mode.
type State int

// The modes a Breaker can be in.
const (
	Closed State = iota
	Open
	HalfOpen
)

// String returns the name dashboards show.
func (s State) String() string {
	switch s {
	case Closed:
		return "closed"
	case Open:
		return "open"
	}
	return "half-open"
}

// Breaker guards calls to a flaky dependency. Times are milliseconds on a clock
// the caller supplies.
type Breaker struct {
	Threshold int   // the failure limit
	Cooldown  int64 // how long to stay open
	state     State
	failures  int
	openedAt  int64
	wait      int64 // how long this opening lasts
	trial     bool  // a trial call is out
}

// State returns the current mode.
func (b *Breaker) State() State { return b.state }

// Failures returns the current failure count.
func (b *Breaker) Failures() int { return b.failures }

// Allow says whether a call may go ahead.
func (b *Breaker) Allow(now int64) error {
	switch b.state {
	case Open:
		if now-b.openedAt < b.wait {
			return ErrOpen
		}
		b.state, b.trial = HalfOpen, true
	case HalfOpen:
		if b.trial {
			return ErrOpen
		}
		b.trial = true
	}
	return nil
}

func (b *Breaker) open(now, wait int64) {
	b.state, b.openedAt, b.wait, b.failures, b.trial = Open, now, wait, 0, false
}

// Record reports how a call went. nil means it worked.
func (b *Breaker) Record(now int64, err error) {
	switch {
	case errors.Is(err, context.Canceled):
		b.trial = false
	case err == nil:
		b.failures = 0
		if b.state == HalfOpen {
			b.state, b.trial = Closed, false
		}
	case b.state == HalfOpen:
		b.open(now, min(2*b.wait, 8*b.Cooldown))
	case b.state == Closed:
		b.failures++
		if b.failures > b.Threshold {
			b.open(now, b.Cooldown)
		}
	}
}

// Do runs fn if the breaker allows it and records the result.
func (b *Breaker) Do(now int64, fn func() error) error {
	if err := b.Allow(now); err != nil {
		return err
	}
	err := fn()
	b.Record(now, err)
	return err
}

// RetryAt returns the time from which the next trial can go.
func (b *Breaker) RetryAt() int64 { return b.openedAt + b.wait }
`,
		},
		Turns: script(
			local("What Threshold means, since people keep asking: it's how many failures we put up with. The breaker opens on the next failure after that, so a Threshold of 3 opens on the 4th. Add `func (b *Breaker) Failures() int`, the current failure count."),
			local("Read docs/oncall.txt and tell me what it asks of the breaker. No code changes."),
			cloud("Add `var ErrOpen = errors.New(\"circuit open\")`. Any call the breaker turns away gets exactly that. Nothing returns it yet."),
			cloud("From the incident review: hammering a dependency that was still down made things worse, so each failed trial call doubles the wait before the next trial, up to 8 times Cooldown. The wait goes back to Cooldown once the breaker closes. There's no trial logic yet. No code changes."),
			local("Add `func (b *Breaker) Allow(now int64) error`, now in milliseconds. Closed: nil. Open: calls are turned away until the wait is over, and a call at exactly that moment goes through, moving the breaker to half-open as the trial call. Half-open: further calls are turned away until the trial's result is in."),
			cloud("Add `func (b *Breaker) Record(now int64, err error)`, which reports how a call went; nil means it worked. While closed, a success clears the failure count, a failure adds to it, and too many open the breaker. While half-open, a success closes it and a failure opens it again."),
			local("Add `func (b *Breaker) Do(now int64, fn func() error) error`: asks Allow, runs fn if it may, records the result and returns fn's error. fn must not run when the call is turned away; Do returns Allow's error then."),
			cloud("Add `func (s State) String() string`."),
			local("Add `func (b *Breaker) RetryAt() int64`: the time from which the next trial can go, while the breaker is open."),
		),
		Carries: []Carry{
			{
				What:        "the breaker opens on the failure after Threshold failures, not at Threshold",
				Kind:        CarryUser,
				Established: 0,
				Needed:      []int{5},
				Tests:       []string{"TestThresholdFailuresTolerated", "TestToleranceAfterSuccess"},
				Absent:      []string{"put up with", "tolerat", "next failure", "the 4th"},
			},
			{
				What:        "cancelled calls don't touch the failure count, and the state names are fixed (docs/oncall.txt)",
				Kind:        CarryDiscovered,
				Established: 1,
				Needed:      []int{5, 7},
				Source:      "docs/oncall.txt",
				Tests:       []string{"TestCancelledNotAFailure", "TestCancelledKeepsCount", "TestWrappedCancelled", "TestCancelledTrial", "TestStateNames"},
				Absent:      []string{"cancel", "dashboard", "deadline"},
			},
			{
				What:        "each failed trial doubles the wait, up to 8 times Cooldown",
				Kind:        CarryUser,
				Established: 3,
				Needed:      []int{4, 5, 8},
				Tests:       []string{"TestFailedTrialDoubles", "TestDoublingStopsAtEightTimes", "TestRetryAtAfterFailedTrial"},
				Absent:      []string{"doubl", "twice", "8 times", "eight", "incident"},
			},
			{
				What:        "ErrOpen is what any call the breaker turns away gets",
				Kind:        CarryEmbodied,
				Established: 2,
				Needed:      []int{4, 6},
				Tests:       []string{"TestAllowOpenError", "TestHalfOpenError", "TestDoTurnedAwayError"},
				Absent:      []string{"erropen", "sentinel"},
			},
		},
	}
}
