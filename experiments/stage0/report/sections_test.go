package report

import (
	"math"
	"strings"
	"testing"
)

// costFixture is the hand-computed ledger behind TestCostHandComputed.
func costFixture() []row {
	s1, s2 := sid(1), sid(2)

	main1 := ledgerRow("X", s1, "cloud", "main", 1000, 600, 100, 200, 0.01)
	main1["reasoning"], main1["wait_ms"], main1["latency_ms"] = 50, 10, 100

	fork := ledgerRow("X", s1, "cloud", "fork_b", 1000, 900, 0, 100, 0.002)
	fork["reasoning"], fork["wait_ms"], fork["latency_ms"] = 20, 30, 300

	main2 := ledgerRow("X", s2, "cloud", "main", 500, 0, 0, 50, 0.005)
	main2["wait_ms"], main2["latency_ms"], main2["attempt"] = 20, 200, 2

	missing := ledgerRow("X", s2, "cloud", "compress", 0, 0, 0, 0, 0)
	missing["usage_missing"], missing["wait_ms"], missing["latency_ms"] = true, 40, 400

	probe := ledgerRow("X", s2, "cloud", "probe", 200, 100, 0, 10, 0.001)
	probe["status"], probe["incomplete"], probe["wait_ms"], probe["latency_ms"] = "incomplete", "max_output_tokens", 50, 500

	over := ledgerRow("X", s2, "cloud", "main", 0, 0, 0, 0, 0)
	over["http"], over["overflow"], over["error"], over["status"], over["wait_ms"], over["latency_ms"] = 400, true, "too long", "", 60, 600

	local1 := ledgerRow("X", s2, "local", "main", 300, 0, 0, 30, 0)
	local1["status"], local1["wait_ms"], local1["latency_ms"] = "stop", 100, 1000

	local2 := ledgerRow("X", s2, "local", "main", 400, 0, 0, 40, 0)
	local2["status"], local2["wait_ms"], local2["latency_ms"] = "length", 300, 3000

	return []row{main1, fork, main2, missing, probe, over, local1, local2}
}

func costFor(t *testing.T, d *Data, arm string) ArmCost {
	t.Helper()
	for _, c := range d.Cost().Arms {
		if c.Arm == arm {
			return c
		}
	}
	t.Fatalf("no cost row for %s", arm)
	return ArmCost{}
}

