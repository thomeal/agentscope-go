package s2

func accA06() Task {
	return Task{
		ID:     "s2-a06-rent-ledger",
		Set:    "acceptance",
		System: system("a tenant rent ledger"),
		Files: map[string]string{
			"go.mod": goMod,
			"ledger.go": `package rent

// Entry is one posting on a tenant's ledger, in cents. Cents is never below 0
// and Kind is charge, payment or deposit.
type Entry struct {
	Tenant string
	Kind   string
	Cents  int
}

// Ledger holds every posting in the order it was made.
type Ledger struct {
	entries []Entry
}

// Post records an entry.
func (l *Ledger) Post(e Entry) { l.entries = append(l.entries, e) }
`,
			"docs/lease.txt": `Standard lease terms

Rent is due on the 1st. Paid on or before the 5th there is no fee. From the 6th on
the late fee is a flat 4000 cents.

The deposit is held in trust and is not rent. It never counts toward what a tenant
has paid or owes.

Tenants give 60 days notice before moving out.
`,
			"ledger_test.go": `package rent

import "testing"

func TestPost(t *testing.T) {
	var l Ledger
	l.Post(Entry{Tenant: "ann", Kind: "charge", Cents: 100})
	if len(l.entries) != 1 || l.entries[0].Tenant != "ann" {
		t.Fatalf("entries = %+v", l.entries)
	}
}
`,
		},
		Hidden: map[string]string{hiddenName: `package rent_test

import (
	"errors"
	"reflect"
	"testing"
	"time"

	rent "fixture"
)

func post(l *rent.Ledger, tenant, kind string, cents int) {
	l.Post(rent.Entry{Tenant: tenant, Kind: kind, Cents: cents})
}

func balance(t *testing.T, l *rent.Ledger, tenant string) int {
	t.Helper()
	got, err := l.Balance(tenant)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func date(year int, month time.Month, day int) time.Time {
	return time.Date(year, month, day, 12, 0, 0, 0, time.UTC)
}

func TestCountPostings(t *testing.T) {
	l := &rent.Ledger{}
	post(l, "ann", "charge", 1)
	post(l, "bob", "payment", 1)
	if got := l.Count(); got != 2 {
		t.Fatalf("Count = %d", got)
	}
}

func TestBalanceCreditIsPositive(t *testing.T) {
	l := &rent.Ledger{}
	post(l, "ann", "payment", 500)
	if got := balance(t, l, "ann"); got != 500 {
		t.Fatalf("Balance = %d", got)
	}
}

func TestBalanceArrearsAreNegative(t *testing.T) {
	l := &rent.Ledger{}
	post(l, "ann", "charge", 700)
	post(l, "ann", "payment", 200)
	if got := balance(t, l, "ann"); got != -500 {
		t.Fatalf("Balance = %d", got)
	}
}

func TestBalanceSettled(t *testing.T) {
	l := &rent.Ledger{}
	post(l, "ann", "charge", 500)
	post(l, "ann", "payment", 500)
	post(l, "bob", "charge", 900)
	if got := balance(t, l, "ann"); got != 0 {
		t.Fatalf("Balance = %d", got)
	}
}

func TestBalanceUnknownSentinel(t *testing.T) {
	if _, err := (&rent.Ledger{}).Balance("ghost"); !errors.Is(err, rent.ErrUnknownTenant) {
		t.Fatalf("err = %v", err)
	}
}

func TestHeldUnknownSentinel(t *testing.T) {
	if _, err := (&rent.Ledger{}).Held("ghost"); !errors.Is(err, rent.ErrUnknownTenant) {
		t.Fatalf("err = %v", err)
	}
}

func TestLateFeeFreeDays(t *testing.T) {
	for _, day := range []int{1, 3, 5} {
		if got := rent.LateFee(day); got != 0 {
			t.Errorf("day %d: %d", day, got)
		}
	}
}

func TestLateFeeAmount(t *testing.T) {
	for _, day := range []int{6, 20, 31} {
		if got := rent.LateFee(day); got != 4000 {
			t.Errorf("day %d: %d", day, got)
		}
	}
}

func TestBalanceIgnoresDeposit(t *testing.T) {
	l := &rent.Ledger{}
	post(l, "ann", "charge", 500)
	post(l, "ann", "payment", 500)
	post(l, "ann", "deposit", 9000)
	if got := balance(t, l, "ann"); got != 0 {
		t.Fatalf("Balance = %d", got)
	}
}

func TestInArrearsIgnoresDeposit(t *testing.T) {
	l := &rent.Ledger{}
	post(l, "ann", "charge", 1000)
	post(l, "ann", "deposit", 1000)
	if got := l.InArrears(); !reflect.DeepEqual(got, []string{"ann"}) {
		t.Fatalf("InArrears = %v", got)
	}
}

func TestHeldSums(t *testing.T) {
	l := &rent.Ledger{}
	post(l, "ann", "deposit", 2000)
	post(l, "ann", "deposit", 500)
	post(l, "ann", "payment", 7)
	post(l, "bob", "charge", 100)
	got, err := l.Held("ann")
	if err != nil {
		t.Fatal(err)
	}
	if got != 2500 {
		t.Fatalf("Held = %d", got)
	}
	got, err = l.Held("bob")
	if err != nil {
		t.Fatal(err)
	}
	if got != 0 {
		t.Fatalf("Held = %d", got)
	}
}

func TestTenantsSorted(t *testing.T) {
	l := &rent.Ledger{}
	post(l, "cal", "charge", 1)
	post(l, "ann", "charge", 1)
	post(l, "cal", "payment", 1)
	post(l, "bob", "deposit", 1)
	if got := l.Tenants(); !reflect.DeepEqual(got, []string{"ann", "bob", "cal"}) {
		t.Fatalf("Tenants = %v", got)
	}
}

func TestInArrearsListsDebtors(t *testing.T) {
	l := &rent.Ledger{}
	post(l, "dee", "charge", 5)
	post(l, "ann", "charge", 100)
	post(l, "bob", "charge", 100)
	post(l, "bob", "payment", 100)
	post(l, "cal", "payment", 50)
	if got := l.InArrears(); !reflect.DeepEqual(got, []string{"ann", "dee"}) {
		t.Fatalf("InArrears = %v", got)
	}
}

func TestProrateFlatMonth(t *testing.T) {
	if got := rent.Prorate(3000, date(2026, time.February, 15)); got != 1600 {
		t.Errorf("Feb 15: %d, want 1600", got)
	}
	if got := rent.Prorate(3000, date(2026, time.May, 16)); got != 1500 {
		t.Errorf("May 16: %d, want 1500", got)
	}
}

func TestProrateChargesMoveInDay(t *testing.T) {
	if got := rent.Prorate(3000, date(2026, time.April, 1)); got != 3000 {
		t.Errorf("Apr 1: %d", got)
	}
	if got := rent.Prorate(3000, date(2026, time.April, 30)); got != 100 {
		t.Errorf("Apr 30: %d", got)
	}
}
`},
		Solution: map[string]string{
			"ledger.go": `package rent

import (
	"errors"
	"sort"
)

// ErrUnknownTenant is for any call that names a tenant with no postings.
var ErrUnknownTenant = errors.New("unknown tenant")

// Entry is one posting on a tenant's ledger, in cents. Cents is never below 0
// and Kind is charge, payment or deposit.
type Entry struct {
	Tenant string
	Kind   string
	Cents  int
}

// Ledger holds every posting in the order it was made.
type Ledger struct {
	entries []Entry
}

// Post records an entry.
func (l *Ledger) Post(e Entry) { l.entries = append(l.entries, e) }

// Count returns the number of postings.
func (l *Ledger) Count() int { return len(l.entries) }

// Tenants returns every tenant with a posting, once each, sorted by name.
func (l *Ledger) Tenants() []string {
	seen := map[string]bool{}
	var out []string
	for _, e := range l.entries {
		if !seen[e.Tenant] {
			seen[e.Tenant] = true
			out = append(out, e.Tenant)
		}
	}
	sort.Strings(out)
	return out
}

func (l *Ledger) standing(tenant string) int {
	total := 0
	for _, e := range l.entries {
		if e.Tenant != tenant {
			continue
		}
		switch e.Kind {
		case "payment":
			total += e.Cents
		case "charge":
			total -= e.Cents
		}
	}
	return total
}

// Balance returns where a tenant stands: money in their favour is positive.
func (l *Ledger) Balance(tenant string) (int, error) {
	if !l.known(tenant) {
		return 0, ErrUnknownTenant
	}
	return l.standing(tenant), nil
}

func (l *Ledger) known(tenant string) bool {
	for _, e := range l.entries {
		if e.Tenant == tenant {
			return true
		}
	}
	return false
}

// Held returns how much deposit money we hold for a tenant.
func (l *Ledger) Held(tenant string) (int, error) {
	if !l.known(tenant) {
		return 0, ErrUnknownTenant
	}
	total := 0
	for _, e := range l.entries {
		if e.Tenant == tenant && e.Kind == "deposit" {
			total += e.Cents
		}
	}
	return total, nil
}

// InArrears returns the tenants who owe us money, sorted by name.
func (l *Ledger) InArrears() []string {
	var out []string
	for _, tenant := range l.Tenants() {
		if l.standing(tenant) < 0 {
			out = append(out, tenant)
		}
	}
	return out
}
`,
			"fees.go": `package rent

const (
	lastFreeDay = 5
	lateCents   = 4000
)

// LateFee returns the late fee for rent paid on that day of the month.
func LateFee(paidDay int) int {
	if paidDay <= lastFreeDay {
		return 0
	}
	return lateCents
}
`,
			"prorate.go": `package rent

import "time"

const flatMonth = 30

// Prorate returns the rent for the first part month after moving in.
func Prorate(monthlyCents int, moveIn time.Time) int {
	days := flatMonth - moveIn.Day() + 1
	return monthlyCents * days / flatMonth
}
`,
		},
		Turns: script(
			cloud("The tenant portal shows balances from the tenant's point of view and this package will do the same: money in their favour is positive, arrears are negative. No balance code yet. Add `func (l *Ledger) Count() int`, the number of postings."),
			local("Add `var ErrUnknownTenant = errors.New(\"unknown tenant\")` to ledger.go. Nothing returns it yet. It's for any call that names a tenant with no postings at all."),
			cloud("Read docs/lease.txt and list what it says about money. No code changes."),
			local("Add `func (l *Ledger) Balance(tenant string) (int, error)`: where that tenant stands right now."),
			local("Add `func LateFee(paidDay int) int` in a new file fees.go: the late fee in cents for rent paid on that day of the month."),
			cloud("Add `func (l *Ledger) Held(tenant string) (int, error)`: how much deposit money we're holding for that tenant, the sum of their deposit postings."),
			local("Owner's call on move-ins: a part month is charged on a flat 30-day month whatever the calendar says, and the move-in day itself is charged. Nothing charges part months yet. Add `func (l *Ledger) Tenants() []string`: every tenant with a posting, once each, sorted by name."),
			cloud("Add `func Prorate(monthlyCents int, moveIn time.Time) int` in a new file prorate.go: the rent for the first part month of someone moving in on that date."),
			local("Add `func (l *Ledger) InArrears() []string`: the tenants who owe us money, sorted by name."),
		),
		Carries: []Carry{
			{
				What:        "a balance is from the tenant's point of view: credit positive, arrears negative",
				Kind:        CarryUser,
				Established: 0,
				Needed:      []int{3},
				Tests:       []string{"TestBalanceCreditIsPositive", "TestBalanceArrearsAreNegative"},
				Absent:      []string{"point of view", "favour", "positive", "negative"},
			},
			{
				What:        "ErrUnknownTenant for any call naming a tenant with no postings",
				Kind:        CarryEmbodied,
				Established: 1,
				Needed:      []int{3, 5},
				Tests:       []string{"TestBalanceUnknownSentinel", "TestHeldUnknownSentinel"},
				Absent:      []string{"errunknowntenant", "unknown tenant", "no postings"},
			},
			{
				What:        "no fee through the 5th, a flat 4000 cents after (docs/lease.txt)",
				Kind:        CarryDiscovered,
				Established: 2,
				Needed:      []int{4},
				Source:      "docs/lease.txt",
				Tests:       []string{"TestLateFeeFreeDays", "TestLateFeeAmount"},
				Absent:      []string{"4000", "5th", "6th"},
			},
			{
				What:        "deposits never count toward a balance (docs/lease.txt)",
				Kind:        CarryDiscovered,
				Established: 2,
				Needed:      []int{3, 8},
				Source:      "docs/lease.txt",
				Tests:       []string{"TestBalanceIgnoresDeposit", "TestInArrearsIgnoresDeposit"},
				Absent:      []string{"in trust", "not rent"},
			},
			{
				What:        "part months use a flat 30-day month and charge the move-in day",
				Kind:        CarryUser,
				Established: 6,
				Needed:      []int{7},
				Tests:       []string{"TestProrateFlatMonth", "TestProrateChargesMoveInDay"},
				Absent:      []string{"30-day", "calendar", "move-in day"},
			},
		},
	}
}
