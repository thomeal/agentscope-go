package report

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// fullRun has every arm the report names, with T1 clean and the gates passing.
func fullRun(t *testing.T) string {
	t.Helper()
	values := map[string]float64{
		"A0": 0.9, "A3-8K": 0.6, "A3-32K": 0.8, "A6": 0.7,
		"T1": 0.8, "A4": 0.7, "A4s": 0.7, "T1+brief": 0.82, "A4-abrupt": 0.65, "A3-8K-h": 0.62,
		"C0": 0.6, "C0h": 0.65, "C3": 0.8, "C3+A": 0.8, "C4": 0.78, "C6": 0.81, "C7": 0.79, "C8": 0.82,
	}
	var scores, ledger []row
	for arm, v := range values {
		// Noise that differs by arm, so SDs and CIs of the differences are not degenerate.
		phase := 0
		for _, c := range arm {
			phase += int(c)
		}
		for i := range 12 {
			scores = append(scores, scoreRow(arm, sid(i), v+float64((i+phase)%4)*0.004+0.123456789e-3))
			ledger = append(ledger, ledgerRow(arm, sid(i), "cloud", "main", 1000, 800, 0, 100, 0.01))
			ledger = append(ledger, ledgerRow(arm, sid(i), "cloud", "fork_b", 1000, 900, 0, 100, 0.002))
			ledger = append(ledger, ledgerRow(arm, sid(i), "local", "main", 500, 0, 0, 50, 0))
		}
	}
	return writeRun(t, scores, ledger)
}

func TestMainWritesBothFilesWithTheSameNumbers(t *testing.T) {
	run := fullRun(t)
	out := filepath.Join(t.TempDir(), "nested", "out")
	var stdout bytes.Buffer
	if err := Main([]string{"--runs", run, "--seed", "7", "--out", out}, &stdout); err != nil {
		t.Fatal(err)
	}
	md, err := os.ReadFile(filepath.Join(out, "report.md"))
	if err != nil {
		t.Fatal(err)
	}
	js, err := os.ReadFile(filepath.Join(out, "report.json"))
	if err != nil {
		t.Fatal(err)
	}

	var r Report
	if err := json.Unmarshal(js, &r); err != nil {
		t.Fatal(err)
	}
	if r.Seed != 7 || r.Resamples != Resamples {
		t.Errorf("seed %d resamples %d", r.Seed, r.Resamples)
	}
	if r.G2 != nil {
		t.Errorf("a report of S1 and C rows judged G2: %+v", r.G2)
	}
	for _, g := range []GateResult{r.G1, r.G6} {
		if g.Verdict != Pass {
			t.Errorf("%s = %s: %s", g.Gate, g.Verdict, g.Reason)
		}
	}
	if r.Privacy.Verdict != Pass {
		t.Errorf("privacy = %s: %s", r.Privacy.Verdict, r.Privacy.Reason)
	}

	// The JSON carries the same numbers as Build, without loss.
	reencoded := &bytes.Buffer{}
	if err := JSON(reencoded, &r); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(reencoded.Bytes(), js) {
		t.Error("report.json does not round trip through Report")
	}

	text := string(md)
	for _, want := range []string{
		"| G1 | PASS |", "| G6 | PASS |", "### T1 on facts", "| Privacy | PASS |",
		"## Data", "## Arms", "## Cost", "## Privacy", "## Sample size", "## Limits",
		"C7 tests only the reuse of the prefix",
		"A0 - A3-32K", "A0 - A3-8K-h", "T1 - A4s", "T1+brief - A4", "T1 - A4-abrupt", "C4 - C3", "C7 - C3", "C3 - C0h",
		"## Secondary analyses", "T1+brief - T1", "retention_without_f3", "Without the sessions used for tuning",
		"Retention by answering side and category", "briefs_compressed", "Latency P95 ms",
		"both record tools", "T1 must beat both",
		"Spend and cache by role", "Wait P95 ms", "Usage missing", "seed 7",
		r.SampleSize.Statement,
	} {
		if !strings.Contains(text, want) {
			t.Errorf("report.md lacks %q", want)
		}
	}

	// Display rounds the value that the JSON keeps in full.
	mean := *r.G1.Conditions[0].Paired.Mean
	if !strings.Contains(text, strconv.FormatFloat(mean, 'f', 4, 64)) {
		t.Errorf("report.md lacks the rounded G1 mean %v", mean)
	}
	full := strconv.FormatFloat(*r.Arms[0].RetentionMean, 'f', -1, 64)
	if !strings.Contains(string(js), full) {
		t.Errorf("report.json lacks full precision value %s", full)
	}
	if strings.Contains(text, full) {
		t.Errorf("report.md should round %s", full)
	}

	if !strings.Contains(stdout.String(), "G1 PASS") || !strings.Contains(stdout.String(), "report.json") {
		t.Errorf("stdout: %s", stdout.String())
	}
}

