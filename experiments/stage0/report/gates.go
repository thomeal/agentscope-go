package report

import (
	"fmt"
	"maps"
	"slices"
	"strings"
)

// Verdict is a gate decision. Verdicts are computed here and never written by hand.
type Verdict string

// The gate decisions.
const (
	Pass         Verdict = "PASS"
	Fail         Verdict = "FAIL"
	Insufficient Verdict = "INSUFFICIENT"
)

// Thresholds the gates test.
const (
	G1MinLoss      = 0.10
	G2MinGain      = 0.05
	G6MinGain      = 0.05
	G6MaxShortfall = 0.05
	G6MinForkCache = 0.70
	G6MaxForkCost  = 0.25
)

// Names read from the rows.
const (
	armC3            = "C3"
	armC3A           = "C3+A"
	counterLossy     = "lossy_events"
	roleMain         = "main"
	roleForkA        = "fork_a"
	roleForkB        = "fork_b"
	roleShorten      = "shorten"
	roleForkMerged   = "fork_merged"
	statusIncomplete = "incomplete"
)

func isForkRole(role string) bool {
	return role == roleForkA || role == roleForkB || role == roleForkMerged
}

// atLeast and above compare against a threshold with the epsilon guard. The value
// itself is never rounded.
func atLeast(x, threshold float64) bool { return x >= threshold-epsilon }
func above(x, threshold float64) bool   { return x > threshold+epsilon }

// Condition is one tested requirement of a gate.
type Condition struct {
	Name      string        `json:"name"`
	Verdict   Verdict       `json:"verdict"`
	Reason    string        `json:"reason"`
	Value     *float64      `json:"value"`
	Threshold float64       `json:"threshold"`
	Paired    *PairedResult `json:"paired,omitempty"`
}

// CounterTotal sums one score counter over an arm. MissingRows counts rows that
// lack the counter, which are not treated as zero.
type CounterTotal struct {
	Arm         string `json:"arm"`
	Counter     string `json:"counter"`
	Rows        int    `json:"rows"`
	Total       int    `json:"total"`
	MissingRows int    `json:"missing_rows"`
}

// ForkCache is the pooled, token-weighted cache rate on an arm's cloud fork rows.
type ForkCache struct {
	Arm string `json:"arm"`
	TokenTotals
	Rate *float64 `json:"rate"`
}

// Alternative reports another summary method against C3, with its cost. It has no
// effect on the verdict. Paired is nil on C3's own reference row.
type Alternative struct {
	Arm                string        `json:"arm"`
	Paired             *PairedResult `json:"paired,omitempty"`
	LedgerRows         int           `json:"ledger_rows"`
	CloudUSD           *float64      `json:"cloud_usd"`
	CloudUSDPerSession *float64      `json:"cloud_usd_per_session"`
	// ForkUSD counts prefix forks and draft-only rewrites. SummaryUSD counts every request that wrote a
	// summary, so it is the column that compares C0 and C7 with the forks.
	ForkUSD       *float64 `json:"fork_usd"`
	SummaryUSD    *float64 `json:"summary_usd"`
	ForkCacheRate *float64 `json:"fork_cache_rate"`
}

// GateResult is a gate's numbers, decision and one-line reason. Conditions decide
// the verdict; Recorded rows and Alternatives never do.
type GateResult struct {
	Gate         string         `json:"gate"`
	Verdict      Verdict        `json:"verdict"`
	Reason       string         `json:"reason"`
	Conditions   []Condition    `json:"conditions"`
	Recorded     []PairedResult `json:"recorded"`
	LossyEvents  *CounterTotal  `json:"lossy_events,omitempty"`
	ForkCache    *ForkCache     `json:"fork_cache,omitempty"`
	Alternatives []Alternative  `json:"alternatives,omitempty"`
}

// insufficientPairs is the shared INSUFFICIENT rule: a required arm is missing or
// fewer than MinPairs pairs exist.
func insufficientPairs(p PairedResult) (string, bool) {
	if len(p.ArmsMissing) > 0 {
		return fmt.Sprintf("%s: required arm missing: %s", p.Label(), strings.Join(p.ArmsMissing, ", ")), true
	}
	if p.N < MinPairs {
		return fmt.Sprintf("%s: %d pairs on %s, need at least %d (unpaired sessions: %d)", p.Label(), p.N, p.Metric, MinPairs, p.Unpaired()), true
	}
	return "", false
}

