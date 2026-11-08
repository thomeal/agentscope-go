package s2

func longA03() Task {
	return Task{
		ID:     "s2-long-a03-helpdesk-sla",
		Set:    "long",
		System: system("a service-level tracker for a customer support desk"),
		Files: map[string]string{
			"go.mod": goMod,
			"ticket.go": `package desk

import (
	"fmt"
	"sort"
)

// Event is one change of a ticket's status.
type Event struct {
	At     int
	Status string
}

// Ticket is one customer problem. Priority runs from 1, the most urgent, to 4.
// Opened and Event.At are minutes, as described in clock.go. Status is "open",
// "waiting" while we wait for the customer, or "closed". Events holds the changes
// of status since the ticket was opened, oldest first.
type Ticket struct {
	ID       string
	Customer string
	Priority int
	Opened   int
	Agent    string
	Status   string
	Events   []Event
	Notes    []Note
}

// Desk holds the tickets and the agents who work on them.
type Desk struct {
	tickets []*Ticket
	next    int
	agents  map[string]bool
}

// New returns an empty desk.
func New() *Desk {
	return &Desk{next: 1001, agents: map[string]bool{}}
}

// Open records a new ticket and returns its ID.
func (d *Desk) Open(customer string, priority, at int) string {
	t := &Ticket{ID: fmt.Sprintf("T-%d", d.next), Customer: customer, Priority: priority, Opened: at, Status: "open"}
	d.next++
	d.tickets = append(d.tickets, t)
	return t.ID
}

// find returns the ticket with an ID, or nil.
func (d *Desk) find(id string) *Ticket {
	for _, t := range d.tickets {
		if t.ID == id {
			return t
		}
	}
	return nil
}

// Get returns a copy of a ticket.
func (d *Desk) Get(id string) (Ticket, bool) {
	t := d.find(id)
	if t == nil {
		return Ticket{}, false
	}
	return *t, true
}

// IDs returns every ticket's ID, sorted.
func (d *Desk) IDs() []string {
	out := make([]string, 0, len(d.tickets))
	for _, t := range d.tickets {
		out = append(out, t.ID)
	}
	sort.Strings(out)
	return out
}
`,
			"clock.go": `package desk

import "fmt"

const dayMinutes = 24 * 60

// Times on the desk are whole minutes. Minute 0 is 00:00 on the Monday of the
// desk's first week. Days are numbered from that Monday as day 0, so day 5 is the
// first Saturday and day 7 the second Monday.

// At returns the minute for a time on a day.
func At(day, hour, minute int) int { return day*dayMinutes + hour*60 + minute }

// DayOf returns the day a minute falls on.
func DayOf(at int) int { return at / dayMinutes }

// Weekday returns the short name of the weekday a minute falls on.
func Weekday(at int) string {
	return [...]string{"Mon", "Tue", "Wed", "Thu", "Fri", "Sat", "Sun"}[DayOf(at)%7]
}

// Stamp writes a minute for people to read, like "Tue 14:05".
func Stamp(at int) string {
	return fmt.Sprintf("%s %02d:%02d", Weekday(at), at%dayMinutes/60, at%60)
}
`,
			"agents.go": `package desk

import (
	"errors"
	"sort"
)

// ErrNoAgent is returned for a name that is not on the desk's list of agents.
var ErrNoAgent = errors.New("no such agent")

// AddAgent puts an agent on the desk.
func (d *Desk) AddAgent(name string) { d.agents[name] = true }

// Agents returns the names of the agents, sorted.
func (d *Desk) Agents() []string {
	out := make([]string, 0, len(d.agents))
	for name := range d.agents {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}
`,
			"notes.go": `package desk

// Note is one remark on a ticket.
type Note struct {
	Author string
	Text   string
}

// Line writes a note the way the desk shows it.
func (n Note) Line() string { return n.Author + ": " + n.Text }
`,
			"report.go": `package desk

import (
	"fmt"
	"strings"
)

// Summary returns how many tickets are in each status, for the wall screen.
func (d *Desk) Summary() string {
	counts := map[string]int{}
	for _, t := range d.tickets {
		counts[t.Status]++
	}
	var b strings.Builder
	for _, status := range []string{"open", "waiting", "closed"} {
		fmt.Fprintf(&b, "%s: %d\n", status, counts[status])
	}
	return b.String()
}

// ByPriority returns how many tickets there are of each priority, closed ones too.
func (d *Desk) ByPriority() map[int]int {
	out := map[int]int{}
	for _, t := range d.tickets {
		out[t.Priority]++
	}
	return out
}
`,
			"docs/sla-policy.txt": `Service levels of the support desk

Every ticket has a target for being resolved, in minutes of SLA time, set by its
priority:

  P1    240
  P2    480
  P3   1440
  P4   4800

SLA time runs only in support hours, which are Monday to Friday, 09:00 to 17:00.
The exception is P1, whose time runs around the clock, nights and weekends too.

A ticket is in breach only once its SLA time has gone past its target. Exactly on
target is still met.

Customers on a premium contract are promised a named agent, which the account
managers arrange themselves. Telephone calls are logged by the call centre system,
and only the tickets it opens here are subject to these targets.
`,
			"docs/breach-report.txt": `Weekly breach report

This goes to the service manager every Monday morning. It is plain text with the
fields separated by a tab, and every line, the last one too, ends in a line feed.

The first line is exactly these four words, separated by tabs:

  ticket  priority  customer  minutes_over

Then one line for each ticket in breach, closed tickets included, with the most
minutes over first and ties by ticket ID.

  priority       written P1 to P4
  minutes_over   how much SLA time the ticket has used beyond its target

The manager pastes this into a spreadsheet, so there is no title line, no total and
no trailing blank line.
`,
			"desk_test.go": `package desk

import (
	"slices"
	"testing"
)

func TestOpenNumbersTickets(t *testing.T) {
	d := New()
	first := d.Open("acme", 3, 0)
	second := d.Open("beta", 2, 0)
	if first != "T-1001" || second != "T-1002" {
		t.Fatalf("IDs %s and %s", first, second)
	}
	if got := d.IDs(); !slices.Equal(got, []string{"T-1001", "T-1002"}) {
		t.Fatalf("IDs = %v", got)
	}
}

func TestClockHelpers(t *testing.T) {
	at := At(8, 14, 5)
	if got := Stamp(at); got != "Tue 14:05" {
		t.Fatalf("Stamp = %q", got)
	}
	if got := DayOf(at); got != 8 {
		t.Fatalf("DayOf = %d", got)
	}
}

func TestSummaryCounts(t *testing.T) {
	d := New()
	d.Open("acme", 3, 0)
	if got := d.Summary(); got != "open: 1\nwaiting: 0\nclosed: 0\n" {
		t.Fatalf("Summary = %q", got)
	}
}

func TestAgentsAreSorted(t *testing.T) {
	d := New()
	d.AddAgent("zoe")
	d.AddAgent("ann")
	if got := d.Agents(); !slices.Equal(got, []string{"ann", "zoe"}) {
		t.Fatalf("Agents = %v", got)
	}
}
`,
		},
		Hidden: map[string]string{hiddenName: `package desk_test

import (
	"errors"
	"slices"
	"strings"
	"testing"

	desk "fixture"
)

// status changes a ticket's status.
func status(t *testing.T, d *desk.Desk, id, to string, at int) {
	t.Helper()
	if err := d.SetStatus(id, to, at); err != nil {
		t.Fatal(err)
	}
}

func elapsed(t *testing.T, d *desk.Desk, id string, now int) int {
	t.Helper()
	got, err := d.Elapsed(id, now)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func deadline(t *testing.T, d *desk.Desk, id string, now int) int {
	t.Helper()
	got, err := d.Deadline(id, now)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

// staffed returns a desk with the agents ann and bob.
func staffed() *desk.Desk {
	d := desk.New()
	d.AddAgent("ann")
	d.AddAgent("bob")
	return d
}

// load gives an agent n P3 tickets.
func load(t *testing.T, d *desk.Desk, agent string, n int) {
	t.Helper()
	for range n {
		if err := d.Assign(d.Open("acme", 3, desk.At(0, 9, 0)), agent); err != nil {
			t.Fatal(err)
		}
	}
}

func TestOpenCountSkipsClosed(t *testing.T) {
	d := desk.New()
	d.Open("acme", 3, 0)
	status(t, d, d.Open("acme", 3, 0), "closed", 5)
	status(t, d, d.Open("acme", 3, 0), "waiting", 5)
	if got := d.OpenCount(); got != 2 {
		t.Errorf("OpenCount = %d", got)
	}
}

func TestSetStatusRecords(t *testing.T) {
	d := desk.New()
	id := d.Open("acme", 3, 0)
	status(t, d, id, "waiting", 5)
	status(t, d, id, "open", 9)
	if got, _ := d.Get(id); got.Status != "open" {
		t.Errorf("Status = %q", got.Status)
	}
	want := []desk.Event{{At: 5, Status: "waiting"}, {At: 9, Status: "open"}}
	if got, err := d.History(id); err != nil || !slices.Equal(got, want) {
		t.Errorf("History = %+v, %v", got, err)
	}
}

func TestSetStatusBadStatus(t *testing.T) {
	d := desk.New()
	if err := d.SetStatus(d.Open("acme", 3, 0), "paused", 5); !errors.Is(err, desk.ErrBadStatus) {
		t.Errorf("err = %v", err)
	}
}

func TestSetStatusUnknown(t *testing.T) {
	if err := desk.New().SetStatus("T-1", "open", 5); !errors.Is(err, desk.ErrUnknownTicket) {
		t.Errorf("err = %v", err)
	}
}

func TestHistoryUnknown(t *testing.T) {
	if _, err := desk.New().History("T-1"); !errors.Is(err, desk.ErrUnknownTicket) {
		t.Errorf("err = %v", err)
	}
}

func TestAssignedUnknown(t *testing.T) {
	if _, err := desk.New().Assigned("T-1"); !errors.Is(err, desk.ErrUnknownTicket) {
		t.Errorf("err = %v", err)
	}
}

func TestAssignUnknownTicket(t *testing.T) {
	if err := staffed().Assign("T-1", "ann"); !errors.Is(err, desk.ErrUnknownTicket) {
		t.Errorf("err = %v", err)
	}
}

func TestElapsedUnknown(t *testing.T) {
	if _, err := desk.New().Elapsed("T-1", 5); !errors.Is(err, desk.ErrUnknownTicket) {
		t.Errorf("err = %v", err)
	}
}

func TestRemainingUnknown(t *testing.T) {
	if _, err := desk.New().Remaining("T-1", 5); !errors.Is(err, desk.ErrUnknownTicket) {
		t.Errorf("err = %v", err)
	}
}

func TestBreachedUnknown(t *testing.T) {
	if _, err := desk.New().Breached("T-1", 5); !errors.Is(err, desk.ErrUnknownTicket) {
		t.Errorf("err = %v", err)
	}
}

func TestAddNoteUnknown(t *testing.T) {
	if err := desk.New().AddNote("T-1", "ann", "hello"); !errors.Is(err, desk.ErrUnknownTicket) {
		t.Errorf("err = %v", err)
	}
}

func TestTranscriptUnknown(t *testing.T) {
	if _, err := desk.New().Transcript("T-1"); !errors.Is(err, desk.ErrUnknownTicket) {
		t.Errorf("err = %v", err)
	}
}

func TestAssignAndWho(t *testing.T) {
	d := staffed()
	id := d.Open("acme", 3, 0)
	if got, err := d.Assigned(id); err != nil || got != "" {
		t.Errorf("before Assign: %q, %v", got, err)
	}
	if err := d.Assign(id, "ann"); err != nil {
		t.Fatal(err)
	}
	if got, err := d.Assigned(id); err != nil || got != "ann" {
		t.Errorf("after Assign: %q, %v", got, err)
	}
	if err := d.Assign(id, "zed"); !errors.Is(err, desk.ErrNoAgent) {
		t.Errorf("unlisted agent: %v", err)
	}
}

func TestAssignLimit(t *testing.T) {
	d := staffed()
	load(t, d, "ann", 8)
	if err := d.Assign(d.Open("acme", 3, 0), "ann"); !errors.Is(err, desk.ErrAgentFull) {
		t.Errorf("err = %v", err)
	}
}

func TestLoadCountsWaitingNotClosed(t *testing.T) {
	d := staffed()
	load(t, d, "ann", 2)
	waiting := d.Open("acme", 3, 0)
	done := d.Open("acme", 3, 0)
	for _, id := range []string{waiting, done} {
		if err := d.Assign(id, "ann"); err != nil {
			t.Fatal(err)
		}
	}
	status(t, d, waiting, "waiting", 5)
	status(t, d, done, "closed", 5)
	if got := d.Load("ann"); got != 3 {
		t.Errorf("Load = %d", got)
	}
}

func TestLoadLeavesOutP1(t *testing.T) {
	d := staffed()
	load(t, d, "ann", 1)
	if err := d.Assign(d.Open("acme", 1, 0), "ann"); err != nil {
		t.Fatal(err)
	}
	if got := d.Load("ann"); got != 1 {
		t.Errorf("Load = %d", got)
	}
}

func TestP1AssignableWhenFull(t *testing.T) {
	d := staffed()
	load(t, d, "ann", 8)
	if err := d.Assign(d.Open("acme", 1, 0), "ann"); err != nil {
		t.Errorf("err = %v", err)
	}
}

func TestCanTake(t *testing.T) {
	d := staffed()
	load(t, d, "ann", 7)
	if got, err := d.CanTake("ann", 3); err != nil || !got {
		t.Errorf("with 7: %v, %v", got, err)
	}
	load(t, d, "ann", 1)
	if got, err := d.CanTake("ann", 3); err != nil || got {
		t.Errorf("with 8: %v, %v", got, err)
	}
}

func TestCanTakeP1WhenFull(t *testing.T) {
	d := staffed()
	load(t, d, "ann", 8)
	if got, err := d.CanTake("ann", 1); err != nil || !got {
		t.Errorf("CanTake = %v, %v", got, err)
	}
	if _, err := d.CanTake("zed", 1); !errors.Is(err, desk.ErrNoAgent) {
		t.Errorf("unlisted agent: %v", err)
	}
}

func TestRebalanceStopsWhenFull(t *testing.T) {
	d := staffed()
	load(t, d, "bob", 7)
	late := d.Open("acme", 2, desk.At(0, 11, 0))
	early := d.Open("acme", 2, desk.At(0, 9, 0))
	for _, id := range []string{late, early} {
		if err := d.Assign(id, "ann"); err != nil {
			t.Fatal(err)
		}
	}
	got, err := d.Rebalance("ann", "bob", desk.At(0, 12, 0))
	if err != nil || got != 1 {
		t.Fatalf("Rebalance = %d, %v", got, err)
	}
	if who, _ := d.Assigned(early); who != "bob" {
		t.Errorf("the closest to breach is with %q", who)
	}
	if who, _ := d.Assigned(late); who != "ann" {
		t.Errorf("the other is with %q", who)
	}
}

func TestRebalanceMovesP1Regardless(t *testing.T) {
	d := staffed()
	load(t, d, "bob", 8)
	id := d.Open("acme", 1, desk.At(0, 9, 0))
	if err := d.Assign(id, "ann"); err != nil {
		t.Fatal(err)
	}
	if got, err := d.Rebalance("ann", "bob", desk.At(0, 10, 0)); err != nil || got != 1 {
		t.Errorf("Rebalance = %d, %v", got, err)
	}
}

func TestRebalanceUnknownAgent(t *testing.T) {
	if _, err := staffed().Rebalance("ann", "zed", 5); !errors.Is(err, desk.ErrNoAgent) {
		t.Errorf("err = %v", err)
	}
}

func TestVIPOneBetterAtOpen(t *testing.T) {
	d := desk.New()
	d.SetVIP("acme")
	got, _ := d.Get(d.Open("acme", 3, 0))
	if got.Priority != 2 {
		t.Errorf("VIP ticket has priority %d", got.Priority)
	}
	other, _ := d.Get(d.Open("beta", 3, 0))
	if other.Priority != 3 {
		t.Errorf("other ticket has priority %d", other.Priority)
	}
	top, _ := d.Get(d.Open("acme", 1, 0))
	if top.Priority != 1 {
		t.Errorf("VIP P1 has priority %d", top.Priority)
	}
}

func TestReprioritiseVIP(t *testing.T) {
	d := desk.New()
	d.SetVIP("acme")
	id := d.Open("acme", 3, 0)
	if err := d.Reprioritise(id, 4); err != nil {
		t.Fatal(err)
	}
	if got, _ := d.Get(id); got.Priority != 3 {
		t.Errorf("Priority = %d", got.Priority)
	}
}

func TestReprioritiseOrdinaryTicket(t *testing.T) {
	d := desk.New()
	id := d.Open("beta", 3, desk.At(0, 9, 0))
	if err := d.Reprioritise(id, 2); err != nil {
		t.Fatal(err)
	}
	if got, _ := d.Get(id); got.Priority != 2 {
		t.Errorf("Priority = %d", got.Priority)
	}
	if got := elapsed(t, d, id, desk.At(0, 10, 0)); got != 60 {
		t.Errorf("Elapsed = %d", got)
	}
}

func TestReprioritiseUnknown(t *testing.T) {
	if err := desk.New().Reprioritise("T-1", 2); !errors.Is(err, desk.ErrUnknownTicket) {
		t.Errorf("err = %v", err)
	}
}

func TestCustomersOnceSorted(t *testing.T) {
	d := desk.New()
	for _, name := range []string{"cee", "acme", "cee"} {
		d.Open(name, 3, 0)
	}
	if got := d.Customers(); !slices.Equal(got, []string{"acme", "cee"}) {
		t.Errorf("Customers = %v", got)
	}
}

func TestTargetTable(t *testing.T) {
	for priority, want := range map[int]int{1: 240, 2: 480, 3: 1440, 4: 4800} {
		if got := desk.Target(priority); got != want {
			t.Errorf("Target(%d) = %d", priority, got)
		}
	}
}

func TestWorkingInsideHours(t *testing.T) {
	if got := desk.New().Working(desk.At(0, 10, 0), desk.At(0, 11, 30)); got != 90 {
		t.Errorf("Working = %d", got)
	}
}

func TestWorkingSkipsNights(t *testing.T) {
	if got := desk.New().Working(desk.At(0, 16, 0), desk.At(1, 10, 0)); got != 120 {
		t.Errorf("Working = %d", got)
	}
}

func TestWorkingBeforeOpening(t *testing.T) {
	if got := desk.New().Working(desk.At(0, 7, 0), desk.At(0, 10, 0)); got != 60 {
		t.Errorf("Working = %d", got)
	}
}

func TestWorkingSkipsWeekend(t *testing.T) {
	if got := desk.New().Working(desk.At(4, 16, 0), desk.At(7, 10, 0)); got != 120 {
		t.Errorf("Working = %d", got)
	}
}

func TestHolidayHasNoSupport(t *testing.T) {
	d := desk.New()
	d.AddHoliday(1)
	if got := d.Working(desk.At(1, 9, 0), desk.At(1, 17, 0)); got != 0 {
		t.Errorf("Working = %d", got)
	}
}

func TestElapsedSupportHoursOnly(t *testing.T) {
	d := desk.New()
	id := d.Open("acme", 3, desk.At(0, 16, 0))
	if got := elapsed(t, d, id, desk.At(1, 10, 0)); got != 120 {
		t.Errorf("Elapsed = %d", got)
	}
}

func TestElapsedOverWeekend(t *testing.T) {
	d := desk.New()
	id := d.Open("acme", 3, desk.At(4, 16, 0))
	if got := elapsed(t, d, id, desk.At(7, 10, 0)); got != 120 {
		t.Errorf("Elapsed = %d", got)
	}
}

func TestElapsedP1AroundTheClock(t *testing.T) {
	d := desk.New()
	id := d.Open("acme", 1, desk.At(0, 16, 0))
	if got := elapsed(t, d, id, desk.At(1, 10, 0)); got != 1080 {
		t.Errorf("Elapsed = %d", got)
	}
}

func TestElapsedStopsAtClose(t *testing.T) {
	d := desk.New()
	id := d.Open("acme", 3, desk.At(0, 9, 0))
	status(t, d, id, "closed", desk.At(0, 10, 0))
	if got := elapsed(t, d, id, desk.At(0, 15, 0)); got != 60 {
		t.Errorf("Elapsed = %d", got)
	}
}

func TestElapsedSkipsWaiting(t *testing.T) {
	d := desk.New()
	id := d.Open("acme", 3, desk.At(0, 9, 0))
	status(t, d, id, "waiting", desk.At(0, 10, 0))
	status(t, d, id, "open", desk.At(0, 12, 0))
	if got := elapsed(t, d, id, desk.At(0, 13, 0)); got != 120 {
		t.Errorf("Elapsed = %d", got)
	}
}

func TestRemainingSkipsWaiting(t *testing.T) {
	d := desk.New()
	id := d.Open("acme", 2, desk.At(0, 9, 0))
	status(t, d, id, "waiting", desk.At(0, 10, 0))
	if got, err := d.Remaining(id, desk.At(0, 13, 0)); err != nil || got != 420 {
		t.Errorf("Remaining = %d, %v", got, err)
	}
}

func TestDeadlineNoneWhileWaiting(t *testing.T) {
	d := desk.New()
	id := d.Open("acme", 3, desk.At(0, 9, 0))
	status(t, d, id, "waiting", desk.At(0, 10, 0))
	if got := deadline(t, d, id, desk.At(0, 11, 0)); got != -1 {
		t.Errorf("Deadline = %d", got)
	}
}

func TestElapsedP1RunsWhileWaiting(t *testing.T) {
	d := desk.New()
	id := d.Open("acme", 1, desk.At(0, 9, 0))
	status(t, d, id, "waiting", desk.At(0, 10, 0))
	if got := elapsed(t, d, id, desk.At(0, 12, 0)); got != 180 {
		t.Errorf("Elapsed = %d", got)
	}
}

func TestBreachedP1WhileWaiting(t *testing.T) {
	d := desk.New()
	id := d.Open("acme", 1, desk.At(0, 9, 0))
	status(t, d, id, "waiting", desk.At(0, 9, 30))
	if got, err := d.Breached(id, desk.At(0, 13, 30)); err != nil || !got {
		t.Errorf("Breached = %v, %v", got, err)
	}
}

func TestDeadlineP1WhileWaiting(t *testing.T) {
	d := desk.New()
	id := d.Open("acme", 1, desk.At(0, 9, 0))
	status(t, d, id, "waiting", desk.At(0, 10, 0))
	if got, want := deadline(t, d, id, desk.At(0, 11, 0)), desk.At(0, 13, 0); got != want {
		t.Errorf("Deadline = %d, want %d", got, want)
	}
}

func TestRemainingGoesNegative(t *testing.T) {
	d := desk.New()
	id := d.Open("acme", 2, desk.At(0, 9, 0))
	if got, err := d.Remaining(id, desk.At(1, 10, 0)); err != nil || got != -60 {
		t.Errorf("Remaining = %d, %v", got, err)
	}
}

func TestBreachedOnlyPastTarget(t *testing.T) {
	d := desk.New()
	id := d.Open("acme", 2, desk.At(0, 9, 0))
	if got, err := d.Breached(id, desk.At(0, 17, 0)); err != nil || got {
		t.Errorf("on target: %v, %v", got, err)
	}
	if got, err := d.Breached(id, desk.At(1, 9, 1)); err != nil || !got {
		t.Errorf("past target: %v, %v", got, err)
	}
}

func TestWarningAtThreshold(t *testing.T) {
	d := desk.New()
	id := d.Open("acme", 2, desk.At(0, 9, 0))
	if got, err := d.Warning(id, desk.At(0, 9, 0)+383); err != nil || got {
		t.Errorf("just under: %v, %v", got, err)
	}
	if got, err := d.Warning(id, desk.At(0, 9, 0)+384); err != nil || !got {
		t.Errorf("at the line: %v, %v", got, err)
	}
}

func TestWarningEndsAtBreach(t *testing.T) {
	d := desk.New()
	id := d.Open("acme", 2, desk.At(0, 9, 0))
	if got, err := d.Warning(id, desk.At(1, 12, 0)); err != nil || got {
		t.Errorf("Warning = %v, %v", got, err)
	}
}

func TestDigestCounts(t *testing.T) {
	d := desk.New()
	d.Open("acme", 1, desk.At(0, 9, 0))
	d.Open("acme", 2, desk.At(0, 9, 0))
	d.Open("acme", 2, desk.At(0, 11, 0))
	status(t, d, d.Open("acme", 1, desk.At(0, 9, 0)), "closed", desk.At(0, 15, 0))
	got := d.Digest(desk.At(0, 16, 0))
	if got["breach"] != 1 || got["warning"] != 1 || got["ok"] != 1 {
		t.Errorf("Digest = %v", got)
	}
}

func TestReopenCarriesOn(t *testing.T) {
	d := desk.New()
	id := d.Open("acme", 3, desk.At(0, 9, 0))
	status(t, d, id, "closed", desk.At(0, 10, 0))
	if err := d.Reopen(id, desk.At(0, 12, 0)); err != nil {
		t.Fatal(err)
	}
	if got, _ := d.Get(id); got.Status != "open" {
		t.Errorf("Status = %q", got.Status)
	}
	if got := elapsed(t, d, id, desk.At(0, 13, 0)); got != 120 {
		t.Errorf("Elapsed = %d", got)
	}
}

func TestReopenRefusals(t *testing.T) {
	d := desk.New()
	id := d.Open("acme", 3, desk.At(0, 9, 0))
	if err := d.Reopen(id, desk.At(0, 10, 0)); !errors.Is(err, desk.ErrNotClosed) {
		t.Errorf("not closed: %v", err)
	}
	status(t, d, id, "closed", desk.At(0, 10, 0))
	if err := d.Reopen(id, desk.At(3, 10, 1)); !errors.Is(err, desk.ErrTooLate) {
		t.Errorf("too late: %v", err)
	}
	if err := d.Reopen(id, desk.At(3, 10, 0)); err != nil {
		t.Errorf("on the last minute: %v", err)
	}
}

func reportDesk(t *testing.T) *desk.Desk {
	t.Helper()
	d := desk.New()
	d.Open("acme", 2, desk.At(0, 9, 0))
	d.Open("beta", 2, desk.At(0, 10, 0))
	d.Open("cee", 3, desk.At(1, 11, 0))
	return d
}

func TestBreachReportHeader(t *testing.T) {
	first, _, _ := strings.Cut(reportDesk(t).BreachReport(desk.At(1, 12, 0)), "\n")
	if first != "ticket\tpriority\tcustomer\tminutes_over" {
		t.Errorf("first line %q", first)
	}
}

func TestBreachReportRows(t *testing.T) {
	got := strings.Split(strings.TrimSuffix(reportDesk(t).BreachReport(desk.At(1, 12, 0)), "\n"), "\n")
	if len(got) != 3 || !strings.HasPrefix(got[1], "T-1001\tP2\tacme\t") || !strings.HasPrefix(got[2], "T-1002\tP2\tbeta\t") {
		t.Errorf("BreachReport = %q", got)
	}
}

func TestBreachReportMinutesOver(t *testing.T) {
	want := "ticket\tpriority\tcustomer\tminutes_over\nT-1001\tP2\tacme\t180\nT-1002\tP2\tbeta\t120\n"
	if got := reportDesk(t).BreachReport(desk.At(1, 12, 0)); got != want {
		t.Errorf("BreachReport = %q", got)
	}
}

func TestBreachReportIncludesClosed(t *testing.T) {
	d := reportDesk(t)
	status(t, d, "T-1001", "closed", desk.At(1, 11, 0))
	if got := d.BreachReport(desk.At(1, 12, 0)); !strings.Contains(got, "T-1001") {
		t.Errorf("BreachReport = %q", got)
	}
}

func TestDeadlineAcrossNight(t *testing.T) {
	d := desk.New()
	id := d.Open("acme", 2, desk.At(0, 16, 0))
	if got, want := deadline(t, d, id, desk.At(0, 16, 0)), desk.At(1, 16, 0); got != want {
		t.Errorf("Deadline = %s, want %s", desk.Stamp(got), desk.Stamp(want))
	}
}

func TestDeadlineOverWeekend(t *testing.T) {
	d := desk.New()
	id := d.Open("acme", 2, desk.At(4, 16, 0))
	if got, want := deadline(t, d, id, desk.At(4, 16, 0)), desk.At(7, 16, 0); got != want {
		t.Errorf("Deadline = %s, want %s", desk.Stamp(got), desk.Stamp(want))
	}
}

func TestDeadlineP1AroundTheClock(t *testing.T) {
	d := desk.New()
	id := d.Open("acme", 1, desk.At(4, 16, 0))
	if got, want := deadline(t, d, id, desk.At(4, 16, 0)), desk.At(4, 20, 0); got != want {
		t.Errorf("Deadline = %s, want %s", desk.Stamp(got), desk.Stamp(want))
	}
}

func TestDeadlineSkipsHoliday(t *testing.T) {
	d := desk.New()
	d.AddHoliday(1)
	id := d.Open("acme", 2, desk.At(0, 16, 0))
	if got, want := deadline(t, d, id, desk.At(0, 16, 30)), desk.At(2, 16, 0); got != want {
		t.Errorf("Deadline = %s, want %s", desk.Stamp(got), desk.Stamp(want))
	}
}

func TestDeadlineAlreadyReached(t *testing.T) {
	d := desk.New()
	id := d.Open("acme", 2, desk.At(0, 9, 0))
	now := desk.At(3, 12, 0)
	if got := deadline(t, d, id, now); got != now {
		t.Errorf("Deadline = %d", got)
	}
}

func TestDeadlineUnknown(t *testing.T) {
	if _, err := desk.New().Deadline("T-1", 5); !errors.Is(err, desk.ErrUnknownTicket) {
		t.Errorf("err = %v", err)
	}
}

func TestQueueOrder(t *testing.T) {
	d := desk.New()
	stopped := d.Open("acme", 3, desk.At(0, 9, 0))
	status(t, d, stopped, "waiting", desk.At(0, 9, 30))
	late := d.Open("acme", 2, desk.At(0, 10, 0))
	early := d.Open("acme", 2, desk.At(0, 9, 0))
	same := d.Open("acme", 2, desk.At(0, 9, 0))
	status(t, d, d.Open("acme", 2, desk.At(0, 9, 0)), "closed", desk.At(0, 9, 30))
	want := []string{early, same, late, stopped}
	if got := d.Queue(desk.At(0, 11, 0)); !slices.Equal(got, want) {
		t.Errorf("Queue = %v, want %v", got, want)
	}
}

func TestWaitingListLongestFirst(t *testing.T) {
	d := desk.New()
	second := d.Open("acme", 3, 0)
	first := d.Open("acme", 3, 0)
	d.Open("acme", 3, 0)
	status(t, d, second, "waiting", 50)
	status(t, d, first, "waiting", 20)
	if got := d.WaitingList(100); !slices.Equal(got, []string{first, second}) {
		t.Errorf("WaitingList = %v", got)
	}
}

func TestAddNoteRules(t *testing.T) {
	d := desk.New()
	id := d.Open("acme", 3, 0)
	if err := d.AddNote(id, "ann", ""); !errors.Is(err, desk.ErrEmptyNote) {
		t.Errorf("empty note: %v", err)
	}
	if err := d.AddNote(id, "ann", "hello"); err != nil {
		t.Fatal(err)
	}
	if got, _ := d.Get(id); len(got.Notes) != 1 || got.Notes[0].Line() != "ann: hello" {
		t.Errorf("Notes = %+v", got.Notes)
	}
}

func TestTranscriptShowsNotes(t *testing.T) {
	d := desk.New()
	id := d.Open("acme", 3, 0)
	for _, text := range []string{"first", "second"} {
		if err := d.AddNote(id, "ann", text); err != nil {
			t.Fatal(err)
		}
	}
	want := []string{"ann: first", "ann: second"}
	if got, err := d.Transcript(id); err != nil || !slices.Equal(got, want) {
		t.Errorf("Transcript = %q, %v", got, err)
	}
}

func TestTranscriptLeavesOutTeamNotes(t *testing.T) {
	d := desk.New()
	id := d.Open("acme", 3, 0)
	for _, text := range []string{"hello", "#customer is rude", "bye"} {
		if err := d.AddNote(id, "ann", text); err != nil {
			t.Fatal(err)
		}
	}
	want := []string{"ann: hello", "ann: bye"}
	if got, err := d.Transcript(id); err != nil || !slices.Equal(got, want) {
		t.Errorf("Transcript = %q, %v", got, err)
	}
}

func TestLastSeenByCustomer(t *testing.T) {
	d := desk.New()
	id := d.Open("acme", 3, 0)
	if got, err := d.LastPublic(id); err != nil || got != "" {
		t.Errorf("no notes: %q, %v", got, err)
	}
	for _, text := range []string{"hello", "#customer is rude"} {
		if err := d.AddNote(id, "ann", text); err != nil {
			t.Fatal(err)
		}
	}
	if got, err := d.LastPublic(id); err != nil || got != "ann: hello" {
		t.Errorf("LastPublic = %q, %v", got, err)
	}
}

func TestCustomerLinesCountOpen(t *testing.T) {
	d := desk.New()
	d.Open("beta", 3, 0)
	d.Open("acme", 3, 0)
	status(t, d, d.Open("acme", 3, 0), "waiting", 5)
	status(t, d, d.Open("beta", 3, 0), "closed", 5)
	want := []string{"acme: 2", "beta: 1"}
	if got := d.CustomerLines(); !slices.Equal(got, want) {
		t.Errorf("CustomerLines = %q", got)
	}
}

func TestComplianceShare(t *testing.T) {
	d := desk.New()
	if got := d.Compliance(desk.At(9, 12, 0)); got != 100 {
		t.Errorf("no closed tickets: %d", got)
	}
	status(t, d, d.Open("acme", 2, desk.At(0, 9, 0)), "closed", desk.At(0, 10, 0))
	status(t, d, d.Open("acme", 2, desk.At(0, 9, 0)), "closed", desk.At(0, 10, 0))
	status(t, d, d.Open("acme", 2, desk.At(0, 9, 0)), "closed", desk.At(3, 10, 0))
	d.Open("acme", 2, desk.At(0, 9, 0))
	if got := d.Compliance(desk.At(9, 12, 0)); got != 66 {
		t.Errorf("Compliance = %d", got)
	}
}

func TestAgentSheetLines(t *testing.T) {
	d := staffed()
	load(t, d, "bob", 2)
	want := []string{"ann: 0", "bob: 2"}
	if got := d.AgentSheet(); !slices.Equal(got, want) {
		t.Errorf("AgentSheet = %q", got)
	}
}
`},
		Solution: map[string]string{
			"ticket.go": `package desk

import (
	"errors"
	"fmt"
	"sort"
)

var (
	// ErrUnknownTicket is returned by every call naming a ticket that is missing.
	ErrUnknownTicket = errors.New("unknown ticket")
	// ErrBadStatus is returned for a status that does not exist.
	ErrBadStatus = errors.New("bad status")
)

// Event is one change of a ticket's status.
type Event struct {
	At     int
	Status string
}

// Ticket is one customer problem. Priority runs from 1, the most urgent, to 4.
// Opened and Event.At are minutes, as described in clock.go. Status is "open",
// "waiting" while we wait for the customer, or "closed". Events holds the changes
// of status since the ticket was opened, oldest first.
type Ticket struct {
	ID       string
	Customer string
	Priority int
	Opened   int
	Agent    string
	Status   string
	Events   []Event
	Notes    []Note
}

// Desk holds the tickets and the agents who work on them.
type Desk struct {
	tickets  []*Ticket
	next     int
	agents   map[string]bool
	vip      map[string]bool
	holidays map[int]bool
}

// New returns an empty desk.
func New() *Desk {
	return &Desk{next: 1001, agents: map[string]bool{}, vip: map[string]bool{}, holidays: map[int]bool{}}
}

// SetVIP puts a customer on the VIP list.
func (d *Desk) SetVIP(customer string) { d.vip[customer] = true }

// uplift returns the priority a customer's ticket really gets.
func (d *Desk) uplift(customer string, priority int) int {
	if d.vip[customer] && priority > 1 {
		return priority - 1
	}
	return priority
}

// Open records a new ticket and returns its ID.
func (d *Desk) Open(customer string, priority, at int) string {
	t := &Ticket{ID: fmt.Sprintf("T-%d", d.next), Customer: customer, Priority: d.uplift(customer, priority), Opened: at, Status: "open"}
	d.next++
	d.tickets = append(d.tickets, t)
	return t.ID
}

// find returns the ticket with an ID, or nil.
func (d *Desk) find(id string) *Ticket {
	for _, t := range d.tickets {
		if t.ID == id {
			return t
		}
	}
	return nil
}

// ticket returns the ticket with an ID, or the error for a missing one.
func (d *Desk) ticket(id string) (*Ticket, error) {
	t := d.find(id)
	if t == nil {
		return nil, ErrUnknownTicket
	}
	return t, nil
}

// Get returns a copy of a ticket.
func (d *Desk) Get(id string) (Ticket, bool) {
	t := d.find(id)
	if t == nil {
		return Ticket{}, false
	}
	return *t, true
}

// IDs returns every ticket's ID, sorted.
func (d *Desk) IDs() []string {
	out := make([]string, 0, len(d.tickets))
	for _, t := range d.tickets {
		out = append(out, t.ID)
	}
	sort.Strings(out)
	return out
}

// OpenCount returns how many tickets are not closed.
func (d *Desk) OpenCount() int {
	n := 0
	for _, t := range d.tickets {
		if t.Status != "closed" {
			n++
		}
	}
	return n
}

// Customers returns each customer once, sorted.
func (d *Desk) Customers() []string {
	seen := map[string]bool{}
	out := []string{}
	for _, t := range d.tickets {
		if !seen[t.Customer] {
			seen[t.Customer] = true
			out = append(out, t.Customer)
		}
	}
	sort.Strings(out)
	return out
}

// SetStatus records a change of a ticket's status.
func (d *Desk) SetStatus(id, status string, at int) error {
	t, err := d.ticket(id)
	if err != nil {
		return err
	}
	if status != "open" && status != "waiting" && status != "closed" {
		return ErrBadStatus
	}
	t.Status = status
	t.Events = append(t.Events, Event{At: at, Status: status})
	return nil
}

// History returns a ticket's changes of status, oldest first.
func (d *Desk) History(id string) ([]Event, error) {
	t, err := d.ticket(id)
	if err != nil {
		return nil, err
	}
	return append([]Event(nil), t.Events...), nil
}

// Assigned returns who has a ticket, or an empty string if nobody does.
func (d *Desk) Assigned(id string) (string, error) {
	t, err := d.ticket(id)
	if err != nil {
		return "", err
	}
	return t.Agent, nil
}
`,
			"status.go": `package desk

import "errors"

var (
	// ErrNotClosed is returned when a ticket that is not closed would be reopened.
	ErrNotClosed = errors.New("ticket is not closed")
	// ErrTooLate is returned when a ticket was closed too long ago to reopen.
	ErrTooLate = errors.New("too late to reopen")
)

const reopenWindow = 3 * dayMinutes

// Reopen puts a closed ticket back to open, and its clock carries on.
func (d *Desk) Reopen(id string, at int) error {
	t, err := d.ticket(id)
	if err != nil {
		return err
	}
	if t.Status != "closed" {
		return ErrNotClosed
	}
	if at-t.Events[len(t.Events)-1].At > reopenWindow {
		return ErrTooLate
	}
	return d.SetStatus(id, "open", at)
}

// Reprioritise changes a ticket's priority. Its SLA time so far stays.
func (d *Desk) Reprioritise(id string, priority int) error {
	t, err := d.ticket(id)
	if err != nil {
		return err
	}
	t.Priority = d.uplift(t.Customer, priority)
	return nil
}
`,
			"clock.go": `package desk

import "fmt"

const (
	dayMinutes  = 24 * 60
	openMinute  = 9 * 60
	closeMinute = 17 * 60
)

// Times on the desk are whole minutes. Minute 0 is 00:00 on the Monday of the
// desk's first week. Days are numbered from that Monday as day 0, so day 5 is the
// first Saturday and day 7 the second Monday.

// At returns the minute for a time on a day.
func At(day, hour, minute int) int { return day*dayMinutes + hour*60 + minute }

// DayOf returns the day a minute falls on.
func DayOf(at int) int { return at / dayMinutes }

// Weekday returns the short name of the weekday a minute falls on.
func Weekday(at int) string {
	return [...]string{"Mon", "Tue", "Wed", "Thu", "Fri", "Sat", "Sun"}[DayOf(at)%7]
}

// Stamp writes a minute for people to read, like "Tue 14:05".
func Stamp(at int) string {
	return fmt.Sprintf("%s %02d:%02d", Weekday(at), at%dayMinutes/60, at%60)
}

// AddHoliday marks a day number as having no support hours.
func (d *Desk) AddHoliday(day int) { d.holidays[day] = true }

// supportDay reports whether a day has support hours.
func (d *Desk) supportDay(day int) bool { return day%7 < 5 && !d.holidays[day] }

// Working returns how many support-hours minutes lie between two minutes.
func (d *Desk) Working(from, to int) int {
	total := 0
	for day := from / dayMinutes; day*dayMinutes < to; day++ {
		if !d.supportDay(day) {
			continue
		}
		start := max(from, day*dayMinutes+openMinute)
		end := min(to, day*dayMinutes+closeMinute)
		if end > start {
			total += end - start
		}
	}
	return total
}
`,
			"sla.go": `package desk

import "sort"

var targets = map[int]int{1: 240, 2: 480, 3: 1440, 4: 4800}

const warnPct = 80

// Target returns the target of a priority in minutes of SLA time.
func Target(priority int) int { return targets[priority] }

// clockRuns reports whether a ticket's clock runs in a status.
func clockRuns(t *Ticket, status string) bool {
	if status == "closed" {
		return false
	}
	if status == "waiting" && t.Priority != 1 {
		return false
	}
	return true
}

// counted returns the SLA time a ticket uses between two minutes with its clock on.
func (d *Desk) counted(t *Ticket, from, to int) int {
	if t.Priority == 1 {
		return to - from
	}
	return d.Working(from, to)
}

// elapsed returns the SLA time a ticket has used up to a minute.
func (d *Desk) elapsed(t *Ticket, now int) int {
	total, status, from := 0, "open", t.Opened
	for _, e := range t.Events {
		if e.At >= now {
			break
		}
		if clockRuns(t, status) {
			total += d.counted(t, from, e.At)
		}
		status, from = e.Status, e.At
	}
	if clockRuns(t, status) && now > from {
		total += d.counted(t, from, now)
	}
	return total
}

// Elapsed returns the SLA time a ticket has used up to a minute.
func (d *Desk) Elapsed(id string, now int) (int, error) {
	t, err := d.ticket(id)
	if err != nil {
		return 0, err
	}
	return d.elapsed(t, now), nil
}

// Remaining returns a ticket's target less its SLA time, negative when past it.
func (d *Desk) Remaining(id string, now int) (int, error) {
	t, err := d.ticket(id)
	if err != nil {
		return 0, err
	}
	return Target(t.Priority) - d.elapsed(t, now), nil
}

// Breached reports whether a ticket is in breach at a minute.
func (d *Desk) Breached(id string, now int) (bool, error) {
	t, err := d.ticket(id)
	if err != nil {
		return false, err
	}
	return d.elapsed(t, now) > Target(t.Priority), nil
}

// Warning reports whether a ticket is at warning level and not yet in breach.
func (d *Desk) Warning(id string, now int) (bool, error) {
	t, err := d.ticket(id)
	if err != nil {
		return false, err
	}
	used, target := d.elapsed(t, now), Target(t.Priority)
	return used*100 >= target*warnPct && used <= target, nil
}

// Digest returns how many tickets not closed are "breach", "warning" and "ok".
func (d *Desk) Digest(now int) map[string]int {
	out := map[string]int{"breach": 0, "warning": 0, "ok": 0}
	for _, t := range d.tickets {
		if t.Status == "closed" {
			continue
		}
		if breached, _ := d.Breached(t.ID, now); breached {
			out["breach"]++
		} else if warning, _ := d.Warning(t.ID, now); warning {
			out["warning"]++
		} else {
			out["ok"]++
		}
	}
	return out
}

// Deadline returns the minute a ticket's SLA time reaches its target if its status
// stays as it is, now if it already has, and -1 if its clock is not running.
func (d *Desk) Deadline(id string, now int) (int, error) {
	t, err := d.ticket(id)
	if err != nil {
		return 0, err
	}
	if !clockRuns(t, t.Status) {
		return -1, nil
	}
	left := Target(t.Priority) - d.elapsed(t, now)
	if left <= 0 {
		return now, nil
	}
	if t.Priority == 1 {
		return now + left, nil
	}
	for day := now / dayMinutes; ; day++ {
		if !d.supportDay(day) {
			continue
		}
		start := max(now, day*dayMinutes+openMinute)
		end := day*dayMinutes + closeMinute
		if end <= start {
			continue
		}
		if end-start >= left {
			return start + left, nil
		}
		left -= end - start
	}
}

// Queue returns the tickets not closed, earliest Deadline first, those without one
// last, ties by ID.
func (d *Desk) Queue(now int) []string {
	due := map[string]int{}
	out := []string{}
	for _, t := range d.tickets {
		if t.Status != "closed" {
			out = append(out, t.ID)
			due[t.ID], _ = d.Deadline(t.ID, now)
			if due[t.ID] < 0 {
				due[t.ID] = int(^uint(0) >> 1)
			}
		}
	}
	sort.Strings(out)
	sort.SliceStable(out, func(a, b int) bool { return due[out[a]] < due[out[b]] })
	return out
}
`,
			"agents.go": `package desk

import (
	"errors"
	"sort"
)

var (
	// ErrNoAgent is returned for a name that is not on the desk's list of agents.
	ErrNoAgent = errors.New("no such agent")
	// ErrAgentFull is returned when an agent cannot take another ticket.
	ErrAgentFull = errors.New("agent is full")
)

const maxLoad = 8

// AddAgent puts an agent on the desk.
func (d *Desk) AddAgent(name string) { d.agents[name] = true }

// Agents returns the names of the agents, sorted.
func (d *Desk) Agents() []string {
	out := make([]string, 0, len(d.agents))
	for name := range d.agents {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// Load returns how many of an agent's tickets count towards their limit.
func (d *Desk) Load(agent string) int {
	n := 0
	for _, t := range d.tickets {
		if t.Agent == agent && t.Status != "closed" && t.Priority != 1 {
			n++
		}
	}
	return n
}

// Assign gives a ticket to an agent.
func (d *Desk) Assign(id, agent string) error {
	t, err := d.ticket(id)
	if err != nil {
		return err
	}
	if !d.agents[agent] {
		return ErrNoAgent
	}
	if t.Agent == agent {
		return nil
	}
	if t.Priority != 1 && d.Load(agent) >= maxLoad {
		return ErrAgentFull
	}
	t.Agent = agent
	return nil
}

// CanTake reports whether an agent could be given one more ticket of a priority.
func (d *Desk) CanTake(agent string, priority int) (bool, error) {
	if !d.agents[agent] {
		return false, ErrNoAgent
	}
	return priority == 1 || d.Load(agent) < maxLoad, nil
}

// Rebalance moves an agent's tickets that are not closed to another agent, those
// closest to breach first, as far as the other agent can take them.
func (d *Desk) Rebalance(from, to string, now int) (int, error) {
	if !d.agents[from] || !d.agents[to] {
		return 0, ErrNoAgent
	}
	var mine []*Ticket
	for _, t := range d.tickets {
		if t.Agent == from && t.Status != "closed" {
			mine = append(mine, t)
		}
	}
	left := func(t *Ticket) int { return Target(t.Priority) - d.elapsed(t, now) }
	sort.Slice(mine, func(a, b int) bool {
		if left(mine[a]) != left(mine[b]) {
			return left(mine[a]) < left(mine[b])
		}
		return mine[a].ID < mine[b].ID
	})
	moved := 0
	for _, t := range mine {
		if t.Priority == 1 || d.Load(to) < maxLoad {
			t.Agent = to
			moved++
		}
	}
	return moved, nil
}
`,
			"notes.go": `package desk

import (
	"errors"
	"strings"
)

// ErrEmptyNote is returned for a note with no text.
var ErrEmptyNote = errors.New("empty note")

// Note is one remark on a ticket.
type Note struct {
	Author string
	Text   string
}

// Line writes a note the way the desk shows it.
func (n Note) Line() string { return n.Author + ": " + n.Text }

// public reports whether the customer may see a note.
func (n Note) public() bool { return !strings.HasPrefix(n.Text, "#") }

// AddNote adds a note to a ticket.
func (d *Desk) AddNote(id, author, text string) error {
	t, err := d.ticket(id)
	if err != nil {
		return err
	}
	if text == "" {
		return ErrEmptyNote
	}
	t.Notes = append(t.Notes, Note{Author: author, Text: text})
	return nil
}

// Transcript returns what the customer sees of a ticket's notes, oldest first.
func (d *Desk) Transcript(id string) ([]string, error) {
	t, err := d.ticket(id)
	if err != nil {
		return nil, err
	}
	out := []string{}
	for _, n := range t.Notes {
		if n.public() {
			out = append(out, n.Line())
		}
	}
	return out, nil
}

// LastPublic returns the latest note the customer can see, or an empty string.
func (d *Desk) LastPublic(id string) (string, error) {
	lines, err := d.Transcript(id)
	if err != nil || len(lines) == 0 {
		return "", err
	}
	return lines[len(lines)-1], nil
}
`,
			"report.go": `package desk

import (
	"fmt"
	"sort"
	"strings"
)

// Summary returns how many tickets are in each status, for the wall screen.
func (d *Desk) Summary() string {
	counts := map[string]int{}
	for _, t := range d.tickets {
		counts[t.Status]++
	}
	var b strings.Builder
	for _, status := range []string{"open", "waiting", "closed"} {
		fmt.Fprintf(&b, "%s: %d\n", status, counts[status])
	}
	return b.String()
}

// ByPriority returns how many tickets there are of each priority, closed ones too.
func (d *Desk) ByPriority() map[int]int {
	out := map[int]int{}
	for _, t := range d.tickets {
		out[t.Priority]++
	}
	return out
}

// BreachReport returns the weekly breach report for the service manager.
func (d *Desk) BreachReport(now int) string {
	type row struct {
		t    *Ticket
		over int
	}
	var rows []row
	for _, t := range d.tickets {
		if over := d.elapsed(t, now) - Target(t.Priority); over > 0 {
			rows = append(rows, row{t, over})
		}
	}
	sort.SliceStable(rows, func(a, b int) bool {
		if rows[a].over != rows[b].over {
			return rows[a].over > rows[b].over
		}
		return rows[a].t.ID < rows[b].t.ID
	})
	var b strings.Builder
	b.WriteString("ticket\tpriority\tcustomer\tminutes_over\n")
	for _, r := range rows {
		fmt.Fprintf(&b, "%s\tP%d\t%s\t%d\n", r.t.ID, r.t.Priority, r.t.Customer, r.over)
	}
	return b.String()
}

// WaitingList returns the tickets in "waiting", those waiting longest first.
func (d *Desk) WaitingList(now int) []string {
	since := map[string]int{}
	out := []string{}
	for _, t := range d.tickets {
		if t.Status == "waiting" {
			since[t.ID] = t.Events[len(t.Events)-1].At
			out = append(out, t.ID)
		}
	}
	sort.SliceStable(out, func(a, b int) bool { return now-since[out[a]] > now-since[out[b]] })
	return out
}

// CustomerLines returns "customer: N" for each customer, sorted, with N the number
// of their tickets not closed.
func (d *Desk) CustomerLines() []string {
	counts := map[string]int{}
	for _, t := range d.tickets {
		if t.Status != "closed" {
			counts[t.Customer]++
		}
	}
	out := []string{}
	for _, name := range d.Customers() {
		out = append(out, fmt.Sprintf("%s: %d", name, counts[name]))
	}
	return out
}

// Compliance returns the whole percentage of closed tickets not in breach.
func (d *Desk) Compliance(now int) int {
	closed, met := 0, 0
	for _, t := range d.tickets {
		if t.Status == "closed" {
			closed++
			if d.elapsed(t, now) <= Target(t.Priority) {
				met++
			}
		}
	}
	if closed == 0 {
		return 100
	}
	return met * 100 / closed
}

// AgentSheet returns "name: load" for each agent, sorted by name.
func (d *Desk) AgentSheet() []string {
	out := []string{}
	for _, name := range d.Agents() {
		out = append(out, fmt.Sprintf("%s: %d", name, d.Load(name)))
	}
	return out
}
`,
		},
		Turns: script(
			local("Read docs/sla-policy.txt and tell me in a few lines what decides whether a ticket is in breach. No code changes."),
			local("Add `func (d *Desk) OpenCount() int` to ticket.go: how many tickets are not closed."),
			cloud("Add `var ErrUnknownTicket = errors.New(\"unknown ticket\")` to ticket.go. Every call that names a ticket we don't have returns it, including all the calls we add later. First one: `func (d *Desk) SetStatus(id, status string, at int) error`, which sets Status and appends to Events. A status other than \"open\", \"waiting\" or \"closed\" returns a new `ErrBadStatus`."),
			cloud("A decision from the support manager: time a ticket spends in \"waiting\" does not count towards its target, because the customer has the ball. Nothing measures time yet. For now add `func (d *Desk) History(id string) ([]Event, error)` to ticket.go: a copy of the ticket's Events."),
			cloud("Another one from the manager: an agent holds at most 8 tickets that are not closed, and P1 tickets are on top of that, so they don't count and can always be assigned. Nothing assigns yet. For now add `func (d *Desk) Assigned(id string) (string, error)` to ticket.go: who has the ticket, \"\" if nobody does."),
			local("Add `func Target(priority int) int` in a new file sla.go: the target for that priority."),
			local("Add `func (d *Desk) Assign(id, agent string) error` to agents.go: gives the ticket to that agent. A name not on the desk's list returns `ErrNoAgent`, and an agent who can't take it returns a new `ErrAgentFull`."),
			local("Add `func (d *Desk) Load(agent string) int` to agents.go: how many of the agent's tickets count towards what they may hold."),
			local("Add `func (d *Desk) SetVIP(customer string)` to ticket.go, which puts a customer on a VIP list. A VIP's ticket always sits one priority better than the one it is given, wherever a priority is given, and P1 stays P1. Apply that in `Open` now."),
			cloud("The service manager sent docs/breach-report.txt. Read it and tell me whether Ticket holds what that report needs. No code changes."),
			cloud("Add `func (d *Desk) Working(from, to int) int` to clock.go: how many minutes of support hours lie between those two minutes."),
			local("Add `func (d *Desk) Elapsed(id string, now int) (int, error)` to sla.go: the SLA time the ticket has used up to minute now. A closed ticket's time stops when it closes."),
			local("Add `func (d *Desk) Remaining(id string, now int) (int, error)` to sla.go, its target less its SLA time, negative when past it, and `func (d *Desk) Breached(id string, now int) (bool, error)`: whether it is in breach."),
			local("Team leads want a warning on a ticket once it has used 80 percent of its target. Nothing warns yet, we'll add that soon. For now add `func (d *Desk) Customers() []string` to ticket.go: each customer once, sorted."),
			local("The desk closes on public holidays. Add `func (d *Desk) AddHoliday(day int)` to clock.go, taking a day number, and make `Working` treat those days as having no support hours."),
			local("The support director changed the rule on \"waiting\": P1 tickets keep running while waiting. Everything else stays as it was. Update what `Elapsed` does."),
			cloud("Time for a tidy-up. Every call that takes a ticket ID does its own lookup and check: add an unexported `func (d *Desk) ticket(id string) (*Ticket, error)` to ticket.go and move ticket.go, agents.go and sla.go onto it. In sla.go, split out one small function that says whether a ticket's clock runs in a given status and one helper for its SLA time up to a minute, and have `Elapsed`, `Remaining` and `Breached` use them. No change in behaviour."),
			cloud("Add `func (d *Desk) Reopen(id string, at int) error` in a new file status.go: a closed ticket goes back to \"open\" and its clock carries on from where it stopped. One that is not closed returns a new `ErrNotClosed`. More than 3 days after it closed, counted in plain minutes, returns a new `ErrTooLate`."),
			cloud("Add `func (d *Desk) AddNote(id, author, text string) error` to notes.go, which appends to Notes, with a new `ErrEmptyNote` for empty text. A note whose text starts with # is internal, for the team only, and a customer must never see it anywhere we show notes."),
			local("Add `func (d *Desk) BreachReport(now int) string` to report.go: the weekly report for the service manager."),
			local("Add `func (d *Desk) CanTake(agent string, priority int) (bool, error)` to agents.go: whether the agent could be given one more ticket of that priority. A name not on the desk's list as before."),
			cloud("Add `func (d *Desk) Rebalance(from, to string, now int) (int, error)` to agents.go: from is going on leave, so move their tickets that are not closed to the other agent, those closest to breach first, ties by ID, as far as that agent can take them. Returns how many moved. A name not on the desk's list as before."),
			cloud("Add `func (d *Desk) Warning(id string, now int) (bool, error)` to sla.go: whether the ticket is at warning level and not yet in breach. Also `func (d *Desk) Digest(now int) map[string]int`: how many tickets that are not closed are \"breach\", \"warning\" and \"ok\"."),
			cloud("Add `func (d *Desk) Reprioritise(id string, priority int) error` to status.go: gives the ticket that priority. Its SLA time so far stays as it is."),
			local("Add `func (d *Desk) WaitingList(now int) []string` to report.go: the IDs of tickets in \"waiting\", those waiting longest first, by plain minutes since they went into it."),
			cloud("Add `func (d *Desk) Deadline(id string, now int) (int, error)` to sla.go: the minute at which the ticket's SLA time reaches its target if its status stays as it is. If it has already reached it, that is now. If its clock isn't running, -1. This has to be exact to the minute across days."),
			cloud("Add `func (d *Desk) Queue(now int) []string` to sla.go: the IDs of tickets that are not closed, earliest `Deadline` first, those with -1 last, ties by ID."),
			local("Add `func (d *Desk) Transcript(id string) ([]string, error)` to notes.go: what the customer sees of the ticket's notes, oldest first, each written with `Line`. Also `func (d *Desk) LastPublic(id string) (string, error)`: the last of those, \"\" if there are none."),
			local("Add `func (d *Desk) CustomerLines() []string` to report.go: `customer: N` for each customer, sorted, with N the number of their tickets that are not closed."),
			local("Add `func (d *Desk) Compliance(now int) int` to report.go: the whole percentage, dropping any fraction, of closed tickets that are not in breach, and 100 when none are closed."),
			local("Add `func (d *Desk) AgentSheet() []string` to report.go: `name: N` for each agent, sorted by name, with N from `Load`."),
		),
		Carries: []Carry{
			{
				What:        "targets by priority, support hours, P1 around the clock, and breach only past target (docs/sla-policy.txt)",
				Kind:        CarryDiscovered,
				Established: 0,
				Needed:      []int{5, 10, 11, 12, 25},
				Source:      "docs/sla-policy.txt",
				Tests: []string{
					"TestTargetTable", "TestWorkingSkipsNights", "TestWorkingBeforeOpening", "TestWorkingSkipsWeekend", "TestElapsedSupportHoursOnly",
					"TestElapsedOverWeekend", "TestElapsedP1AroundTheClock", "TestRemainingGoesNegative", "TestBreachedOnlyPastTarget", "TestBreachReportMinutesOver",
					"TestDeadlineAcrossNight", "TestDeadlineOverWeekend", "TestDeadlineP1AroundTheClock", "TestDeadlineSkipsHoliday",
				},
				Absent: []string{"240", "480", "1440", "4800", "17:00", "09:00", "around the clock", "past target"},
			},
			{
				What:        "ErrUnknownTicket from every call that names a missing ticket",
				Kind:        CarryEmbodied,
				Established: 2,
				Needed:      []int{3, 4, 6, 11, 12, 18, 27},
				Tests: []string{
					"TestSetStatusUnknown", "TestHistoryUnknown", "TestAssignedUnknown", "TestAssignUnknownTicket", "TestElapsedUnknown",
					"TestRemainingUnknown", "TestBreachedUnknown", "TestAddNoteUnknown", "TestTranscriptUnknown", "TestReprioritiseUnknown", "TestDeadlineUnknown",
				},
				Absent: []string{"errunknownticket", "unknown ticket", "we don't have"},
			},
			{
				What:        "time in waiting does not count towards the target",
				Kind:        CarryUser,
				Established: 3,
				Needed:      []int{11, 25},
				Tests:       []string{"TestElapsedSkipsWaiting", "TestRemainingSkipsWaiting", "TestDeadlineNoneWhileWaiting"},
				Absent:      []string{"does not count", "doesn't count", "has the ball"},
			},
			{
				What:        "an agent holds at most 8 tickets, and P1 tickets are outside that limit",
				Kind:        CarryUser,
				Established: 4,
				Needed:      []int{6, 7, 20, 21},
				Tests: []string{
					"TestAssignLimit", "TestLoadLeavesOutP1", "TestP1AssignableWhenFull", "TestCanTake", "TestCanTakeP1WhenFull",
					"TestRebalanceStopsWhenFull", "TestRebalanceMovesP1Regardless",
				},
				Absent: []string{"at most 8", "8 tickets", "on top of that"},
			},
			{
				What:        "a VIP's ticket sits one priority better wherever a priority is given",
				Kind:        CarryUser,
				Established: 8,
				Needed:      []int{23},
				Tests:       []string{"TestReprioritiseVIP"},
				Absent:      []string{"vip", "one priority better"},
			},
			{
				What:        "the breach report's tabs, header, closed tickets and order (docs/breach-report.txt)",
				Kind:        CarryDiscovered,
				Established: 9,
				Needed:      []int{19},
				Source:      "docs/breach-report.txt",
				Tests:       []string{"TestBreachReportHeader", "TestBreachReportRows", "TestBreachReportMinutesOver", "TestBreachReportIncludesClosed"},
				Absent:      []string{"minutes_over", "breach-report", "tab-separated"},
			},
			{
				What:        "a warning starts at 80 percent of the target",
				Kind:        CarryUser,
				Established: 13,
				Needed:      []int{22},
				Tests:       []string{"TestWarningAtThreshold", "TestDigestCounts"},
				Absent:      []string{"80 percent", "80%", "warning on a ticket"},
			},
			{
				What:        "public holidays have no support hours",
				Kind:        CarryEmbodied,
				Established: 14,
				Needed:      []int{25},
				Tests:       []string{"TestDeadlineSkipsHoliday"},
				Absent:      []string{"holiday"},
			},
			{
				What:        "P1 tickets changed: their clock keeps running while waiting, though other tickets' still stops",
				Kind:        CarryUser,
				Established: 15,
				Needed:      []int{25},
				Tests:       []string{"TestElapsedP1RunsWhileWaiting", "TestBreachedP1WhileWaiting", "TestDeadlineP1WhileWaiting"},
				Absent:      []string{"keep running", "keeps running"},
			},
			{
				What:        "a note starting with # is internal and a customer never sees it",
				Kind:        CarryUser,
				Established: 18,
				Needed:      []int{27},
				Tests:       []string{"TestTranscriptLeavesOutTeamNotes", "TestLastSeenByCustomer"},
				Absent:      []string{"internal", "starts with #"},
			},
		},
	}
}
