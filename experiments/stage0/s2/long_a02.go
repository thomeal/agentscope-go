package s2

func longA02() Task {
	return Task{
		ID:     "s2-long-a02-sales-commissions",
		Set:    "long",
		System: system("a sales commission ledger for a finance team"),
		Files: map[string]string{
			"go.mod": goMod,
			"ledger.go": `package commission

import (
	"errors"
	"sort"
)

// ErrDuplicateRep is returned when a rep's name is already on the ledger.
var ErrDuplicateRep = errors.New("duplicate rep")

// Rep is a salesperson. QuotaCents is the yearly target and Manager is the name
// of the rep they report to, if any.
type Rep struct {
	Name       string
	QuotaCents int
	Manager    string
}

// Ledger holds the reps and everything they have sold.
type Ledger struct {
	reps  map[string]Rep
	deals []*Deal
}

// New returns an empty ledger.
func New() *Ledger {
	return &Ledger{reps: map[string]Rep{}}
}

// AddRep puts a rep on the ledger.
func (l *Ledger) AddRep(r Rep) error {
	if _, ok := l.reps[r.Name]; ok {
		return ErrDuplicateRep
	}
	l.reps[r.Name] = r
	return nil
}

// Reps returns the names of all reps, sorted.
func (l *Ledger) Reps() []string {
	out := make([]string, 0, len(l.reps))
	for name := range l.reps {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// Reports returns the names of the reps a manager has, sorted.
func (l *Ledger) Reports(manager string) []string {
	out := []string{}
	for _, name := range l.Reps() {
		if l.reps[name].Manager == manager {
			out = append(out, name)
		}
	}
	return out
}

// DealCount returns how many deals are on the ledger, cancelled ones too.
func (l *Ledger) DealCount() int { return len(l.deals) }
`,
			"deal.go": `package commission

// Split gives one rep a percentage of a deal.
type Split struct {
	Rep     string
	Percent int
}

// Share is what one rep is credited with for a deal.
type Share struct {
	Rep   string
	Cents int
}

// Deal is one sale. ListCents is the price before the discount and DiscountPct a
// whole percentage. Closed and Cancelled are YYYY-MM-DD dates, and Cancelled is
// empty unless the deal was cancelled. Rep owns the deal. When Splits is empty the
// whole deal is credited to Rep, otherwise only the reps listed there are credited.
type Deal struct {
	ID          string
	Rep         string
	Kind        string // "new" or "renewal"
	ListCents   int
	DiscountPct int
	Closed      string
	Cancelled   string
	Splits      []Split
}

// IsSplit reports whether the deal is shared between reps.
func (d Deal) IsSplit() bool { return len(d.Splits) > 0 }

// Valid reports whether the deal's own fields make sense.
func (d Deal) Valid() bool {
	if d.Kind != "new" && d.Kind != "renewal" {
		return false
	}
	if d.ListCents <= 0 || d.DiscountPct < 0 || d.DiscountPct > 100 {
		return false
	}
	return ValidDate(d.Closed)
}
`,
			"periods.go": `package commission

import "time"

const dateLayout = "2006-01-02"

// ValidDate reports whether a date is in the YYYY-MM-DD form records use.
func ValidDate(date string) bool {
	_, err := time.Parse(dateLayout, date)
	return err == nil
}

// parseDate reads a date that has passed ValidDate.
func parseDate(date string) time.Time {
	t, err := time.Parse(dateLayout, date)
	if err != nil {
		panic("commission: bad date " + date)
	}
	return t
}

// yearMonth returns the year and month of a date.
func yearMonth(date string) (int, int) {
	t := parseDate(date)
	return t.Year(), int(t.Month())
}
`,
			"statement.go": `package commission

// Line is one entry on a statement. Deal is the deal's ID and Cents the amount,
// which is what the rep is paid for that line.
type Line struct {
	Deal  string
	Kind  string
	Cents int
}

// Sum adds up the amounts of some lines.
func Sum(lines []Line) int {
	total := 0
	for _, line := range lines {
		total += line.Cents
	}
	return total
}
`,
			"docs/comp-plan.txt": `Sales compensation plan

Commission on new business goes by bands, on what the rep has sold so far in the
fiscal year. Each rate applies only to the slice of a deal's value that falls in
its band, so one deal can cross bands:

  first 50,000.00 sold in the year     4 percent
  next 100,000.00                      6 percent
  anything above 150,000.00            8 percent

What a rep has sold so far is the net value credited to them on new deals closed
earlier in that fiscal year. Value credited on a deal counts in full, whatever
commission it earned.

Renewals pay a flat 2 percent of their net value. They do not go through the bands
and they do not add to what the rep has sold so far.

A new deal sold at a discount of more than 20 percent earns half of what the bands
give it. Its net value still counts in full towards what the rep has sold so far.

Contests and spot bonuses are run by sales ops and are paid through payroll, not
through this ledger. Managers have no override on their reports' deals.
`,
			"docs/statement-format.txt": `Statement file for payroll

Plain text, and every line, the last one too, ends in a line feed. Fields are
separated by a vertical bar.

The first line is:

  STATEMENT|<rep>|<period>

Then one line for each line of the statement, in statement order:

  <deal id>|<kind>|<amount>

The last line is:

  TOTAL|<sum of the amounts>

Amounts are in euros with a decimal comma and always two decimals, for example
1234,50 and 0,05. There is no thousands separator. A negative amount has a minus
sign in front, for example -12,00.

Payroll loads these files on the 25th. A file loaded twice is rejected by their
side, so a correction has to come as a new period's line, not a changed file.
`,
			"docs/adjustments-policy.txt": `Manual adjustments to commission

Finance can put a manual correction on a rep's statement for a period. It is a
line of its own and never changes the deal it is about.

An adjustment is refused if any of these is true:

  - it has no reason, or a reason longer than 40 characters
  - its amount is zero
  - its amount is more than 1,000.00 in either direction

Anything larger goes to payroll as a separate payment and is not on the statement
at all. Adjustments do not count as sold value and have no effect on any band or
on quota attainment. The finance controller reviews the list of adjustments at the
end of each quarter, so the reason should say what happened and not who asked.
`,
			"commission_test.go": `package commission

import (
	"errors"
	"slices"
	"testing"
)

func TestAddRepRefusesDuplicate(t *testing.T) {
	l := New()
	if err := l.AddRep(Rep{Name: "ann"}); err != nil {
		t.Fatal(err)
	}
	if err := l.AddRep(Rep{Name: "ann"}); !errors.Is(err, ErrDuplicateRep) {
		t.Fatalf("second AddRep: %v", err)
	}
}

func TestRepsAndReports(t *testing.T) {
	l := New()
	for _, r := range []Rep{{Name: "cy", Manager: "ann"}, {Name: "ann"}, {Name: "bob", Manager: "ann"}} {
		if err := l.AddRep(r); err != nil {
			t.Fatal(err)
		}
	}
	if got := l.Reps(); !slices.Equal(got, []string{"ann", "bob", "cy"}) {
		t.Fatalf("Reps = %v", got)
	}
	if got := l.Reports("ann"); !slices.Equal(got, []string{"bob", "cy"}) {
		t.Fatalf("Reports = %v", got)
	}
}

func TestValidDealFields(t *testing.T) {
	good := Deal{Kind: "new", ListCents: 100, Closed: "2026-05-01"}
	if !good.Valid() {
		t.Fatal("a good deal is not valid")
	}
	bad := good
	bad.Closed = "yesterday"
	if bad.Valid() {
		t.Fatal("a bad date is valid")
	}
}

func TestSumOfLines(t *testing.T) {
	if got := Sum([]Line{{Cents: 5}, {Cents: 7}}); got != 12 {
		t.Fatalf("Sum = %d", got)
	}
}
`,
		},
		Hidden: map[string]string{hiddenName: `package commission_test

import (
	"errors"
	"slices"
	"strings"
	"testing"

	commission "fixture"
)

func ledger(t *testing.T, names ...string) *commission.Ledger {
	t.Helper()
	l := commission.New()
	for _, name := range names {
		if err := l.AddRep(commission.Rep{Name: name, QuotaCents: 10000000}); err != nil {
			t.Fatal(err)
		}
	}
	return l
}

func add(t *testing.T, l *commission.Ledger, d commission.Deal) {
	t.Helper()
	if err := l.AddDeal(d); err != nil {
		t.Fatal(err)
	}
}

func sale(id, rep string, cents int, closed string) commission.Deal {
	return commission.Deal{ID: id, Rep: rep, Kind: "new", ListCents: cents, Closed: closed}
}

func earned(t *testing.T, l *commission.Ledger, rep, fy string) map[string]int {
	t.Helper()
	got, err := l.Earned(rep, fy)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func lines(t *testing.T, l *commission.Ledger, rep, date string) []commission.Line {
	t.Helper()
	got, err := l.Lines(rep, commission.PeriodOf(date))
	if err != nil {
		t.Fatal(err)
	}
	return got
}

// cancelled returns a ledger with a 10,000.00 deal closed 2026-05-10, which earns
// 400.00, cancelled on a date.
func cancelled(t *testing.T, date string) *commission.Ledger {
	t.Helper()
	l := ledger(t, "ann")
	add(t, l, sale("D1", "ann", 1000000, "2026-05-10"))
	if err := l.Cancel("D1", date); err != nil {
		t.Fatal(err)
	}
	return l
}

func TestRepCountCounts(t *testing.T) {
	if got := ledger(t, "ann", "bob").RepCount(); got != 2 {
		t.Errorf("RepCount = %d", got)
	}
}

func TestNetCentsDropsFraction(t *testing.T) {
	d := commission.Deal{ListCents: 1005, DiscountPct: 10}
	if got := d.NetCents(); got != 904 {
		t.Errorf("NetCents = %d", got)
	}
}

func TestDealLookup(t *testing.T) {
	l := ledger(t, "ann")
	add(t, l, sale("D1", "ann", 100, "2026-05-01"))
	if d, ok := l.Deal("D1"); !ok || d.Rep != "ann" {
		t.Errorf("Deal = %+v, %v", d, ok)
	}
	if _, ok := l.Deal("D9"); ok {
		t.Error("found a deal that is not there")
	}
}

func TestDealsInClosingOrder(t *testing.T) {
	l := ledger(t, "ann", "bob")
	add(t, l, sale("D3", "ann", 100, "2026-06-01"))
	add(t, l, sale("D2", "ann", 100, "2026-05-01"))
	add(t, l, sale("D1", "ann", 100, "2026-05-01"))
	add(t, l, sale("D4", "bob", 100, "2026-05-01"))
	got, err := l.Deals("ann")
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, d := range got {
		ids = append(ids, d.ID)
	}
	if !slices.Equal(ids, []string{"D1", "D2", "D3"}) {
		t.Errorf("Deals = %v", ids)
	}
}

func TestCancelRecordsDate(t *testing.T) {
	l := cancelled(t, "2026-06-01")
	if d, _ := l.Deal("D1"); d.Cancelled != "2026-06-01" {
		t.Errorf("Cancelled = %q", d.Cancelled)
	}
	if err := l.Cancel("D9", "2026-06-01"); !errors.Is(err, commission.ErrNoDeal) {
		t.Errorf("missing deal: %v", err)
	}
}

func TestDaysBetweenDates(t *testing.T) {
	for _, c := range []struct {
		from, to string
		want     int
	}{{"2026-05-10", "2026-05-10", 0}, {"2026-05-10", "2026-08-08", 90}, {"2028-02-28", "2028-03-01", 2}} {
		if got := commission.DaysBetween(c.from, c.to); got != c.want {
			t.Errorf("DaysBetween(%s, %s) = %d", c.from, c.to, got)
		}
	}
}

func TestPeriodOfStartOfYear(t *testing.T) {
	if got := commission.PeriodOf("2026-04-01"); got != "FY26Q1" {
		t.Errorf("PeriodOf = %q", got)
	}
	if got := commission.FiscalYear("2026-04-01"); got != "FY26" {
		t.Errorf("FiscalYear = %q", got)
	}
}

func TestPeriodOfQuarterEdges(t *testing.T) {
	for date, want := range map[string]string{"2026-06-30": "FY26Q1", "2026-07-01": "FY26Q2", "2026-12-31": "FY26Q3"} {
		if got := commission.PeriodOf(date); got != want {
			t.Errorf("PeriodOf(%s) = %q", date, got)
		}
	}
}

func TestPeriodOfEarlyInCalendarYear(t *testing.T) {
	for date, want := range map[string]string{"2027-01-02": "FY26Q4", "2027-03-31": "FY26Q4"} {
		if got := commission.PeriodOf(date); got != want {
			t.Errorf("PeriodOf(%s) = %q", date, got)
		}
	}
	if got := commission.FiscalYear("2027-03-31"); got != "FY26" {
		t.Errorf("FiscalYear = %q", got)
	}
}

func TestYearToDateFollowsFiscalYear(t *testing.T) {
	l := ledger(t, "ann")
	add(t, l, sale("D1", "ann", 700, "2026-03-20"))
	add(t, l, sale("D2", "ann", 500, "2026-04-02"))
	if got, err := l.YearToDate("ann", "FY26"); err != nil || got != 500 {
		t.Errorf("YearToDate = %d, %v", got, err)
	}
}

func TestBandsRestartWithFiscalYear(t *testing.T) {
	l := ledger(t, "ann")
	add(t, l, sale("D1", "ann", 20000000, "2027-03-20"))
	add(t, l, sale("D2", "ann", 100000, "2027-04-02"))
	if got := earned(t, l, "ann", "FY27")["D2"]; got != 4000 {
		t.Errorf("Earned = %d", got)
	}
}

func TestTieredFirstBand(t *testing.T) {
	if got := commission.Tiered(0, 100000); got != 4000 {
		t.Errorf("Tiered = %d", got)
	}
}

func TestTieredMiddleBand(t *testing.T) {
	if got := commission.Tiered(6000000, 1000000); got != 60000 {
		t.Errorf("Tiered = %d", got)
	}
}

func TestTieredAcrossBands(t *testing.T) {
	if got := commission.Tiered(4000000, 2000000); got != 100000 {
		t.Errorf("Tiered = %d", got)
	}
}

func TestTieredAllBands(t *testing.T) {
	if got := commission.Tiered(4000000, 12000000); got != 740000 {
		t.Errorf("Tiered = %d", got)
	}
}

func TestTieredTopBand(t *testing.T) {
	if got := commission.Tiered(20000000, 100000); got != 10000 {
		t.Errorf("Tiered = %d", got)
	}
}

func TestTieredRoundsDown(t *testing.T) {
	if got := commission.Tiered(0, 1015); got != 40 {
		t.Errorf("Tiered = %d", got)
	}
}

func TestTieredRoundsOnce(t *testing.T) {
	if got := commission.Tiered(4999990, 25); got != 1 {
		t.Errorf("Tiered = %d", got)
	}
}

func TestRenewalFlat(t *testing.T) {
	for _, ytd := range []int{0, 20000000} {
		if got := commission.DealCommission("renewal", 0, 100000, ytd); got != 2000 {
			t.Errorf("DealCommission at ytd %d = %d", ytd, got)
		}
	}
}

func TestRenewalAddsNothingSold(t *testing.T) {
	l := ledger(t, "ann")
	d := sale("D1", "ann", 20000000, "2026-05-01")
	d.Kind = "renewal"
	add(t, l, d)
	add(t, l, sale("D2", "ann", 100000, "2026-05-02"))
	if got := earned(t, l, "ann", "FY26")["D2"]; got != 4000 {
		t.Errorf("Earned = %d", got)
	}
}

func TestYearToDateLeavesOutRenewals(t *testing.T) {
	l := ledger(t, "ann")
	d := sale("D1", "ann", 700, "2026-05-01")
	d.Kind = "renewal"
	add(t, l, d)
	add(t, l, sale("D2", "ann", 500, "2026-05-02"))
	if got, err := l.YearToDate("ann", "FY26"); err != nil || got != 500 {
		t.Errorf("YearToDate = %d, %v", got, err)
	}
}

func TestHeavyDiscountPaysHalf(t *testing.T) {
	if got := commission.DealCommission("new", 21, 100000, 0); got != 2000 {
		t.Errorf("DealCommission = %d", got)
	}
}

func TestDiscountAtTheLineIsFull(t *testing.T) {
	if got := commission.DealCommission("new", 20, 100000, 0); got != 4000 {
		t.Errorf("DealCommission = %d", got)
	}
}

func TestHalfRoundsDown(t *testing.T) {
	if got := commission.DealCommission("new", 25, 1025, 0); got != 20 {
		t.Errorf("DealCommission = %d", got)
	}
}

func TestHeavyDiscountStillCounts(t *testing.T) {
	l := ledger(t, "ann")
	d := sale("D1", "ann", 12000000, "2026-05-01")
	d.DiscountPct = 50
	add(t, l, d)
	add(t, l, sale("D2", "ann", 100000, "2026-05-02"))
	if got := earned(t, l, "ann", "FY26")["D2"]; got != 6000 {
		t.Errorf("Earned = %d", got)
	}
}

func TestEarnedTopBand(t *testing.T) {
	l := ledger(t, "ann")
	add(t, l, sale("D1", "ann", 20000000, "2026-05-01"))
	add(t, l, sale("D2", "ann", 100000, "2026-05-02"))
	if got := earned(t, l, "ann", "FY26")["D2"]; got != 10000 {
		t.Errorf("Earned = %d", got)
	}
}

func TestWhatIfTopBand(t *testing.T) {
	l := ledger(t, "ann")
	add(t, l, sale("D1", "ann", 20000000, "2026-05-01"))
	if got, err := l.WhatIf("ann", "FY26", 100000); err != nil || got != 10000 {
		t.Errorf("WhatIf = %d, %v", got, err)
	}
}

func TestWhatIfFirstBand(t *testing.T) {
	l := ledger(t, "ann")
	add(t, l, sale("D1", "ann", 100000, "2026-05-01"))
	if got, err := l.WhatIf("ann", "FY26", 100000); err != nil || got != 4000 {
		t.Errorf("WhatIf = %d, %v", got, err)
	}
}

func TestForecastTopBand(t *testing.T) {
	l := ledger(t, "ann")
	add(t, l, sale("D1", "ann", 20000000, "2026-05-01"))
	if got, err := l.Forecast("ann", "FY26", []int{100000, 100000}); err != nil || got != 20000 {
		t.Errorf("Forecast = %d, %v", got, err)
	}
}

func TestForecastBuildsUp(t *testing.T) {
	l := ledger(t, "ann")
	add(t, l, sale("D1", "ann", 4000000, "2026-05-01"))
	if got, err := l.Forecast("ann", "FY26", []int{1000000, 1000000}); err != nil || got != 100000 {
		t.Errorf("Forecast = %d, %v", got, err)
	}
}

func TestForecastRoundsPerDeal(t *testing.T) {
	l := ledger(t, "ann")
	if got, err := l.Forecast("ann", "FY26", []int{1015, 1015}); err != nil || got != 80 {
		t.Errorf("Forecast = %d, %v", got, err)
	}
}

func TestSharesWholeDeal(t *testing.T) {
	got, err := commission.Shares(commission.Deal{Rep: "ann", ListCents: 1000, DiscountPct: 10})
	if err != nil || !slices.Equal(got, []commission.Share{{Rep: "ann", Cents: 900}}) {
		t.Errorf("Shares = %+v, %v", got, err)
	}
}

func TestSharesDivideNetValue(t *testing.T) {
	d := commission.Deal{Rep: "ann", ListCents: 1000, DiscountPct: 10, Splits: []commission.Split{{Rep: "ann", Percent: 50}, {Rep: "bob", Percent: 50}}}
	got, err := commission.Shares(d)
	if err != nil || !slices.Equal(got, []commission.Share{{Rep: "ann", Cents: 450}, {Rep: "bob", Cents: 450}}) {
		t.Errorf("Shares = %+v, %v", got, err)
	}
}

func TestSharesLeftoverToFirst(t *testing.T) {
	d := commission.Deal{Rep: "cy", ListCents: 101, Splits: []commission.Split{{Rep: "ann", Percent: 33}, {Rep: "bob", Percent: 33}, {Rep: "cy", Percent: 34}}}
	want := []commission.Share{{Rep: "ann", Cents: 34}, {Rep: "bob", Cents: 33}, {Rep: "cy", Cents: 34}}
	if got, err := commission.Shares(d); err != nil || !slices.Equal(got, want) {
		t.Errorf("Shares = %+v, %v", got, err)
	}
}

func TestSharesBadTotal(t *testing.T) {
	d := commission.Deal{Rep: "ann", ListCents: 100, Splits: []commission.Split{{Rep: "ann", Percent: 60}, {Rep: "bob", Percent: 50}}}
	if _, err := commission.Shares(d); !errors.Is(err, commission.ErrBadSplit) {
		t.Errorf("err = %v", err)
	}
}

func TestEarnedSplitDeal(t *testing.T) {
	l := ledger(t, "ann", "bob")
	d := sale("D1", "ann", 100000, "2026-05-01")
	d.Splits = []commission.Split{{Rep: "ann", Percent: 50}, {Rep: "bob", Percent: 50}}
	add(t, l, d)
	for _, rep := range []string{"ann", "bob"} {
		if got := earned(t, l, rep, "FY26")["D1"]; got != 2000 {
			t.Errorf("%s earned %d", rep, got)
		}
	}
}

func TestAddDealUnknownRep(t *testing.T) {
	err := ledger(t, "ann").AddDeal(sale("D1", "zed", 100, "2026-05-01"))
	if !errors.Is(err, commission.ErrUnknownRep) {
		t.Errorf("err = %v", err)
	}
}

func TestAddDealUnknownSplitRep(t *testing.T) {
	d := sale("D1", "ann", 100, "2026-05-01")
	d.Splits = []commission.Split{{Rep: "ann", Percent: 50}, {Rep: "zed", Percent: 50}}
	if err := ledger(t, "ann").AddDeal(d); !errors.Is(err, commission.ErrUnknownRep) {
		t.Errorf("err = %v", err)
	}
}

func TestDealsUnknownRep(t *testing.T) {
	if _, err := ledger(t, "ann").Deals("zed"); !errors.Is(err, commission.ErrUnknownRep) {
		t.Errorf("err = %v", err)
	}
}

func TestQuotaUnknownRep(t *testing.T) {
	l := ledger(t, "ann")
	if got, err := l.Quota("ann"); err != nil || got != 10000000 {
		t.Errorf("Quota = %d, %v", got, err)
	}
	if _, err := l.Quota("zed"); !errors.Is(err, commission.ErrUnknownRep) {
		t.Errorf("err = %v", err)
	}
}

func TestEarnedUnknownRep(t *testing.T) {
	if _, err := ledger(t, "ann").Earned("zed", "FY26"); !errors.Is(err, commission.ErrUnknownRep) {
		t.Errorf("err = %v", err)
	}
}

func TestLinesUnknownRep(t *testing.T) {
	if _, err := ledger(t, "ann").Lines("zed", "FY26Q1"); !errors.Is(err, commission.ErrUnknownRep) {
		t.Errorf("err = %v", err)
	}
}

func TestTotalUnknownRep(t *testing.T) {
	if _, err := ledger(t, "ann").Total("zed", "FY26Q1"); !errors.Is(err, commission.ErrUnknownRep) {
		t.Errorf("err = %v", err)
	}
}

func TestAttainmentUnknownRep(t *testing.T) {
	if _, err := ledger(t, "ann").Attainment("zed", "FY26"); !errors.Is(err, commission.ErrUnknownRep) {
		t.Errorf("err = %v", err)
	}
}

func TestTeamTotalUnknownRep(t *testing.T) {
	if _, err := ledger(t, "ann").TeamTotal("zed", "FY26Q1"); !errors.Is(err, commission.ErrUnknownRep) {
		t.Errorf("err = %v", err)
	}
}

func TestHouseEarnsNothing(t *testing.T) {
	l := ledger(t, "HOUSE")
	add(t, l, sale("D1", "HOUSE", 100000, "2026-05-01"))
	got := earned(t, l, "HOUSE", "FY26")
	if amount, ok := got["D1"]; !ok || amount != 0 {
		t.Errorf("Earned = %v", got)
	}
}

func TestHouseStaysOnStatement(t *testing.T) {
	l := ledger(t, "HOUSE")
	add(t, l, sale("D1", "HOUSE", 100000, "2026-05-01"))
	want := []commission.Line{{Deal: "D1", Kind: "new", Cents: 0}}
	if got := lines(t, l, "HOUSE", "2026-05-01"); !slices.Equal(got, want) {
		t.Errorf("Lines = %+v", got)
	}
}

func TestLinesOfAPeriod(t *testing.T) {
	l := ledger(t, "ann")
	add(t, l, sale("D2", "ann", 100000, "2026-05-02"))
	add(t, l, sale("D1", "ann", 100000, "2026-05-01"))
	add(t, l, sale("D3", "ann", 100000, "2026-09-01"))
	want := []commission.Line{{Deal: "D1", Kind: "new", Cents: 4000}, {Deal: "D2", Kind: "new", Cents: 4000}}
	if got := lines(t, l, "ann", "2026-05-01"); !slices.Equal(got, want) {
		t.Errorf("Lines = %+v", got)
	}
}

func TestClawbackLine(t *testing.T) {
	l := cancelled(t, "2026-08-01")
	want := []commission.Line{{Deal: "D1", Kind: "clawback", Cents: -40000}}
	if got := lines(t, l, "ann", "2026-08-01"); !slices.Equal(got, want) {
		t.Errorf("Lines = %+v", got)
	}
}

func TestClawbackDayNinety(t *testing.T) {
	l := cancelled(t, "2026-08-08")
	want := []commission.Line{{Deal: "D1", Kind: "clawback", Cents: -40000}}
	if got := lines(t, l, "ann", "2026-08-08"); !slices.Equal(got, want) {
		t.Errorf("Lines = %+v", got)
	}
}

func TestNoClawbackDayNinetyOne(t *testing.T) {
	l := cancelled(t, "2026-08-09")
	if got := lines(t, l, "ann", "2026-08-09"); len(got) != 0 {
		t.Errorf("Lines = %+v", got)
	}
}

func TestClawbackLeavesItsOwnPeriodAlone(t *testing.T) {
	l := cancelled(t, "2026-08-01")
	want := []commission.Line{{Deal: "D1", Kind: "new", Cents: 40000}}
	if got := lines(t, l, "ann", "2026-05-10"); !slices.Equal(got, want) {
		t.Errorf("Lines = %+v", got)
	}
}

func TestClawbackDoesNotRecalculate(t *testing.T) {
	l := ledger(t, "ann")
	add(t, l, sale("D1", "ann", 6000000, "2026-05-10"))
	if err := l.Cancel("D1", "2026-06-01"); err != nil {
		t.Fatal(err)
	}
	add(t, l, sale("D2", "ann", 100000, "2026-06-02"))
	if got := earned(t, l, "ann", "FY26")["D2"]; got != 6000 {
		t.Errorf("Earned = %d", got)
	}
}

func TestTotalTakesOffClawback(t *testing.T) {
	l := cancelled(t, "2026-05-20")
	add(t, l, sale("D2", "ann", 100000, "2026-05-21"))
	got, err := l.Total("ann", commission.PeriodOf("2026-05-21"))
	if err != nil || got != 4000 {
		t.Errorf("Total = %d, %v", got, err)
	}
}

func TestTotalSumsLines(t *testing.T) {
	l := ledger(t, "ann")
	add(t, l, sale("D1", "ann", 100000, "2026-05-01"))
	add(t, l, sale("D2", "ann", 200000, "2026-05-02"))
	got, err := l.Total("ann", commission.PeriodOf("2026-05-01"))
	if err != nil || got != 12000 {
		t.Errorf("Total = %d, %v", got, err)
	}
}

func TestRenderWholeFile(t *testing.T) {
	l := ledger(t, "ann")
	add(t, l, sale("D1", "ann", 100000, "2026-05-01"))
	add(t, l, sale("D2", "ann", 200000, "2026-05-02"))
	period := commission.PeriodOf("2026-05-01")
	want := "STATEMENT|ann|" + period + "\nD1|new|40,00\nD2|new|80,00\nTOTAL|120,00\n"
	if got, err := l.Render("ann", period); err != nil || got != want {
		t.Errorf("Render = %q, %v", got, err)
	}
}

func TestRenderSmallAmount(t *testing.T) {
	l := ledger(t, "ann")
	add(t, l, sale("D1", "ann", 125, "2026-05-01"))
	got, err := l.Render("ann", commission.PeriodOf("2026-05-01"))
	if err != nil || !strings.Contains(got, "D1|new|0,05\n") {
		t.Errorf("Render = %q, %v", got, err)
	}
}

func TestRenderNegativeAmount(t *testing.T) {
	l := cancelled(t, "2026-08-01")
	got, err := l.Render("ann", commission.PeriodOf("2026-08-01"))
	if err != nil || !strings.Contains(got, "D1|clawback|-400,00\n") || !strings.Contains(got, "TOTAL|-400,00\n") {
		t.Errorf("Render = %q, %v", got, err)
	}
}

func TestAttainmentWholePercent(t *testing.T) {
	l := ledger(t, "ann")
	add(t, l, sale("D1", "ann", 2559000, "2026-05-01"))
	if got, err := l.Attainment("ann", "FY26"); err != nil || got != 25 {
		t.Errorf("Attainment = %d, %v", got, err)
	}
}

func TestAttainmentNoQuota(t *testing.T) {
	l := commission.New()
	if err := l.AddRep(commission.Rep{Name: "ann"}); err != nil {
		t.Fatal(err)
	}
	add(t, l, sale("D1", "ann", 100, "2026-05-01"))
	if got, err := l.Attainment("ann", "FY26"); err != nil || got != 0 {
		t.Errorf("Attainment = %d, %v", got, err)
	}
}

func TestLeaderboardOrder(t *testing.T) {
	l := ledger(t, "ann", "bob", "cy", "dee")
	add(t, l, sale("D1", "bob", 100000, "2026-05-01"))
	add(t, l, sale("D2", "dee", 300000, "2026-05-01"))
	add(t, l, sale("D3", "cy", 100000, "2026-05-01"))
	want := []string{"dee", "bob", "cy", "ann"}
	if got := l.Leaderboard(commission.PeriodOf("2026-05-01")); !slices.Equal(got, want) {
		t.Errorf("Leaderboard = %v", got)
	}
}

func TestTeamTotalWithReports(t *testing.T) {
	l := commission.New()
	for _, r := range []commission.Rep{{Name: "ann"}, {Name: "bob", Manager: "ann"}, {Name: "cy"}} {
		if err := l.AddRep(r); err != nil {
			t.Fatal(err)
		}
	}
	add(t, l, sale("D1", "ann", 100000, "2026-05-01"))
	add(t, l, sale("D2", "bob", 200000, "2026-05-01"))
	add(t, l, sale("D3", "cy", 300000, "2026-05-01"))
	if got, err := l.TeamTotal("ann", commission.PeriodOf("2026-05-01")); err != nil || got != 12000 {
		t.Errorf("TeamTotal = %d, %v", got, err)
	}
}

func TestPeriodStartDates(t *testing.T) {
	for period, want := range map[string]string{"FY26Q1": "2026-04-01", "FY26Q2": "2026-07-01", "FY26Q4": "2027-01-01"} {
		if got := commission.PeriodStart(period); got != want {
			t.Errorf("PeriodStart(%s) = %q", period, got)
		}
	}
}

func TestPeriodsOfAYear(t *testing.T) {
	want := []string{"FY26Q1", "FY26Q2", "FY26Q3", "FY26Q4"}
	if got := commission.Periods("FY26"); !slices.Equal(got, want) {
		t.Errorf("Periods = %v", got)
	}
}

func TestYearTotalAcrossPeriods(t *testing.T) {
	l := ledger(t, "ann")
	add(t, l, sale("D1", "ann", 100000, "2026-05-01"))
	add(t, l, sale("D2", "ann", 200000, "2026-08-01"))
	if got, err := l.YearTotal("ann", "FY26"); err != nil || got != 12000 {
		t.Errorf("YearTotal = %d, %v", got, err)
	}
}

func TestAdjustUnknownRep(t *testing.T) {
	if err := ledger(t, "ann").Adjust("zed", "FY26Q1", 100, "typo"); !errors.Is(err, commission.ErrUnknownRep) {
		t.Errorf("err = %v", err)
	}
}

func TestAdjustZeroRefused(t *testing.T) {
	if err := ledger(t, "ann").Adjust("ann", "FY26Q1", 0, "typo"); !errors.Is(err, commission.ErrBadAdjustment) {
		t.Errorf("err = %v", err)
	}
}

func TestAdjustAmountLimit(t *testing.T) {
	l := ledger(t, "ann")
	for _, cents := range []int{100000, -100000} {
		if err := l.Adjust("ann", "FY26Q1", cents, "typo"); err != nil {
			t.Errorf("%d: %v", cents, err)
		}
	}
	for _, cents := range []int{100001, -100001} {
		if err := l.Adjust("ann", "FY26Q1", cents, "typo"); !errors.Is(err, commission.ErrBadAdjustment) {
			t.Errorf("%d: %v", cents, err)
		}
	}
}

func TestAdjustReasonLength(t *testing.T) {
	l := ledger(t, "ann")
	if err := l.Adjust("ann", "FY26Q1", 100, strings.Repeat("x", 40)); err != nil {
		t.Errorf("40 characters: %v", err)
	}
	for _, reason := range []string{"", strings.Repeat("x", 41)} {
		if err := l.Adjust("ann", "FY26Q1", 100, reason); !errors.Is(err, commission.ErrBadAdjustment) {
			t.Errorf("reason %q: %v", reason, err)
		}
	}
}

func TestLinesShowAdjustmentsLast(t *testing.T) {
	l := ledger(t, "ann")
	period := commission.PeriodOf("2026-05-01")
	for _, a := range []struct {
		cents  int
		reason string
	}{{500, "typo"}, {-200, "refund"}} {
		if err := l.Adjust("ann", period, a.cents, a.reason); err != nil {
			t.Fatal(err)
		}
	}
	add(t, l, sale("D1", "ann", 100000, "2026-05-01"))
	want := []commission.Line{{Deal: "D1", Kind: "new", Cents: 4000}, {Deal: "typo", Kind: "adjustment", Cents: 500}, {Deal: "refund", Kind: "adjustment", Cents: -200}}
	if got := lines(t, l, "ann", "2026-05-01"); !slices.Equal(got, want) {
		t.Errorf("Lines = %+v", got)
	}
}

func TestAccrualAllReps(t *testing.T) {
	l := ledger(t, "ann", "bob")
	add(t, l, sale("D1", "ann", 100000, "2026-05-01"))
	add(t, l, sale("D2", "bob", 200000, "2026-05-01"))
	if got := l.Accrual(commission.PeriodOf("2026-05-01")); got != 12000 {
		t.Errorf("Accrual = %d", got)
	}
}
`},
		Solution: map[string]string{
			"ledger.go": `package commission

import (
	"errors"
	"slices"
	"sort"
)

var (
	// ErrDuplicateRep is returned when a rep's name is already on the ledger.
	ErrDuplicateRep = errors.New("duplicate rep")
	// ErrUnknownRep is returned by every call naming a rep who is missing.
	ErrUnknownRep = errors.New("unknown rep")
	// ErrNoDeal is returned for an ID no deal has.
	ErrNoDeal = errors.New("no such deal")
)

// Rep is a salesperson. QuotaCents is the yearly target and Manager is the name
// of the rep they report to, if any.
type Rep struct {
	Name       string
	QuotaCents int
	Manager    string
}

// Ledger holds the reps and everything they have sold.
type Ledger struct {
	reps        map[string]Rep
	deals       []*Deal
	adjustments []Adjustment
}

// New returns an empty ledger.
func New() *Ledger {
	return &Ledger{reps: map[string]Rep{}}
}

// AddRep puts a rep on the ledger.
func (l *Ledger) AddRep(r Rep) error {
	if _, ok := l.reps[r.Name]; ok {
		return ErrDuplicateRep
	}
	l.reps[r.Name] = r
	return nil
}

// checkRep returns the error for a rep who is not on the ledger.
func (l *Ledger) checkRep(rep string) error {
	if _, ok := l.reps[rep]; !ok {
		return ErrUnknownRep
	}
	return nil
}

// RepCount returns how many reps are on the ledger.
func (l *Ledger) RepCount() int { return len(l.reps) }

// Quota returns a rep's QuotaCents.
func (l *Ledger) Quota(rep string) (int, error) {
	if err := l.checkRep(rep); err != nil {
		return 0, err
	}
	return l.reps[rep].QuotaCents, nil
}

// Reps returns the names of all reps, sorted.
func (l *Ledger) Reps() []string {
	out := make([]string, 0, len(l.reps))
	for name := range l.reps {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// Reports returns the names of the reps a manager has, sorted.
func (l *Ledger) Reports(manager string) []string {
	out := []string{}
	for _, name := range l.Reps() {
		if l.reps[name].Manager == manager {
			out = append(out, name)
		}
	}
	return out
}

// DealCount returns how many deals are on the ledger, cancelled ones too.
func (l *Ledger) DealCount() int { return len(l.deals) }

// AddDeal records a deal. Every rep it names has to be on the ledger.
func (l *Ledger) AddDeal(d Deal) error {
	if err := l.checkRep(d.Rep); err != nil {
		return err
	}
	for _, s := range d.Splits {
		if err := l.checkRep(s.Rep); err != nil {
			return err
		}
	}
	l.deals = append(l.deals, &d)
	return nil
}

// Deal returns a copy of a deal by ID.
func (l *Ledger) Deal(id string) (Deal, bool) {
	for _, d := range l.deals {
		if d.ID == id {
			return *d, true
		}
	}
	return Deal{}, false
}

// closeOrder returns the deals in closing order, ties by ID.
func (l *Ledger) closeOrder() []*Deal {
	out := slices.Clone(l.deals)
	sort.SliceStable(out, func(a, b int) bool {
		if out[a].Closed != out[b].Closed {
			return out[a].Closed < out[b].Closed
		}
		return out[a].ID < out[b].ID
	})
	return out
}

// Deals returns the deals a rep owns, in closing order.
func (l *Ledger) Deals(rep string) ([]Deal, error) {
	if err := l.checkRep(rep); err != nil {
		return nil, err
	}
	out := []Deal{}
	for _, d := range l.closeOrder() {
		if d.Rep == rep {
			out = append(out, *d)
		}
	}
	return out, nil
}

// Cancel records the date a deal was cancelled.
func (l *Ledger) Cancel(id, date string) error {
	for _, d := range l.deals {
		if d.ID == id {
			d.Cancelled = date
			return nil
		}
	}
	return ErrNoDeal
}
`,
			"deal.go": `package commission

import "errors"

// ErrBadSplit is returned for split percentages that do not make a whole.
var ErrBadSplit = errors.New("bad split")

// Split gives one rep a percentage of a deal.
type Split struct {
	Rep     string
	Percent int
}

// Share is what one rep is credited with for a deal.
type Share struct {
	Rep   string
	Cents int
}

// Deal is one sale. ListCents is the price before the discount and DiscountPct a
// whole percentage. Closed and Cancelled are YYYY-MM-DD dates, and Cancelled is
// empty unless the deal was cancelled. Rep owns the deal. When Splits is empty the
// whole deal is credited to Rep, otherwise only the reps listed there are credited.
type Deal struct {
	ID          string
	Rep         string
	Kind        string // "new" or "renewal"
	ListCents   int
	DiscountPct int
	Closed      string
	Cancelled   string
	Splits      []Split
}

// IsSplit reports whether the deal is shared between reps.
func (d Deal) IsSplit() bool { return len(d.Splits) > 0 }

// Valid reports whether the deal's own fields make sense.
func (d Deal) Valid() bool {
	if d.Kind != "new" && d.Kind != "renewal" {
		return false
	}
	if d.ListCents <= 0 || d.DiscountPct < 0 || d.DiscountPct > 100 {
		return false
	}
	return ValidDate(d.Closed)
}

// NetCents returns the list price less the discount.
func (d Deal) NetCents() int { return d.ListCents * (100 - d.DiscountPct) / 100 }

// Shares returns what each rep is credited with for a deal, in the order listed.
func Shares(d Deal) ([]Share, error) {
	net := d.NetCents()
	if len(d.Splits) == 0 {
		return []Share{{Rep: d.Rep, Cents: net}}, nil
	}
	out := make([]Share, len(d.Splits))
	total, given := 0, 0
	for i, s := range d.Splits {
		cents := net * s.Percent / 100
		out[i] = Share{Rep: s.Rep, Cents: cents}
		total += s.Percent
		given += cents
	}
	if total != 100 {
		return nil, ErrBadSplit
	}
	out[0].Cents += net - given
	return out, nil
}
`,
			"periods.go": `package commission

import (
	"fmt"
	"time"
)

const dateLayout = "2006-01-02"

// ValidDate reports whether a date is in the YYYY-MM-DD form records use.
func ValidDate(date string) bool {
	_, err := time.Parse(dateLayout, date)
	return err == nil
}

// parseDate reads a date that has passed ValidDate.
func parseDate(date string) time.Time {
	t, err := time.Parse(dateLayout, date)
	if err != nil {
		panic("commission: bad date " + date)
	}
	return t
}

// yearMonth returns the year and month of a date.
func yearMonth(date string) (int, int) {
	t := parseDate(date)
	return t.Year(), int(t.Month())
}

// fiscal returns the two-digit fiscal year and the quarter a date falls in.
func fiscal(date string) (int, int) {
	y, m := yearMonth(date)
	if m < 4 {
		y--
		m += 12
	}
	return y % 100, (m-4)/3 + 1
}

// PeriodOf returns the period a date falls in, written like FY26Q1.
func PeriodOf(date string) string {
	y, q := fiscal(date)
	return fmt.Sprintf("FY%02dQ%d", y, q)
}

// FiscalYear returns the fiscal year a date falls in, written like FY26.
func FiscalYear(date string) string {
	y, _ := fiscal(date)
	return fmt.Sprintf("FY%02d", y)
}

// DaysBetween returns the number of days from one date to the other.
func DaysBetween(from, to string) int {
	return int(parseDate(to).Sub(parseDate(from)).Hours() / 24)
}

// PeriodStart returns the first date of a period written like FY26Q1.
func PeriodStart(period string) string {
	var y, q int
	if _, err := fmt.Sscanf(period, "FY%02dQ%d", &y, &q); err != nil {
		return ""
	}
	month := 4 + (q-1)*3
	year := 2000 + y
	if month > 12 {
		year++
		month -= 12
	}
	return fmt.Sprintf("%04d-%02d-01", year, month)
}

// Periods returns the four periods of a fiscal year written like FY26.
func Periods(fy string) []string {
	out := make([]string, 0, 4)
	for q := 1; q <= 4; q++ {
		out = append(out, fmt.Sprintf("%sQ%d", fy, q))
	}
	return out
}
`,
			"adjust.go": `package commission

import (
	"errors"
	"unicode/utf8"
)

// ErrBadAdjustment is returned for an adjustment that may not be made.
var ErrBadAdjustment = errors.New("bad adjustment")

const (
	maxAdjustment = 100000
	maxReason     = 40
)

// Adjustment is a manual correction to a rep's statement for a period.
type Adjustment struct {
	Rep    string
	Period string
	Cents  int
	Reason string
}

// Adjust records a manual adjustment.
func (l *Ledger) Adjust(rep, period string, cents int, reason string) error {
	if err := l.checkRep(rep); err != nil {
		return err
	}
	if reason == "" || utf8.RuneCountInString(reason) > maxReason {
		return ErrBadAdjustment
	}
	if cents == 0 || cents > maxAdjustment || cents < -maxAdjustment {
		return ErrBadAdjustment
	}
	l.adjustments = append(l.adjustments, Adjustment{Rep: rep, Period: period, Cents: cents, Reason: reason})
	return nil
}

// adjustmentLines returns a rep's adjustments for a period as statement lines, in
// the order they were made, with the reason in Deal.
func (l *Ledger) adjustmentLines(rep, period string) []Line {
	var out []Line
	for _, a := range l.adjustments {
		if a.Rep == rep && a.Period == period {
			out = append(out, Line{Deal: a.Reason, Kind: "adjustment", Cents: a.Cents})
		}
	}
	return out
}
`,
			"rates.go": `package commission

import "math"

// band is one slice of what a rep sells in a fiscal year: its rate applies up to
// the value upto.
type band struct {
	upto int
	pct  int
}

var bands = []band{
	{5000000, 4},
	{15000000, 6},
	{math.MaxInt, 10},
}

const (
	renewalPct    = 2
	heavyDiscount = 20
)

// bandSum returns the commission on net after ytd, in hundredths of a cent.
func bandSum(ytd, net int) int {
	sum, at, left := 0, ytd, net
	for _, b := range bands {
		if left <= 0 {
			break
		}
		if at >= b.upto {
			continue
		}
		slice := min(left, b.upto-at)
		sum += slice * b.pct
		at += slice
		left -= slice
	}
	return sum
}

// roundDown turns hundredths of a cent into whole cents. It happens once per deal.
func roundDown(hundredths int) int { return hundredths / 100 }

// Tiered returns the commission on a new deal of net value for a rep who has
// already sold ytd in this fiscal year.
func Tiered(ytd, net int) int { return roundDown(bandSum(ytd, net)) }

// DealCommission returns the commission on any deal.
func DealCommission(kind string, discountPct, net, ytd int) int {
	if kind == "renewal" {
		return roundDown(net * renewalPct)
	}
	sum := bandSum(ytd, net)
	if discountPct > heavyDiscount {
		sum /= 2
	}
	return roundDown(sum)
}
`,
			"earned.go": `package commission

const houseRep = "HOUSE"

// credit is one deal credited to one rep.
type credit struct {
	deal  *Deal
	net   int
	cents int
}

// credits returns a rep's credited deals across all years, in closing order.
func (l *Ledger) credits(rep string) []credit {
	var out []credit
	ytd := map[string]int{}
	for _, d := range l.closeOrder() {
		shares, err := Shares(*d)
		if err != nil {
			continue
		}
		for _, s := range shares {
			if s.Rep != rep {
				continue
			}
			fy := FiscalYear(d.Closed)
			cents := DealCommission(d.Kind, d.DiscountPct, s.Cents, ytd[fy])
			if rep == houseRep {
				cents = 0
			}
			if d.Kind == "new" {
				ytd[fy] += s.Cents
			}
			out = append(out, credit{deal: d, net: s.Cents, cents: cents})
		}
	}
	return out
}

// Earned returns the commission a rep earned on each deal closed in a fiscal year.
func (l *Ledger) Earned(rep, fy string) (map[string]int, error) {
	if err := l.checkRep(rep); err != nil {
		return nil, err
	}
	out := map[string]int{}
	for _, c := range l.credits(rep) {
		if FiscalYear(c.deal.Closed) == fy {
			out[c.deal.ID] = c.cents
		}
	}
	return out, nil
}

// YearToDate returns the value a rep has sold so far in a fiscal year.
func (l *Ledger) YearToDate(rep, fy string) (int, error) {
	if err := l.checkRep(rep); err != nil {
		return 0, err
	}
	total := 0
	for _, c := range l.credits(rep) {
		if c.deal.Kind == "new" && FiscalYear(c.deal.Closed) == fy {
			total += c.net
		}
	}
	return total, nil
}

// Attainment returns YearToDate as a whole percentage of the rep's quota.
func (l *Ledger) Attainment(rep, fy string) (int, error) {
	sold, err := l.YearToDate(rep, fy)
	if err != nil {
		return 0, err
	}
	quota := l.reps[rep].QuotaCents
	if quota == 0 {
		return 0, nil
	}
	return sold * 100 / quota, nil
}

// WhatIf returns what one more full-price new deal would earn on top of what the
// rep has sold in a fiscal year.
func (l *Ledger) WhatIf(rep, fy string, extra int) (int, error) {
	sold, err := l.YearToDate(rep, fy)
	if err != nil {
		return 0, err
	}
	return DealCommission("new", 0, extra, sold), nil
}

// Forecast returns the commission on planned full-price new deals, closed in that
// order on top of what the rep has sold in a fiscal year.
func (l *Ledger) Forecast(rep, fy string, planned []int) (int, error) {
	sold, err := l.YearToDate(rep, fy)
	if err != nil {
		return 0, err
	}
	total := 0
	for _, net := range planned {
		total += DealCommission("new", 0, net, sold)
		sold += net
	}
	return total, nil
}
`,
			"statement.go": `package commission

import (
	"fmt"
	"sort"
	"strings"
)

const clawbackDays = 90

// Line is one entry on a statement. Deal is the deal's ID and Cents the amount,
// which is what the rep is paid for that line.
type Line struct {
	Deal  string
	Kind  string
	Cents int
}

// Sum adds up the amounts of some lines.
func Sum(lines []Line) int {
	total := 0
	for _, line := range lines {
		total += line.Cents
	}
	return total
}

// clawedBack reports whether a deal's cancellation takes its commission back.
func clawedBack(d Deal) bool {
	return d.Cancelled != "" && DaysBetween(d.Closed, d.Cancelled) <= clawbackDays
}

// Lines returns a rep's statement for a period: the deals closed in it in closing
// order, then the clawbacks that fall in it.
func (l *Ledger) Lines(rep, period string) ([]Line, error) {
	if err := l.checkRep(rep); err != nil {
		return nil, err
	}
	out := []Line{}
	var back []Line
	for _, c := range l.credits(rep) {
		if PeriodOf(c.deal.Closed) == period {
			out = append(out, Line{Deal: c.deal.ID, Kind: c.deal.Kind, Cents: c.cents})
		}
		if clawedBack(*c.deal) && PeriodOf(c.deal.Cancelled) == period {
			back = append(back, Line{Deal: c.deal.ID, Kind: "clawback", Cents: -c.cents})
		}
	}
	return append(append(out, back...), l.adjustmentLines(rep, period)...), nil
}

// Total returns the sum of a rep's lines for a period.
func (l *Ledger) Total(rep, period string) (int, error) {
	lines, err := l.Lines(rep, period)
	if err != nil {
		return 0, err
	}
	return Sum(lines), nil
}

// euros writes an amount the way the statement file wants it.
func euros(cents int) string {
	sign := ""
	if cents < 0 {
		sign, cents = "-", -cents
	}
	return fmt.Sprintf("%s%d,%02d", sign, cents/100, cents%100)
}

// Render returns the statement file for payroll.
func (l *Ledger) Render(rep, period string) (string, error) {
	lines, err := l.Lines(rep, period)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "STATEMENT|%s|%s\n", rep, period)
	for _, line := range lines {
		fmt.Fprintf(&b, "%s|%s|%s\n", line.Deal, line.Kind, euros(line.Cents))
	}
	fmt.Fprintf(&b, "TOTAL|%s\n", euros(Sum(lines)))
	return b.String(), nil
}

// Leaderboard returns every rep's name, highest Total first, ties by name.
func (l *Ledger) Leaderboard(period string) []string {
	out := l.Reps()
	totals := map[string]int{}
	for _, name := range out {
		totals[name], _ = l.Total(name, period)
	}
	sort.SliceStable(out, func(a, b int) bool { return totals[out[a]] > totals[out[b]] })
	return out
}

// TeamTotal returns a manager's own Total plus that of each of their reports.
func (l *Ledger) TeamTotal(manager, period string) (int, error) {
	total, err := l.Total(manager, period)
	if err != nil {
		return 0, err
	}
	for _, name := range l.Reports(manager) {
		own, err := l.Total(name, period)
		if err != nil {
			return 0, err
		}
		total += own
	}
	return total, nil
}

// Accrual returns the sum of every rep's Total for a period.
func (l *Ledger) Accrual(period string) int {
	total := 0
	for _, name := range l.Reps() {
		own, _ := l.Total(name, period)
		total += own
	}
	return total
}

// YearTotal returns the sum of a rep's Total over the periods of a fiscal year.
func (l *Ledger) YearTotal(rep, fy string) (int, error) {
	total := 0
	for _, period := range Periods(fy) {
		own, err := l.Total(rep, period)
		if err != nil {
			return 0, err
		}
		total += own
	}
	return total, nil
}
`,
		},
		Turns: script(
			cloud("Our fiscal year starts on 1 April and is named for the calendar year it starts in, so 15 April 2026 is in FY26 Q1 and 2 January 2027 is in FY26 Q4. Everything to do with periods works that way. Nothing uses periods yet. For now add `func (l *Ledger) RepCount() int` to ledger.go."),
			local("Read docs/comp-plan.txt and tell me in a few lines how a deal's commission is worked out. No code changes."),
			local("Add `func (d Deal) NetCents() int` to deal.go: the list price less the discount, dropping any fraction of a cent."),
			cloud("Add `var ErrUnknownRep = errors.New(\"unknown rep\")` to ledger.go. Every call that names a rep not on the ledger returns it, including all the calls we add later, and that covers every rep a deal names. First one: `func (l *Ledger) AddDeal(d Deal) error`, which records a deal."),
			cloud("Sales ops ruled on split deals. Percentages divide the deal's net value, not its commission, each share drops its fraction of a cent, and any leftover cents go to the first rep listed. Percentages that don't add up to 100 are an error: add `var ErrBadSplit = errors.New(\"bad split\")` to deal.go, nothing returns it yet. Also add `func (l *Ledger) Deals(rep string) ([]Deal, error)`: the deals that rep owns, in closing order, ties by ID."),
			cloud("Payroll sent docs/statement-format.txt. Read it and tell me whether `Line` in statement.go holds what that file needs. No code changes."),
			local("Finance says commission on a deal rounds down to the whole cent, and only once per deal, at the very end of working it out. Nothing computes commission yet. For now add `func (l *Ledger) Deal(id string) (Deal, bool)` to ledger.go: a copy of the deal with that ID."),
			local("Add `var ErrNoDeal = errors.New(\"no such deal\")` and `func (l *Ledger) Cancel(id, date string) error` to ledger.go: records the date in Cancelled. Finance's rule for later: a deal cancelled 90 days or fewer after it closed loses its commission, as a negative line of kind \"clawback\" in the period of the cancellation. Later than that, nothing happens. Other deals are never recalculated."),
			local("Add `func PeriodOf(date string) string` to periods.go: the period a YYYY-MM-DD date falls in, written like FY26Q1. Also `func FiscalYear(date string) string`, written like FY26."),
			local("Add `func DaysBetween(from, to string) int` to periods.go: the number of days from one date to the other."),
			cloud("Add `func Tiered(ytd, net int) int` in a new file rates.go: the commission on a new deal of that net value for a rep who has already sold ytd this fiscal year."),
			cloud("Add `func DealCommission(kind string, discountPct, net, ytd int) int` to rates.go: the commission on any deal, with its kind and discount taken into account."),
			local("Add `func Shares(d Deal) ([]Share, error)` to deal.go: what each rep is credited with for that deal, in the order listed."),
			local("Sales ops again: anything credited to the rep named HOUSE earns no commission, but it still shows on statements, at 0. Nothing computes statements yet. For now add `func (l *Ledger) Quota(rep string) (int, error)` to ledger.go: that rep's QuotaCents."),
			cloud("Add `func (l *Ledger) Earned(rep, fy string) (map[string]int, error)` in a new file earned.go: the commission that rep earned on each deal closed in that fiscal year, by deal ID. fy is written like FY26. Work through deals in closing order, ties by ID."),
			cloud("Add `func (l *Ledger) Lines(rep, period string) ([]Line, error)` to statement.go: the rep's statement for that period. One line per deal credited to the rep and closed in that period, with Kind the deal's kind, in closing order, followed by whatever lines cancellations call for."),
			local("Add `func (l *Ledger) Total(rep, period string) (int, error)` to statement.go: the sum of the rep's lines for that period."),
			local("The comp committee raised the top rate to 10 percent, for everything. The plan document won't be reissued until January. Update `Tiered` in rates.go."),
			local("Add `func (l *Ledger) Render(rep, period string) (string, error)` to statement.go: the statement file that payroll takes."),
			cloud("`Earned` and `Lines` both walk the deals in their own way. Add an unexported helper in earned.go that gives one rep's credited deals, with their share and commission, across all years, and put both on top of it. Move the closing-order sort to ledger.go if it isn't there. No change in what they return."),
			cloud("Add `func (l *Ledger) YearToDate(rep, fy string) (int, error)` to earned.go: the value that counts as sold by that rep so far in that fiscal year."),
			cloud("The finance controller sent docs/adjustments-policy.txt, because manual corrections are coming next. Read it and tell me in a few lines what it allows. No code changes."),
			local("Add `func (l *Ledger) Attainment(rep, fy string) (int, error)` to earned.go: `YearToDate` as a whole percentage of QuotaCents, dropping any fraction, and 0 when the quota is 0."),
			local("Add `func (l *Ledger) Leaderboard(period string) []string` to statement.go: every rep's name, highest `Total` first, ties by name."),
			cloud("Add `func (l *Ledger) WhatIf(rep, fy string, extra int) (int, error)` to earned.go: what one more new deal at full price with net value extra would earn, on top of what the rep has sold in that fiscal year."),
			local("Add `func (l *Ledger) TeamTotal(manager, period string) (int, error)` to statement.go: the manager's own `Total` plus that of each rep whose Manager is that name."),
			local("Add `func PeriodStart(period string) string` to periods.go: the first date, as YYYY-MM-DD, of a period written like FY26Q1. All our years are 20xx."),
			cloud("Add `func (l *Ledger) Forecast(rep, fy string, planned []int) (int, error)` to earned.go: the commission on a list of planned full-price new deals, given by net value, closed one after the other in that order on top of what the rep has sold in that fiscal year."),
			local("Add `func (l *Ledger) Accrual(period string) int` to statement.go: the sum of every rep's `Total` for that period."),
			local("Add `var ErrBadAdjustment = errors.New(\"bad adjustment\")` and `func (l *Ledger) Adjust(rep, period string, cents int, reason string) error` in a new file adjust.go: records a manual correction on the rep's statement for that period, and refuses with that error whatever may not be made."),
			cloud("`Lines` should now show adjustments too, after everything else for that period, in the order they were made, with Kind \"adjustment\" and the reason in Deal. Keep the code for that in adjust.go."),
			local("Add `func Periods(fy string) []string` to periods.go: the four periods of that fiscal year in order, and `func (l *Ledger) YearTotal(rep, fy string) (int, error)` to statement.go: the sum of the rep's `Total` over them."),
		),
		Carries: []Carry{
			{
				What:        "the fiscal year starts on 1 April and is named for the year it starts in",
				Kind:        CarryUser,
				Established: 0,
				Needed:      []int{8, 14, 20, 26},
				Tests:       []string{"TestPeriodOfStartOfYear", "TestPeriodOfQuarterEdges", "TestPeriodOfEarlyInCalendarYear", "TestYearToDateFollowsFiscalYear", "TestBandsRestartWithFiscalYear", "TestPeriodStartDates"},
				Absent:      []string{"april", "calendar year", "fiscal year starts"},
			},
			{
				What:        "commission bands apply by slice, renewals are flat and outside them, and a discount over 20 percent pays half (docs/comp-plan.txt)",
				Kind:        CarryDiscovered,
				Established: 1,
				Needed:      []int{10, 11, 14, 20, 24},
				Source:      "docs/comp-plan.txt",
				Tests: []string{
					"TestTieredMiddleBand", "TestTieredAcrossBands", "TestTieredAllBands", "TestRenewalFlat", "TestRenewalAddsNothingSold", "TestYearToDateLeavesOutRenewals",
					"TestHeavyDiscountPaysHalf", "TestDiscountAtTheLineIsFull", "TestHalfRoundsDown", "TestHeavyDiscountStillCounts", "TestForecastBuildsUp",
					"TestClawbackDoesNotRecalculate",
				},
				Absent: []string{"50,000", "100,000", "150,000", "8 percent", "2 percent", "20 percent"},
			},
			{
				What:        "every call naming a missing rep returns ErrUnknownRep",
				Kind:        CarryEmbodied,
				Established: 3,
				Needed:      []int{4, 13, 14, 16, 22, 25, 29},
				Tests: []string{
					"TestAddDealUnknownRep", "TestAddDealUnknownSplitRep", "TestDealsUnknownRep", "TestQuotaUnknownRep", "TestEarnedUnknownRep",
					"TestLinesUnknownRep", "TestTotalUnknownRep", "TestAttainmentUnknownRep", "TestTeamTotalUnknownRep", "TestAdjustUnknownRep",
				},
				Absent: []string{"errunknownrep", "unknown rep", "not on the ledger"},
			},
			{
				What:        "leftover cents of a split go to the first rep, and percentages must add up to 100",
				Kind:        CarryUser,
				Established: 4,
				Needed:      []int{12},
				Tests:       []string{"TestSharesLeftoverToFirst", "TestSharesBadTotal"},
				Absent:      []string{"leftover", "left over", "errbadsplit", "add up to 100"},
			},
			{
				What:        "the statement file's fields, decimal comma and total line (docs/statement-format.txt)",
				Kind:        CarryDiscovered,
				Established: 5,
				Needed:      []int{18},
				Source:      "docs/statement-format.txt",
				Tests:       []string{"TestRenderWholeFile", "TestRenderSmallAmount", "TestRenderNegativeAmount"},
				Absent:      []string{"decimal comma", "statement|", "total|", "statement-format"},
			},
			{
				What:        "commission rounds down, once per deal, at the end",
				Kind:        CarryUser,
				Established: 6,
				Needed:      []int{10, 11, 27},
				Tests:       []string{"TestTieredRoundsDown", "TestTieredRoundsOnce", "TestHalfRoundsDown", "TestForecastRoundsPerDeal"},
				Absent:      []string{"rounds down", "round down", "whole cent"},
			},
			{
				What:        "a deal cancelled within 90 days gets a negative clawback line in the period of the cancellation, and nothing is recalculated",
				Kind:        CarryUser,
				Established: 7,
				Needed:      []int{15},
				Tests: []string{
					"TestClawbackLine", "TestClawbackDayNinety", "TestNoClawbackDayNinetyOne", "TestClawbackLeavesItsOwnPeriodAlone",
					"TestClawbackDoesNotRecalculate", "TestTotalTakesOffClawback", "TestRenderNegativeAmount",
				},
				Absent: []string{"clawback", "90 days", "day 90", "recalculated"},
			},
			{
				What:        "the rep named HOUSE earns nothing but stays on statements",
				Kind:        CarryUser,
				Established: 13,
				Needed:      []int{14, 15},
				Tests:       []string{"TestHouseEarnsNothing", "TestHouseStaysOnStatement"},
				Absent:      []string{"house"},
			},
			{
				What:        "the top rate went up from the plan document's 8 percent to 10 percent, though the document still says otherwise",
				Kind:        CarryUser,
				Established: 17,
				Needed:      []int{24, 27},
				Tests:       []string{"TestTieredAllBands", "TestTieredTopBand", "TestEarnedTopBand", "TestWhatIfTopBand", "TestForecastTopBand"},
				Absent:      []string{"10 percent", "reissued"},
			},
			{
				What:        "what makes an adjustment refused: its reason, a zero amount, or more than 1,000.00 either way (docs/adjustments-policy.txt)",
				Kind:        CarryDiscovered,
				Established: 21,
				Needed:      []int{29},
				Source:      "docs/adjustments-policy.txt",
				Tests:       []string{"TestAdjustZeroRefused", "TestAdjustAmountLimit", "TestAdjustReasonLength"},
				Absent:      []string{"40 characters", "1,000.00", "zero"},
			},
		},
	}
}
