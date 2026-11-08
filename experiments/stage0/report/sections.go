package report

import (
	"fmt"
	"math"
	"strings"
)

const privacyArm = "T1"

// PrivacyRow states canary hits for one arm from both sources. ScoreRowsMissing
// counts score rows without canary_hits, which are not treated as zero.
// ReplacedHits sums hits on rows a later run replaced: they do not decide the
// verdict, but a canary that reached the cloud stays on record.
type PrivacyRow struct {
	Arm              string `json:"arm"`
	ScoreRows        int    `json:"score_rows"`
	ScoreHits        int    `json:"score_hits"`
	ScoreRowsMissing int    `json:"score_rows_missing"`
	LedgerRows       int    `json:"ledger_rows"`
	LedgerHits       int    `json:"ledger_hits"`
	ReplacedHits     int    `json:"replaced_hits"`
}

// PrivacySection reports canary hits per arm. Only T1 decides the verdict.
type PrivacySection struct {
	Rows    []PrivacyRow `json:"rows"`
	Verdict Verdict      `json:"verdict"`
	Reason  string       `json:"reason"`
}

// Privacy reads canary hits from scores.jsonl and the ledger. T1 must have 0 hits
// in both. FAIL needs one hit. PASS needs both sources to exist for T1 and every
// T1 score row to state its count; otherwise 0 hits is not established.
func (d *Data) Privacy() PrivacySection {
	s := PrivacySection{Rows: []PrivacyRow{}}
	var t1 PrivacyRow
	for _, arm := range d.armNames() {
		row := PrivacyRow{Arm: arm, ScoreRows: len(d.Scores[arm]), ReplacedHits: d.replacedHits[arm]}
		for _, sc := range d.Scores[arm] {
			if sc.CanaryHits == nil {
				row.ScoreRowsMissing++
			} else {
				row.ScoreHits += *sc.CanaryHits
			}
		}
		for _, rows := range d.Ledger[arm] {
			row.LedgerRows += len(rows)
			for _, r := range rows {
				row.LedgerHits += r.CanaryHits
			}
		}
		s.Rows = append(s.Rows, row)
		if arm == privacyArm {
			t1 = row
		}
	}
	switch {
	case t1.ScoreHits+t1.LedgerHits > 0:
		s.Verdict = Fail
		s.Reason = fmt.Sprintf("T1 sent canaries to the cloud: %d hits in scores, %d in the ledger", t1.ScoreHits, t1.LedgerHits)
	case t1.ScoreRows == 0 && t1.LedgerRows == 0:
		s.Verdict = Insufficient
		s.Reason = "required arm missing: T1"
	case t1.ScoreRows == 0:
		s.Verdict = Insufficient
		s.Reason = "T1 has ledger rows and no score rows, so scores cannot confirm 0 hits"
	case t1.LedgerRows == 0:
		s.Verdict = Insufficient
		s.Reason = "T1 has score rows and no ledger rows, so the ledger cannot confirm 0 hits"
	case t1.ScoreRowsMissing > 0:
		s.Verdict = Insufficient
		s.Reason = fmt.Sprintf("%d of %d T1 score rows lack canary_hits", t1.ScoreRowsMissing, t1.ScoreRows)
	default:
		s.Verdict = Pass
		s.Reason = fmt.Sprintf("T1 has 0 canary hits over %d score rows and %d ledger rows", t1.ScoreRows, t1.LedgerRows)
	}
	if t1.ReplacedHits > 0 {
		s.Reason += fmt.Sprintf("; %d hits on T1 rows replaced by a later run", t1.ReplacedHits)
	}
	return s
}

// The fixed quantities of the sample size approximation.
const (
	zPowerSum = 1.96 + 0.84
	deltaLow  = 0.05
	deltaHigh = 0.10
)

// NeededPairs is n = (1.96+0.84)^2 sd^2 / delta^2, rounded up: the pairs needed to
// detect a true difference of delta at 80% power, two-sided 5%. It is the normal
// approximation and takes sd as known.
func NeededPairs(sd, delta float64) int {
	n := zPowerSum * zPowerSum * sd * sd / (delta * delta)
	return int(math.Ceil(n - epsilon))
}

// SampleSizeRow is the size one comparison needs given its observed SD. DrivesL4
// marks the comparisons that set the L4 acceptance size.
type SampleSizeRow struct {
	Comparison string   `json:"comparison"`
	Metric     string   `json:"metric"`
	Pairs      int      `json:"pairs"`
	SD         *float64 `json:"sd_diff"`
	NeedLow    *int     `json:"needed_for_0_05"`
	NeedHigh   *int     `json:"needed_for_0_10"`
	DrivesL4   bool     `json:"drives_l4"`
}

