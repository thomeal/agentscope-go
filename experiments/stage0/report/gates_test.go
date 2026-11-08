package report

import (
	"strings"
	"testing"
)

func TestG1(t *testing.T) {
	// 0.9 - 0.8 is 0.09999999999999998, so the boundary needs the epsilon guard.
	if 0.9-floatVar(0.8) >= 0.10 {
		t.Fatal("fixture no longer shows floating point noise")
	}
	cases := []struct {
		name    string
		a0, a3  float64
		pairs   int
		verdict Verdict
	}{
		{"well above", 0.9, 0.5, 12, Pass},
		{"exactly 0.10", 0.9, 0.8, 12, Pass},
		{"just below 0.10", 0.9, 0.8001, 12, Fail},
		{"A3 better", 0.5, 0.9, 12, Fail},
		{"exactly 10 pairs", 0.9, 0.5, 10, Pass},
		{"9 pairs", 0.9, 0.5, 9, Insufficient},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d := loadScores(t, armRows("A0", c.pairs, constant(c.a0)), armRows("A3-8K", c.pairs, constant(c.a3)))
			g := d.G1(seed)
			wantVerdict(t, "G1", g.Verdict, c.verdict, g.Reason)
			wantNear(t, "value", g.Conditions[0].Value, c.a0-c.a3, 1e-12)
		})
	}
	for _, missing := range []string{"A0", "A3-8K"} {
		t.Run("missing "+missing, func(t *testing.T) {
			other := "A0"
			if missing == "A0" {
				other = "A3-8K"
			}
			g := loadScores(t, armRows(other, 12, constant(0.5))).G1(seed)
			wantVerdict(t, "G1", g.Verdict, Insufficient, g.Reason)
			if !strings.Contains(g.Reason, missing) {
				t.Errorf("reason should name %s: %s", missing, g.Reason)
			}
		})
	}
}

func floatVar(x float64) float64 { return x }

func TestG1RecordedRowsDoNotAffectTheVerdict(t *testing.T) {
	base := [][]row{armRows("A0", 12, constant(0.9)), armRows("A3-8K", 12, constant(0.5))}
	g := loadScores(t, base...).G1(seed)
	wantVerdict(t, "G1 without recorded arms", g.Verdict, Pass, g.Reason)
	if len(g.Recorded) != 3 || g.Recorded[0].N != 0 || g.Recorded[1].N != 0 || g.Recorded[2].N != 0 {
		t.Errorf("recorded rows should be present and empty: %+v", g.Recorded)
	}

	// A3-32K and A6 far below the threshold still leave a failing primary failing,
	// and big recorded gaps do not rescue it.
	fail := loadScores(t,
		armRows("A0", 12, constant(0.9)), armRows("A3-8K", 12, constant(0.85)),
		armRows("A3-32K", 12, constant(0.1)), armRows("A6", 12, constant(0.1)),
	).G1(seed)
	wantVerdict(t, "G1 with recorded arms", fail.Verdict, Fail, fail.Reason)
	if fail.Recorded[0].Label() != "A0 - A3-8K-h" || fail.Recorded[1].Label() != "A0 - A3-32K" || fail.Recorded[2].Label() != "A0 - A6" {
		t.Errorf("recorded labels: %s, %s, %s", fail.Recorded[0].Label(), fail.Recorded[1].Label(), fail.Recorded[2].Label())
	}
	wantNear(t, "A0 - A3-32K", fail.Recorded[1].Mean, 0.8, 1e-12)
	if fail.Conditions[0].Paired.CILow == nil {
		t.Error("G1 should report the CI")
	}
}

func alternating(mean, spread float64) func(int) float64 {
	return func(i int) float64 {
		if i%2 == 0 {
			return mean + spread
		}
		return mean - spread
	}
}

