package s2

func task05() Task {
	return Task{
		ID:     "s2-05-warehouse-stock",
		Set:    "tuning",
		System: system("a warehouse stock tracker"),
		Files: map[string]string{
			"go.mod": goMod,
			"stock.go": `package warehouse

// Stock tracks units on hand per SKU.
type Stock struct {
	qty map[string]int
}

// New returns an empty Stock.
func New() *Stock { return &Stock{qty: map[string]int{}} }

// Receive adds delivered units.
func (s *Stock) Receive(sku string, qty int) { s.qty[sku] += qty }

// OnHand reports units on hand.
func (s *Stock) OnHand(sku string) int { return s.qty[sku] }
`,
			"supplier/format.txt": `Supplier delivery files

One delivery per line, written as SKU:CASES, for example A1:3.
Quantities are in cases. One case holds 12 units.
A line starting with ! is a back-order: it has not been delivered yet, so it must
not be counted.
`,
			"stock_test.go": `package warehouse

import "testing"

func TestReceive(t *testing.T) {
	s := New()
	s.Receive("A1", 3)
	s.Receive("A1", 4)
	if got := s.OnHand("A1"); got != 7 {
		t.Fatalf("OnHand = %d", got)
	}
}
`,
		},
		Hidden: map[string]string{hiddenName: `package warehouse_test

import (
	"errors"
	"testing"

	warehouse "fixture"
)

func stocked(n int) *warehouse.Stock {
	s := warehouse.New()
	s.Receive("A1", n)
	return s
}

func lastEntry(s *warehouse.Stock) string {
	a := s.Audit()
	if len(a) == 0 {
		return ""
	}
	return a[len(a)-1]
}

func TestTotal(t *testing.T) {
	s := stocked(3)
	s.Receive("B2", 4)
	if got := s.Total(); got != 7 {
		t.Fatalf("Total = %d", got)
	}
}

func TestReceiveAudited(t *testing.T) {
	if got := lastEntry(stocked(12)); got != "receive A1 12" {
		t.Fatalf("entry %q", got)
	}
}

func TestShipReducesStock(t *testing.T) {
	s := stocked(10)
	if err := s.Ship("A1", 5); err != nil {
		t.Fatal(err)
	}
	if got := s.OnHand("A1"); got != 5 {
		t.Fatalf("OnHand = %d", got)
	}
}

func TestShipBeyondStockFails(t *testing.T) {
	s := stocked(3)
	if err := s.Ship("A1", 9); err == nil {
		t.Fatal("no error")
	}
	if got := s.OnHand("A1"); got != 3 {
		t.Fatalf("OnHand = %d", got)
	}
}

func TestShipRefusedBelowFloor(t *testing.T) {
	s := stocked(10)
	if err := s.Ship("A1", 6); !errors.Is(err, warehouse.ErrBelowFloor) {
		t.Fatalf("err = %v", err)
	}
	if got := s.OnHand("A1"); got != 10 {
		t.Fatalf("OnHand = %d", got)
	}
	if got := lastEntry(s); got != "receive A1 10" {
		t.Fatalf("a refused shipment left the entry %q", got)
	}
}

func TestShippable(t *testing.T) {
	s := stocked(12)
	s.Receive("B2", 3)
	if got := s.Shippable("A1"); got != 7 {
		t.Errorf("12 on hand: %d", got)
	}
	if got := s.Shippable("B2"); got != 0 {
		t.Errorf("3 on hand: %d", got)
	}
}

func TestShipAudited(t *testing.T) {
	s := stocked(20)
	if err := s.Ship("A1", 3); err != nil {
		t.Fatal(err)
	}
	if got := lastEntry(s); got != "ship A1 3" {
		t.Fatalf("entry %q", got)
	}
}

func TestAdjust(t *testing.T) {
	s := stocked(10)
	s.Adjust("A1", -2)
	if got := s.OnHand("A1"); got != 8 {
		t.Fatalf("OnHand = %d", got)
	}
}

func TestAdjustAudited(t *testing.T) {
	s := stocked(10)
	s.Adjust("A1", -2)
	if got := lastEntry(s); got != "adjust A1 -2" {
		t.Fatalf("entry %q", got)
	}
}

func TestScrap(t *testing.T) {
	s := stocked(4)
	if err := s.Scrap("A1", 3); err != nil {
		t.Fatal(err)
	}
	if got := s.OnHand("A1"); got != 1 {
		t.Fatalf("OnHand = %d", got)
	}
	if err := s.Scrap("A1", 2); err == nil {
		t.Fatal("scrapping too much gave no error")
	}
	if got := s.OnHand("A1"); got != 1 {
		t.Fatalf("OnHand = %d", got)
	}
}

func TestScrapAudited(t *testing.T) {
	s := stocked(4)
	if err := s.Scrap("A1", 3); err != nil {
		t.Fatal(err)
	}
	if got := lastEntry(s); got != "scrap A1 3" {
		t.Fatalf("entry %q", got)
	}
}

func TestImportCases(t *testing.T) {
	s := warehouse.New()
	if err := s.Import("A1:2\nB2:1"); err != nil {
		t.Fatal(err)
	}
	if s.OnHand("A1") != 24 || s.OnHand("B2") != 12 {
		t.Fatalf("A1 %d, B2 %d", s.OnHand("A1"), s.OnHand("B2"))
	}
}

func TestImportSkipsBackOrders(t *testing.T) {
	s := warehouse.New()
	if err := s.Import("A1:1\n!B2:5"); err != nil {
		t.Fatal(err)
	}
	if s.OnHand("B2") != 0 || s.OnHand("!B2") != 0 {
		t.Fatalf("B2 %d, !B2 %d", s.OnHand("B2"), s.OnHand("!B2"))
	}
}

func TestImportBadLine(t *testing.T) {
	if err := warehouse.New().Import("garbage"); err == nil {
		t.Fatal("no error")
	}
}
`},
		Solution: map[string]string{
			"stock.go": `package warehouse

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// ErrBelowFloor refuses a shipment that would go under the safety floor.
var ErrBelowFloor = errors.New("below safety floor")

const safetyFloor = 5

// Stock tracks units on hand per SKU.
type Stock struct {
	qty   map[string]int
	audit []string
}

// New returns an empty Stock.
func New() *Stock { return &Stock{qty: map[string]int{}} }

func (s *Stock) record(op, sku string, qty int) {
	s.audit = append(s.audit, fmt.Sprintf("%s %s %d", op, sku, qty))
}

// Receive adds delivered units.
func (s *Stock) Receive(sku string, qty int) {
	s.qty[sku] += qty
	s.record("receive", sku, qty)
}

// OnHand reports units on hand.
func (s *Stock) OnHand(sku string) int { return s.qty[sku] }

// Audit returns the audit entries, oldest first.
func (s *Stock) Audit() []string { return append([]string(nil), s.audit...) }

// Total returns the units on hand across all SKUs.
func (s *Stock) Total() int {
	total := 0
	for _, n := range s.qty {
		total += n
	}
	return total
}

// Ship takes units out of stock for a customer shipment.
func (s *Stock) Ship(sku string, qty int) error {
	if s.qty[sku]-qty < safetyFloor {
		return ErrBelowFloor
	}
	s.qty[sku] -= qty
	s.record("ship", sku, qty)
	return nil
}

// Shippable returns how many units could go out in one shipment now.
func (s *Stock) Shippable(sku string) int {
	return max(s.qty[sku]-safetyFloor, 0)
}

// Adjust applies a stocktake correction.
func (s *Stock) Adjust(sku string, delta int) {
	s.qty[sku] += delta
	s.record("adjust", sku, delta)
}

// Scrap writes off damaged units.
func (s *Stock) Scrap(sku string, qty int) error {
	if qty > s.qty[sku] {
		return errors.New("cannot scrap more than is on hand")
	}
	s.qty[sku] -= qty
	s.record("scrap", sku, qty)
	return nil
}

// Import loads a supplier file into stock.
func (s *Stock) Import(text string) error {
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "!") {
			continue
		}
		sku, cases, ok := strings.Cut(line, ":")
		if !ok {
			return fmt.Errorf("bad supplier line %q", line)
		}
		n, err := strconv.Atoi(cases)
		if err != nil {
			return fmt.Errorf("bad supplier line %q", line)
		}
		s.Receive(sku, n*12)
	}
	return nil
}
`,
		},
		Turns: script(
			cloud("Add `func (s *Stock) Total() int`, the units on hand across all SKUs. Also add `var ErrBelowFloor = errors.New(\"below safety floor\")`. It's for a policy from the warehouse manager that we'll code when shipping arrives: a shipment may never leave a SKU with fewer than 5 units on hand, and such a shipment is refused outright with ErrBelowFloor and changes nothing."),
			local("Add an audit trail: `func (s *Stock) Audit() []string` returns the entries oldest first, and Receive records an entry like \"receive A1 12\": the method name in lower case, the SKU, then the quantity. From now on every method that changes quantities records one entry in that form when it changes something."),
			local("Read supplier/format.txt and tell me how to read those files. No code changes."),
			local("Add `func (s *Stock) Ship(sku string, qty int) error`, which takes units out of stock for a customer shipment."),
			cloud("Add `func (s *Stock) Adjust(sku string, delta int)`, a stocktake correction that adds delta to the units on hand. delta may be negative."),
			cloud("Add `func (s *Stock) Import(text string) error`, which loads a supplier file into stock. A line it cannot parse is an error."),
			local("Add `func (s *Stock) Scrap(sku string, qty int) error`, which writes off damaged units. Scrapping more than is on hand is an error and changes nothing."),
			cloud("Add `func (s *Stock) Shippable(sku string) int`: how many units of a SKU could go out in a single shipment right now."),
		),
		Carries: []Carry{
			{
				What:        "a shipment may not leave under 5 units on hand; refused with ErrBelowFloor",
				Kind:        CarryUser,
				Established: 0,
				Needed:      []int{3, 7},
				Tests:       []string{"TestShipRefusedBelowFloor", "TestShippable"},
				Absent:      []string{"floor", "fewer than", "5 units"},
			},
			{
				What:        "every method changing quantities records an audit entry named after it",
				Kind:        CarryEmbodied,
				Established: 1,
				Needed:      []int{3, 4, 6},
				Tests:       []string{"TestShipAudited", "TestAdjustAudited", "TestScrapAudited"},
				Absent:      []string{"audit", "entry", "record"},
			},
			{
				What:        "supplier files count cases of 12 and ! lines are back-orders (supplier/format.txt)",
				Kind:        CarryDiscovered,
				Established: 2,
				Needed:      []int{5},
				Source:      "supplier/format.txt",
				Tests:       []string{"TestImportCases", "TestImportSkipsBackOrders"},
				Absent:      []string{"cases", "12", "back-order"},
			},
		},
	}
}
