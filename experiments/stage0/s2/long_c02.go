package s2

func longC02() Task {
	return Task{
		ID:     "s2-long-c02-lease-manager",
		Set:    "long",
		System: system("leased, an in-process lease manager with fencing tokens and an injected clock"),
		Files: map[string]string{
			"go.mod": goMod,
			"clock.go": `package leases

import "time"

// Clock reads the current time.
type Clock func() time.Time

// FakeClock is a clock that only moves when told to.
type FakeClock struct{ now time.Time }

// NewFakeClock returns a clock standing at start.
func NewFakeClock(start time.Time) *FakeClock { return &FakeClock{now: start} }

// Now returns the time the clock stands at.
func (c *FakeClock) Now() time.Time { return c.now }

// Advance moves the clock forward by d.
func (c *FakeClock) Advance(d time.Duration) { c.now = c.now.Add(d) }
`,
			"docs/policy.md": `# Lease policy

Agreed with the teams that build on the lease service. Changing any of this needs their
sign-off, so ask before you do.

## Time to live

Clients ask for a TTL, but they do not get whatever they ask for, because a client that dies
holding a long lease blocks everyone behind it, and a very short one thrashes.

- A TTL of zero means the default, which is 30 seconds.
- Anything shorter than 5 seconds is raised to 5 seconds.
- Anything longer than 5 minutes is lowered to 5 minutes.

These limits hold every time a TTL takes effect, however it got to us.

## Owner names

Clients send their owner name however their configuration happens to spell it. "Alice",
"alice " and " ALICE" are the same client: leading and trailing spaces do not count and case
is ignored. We store and return the trimmed lower-case form, and compare only that.

## Lease names

Lease names are chosen by the teams and contain no spaces. They are case-sensitive and we do
not touch them: "DB" and "db" are two different leases.

## Availability

The service runs as one process with a warm standby. Failover takes the standby a few seconds,
during which clients see errors and retry with backoff. Nothing in the code depends on this.
`,
			"docs/wire.md": `# Snapshot format

The standby restores from a snapshot, a plain text file. Each line is one item, with fields
separated by single spaces, and the file ends with a newline.

    next <token>
    lease <name> <owner> <token> <expires>
    high <name> <token>
    wait <name> <owner> <ttl>

- next comes first and holds the token to hand out next.
- lease lines come after it, ordered by name. expires is in Unix seconds.
- high lines come after those, ordered by name, one for every lease name that was ever granted.
  token is the newest token granted on that name, whether or not the lease is still in force.
- wait lines come last, ordered by name and, within a name, in queue order. ttl is in whole
  seconds.

A lease whose time is already up when the snapshot is taken is left out, though its high
line stays. Snapshots stay inside the service, so they hold owner names in full.
`,
			"journal.go": `package leases

import "time"

// Event is one thing that happened to a lease.
type Event struct {
	At          time.Time
	Kind        string
	Name, Owner string
}

// Journal is the audit trail of a manager, oldest event first.
type Journal struct {
	events []Event
}

// Add appends an event.
func (j *Journal) Add(e Event) { j.events = append(j.events, e) }

// Events returns a copy of all the events.
func (j *Journal) Events() []Event { return append([]Event(nil), j.events...) }

// OfKind returns the events of one kind.
func (j *Journal) OfKind(kind string) []Event {
	var out []Event
	for _, e := range j.events {
		if e.Kind == kind {
			out = append(out, e)
		}
	}
	return out
}

// Since returns the events at or after t.
func (j *Journal) Since(t time.Time) []Event {
	var out []Event
	for _, e := range j.events {
		if !e.At.Before(t) {
			out = append(out, e)
		}
	}
	return out
}

// Counts returns how many events there are of each kind.
func (j *Journal) Counts() map[string]int {
	out := map[string]int{}
	for _, e := range j.events {
		out[e.Kind]++
	}
	return out
}
`,
			"manager.go": `package leases

import (
	"errors"
	"time"
)

// ErrBusy is for a lease that is already held.
var ErrBusy = errors.New("lease is busy")

// Lease is a claim on a name that runs out at Expires.
type Lease struct {
	Name, Owner string
	Token       int64
	Expires     time.Time
}

// Manager hands out leases.
type Manager struct {
	clock  Clock
	leases map[string]*Lease
}

// New returns a manager with no leases.
func New(clock Clock) *Manager {
	return &Manager{clock: clock, leases: map[string]*Lease{}}
}

// Acquire gives name to owner for ttl, or returns ErrBusy while someone holds it.
func (m *Manager) Acquire(name, owner string, ttl time.Duration) (Lease, error) {
	now := m.clock()
	if l, ok := m.leases[name]; ok && !now.After(l.Expires) {
		return Lease{}, ErrBusy
	}
	l := &Lease{Name: name, Owner: owner, Expires: now.Add(ttl)}
	m.leases[name] = l
	return *l, nil
}

// Get returns the lease on name while it is held.
func (m *Manager) Get(name string) (Lease, bool) {
	l, ok := m.leases[name]
	if !ok || m.clock().After(l.Expires) {
		return Lease{}, false
	}
	return *l, true
}
`,
			"manager_test.go": `package leases

import (
	"testing"
	"time"
)

func TestBusyWhileHeld(t *testing.T) {
	c := NewFakeClock(time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC))
	m := New(c.Now)
	if _, err := m.Acquire("db", "alice", 30*time.Second); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Acquire("db", "bob", 30*time.Second); err != ErrBusy {
		t.Fatalf("second Acquire: %v", err)
	}
	c.Advance(31 * time.Second)
	if _, ok := m.Get("db"); ok {
		t.Fatal("the lease outlived its time")
	}
	if _, err := m.Acquire("db", "bob", 30*time.Second); err != nil {
		t.Fatalf("after expiry: %v", err)
	}
}

func TestJournalFilters(t *testing.T) {
	start := time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)
	var j Journal
	j.Add(Event{At: start, Kind: "a", Name: "x"})
	j.Add(Event{At: start.Add(time.Minute), Kind: "b", Name: "y"})
	j.Add(Event{At: start.Add(time.Hour), Kind: "a", Name: "z"})
	if got := j.OfKind("a"); len(got) != 2 || got[1].Name != "z" {
		t.Fatalf("OfKind = %+v", got)
	}
	if got := j.Since(start.Add(time.Minute)); len(got) != 2 || got[0].Name != "y" {
		t.Fatalf("Since = %+v", got)
	}
	if got := j.Counts(); got["a"] != 2 || got["b"] != 1 {
		t.Fatalf("Counts = %v", got)
	}
}
`,
		},
		Hidden: map[string]string{hiddenName: `package leases_test

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	leases "fixture"
)

var start = time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)

const ttl = 30 * time.Second

func newManager() (*leases.FakeClock, *leases.Manager) {
	c := leases.NewFakeClock(start)
	return c, leases.New(c.Now)
}

func acquire(t *testing.T, m *leases.Manager, name, owner string) leases.Lease {
	t.Helper()
	l, err := m.Acquire(name, owner, ttl)
	if err != nil {
		t.Fatal(err)
	}
	return l
}

// fillOwner has owner take as many of n leases as the manager lets it.
func fillOwner(m *leases.Manager, owner string, n int) {
	for i := range n {
		m.Acquire(fmt.Sprintf("%s-%d", owner, i), owner, ttl)
	}
}

func kinds(m *leases.Manager) []string {
	var out []string
	for _, e := range m.Journal().Events() {
		out = append(out, e.Kind)
	}
	return out
}

func TestAcquireFollowsPolicy(t *testing.T) {
	cases := []struct {
		ttl, want time.Duration
	}{{0, 30 * time.Second}, {time.Second, 5 * time.Second}, {time.Hour, 5 * time.Minute}, {time.Minute, time.Minute}}
	for _, tc := range cases {
		c, m := newManager()
		l, err := m.Acquire("db", " ALICE ", tc.ttl)
		if err != nil {
			t.Fatal(err)
		}
		if !l.Expires.Equal(c.Now().Add(tc.want)) {
			t.Errorf("ttl %v runs to %v", tc.ttl, l.Expires)
		}
		if l.Owner != "alice" {
			t.Errorf("owner %q", l.Owner)
		}
	}
}

func TestTokensCountAcrossLeases(t *testing.T) {
	_, m := newManager()
	a := acquire(t, m, "a", "x")
	b := acquire(t, m, "b", "x")
	if a.Token != 100 || b.Token != 101 {
		t.Fatalf("tokens %d, %d", a.Token, b.Token)
	}
}

func TestLeaseOverAtExactExpiry(t *testing.T) {
	c, m := newManager()
	acquire(t, m, "db", "alice")
	c.Advance(ttl - time.Nanosecond)
	if _, ok := m.Get("db"); !ok {
		t.Fatal("the lease ended early")
	}
	c.Advance(time.Nanosecond)
	if _, ok := m.Get("db"); ok {
		t.Fatal("Get still shows the lease at its expiry")
	}
	if _, err := m.Acquire("db", "bob", ttl); err != nil {
		t.Fatalf("Acquire at the expiry: %v", err)
	}
}

func TestReleaseChecksItsToken(t *testing.T) {
	_, m := newManager()
	l := acquire(t, m, "db", "alice")
	if err := m.Release("nothing", 1); !errors.Is(err, leases.ErrNotHeld) {
		t.Errorf("unknown name: %v", err)
	}
	if err := m.Release("db", l.Token+1); !errors.Is(err, leases.ErrStaleToken) {
		t.Errorf("wrong token: %v", err)
	}
	if err := m.Release("db", l.Token); err != nil {
		t.Fatal(err)
	}
	if _, ok := m.Get("db"); ok {
		t.Error("the lease is still there")
	}
}

func TestReleaseAtExpiry(t *testing.T) {
	c, m := newManager()
	l := acquire(t, m, "db", "alice")
	c.Advance(ttl)
	if err := m.Release("db", l.Token); !errors.Is(err, leases.ErrNotHeld) {
		t.Fatalf("err = %v", err)
	}
}

func TestRenewExtendsFromNow(t *testing.T) {
	c, m := newManager()
	l := acquire(t, m, "db", "alice")
	c.Advance(10 * time.Second)
	got, err := m.Renew("db", l.Token, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Expires.Equal(c.Now().Add(time.Minute)) {
		t.Fatalf("Expires = %v", got.Expires)
	}
	if now, _ := m.Get("db"); !now.Expires.Equal(got.Expires) {
		t.Fatal("the manager did not keep the new expiry")
	}
}

func TestRenewKeepsToken(t *testing.T) {
	_, m := newManager()
	l := acquire(t, m, "db", "alice")
	got, err := m.Renew("db", l.Token, time.Minute)
	if err != nil || got.Token != l.Token {
		t.Fatalf("Renew = %+v, %v", got, err)
	}
	if next := acquire(t, m, "other", "alice"); next.Token != l.Token+1 {
		t.Fatalf("the next token is %d", next.Token)
	}
}

func TestRenewFollowsTTLPolicy(t *testing.T) {
	c, m := newManager()
	l := acquire(t, m, "db", "alice")
	got, err := m.Renew("db", l.Token, time.Hour)
	if err != nil || !got.Expires.Equal(c.Now().Add(5*time.Minute)) {
		t.Errorf("an hour: %+v, %v", got, err)
	}
	got, err = m.Renew("db", got.Token, 0)
	if err != nil || !got.Expires.Equal(c.Now().Add(30*time.Second)) {
		t.Errorf("zero: %+v, %v", got, err)
	}
}

func TestRenewAtExpiry(t *testing.T) {
	c, m := newManager()
	l := acquire(t, m, "db", "alice")
	c.Advance(ttl)
	if _, err := m.Renew("db", l.Token, ttl); !errors.Is(err, leases.ErrNotHeld) {
		t.Fatalf("err = %v", err)
	}
}

func TestRenewWrongToken(t *testing.T) {
	_, m := newManager()
	l := acquire(t, m, "db", "alice")
	if _, err := m.Renew("db", l.Token+5, ttl); !errors.Is(err, leases.ErrStaleToken) {
		t.Fatalf("err = %v", err)
	}
}

func TestJournalShowsBasics(t *testing.T) {
	c, m := newManager()
	l := acquire(t, m, "db", "alice")
	c.Advance(time.Second)
	l, err := m.Renew("db", l.Token, ttl)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Release("db", l.Token); err != nil {
		t.Fatal(err)
	}
	if got := kinds(m); !reflect.DeepEqual(got, []string{"acquire", "renew", "release"}) {
		t.Fatalf("kinds = %v", got)
	}
	e := m.Journal().Events()[1]
	if e.Name != "db" || e.Owner != "alice" || !e.At.Equal(c.Now()) {
		t.Fatalf("event %+v", e)
	}
}

func TestEnqueueGivesPlaces(t *testing.T) {
	_, m := newManager()
	if n, err := m.Enqueue("free", "alice", ttl); err != nil || n != 0 {
		t.Fatalf("free name: %d, %v", n, err)
	}
	if l, ok := m.Get("free"); !ok || l.Owner != "alice" {
		t.Fatalf("free name: %+v, %v", l, ok)
	}
	if n, err := m.Enqueue("free", "bob", ttl); err != nil || n != 1 {
		t.Fatalf("bob: %d, %v", n, err)
	}
	if n, err := m.Enqueue("free", "carol", ttl); err != nil || n != 2 {
		t.Fatalf("carol: %d, %v", n, err)
	}
	if n, err := m.Enqueue("free", "bob", ttl); err != nil || n != 1 {
		t.Fatalf("bob again: %d, %v", n, err)
	}
}

func TestAcquireCannotJumpTheQueue(t *testing.T) {
	c, m := newManager()
	acquire(t, m, "db", "alice")
	if _, err := m.Enqueue("db", "bob", ttl); err != nil {
		t.Fatal(err)
	}
	c.Advance(ttl)
	if _, err := m.Acquire("db", "carol", ttl); !errors.Is(err, leases.ErrBusy) {
		t.Fatalf("err = %v", err)
	}
	if l, ok := m.Get("db"); !ok || l.Owner != "bob" {
		t.Fatalf("Get = %+v, %v", l, ok)
	}
}

func TestReleasePromotesFirstWaiter(t *testing.T) {
	c, m := newManager()
	l := acquire(t, m, "db", "alice")
	if _, err := m.Enqueue("db", "bob", time.Minute); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Enqueue("db", "carol", ttl); err != nil {
		t.Fatal(err)
	}
	c.Advance(10 * time.Second)
	if err := m.Release("db", l.Token); err != nil {
		t.Fatal(err)
	}
	got, ok := m.Get("db")
	if !ok || got.Owner != "bob" || got.Token != l.Token+1 || !got.Expires.Equal(c.Now().Add(time.Minute)) {
		t.Fatalf("Get = %+v, %v", got, ok)
	}
	if q := m.Queue("db"); !reflect.DeepEqual(q, []string{"carol"}) {
		t.Fatalf("Queue = %v", q)
	}
}

func TestPromotedWaiterFollowsTTLPolicy(t *testing.T) {
	c, m := newManager()
	l := acquire(t, m, "db", "alice")
	if _, err := m.Enqueue("db", "bob", time.Hour); err != nil {
		t.Fatal(err)
	}
	if err := m.Release("db", l.Token); err != nil {
		t.Fatal(err)
	}
	if got, _ := m.Get("db"); !got.Expires.Equal(c.Now().Add(5 * time.Minute)) {
		t.Fatalf("Expires = %v", got.Expires)
	}
}

func TestEnqueueNormalizesOwner(t *testing.T) {
	_, m := newManager()
	l := acquire(t, m, "db", "alice")
	if _, err := m.Enqueue("db", " BOB", ttl); err != nil {
		t.Fatal(err)
	}
	if n, err := m.Enqueue("db", "bob ", ttl); err != nil || n != 1 {
		t.Fatalf("second spelling: %d, %v", n, err)
	}
	if err := m.Release("db", l.Token); err != nil {
		t.Fatal(err)
	}
	if got, _ := m.Get("db"); got.Owner != "bob" {
		t.Fatalf("Owner = %q", got.Owner)
	}
}

func TestPromotionIsJournaled(t *testing.T) {
	_, m := newManager()
	l := acquire(t, m, "db", "alice")
	if _, err := m.Enqueue("db", "bob", ttl); err != nil {
		t.Fatal(err)
	}
	if err := m.Release("db", l.Token); err != nil {
		t.Fatal(err)
	}
	got := m.Journal().OfKind("promote")
	if len(got) != 1 || got[0].Name != "db" || got[0].Owner != "bob" {
		t.Fatalf("events %+v", got)
	}
}

func TestTickEndsLeasesAndPromotes(t *testing.T) {
	c, m := newManager()
	acquire(t, m, "a", "alice")
	acquire(t, m, "b", "alice")
	if _, err := m.Enqueue("b", "bob", ttl); err != nil {
		t.Fatal(err)
	}
	c.Advance(ttl - time.Second)
	if n := m.Tick(); n != 0 {
		t.Fatalf("Tick before the time is up: %d", n)
	}
	c.Advance(time.Second)
	if n := m.Tick(); n != 2 {
		t.Fatalf("Tick = %d", n)
	}
	if l, ok := m.Get("b"); !ok || l.Owner != "bob" {
		t.Fatalf("Get = %+v, %v", l, ok)
	}
	if _, ok := m.Get("a"); ok {
		t.Fatal("a is still held")
	}
}

func TestExpiryIsJournaled(t *testing.T) {
	c, m := newManager()
	acquire(t, m, "db", "alice")
	c.Advance(ttl)
	m.Tick()
	got := m.Journal().OfKind("expire")
	if len(got) != 1 || got[0].Name != "db" || got[0].Owner != "alice" {
		t.Fatalf("events %+v", got)
	}
}

func TestCancelTakesOwnerOut(t *testing.T) {
	_, m := newManager()
	acquire(t, m, "db", "alice")
	for _, owner := range []string{"bob", "carol", "dan"} {
		if _, err := m.Enqueue("db", owner, ttl); err != nil {
			t.Fatal(err)
		}
	}
	if err := m.Cancel("db", "carol"); err != nil {
		t.Fatal(err)
	}
	if got := m.Queue("db"); !reflect.DeepEqual(got, []string{"bob", "dan"}) {
		t.Fatalf("Queue = %v", got)
	}
	if err := m.Cancel("db", "carol"); !errors.Is(err, leases.ErrNotWaiting) {
		t.Fatalf("second Cancel: %v", err)
	}
	if m.Position("db", "dan") != 2 || m.Position("db", "carol") != 0 {
		t.Fatalf("Position: %d, %d", m.Position("db", "dan"), m.Position("db", "carol"))
	}
}

func TestCancelIgnoresOwnerSpelling(t *testing.T) {
	_, m := newManager()
	acquire(t, m, "db", "alice")
	if _, err := m.Enqueue("db", "bob", ttl); err != nil {
		t.Fatal(err)
	}
	if err := m.Cancel("db", " Bob "); err != nil {
		t.Fatal(err)
	}
}

func TestPositionIgnoresOwnerSpelling(t *testing.T) {
	_, m := newManager()
	acquire(t, m, "db", "alice")
	if _, err := m.Enqueue("db", "bob", ttl); err != nil {
		t.Fatal(err)
	}
	if n := m.Position("db", "BOB"); n != 1 {
		t.Fatalf("Position = %d", n)
	}
}

func TestOwnerLimitStopsAcquire(t *testing.T) {
	_, m := newManager()
	fillOwner(m, "alice", 9)
	before := len(m.OwnerLeases("alice"))
	if _, err := m.Acquire("one-more", "alice", ttl); !errors.Is(err, leases.ErrOwnerLimit) {
		t.Fatalf("err = %v", err)
	}
	if got := len(m.OwnerLeases("alice")); got != before || got < 3 {
		t.Fatalf("holds %d, then %d", before, got)
	}
}

func TestOwnerMayHoldFive(t *testing.T) {
	_, m := newManager()
	for i := range 5 {
		if _, err := m.Acquire(fmt.Sprint("k", i), "alice", ttl); err != nil {
			t.Fatalf("lease %d: %v", i+1, err)
		}
	}
	if _, err := m.Acquire("k5", "alice", ttl); !errors.Is(err, leases.ErrOwnerLimit) {
		t.Fatalf("the sixth: %v", err)
	}
}

func TestPromotionPassesOverOwnerAtLimit(t *testing.T) {
	_, m := newManager()
	fillOwner(m, "bob", 9)
	l := acquire(t, m, "db", "alice")
	for _, owner := range []string{"bob", "carol"} {
		if _, err := m.Enqueue("db", owner, ttl); err != nil {
			t.Fatal(err)
		}
	}
	if err := m.Release("db", l.Token); err != nil {
		t.Fatal(err)
	}
	if got, _ := m.Get("db"); got.Owner != "carol" {
		t.Fatalf("Owner = %q", got.Owner)
	}
	if got := m.Queue("db"); !reflect.DeepEqual(got, []string{"bob"}) {
		t.Fatalf("Queue = %v", got)
	}
}

func TestTransferMovesLease(t *testing.T) {
	c, m := newManager()
	l := acquire(t, m, "db", "alice")
	c.Advance(10 * time.Second)
	got, err := m.Transfer("db", l.Token, "bob")
	if err != nil || got.Owner != "bob" || !got.Expires.Equal(l.Expires) {
		t.Fatalf("Transfer = %+v, %v", got, err)
	}
	if now, ok := m.Get("db"); !ok || now.Owner != "bob" {
		t.Fatalf("Get = %+v, %v", now, ok)
	}
}

func TestTransferGetsFreshToken(t *testing.T) {
	_, m := newManager()
	l := acquire(t, m, "db", "alice")
	got, err := m.Transfer("db", l.Token, "bob")
	if err != nil || got.Token != l.Token+1 {
		t.Fatalf("Transfer = %+v, %v", got, err)
	}
	if next := acquire(t, m, "other", "alice"); next.Token != l.Token+2 {
		t.Fatalf("the next token is %d", next.Token)
	}
	if err := m.Release("db", l.Token); !errors.Is(err, leases.ErrStaleToken) {
		t.Fatalf("the old holder's token: %v", err)
	}
}

func TestTransferNormalizesOwner(t *testing.T) {
	_, m := newManager()
	l := acquire(t, m, "db", "alice")
	got, err := m.Transfer("db", l.Token, " BOB ")
	if err != nil || got.Owner != "bob" {
		t.Fatalf("Transfer = %+v, %v", got, err)
	}
}

func TestTransferWrongToken(t *testing.T) {
	_, m := newManager()
	l := acquire(t, m, "db", "alice")
	if _, err := m.Transfer("db", l.Token+5, "bob"); !errors.Is(err, leases.ErrStaleToken) {
		t.Fatalf("err = %v", err)
	}
}

func TestTransferAtExpiry(t *testing.T) {
	c, m := newManager()
	l := acquire(t, m, "db", "alice")
	c.Advance(ttl)
	if _, err := m.Transfer("db", l.Token, "bob"); !errors.Is(err, leases.ErrNotHeld) {
		t.Fatalf("err = %v", err)
	}
}

func TestTransferIsJournaled(t *testing.T) {
	_, m := newManager()
	l := acquire(t, m, "db", "alice")
	if _, err := m.Transfer("db", l.Token, "bob"); err != nil {
		t.Fatal(err)
	}
	got := m.Journal().OfKind("transfer")
	if len(got) != 1 || got[0].Name != "db" || got[0].Owner != "bob" {
		t.Fatalf("events %+v", got)
	}
}

func TestTransferRespectsOwnerLimit(t *testing.T) {
	_, m := newManager()
	fillOwner(m, "bob", 9)
	l := acquire(t, m, "db", "alice")
	if _, err := m.Transfer("db", l.Token, "bob"); !errors.Is(err, leases.ErrOwnerLimit) {
		t.Fatalf("err = %v", err)
	}
	if got, _ := m.Get("db"); got.Owner != "alice" {
		t.Fatalf("Owner = %q", got.Owner)
	}
}

func TestOwnerLeasesOnlyTheirs(t *testing.T) {
	_, m := newManager()
	acquire(t, m, "a", "alice")
	acquire(t, m, "b", "bob")
	acquire(t, m, "c", "alice")
	got := m.OwnerLeases("alice")
	if len(got) != 2 {
		t.Fatalf("OwnerLeases = %+v", got)
	}
	if m.OwnerLeases("nobody") != nil && len(m.OwnerLeases("nobody")) != 0 {
		t.Fatal("an unknown owner has leases")
	}
}

func TestOwnerLeasesIgnoresOwnerSpelling(t *testing.T) {
	_, m := newManager()
	acquire(t, m, "a", "alice")
	if got := m.OwnerLeases(" ALICE"); len(got) != 1 {
		t.Fatalf("OwnerLeases = %+v", got)
	}
}

func TestOwnerLeasesAtExpiry(t *testing.T) {
	c, m := newManager()
	acquire(t, m, "a", "alice")
	c.Advance(ttl)
	if got := m.OwnerLeases("alice"); len(got) != 0 {
		t.Fatalf("OwnerLeases = %+v", got)
	}
}

func TestOwnerLeasesSoonestFirst(t *testing.T) {
	c, m := newManager()
	acquire(t, m, "z", "alice")
	c.Advance(time.Second)
	acquire(t, m, "c", "alice")
	acquire(t, m, "a", "alice")
	var got []string
	for _, l := range m.OwnerLeases("alice") {
		got = append(got, l.Name)
	}
	if !reflect.DeepEqual(got, []string{"z", "a", "c"}) {
		t.Fatalf("order %v", got)
	}
}

func TestStatsCounts(t *testing.T) {
	c, m := newManager()
	acquire(t, m, "a", "alice")
	if _, err := m.Acquire("b", "alice", time.Minute); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Enqueue("b", "bob", ttl); err != nil {
		t.Fatal(err)
	}
	c.Advance(ttl + time.Second)
	want := leases.Stats{Active: 1, Overdue: 1, Waiting: 1}
	if got := m.Stats(); got != want {
		t.Fatalf("Stats = %+v", got)
	}
}

func TestStatsAtExpiry(t *testing.T) {
	c, m := newManager()
	acquire(t, m, "a", "alice")
	c.Advance(ttl)
	want := leases.Stats{Overdue: 1}
	if got := m.Stats(); got != want {
		t.Fatalf("Stats = %+v", got)
	}
}

func TestCheckTokenTakesTheNewest(t *testing.T) {
	c, m := newManager()
	if err := m.CheckToken("db", 100); !errors.Is(err, leases.ErrNotHeld) {
		t.Fatalf("a name never granted: %v", err)
	}
	l := acquire(t, m, "db", "alice")
	if err := m.CheckToken("db", l.Token); err != nil {
		t.Fatal(err)
	}
	c.Advance(ttl + time.Minute)
	if err := m.CheckToken("db", l.Token); err != nil {
		t.Fatalf("after expiry, with nobody newer: %v", err)
	}
}

func TestCheckTokenOlderToken(t *testing.T) {
	c, m := newManager()
	old := acquire(t, m, "db", "alice")
	c.Advance(ttl)
	acquire(t, m, "db", "bob")
	if err := m.CheckToken("db", old.Token); !errors.Is(err, leases.ErrStaleToken) {
		t.Fatalf("err = %v", err)
	}
}

func TestDrainReleasesEverything(t *testing.T) {
	_, m := newManager()
	acquire(t, m, "a", "alice")
	acquire(t, m, "b", "alice")
	acquire(t, m, "c", "bob")
	if _, err := m.Enqueue("a", "carol", ttl); err != nil {
		t.Fatal(err)
	}
	if n := m.Drain("alice"); n != 2 {
		t.Fatalf("Drain = %d", n)
	}
	if got, _ := m.Get("a"); got.Owner != "carol" {
		t.Fatalf("a went to %q", got.Owner)
	}
	if _, ok := m.Get("b"); ok {
		t.Fatal("b is still held")
	}
	if got, _ := m.Get("c"); got.Owner != "bob" {
		t.Fatalf("c went to %q", got.Owner)
	}
}

func TestRoomReportsWhatIsLeft(t *testing.T) {
	_, m := newManager()
	acquire(t, m, "a", "alice")
	acquire(t, m, "b", "alice")
	if n := m.Room("alice"); n != 3 {
		t.Fatalf("Room = %d", n)
	}
	if n := m.Room("nobody"); n != 5 {
		t.Fatalf("Room of a newcomer = %d", n)
	}
}

func TestRoomIgnoresOwnerSpelling(t *testing.T) {
	_, m := newManager()
	acquire(t, m, "a", "alice")
	if got, want := m.Room(" ALICE "), m.Room("alice"); got != want {
		t.Fatalf("Room = %d, want %d", got, want)
	}
}

func TestRenderFormat(t *testing.T) {
	c, m := newManager()
	c.Advance(time.Hour)
	acquire(t, m, "db", "alice")
	got := strings.Split(strings.TrimSpace(m.Journal().Render()), "\n")
	if len(got) != 1 || !strings.HasPrefix(got[0], "2030-01-02T04:04:05Z acquire db ") {
		t.Fatalf("Render = %q", got)
	}
}

func TestRenderMasksOwners(t *testing.T) {
	_, m := newManager()
	acquire(t, m, "db", "alice")
	got := strings.TrimSpace(m.Journal().Render())
	if !strings.HasSuffix(got, " al***") || strings.Contains(got, "alice") {
		t.Fatalf("Render = %q", got)
	}
}

func TestSnapshotFormat(t *testing.T) {
	c, m := newManager()
	acquire(t, m, "db", "alice")
	acquire(t, m, "api", "bob")
	if _, err := m.Enqueue("db", "carol", time.Minute); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Enqueue("db", "dan", time.Minute+1500*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	expires := c.Now().Add(ttl).Unix()
	want := fmt.Sprintf("next 102\nlease api bob 101 %d\nlease db alice 100 %d\nhigh api 101\nhigh db 100\nwait db carol 60\nwait db dan 61\n", expires, expires)
	if got := m.Snapshot(); got != want {
		t.Fatalf("Snapshot = %q", got)
	}
}

func TestSnapshotAtExpiry(t *testing.T) {
	c, m := newManager()
	acquire(t, m, "db", "alice")
	c.Advance(ttl)
	got := m.Snapshot()
	if strings.Contains(got, "lease ") || !strings.Contains(got, "high db 100\n") {
		t.Fatalf("Snapshot = %q", got)
	}
}

func TestRestoreRoundTrip(t *testing.T) {
	c, m := newManager()
	acquire(t, m, "db", "alice")
	if _, err := m.Enqueue("db", "carol", time.Minute); err != nil {
		t.Fatal(err)
	}
	r, err := leases.Restore(m.Snapshot(), c.Now)
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := r.Get("db"); !ok || got.Owner != "alice" || got.Token != 100 {
		t.Fatalf("Get = %+v, %v", got, ok)
	}
	if got := r.Queue("db"); !reflect.DeepEqual(got, []string{"carol"}) {
		t.Fatalf("Queue = %v", got)
	}
	if err := r.CheckToken("db", 100); err != nil {
		t.Fatal(err)
	}
	if next := acquire(t, r, "other", "x"); next.Token != 101 {
		t.Fatalf("the next token is %d", next.Token)
	}
}

func TestRestoreRefusesBadLines(t *testing.T) {
	c, _ := newManager()
	for _, text := range []string{"next many\n", "mystery 1\n", "lease db alice 100\n", "next 5\nhigh db\n"} {
		if _, err := leases.Restore(text, c.Now); !errors.Is(err, leases.ErrBadSnapshot) {
			t.Errorf("%q: %v", text, err)
		}
	}
}

func TestAcquireAllGrantsOrNothing(t *testing.T) {
	_, m := newManager()
	acquire(t, m, "b", "bob")
	if _, err := m.AcquireAll([]string{"a", "b"}, "alice", ttl); !errors.Is(err, leases.ErrBusy) {
		t.Fatalf("err = %v", err)
	}
	if _, ok := m.Get("a"); ok {
		t.Fatal("a was granted by a failed call")
	}
	got, err := m.AcquireAll([]string{"y", "x"}, "alice", ttl)
	if err != nil || len(got) != 2 || got[0].Name != "y" || got[1].Name != "x" {
		t.Fatalf("AcquireAll = %+v, %v", got, err)
	}
}

func TestAcquireAllFollowsTTLPolicy(t *testing.T) {
	c, m := newManager()
	got, err := m.AcquireAll([]string{"a"}, "alice", time.Hour)
	if err != nil || !got[0].Expires.Equal(c.Now().Add(5*time.Minute)) {
		t.Fatalf("AcquireAll = %+v, %v", got, err)
	}
}

func TestAcquireAllNormalizesOwner(t *testing.T) {
	_, m := newManager()
	got, err := m.AcquireAll([]string{"a"}, " ALICE", ttl)
	if err != nil || got[0].Owner != "alice" {
		t.Fatalf("AcquireAll = %+v, %v", got, err)
	}
}

func TestAcquireAllRespectsOwnerLimit(t *testing.T) {
	_, m := newManager()
	fillOwner(m, "alice", 4)
	if _, err := m.AcquireAll([]string{"x", "y", "z"}, "alice", ttl); !errors.Is(err, leases.ErrOwnerLimit) {
		t.Fatalf("err = %v", err)
	}
	if _, ok := m.Get("x"); ok {
		t.Fatal("x was granted by a failed call")
	}
}

func TestStatusFormat(t *testing.T) {
	c, m := newManager()
	acquire(t, m, "db", "alice")
	c.Advance(10*time.Second + 500*time.Millisecond)
	f := strings.Fields(strings.TrimSpace(m.Status()))
	if len(f) != 4 || f[0] != "db" || f[2] != "100" || f[3] != "19" {
		t.Fatalf("Status = %q", f)
	}
}

func TestStatusMasksOwners(t *testing.T) {
	_, m := newManager()
	acquire(t, m, "db", "alice")
	if f := strings.Fields(m.Status()); f[1] != "al***" {
		t.Fatalf("Status = %q", f)
	}
}

func TestStatusSoonestFirst(t *testing.T) {
	c, m := newManager()
	acquire(t, m, "z", "x")
	c.Advance(time.Second)
	acquire(t, m, "a", "x")
	lines := strings.Split(strings.TrimSpace(m.Status()), "\n")
	if len(lines) != 2 || !strings.HasPrefix(lines[0], "z ") {
		t.Fatalf("Status = %q", lines)
	}
}

func TestExpiringWithinPicksLeases(t *testing.T) {
	c, m := newManager()
	acquire(t, m, "soon", "x")
	if _, err := m.Acquire("later", "x", time.Minute); err != nil {
		t.Fatal(err)
	}
	c.Advance(10 * time.Second)
	got := m.ExpiringWithin(20 * time.Second)
	if len(got) != 1 || got[0].Name != "soon" {
		t.Fatalf("ExpiringWithin = %+v", got)
	}
	c.Advance(21 * time.Second)
	if got := m.ExpiringWithin(40 * time.Second); len(got) != 1 || got[0].Name != "later" {
		t.Fatalf("with the first one over: %+v", got)
	}
}

func TestExpiringWithinSoonestFirst(t *testing.T) {
	c, m := newManager()
	acquire(t, m, "z", "x")
	c.Advance(time.Second)
	acquire(t, m, "a", "x")
	got := m.ExpiringWithin(time.Hour)
	if len(got) != 2 || got[0].Name != "z" {
		t.Fatalf("ExpiringWithin = %+v", got)
	}
}

func TestHistoryOfOneLease(t *testing.T) {
	_, m := newManager()
	l := acquire(t, m, "a", "x")
	acquire(t, m, "b", "x")
	if err := m.Release("a", l.Token); err != nil {
		t.Fatal(err)
	}
	got := m.Journal().History("a")
	if len(got) != 2 || got[0].Kind != "acquire" || got[1].Kind != "release" {
		t.Fatalf("History = %+v", got)
	}
}

func TestBusiestOrder(t *testing.T) {
	var j leases.Journal
	for _, name := range []string{"b", "c", "a", "c", "a", "c", "d"} {
		j.Add(leases.Event{At: start, Kind: "k", Name: name})
	}
	got := j.Busiest(3)
	want := []leases.NameCount{{Name: "c", Count: 3}, {Name: "a", Count: 2}, {Name: "b", Count: 1}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Busiest = %+v", got)
	}
	if got := j.Busiest(10); len(got) != 4 {
		t.Fatalf("Busiest(10) = %+v", got)
	}
}

func TestOwnersRows(t *testing.T) {
	c, m := newManager()
	acquire(t, m, "a", "zed")
	acquire(t, m, "b", "alice")
	if _, err := m.Acquire("gone", "old", time.Second*10); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Enqueue("a", "alice", ttl); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Enqueue("b", "mid", ttl); err != nil {
		t.Fatal(err)
	}
	c.Advance(11 * time.Second)
	want := []leases.OwnerRow{{Owner: "alice", Held: 1, Waiting: 1}, {Owner: "mid", Waiting: 1}, {Owner: "zed", Held: 1}}
	if got := m.Owners(); !reflect.DeepEqual(got, want) {
		t.Fatalf("Owners = %+v", got)
	}
}

func TestOwnerReportFormat(t *testing.T) {
	_, m := newManager()
	acquire(t, m, "a", "alice")
	if _, err := m.Enqueue("a", "bob", ttl); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(m.OwnerReport()), "\n")
	if len(lines) != 2 || !strings.HasSuffix(lines[0], " 1 0") || !strings.HasSuffix(lines[1], " 0 1") {
		t.Fatalf("OwnerReport = %q", lines)
	}
}

func TestOwnerReportMasksOwners(t *testing.T) {
	_, m := newManager()
	acquire(t, m, "a", "alice")
	if got := m.OwnerReport(); got != "al*** 1 0\n" {
		t.Fatalf("OwnerReport = %q", got)
	}
}

func TestForgetRetiresAName(t *testing.T) {
	c, m := newManager()
	acquire(t, m, "db", "alice")
	if err := m.Forget("db"); !errors.Is(err, leases.ErrBusy) {
		t.Fatalf("while held: %v", err)
	}
	c.Advance(ttl)
	if err := m.Forget("db"); err != nil {
		t.Fatalf("once it is over: %v", err)
	}
	if err := m.CheckToken("db", 100); !errors.Is(err, leases.ErrNotHeld) {
		t.Fatalf("CheckToken: %v", err)
	}
	if strings.Contains(m.Snapshot(), "high db") {
		t.Fatal("the snapshot still has it")
	}
}

func TestPreemptTakesOver(t *testing.T) {
	c, m := newManager()
	acquire(t, m, "db", "alice")
	if _, err := m.Enqueue("db", "bob", ttl); err != nil {
		t.Fatal(err)
	}
	got, err := m.Preempt("db", "ops", time.Minute)
	if err != nil || got.Owner != "ops" || !got.Expires.Equal(c.Now().Add(time.Minute)) {
		t.Fatalf("Preempt = %+v, %v", got, err)
	}
	if q := m.Queue("db"); !reflect.DeepEqual(q, []string{"bob"}) {
		t.Fatalf("Queue = %v", q)
	}
}

func TestPreemptGetsFreshToken(t *testing.T) {
	_, m := newManager()
	l := acquire(t, m, "db", "alice")
	got, err := m.Preempt("db", "ops", ttl)
	if err != nil || got.Token != l.Token+1 {
		t.Fatalf("Preempt = %+v, %v", got, err)
	}
	if err := m.Release("db", l.Token); !errors.Is(err, leases.ErrStaleToken) {
		t.Fatalf("the old holder's Release: %v", err)
	}
}

func TestPreemptFollowsTTLPolicy(t *testing.T) {
	c, m := newManager()
	got, err := m.Preempt("db", "ops", time.Hour)
	if err != nil || !got.Expires.Equal(c.Now().Add(5*time.Minute)) {
		t.Fatalf("Preempt = %+v, %v", got, err)
	}
}

func TestPreemptNormalizesOwner(t *testing.T) {
	_, m := newManager()
	got, err := m.Preempt("db", " OPS", ttl)
	if err != nil || got.Owner != "ops" {
		t.Fatalf("Preempt = %+v, %v", got, err)
	}
}

func TestPreemptIsJournaled(t *testing.T) {
	_, m := newManager()
	acquire(t, m, "db", "alice")
	if _, err := m.Preempt("db", "ops", ttl); err != nil {
		t.Fatal(err)
	}
	got := m.Journal().OfKind("preempt")
	if len(got) != 1 || got[0].Name != "db" || got[0].Owner != "ops" {
		t.Fatalf("events %+v", got)
	}
}

func TestPreemptRespectsOwnerLimit(t *testing.T) {
	_, m := newManager()
	fillOwner(m, "ops", 9)
	if _, err := m.Preempt("db", "ops", ttl); !errors.Is(err, leases.ErrOwnerLimit) {
		t.Fatalf("err = %v", err)
	}
}
`},
		Solution: map[string]string{
			"journal.go": `package leases

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// Event is one thing that happened to a lease.
type Event struct {
	At          time.Time
	Kind        string
	Name, Owner string
}

// Journal is the audit trail of a manager, oldest event first.
type Journal struct {
	events []Event
}

// Add appends an event.
func (j *Journal) Add(e Event) { j.events = append(j.events, e) }

// Events returns a copy of all the events.
func (j *Journal) Events() []Event { return append([]Event(nil), j.events...) }

// OfKind returns the events of one kind.
func (j *Journal) OfKind(kind string) []Event {
	var out []Event
	for _, e := range j.events {
		if e.Kind == kind {
			out = append(out, e)
		}
	}
	return out
}

// Since returns the events at or after t.
func (j *Journal) Since(t time.Time) []Event {
	var out []Event
	for _, e := range j.events {
		if !e.At.Before(t) {
			out = append(out, e)
		}
	}
	return out
}

// Counts returns how many events there are of each kind.
func (j *Journal) Counts() map[string]int {
	out := map[string]int{}
	for _, e := range j.events {
		out[e.Kind]++
	}
	return out
}

// History returns the events of one lease.
func (j *Journal) History(name string) []Event {
	var out []Event
	for _, e := range j.events {
		if e.Name == name {
			out = append(out, e)
		}
	}
	return out
}

// NameCount says how many events a lease has.
type NameCount struct {
	Name  string
	Count int
}

// Busiest returns the n leases with the most events, most first, in name order where
// the counts are equal.
func (j *Journal) Busiest(n int) []NameCount {
	counts := map[string]int{}
	for _, e := range j.events {
		counts[e.Name]++
	}
	out := make([]NameCount, 0, len(counts))
	for name, count := range counts {
		out = append(out, NameCount{name, count})
	}
	sort.Slice(out, func(a, b int) bool {
		if out[a].Count != out[b].Count {
			return out[a].Count > out[b].Count
		}
		return out[a].Name < out[b].Name
	})
	return out[:min(n, len(out))]
}

// Render returns one line per event: the time in RFC 3339 in UTC, the kind, the lease
// name and the owner.
func (j *Journal) Render() string {
	var b strings.Builder
	for _, e := range j.events {
		fmt.Fprintf(&b, "%s %s %s %s\n", e.At.UTC().Format(time.RFC3339), e.Kind, e.Name, maskOwner(e.Owner))
	}
	return b.String()
}
`,
			"manager.go": `package leases

import (
	"errors"
	"time"
)

var (
	// ErrBusy is for a lease that is already held or has a queue.
	ErrBusy = errors.New("lease is busy")
	// ErrNotHeld is for a name with no lease in force.
	ErrNotHeld = errors.New("lease is not held")
	// ErrStaleToken is for a token that is not the lease's current one.
	ErrStaleToken = errors.New("stale token")
	// ErrOwnerLimit is for an owner already holding as many leases as allowed.
	ErrOwnerLimit = errors.New("owner holds too many leases")
)

// firstToken is the first token a manager hands out.
const firstToken = 100

// Lease is a claim on a name that runs out at Expires.
type Lease struct {
	Name, Owner string
	Token       int64
	Expires     time.Time
}

// Manager hands out leases.
type Manager struct {
	clock   Clock
	leases  map[string]*Lease
	waiters map[string][]waiter
	highest map[string]int64 // the newest token ever granted on each name
	next    int64            // the next token to hand out
	journal *Journal
}

// New returns a manager with no leases.
func New(clock Clock) *Manager {
	return &Manager{
		clock:   clock,
		leases:  map[string]*Lease{},
		waiters: map[string][]waiter{},
		highest: map[string]int64{},
		next:    firstToken,
		journal: &Journal{},
	}
}

// Journal returns the manager's audit trail.
func (m *Manager) Journal() *Journal { return m.journal }

func (m *Manager) record(kind, name, owner string) {
	m.journal.Add(Event{At: m.clock(), Kind: kind, Name: name, Owner: owner})
}

// held returns how many leases in force owner has.
func (m *Manager) held(owner string) int {
	now := m.clock()
	n := 0
	for _, l := range m.leases {
		if l.Owner == owner && now.Before(l.Expires) {
			n++
		}
	}
	return n
}

// grant gives name to owner for ttl under the next token.
func (m *Manager) grant(name, owner string, ttl time.Duration) Lease {
	l := &Lease{Name: name, Owner: owner, Token: m.next, Expires: m.clock().Add(ttl)}
	m.next++
	m.highest[name] = l.Token
	m.leases[name] = l
	return *l
}

// Acquire gives name to owner for ttl. It returns ErrBusy while someone holds the
// lease or anyone is waiting for it.
func (m *Manager) Acquire(name, owner string, ttl time.Duration) (Lease, error) {
	owner = normalOwner(owner)
	m.settle(name)
	if _, held := m.leases[name]; held || len(m.waiters[name]) > 0 {
		return Lease{}, ErrBusy
	}
	if m.held(owner) >= maxPerOwner {
		return Lease{}, ErrOwnerLimit
	}
	l := m.grant(name, owner, clampTTL(ttl))
	m.record("acquire", name, owner)
	return l, nil
}

// Get returns the lease on name while it is held.
func (m *Manager) Get(name string) (Lease, bool) {
	l, ok := m.leases[name]
	if !ok || !m.clock().Before(l.Expires) {
		return Lease{}, false
	}
	return *l, true
}

// Release ends a lease and gives it to the first waiter.
func (m *Manager) Release(name string, token int64) error {
	l, ok := m.leases[name]
	if !ok || !m.clock().Before(l.Expires) {
		return ErrNotHeld
	}
	if l.Token != token {
		return ErrStaleToken
	}
	delete(m.leases, name)
	m.record("release", name, l.Owner)
	m.promote(name)
	return nil
}

// Renew makes a lease run for ttl from now. It keeps its token.
func (m *Manager) Renew(name string, token int64, ttl time.Duration) (Lease, error) {
	l, ok := m.leases[name]
	if !ok || !m.clock().Before(l.Expires) {
		return Lease{}, ErrNotHeld
	}
	if l.Token != token {
		return Lease{}, ErrStaleToken
	}
	l.Expires = m.clock().Add(clampTTL(ttl))
	m.record("renew", name, l.Owner)
	return *l, nil
}
`,
			"ownership.go": `package leases

import "time"

// Transfer hands a lease in force to another owner, with its expiry as it is.
func (m *Manager) Transfer(name string, token int64, owner string) (Lease, error) {
	owner = normalOwner(owner)
	l, ok := m.leases[name]
	if !ok || !m.clock().Before(l.Expires) {
		return Lease{}, ErrNotHeld
	}
	if l.Token != token {
		return Lease{}, ErrStaleToken
	}
	if m.held(owner) >= maxPerOwner {
		return Lease{}, ErrOwnerLimit
	}
	l.Owner = owner
	l.Token = m.next
	m.next++
	m.highest[name] = l.Token
	m.record("transfer", name, owner)
	return *l, nil
}

// Preempt is an operator's override: it takes the lease from whoever holds it and
// gives it to owner right away. The queue stays as it is.
func (m *Manager) Preempt(name, owner string, ttl time.Duration) (Lease, error) {
	owner = normalOwner(owner)
	if m.held(owner) >= maxPerOwner {
		return Lease{}, ErrOwnerLimit
	}
	l := m.grant(name, owner, clampTTL(ttl))
	m.record("preempt", name, owner)
	return l, nil
}

// CheckToken lets a storage service fence off writes from old holders. It accepts only
// the newest token ever granted on name, whether or not that lease is still in force.
func (m *Manager) CheckToken(name string, token int64) error {
	high, ok := m.highest[name]
	if !ok {
		return ErrNotHeld
	}
	if token != high {
		return ErrStaleToken
	}
	return nil
}

// Forget drops everything the manager remembers about a name that has been retired. It
// returns ErrBusy while the name is held or anyone is waiting for it.
func (m *Manager) Forget(name string) error {
	m.settle(name)
	if _, held := m.leases[name]; held || len(m.waiters[name]) > 0 {
		return ErrBusy
	}
	delete(m.highest, name)
	return nil
}

// Drain releases everything owner holds, each lease going to its first waiter, and
// returns how many it released.
func (m *Manager) Drain(owner string) int {
	held := m.OwnerLeases(owner)
	for _, l := range held {
		m.Release(l.Name, l.Token)
	}
	return len(held)
}

// AcquireAll gives every name to owner or none of them, and returns the leases in the
// order of names.
func (m *Manager) AcquireAll(names []string, owner string, ttl time.Duration) ([]Lease, error) {
	owner = normalOwner(owner)
	if m.held(owner)+len(names) > maxPerOwner {
		return nil, ErrOwnerLimit
	}
	for _, name := range names {
		m.settle(name)
		if _, held := m.leases[name]; held || len(m.waiters[name]) > 0 {
			return nil, ErrBusy
		}
	}
	out := make([]Lease, 0, len(names))
	for _, name := range names {
		out = append(out, m.grant(name, owner, clampTTL(ttl)))
		m.record("acquire", name, owner)
	}
	return out, nil
}
`,
			"policy.go": `package leases

import (
	"strings"
	"time"
)

const (
	minTTL     = 5 * time.Second
	maxTTL     = 5 * time.Minute
	defaultTTL = 30 * time.Second
)

// maxPerOwner is how many leases one owner can hold at any one time.
const maxPerOwner = 5

// clampTTL applies the limits in docs/policy.md.
func clampTTL(ttl time.Duration) time.Duration {
	if ttl == 0 {
		return defaultTTL
	}
	return min(max(ttl, minTTL), maxTTL)
}

// normalOwner returns the form of an owner name that we store and compare.
func normalOwner(owner string) string {
	return strings.ToLower(strings.TrimSpace(owner))
}

// maskOwner returns what may be shown of an owner name in anything handed out as text.
func maskOwner(owner string) string {
	if len(owner) > 2 {
		owner = owner[:2]
	}
	return owner + "***"
}
`,
			"report.go": `package leases

import (
	"fmt"
	"maps"
	"slices"
	"sort"
	"strings"
	"time"
)

func sortedKeys[V any](m map[string]V) []string {
	return slices.Sorted(maps.Keys(m))
}

// live returns the leases in force, in the order reports show them.
func (m *Manager) live() []Lease {
	now := m.clock()
	var out []Lease
	for _, l := range m.leases {
		if now.Before(l.Expires) {
			out = append(out, *l)
		}
	}
	sort.Slice(out, func(a, b int) bool {
		if !out[a].Expires.Equal(out[b].Expires) {
			return out[a].Expires.Before(out[b].Expires)
		}
		return out[a].Name < out[b].Name
	})
	return out
}

// OwnerLeases returns the leases in force that owner holds.
func (m *Manager) OwnerLeases(owner string) []Lease {
	owner = normalOwner(owner)
	var out []Lease
	for _, l := range m.live() {
		if l.Owner == owner {
			out = append(out, l)
		}
	}
	return out
}

// Room returns how many more leases owner could take right now.
func (m *Manager) Room(owner string) int {
	return maxPerOwner - m.held(normalOwner(owner))
}

// Stats describes a manager at one moment.
type Stats struct {
	Active  int // leases in force
	Overdue int // leases whose time is up but which Tick has not removed yet
	Waiting int // waiters in all the queues
}

// Stats reports on the manager without changing anything.
func (m *Manager) Stats() Stats {
	st := Stats{Active: len(m.live())}
	st.Overdue = len(m.leases) - st.Active
	for _, q := range m.waiters {
		st.Waiting += len(q)
	}
	return st
}

// ExpiringWithin returns the leases in force that run out within d of now.
func (m *Manager) ExpiringWithin(d time.Duration) []Lease {
	limit := m.clock().Add(d)
	var out []Lease
	for _, l := range m.live() {
		if !l.Expires.After(limit) {
			out = append(out, l)
		}
	}
	return out
}

// Status returns one line per lease in force: its name, its owner, its token and the
// whole seconds it has left.
func (m *Manager) Status() string {
	now := m.clock()
	var b strings.Builder
	for _, l := range m.live() {
		fmt.Fprintf(&b, "%s %s %d %d\n", l.Name, maskOwner(l.Owner), l.Token, int64(l.Expires.Sub(now)/time.Second))
	}
	return b.String()
}

// OwnerRow is one owner's line in a report.
type OwnerRow struct {
	Owner         string
	Held, Waiting int
}

// Owners returns every owner with a lease in force or a place in a queue, by owner name.
func (m *Manager) Owners() []OwnerRow {
	rows := map[string]*OwnerRow{}
	row := func(owner string) *OwnerRow {
		if rows[owner] == nil {
			rows[owner] = &OwnerRow{Owner: owner}
		}
		return rows[owner]
	}
	for _, l := range m.live() {
		row(l.Owner).Held++
	}
	for _, q := range m.waiters {
		for _, w := range q {
			row(w.owner).Waiting++
		}
	}
	out := make([]OwnerRow, 0, len(rows))
	for _, owner := range sortedKeys(rows) {
		out = append(out, *rows[owner])
	}
	return out
}

// OwnerReport returns one line per row of Owners: the owner, then the two counts.
func (m *Manager) OwnerReport() string {
	var b strings.Builder
	for _, r := range m.Owners() {
		fmt.Fprintf(&b, "%s %d %d\n", maskOwner(r.Owner), r.Held, r.Waiting)
	}
	return b.String()
}
`,
			"snapshot.go": `package leases

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// ErrBadSnapshot is for a snapshot line that cannot be read.
var ErrBadSnapshot = errors.New("bad snapshot")

// Snapshot writes the manager's state in the format of docs/wire.md.
func (m *Manager) Snapshot() string {
	now := m.clock()
	var b strings.Builder
	fmt.Fprintf(&b, "next %d\n", m.next)
	for _, name := range sortedKeys(m.leases) {
		if l := m.leases[name]; now.Before(l.Expires) {
			fmt.Fprintf(&b, "lease %s %s %d %d\n", l.Name, l.Owner, l.Token, l.Expires.Unix())
		}
	}
	for _, name := range sortedKeys(m.highest) {
		fmt.Fprintf(&b, "high %s %d\n", name, m.highest[name])
	}
	for _, name := range sortedKeys(m.waiters) {
		for _, w := range m.waiters[name] {
			fmt.Fprintf(&b, "wait %s %s %d\n", name, w.owner, int64(w.ttl/time.Second))
		}
	}
	return b.String()
}

// Restore builds a manager from a snapshot.
func Restore(text string, clock Clock) (*Manager, error) {
	m := New(clock)
	for _, line := range strings.Split(strings.TrimSpace(text), "\n") {
		if err := m.restoreLine(strings.Fields(line)); err != nil {
			return nil, fmt.Errorf("%q: %w", line, err)
		}
	}
	return m, nil
}

func (m *Manager) restoreLine(f []string) error {
	if len(f) == 0 {
		return ErrBadSnapshot
	}
	n, err := strconv.ParseInt(f[len(f)-1], 10, 64)
	if err != nil {
		return ErrBadSnapshot
	}
	switch {
	case f[0] == "next" && len(f) == 2:
		m.next = n
	case f[0] == "high" && len(f) == 3:
		m.highest[f[1]] = n
	case f[0] == "wait" && len(f) == 4:
		m.waiters[f[1]] = append(m.waiters[f[1]], waiter{owner: f[2], ttl: time.Duration(n) * time.Second})
	case f[0] == "lease" && len(f) == 5:
		token, err := strconv.ParseInt(f[3], 10, 64)
		if err != nil {
			return ErrBadSnapshot
		}
		m.leases[f[1]] = &Lease{Name: f[1], Owner: f[2], Token: token, Expires: time.Unix(n, 0)}
	default:
		return ErrBadSnapshot
	}
	return nil
}
`,
			"waiters.go": `package leases

import (
	"errors"
	"slices"
	"time"
)

// ErrNotWaiting is for an owner that is not in a queue.
var ErrNotWaiting = errors.New("owner is not waiting")

// waiter is one place in the queue for a lease.
type waiter struct {
	owner string
	ttl   time.Duration
}

// Enqueue puts owner in the queue for name and returns its place, counting from 1. A
// free name with nobody waiting is an ordinary Acquire, and the place is 0.
func (m *Manager) Enqueue(name, owner string, ttl time.Duration) (int, error) {
	owner = normalOwner(owner)
	m.settle(name)
	q := m.waiters[name]
	if _, held := m.leases[name]; !held && len(q) == 0 {
		_, err := m.Acquire(name, owner, ttl)
		return 0, err
	}
	for i, w := range q {
		if w.owner == owner {
			return i + 1, nil
		}
	}
	m.waiters[name] = append(q, waiter{owner: owner, ttl: clampTTL(ttl)})
	return len(q) + 1, nil
}

// Cancel takes owner out of the queue for name.
func (m *Manager) Cancel(name, owner string) error {
	owner = normalOwner(owner)
	q := m.waiters[name]
	for i, w := range q {
		if w.owner == owner {
			m.waiters[name] = slices.Delete(q, i, i+1)
			return nil
		}
	}
	return ErrNotWaiting
}

// Position returns owner's place in the queue for name, or 0 if it is not there.
func (m *Manager) Position(name, owner string) int {
	owner = normalOwner(owner)
	for i, w := range m.waiters[name] {
		if w.owner == owner {
			return i + 1
		}
	}
	return 0
}

// Queue returns the owners waiting for name, first in line first.
func (m *Manager) Queue(name string) []string {
	var out []string
	for _, w := range m.waiters[name] {
		out = append(out, w.owner)
	}
	return out
}

// promote gives a free name to the first waiter who can take it. A waiter at the limit
// is passed over and keeps its place.
func (m *Manager) promote(name string) {
	q := m.waiters[name]
	for i, w := range q {
		if m.held(w.owner) < maxPerOwner {
			m.waiters[name] = slices.Delete(q, i, i+1)
			m.grant(name, w.owner, w.ttl)
			m.record("promote", name, w.owner)
			return
		}
	}
}

// settle ends the lease on name if its time is up and fills a free name from the
// queue. It reports whether a lease ended.
func (m *Manager) settle(name string) bool {
	ended := false
	if l, ok := m.leases[name]; ok && !m.clock().Before(l.Expires) {
		delete(m.leases, name)
		m.record("expire", name, l.Owner)
		ended = true
	}
	if _, held := m.leases[name]; !held {
		m.promote(name)
	}
	return ended
}

// Tick ends every lease whose time is up, fills free names from their queues and
// returns how many leases ended.
func (m *Manager) Tick() int {
	names := sortedKeys(m.waiters)
	for _, name := range sortedKeys(m.leases) {
		if !slices.Contains(names, name) {
			names = append(names, name)
		}
	}
	ended := 0
	for _, name := range names {
		if m.settle(name) {
			ended++
		}
	}
	return ended
}
`,
		},
		Turns: script(
			local("Read docs/policy.md and make Acquire follow what it says about TTLs and owner names. Put the helpers in a new policy.go."),
			cloud("Leases get fencing tokens now. There is one counter for the whole manager, not one per name, and the first token is 100. Every new holder of a lease gets the next token, and a lease keeps its token for as long as it has the same holder, whatever else happens to it. Set Lease.Token in Acquire."),
			cloud("Fix a boundary while it's cheap: a lease is over at exactly its Expires instant, not a moment later. Acquire and Get both get this wrong today. It's the rule for anything that looks at a lease from now on."),
			cloud("Add `func (m *Manager) Release(name string, token int64) error`, returning `ErrNotHeld`, a new `errors.New(\"lease is not held\")`, when no lease is in force and `ErrStaleToken`, a new `errors.New(\"stale token\")`, when the token isn't the lease's. And something for later, no code for it today: whenever we report a set of leases, the soonest expiry comes first, with ties by name."),
			local("Add `func (m *Manager) Renew(name string, token int64, ttl time.Duration) (Lease, error)`: the lease then runs for ttl from now."),
			local("Wire in journal.go. The manager owns a Journal, `func (m *Manager) Journal() *Journal` returns it, and every change of a lease's holder or expiry adds exactly one event with the lease's name and its owner after the change. Kind is the lower-case name of the method that did it, so acquire, renew and release so far."),
			cloud("Add a queue in a new waiters.go. `func (m *Manager) Enqueue(name, owner string, ttl time.Duration) (int, error)` puts owner at the back and returns its place, counting from 1. An owner already queued keeps its place. A free name with nobody waiting is an ordinary Acquire, and the place is 0. Nobody may jump the queue: while anyone waits, Acquire returns ErrBusy."),
			cloud("When a lease is released, the first waiter gets it straight away, with the TTL it asked for when it queued, running from now. That is an event of kind promote."),
			cloud("Add `func (m *Manager) Tick() int`, which ends every lease whose time is up, an event of kind expire, gives each freed name to its first waiter and returns how many leases ended. Acquire and Enqueue do the same for their own name before they look at it, so a waiter never loses its turn because Tick hasn't run."),
			cloud("manager.go is doing too much. Move the settling and promotion logic to waiters.go, and make sure it reads as one small set of helpers shared by Release, Tick, Acquire and Enqueue. No behaviour changes."),
			local("Add `func (m *Manager) Cancel(name, owner string) error`, returning `ErrNotWaiting`, a new `errors.New(\"owner is not waiting\")`, if owner isn't queued for name, and `func (m *Manager) Position(name, owner string) int`, its place, or 0 if it isn't queued. Both in waiters.go."),
			local("From legal, for anything we hand out as text, nothing to build yet: logs and status pages must not contain full owner names, only the first two characters followed by ***. Today, add `func (m *Manager) Queue(name string) []string` to waiters.go, the owners queued for name, first in line first."),
			cloud("No owner may hold more than 3 leases at any one time. Acquire returns `ErrOwnerLimit`, a new `errors.New(\"owner holds too many leases\")`. A waiter at that limit is passed over when a lease frees up, and keeps its place in the queue."),
			local("Add a new ownership.go with `func (m *Manager) Transfer(name string, token int64, owner string) (Lease, error)`: it hands a lease in force to another owner and leaves its expiry as it is."),
			local("Add a new report.go with `func (m *Manager) OwnerLeases(owner string) []Lease`, the leases in force that owner holds."),
			local("Add to report.go `type Stats struct{ Active, Overdue, Waiting int }` and `func (m *Manager) Stats() Stats`: leases in force, leases whose time is up but which Tick hasn't removed yet, and waiters in all the queues. It changes nothing."),
			cloud("Add `func (m *Manager) CheckToken(name string, token int64) error` to ownership.go so storage services can fence off old holders. It accepts only the newest token ever granted on that name, whether or not that lease is still in force, so the manager has to remember it after a lease ends. Any other token gets the same error Release gives for a wrong token. A name never granted gets ErrNotHeld."),
			local("Add `func (m *Manager) Drain(owner string) int` to report.go, for shutting a client down: it releases everything owner holds, just as Release would, and returns how many it released."),
			cloud("The teams with big batch jobs complained, so the cap on how many leases one owner can hold goes up to 5. Please change it."),
			local("Add `func (m *Manager) Room(owner string) int` to report.go: how many more leases owner could take right now."),
			cloud("Add `func (j *Journal) Render() string` to journal.go: one line per event, with the time in RFC 3339 in UTC, the kind, the lease name and the owner, separated by single spaces."),
			cloud("Read docs/wire.md and add `func (m *Manager) Snapshot() string` in a new snapshot.go, in that format."),
			cloud("Add `func Restore(text string, clock Clock) (*Manager, error)` to snapshot.go, the inverse of Snapshot. A line it can't read fails the whole restore with `ErrBadSnapshot`, a new `errors.New(\"bad snapshot\")`, which may be wrapped."),
			local("Add `func (m *Manager) AcquireAll(names []string, owner string, ttl time.Duration) ([]Lease, error)` to report.go: it gives owner every one of the names or none of them, and returns the leases in the order of names. Each lease is an acquire in every other way."),
			cloud("Add `func (m *Manager) Status() string` to report.go: one line per lease in force, with its name, its owner, its token and the whole seconds it has left, rounded down and separated by single spaces."),
			local("Add `func (m *Manager) ExpiringWithin(d time.Duration) []Lease` to report.go: the leases in force that run out within d from now."),
			cloud("Add `func (m *Manager) Preempt(name, owner string, ttl time.Duration) (Lease, error)` to ownership.go, an operator's override: it takes the lease from whoever holds it, if anyone, and gives it to owner for ttl right away, whoever is waiting. The queue stays as it is and the old holder doesn't join it."),
			local("In journal.go add `func (j *Journal) History(name string) []Event`, the events of one lease, `type NameCount struct { Name string; Count int }` and `func (j *Journal) Busiest(n int) []NameCount`: the n leases with the most events, most first, in name order where the counts are equal."),
			local("Add to report.go `type OwnerRow struct { Owner string; Held, Waiting int }` and `func (m *Manager) Owners() []OwnerRow`: one row for every owner with a lease in force or a place in a queue, in order of owner name, with how many leases it holds and how many queues it is in."),
			cloud("Add `func (m *Manager) OwnerReport() string` to report.go for the capacity page: one line per row of Owners, with the owner, then Held, then Waiting, separated by single spaces."),
			cloud("Add `func (m *Manager) Forget(name string) error` to ownership.go for retiring a name: it drops all the manager remembers about it, so CheckToken treats it as never granted. It returns ErrBusy while the name is held or anyone is waiting for it."),
			local("report.go has AcquireAll and Drain in it, which aren't reports. Move them to ownership.go. No behaviour changes."),
		),
		Carries: []Carry{
			{
				What:        "every TTL is clamped to the policy limits, zero meaning the default (docs/policy.md)",
				Kind:        CarryDiscovered,
				Established: 0,
				Needed:      []int{4, 6, 23, 26},
				Source:      "docs/policy.md",
				Tests:       []string{"TestRenewFollowsTTLPolicy", "TestPromotedWaiterFollowsTTLPolicy", "TestAcquireAllFollowsTTLPolicy", "TestPreemptFollowsTTLPolicy"},
				Absent:      []string{"policy.md", "5 seconds", "30 seconds", "5 minutes", "300 seconds", "clamp", "raised to", "lowered to"},
			},
			{
				What:        "owner names are compared and stored trimmed and in lower case (docs/policy.md)",
				Kind:        CarryDiscovered,
				Established: 0,
				Needed:      []int{6, 10, 13, 14, 19, 23, 26},
				Source:      "docs/policy.md",
				Tests:       []string{"TestEnqueueNormalizesOwner", "TestCancelIgnoresOwnerSpelling", "TestPositionIgnoresOwnerSpelling", "TestTransferNormalizesOwner", "TestOwnerLeasesIgnoresOwnerSpelling", "TestRoomIgnoresOwnerSpelling", "TestAcquireAllNormalizesOwner", "TestPreemptNormalizesOwner"},
				Absent:      []string{"policy.md", "same client", "leading", "trailing", "ignore case", "trimmed", "normaliz", "normalis"},
			},
			{
				What:        "one token counter for the manager: a new holder gets the next token, a renewal keeps it",
				Kind:        CarryUser,
				Established: 1,
				Needed:      []int{4, 13, 26},
				Tests:       []string{"TestRenewKeepsToken", "TestTransferGetsFreshToken", "TestPreemptGetsFreshToken"},
				Absent:      []string{"one counter", "first token", "next token", "new holder", "keeps its token", "new token", "fresh token"},
			},
			{
				What:        "a lease is over at exactly its Expires instant",
				Kind:        CarryUser,
				Established: 2,
				Needed:      []int{3, 4, 13, 14, 15, 21},
				Tests:       []string{"TestReleaseAtExpiry", "TestRenewAtExpiry", "TestTransferAtExpiry", "TestOwnerLeasesAtExpiry", "TestStatsAtExpiry", "TestSnapshotAtExpiry"},
				Absent:      []string{"exactly its", "expires instant", "not a moment", "boundary"},
			},
			{
				What:        "reports list the soonest expiry first, ties by name",
				Kind:        CarryUser,
				Established: 3,
				Needed:      []int{14, 24, 25},
				Tests:       []string{"TestOwnerLeasesSoonestFirst", "TestStatusSoonestFirst", "TestExpiringWithinSoonestFirst"},
				Absent:      []string{"soonest", "ties by", "expiry first"},
			},
			{
				What:        "a wrong token is ErrStaleToken",
				Kind:        CarryEmbodied,
				Established: 3,
				Needed:      []int{4, 13, 16},
				Tests:       []string{"TestRenewWrongToken", "TestTransferWrongToken", "TestCheckTokenOlderToken"},
				Absent:      []string{"errstaletoken", "stale"},
			},
			{
				What:        "every change of holder or expiry adds one journal event, named after the method",
				Kind:        CarryUser,
				Established: 5,
				Needed:      []int{13, 26},
				Tests:       []string{"TestTransferIsJournaled", "TestPreemptIsJournaled"},
				Absent:      []string{"every change", "exactly one", "method that did it"},
			},
			{
				What:        "text handed out shows only the first two characters of an owner name and then ***",
				Kind:        CarryUser,
				Established: 11,
				Needed:      []int{20, 24, 29},
				Tests:       []string{"TestRenderMasksOwners", "TestStatusMasksOwners", "TestOwnerReportMasksOwners"},
				Absent:      []string{"legal", "mask", "***", "first two", "full owner"},
			},
			{
				What:        "no owner holds more than the limit at any one time, however it gets a lease",
				Kind:        CarryUser,
				Established: 12,
				Needed:      []int{13, 23, 26},
				Tests:       []string{"TestTransferRespectsOwnerLimit", "TestAcquireAllRespectsOwnerLimit", "TestPreemptRespectsOwnerLimit"},
				Absent:      []string{"more than 3", "errownerlimit", "owner limit", "too many leases", "passed over"},
			},
			{
				What:        "revised: the limit per owner is 5",
				Kind:        CarryUser,
				Established: 18,
				Needed:      []int{19},
				Tests:       []string{"TestOwnerMayHoldFive", "TestRoomReportsWhatIsLeft"},
				Absent:      []string{"up to 5", "cap on", "five"},
			},
		},
	}
}
