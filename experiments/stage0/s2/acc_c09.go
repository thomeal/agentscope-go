package s2

func accC09() Task {
	return Task{
		ID:     "s2-c09-reorder-forecast",
		Set:    "acceptance",
		System: system("a reorder forecasting tool"),
		Files: map[string]string{
			"go.mod": goMod,
			"item.go": `package reorder

// Item is a stocked product. Sales lists the units sold per day, oldest first.
// LeadDays is how long a delivery takes.
type Item struct {
	SKU      string
	OnHand   int
	LeadDays int
	Sales    []int
}

// TotalSold adds up the whole sales history.
func (it Item) TotalSold() int {
	sum := 0
	for _, s := range it.Sales {
		sum += s
	}
	return sum
}
`,
			"ops/suppliers.txt": `Supplier terms

All our suppliers ship full cases only, and a case is 12 units across the whole
range. Any quantity we order has to be a whole number of cases, going up rather
than down so that we are not short.

Deliveries are accepted on weekdays at the rear dock.
`,
			"item_test.go": `package reorder

import "testing"

func TestTotalSold(t *testing.T) {
	if got := (Item{Sales: []int{1, 2, 3}}).TotalSold(); got != 6 {
		t.Fatalf("TotalSold = %d", got)
	}
}
`,
		},
		Hidden: map[string]string{hiddenName: `package reorder_test

import (
	"math"
	"testing"

	reorder "fixture"
)

func near(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

// repeat returns n days of the value v.
func repeat(n, v int) []int {
	out := make([]int, n)
	for i := range out {
		out[i] = v
	}
	return out
}

func TestEmpty(t *testing.T) {
	if !(reorder.Item{}).Empty() || (reorder.Item{Sales: []int{0}}).Empty() {
		t.Fatal("Empty is wrong")
	}
}

func TestStockOutDays(t *testing.T) {
	if got := (reorder.Item{Sales: []int{0, 4, 0, 1}}).StockOutDays(); got != 2 {
		t.Fatalf("StockOutDays = %d", got)
	}
}

func TestRateSteady(t *testing.T) {
	if got := (reorder.Item{Sales: repeat(10, 10)}).Rate(); !near(got, 10) {
		t.Fatalf("Rate = %v", got)
	}
}

func TestRateNothingToGoOn(t *testing.T) {
	if got := (reorder.Item{}).Rate(); got != 0 {
		t.Fatalf("no history: %v", got)
	}
	if got := (reorder.Item{Sales: []int{0, 0}}).Rate(); got != 0 {
		t.Fatalf("no sales: %v", got)
	}
}

func TestRateRecentHistoryOnly(t *testing.T) {
	sales := append(repeat(30, 100), repeat(28, 10)...)
	if got := (reorder.Item{Sales: sales}).Rate(); !near(got, 10) {
		t.Fatalf("Rate = %v", got)
	}
}

func TestRateLeavesOutStockOuts(t *testing.T) {
	if got := (reorder.Item{Sales: []int{10, 0, 10, 0}}).Rate(); !near(got, 10) {
		t.Fatalf("Rate = %v", got)
	}
}

func TestRateWindowIsCalendarDays(t *testing.T) {
	sales := append(append(repeat(5, 100), repeat(14, 10)...), repeat(14, 0)...)
	if got := (reorder.Item{Sales: sales}).Rate(); !near(got, 10) {
		t.Fatalf("Rate = %v", got)
	}
}

func TestReorderPointNoDemand(t *testing.T) {
	if got := (reorder.Item{LeadDays: 5}).ReorderPoint(); got != 0 {
		t.Fatalf("ReorderPoint = %d", got)
	}
}

func TestReorderPointGrowsWithLead(t *testing.T) {
	short := reorder.Item{LeadDays: 2, Sales: repeat(10, 4)}
	long := reorder.Item{LeadDays: 5, Sales: repeat(10, 4)}
	if long.ReorderPoint()-short.ReorderPoint() != 12 {
		t.Fatalf("%d and %d", short.ReorderPoint(), long.ReorderPoint())
	}
}

func TestReorderPointSafetyMargin(t *testing.T) {
	if got := (reorder.Item{LeadDays: 4, Sales: repeat(10, 10)}).ReorderPoint(); got != 70 {
		t.Fatalf("ReorderPoint = %d", got)
	}
	if reorder.SafetyDays != 3 {
		t.Fatalf("SafetyDays = %d", reorder.SafetyDays)
	}
}

func TestReorderPointUpToAWholeUnit(t *testing.T) {
	if got := (reorder.Item{LeadDays: 2, Sales: []int{1, 2}}).ReorderPoint(); got != 8 {
		t.Fatalf("ReorderPoint = %d", got)
	}
}

func TestOrderQtyCoversTheNeed(t *testing.T) {
	it := reorder.Item{OnHand: 5, Sales: repeat(10, 10)}
	if got := it.OrderQty(3); got < 25 || got > 36 {
		t.Fatalf("OrderQty = %d", got)
	}
}

func TestOrderQtyEnoughOnHand(t *testing.T) {
	it := reorder.Item{OnHand: 100, Sales: repeat(10, 10)}
	if got := it.OrderQty(3); got != 0 {
		t.Fatalf("OrderQty = %d", got)
	}
}

func TestOrderQtyWholeCases(t *testing.T) {
	it := reorder.Item{OnHand: 5, Sales: repeat(10, 10)}
	if got := it.OrderQty(3); got != 36 {
		t.Fatalf("OrderQty = %d", got)
	}
}

func TestOrderQtyExactCase(t *testing.T) {
	it := reorder.Item{OnHand: 6, Sales: repeat(10, 10)}
	if got := it.OrderQty(3); got != 24 {
		t.Fatalf("OrderQty = %d", got)
	}
}

func TestOrderQtyUpToACase(t *testing.T) {
	it := reorder.Item{OnHand: 7, Sales: repeat(10, 10)}
	if got := it.OrderQty(3); got != 24 {
		t.Fatalf("OrderQty = %d", got)
	}
}

func TestTrendSteady(t *testing.T) {
	if got := (reorder.Item{Sales: repeat(20, 5)}).Trend(); !near(got, 1) {
		t.Fatalf("Trend = %v", got)
	}
}

func TestTrendNoRate(t *testing.T) {
	if got := (reorder.Item{}).Trend(); got != 0 {
		t.Fatalf("Trend = %v", got)
	}
}

func TestTrendRising(t *testing.T) {
	sales := append(repeat(21, 10), repeat(7, 20)...)
	if got := (reorder.Item{Sales: sales}).Trend(); !near(got, 1.6) {
		t.Fatalf("Trend = %v", got)
	}
}

func TestTrendLeavesOutStockOuts(t *testing.T) {
	sales := append(repeat(21, 10), 20, 0, 20, 0, 20, 0, 20)
	want := 20.0 / (290.0 / 25.0)
	if got := (reorder.Item{Sales: sales}).Trend(); !near(got, want) {
		t.Fatalf("Trend = %v, want %v", got, want)
	}
}
`},
		Solution: map[string]string{
			"item.go": `package reorder

import "math"

// SafetyDays is how many extra days of demand a reorder decision must cover.
const SafetyDays = 3

const (
	demandDays = 28
	casePack   = 12
)

// Item is a stocked product. Sales lists the units sold per day, oldest first.
// LeadDays is how long a delivery takes.
type Item struct {
	SKU      string
	OnHand   int
	LeadDays int
	Sales    []int
}

// TotalSold adds up the whole sales history.
func (it Item) TotalSold() int {
	sum := 0
	for _, s := range it.Sales {
		sum += s
	}
	return sum
}

// Empty reports whether the item has no sales history.
func (it Item) Empty() bool { return len(it.Sales) == 0 }

// StockOutDays counts the days in Sales with a zero.
func (it Item) StockOutDays() int {
	n := 0
	for _, s := range it.Sales {
		if s == 0 {
			n++
		}
	}
	return n
}

func average(sales []int) float64 {
	sum, days := 0, 0
	for _, s := range sales {
		if s > 0 {
			sum += s
			days++
		}
	}
	if days == 0 {
		return 0
	}
	return float64(sum) / float64(days)
}

// Rate returns the average units sold per day.
func (it Item) Rate() float64 {
	sales := it.Sales
	if len(sales) > demandDays {
		sales = sales[len(sales)-demandDays:]
	}
	return average(sales)
}

// ReorderPoint returns the stock level at or below which we reorder.
func (it Item) ReorderPoint() int {
	return int(math.Ceil(float64(it.LeadDays+SafetyDays) * it.Rate()))
}

// OrderQty returns what to order to have days of demand in stock.
func (it Item) OrderQty(days int) int {
	need := int(math.Ceil(float64(days)*it.Rate())) - it.OnHand
	if need <= 0 {
		return 0
	}
	return (need + casePack - 1) / casePack * casePack
}

// Trend compares the last week's average with Rate.
func (it Item) Trend() float64 {
	rate := it.Rate()
	if rate == 0 {
		return 0
	}
	return average(it.Sales[max(len(it.Sales)-7, 0):]) / rate
}
`,
		},
		Turns: script(
			cloud("What demand means for this tool: only the most recent 28 days of Sales. Anything older is another season and tells us nothing. Nothing computes demand yet. Add `func (it Item) Empty() bool`, whether the item has no sales history."),
			local("One thing about the sales data: a zero means we had none on the shelf, not that nobody wanted it, so those days don't go into any average of what we sell. Nothing averages yet. Add `func (it Item) StockOutDays() int`, how many days in Sales are zero."),
			cloud("Add `const SafetyDays = 3`. Nothing reads it yet. A reorder decision has to cover the delivery time plus that many more days of demand."),
			local("Add `func (it Item) Rate() float64`: the average units sold per day. With nothing to go on it's 0."),
			local("Add `func (it Item) ReorderPoint() int`: the stock level at or below which we place an order, going up to a whole unit when the sum isn't one."),
			local("Read ops/suppliers.txt and tell me what it means for what we order. No code changes."),
			cloud("Add `func (it Item) OrderQty(days int) int`: how many units to order now so that stock reaches days of demand, going up to a whole unit when the sum isn't one. With enough on hand it's 0."),
			cloud("Add `func (it Item) Trend() float64`: the average units sold per day across the last 7 entries of Sales, divided by Rate. With a Rate of 0 it's 0."),
		),
		Carries: []Carry{
			{
				What:        "demand looks only at the most recent 28 days",
				Kind:        CarryUser,
				Established: 0,
				Needed:      []int{3},
				Tests:       []string{"TestRateRecentHistoryOnly", "TestRateWindowIsCalendarDays"},
				Absent:      []string{"28", "most recent", "season", "older"},
			},
			{
				What:        "zero days are stock-outs and stay out of averages",
				Kind:        CarryUser,
				Established: 1,
				Needed:      []int{3, 7},
				Tests:       []string{"TestRateLeavesOutStockOuts", "TestRateWindowIsCalendarDays", "TestTrendLeavesOutStockOuts"},
				Absent:      []string{"zero", "none on the shelf", "stock-out", "out of stock", "stockout"},
			},
			{
				What:        "a reorder decision covers delivery time plus SafetyDays",
				Kind:        CarryEmbodied,
				Established: 2,
				Needed:      []int{4},
				Tests:       []string{"TestReorderPointSafetyMargin", "TestReorderPointUpToAWholeUnit"},
				Absent:      []string{"safetydays", "safety", "delivery time"},
			},
			{
				What:        "orders are whole cases of 12 (ops/suppliers.txt)",
				Kind:        CarryDiscovered,
				Established: 5,
				Needed:      []int{6},
				Source:      "ops/suppliers.txt",
				Tests:       []string{"TestOrderQtyWholeCases", "TestOrderQtyUpToACase"},
				Absent:      []string{"case", "pack", "supplier", "12 units"},
			},
		},
	}
}