// SampleSizeSection turns observed variance into the size L4 needs.
type SampleSizeSection struct {
	Rows []SampleSizeRow `json:"rows"`
	// L4Low and L4High are the acceptance sizes for a true difference of 0.05 and
	// 0.10: the largest need among DrivesL4 rows, never below MinPairs. L4Complete
	// is false when a driving row has no SD, which makes them lower bounds.
	L4Low      *int   `json:"l4_for_0_05"`
	L4High     *int   `json:"l4_for_0_10"`
	L4Complete bool   `json:"l4_complete"`
	Statement  string `json:"statement"`
}

// SampleSize computes the needed pairs for each gate comparison. The two
// comparisons gated on a CI at 0.05, T1 - A4 and C3 - C0, set the L4 size.
func (d *Data) SampleSize() SampleSizeSection {
	return d.sampleSize([]sizeComparison{
		{"A0", "A3-8K", Retention, false},
		{"T1", "A4", Retention, false},
		{"C3", "C0", Retention, true},
		{"C3", "C6", Retention, false},
		{"C3", "C8", OwnRetention, false},
	}, "sessions")
}

// TaskSampleSize is the same section for G2, which counts tasks.
func (d *Data) TaskSampleSize() SampleSizeSection {
	return d.sampleSize([]sizeComparison{
		{"A0", "A3-8K", TaskScore, false},
		{"T1", "A4", TaskScore, true},
		{"T1", "A4s", TaskScore, true},
		{"T1+read", "A4+read", TaskScore, false},
	}, "tasks")
}

// ReadSampleSize is the section for T1+read, which acceptance runs if it is the form that
// passed, and it has to beat every control on the gate.
func (d *Data) ReadSampleSize() SampleSizeSection {
	s := d.sampleSize([]sizeComparison{
		{"T1+read", "A4", TaskScore, true},
		{"T1+read", "A4s", TaskScore, true},
		{"T1+read", "A4+read", TaskScore, true},
	}, "tasks")
	s.Statement = "If T1+read is the form that passed: " + s.Statement
	return s
}

// sizeComparison is one row of the section. drives says whether it sets the L4 size.
type sizeComparison struct {
	a, b   string
	metric Metric
	drives bool
}

func (d *Data) sampleSize(comparisons []sizeComparison, unit string) SampleSizeSection {
	s := SampleSizeSection{Rows: []SampleSizeRow{}, L4Complete: true}
	var driver string
	var low, high int
	var noSD, thinSD []string
	for _, c := range comparisons {
		_, p := d.pairDiffs(c.a, c.b, c.metric)
		row := SampleSizeRow{Comparison: p.Label(), Metric: p.Metric, Pairs: p.N, SD: p.SD, DrivesL4: c.drives}
		if p.SD != nil {
			nl, nh := NeededPairs(*p.SD, deltaLow), NeededPairs(*p.SD, deltaHigh)
			row.NeedLow, row.NeedHigh = &nl, &nh
			if c.drives {
				if driver == "" || nl > low {
					driver = p.Label()
				}
				low, high = max(low, nl), max(high, nh)
				if p.N < MinPairs {
					thinSD = append(thinSD, p.Label())
				}
			}
		} else if c.drives {
			s.L4Complete = false
			noSD = append(noSD, fmt.Sprintf("%s (%d pairs)", p.Label(), p.N))
		}
		s.Rows = append(s.Rows, row)
	}
	if driver == "" {
		s.Statement = "L4 acceptance size is undetermined: no SD for " + strings.Join(noSD, ", ")
		return s
	}
	low, high = max(low, MinPairs), max(high, MinPairs)
	s.L4Low, s.L4High = &low, &high
	prefix := "L4 acceptance size"
	if !s.L4Complete {
		prefix = "L4 acceptance size, a lower bound"
	}
	s.Statement = fmt.Sprintf("%s: %d paired %s for a true difference of 0.05 and %d for 0.10 (80%% power, two-sided 5%%, floor of %d pairs), driven by %s",
		prefix, low, unit, high, MinPairs, driver)
	if !s.L4Complete {
		s.Statement += "; no SD for " + strings.Join(noSD, ", ")
	}
	if len(thinSD) > 0 {
		s.Statement += fmt.Sprintf("; SD rests on fewer than %d pairs for %s", MinPairs, strings.Join(thinSD, ", "))
	}
	return s
}