func TestG2(t *testing.T) {
	// 0.85 - 0.8 is 0.04999999999999993, so this boundary needs the epsilon guard.
	cases := []struct {
		name    string
		t1      func(int) float64
		pairs   int
		verdict Verdict
	}{
		{"clear gain", constant(0.9), 12, Pass},
		{"exactly 0.05 with a tight CI", constant(0.85), 12, Pass},
		{"just below 0.05", constant(0.8499), 12, Fail},
		{"mean 0.05 with a CI crossing 0", alternating(0.85, 0.15), 20, Fail},
		{"no difference", constant(0.8), 12, Fail},
		{"T1 worse", constant(0.7), 12, Fail},
		{"9 pairs", constant(0.9), 9, Insufficient},
		{"10 pairs", constant(0.9), 10, Pass},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d := loadScores(t, taskRows("T1", c.pairs, c.t1), taskRows("A4", c.pairs, constant(0.8)), taskRows("A4s", c.pairs, constant(0.8)))
			g := d.G2(seed)
			wantVerdict(t, "G2", g.Verdict, c.verdict, g.Reason)
		})
	}
	t.Run("CI lower bound exactly 0 is not above 0", func(t *testing.T) {
		// Nine zeros and one 0.5: mean 0.05, and over a third of resamples miss the
		// 0.5, so the 2.5th percentile of their means is exactly 0.
		t1 := func(i int) float64 {
			if i == 0 {
				return 0.5
			}
			return 0
		}
		g := loadScores(t, taskRows("T1", 10, t1), taskRows("A4", 10, constant(0)), taskRows("A4s", 10, constant(0))).G2(seed)
		p := g.Conditions[0].Paired
		if *p.CILow != 0 {
			t.Fatalf("fixture: CI lower bound = %v, want exactly 0", *p.CILow)
		}
		wantNear(t, "mean", p.Mean, 0.05, 1e-12)
		wantVerdict(t, "G2", g.Verdict, Fail, g.Reason)
	})
	t.Run("missing arm", func(t *testing.T) {
		g := loadScores(t, taskRows("T1", 12, constant(0.9))).G2(seed)
		wantVerdict(t, "G2", g.Verdict, Insufficient, g.Reason)
	})
	t.Run("T1 must also beat A4s", func(t *testing.T) {
		g := loadScores(t, taskRows("T1", 12, constant(0.9)), taskRows("A4", 12, constant(0.8)), taskRows("A4s", 12, constant(0.88))).G2(seed)
		wantVerdict(t, "G2", g.Verdict, Fail, g.Reason)
		wantVerdict(t, "T1 - A4", g.Conditions[0].Verdict, Pass, g.Conditions[0].Reason)
		wantVerdict(t, "T1 - A4s", g.Conditions[1].Verdict, Fail, g.Conditions[1].Reason)
	})
	t.Run("A4s missing leaves the gate undecided", func(t *testing.T) {
		g := loadScores(t, taskRows("T1", 12, constant(0.9)), taskRows("A4", 12, constant(0.8))).G2(seed)
		wantVerdict(t, "G2", g.Verdict, Insufficient, g.Reason)
	})
	t.Run("recorded rows do not affect the verdict", func(t *testing.T) {
		g := loadScores(t,
			taskRows("T1", 12, constant(0.9)), taskRows("A4", 12, constant(0.8)), taskRows("A4s", 12, constant(0.8)),
			taskRows("A4-abrupt", 12, constant(0.95)), taskRows("A0", 12, constant(0.1)),
		).G2(seed)
		wantVerdict(t, "G2", g.Verdict, Pass, g.Reason)
		var abrupt, a0 *float64
		for _, p := range g.Recorded {
			if p.Metric != TaskScore.Name {
				continue
			}
			switch p.Label() {
			case "T1 - A4-abrupt":
				abrupt = p.Mean
			case "T1 - A0":
				a0 = p.Mean
			}
		}
		wantNear(t, "T1 - A4-abrupt", abrupt, -0.05, 1e-12)
		wantNear(t, "T1 - A0", a0, 0.8, 1e-12)
	})
	t.Run("retention rows cannot decide it", func(t *testing.T) {
		g := loadScores(t, armRows("T1", 12, constant(0.9)), armRows("A4", 12, constant(0.1)), armRows("A4s", 12, constant(0.1))).G2(seed)
		wantVerdict(t, "G2", g.Verdict, Insufficient, g.Reason)
	})
}

func TestG2ReadHasToBeatTheControlWithTheList(t *testing.T) {
	base := func(control float64) *Data {
		return loadScores(t, taskRows("T1+read", 12, constant(0.9)), taskRows("A4", 12, constant(0.7)),
			taskRows("A4s", 12, constant(0.7)), taskRows("A4+read", 12, constant(control)))
	}
	g := base(0.7).G2Read(seed)
	wantVerdict(t, "G2 read", g.Verdict, Pass, g.Reason)
	g = base(0.88).G2Read(seed)
	wantVerdict(t, "G2 read", g.Verdict, Fail, g.Reason)
	wantVerdict(t, "T1+read - A4", g.Conditions[0].Verdict, Pass, g.Conditions[0].Reason)
	wantVerdict(t, "T1+read - A4+read", g.Conditions[2].Verdict, Fail, g.Conditions[2].Reason)
}

