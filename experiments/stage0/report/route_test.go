package report

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// routeFixture builds rows from a small script. Each item says which side is right, what
// each classifier chose in R2 and R3, and at what confidence.
type routeCase struct {
	kind             string
	owner            string
	cloudOK, localOK bool
	r1               string
	r2, r3           string
	r3Stale          string
	r2Conf, r3Conf   float64
	edge             bool
	seed             int
}

func routeRow(i int, c routeCase) row {
	worlds := row{}
	for _, w := range []string{"r5k", "r20k", "none"} {
		worlds[w] = row{"local": row{"correct": c.localOK, "reply": "x"}, "has_summary": w != "none", "behind_tokens": 100}
	}
	seed := c.seed
	if seed == 0 {
		seed = 1000 + i
	}
	decisions := []row{}
	if !c.edge {
		decisions = append(decisions, row{"router": "R2", "classifier": "stub", "choice": c.r2, "confidence": c.r2Conf, "latency_ms": 10, "state_bytes": 100})
		for _, w := range []string{"r5k", "r20k", "none"} {
			choice := c.r3
			if w != "r5k" && c.r3Stale != "" {
				choice = c.r3Stale
			}
			decisions = append(decisions, row{"router": "R3", "world": w, "classifier": "stub", "choice": choice, "confidence": c.r3Conf, "latency_ms": 20, "state_bytes": 5000})
		}
	}
	return row{
		"arm": "S3", "session": sid(i), "set": "s3-tuning", "retention": nil, "counters": row{},
		"route": row{
			"kind": c.kind, "seed": seed, "owner": c.owner, "disclosure": "shared", "cloud_eligible": !c.edge,
			"cloud":     row{"correct": c.cloudOK, "reply": "x"},
			"worlds":    worlds,
			"rules":     row{"R0": c.owner, "R1": c.r1, "R1s": c.r1},
			"decisions": decisions,
		},
	}
}

func routeData(t *testing.T, cases []routeCase) *Data {
	t.Helper()
	return routeDataOn(t, "s3-tuning", cases)
}

func routeDataOn(t *testing.T, set string, cases []routeCase) *Data {
	t.Helper()
	var scores []row
	for i, c := range cases {
		r := routeRow(i, c)
		r["set"] = set
		scores = append(scores, r)
	}
	return mustLoad(t, writeRun(t, scores, nil))
}

func stubThetas(v float64) map[string]float64 {
	return map[string]float64{"stub/R2": v, "stub/R3": v}
}

func rowOf(t *testing.T, s RouteSection, world, router, saw string) RouterRow {
	t.Helper()
	for _, r := range s.Rows {
		if r.World == world && r.Router == router && r.SawSummary == saw {
			return r
		}
	}
	t.Fatalf("no row %s %s %s", world, router, saw)
	return RouterRow{}
}

func TestRouteRegretAndItsParts(t *testing.T) {
	cases := []routeCase{
		// Only the cloud is right, and the router stays on local: a missed answer.
		{kind: "cloud_context", owner: "local", cloudOK: true, r1: "local", r2: "local", r3: "cloud", r2Conf: 0.9, r3Conf: 0.9},
		// Both are right, and the router takes the cloud: a needless call.
		{kind: "followup", owner: "cloud", cloudOK: true, localOK: true, r1: "cloud", r2: "cloud", r3: "local", r2Conf: 0.9, r3Conf: 0.9},
		// Only local is right, and the router takes the cloud.
		{kind: "local_tool", owner: "cloud", localOK: true, r1: "local", r2: "cloud", r3: "local", r2Conf: 0.9, r3Conf: 0.9},
		// Neither is right, so the oracle takes the free side.
		{kind: "reasoning", owner: "cloud", r1: "cloud", r2: "cloud", r3: "local", r2Conf: 0.9, r3Conf: 0.9},
	}
	s := routeData(t, cases).Route(seed, stubThetas(0.8))
	r2 := rowOf(t, s, "r5k", "R2", "")
	// Utilities: 0.75 lost; 0.25 lost; 1.25 lost; 0.25 lost.
	if d := r2.Regret - (0.75+0.25+1.25+0.25)/4; d > 1e-12 || d < -1e-12 {
		t.Errorf("R2 regret = %v", r2.Regret)
	}
	if r2.Accuracy != 0.25 || r2.MissedAnswer != 0.5 || r2.NeedlessCloud != 0.75 || r2.CloudShare != 0.75 {
		t.Errorf("R2 = %+v", r2)
	}
	r3 := rowOf(t, s, "r5k", "R3", "r5k")
	if r3.Regret != 0 || r3.Accuracy != 0.75 || r3.MissedAnswer != 0 {
		t.Errorf("R3 = %+v", r3)
	}
	oracle := rowOf(t, s, "r5k", "R4", "")
	if oracle.Regret != 0 || oracle.CloudShare != 0.25 {
		t.Errorf("oracle = %+v", oracle)
	}
	r0 := rowOf(t, s, "r5k", "R0", "")
	if r0.Switches != 0 {
		t.Errorf("R0 switched: %+v", r0)
	}
}

