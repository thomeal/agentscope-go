package s2

func task10() Task {
	return Task{
		ID:     "s2-10-sensor-stats",
		Set:    "tuning",
		System: system("a statistics library for sensor readings"),
		Files: map[string]string{
			"go.mod": goMod,
			"readings.go": `package stats

import (
	"strconv"
	"strings"
)

// ParseReadings reads one number per line.
func ParseReadings(text string) ([]float64, error) {
	var out []float64
	for _, line := range strings.Split(text, "\n") {
		v, err := strconv.ParseFloat(strings.TrimSpace(line), 64)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}
`,
			"docs/percentile.txt": `How we compute percentiles

Dashboards use the nearest-rank method with no interpolation: sort ascending, take
rank = ceil(p/100 * N) counting from 1, and return the value at that rank.
p = 0 returns the smallest value.
`,
			"readings_test.go": `package stats

import "testing"

func TestParseReadings(t *testing.T) {
	got, err := ParseReadings("1.5\n2")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != 1.5 || got[1] != 2 {
		t.Fatalf("got %v", got)
	}
}
`,
		},
		Hidden: map[string]string{hiddenName: `package stats_test

import (
	"testing"

	stats "fixture"
)

// call keeps one statistic's panic from ending the other tests.
func call(f func() float64) (v float64, panicked bool) {
	defer func() {
		if recover() != nil {
			panicked = true
		}
	}()
	return f(), false
}

func wantZero(t *testing.T, f func() float64) {
	t.Helper()
	v, panicked := call(f)
	if panicked {
		t.Fatal("panicked")
	}
	if v != 0 {
		t.Fatalf("got %v, want 0", v)
	}
}

func same(a, b []float64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestParseSkipsBlankLines(t *testing.T) {
	got, err := stats.ParseReadings("1\n\n 2 \n\n")
	if err != nil || !same(got, []float64{1, 2}) {
		t.Fatalf("got %v, %v", got, err)
	}
}

func TestMeanPlain(t *testing.T) {
	if got := stats.Mean([]float64{2, 4, 9}); got != 5 {
		t.Fatalf("Mean = %v", got)
	}
}

func TestMaxPlain(t *testing.T) {
	if got := stats.Max([]float64{2, 9, 4}); got != 9 {
		t.Fatalf("Max = %v", got)
	}
}

func TestMedianPlain(t *testing.T) {
	if got := stats.Median([]float64{9, 1, 5}); got != 5 {
		t.Errorf("odd count: %v", got)
	}
	if got := stats.Median([]float64{9, 1, 5, 3}); got != 4 {
		t.Errorf("even count: %v", got)
	}
}

func TestPercentileLargest(t *testing.T) {
	if got := stats.Percentile([]float64{3, 1, 2}, 100); got != 3 {
		t.Fatalf("Percentile = %v", got)
	}
}

func TestSpreadPlain(t *testing.T) {
	if got := stats.Spread([]float64{4, 9, 2}); got != 7 {
		t.Fatalf("Spread = %v", got)
	}
}

func TestMeanSkipsFaults(t *testing.T) {
	if got := stats.Mean([]float64{4, -100, 6}); got != 5 {
		t.Fatalf("Mean = %v", got)
	}
}

// A fault can only change the largest value when nothing else is left, so this
// depends on both rules.
func TestMaxSkipsFaults(t *testing.T) {
	wantZero(t, func() float64 { return stats.Max([]float64{-1, -2}) })
}

func TestMedianSkipsFaults(t *testing.T) {
	if got := stats.Median([]float64{1, -5, 3, 100}); got != 3 {
		t.Fatalf("Median = %v", got)
	}
}

func TestPercentileSkipsFaults(t *testing.T) {
	if got := stats.Percentile([]float64{-1, 10, 20, 30}, 0); got != 10 {
		t.Fatalf("Percentile = %v", got)
	}
}

func TestSpreadSkipsFaults(t *testing.T) {
	if got := stats.Spread([]float64{5, -1, 2}); got != 3 {
		t.Fatalf("Spread = %v", got)
	}
}

func TestAllFaultsIsZero(t *testing.T) {
	wantZero(t, func() float64 { return stats.Mean([]float64{-1, -2}) })
}

func TestEmptyMean(t *testing.T) {
	wantZero(t, func() float64 { return stats.Mean(nil) })
}

func TestEmptyMax(t *testing.T) {
	wantZero(t, func() float64 { return stats.Max(nil) })
}

func TestEmptyMedian(t *testing.T) {
	wantZero(t, func() float64 { return stats.Median(nil) })
}

func TestEmptyPercentile(t *testing.T) {
	wantZero(t, func() float64 { return stats.Percentile(nil, 50) })
}

func TestEmptySpread(t *testing.T) {
	wantZero(t, func() float64 { return stats.Spread(nil) })
}

func TestPercentileNearestRank(t *testing.T) {
	if got := stats.Percentile([]float64{40, 10, 30, 20}, 50); got != 20 {
		t.Errorf("p50 = %v", got)
	}
	if got := stats.Percentile([]float64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}, 90); got != 9 {
		t.Errorf("p90 = %v", got)
	}
}

func TestSortedResult(t *testing.T) {
	if got := stats.Sorted([]float64{3, 1, 2}); !same(got, []float64{1, 2, 3}) {
		t.Fatalf("Sorted = %v", got)
	}
}

func TestSortedKeepsInput(t *testing.T) {
	in := []float64{3, 1, 2}
	stats.Sorted(in)
	if !same(in, []float64{3, 1, 2}) {
		t.Fatalf("input became %v", in)
	}
}

func TestTopNResult(t *testing.T) {
	if got := stats.TopN([]float64{3, 1, 2}, 2); !same(got, []float64{3, 2}) {
		t.Errorf("TopN = %v", got)
	}
	if got := stats.TopN([]float64{3, 1}, 5); !same(got, []float64{3, 1}) {
		t.Errorf("TopN = %v", got)
	}
}

func TestTopNKeepsInput(t *testing.T) {
	in := []float64{1, 3, 2}
	stats.TopN(in, 2)
	if !same(in, []float64{1, 3, 2}) {
		t.Fatalf("input became %v", in)
	}
}
`},
		Solution: map[string]string{
			"readings.go": `package stats

import (
	"strconv"
	"strings"
)

// ParseReadings reads one number per line. Blank lines are skipped.
func ParseReadings(text string) ([]float64, error) {
	var out []float64
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		v, err := strconv.ParseFloat(line, 64)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}
`,
			"stats.go": `package stats

import (
	"math"
	"slices"
	"sort"
)

// usable returns a sorted copy of the readings that are not sensor faults.
func usable(values []float64) []float64 {
	out := make([]float64, 0, len(values))
	for _, v := range values {
		if v >= 0 {
			out = append(out, v)
		}
	}
	sort.Float64s(out)
	return out
}

// Mean returns the average of the usable readings.
func Mean(values []float64) float64 {
	v := usable(values)
	if len(v) == 0 {
		return 0
	}
	sum := 0.0
	for _, x := range v {
		sum += x
	}
	return sum / float64(len(v))
}

// Max returns the largest usable reading.
func Max(values []float64) float64 {
	v := usable(values)
	if len(v) == 0 {
		return 0
	}
	return v[len(v)-1]
}

// Median returns the middle usable reading.
func Median(values []float64) float64 {
	v := usable(values)
	if len(v) == 0 {
		return 0
	}
	mid := len(v) / 2
	if len(v)%2 == 1 {
		return v[mid]
	}
	return (v[mid-1] + v[mid]) / 2
}

// Percentile returns the nearest-rank percentile of the usable readings.
func Percentile(values []float64, p float64) float64 {
	v := usable(values)
	if len(v) == 0 {
		return 0
	}
	rank := int(math.Ceil(p / 100 * float64(len(v))))
	return v[max(rank, 1)-1]
}

// Spread returns the largest usable reading minus the smallest.
func Spread(values []float64) float64 {
	v := usable(values)
	if len(v) == 0 {
		return 0
	}
	return v[len(v)-1] - v[0]
}

// Sorted returns the values in ascending order.
func Sorted(values []float64) []float64 {
	out := slices.Clone(values)
	sort.Float64s(out)
	return out
}

// TopN returns the n largest values, largest first.
func TopN(values []float64, n int) []float64 {
	out := slices.Clone(values)
	sort.Sort(sort.Reverse(sort.Float64Slice(out)))
	return out[:min(n, len(out))]
}
`,
		},
		Turns: script(
			local("We're about to write statistics over sensor readings. Two rules for all of them: a negative reading is a sensor fault and is left out as if it weren't there, and with nothing to work on a statistic returns 0. No code for that yet. To start, make ParseReadings skip blank lines."),
			cloud("Add `func Mean(values []float64) float64`."),
			cloud("Read docs/percentile.txt and tell me how we compute percentiles here. No code changes."),
			cloud("Callers share these slices between goroutines, so no function in this package may ever modify a slice it was given. Add `func Max(values []float64) float64`."),
			local("Add `func Median(values []float64) float64`. For an even count it's the average of the two middle values."),
			local("Add `func Percentile(values []float64, p float64) float64`, with p between 0 and 100."),
			cloud("Add `func Spread(values []float64) float64`: the largest value minus the smallest."),
			local("Add `func Sorted(values []float64) []float64`: the values in ascending order."),
			local("Add `func TopN(values []float64, n int) []float64`: the n largest values, largest first. With fewer than n values, return what there is."),
		),
		Carries: []Carry{
			{
				What:        "a negative reading is a sensor fault and every statistic leaves it out",
				Kind:        CarryUser,
				Established: 0,
				Needed:      []int{1, 3, 4, 5, 6},
				Tests:       []string{"TestMeanSkipsFaults", "TestMaxSkipsFaults", "TestMedianSkipsFaults", "TestPercentileSkipsFaults", "TestSpreadSkipsFaults", "TestAllFaultsIsZero"},
				Absent:      []string{"negative", "fault", "left out"},
			},
			{
				What:        "with nothing to work on a statistic returns 0",
				Kind:        CarryUser,
				Established: 0,
				Needed:      []int{1, 3, 4, 5, 6},
				Tests:       []string{"TestEmptyMean", "TestEmptyMax", "TestEmptyMedian", "TestEmptyPercentile", "TestEmptySpread", "TestAllFaultsIsZero", "TestMaxSkipsFaults"},
				Absent:      []string{"returns 0", "nothing to work on", "empty"},
			},
			{
				What:        "percentiles use the nearest-rank method (docs/percentile.txt)",
				Kind:        CarryDiscovered,
				Established: 2,
				Needed:      []int{5},
				Source:      "docs/percentile.txt",
				Tests:       []string{"TestPercentileNearestRank"},
				Absent:      []string{"nearest", "rank", "interpolat"},
			},
			{
				What:        "no function may modify a slice it was given",
				Kind:        CarryUser,
				Established: 3,
				Needed:      []int{7, 8},
				Tests:       []string{"TestSortedKeepsInput", "TestTopNKeepsInput"},
				Absent:      []string{"modify", "goroutine", "in place", "copy"},
			},
		},
	}
}