// g6Case describes a passing C group; each test moves one value.
type g6Case struct {
	pairs                    int
	c0, c3, c6, c3Own, c8Own float64
	lossy                    int
	forkInput, forkCached    int
	forkWrite                int
	skipArm                  string
	noForkRows               bool
	noLossyCounter           bool
	// The spend of the arm that runs both forks.
	forkAUSD, forkBUSD, mainUSD float64
	noCostRows                  bool
}

func passingG6() g6Case {
	return g6Case{
		pairs: 12, c0: 0.7, c3: 0.8, c6: 0.8, c3Own: 0.7, c8Own: 0.7,
		forkInput: 1000, forkCached: 800, forkWrite: 100,
		forkAUSD: 0.10, forkBUSD: 0.10, mainUSD: 1.00,
	}
}

func (c g6Case) load(t *testing.T) *Data {
	t.Helper()
	var scores, ledger []row
	add := func(arm string, retention, own float64) {
		if arm == c.skipArm {
			return
		}
		for i := range c.pairs {
			r := scoreRow(arm, sid(i), retention)
			r["own_retention"] = own
			if arm == "C3" {
				if c.noLossyCounter {
					delete(r, "counters")
				} else if i == 0 {
					r["counters"] = row{"lossy_events": c.lossy}
				}
			}
			scores = append(scores, r)
		}
	}
	add("C0", c.c0, c.c3Own)
	add("C3", c.c3, c.c3Own)
	add("C6", c.c6, c.c3Own)
	add("C8", c.c3, c.c8Own)
	if !c.noForkRows && c.skipArm != "C3" {
		ledger = append(ledger,
			ledgerRow("C3", sid(0), "cloud", "fork_b", c.forkInput, c.forkCached, c.forkWrite, 100, 0.01),
			// Other roles and sides do not enter the fork rate.
			ledgerRow("C3", sid(0), "cloud", "main", 5000, 0, 0, 100, 0.5),
			ledgerRow("C3", sid(0), "local", "fork_b", 5000, 0, 0, 100, 0),
		)
	}
	if !c.noCostRows {
		ledger = append(ledger,
			ledgerRow("C3+A", sid(0), "cloud", "fork_a", 1000, 900, 0, 100, c.forkAUSD),
			ledgerRow("C3+A", sid(0), "cloud", "fork_b", 1000, 900, 0, 100, c.forkBUSD),
			ledgerRow("C3+A", sid(0), "cloud", "main", 1000, 900, 0, 100, c.mainUSD),
		)
	}
	return mustLoad(t, writeRun(t, scores, ledger))
}

func conditionVerdicts(g GateResult) []Verdict {
	out := make([]Verdict, len(g.Conditions))
	for i, c := range g.Conditions {
		out[i] = c.Verdict
	}
	return out
}