func TestRouteThresholdStaysWithTheOwnerAndFailureToo(t *testing.T) {
	base := routeCase{kind: "followup", owner: "cloud", localOK: true, r1: "cloud", r2: "local", r3: "local", r2Conf: 0.6, r3Conf: 0.85}
	s := routeData(t, []routeCase{base}).Route(seed, stubThetas(0.8))
	if r := rowOf(t, s, "r5k", "R2", ""); r.CloudShare != 1 || r.Switches != 0 {
		t.Errorf("below the threshold, R2 left the owner: %+v", r)
	}
	if r := rowOf(t, s, "r5k", "R3", "r5k"); r.CloudShare != 0 || r.Switches != 1 {
		t.Errorf("above the threshold, R3 stayed: %+v", r)
	}
	s = routeData(t, []routeCase{base}).Route(seed, stubThetas(0.5))
	if r := rowOf(t, s, "r5k", "R2", ""); r.CloudShare != 0 {
		t.Errorf("a lower threshold changed nothing: %+v", r)
	}
	rows := []row{routeRow(0, base)}
	route := rows[0]["route"].(row)
	route["decisions"] = []row{{"router": "R2", "classifier": "stub", "error": "boom"}, {"router": "R3", "world": "r5k", "classifier": "stub", "error": "boom"}}
	s = mustLoad(t, writeRun(t, rows, nil)).Route(seed, nil)
	if r := rowOf(t, s, "r5k", "R2", ""); r.CloudShare != 1 || r.Failed != 1 {
		t.Errorf("a failed decision did not stay with the owner: %+v", r)
	}
	if s.Thetas["stub/R2"] != RouteDefaultTheta || s.Thetas["stub/R3"] != RouteDefaultTheta {
		t.Errorf("thetas = %v", s.Thetas)
	}
}

func TestRouteEdgeOnlyAlwaysGoesLocal(t *testing.T) {
	c := routeCase{kind: "edge_private", owner: "cloud", cloudOK: true, localOK: false, r1: "local", edge: true}
	s := routeData(t, []routeCase{c}).Route(seed, nil)
	for _, r := range s.Rows {
		if r.CloudShare != 0 {
			t.Errorf("%s took the cloud on an edge_only query", r.Router)
		}
		if r.Regret != 0 {
			t.Errorf("%s has regret %v where no other route exists", r.Router, r.Regret)
		}
	}
}

func g7Cases(n int, r3Right bool) []routeCase {
	var out []routeCase
	for i := range n {
		c := routeCase{kind: "cloud_context", owner: "local", cloudOK: true, r1: "local", r2: "local", r2Conf: 0.9, r3Conf: 0.9, r3: "local"}
		if r3Right {
			c.r3 = "cloud"
		}
		if i%2 == 0 {
			c = routeCase{kind: "followup", owner: "cloud", localOK: true, cloudOK: true, r1: "local", r2: "local", r3: "local", r2Conf: 0.9, r3Conf: 0.9}
		}
		out = append(out, c)
	}
	return out
}