func TestCostHandComputed(t *testing.T) {
	d := mustLoad(t, writeRun(t, []row{scoreRow("X", sid(1), 0.5)}, costFixture()))
	c := costFor(t, d, "X")

	if c.LedgerSessions != 2 || c.ScoredSessions != 1 || c.Rows != 8 {
		t.Errorf("sessions %d scored %d rows %d", c.LedgerSessions, c.ScoredSessions, c.Rows)
	}
	wantNear(t, "cloud USD", c.CloudUSD, 0.018, 1e-12)
	wantNear(t, "cloud USD/session", c.CloudUSDPerSession, 0.009, 1e-12)
	wantNear(t, "fork USD", c.ForkUSD, 0.002, 1e-12)
	wantNear(t, "main USD", c.MainUSD, 0.015, 1e-12)
	wantNear(t, "fork/main", c.ForkToMain, 0.002/0.015, 1e-12)
	wantNear(t, "fork share", c.ForkShare, 0.002/0.017, 1e-12)

	// Cloud tokens: input 1000+1000+500+200, cached 600+900+100, write 100,
	// uncached 300+100+500+100, output 200+100+50+10, reasoning 50+20.
	cl := c.Cloud
	if cl.Calls != 6 || cl.Input != 2700 || cl.Cached != 1600 || cl.Write != 100 || cl.Uncached != 1000 || cl.Output != 360 || cl.Reasoning != 70 {
		t.Errorf("cloud totals: %+v", cl.TokenTotals)
	}
	wantNear(t, "cloud cache rate", cl.CacheHitRate, 1600.0/2700, 1e-12)
	wantNear(t, "cloud reasoning share", cl.ReasoningShare, 70.0/360, 1e-12)
	// Wait 10 20 30 40 50 60: P50 35, P95 at rank 4.75 is 50 + .75*10.
	wantNear(t, "cloud wait P50", cl.WaitP50, 35, 1e-9)
	wantNear(t, "cloud wait P95", cl.WaitP95, 57.5, 1e-9)

	lo := c.Local
	if lo.Calls != 2 || lo.Input != 700 || lo.Output != 70 || lo.Uncached != 700 {
		t.Errorf("local totals: %+v", lo.TokenTotals)
	}
	wantNear(t, "local latency P50", lo.LatencyP50, 2000, 1e-9)
	wantNear(t, "local latency P95", lo.LatencyP95, 2900, 1e-9)
	wantNear(t, "local wait P50", lo.WaitP50, 200, 1e-9)
	wantNear(t, "local wait P95", lo.WaitP95, 290, 1e-9)
	wantNear(t, "local reasoning share", lo.ReasoningShare, 0, 1e-12)
	// The local transport records cached tokens only when reported, so a zero total
	// is not a measured rate of 0.
	if lo.CacheHitRate != nil {
		t.Errorf("local cache rate = %v, want undefined", *lo.CacheHitRate)
	}

	if c.UsageMissing != 1 || c.IncompleteRows != 1 || c.LocalLengthRows != 1 || c.RetriedRows != 1 || c.OverflowRows != 1 || c.ErrorRows != 1 || c.UnknownSideRows != 0 {
		t.Errorf("row counts: missing %d incomplete %d length %d retried %d overflow %d errors %d unknown %d",
			c.UsageMissing, c.IncompleteRows, c.LocalLengthRows, c.RetriedRows, c.OverflowRows, c.ErrorRows, c.UnknownSideRows)
	}

	byRole := map[string]RoleCost{}
	for _, rc := range c.ByRole {
		byRole[rc.Side+"/"+rc.Role] = rc
	}
	if len(byRole) != 5 {
		t.Fatalf("roles: %v", byRole)
	}
	mainRole := byRole["cloud/main"]
	if mainRole.Calls != 3 || mainRole.Cached != 600 || mainRole.Write != 100 || mainRole.Uncached != 800 {
		t.Errorf("cloud/main: %+v", mainRole)
	}
	if !near(mainRole.USD, 0.015, 1e-12) {
		t.Errorf("cloud/main USD = %v", mainRole.USD)
	}
	wantNear(t, "cloud/main USD/session", mainRole.USDPerSession, 0.0075, 1e-12)
	wantNear(t, "cloud/main cache", mainRole.CacheHitRate, 600.0/1500, 1e-12)
	wantNear(t, "cloud/fork_b cache", byRole["cloud/fork_b"].CacheHitRate, 0.9, 1e-12)
	if rc := byRole["cloud/compress"]; rc.UsageMissing != 1 || rc.CacheHitRate != nil {
		t.Errorf("cloud/compress should state missing usage and no rate: %+v", rc)
	}

	if rc := byRole["local/main"]; rc.CacheHitRate != nil {
		t.Errorf("local/main cache rate = %v, want undefined", *rc.CacheHitRate)
	}

	want := []string{"cloud/compress", "cloud/fork_b", "cloud/main", "cloud/probe", "local/main"}
	for i, rc := range c.ByRole {
		if got := rc.Side + "/" + rc.Role; got != want[i] {
			t.Errorf("ByRole[%d] = %s, want %s", i, got, want[i])
		}
	}
}

func TestCostStatesAbsentLedgerAndUnknownSide(t *testing.T) {
	d := mustLoad(t, writeRun(t,
		[]row{scoreRow("NoLedger", sid(0), 0.5)},
		[]row{ledgerRow("Odd", sid(0), "edge", "main", 10, 0, 0, 1, 5.0)},
	))
	none := costFor(t, d, "NoLedger")
	if none.CloudUSD != nil || none.CloudUSDPerSession != nil || none.Rows != 0 || none.ScoredSessions != 1 {
		t.Errorf("an absent ledger must not read as zero spend: %+v", none)
	}
	if none.Local.LatencyP50 != nil || none.Cloud.WaitP95 != nil || none.Cloud.CacheHitRate != nil {
		t.Errorf("absent timing must stay undefined: %+v", none)
	}

	odd := costFor(t, d, "Odd")
	if odd.UnknownSideRows != 1 || len(odd.ByRole) != 1 || odd.Cloud.Calls != 0 || odd.Local.Calls != 0 {
		t.Errorf("unknown side: %+v", odd)
	}
	wantNear(t, "cloud USD excludes the unknown side", odd.CloudUSD, 0, 0)
}