func TestG6Conditions(t *testing.T) {
	cases := []struct {
		name    string
		change  func(*g6Case)
		verdict Verdict
		failing int // index of the only sub-condition not passing, or -1
	}{
		{"all pass", func(*g6Case) {}, Pass, -1},
		{"C3 - C0 at exactly 0.05", func(c *g6Case) { c.c0 = 0.75 }, Pass, -1},
		{"C3 - C0 just below 0.05", func(c *g6Case) { c.c0 = 0.7501 }, Fail, 0},
		{"C3 exactly 0.05 below C6", func(c *g6Case) { c.c6 = 0.85 }, Pass, -1},
		{"C3 more than 0.05 below C6", func(c *g6Case) { c.c6 = 0.8501 }, Fail, 1},
		{"C3 exactly 0.05 below C8 on own", func(c *g6Case) { c.c8Own = 0.75 }, Pass, -1},
		{"C3 more than 0.05 below C8 on own", func(c *g6Case) { c.c8Own = 0.7501 }, Fail, 2},
		{"one lossy event", func(c *g6Case) { c.lossy = 1 }, Fail, 3},
		{"lossy counter missing is not zero", func(c *g6Case) { c.noLossyCounter = true }, Insufficient, 3},
		{"fork cache exactly 0.70", func(c *g6Case) { c.forkCached, c.forkWrite = 700, 0 }, Pass, -1},
		{"fork cache just below 0.70", func(c *g6Case) { c.forkCached, c.forkWrite = 699, 0 }, Fail, 4},
		{"no fork rows", func(c *g6Case) { c.noForkRows = true }, Insufficient, 4},
		{"C6 missing", func(c *g6Case) { c.skipArm = "C6" }, Insufficient, 1},
		{"C8 missing", func(c *g6Case) { c.skipArm = "C8" }, Insufficient, 2},
		{"C0 missing", func(c *g6Case) { c.skipArm = "C0" }, Insufficient, 0},
		{"forks at exactly a quarter of main", func(c *g6Case) { c.forkAUSD, c.forkBUSD = 0.15, 0.10 }, Pass, -1},
		{"forks just above a quarter of main", func(c *g6Case) { c.forkAUSD, c.forkBUSD = 0.15, 0.1001 }, Fail, 5},
		{"no C3+A rows", func(c *g6Case) { c.noCostRows = true }, Insufficient, 5},
		{"C3+A with no main spend", func(c *g6Case) { c.mainUSD = 0 }, Insufficient, 5},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := passingG6()
			tc.change(&c)
			g := c.load(t).G6(seed)
			wantVerdict(t, "G6", g.Verdict, tc.verdict, g.Reason)
			if len(g.Conditions) != 6 {
				t.Fatalf("conditions = %d, want 6", len(g.Conditions))
			}
			for i, v := range conditionVerdicts(g) {
				want := Pass
				if i == tc.failing {
					want = tc.verdict
				}
				wantVerdict(t, g.Conditions[i].Name, v, want, g.Conditions[i].Reason)
			}
		})
	}
}

func TestG6InsufficientCases(t *testing.T) {
	c3Missing := passingG6()
	c3Missing.skipArm = "C3"
	g := c3Missing.load(t).G6(seed)
	wantVerdict(t, "G6 without C3", g.Verdict, Insufficient, g.Reason)
	// The cost condition reads C3+A, which runs on its own.
	for _, cond := range g.Conditions[:5] {
		wantVerdict(t, cond.Name, cond.Verdict, Insufficient, cond.Reason)
	}

	few := passingG6()
	few.pairs = 9
	g = few.load(t).G6(seed)
	wantVerdict(t, "G6 with 9 pairs", g.Verdict, Insufficient, g.Reason)

	// A required arm missing is INSUFFICIENT even when another part already fails,
	// and the reason says so.
	both := passingG6()
	both.skipArm = "C6"
	both.lossy = 2
	g = both.load(t).G6(seed)
	wantVerdict(t, "G6 missing arm and a failure", g.Verdict, Insufficient, g.Reason)
	if !strings.Contains(g.Reason, "already failing") || !strings.Contains(g.Reason, "lossy_events") {
		t.Errorf("reason should name the failing part: %s", g.Reason)
	}
}

func TestG6UsesOwnRetentionForC8(t *testing.T) {
	c := passingG6()
	c.c3Own, c.c8Own = 0.2, 0.2
	g := c.load(t).G6(seed)
	wantVerdict(t, "G6", g.Verdict, Pass, g.Reason)
	if got := g.Conditions[2].Paired.Metric; got != "own_retention" {
		t.Errorf("C8 comparison metric = %s", got)
	}
	if got := g.Conditions[1].Paired.Metric; got != "retention" {
		t.Errorf("C6 comparison metric = %s", got)
	}
}