func TestG7Verdicts(t *testing.T) {
	s := routeData(t, g7Cases(12, true)).Route(seed, stubThetas(0.8))
	if len(s.Gates) != 1 || s.Gates[0].Verdict != Pass {
		t.Fatalf("clear gain: %+v", s.Gates)
	}
	wantNear(t, "reduction", s.Gates[0].Conditions[0].Value, 1.0, 1e-12)
	if s.Comparisons[0].CILow == nil || *s.Comparisons[0].CILow <= 0 {
		t.Errorf("CI = %+v", s.Comparisons[0])
	}

	s = routeData(t, g7Cases(12, false)).Route(seed, stubThetas(0.8))
	if s.Gates[0].Verdict != Fail {
		t.Errorf("no gain: %+v", s.Gates[0])
	}

	s = routeData(t, g7Cases(9, true)).Route(seed, stubThetas(0.8))
	if s.Gates[0].Verdict != Insufficient {
		t.Errorf("9 items: %+v", s.Gates[0])
	}

	var perfect []routeCase
	for range 12 {
		perfect = append(perfect, routeCase{kind: "followup", owner: "local", localOK: true, r1: "local", r2: "local", r3: "local", r2Conf: 0.9, r3Conf: 0.9})
	}
	s = routeData(t, perfect).Route(seed, stubThetas(0.8))
	if s.Gates[0].Verdict != Fail || !strings.Contains(s.Gates[0].Conditions[0].Reason, "nothing to reduce") {
		t.Errorf("no regret to reduce: %+v", s.Gates[0].Conditions[0])
	}
}

func TestClusterCIKeepsPairsTogether(t *testing.T) {
	// Two halves of a pair always count together, so a group's mean cannot be split. A
	// resample drawing only the one group gets its mean, and no other value is possible
	// with a single group.
	groups := map[int64][]float64{7: {0, 1}}
	lo, hi := clusterCI(groups, seed)
	if lo != 0.5 || hi != 0.5 {
		t.Errorf("CI = [%v, %v]", lo, hi)
	}
}

func TestRouteCalibrationPicksTheLowestRegret(t *testing.T) {
	// Choosing local is right, and the classifier is only sure at 0.65.
	c := routeCase{kind: "followup", owner: "cloud", localOK: true, r1: "cloud", r2: "local", r3: "local", r2Conf: 0.65, r3Conf: 0.65}
	var cases []routeCase
	for range 3 {
		cases = append(cases, c)
	}
	s := routeData(t, cases).Route(seed, nil)
	for _, cal := range s.Calibration {
		if cal.Best != 0.6 {
			t.Errorf("%s %s best = %v: %v", cal.Router, cal.World, cal.Best, cal.ByTheta)
		}
	}
}

func TestParseThetas(t *testing.T) {
	got, err := ParseThetas("decisions/R2=0.7, decisions/R3=0.85")
	if err != nil || got["decisions/R2"] != 0.7 || got["decisions/R3"] != 0.85 {
		t.Errorf("got %v, %v", got, err)
	}
	for _, bad := range []string{"decisions", "decisions=high", "decisions=1.5"} {
		if _, err := ParseThetas(bad); err == nil {
			t.Errorf("%q was accepted", bad)
		}
	}
}

