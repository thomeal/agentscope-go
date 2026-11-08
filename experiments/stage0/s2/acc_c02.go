package s2

func accC02() Task {
	return Task{
		ID:     "s2-c02-csv-merge",
		Set:    "acceptance",
		System: system("a CSV merging tool"),
		Files: map[string]string{
			"go.mod": goMod,
			"table.go": `package csvmerge

import (
	"encoding/csv"
	"strings"
)

// Table is a parsed CSV file: a header and its data rows.
type Table struct {
	Header []string
	Rows   [][]string
}

// Parse reads CSV text whose first line is the header.
func Parse(text string) (Table, error) {
	records, err := csv.NewReader(strings.NewReader(text)).ReadAll()
	if err != nil {
		return Table{}, err
	}
	if len(records) == 0 {
		return Table{}, nil
	}
	return Table{Header: records[0], Rows: records[1:]}, nil
}
`,
			"docs/partner.txt": `Partner export notes

Every partner export ends with a totals line, and some have subtotals in the
middle. Those lines leave the key cell empty. They are not records and must never
end up in anything we compute or produce from the data.

Exports arrive UTF-8 encoded, comma-separated, with a header line.
`,
			"table_test.go": `package csvmerge

import "testing"

func TestParse(t *testing.T) {
	got, err := Parse("id,qty\nA,1\nB,2\n")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Header) != 2 || len(got.Rows) != 2 || got.Rows[1][1] != "2" {
		t.Fatalf("got %+v", got)
	}
}
`,
		},
		Hidden: map[string]string{hiddenName: `package csvmerge_test

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	csvmerge "fixture"
)

func table(t *testing.T, text string) csvmerge.Table {
	t.Helper()
	tab, err := csvmerge.Parse(text)
	if err != nil {
		t.Fatal(err)
	}
	return tab
}

func lines(s string) []string { return strings.Split(strings.TrimSpace(s), "\n") }

func keysOf(m csvmerge.Table, col int) []string {
	var out []string
	for _, r := range m.Rows {
		out = append(out, r[col])
	}
	return out
}

func TestColumn(t *testing.T) {
	tab := table(t, "id,qty\nA,1\n")
	if tab.Column("qty") != 1 || tab.Column("nope") != -1 {
		t.Fatalf("Column: %d, %d", tab.Column("qty"), tab.Column("nope"))
	}
}

func TestKeysFirstAppearance(t *testing.T) {
	got, err := csvmerge.Keys(table(t, "id,qty\nB,1\nA,2\nB,3\n"), "id")
	if err != nil || !reflect.DeepEqual(got, []string{"B", "A"}) {
		t.Fatalf("Keys = %v, %v", got, err)
	}
}

func TestKeysNoKeySentinel(t *testing.T) {
	if _, err := csvmerge.Keys(table(t, "id,qty\nA,1\n"), "sku"); !errors.Is(err, csvmerge.ErrNoKey) {
		t.Fatalf("err = %v", err)
	}
}

func TestKeysSkipTotals(t *testing.T) {
	got, err := csvmerge.Keys(table(t, "id,qty\nA,1\n,1\nB,2\n,3\n"), "id")
	if err != nil || !reflect.DeepEqual(got, []string{"A", "B"}) {
		t.Fatalf("Keys = %v, %v", got, err)
	}
}

func TestMergeOrderAndColumns(t *testing.T) {
	a := table(t, "id,qty\nB,1\nA,2\n")
	b := table(t, "note,qty,id\nx,7,C\ny,8,D\n")
	m, err := csvmerge.Merge(a, b, "id")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(m.Header, []string{"id", "qty"}) {
		t.Fatalf("Header = %v", m.Header)
	}
	want := [][]string{{"B", "1"}, {"A", "2"}, {"C", "7"}, {"D", "8"}}
	if !reflect.DeepEqual(m.Rows, want) {
		t.Fatalf("Rows = %v", m.Rows)
	}
}

func TestMergeFirstTableStands(t *testing.T) {
	m, err := csvmerge.Merge(table(t, "id,qty\nA,1\n"), table(t, "id,qty\nA,9\n"), "id")
	if err != nil || !reflect.DeepEqual(m.Rows, [][]string{{"A", "1"}}) {
		t.Fatalf("Merge = %v, %v", m.Rows, err)
	}
}

func TestMergeFirstTableStandsAcrossColumnOrder(t *testing.T) {
	a := table(t, "id,qty,bin\nA,1,x\nB,2,y\n")
	b := table(t, "bin,id,qty\nz,B,9\nw,C,3\n")
	m, err := csvmerge.Merge(a, b, "id")
	want := [][]string{{"A", "1", "x"}, {"B", "2", "y"}, {"C", "3", "w"}}
	if err != nil || !reflect.DeepEqual(m.Rows, want) {
		t.Fatalf("Merge = %v, %v", m.Rows, err)
	}
}

func TestMergeNoKeySentinel(t *testing.T) {
	a := table(t, "id,qty\nA,1\n")
	b := table(t, "sku,qty\nA,1\n")
	if _, err := csvmerge.Merge(a, b, "id"); !errors.Is(err, csvmerge.ErrNoKey) {
		t.Errorf("second table: %v", err)
	}
	if _, err := csvmerge.Merge(b, a, "id"); !errors.Is(err, csvmerge.ErrNoKey) {
		t.Errorf("first table: %v", err)
	}
}

func TestMergeSkipsTotals(t *testing.T) {
	m, err := csvmerge.Merge(table(t, "id,qty\nA,1\n,1\n"), table(t, "id,qty\nB,2\n,5\n"), "id")
	if err != nil {
		t.Fatal(err)
	}
	if got := keysOf(m, 0); !reflect.DeepEqual(got, []string{"A", "B"}) {
		t.Fatalf("keys = %v", got)
	}
}

func TestConflictsSorted(t *testing.T) {
	a := table(t, "id,qty\nZ,1\nA,1\nS,5\nO,1\n")
	b := table(t, "id,qty,note\nA,2,n\nS,5,m\nZ,3,o\n")
	got, err := csvmerge.Conflicts(a, b, "id")
	if err != nil || !reflect.DeepEqual(got, []string{"A", "Z"}) {
		t.Fatalf("Conflicts = %v, %v", got, err)
	}
}

func TestConflictsNoKeySentinel(t *testing.T) {
	a := table(t, "id,qty\nA,1\n")
	b := table(t, "sku,qty\nA,1\n")
	if _, err := csvmerge.Conflicts(a, b, "id"); !errors.Is(err, csvmerge.ErrNoKey) {
		t.Fatalf("err = %v", err)
	}
}

func TestConflictsSkipTotals(t *testing.T) {
	got, err := csvmerge.Conflicts(table(t, "id,qty\n,3\nA,1\n"), table(t, "id,qty\n,4\nA,1\n"), "id")
	if err != nil || len(got) != 0 {
		t.Fatalf("Conflicts = %q, %v", got, err)
	}
}

func TestRenderHeaderFirst(t *testing.T) {
	out, err := csvmerge.Render(table(t, "id,qty\nA,1\nB,2\n"))
	if err != nil {
		t.Fatal(err)
	}
	got := lines(out)
	if len(got) != 3 || !strings.HasPrefix(got[0], "id") || !strings.HasPrefix(got[2], "B") {
		t.Fatalf("Render = %q", out)
	}
}

func TestRenderSemicolons(t *testing.T) {
	out, err := csvmerge.Render(table(t, "id,qty\nA,1\n"))
	if err != nil {
		t.Fatal(err)
	}
	if got := lines(out); len(got) != 2 || got[0] != "id;qty" || got[1] != "A;1" {
		t.Fatalf("Render = %q", out)
	}
}

func TestCountsOneLinePerKey(t *testing.T) {
	out, err := csvmerge.Counts(table(t, "id,qty\nB,1\nA,2\nB,3\n"), "id")
	if err != nil {
		t.Fatal(err)
	}
	got := lines(out)
	if len(got) != 3 || !strings.HasPrefix(got[1], "B") || !strings.HasPrefix(got[2], "A") {
		t.Fatalf("Counts = %q", out)
	}
}

func TestCountsSemicolons(t *testing.T) {
	out, err := csvmerge.Counts(table(t, "id,qty\nB,1\nA,2\nB,3\n"), "id")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"key;count", "B;2", "A;1"}
	if got := lines(out); !reflect.DeepEqual(got, want) {
		t.Fatalf("Counts = %q", out)
	}
}

func TestCountsNoKeySentinel(t *testing.T) {
	if _, err := csvmerge.Counts(table(t, "id,qty\nA,1\n"), "sku"); !errors.Is(err, csvmerge.ErrNoKey) {
		t.Fatalf("err = %v", err)
	}
}

func TestCountsSkipTotals(t *testing.T) {
	out, err := csvmerge.Counts(table(t, "id,qty\nA,1\n,9\n"), "id")
	if err != nil {
		t.Fatal(err)
	}
	if got := lines(out); len(got) != 2 {
		t.Fatalf("Counts = %q", out)
	}
}
`},
		Solution: map[string]string{
			"table.go": `package csvmerge

import (
	"encoding/csv"
	"errors"
	"sort"
	"strconv"
	"strings"
)

// ErrNoKey is for a key column that is not in a table's header.
var ErrNoKey = errors.New("key column missing")

// Table is a parsed CSV file: a header and its data rows.
type Table struct {
	Header []string
	Rows   [][]string
}

// Parse reads CSV text whose first line is the header.
func Parse(text string) (Table, error) {
	records, err := csv.NewReader(strings.NewReader(text)).ReadAll()
	if err != nil {
		return Table{}, err
	}
	if len(records) == 0 {
		return Table{}, nil
	}
	return Table{Header: records[0], Rows: records[1:]}, nil
}

// Column returns the index of the named column, or -1 when it is missing.
func (t Table) Column(name string) int {
	for i, h := range t.Header {
		if h == name {
			return i
		}
	}
	return -1
}

// records returns the rows that are real data, leaving out totals lines.
func records(t Table, col int) [][]string {
	var out [][]string
	for _, r := range t.Rows {
		if r[col] != "" {
			out = append(out, r)
		}
	}
	return out
}

// Keys returns the distinct values of the key column in order of first appearance.
func Keys(t Table, key string) ([]string, error) {
	col := t.Column(key)
	if col < 0 {
		return nil, ErrNoKey
	}
	seen := map[string]bool{}
	var out []string
	for _, r := range records(t, col) {
		if !seen[r[col]] {
			seen[r[col]] = true
			out = append(out, r[col])
		}
	}
	return out, nil
}

// Merge combines two tables into one row per key, under a's header.
func Merge(a, b Table, key string) (Table, error) {
	ka, kb := a.Column(key), b.Column(key)
	if ka < 0 || kb < 0 {
		return Table{}, ErrNoKey
	}
	out := Table{Header: a.Header}
	at := map[string]int{}
	for _, r := range records(a, ka) {
		if _, dup := at[r[ka]]; !dup {
			at[r[ka]] = len(out.Rows)
			out.Rows = append(out.Rows, r)
		}
	}
	for _, r := range records(b, kb) {
		row := make([]string, len(a.Header))
		for i, name := range a.Header {
			if j := b.Column(name); j >= 0 {
				row[i] = r[j]
			}
		}
		if _, dup := at[r[kb]]; dup {
			continue
		}
		at[r[kb]] = len(out.Rows)
		out.Rows = append(out.Rows, row)
	}
	return out, nil
}

// Conflicts returns the keys in both tables whose rows disagree, sorted.
func Conflicts(a, b Table, key string) ([]string, error) {
	ka, kb := a.Column(key), b.Column(key)
	if ka < 0 || kb < 0 {
		return nil, ErrNoKey
	}
	other := map[string][]string{}
	for _, r := range records(b, kb) {
		other[r[kb]] = r
	}
	var out []string
	for _, r := range records(a, ka) {
		s, ok := other[r[ka]]
		if !ok {
			continue
		}
		for i, name := range a.Header {
			if j := b.Column(name); j >= 0 && r[i] != s[j] {
				out = append(out, r[ka])
				break
			}
		}
	}
	sort.Strings(out)
	return out, nil
}

func encode(rows [][]string) (string, error) {
	var b strings.Builder
	w := csv.NewWriter(&b)
	w.Comma = ';'
	if err := w.WriteAll(rows); err != nil {
		return "", err
	}
	return b.String(), nil
}

// Render turns a table into CSV text with the header first.
func Render(t Table) (string, error) {
	return encode(append([][]string{t.Header}, t.Rows...))
}

// Counts reports how many rows each key has.
func Counts(t Table, key string) (string, error) {
	keys, err := Keys(t, key)
	if err != nil {
		return "", err
	}
	col := t.Column(key)
	n := map[string]int{}
	for _, r := range records(t, col) {
		n[r[col]]++
	}
	rows := [][]string{{"key", "count"}}
	for _, k := range keys {
		rows = append(rows, []string{k, strconv.Itoa(n[k])})
	}
	return encode(rows)
}
`,
		},
		Turns: script(
			cloud("Some background for the merge work: the first table passed in is the system of record, so when both tables have a row for the same key its row stands and the other is ignored. Nothing to code for that today. Add `func (t Table) Column(name string) int`, the index of the named column, or -1 if it's missing."),
			cloud("Add `var ErrNoKey = errors.New(\"key column missing\")`. Nothing returns it yet. It's what we return from now on when the key column we're asked to use isn't in a table's header."),
			local("Read docs/partner.txt and give me the one thing in it that matters for how we process rows. No code changes."),
			cloud("Add `func Keys(t Table, key string) ([]string, error)`: the distinct values of the key column, in order of first appearance."),
			local("Add `func Merge(a, b Table, key string) (Table, error)`, which returns one row per key under a's header. Columns of b are matched to a by name, and any that a doesn't have are dropped. Rows from a come first in a's order, then the rows only b has in b's order."),
			local("Add `func Conflicts(a, b Table, key string) ([]string, error)`: the keys present in both tables whose rows disagree in a column they share, sorted. Separately, the finance tool that picks up our output wants semicolons between fields rather than commas, so anything we write out uses those. Nothing writes yet."),
			cloud("Add `func Render(t Table) (string, error)`, which turns a table into CSV text, header line first."),
			local("Add `func Counts(t Table, key string) (string, error)`. It returns CSV text with a header line of key and count, then one line per key in order of first appearance, giving that key and how many rows have it."),
		),
		Carries: []Carry{
			{
				What:        "on a key collision the first table's row stands",
				Kind:        CarryUser,
				Established: 0,
				Needed:      []int{4},
				Tests:       []string{"TestMergeFirstTableStands", "TestMergeFirstTableStandsAcrossColumnOrder"},
				Absent:      []string{"system of record", "stands", "ignored", "wins", "precedence"},
			},
			{
				What:        "ErrNoKey when the key column is not in a header",
				Kind:        CarryEmbodied,
				Established: 1,
				Needed:      []int{3, 4, 5, 7},
				Tests:       []string{"TestKeysNoKeySentinel", "TestMergeNoKeySentinel", "TestConflictsNoKeySentinel", "TestCountsNoKeySentinel"},
				Absent:      []string{"errnokey", "key column missing", "sentinel"},
			},
			{
				What:        "rows with an empty key are totals, not records (docs/partner.txt)",
				Kind:        CarryDiscovered,
				Established: 2,
				Needed:      []int{3, 4, 5, 7},
				Source:      "docs/partner.txt",
				Tests:       []string{"TestKeysSkipTotals", "TestMergeSkipsTotals", "TestConflictsSkipTotals", "TestCountsSkipTotals"},
				Absent:      []string{"totals", "subtotal", "blank", "empty", "partner"},
			},
			{
				What:        "anything written out uses semicolons between fields",
				Kind:        CarryUser,
				Established: 5,
				Needed:      []int{6, 7},
				Tests:       []string{"TestRenderSemicolons", "TestCountsSemicolons"},
				Absent:      []string{"semicolon", "finance", "delimiter", "separator"},
			},
		},
	}
}