func TestG6ForkCacheIsPooledByTokens(t *testing.T) {
	var scores []row
	for _, arm := range []string{"C0", "C3", "C6", "C8"} {
		retention := 0.8
		if arm == "C0" {
			retention = 0.7
		}
		scores = append(scores, armRows(arm, 12, constant(retention))...)
	}
	missing := ledgerRow("C3", sid(2), "cloud", "fork_b", 0, 0, 0, 0, 0)
	missing["usage_missing"] = true
	ledger := []row{
		// Pooled by tokens the rate is 1700/2800 = .607. The mean of the three row
		// rates (.9, 0, 1) is .633, and any single role gives another answer.
		ledgerRow("C3", sid(0), "cloud", "fork_a", 1000, 900, 50, 100, 0.01),
		ledgerRow("C3", sid(1), "cloud", "fork_b", 1000, 0, 100, 100, 0.01),
		ledgerRow("C3", sid(1), "cloud", "fork_merged", 500, 800, 0, 100, 0.01),
		missing,
	}
	d := mustLoad(t, writeRun(t, scores, ledger))
	f := d.G6(seed).ForkCache
	// fork_merged reports cached above input: uncached clamps to 0 and is counted.
	if f.Cached != 1700 || f.Write != 150 || f.Uncached != 50+900+0 || f.Inconsistent != 1 || f.UsageMissing != 1 || f.Calls != 4 {
		t.Errorf("fork totals: %+v", f.TokenTotals)
	}
	wantNear(t, "rate", f.Rate, 1700.0/(1700+150+950), 1e-12)
}

func TestG6AlternativesReportCostAndDoNotAffectTheVerdict(t *testing.T) {
	c := passingG6()
	d := c.load(t)
	if g := d.G6(seed); g.Alternatives[2].CloudUSD != nil || g.Alternatives[3].Paired.N != 0 {
		t.Errorf("absent C4 and C7 should stay undefined: %+v", g.Alternatives)
	}

	var scores []row
	scores = append(scores, armRows("C0", 12, constant(0.7))...)
	scores = append(scores, armRows("C3", 12, constant(0.8))...)
	scores = append(scores, armRows("C6", 12, constant(0.8))...)
	scores = append(scores, armRows("C8", 12, constant(0.8))...)
	scores = append(scores, armRows("C4", 12, constant(0.9))...)
	scores = append(scores, armRows("C7", 12, constant(0.1))...)
	ledger := []row{
		ledgerRow("C3", sid(0), "cloud", "fork_b", 1000, 900, 0, 100, 0.25),
		ledgerRow("C4", sid(0), "cloud", "fork_merged", 1000, 500, 0, 100, 0.5),
		ledgerRow("C4", sid(1), "cloud", "main", 1000, 0, 0, 100, 1.0),
		ledgerRow("C7", sid(0), "cloud", "fork_b", 1000, 0, 1000, 100, 2.0),
		ledgerRow("C3+A", sid(0), "cloud", "fork_a", 1000, 900, 0, 100, 0.01),
		ledgerRow("C3+A", sid(0), "cloud", "main", 1000, 900, 0, 100, 1.0),
	}
	g := mustLoad(t, writeRun(t, scores, ledger)).G6(seed)
	wantVerdict(t, "G6", g.Verdict, Pass, g.Reason)
	if len(g.Alternatives) != 4 {
		t.Fatalf("alternatives = %d, want 4", len(g.Alternatives))
	}
	ref, c4, c7 := g.Alternatives[0], g.Alternatives[2], g.Alternatives[3]
	if ref.Arm != "C3" || ref.Paired != nil {
		t.Errorf("reference row: %+v", ref)
	}
	wantNear(t, "C3 USD", ref.CloudUSD, 0.25, 1e-12)
	wantNear(t, "C4 - C3", c4.Paired.Mean, 0.1, 1e-12)
	wantNear(t, "C7 - C3", c7.Paired.Mean, -0.7, 1e-12)
	wantNear(t, "C4 USD", c4.CloudUSD, 1.5, 1e-12)
	wantNear(t, "C4 USD per session", c4.CloudUSDPerSession, 0.75, 1e-12)
	wantNear(t, "C4 fork USD", c4.ForkUSD, 0.5, 1e-12)
	wantNear(t, "C4 fork rate", c4.ForkCacheRate, 0.5, 1e-12)
	wantNear(t, "C7 fork rate", c7.ForkCacheRate, 0, 1e-12)
}

func TestReasonsAreOneLine(t *testing.T) {
	r := Build(passingG6().load(t), seed)
	lines := []string{r.G1.Reason, r.G6.Reason, r.Privacy.Reason, r.SampleSize.Statement}
	for _, g := range []GateResult{r.G1, r.G6} {
		for _, c := range g.Conditions {
			lines = append(lines, c.Reason)
		}
	}
	for _, line := range lines {
		if line == "" || strings.ContainsAny(line, "\r\n") {
			t.Errorf("reason is not one line: %q", line)
		}
	}
}
