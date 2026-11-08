package s2

func task07() Task {
	return Task{
		ID:     "s2-07-sales-export",
		Set:    "tuning",
		System: system("a sales reporting library"),
		Files: map[string]string{
			"go.mod": goMod,
			"sale.go": `package report

// Sale is one recorded sale.
type Sale struct {
	Region string
	Rep    string
	Cents  int
}
`,
			"totals.go": `package report

// Sum returns the total cents over all sales.
func Sum(sales []Sale) int {
	total := 0
	for _, s := range sales {
		total += s.Cents
	}
	return total
}
`,
			"importer/spec.txt": `Importer file format, version 3

The first line is always the header, naming the columns in lower case.
Columns are separated by a semicolon.
Amounts are written in currency units with a decimal comma and exactly two
decimals, for example 12,50.
Every line ends with a single newline, including the last.
`,
			"report_test.go": `package report

import "testing"

func TestSum(t *testing.T) {
	if got := Sum([]Sale{{Cents: 5}, {Cents: 7}}); got != 12 {
		t.Fatalf("Sum = %d", got)
	}
}
`,
		},
		Hidden: map[string]string{hiddenName: `package report_test

import (
	"strings"
	"testing"

	report "fixture"
)

func TestReps(t *testing.T) {
	got := report.Reps([]report.Sale{{Rep: "Bo"}, {Rep: "Al"}, {Rep: "Bo"}})
	if len(got) != 2 || got[0] != "Al" || got[1] != "Bo" {
		t.Fatalf("Reps = %v", got)
	}
}

func TestExportRowsForm(t *testing.T) {
	got := report.ExportRows([]report.Sale{{Region: "North", Rep: "Ann", Cents: 1250}, {Region: "South", Rep: "Bob", Cents: 5}})
	want := "region;rep;amount\nNorth;Ann;12,50\nSouth;Bob;0,05\n"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestExportRowsUnassigned(t *testing.T) {
	got := report.ExportRows([]report.Sale{{Rep: "Bob", Cents: 5}})
	if !strings.Contains(got, "UNASSIGNED") {
		t.Fatalf("got %q", got)
	}
}

func TestExportRowsSkipsZero(t *testing.T) {
	got := report.ExportRows([]report.Sale{{Region: "North", Rep: "Zed", Cents: 0}, {Region: "North", Rep: "Ann", Cents: 100}})
	if strings.Contains(got, "Zed") || !strings.Contains(got, "Ann") {
		t.Fatalf("got %q", got)
	}
}

func TestCountSkipsZero(t *testing.T) {
	if got := report.Count([]report.Sale{{Cents: 0}, {Cents: 3}, {Cents: 4}}); got != 2 {
		t.Fatalf("Count = %d", got)
	}
}

func TestByRegionTotals(t *testing.T) {
	got := report.ByRegion([]report.Sale{{Region: "North", Cents: 5}, {Region: "North", Cents: 7}, {Region: "South", Cents: 1}})
	if len(got) != 2 || got["North"] != 12 || got["South"] != 1 {
		t.Fatalf("ByRegion = %v", got)
	}
}

func TestByRegionUnassigned(t *testing.T) {
	got := report.ByRegion([]report.Sale{{Cents: 5}})
	if len(got) != 1 || got["UNASSIGNED"] != 5 {
		t.Fatalf("ByRegion = %v", got)
	}
}

func TestByRegionOmitsZero(t *testing.T) {
	got := report.ByRegion([]report.Sale{{Region: "Ghost", Cents: 0}, {Region: "North", Cents: 5}})
	if _, ok := got["Ghost"]; ok || got["North"] != 5 {
		t.Fatalf("ByRegion = %v", got)
	}
}

func TestExportRegionsForm(t *testing.T) {
	got := report.ExportRegions([]report.Sale{{Region: "South", Cents: 550}, {Region: "North", Cents: 2000}})
	want := "region;amount\nNorth;20,00\nSouth;5,50\n"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestExportRegionsOrder(t *testing.T) {
	got := report.ExportRegions([]report.Sale{{Region: "South", Cents: 1}, {Region: "North", Cents: 2}, {Region: "West", Cents: 3}})
	west, north, south := strings.Index(got, "West"), strings.Index(got, "North"), strings.Index(got, "South")
	if west < 0 || !(west < north && north < south) {
		t.Fatalf("got %q", got)
	}
}

func TestExportRegionsUnassigned(t *testing.T) {
	if got := report.ExportRegions([]report.Sale{{Cents: 5}}); !strings.Contains(got, "UNASSIGNED") {
		t.Fatalf("got %q", got)
	}
}

func TestExportRegionsOmitsZero(t *testing.T) {
	got := report.ExportRegions([]report.Sale{{Region: "Ghost", Cents: 0}, {Region: "North", Cents: 5}})
	if strings.Contains(got, "Ghost") {
		t.Fatalf("got %q", got)
	}
}

func TestTopOrder(t *testing.T) {
	got := report.Top([]report.Sale{{Rep: "a", Cents: 1}, {Rep: "b", Cents: 9}, {Rep: "c", Cents: 5}}, 2)
	if len(got) != 2 || got[0].Rep != "b" || got[1].Rep != "c" {
		t.Fatalf("Top = %+v", got)
	}
	if got := report.Top([]report.Sale{{Rep: "a", Cents: 1}}, 5); len(got) != 1 {
		t.Fatalf("Top = %+v", got)
	}
}

func TestTopOmitsZero(t *testing.T) {
	got := report.Top([]report.Sale{{Rep: "a", Cents: 4}, {Rep: "b", Cents: 0}}, 5)
	if len(got) != 1 || got[0].Rep != "a" {
		t.Fatalf("Top = %+v", got)
	}
}
`},
		Solution: map[string]string{
			"export.go": `package report

import (
	"fmt"
	"sort"
	"strings"
)

// Reps returns the distinct reps, sorted.
func Reps(sales []Sale) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range sales {
		if !seen[s.Rep] {
			seen[s.Rep] = true
			out = append(out, s.Rep)
		}
	}
	sort.Strings(out)
	return out
}

func label(region string) string {
	if region == "" {
		return "UNASSIGNED"
	}
	return region
}

func amount(cents int) string { return fmt.Sprintf("%d,%02d", cents/100, cents%100) }

// ExportRows renders the sales as an importer file.
func ExportRows(sales []Sale) string {
	var b strings.Builder
	b.WriteString("region;rep;amount\n")
	for _, s := range sales {
		if s.Cents != 0 {
			fmt.Fprintf(&b, "%s;%s;%s\n", label(s.Region), s.Rep, amount(s.Cents))
		}
	}
	return b.String()
}

// Count returns the number of sales that count.
func Count(sales []Sale) int {
	n := 0
	for _, s := range sales {
		if s.Cents != 0 {
			n++
		}
	}
	return n
}

// ByRegion returns total cents per region.
func ByRegion(sales []Sale) map[string]int {
	out := map[string]int{}
	for _, s := range sales {
		if s.Cents != 0 {
			out[label(s.Region)] += s.Cents
		}
	}
	return out
}

// ExportRegions renders region totals as an importer file, biggest first.
func ExportRegions(sales []Sale) string {
	totals := ByRegion(sales)
	regions := make([]string, 0, len(totals))
	for r := range totals {
		regions = append(regions, r)
	}
	sort.Slice(regions, func(a, b int) bool {
		if totals[regions[a]] != totals[regions[b]] {
			return totals[regions[a]] > totals[regions[b]]
		}
		return regions[a] < regions[b]
	})
	var b strings.Builder
	b.WriteString("region;amount\n")
	for _, r := range regions {
		fmt.Fprintf(&b, "%s;%s\n", r, amount(totals[r]))
	}
	return b.String()
}

// Top returns the n biggest sales, biggest first.
func Top(sales []Sale, n int) []Sale {
	out := make([]Sale, 0, len(sales))
	for _, s := range sales {
		if s.Cents != 0 {
			out = append(out, s)
		}
	}
	sort.SliceStable(out, func(a, b int) bool { return out[a].Cents > out[b].Cents })
	return out[:min(n, len(out))]
}
`,
		},
		Turns: script(
			cloud("Add `func Reps(sales []Sale) []string`, the distinct reps, sorted. Also a business rule for all the reporting we're about to add: a sale with an empty Region belongs to the region UNASSIGNED, and that label is what appears wherever regions are shown."),
			cloud("Read importer/spec.txt and summarise what a file for the importer must look like. No code changes."),
			local("Add `func ExportRows(sales []Sale) string`, an importer file of the sales with the columns region, rep and amount, one row per sale in the order given."),
			local("New rule from the sales director: sales of zero cents are noise and never show up in any report or export. Update ExportRows, and add `func Count(sales []Sale) int`, the number of sales that count."),
			cloud("Add `func ByRegion(sales []Sale) map[string]int`: total cents per region."),
			cloud("Add `func ExportRegions(sales []Sale) string`, an importer file of region totals with the columns region and amount, biggest total first."),
			local("Add `func Top(sales []Sale, n int) []Sale`: the n biggest sales, biggest first. With fewer than n to show, return what there is."),
		),
		Carries: []Carry{
			{
				What:        "an empty Region is shown as UNASSIGNED",
				Kind:        CarryUser,
				Established: 0,
				Needed:      []int{2, 4, 5},
				Tests:       []string{"TestExportRowsUnassigned", "TestByRegionUnassigned", "TestExportRegionsUnassigned"},
				Absent:      []string{"unassigned", "empty region"},
			},
			{
				What:        "importer files use a header, semicolons and a decimal comma (importer/spec.txt)",
				Kind:        CarryDiscovered,
				Established: 1,
				Needed:      []int{2, 5},
				Source:      "importer/spec.txt",
				Tests:       []string{"TestExportRowsForm", "TestExportRegionsForm"},
				Absent:      []string{"semicolon", "decimal", "header"},
			},
			{
				What:        "zero-cent sales never show up in any report or export",
				Kind:        CarryEmbodied,
				Established: 3,
				Needed:      []int{4, 5, 6},
				Tests:       []string{"TestByRegionOmitsZero", "TestExportRegionsOmitsZero", "TestTopOmitsZero"},
				Absent:      []string{"zero", "noise"},
			},
		},
	}
}
