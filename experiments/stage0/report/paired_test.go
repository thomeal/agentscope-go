package report

import (
	"math"
	"math/rand/v2"
	"reflect"
	"slices"
	"testing"
)

func TestPairedHandComputed(t *testing.T) {
	diffs := []float64{0.1, 0.2, 0.3, 0.4}
	d := loadScores(t,
		armRows("X", 4, func(i int) float64 { return 0.5 + diffs[i] }),
		armRows("Y", 4, constant(0.5)),
	)
	p := d.Paired("X", "Y", Retention, seed)
	if p.N != 4 {
		t.Fatalf("N = %d, want 4", p.N)
	}
	wantNear(t, "Mean", p.Mean, 0.25, 1e-12)
	// Squared deviations .0225 .0025 .0025 .0225 sum to .05; .05/3 under the root.
	wantNear(t, "SD", p.SD, math.Sqrt(0.05/3), 1e-12)
	if *p.CILow > 0.25 || *p.CIHigh < 0.25 || *p.CILow < 0.1 || *p.CIHigh > 0.4 {
		t.Errorf("CI [%v, %v] should bracket 0.25 inside the data range", *p.CILow, *p.CIHigh)
	}
	if p.Resamples != 10000 || p.Seed != seed {
		t.Errorf("Resamples %d Seed %d", p.Resamples, p.Seed)
	}
}

func TestPairedRecoversAKnownDifference(t *testing.T) {
	const truth, noise, n = 0.12, 0.05, 200
	rng := rand.New(rand.NewPCG(7, 7))
	values := make([]float64, n)
	for i := range values {
		values[i] = truth + noise*rng.NormFloat64()
	}
	d := loadScores(t,
		armRows("X", n, func(i int) float64 { return 0.3 + values[i] }),
		armRows("Y", n, constant(0.3)),
	)
	p := d.Paired("X", "Y", Retention, seed)
	wantNear(t, "Mean", p.Mean, truth, 3*noise/math.Sqrt(n))
	wantNear(t, "SD", p.SD, noise, 0.15*noise)
	if *p.CILow > truth || *p.CIHigh < truth {
		t.Errorf("CI [%v, %v] does not contain %v", *p.CILow, *p.CIHigh, truth)
	}
	width := *p.CIHigh - *p.CILow
	wantWidth := 2 * 1.96 * noise / math.Sqrt(n)
	if !near(width, wantWidth, 0.2*wantWidth) {
		t.Errorf("CI width %v, want about %v", width, wantWidth)
	}
}

// TestBootstrapCoverage checks the CI does its job: about 95% of intervals from
// independent samples contain the true mean. The percentile bootstrap runs slightly
// narrow at small n, so the lower bound is loose.
func TestBootstrapCoverage(t *testing.T) {
	const truth, datasets, n, resamples = 0.1, 300, 40, 1000
	rng := rand.New(rand.NewPCG(11, 13))
	covered := 0
	diffs := make([]float64, n)
	for i := range datasets {
		for j := range diffs {
			diffs[j] = truth + 0.2*rng.NormFloat64()
		}
		lo, hi := bootstrapCI(diffs, int64(i), resamples)
		if lo <= truth && truth <= hi {
			covered++
		}
	}
	rate := float64(covered) / datasets
	if rate < 0.89 || rate > 0.98 {
		t.Errorf("coverage = %v, want about 0.95", rate)
	}
}

func TestPairedCIIsDeterministic(t *testing.T) {
	rng := rand.New(rand.NewPCG(3, 3))
	values := make([]float64, 30)
	for i := range values {
		values[i] = rng.Float64()
	}
	x := armRows("X", 30, func(i int) float64 { return values[i] })
	y := armRows("Y", 30, constant(0.5))
	d := loadScores(t, x, y)

	a, b := d.Paired("X", "Y", Retention, seed), d.Paired("X", "Y", Retention, seed)
	if !reflect.DeepEqual(a, b) {
		t.Errorf("same seed differs: %+v vs %+v", a, b)
	}
	other := d.Paired("X", "Y", Retention, seed+1)
	if *other.CILow == *a.CILow && *other.CIHigh == *a.CIHigh {
		t.Error("a different seed gave an identical CI")
	}

	// Row order in the files must not change the resampling.
	slices.Reverse(x)
	slices.Reverse(y)
	shuffled := loadScores(t, y, x).Paired("X", "Y", Retention, seed)
	if !reflect.DeepEqual(a, shuffled) {
		t.Errorf("file order changed the result: %+v vs %+v", a, shuffled)
	}
}

