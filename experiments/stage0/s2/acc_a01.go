package s2

func accA01() Task {
	return Task{
		ID:     "s2-a01-library-loans",
		Set:    "acceptance",
		System: system("a library lending desk"),
		Files: map[string]string{
			"go.mod": goMod,
			"loan.go": `package library

// Loan is one book out with a member. DueDay is a day number.
type Loan struct {
	Member string
	ISBN   string
	DueDay int
}

// Desk holds the books currently out.
type Desk struct {
	loans []Loan
}

// Out returns the current loans.
func (d *Desk) Out() []Loan { return append([]Loan(nil), d.loans...) }
`,
			"docs/membership.txt": `Membership notes

A card number starting with J- is a junior card. Junior cards are never charged
a late fee, however late the book comes back.

Cards are renewed once a year at the front desk, and a lost card is replaced free.
`,
			"loan_test.go": `package library

import "testing"

func TestOutIsACopy(t *testing.T) {
	d := &Desk{loans: []Loan{{Member: "A1", ISBN: "x", DueDay: 5}}}
	out := d.Out()
	out[0].DueDay = 9
	if d.loans[0].DueDay != 5 {
		t.Fatal("Out exposes the desk's own slice")
	}
}
`,
		},
		Hidden: map[string]string{hiddenName: `package library_test

import (
	"errors"
	"testing"

	library "fixture"
)

func checkout(t *testing.T, d *library.Desk, member, isbn string, today int) library.Loan {
	t.Helper()
	l, err := d.Checkout(member, isbn, today)
	if err != nil {
		t.Fatal(err)
	}
	return l
}

func TestCountAfterCheckouts(t *testing.T) {
	d := &library.Desk{}
	if d.Count() != 0 {
		t.Fatalf("empty desk: Count = %d", d.Count())
	}
	checkout(t, d, "A1", "a", 1)
	checkout(t, d, "A1", "b", 1)
	if d.Count() != 2 {
		t.Fatalf("Count = %d", d.Count())
	}
}

func TestCheckoutRecords(t *testing.T) {
	d := &library.Desk{}
	l := checkout(t, d, "A1", "isbn-1", 100)
	if l.Member != "A1" || l.ISBN != "isbn-1" {
		t.Fatalf("Checkout returned %+v", l)
	}
	out := d.Out()
	if len(out) != 1 || out[0].ISBN != "isbn-1" || out[0].DueDay != l.DueDay {
		t.Fatalf("Out = %+v", out)
	}
}

func TestCheckoutAlreadyOut(t *testing.T) {
	d := &library.Desk{}
	checkout(t, d, "A1", "x", 100)
	if _, err := d.Checkout("B2", "x", 101); !errors.Is(err, library.ErrAlreadyOut) {
		t.Fatalf("err = %v", err)
	}
	if d.Count() != 1 {
		t.Fatalf("Count = %d", d.Count())
	}
}

func TestCheckoutRuns21Days(t *testing.T) {
	l := checkout(t, &library.Desk{}, "A1", "x", 100)
	if l.DueDay != 121 {
		t.Fatalf("DueDay = %d, want 121", l.DueDay)
	}
}

func TestRenewCountsFromDueDay(t *testing.T) {
	d := &library.Desk{}
	first := checkout(t, d, "A1", "x", 100)
	period := first.DueDay - 100
	renewed, err := d.Renew("x", 103)
	if err != nil {
		t.Fatal(err)
	}
	if renewed.DueDay != first.DueDay+period {
		t.Fatalf("due %d renewed on day 103 gave %d, want %d", first.DueDay, renewed.DueDay, first.DueDay+period)
	}
}

func TestRenewStored(t *testing.T) {
	d := &library.Desk{}
	first := checkout(t, d, "A1", "x", 100)
	renewed, err := d.Renew("x", 103)
	if err != nil {
		t.Fatal(err)
	}
	if renewed.DueDay <= first.DueDay {
		t.Fatalf("due day went from %d to %d", first.DueDay, renewed.DueDay)
	}
	if got := d.Out()[0].DueDay; got != renewed.DueDay {
		t.Fatalf("Out has %d, Renew returned %d", got, renewed.DueDay)
	}
}

func TestRenewOverdueRefused(t *testing.T) {
	d := &library.Desk{}
	first := checkout(t, d, "A1", "x", 100)
	if _, err := d.Renew("x", first.DueDay+1); !errors.Is(err, library.ErrOverdue) {
		t.Fatalf("err = %v", err)
	}
	if got := d.Out()[0].DueDay; got != first.DueDay {
		t.Fatalf("a refused renewal moved the due day to %d", got)
	}
}

func TestRenewMissingSentinel(t *testing.T) {
	if _, err := (&library.Desk{}).Renew("ghost", 1); !errors.Is(err, library.ErrNotOut) {
		t.Fatalf("err = %v", err)
	}
}

func TestReturnMissingSentinel(t *testing.T) {
	if _, err := (&library.Desk{}).Return("ghost", 1); !errors.Is(err, library.ErrNotOut) {
		t.Fatalf("err = %v", err)
	}
}

func TestTransferMissingSentinel(t *testing.T) {
	if err := (&library.Desk{}).Transfer("ghost", "B2"); !errors.Is(err, library.ErrNotOut) {
		t.Fatalf("err = %v", err)
	}
}

func TestLateFeeRate(t *testing.T) {
	l := library.Loan{Member: "A1", ISBN: "x", DueDay: 10}
	for returned, want := range map[int]int{5: 0, 10: 0, 11: 15, 13: 45} {
		if got := library.LateFee(l, returned); got != want {
			t.Errorf("returned on day %d: %d, want %d", returned, got, want)
		}
	}
}

func TestLateFeeCap(t *testing.T) {
	l := library.Loan{Member: "A1", ISBN: "x", DueDay: 10}
	if got := library.LateFee(l, 1000); got != 300 {
		t.Fatalf("fee = %d", got)
	}
}

func TestLateFeeJuniorCard(t *testing.T) {
	l := library.Loan{Member: "J-77", ISBN: "x", DueDay: 10}
	if got := library.LateFee(l, 40); got != 0 {
		t.Fatalf("fee = %d", got)
	}
}

func TestReturnJuniorCard(t *testing.T) {
	d := &library.Desk{}
	l := checkout(t, d, "J-77", "x", 100)
	fee, err := d.Return("x", l.DueDay+10)
	if err != nil {
		t.Fatal(err)
	}
	if fee != 0 {
		t.Fatalf("fee = %d", fee)
	}
}

func TestReturnChargesFee(t *testing.T) {
	d := &library.Desk{}
	l := checkout(t, d, "A1", "x", 100)
	fee, err := d.Return("x", l.DueDay+2)
	if err != nil {
		t.Fatal(err)
	}
	if fee != 30 || d.Count() != 0 {
		t.Fatalf("fee %d, Count %d", fee, d.Count())
	}
}

func TestTransferKeepsDueDay(t *testing.T) {
	d := &library.Desk{}
	l := checkout(t, d, "A1", "x", 100)
	if err := d.Transfer("x", "B2"); err != nil {
		t.Fatal(err)
	}
	out := d.Out()
	if len(out) != 1 || out[0].Member != "B2" || out[0].DueDay != l.DueDay {
		t.Fatalf("Out = %+v", out)
	}
}
`},
		Solution: map[string]string{
			"loan.go": `package library

import "errors"

// ErrNotOut is for any call that names a book the desk doesn't have out.
var ErrNotOut = errors.New("book not out")

// ErrAlreadyOut refuses a checkout of a book that is already out.
var ErrAlreadyOut = errors.New("already out")

// ErrOverdue refuses a renewal of an overdue book.
var ErrOverdue = errors.New("overdue")

// loanDays is how long a loan runs and what a renewal adds to its due day.
const loanDays = 21

// Loan is one book out with a member. DueDay is a day number.
type Loan struct {
	Member string
	ISBN   string
	DueDay int
}

// Desk holds the books currently out.
type Desk struct {
	loans []Loan
}

// Out returns the current loans.
func (d *Desk) Out() []Loan { return append([]Loan(nil), d.loans...) }

// Count returns the number of books out.
func (d *Desk) Count() int { return len(d.loans) }

func (d *Desk) find(isbn string) int {
	for i, l := range d.loans {
		if l.ISBN == isbn {
			return i
		}
	}
	return -1
}

// Checkout records a new loan and returns it.
func (d *Desk) Checkout(member, isbn string, today int) (Loan, error) {
	if d.find(isbn) >= 0 {
		return Loan{}, ErrAlreadyOut
	}
	l := Loan{Member: member, ISBN: isbn, DueDay: today + loanDays}
	d.loans = append(d.loans, l)
	return l, nil
}

// Return takes a book back and returns the late fee in cents.
func (d *Desk) Return(isbn string, today int) (int, error) {
	i := d.find(isbn)
	if i < 0 {
		return 0, ErrNotOut
	}
	fee := LateFee(d.loans[i], today)
	d.loans = append(d.loans[:i], d.loans[i+1:]...)
	return fee, nil
}

// Renew extends a loan and returns it.
func (d *Desk) Renew(isbn string, today int) (Loan, error) {
	i := d.find(isbn)
	if i < 0 {
		return Loan{}, ErrNotOut
	}
	if today > d.loans[i].DueDay {
		return Loan{}, ErrOverdue
	}
	d.loans[i].DueDay += loanDays
	return d.loans[i], nil
}

// Transfer moves a loan onto another card.
func (d *Desk) Transfer(isbn, member string) error {
	i := d.find(isbn)
	if i < 0 {
		return ErrNotOut
	}
	d.loans[i].Member = member
	return nil
}
`,
			"fees.go": `package library

import "strings"

const (
	feePerDay = 15
	maxFee    = 300
)

// LateFee returns the fee in cents when the book comes back on day returned.
func LateFee(l Loan, returned int) int {
	if strings.HasPrefix(l.Member, "J-") {
		return 0
	}
	return min(max(returned-l.DueDay, 0)*feePerDay, maxFee)
}
`,
		},
		Turns: script(
			local("How lending works here, since people get it wrong: a loan runs 21 days, and a renewal adds another 21 to the book's current due day, not to the day the member walks in, so renewing early never costs anyone days. No lending code exists yet. For now add `func (d *Desk) Count() int`, the number of books out."),
			cloud("Add `var ErrNotOut = errors.New(\"book not out\")` to loan.go. Nothing returns it yet, but it's the error for any call that names a book the desk doesn't have out."),
			local("Read docs/membership.txt and tell me what in it touches money. No code changes."),
			cloud("Add `func (d *Desk) Checkout(member, isbn string, today int) (Loan, error)`: it records a new loan for the book and returns it. If that ISBN is already out, return `ErrAlreadyOut`, declared as `var ErrAlreadyOut = errors.New(\"already out\")`."),
			cloud("Add `func LateFee(l Loan, returned int) int` in a new file fees.go: the fee in cents when the book comes back on day returned. It's 15 cents for each day after the due day, and never more than 300 cents."),
			local("Add `func (d *Desk) Return(isbn string, today int) (int, error)`: takes the book back and returns the late fee in cents."),
			cloud("Add `func (d *Desk) Renew(isbn string, today int) (Loan, error)`: renews the book and returns the updated loan. A book that's already overdue can't be renewed: if today is after its due day, return `ErrOverdue`, declared as `var ErrOverdue = errors.New(\"overdue\")`."),
			local("Add `func (d *Desk) Transfer(isbn, member string) error`, for when a family moves a book onto another card. The due day doesn't change."),
		),
		Carries: []Carry{
			{
				What:        "a loan runs 21 days",
				Kind:        CarryUser,
				Established: 0,
				Needed:      []int{3},
				Tests:       []string{"TestCheckoutRuns21Days"},
				Absent:      []string{"21", "three weeks"},
			},
			{
				What:        "a renewal counts from the current due day, not from today",
				Kind:        CarryUser,
				Established: 0,
				Needed:      []int{6},
				Tests:       []string{"TestRenewCountsFromDueDay"},
				Absent:      []string{"current due day", "walks in", "renewing early"},
			},
			{
				What:        "ErrNotOut for any call naming a book that isn't out",
				Kind:        CarryEmbodied,
				Established: 1,
				Needed:      []int{5, 6, 7},
				Tests:       []string{"TestRenewMissingSentinel", "TestReturnMissingSentinel", "TestTransferMissingSentinel"},
				Absent:      []string{"errnotout", "book not out", "doesn't have out"},
			},
			{
				What:        "J- cards are never charged a late fee (docs/membership.txt)",
				Kind:        CarryDiscovered,
				Established: 2,
				Needed:      []int{4, 5},
				Source:      "docs/membership.txt",
				Tests:       []string{"TestLateFeeJuniorCard", "TestReturnJuniorCard"},
				Absent:      []string{"junior", "j-", "never charged"},
			},
		},
	}
}