func TestCostCountsUsageMissingRows(t *testing.T) {
	missing := ledgerRow("X", sid(0), "cloud", "main", 0, 0, 0, 0, 0)
	missing["usage_missing"] = true
	other := ledgerRow("X", sid(0), "local", "main", 0, 0, 0, 0, 0)
	other["usage_missing"] = true
	normal := ledgerRow("X", sid(0), "cloud", "main", 100, 50, 0, 10, 0.1)
	d := mustLoad(t, writeRun(t, []row{}, []row{missing, other, normal}))
	c := costFor(t, d, "X")
	if c.UsageMissing != 2 || c.Cloud.UsageMissing != 1 || c.Local.UsageMissing != 1 {
		t.Errorf("usage missing: arm %d cloud %d local %d", c.UsageMissing, c.Cloud.UsageMissing, c.Local.UsageMissing)
	}
	if c.ByRole[0].UsageMissing != 1 {
		t.Errorf("role usage missing: %+v", c.ByRole[0])
	}
	wantNear(t, "rate reads only known tokens", c.Cloud.CacheHitRate, 0.5, 1e-12)
}

func TestPrivacy(t *testing.T) {
	hit := func(n int) row {
		r := scoreRow("T1", sid(0), 0.5)
		r["canary_hits"] = n
		return r
	}
	clean := ledgerRow("T1", sid(0), "cloud", "main", 1, 0, 0, 1, 0)
	leak := ledgerRow("T1", sid(0), "cloud", "main", 1, 0, 0, 1, 0)
	leak["canary_hits"] = 2
	noCount := scoreRow("T1", sid(1), 0.5)
	delete(noCount, "canary_hits")
	otherArm := scoreRow("A0", sid(0), 0.5)
	otherArm["canary_hits"] = 9

	cases := []struct {
		name          string
		scores        []row
		ledger        []row
		verdict       Verdict
		scoreHits     int
		ledgerHits    int
		scoreRowsMiss int
	}{
		{"clean", []row{hit(0)}, []row{clean}, Pass, 0, 0, 0},
		{"other arms do not decide", []row{hit(0), otherArm}, []row{clean}, Pass, 0, 0, 0},
		{"hit in scores", []row{hit(1)}, []row{clean}, Fail, 1, 0, 0},
		{"hit in ledger", []row{hit(0)}, []row{leak}, Fail, 0, 2, 0},
		{"hit outranks a missing count", []row{hit(1), noCount}, []row{clean}, Fail, 1, 0, 1},
		{"missing count is not zero", []row{hit(0), noCount}, []row{clean}, Insufficient, 0, 0, 1},
		{"no T1", []row{otherArm}, []row{}, Insufficient, 0, 0, 0},
		{"no T1 ledger", []row{hit(0)}, []row{}, Insufficient, 0, 0, 0},
		{"no T1 scores", []row{}, []row{clean}, Insufficient, 0, 0, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := mustLoad(t, writeRun(t, c.scores, c.ledger)).Privacy()
			wantVerdict(t, "privacy", s.Verdict, c.verdict, s.Reason)
			for _, r := range s.Rows {
				if r.Arm != "T1" {
					continue
				}
				if r.ScoreHits != c.scoreHits || r.LedgerHits != c.ledgerHits || r.ScoreRowsMissing != c.scoreRowsMiss {
					t.Errorf("T1 row: %+v", r)
				}
			}
		})
	}

	s := mustLoad(t, writeRun(t, []row{hit(0), otherArm}, []row{clean})).Privacy()
	for _, r := range s.Rows {
		if r.Arm == "A0" && r.ScoreHits != 9 {
			t.Errorf("A0 hits should be recorded: %+v", r)
		}
	}
}

func TestPrivacyKeepsHitsOnReplacedRowsOnRecord(t *testing.T) {
	leaky := scoreRow("T1", sid(0), 0.5)
	leaky["canary_hits"] = 1
	leakyLedger := ledgerRow("T1", sid(0), "cloud", "main", 1, 0, 0, 1, 0)
	leakyLedger["canary_hits"] = 2
	first := writeRun(t, []row{leaky}, []row{leakyLedger})
	second := writeRun(t, []row{scoreRow("T1", sid(0), 0.5)}, []row{ledgerRow("T1", sid(0), "cloud", "main", 1, 0, 0, 1, 0)})

	s := mustLoad(t, first, second).Privacy()
	wantVerdict(t, "privacy", s.Verdict, Pass, s.Reason)
	if s.Rows[0].ReplacedHits != 3 || s.Rows[0].ScoreHits != 0 || s.Rows[0].LedgerHits != 0 {
		t.Errorf("row: %+v", s.Rows[0])
	}
	if !strings.Contains(s.Reason, "3 hits on T1 rows replaced") {
		t.Errorf("reason should state the replaced hits: %s", s.Reason)
	}
}

