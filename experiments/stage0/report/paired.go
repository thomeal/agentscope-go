package report

import (
	"fmt"
	"maps"
	"math"
	"math/rand/v2"
	"slices"
	"strings"
)

const (
	// Resamples is the number of bootstrap resamples behind every CI.
	Resamples = 10000
	// MinPairs is the pair count below which a gate is INSUFFICIENT.
	MinPairs = 10

	// epsilon guards threshold comparisons against binary floating point noise:
	// 0.9 - 0.8 is 0.09999999999999998, which must meet a threshold of 0.10.
	// Scores are ratios of small counts, so no real difference is this small.
	epsilon = 1e-9

	// bootstrapStream is the fixed second PCG word; the caller's seed is the first.
	bootstrapStream = 0x5354414745304253

	// repeatSep separates a task's id from its repeat number in an S2 row. It matches the
	// runner's, and the report knows nothing else about repeats.
	repeatSep = "~r"
)

// Metric names a per-session number. Of reports false when a row lacks the value;
// such a row is stated as missing rather than scored as zero.
type Metric struct {
	Name string
	Of   func(Score) (float64, bool)
}

// The metrics the gates read.
var (
	Retention    = Metric{Name: "retention", Of: func(s Score) (float64, bool) { return deref(s.Retention) }}
	OwnRetention = Metric{Name: "own_retention", Of: func(s Score) (float64, bool) { return deref(s.OwnRetention) }}
	// TaskScore is the share of hidden tests that pass at the end of an S2 task. A scorer
	// failure says nothing about the arm, so it has no value. A row of any other set has none
	// either, so a gate on it cannot read a retention by mistake.
	TaskScore = Metric{Name: "task_score", Of: func(s Score) (float64, bool) {
		if s.S2 == nil || s.S2.Infra != "" {
			return 0, false
		}
		return deref(s.Retention)
	}}
	// AllPass is 1 for an S2 task whose hidden tests all pass and 0 for any other.
	AllPass = Metric{Name: "all_pass", Of: func(s Score) (float64, bool) {
		if _, ok := TaskScore.Of(s); !ok {
			return 0, false
		}
		if s.S2.Total > 0 && s.S2.Passed == s.S2.Total && !s.S2.BuildFailed {
			return 1, true
		}
		return 0, true
	}}
)

func deref(p *float64) (float64, bool) {
	if p == nil {
		return 0, false
	}
	return *p, true
}

// PairedResult is a paired comparison of metric(A) - metric(B) at session level.
// Mean, SD and the CI bounds are nil when they are undefined for the pair count:
// Mean needs one pair, SD and the CI need two. N counts sessions or tasks, and Rows counts
// the pairs behind them, which is more only when an S2 task ran more than once.
type PairedResult struct {
	A         string   `json:"a"`
	B         string   `json:"b"`
	Metric    string   `json:"metric"`
	N         int      `json:"n"`
	Rows      int      `json:"rows"`
	Mean      *float64 `json:"mean_diff"`
	SD        *float64 `json:"sd_diff"`
	CILow     *float64 `json:"ci_low"`
	CIHigh    *float64 `json:"ci_high"`
	Seed      int64    `json:"seed"`
	Resamples int      `json:"resamples"`
	// ArmsMissing lists either arm when it has no score rows at all.
	ArmsMissing []string `json:"arms_missing"`
	// MissingFromA holds sessions that only B has, and MissingFromB the reverse.
	MissingFromA []string `json:"missing_from_a"`
	MissingFromB []string `json:"missing_from_b"`
	// MetricMissingA and MetricMissingB hold sessions present in both arms whose
	// row in that arm has no value for the metric.
	MetricMissingA []string `json:"metric_missing_a"`
	MetricMissingB []string `json:"metric_missing_b"`
	// ErrorsA and ErrorsB count paired rows carrying an error. They stay in the
	// pairs and count with whatever they scored.
	ErrorsA int `json:"errors_a"`
	ErrorsB int `json:"errors_b"`
}

// Label names the comparison.
func (p PairedResult) Label() string { return p.A + " - " + p.B }

// Unpaired is the number of sessions that could not form a pair.
func (p PairedResult) Unpaired() int {
	seen := map[string]bool{}
	for _, list := range [][]string{p.MissingFromA, p.MissingFromB, p.MetricMissingA, p.MetricMissingB} {
		for _, id := range list {
			seen[id] = true
		}
	}
	return len(seen)
}

