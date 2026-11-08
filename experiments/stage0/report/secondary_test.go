package report

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeManifestFile(t *testing.T, dir string, m Manifest) {
	t.Helper()
	raw, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ManifestFile), raw, 0o644); err != nil {
		t.Fatal(err)
	}
}

func setRows(arm, set string, n int, v float64) []row {
	rows := armRows(arm, n, constant(v))
	for _, r := range rows {
		r["set"] = set
	}
	return rows
}

func TestCheckPoolingRefusesWhatCannotBePooled(t *testing.T) {
	cases := []struct {
		name string
		a, b string
		want string
	}{
		{"S2 with S1", "tuning", "s2", "S2 rows"},
		{"S2 acceptance with S1", "tuning", "s2-acceptance", "S2 rows"},
		{"S2 tuning with S2 acceptance", "s2", "s2-acceptance", "cannot be pooled"},
		{"tuning with acceptance", "tuning", "acceptance", "cannot be pooled"},
		{"C tuning with C acceptance", "c-tuning", "c-acceptance", "cannot be pooled"},
		{"S1 with C", "tuning", "c-tuning", ""},
		{"one set", "tuning", "tuning", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			scores := append(setRows("A0", c.a, 3, 0.9), setRows("A3-8K", c.b, 3, 0.5)...)
			err := mustLoad(t, writeRun(t, scores, nil)).CheckPooling()
			if c.want == "" {
				if err != nil {
					t.Errorf("err = %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Errorf("err = %v, want %q", err, c.want)
			}
		})
	}
}

func TestCheckPoolingRefusesDifferentBudgets(t *testing.T) {
	first := writeRun(t, armRows("A0", 3, constant(0.9)), nil)
	second := writeRun(t, armRows("A4", 3, constant(0.9)), nil)
	third := writeRun(t, armRows("T1", 3, constant(0.9)), nil)
	writeManifestFile(t, first, Manifest{Run: "a", Budgets: map[string]any{"fork_b_target": 2500}})
	writeManifestFile(t, second, Manifest{Run: "b", Budgets: map[string]any{"fork_b_target": 2500}})
	writeManifestFile(t, third, Manifest{Run: "c", Budgets: map[string]any{"fork_b_target": 1000}})

	same := mustLoad(t, first, second)
	if err := same.CheckPooling(); err != nil {
		t.Errorf("same budgets: %v", err)
	}
	if err := mustLoad(t, first, second, third).CheckPooling(); err == nil || !strings.Contains(err.Error(), "different budgets") {
		t.Errorf("different budgets: %v", err)
	}
	// Only the rows from before manifests existed can go without one, and the report says so.
	bare := writeBareRun(t, armRows("A3-8K", 3, constant(0.9)), nil)
	d := mustLoad(t, first, bare)
	if err := d.CheckPooling(); err != nil {
		t.Errorf("no manifest on A3-8K: %v", err)
	}
	joined := strings.Join(Build(d, seed).Limits, "\n")
	if !strings.Contains(joined, bare+" has no manifest") {
		t.Errorf("limits do not state the missing manifest: %s", joined)
	}
	for _, arm := range []string{"T1", "A4", "C3"} {
		bare := writeBareRun(t, armRows(arm, 3, constant(0.9)), nil)
		if err := mustLoad(t, first, bare).CheckPooling(); err == nil || !strings.Contains(err.Error(), "no manifest") {
			t.Errorf("%s with no manifest: %v", arm, err)
		}
	}
}

func TestReportMarksBudgetsOtherThanTheFrozenOnes(t *testing.T) {
	old := FrozenBudgets
	t.Cleanup(func() { FrozenBudgets = old })
	FrozenBudgets = map[string]any{"fork_b_target": 2500}
	run := writeRun(t, armRows("T1", 3, constant(0.9)), nil)
	if got := Build(mustLoad(t, run), seed).NonPrimary; len(got) != 0 {
		t.Errorf("the frozen budgets were marked: %v", got)
	}
	FrozenBudgets = map[string]any{"fork_b_target": 1000}
	out := t.TempDir()
	var stdout bytes.Buffer
	if err := Main([]string{"--runs", run, "--out", out}, &stdout); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), "NOT A PRIMARY RESULT") {
		t.Errorf("stdout = %s", stdout.String())
	}
	md, err := os.ReadFile(filepath.Join(out, "report.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(strings.TrimLeft(string(md), "# \n"), "") || !strings.Contains(string(md), "**Not a primary result.**") {
		t.Errorf("report.md does not say so")
	}
}

