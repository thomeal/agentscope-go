package s2

func accC07() Task {
	return Task{
		ID:     "s2-c07-income-tax",
		Set:    "acceptance",
		System: system("an income tax calculator"),
		Files: map[string]string{
			"go.mod": goMod,
			"brackets.go": `package tax

// Bracket is one band of the schedule. Income from From up to the next bracket's
// From is taxed at Permille parts per thousand. Amounts are int64 cents.
type Bracket struct {
	From     int64
	Permille int64
}

// Schedule lists the brackets in rising order.
var Schedule = []Bracket{
	{From: 0, Permille: 100},
	{From: 2000000, Permille: 200},
	{From: 5000000, Permille: 300},
	{From: 10000000, Permille: 400},
}
`,
			"rules/2026.txt": `Tax year 2026 rules

Before the schedule applies, every filer gets a personal allowance of 1000000
cents off their income, and a further 150000 cents for each dependent. Taxable
income never goes below zero.

Filing opens in January. Paper returns go to the regional office.
`,
			"brackets_test.go": `package tax

import "testing"

func TestScheduleRises(t *testing.T) {
	for i := 1; i < len(Schedule); i++ {
		if Schedule[i].From <= Schedule[i-1].From {
			t.Fatalf("bracket %d is out of order", i)
		}
	}
}
`,
		},
		Hidden: map[string]string{hiddenName: `package tax_test

import (
	"testing"

	tax "fixture"
)

func TestBand(t *testing.T) {
	for income, want := range map[int64]int64{0: 100, 1999999: 100, 2000000: 200, 20000000: 400} {
		if got := tax.Band(income).Permille; got != want {
			t.Errorf("Band(%d) = %d, want %d", income, got, want)
		}
	}
}

func TestBandsIsACopy(t *testing.T) {
	got := tax.Bands()
	if len(got) != 4 {
		t.Fatalf("Bands = %v", got)
	}
	got[0].Permille = 999
	if tax.Schedule[0].Permille != 100 {
		t.Fatal("changing the result changed Schedule")
	}
}

func TestTaxableDeductsAllowance(t *testing.T) {
	if got := tax.Taxable(5000000, 0); got != 4000000 {
		t.Fatalf("Taxable = %d", got)
	}
}

func TestTaxableDependents(t *testing.T) {
	if got := tax.Taxable(5000000, 2); got != 3700000 {
		t.Fatalf("Taxable = %d", got)
	}
}

func TestTaxableFloor(t *testing.T) {
	if got := tax.Taxable(500000, 1); got != 0 {
		t.Fatalf("Taxable = %d", got)
	}
}

func TestDueAfterAllowance(t *testing.T) {
	if got := tax.Due(3000000, 0); got != 200000 {
		t.Errorf("one band: %d", got)
	}
	if got := tax.Due(7000000, 0); got != 1100000 {
		t.Errorf("three bands: %d", got)
	}
}

func TestDueRises(t *testing.T) {
	if tax.Due(0, 0) != 0 {
		t.Fatalf("Due(0, 0) = %d", tax.Due(0, 0))
	}
	if !(tax.Due(9000000, 0) > tax.Due(8000000, 0)) {
		t.Fatal("more income is not more tax")
	}
}

func TestDueIsNotFlat(t *testing.T) {
	const income = 100000000
	if got := tax.Due(income, 0); got >= income*400/1000 {
		t.Fatalf("Due = %d, the top rate on everything", got)
	}
}

func TestDueWholeDollars(t *testing.T) {
	for _, income := range []int64{1012345, 2345678, 7654321} {
		got := tax.Due(income, 0)
		if got == 0 || got%100 != 0 {
			t.Errorf("Due(%d) = %d", income, got)
		}
	}
}

func TestDueFavoursTheTaxpayer(t *testing.T) {
	if a, b := tax.Due(3000400, 0), tax.Due(3000000, 0); a != b {
		t.Fatalf("a few more dollars of income: %d, then %d", b, a)
	}
}

func TestEffectiveNoIncome(t *testing.T) {
	if got := tax.Effective(0, 0); got != 0 {
		t.Fatalf("Effective = %d", got)
	}
}

func TestEffectiveBasisPoints(t *testing.T) {
	const income = 7000000
	want := tax.Due(income, 0) * 10000 / income
	if got := tax.Effective(income, 0); got != want {
		t.Fatalf("Effective = %d, want %d", got, want)
	}
}

func TestMarginalBasisPoints(t *testing.T) {
	if got := tax.Marginal(50000000, 0); got != 4000 {
		t.Fatalf("Marginal = %d", got)
	}
}

func TestMarginalUsesTaxable(t *testing.T) {
	if a, b := tax.Marginal(2900000, 0), tax.Marginal(100, 0); a != b {
		t.Errorf("no dependents: %d and %d", a, b)
	}
	if a, b := tax.Marginal(3100000, 1), tax.Marginal(100, 0); a != b {
		t.Errorf("one dependent: %d and %d", a, b)
	}
}

func TestMonthlyTwelfth(t *testing.T) {
	const income = 7000000
	if got, due := tax.Monthly(income, 0), tax.Due(income, 0); got > due/12 || got < due/12-100 {
		t.Fatalf("Monthly = %d of %d", got, due)
	}
}

func TestMonthlyWholeDollars(t *testing.T) {
	for _, income := range []int64{7000000, 8123456, 2345678} {
		if got := tax.Monthly(income, 0); got == 0 || got%100 != 0 {
			t.Errorf("Monthly(%d) = %d", income, got)
		}
	}
}
`},
		Solution: map[string]string{
			"brackets.go": `package tax

const (
	allowance    = 1000000
	perDependent = 150000
	basisPoints  = 10000
)

// Bracket is one band of the schedule. Income from From up to the next bracket's
// From is taxed at Permille parts per thousand. Amounts are int64 cents.
type Bracket struct {
	From     int64
	Permille int64
}

// Schedule lists the brackets in rising order.
var Schedule = []Bracket{
	{From: 0, Permille: 100},
	{From: 2000000, Permille: 200},
	{From: 5000000, Permille: 300},
	{From: 10000000, Permille: 400},
}

// Band returns the bracket that income falls in.
func Band(income int64) Bracket {
	band := Schedule[0]
	for _, b := range Schedule {
		if b.From <= income {
			band = b
		}
	}
	return band
}

// Bands returns a copy of Schedule.
func Bands() []Bracket { return append([]Bracket(nil), Schedule...) }

// Taxable returns the part of an income that is subject to tax.
func Taxable(income int64, dependents int) int64 {
	return max(income-allowance-perDependent*int64(dependents), 0)
}

func wholeDollars(cents int64) int64 { return cents / 100 * 100 }

// Due returns the tax on an income under Schedule.
func Due(income int64, dependents int) int64 {
	taxable := Taxable(income, dependents)
	var due int64
	for i, b := range Schedule {
		upper := taxable
		if i+1 < len(Schedule) {
			upper = min(upper, Schedule[i+1].From)
		}
		if upper > b.From {
			due += (upper - b.From) * b.Permille / 1000
		}
	}
	return wholeDollars(due)
}

// Effective returns the tax due as a share of the income before allowances.
func Effective(income int64, dependents int) int64 {
	if income <= 0 {
		return 0
	}
	return Due(income, dependents) * basisPoints / income
}

// Marginal returns the rate on the next amount earned.
func Marginal(income int64, dependents int) int64 {
	return Band(Taxable(income, dependents)).Permille * basisPoints / 1000
}

// Monthly returns what to withhold each month.
func Monthly(income int64, dependents int) int64 {
	return wholeDollars(Due(income, dependents) / 12)
}
`,
		},
		Turns: script(
			local("Policy for this package: any tax amount we produce is rounded down to a whole dollar, so a multiple of 100 cents, in the taxpayer's favour. Nothing produces one yet. Add `func Band(income int64) Bracket`: the bracket that an amount falls in, which is the last one whose From is not above it."),
			cloud("Read rules/2026.txt and tell me what we need to take from it. No code changes."),
			local("Add `func Taxable(income int64, dependents int) int64`: how much of a filer's income is actually subject to tax."),
			cloud("Add `func Due(income int64, dependents int) int64`: the tax on that filer's income under Schedule. Each rate applies only to the part of the taxable amount that lies inside its bracket."),
			cloud("Payroll and the reports people want every rate we report as an int64 in basis points, truncated. Nothing reports one yet. For now add `func Bands() []Bracket`, which returns a copy of Schedule."),
			local("Add `func Effective(income int64, dependents int) int64`: the tax due as a share of the income before anything is taken off it. With no income it's 0."),
			local("Add `func Marginal(income int64, dependents int) int64`: the rate that would apply to the next amount this filer earns."),
			cloud("Add `func Monthly(income int64, dependents int) int64`: what to withhold each month so that twelve payments cover the year's tax as nearly as they can without going over."),
		),
		Carries: []Carry{
			{
				What:        "tax amounts are rounded down to a whole dollar",
				Kind:        CarryUser,
				Established: 0,
				Needed:      []int{3, 7},
				Tests:       []string{"TestDueWholeDollars", "TestDueFavoursTheTaxpayer", "TestMonthlyWholeDollars", "TestMonthlyTwelfth"},
				Absent:      []string{"round", "whole dollar", "favour", "multiple of"},
			},
			{
				What:        "filers get the allowances in rules/2026.txt before the schedule",
				Kind:        CarryDiscovered,
				Established: 1,
				Needed:      []int{2, 3, 6},
				Source:      "rules/2026.txt",
				Tests:       []string{"TestTaxableDeductsAllowance", "TestTaxableDependents", "TestTaxableFloor", "TestDueAfterAllowance", "TestMarginalUsesTaxable"},
				Absent:      []string{"allowance", "1000000", "150000", "deduct", "rules/"},
			},
			{
				What:        "rates are reported as int64 basis points, truncated",
				Kind:        CarryUser,
				Established: 4,
				Needed:      []int{5, 6},
				Tests:       []string{"TestEffectiveBasisPoints", "TestMarginalBasisPoints"},
				Absent:      []string{"basis point", "payroll", "truncat", "bps"},
			},
		},
	}
}
