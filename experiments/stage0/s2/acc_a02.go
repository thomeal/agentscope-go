package s2

func accA02() Task {
	return Task{
		ID:     "s2-a02-payroll-hours",
		Set:    "acceptance",
		System: system("a payroll hours calculator"),
		Files: map[string]string{
			"go.mod": goMod,
			"sheet.go": `package payroll

// Shift is one stretch of work. Start and End are minutes after midnight, as
// the time clock writes them.
type Shift struct {
	Worker string
	Week   int
	Start  int
	End    int
}

// Sheet holds the shifts recorded so far.
type Sheet struct {
	shifts []Shift
}

// Add records a shift.
func (s *Sheet) Add(sh Shift) { s.shifts = append(s.shifts, sh) }
`,
			"docs/timeclock.txt": `Time clock export notes

The clock records only the time of day. A shift that runs past midnight therefore
has an End smaller than its Start, and it finished on the following day.

Clocks are synced weekly. A badge swipe within a minute of the last one is dropped
by the clock itself and never reaches us.
`,
			"sheet_test.go": `package payroll

import "testing"

func TestAdd(t *testing.T) {
	var s Sheet
	s.Add(Shift{Worker: "ann", Week: 1, Start: 540, End: 600})
	if len(s.shifts) != 1 || s.shifts[0].Worker != "ann" {
		t.Fatalf("shifts = %+v", s.shifts)
	}
}
`,
		},
		Hidden: map[string]string{hiddenName: `package payroll_test

import (
	"errors"
	"testing"

	payroll "fixture"
)

// sheet holds n ten-hour day shifts for ann in week 1.
func sheet(n int) *payroll.Sheet {
	s := &payroll.Sheet{}
	for range n {
		s.Add(payroll.Shift{Worker: "ann", Week: 1, Start: 480, End: 1080})
	}
	return s
}

func TestCountShifts(t *testing.T) {
	if got := sheet(3).Count(); got != 3 {
		t.Fatalf("Count = %d", got)
	}
}

func TestMinutesSameDay(t *testing.T) {
	if got := (payroll.Shift{Start: 540, End: 600}).Minutes(); got != 60 {
		t.Fatalf("Minutes = %d", got)
	}
}

func TestMinutesPastMidnight(t *testing.T) {
	if got := (payroll.Shift{Start: 1380, End: 60}).Minutes(); got != 120 {
		t.Fatalf("Minutes = %d", got)
	}
}

func TestWeekMinutesPastMidnight(t *testing.T) {
	s := &payroll.Sheet{}
	s.Add(payroll.Shift{Worker: "ann", Week: 1, Start: 1380, End: 60})
	s.Add(payroll.Shift{Worker: "ann", Week: 1, Start: 540, End: 600})
	got, err := s.WeekMinutes("ann", 1)
	if err != nil {
		t.Fatal(err)
	}
	if got != 180 {
		t.Fatalf("WeekMinutes = %d", got)
	}
}

func TestWeekMinutesFilters(t *testing.T) {
	s := sheet(2)
	s.Add(payroll.Shift{Worker: "bob", Week: 1, Start: 0, End: 100})
	s.Add(payroll.Shift{Worker: "ann", Week: 2, Start: 0, End: 100})
	got, err := s.WeekMinutes("ann", 1)
	if err != nil {
		t.Fatal(err)
	}
	if got != 1200 {
		t.Fatalf("WeekMinutes = %d", got)
	}
}

func TestWeekMinutesNoShifts(t *testing.T) {
	s := sheet(1)
	if _, err := s.WeekMinutes("bob", 1); !errors.Is(err, payroll.ErrNoShifts) {
		t.Errorf("unknown worker: %v", err)
	}
	if _, err := s.WeekMinutes("ann", 2); !errors.Is(err, payroll.ErrNoShifts) {
		t.Errorf("empty week: %v", err)
	}
}

func TestOvertimeNoShifts(t *testing.T) {
	if _, err := sheet(1).Overtime("bob", 1); !errors.Is(err, payroll.ErrNoShifts) {
		t.Fatalf("err = %v", err)
	}
}

func TestWeekPayNoShifts(t *testing.T) {
	if _, err := sheet(1).WeekPay("bob", 1, 1000); !errors.Is(err, payroll.ErrNoShifts) {
		t.Fatalf("err = %v", err)
	}
}

func TestPayWholeAmounts(t *testing.T) {
	for _, c := range []struct{ minutes, rate, want int }{{120, 1500, 3000}, {90, 2000, 3000}, {0, 1000, 0}} {
		if got := payroll.Pay(c.minutes, c.rate); got != c.want {
			t.Errorf("Pay(%d, %d) = %d, want %d", c.minutes, c.rate, got, c.want)
		}
	}
}

func TestPayRoundsUp(t *testing.T) {
	if got := payroll.Pay(61, 1000); got != 1017 {
		t.Errorf("Pay(61, 1000) = %d", got)
	}
	if got := payroll.Pay(1, 1); got != 1 {
		t.Errorf("Pay(1, 1) = %d", got)
	}
}

func TestWeekPayRoundsUp(t *testing.T) {
	s := &payroll.Sheet{}
	s.Add(payroll.Shift{Worker: "ann", Week: 1, Start: 0, End: 61})
	got, err := s.WeekPay("ann", 1, 1000)
	if err != nil {
		t.Fatal(err)
	}
	if got != 1017 {
		t.Fatalf("WeekPay = %d", got)
	}
}

func TestOvertimeNoneInShortWeek(t *testing.T) {
	got, err := sheet(1).Overtime("ann", 1)
	if err != nil {
		t.Fatal(err)
	}
	if got != 0 {
		t.Fatalf("Overtime = %d", got)
	}
}

func TestOvertimeThreshold(t *testing.T) {
	got, err := sheet(4).Overtime("ann", 1)
	if err != nil {
		t.Fatal(err)
	}
	if got != 120 {
		t.Fatalf("40 hours gave %d overtime minutes, want 120", got)
	}
}

func TestWeekPayOvertimeRate(t *testing.T) {
	got, err := sheet(4).WeekPay("ann", 1, 1000)
	if err != nil {
		t.Fatal(err)
	}
	if got != 40500 {
		t.Fatalf("WeekPay = %d, want 40500", got)
	}
}

func TestWeekPayShortWeek(t *testing.T) {
	got, err := sheet(1).WeekPay("ann", 1, 1000)
	if err != nil {
		t.Fatal(err)
	}
	if got != 10000 {
		t.Fatalf("WeekPay = %d", got)
	}
}
`},
		Solution: map[string]string{
			"sheet.go": `package payroll

// Shift is one stretch of work. Start and End are minutes after midnight, as
// the time clock writes them.
type Shift struct {
	Worker string
	Week   int
	Start  int
	End    int
}

// Sheet holds the shifts recorded so far.
type Sheet struct {
	shifts []Shift
}

// Add records a shift.
func (s *Sheet) Add(sh Shift) { s.shifts = append(s.shifts, sh) }

// Count returns the number of shifts held.
func (s *Sheet) Count() int { return len(s.shifts) }
`,
			"hours.go": `package payroll

import "errors"

// ErrNoShifts is for any call about a worker's week with no shifts in it.
var ErrNoShifts = errors.New("no shifts")

const dayMinutes = 24 * 60

// Minutes returns how many minutes the shift lasted.
func (sh Shift) Minutes() int {
	if sh.End < sh.Start {
		return sh.End + dayMinutes - sh.Start
	}
	return sh.End - sh.Start
}

// WeekMinutes returns the total minutes a worker did in a week.
func (s *Sheet) WeekMinutes(worker string, week int) (int, error) {
	total, found := 0, false
	for _, sh := range s.shifts {
		if sh.Worker == worker && sh.Week == week {
			total += sh.Minutes()
			found = true
		}
	}
	if !found {
		return 0, ErrNoShifts
	}
	return total, nil
}

// Overtime returns the overtime minutes a worker did in a week.
func (s *Sheet) Overtime(worker string, week int) (int, error) {
	total, err := s.WeekMinutes(worker, week)
	if err != nil {
		return 0, err
	}
	return max(total-overtimeAfter, 0), nil
}
`,
			"pay.go": `package payroll

const (
	overtimeAfter = 38 * 60
	overtimeNum   = 5
	overtimeDen   = 4
)

// Pay returns the money in cents for minutes at an hourly rate.
func Pay(minutes int, centsPerHour int) int {
	return (minutes*centsPerHour + 59) / 60
}

// WeekPay returns what a worker is owed for a week, overtime included.
func (s *Sheet) WeekPay(worker string, week int, centsPerHour int) (int, error) {
	total, err := s.WeekMinutes(worker, week)
	if err != nil {
		return 0, err
	}
	over := max(total-overtimeAfter, 0)
	return Pay(total-over, centsPerHour) + Pay(over, centsPerHour*overtimeNum/overtimeDen), nil
}
`,
		},
		Turns: script(
			cloud("Finance signed off the pay rule today: a fraction of a cent always rounds up, in the worker's favour. Nothing handles money yet. For now add `func (s *Sheet) Count() int`, the number of shifts held."),
			cloud("Read docs/timeclock.txt and tell me what it means for how we read a Shift. No code changes."),
			local("Add `func (sh Shift) Minutes() int` in a new file hours.go: how many minutes the shift lasted."),
			local("In hours.go add `var ErrNoShifts = errors.New(\"no shifts\")`. Nothing returns it yet. Any call that asks about a worker's week when they have no shifts in that week should give back this error."),
			local("Add `func Pay(minutes int, centsPerHour int) int` in a new file pay.go: the money in cents for that many minutes at that hourly rate."),
			cloud("HR sent the award terms: overtime starts after 38 hours in a week and pays time and a quarter. We'll get to overtime shortly. First add `func (s *Sheet) WeekMinutes(worker string, week int) (int, error)`: the total minutes that worker did in that week."),
			local("Add `func (s *Sheet) Overtime(worker string, week int) (int, error)`: how many overtime minutes that worker did in that week."),
			cloud("Add `func (s *Sheet) WeekPay(worker string, week int, centsPerHour int) (int, error)`: what that worker is owed for that week at that hourly rate, overtime included."),
		),
		Carries: []Carry{
			{
				What:        "a fraction of a cent rounds up",
				Kind:        CarryUser,
				Established: 0,
				Needed:      []int{4, 7},
				Tests:       []string{"TestPayRoundsUp", "TestWeekPayRoundsUp"},
				Absent:      []string{"round", "fraction", "favour"},
			},
			{
				What:        "an End smaller than Start means the shift ended the next day (docs/timeclock.txt)",
				Kind:        CarryDiscovered,
				Established: 1,
				Needed:      []int{2, 5},
				Source:      "docs/timeclock.txt",
				Tests:       []string{"TestMinutesPastMidnight", "TestWeekMinutesPastMidnight"},
				Absent:      []string{"midnight", "following day", "next day"},
			},
			{
				What:        "ErrNoShifts for a worker's week with no shifts",
				Kind:        CarryEmbodied,
				Established: 3,
				Needed:      []int{5, 6, 7},
				Tests:       []string{"TestWeekMinutesNoShifts", "TestOvertimeNoShifts", "TestWeekPayNoShifts"},
				Absent:      []string{"errnoshifts", "no shifts"},
			},
			{
				What:        "overtime starts after 38 hours and pays time and a quarter",
				Kind:        CarryUser,
				Established: 5,
				Needed:      []int{6, 7},
				Tests:       []string{"TestOvertimeThreshold", "TestWeekPayOvertimeRate"},
				Absent:      []string{"38", "quarter", "1.25"},
			},
		},
	}
}
