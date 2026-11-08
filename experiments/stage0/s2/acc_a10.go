package s2

func accA10() Task {
	return Task{
		ID:     "s2-a10-parcel-lockers",
		Set:    "acceptance",
		System: system("a parcel locker bank"),
		Files: map[string]string{
			"go.mod": goMod,
			"locker.go": `package lockers

// Size is a locker or parcel size.
type Size int

// The sizes, in order.
const (
	Small Size = iota
	Medium
	Large
)

// Locker is one door in a bank. Parcel is empty while the door is free.
type Locker struct {
	ID     string
	Size   Size
	Parcel string
}

// Bank is a wall of lockers.
type Bank struct {
	lockers []Locker
}

// NewBank builds a bank from its lockers, in wall order.
func NewBank(lockers ...Locker) *Bank { return &Bank{lockers: lockers} }

// Occupied returns the number of lockers holding a parcel.
func (b *Bank) Occupied() int {
	n := 0
	for _, l := range b.lockers {
		if l.Parcel != "" {
			n++
		}
	}
	return n
}
`,
			"docs/storage.txt": `Storage terms

A parcel is stored free for the first 48 hours. After that, each started 24 hours
costs 200 cents.

A parcel still there after 7 days goes back to the sender on the next courier run.
`,
			"locker_test.go": `package lockers

import "testing"

func TestOccupied(t *testing.T) {
	b := NewBank(Locker{ID: "A", Parcel: "p"}, Locker{ID: "B"})
	if got := b.Occupied(); got != 1 {
		t.Fatalf("Occupied = %d", got)
	}
}
`,
		},
		Hidden: map[string]string{hiddenName: `package lockers_test

import (
	"errors"
	"reflect"
	"testing"

	lockers "fixture"
)

const hour = 60

func wall() *lockers.Bank {
	return lockers.NewBank(
		lockers.Locker{ID: "L1", Size: lockers.Large},
		lockers.Locker{ID: "M1", Size: lockers.Medium},
		lockers.Locker{ID: "S1", Size: lockers.Small},
	)
}

func deposit(t *testing.T, b *lockers.Bank, parcel string, size lockers.Size, now int) lockers.Slip {
	t.Helper()
	s, err := b.Deposit(parcel, size, now)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestFreeCount(t *testing.T) {
	b := wall()
	if got := b.Free(); got != 3 {
		t.Fatalf("Free = %d", got)
	}
	deposit(t, b, "p1", lockers.Small, 0)
	if got := b.Free(); got != 2 {
		t.Fatalf("Free = %d", got)
	}
}

func TestDepositOnlyWhereItFits(t *testing.T) {
	b := lockers.NewBank(lockers.Locker{ID: "S1", Size: lockers.Small}, lockers.Locker{ID: "L1", Size: lockers.Large})
	if got := deposit(t, b, "p1", lockers.Large, 0).Locker; got != "L1" {
		t.Fatalf("Locker = %q", got)
	}
}

func TestDepositSmallestFree(t *testing.T) {
	b := wall()
	if got := deposit(t, b, "p1", lockers.Small, 0).Locker; got != "S1" {
		t.Fatalf("Locker = %q", got)
	}
}

func TestDepositNextSizeUp(t *testing.T) {
	b := lockers.NewBank(
		lockers.Locker{ID: "L1", Size: lockers.Large},
		lockers.Locker{ID: "S1", Size: lockers.Small},
		lockers.Locker{ID: "M1", Size: lockers.Medium},
	)
	deposit(t, b, "p1", lockers.Small, 0)
	if got := deposit(t, b, "p2", lockers.Small, 0).Locker; got != "M1" {
		t.Fatalf("Locker = %q", got)
	}
}

func TestDepositNothingFitsSentinel(t *testing.T) {
	b := lockers.NewBank(lockers.Locker{ID: "S1", Size: lockers.Small})
	if _, err := b.Deposit("p1", lockers.Large, 0); !errors.Is(err, lockers.ErrNoLocker) {
		t.Fatalf("err = %v", err)
	}
}

func TestCodeHasSixDigits(t *testing.T) {
	b := wall()
	for _, parcel := range []string{"p1", "p2", "p3"} {
		code := deposit(t, b, parcel, lockers.Small, 0).Code
		if len(code) != 6 {
			t.Errorf("code %q", code)
		}
		for _, r := range code {
			if r < '0' || r > '9' {
				t.Errorf("code %q", code)
			}
		}
	}
}

func TestCodesDistinct(t *testing.T) {
	b := wall()
	if deposit(t, b, "p1", lockers.Small, 0).Code == deposit(t, b, "p2", lockers.Small, 0).Code {
		t.Fatal("two parcels share a code")
	}
}

func TestFeeFreePeriod(t *testing.T) {
	for _, stored := range []int{0, 60, 48 * hour} {
		if got := lockers.Fee(stored); got != 0 {
			t.Errorf("Fee(%d) = %d", stored, got)
		}
	}
}

func TestFeeStartedDays(t *testing.T) {
	for stored, want := range map[int]int{48*hour + 1: 200, 72 * hour: 200, 72*hour + 1: 400} {
		if got := lockers.Fee(stored); got != want {
			t.Errorf("Fee(%d) = %d, want %d", stored, got, want)
		}
	}
}

func TestPickupReleases(t *testing.T) {
	b := wall()
	s := deposit(t, b, "p1", lockers.Small, 100)
	fee, err := b.Pickup(s.Code, 101)
	if err != nil {
		t.Fatal(err)
	}
	if fee != 0 || b.Occupied() != 0 {
		t.Fatalf("fee %d, Occupied %d", fee, b.Occupied())
	}
}

func TestPickupCharges(t *testing.T) {
	b := wall()
	s := deposit(t, b, "p1", lockers.Small, 100)
	fee, err := b.Pickup(s.Code, 100+72*hour)
	if err != nil {
		t.Fatal(err)
	}
	if fee != 200 {
		t.Fatalf("fee = %d", fee)
	}
}

func TestPickupBadCode(t *testing.T) {
	b := wall()
	s := deposit(t, b, "p1", lockers.Small, 100)
	if _, err := b.Pickup("nope", 101); !errors.Is(err, lockers.ErrBadCode) {
		t.Errorf("wrong code: %v", err)
	}
	if b.Occupied() != 1 {
		t.Errorf("Occupied = %d", b.Occupied())
	}
	if _, err := b.Pickup(s.Code, 101); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Pickup(s.Code, 102); !errors.Is(err, lockers.ErrBadCode) {
		t.Errorf("used code: %v", err)
	}
}

func TestOverdueNotYet(t *testing.T) {
	b := wall()
	deposit(t, b, "p1", lockers.Small, 0)
	if got := b.Overdue(7*24*hour - 1); len(got) != 0 {
		t.Fatalf("Overdue = %v", got)
	}
}

func TestOverduePastLimit(t *testing.T) {
	b := wall()
	deposit(t, b, "p1", lockers.Small, 0)
	if got := b.Overdue(7*24*hour + 1); !reflect.DeepEqual(got, []string{"p1"}) {
		t.Fatalf("Overdue = %v", got)
	}
}

func TestOverdueWallOrder(t *testing.T) {
	b := lockers.NewBank(lockers.Locker{ID: "S1", Size: lockers.Small}, lockers.Locker{ID: "S2", Size: lockers.Small})
	deposit(t, b, "p1", lockers.Small, 0)
	deposit(t, b, "p2", lockers.Small, 1)
	if got := b.Overdue(1000 * 24 * hour); !reflect.DeepEqual(got, []string{"p1", "p2"}) {
		t.Fatalf("Overdue = %v", got)
	}
}

func TestPeekLeavesParcel(t *testing.T) {
	b := wall()
	s := deposit(t, b, "p1", lockers.Small, 100)
	got, err := b.Peek(s.Code)
	if err != nil {
		t.Fatal(err)
	}
	if got != "p1" || b.Occupied() != 1 {
		t.Fatalf("Peek = %q, Occupied %d", got, b.Occupied())
	}
}

func TestPeekBadCodeSentinel(t *testing.T) {
	if _, err := wall().Peek("nope"); !errors.Is(err, lockers.ErrBadCode) {
		t.Fatalf("err = %v", err)
	}
}
`},
		Solution: map[string]string{
			"locker.go": `package lockers

import (
	"errors"
	"fmt"
)

// ErrNoLocker is for when nothing free fits a parcel.
var ErrNoLocker = errors.New("no locker")

// ErrBadCode refuses a code that doesn't match a parcel in the bank.
var ErrBadCode = errors.New("bad code")

// Size is a locker or parcel size.
type Size int

// The sizes, in order.
const (
	Small Size = iota
	Medium
	Large
)

// Locker is one door in a bank. Parcel is empty while the door is free.
type Locker struct {
	ID     string
	Size   Size
	Parcel string
	code   string
	since  int
}

// Slip is what the person dropping a parcel off takes away.
type Slip struct {
	Locker string
	Code   string
}

// Bank is a wall of lockers.
type Bank struct {
	lockers []Locker
	issued  int
}

// NewBank builds a bank from its lockers, in wall order.
func NewBank(lockers ...Locker) *Bank { return &Bank{lockers: lockers} }

// Occupied returns the number of lockers holding a parcel.
func (b *Bank) Occupied() int {
	n := 0
	for _, l := range b.lockers {
		if l.Parcel != "" {
			n++
		}
	}
	return n
}

// Free returns the number of empty lockers.
func (b *Bank) Free() int { return len(b.lockers) - b.Occupied() }

// Deposit stores a parcel. now is minutes since the bank was installed.
func (b *Bank) Deposit(parcel string, size Size, now int) (Slip, error) {
	best := -1
	for i, l := range b.lockers {
		if l.Parcel == "" && l.Size >= size && (best < 0 || l.Size < b.lockers[best].Size) {
			best = i
		}
	}
	if best < 0 {
		return Slip{}, ErrNoLocker
	}
	b.issued++
	l := &b.lockers[best]
	l.Parcel, l.code, l.since = parcel, fmt.Sprintf("%06d", b.issued), now
	return Slip{Locker: l.ID, Code: l.code}, nil
}

func (b *Bank) find(code string) int {
	for i, l := range b.lockers {
		if l.Parcel != "" && l.code == code {
			return i
		}
	}
	return -1
}

// Pickup hands a parcel over and returns the charge.
func (b *Bank) Pickup(code string, now int) (int, error) {
	i := b.find(code)
	if i < 0 {
		return 0, ErrBadCode
	}
	fee := Fee(now - b.lockers[i].since)
	b.lockers[i].Parcel = ""
	return fee, nil
}

// Overdue returns the parcels due back to the sender, in wall order.
func (b *Bank) Overdue(now int) []string {
	var out []string
	for _, l := range b.lockers {
		if l.Parcel != "" && now-l.since > overdueAfter {
			out = append(out, l.Parcel)
		}
	}
	return out
}

// Peek returns the parcel behind a code, leaving it where it is.
func (b *Bank) Peek(code string) (string, error) {
	i := b.find(code)
	if i < 0 {
		return "", ErrBadCode
	}
	return b.lockers[i].Parcel, nil
}
`,
			"fees.go": `package lockers

const (
	hour         = 60
	freeFor      = 48 * hour
	block        = 24 * hour
	overdueAfter = 7 * 24 * hour
	feePerBlock  = 200
)

// Fee returns the charge in cents for a parcel stored that long.
func Fee(stored int) int {
	over := stored - freeFor
	if over <= 0 {
		return 0
	}
	return (over + block - 1) / block * feePerBlock
}
`,
		},
		Turns: script(
			cloud("Operations wants big lockers kept free for big parcels, so a parcel always goes into the smallest free locker it fits in, not just the first one on the wall. Nothing assigns lockers yet. Add `func (b *Bank) Free() int`, the number of empty lockers."),
			cloud("Pickup codes get read off a slip and typed at a six-digit keypad, so a code is always exactly six digits, with leading zeros where needed. Nothing issues codes yet. Add `var ErrNoLocker = errors.New(\"no locker\")`, which nothing returns yet. It's for when nothing free fits a parcel."),
			local("Read docs/storage.txt and summarise what we charge. No code changes."),
			local("Add `type Slip struct { Locker string; Code string }` and `func (b *Bank) Deposit(parcel string, size Size, now int) (Slip, error)`: stores the parcel and returns a slip with the locker's ID and a new pickup code. A locker fits a parcel of its own size or smaller. now is minutes since the bank was installed, and anything else here that deals in time uses that same unit."),
			cloud("Add `func Fee(stored int) int` in a new file fees.go: the charge in cents for a parcel stored that long."),
			local("Add `var ErrBadCode = errors.New(\"bad code\")` and `func (b *Bank) Pickup(code string, now int) (int, error)`: hands the parcel over, empties the locker and returns the charge. A code that doesn't match a parcel in the bank gets ErrBadCode, which includes a code that's been used already."),
			cloud("Add `func (b *Bank) Overdue(now int) []string`: the parcels due to go back to the sender, in wall order."),
			cloud("Add `func (b *Bank) Peek(code string) (string, error)`: which parcel is waiting behind a code, leaving it where it is."),
		),
		Carries: []Carry{
			{
				What:        "a parcel goes into the smallest free locker it fits",
				Kind:        CarryUser,
				Established: 0,
				Needed:      []int{3},
				Tests:       []string{"TestDepositSmallestFree", "TestDepositNextSizeUp"},
				Absent:      []string{"smallest", "first one on the wall", "kept free"},
			},
			{
				What:        "pickup codes are exactly six digits with leading zeros",
				Kind:        CarryUser,
				Established: 1,
				Needed:      []int{3},
				Tests:       []string{"TestCodeHasSixDigits"},
				Absent:      []string{"six-digit", "six digits", "leading zero"},
			},
			{
				What:        "ErrNoLocker for when nothing free fits a parcel",
				Kind:        CarryEmbodied,
				Established: 1,
				Needed:      []int{3},
				Tests:       []string{"TestDepositNothingFitsSentinel"},
				Absent:      []string{"errnolocker", "no locker", "nothing free"},
			},
			{
				What:        "48 free hours, then 200 cents per started 24 hours, and back to the sender after 7 days (docs/storage.txt)",
				Kind:        CarryDiscovered,
				Established: 2,
				Needed:      []int{4, 5, 6},
				Source:      "docs/storage.txt",
				Tests:       []string{"TestFeeStartedDays", "TestPickupCharges", "TestOverduePastLimit"},
				Absent:      []string{"48 hours", "200 cents", "7 days", "24 hours"},
			},
			{
				What:        "time in this package is in minutes",
				Kind:        CarryUser,
				Established: 3,
				Needed:      []int{4, 5, 6},
				Tests:       []string{"TestFeeFreePeriod", "TestFeeStartedDays", "TestPickupCharges", "TestOverdueNotYet"},
				Absent:      []string{"minutes since", "same unit", "bank was installed"},
			},
			{
				What:        "ErrBadCode for a code that doesn't match a parcel in the bank",
				Kind:        CarryEmbodied,
				Established: 5,
				Needed:      []int{7},
				Tests:       []string{"TestPeekBadCodeSentinel"},
				Absent:      []string{"errbadcode", "bad code", "doesn't match"},
			},
		},
	}
}
