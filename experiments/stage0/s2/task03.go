package s2

func task03() Task {
	return Task{
		ID:     "s2-03-catalog-listing",
		Set:    "tuning",
		System: system("a product catalog"),
		Files: map[string]string{
			"go.mod": goMod,
			"item.go": `package catalog

// Item is one product.
type Item struct {
	SKU        string
	Title      string
	PriceCents int
}
`,
			"store.go": `package catalog

// Store keeps items in the order they were added.
type Store struct {
	items []Item
}

// Add stores an item.
func (s *Store) Add(it Item) { s.items = append(s.items, it) }

// All returns the items in insertion order.
func (s *Store) All() []Item { return append([]Item(nil), s.items...) }
`,
			"docs/api.txt": `Listing API notes

Every listing response is ordered by price, lowest first. Items with the same
price are ordered by SKU. Clients cache on this order, so it must not vary.

Titles may contain any characters.
`,
			"store_test.go": `package catalog

import "testing"

func TestAllKeepsInsertionOrder(t *testing.T) {
	var s Store
	s.Add(Item{SKU: "b"})
	s.Add(Item{SKU: "a"})
	got := s.All()
	if len(got) != 2 || got[0].SKU != "b" || got[1].SKU != "a" {
		t.Fatalf("got %+v", got)
	}
}
`,
		},
		Hidden: map[string]string{hiddenName: `package catalog_test

import (
	"fmt"
	"testing"

	catalog "fixture"
)

// filled returns n items already in listing order, so that only tests which ask
// about ordering depend on it.
func filled(n int) *catalog.Store {
	s := &catalog.Store{}
	for i := range n {
		s.Add(catalog.Item{SKU: fmt.Sprintf("S%03d", i), Title: "Widget", PriceCents: 100 + i})
	}
	return s
}

func skus(items []catalog.Item) string {
	out := ""
	for _, it := range items {
		out += it.SKU + " "
	}
	return out
}

func shuffled() *catalog.Store {
	s := &catalog.Store{}
	s.Add(catalog.Item{SKU: "B", Title: "x", PriceCents: 200})
	s.Add(catalog.Item{SKU: "A", Title: "x", PriceCents: 200})
	s.Add(catalog.Item{SKU: "C", Title: "x", PriceCents: 100})
	return s
}

func TestCount(t *testing.T) {
	if got := filled(3).Count(); got != 3 {
		t.Fatalf("Count = %d", got)
	}
}

func TestBySKU(t *testing.T) {
	s := filled(3)
	if it, ok := s.BySKU("S001"); !ok || it.PriceCents != 101 {
		t.Fatalf("BySKU = %+v, %v", it, ok)
	}
	if _, ok := s.BySKU("nope"); ok {
		t.Fatal("found a missing SKU")
	}
}

func TestTitles(t *testing.T) {
	s := &catalog.Store{}
	s.Add(catalog.Item{SKU: "1", Title: "b"})
	s.Add(catalog.Item{SKU: "2", Title: "a"})
	if got := s.Titles(); len(got) != 2 || got[0] != "b" || got[1] != "a" {
		t.Fatalf("Titles = %v", got)
	}
}

func TestListPastEndEmpty(t *testing.T) {
	if got := filled(3).List(99, 10); len(got) != 0 {
		t.Fatalf("List = %v", got)
	}
}

func TestListPagesStartAtOne(t *testing.T) {
	s := filled(5)
	if got := skus(s.List(1, 2)); got != "S000 S001 " {
		t.Errorf("page 1: %s", got)
	}
	if got := skus(s.List(2, 2)); got != "S002 S003 " {
		t.Errorf("page 2: %s", got)
	}
	if got := skus(s.List(-4, 2)); got != "S000 S001 " {
		t.Errorf("page -4: %s", got)
	}
}

func TestSearchPagesStartAtOne(t *testing.T) {
	s := filled(5)
	if got := skus(s.Search("widget", 1, 2)); got != "S000 S001 " {
		t.Errorf("page 1: %s", got)
	}
	if got := skus(s.Search("widget", 3, 2)); got != "S004 " {
		t.Errorf("page 3: %s", got)
	}
}

// Page 0 means the first page whether or not pages count from one, which keeps
// this test about matching alone.
func TestSearchIgnoresCase(t *testing.T) {
	s := filled(2)
	s.Add(catalog.Item{SKU: "Z", Title: "Gadget", PriceCents: 900})
	if got := skus(s.Search("GADG", 0, 10)); got != "Z " {
		t.Fatalf("Search = %s", got)
	}
}

func TestListDefaultSize(t *testing.T) {
	s := filled(60)
	if got := len(s.List(1, 0)); got != 25 {
		t.Errorf("size 0 gave %d items", got)
	}
	if got := len(s.List(1, -3)); got != 25 {
		t.Errorf("size -3 gave %d items", got)
	}
}

func TestPageCountDefault(t *testing.T) {
	if got := catalog.PageCount(41, 0); got != 2 {
		t.Fatalf("PageCount(41, 0) = %d", got)
	}
}

func TestPageCountExplicit(t *testing.T) {
	for _, c := range [][3]int{{10, 3, 4}, {10, 5, 2}, {0, 5, 0}, {1, 5, 1}} {
		if got := catalog.PageCount(c[0], c[1]); got != c[2] {
			t.Errorf("PageCount(%d, %d) = %d", c[0], c[1], got)
		}
	}
}

func TestListOrdering(t *testing.T) {
	if got := skus(shuffled().List(0, 10)); got != "C A B " {
		t.Fatalf("List = %s", got)
	}
}

func TestSearchOrdering(t *testing.T) {
	if got := skus(shuffled().Search("x", 0, 10)); got != "C A B " {
		t.Fatalf("Search = %s", got)
	}
}
`},
		Solution: map[string]string{
			"store.go": `package catalog

// Store keeps items in the order they were added.
type Store struct {
	items []Item
}

// Add stores an item.
func (s *Store) Add(it Item) { s.items = append(s.items, it) }

// All returns the items in insertion order.
func (s *Store) All() []Item { return append([]Item(nil), s.items...) }

// Count returns the number of items.
func (s *Store) Count() int { return len(s.items) }

// BySKU finds an item.
func (s *Store) BySKU(sku string) (Item, bool) {
	for _, it := range s.items {
		if it.SKU == sku {
			return it, true
		}
	}
	return Item{}, false
}

// Titles returns all titles in insertion order.
func (s *Store) Titles() []string {
	out := make([]string, 0, len(s.items))
	for _, it := range s.items {
		out = append(out, it.Title)
	}
	return out
}
`,
			"list.go": `package catalog

import (
	"sort"
	"strings"
)

const defaultPageSize = 25

func ordered(items []Item) []Item {
	out := append([]Item(nil), items...)
	sort.Slice(out, func(a, b int) bool {
		if out[a].PriceCents != out[b].PriceCents {
			return out[a].PriceCents < out[b].PriceCents
		}
		return out[a].SKU < out[b].SKU
	})
	return out
}

func paginate(items []Item, page, size int) []Item {
	if page < 1 {
		page = 1
	}
	if size <= 0 {
		size = defaultPageSize
	}
	start := (page - 1) * size
	if start >= len(items) {
		return nil
	}
	return items[start:min(start+size, len(items))]
}

// List returns one page of the store's items.
func (s *Store) List(page, size int) []Item {
	return paginate(ordered(s.items), page, size)
}

// Search returns one page of the items whose title contains q, ignoring case.
func (s *Store) Search(q string, page, size int) []Item {
	var hits []Item
	for _, it := range s.items {
		if strings.Contains(strings.ToLower(it.Title), strings.ToLower(q)) {
			hits = append(hits, it)
		}
	}
	return paginate(ordered(hits), page, size)
}

// PageCount returns how many pages total items take at that size.
func PageCount(total, size int) int {
	if size <= 0 {
		size = defaultPageSize
	}
	return (total + size - 1) / size
}
`,
		},
		Turns: script(
			cloud("A decision for the listing API we are about to build: pages are numbered from 1, and a page number below 1 means the first page. To warm up, add `func (s *Store) Count() int`."),
			cloud("Another decision: a caller who passes a page size of zero or less gets the default size, which is 20. No code for that yet. Add `func (s *Store) BySKU(sku string) (Item, bool)`."),
			local("Read docs/api.txt and tell me what it requires of listings. No code changes."),
			local("Product changed its mind: the default page size is 25. Also add `func (s *Store) Titles() []string`, all titles in insertion order."),
			cloud("Add `func (s *Store) List(page, size int) []Item`, which returns one page of the store's items. A page past the end is empty, not an error."),
			local("Add `func (s *Store) Search(q string, page, size int) []Item`: items whose title contains q ignoring letter case, one page at a time."),
			cloud("Add `func PageCount(total, size int) int`: how many pages it takes to show total items at that size. Zero items means zero pages."),
		),
		Carries: []Carry{
			{
				What:        "pages count from 1, and below 1 means the first page",
				Kind:        CarryUser,
				Established: 0,
				Needed:      []int{4, 5},
				Tests:       []string{"TestListPagesStartAtOne", "TestSearchPagesStartAtOne"},
				Absent:      []string{"numbered from", "start at", "first page", "1-based", "one-based"},
			},
			{
				What:        "a size of zero or less means the default, revised from 20 to 25",
				Kind:        CarryUser,
				Established: 3,
				Needed:      []int{4, 6},
				Tests:       []string{"TestListDefaultSize", "TestPageCountDefault"},
				Absent:      []string{"default", "25", "20"},
			},
			{
				What:        "listings are ordered by price, then SKU (docs/api.txt)",
				Kind:        CarryDiscovered,
				Established: 2,
				Needed:      []int{4, 5},
				Source:      "docs/api.txt",
				Tests:       []string{"TestListOrdering", "TestSearchOrdering"},
				Absent:      []string{"ordered", "price", "lowest", "sku"},
			},
		},
	}
}
