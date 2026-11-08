package s2

func accC08() Task {
	return Task{
		ID:     "s2-c08-calendar-recurrence",
		Set:    "acceptance",
		System: system("a calendar recurrence library"),
		Files: map[string]string{
			"go.mod": goMod,
			"rule.go": `package recur

import "time"

// Rule is a repeating event: every Interval Units from Start. Unit is "week" or
// "month".
type Rule struct {
	Start    time.Time
	Unit     string
	Interval int
}

// Valid reports whether the rule can produce dates.
func (r Rule) Valid() bool {
	return r.Interval >= 1 && (r.Unit == "week" || r.Unit == "month")
}
`,
			"docs/office.txt": `Office calendar notes

The office is closed at weekends. An occurrence that falls on a Saturday or a
Sunday is held on the following Monday instead, and that Monday is its date
everywhere. Later occurrences are still worked out from the original pattern.

The front desk updates the wall planner on Fridays.
`,
			"rule_test.go": `package recur

import "testing"

func TestValid(t *testing.T) {
	if !(Rule{Unit: "week", Interval: 1}).Valid() {
		t.Fatal("should be valid")
	}
	if (Rule{Unit: "week"}).Valid() {
		t.Fatal("should not be valid")
	}
}
`,
		},
		Hidden: map[string]string{hiddenName: `package recur_test

import (
	"errors"
	"testing"
	"time"

	recur "fixture"
)

func day(y int, m time.Month, d int) time.Time { return time.Date(y, m, d, 0, 0, 0, 0, time.UTC) }

// mondays runs weekly from Monday 1 January 2024.
func mondays() recur.Rule { return recur.Rule{Start: day(2024, 1, 1), Unit: "week", Interval: 1} }

// saturdays runs weekly from Saturday 6 January 2024.
func saturdays() recur.Rule { return recur.Rule{Start: day(2024, 1, 6), Unit: "week", Interval: 1} }

// monthEnds runs monthly from Wednesday 31 January 2024.
func monthEnds() recur.Rule { return recur.Rule{Start: day(2024, 1, 31), Unit: "month", Interval: 1} }

var bad = recur.Rule{Start: day(2024, 1, 1), Unit: "day", Interval: 1}

func dates(got []time.Time, want ...time.Time) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if !got[i].Equal(want[i]) {
			return false
		}
	}
	return true
}

func nth(t *testing.T, r recur.Rule, n int, want time.Time) {
	t.Helper()
	got, err := r.Nth(n)
	if err != nil || !got.Equal(want) {
		t.Errorf("Nth(%d) = %v, %v, want %v", n, got, err, want)
	}
}

func TestDaysIn(t *testing.T) {
	if recur.DaysIn(2024, time.February) != 29 || recur.DaysIn(2023, time.February) != 28 || recur.DaysIn(2024, time.April) != 30 {
		t.Fatal("wrong month lengths")
	}
}

func TestNthEveryOtherWeek(t *testing.T) {
	nth(t, recur.Rule{Start: day(2024, 1, 1), Unit: "week", Interval: 2}, 3, day(2024, 2, 12))
}

func TestNthMonthly(t *testing.T) {
	nth(t, recur.Rule{Start: day(2024, 1, 15), Unit: "month", Interval: 1}, 1, day(2024, 2, 15))
}

func TestNthNegative(t *testing.T) {
	if _, err := mondays().Nth(-1); err == nil {
		t.Fatal("no error")
	}
}

func TestNthMonthEnd(t *testing.T) {
	nth(t, monthEnds(), 1, day(2024, 2, 29))
	nth(t, monthEnds(), 3, day(2024, 4, 30))
	nth(t, monthEnds(), 4, day(2024, 5, 31))
}

func TestNthMovesOffWeekend(t *testing.T) {
	nth(t, saturdays(), 0, day(2024, 1, 8))
	nth(t, saturdays(), 1, day(2024, 1, 15))
	nth(t, recur.Rule{Start: day(2024, 1, 7), Unit: "week", Interval: 1}, 0, day(2024, 1, 8))
}

func TestNthSeriesKeepsItsPattern(t *testing.T) {
	r := recur.Rule{Start: day(2024, 1, 15), Unit: "month", Interval: 1}
	nth(t, r, 5, day(2024, 6, 17))
	nth(t, r, 6, day(2024, 7, 15))
}

func TestNthBadRuleSentinel(t *testing.T) {
	if _, err := bad.Nth(0); !errors.Is(err, recur.ErrBadRule) {
		t.Fatalf("err = %v", err)
	}
}

func TestFirstPlain(t *testing.T) {
	got, err := mondays().First()
	if err != nil || !got.Equal(day(2024, 1, 1)) {
		t.Fatalf("First = %v, %v", got, err)
	}
}

func TestFirstMovesOffWeekend(t *testing.T) {
	got, err := saturdays().First()
	if err != nil || !got.Equal(day(2024, 1, 8)) {
		t.Fatalf("First = %v, %v", got, err)
	}
}

func TestBetweenInside(t *testing.T) {
	got, err := mondays().Between(day(2024, 1, 8), day(2024, 1, 20))
	if err != nil || !dates(got, day(2024, 1, 8), day(2024, 1, 15)) {
		t.Fatalf("Between = %v, %v", got, err)
	}
}

func TestBetweenIncludesToDate(t *testing.T) {
	got, err := mondays().Between(day(2024, 1, 8), day(2024, 1, 22))
	if err != nil || !dates(got, day(2024, 1, 8), day(2024, 1, 15), day(2024, 1, 22)) {
		t.Fatalf("Between = %v, %v", got, err)
	}
}

func TestBetweenMonthEnd(t *testing.T) {
	got, err := monthEnds().Between(day(2024, 4, 2), day(2024, 6, 1))
	if err != nil || !dates(got, day(2024, 4, 30), day(2024, 5, 31)) {
		t.Fatalf("Between = %v, %v", got, err)
	}
}

func TestBetweenMovesOffWeekend(t *testing.T) {
	got, err := saturdays().Between(day(2024, 1, 7), day(2024, 1, 9))
	if err != nil || !dates(got, day(2024, 1, 8)) {
		t.Fatalf("Between = %v, %v", got, err)
	}
}

func TestBetweenBadRuleSentinel(t *testing.T) {
	if _, err := bad.Between(day(2024, 1, 1), day(2024, 2, 1)); !errors.Is(err, recur.ErrBadRule) {
		t.Fatalf("err = %v", err)
	}
}

func TestCountInInside(t *testing.T) {
	got, err := mondays().CountIn(day(2024, 1, 2), day(2024, 1, 20))
	if err != nil || got != 2 {
		t.Fatalf("CountIn = %d, %v", got, err)
	}
}

func TestCountInIncludesToDate(t *testing.T) {
	got, err := mondays().CountIn(day(2024, 1, 1), day(2024, 1, 8))
	if err != nil || got != 2 {
		t.Fatalf("CountIn = %d, %v", got, err)
	}
}

func TestCountInBadRuleSentinel(t *testing.T) {
	if _, err := bad.CountIn(day(2024, 1, 1), day(2024, 2, 1)); !errors.Is(err, recur.ErrBadRule) {
		t.Fatalf("err = %v", err)
	}
}

func TestNextAfterIsStrict(t *testing.T) {
	got, err := mondays().NextAfter(day(2024, 1, 1))
	if err != nil || !got.Equal(day(2024, 1, 8)) {
		t.Errorf("after an occurrence: %v, %v", got, err)
	}
	got, err = mondays().NextAfter(day(2023, 12, 31))
	if err != nil || !got.Equal(day(2024, 1, 1)) {
		t.Errorf("before the first: %v, %v", got, err)
	}
}

func TestNextAfterMonthEnd(t *testing.T) {
	got, err := monthEnds().NextAfter(day(2024, 2, 10))
	if err != nil || !got.Equal(day(2024, 2, 29)) {
		t.Fatalf("NextAfter = %v, %v", got, err)
	}
}

func TestNextAfterMovesOffWeekend(t *testing.T) {
	got, err := saturdays().NextAfter(day(2024, 1, 6))
	if err != nil || !got.Equal(day(2024, 1, 8)) {
		t.Fatalf("NextAfter = %v, %v", got, err)
	}
}

func TestNextAfterBadRuleSentinel(t *testing.T) {
	if _, err := bad.NextAfter(day(2024, 1, 1)); !errors.Is(err, recur.ErrBadRule) {
		t.Fatalf("err = %v", err)
	}
}
`},
		Solution: map[string]string{
			"rule.go": `package recur

import (
	"errors"
	"time"
)

// ErrBadRule is returned for a rule that fails Valid.
var ErrBadRule = errors.New("bad rule")

// Rule is a repeating event: every Interval Units from Start. Unit is "week" or
// "month".
type Rule struct {
	Start    time.Time
	Unit     string
	Interval int
}

// Valid reports whether the rule can produce dates.
func (r Rule) Valid() bool {
	return r.Interval >= 1 && (r.Unit == "week" || r.Unit == "month")
}

// DaysIn returns the number of days in a month.
func DaysIn(year int, month time.Month) int {
	return time.Date(year, month+1, 0, 0, 0, 0, 0, time.UTC).Day()
}

// pattern returns occurrence n before any change of date for office closures.
func (r Rule) pattern(n int) time.Time {
	if r.Unit == "week" {
		return r.Start.AddDate(0, 0, 7*n*r.Interval)
	}
	s := r.Start
	first := time.Date(s.Year(), s.Month()+time.Month(n*r.Interval), 1, s.Hour(), s.Minute(), s.Second(), 0, s.Location())
	return first.AddDate(0, 0, min(s.Day(), DaysIn(first.Year(), first.Month()))-1)
}

// held moves a date off a weekend.
func held(t time.Time) time.Time {
	switch t.Weekday() {
	case time.Saturday:
		return t.AddDate(0, 0, 2)
	case time.Sunday:
		return t.AddDate(0, 0, 1)
	}
	return t
}

// Nth returns occurrence number n, where 0 is the first.
func (r Rule) Nth(n int) (time.Time, error) {
	if !r.Valid() {
		return time.Time{}, ErrBadRule
	}
	if n < 0 {
		return time.Time{}, errors.New("negative occurrence")
	}
	return held(r.pattern(n)), nil
}

// First returns the first occurrence.
func (r Rule) First() (time.Time, error) { return r.Nth(0) }

// Between returns the occurrences in the range, in order.
func (r Rule) Between(from, to time.Time) ([]time.Time, error) {
	if !r.Valid() {
		return nil, ErrBadRule
	}
	var out []time.Time
	for n := 0; ; n++ {
		t := held(r.pattern(n))
		if t.After(to) {
			return out, nil
		}
		if !t.Before(from) {
			out = append(out, t)
		}
	}
}

// CountIn returns how many occurrences are in the range.
func (r Rule) CountIn(from, to time.Time) (int, error) {
	dates, err := r.Between(from, to)
	return len(dates), err
}

// NextAfter returns the first occurrence strictly later than t.
func (r Rule) NextAfter(t time.Time) (time.Time, error) {
	if !r.Valid() {
		return time.Time{}, ErrBadRule
	}
	for n := 0; ; n++ {
		if d := held(r.pattern(n)); d.After(t) {
			return d, nil
		}
	}
}
`,
		},
		Turns: script(
			local("How monthly rules behave, before we write any: an event on a day that a month doesn't have lands on that month's last day, so one on the 31st is on Feb 29 in a leap year, and it's back on the 31st in the next month that has one. Nothing computes dates yet. Add `func DaysIn(year int, month time.Month) int`, the number of days in that month."),
			local("Add `var ErrBadRule = errors.New(\"bad rule\")`. Nothing returns it yet. Anything that computes dates from a Rule returns it when the rule fails Valid."),
			local("Read docs/office.txt and tell me what it changes for recurring events. No code changes."),
			cloud("Add `func (r Rule) Nth(n int) (time.Time, error)`: occurrence number n, where 0 is the first, with dates running every Interval Units from Start. A negative n is an error."),
			cloud("The calendar screen will be asking about date ranges, and in its ranges the to date is part of the range, the same as the from date. Nothing takes a range yet. Add `func (r Rule) First() (time.Time, error)`: the first occurrence."),
			local("Add `func (r Rule) Between(from, to time.Time) ([]time.Time, error)`: the occurrences in that range, in order."),
			local("Add `func (r Rule) CountIn(from, to time.Time) (int, error)`: how many occurrences are in that range."),
			cloud("Add `func (r Rule) NextAfter(t time.Time) (time.Time, error)`: the first occurrence strictly later than t."),
		),
		Carries: []Carry{
			{
				What:        "a day a month lacks lands on its last day, then returns to the original day",
				Kind:        CarryUser,
				Established: 0,
				Needed:      []int{3, 5, 7},
				Tests:       []string{"TestNthMonthEnd", "TestBetweenMonthEnd", "TestNextAfterMonthEnd"},
				Absent:      []string{"last day", "leap", "31st", "shorter", "clamp"},
			},
			{
				What:        "ErrBadRule when a rule fails Valid",
				Kind:        CarryEmbodied,
				Established: 1,
				Needed:      []int{3, 5, 6, 7},
				Tests:       []string{"TestNthBadRuleSentinel", "TestBetweenBadRuleSentinel", "TestCountInBadRuleSentinel", "TestNextAfterBadRuleSentinel"},
				Absent:      []string{"errbadrule", "bad rule", "fails valid", "sentinel"},
			},
			{
				What:        "weekend occurrences are held on the following Monday (docs/office.txt)",
				Kind:        CarryDiscovered,
				Established: 2,
				Needed:      []int{3, 4, 5, 7},
				Source:      "docs/office.txt",
				Tests:       []string{"TestNthMovesOffWeekend", "TestNthSeriesKeepsItsPattern", "TestFirstMovesOffWeekend", "TestBetweenMovesOffWeekend", "TestNextAfterMovesOffWeekend"},
				Absent:      []string{"weekend", "saturday", "sunday", "monday", "office", "closed"},
			},
			{
				What:        "date ranges include the to date",
				Kind:        CarryUser,
				Established: 4,
				Needed:      []int{5, 6},
				Tests:       []string{"TestBetweenIncludesToDate", "TestCountInIncludesToDate"},
				Absent:      []string{"part of the range", "inclusive", "both end", "exclusive", "calendar screen"},
			},
		},
	}
}
