package s2

func longC04() Task {
	return Task{
		ID:     "s2-long-c04-process-supervisor",
		Set:    "long",
		System: system("procvisor, an in-process process supervisor with simulated hosts and an injected clock"),
		Files: map[string]string{
			"go.mod": goMod,
			"docs/operations.md": `# Running the supervisor

Notes from whoever was on call last.

## Exit codes

Our services follow one convention, and the supervisor has to know it:

- 0 means a clean exit.
- 143 means the process went down on a SIGTERM, usually ours, sometimes from an operator on the
  box. It is not a crash and it does not restart, whatever the policy: the unit is stopped and
  stays stopped until somebody starts it.
- 78 means a configuration error (EX_CONFIG). Restarting cannot help, so the unit goes straight
  to failed, whatever the policy, without waiting for any delay.
- Anything else is a crash.

## Hosts

Each host runs one supervisor. The host agent tells it when a process ends and it tells the
agent what to start and stop. If the agent is down, nothing happens until it is back, which is
why nothing in here should block on it.

## Dashboards

The status page reads what the supervisor prints and shows it as it is, so anything printed
should be fit for everyone in the company to read.
`,
			"docs/units.md": `# Unit files

A unit file is a few lines of key=value. Blank lines and lines starting with # do not count.

    # the public API
    name=api
    after=db,cache
    restart=always
    env.PORT=8080

name is required. after is a comma-separated list of the units this one needs, empty by
default. restart is always, on-failure or never, and on-failure by default. env.X sets the
environment variable X for the process, and a unit can have any number of those.

A missing name, an unknown key, an unknown restart policy or a line without an equals sign is a
mistake in the file. Report it. Do not start a unit from a file you only half understood.

## Names

A unit's name ends up in paths and metric labels, so it is limited to lower-case letters, digits
and hyphens. It has to start with a letter and can have at most 32 characters. This holds for
every unit, however it came to exist.

## Where they live

Unit files are kept in the configuration repository and copied onto each host at deploy time.
Nothing in this module reads from disk: the host agent hands us the text.
`,
			"events.go": `package supervisor

import "time"

// Event is one thing the supervisor did or saw. Code is the exit code of an exit.
type Event struct {
	At   time.Duration
	Kind string
	Unit string
	Code int
}

// EventLog keeps the most recent events, oldest first.
type EventLog struct {
	limit  int
	events []Event
}

// NewEventLog returns a log that holds at most limit events.
func NewEventLog(limit int) *EventLog { return &EventLog{limit: limit} }

// Add appends an event, dropping the oldest one if the log is full.
func (l *EventLog) Add(e Event) {
	l.events = append(l.events, e)
	if len(l.events) > l.limit {
		l.events = l.events[len(l.events)-l.limit:]
	}
}

// All returns a copy of the events.
func (l *EventLog) All() []Event { return append([]Event(nil), l.events...) }

// ForUnit returns the events of one unit.
func (l *EventLog) ForUnit(name string) []Event {
	var out []Event
	for _, e := range l.events {
		if e.Unit == name {
			out = append(out, e)
		}
	}
	return out
}
`,
			"host.go": `package supervisor

// Host starts and stops the actual processes. The supervisor only talks to it.
type Host interface {
	Start(name string) error
	Stop(name string)
}

// FakeHost records what it is asked to do, and can refuse to start a unit.
type FakeHost struct {
	Calls  []string
	Refuse map[string]error
}

// NewFakeHost returns a host that does everything it is asked.
func NewFakeHost() *FakeHost { return &FakeHost{Refuse: map[string]error{}} }

// Start records the call, or returns the error set for the unit in Refuse.
func (h *FakeHost) Start(name string) error {
	if err := h.Refuse[name]; err != nil {
		return err
	}
	h.Calls = append(h.Calls, "start "+name)
	return nil
}

// Stop records the call.
func (h *FakeHost) Stop(name string) { h.Calls = append(h.Calls, "stop "+name) }

// Clear forgets the calls so far.
func (h *FakeHost) Clear() { h.Calls = nil }
`,
			"supervisor.go": `package supervisor

import (
	"errors"
	"sort"
	"time"
)

// ErrDuplicate is for a unit name that is already taken.
var ErrDuplicate = errors.New("unit already added")

// Clock reads the time since the host booted.
type Clock func() time.Duration

// Supervisor keeps a set of units. So far it only knows what they are.
type Supervisor struct {
	host  Host
	clock Clock
	units map[string]Unit
}

// New returns a supervisor with no units.
func New(host Host, clock Clock) *Supervisor {
	return &Supervisor{host: host, clock: clock, units: map[string]Unit{}}
}

// Add adds a unit. It does not start anything.
func (s *Supervisor) Add(u Unit) error {
	if _, ok := s.units[u.Name]; ok {
		return ErrDuplicate
	}
	s.units[u.Name] = u
	return nil
}

// Names returns the unit names, sorted.
func (s *Supervisor) Names() []string {
	names := make([]string, 0, len(s.units))
	for name := range s.units {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
`,
			"supervisor_test.go": `package supervisor

import (
	"errors"
	"testing"
	"time"
)

func TestAddKeepsNames(t *testing.T) {
	s := New(NewFakeHost(), func() time.Duration { return 0 })
	for _, name := range []string{"web", "db"} {
		if err := s.Add(Unit{Name: name, Restart: "always"}); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Add(Unit{Name: "db"}); !errors.Is(err, ErrDuplicate) {
		t.Fatalf("a second db: %v", err)
	}
	if got := s.Names(); len(got) != 2 || got[0] != "db" {
		t.Fatalf("Names = %v", got)
	}
}

func TestFakeHostRecordsCalls(t *testing.T) {
	h := NewFakeHost()
	h.Refuse["bad"] = errors.New("no")
	if err := h.Start("good"); err != nil {
		t.Fatal(err)
	}
	if err := h.Start("bad"); err == nil {
		t.Fatal("bad started")
	}
	h.Stop("good")
	if len(h.Calls) != 2 || h.Calls[1] != "stop good" {
		t.Fatalf("Calls = %v", h.Calls)
	}
}

func TestEventLogDropsOldest(t *testing.T) {
	l := NewEventLog(2)
	l.Add(Event{Kind: "a", Unit: "x"})
	l.Add(Event{Kind: "b", Unit: "y"})
	l.Add(Event{Kind: "c", Unit: "x"})
	if got := l.All(); len(got) != 2 || got[0].Kind != "b" {
		t.Fatalf("All = %+v", got)
	}
	if got := l.ForUnit("x"); len(got) != 1 || got[0].Kind != "c" {
		t.Fatalf("ForUnit = %+v", got)
	}
}
`,
			"unit.go": `package supervisor

// Unit describes one process to keep running.
type Unit struct {
	Name    string
	After   []string // the units this one needs running first
	Restart string   // always, on-failure or never
	Env     map[string]string
}
`,
		},
		Hidden: map[string]string{hiddenName: `package supervisor_test

import (
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	supervisor "fixture"
)

type rig struct {
	now  time.Duration
	host *supervisor.FakeHost
	s    *supervisor.Supervisor
}

func newRig(t *testing.T, units ...supervisor.Unit) *rig {
	t.Helper()
	r := &rig{now: time.Hour, host: supervisor.NewFakeHost()}
	r.s = supervisor.New(r.host, func() time.Duration { return r.now })
	for _, u := range units {
		if err := r.s.Add(u); err != nil {
			t.Fatal(err)
		}
	}
	return r
}

func unit(name, restart string, after ...string) supervisor.Unit {
	return supervisor.Unit{Name: name, Restart: restart, After: after}
}

// started has one running unit called api.
func started(t *testing.T, restart string) *rig {
	t.Helper()
	r := newRig(t, unit("api", restart))
	if err := r.s.StartAll(); err != nil {
		t.Fatal(err)
	}
	return r
}

// stack has db, cache after db, api after both and worker after db, all running. Their
// start order is db, cache, api, worker.
func stack(t *testing.T) *rig {
	t.Helper()
	r := newRig(t, unit("worker", "on-failure", "db"), unit("api", "on-failure", "cache", "db"), unit("cache", "on-failure", "db"), unit("db", "on-failure"))
	if err := r.s.StartAll(); err != nil {
		t.Fatal(err)
	}
	return r
}

func (r *rig) state(t *testing.T, name string) supervisor.State {
	t.Helper()
	got, err := r.s.State(name)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func (r *rig) exit(t *testing.T, name string, code int) {
	t.Helper()
	if err := r.s.Exited(name, code); err != nil {
		t.Fatal(err)
	}
}

// crashNow crashes a running unit and brings it back once its delay is over, and returns
// the delay it had to wait, in whole seconds.
func (r *rig) crashNow(t *testing.T, name string) time.Duration {
	t.Helper()
	r.exit(t, name, 1)
	var waited time.Duration
	for r.s.Tick() == 0 {
		r.now += 100 * time.Millisecond
		waited += 100 * time.Millisecond
		if waited > 10*time.Minute {
			t.Fatal("it never restarted")
		}
	}
	return waited.Truncate(time.Second)
}

// crashLoop crashes a running unit until it has failed.
func (r *rig) crashLoop(t *testing.T, name string) {
	t.Helper()
	for range 4 {
		r.crashNow(t, name)
	}
	r.exit(t, name, 1)
}

func TestGetFindsUnits(t *testing.T) {
	r := newRig(t, unit("api", "always"))
	got, err := r.s.Get("api")
	if err != nil || got.Name != "api" || got.Restart != "always" {
		t.Fatalf("Get = %+v, %v", got, err)
	}
	if _, err := r.s.Get("nope"); !errors.Is(err, supervisor.ErrUnknownUnit) {
		t.Fatalf("an unknown name: %v", err)
	}
}

func TestParseUnitReadsAll(t *testing.T) {
	got, err := supervisor.ParseUnit("# comment\n\nname=api\nafter=db,cache\nrestart=always\nenv.PORT=8080\nenv.MODE=a=b\n")
	if err != nil {
		t.Fatal(err)
	}
	want := supervisor.Unit{Name: "api", After: []string{"db", "cache"}, Restart: "always", Env: map[string]string{"PORT": "8080", "MODE": "a=b"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ParseUnit = %+v", got)
	}
}

func TestParseUnitDefaults(t *testing.T) {
	got, err := supervisor.ParseUnit("name=api")
	if err != nil || got.Restart != "on-failure" || len(got.After) != 0 {
		t.Fatalf("ParseUnit = %+v, %v", got, err)
	}
}

func TestParseUnitRefusesMistakes(t *testing.T) {
	for _, text := range []string{"", "after=db", "name=api\nrestart=sometimes", "name=api\ncolor=red", "name=api\nstray", "name=Api", "name=1api", "name=a_b", "name=" + strings.Repeat("a", 33)} {
		if _, err := supervisor.ParseUnit(text); !errors.Is(err, supervisor.ErrBadUnit) {
			t.Errorf("%q: %v", text, err)
		}
	}
	if _, err := supervisor.ParseUnit("name=" + strings.Repeat("a", 32)); err != nil {
		t.Errorf("a name of 32: %v", err)
	}
}

func TestStartOrderRespectsDependencies(t *testing.T) {
	r := stack(t)
	got, err := r.s.StartOrder()
	want := []string{"db", "cache", "api", "worker"}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("StartOrder = %v, %v", got, err)
	}
}

func TestStartOrderFailures(t *testing.T) {
	r := newRig(t, unit("a", "never", "b"), unit("b", "never", "a"))
	if _, err := r.s.StartOrder(); !errors.Is(err, supervisor.ErrCycle) {
		t.Errorf("a cycle: %v", err)
	}
	r = newRig(t, unit("a", "never", "ghost"))
	if _, err := r.s.StartOrder(); !errors.Is(err, supervisor.ErrMissingDep) {
		t.Errorf("a missing dependency: %v", err)
	}
}

func TestStartAllStartsInOrder(t *testing.T) {
	r := stack(t)
	want := []string{"start db", "start cache", "start api", "start worker"}
	if !reflect.DeepEqual(r.host.Calls, want) {
		t.Fatalf("Calls = %v", r.host.Calls)
	}
	if r.state(t, "api") != supervisor.StateRunning {
		t.Fatal("api is not running")
	}
}

func TestStartAllStopsAtARefusal(t *testing.T) {
	boom := errors.New("boom")
	r := newRig(t, unit("a", "never"), unit("b", "never", "a"), unit("c", "never", "b"))
	r.host.Refuse["b"] = boom
	if err := r.s.StartAll(); !errors.Is(err, boom) {
		t.Fatalf("err = %v", err)
	}
	if r.state(t, "a") != supervisor.StateRunning || r.state(t, "c") != supervisor.StateStopped {
		t.Fatalf("calls %v", r.host.Calls)
	}
}

func TestUptimeOfUnits(t *testing.T) {
	r := started(t, "always")
	r.now += time.Minute
	if got, err := r.s.Uptime("api"); err != nil || got != time.Minute {
		t.Fatalf("Uptime = %v, %v", got, err)
	}
	r.exit(t, "api", 1)
	if got, err := r.s.Uptime("api"); err != nil || got != 0 {
		t.Fatalf("after a crash: %v, %v", got, err)
	}
}

func TestEventsRecordStarts(t *testing.T) {
	r := started(t, "always")
	got := r.s.Events()
	if len(got) != 1 || got[0].Kind != "start" || got[0].Unit != "api" || got[0].At != r.now {
		t.Fatalf("Events = %+v", got)
	}
}

func TestRunningIsSorted(t *testing.T) {
	r := stack(t)
	want := []string{"api", "cache", "db", "worker"}
	if got := r.s.Running(); !reflect.DeepEqual(got, want) {
		t.Fatalf("Running = %v", got)
	}
}

func TestLogsOfOneUnit(t *testing.T) {
	r := stack(t)
	r.exit(t, "worker", 3)
	r.exit(t, "api", 1)
	r.now += time.Second + time.Millisecond
	r.s.Tick()
	r.exit(t, "api", 2)
	got, err := r.s.Logs("api", 1)
	if err != nil || len(got) != 1 || got[0].Kind != "exit" || got[0].Code != 2 {
		t.Fatalf("Logs = %+v, %v", got, err)
	}
	all, err := r.s.Logs("api", 10)
	if err != nil || len(all) < 3 || all[0].Kind != "start" || all[len(all)-1].Code != 2 {
		t.Fatalf("Logs = %+v, %v", all, err)
	}
	for _, e := range all {
		if e.Unit != "api" {
			t.Fatalf("Logs = %+v", all)
		}
	}
}

func TestLogsUnknownUnit(t *testing.T) {
	r := newRig(t)
	if _, err := r.s.Logs("nope", 1); !errors.Is(err, supervisor.ErrUnknownUnit) {
		t.Fatalf("err = %v", err)
	}
}

func TestDependentsAreTransitive(t *testing.T) {
	r := stack(t)
	got, err := r.s.Dependents("db")
	if err != nil || !reflect.DeepEqual(got, []string{"cache", "api", "worker"}) {
		t.Fatalf("db: %v, %v", got, err)
	}
	got, err = r.s.Dependents("cache")
	if err != nil || !reflect.DeepEqual(got, []string{"api"}) {
		t.Fatalf("cache: %v, %v", got, err)
	}
}

func TestDependentsUnknownUnit(t *testing.T) {
	r := newRig(t)
	if _, err := r.s.Dependents("nope"); !errors.Is(err, supervisor.ErrUnknownUnit) {
		t.Fatalf("err = %v", err)
	}
}

func TestRestartPolicies(t *testing.T) {
	cases := []struct {
		restart string
		code    int
		want    supervisor.State
	}{
		{"always", 0, supervisor.StateBackoff},
		{"always", 1, supervisor.StateBackoff},
		{"on-failure", 0, supervisor.StateDone},
		{"on-failure", 1, supervisor.StateBackoff},
		{"never", 0, supervisor.StateDone},
		{"never", 1, supervisor.StateFailed},
	}
	for _, tc := range cases {
		r := started(t, tc.restart)
		r.exit(t, "api", tc.code)
		if got := r.state(t, "api"); got != tc.want {
			t.Errorf("%s, exit %d: %s", tc.restart, tc.code, got)
		}
	}
}

func TestExitedUnknownUnit(t *testing.T) {
	r := newRig(t)
	if err := r.s.Exited("nope", 1); !errors.Is(err, supervisor.ErrUnknownUnit) {
		t.Fatalf("err = %v", err)
	}
}

func TestExit143IsNotACrash(t *testing.T) {
	for _, restart := range []string{"always", "on-failure", "never"} {
		r := started(t, restart)
		r.exit(t, "api", 143)
		if got := r.state(t, "api"); got != supervisor.StateStopped {
			t.Errorf("%s: %s", restart, got)
		}
		r.now += time.Hour
		if n := r.s.Tick(); n != 0 {
			t.Errorf("%s: it restarted", restart)
		}
	}
}

func TestExit78GoesStraightToFailed(t *testing.T) {
	for _, restart := range []string{"always", "on-failure"} {
		r := started(t, restart)
		r.exit(t, "api", 78)
		if got := r.state(t, "api"); got != supervisor.StateFailed {
			t.Errorf("%s: %s", restart, got)
		}
	}
}

func TestTickRestartsWhenDelayIsOver(t *testing.T) {
	r := started(t, "on-failure")
	r.exit(t, "api", 1)
	if n := r.s.Tick(); n != 0 {
		t.Fatalf("restarted at once: %d", n)
	}
	r.now += time.Second + time.Millisecond
	if n := r.s.Tick(); n != 1 {
		t.Fatalf("after the delay: %d", n)
	}
	if r.state(t, "api") != supervisor.StateRunning {
		t.Fatal("not running")
	}
	if n := r.s.Tick(); n != 0 {
		t.Fatalf("a second restart: %d", n)
	}
}

func TestBackoffDoubles(t *testing.T) {
	r := started(t, "on-failure")
	var got []time.Duration
	for range 3 {
		got = append(got, r.crashNow(t, "api"))
	}
	want := []time.Duration{time.Second, 2 * time.Second, 4 * time.Second}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("delays %v", got)
	}
}

// spaced crashes a unit over and over with a good while in between, so that no crash
// loop gets in the way, and returns its delays.
func (r *rig) spaced(t *testing.T, name string, n int) []time.Duration {
	t.Helper()
	var got []time.Duration
	for range n {
		r.now += 61 * time.Second
		got = append(got, r.crashNow(t, name))
	}
	return got
}

func TestBackoffCap(t *testing.T) {
	got := started(t, "on-failure").spaced(t, "api", 8)
	if got[5] != 32*time.Second || got[6] != 60*time.Second || got[7] != 60*time.Second {
		t.Fatalf("delays %v", got)
	}
}

func TestCleanExitUnderAlwaysDoesNotGrowDelay(t *testing.T) {
	r := started(t, "always")
	r.exit(t, "api", 0)
	r.now += time.Second + time.Millisecond
	if n := r.s.Tick(); n != 1 {
		t.Fatalf("Tick = %d", n)
	}
	r.exit(t, "api", 0)
	r.now += time.Second + time.Millisecond
	if n := r.s.Tick(); n != 1 {
		t.Fatalf("second time: %d", n)
	}
}

func TestRefusedRestartCountsAsCrash(t *testing.T) {
	r := started(t, "on-failure")
	r.host.Refuse["api"] = errors.New("no")
	r.exit(t, "api", 1)
	r.now += time.Second + time.Millisecond
	if n := r.s.Tick(); n != 0 {
		t.Fatalf("Tick = %d", n)
	}
	if r.state(t, "api") != supervisor.StateBackoff {
		t.Fatal("it should be waiting again")
	}
	if n, err := r.s.CrashCount("api"); err != nil || n != 2 {
		t.Fatalf("CrashCount = %d, %v", n, err)
	}
}

func TestCrashLoopFails(t *testing.T) {
	r := started(t, "always")
	for range 4 {
		r.crashNow(t, "api")
	}
	r.exit(t, "api", 1)
	if got := r.state(t, "api"); got != supervisor.StateFailed {
		t.Fatalf("State = %s", got)
	}
	r.now += time.Hour
	if n := r.s.Tick(); n != 0 {
		t.Fatalf("a failed unit restarted: %d", n)
	}
}

func TestFailureBlocksDependents(t *testing.T) {
	r := stack(t)
	r.crashLoop(t, "cache")
	if r.state(t, "api") != supervisor.StateBlocked || r.state(t, "worker") != supervisor.StateRunning {
		t.Fatalf("api %s, worker %s", r.state(t, "api"), r.state(t, "worker"))
	}
	if !slices.Contains(r.host.Calls, "stop api") || slices.Contains(r.host.Calls, "stop worker") {
		t.Fatalf("Calls = %v", r.host.Calls)
	}
}

func TestCrashLoopStopsDependentsInReverse(t *testing.T) {
	r := stack(t)
	r.crashLoop(t, "db")
	got := r.host.Calls[len(r.host.Calls)-3:]
	if !reflect.DeepEqual(got, []string{"stop worker", "stop api", "stop cache"}) {
		t.Fatalf("Calls = %v", r.host.Calls)
	}
}

func TestStopAllReversesStartOrder(t *testing.T) {
	r := stack(t)
	r.host.Clear()
	r.s.StopAll()
	want := []string{"stop worker", "stop api", "stop cache", "stop db"}
	if !reflect.DeepEqual(r.host.Calls, want) {
		t.Fatalf("Calls = %v", r.host.Calls)
	}
}

func TestStopAllLeavesUnitsStopped(t *testing.T) {
	r := stack(t)
	r.exit(t, "worker", 1)
	r.host.Clear()
	r.s.StopAll()
	for _, name := range r.s.Names() {
		if got := r.state(t, name); got != supervisor.StateStopped {
			t.Errorf("%s: %s", name, got)
		}
	}
	if len(r.host.Calls) != 3 {
		t.Errorf("Calls = %v", r.host.Calls)
	}
	r.now += time.Hour
	if n := r.s.Tick(); n != 0 {
		t.Errorf("it restarted: %d", n)
	}
}

func TestExitAfterStopIsNotACrash(t *testing.T) {
	r := stack(t)
	r.s.StopAll()
	r.exit(t, "api", 1)
	if got := r.state(t, "api"); got != supervisor.StateStopped {
		t.Fatalf("State = %s", got)
	}
	if n, err := r.s.CrashCount("api"); err != nil || n != 0 {
		t.Fatalf("CrashCount = %d, %v", n, err)
	}
}

func TestStopStopsDependentsFirst(t *testing.T) {
	r := stack(t)
	r.host.Clear()
	if err := r.s.Stop("db"); err != nil {
		t.Fatal(err)
	}
	want := []string{"stop worker", "stop api", "stop cache", "stop db"}
	if !reflect.DeepEqual(r.host.Calls, want) {
		t.Fatalf("Calls = %v", r.host.Calls)
	}
}

func TestStopLeavesTheRest(t *testing.T) {
	r := stack(t)
	if err := r.s.Stop("cache"); err != nil {
		t.Fatal(err)
	}
	if r.state(t, "api") != supervisor.StateStopped || r.state(t, "cache") != supervisor.StateStopped {
		t.Fatal("cache or api is still up")
	}
	if r.state(t, "db") != supervisor.StateRunning || r.state(t, "worker") != supervisor.StateRunning {
		t.Fatal("something else went down")
	}
}

func TestStopUnknownUnit(t *testing.T) {
	r := newRig(t)
	if err := r.s.Stop("nope"); !errors.Is(err, supervisor.ErrUnknownUnit) {
		t.Fatalf("err = %v", err)
	}
}

func TestResetBringsThingsBack(t *testing.T) {
	r := stack(t)
	r.crashLoop(t, "cache")
	r.host.Clear()
	if err := r.s.Reset("cache"); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(r.host.Calls, []string{"start cache", "start api"}) {
		t.Fatalf("Calls = %v", r.host.Calls)
	}
	if n, err := r.s.CrashCount("cache"); err != nil || n != 0 {
		t.Fatalf("CrashCount = %d, %v", n, err)
	}
	if got, err := r.s.NextDelay("cache"); err != nil || got != time.Second {
		t.Fatalf("NextDelay = %v, %v", got, err)
	}
}

func TestResetNeedsAFailedUnit(t *testing.T) {
	r := started(t, "always")
	if err := r.s.Reset("api"); !errors.Is(err, supervisor.ErrNotFailed) {
		t.Fatalf("err = %v", err)
	}
}

func TestResetUnknownUnit(t *testing.T) {
	r := newRig(t)
	if err := r.s.Reset("nope"); !errors.Is(err, supervisor.ErrUnknownUnit) {
		t.Fatalf("err = %v", err)
	}
}

func TestCrashCountCounts(t *testing.T) {
	r := started(t, "always")
	r.crashNow(t, "api")
	r.exit(t, "api", 1)
	if n, err := r.s.CrashCount("api"); err != nil || n != 2 {
		t.Fatalf("CrashCount = %d, %v", n, err)
	}
}

func TestCrashCountUsesTheWindow(t *testing.T) {
	r := started(t, "always")
	r.crashNow(t, "api")
	r.now += 61 * time.Second
	r.exit(t, "api", 1)
	if n, err := r.s.CrashCount("api"); err != nil || n != 1 {
		t.Fatalf("CrashCount = %d, %v", n, err)
	}
}

func TestCrashCountUnknownUnit(t *testing.T) {
	r := newRig(t)
	if _, err := r.s.CrashCount("nope"); !errors.Is(err, supervisor.ErrUnknownUnit) {
		t.Fatalf("err = %v", err)
	}
}

func TestNextDelayGrows(t *testing.T) {
	r := started(t, "always")
	if got, err := r.s.NextDelay("api"); err != nil || got != time.Second {
		t.Fatalf("at first: %v, %v", got, err)
	}
	r.crashNow(t, "api")
	r.crashNow(t, "api")
	if got, err := r.s.NextDelay("api"); err != nil || got != 4*time.Second {
		t.Fatalf("after two crashes: %v, %v", got, err)
	}
}

func TestNextDelayUnknownUnit(t *testing.T) {
	r := newRig(t)
	if _, err := r.s.NextDelay("nope"); !errors.Is(err, supervisor.ErrUnknownUnit) {
		t.Fatalf("err = %v", err)
	}
}

func TestStatusTableLines(t *testing.T) {
	r := stack(t)
	lines := strings.Split(strings.TrimSpace(r.s.StatusTable()), "\n")
	if len(lines) != 4 || !strings.HasPrefix(lines[0], "api running ") || !strings.HasPrefix(lines[3], "worker running ") {
		t.Fatalf("StatusTable = %q", lines)
	}
	if !strings.HasSuffix(lines[0], " 0") {
		t.Fatalf("StatusTable = %q", lines)
	}
}

func TestStatusTableUptimeFormat(t *testing.T) {
	r := started(t, "always")
	r.now += time.Hour + 2*time.Minute + 3*time.Second + 900*time.Millisecond
	if got := strings.TrimSpace(r.s.StatusTable()); got != "api running 01:02:03 0" {
		t.Fatalf("StatusTable = %q", got)
	}
}

func TestStatusTableCountsRecentCrashes(t *testing.T) {
	r := started(t, "always")
	r.crashNow(t, "api")
	r.now += 61 * time.Second
	r.exit(t, "api", 1)
	got := strings.Fields(r.s.StatusTable())
	if len(got) != 4 || got[3] != "1" {
		t.Fatalf("StatusTable = %q", got)
	}
}

func TestForecastDoubles(t *testing.T) {
	r := started(t, "always")
	got, err := r.s.Forecast("api", 4)
	want := []time.Duration{time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("Forecast = %v, %v", got, err)
	}
	r.crashNow(t, "api")
	got, err = r.s.Forecast("api", 2)
	if err != nil || !reflect.DeepEqual(got, []time.Duration{2 * time.Second, 4 * time.Second}) {
		t.Fatalf("after a crash: %v, %v", got, err)
	}
}

func TestForecastStopsAtTheCap(t *testing.T) {
	got, err := started(t, "always").s.Forecast("api", 9)
	if err != nil || len(got) != 9 {
		t.Fatalf("Forecast = %v, %v", got, err)
	}
	if got[5] != 32*time.Second || got[6] != 60*time.Second || got[8] != 60*time.Second {
		t.Fatalf("Forecast = %v", got)
	}
}

func TestForecastStaysSaneOnLongRuns(t *testing.T) {
	got, err := started(t, "always").s.Forecast("api", 70)
	if err != nil || len(got) != 70 {
		t.Fatalf("Forecast = %v, %v", got, err)
	}
	for i := 1; i < len(got); i++ {
		if got[i] < got[i-1] {
			t.Fatalf("Forecast = %v", got)
		}
	}
}

func TestForecastUnknownUnit(t *testing.T) {
	r := newRig(t)
	if _, err := r.s.Forecast("nope", 1); !errors.Is(err, supervisor.ErrUnknownUnit) {
		t.Fatalf("err = %v", err)
	}
}

func envUnit() supervisor.Unit {
	u := unit("api", "always", "db", "cache")
	u.Env = map[string]string{"PORT": "8080", "API_TOKEN": "hunter2", "DB_SECRET": "swordfish", "TOKEN_DIR": "/tmp"}
	return u
}

func TestDescribeLines(t *testing.T) {
	r := newRig(t, unit("db", "always"), unit("cache", "always"), envUnit())
	got, err := r.s.Describe("api")
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(got, "\n")
	if lines[0] != "unit api" || lines[1] != "state stopped" || !strings.HasPrefix(lines[2], "up ") || lines[3] != "after db,cache" {
		t.Fatalf("Describe = %q", lines)
	}
	if !strings.HasPrefix(lines[4], "env API_TOKEN=") || lines[6] != "env PORT=8080" || lines[7] != "env TOKEN_DIR=/tmp" {
		t.Fatalf("Describe = %q", lines)
	}
}

func TestDescribeUptimeFormat(t *testing.T) {
	r := started(t, "always")
	r.now += 90*time.Second + 400*time.Millisecond
	got, err := r.s.Describe("api")
	if err != nil || !strings.Contains(got, "\nup 00:01:30\n") {
		t.Fatalf("Describe = %q, %v", got, err)
	}
}

func TestDescribeHidesSecrets(t *testing.T) {
	r := newRig(t, unit("db", "always"), unit("cache", "always"), envUnit())
	got, err := r.s.Describe("api")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got, "hunter2") || strings.Contains(got, "swordfish") {
		t.Fatalf("Describe = %q", got)
	}
	if !strings.Contains(got, "env API_TOKEN=<hidden>\n") || !strings.Contains(got, "env DB_SECRET=<hidden>\n") {
		t.Fatalf("Describe = %q", got)
	}
}

func TestDescribeUnknownUnit(t *testing.T) {
	r := newRig(t)
	if _, err := r.s.Describe("nope"); !errors.Is(err, supervisor.ErrUnknownUnit) {
		t.Fatalf("err = %v", err)
	}
}

func TestCloneCopiesAUnit(t *testing.T) {
	r := newRig(t, unit("db", "always"), unit("cache", "always"), envUnit())
	if err := r.s.Clone("api", "api-2"); err != nil {
		t.Fatal(err)
	}
	got, err := r.s.Get("api-2")
	if err != nil || got.Restart != "always" || got.Env["PORT"] != "8080" || !reflect.DeepEqual(got.After, []string{"db", "cache"}) {
		t.Fatalf("Get = %+v, %v", got, err)
	}
	got.Env["PORT"] = "9"
	if orig, _ := r.s.Get("api"); orig.Env["PORT"] != "8080" {
		t.Fatal("the copy shares its Env with the original")
	}
	if r.state(t, "api-2") != supervisor.StateStopped {
		t.Fatal("the copy has a history")
	}
	if err := r.s.Clone("api", "db"); !errors.Is(err, supervisor.ErrDuplicate) {
		t.Fatalf("a name in use: %v", err)
	}
}

func TestCloneRefusesBadName(t *testing.T) {
	r := newRig(t, unit("api", "always"))
	for _, name := range []string{"API", "2api", "a b", "a_b", strings.Repeat("a", 33)} {
		if err := r.s.Clone("api", name); !errors.Is(err, supervisor.ErrBadUnit) {
			t.Errorf("%q: %v", name, err)
		}
	}
}

func TestCloneUnknownUnit(t *testing.T) {
	r := newRig(t)
	if err := r.s.Clone("nope", "new"); !errors.Is(err, supervisor.ErrUnknownUnit) {
		t.Fatalf("err = %v", err)
	}
}

func TestRestartRestartsUnitAndDependents(t *testing.T) {
	r := stack(t)
	r.host.Clear()
	if err := r.s.Restart("cache"); err != nil {
		t.Fatal(err)
	}
	if len(r.host.Calls) != 4 || r.host.Calls[2] != "start cache" || r.host.Calls[3] != "start api" {
		t.Fatalf("Calls = %v", r.host.Calls)
	}
	if r.state(t, "api") != supervisor.StateRunning {
		t.Fatal("api is not back")
	}
}

func TestRestartIgnoresTheOldProcess(t *testing.T) {
	r := stack(t)
	if err := r.s.Restart("cache"); err != nil {
		t.Fatal(err)
	}
	r.exit(t, "cache", 1)
	if r.state(t, "cache") != supervisor.StateRunning {
		t.Fatal("the old process going away counted as a crash")
	}
}

func TestRestartUnknownUnit(t *testing.T) {
	r := newRig(t)
	if err := r.s.Restart("nope"); !errors.Is(err, supervisor.ErrUnknownUnit) {
		t.Fatalf("err = %v", err)
	}
}

func TestExportLines(t *testing.T) {
	r := newRig(t, unit("db", "always"), unit("cache", "always"))
	u := unit("api", "always")
	u.Env = map[string]string{"Z": "1", "A": "2"}
	if err := r.s.Add(u); err != nil {
		t.Fatal(err)
	}
	want := "api;stopped;A=2,Z=1\ncache;stopped;\ndb;stopped;"
	if got := strings.TrimSpace(r.s.Export()); got != want {
		t.Fatalf("Export = %q", got)
	}
}

func TestExportHidesSecrets(t *testing.T) {
	r := newRig(t, unit("db", "always"), unit("cache", "always"), envUnit())
	got := r.s.Export()
	if strings.Contains(got, "hunter2") || !strings.Contains(got, "API_TOKEN=<hidden>") {
		t.Fatalf("Export = %q", got)
	}
}

func TestStableRunResetsBackoff(t *testing.T) {
	r := started(t, "always")
	r.crashNow(t, "api")
	r.crashNow(t, "api")
	r.now += 5 * time.Minute
	if got := r.crashNow(t, "api"); got != time.Second {
		t.Fatalf("delay %v", got)
	}
	if n, err := r.s.CrashCount("api"); err != nil || n < 1 {
		t.Fatalf("CrashCount = %d, %v", n, err)
	}
}

func TestNextDelaySeesAStableRun(t *testing.T) {
	r := started(t, "always")
	r.crashNow(t, "api")
	r.crashNow(t, "api")
	r.now += 5 * time.Minute
	if got, err := r.s.NextDelay("api"); err != nil || got != time.Second {
		t.Fatalf("NextDelay = %v, %v", got, err)
	}
}

func TestFailedAndBlockedLists(t *testing.T) {
	r := stack(t)
	r.crashLoop(t, "db")
	if got := r.s.Failed(); !reflect.DeepEqual(got, []string{"db"}) {
		t.Fatalf("Failed = %v", got)
	}
	if got := r.s.Blocked(); !reflect.DeepEqual(got, []string{"api", "cache", "worker"}) {
		t.Fatalf("Blocked = %v", got)
	}
}

func TestRecentEventsLookBack(t *testing.T) {
	r := started(t, "always")
	r.now += time.Minute
	r.exit(t, "api", 1)
	got := r.s.RecentEvents(30 * time.Second)
	if len(got) != 1 || got[0].Kind != "exit" {
		t.Fatalf("RecentEvents = %+v", got)
	}
	if got := r.s.RecentEvents(time.Hour); len(got) != 2 {
		t.Fatalf("a long look back: %+v", got)
	}
}
`},
		Solution: map[string]string{
			"events.go": `package supervisor

import "time"

// Event is one thing the supervisor did or saw. Code is the exit code of an exit.
type Event struct {
	At   time.Duration
	Kind string
	Unit string
	Code int
}

// EventLog keeps the most recent events, oldest first.
type EventLog struct {
	limit  int
	events []Event
}

// NewEventLog returns a log that holds at most limit events.
func NewEventLog(limit int) *EventLog { return &EventLog{limit: limit} }

// Add appends an event, dropping the oldest one if the log is full.
func (l *EventLog) Add(e Event) {
	l.events = append(l.events, e)
	if len(l.events) > l.limit {
		l.events = l.events[len(l.events)-l.limit:]
	}
}

// All returns a copy of the events.
func (l *EventLog) All() []Event { return append([]Event(nil), l.events...) }

// ForUnit returns the events of one unit.
func (l *EventLog) ForUnit(name string) []Event {
	var out []Event
	for _, e := range l.events {
		if e.Unit == name {
			out = append(out, e)
		}
	}
	return out
}

// Since returns the events at or after t.
func (l *EventLog) Since(t time.Duration) []Event {
	var out []Event
	for _, e := range l.events {
		if e.At >= t {
			out = append(out, e)
		}
	}
	return out
}

// Events returns everything in the supervisor's log.
func (s *Supervisor) Events() []Event { return s.log.All() }

// RecentEvents returns the events of the last d.
func (s *Supervisor) RecentEvents(d time.Duration) []Event {
	return s.log.Since(s.clock() - d)
}

// Logs returns the last n events of a unit, oldest first.
func (s *Supervisor) Logs(name string, n int) ([]Event, error) {
	if _, err := s.entryOf(name); err != nil {
		return nil, err
	}
	events := s.log.ForUnit(name)
	return events[max(len(events)-n, 0):], nil
}
`,
			"graph.go": `package supervisor

import (
	"errors"
	"fmt"
	"sort"
)

var (
	// ErrCycle is for units that depend on each other in a circle.
	ErrCycle = errors.New("dependency cycle")
	// ErrMissingDep is for a unit that needs a unit we do not have.
	ErrMissingDep = errors.New("missing dependency")
)

// edges returns how many units each unit waits for, and which units wait for each.
func (s *Supervisor) edges() (map[string]int, map[string][]string, error) {
	blocked := map[string]int{}
	waiting := map[string][]string{}
	for name, e := range s.units {
		for _, dep := range e.unit.After {
			if _, ok := s.units[dep]; !ok {
				return nil, nil, fmt.Errorf("%s needs %s: %w", name, dep, ErrMissingDep)
			}
			blocked[name]++
			waiting[dep] = append(waiting[dep], name)
		}
	}
	return blocked, waiting, nil
}

// StartOrder returns every unit after all it depends on. Among the units free to go
// next, the first by name goes first.
func (s *Supervisor) StartOrder() ([]string, error) {
	blocked, waiting, err := s.edges()
	if err != nil {
		return nil, err
	}
	var ready, order []string
	for name := range s.units {
		if blocked[name] == 0 {
			ready = append(ready, name)
		}
	}
	for len(ready) > 0 {
		sort.Strings(ready)
		name := ready[0]
		ready = ready[1:]
		order = append(order, name)
		for _, next := range waiting[name] {
			if blocked[next]--; blocked[next] == 0 {
				ready = append(ready, next)
			}
		}
	}
	if len(order) != len(s.units) {
		return nil, ErrCycle
	}
	return order, nil
}

// Dependents returns the units that depend on name, directly or through others, in
// StartOrder.
func (s *Supervisor) Dependents(name string) ([]string, error) {
	if _, err := s.entryOf(name); err != nil {
		return nil, err
	}
	order, err := s.StartOrder()
	if err != nil {
		return nil, err
	}
	affected := map[string]bool{name: true}
	var out []string
	for _, candidate := range order {
		for _, dep := range s.units[candidate].unit.After {
			if affected[dep] {
				affected[candidate] = true
				out = append(out, candidate)
				break
			}
		}
	}
	return out, nil
}
`,
			"report.go": `package supervisor

import (
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"
)

// clockFormat shows a duration the way everything we print shows it.
func clockFormat(d time.Duration) string {
	total := int64(d / time.Second)
	return fmt.Sprintf("%02d:%02d:%02d", total/3600, total/60%60, total%60)
}

// envLines returns KEY=value per variable, sorted by key, with secrets hidden.
func envLines(env map[string]string) []string {
	out := make([]string, 0, len(env))
	for _, key := range slices.Sorted(maps.Keys(env)) {
		value := env[key]
		if strings.HasSuffix(key, "_TOKEN") || strings.HasSuffix(key, "_SECRET") {
			value = "<hidden>"
		}
		out = append(out, key+"="+value)
	}
	return out
}

// CrashCount returns the crashes that count toward a crash loop right now.
func (s *Supervisor) CrashCount(name string) (int, error) {
	e, err := s.entryOf(name)
	if err != nil {
		return 0, err
	}
	return recentCrashes(e, s.clock()), nil
}

// NextDelay returns how long a unit would wait if it crashed right now.
func (s *Supervisor) NextDelay(name string) (time.Duration, error) {
	e, err := s.entryOf(name)
	if err != nil {
		return 0, err
	}
	return backoffFor(s.currentStreak(e)), nil
}

// Forecast returns the next n delays a unit would get if it kept crashing.
func (s *Supervisor) Forecast(name string, n int) ([]time.Duration, error) {
	e, err := s.entryOf(name)
	if err != nil {
		return nil, err
	}
	out := make([]time.Duration, 0, n)
	for i := range n {
		out = append(out, backoffFor(s.currentStreak(e)+i))
	}
	return out, nil
}

// StatusTable returns one line per unit, sorted by name.
func (s *Supervisor) StatusTable() string {
	var b strings.Builder
	for _, name := range s.Names() {
		e := s.units[name]
		fmt.Fprintf(&b, "%s %s %s %d\n", name, e.state, clockFormat(s.uptime(e)), recentCrashes(e, s.clock()))
	}
	return b.String()
}

// Describe returns the operator's view of one unit, one item per line.
func (s *Supervisor) Describe(name string) (string, error) {
	e, err := s.entryOf(name)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "unit %s\nstate %s\nup %s\nafter %s\n", name, e.state, clockFormat(s.uptime(e)), strings.Join(e.unit.After, ","))
	for _, line := range envLines(e.unit.Env) {
		fmt.Fprintf(&b, "env %s\n", line)
	}
	return b.String(), nil
}

// Export returns one line per unit, sorted by name, with fields separated by semicolons.
func (s *Supervisor) Export() string {
	var b strings.Builder
	for _, name := range s.Names() {
		e := s.units[name]
		fmt.Fprintf(&b, "%s;%s;%s\n", name, e.state, strings.Join(envLines(e.unit.Env), ","))
	}
	return b.String()
}
`,
			"restart.go": `package supervisor

import (
	"errors"
	"slices"
	"time"
)

// ErrNotFailed is for a Reset of a unit that has not failed.
var ErrNotFailed = errors.New("unit has not failed")

const (
	exitTerminated = 143 // went down on a SIGTERM: not a crash, stays stopped
	exitBadConfig  = 78  // a configuration error: restarting cannot help

	firstBackoff = time.Second
	maxBackoff   = 60 * time.Second

	crashLimit  = 5
	crashWindow = time.Minute
	stableAfter = 5 * time.Minute
)

// backoffFor returns the delay after a streak of crashes.
func backoffFor(streak int) time.Duration {
	if streak > 10 {
		return maxBackoff
	}
	return min(firstBackoff<<streak, maxBackoff)
}

// recentCrashes returns how many crashes count toward a crash loop right now.
func recentCrashes(e *entry, now time.Duration) int {
	n := 0
	for _, at := range e.crashes {
		if now-at < crashWindow {
			n++
		}
	}
	return n
}

// fail puts a unit in StateFailed and blocks everything that depends on it.
func (s *Supervisor) fail(e *entry) {
	e.state = StateFailed
	s.record("failed", e.unit.Name, 0)
	dependents, err := s.Dependents(e.unit.Name)
	if err != nil {
		return
	}
	slices.Reverse(dependents)
	for _, name := range dependents {
		d := s.units[name]
		s.halt(d)
		d.state = StateBlocked
	}
}

// crashed records a crash and decides what becomes of the unit.
func (s *Supervisor) crashed(e *entry) {
	now := s.clock()
	e.crashes = append(e.crashes, now)
	if e.unit.Restart == "never" || recentCrashes(e, now) >= crashLimit {
		s.fail(e)
		return
	}
	e.streak = s.currentStreak(e)
	e.state, e.restartAt = StateBackoff, now+backoffFor(e.streak)
	e.streak++
}

// currentStreak returns the streak a crash right now would see: a long enough run
// starts it over.
func (s *Supervisor) currentStreak(e *entry) int {
	if s.uptime(e) >= stableAfter {
		return 0
	}
	return e.streak
}

// Exited is what the host calls when a process ends.
func (s *Supervisor) Exited(name string, code int) error {
	e, err := s.entryOf(name)
	if err != nil {
		return err
	}
	s.record("exit", name, code)
	switch {
	case e.stopping:
		e.stopping = false
	case code == exitTerminated:
		e.state = StateStopped
	case code == exitBadConfig:
		s.fail(e)
	case code == 0 && e.unit.Restart == "always":
		e.state, e.restartAt = StateBackoff, s.clock()+firstBackoff
	case code == 0:
		e.state = StateDone
	default:
		s.crashed(e)
	}
	return nil
}

// Tick restarts the units whose delay is over and returns how many.
func (s *Supervisor) Tick() int {
	now := s.clock()
	restarted := 0
	for _, name := range s.Names() {
		e := s.units[name]
		if e.state != StateBackoff || now < e.restartAt {
			continue
		}
		if err := s.launch(e, "restart"); err != nil {
			s.crashed(e)
			continue
		}
		restarted++
	}
	return restarted
}

// ready reports whether everything a unit needs is up.
func (s *Supervisor) ready(e *entry) bool {
	for _, dep := range e.unit.After {
		if state := s.units[dep].state; state != StateRunning && state != StateDone {
			return false
		}
	}
	return true
}

// Reset forgets a failed unit's crashes and backoff and starts it, and then whatever it
// blocked that can now run.
func (s *Supervisor) Reset(name string) error {
	e, err := s.entryOf(name)
	if err != nil {
		return err
	}
	if e.state != StateFailed {
		return ErrNotFailed
	}
	e.crashes, e.streak = nil, 0
	if err := s.launch(e, "start"); err != nil {
		return err
	}
	dependents, err := s.Dependents(name)
	if err != nil {
		return err
	}
	for _, dep := range dependents {
		if d := s.units[dep]; d.state == StateBlocked && s.ready(d) {
			if err := s.launch(d, "start"); err != nil {
				return err
			}
		}
	}
	return nil
}
`,
			"stop.go": `package supervisor

import "slices"

// halt stops one unit through the host if it has a process, and leaves it stopped.
func (s *Supervisor) halt(e *entry) {
	if e.state == StateRunning {
		s.host.Stop(e.unit.Name)
		e.stopping = true
		s.record("stop", e.unit.Name, 0)
	}
	if e.state == StateRunning || e.state == StateBackoff {
		e.state = StateStopped
	}
}

// StopAll stops every running or backing-off unit, in the reverse of StartOrder.
func (s *Supervisor) StopAll() {
	order, err := s.StartOrder()
	if err != nil {
		return
	}
	slices.Reverse(order)
	for _, name := range order {
		s.halt(s.units[name])
	}
}

// Stop stops a unit and everything that depends on it, in the reverse of StartOrder.
func (s *Supervisor) Stop(name string) error {
	dependents, err := s.Dependents(name)
	if err != nil {
		return err
	}
	order := append([]string{name}, dependents...)
	slices.Reverse(order)
	for _, target := range order {
		s.halt(s.units[target])
	}
	return nil
}

// Restart stops a unit and its dependents and starts them again in StartOrder. Its
// crash history stays.
func (s *Supervisor) Restart(name string) error {
	if err := s.Stop(name); err != nil {
		return err
	}
	dependents, err := s.Dependents(name)
	if err != nil {
		return err
	}
	for _, target := range append([]string{name}, dependents...) {
		if e := s.units[target]; e.state == StateStopped {
			if err := s.launch(e, "start"); err != nil {
				return err
			}
		}
	}
	return nil
}
`,
			"supervisor.go": `package supervisor

import (
	"errors"
	"slices"
	"sort"
	"time"
)

var (
	// ErrDuplicate is for a unit name that is already taken.
	ErrDuplicate = errors.New("unit already added")
	// ErrUnknownUnit is for a name we do not have.
	ErrUnknownUnit = errors.New("unknown unit")
)

// Clock reads the time since the host booted.
type Clock func() time.Duration

// State is where a unit stands.
type State string

const (
	StateStopped State = "stopped"
	StateRunning State = "running"
	StateBackoff State = "backoff"
	StateFailed  State = "failed"
	StateDone    State = "done"
	StateBlocked State = "blocked"
)

// entry is a unit and what has happened to it.
type entry struct {
	unit      Unit
	state     State
	startedAt time.Duration
	restartAt time.Duration
	crashes   []time.Duration
	streak    int  // crashes since its backoff last started over
	stopping  bool // we asked the host to stop it
}

// Supervisor keeps a set of units running.
type Supervisor struct {
	host  Host
	clock Clock
	units map[string]*entry
	log   *EventLog
}

// New returns a supervisor with no units.
func New(host Host, clock Clock) *Supervisor {
	return &Supervisor{host: host, clock: clock, units: map[string]*entry{}, log: NewEventLog(100)}
}

// Add adds a unit. It does not start anything.
func (s *Supervisor) Add(u Unit) error {
	if _, ok := s.units[u.Name]; ok {
		return ErrDuplicate
	}
	s.units[u.Name] = &entry{unit: u, state: StateStopped}
	return nil
}

// Names returns the unit names, sorted.
func (s *Supervisor) Names() []string {
	names := make([]string, 0, len(s.units))
	for name := range s.units {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// entryOf finds a unit that later calls need.
func (s *Supervisor) entryOf(name string) (*entry, error) {
	e, ok := s.units[name]
	if !ok {
		return nil, ErrUnknownUnit
	}
	return e, nil
}

// Get returns a unit.
func (s *Supervisor) Get(name string) (Unit, error) {
	e, ok := s.units[name]
	if !ok {
		return Unit{}, ErrUnknownUnit
	}
	return e.unit, nil
}

// State returns where a unit stands.
func (s *Supervisor) State(name string) (State, error) {
	e, err := s.entryOf(name)
	if err != nil {
		return "", err
	}
	return e.state, nil
}

func (s *Supervisor) record(kind, name string, code int) {
	s.log.Add(Event{At: s.clock(), Kind: kind, Unit: name, Code: code})
}

// launch starts a unit through the host.
func (s *Supervisor) launch(e *entry, kind string) error {
	if err := s.host.Start(e.unit.Name); err != nil {
		return err
	}
	e.state, e.startedAt = StateRunning, s.clock()
	s.record(kind, e.unit.Name, 0)
	return nil
}

// StartAll starts every unit in StartOrder. It stops at the first the host refuses.
func (s *Supervisor) StartAll() error {
	order, err := s.StartOrder()
	if err != nil {
		return err
	}
	for _, name := range order {
		if err := s.launch(s.units[name], "start"); err != nil {
			return err
		}
	}
	return nil
}

func (s *Supervisor) uptime(e *entry) time.Duration {
	if e.state != StateRunning {
		return 0
	}
	return s.clock() - e.startedAt
}

// Uptime returns how long a running unit has been up, and 0 for one that is not running.
func (s *Supervisor) Uptime(name string) (time.Duration, error) {
	e, err := s.entryOf(name)
	if err != nil {
		return 0, err
	}
	return s.uptime(e), nil
}

// inState returns the units in one state, sorted.
func (s *Supervisor) inState(state State) []string {
	var out []string
	for _, name := range s.Names() {
		if s.units[name].state == state {
			out = append(out, name)
		}
	}
	return out
}

// Running returns the running units.
func (s *Supervisor) Running() []string { return s.inState(StateRunning) }

// Failed returns the failed units.
func (s *Supervisor) Failed() []string { return s.inState(StateFailed) }

// Blocked returns the blocked units.
func (s *Supervisor) Blocked() []string { return s.inState(StateBlocked) }

// Clone adds a copy of a unit under a new name, with its own Env and no history.
func (s *Supervisor) Clone(name, newName string) error {
	e, err := s.entryOf(name)
	if err != nil {
		return err
	}
	if !validName(newName) {
		return ErrBadUnit
	}
	u := e.unit
	u.Name = newName
	u.After = slices.Clone(u.After)
	u.Env = map[string]string{}
	for k, v := range e.unit.Env {
		u.Env[k] = v
	}
	return s.Add(u)
}
`,
			"unit.go": `package supervisor

import (
	"errors"
	"strings"
)

// ErrBadUnit is for a unit file or unit name that cannot be accepted.
var ErrBadUnit = errors.New("bad unit")

// Unit describes one process to keep running.
type Unit struct {
	Name    string
	After   []string // the units this one needs running first
	Restart string   // always, on-failure or never
	Env     map[string]string
}

// validName reports whether a unit may have this name, as docs/units.md lays it down.
func validName(name string) bool {
	if name == "" || len(name) > 32 || name[0] < 'a' || name[0] > 'z' {
		return false
	}
	for _, c := range name {
		if (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '-' {
			return false
		}
	}
	return true
}

// setField applies one key=value line of a unit file.
func (u *Unit) setField(key, value string) error {
	name, isEnv := strings.CutPrefix(key, "env.")
	switch {
	case isEnv && name != "":
		u.Env[name] = value
	case key == "name" && validName(value):
		u.Name = value
	case key == "after":
		u.After = nil
		for _, dep := range strings.Split(value, ",") {
			if dep != "" {
				u.After = append(u.After, dep)
			}
		}
	case key == "restart" && (value == "always" || value == "on-failure" || value == "never"):
		u.Restart = value
	default:
		return ErrBadUnit
	}
	return nil
}

// ParseUnit reads a unit file.
func ParseUnit(text string) (Unit, error) {
	u := Unit{Restart: "on-failure", Env: map[string]string{}}
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			return Unit{}, ErrBadUnit
		}
		if err := u.setField(key, value); err != nil {
			return Unit{}, err
		}
	}
	if u.Name == "" {
		return Unit{}, ErrBadUnit
	}
	return u, nil
}
`,
		},
		Turns: script(
			cloud("Add `func (s *Supervisor) Get(name string) (Unit, error)`, returning `ErrUnknownUnit`, a new `errors.New(\"unknown unit\")`, for a name we don't have. Anything else that takes a unit name does the same from now on."),
			local("Read docs/units.md and add `func ParseUnit(text string) (Unit, error)` to unit.go, with `ErrBadUnit`, a new `errors.New(\"bad unit\")`, for whatever the file calls a mistake or doesn't allow."),
			local("Read docs/operations.md and tell me briefly what it says about exit codes. No code changes."),
			cloud("A heads-up on restarts, which come later, no code for them today: a unit's backoff starts at 1 second and doubles with each crash, capped at 32 seconds. Today add a new graph.go with `func (s *Supervisor) StartOrder() ([]string, error)`: every unit after all it depends on, and among those free to go next, the first by name. `ErrCycle`, a new `errors.New(\"dependency cycle\")`, and `ErrMissingDep`, a new `errors.New(\"missing dependency\")`, are the two ways that can fail."),
			cloud("Add `type State string` with `StateStopped`, `StateRunning`, `StateBackoff`, `StateFailed`, `StateDone` and `StateBlocked`, each holding its word in lower case, `func (s *Supervisor) State(name string) (State, error)`, and `func (s *Supervisor) StartAll() error`, which starts every unit through the host in StartOrder. If the host refuses one, it stops there and returns that error. For later: whenever we stop several units, it's the exact reverse of StartOrder, no exceptions."),
			cloud("Add `func (s *Supervisor) Uptime(name string) (time.Duration, error)`: how long a running unit has been up, and 0 for one that isn't running. And for when we print these, no code yet: a duration is always shown as HH:MM:SS, rounded down to the second."),
			local("Wire in events.go. The supervisor owns an EventLog holding 100 events, and `func (s *Supervisor) Events() []Event` returns them. StartAll adds an event of Kind start per unit."),
			local("Security asked for this, nothing to build yet: the values of env variables whose names end in _TOKEN or _SECRET must never appear in anything we print. We show `<hidden>` instead. Today, just add `func (s *Supervisor) Running() []string`, the running units, sorted."),
			local("Add `func (s *Supervisor) Logs(name string, n int) ([]Event, error)` to events.go: the last n events of that unit, oldest first. EventLog.ForUnit does most of it."),
			cloud("Add to graph.go `func (s *Supervisor) Dependents(name string) ([]string, error)`: the units that depend on it, directly or through others, in StartOrder."),
			cloud("Add `func (s *Supervisor) Exited(name string, code int) error`, which the host calls when a process ends, with an event of Kind exit holding the code. Under `always` a unit restarts after any exit, under `on-failure` only after a crash, and under `never` not at all. For now that just means StateBackoff. A crash under `never` is StateFailed, and a clean exit that doesn't restart is StateDone."),
			cloud("Add the restarts. A crash holds the unit in StateBackoff for its backoff delay, and `func (s *Supervisor) Tick() int` restarts through the host the units whose delay is over, and returns how many. A start the host refuses counts as one more crash. A clean exit under `always` waits only the shortest delay and doesn't make later ones longer."),
			cloud("Crash loops. A unit that crashes 5 times within a minute goes to StateFailed and stays there, with no more restarts. Everything that depends on it, directly or not, is stopped through the host and goes to StateBlocked."),
			local("Add a new stop.go with `func (s *Supervisor) StopAll()`: it stops every running unit through the host and leaves it and any unit waiting to restart in StateStopped. When Exited arrives later for a unit we stopped, that's just the process going away, whatever its code."),
			local("Add to stop.go `func (s *Supervisor) Stop(name string) error`: it does the same for that unit and everything that depends on it."),
			cloud("Add `func (s *Supervisor) Reset(name string) error` for a unit in StateFailed, and `ErrNotFailed`, a new `errors.New(\"unit has not failed\")`, for any other. It forgets the unit's crashes and backoff and starts it, and then starts whatever it blocked that can now run, in StartOrder."),
			local("Add `func (s *Supervisor) CrashCount(name string) (int, error)`, the crashes that count toward a crash loop right now, and `func (s *Supervisor) NextDelay(name string) (time.Duration, error)`, how long the unit would wait if it crashed right now."),
			local("Add a new report.go with `func (s *Supervisor) StatusTable() string`: one line per unit, sorted by name, with the name, the state, the uptime and the CrashCount, separated by single spaces."),
			local("Ops is seeing restart storms, so the longest backoff goes from where it is to 60 seconds. Please make the change."),
			cloud("Add to report.go `func (s *Supervisor) Forecast(name string, n int) ([]time.Duration, error)`, for the capacity page: the next n delays that unit would get if it kept crashing."),
			cloud("Add to report.go `func (s *Supervisor) Describe(name string) (string, error)`, the operator's view of one unit, one item per line: `unit <name>`, `state <state>`, `up <uptime>`, `after <units, comma-separated>`, and then `env <KEY>=<value>` per variable, sorted by key."),
			cloud("Add `func (s *Supervisor) Clone(name, newName string) error` for scaling out: a copy of the unit under a new name, with its own Env map and no history. A name already in use gets ErrDuplicate."),
			local("Add to stop.go `func (s *Supervisor) Restart(name string) error`: Stop on that unit, and then it and its dependents start again in StartOrder. Its crash history stays."),
			local("Add to report.go `func (s *Supervisor) Export() string`: a line per unit, sorted by name, with the name, the state and its env as KEY=VALUE pairs sorted by key and joined by commas. Those three are separated by semicolons."),
			cloud("A unit that stayed up for at least 5 minutes before it crashed has earned a clean slate: its backoff starts over from the beginning. The crash still counts."),
			local("Add `func (s *Supervisor) Failed() []string` and `func (s *Supervisor) Blocked() []string`, like Running."),
			cloud("supervisor.go is too big. Move Exited, Tick, Reset and all the crash handling to a new restart.go, with the constants they use at the top. No behaviour changes."),
			local("Add to events.go `func (l *EventLog) Since(t time.Duration) []Event`, the events at or after t, and `func (s *Supervisor) RecentEvents(d time.Duration) []Event`, the events of the last d."),
		),
		Carries: []Carry{
			{
				What:        "anything taking a unit name returns ErrUnknownUnit when there is no such unit",
				Kind:        CarryEmbodied,
				Established: 0,
				Needed:      []int{8, 9, 10, 14, 15, 16, 19, 20, 21, 22},
				Tests:       []string{"TestLogsUnknownUnit", "TestDependentsUnknownUnit", "TestExitedUnknownUnit", "TestStopUnknownUnit", "TestResetUnknownUnit", "TestCrashCountUnknownUnit", "TestNextDelayUnknownUnit", "TestForecastUnknownUnit", "TestDescribeUnknownUnit", "TestCloneUnknownUnit", "TestRestartUnknownUnit"},
				Absent:      []string{"errunknownunit", "unknown unit", "a name we don't have"},
			},
			{
				What:        "a unit's name is lower-case letters, digits and hyphens, starts with a letter, 32 characters at most (docs/units.md)",
				Kind:        CarryDiscovered,
				Established: 1,
				Needed:      []int{21},
				Source:      "docs/units.md",
				Tests:       []string{"TestCloneRefusesBadName"},
				Absent:      []string{"units.md", "lower-case letters", "32 characters", "hyphens"},
			},
			{
				What:        "exit 143 is not a crash and stays stopped, exit 78 goes straight to failed (docs/operations.md)",
				Kind:        CarryDiscovered,
				Established: 2,
				Needed:      []int{10},
				Source:      "docs/operations.md",
				Tests:       []string{"TestExit143IsNotACrash", "TestExit78GoesStraightToFailed"},
				Absent:      []string{"operations.md", "143", "78", "sigterm", "ex_config"},
			},
			{
				What:        "backoff starts at 1 second and doubles with each crash, up to a cap",
				Kind:        CarryUser,
				Established: 3,
				Needed:      []int{11, 16, 19},
				Tests:       []string{"TestBackoffDoubles", "TestBackoffCap", "TestNextDelayGrows", "TestForecastDoubles", "TestForecastStopsAtTheCap"},
				Absent:      []string{"1 second", "doubles", "32 seconds", "backoff starts at"},
			},
			{
				What:        "whenever several units stop, it is the exact reverse of StartOrder",
				Kind:        CarryUser,
				Established: 4,
				Needed:      []int{12, 13, 14},
				Tests:       []string{"TestStopAllReversesStartOrder", "TestStopStopsDependentsFirst", "TestCrashLoopStopsDependentsInReverse"},
				Absent:      []string{"exact reverse", "reverse of", "no exceptions"},
			},
			{
				What:        "a duration is always printed as HH:MM:SS, rounded down to the second",
				Kind:        CarryUser,
				Established: 5,
				Needed:      []int{17, 20},
				Tests:       []string{"TestStatusTableUptimeFormat", "TestDescribeUptimeFormat"},
				Absent:      []string{"hh:mm:ss", "rounded down"},
			},
			{
				What:        "the values of env variables ending in _TOKEN or _SECRET are printed as <hidden>",
				Kind:        CarryUser,
				Established: 7,
				Needed:      []int{20, 23},
				Tests:       []string{"TestDescribeHidesSecrets", "TestExportHidesSecrets"},
				Absent:      []string{"_token", "_secret", "<hidden>", "security asked"},
			},
			{
				What:        "5 crashes within a minute is a crash loop, so only crashes in that window count",
				Kind:        CarryUser,
				Established: 12,
				Needed:      []int{16, 17},
				Tests:       []string{"TestCrashCountUsesTheWindow", "TestStatusTableCountsRecentCrashes"},
				Absent:      []string{"within a minute", "5 times"},
			},
			{
				What:        "revised: the longest backoff is 60 seconds",
				Kind:        CarryUser,
				Established: 18,
				Needed:      []int{19},
				Tests:       []string{"TestBackoffCap", "TestForecastStopsAtTheCap"},
				Absent:      []string{"60 seconds", "restart storms"},
			},
		},
	}
}
