package s2

func task15() Task {
	return Task{
		ID:     "s2-15-shipping-rates",
		Set:    "tuning",
		System: system("a shipping cost calculator"),
		Files: map[string]string{
			"go.mod": goMod,
			"parcel.go": `package shipping

// Parcel is one item to ship.
type Parcel struct {
	Weight int
	Zone   string
}

// Valid reports whether the parcel has a weight and a zone.
func (p Parcel) Valid() bool { return p.Weight > 0 && p.Zone != "" }
`,
			"rates.go": `package shipping

// ratePerKilo is the price per kilo for each zone.
var ratePerKilo = map[string]int{"A": 450, "B": 700, "C": 1100}
`,
			"docs/zones.txt": `Zones

A is domestic, B is neighbouring countries, C is overseas.
Overseas parcels carry a customs surcharge of 12.00 each. A and B have none.
A parcel with a zone code we don't recognise is charged as overseas, never
rejected.
`,
			"parcel_test.go": `package shipping

import "testing"

func TestValid(t *testing.T) {
	if !(Parcel{Weight: 1, Zone: "A"}).Valid() || (Parcel{Zone: "A"}).Valid() {
		t.Fatal("wrong answer")
	}
}
`,
		},
		Hidden: map[string]string{hiddenName: `package shipping_test

import (
	"errors"
	"testing"

	shipping "fixture"
)

// A weight of 1 bills as one kilo whatever unit a weight is read in, which keeps
// the tests that use it about something else.
func oneIn(zone string) shipping.Parcel { return shipping.Parcel{Weight: 1, Zone: zone} }

func TestTotalWeight(t *testing.T) {
	if got := shipping.TotalWeight([]shipping.Parcel{{Weight: 3}, {Weight: 4}}); got != 7 {
		t.Fatalf("TotalWeight = %d", got)
	}
}

func TestZones(t *testing.T) {
	got := shipping.Zones()
	if len(got) != 3 || got[0] != "A" || got[2] != "C" {
		t.Fatalf("Zones = %v", got)
	}
}

func TestRateFor(t *testing.T) {
	if r, ok := shipping.RateFor("B"); !ok || r != 700 {
		t.Errorf("RateFor(B) = %d, %v", r, ok)
	}
	if _, ok := shipping.RateFor("Q"); ok {
		t.Error("RateFor(Q) found a rate")
	}
}

func TestBillableKilos(t *testing.T) {
	for weight, want := range map[int]int{1: 1, 1000: 1, 1001: 2, 2500: 3} {
		if got := shipping.BillableKilos(weight); got != want {
			t.Errorf("BillableKilos(%d) = %d", weight, got)
		}
	}
}

func TestQuotePerStartedKilo(t *testing.T) {
	got, err := shipping.Quote(shipping.Parcel{Weight: 1500, Zone: "A"})
	if err != nil || got != 900 {
		t.Fatalf("Quote = %d, %v", got, err)
	}
}

func TestQuoteAtLimit(t *testing.T) {
	got, err := shipping.Quote(shipping.Parcel{Weight: 30000, Zone: "A"})
	if err != nil || got != 13500 {
		t.Fatalf("Quote = %d, %v", got, err)
	}
}

func TestAcceptsInGrams(t *testing.T) {
	if !shipping.Accepts(30000) {
		t.Fatal("30000 was refused")
	}
}

func TestSurchargeZones(t *testing.T) {
	for zone, want := range map[string]int{"A": 0, "B": 0, "C": 1200} {
		if got := shipping.Surcharge(zone); got != want {
			t.Errorf("Surcharge(%s) = %d", zone, got)
		}
	}
}

func TestQuoteOverseasSurcharge(t *testing.T) {
	got, err := shipping.Quote(oneIn("C"))
	if err != nil || got != 2300 {
		t.Fatalf("Quote = %d, %v", got, err)
	}
}

func TestQuoteUnknownZone(t *testing.T) {
	got, err := shipping.Quote(oneIn("Q"))
	if err != nil || got != 2300 {
		t.Fatalf("Quote = %d, %v", got, err)
	}
}

func TestQuotePlainZone(t *testing.T) {
	got, err := shipping.Quote(oneIn("B"))
	if err != nil || got != 700 {
		t.Fatalf("Quote = %d, %v", got, err)
	}
}

func TestQuoteTooHeavy(t *testing.T) {
	if _, err := shipping.Quote(shipping.Parcel{Weight: 30001, Zone: "A"}); !errors.Is(err, shipping.ErrTooHeavy) {
		t.Fatalf("err = %v", err)
	}
}

func TestOrderPropagatesTooHeavy(t *testing.T) {
	_, err := shipping.OrderShipping(100, []shipping.Parcel{oneIn("A"), {Weight: 30001, Zone: "A"}})
	if !errors.Is(err, shipping.ErrTooHeavy) {
		t.Fatalf("err = %v", err)
	}
}

func TestAcceptsRefusesOver(t *testing.T) {
	if shipping.Accepts(30001) {
		t.Fatal("30001 was accepted")
	}
}

func TestOrderSums(t *testing.T) {
	got, err := shipping.OrderShipping(0, []shipping.Parcel{oneIn("A"), oneIn("B")})
	if err != nil || got != 1150 {
		t.Fatalf("OrderShipping = %d, %v", got, err)
	}
}

func TestOrderChargedBelowThreshold(t *testing.T) {
	for _, order := range []int{6000, 7499} {
		got, err := shipping.OrderShipping(order, []shipping.Parcel{oneIn("A")})
		if err != nil || got != 450 {
			t.Errorf("order %d: %d, %v", order, got, err)
		}
	}
}

func TestOrderFreeAtThreshold(t *testing.T) {
	for _, order := range []int{7500, 20000} {
		got, err := shipping.OrderShipping(order, []shipping.Parcel{oneIn("A")})
		if err != nil || got != 0 {
			t.Errorf("order %d: %d, %v", order, got, err)
		}
	}
}
`},
		Solution: map[string]string{
			"parcel.go": `package shipping

// Parcel is one item to ship. Weight is in grams.
type Parcel struct {
	Weight int
	Zone   string
}

// Valid reports whether the parcel has a weight and a zone.
func (p Parcel) Valid() bool { return p.Weight > 0 && p.Zone != "" }

// TotalWeight sums the weights of the parcels.
func TotalWeight(parcels []Parcel) int {
	total := 0
	for _, p := range parcels {
		total += p.Weight
	}
	return total
}
`,
			"rates.go": `package shipping

import (
	"errors"
	"sort"
)

// ErrTooHeavy refuses a parcel carriers will not take.
var ErrTooHeavy = errors.New("parcel too heavy")

const (
	gramsPerKilo = 1000
	maxWeight    = 30000
	freeFrom     = 7500
)

// ratePerKilo is the price per kilo for each zone.
var ratePerKilo = map[string]int{"A": 450, "B": 700, "C": 1100}

// Zones returns the zone codes, sorted.
func Zones() []string {
	out := make([]string, 0, len(ratePerKilo))
	for z := range ratePerKilo {
		out = append(out, z)
	}
	sort.Strings(out)
	return out
}

// RateFor returns the rate for a zone, if we have one.
func RateFor(zone string) (int, bool) {
	r, ok := ratePerKilo[zone]
	return r, ok
}

// BillableKilos returns the kilos charged for a weight in grams.
func BillableKilos(weight int) int {
	return (weight + gramsPerKilo - 1) / gramsPerKilo
}

// Surcharge returns the extra charge per parcel for a zone.
func Surcharge(zone string) int {
	if zone == "A" || zone == "B" {
		return 0
	}
	return 1200
}

// Accepts reports whether carriers will take a parcel of that weight.
func Accepts(weight int) bool { return weight <= maxWeight }

// Quote returns the shipping price of one parcel.
func Quote(p Parcel) (int, error) {
	if !Accepts(p.Weight) {
		return 0, ErrTooHeavy
	}
	rate, ok := RateFor(p.Zone)
	if !ok {
		rate = ratePerKilo["C"]
	}
	return rate*BillableKilos(p.Weight) + Surcharge(p.Zone), nil
}

// OrderShipping returns what an order pays for shipping.
func OrderShipping(orderCents int, parcels []Parcel) (int, error) {
	total := 0
	for _, p := range parcels {
		q, err := Quote(p)
		if err != nil {
			return 0, err
		}
		total += q
	}
	if orderCents >= freeFrom {
		return 0, nil
	}
	return total, nil
}
`,
		},
		Turns: script(
			cloud("Units for this package, so there are no surprises: weights are whole grams and money is whole cents, everywhere. To begin, add `func TotalWeight(parcels []Parcel) int`."),
			cloud("A commercial term we'll need for order totals: an order of 50.00 or more ships free. Nothing to code yet. Add `func Zones() []string`: the zone codes in ratePerKilo, sorted."),
			local("Read docs/zones.txt and summarise it. No code changes."),
			local("Add `func BillableKilos(weight int) int`: the number of kilos we charge for a weight. Every started kilo counts as a whole one."),
			local("Commercial moved the threshold for shipping free: an order now has to reach 75.00. Also add `func RateFor(zone string) (int, bool)`, the rate for a zone, if we have one."),
			cloud("Add `func Surcharge(zone string) int`, the extra charge per parcel for a zone."),
			cloud("Carriers refuse parcels over 30 kg, so add `var ErrTooHeavy = errors.New(\"parcel too heavy\")` for that. We'll use it in a moment."),
			local("Add `func Quote(p Parcel) (int, error)`, the shipping price of one parcel: the zone's rate for its billable kilos plus any surcharge."),
			cloud("Add `func OrderShipping(orderCents int, parcels []Parcel) (int, error)`: what an order pays for shipping, based on Quote for each of its parcels."),
			local("Add `func Accepts(weight int) bool`: whether carriers will take a parcel of that weight."),
		),
		Carries: []Carry{
			{
				What:        "weights are whole grams",
				Kind:        CarryUser,
				Established: 0,
				Needed:      []int{3, 7, 9},
				Tests:       []string{"TestBillableKilos", "TestQuotePerStartedKilo", "TestQuoteAtLimit", "TestAcceptsInGrams"},
				Absent:      []string{"gram"},
			},
			{
				What:        "orders of 75.00 or more ship free, revised from 50.00",
				Kind:        CarryUser,
				Established: 4,
				Needed:      []int{8},
				Tests:       []string{"TestOrderChargedBelowThreshold", "TestOrderFreeAtThreshold"},
				Absent:      []string{"75.00", "50.00", "free", "threshold"},
			},
			{
				What:        "overseas parcels carry 12.00 and unknown zones count as overseas (docs/zones.txt)",
				Kind:        CarryDiscovered,
				Established: 2,
				Needed:      []int{5, 7},
				Source:      "docs/zones.txt",
				Tests:       []string{"TestSurchargeZones", "TestQuoteOverseasSurcharge", "TestQuoteUnknownZone"},
				Absent:      []string{"overseas", "customs", "12.00"},
			},
			{
				What:        "parcels over 30 kg are refused with ErrTooHeavy",
				Kind:        CarryUser,
				Established: 6,
				Needed:      []int{7, 8, 9},
				Tests:       []string{"TestQuoteTooHeavy", "TestOrderPropagatesTooHeavy", "TestAcceptsRefusesOver"},
				Absent:      []string{"30 kg", "errtooheavy"},
			},
		},
	}
}
