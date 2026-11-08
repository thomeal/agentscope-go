package s2

func accA05() Task {
	return Task{
		ID:     "s2-a05-expense-reports",
		Set:    "acceptance",
		System: system("an expense report tool"),
		Files: map[string]string{
			"go.mod": goMod,
			"report.go": `package expenses

// Line is one claimed expense.
type Line struct {
	Category string
	Cents    int
	Receipt  bool
}

// Report is one person's claim. Its state is draft, submitted or approved.
type Report struct {
	Owner string
	Lines []Line
	state string
}

// NewReport begins an empty draft.
func NewReport(owner string) *Report { return &Report{Owner: owner, state: "draft"} }

// State returns the report's current state.
func (r *Report) State() string { return r.state }
`,
			"docs/policy.txt": `Expense policy

A line of 2500 cents or more without a receipt is not paid at all. Smaller lines
are paid without one.

A line in the meals category is paid up to 3000 cents. Anything above that is the
employee's to cover.

Reports are paid with the next salary run.
`,
			"report_test.go": `package expenses

import "testing"

func TestNewReportIsDraft(t *testing.T) {
	r := NewReport("ann")
	if r.State() != "draft" || r.Owner != "ann" || len(r.Lines) != 0 {
		t.Fatalf("report = %+v", r)
	}
}
`,
		},
		Hidden: map[string]string{hiddenName: `package expenses_test

import (
	"errors"
	"testing"

	expenses "fixture"
)

func report(t *testing.T, lines ...expenses.Line) *expenses.Report {
	t.Helper()
	r := expenses.NewReport("ann")
	for _, l := range lines {
		if err := r.Add(l); err != nil {
			t.Fatal(err)
		}
	}
	return r
}

func submitted(t *testing.T) *expenses.Report {
	t.Helper()
	r := report(t, expenses.Line{Category: "taxi", Cents: 100})
	if err := r.Submit(); err != nil {
		t.Fatal(err)
	}
	return r
}

func TestTotalAsClaimed(t *testing.T) {
	r := report(t, expenses.Line{Category: "taxi", Cents: 100}, expenses.Line{Category: "meals", Cents: 9000})
	if got := r.Total(); got != 9100 {
		t.Fatalf("Total = %d", got)
	}
}

func TestAddAppends(t *testing.T) {
	r := report(t, expenses.Line{Category: "taxi", Cents: 100}, expenses.Line{Category: "hotel", Cents: 200})
	if len(r.Lines) != 2 || r.Lines[1].Category != "hotel" {
		t.Fatalf("Lines = %+v", r.Lines)
	}
}

func TestAddZeroRefused(t *testing.T) {
	r := expenses.NewReport("ann")
	if err := r.Add(expenses.Line{Category: "taxi"}); err == nil {
		t.Fatal("a zero line was accepted")
	}
	if len(r.Lines) != 0 {
		t.Fatalf("Lines = %+v", r.Lines)
	}
}

func TestAddNegativeAccepted(t *testing.T) {
	r := expenses.NewReport("ann")
	if err := r.Add(expenses.Line{Category: "taxi", Cents: -500}); err != nil {
		t.Fatal(err)
	}
	if got := r.Total(); got != -500 {
		t.Fatalf("Total = %d", got)
	}
}

func TestAddLocked(t *testing.T) {
	r := submitted(t)
	if err := r.Add(expenses.Line{Category: "taxi", Cents: 5}); !errors.Is(err, expenses.ErrLocked) {
		t.Fatalf("err = %v", err)
	}
	if len(r.Lines) != 1 {
		t.Fatalf("Lines = %+v", r.Lines)
	}
}

func TestPayableSmallLines(t *testing.T) {
	r := report(t, expenses.Line{Category: "taxi", Cents: 100}, expenses.Line{Category: "hotel", Cents: 200})
	if got := r.Payable(); got != 300 {
		t.Fatalf("Payable = %d", got)
	}
}

func TestPayableNeedsReceipt(t *testing.T) {
	cases := []struct {
		line expenses.Line
		want int
	}{
		{expenses.Line{Category: "hotel", Cents: 2500}, 0},
		{expenses.Line{Category: "hotel", Cents: 2499}, 2499},
		{expenses.Line{Category: "hotel", Cents: 2500, Receipt: true}, 2500},
	}
	for _, c := range cases {
		if got := report(t, c.line).Payable(); got != c.want {
			t.Errorf("%+v pays %d, want %d", c.line, got, c.want)
		}
	}
}

func TestPayableMealsLimit(t *testing.T) {
	r := report(t, expenses.Line{Category: "meals", Cents: 3500, Receipt: true})
	if got := r.Payable(); got != 3000 {
		t.Errorf("Payable = %d", got)
	}
}

func TestSubmitMoves(t *testing.T) {
	if got := submitted(t).State(); got != "submitted" {
		t.Fatalf("State = %q", got)
	}
}

func TestSubmitNotReady(t *testing.T) {
	if err := expenses.NewReport("ann").Submit(); !errors.Is(err, expenses.ErrNotReady) {
		t.Errorf("empty: %v", err)
	}
	if err := submitted(t).Submit(); !errors.Is(err, expenses.ErrNotReady) {
		t.Errorf("already submitted: %v", err)
	}
}

func TestApproveMoves(t *testing.T) {
	r := submitted(t)
	if err := r.Approve("boss"); err != nil {
		t.Fatal(err)
	}
	if got := r.State(); got != "approved" {
		t.Fatalf("State = %q", got)
	}
}

func TestApproveNotSubmitted(t *testing.T) {
	r := report(t, expenses.Line{Category: "taxi", Cents: 100})
	if err := r.Approve("boss"); !errors.Is(err, expenses.ErrNotReady) {
		t.Fatalf("err = %v", err)
	}
}

func TestApproveOwnReport(t *testing.T) {
	r := submitted(t)
	if err := r.Approve("ann"); !errors.Is(err, expenses.ErrOwnReport) {
		t.Fatalf("err = %v", err)
	}
	if got := r.State(); got != "submitted" {
		t.Fatalf("State = %q", got)
	}
}

func TestRejectOwnReport(t *testing.T) {
	r := submitted(t)
	if err := r.Reject("ann"); !errors.Is(err, expenses.ErrOwnReport) {
		t.Fatalf("err = %v", err)
	}
	if got := r.State(); got != "submitted" {
		t.Fatalf("State = %q", got)
	}
}

func TestRejectReopens(t *testing.T) {
	r := submitted(t)
	if err := r.Reject("boss"); err != nil {
		t.Fatal(err)
	}
	if got := r.State(); got != "draft" {
		t.Fatalf("State = %q", got)
	}
	if err := r.Add(expenses.Line{Category: "taxi", Cents: 5}); err != nil {
		t.Fatal(err)
	}
}

func TestRejectNotSubmitted(t *testing.T) {
	r := report(t, expenses.Line{Category: "taxi", Cents: 100})
	if err := r.Reject("boss"); !errors.Is(err, expenses.ErrNotReady) {
		t.Fatalf("err = %v", err)
	}
}

func TestRemoveDrops(t *testing.T) {
	r := report(t, expenses.Line{Category: "taxi", Cents: 100}, expenses.Line{Category: "hotel", Cents: 200})
	if err := r.Remove(0); err != nil {
		t.Fatal(err)
	}
	if len(r.Lines) != 1 || r.Lines[0].Category != "hotel" {
		t.Fatalf("Lines = %+v", r.Lines)
	}
	if err := r.Remove(5); err == nil {
		t.Fatal("no error for an index out of range")
	}
}

func TestRemoveLocked(t *testing.T) {
	r := submitted(t)
	if err := r.Remove(0); !errors.Is(err, expenses.ErrLocked) {
		t.Fatalf("err = %v", err)
	}
	if len(r.Lines) != 1 {
		t.Fatalf("Lines = %+v", r.Lines)
	}
}

func TestMileageRate(t *testing.T) {
	if got := expenses.Mileage(1000); got != 42 {
		t.Errorf("1 km: %d", got)
	}
	if got := expenses.Mileage(5000); got != 210 {
		t.Errorf("5 km: %d", got)
	}
}

func TestMileageStartedKilometre(t *testing.T) {
	if got := expenses.Mileage(1500); got != 84 {
		t.Errorf("1500 m: %d", got)
	}
	if got := expenses.Mileage(1); got != 42 {
		t.Errorf("1 m: %d", got)
	}
}
`},
		Solution: map[string]string{
			"report.go": `package expenses

import "errors"

// ErrLocked refuses a change to the lines of a report that isn't a draft.
var ErrLocked = errors.New("report locked")

// ErrOwnReport refuses anyone deciding their own report.
var ErrOwnReport = errors.New("own report")

// ErrNotReady refuses a move the report's state does not allow.
var ErrNotReady = errors.New("not ready")

// Line is one claimed expense.
type Line struct {
	Category string
	Cents    int
	Receipt  bool
}

// Report is one person's claim. Its state is draft, submitted or approved.
type Report struct {
	Owner string
	Lines []Line
	state string
}

// NewReport begins an empty draft.
func NewReport(owner string) *Report { return &Report{Owner: owner, state: "draft"} }

// State returns the report's current state.
func (r *Report) State() string { return r.state }

// Total returns the sum of the lines as claimed.
func (r *Report) Total() int {
	total := 0
	for _, l := range r.Lines {
		total += l.Cents
	}
	return total
}

// Add appends a line to a draft.
func (r *Report) Add(l Line) error {
	if r.state != "draft" {
		return ErrLocked
	}
	if l.Cents == 0 {
		return errors.New("zero line")
	}
	r.Lines = append(r.Lines, l)
	return nil
}

// Remove drops a line from a draft.
func (r *Report) Remove(i int) error {
	if r.state != "draft" {
		return ErrLocked
	}
	if i < 0 || i >= len(r.Lines) {
		return errors.New("no such line")
	}
	r.Lines = append(r.Lines[:i], r.Lines[i+1:]...)
	return nil
}

// Submit moves a draft to submitted.
func (r *Report) Submit() error {
	if r.state != "draft" || len(r.Lines) == 0 {
		return ErrNotReady
	}
	r.state = "submitted"
	return nil
}

func (r *Report) decide(approver, state string) error {
	if r.state != "submitted" {
		return ErrNotReady
	}
	if approver == r.Owner {
		return ErrOwnReport
	}
	r.state = state
	return nil
}

// Approve moves a submitted report to approved.
func (r *Report) Approve(approver string) error { return r.decide(approver, "approved") }

// Reject sends a submitted report back to draft.
func (r *Report) Reject(approver string) error { return r.decide(approver, "draft") }
`,
			"pay.go": `package expenses

const (
	receiptFrom = 2500
	mealLimit   = 3000
	centsPerKm  = 42
)

func paid(l Line) int {
	if l.Cents >= receiptFrom && !l.Receipt {
		return 0
	}
	if l.Category == "meals" {
		return min(l.Cents, mealLimit)
	}
	return l.Cents
}

// Payable returns how much of the report gets paid out.
func (r *Report) Payable() int {
	total := 0
	for _, l := range r.Lines {
		total += paid(l)
	}
	return total
}

// Mileage returns the cents paid for a trip of that many meters.
func Mileage(meters int) int { return (meters + 999) / 1000 * centsPerKm }
`,
		},
		Turns: script(
			local("Bookkeeping rule for when we start accepting lines: a negative line is fine, that's how people record money they got back, but a line of exactly zero is always a typo and gets refused. Nothing accepts lines yet. Add `func (r *Report) Total() int`: the sum of the lines as claimed."),
			cloud("Read docs/policy.txt and tell me what decides how much of a line gets paid. No code changes."),
			cloud("Add `var ErrLocked = errors.New(\"report locked\")` and `func (r *Report) Add(l Line) error`, which appends a line to a draft. Only a draft's lines can change: any call that would change the lines of a report in another state returns ErrLocked."),
			local("Add `func (r *Report) Payable() int` in a new file pay.go: how much of the report actually gets paid out."),
			local("Audit's one hard requirement for approvals: nobody decides their own report, whether that's approving or rejecting it. Declare `var ErrOwnReport = errors.New(\"own report\")` for that. There's no approval code yet. Add `var ErrNotReady = errors.New(\"not ready\")` and `func (r *Report) Submit() error`, which moves a draft to submitted. An empty report, or one that isn't a draft, gets ErrNotReady."),
			cloud("Add `func (r *Report) Approve(approver string) error`, which moves a submitted report to approved. If it isn't submitted, return ErrNotReady."),
			local("Add `func (r *Report) Remove(i int) error`, which drops line i. An index out of range gets any error."),
			cloud("Fleet gave us the mileage terms: 42 cents per started kilometre. Nobody's asked for the form yet. Add `func (r *Report) Reject(approver string) error`, which sends a submitted report back to draft so the owner can fix it. If it isn't submitted, return ErrNotReady."),
			local("Add `func Mileage(meters int) int` in pay.go: the cents paid for a trip of that many meters."),
		),
		Carries: []Carry{
			{
				What:        "negative lines are accepted and a zero line is refused",
				Kind:        CarryUser,
				Established: 0,
				Needed:      []int{2},
				Tests:       []string{"TestAddZeroRefused", "TestAddNegativeAccepted"},
				Absent:      []string{"exactly zero", "typo", "negative", "zero"},
			},
			{
				What:        "no pay without a receipt from 2500 cents, meals paid up to 3000 cents (docs/policy.txt)",
				Kind:        CarryDiscovered,
				Established: 1,
				Needed:      []int{3},
				Source:      "docs/policy.txt",
				Tests:       []string{"TestPayableNeedsReceipt", "TestPayableMealsLimit"},
				Absent:      []string{"receipt", "2500", "3000", "meals"},
			},
			{
				What:        "ErrLocked for any change to the lines of a report that isn't a draft",
				Kind:        CarryEmbodied,
				Established: 2,
				Needed:      []int{6},
				Tests:       []string{"TestRemoveLocked"},
				Absent:      []string{"errlocked", "report locked", "only a draft"},
			},
			{
				What:        "nobody decides their own report, ErrOwnReport",
				Kind:        CarryEmbodied,
				Established: 4,
				Needed:      []int{5, 7},
				Tests:       []string{"TestApproveOwnReport", "TestRejectOwnReport"},
				Absent:      []string{"errownreport", "own report", "their own"},
			},
			{
				What:        "mileage pays 42 cents per started kilometre",
				Kind:        CarryUser,
				Established: 7,
				Needed:      []int{8},
				Tests:       []string{"TestMileageRate", "TestMileageStartedKilometre"},
				Absent:      []string{"42", "started", "kilometre", " km"},
			},
		},
	}
}