func TestMainWritesARouteReport(t *testing.T) {
	var scores []row
	for i, c := range g7Cases(12, true) {
		scores = append(scores, routeRow(i, c))
	}
	run := writeRun(t, scores, []row{ledgerRow("cls:stub", sid(0), "cloud", "classifier", 1000, 0, 0, 0, 0.001)})
	out := t.TempDir()
	var stdout bytes.Buffer
	if err := Main([]string{"--runs", run, "--out", out, "--theta", "stub/R2=0.7,stub/R3=0.7"}, &stdout); err != nil {
		t.Fatal(err)
	}
	md, err := os.ReadFile(filepath.Join(out, "report.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"## S3 routing", "G7 stub", "### Routers", "R3 stub@r5k", "### Threshold calibration", "### Classifiers", "cls:stub", "S3 restates labels"} {
		if !strings.Contains(string(md), want) {
			t.Errorf("report.md lacks %q", want)
		}
	}
	if !strings.Contains(stdout.String(), "G7 stub PASS") {
		t.Errorf("stdout = %s", stdout.String())
	}
	if strings.Contains(string(md), "## Verdicts") {
		t.Error("an S3 report printed the session gates")
	}
}

func TestS3RowsCannotShareAReport(t *testing.T) {
	route := routeRow(0, routeCase{kind: "followup", owner: "cloud", r1: "cloud", r2: "cloud", r3: "cloud"})
	err := mustLoad(t, writeRun(t, append(armRows("A0", 2, constant(0.9)), route), nil)).CheckPooling()
	if err == nil || !strings.Contains(err.Error(), "S3 rows") {
		t.Errorf("with S1: %v", err)
	}
	other := routeRow(1, routeCase{kind: "followup", owner: "cloud", r1: "cloud", r2: "cloud", r3: "cloud"})
	other["set"] = "s3-acceptance"
	err = mustLoad(t, writeRun(t, []row{route, other}, nil)).CheckPooling()
	if err == nil || !strings.Contains(err.Error(), "cannot be pooled") {
		t.Errorf("tuning with acceptance: %v", err)
	}
}

// regretCases gives R1, R2 and R3 a chosen number of wrong routes out of n. Each wrong route
// misses an answer only the cloud has, so each costs the same.
func regretCases(n, r1Wrong, r2Wrong, r3Wrong int) []routeCase {
	side := func(i, wrong int) string {
		if i < wrong {
			return "local"
		}
		return "cloud"
	}
	var out []routeCase
	for i := range n {
		out = append(out, routeCase{
			kind: "cloud_context", owner: "local", cloudOK: true,
			r1: side(i, r1Wrong), r2: side(i, r2Wrong), r3: side(i, r3Wrong),
			r2Conf: 0.9, r3Conf: 0.9,
		})
	}
	return out
}

func TestG7NeedsTwentyPercentAgainstEachComparator(t *testing.T) {
	cases := []struct {
		name       string
		r1, r2, r3 int
		want       Verdict
	}{
		{"both clearly better", 20, 20, 10, Pass},
		{"a tenth better on both", 20, 20, 18, Fail},
		{"better than R2 only", 10, 20, 10, Fail},
		{"better than R1 only", 20, 10, 10, Fail},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := routeData(t, regretCases(40, c.r1, c.r2, c.r3)).Route(seed, stubThetas(0.8))
			if s.Gates[0].Verdict != c.want {
				t.Errorf("verdict %s: %+v", s.Gates[0].Verdict, s.Gates[0].Conditions)
			}
		})
	}
}

func TestG7RequiresTheCIOnAcceptanceOnly(t *testing.T) {
	// The whole gain sits on one seed, so a resample can miss it and the CI reaches 0.
	cases := regretCases(10, 1, 1, 0)
	if s := routeDataOn(t, "s3-tuning", cases).Route(seed, stubThetas(0.8)); s.Gates[0].Verdict != Pass {
		t.Errorf("pilot: %+v", s.Gates[0].Conditions)
	}
	s := routeDataOn(t, setS3Acceptance, cases).Route(seed, stubThetas(0.8))
	if s.Gates[0].Verdict != Fail || !strings.Contains(s.Gates[0].Conditions[0].Reason, "CI reaches 0") {
		t.Errorf("acceptance: %+v", s.Gates[0].Conditions)
	}
	// The CI is printed on both, because a reader of the pilot needs it too.
	s = routeDataOn(t, "s3-tuning", cases).Route(seed, stubThetas(0.8))
	if s.Comparisons[0].CILow == nil || s.Comparisons[0].CIHigh == nil {
		t.Errorf("pilot CI = %+v", s.Comparisons[0])
	}
}

func TestG7JudgesR3OnTheFreshSummary(t *testing.T) {
	good := regretCases(20, 10, 10, 0)
	for i := range good {
		good[i].r3Stale = "local"
	}
	if s := routeData(t, good).Route(seed, stubThetas(0.8)); s.Gates[0].Verdict != Pass {
		t.Errorf("right on r5k, wrong elsewhere: %+v", s.Gates[0].Conditions)
	}
	bad := regretCases(20, 10, 10, 10)
	for i := range bad {
		bad[i].r3Stale = "cloud"
	}
	if s := routeData(t, bad).Route(seed, stubThetas(0.8)); s.Gates[0].Verdict != Fail {
		t.Errorf("wrong on r5k, right elsewhere: %+v", s.Gates[0].Conditions)
	}
}

func TestG7ResamplesSeedsNotItems(t *testing.T) {
	width := func(cases []routeCase) float64 {
		s := routeData(t, cases).Route(seed, stubThetas(0.8))
		return *s.Comparisons[0].CIHigh - *s.Comparisons[0].CILow
	}
	apart := regretCases(20, 10, 10, 5)
	if w := width(apart); w <= 0 {
		t.Errorf("distinct seeds: width %v", w)
	}
	together := regretCases(20, 10, 10, 5)
	for i := range together {
		together[i].seed = 7
	}
	if w := width(together); w != 0 {
		t.Errorf("one seed: width %v", w)
	}
}