func TestRetentionWithoutF3CountsEveryOtherCategory(t *testing.T) {
	got, ok := RetentionWithoutF3.Of(Score{ByCategory: map[string][2]int{
		"F1": {1, 2}, "F2": {3, 4}, "F3": {0, 100}, "F4": {5, 6}, "F5": {7, 8}, "F6": {0, 50},
	}})
	if !ok || got != 16.0/20.0 {
		t.Errorf("got %v, %v", got, ok)
	}
	if _, ok := RetentionWithoutF3.Of(Score{ByCategory: map[string][2]int{"F3": {1, 2}}}); ok {
		t.Error("nothing but F3 still gave a value")
	}
}

func TestForkCostIncludesScriptedStepsAndRewrites(t *testing.T) {
	rows := armRows("C3+A", 2, constant(0.5))
	for _, r := range rows {
		r["imputed_main_usd"] = 0.5
	}
	ledger := []row{
		ledgerRow("C3+A", sid(0), "cloud", "main", 1000, 0, 0, 0, 1.0),
		ledgerRow("C3+A", sid(0), "cloud", "fork_b", 1000, 0, 0, 0, 0.3),
		ledgerRow("C3+A", sid(0), "cloud", "fork_a", 1000, 0, 0, 0, 0.2),
		ledgerRow("C3+A", sid(0), "cloud", "shorten", 1000, 0, 0, 0, 0.1),
	}
	c := mustLoad(t, writeRun(t, rows, ledger)).armCost("C3+A")
	if *c.ImputedMainUSD != 1.0 || *c.MainUSD != 1.0 || *c.ForkUSD != 0.6 {
		t.Fatalf("cost = imputed %v, main %v, fork %v", *c.ImputedMainUSD, *c.MainUSD, *c.ForkUSD)
	}
	wantNear(t, "fork to main", c.ForkToMain, 0.3, 1e-12)
}

func TestMainRefusesMixedSets(t *testing.T) {
	run := writeRun(t, append(setRows("A0", "tuning", 3, 0.9), setRows("A3-8K", "s2", 3, 0.5)...), nil)
	var out bytes.Buffer
	err := Main([]string{"--runs", run, "--out", t.TempDir()}, &out)
	if err == nil || !strings.Contains(err.Error(), "S2 rows") {
		t.Errorf("err = %v", err)
	}
}

func s2Row(arm string, i int, score any, outcome row) row {
	r := scoreRow(arm, sid(i), 0)
	r["set"], r["retention"], r["s2"] = "s2", score, outcome
	return r
}