// pointCondition requires mean >= threshold.
func pointCondition(name string, p PairedResult, threshold float64) Condition {
	c := Condition{Name: name, Value: p.Mean, Threshold: threshold, Paired: &p}
	if why, short := insufficientPairs(p); short {
		c.Verdict, c.Reason = Insufficient, why
		return c
	}
	if atLeast(*p.Mean, threshold) {
		c.Verdict = Pass
		c.Reason = fmt.Sprintf("%s, at or above %.2f", p.describe(), threshold)
	} else {
		c.Verdict = Fail
		c.Reason = fmt.Sprintf("%s, below %.2f", p.describe(), threshold)
	}
	return c
}

// gainCondition requires CI lower bound > 0 and mean >= threshold.
func gainCondition(name string, p PairedResult, threshold float64) Condition {
	c := Condition{Name: name, Value: p.Mean, Threshold: threshold, Paired: &p}
	if why, short := insufficientPairs(p); short {
		c.Verdict, c.Reason = Insufficient, why
		return c
	}
	ciOK := above(*p.CILow, 0)
	pointOK := atLeast(*p.Mean, threshold)
	switch {
	case ciOK && pointOK:
		c.Verdict = Pass
		c.Reason = fmt.Sprintf("%s: CI lower bound above 0 and mean at or above %.2f", p.describe(), threshold)
	case !ciOK && !pointOK:
		c.Verdict = Fail
		c.Reason = fmt.Sprintf("%s: CI lower bound not above 0 and mean below %.2f", p.describe(), threshold)
	case !ciOK:
		c.Verdict = Fail
		c.Reason = fmt.Sprintf("%s: CI lower bound not above 0", p.describe())
	default:
		c.Verdict = Fail
		c.Reason = fmt.Sprintf("%s: mean below %.2f", p.describe(), threshold)
	}
	return c
}

// rollup folds sub-conditions. INSUFFICIENT takes precedence over FAIL because a
// missing required arm is INSUFFICIENT by definition; the reason still names any
// sub-condition that already fails.
func rollup(conds []Condition) (Verdict, string) {
	var short, failed []string
	for _, c := range conds {
		switch c.Verdict {
		case Insufficient:
			short = append(short, c.Name)
		case Fail:
			failed = append(failed, c.Name)
		}
	}
	switch {
	case len(short) > 0:
		reason := "insufficient data for: " + strings.Join(short, "; ")
		if len(failed) > 0 {
			reason += "; already failing: " + strings.Join(failed, "; ")
		}
		return Insufficient, reason
	case len(failed) > 0:
		return Fail, "failed: " + strings.Join(failed, "; ")
	default:
		return Pass, fmt.Sprintf("all %d sub-conditions pass", len(conds))
	}
}

// G1 tests whether the problem is real: A0 - A3-8K on retention is at least 0.10
// as a point estimate. A0 against A3-8K-h, A3-32K and A6 are recorded only.
func (d *Data) G1(seed int64) GateResult {
	p := d.Paired("A0", "A3-8K", Retention, seed)
	c := pointCondition("A0 - A3-8K retention loss >= 0.10", p, G1MinLoss)
	reason := c.Reason
	if c.Verdict == Pass {
		reason += ": the problem is real"
	}
	return GateResult{
		Gate: "G1", Verdict: c.Verdict, Reason: reason,
		Conditions: []Condition{c},
		Recorded: []PairedResult{
			d.Paired("A0", "A3-8K-h", Retention, seed),
			d.Paired("A0", "A3-32K", Retention, seed),
			d.Paired("A0", "A6", Retention, seed),
		},
	}
}

// G2 tests T1 against A4 on S2's hidden tests: CI lower bound above 0 and mean at least 0.05.
// T1 must also beat A4s by the same terms, because T1 holds its summary in the system
// prompt and plain A4 holds its brief in Context, which favors T1. The share of tasks that
// pass whole, T1 - A4-abrupt, T1 - A0 and T1 - A3-8K are recorded only.
func (d *Data) G2(seed int64) GateResult {
	conds := []Condition{
		gainCondition("T1 - A4 task score: CI lower > 0 and mean >= 0.05", d.Paired("T1", "A4", TaskScore, seed), G2MinGain),
		gainCondition("T1 - A4s task score: CI lower > 0 and mean >= 0.05", d.Paired("T1", "A4s", TaskScore, seed), G2MinGain),
	}
	verdict, reason := rollup(conds)
	return GateResult{
		Gate: "G2", Verdict: verdict, Reason: reason,
		Conditions: conds,
		Recorded: []PairedResult{
			d.Paired("T1", "A4", AllPass, seed),
			d.Paired("T1", "A4s", AllPass, seed),
			d.Paired("T1", "A4-abrupt", TaskScore, seed),
			d.Paired("T1", "A0", TaskScore, seed),
			d.Paired("T1", "A0", AllPass, seed),
			d.Paired("T1", "A3-8K", TaskScore, seed),
			d.Paired("T1", "A3-8K", AllPass, seed),
		},
	}
}

