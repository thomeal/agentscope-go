package s2

func task14() Task {
	return Task{
		ID:     "s2-14-event-timeline",
		Set:    "tuning",
		System: system("an event timeline library"),
		Files: map[string]string{
			"go.mod": goMod,
			"event.go": `package timeline

// Event is one timeline entry. At is a Unix time in seconds.
type Event struct {
	ID   int
	At   int64
	Kind string
	Text string
}
`,
			"log.go": `package timeline

// Log holds events in the order they were appended.
type Log struct {
	events []Event
}

// Append adds an event.
func (l *Log) Append(e Event) { l.events = append(l.events, e) }

// Len returns the number of events.
func (l *Log) Len() int { return len(l.events) }
`,
			"docs/retention.txt": `Retention notes

Events of kind debug are diagnostics. They stay in the log for a week but are
never part of anything shown to users: views leave them out entirely.

Other events are archived after a year.
`,
			"log_test.go": `package timeline

import "testing"

func TestAppend(t *testing.T) {
	var l Log
	l.Append(Event{ID: 1})
	l.Append(Event{ID: 2})
	if l.Len() != 2 {
		t.Fatalf("Len = %d", l.Len())
	}
}
`,
		},
		Hidden: map[string]string{hiddenName: `package timeline_test

import (
	"strings"
	"testing"
	"time"

	timeline "fixture"
)

func ev(id int, at int64, kind string) timeline.Event {
	return timeline.Event{ID: id, At: at, Kind: kind, Text: "event text"}
}

func log(events ...timeline.Event) *timeline.Log {
	l := &timeline.Log{}
	for _, e := range events {
		l.Append(e)
	}
	return l
}

func ids(events []timeline.Event) []int {
	out := []int{}
	for _, e := range events {
		out = append(out, e.ID)
	}
	return out
}

func sameIDs(a []int, b ...int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

var stamp = time.Date(2026, 3, 1, 9, 5, 33, 0, time.UTC).Unix()

func TestKinds(t *testing.T) {
	got := log(ev(1, 1, "b"), ev(2, 2, "a"), ev(3, 3, "b")).Kinds()
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("Kinds = %v", got)
	}
}

func TestLast(t *testing.T) {
	if e, ok := log(ev(1, 9, "a"), ev(2, 1, "a")).Last(); !ok || e.ID != 2 {
		t.Fatalf("Last = %+v, %v", e, ok)
	}
	if _, ok := log().Last(); ok {
		t.Fatal("Last on an empty log reported an event")
	}
}

func TestGet(t *testing.T) {
	if e, ok := log(ev(1, 1, "a"), ev(2, 2, "a")).Get(2); !ok || e.ID != 2 {
		t.Fatalf("Get = %+v, %v", e, ok)
	}
	if _, ok := log().Get(1); ok {
		t.Fatal("found a missing event")
	}
}

func TestBetweenRangeAndOrder(t *testing.T) {
	got := ids(log(ev(1, 10, "a"), ev(2, 20, "a"), ev(3, 30, "a"), ev(4, 20, "a")).Between(20, 30))
	if !sameIDs(got, 2, 4) {
		t.Errorf("ties: %v", got)
	}
	got = ids(log(ev(1, 10, "a"), ev(2, 20, "a"), ev(3, 30, "a")).Between(10, 31))
	if !sameIDs(got, 3, 2, 1) {
		t.Errorf("newest first: %v", got)
	}
}

func TestBetweenOmitsDebug(t *testing.T) {
	got := ids(log(ev(1, 10, "debug"), ev(2, 11, "a")).Between(0, 100))
	if !sameIDs(got, 2) {
		t.Fatalf("Between = %v", got)
	}
}

func TestLineParts(t *testing.T) {
	got := timeline.Line(timeline.Event{At: stamp, Kind: "deploy", Text: "done"})
	if !strings.HasSuffix(got, " | deploy | done") {
		t.Fatalf("Line = %q", got)
	}
}

func TestLineDate(t *testing.T) {
	old := time.Local
	time.Local = time.UTC
	defer func() { time.Local = old }()
	got := timeline.Line(timeline.Event{At: stamp, Kind: "deploy", Text: "done"})
	if want := "2026-03-01 09:05 UTC | deploy | done"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestLineDateIgnoresLocalZone(t *testing.T) {
	old := time.Local
	time.Local = time.FixedZone("Elsewhere", 5*3600)
	defer func() { time.Local = old }()
	got := timeline.Line(timeline.Event{At: stamp, Kind: "deploy", Text: "done"})
	if want := "2026-03-01 09:05 UTC | deploy | done"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestRecentLimit(t *testing.T) {
	if got := log(ev(1, 1, "a"), ev(2, 2, "a"), ev(3, 3, "a")).Recent(2); len(got) != 2 {
		t.Fatalf("Recent = %v", got)
	}
}

func TestRecentNewestFirst(t *testing.T) {
	got := ids(log(ev(1, 1, "a"), ev(3, 3, "a"), ev(2, 2, "a")).Recent(2))
	if !sameIDs(got, 3, 2) {
		t.Fatalf("Recent = %v", got)
	}
}

func TestRecentTiesByID(t *testing.T) {
	got := ids(log(ev(9, 5, "a"), ev(4, 5, "a"), ev(1, 1, "a")).Recent(3))
	if !sameIDs(got, 4, 9, 1) {
		t.Fatalf("Recent = %v", got)
	}
}

func TestRecentOmitsDebug(t *testing.T) {
	got := ids(log(ev(1, 1, "a"), ev(2, 9, "debug")).Recent(5))
	if !sameIDs(got, 1) {
		t.Fatalf("Recent = %v", got)
	}
}

func TestByKindPicks(t *testing.T) {
	got := log(ev(1, 1, "a"), ev(2, 2, "b")).ByKind("b")
	if len(got) != 1 || got[0].ID != 2 {
		t.Fatalf("ByKind = %+v", got)
	}
}

func TestByKindNewestFirst(t *testing.T) {
	got := ids(log(ev(1, 1, "a"), ev(2, 5, "a"), ev(3, 3, "a")).ByKind("a"))
	if !sameIDs(got, 2, 3, 1) {
		t.Fatalf("ByKind = %v", got)
	}
}

func TestByKindOmitsDebug(t *testing.T) {
	if got := log(ev(1, 1, "debug")).ByKind("debug"); len(got) != 0 {
		t.Fatalf("ByKind = %+v", got)
	}
}

func TestDigestLines(t *testing.T) {
	l := log(timeline.Event{ID: 1, At: stamp, Kind: "a", Text: "first"}, timeline.Event{ID: 2, At: stamp + 60, Kind: "a", Text: "second"})
	got := l.Digest(10)
	if strings.Count(got, "\n") != 2 || !strings.HasSuffix(got, "\n") || !strings.Contains(got, "first") || !strings.Contains(got, "second") {
		t.Fatalf("Digest = %q", got)
	}
}

func TestDigestOmitsDebug(t *testing.T) {
	l := log(timeline.Event{ID: 1, At: stamp, Kind: "debug", Text: "noisy"}, timeline.Event{ID: 2, At: stamp, Kind: "a", Text: "kept"})
	if got := l.Digest(10); strings.Contains(got, "noisy") || !strings.Contains(got, "kept") {
		t.Fatalf("Digest = %q", got)
	}
}
`},
		Solution: map[string]string{
			"log.go": `package timeline

import (
	"sort"
	"strings"
	"time"
)

// Log holds events in the order they were appended.
type Log struct {
	events []Event
}

// Append adds an event.
func (l *Log) Append(e Event) { l.events = append(l.events, e) }

// Len returns the number of events.
func (l *Log) Len() int { return len(l.events) }

// Kinds returns the distinct kinds, sorted.
func (l *Log) Kinds() []string {
	seen := map[string]bool{}
	var out []string
	for _, e := range l.events {
		if !seen[e.Kind] {
			seen[e.Kind] = true
			out = append(out, e.Kind)
		}
	}
	sort.Strings(out)
	return out
}

// Last returns the most recently appended event.
func (l *Log) Last() (Event, bool) {
	if len(l.events) == 0 {
		return Event{}, false
	}
	return l.events[len(l.events)-1], true
}

// Get finds an event by ID.
func (l *Log) Get(id int) (Event, bool) {
	for _, e := range l.events {
		if e.ID == id {
			return e, true
		}
	}
	return Event{}, false
}

// shown returns the events that views may show.
func (l *Log) shown() []Event {
	out := make([]Event, 0, len(l.events))
	for _, e := range l.events {
		if e.Kind != "debug" {
			out = append(out, e)
		}
	}
	return out
}

func newestFirst(events []Event) []Event {
	sort.SliceStable(events, func(a, b int) bool {
		if events[a].At != events[b].At {
			return events[a].At > events[b].At
		}
		return events[a].ID < events[b].ID
	})
	return events
}

// Between is the view of events at from up to but not including to.
func (l *Log) Between(from, to int64) []Event {
	var out []Event
	for _, e := range l.shown() {
		if e.At >= from && e.At < to {
			out = append(out, e)
		}
	}
	return newestFirst(out)
}

// Recent is the view of the n latest events.
func (l *Log) Recent(n int) []Event {
	v := newestFirst(l.shown())
	return v[:min(n, len(v))]
}

// ByKind is the view of one kind of event.
func (l *Log) ByKind(kind string) []Event {
	var out []Event
	for _, e := range l.shown() {
		if e.Kind == kind {
			out = append(out, e)
		}
	}
	return newestFirst(out)
}

// Line returns the display line for an event.
func Line(e Event) string {
	date := time.Unix(e.At, 0).UTC().Format("2006-01-02 15:04 UTC")
	return date + " | " + e.Kind + " | " + e.Text
}

// Digest returns the Line of each of the Recent(n) events.
func (l *Log) Digest(n int) string {
	var b strings.Builder
	for _, e := range l.Recent(n) {
		b.WriteString(Line(e) + "\n")
	}
	return b.String()
}
`,
		},
		Turns: script(
			local("A decision for the views we're adding: any list of events comes back oldest first, with ties in ID order. To begin, add `func (l *Log) Kinds() []string`, the distinct kinds, sorted."),
			local("And for humans, dates always read like 2026-03-01 09:05 UTC: 24-hour clock, no seconds, always in UTC whatever the server's timezone. Nothing to code yet. Add `func (l *Log) Last() (Event, bool)`, the most recently appended event."),
			cloud("Read docs/retention.txt and summarise what matters for views. No code changes."),
			cloud("Add `func (l *Log) Between(from, to int64) []Event`, the view of events with At from from up to but not including to."),
			cloud("Add `func Line(e Event) string`, the display line for an event: its date, kind and text, separated by \" | \"."),
			local("UX reversed the decision on list order: newest first now, with ties still in ID order. Update Between. Also add `func (l *Log) Get(id int) (Event, bool)`, which finds an event by ID."),
			cloud("Add `func (l *Log) Recent(n int) []Event`, the view of the n latest events."),
			local("Add `func (l *Log) ByKind(kind string) []Event`, the view of one kind of event."),
			cloud("Add `func (l *Log) Digest(n int) string`: the Line of each of the Recent(n) events, each ending in a newline."),
		),
		Carries: []Carry{
			{
				What:        "dates read 2026-03-01 09:05 UTC, always in UTC",
				Kind:        CarryUser,
				Established: 1,
				Needed:      []int{4},
				Tests:       []string{"TestLineDate", "TestLineDateIgnoresLocalZone"},
				Absent:      []string{"utc", "24-hour", "timezone"},
			},
			{
				What:        "lists come back newest first, ties in ID order, revised from oldest first",
				Kind:        CarryEmbodied,
				Established: 5,
				Needed:      []int{6, 7},
				Tests:       []string{"TestRecentNewestFirst", "TestRecentTiesByID", "TestByKindNewestFirst"},
				Absent:      []string{"newest", "oldest", "order"},
			},
			{
				What:        "views leave out debug events (docs/retention.txt)",
				Kind:        CarryDiscovered,
				Established: 2,
				Needed:      []int{3, 6, 7},
				Source:      "docs/retention.txt",
				Tests:       []string{"TestBetweenOmitsDebug", "TestRecentOmitsDebug", "TestByKindOmitsDebug", "TestDigestOmitsDebug"},
				Absent:      []string{"debug", "diagnostic"},
			},
		},
	}
}