func TestS2ReportJudgesG2AndKeepsScorerFailuresOut(t *testing.T) {
	var scores []row
	for i := range 12 {
		a3 := s2Row("A3-8K", i, 0.5, row{"passed": 1, "total": 2})
		if i == 0 {
			a3 = s2Row("A3-8K", i, 0.0, row{"passed": 0, "total": 2, "build_failed": true})
		}
		t1 := s2Row("T1", i, 0.75, row{"passed": 3, "total": 4})
		if i == 1 {
			t1 = s2Row("T1", i, nil, row{"infra": "go: not found"})
		}
		scores = append(scores,
			s2Row("A0", i, 1.0, row{"passed": 2, "total": 2}),
			a3, t1,
			s2Row("A4", i, 0.5, row{"passed": 2, "total": 4}),
			s2Row("A4s", i, 0.5, row{"passed": 2, "total": 4}),
		)
	}
	run := writeRun(t, scores, nil)
	out := t.TempDir()
	var stdout bytes.Buffer
	if err := Main([]string{"--runs", run, "--out", out}, &stdout); err != nil {
		t.Fatal(err)
	}
	md, err := os.ReadFile(filepath.Join(out, "report.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"## Verdicts", "## S2 by arm", "A0 - A3-8K", "T1 - A4s", "task_score", "all_pass", "Scorer failed", "pilot verdict", "paired tasks"} {
		if !strings.Contains(string(md), want) {
			t.Errorf("report.md lacks %q", want)
		}
	}
	if !strings.Contains(stdout.String(), "G2 PASS") {
		t.Errorf("stdout: %s", stdout.String())
	}
	if strings.Contains(stdout.String(), "G1 ") || strings.Contains(stdout.String(), "G6 ") {
		t.Errorf("an S2 report decided a gate that S1 and CBench decide: %s", stdout.String())
	}
	d := mustLoad(t, run)
	s := d.S2(seed)
	byArm := map[string]S2ArmRow{}
	for _, a := range s.Arms {
		byArm[a.Arm] = a
	}
	if a := byArm["T1"]; a.Tasks != 12 || a.Scored != 11 || a.ScorerFailed != 1 {
		t.Errorf("T1 = %+v", a)
	}
	if a := byArm["A3-8K"]; a.BuildFailed != 1 || a.ScorerFailed != 0 {
		t.Errorf("A3-8K = %+v", a)
	}
	wantNear(t, "T1 mean leaves the scorer failure out", byArm["T1"].Mean, 0.75, 1e-12)
	t1a4 := *d.G2(seed).Conditions[0].Paired
	if t1a4.N != 11 || len(t1a4.MetricMissingA) != 1 {
		t.Errorf("T1 - A4 = %+v", t1a4)
	}
}

func TestSecondaryAnalyses(t *testing.T) {
	withCats := func(arm string, v float64, f3 [2]int) []row {
		rows := armRows(arm, 12, constant(v))
		for _, r := range rows {
			r["by_category"] = row{"F1": []int{8, 10}, "F3": []int{f3[0], f3[1]}, "F5": []int{1, 2}}
			r["by_recipient"] = row{"local": row{"F3": []int{f3[0], f3[1]}}, "cloud": row{"F1": []int{8, 10}}}
			r["counters"] = row{"compress": 2, "briefs_compressed": 1}
		}
		return rows
	}
	tuned := scoreRow("C3", TuningSessions[0], 0.0)
	base := scoreRow("C0", TuningSessions[0], 1.0)
	scores := append(append(withCats("C3", 0.8, [2]int{5, 10}), withCats("C0", 0.7, [2]int{0, 10})...), tuned, base)
	d := mustLoad(t, writeRun(t, scores, nil))
	s := d.Secondary(seed)

	noF3 := s.WithoutF3[0]
	if noF3.Label() != "C3 - C0" || noF3.Metric != RetentionWithoutF3.Name {
		t.Errorf("row = %s on %s", noF3.Label(), noF3.Metric)
	}
	// F3 differs and nothing else does, so without it the two arms are the same.
	if noF3.N != 13 || *noF3.Mean != 0 {
		t.Errorf("without F3: n %d mean %v, missing %v", noF3.N, *noF3.Mean, noF3.MetricMissingA)
	}

	// The tuning session is the only pair going the other way.
	all := d.Paired("C3", "C0", Retention, seed)
	var rest PairedResult
	for _, p := range s.ExcludingTuning {
		if p.Label() == "C3 - C0" {
			rest = p
		}
	}
	if all.N != 13 || rest.N != 12 {
		t.Fatalf("pairs: all %d, without tuning %d", all.N, rest.N)
	}
	wantNear(t, "C3 - C0 without tuning", rest.Mean, 0.1, 1e-12)

	found := false
	for _, x := range s.ByRecipient {
		if x.Arm == "C3" && x.Recipient == "local" && x.Category == "F3" {
			found = true
			if x.Correct != 60 || x.Total != 120 {
				t.Errorf("C3 local F3 = %+v", x)
			}
			wantNear(t, "rate", x.Rate, 0.5, 1e-12)
		}
	}
	if !found {
		t.Error("no row for C3 local F3")
	}
	for _, c := range s.Counters {
		if c.Arm == "C0" && c.Totals["briefs_compressed"] != 12 {
			t.Errorf("C0 counters = %+v", c.Totals)
		}
	}
}

func TestSummaryUSDCountsEveryRequestThatWritesASummary(t *testing.T) {
	ledger := []row{
		ledgerRow("C0", sid(0), "cloud", "compress", 1000, 0, 0, 100, 0.5),
		ledgerRow("C7", sid(0), "cloud", "small_summary", 1000, 0, 0, 100, 0.25),
		ledgerRow("C3", sid(0), "cloud", "fork_b", 1000, 900, 0, 100, 0.125),
		ledgerRow("C3", sid(0), "cloud", "main", 1000, 900, 0, 100, 1),
	}
	d := mustLoad(t, writeRun(t, armRows("C3", 1, constant(0.5)), ledger))
	wantNear(t, "C0", d.armCost("C0").SummaryUSD, 0.5, 1e-12)
	wantNear(t, "C7", d.armCost("C7").SummaryUSD, 0.25, 1e-12)
	wantNear(t, "C3", d.armCost("C3").SummaryUSD, 0.125, 1e-12)
	if got := d.armCost("C0").ForkUSD; got == nil || *got != 0 {
		t.Errorf("C0 prefix fork USD = %v", got)
	}
}

// Repeats of a task share it, so they make one pair, and the CI is the CI of the task means.
func TestRepeatsOfATaskAreOneUnit(t *testing.T) {
	var repeated, means []row
	for i := range 12 {
		spread := 0.1 * float64(i%3)
		a4 := s2Row("A4", i, 0.5, row{"passed": 1, "total": 2})
		means = append(means, a4, s2Row("T1", i, 0.6+spread, row{"passed": 1, "total": 2}))
		for r, v := range []float64{0.5 + spread, 0.6 + spread, 0.7 + spread} {
			id := sid(i)
			if r > 0 {
				id = fmt.Sprintf("%s~r%d", id, r+1)
			}
			x := s2Row("T1", i, v, row{"passed": 1, "total": 2})
			x["session"] = id
			y := s2Row("A4", i, 0.5, row{"passed": 1, "total": 2})
			y["session"] = id
			repeated = append(repeated, x, y)
		}
	}
	got := loadScores(t, repeated).Paired("T1", "A4", TaskScore, seed)
	want := loadScores(t, means).Paired("T1", "A4", TaskScore, seed)
	if got.N != 12 || got.Rows != 36 || want.Rows != 12 {
		t.Fatalf("n %d, rows %d and %d", got.N, got.Rows, want.Rows)
	}
	wantNear(t, "mean", got.Mean, *want.Mean, 1e-12)
	wantNear(t, "sd", got.SD, *want.SD, 1e-12)
	wantNear(t, "ci low", got.CILow, *want.CILow, 1e-12)
	wantNear(t, "ci high", got.CIHigh, *want.CIHigh, 1e-12)

	d := loadScores(t, repeated)
	byArm := map[string]S2ArmRow{}
	for _, a := range d.S2(seed).Arms {
		byArm[a.Arm] = a
	}
	if a := byArm["T1"]; a.Tasks != 12 || a.Runs != 36 {
		t.Errorf("T1 = %+v", a)
	}
}

func TestTaskMetricsReadOnlyWhatTheScorerSaid(t *testing.T) {
	one := 0.5
	for _, c := range []struct {
		name      string
		s         Score
		score, ok bool
		whole     float64
	}{
		{"a session row", Score{Retention: &one}, false, false, 0},
		{"a scorer failure", Score{Retention: &one, S2: &S2Outcome{Infra: "go: not found"}}, false, false, 0},
		{"some tests", Score{Retention: &one, S2: &S2Outcome{Passed: 1, Total: 2}}, true, true, 0},
		{"all tests", Score{Retention: &one, S2: &S2Outcome{Passed: 2, Total: 2}}, true, true, 1},
		{"no tests ran", Score{Retention: &one, S2: &S2Outcome{}}, true, true, 0},
		{"a build failure", Score{Retention: &one, S2: &S2Outcome{Passed: 2, Total: 2, BuildFailed: true}}, true, true, 0},
	} {
		t.Run(c.name, func(t *testing.T) {
			if _, ok := TaskScore.Of(c.s); ok != c.score {
				t.Errorf("task score present = %v", ok)
			}
			v, ok := AllPass.Of(c.s)
			if ok != c.ok || v != c.whole {
				t.Errorf("all pass = %v, %v", v, ok)
			}
		})
	}
}

func TestTaskSampleSizeCountsTasks(t *testing.T) {
	d := loadScores(t, taskRows("T1", 20, alternating(0.5, 0.1)), taskRows("A4", 20, constant(0.5)), taskRows("A4s", 20, constant(0.5)))
	s := d.TaskSampleSize()
	if !s.L4Complete || *s.L4Low != 34 || !strings.Contains(s.Statement, "34 paired tasks") {
		t.Errorf("%+v: %s", s, s.Statement)
	}
}

func TestG2ConditionsReadTheTaskScore(t *testing.T) {
	d := loadScores(t, taskRows("T1", 12, constant(0.9)), taskRows("T1+read", 12, constant(0.9)), taskRows("A4", 12, constant(0.8)), taskRows("A4s", 12, constant(0.8)), taskRows("A4+read", 12, constant(0.8)))
	for _, g := range []GateResult{d.G2(seed), d.G2Read(seed)} {
		for _, c := range g.Conditions {
			if c.Paired == nil || c.Paired.Metric != TaskScore.Name {
				t.Errorf("%s: %+v", c.Name, c.Paired)
			}
		}
	}
}

func TestNonPrimaryNamesUncommittedAndUnknownCode(t *testing.T) {
	d := &Data{Manifests: []Manifest{{Run: "a", Commit: "abc"}, {Run: "b", Commit: "abc-dirty"}, {Run: "c", Commit: "unknown"}}}
	if got := fmt.Sprint(d.nonPrimary()); got != "[b c]" {
		t.Errorf("non primary = %s", got)
	}
}

func withSet(set string, groups ...[]row) []row {
	var out []row
	for _, g := range groups {
		for _, r := range g {
			r["set"] = set
			out = append(out, r)
		}
	}
	return out
}

func TestS2AcceptanceRowsAreJudgedAndNotCalledAPilot(t *testing.T) {
	arms := func() [][]row {
		return [][]row{taskRows("T1", 12, constant(0.9)), taskRows("A4", 12, constant(0.8)), taskRows("A4s", 12, constant(0.8))}
	}
	render := func(rows []row) (string, *Report) {
		r := Build(mustLoad(t, writeRun(t, rows, nil)), seed)
		var b bytes.Buffer
		if err := Markdown(&b, r); err != nil {
			t.Fatal(err)
		}
		return b.String(), r
	}
	md, r := render(withSet("s2-acceptance", arms()...))
	if r.G2 == nil || r.G2.Verdict != Pass {
		t.Fatalf("G2 = %+v", r.G2)
	}
	if strings.Contains(md, "pilot verdict") {
		t.Error("an acceptance report is called a pilot")
	}
	if !strings.Contains(md, "task level") || strings.Contains(md, "session level") {
		t.Error("the bootstrap line names the wrong unit")
	}
	for _, want := range []string{"two tries at one gate", "acceptance runs only the form that passed", "event_reads", "If T1+read is the form that passed"} {
		if !strings.Contains(md, want) {
			t.Errorf("report.md lacks %q", want)
		}
	}
	if md, _ := render(withSet("s2", arms()...)); !strings.Contains(md, "pilot verdict") {
		t.Error("a tuning report is not called a pilot")
	}
}

// A task with more repeats does not count for more in an arm's mean.
func TestArmMeanGivesEachTaskOneVote(t *testing.T) {
	var rows []row
	for i := range 2 {
		x := s2Row("T1", i, 1.0, row{"passed": 1, "total": 1})
		rows = append(rows, x)
	}
	for r := 2; r <= 4; r++ {
		x := s2Row("T1", 0, 1.0, row{"passed": 1, "total": 1})
		x["session"] = fmt.Sprintf("%s~r%d", sid(0), r)
		rows = append(rows, x)
	}
	y := s2Row("T1", 2, 0.0, row{"passed": 0, "total": 1})
	rows = append(rows, y)
	s := mustLoad(t, writeRun(t, rows, nil)).S2(seed)
	wantNear(t, "mean", s.Arms[0].Mean, 2.0/3, 1e-12)
	wantNear(t, "all pass", s.Arms[0].AllPass, 2.0/3, 1e-12)
	if s.Arms[0].Tasks != 3 || s.Arms[0].Runs != 6 {
		t.Errorf("%+v", s.Arms[0])
	}
}

func TestRepeatedPairsShowTheirRows(t *testing.T) {
	if got := nText(PairedResult{N: 12, Rows: 36}); !strings.Contains(got, "36 rows") {
		t.Errorf("n = %q", got)
	}
	if got := nText(PairedResult{N: 12, Rows: 12}); got != "12" {
		t.Errorf("n = %q", got)
	}
}

func TestReadFormSetsItsOwnSize(t *testing.T) {
	controls := []string{"A4", "A4s", "A4+read"}
	for _, wide := range controls {
		var rows [][]row
		rows = append(rows, taskRows("T1+read", 20, constant(0.5)))
		for _, arm := range controls {
			f := constant(0.5)
			if arm == wide {
				f = alternating(0.5, 0.3)
			}
			rows = append(rows, taskRows(arm, 20, f))
		}
		s := loadScores(t, rows...).ReadSampleSize()
		if !s.L4Complete || s.L4Low == nil || *s.L4Low <= 100 || !strings.Contains(s.Statement, "If T1+read") || !strings.Contains(s.Statement, "T1+read - "+wide) {
			t.Errorf("%s: %+v: %s", wide, s, s.Statement)
		}
	}
}