// G2Read is G2 for T1 with the event list, which is the plan's T1 if plain T1 falls short. The
// list does not need the log, so A4 could have it too, and T1+read has to beat that as well.
func (d *Data) G2Read(seed int64) GateResult {
	conds := []Condition{
		gainCondition("T1+read - A4 task score: CI lower > 0 and mean >= 0.05", d.Paired("T1+read", "A4", TaskScore, seed), G2MinGain),
		gainCondition("T1+read - A4s task score: CI lower > 0 and mean >= 0.05", d.Paired("T1+read", "A4s", TaskScore, seed), G2MinGain),
		gainCondition("T1+read - A4+read task score: CI lower > 0 and mean >= 0.05", d.Paired("T1+read", "A4+read", TaskScore, seed), G2MinGain),
	}
	verdict, reason := rollup(conds)
	return GateResult{
		Gate: "G2 with the event list", Verdict: verdict, Reason: reason,
		Conditions: conds,
		Recorded: []PairedResult{
			d.Paired("T1+read", "T1", TaskScore, seed),
			d.Paired("A4+read", "A4", TaskScore, seed),
			d.Paired("T1+read", "A4+read", AllPass, seed),
		},
	}
}

// G6 tests C3 on the C-group arms. It passes only when every sub-condition does.
// C4 and C7 against C3 are reported with their cost and do not affect the verdict.
func (d *Data) G6(seed int64) GateResult {
	lossy, lossyCond := d.lossyCondition(armC3)
	fork, forkCond := d.forkCacheCondition(armC3)
	conds := []Condition{
		gainCondition("C3 - C0 retention: CI lower > 0 and mean >= 0.05", d.Paired(armC3, "C0", Retention, seed), G6MinGain),
		pointCondition("C3 - C6 retention >= -0.05", d.Paired(armC3, "C6", Retention, seed), -G6MaxShortfall),
		pointCondition("C3 - C8 own_retention >= -0.05", d.Paired(armC3, "C8", OwnRetention, seed), -G6MaxShortfall),
		lossyCond,
		forkCond,
		d.forkCostCondition(armC3A),
	}
	verdict, reason := rollup(conds)
	alternatives := []Alternative{d.alternative(armC3, nil)}
	for _, arm := range []string{"C0h", "C4", "C7"} {
		p := d.Paired(arm, armC3, Retention, seed)
		alternatives = append(alternatives, d.alternative(arm, &p))
	}
	return GateResult{
		Gate: "G6", Verdict: verdict, Reason: reason,
		Conditions:   conds,
		Recorded:     []PairedResult{d.Paired(armC3, "C0h", Retention, seed)},
		LossyEvents:  lossy,
		ForkCache:    fork,
		Alternatives: alternatives,
	}
}

// rewriteNote states how often a summary was sent back. A bounce lets the writer choose what
// to drop, so it is not a lossy event, and a reader of a clean count should see it.
func (d *Data) rewriteNote(arm string) string {
	n := 0
	for _, s := range d.Scores[arm] {
		if v, ok := s.counter("summary_rewrites"); ok {
			n += v
		}
	}
	return fmt.Sprintf("; summaries were sent back %d times", n)
}

