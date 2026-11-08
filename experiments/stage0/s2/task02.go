package s2

func task02() Task {
	return Task{
		ID:     "s2-02-invoice-totals",
		Set:    "tuning",
		System: system("an invoicing library"),
		Files: map[string]string{
			"go.mod": goMod,
			"invoice.go": `package billing

// Line is one invoice line.
type Line struct {
	SKU       string
	Qty       int
	UnitCents int
}

// Invoice is a list of lines.
type Invoice struct {
	Lines []Line
}

// Subtotal sums quantity times unit price over all lines.
func (inv Invoice) Subtotal() int {
	total := 0
	for _, l := range inv.Lines {
		total += l.Qty * l.UnitCents
	}
	return total
}
`,
			"format.go": `package billing

import "fmt"

// FormatCents renders an amount as dollars and cents, for example 12.05.
func FormatCents(c int) string {
	return fmt.Sprintf("%d.%02d", c/100, c%100)
}
`,
			"invoice_test.go": `package billing

import "testing"

func TestSubtotal(t *testing.T) {
	inv := Invoice{Lines: []Line{{SKU: "a", Qty: 2, UnitCents: 150}, {SKU: "b", Qty: 1, UnitCents: 99}}}
	if got := inv.Subtotal(); got != 399 {
		t.Fatalf("Subtotal = %d", got)
	}
}

func TestFormatCents(t *testing.T) {
	if got := FormatCents(1205); got != "12.05" {
		t.Fatalf("FormatCents = %q", got)
	}
}
`,
		},
		Hidden: map[string]string{hiddenName: `package billing_test

import (
	"errors"
	"testing"

	billing "fixture"
)

func TestUnits(t *testing.T) {
	inv := billing.Invoice{Lines: []billing.Line{{SKU: "a", Qty: 2, UnitCents: 1}, {SKU: "b", Qty: 3, UnitCents: 1}}}
	if got := inv.Units(); got != 5 {
		t.Fatalf("Units = %d", got)
	}
}

func TestAddValid(t *testing.T) {
	inv := &billing.Invoice{}
	if err := inv.Add(billing.Line{SKU: "a", Qty: 1, UnitCents: 0}); err != nil {
		t.Fatal(err)
	}
	if len(inv.Lines) != 1 {
		t.Fatalf("lines = %d", len(inv.Lines))
	}
}

func TestAddInvalid(t *testing.T) {
	inv := &billing.Invoice{}
	for _, l := range []billing.Line{{SKU: "a", Qty: 0, UnitCents: 5}, {SKU: "a", Qty: 2, UnitCents: -1}} {
		if err := inv.Add(l); !errors.Is(err, billing.ErrBadLine) {
			t.Errorf("Add(%+v) = %v", l, err)
		}
	}
	if len(inv.Lines) != 0 {
		t.Fatalf("lines = %d", len(inv.Lines))
	}
}

func TestDiscountExact(t *testing.T) {
	if got := billing.Discount(1000, 10); got != 100 {
		t.Fatalf("Discount = %d", got)
	}
}

func TestDiscountRounds(t *testing.T) {
	if got := billing.Discount(199, 15); got != 30 {
		t.Errorf("29.85 cents became %d", got)
	}
	if got := billing.Discount(50, 25); got != 13 {
		t.Errorf("12.5 cents became %d", got)
	}
}

func TestTaxExact(t *testing.T) {
	if got := billing.Tax(1000, 175); got != 175 {
		t.Fatalf("Tax = %d", got)
	}
}

func TestTaxRounds(t *testing.T) {
	if got := billing.Tax(999, 175); got != 175 {
		t.Errorf("174.825 cents became %d", got)
	}
	if got := billing.Tax(10, 50); got != 1 {
		t.Errorf("half a cent became %d", got)
	}
}

func TestTotalNoDiscount(t *testing.T) {
	inv := billing.Invoice{Lines: []billing.Line{{SKU: "a", Qty: 2, UnitCents: 500}}}
	if got := inv.Total(0, 100); got != 1100 {
		t.Fatalf("Total = %d", got)
	}
}

func TestTotalTaxAfterDiscount(t *testing.T) {
	inv := billing.Invoice{Lines: []billing.Line{{SKU: "a", Qty: 1, UnitCents: 1000}}}
	if got := inv.Total(10, 200); got != 1080 {
		t.Fatalf("Total = %d, want 900 after discount plus 180 tax", got)
	}
}

func TestParseLineOK(t *testing.T) {
	l, err := billing.ParseLine("widget,3,250")
	if err != nil || l != (billing.Line{SKU: "widget", Qty: 3, UnitCents: 250}) {
		t.Fatalf("ParseLine = %+v, %v", l, err)
	}
}

func TestParseLineBad(t *testing.T) {
	for _, s := range []string{"", "widget,3", "widget,x,250", "widget,0,250", "widget,3,-1"} {
		if _, err := billing.ParseLine(s); err == nil {
			t.Errorf("ParseLine(%q) succeeded", s)
		}
	}
}

func TestParseLineSentinel(t *testing.T) {
	for _, s := range []string{"widget,3", "widget,x,250", "widget,0,250"} {
		if _, err := billing.ParseLine(s); !errors.Is(err, billing.ErrBadLine) {
			t.Errorf("ParseLine(%q) = %v", s, err)
		}
	}
}

func TestProrateExact(t *testing.T) {
	if got := billing.Prorate(3000, 10, 30); got != 1000 {
		t.Fatalf("Prorate = %d", got)
	}
}

func TestProrateRounds(t *testing.T) {
	if got := billing.Prorate(1000, 2, 3); got != 667 {
		t.Fatalf("666.67 cents became %d", got)
	}
}

func TestLateFeeExact(t *testing.T) {
	if got := billing.LateFee(1000); got != 15 {
		t.Fatalf("LateFee = %d", got)
	}
}

func TestLateFeeRounds(t *testing.T) {
	if got := billing.LateFee(999); got != 15 {
		t.Errorf("14.985 cents became %d", got)
	}
	if got := billing.LateFee(100); got != 2 {
		t.Errorf("1.5 cents became %d", got)
	}
}
`},
		Solution: map[string]string{
			"invoice.go": `package billing

import (
	"errors"
	"strconv"
	"strings"
)

// ErrBadLine marks an invalid invoice line, wherever it is found.
var ErrBadLine = errors.New("bad invoice line")

// Line is one invoice line.
type Line struct {
	SKU       string
	Qty       int
	UnitCents int
}

// Invoice is a list of lines.
type Invoice struct {
	Lines []Line
}

// Subtotal sums quantity times unit price over all lines.
func (inv Invoice) Subtotal() int {
	total := 0
	for _, l := range inv.Lines {
		total += l.Qty * l.UnitCents
	}
	return total
}

// Units returns the total quantity across all lines.
func (inv Invoice) Units() int {
	total := 0
	for _, l := range inv.Lines {
		total += l.Qty
	}
	return total
}

func valid(l Line) bool { return l.Qty >= 1 && l.UnitCents >= 0 }

// Add appends a valid line.
func (inv *Invoice) Add(l Line) error {
	if !valid(l) {
		return ErrBadLine
	}
	inv.Lines = append(inv.Lines, l)
	return nil
}

// Total returns the amount due: discount first, then tax on what remains.
func (inv Invoice) Total(discountPercent, taxPermille int) int {
	sub := inv.Subtotal()
	net := sub - Discount(sub, discountPercent)
	return net + Tax(net, taxPermille)
}

// ParseLine reads the text form SKU,qty,unitCents.
func ParseLine(s string) (Line, error) {
	parts := strings.Split(s, ",")
	if len(parts) != 3 {
		return Line{}, ErrBadLine
	}
	qty, err1 := strconv.Atoi(parts[1])
	unit, err2 := strconv.Atoi(parts[2])
	l := Line{SKU: parts[0], Qty: qty, UnitCents: unit}
	if err1 != nil || err2 != nil || !valid(l) {
		return Line{}, ErrBadLine
	}
	return l, nil
}
`,
			"money.go": `package billing

// roundDiv divides to the nearest whole number, with an exact half going up.
func roundDiv(n, d int) int { return (2*n + d) / (2 * d) }

// Discount returns the cents a percentage discount takes off an amount.
func Discount(amountCents, percent int) int { return roundDiv(amountCents*percent, 100) }

// Tax returns the tax on an amount at a rate in tenths of a percent.
func Tax(amountCents, permille int) int { return roundDiv(amountCents*permille, 1000) }

// Prorate returns the share of an amount for the days used in a period.
func Prorate(amountCents, daysUsed, daysInPeriod int) int {
	return roundDiv(amountCents*daysUsed, daysInPeriod)
}

// LateFee returns 1.5% of an amount.
func LateFee(amountCents int) int { return Tax(amountCents, 15) }
`,
		},
		Turns: script(
			local("A house rule for this billing package: when a calculation yields a fraction of a cent, round to the nearest cent, and an exact half goes up. Never just truncate. Nothing needs it yet. To start, add `func (inv Invoice) Units() int`, the total quantity across all lines."),
			local("Add `var ErrBadLine = errors.New(\"bad invoice line\")` and `func (inv *Invoice) Add(l Line) error`, which appends a valid line. A line with Qty below 1 or negative UnitCents is invalid. ErrBadLine is how callers will recognise an invalid line coming from any function in this package."),
			cloud("Add `func Discount(amountCents, percent int) int`, the number of cents a percentage discount takes off an amount."),
			cloud("Finance confirmed how totals work: the discount comes off first and tax is charged on what remains. We'll code that later. For now add `func Tax(amountCents, permille int) int`, the tax on an amount at a rate in tenths of a percent, so 175 means 17.5%."),
			local("Add `func (inv Invoice) Total(discountPercent, taxPermille int) int`, the amount due for the invoice at that discount and tax rate."),
			cloud("Add `func ParseLine(s string) (Line, error)` for the text form SKU,qty,unitCents, for example widget,3,250. Malformed text is an error, and so is a line that could not be added to an invoice."),
			local("Add `func Prorate(amountCents, daysUsed, daysInPeriod int) int`: the share of an amount for the days used out of the days in the period."),
			cloud("Add `func LateFee(amountCents int) int`, which is 1.5% of the amount."),
		),
		Carries: []Carry{
			{
				What:        "fractions of a cent round to nearest, half up, never truncate",
				Kind:        CarryUser,
				Established: 0,
				Needed:      []int{2, 3, 6, 7},
				Tests:       []string{"TestDiscountRounds", "TestTaxRounds", "TestProrateRounds", "TestLateFeeRounds"},
				Absent:      []string{"round", "truncate", "nearest"},
			},
			{
				What:        "discount comes off first, tax is charged on what remains",
				Kind:        CarryUser,
				Established: 3,
				Needed:      []int{4},
				Tests:       []string{"TestTotalTaxAfterDiscount"},
				Absent:      []string{"comes off first", "what remains", "after the discount"},
			},
			{
				What:        "ErrBadLine for an invalid line from any function",
				Kind:        CarryEmbodied,
				Established: 1,
				Needed:      []int{5},
				Tests:       []string{"TestParseLineSentinel"},
				Absent:      []string{"errbadline", "sentinel"},
			},
		},
	}
}