func TestMainIsByteDeterministic(t *testing.T) {
	run := fullRun(t)
	first, second := t.TempDir(), t.TempDir()
	for _, out := range []string{first, second} {
		var stdout bytes.Buffer
		if err := Main([]string{"--runs", run, "--seed", "3", "--out", out}, &stdout); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"report.md", "report.json"} {
		a, err := os.ReadFile(filepath.Join(first, name))
		if err != nil {
			t.Fatal(err)
		}
		b, err := os.ReadFile(filepath.Join(second, name))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(a, b) {
			t.Errorf("%s differs between identical runs", name)
		}
	}
}

func TestMainAcceptsSeveralRunsAndReplaces(t *testing.T) {
	a := writeRun(t, []row{scoreRow("T1", sid(0), 0.1)}, []row{})
	b := writeRun(t, []row{scoreRow("T1", sid(0), 0.2)}, []row{})
	out := t.TempDir()
	var stdout bytes.Buffer
	if err := Main([]string{"--runs", a + "," + b, "--out", out}, &stdout); err != nil {
		t.Fatal(err)
	}
	js, err := os.ReadFile(filepath.Join(out, "report.json"))
	if err != nil {
		t.Fatal(err)
	}
	var r Report
	if err := json.Unmarshal(js, &r); err != nil {
		t.Fatal(err)
	}
	if r.Load.ScoreRowsReplaced != 1 || len(r.RunDirs) != 2 {
		t.Errorf("replaced %d dirs %v", r.Load.ScoreRowsReplaced, r.RunDirs)
	}
	wantNear(t, "T1 retention", r.Arms[0].RetentionMean, 0.2, 1e-12)
}

func TestMainRejectsBadArguments(t *testing.T) {
	run := writeRun(t, []row{}, []row{})
	cases := map[string][]string{
		"no runs":        {"--out", t.TempDir()},
		"empty runs":     {"--runs", " , ", "--out", t.TempDir()},
		"no out":         {"--runs", run},
		"absent run":     {"--runs", filepath.Join(run, "typo"), "--out", t.TempDir()},
		"bad seed":       {"--runs", run, "--out", t.TempDir(), "--seed", "x"},
		"stray argument": {"--runs", run, "--out", t.TempDir(), "extra"},
	}
	for name, args := range cases {
		var stdout bytes.Buffer
		if err := Main(args, &stdout); err == nil {
			t.Errorf("%s: Main succeeded", name)
		}
	}
}

func TestMarkdownStatesGapsInsteadOfHidingThem(t *testing.T) {
	missingOwn := scoreRow("T1", sid(0), 0.5)
	delete(missingOwn, "own_retention")
	dir := writeRun(t, []row{missingOwn, scoreRow("A4", sid(1), 0.5)}, nil)
	f, err := os.OpenFile(filepath.Join(dir, scoresFile), os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("not json | at all\n"); err != nil {
		t.Fatal(err)
	}
	f.Close()

	var b bytes.Buffer
	if err := Markdown(&b, Build(mustLoad(t, dir), seed)); err != nil {
		t.Fatal(err)
	}
	text := b.String()
	for _, want := range []string{
		"| G1 | INSUFFICIENT |", "| G6 | INSUFFICIENT |", "| Privacy | INSUFFICIENT |",
		"- Missing file:", "- Malformed:", "n/a",
		"T1 - A4, missing from T1: 1: s001",
		"T1 - A4, missing from A4: 1: s000",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("report.md lacks %q", want)
		}
	}
}

func TestMarkdownCapsSessionListsAndStatesTheRest(t *testing.T) {
	d := loadScores(t, armRows("T1", maxListedSessions+3, constant(0.5)))
	var b bytes.Buffer
	if err := Markdown(&b, Build(d, seed)); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(b.String(), "and 3 more") {
		t.Error("a capped list should state how many it left out")
	}
	// JSON keeps every id.
	var js bytes.Buffer
	if err := JSON(&js, Build(d, seed)); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(js.String(), sid(maxListedSessions+2)) {
		t.Error("report.json should list every session")
	}
}

func TestPrivacyFailureReachesBothOutputs(t *testing.T) {
	leak := scoreRow("T1", sid(0), 0.5)
	leak["canary_hits"] = 1
	r := Build(mustLoad(t, writeRun(t, []row{leak}, []row{ledgerRow("T1", sid(0), "cloud", "main", 1, 0, 0, 1, 0)})), seed)
	var md, js bytes.Buffer
	if err := Markdown(&md, r); err != nil {
		t.Fatal(err)
	}
	if err := JSON(&js, r); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(md.String(), "## Privacy: FAIL") {
		t.Error("report.md should say the privacy section failed")
	}
	if !strings.Contains(js.String(), `"verdict": "FAIL"`) {
		t.Error("report.json should say the privacy section failed")
	}
}