func TestNeededPairs(t *testing.T) {
	cases := []struct {
		sd, delta float64
		want      int
	}{
		{0.2, 0.05, 126},  // 7.84 * .04 / .0025 = 125.44
		{0.2, 0.10, 32},   // 7.84 * .04 / .01 = 31.36
		{0.25, 0.05, 196}, // exactly 196: must not round up to 197 on noise
		{0, 0.05, 0},
	}
	for _, c := range cases {
		if got := NeededPairs(c.sd, c.delta); got != c.want {
			t.Errorf("NeededPairs(%v, %v) = %d, want %d", c.sd, c.delta, got, c.want)
		}
	}
}

func TestSampleSizeUsesObservedSD(t *testing.T) {
	// Alternating differences of +-s over 20 pairs have SD s*sqrt(20/19).
	d := loadScores(t,
		armRows("T1", 20, alternating(0.5, 0.1)), armRows("A4", 20, constant(0.5)),
		armRows("C3", 20, alternating(0.5, 0.2)), armRows("C0", 20, constant(0.5)),
	)
	s := d.SampleSize()
	byName := map[string]SampleSizeRow{}
	for _, r := range s.Rows {
		byName[r.Comparison] = r
	}
	t1 := byName["T1 - A4"]
	wantNear(t, "T1 SD", t1.SD, 0.1*math.Sqrt(20.0/19), 1e-12)
	// .01*20/19*7.84/.0025 = 33.01 and /.01 = 8.25
	if *t1.NeedLow != 34 || *t1.NeedHigh != 9 || t1.DrivesL4 || t1.Pairs != 20 {
		t.Errorf("T1 row: %+v, low %d high %d", t1, *t1.NeedLow, *t1.NeedHigh)
	}
	c3 := byName["C3 - C0"]
	// .04*20/19*7.84/.0025 = 132.04 and /.01 = 33.01
	if *c3.NeedLow != 133 || *c3.NeedHigh != 34 {
		t.Errorf("C3 row: low %d high %d", *c3.NeedLow, *c3.NeedHigh)
	}
	if !s.L4Complete || *s.L4Low != 133 || *s.L4High != 34 {
		t.Errorf("L4: complete %v low %v high %v", s.L4Complete, *s.L4Low, *s.L4High)
	}
	if !strings.Contains(s.Statement, "133") || !strings.Contains(s.Statement, "C3 - C0") {
		t.Errorf("statement: %s", s.Statement)
	}
	if byName["A0 - A3-8K"].SD != nil || byName["A0 - A3-8K"].DrivesL4 {
		t.Errorf("absent comparison should have no SD: %+v", byName["A0 - A3-8K"])
	}
}

func TestSampleSizeStatesWhatItLacks(t *testing.T) {
	empty := loadScores(t, armRows("Z", 1, constant(0.5))).SampleSize()
	if empty.L4Low != nil || empty.L4Complete {
		t.Errorf("no driving SD: %+v", empty)
	}
	if !strings.Contains(empty.Statement, "undetermined") {
		t.Errorf("statement: %s", empty.Statement)
	}

	partial := loadScores(t, taskRows("T1", 20, alternating(0.5, 0.1)), taskRows("A4", 20, constant(0.5))).TaskSampleSize()
	if partial.L4Complete || *partial.L4Low != 34 {
		t.Errorf("partial: %+v", partial)
	}
	if !strings.Contains(partial.Statement, "lower bound") || !strings.Contains(partial.Statement, "T1 - A4s") {
		t.Errorf("statement: %s", partial.Statement)
	}

	// A need below the gate's own minimum still floors at MinPairs.
	low := loadScores(t,
		armRows("T1", 4, constant(0.6)), armRows("A4", 4, constant(0.5)),
		armRows("C3", 4, constant(0.6)), armRows("C0", 4, constant(0.5)),
	).SampleSize()
	if *low.L4Low != MinPairs || *low.L4High != MinPairs {
		t.Errorf("floor: %d, %d", *low.L4Low, *low.L4High)
	}
	if !strings.Contains(low.Statement, "fewer than 10 pairs") {
		t.Errorf("a thin SD should be stated: %s", low.Statement)
	}
}
