package s2

func accC01() Task {
	return Task{
		ID:     "s2-c01-unit-conversion",
		Set:    "acceptance",
		System: system("a unit conversion library"),
		Files: map[string]string{
			"go.mod": goMod,
			"units.go": `package units

// metres gives the size of each length unit in metres.
var metres = map[string]float64{
	"m":  1,
	"km": 1000,
	"cm": 0.01,
	"in": 0.0254,
	"ft": 0.3048,
	"mi": 1609.344,
}

// Known reports whether the unit is in the table.
func Known(unit string) bool {
	_, ok := metres[unit]
	return ok
}
`,
			"docs/feeds.txt": `Partner feed notes

The partner feed is typed up by hand. A unit can arrive in any mix of letter
case and with stray spaces on either side, so " KM" and "km" are the same unit.
Anything that takes a unit name from a caller has to treat them as the same.

The feed is delivered once a day and is never larger than a few thousand rows.
`,
			"units_test.go": `package units

import "testing"

func TestKnown(t *testing.T) {
	if !Known("km") || Known("furlong") {
		t.Fatal("wrong table")
	}
}
`,
		},
		Hidden: map[string]string{hiddenName: `package units_test

import (
	"errors"
	"math"
	"testing"

	units "fixture"
)

func near(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func TestNamesSorted(t *testing.T) {
	got := units.Names()
	want := []string{"cm", "ft", "in", "km", "m", "mi"}
	if len(got) != len(want) {
		t.Fatalf("Names = %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Names = %v", got)
		}
	}
}

func TestConvertBetweenUnits(t *testing.T) {
	got, err := units.Convert(2, "km", "m")
	if err != nil || !near(got, 2000) {
		t.Fatalf("2 km = %v m, %v", got, err)
	}
	got, err = units.Convert(250, "cm", "m")
	if err != nil || !near(got, 2.5) {
		t.Fatalf("250 cm = %v m, %v", got, err)
	}
}

func TestConvertRounds(t *testing.T) {
	got, err := units.Convert(1, "cm", "in")
	if err != nil || !near(got, 0.394) {
		t.Fatalf("1 cm = %v in, %v", got, err)
	}
	got, err = units.Convert(-3, "ft", "km")
	if err != nil || !near(got, -0.001) {
		t.Fatalf("-3 ft = %v km, %v", got, err)
	}
}

func TestConvertUnknownSentinel(t *testing.T) {
	if _, err := units.Convert(1, "furlong", "m"); !errors.Is(err, units.ErrUnknownUnit) {
		t.Errorf("from: %v", err)
	}
	if _, err := units.Convert(1, "m", "furlong"); !errors.Is(err, units.ErrUnknownUnit) {
		t.Errorf("to: %v", err)
	}
}

func TestConvertAllKeepsInput(t *testing.T) {
	in := []float64{1, 2}
	got, err := units.ConvertAll(in, "km", "m")
	if err != nil || len(got) != 2 || !near(got[0], 1000) || !near(got[1], 2000) {
		t.Fatalf("ConvertAll = %v, %v", got, err)
	}
	if in[0] != 1 || in[1] != 2 {
		t.Fatalf("input changed: %v", in)
	}
}

func TestConvertAllRounds(t *testing.T) {
	got, err := units.ConvertAll([]float64{1, 2}, "cm", "in")
	if err != nil || len(got) != 2 || !near(got[0], 0.394) || !near(got[1], 0.787) {
		t.Fatalf("ConvertAll = %v, %v", got, err)
	}
}

func TestConvertAllFeedNames(t *testing.T) {
	got, err := units.ConvertAll([]float64{3}, " KM", "m ")
	if err != nil || len(got) != 1 || !near(got[0], 3000) {
		t.Fatalf("ConvertAll = %v, %v", got, err)
	}
}

func TestParseNumberAndUnit(t *testing.T) {
	v, unit, err := units.Parse("2.5 km")
	if err != nil || v != 2.5 || unit != "km" {
		t.Fatalf("Parse = %v, %q, %v", v, unit, err)
	}
}

func TestParseRejectsGarbage(t *testing.T) {
	for _, s := range []string{"", "km", "two km", "2"} {
		if _, _, err := units.Parse(s); err == nil {
			t.Errorf("Parse(%q) gave no error", s)
		}
	}
}

func TestParseUnknownSentinel(t *testing.T) {
	if _, _, err := units.Parse("2 furlong"); !errors.Is(err, units.ErrUnknownUnit) {
		t.Fatalf("err = %v", err)
	}
}

func TestParseFeedNames(t *testing.T) {
	v, unit, err := units.Parse("7 Mi")
	if err != nil || v != 7 || unit != "mi" {
		t.Fatalf("Parse = %v, %q, %v", v, unit, err)
	}
}

func TestSumAdds(t *testing.T) {
	got, err := units.Sum([]string{"2 km", "500 m"}, "m")
	if err != nil || !near(got, 2500) {
		t.Fatalf("Sum = %v, %v", got, err)
	}
}

func TestSumRounds(t *testing.T) {
	got, err := units.Sum([]string{"1 ft", "1 in"}, "m")
	if err != nil || !near(got, 0.33) {
		t.Fatalf("Sum = %v, %v", got, err)
	}
}

func TestSumUnknownSentinel(t *testing.T) {
	if _, err := units.Sum([]string{"1 furlong"}, "m"); !errors.Is(err, units.ErrUnknownUnit) {
		t.Errorf("part: %v", err)
	}
	if _, err := units.Sum([]string{"1 m"}, "furlong"); !errors.Is(err, units.ErrUnknownUnit) {
		t.Errorf("target: %v", err)
	}
}

func TestSumFeedNames(t *testing.T) {
	got, err := units.Sum([]string{"1 KM", "1 Km"}, " M")
	if err != nil || !near(got, 2000) {
		t.Fatalf("Sum = %v, %v", got, err)
	}
}
`},
		Solution: map[string]string{
			"units.go": `package units

import (
	"errors"
	"math"
	"sort"
	"strconv"
	"strings"
)

// ErrUnknownUnit is what callers check for when a unit is not in the table.
var ErrUnknownUnit = errors.New("unknown unit")

// metres gives the size of each length unit in metres.
var metres = map[string]float64{
	"m":  1,
	"km": 1000,
	"cm": 0.01,
	"in": 0.0254,
	"ft": 0.3048,
	"mi": 1609.344,
}

// Known reports whether the unit is in the table.
func Known(unit string) bool {
	_, ok := metres[unit]
	return ok
}

// Names returns the units in the table in alphabetical order.
func Names() []string {
	names := make([]string, 0, len(metres))
	for name := range metres {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// normal puts a unit name as a caller wrote it into the form the table uses.
func normal(unit string) string { return strings.ToLower(strings.TrimSpace(unit)) }

// tidy gives a result the precision the spec sheets print.
func tidy(v float64) float64 { return math.Round(v*1000) / 1000 }

func size(unit string) (float64, error) {
	f, ok := metres[normal(unit)]
	if !ok {
		return 0, ErrUnknownUnit
	}
	return f, nil
}

// Convert converts a length between two units in the table.
func Convert(value float64, from, to string) (float64, error) {
	f, err := size(from)
	if err != nil {
		return 0, err
	}
	t, err := size(to)
	if err != nil {
		return 0, err
	}
	return tidy(value * f / t), nil
}

// ConvertAll converts every value into a new slice.
func ConvertAll(values []float64, from, to string) ([]float64, error) {
	out := make([]float64, len(values))
	for i, v := range values {
		c, err := Convert(v, from, to)
		if err != nil {
			return nil, err
		}
		out[i] = c
	}
	return out, nil
}

// Parse splits a quantity such as "2 km" into its number and its unit.
func Parse(s string) (float64, string, error) {
	fields := strings.Fields(s)
	if len(fields) != 2 {
		return 0, "", errors.New("want a number and a unit")
	}
	v, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return 0, "", err
	}
	if _, err := size(fields[1]); err != nil {
		return 0, "", err
	}
	return v, normal(fields[1]), nil
}

// Sum adds quantities and returns the total in the unit to.
func Sum(parts []string, to string) (float64, error) {
	total := 0.0
	for _, part := range parts {
		v, unit, err := Parse(part)
		if err != nil {
			return 0, err
		}
		total += v * metres[unit]
	}
	return Convert(total, "m", to)
}
`,
		},
		Turns: script(
			local("One rule for this package before anything else: whatever a conversion returns goes straight onto the printed spec sheets, so results are rounded to 3 decimal places. Nothing needs that yet. Add `func Names() []string`, the units in the table in alphabetical order."),
			cloud("Add `var ErrUnknownUnit = errors.New(\"unknown unit\")`. Nothing returns it yet, but it's what callers will check for whenever a unit isn't one we have."),
			cloud("Add `func Convert(value float64, from, to string) (float64, error)`, which converts a length between any two units in the table."),
			local("Read docs/feeds.txt and tell me in a line or two what it means for this package. No code changes."),
			cloud("Add `func ConvertAll(values []float64, from, to string) ([]float64, error)`. It returns a new slice and leaves the input alone; on an error it returns no partial result."),
			local("Add `func Parse(s string) (float64, string, error)`. It takes a quantity such as \"2 km\" and returns the number and the unit, with the unit spelled as in the table. A missing or bad number, or a unit we don't have, is an error."),
			local("Add `func Sum(parts []string, to string) (float64, error)`. It adds up quantities written as for Parse, which can be in different units, and returns the total expressed in the unit to."),
		),
		Carries: []Carry{
			{
				What:        "conversion results are rounded to 3 decimal places",
				Kind:        CarryUser,
				Established: 0,
				Needed:      []int{2, 4, 6},
				Tests:       []string{"TestConvertRounds", "TestConvertAllRounds", "TestSumRounds"},
				Absent:      []string{"round", "decimal", "3 places", "three places", "spec sheet"},
			},
			{
				What:        "ErrUnknownUnit for any unit not in the table",
				Kind:        CarryEmbodied,
				Established: 1,
				Needed:      []int{2, 5, 6},
				Tests:       []string{"TestConvertUnknownSentinel", "TestParseUnknownSentinel", "TestSumUnknownSentinel"},
				Absent:      []string{"errunknownunit", "sentinel", "check for"},
			},
			{
				What:        "unit names match in any letter case with stray spaces (docs/feeds.txt)",
				Kind:        CarryDiscovered,
				Established: 3,
				Needed:      []int{4, 5, 6},
				Source:      "docs/feeds.txt",
				Tests:       []string{"TestConvertAllFeedNames", "TestParseFeedNames", "TestSumFeedNames"},
				Absent:      []string{"letter case", "spaces", "whitespace", "trim", "upper", "lower", "feed"},
			},
		},
	}
}
