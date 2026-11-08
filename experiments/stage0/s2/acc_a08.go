package s2

func accA08() Task {
	return Task{
		ID:     "s2-a08-restaurant-tabs",
		Set:    "acceptance",
		System: system("a restaurant tab system"),
		Files: map[string]string{
			"go.mod": goMod,
			"tab.go": `package tabs

// Item is one thing ordered. Cents is the menu price.
type Item struct {
	Name  string
	Cents int
	Comp  bool
}

// Tab is a table's running bill.
type Tab struct {
	Party int
	Items []Item
}

// Subtotal returns the menu price of everything not comped.
func (t *Tab) Subtotal() int {
	total := 0
	for _, it := range t.Items {
		if !it.Comp {
			total += it.Cents
		}
	}
	return total
}

// Add puts an item on the tab.
func (t *Tab) Add(it Item) error {
	t.Items = append(t.Items, it)
	return nil
}
`,
			"docs/house.txt": `House notes for the floor staff

Anything on the drinks list has a name starting with bar: and by licensing law we
can't give drinks away, so a drink is never comped.

Bread is free and isn't rung up. Tables turn every 90 minutes at peak.
`,
			"tab_test.go": `package tabs

import "testing"

func TestSubtotalSkipsComps(t *testing.T) {
	tab := &Tab{}
	if err := tab.Add(Item{Name: "soup", Cents: 300}); err != nil {
		t.Fatal(err)
	}
	if err := tab.Add(Item{Name: "cake", Cents: 200, Comp: true}); err != nil {
		t.Fatal(err)
	}
	if got := tab.Subtotal(); got != 300 {
		t.Fatalf("Subtotal = %d", got)
	}
}
`,
		},
		Hidden: map[string]string{hiddenName: `package tabs_test

import (
	"errors"
	"reflect"
	"testing"

	tabs "fixture"
)

func tab(t *testing.T, party int, items ...tabs.Item) *tabs.Tab {
	t.Helper()
	b := &tabs.Tab{Party: party}
	for _, it := range items {
		if err := b.Add(it); err != nil {
			t.Fatal(err)
		}
	}
	return b
}

func TestCountItems(t *testing.T) {
	b := tab(t, 2, tabs.Item{Name: "soup", Cents: 300}, tabs.Item{Name: "cake", Cents: 200})
	if got := b.Count(); got != 2 {
		t.Fatalf("Count = %d", got)
	}
}

func TestAddAfterClose(t *testing.T) {
	b := tab(t, 2, tabs.Item{Name: "soup", Cents: 300})
	b.Close()
	if err := b.Add(tabs.Item{Name: "cake", Cents: 200}); !errors.Is(err, tabs.ErrClosed) {
		t.Fatalf("err = %v", err)
	}
	if got := b.Count(); got != 1 {
		t.Fatalf("Count = %d", got)
	}
}

func TestCompFirstMatch(t *testing.T) {
	b := tab(t, 2, tabs.Item{Name: "soup", Cents: 300}, tabs.Item{Name: "soup", Cents: 300})
	if err := b.CompItem("soup"); err != nil {
		t.Fatal(err)
	}
	if !b.Items[0].Comp || b.Items[1].Comp {
		t.Fatalf("Items = %+v", b.Items)
	}
}

func TestCompMissing(t *testing.T) {
	if err := tab(t, 2).CompItem("soup"); err == nil {
		t.Fatal("no error")
	}
}

func TestCompAfterClose(t *testing.T) {
	b := tab(t, 2, tabs.Item{Name: "soup", Cents: 300})
	b.Close()
	if err := b.CompItem("soup"); !errors.Is(err, tabs.ErrClosed) {
		t.Fatalf("err = %v", err)
	}
	if b.Items[0].Comp {
		t.Fatal("a closed tab changed")
	}
}

func TestDrinkNeverComped(t *testing.T) {
	b := tab(t, 2, tabs.Item{Name: "bar:wine", Cents: 900})
	if err := b.CompItem("bar:wine"); !errors.Is(err, tabs.ErrNoComp) {
		t.Fatalf("err = %v", err)
	}
	if b.Items[0].Comp || b.Subtotal() != 900 {
		t.Fatalf("Items = %+v", b.Items)
	}
}

func TestTaxEmptyTab(t *testing.T) {
	if got := tab(t, 2).Tax(); got != 0 {
		t.Fatalf("Tax = %d", got)
	}
}

func TestTaxComesOutOfPrice(t *testing.T) {
	if got := tab(t, 2, tabs.Item{Name: "soup", Cents: 1100}).Tax(); got != 100 {
		t.Fatalf("Tax = %d, want 100", got)
	}
}

func TestTaxSkipsComps(t *testing.T) {
	b := tab(t, 2, tabs.Item{Name: "soup", Cents: 2200}, tabs.Item{Name: "cake", Cents: 1100, Comp: true})
	if got := b.Tax(); got != 200 {
		t.Fatalf("Tax = %d, want 200", got)
	}
}

func TestRemoveFirstMatch(t *testing.T) {
	b := tab(t, 2, tabs.Item{Name: "soup", Cents: 300}, tabs.Item{Name: "soup", Cents: 400})
	if err := b.Remove("soup"); err != nil {
		t.Fatal(err)
	}
	if len(b.Items) != 1 || b.Items[0].Cents != 400 {
		t.Fatalf("Items = %+v", b.Items)
	}
}

func TestRemoveMissing(t *testing.T) {
	if err := tab(t, 2).Remove("soup"); err == nil {
		t.Fatal("no error")
	}
}

func TestRemoveAfterClose(t *testing.T) {
	b := tab(t, 2, tabs.Item{Name: "soup", Cents: 300})
	b.Close()
	if err := b.Remove("soup"); !errors.Is(err, tabs.ErrClosed) {
		t.Fatalf("err = %v", err)
	}
	if got := b.Count(); got != 1 {
		t.Fatalf("Count = %d", got)
	}
}

func TestTotalSmallParty(t *testing.T) {
	b := tab(t, 7, tabs.Item{Name: "soup", Cents: 1000}, tabs.Item{Name: "cake", Cents: 500, Comp: true})
	if got := b.Total(); got != 1000 {
		t.Fatalf("Total = %d", got)
	}
}

func TestTotalLargeParty(t *testing.T) {
	if got := tab(t, 8, tabs.Item{Name: "soup", Cents: 1000}).Total(); got != 1150 {
		t.Fatalf("Total = %d, want 1150", got)
	}
}

func TestTotalChargeOnSubtotal(t *testing.T) {
	b := tab(t, 12, tabs.Item{Name: "soup", Cents: 2000}, tabs.Item{Name: "cake", Cents: 500, Comp: true})
	if got := b.Total(); got != 2300 {
		t.Fatalf("Total = %d, want 2300", got)
	}
}

func TestSplitEven(t *testing.T) {
	if got := tabs.Split(100, 4); !reflect.DeepEqual(got, []int{25, 25, 25, 25}) {
		t.Fatalf("Split = %v", got)
	}
}

func TestSplitAddsUp(t *testing.T) {
	sum := 0
	got := tabs.Split(1001, 3)
	for _, share := range got {
		sum += share
	}
	if sum != 1001 || len(got) != 3 {
		t.Fatalf("Split = %v", got)
	}
}

func TestSplitLeftoverSpread(t *testing.T) {
	if got := tabs.Split(101, 3); !reflect.DeepEqual(got, []int{34, 34, 33}) {
		t.Fatalf("Split = %v", got)
	}
}

func TestSplitLargerFirst(t *testing.T) {
	if got := tabs.Split(100, 3); !reflect.DeepEqual(got, []int{34, 33, 33}) {
		t.Fatalf("Split = %v", got)
	}
}
`},
		Solution: map[string]string{
			"tab.go": `package tabs

import (
	"errors"
	"strings"
)

// ErrClosed refuses any change to a closed tab.
var ErrClosed = errors.New("tab closed")

// ErrNoComp refuses comping an item we aren't allowed to comp.
var ErrNoComp = errors.New("cannot comp")

const (
	serviceFrom    = 8
	servicePercent = 15
)

// Item is one thing ordered. Cents is the menu price.
type Item struct {
	Name  string
	Cents int
	Comp  bool
}

// Tab is a table's running bill.
type Tab struct {
	Party  int
	Items  []Item
	closed bool
}

// Subtotal returns the menu price of everything not comped.
func (t *Tab) Subtotal() int {
	total := 0
	for _, it := range t.Items {
		if !it.Comp {
			total += it.Cents
		}
	}
	return total
}

// Add puts an item on the tab.
func (t *Tab) Add(it Item) error {
	if t.closed {
		return ErrClosed
	}
	t.Items = append(t.Items, it)
	return nil
}

// Count returns the number of items on the tab.
func (t *Tab) Count() int { return len(t.Items) }

// Close stops the tab from changing.
func (t *Tab) Close() { t.closed = true }

func (t *Tab) find(name string) int {
	for i, it := range t.Items {
		if it.Name == name {
			return i
		}
	}
	return -1
}

// CompItem marks the first item with that name as comped.
func (t *Tab) CompItem(name string) error {
	if t.closed {
		return ErrClosed
	}
	if strings.HasPrefix(name, "bar:") {
		return ErrNoComp
	}
	i := t.find(name)
	if i < 0 {
		return errors.New("not on the tab")
	}
	t.Items[i].Comp = true
	return nil
}

// Remove takes the first item with that name off the tab.
func (t *Tab) Remove(name string) error {
	if t.closed {
		return ErrClosed
	}
	i := t.find(name)
	if i < 0 {
		return errors.New("not on the tab")
	}
	t.Items = append(t.Items[:i], t.Items[i+1:]...)
	return nil
}

// Tax returns the sales tax in the Subtotal, which already includes it.
func (t *Tab) Tax() int { return t.Subtotal() / 11 }

// Total returns what the table pays.
func (t *Tab) Total() int {
	sub := t.Subtotal()
	if t.Party >= serviceFrom {
		return sub + sub*servicePercent/100
	}
	return sub
}
`,
			"split.go": `package tabs

// Split divides a bill of total cents among that many people.
func Split(total, ways int) []int {
	shares := make([]int, ways)
	for i := range shares {
		shares[i] = total / ways
		if i < total%ways {
			shares[i]++
		}
	}
	return shares
}
`,
		},
		Turns: script(
			cloud("For the tax report: menu prices here already include our 10% sales tax, so tax gets pulled out of a price and never added on top. Nothing reports tax yet. Add `func (t *Tab) Count() int`, the number of items on the tab."),
			local("Add `var ErrClosed = errors.New(\"tab closed\")` and `func (t *Tab) Close()`. A closed tab can't change any more: make Add return ErrClosed once it's closed, and the same goes for anything else that changes a tab."),
			cloud("Read docs/house.txt and tell me what it means for comps. No code changes."),
			local("Add `var ErrNoComp = errors.New(\"cannot comp\")` and `func (t *Tab) CompItem(name string) error`: marks the first item with that name as comped. An item we aren't allowed to comp gets ErrNoComp, and a name that isn't on the tab gets any other error."),
			local("Add `func (t *Tab) Tax() int`: the sales tax for the tab's Subtotal."),
			cloud("The owner's decided on service: parties of 8 or more pay a 15% service charge on Subtotal, and smaller parties pay none. Nothing adds it yet. Add `func (t *Tab) Remove(name string) error`: takes the first item with that name off the tab. A name that isn't on the tab gets any error."),
			local("Add `func (t *Tab) Total() int`: what the table pays. Separately, the servers asked how split bills will work, and I told them the shares never differ by more than a cent and the larger shares come first. That isn't built yet."),
			cloud("Add `func Split(total, ways int) []int` in a new file split.go: divides a bill of total cents among that many people."),
		),
		Carries: []Carry{
			{
				What:        "menu prices include the 10% tax, so tax is pulled out and not added on top",
				Kind:        CarryUser,
				Established: 0,
				Needed:      []int{4},
				Tests:       []string{"TestTaxComesOutOfPrice", "TestTaxSkipsComps"},
				Absent:      []string{"pulled out", "on top", "10%", "already include"},
			},
			{
				What:        "ErrClosed for anything that changes a closed tab",
				Kind:        CarryEmbodied,
				Established: 1,
				Needed:      []int{3, 5},
				Tests:       []string{"TestCompAfterClose", "TestRemoveAfterClose"},
				Absent:      []string{"errclosed", "tab closed", "closed"},
			},
			{
				What:        "names starting with bar: are drinks, which are never comped (docs/house.txt)",
				Kind:        CarryDiscovered,
				Established: 2,
				Needed:      []int{3},
				Source:      "docs/house.txt",
				Tests:       []string{"TestDrinkNeverComped"},
				Absent:      []string{"bar:", "drink", "licensing"},
			},
			{
				What:        "parties of 8 or more pay 15% service on Subtotal",
				Kind:        CarryUser,
				Established: 5,
				Needed:      []int{6},
				Tests:       []string{"TestTotalLargeParty", "TestTotalChargeOnSubtotal"},
				Absent:      []string{"15%", "8 or more", "service charge"},
			},
			{
				What:        "split shares differ by at most a cent, larger shares first",
				Kind:        CarryUser,
				Established: 6,
				Needed:      []int{7},
				Tests:       []string{"TestSplitLeftoverSpread", "TestSplitLargerFirst"},
				Absent:      []string{"differ", "larger shares", "a cent"},
			},
		},
	}
}
