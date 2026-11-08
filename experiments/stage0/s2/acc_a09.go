package s2

func accA09() Task {
	return Task{
		ID:     "s2-a09-event-ticketing",
		Set:    "acceptance",
		System: system("an event ticketing system"),
		Files: map[string]string{
			"go.mod": goMod,
			"event.go": `package tickets

// Event is one show. A seat code is one row letter and a number, like B12. The
// sold map goes from seat code to buyer.
type Event struct {
	Name string
	sold map[string]string
}

// NewEvent creates an event with no seats sold.
func NewEvent(name string) *Event { return &Event{Name: name, sold: map[string]string{}} }

// Buyer returns who bought a seat.
func (e *Event) Buyer(seat string) (string, bool) {
	b, ok := e.sold[seat]
	return b, ok
}
`,
			"docs/venue.txt": `Venue agreement, box office summary

No single buyer may end up with more than 6 seats at one event. The fire marshal
reads our sales reports.

Doors open an hour before the show. The bar closes at the interval.
`,
			"event_test.go": `package tickets

import "testing"

func TestBuyer(t *testing.T) {
	e := NewEvent("gala")
	e.sold["C3"] = "ann"
	if b, ok := e.Buyer("C3"); !ok || b != "ann" {
		t.Fatalf("Buyer = %q, %v", b, ok)
	}
	if _, ok := e.Buyer("C4"); ok {
		t.Fatal("an unsold seat has a buyer")
	}
}
`,
		},
		Hidden: map[string]string{hiddenName: `package tickets_test

import (
	"errors"
	"reflect"
	"sort"
	"strconv"
	"testing"

	tickets "fixture"
)

func event(t *testing.T, seats ...string) *tickets.Event {
	t.Helper()
	e := tickets.NewEvent("gala")
	for i, seat := range seats {
		if err := e.Sell(seat, "buyer"+strconv.Itoa(i)); err != nil {
			t.Fatal(err)
		}
	}
	return e
}

func TestCountSold(t *testing.T) {
	if got := event(t, "C1", "C2").Count(); got != 2 {
		t.Fatalf("Count = %d", got)
	}
}

func TestSellRecords(t *testing.T) {
	e := tickets.NewEvent("gala")
	if err := e.Sell("C1", "ann"); err != nil {
		t.Fatal(err)
	}
	if b, ok := e.Buyer("C1"); !ok || b != "ann" {
		t.Fatalf("Buyer = %q, %v", b, ok)
	}
}

func TestSellTaken(t *testing.T) {
	e := event(t, "C1")
	if err := e.Sell("C1", "ann"); !errors.Is(err, tickets.ErrSeatTaken) {
		t.Fatalf("err = %v", err)
	}
}

func sixForAnn(t *testing.T) *tickets.Event {
	t.Helper()
	e := tickets.NewEvent("gala")
	for i := 1; i <= 6; i++ {
		if err := e.Sell("A"+strconv.Itoa(i), "ann"); err != nil {
			t.Fatalf("seat %d: %v", i, err)
		}
	}
	return e
}

func TestSellBuyerLimit(t *testing.T) {
	e := sixForAnn(t)
	if err := e.Sell("B1", "ann"); !errors.Is(err, tickets.ErrBuyerLimit) {
		t.Fatalf("err = %v", err)
	}
	if err := e.Sell("B1", "bob"); err != nil {
		t.Fatal(err)
	}
}

func TestConfirmBuyerLimit(t *testing.T) {
	e := sixForAnn(t)
	err := e.Hold("B1", "ann", 1000)
	if err == nil {
		err = e.Confirm("B1", "ann", 1001)
	}
	if !errors.Is(err, tickets.ErrBuyerLimit) {
		t.Fatalf("err = %v", err)
	}
	if _, sold := e.Buyer("B1"); sold {
		t.Fatal("a seventh seat was sold")
	}
}

func TestSoldHoldsEverySeat(t *testing.T) {
	got := event(t, "B2", "A10", "A2").Sold()
	sort.Strings(got)
	if !reflect.DeepEqual(got, []string{"A10", "A2", "B2"}) {
		t.Fatalf("Sold = %v", got)
	}
}

func TestSoldRowThenNumber(t *testing.T) {
	got := event(t, "B2", "A10", "B1", "A2").Sold()
	if !reflect.DeepEqual(got, []string{"A2", "A10", "B1", "B2"}) {
		t.Fatalf("Sold = %v", got)
	}
}

func TestSoldNumbersPastNine(t *testing.T) {
	got := event(t, "C100", "C11", "C9").Sold()
	if !reflect.DeepEqual(got, []string{"C9", "C11", "C100"}) {
		t.Fatalf("Sold = %v", got)
	}
}

func TestHoldBlocksOthers(t *testing.T) {
	e := tickets.NewEvent("gala")
	if err := e.Hold("C1", "ann", 1000); err != nil {
		t.Fatal(err)
	}
	if err := e.Hold("C1", "bob", 1001); err == nil {
		t.Fatal("two buyers hold one seat")
	}
}

func TestHoldSoldSentinel(t *testing.T) {
	if err := event(t, "C1").Hold("C1", "ann", 1000); !errors.Is(err, tickets.ErrSeatTaken) {
		t.Fatalf("err = %v", err)
	}
}

func TestHoldHeldSentinel(t *testing.T) {
	e := tickets.NewEvent("gala")
	if err := e.Hold("C1", "ann", 1000); err != nil {
		t.Fatal(err)
	}
	if err := e.Hold("C1", "bob", 1001); !errors.Is(err, tickets.ErrSeatTaken) {
		t.Fatalf("err = %v", err)
	}
}

func TestHoldRunsOutAfterTenMinutes(t *testing.T) {
	e := tickets.NewEvent("gala")
	if err := e.Hold("C1", "ann", 1000); err != nil {
		t.Fatal(err)
	}
	if err := e.Hold("C1", "bob", 1599); err == nil {
		t.Fatal("the hold ran out early")
	}
	if err := e.Hold("C1", "bob", 1600); err != nil {
		t.Fatalf("the seat is not free at 600 seconds: %v", err)
	}
}

func TestConfirmSells(t *testing.T) {
	e := tickets.NewEvent("gala")
	if err := e.Hold("C1", "ann", 1000); err != nil {
		t.Fatal(err)
	}
	if err := e.Confirm("C1", "ann", 1001); err != nil {
		t.Fatal(err)
	}
	if b, ok := e.Buyer("C1"); !ok || b != "ann" {
		t.Fatalf("Buyer = %q, %v", b, ok)
	}
}

func TestConfirmWithoutHold(t *testing.T) {
	e := tickets.NewEvent("gala")
	if err := e.Hold("C1", "ann", 1000); err != nil {
		t.Fatal(err)
	}
	if err := e.Confirm("C1", "bob", 1001); !errors.Is(err, tickets.ErrNoHold) {
		t.Errorf("another buyer: %v", err)
	}
	if err := e.Confirm("C2", "ann", 1001); !errors.Is(err, tickets.ErrNoHold) {
		t.Errorf("another seat: %v", err)
	}
}

func TestConfirmAfterHoldRanOut(t *testing.T) {
	e := tickets.NewEvent("gala")
	if err := e.Hold("C1", "ann", 1000); err != nil {
		t.Fatal(err)
	}
	if err := e.Confirm("C1", "ann", 1600); !errors.Is(err, tickets.ErrNoHold) {
		t.Fatalf("err = %v", err)
	}
	if _, sold := e.Buyer("C1"); sold {
		t.Fatal("the seat was sold")
	}
}

func TestReleaseFreesSeat(t *testing.T) {
	e := event(t, "C1")
	if err := e.Release("C1"); err != nil {
		t.Fatal(err)
	}
	if err := e.Sell("C1", "ann"); err != nil {
		t.Fatal(err)
	}
	if err := e.Release("C9"); !errors.Is(err, tickets.ErrNotSold) {
		t.Fatalf("err = %v", err)
	}
}

func TestPriceLate(t *testing.T) {
	for _, days := range []int{0, 10, 29} {
		if got := tickets.Price(1000, days); got != 1000 {
			t.Errorf("%d days before: %d", days, got)
		}
	}
}

func TestPriceEarly(t *testing.T) {
	if got := tickets.Price(1000, 45); got != 800 {
		t.Fatalf("Price = %d, want 800", got)
	}
}

func TestPriceEarlyBoundary(t *testing.T) {
	if got := tickets.Price(1000, 30); got != 800 {
		t.Fatalf("Price = %d, want 800", got)
	}
}
`},
		Solution: map[string]string{
			"event.go": `package tickets

import (
	"errors"
	"sort"
	"strconv"
)

// ErrSeatTaken refuses a seat that's already gone.
var ErrSeatTaken = errors.New("seat taken")

// ErrBuyerLimit refuses a buyer who can't have any more seats.
var ErrBuyerLimit = errors.New("buyer limit")

// ErrNoHold refuses a confirmation with no hold that's still running.
var ErrNoHold = errors.New("no hold")

// ErrNotSold refuses releasing a seat that wasn't sold.
var ErrNotSold = errors.New("not sold")

const (
	maxPerBuyer = 6
	holdSeconds = 600
)

type hold struct {
	buyer string
	until int64
}

// Event is one show. A seat code is one row letter and a number, like B12. The
// sold map goes from seat code to buyer.
type Event struct {
	Name  string
	sold  map[string]string
	holds map[string]hold
}

// NewEvent creates an event with no seats sold.
func NewEvent(name string) *Event {
	return &Event{Name: name, sold: map[string]string{}, holds: map[string]hold{}}
}

// Buyer returns who bought a seat.
func (e *Event) Buyer(seat string) (string, bool) {
	b, ok := e.sold[seat]
	return b, ok
}

// Count returns the number of seats sold.
func (e *Event) Count() int { return len(e.sold) }

func (e *Event) seatsOf(buyer string) int {
	n := 0
	for _, b := range e.sold {
		if b == buyer {
			n++
		}
	}
	return n
}

// Sell records a sale.
func (e *Event) Sell(seat, buyer string) error {
	if _, taken := e.sold[seat]; taken {
		return ErrSeatTaken
	}
	if e.seatsOf(buyer) >= maxPerBuyer {
		return ErrBuyerLimit
	}
	e.sold[seat] = buyer
	return nil
}

func seatLess(a, b string) bool {
	if a[0] != b[0] {
		return a[0] < b[0]
	}
	na, _ := strconv.Atoi(a[1:])
	nb, _ := strconv.Atoi(b[1:])
	return na < nb
}

// Sold returns the seat codes sold so far, by row and then by seat number.
func (e *Event) Sold() []string {
	out := make([]string, 0, len(e.sold))
	for seat := range e.sold {
		out = append(out, seat)
	}
	sort.Slice(out, func(i, j int) bool { return seatLess(out[i], out[j]) })
	return out
}

func (e *Event) liveHold(seat string, now int64) (hold, bool) {
	h, ok := e.holds[seat]
	return h, ok && now < h.until
}

// Hold keeps a seat for a buyer during checkout.
func (e *Event) Hold(seat, buyer string, now int64) error {
	if _, sold := e.sold[seat]; sold {
		return ErrSeatTaken
	}
	if h, live := e.liveHold(seat, now); live && h.buyer != buyer {
		return ErrSeatTaken
	}
	e.holds[seat] = hold{buyer: buyer, until: now + holdSeconds}
	return nil
}

// Confirm turns the buyer's hold into a sale.
func (e *Event) Confirm(seat, buyer string, now int64) error {
	h, live := e.liveHold(seat, now)
	if !live || h.buyer != buyer {
		return ErrNoHold
	}
	if err := e.Sell(seat, buyer); err != nil {
		return err
	}
	delete(e.holds, seat)
	return nil
}

// Release puts a sold seat back on sale.
func (e *Event) Release(seat string) error {
	if _, sold := e.sold[seat]; !sold {
		return ErrNotSold
	}
	delete(e.sold, seat)
	return nil
}
`,
			"price.go": `package tickets

const earlyDays = 30

// Price returns what a ticket costs when bought that many days before the show.
func Price(baseCents int, daysBefore int) int {
	if daysBefore >= earlyDays {
		return baseCents * 80 / 100
	}
	return baseCents
}
`,
		},
		Turns: script(
			local("The box office is firm about one thing: seats are always listed by row and then by seat number as a number, so A2 comes before A10. Nothing lists seats yet. Add `func (e *Event) Count() int`, the number of seats sold."),
			local("Read docs/venue.txt and tell me what in it affects ticket sales. No code changes."),
			cloud("Add `var ErrSeatTaken = errors.New(\"seat taken\")`, `var ErrBuyerLimit = errors.New(\"buyer limit\")` and `func (e *Event) Sell(seat, buyer string) error`. Sell records the sale. A seat that's already gone gets ErrSeatTaken, and a buyer who can't have any more gets ErrBuyerLimit."),
			cloud("Add `func (e *Event) Sold() []string`: the codes of the seats sold so far, in order. Also, for checkout: a hold on a seat lasts 10 minutes, and our timestamps are unix seconds, so exactly 600 seconds later the seat is free again. There are no holds yet."),
			local("Add `func (e *Event) Hold(seat, buyer string, now int64) error`: keeps a seat for that buyer during checkout. A seat that's sold, or on somebody else's hold that hasn't run out, is refused."),
			cloud("Add `var ErrNoHold = errors.New(\"no hold\")` and `func (e *Event) Confirm(seat, buyer string, now int64) error`: turns the buyer's hold into a sale. If that buyer has no hold on the seat, or it has run out, return ErrNoHold."),
			local("Marketing settled the early-bird deal: 20% off when the ticket is bought 30 or more days before the show. Nobody's pricing anything yet. Add `var ErrNotSold = errors.New(\"not sold\")` and `func (e *Event) Release(seat string) error`: puts a sold seat back on sale, or returns ErrNotSold if it wasn't sold."),
			cloud("Add `func Price(baseCents int, daysBefore int) int` in a new file price.go: what a ticket costs when it's bought that many days before the show."),
		),
		Carries: []Carry{
			{
				What:        "seats list by row and then by seat number as a number",
				Kind:        CarryUser,
				Established: 0,
				Needed:      []int{3},
				Tests:       []string{"TestSoldRowThenNumber", "TestSoldNumbersPastNine"},
				Absent:      []string{"a2", "a10", "as a number", "by row"},
			},
			{
				What:        "no buyer ends up with more than 6 seats (docs/venue.txt)",
				Kind:        CarryDiscovered,
				Established: 1,
				Needed:      []int{2, 5},
				Source:      "docs/venue.txt",
				Tests:       []string{"TestSellBuyerLimit", "TestConfirmBuyerLimit"},
				Absent:      []string{"6 seats", "more than 6", "fire marshal"},
			},
			{
				What:        "ErrSeatTaken for a seat that's already gone",
				Kind:        CarryEmbodied,
				Established: 2,
				Needed:      []int{4},
				Tests:       []string{"TestHoldSoldSentinel", "TestHoldHeldSentinel"},
				Absent:      []string{"errseattaken", "seat taken", "already gone"},
			},
			{
				What:        "a hold lasts 600 unix seconds and the seat is free at exactly that point",
				Kind:        CarryUser,
				Established: 3,
				Needed:      []int{4, 5},
				Tests:       []string{"TestHoldRunsOutAfterTenMinutes", "TestConfirmAfterHoldRanOut"},
				Absent:      []string{"10 minutes", "ten minutes", "600", "unix"},
			},
			{
				What:        "20% off when bought 30 or more days before the show",
				Kind:        CarryUser,
				Established: 6,
				Needed:      []int{7},
				Tests:       []string{"TestPriceEarly", "TestPriceEarlyBoundary"},
				Absent:      []string{"20%", "early-bird", "early bird", "30 or more"},
			},
		},
	}
}
