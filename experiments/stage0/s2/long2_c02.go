package s2

func long2C02() Task {
	return Task{
		ID:     "s2-long2-c02-tensile-tests",
		Set:    "long",
		System: system("a tensile test analysis tool for a materials lab"),
		Files: map[string]string{
			"go.mod": goMod,
			"specimen.go": `package tensile

import "math"

// Specimen describes one tensile specimen as the lab system records it.
// Lengths are in mm. A specimen with a diameter is round; one without is
// flat and has a width and a thickness instead.
type Specimen struct {
	ID        string
	Material  string
	Gauge     float64 // gauge length
	Width     float64 // flat specimens only
	Thickness float64 // flat specimens only
	Diameter  float64 // round specimens only; 0 for a flat specimen
}

// Area returns the specimen's cross-section in mm².
func (s Specimen) Area() float64 {
	if s.Diameter > 0 {
		return math.Pi * s.Diameter * s.Diameter / 4
	}
	return s.Width * s.Thickness
}

// Kind returns "round" or "flat", as the lab system shows it.
func (s Specimen) Kind() string {
	if s.Diameter > 0 {
		return "round"
	}
	return "flat"
}
`,
			"curve.go": `package tensile

// Point is one reading from the machine: force in N, extension in mm.
type Point struct {
	Force     float64
	Extension float64
}

// Test is one specimen's tensile test, with its readings in time order.
type Test struct {
	Specimen Specimen
	Points   []Point
}

// MaxForce returns the highest force in the test, or 0 when there are no
// points.
func (t Test) MaxForce() float64 {
	m := 0.0
	for _, p := range t.Points {
		m = max(m, p.Force)
	}
	return m
}

// MaxExtension returns the largest extension in the test, or 0 when there
// are no points.
func (t Test) MaxExtension() float64 {
	m := 0.0
	for _, p := range t.Points {
		m = max(m, p.Extension)
	}
	return m
}
`,
			"tensile_test.go": `package tensile

import (
	"math"
	"testing"
)

func TestArea(t *testing.T) {
	flat := Specimen{ID: "F1", Gauge: 50, Width: 20, Thickness: 5}
	if got := flat.Area(); got != 100 {
		t.Errorf("flat Area = %v", got)
	}
	round := Specimen{ID: "R1", Gauge: 40, Diameter: 10}
	if got := round.Area(); math.Abs(got-78.5398) > 1e-3 {
		t.Errorf("round Area = %v", got)
	}
	if flat.Kind() != "flat" || round.Kind() != "round" {
		t.Errorf("Kind = %q, %q", flat.Kind(), round.Kind())
	}
}

func TestMaxima(t *testing.T) {
	test := Test{Points: []Point{{Force: 10, Extension: 0}, {Force: 30, Extension: 1}, {Force: 20, Extension: 2}}}
	if got := test.MaxForce(); got != 30 {
		t.Errorf("MaxForce = %v", got)
	}
	if got := test.MaxExtension(); got != 2 {
		t.Errorf("MaxExtension = %v", got)
	}
	if (Test{}).MaxForce() != 0 || (Test{}).MaxExtension() != 0 {
		t.Error("an empty test has maxima")
	}
}
`,
			"docs/machine-export.txt": `Export files from the tensile machine in bay 2

The machine writes one text file per specimen when a test finishes, named
after the specimen. Files are UTF-8.

Lines that start with # are comments (operator, date, load cell, crosshead
speed) and carry nothing we use.

The first line that is not a comment is the column header, always
"force;extension". Each line after it is one reading, in time order: the
force in kilonewtons and the extension in millimetres, separated by a
semicolon. The controller runs with a German locale, so numbers are written
with a decimal comma: "12,5" is twelve and a half. There are no thousands
separators.

The readings end at the first blank line. What follows it is the machine's
own summary block (its idea of the maximum force and so on), which we do
not trust and never read.

The machine samples at 50 Hz, so a test of a few minutes gives several
thousand readings. Files are copied to the shared drive each evening; the
copies on the machine itself are deleted after thirty days.
`,
			"docs/spec-sheet.txt": `Material requirements, from the purchasing specification, revision C

Strengths are in MPa and elongation is the percentage at fracture. A single
value is a minimum. A tensile range gives the lowest and the highest
acceptable value, both included; "260+" means 260 or more, with no upper
limit. A dash means the property is not assessed for that material.

material   yield   tensile    elongation
S235       235     360-510    26
S355       355     470-630    22
AL6061     240     260+       10
CU-ETP     -       200-250    30

A specimen meets its material when it meets every assessed requirement.
Materials not listed here cannot be certified by this lab.

Specimens are tested at room temperature. Steel specimens are flat bars
cut across the rolling direction; aluminium and copper specimens are
machined round. Purchasing owns this sheet; send changes to them, not to
the lab.
`,
		},
		Hidden: map[string]string{hiddenName: `package tensile_test

import (
	"errors"
	"math"
	"reflect"
	"strings"
	"testing"

	tensile "fixture"
)

// ref is a stress-strain curve: strain, then stress in MPa. Its peak is at
// index 10 and it breaks at index 12.
var ref = [][2]float64{
	{0, 0}, {0.0002, 20}, {0.0003, 40}, {0.0006, 100}, {0.0009, 160}, {0.0012, 220},
	{0.0016, 300}, {0.0040, 340}, {0.0200, 400}, {0.0800, 440}, {0.1500, 450},
	{0.3000, 400}, {0.3100, 150}, {0.3200, 10},
}

func flat(id, material string) tensile.Specimen {
	return tensile.Specimen{ID: id, Material: material, Gauge: 50, Width: 20, Thickness: 5}
}

// build turns strain and stress into readings for a flat specimen of 100 mm² and 50 mm gauge.
func build(s tensile.Specimen, curve [][2]float64) tensile.Test {
	t := tensile.Test{Specimen: s}
	for _, c := range curve {
		t.Points = append(t.Points, tensile.Point{Force: c[1] * 100, Extension: c[0] * 50})
	}
	return t
}

func near(a, b, tol float64) bool { return math.Abs(a-b) <= tol }

func TestValidate(t *testing.T) {
	bad := []tensile.Specimen{
		{ID: "a", Gauge: 0, Width: 20, Thickness: 5},
		{ID: "b", Gauge: 50, Width: 0, Thickness: 5},
		{ID: "c", Gauge: 50, Diameter: -1},
	}
	for _, s := range bad {
		if err := s.Validate(); !errors.Is(err, tensile.ErrBadData) {
			t.Errorf("%s: %v", s.ID, err)
		}
	}
	if err := (tensile.Specimen{ID: "d", Gauge: 50, Diameter: 8}).Validate(); err != nil {
		t.Errorf("round: %v", err)
	}
}

func TestParseSpecimen(t *testing.T) {
	got, err := tensile.ParseSpecimen("H2291-04,S355,50,20,5,0")
	want := tensile.Specimen{ID: "H2291-04", Material: "S355", Gauge: 50, Width: 20, Thickness: 5}
	if err != nil || got != want {
		t.Fatalf("ParseSpecimen = %+v, %v", got, err)
	}
}

func TestParseSpecimenInvalid(t *testing.T) {
	if _, err := tensile.ParseSpecimen("H1-01,S355,0,20,5,0"); !errors.Is(err, tensile.ErrBadData) {
		t.Fatalf("err = %v", err)
	}
}

func TestParseSpecimenBadNumber(t *testing.T) {
	if _, err := tensile.ParseSpecimen("H1-01,S355,50,2O,5,0"); !errors.Is(err, tensile.ErrBadData) {
		t.Fatalf("err = %v", err)
	}
}

func TestParseSpecimenFieldCount(t *testing.T) {
	if _, err := tensile.ParseSpecimen("H1-01,S355,50,20"); !errors.Is(err, tensile.ErrBadData) {
		t.Fatalf("err = %v", err)
	}
}

func TestParseSpecimenSpaces(t *testing.T) {
	got, err := tensile.ParseSpecimen(" H1-01 , S355 , 50 , 20 , 5 , 0 ")
	if err != nil || got.ID != "H1-01" || got.Material != "S355" || got.Thickness != 5 {
		t.Fatalf("ParseSpecimen = %+v, %v", got, err)
	}
}

func TestParseSpecimenEmptyID(t *testing.T) {
	if _, err := tensile.ParseSpecimen(" ,S355,50,20,5,0"); !errors.Is(err, tensile.ErrBadData) {
		t.Fatalf("err = %v", err)
	}
}

func TestParseReadingsUnits(t *testing.T) {
	text := "# operator: KL\n# load cell 100 kN\nforce;extension\n0,5;0,010\n12,25;1,5\n"
	got, err := tensile.ParseReadings(text)
	want := []tensile.Point{{Force: 500, Extension: 0.01}, {Force: 12250, Extension: 1.5}}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("ParseReadings = %v, %v", got, err)
	}
}

func TestParseReadingsSummaryIgnored(t *testing.T) {
	text := "force;extension\n1,0;0,1\n2,0;0,2\n\nFmax;2,0\nmax ext;0,2\n"
	got, err := tensile.ParseReadings(text)
	if err != nil || len(got) != 2 || got[1].Force != 2000 {
		t.Fatalf("ParseReadings = %v, %v", got, err)
	}
}

func TestParseReadingsBadLine(t *testing.T) {
	if _, err := tensile.ParseReadings("force;extension\n1,0;0,1\nabc;0,2\n"); !errors.Is(err, tensile.ErrBadData) {
		t.Fatalf("err = %v", err)
	}
}

func TestCurve(t *testing.T) {
	test := tensile.Test{Specimen: flat("c1", "S235"), Points: []tensile.Point{{Force: 2000, Extension: 0.5}}}
	got := test.Curve()
	if len(got) != 1 || !near(got[0].Strain, 0.01, 1e-12) || !near(got[0].Stress, 20, 1e-9) {
		t.Fatalf("Curve = %+v", got)
	}
}

func TestUTS(t *testing.T) {
	if got := build(flat("u1", "S235"), ref).UTS(); !near(got, 450, 1e-9) {
		t.Fatalf("UTS = %v", got)
	}
	if got := (tensile.Test{Specimen: flat("u2", "S235")}).UTS(); got != 0 {
		t.Fatalf("UTS of nothing = %v", got)
	}
}

func TestEnergy(t *testing.T) {
	test := tensile.Test{Specimen: flat("e1", "S235"), Points: []tensile.Point{{0, 0}, {1000, 1}, {1000, 3}}}
	if got := test.Energy(); !near(got, 2.5, 1e-12) {
		t.Fatalf("Energy = %v", got)
	}
}

func TestPeakIndex(t *testing.T) {
	test := tensile.Test{Points: []tensile.Point{{1, 0}, {5, 1}, {5, 2}, {2, 3}}}
	if got := test.PeakIndex(); got != 1 {
		t.Fatalf("PeakIndex = %d", got)
	}
	if got := (tensile.Test{}).PeakIndex(); got != -1 {
		t.Fatalf("PeakIndex of nothing = %d", got)
	}
}

func TestMeanStdDev(t *testing.T) {
	if tensile.Mean([]float64{2, 4, 6}) != 4 || tensile.Mean(nil) != 0 {
		t.Error("Mean")
	}
	if !near(tensile.StdDev([]float64{2, 4, 6}), 2, 1e-12) || tensile.StdDev([]float64{5}) != 0 {
		t.Error("StdDev")
	}
}

func TestMedian(t *testing.T) {
	odd := []float64{3, 1, 2}
	if tensile.Median(odd) != 2 || tensile.Median([]float64{4, 1, 3, 2}) != 2.5 || tensile.Median(nil) != 0 {
		t.Error("Median")
	}
	if !reflect.DeepEqual(odd, []float64{3, 1, 2}) {
		t.Errorf("Median changed its input: %v", odd)
	}
}

func TestUsableStopsAtFracture(t *testing.T) {
	got := build(flat("b1", "S235"), ref).Usable()
	if len(got) != 12 || !near(got[11].Extension, 15, 1e-9) {
		t.Fatalf("Usable = %v", got)
	}
}

func TestUsableWithoutFracture(t *testing.T) {
	test := build(flat("b2", "S235"), [][2]float64{{0, 0}, {0.01, 100}, {0.02, 200}, {0.03, 150}})
	if got := test.Usable(); len(got) != 4 {
		t.Fatalf("Usable = %v", got)
	}
}

func TestElongation(t *testing.T) {
	if got := build(flat("b3", "S235"), ref).Elongation(); !near(got, 30, 1e-9) {
		t.Fatalf("Elongation = %v", got)
	}
}

func TestModulus(t *testing.T) {
	got, err := build(flat("m1", "S235"), ref).Modulus()
	if err != nil || !near(got, 200000, 1) {
		t.Fatalf("Modulus = %v, %v", got, err)
	}
}

func TestModulusTooFewPoints(t *testing.T) {
	_, err := build(flat("m2", "S235"), [][2]float64{{0, 0}, {0.01, 100}}).Modulus()
	if !errors.Is(err, tensile.ErrBadData) {
		t.Fatalf("err = %v", err)
	}
}

func TestYield(t *testing.T) {
	got, err := build(flat("y1", "S235"), ref).Yield()
	if err != nil || !near(got, 334.5454545, 0.01) {
		t.Fatalf("Yield = %v, %v", got, err)
	}
}

func TestYieldNeverCrosses(t *testing.T) {
	linear := [][2]float64{{0, 0}, {0.0003, 60}, {0.00045, 90}, {0.0008, 160}, {0.001, 200}}
	if _, err := build(flat("y2", "S235"), linear).Yield(); !errors.Is(err, tensile.ErrBadData) {
		t.Fatalf("err = %v", err)
	}
}

func TestAnalyze(t *testing.T) {
	p, err := tensile.Analyze(build(flat("a1", "S235"), ref))
	if err != nil || !near(p.Yield, 334.5454545, 0.01) || !near(p.Tensile, 450, 1e-9) || !near(p.Elongation, 30, 1e-9) || !near(p.Modulus, 200000, 1) {
		t.Fatalf("Analyze = %+v, %v", p, err)
	}
}

func TestCheckPasses(t *testing.T) {
	got, err := tensile.Check("S355", tensile.Properties{Yield: 380, Tensile: 520, Elongation: 25})
	if err != nil || len(got) != 0 {
		t.Fatalf("Check = %v, %v", got, err)
	}
}

func TestCheckListsFailures(t *testing.T) {
	got, err := tensile.Check("S235", tensile.Properties{Yield: 230, Tensile: 350, Elongation: 20})
	if err != nil || !reflect.DeepEqual(got, []string{"yield", "tensile", "elongation"}) {
		t.Fatalf("Check = %v, %v", got, err)
	}
}

func TestCheckTensileAboveRange(t *testing.T) {
	got, err := tensile.Check("S355", tensile.Properties{Yield: 400, Tensile: 650, Elongation: 24})
	if err != nil || !reflect.DeepEqual(got, []string{"tensile"}) {
		t.Fatalf("Check = %v, %v", got, err)
	}
}

func TestCheckCopper(t *testing.T) {
	got, err := tensile.Check("CU-ETP", tensile.Properties{Yield: 50, Tensile: 260, Elongation: 35})
	if err != nil || !reflect.DeepEqual(got, []string{"tensile"}) {
		t.Fatalf("Check = %v, %v", got, err)
	}
}

func TestCheckOpenEnded(t *testing.T) {
	got, err := tensile.Check("AL6061", tensile.Properties{Yield: 250, Tensile: 900, Elongation: 12})
	if err != nil || len(got) != 0 {
		t.Fatalf("Check = %v, %v", got, err)
	}
}

func TestCheckUnknownMaterial(t *testing.T) {
	if _, err := tensile.Check("S275", tensile.Properties{}); !errors.Is(err, tensile.ErrUnknownMaterial) {
		t.Fatalf("err = %v", err)
	}
}

func TestHeatOf(t *testing.T) {
	if got := tensile.HeatOf("H2291-04"); got != "H2291" {
		t.Errorf("HeatOf = %q", got)
	}
	if got := tensile.HeatOf("H22-91-04"); got != "H22-91" {
		t.Errorf("HeatOf = %q", got)
	}
}

func TestGroupByHeat(t *testing.T) {
	tests := []tensile.Test{
		build(flat("H22-91-01", "S235"), ref),
		build(flat("H23-05-01", "S235"), ref),
		build(flat("H22-91-02", "S235"), ref),
	}
	got := tensile.GroupByHeat(tests)
	a := got["H22-91"]
	if len(got) != 2 || len(a) != 2 || a[1].Specimen.ID != "H22-91-02" || len(got["H23-05"]) != 1 {
		t.Fatalf("GroupByHeat = %v", got)
	}
}

func TestHeatAccepted(t *testing.T) {
	tests := []tensile.Test{build(flat("H1-01", "S235"), ref), build(flat("H1-02", "S235"), ref), build(flat("H1-03", "S235"), ref)}
	if ok, err := tensile.HeatAccepted(tests); err != nil || !ok {
		t.Fatalf("HeatAccepted = %v, %v", ok, err)
	}
}

func TestHeatAcceptedNeedsEnough(t *testing.T) {
	tests := []tensile.Test{build(flat("H1-01", "S235"), ref), build(flat("H1-02", "S235"), ref)}
	if ok, err := tensile.HeatAccepted(tests); err != nil || ok {
		t.Fatalf("HeatAccepted = %v, %v", ok, err)
	}
}

func TestHeatAcceptedOneFails(t *testing.T) {
	tests := []tensile.Test{build(flat("H1-01", "S235"), ref), build(flat("H1-02", "S355"), ref), build(flat("H1-03", "S235"), ref)}
	if ok, err := tensile.HeatAccepted(tests); err != nil || ok {
		t.Fatalf("HeatAccepted = %v, %v", ok, err)
	}
}

func TestSummarizeHeats(t *testing.T) {
	got := tensile.SummarizeHeats(map[string]tensile.Properties{
		"H2-01": {Yield: 310, Tensile: 460, Elongation: 25},
		"H1-01": {Yield: 300, Tensile: 450, Elongation: 30},
		"H1-02": {Yield: 320, Tensile: 470, Elongation: 28},
	})
	want := []tensile.HeatSummary{
		{Heat: "H1", Specimens: 2, MeanYield: 310, MinYield: 300, MeanTensile: 460, MinElongation: 28},
		{Heat: "H2", Specimens: 1, MeanYield: 310, MinYield: 310, MeanTensile: 460, MinElongation: 25},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("SummarizeHeats = %+v", got)
	}
}

func TestOutliers(t *testing.T) {
	results := map[string]tensile.Properties{"H2-01": {Yield: 500}}
	for i, y := range []float64{300, 301, 299, 300, 302, 298, 400} {
		results["H1-0"+string(rune('1'+i))] = tensile.Properties{Yield: y}
	}
	if got := tensile.Outliers(results); !reflect.DeepEqual(got, []string{"H1-07"}) {
		t.Fatalf("Outliers = %v", got)
	}
}

func TestZeroed(t *testing.T) {
	test := tensile.Test{Specimen: flat("z1", "S235"), Points: []tensile.Point{{100, 0.25}, {300, 1.25}}}
	got := test.Zeroed()
	if !reflect.DeepEqual(got.Points, []tensile.Point{{0, 0}, {200, 1}}) || got.Specimen.ID != "z1" {
		t.Fatalf("Zeroed = %+v", got)
	}
	if test.Points[0].Force != 100 {
		t.Fatal("Zeroed changed the original test")
	}
}

func TestCertificateLayout(t *testing.T) {
	got, err := tensile.Certificate(flat("H2291-04", "S355"), tensile.Properties{Yield: 380, Tensile: 520, Elongation: 25, Modulus: 205000})
	want := "specimen H2291-04\nmaterial S355\nyield 380 MPa\ntensile 520 MPa\nelongation 25.0%\nresult pass\n"
	if err != nil || got != want {
		t.Fatalf("Certificate = %q, %v", got, err)
	}
}

func TestCertificateFailure(t *testing.T) {
	got, err := tensile.Certificate(flat("H2291-05", "S355"), tensile.Properties{Yield: 340, Tensile: 650, Elongation: 20})
	if err != nil || !strings.HasSuffix(got, "\nresult fail: yield, tensile, elongation\n") {
		t.Fatalf("Certificate = %q, %v", got, err)
	}
}

func TestCertificateStrengthsRounded(t *testing.T) {
	got, err := tensile.Certificate(flat("H2291-06", "S235"), tensile.Properties{Yield: 334.55, Tensile: 450.9, Elongation: 30})
	if err != nil || !strings.Contains(got, "\nyield 334") || !strings.Contains(got, "\ntensile 450") {
		t.Fatalf("Certificate = %q, %v", got, err)
	}
}

func TestCertificateUnknownMaterial(t *testing.T) {
	if _, err := tensile.Certificate(flat("X1-01", "S275"), tensile.Properties{}); !errors.Is(err, tensile.ErrUnknownMaterial) {
		t.Fatalf("err = %v", err)
	}
}

func TestLabel(t *testing.T) {
	got := tensile.Label(flat("H2291-04", "S355"), tensile.Properties{Yield: 380, Tensile: 520, Elongation: 25.5})
	if got != "H2291-04 / 380 MPa / 520 MPa / 25.5%" {
		t.Fatalf("Label = %q", got)
	}
}

func TestLabelStrengthsRounded(t *testing.T) {
	got := tensile.Label(flat("H2291-07", "S235"), tensile.Properties{Yield: 299.99, Tensile: 410.5, Elongation: 30})
	if !strings.Contains(got, "/ 299") || !strings.Contains(got, "/ 410") {
		t.Fatalf("Label = %q", got)
	}
}

func TestUniformElongation(t *testing.T) {
	if got := build(flat("g1", "S235"), ref).UniformElongation(); !near(got, 15, 1e-9) {
		t.Fatalf("UniformElongation = %v", got)
	}
	if got := (tensile.Test{Specimen: flat("g2", "S235")}).UniformElongation(); got != 0 {
		t.Fatalf("UniformElongation of nothing = %v", got)
	}
}

func TestToughness(t *testing.T) {
	if got := build(flat("k1", "S235"), ref).Toughness(); !near(got, 127.014, 1e-6) {
		t.Fatalf("Toughness = %v", got)
	}
}

func TestHeatReport(t *testing.T) {
	s := tensile.HeatSummary{Heat: "H1", Specimens: 3, MeanYield: 340, MinYield: 331, MeanTensile: 452, MinElongation: 28.5}
	want := "heat H1\nspecimens 3\nmean yield 340 MPa\nlowest yield 331 MPa\nmean tensile 452 MPa\nlowest elongation 28.5%\nverdict accepted\n"
	if got := tensile.HeatReport(s, true); got != want {
		t.Fatalf("HeatReport = %q", got)
	}
	if got := tensile.HeatReport(s, false); !strings.HasSuffix(got, "\nverdict rejected\n") {
		t.Fatalf("HeatReport = %q", got)
	}
}

func TestHeatReportStrengthsRounded(t *testing.T) {
	s := tensile.HeatSummary{Heat: "H2", Specimens: 4, MeanYield: 334.6, MinYield: 330.5, MeanTensile: 450.9, MinElongation: 28}
	got := tensile.HeatReport(s, true)
	if !strings.Contains(got, "\nmean yield 334") || !strings.Contains(got, "\nlowest yield 330") || !strings.Contains(got, "\nmean tensile 450") {
		t.Fatalf("HeatReport = %q", got)
	}
}
`},
		Solution: map[string]string{
			"specimen.go": `package tensile

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
)

// ErrBadData is returned, possibly wrapped, for any data we cannot use.
var ErrBadData = errors.New("bad test data")

// Specimen describes one tensile specimen as the lab system records it.
// Lengths are in mm. A specimen with a diameter is round; one without is
// flat and has a width and a thickness instead.
type Specimen struct {
	ID        string
	Material  string
	Gauge     float64 // gauge length
	Width     float64 // flat specimens only
	Thickness float64 // flat specimens only
	Diameter  float64 // round specimens only; 0 for a flat specimen
}

// Area returns the specimen's cross-section in mm².
func (s Specimen) Area() float64 {
	if s.Diameter > 0 {
		return math.Pi * s.Diameter * s.Diameter / 4
	}
	return s.Width * s.Thickness
}

// Kind returns "round" or "flat", as the lab system shows it.
func (s Specimen) Kind() string {
	if s.Diameter > 0 {
		return "round"
	}
	return "flat"
}

// Validate reports ErrBadData for dimensions we cannot use.
func (s Specimen) Validate() error {
	switch {
	case s.Gauge <= 0:
		return fmt.Errorf("%w: %s: gauge length %v", ErrBadData, s.ID, s.Gauge)
	case s.Diameter < 0:
		return fmt.Errorf("%w: %s: diameter %v", ErrBadData, s.ID, s.Diameter)
	case s.Diameter == 0 && (s.Width <= 0 || s.Thickness <= 0):
		return fmt.Errorf("%w: %s: section %v x %v", ErrBadData, s.ID, s.Width, s.Thickness)
	}
	return nil
}

// ParseSpecimen reads a line from the lab system: ID, material, gauge,
// width, thickness and diameter, separated by commas, the numbers in mm.
func ParseSpecimen(line string) (Specimen, error) {
	fields := strings.Split(line, ",")
	if len(fields) != 6 {
		return Specimen{}, fmt.Errorf("%w: %d fields in %q", ErrBadData, len(fields), line)
	}
	for i := range fields {
		fields[i] = strings.TrimSpace(fields[i])
	}
	if fields[0] == "" {
		return Specimen{}, fmt.Errorf("%w: no ID in %q", ErrBadData, line)
	}
	var nums [4]float64
	for i := range nums {
		v, err := strconv.ParseFloat(fields[2+i], 64)
		if err != nil {
			return Specimen{}, fmt.Errorf("%w: %v", ErrBadData, err)
		}
		nums[i] = v
	}
	s := Specimen{ID: fields[0], Material: fields[1], Gauge: nums[0], Width: nums[1], Thickness: nums[2], Diameter: nums[3]}
	return s, s.Validate()
}
`,
			"curve.go": `package tensile

// Point is one reading from the machine: force in N, extension in mm.
type Point struct {
	Force     float64
	Extension float64
}

// Test is one specimen's tensile test, with its readings in time order.
type Test struct {
	Specimen Specimen
	Points   []Point
}

// MaxForce returns the highest force in the test, or 0 when there are no
// points.
func (t Test) MaxForce() float64 {
	m := 0.0
	for _, p := range t.Points {
		m = max(m, p.Force)
	}
	return m
}

// MaxExtension returns the largest extension in the test, or 0 when there
// are no points.
func (t Test) MaxExtension() float64 {
	m := 0.0
	for _, p := range t.Points {
		m = max(m, p.Extension)
	}
	return m
}

// CurvePoint is one point of the stress-strain curve: strain is extension
// over gauge length, stress is in MPa.
type CurvePoint struct {
	Strain, Stress float64
}

// Curve returns the stress-strain curve, one CurvePoint for every point.
func (t Test) Curve() []CurvePoint {
	area := t.Specimen.Area()
	out := make([]CurvePoint, len(t.Points))
	for i, p := range t.Points {
		out[i] = CurvePoint{Strain: p.Extension / t.Specimen.Gauge, Stress: p.Force / area}
	}
	return out
}

// UTS returns the tensile strength, the highest stress in the curve in MPa,
// or 0 when there are no points.
func (t Test) UTS() float64 {
	m := 0.0
	for _, c := range t.Curve() {
		m = max(m, c.Stress)
	}
	return m
}

// Energy returns the work done in joules: the area under force against
// extension by the trapezoid rule, N times mm being mJ.
func (t Test) Energy() float64 {
	sum := 0.0
	for i := 1; i < len(t.Points); i++ {
		a, b := t.Points[i-1], t.Points[i]
		sum += (a.Force + b.Force) / 2 * (b.Extension - a.Extension)
	}
	return sum / 1000
}

// PeakIndex returns the index of the point with the highest force, the
// first one on a tie, or -1 when there are no points.
func (t Test) PeakIndex() int {
	peak := -1
	for i, p := range t.Points {
		if peak < 0 || p.Force > t.Points[peak].Force {
			peak = i
		}
	}
	return peak
}

// fractureShare is the share of the maximum force below which the specimen
// has broken, once the maximum has passed.
const fractureShare = 0.4

// Usable returns the points that are part of the test: those before
// fracture, the first point after the peak below fractureShare of it.
func (t Test) Usable() []Point {
	peak := t.PeakIndex()
	if peak < 0 {
		return nil
	}
	limit := fractureShare * t.Points[peak].Force
	for i := peak + 1; i < len(t.Points); i++ {
		if t.Points[i].Force < limit {
			return t.Points[:i]
		}
	}
	return t.Points
}

// Zeroed returns the test with the first point's force and extension
// subtracted from every point, which removes the machine's preload.
func (t Test) Zeroed() Test {
	if len(t.Points) == 0 {
		return t
	}
	first := t.Points[0]
	out := Test{Specimen: t.Specimen, Points: make([]Point, len(t.Points))}
	for i, p := range t.Points {
		out.Points[i] = Point{Force: p.Force - first.Force, Extension: p.Extension - first.Extension}
	}
	return out
}
`,
			"import.go": `package tensile

import (
	"fmt"
	"strconv"
	"strings"
)

// ParseReadings reads one of the machine's export files and returns its
// readings, force in N and extension in mm. Comment lines are skipped, the
// first other line is the column header, and the readings end at the first
// blank line, before the machine's own summary.
func ParseReadings(text string) ([]Point, error) {
	var out []Point
	header := false
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "#") {
			continue
		}
		if !header {
			header = true
			continue
		}
		if line == "" {
			break
		}
		p, err := reading(line)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, nil
}

// reading parses one data line: kilonewtons and millimetres, separated by a
// semicolon and written with decimal commas.
func reading(line string) (Point, error) {
	force, ext, ok := strings.Cut(line, ";")
	if !ok {
		return Point{}, fmt.Errorf("%w: reading %q", ErrBadData, line)
	}
	kn, err1 := strconv.ParseFloat(strings.Replace(force, ",", ".", 1), 64)
	mm, err2 := strconv.ParseFloat(strings.Replace(ext, ",", ".", 1), 64)
	if err1 != nil || err2 != nil {
		return Point{}, fmt.Errorf("%w: reading %q", ErrBadData, line)
	}
	return Point{Force: kn * 1000, Extension: mm}, nil
}
`,
			"stats.go": `package tensile

import (
	"math"
	"slices"
)

// Mean returns the mean of the values, or 0 when there are none.
func Mean(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	sum := 0.0
	for _, v := range values {
		sum += v
	}
	return sum / float64(len(values))
}

// StdDev returns the sample standard deviation of the values, dividing by
// n-1, or 0 for fewer than two values.
func StdDev(values []float64) float64 {
	if len(values) < 2 {
		return 0
	}
	m, sum := Mean(values), 0.0
	for _, v := range values {
		sum += (v - m) * (v - m)
	}
	return math.Sqrt(sum / float64(len(values)-1))
}

// Median returns the middle value, or the mean of the two middle ones, or 0
// when there are no values. The input is left as it was.
func Median(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	s := slices.Sorted(slices.Values(values))
	n := len(s)
	if n%2 == 1 {
		return s[n/2]
	}
	return (s[n/2-1] + s[n/2]) / 2
}
`,
			"props.go": `package tensile

import "fmt"

// Properties are a specimen's results: yield and tensile strength in MPa,
// elongation at fracture in percent, modulus in MPa.
type Properties struct {
	Yield, Tensile, Elongation, Modulus float64
}

// The modulus is fitted to the points before the maximum force whose stress
// lies between these shares of the maximum stress.
const (
	fitLow  = 0.10
	fitHigh = 0.50
)

// Modulus returns Young's modulus E in MPa: the least-squares slope of
// stress against strain over the fitting range.
func (t Test) Modulus() (float64, error) {
	curve, top, peak := t.Curve(), t.UTS(), t.PeakIndex()
	var xs, ys []float64
	for i := 0; i < peak; i++ {
		if c := curve[i]; c.Stress >= fitLow*top && c.Stress <= fitHigh*top {
			xs = append(xs, c.Strain)
			ys = append(ys, c.Stress)
		}
	}
	if len(xs) < 2 {
		return 0, fmt.Errorf("%w: %d points to fit the modulus", ErrBadData, len(xs))
	}
	mx, my := Mean(xs), Mean(ys)
	var sxy, sxx float64
	for i := range xs {
		sxy += (xs[i] - mx) * (ys[i] - my)
		sxx += (xs[i] - mx) * (xs[i] - mx)
	}
	return sxy / sxx, nil
}

// offsetStrain is the plastic strain of the 0.2% offset method.
const offsetStrain = 0.002

// Yield returns the 0.2% offset yield strength in MPa: where the curve first
// crosses the line of slope E through offsetStrain, interpolated linearly
// between the first point on or below the line and the point before it.
func (t Test) Yield() (float64, error) {
	e, err := t.Modulus()
	if err != nil {
		return 0, err
	}
	curve := t.Curve()
	for i := 1; i < len(curve); i++ {
		p, c := curve[i-1], curve[i]
		if d1 := c.Stress - e*(c.Strain-offsetStrain); d1 <= 0 {
			d0 := p.Stress - e*(p.Strain-offsetStrain)
			return p.Stress + (c.Stress-p.Stress)*d0/(d0-d1), nil
		}
	}
	return 0, fmt.Errorf("%w: the curve never meets the offset line", ErrBadData)
}

// Elongation returns the strain at the last usable point as a percentage,
// or 0 when there are none.
func (t Test) Elongation() float64 {
	usable := t.Usable()
	if len(usable) == 0 {
		return 0
	}
	return 100 * usable[len(usable)-1].Extension / t.Specimen.Gauge
}

// UniformElongation returns the strain at the maximum force as a
// percentage, or 0 when there are no points.
func (t Test) UniformElongation() float64 {
	peak := t.PeakIndex()
	if peak < 0 {
		return 0
	}
	return 100 * t.Points[peak].Extension / t.Specimen.Gauge
}

// Toughness returns the area under the stress-strain curve over the usable
// points, by the trapezoid rule, in MJ/m³ (MPa times strain).
func (t Test) Toughness() float64 {
	usable := Test{Specimen: t.Specimen, Points: t.Usable()}.Curve()
	sum := 0.0
	for i := 1; i < len(usable); i++ {
		a, b := usable[i-1], usable[i]
		sum += (a.Stress + b.Stress) / 2 * (b.Strain - a.Strain)
	}
	return sum
}

// Analyze returns all four properties of a test.
func Analyze(t Test) (Properties, error) {
	e, err := t.Modulus()
	if err != nil {
		return Properties{}, err
	}
	y, err := t.Yield()
	if err != nil {
		return Properties{}, err
	}
	return Properties{Yield: y, Tensile: t.UTS(), Elongation: t.Elongation(), Modulus: e}, nil
}
`,
			"spec.go": `package tensile

import (
	"errors"
	"fmt"
)

// ErrUnknownMaterial is returned for a material we have no requirements for.
var ErrUnknownMaterial = errors.New("unknown material")

// requirement is one material's line of the purchasing specification. A
// zero yield is not assessed, and a zero tensileMax has no upper limit.
type requirement struct {
	yield, tensileMin, tensileMax, elongation float64
}

// requirements are from docs/spec-sheet.txt.
var requirements = map[string]requirement{
	"S235":   {yield: 235, tensileMin: 360, tensileMax: 510, elongation: 26},
	"S355":   {yield: 355, tensileMin: 470, tensileMax: 630, elongation: 22},
	"AL6061": {yield: 240, tensileMin: 260, elongation: 10},
	"CU-ETP": {tensileMin: 200, tensileMax: 250, elongation: 30},
}

// Check returns the requirements of the material that p does not meet, in
// the order yield, tensile, elongation, and none when it meets them all.
func Check(material string, p Properties) ([]string, error) {
	req, ok := requirements[material]
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrUnknownMaterial, material)
	}
	var failed []string
	if p.Yield < req.yield {
		failed = append(failed, "yield")
	}
	if p.Tensile < req.tensileMin || (req.tensileMax > 0 && p.Tensile > req.tensileMax) {
		failed = append(failed, "tensile")
	}
	if p.Elongation < req.elongation {
		failed = append(failed, "elongation")
	}
	return failed, nil
}
`,
			"heat.go": `package tensile

import (
	"math"
	"sort"
	"strings"
)

// minSpecimens is how many specimens a heat needs before it can be accepted.
const minSpecimens = 3

// HeatOf returns the heat number of a specimen ID: everything before its
// last dash, since heat numbers may contain dashes of their own.
func HeatOf(id string) string {
	if i := strings.LastIndex(id, "-"); i >= 0 {
		return id[:i]
	}
	return id
}

// GroupByHeat returns the tests keyed by the heat of their specimen's ID,
// each heat's tests in input order.
func GroupByHeat(tests []Test) map[string][]Test {
	out := map[string][]Test{}
	for _, t := range tests {
		heat := HeatOf(t.Specimen.ID)
		out[heat] = append(out[heat], t)
	}
	return out
}

// HeatAccepted reports whether one heat's tests accept it.
func HeatAccepted(tests []Test) (bool, error) {
	if len(tests) < minSpecimens {
		return false, nil
	}
	for _, t := range tests {
		p, err := Analyze(t)
		if err != nil {
			return false, err
		}
		failed, err := Check(t.Specimen.Material, p)
		if err != nil {
			return false, err
		}
		if len(failed) > 0 {
			return false, nil
		}
	}
	return true, nil
}

// HeatSummary is the results of one heat's specimens.
type HeatSummary struct {
	Heat                                            string
	Specimens                                       int
	MeanYield, MinYield, MeanTensile, MinElongation float64
}

// SummarizeHeats summarises results, keyed by specimen ID, one summary per
// heat, sorted by heat.
func SummarizeHeats(results map[string]Properties) []HeatSummary {
	byHeat := map[string][]Properties{}
	for id, p := range results {
		byHeat[HeatOf(id)] = append(byHeat[HeatOf(id)], p)
	}
	var out []HeatSummary
	for heat, ps := range byHeat {
		s := HeatSummary{Heat: heat, Specimens: len(ps), MinYield: math.Inf(1), MinElongation: math.Inf(1)}
		var yields, tensiles []float64
		for _, p := range ps {
			yields, tensiles = append(yields, p.Yield), append(tensiles, p.Tensile)
			s.MinYield, s.MinElongation = min(s.MinYield, p.Yield), min(s.MinElongation, p.Elongation)
		}
		s.MeanYield, s.MeanTensile = Mean(yields), Mean(tensiles)
		out = append(out, s)
	}
	sort.Slice(out, func(a, b int) bool { return out[a].Heat < out[b].Heat })
	return out
}

// Outliers returns the IDs of specimens whose yield is more than two
// standard deviations away from the mean yield of their heat, sorted.
func Outliers(results map[string]Properties) []string {
	yields := map[string][]float64{}
	for id, p := range results {
		yields[HeatOf(id)] = append(yields[HeatOf(id)], p.Yield)
	}
	var out []string
	for id, p := range results {
		ys := yields[HeatOf(id)]
		if math.Abs(p.Yield-Mean(ys)) > 2*StdDev(ys) {
			out = append(out, id)
		}
	}
	sort.Strings(out)
	return out
}
`,
			"report.go": `package tensile

import (
	"fmt"
	"math"
	"strings"
)

// mpa writes a strength for customers: whole MPa with the unit, rounded
// down so that we never overstate it.
func mpa(v float64) string { return fmt.Sprintf("%d MPa", int(math.Floor(v))) }

// percent writes an elongation for customers.
func percent(v float64) string { return fmt.Sprintf("%.1f%%", v) }

// Certificate writes a specimen's test certificate, one line per item.
func Certificate(s Specimen, p Properties) (string, error) {
	failed, err := Check(s.Material, p)
	if err != nil {
		return "", err
	}
	result := "pass"
	if len(failed) > 0 {
		result = "fail: " + strings.Join(failed, ", ")
	}
	lines := []string{
		"specimen " + s.ID,
		"material " + s.Material,
		"yield " + mpa(p.Yield),
		"tensile " + mpa(p.Tensile),
		"elongation " + percent(p.Elongation),
		"result " + result,
	}
	return strings.Join(lines, "\n") + "\n", nil
}

// Label returns the one-line tag stuck on a broken specimen.
func Label(s Specimen, p Properties) string {
	return strings.Join([]string{s.ID, mpa(p.Yield), mpa(p.Tensile), percent(p.Elongation)}, " / ")
}

// HeatReport writes the heat report we send customers.
func HeatReport(s HeatSummary, accepted bool) string {
	verdict := "rejected"
	if accepted {
		verdict = "accepted"
	}
	lines := []string{
		"heat " + s.Heat,
		fmt.Sprintf("specimens %d", s.Specimens),
		"mean yield " + mpa(s.MeanYield),
		"lowest yield " + mpa(s.MinYield),
		"mean tensile " + mpa(s.MeanTensile),
		"lowest elongation " + percent(s.MinElongation),
		"verdict " + verdict,
	}
	return strings.Join(lines, "\n") + "\n"
}
`,
		},
		Turns: script(
			cloud("Add `var ErrBadData = errors.New(\"bad test data\")` to specimen.go, and `func (s Specimen) Validate() error`, which returns it, wrapped with detail if you like, when the gauge length is not positive, the diameter is negative, or a flat specimen (diameter 0) has a width or thickness that is not positive. From now on this is the error for any data we cannot use."),
			cloud("Background we'll need later: fracture is the first point after the maximum force where the force falls below 40% of that maximum, and that point and everything after it are not part of the test. Nothing to code for it yet. Add `type CurvePoint struct { Strain, Stress float64 }` and `func (t Test) Curve() []CurvePoint` to curve.go: for every point, the strain (extension over gauge length) and the stress in MPa (force over area)."),
			local("Read docs/machine-export.txt and tell me briefly how the machine's files are laid out. No code changes."),
			local("Add `func (t Test) UTS() float64` to curve.go: the tensile strength, the highest stress in the test's curve, in MPa, or 0 when there are no points."),
			local("For the modulus, which comes later: the lab fits E by least squares to the points before the maximum force whose stress lies between 10% and 50% of the maximum stress. Nothing to code for that yet. Add `func (t Test) Energy() float64` to curve.go: the work done in joules, the area under force against extension by the trapezoid rule over all points (N times mm is mJ)."),
			cloud("About specimen IDs, for the heat work later: an ID is the heat number, a dash, and the specimen's number in that heat, like H2291-04, and heat numbers may contain dashes of their own. Nothing to code for that yet. Start stats.go with `func Mean(values []float64) float64` and `func StdDev(values []float64) float64`, the sample standard deviation (dividing by n-1). Mean is 0 for no values, StdDev 0 for fewer than two."),
			cloud("Read docs/spec-sheet.txt. Then start props.go with `type Properties struct { Yield, Tensile, Elongation, Modulus float64 }` for a specimen's results: yield and tensile strength in MPa, elongation at fracture in percent, modulus in MPa. No functions yet."),
			local("Add `func ParseSpecimen(line string) (Specimen, error)` to specimen.go, for a line from the lab system: ID, material, gauge, width, thickness and diameter, separated by commas, the numbers in mm written with a decimal point. Run Validate on the result before returning it."),
			cloud("Certificates come later, but here is the lab's rule for anything we print for customers: strengths as whole MPa with the unit, like 412 MPa, rounded to the nearest, and elongation as a percentage with one decimal, like 23.5%. Nothing to print yet. Add `func Median(values []float64) float64` to stats.go: the middle value, or the mean of the two middle ones; 0 for no values; the input left as it was."),
			local("Background for accepting heats later: a heat is accepted only when it has at least 3 specimens and every one of them meets its material. Nothing to code for that yet. Add `func (t Test) PeakIndex() int` to curve.go: the index of the point with the highest force, the first one on a tie, or -1 when there are no points."),
			cloud("Add `func ParseReadings(text string) ([]Point, error)` in a new file import.go, for the machine's export files: it returns the readings as Points, force in N and extension in mm."),
			local("Add `func (t Test) Usable() []Point` to curve.go: the points that are part of the test, in order."),
			cloud("Add `func (t Test) Modulus() (float64, error)` to props.go: Young's modulus E in MPa, from the test's curve. It is an error when fewer than two points are there to fit."),
			local("Add `func (t Test) Elongation() float64` to props.go: the strain at the last usable point, as a percentage, or 0 when there are none."),
			cloud("Add `func (t Test) Yield() (float64, error)` to props.go, the 0.2% offset yield strength in MPa. With E from Modulus, find the first point whose stress is at or below E × (strain − 0.002), and interpolate linearly between it and the point before it to get the stress where the curve crosses that line. If no point qualifies, the data is unusable."),
			cloud("QA changed how strengths are rounded for customers: always down to the whole MPa below, so we never overstate them. Still nothing printed. Add `func Analyze(t Test) (Properties, error)` to props.go: all four properties of a test, Tensile being UTS, failing if any of them cannot be computed."),
			local("Add `func HeatOf(id string) string` to specimen.go: the heat number of a specimen ID."),
			local("Add `var ErrUnknownMaterial = errors.New(\"unknown material\")` and `func Check(material string, p Properties) ([]string, error)` to props.go: the requirements for that material that p does not meet, named yield, tensile and elongation, in that order; none when it meets them all; ErrUnknownMaterial for a material we have no requirements for."),
			local("Add `func GroupByHeat(tests []Test) map[string][]Test` to specimen.go: the tests keyed by the heat of their specimen's ID, each heat's tests in input order."),
			cloud("Add `func HeatAccepted(tests []Test) (bool, error)` to props.go: whether one heat's tests accept it, using Analyze and Check on each test. Any error from those is returned."),
			cloud("props.go and specimen.go have grown. Move Check and the requirements into a new spec.go, and HeatOf, GroupByHeat and HeatAccepted into a new heat.go. No behaviour changes."),
			cloud("Add `type HeatSummary struct { Heat string; Specimens int; MeanYield, MinYield, MeanTensile, MinElongation float64 }` and `func SummarizeHeats(results map[string]Properties) []HeatSummary` to heat.go. results is keyed by specimen ID; return one summary per heat, sorted by heat."),
			local("Add `func (t Test) Zeroed() Test` to curve.go: a copy of the test with the first point's force and extension subtracted from every point, which removes the machine's preload. A test without points comes back as it is."),
			local("Add `func Certificate(s Specimen, p Properties) (string, error)` in a new report.go. Its lines are \"specimen <ID>\", \"material <material>\", \"yield <value>\", \"tensile <value>\", \"elongation <value>\" and \"result pass\", or \"result fail: \" followed by the requirements Check says were not met, joined by \", \". Every line ends with a newline. Errors from Check are returned."),
			cloud("Add `func Outliers(results map[string]Properties) []string` to heat.go: the IDs of the specimens whose yield is more than two standard deviations (StdDev) away from the mean yield of their heat, sorted."),
			local("Add `func Label(s Specimen, p Properties) string` to report.go, the one-line tag stuck on a broken specimen: its ID, yield, tensile and elongation, joined by \" / \"."),
			local("ParseSpecimen should accept spaces around each field and ignore them, and refuse a line whose ID is empty."),
			local("Add `func (t Test) UniformElongation() float64` to props.go: the strain at the maximum force (PeakIndex), as a percentage, or 0 when there are no points."),
			cloud("Add `func (t Test) Toughness() float64` to props.go: the area under the stress-strain curve over the usable points, by the trapezoid rule, in MJ/m³ (MPa times strain)."),
			local("Add `func HeatReport(s HeatSummary, accepted bool) string` to report.go, the heat report we send customers. Its lines are \"heat <heat>\", \"specimens <count>\", \"mean yield <value>\", \"lowest yield <value>\", \"mean tensile <value>\", \"lowest elongation <value>\", then \"verdict accepted\" or \"verdict rejected\", each ending with a newline."),
		),
		Carries: []Carry{
			{
				What:        "ErrBadData for any data we cannot use",
				Kind:        CarryEmbodied,
				Established: 0,
				Needed:      []int{7, 10, 12, 14, 26},
				Tests:       []string{"TestParseSpecimenBadNumber", "TestParseSpecimenFieldCount", "TestParseSpecimenEmptyID", "TestParseReadingsBadLine", "TestModulusTooFewPoints", "TestYieldNeverCrosses"},
				Absent:      []string{"errbaddata", "bad test data", "cannot use", "sentinel", "errors.is"},
			},
			{
				What:        "fracture is the first point after the peak below 40% of the maximum force",
				Kind:        CarryUser,
				Established: 1,
				Needed:      []int{11},
				Tests:       []string{"TestUsableStopsAtFracture", "TestElongation", "TestAnalyze", "TestToughness"},
				Absent:      []string{"40%", "falls below", "everything after it"},
			},
			{
				What:        "export files: kN, decimal commas, comments, readings end at a blank line (docs/machine-export.txt)",
				Kind:        CarryDiscovered,
				Established: 2,
				Needed:      []int{10},
				Source:      "docs/machine-export.txt",
				Tests:       []string{"TestParseReadingsUnits", "TestParseReadingsSummaryIgnored"},
				Absent:      []string{"decimal comma", "kilonewton", "semicolon", "blank line", "summary block", "#", "german"},
			},
			{
				What:        "E is fitted by least squares to the points before the peak between 10% and 50% of the maximum stress",
				Kind:        CarryUser,
				Established: 4,
				Needed:      []int{12},
				Tests:       []string{"TestModulus", "TestYield", "TestAnalyze"},
				Absent:      []string{"10%", "50%", "least squares", "lies between"},
			},
			{
				What:        "the heat is everything before the last dash of a specimen ID",
				Kind:        CarryUser,
				Established: 5,
				Needed:      []int{16, 18},
				Tests:       []string{"TestHeatOf", "TestGroupByHeat"},
				Absent:      []string{"dashes of their own", "h2291-04", "last dash", "a dash, and"},
			},
			{
				What:        "tensile ranges have an upper limit too (docs/spec-sheet.txt)",
				Kind:        CarryDiscovered,
				Established: 6,
				Needed:      []int{17, 23},
				Source:      "docs/spec-sheet.txt",
				Tests:       []string{"TestCheckTensileAboveRange", "TestCheckCopper", "TestCertificateFailure"},
				Absent:      []string{"spec-sheet", "470", "630", "s355", "s235", "cu-etp", "al6061", "both included", "upper limit"},
			},
			{
				What:        "customers see strengths as whole MPa with the unit and elongation as a percentage with one decimal",
				Kind:        CarryUser,
				Established: 8,
				Needed:      []int{23, 25, 29},
				Tests:       []string{"TestCertificateLayout", "TestLabel", "TestHeatReport"},
				Absent:      []string{"412 mpa", "23.5%", "one decimal", "with the unit", "to the nearest", "percentage with"},
			},
			{
				What:        "a heat needs at least 3 specimens to be accepted",
				Kind:        CarryUser,
				Established: 9,
				Needed:      []int{19},
				Tests:       []string{"TestHeatAcceptedNeedsEnough"},
				Absent:      []string{"at least 3", "3 specimens", "three specimens", "every one of them"},
			},
			{
				What:        "strengths for customers are rounded down, revised from rounding to the nearest",
				Kind:        CarryUser,
				Established: 15,
				Needed:      []int{23, 25, 29},
				Tests:       []string{"TestCertificateStrengthsRounded", "TestLabelStrengthsRounded", "TestHeatReportStrengthsRounded"},
				Absent:      []string{"always down", "rounded down", "whole mpa below", "overstate", "floor"},
			},
		},
	}
}