func TestPairedReportsWhatCannotPair(t *testing.T) {
	onlyX := scoreRow("X", "only-x", 0.5)
	onlyY := scoreRow("Y", "only-y", 0.5)
	noMetricX := scoreRow("X", "no-metric-x", 0.5)
	delete(noMetricX, "retention")
	noMetricY := scoreRow("Y", "no-metric-x", 0.5)
	nullMetricY := scoreRow("Y", "null-metric-y", 0.5)
	nullMetricY["retention"] = nil
	nullMetricX := scoreRow("X", "null-metric-y", 0.5)
	// A session with an error is a row and counts with whatever it scored.
	errored := scoreRow("X", "errored", 0)
	errored["error"] = "session stopped"
	erroredPeer := scoreRow("Y", "errored", 0.5)
	good := scoreRow("X", "good", 0.5)
	goodPeer := scoreRow("Y", "good", 0.25)

	d := loadScores(t, []row{onlyX, onlyY, noMetricX, noMetricY, nullMetricX, nullMetricY, errored, erroredPeer, good, goodPeer})
	p := d.Paired("X", "Y", Retention, seed)

	if p.N != 2 {
		t.Errorf("N = %d, want 2", p.N)
	}
	wantNear(t, "Mean", p.Mean, (-0.5+0.25)/2, 1e-12)
	if !reflect.DeepEqual(p.MissingFromA, []string{"only-y"}) || !reflect.DeepEqual(p.MissingFromB, []string{"only-x"}) {
		t.Errorf("missing A %v, B %v", p.MissingFromA, p.MissingFromB)
	}
	if !reflect.DeepEqual(p.MetricMissingA, []string{"no-metric-x"}) || !reflect.DeepEqual(p.MetricMissingB, []string{"null-metric-y"}) {
		t.Errorf("metric missing A %v, B %v", p.MetricMissingA, p.MetricMissingB)
	}
	if p.ErrorsA != 1 || p.ErrorsB != 0 {
		t.Errorf("errors A %d B %d, want 1 and 0", p.ErrorsA, p.ErrorsB)
	}
	if p.Unpaired() != 4 {
		t.Errorf("Unpaired = %d, want 4", p.Unpaired())
	}
}

func TestPairedStatesUndefinedNumbers(t *testing.T) {
	d := loadScores(t, armRows("X", 1, constant(0.5)), armRows("Y", 1, constant(0.25)))
	one := d.Paired("X", "Y", Retention, seed)
	wantNear(t, "Mean", one.Mean, 0.25, 1e-12)
	if one.SD != nil || one.CILow != nil || one.CIHigh != nil {
		t.Errorf("one pair should leave SD and CI undefined: %+v", one)
	}
	none := d.Paired("X", "absent", Retention, seed)
	if none.N != 0 || none.Mean != nil {
		t.Errorf("no pairs should leave the mean undefined: %+v", none)
	}
	if !reflect.DeepEqual(none.ArmsMissing, []string{"absent"}) {
		t.Errorf("ArmsMissing = %v", none.ArmsMissing)
	}
	if !reflect.DeepEqual(none.MissingFromB, []string{sid(0)}) {
		t.Errorf("MissingFromB = %v", none.MissingFromB)
	}
}

func TestPercentile(t *testing.T) {
	xs := make([]float64, 100)
	for i := range xs {
		xs[i] = float64(i + 1)
	}
	for _, c := range []struct{ p, want float64 }{{0, 1}, {0.5, 50.5}, {0.95, 95.05}, {1, 100}} {
		if got := percentile(xs, c.p); !near(got, c.want, 1e-9) {
			t.Errorf("percentile(%v) = %v, want %v", c.p, got, c.want)
		}
	}
	if got := percentile([]float64{7}, 0.95); got != 7 {
		t.Errorf("single value percentile = %v", got)
	}
}