// pairDiffs builds the differences, in session id order so that a seed gives the
// same CI whatever order the files listed their rows in.
func (d *Data) pairDiffs(a, b string, metric Metric) ([]float64, PairedResult) {
	r := PairedResult{
		A: a, B: b, Metric: metric.Name,
		ArmsMissing:    []string{},
		MissingFromA:   []string{},
		MissingFromB:   []string{},
		MetricMissingA: []string{},
		MetricMissingB: []string{},
	}
	sa, sb := d.Scores[a], d.Scores[b]
	if len(sa) == 0 {
		r.ArmsMissing = append(r.ArmsMissing, a)
	}
	if len(sb) == 0 && b != a {
		r.ArmsMissing = append(r.ArmsMissing, b)
	}
	ids := map[string]bool{}
	for id := range sa {
		ids[id] = true
	}
	for id := range sb {
		ids[id] = true
	}
	byUnit := map[string][]float64{}
	for _, id := range slices.Sorted(maps.Keys(ids)) {
		ra, inA := sa[id]
		rb, inB := sb[id]
		if !inA {
			r.MissingFromA = append(r.MissingFromA, id)
			continue
		}
		if !inB {
			r.MissingFromB = append(r.MissingFromB, id)
			continue
		}
		va, okA := metric.Of(ra)
		vb, okB := metric.Of(rb)
		if !okA {
			r.MetricMissingA = append(r.MetricMissingA, id)
		}
		if !okB {
			r.MetricMissingB = append(r.MetricMissingB, id)
		}
		if !okA || !okB {
			continue
		}
		unit, _, _ := strings.Cut(id, repeatSep)
		byUnit[unit] = append(byUnit[unit], va-vb)
		r.Rows++
		if ra.Error != "" {
			r.ErrorsA++
		}
		if rb.Error != "" {
			r.ErrorsB++
		}
	}
	// Repeats of a task are not independent, so the task is the unit: the SD and the CI see
	// one mean per task. With no repeats this is the differences as they were.
	diffs := make([]float64, 0, len(byUnit))
	for _, unit := range slices.Sorted(maps.Keys(byUnit)) {
		diffs = append(diffs, *meanOf(byUnit[unit]))
	}
	r.N = len(diffs)
	r.Mean = meanOf(diffs)
	r.SD = sampleSD(diffs)
	return diffs, r
}

// Paired compares two arms on sessions they share, by session id. The CI is a
// percentile bootstrap at session level with Resamples resamples; the same seed
// and data give the same CI.
func (d *Data) Paired(a, b string, metric Metric, seed int64) PairedResult {
	diffs, r := d.pairDiffs(a, b, metric)
	r.Seed, r.Resamples = seed, Resamples
	if len(diffs) >= 2 {
		lo, hi := bootstrapCI(diffs, seed, Resamples)
		r.CILow, r.CIHigh = &lo, &hi
	}
	return r
}

func meanOf(xs []float64) *float64 {
	if len(xs) == 0 {
		return nil
	}
	sum := 0.0
	for _, x := range xs {
		sum += x
	}
	m := sum / float64(len(xs))
	return &m
}

// sampleSD is the n-1 standard deviation; nil below two values.
func sampleSD(xs []float64) *float64 {
	if len(xs) < 2 {
		return nil
	}
	m := *meanOf(xs)
	ss := 0.0
	for _, x := range xs {
		ss += (x - m) * (x - m)
	}
	sd := math.Sqrt(ss / float64(len(xs)-1))
	return &sd
}

// percentile interpolates linearly between closest ranks (R type 7). sorted must
// be ascending and non-empty; p is in [0, 1].
func percentile(sorted []float64, p float64) float64 {
	pos := p * float64(len(sorted)-1)
	lo := int(math.Floor(pos))
	hi := min(lo+1, len(sorted)-1)
	return sorted[lo] + (pos-float64(lo))*(sorted[hi]-sorted[lo])
}

// bootstrapCI returns the 2.5th and 97.5th percentiles of the resampled means.
func bootstrapCI(diffs []float64, seed int64, resamples int) (lo, hi float64) {
	rng := rand.New(rand.NewPCG(uint64(seed), bootstrapStream))
	n := len(diffs)
	means := make([]float64, resamples)
	for i := range means {
		sum := 0.0
		for range n {
			sum += diffs[rng.IntN(n)]
		}
		means[i] = sum / float64(n)
	}
	slices.Sort(means)
	return percentile(means, 0.025), percentile(means, 0.975)
}

// describe renders the numbers of a comparison on one line, for reasons.
func (p PairedResult) describe() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s %s mean %s on %d pairs, 95%% CI %s", p.Label(), p.Metric, fnum(p.Mean, 4), p.N, ciText(p.CILow, p.CIHigh))
	if u := p.Unpaired(); u > 0 {
		fmt.Fprintf(&b, ", unpaired sessions: %d", u)
	}
	if e := p.ErrorsA + p.ErrorsB; e > 0 {
		fmt.Fprintf(&b, ", paired rows with an error: %d", e)
	}
	return b.String()
}
