package s2

func accC03() Task {
	return Task{
		ID:     "s2-c03-downsample",
		Set:    "acceptance",
		System: system("a time-series downsampling library"),
		Files: map[string]string{
			"go.mod": goMod,
			"series.go": `package series

// Point is one sample. T is seconds since the Unix epoch.
type Point struct {
	T int64
	V float64
}

// Span returns the last time minus the first, for points in time order.
func Span(points []Point) int64 {
	if len(points) == 0 {
		return 0
	}
	return points[len(points)-1].T - points[0].T
}
`,
			"series_test.go": `package series

import "testing"

func TestSpan(t *testing.T) {
	if got := Span([]Point{{T: 5}, {T: 12}}); got != 7 {
		t.Fatalf("Span = %d", got)
	}
	if got := Span(nil); got != 0 {
		t.Fatalf("Span(nil) = %d", got)
	}
}
`,
		},
		Hidden: map[string]string{hiddenName: `package series_test

import (
	"errors"
	"math"
	"testing"

	series "fixture"
)

type pt = series.Point

func same(got, want []pt) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i].T != want[i].T || !(math.Abs(got[i].V-want[i].V) <= 1e-9) {
			return false
		}
	}
	return true
}

func TestSorted(t *testing.T) {
	in := []pt{{T: 9, V: 1}, {T: 3, V: 2}}
	got := series.Sorted(in)
	if !same(got, []pt{{T: 3, V: 2}, {T: 9, V: 1}}) {
		t.Fatalf("Sorted = %v", got)
	}
	if in[0].T != 9 {
		t.Fatalf("input reordered: %v", in)
	}
}

func TestMeanAverages(t *testing.T) {
	got, err := series.Mean([]pt{{T: 0, V: 1}, {T: 5, V: 3}}, 10)
	if err != nil || len(got) != 1 || math.Abs(got[0].V-2) > 1e-9 {
		t.Fatalf("Mean = %v, %v", got, err)
	}
}

func TestMeanAlignedLabels(t *testing.T) {
	got, err := series.Mean([]pt{{T: 67, V: 1}, {T: 75, V: 3}, {T: 125, V: 5}}, 60)
	if err != nil || !same(got, []pt{{T: 60, V: 2}, {T: 120, V: 5}}) {
		t.Fatalf("Mean = %v, %v", got, err)
	}
}

func TestMeanLeavesGaps(t *testing.T) {
	got, err := series.Mean([]pt{{T: 0, V: 1}, {T: 200, V: 3}}, 60)
	if err != nil || !same(got, []pt{{T: 0, V: 1}, {T: 180, V: 3}}) {
		t.Fatalf("Mean = %v, %v", got, err)
	}
}

func TestMeanBadWidthSentinel(t *testing.T) {
	for _, w := range []int64{0, -5} {
		if _, err := series.Mean([]pt{{T: 1}}, w); !errors.Is(err, series.ErrBadWidth) {
			t.Errorf("width %d: %v", w, err)
		}
	}
}

func TestMaxPicksLargest(t *testing.T) {
	got, err := series.Max([]pt{{T: 0, V: 1}, {T: 5, V: 8}, {T: 9, V: 3}}, 10)
	if err != nil || len(got) != 1 || got[0].V != 8 {
		t.Fatalf("Max = %v, %v", got, err)
	}
}

func TestMaxAlignedLabels(t *testing.T) {
	got, err := series.Max([]pt{{T: 67, V: 1}, {T: 75, V: 3}, {T: 125, V: 5}}, 60)
	if err != nil || !same(got, []pt{{T: 60, V: 3}, {T: 120, V: 5}}) {
		t.Fatalf("Max = %v, %v", got, err)
	}
}

func TestMaxBadWidthSentinel(t *testing.T) {
	if _, err := series.Max([]pt{{T: 1}}, 0); !errors.Is(err, series.ErrBadWidth) {
		t.Fatalf("err = %v", err)
	}
}

func TestMaxSkipsDropouts(t *testing.T) {
	got, err := series.Max([]pt{{T: 0, V: math.NaN()}, {T: 1, V: 4}, {T: 2, V: math.NaN()}}, 10)
	if err != nil || !same(got, []pt{{T: 0, V: 4}}) {
		t.Fatalf("Max = %v, %v", got, err)
	}
}

func TestMeanSkipsDropouts(t *testing.T) {
	got, err := series.Mean([]pt{{T: 0, V: 2}, {T: 1, V: math.NaN()}, {T: 2, V: 4}, {T: 70, V: math.NaN()}}, 60)
	if err != nil || !same(got, []pt{{T: 0, V: 3}}) {
		t.Fatalf("Mean = %v, %v", got, err)
	}
}

func TestLastTakesLatest(t *testing.T) {
	got, err := series.Last([]pt{{T: 5, V: 1}, {T: 9, V: 7}}, 10)
	if err != nil || len(got) != 1 || got[0].V != 7 {
		t.Fatalf("Last = %v, %v", got, err)
	}
}

func TestLastAlignedLabels(t *testing.T) {
	got, err := series.Last([]pt{{T: 61, V: 1}, {T: 119, V: 2}, {T: 121, V: 3}}, 60)
	if err != nil || !same(got, []pt{{T: 60, V: 2}, {T: 120, V: 3}}) {
		t.Fatalf("Last = %v, %v", got, err)
	}
}

func TestLastSkipsDropouts(t *testing.T) {
	got, err := series.Last([]pt{{T: 0, V: 6}, {T: 2, V: math.NaN()}}, 10)
	if err != nil || !same(got, []pt{{T: 0, V: 6}}) {
		t.Fatalf("Last = %v, %v", got, err)
	}
}

func TestLastBadWidthSentinel(t *testing.T) {
	if _, err := series.Last([]pt{{T: 1}}, -1); !errors.Is(err, series.ErrBadWidth) {
		t.Fatalf("err = %v", err)
	}
}

func TestCountsTallies(t *testing.T) {
	got, err := series.Counts([]pt{{T: 0, V: 1}, {T: 1, V: 2}, {T: 90, V: 3}}, 60)
	if err != nil || !same(got, []pt{{T: 0, V: 2}, {T: 60, V: 1}}) {
		t.Fatalf("Counts = %v, %v", got, err)
	}
}

func TestCountsSkipsDropouts(t *testing.T) {
	got, err := series.Counts([]pt{{T: 0, V: 1}, {T: 1, V: math.NaN()}}, 60)
	if err != nil || !same(got, []pt{{T: 0, V: 1}}) {
		t.Fatalf("Counts = %v, %v", got, err)
	}
}
`},
		Solution: map[string]string{
			"series.go": `package series

import (
	"errors"
	"math"
	"sort"
)

// ErrBadWidth refuses a bucket width that is not positive.
var ErrBadWidth = errors.New("bad bucket width")

// Point is one sample. T is seconds since the Unix epoch.
type Point struct {
	T int64
	V float64
}

// Span returns the last time minus the first, for points in time order.
func Span(points []Point) int64 {
	if len(points) == 0 {
		return 0
	}
	return points[len(points)-1].T - points[0].T
}

// Sorted returns a copy of the points in time order.
func Sorted(points []Point) []Point {
	out := append([]Point(nil), points...)
	sort.SliceStable(out, func(a, b int) bool { return out[a].T < out[b].T })
	return out
}

// label returns the start of the bucket holding t.
func label(t, width int64) int64 { return t - ((t%width)+width)%width }

// apply reduces each non-empty bucket of the points to one value.
func apply(points []Point, width int64, reduce func([]float64) float64) ([]Point, error) {
	if width <= 0 {
		return nil, ErrBadWidth
	}
	var out []Point
	var vals []float64
	var cur int64
	flush := func() {
		if len(vals) > 0 {
			out = append(out, Point{T: cur, V: reduce(vals)})
		}
		vals = nil
	}
	for _, p := range Sorted(points) {
		if math.IsNaN(p.V) {
			continue
		}
		if l := label(p.T, width); l != cur {
			flush()
			cur = l
		}
		vals = append(vals, p.V)
	}
	flush()
	return out, nil
}

// Mean reduces each bucket to the average of its samples.
func Mean(points []Point, width int64) ([]Point, error) {
	return apply(points, width, func(v []float64) float64 {
		sum := 0.0
		for _, x := range v {
			sum += x
		}
		return sum / float64(len(v))
	})
}

// Max reduces each bucket to its largest sample.
func Max(points []Point, width int64) ([]Point, error) {
	return apply(points, width, func(v []float64) float64 {
		best := v[0]
		for _, x := range v {
			best = max(best, x)
		}
		return best
	})
}

// Last reduces each bucket to its latest sample.
func Last(points []Point, width int64) ([]Point, error) {
	return apply(points, width, func(v []float64) float64 { return v[len(v)-1] })
}

// Counts reduces each bucket to the number of samples in it.
func Counts(points []Point, width int64) ([]Point, error) {
	return apply(points, width, func(v []float64) float64 { return float64(len(v)) })
}
`,
		},
		Turns: script(
			local("A decision on the downsampling work: a bucket is labelled with its start, and starts are multiples of the width counted from zero, so with width 60 a sample at 67 belongs to the bucket labelled 60, wherever the data begins. That keeps charts from different devices lined up. Nothing to code for it yet. Add `func Sorted(points []Point) []Point`, which returns a copy in time order and doesn't touch the input."),
			cloud("Add `var ErrBadWidth = errors.New(\"bad bucket width\")`. Nothing returns it yet. Every downsampling call will take a width in seconds, and a width below 1 gets this error."),
			cloud("Add `func Mean(points []Point, width int64) ([]Point, error)`: one point per bucket, in time order, holding the average of the samples in that bucket. The input may be out of order."),
			cloud("Our gauges report NaN when they drop out, and those samples aren't readings at all. They count as if they weren't there, and a bucket with nothing else in it doesn't appear. Update Mean for that."),
			local("Add `func Max(points []Point, width int64) ([]Point, error)`: one point per bucket in time order holding the largest sample in the bucket."),
			local("Add `func Last(points []Point, width int64) ([]Point, error)`: one point per bucket in time order holding the latest sample in that bucket."),
			cloud("Add `func Counts(points []Point, width int64) ([]Point, error)`: one point per bucket in time order, where V is how many samples the bucket has."),
		),
		Carries: []Carry{
			{
				What:        "bucket labels are multiples of the width, independent of the first sample",
				Kind:        CarryUser,
				Established: 0,
				Needed:      []int{2, 4, 5},
				Tests:       []string{"TestMeanAlignedLabels", "TestMaxAlignedLabels", "TestLastAlignedLabels"},
				Absent:      []string{"multiple", "aligned", "labelled", "counted from zero", "start"},
			},
			{
				What:        "ErrBadWidth for a width below 1",
				Kind:        CarryEmbodied,
				Established: 1,
				Needed:      []int{2, 4, 5},
				Tests:       []string{"TestMeanBadWidthSentinel", "TestMaxBadWidthSentinel", "TestLastBadWidthSentinel"},
				Absent:      []string{"errbadwidth", "below 1", "bad bucket", "sentinel"},
			},
			{
				What:        "NaN samples are dropouts and count as absent",
				Kind:        CarryEmbodied,
				Established: 3,
				Needed:      []int{4, 5, 6},
				Tests:       []string{"TestMeanSkipsDropouts", "TestMaxSkipsDropouts", "TestLastSkipsDropouts", "TestCountsSkipsDropouts"},
				Absent:      []string{"nan", "drop out", "dropout", "gauge"},
			},
		},
	}
}