func (d *Data) lossyCondition(arm string) (*CounterTotal, Condition) {
	total := &CounterTotal{Arm: arm, Counter: counterLossy, Rows: len(d.Scores[arm])}
	for _, s := range d.Scores[arm] {
		if v, ok := s.counter(counterLossy); ok {
			total.Total += v
		} else {
			total.MissingRows++
		}
	}
	value := float64(total.Total)
	c := Condition{Name: arm + " lossy_events total = 0", Value: &value, Threshold: 0}
	switch {
	case total.Rows == 0:
		c.Value = nil
		c.Verdict = Insufficient
		c.Reason = "required arm missing: " + arm
	case total.Total > 0:
		c.Verdict = Fail
		c.Reason = fmt.Sprintf("%s has %d lossy events over %d rows (%d rows lack the counter)%s", arm, total.Total, total.Rows, total.MissingRows, d.rewriteNote(arm))
	case total.MissingRows > 0:
		c.Verdict = Insufficient
		c.Reason = fmt.Sprintf("%s: %d of %d rows lack lossy_events, so a total of 0 is not established", arm, total.MissingRows, total.Rows)
	default:
		c.Verdict = Pass
		c.Reason = fmt.Sprintf("%s has 0 lossy events over %d rows%s", arm, total.Rows, d.rewriteNote(arm))
	}
	return total, c
}

// forkCostCondition is §15's "Fork A + Fork B cost at most 25% of main", on the arm that
// runs both forks.
func (d *Data) forkCostCondition(arm string) Condition {
	cost := d.armCost(arm)
	c := Condition{Name: arm + " (Fork A + Fork B) / main <= 0.25", Value: cost.ForkToMain, Threshold: G6MaxForkCost}
	switch {
	case cost.Rows == 0:
		c.Verdict, c.Reason = Insufficient, "required arm missing: "+arm
	case cost.ForkToMain == nil:
		c.Verdict, c.Reason = Insufficient, arm+" has no main spend to compare with"
	case *cost.ForkToMain <= G6MaxForkCost+epsilon:
		c.Verdict = Pass
		c.Reason = fmt.Sprintf("%s fork USD %s over main USD %s plus imputed %s is %.4f, at or below 0.25", arm, fnum(cost.ForkUSD, 4), fnum(cost.MainUSD, 4), fnum(cost.ImputedMainUSD, 4), *cost.ForkToMain)
	default:
		c.Verdict = Fail
		c.Reason = fmt.Sprintf("%s fork USD %s over main USD %s plus imputed %s is %.4f, above 0.25", arm, fnum(cost.ForkUSD, 4), fnum(cost.MainUSD, 4), fnum(cost.ImputedMainUSD, 4), *cost.ForkToMain)
	}
	return c
}

func (d *Data) forkCacheCondition(arm string) (*ForkCache, Condition) {
	fork := d.forkCache(arm)
	c := Condition{Name: arm + " fork cache rate >= 0.70", Value: fork.Rate, Threshold: G6MinForkCache}
	note := fmt.Sprintf("%d cloud fork rows, %d cached of %d tokens, %d usage_missing", fork.Calls, fork.Cached, fork.Cached+fork.Write+fork.Uncached, fork.UsageMissing)
	switch {
	case fork.Rate == nil:
		c.Verdict = Insufficient
		c.Reason = fmt.Sprintf("%s: no fork token counts in the ledger (%s)", arm, note)
	case atLeast(*fork.Rate, G6MinForkCache):
		c.Verdict = Pass
		c.Reason = fmt.Sprintf("%s fork cache rate %.4f at or above 0.70 (%s)", arm, *fork.Rate, note)
	default:
		c.Verdict = Fail
		c.Reason = fmt.Sprintf("%s fork cache rate %.4f below 0.70 (%s)", arm, *fork.Rate, note)
	}
	return fork, c
}

// forkCache pools cloud fork_a, fork_b and fork_merged rows by tokens.
func (d *Data) forkCache(arm string) *ForkCache {
	fork := &ForkCache{Arm: arm}
	for _, rows := range d.Ledger[arm] {
		for _, r := range rows {
			if r.Side == sideCloud && isForkRole(r.Role) {
				fork.add(r)
			}
		}
	}
	fork.Rate = fork.CacheHitRate()
	return fork
}

func (d *Data) alternative(arm string, p *PairedResult) Alternative {
	cost := d.armCost(arm)
	return Alternative{
		Arm:                arm,
		Paired:             p,
		LedgerRows:         cost.Rows,
		CloudUSD:           cost.CloudUSD,
		CloudUSDPerSession: cost.CloudUSDPerSession,
		ForkUSD:            cost.ForkUSD,
		SummaryUSD:         cost.SummaryUSD,
		ForkCacheRate:      d.forkCache(arm).Rate,
	}
}

func sortedKeys[V any](m map[string]V) []string {
	return slices.Sorted(maps.Keys(m))
}