func TestThetasAreKeptPerRouter(t *testing.T) {
	// Sure of the right side at 0.65: a threshold of 0.6 lets it through and 0.8 does not.
	c := routeCase{kind: "followup", owner: "cloud", localOK: true, r1: "cloud", r2: "local", r3: "local", r2Conf: 0.65, r3Conf: 0.65}
	var cases []routeCase
	for range 12 {
		cases = append(cases, c)
	}
	s := routeData(t, cases).Route(seed, map[string]float64{"stub/R2": 0.6, "stub/R3": 0.8})
	r2, r3 := rowOf(t, s, "r5k", "R2", ""), rowOf(t, s, "r5k", "R3", "r5k")
	if r2.Regret >= r3.Regret {
		t.Errorf("R2 at 0.6 should beat R3 at 0.8: %v and %v", r2.Regret, r3.Regret)
	}
	if r2.Theta != 0.6 || r3.Theta != 0.8 {
		t.Errorf("thetas %v and %v", r2.Theta, r3.Theta)
	}
}

func TestCheckThetas(t *testing.T) {
	cases := g7Cases(12, true)
	tuning := routeDataOn(t, "s3-tuning", cases)
	if err := tuning.CheckThetas(nil); err != nil {
		t.Errorf("pilot with no thresholds: %v", err)
	}
	if err := tuning.CheckThetas(map[string]float64{"stub": 0.7}); err == nil {
		t.Error("the old whole-classifier key was accepted")
	}
	if err := tuning.CheckThetas(map[string]float64{"typo/R2": 0.7}); err == nil {
		t.Error("an unknown classifier was accepted")
	}
	acceptance := routeDataOn(t, setS3Acceptance, cases)
	if err := acceptance.CheckThetas(map[string]float64{"stub/R2": 0.7}); err == nil || !strings.Contains(err.Error(), "stub/R3") {
		t.Errorf("acceptance missing R3: %v", err)
	}
	if err := acceptance.CheckThetas(stubThetas(0.7)); err != nil {
		t.Errorf("acceptance: %v", err)
	}
}

func TestRouteFailureRateIsPerRouter(t *testing.T) {
	// Failures spread over two routers, neither past 10%, must not add up to a refusal.
	cases := regretCases(20, 10, 10, 0)
	raw := func(c routeCase, i int) row { return routeRow(i, c) }
	var scores []row
	for i, c := range cases {
		r := raw(c, i)
		decisions := r["route"].(row)["decisions"].([]row)
		switch i {
		case 0, 1:
			decisions[0]["error"], decisions[0]["choice"] = "boom", ""
		case 2, 3:
			decisions[1]["error"], decisions[1]["choice"] = "boom", ""
		}
		scores = append(scores, r)
	}
	s := mustLoad(t, writeRun(t, scores, nil)).Route(seed, stubThetas(0.8))
	if s.Gates[0].Verdict == Insufficient {
		t.Errorf("2 failures on each of two routers over 20: %+v", s.Gates[0].Conditions)
	}
	for i := range scores {
		decisions := scores[i]["route"].(row)["decisions"].([]row)
		if i < 3 {
			decisions[1]["error"], decisions[1]["choice"] = "boom", ""
		}
	}
	s = mustLoad(t, writeRun(t, scores, nil)).Route(seed, stubThetas(0.8))
	if s.Gates[0].Verdict != Insufficient {
		t.Errorf("4 failures on one router over 20: %+v", s.Gates[0].Conditions)
	}
}

func TestRouteReportsWhatCrossedALine(t *testing.T) {
	cases := regretCases(12, 6, 6, 0)
	var scores []row
	for i, c := range cases {
		r := routeRow(i, c)
		route := r["route"].(row)
		if i == 0 {
			route["decisions"].([]row)[1]["canary_hits"] = 3
			route["cloud"].(row)["correct"] = false
			route["cloud"].(row)["holds_answer"] = true
			r["canary_hits"] = 2
		}
		scores = append(scores, r)
	}
	s := mustLoad(t, writeRun(t, scores, nil)).Route(seed, stubThetas(0.8))
	if s.Privacy.ClassifierHits != 3 || s.Privacy.CloudHits != 2 {
		t.Errorf("privacy = %+v", s.Privacy)
	}
	if s.Strictness[0].Side != "cloud" || s.Strictness[0].HoldsAnswer != 1 || s.Strictness[0].Wrong != 1 {
		t.Errorf("strictness = %+v", s.Strictness)
	}
	if len(s.Kinds) == 0 || len(s.Kinds[0].Rows) == 0 {
		t.Errorf("kinds = %+v", s.Kinds)
	}
}

func TestPoolingRefusesRunsFromDifferentCode(t *testing.T) {
	write := func(commit string) string {
		dir := writeBareRun(t, armRows("A4", 2, constant(0.9)), nil)
		raw, err := json.Marshal(Manifest{Run: filepath.Base(dir), Set: "pilot", Commit: commit, Budgets: map[string]any{"fork_b_target": 2500}})
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, ManifestFile), raw, 0o644); err != nil {
			t.Fatal(err)
		}
		return dir
	}
	a, b := write("aaa"), write("bbb")
	if err := mustLoad(t, a, b).CheckPooling(); err == nil || !strings.Contains(err.Error(), "different code") {
		t.Errorf("two commits: %v", err)
	}
	if err := mustLoad(t, a, write("aaa")).CheckPooling(); err != nil {
		t.Errorf("one commit: %v", err)
	}
	dirty := mustLoad(t, write("aaa-dirty"))
	if got := dirty.nonPrimary(); len(got) != 1 {
		t.Errorf("a dirty run is primary: %v", got)
	}
	if got := mustLoad(t, write("aaa")).nonPrimary(); len(got) != 0 {
		t.Errorf("a clean run is not primary: %v", got)
	}
}

// The refusal has to be on the path a report really takes.
func TestMainRefusesThresholdsItCannotHonor(t *testing.T) {
	var scores []row
	for i, c := range g7Cases(12, true) {
		r := routeRow(i, c)
		r["set"] = setS3Acceptance
		scores = append(scores, r)
	}
	run := writeRun(t, scores, nil)
	var stdout bytes.Buffer
	err := Main([]string{"--runs", run, "--out", t.TempDir()}, &stdout)
	if err == nil || !strings.Contains(err.Error(), "stub/R2") {
		t.Errorf("acceptance with no thresholds: %v", err)
	}
	if err := Main([]string{"--runs", run, "--out", t.TempDir(), "--theta", "stub/R2=0.7,stub/R3=0.7"}, &stdout); err != nil {
		t.Errorf("acceptance with thresholds: %v", err)
	}
	plain := writeRun(t, armRows("A0", 2, constant(0.9)), []row{})
	if err := Main([]string{"--runs", plain, "--out", t.TempDir(), "--theta", "stub/R2=0.7"}, &stdout); err == nil {
		t.Error("a threshold on a report with no S3 rows was accepted")
	}
}

func TestG7FailureRateCountsEligibleItemsOnly(t *testing.T) {
	var scores []row
	for i, c := range regretCases(20, 10, 10, 0) {
		r := routeRow(i, c)
		if i < 2 {
			r["route"].(row)["decisions"].([]row)[0]["error"] = "boom"
			r["route"].(row)["decisions"].([]row)[0]["choice"] = ""
		}
		scores = append(scores, r)
	}
	// 2 failures over 20 eligible stands. With 20 edge_only items added it is still 2 over 20.
	for i := range 20 {
		c := routeCase{kind: "edge_private", owner: "cloud", localOK: true, r1: "local", edge: true}
		scores = append(scores, routeRow(100+i, c))
	}
	for i := 2; i < 3; i++ {
		scores[i]["route"].(row)["decisions"].([]row)[0]["error"] = "boom"
		scores[i]["route"].(row)["decisions"].([]row)[0]["choice"] = ""
	}
	s := mustLoad(t, writeRun(t, scores, nil)).Route(seed, stubThetas(0.8))
	// 3 over 20 eligible is 15%, past the limit. 3 over the 40 items would be 7.5%, under it.
	if s.Gates[0].Verdict != Insufficient || !strings.Contains(s.Gates[0].Conditions[0].Reason, "over 20 eligible") {
		t.Errorf("%+v", s.Gates[0].Conditions)
	}
}
