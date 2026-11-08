package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/agentscope-ai/agentscope-go/v2/experiments/stage0/arms"
	"github.com/agentscope-ai/agentscope-go/v2/experiments/stage0/corpus"
	"github.com/agentscope-ai/agentscope-go/v2/experiments/stage0/report"
	"github.com/agentscope-ai/agentscope-go/v2/experiments/stage0/s2"
)

const testKey = "sk-test-do-not-write-me-123"

func TestTuningSessionsAreTheOnesUsedForTuning(t *testing.T) {
	want := []string{}
	for _, s := range corpus.Pilot()[:5] {
		want = append(want, s.ID)
	}
	for _, b := range corpus.CBenchPilot()[:2] {
		want = append(want, b.ID)
	}
	if !slices.Equal(report.TuningSessions, want) {
		t.Errorf("report.TuningSessions = %v, want %v", report.TuningSessions, want)
	}
}

func TestRouteRowsReadBackIntoTheReport(t *testing.T) {
	yes := true
	want := arms.Row{Arm: arms.ArmRoute, Session: "s3-tuning-00000", Set: "s3-tuning", Route: &arms.RouteOutcome{
		Kind: "followup", PairID: "p1", Seed: 9, Owner: "cloud", Disclosure: "shared", CloudEligible: yes,
		Cloud:  arms.SideResult{Correct: false, Reply: "A", Error: "e", HoldsAnswer: true},
		Worlds: map[string]*arms.WorldResult{arms.WorldFresh: {Local: arms.SideResult{Correct: true, Reply: "B", HoldsAnswer: true}, HasSummary: true, SummaryChars: 3, BehindTokens: 4, Imported: 5, Dropped: 6}},
		Rules:  map[string]string{arms.RouterOwner: "cloud"},
		Decisions: []arms.RouteDecision{{Router: arms.RouterState, World: arms.WorldFresh, Classifier: "c", Choice: "local", Confidence: 0.5,
			Probabilities: map[string]float64{"local": 0.5}, LatencyMS: 7, StateBytes: 8, InputTokens: 9, CanaryHits: 2, Error: "f"}},
	}}
	raw, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	var got report.Score
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got.Route == nil || got.Retention != nil {
		t.Fatalf("score = %+v", got)
	}
	// Written back, a mirror that lost or renamed a field would show as a difference.
	back, err := json.Marshal(got.Route)
	if err != nil {
		t.Fatal(err)
	}
	orig, err := json.Marshal(want.Route)
	if err != nil {
		t.Fatal(err)
	}
	if string(back) != string(orig) {
		t.Errorf("the report's mirror differs:\n%s\n%s", back, orig)
	}
}

func TestResumeRefusesOtherCode(t *testing.T) {
	m := report.Manifest{Run: "r", Commit: "aaa", Budgets: map[string]any{"x": 1}}
	if err := checkResume(m, m, "path"); err != nil {
		t.Errorf("same run: %v", err)
	}
	other := m
	other.Commit = "bbb"
	if err := checkResume(m, other, "path"); err == nil || !strings.Contains(err.Error(), "commit") {
		t.Errorf("other commit: %v", err)
	}
	other = m
	other.Budgets = map[string]any{"x": 2}
	if err := checkResume(m, other, "path"); err == nil || !strings.Contains(err.Error(), "budgets") {
		t.Errorf("other budgets: %v", err)
	}
}

func TestManifestHoldsTheBudgetsAndARunKeepsThem(t *testing.T) {
	dir := t.TempDir()
	if err := writeManifest(dir, "r", "pilot"); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, report.ManifestFile))
	if err != nil {
		t.Fatal(err)
	}
	var m report.Manifest
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	if m.Run != "r" || m.Set != "pilot" || m.Budgets["fork_b_target"] != float64(2500) {
		t.Errorf("manifest = %+v", m)
	}
	// The same budgets may resume.
	if err := writeManifest(dir, "r", "pilot"); err != nil {
		t.Fatal(err)
	}
	// Others may not, because one run would then hold two conditions.
	arms.SetVariant(arms.Variant{Refresh: 20000, Target: 1000})
	defer arms.SetVariant(arms.Variant{Refresh: 5000, Target: 2500})
	if err := writeManifest(dir, "r", "pilot"); err == nil || !strings.Contains(err.Error(), "other budgets") {
		t.Errorf("err = %v", err)
	}
}

// nullServer is the smallest provider that lets a session finish: empty replies on
// both APIs. arms has the full fake; this one only proves the CLI's wiring.
func nullServer(t *testing.T, window int) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "application/json")
		var body map[string]any
		switch r.URL.Path {
		case "/v1/models":
			body = map[string]any{"data": []any{map[string]any{"id": "qwen", "max_model_len": window}}}
		case "/tokenize":
			body = map[string]any{"count": 10}
		case "/v1/responses":
			body = map[string]any{
				"status": "completed",
				"output": []any{map[string]any{"type": "message", "role": "assistant", "content": []any{map[string]any{"type": "output_text", "text": ""}}}},
				"usage":  map[string]any{"input_tokens": 1000, "output_tokens": 10},
			}
		case "/v1/chat/completions":
			body = map[string]any{
				"choices": []any{map[string]any{"index": 0, "message": map[string]any{"role": "assistant", "content": ""}, "finish_reason": "stop"}},
				"usage":   map[string]any{"prompt_tokens": 100, "completion_tokens": 1},
			}
		default:
			http.NotFound(w, r)
			return
		}
		if err := json.NewEncoder(w).Encode(body); err != nil {
			t.Error(err)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

type cliFixture struct {
	dir, env, url string
}

func newCLI(t *testing.T, window int) cliFixture {
	t.Helper()
	dir := t.TempDir()
	env := filepath.Join(dir, ".env")
	if err := os.WriteFile(env, []byte("OPENAI_API_KEY="+testKey+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return cliFixture{dir: dir, env: env, url: nullServer(t, window).URL}
}

func (c cliFixture) args(extra ...string) []string {
	base := []string{
		"run", "--dir", filepath.Join(c.dir, "runs"), "--env", c.env,
		"--local-url", c.url, "--cloud-url", c.url, "--local-model", "qwen",
	}
	return append(base, extra...)
}

func TestRunWritesRowsResumesAndReports(t *testing.T) {
	c := newCLI(t, 8192)
	args := c.args("--run", "pilot1", "--arms", "A0,A3-8K,T1", "--set", "pilot", "--limit-sessions", "1",
		"--workers", "2", "--budget", "5", "--spent", "0")
	var out, errb bytes.Buffer
	if code := realMain(args, &out, &errb); code != 0 {
		t.Fatalf("exit %d: %s\n%s", code, errb.String(), out.String())
	}
	dir := filepath.Join(c.dir, "runs", "pilot1")
	scores, err := os.ReadFile(filepath.Join(dir, "scores.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(string(scores), "\n"); got != 5 {
		t.Errorf("%d rows, want three arms, T1+brief and T1+read:\n%s", got, scores)
	}
	for _, arm := range []string{"A0 ", "A3-8K ", "T1 ", "T1+brief ", "T1+read "} {
		if !strings.Contains(out.String(), arm) {
			t.Errorf("no progress line for %s:\n%s", arm, out.String())
		}
	}
	if strings.Contains(out.String(), "ERROR") {
		t.Errorf("unexpected error line:\n%s", out.String())
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		raw, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(raw, []byte(testKey)) {
			t.Errorf("%s holds the API key", e.Name())
		}
	}

	// The spend so far has to be declared, so a restart cannot forget it.
	out.Reset()
	if code := realMain(args, &out, &errb); code != 1 || !strings.Contains(errb.String(), "ledger already holds") {
		t.Errorf("restart with --spent 0: exit %d, %s", code, errb.String())
	}
	args[len(args)-1] = "1"
	out.Reset()
	errb.Reset()
	if code := realMain(args, &out, &errb); code != 0 {
		t.Fatalf("resume exit %d: %s", code, errb.String())
	}
	if !strings.Contains(out.String(), "0 finished, 3 skipped") {
		t.Errorf("resume output:\n%s", out.String())
	}

	reportDir := filepath.Join(c.dir, "report")
	out.Reset()
	errb.Reset()
	if code := realMain([]string{"report", "--runs", dir, "--out", reportDir}, &out, &errb); code != 0 {
		t.Fatalf("report exit %d: %s", code, errb.String())
	}
	if entries, err := os.ReadDir(reportDir); err != nil || len(entries) == 0 {
		t.Errorf("report wrote %d files, %v", len(entries), err)
	}
}

func TestBudgetStopExitsNonZero(t *testing.T) {
	c := newCLI(t, 8192)
	args := c.args("--run", "stop", "--arms", "A0", "--set", "pilot", "--limit-sessions", "2",
		"--budget", "0.00001", "--spent", "0.00001")
	var out, errb bytes.Buffer
	if code := realMain(args, &out, &errb); code != 3 {
		t.Fatalf("exit %d: %s\n%s", code, errb.String(), out.String())
	}
	if !strings.Contains(out.String(), "ERROR budget stop") {
		t.Errorf("output:\n%s", out.String())
	}
}

func TestRunRefusesBadConfigurations(t *testing.T) {
	c := newCLI(t, 8192)
	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{"no budget", []string{"--run", "x", "--arms", "A0", "--set", "pilot", "--spent", "0"}, "--budget"},
		{"secondary arm", []string{"--run", "x", "--arms", "T1+brief", "--set", "pilot", "--budget", "1"}, "cannot run"},
		{"unknown arm", []string{"--run", "x", "--arms", "A9", "--set", "pilot", "--budget", "1"}, "cannot run"},
		{"bench arm on sessions", []string{"--run", "x", "--arms", "C3", "--set", "pilot", "--budget", "1"}, "cannot run"},
		{"session arm on bench", []string{"--run", "x", "--arms", "T1", "--set", "cpilot", "--budget", "1"}, "cannot run"},
		{"unknown set", []string{"--run", "x", "--arms", "A0", "--set", "nope", "--budget", "1"}, "unknown set"},
		{"acceptance without n", []string{"--run", "x", "--arms", "A0", "--set", "acceptance", "--budget", "1"}, "--n"},
		{"window mismatch", []string{"--run", "x", "--arms", "A3-32K", "--set", "pilot", "--budget", "1"}, "max_model_len"},
		{"two windows", []string{"--run", "x", "--arms", "A3-8K,A3-32K", "--set", "pilot", "--budget", "1"}, "max_model_len"},
		{"read arm on sessions", []string{"--run", "x", "--arms", "T1+read", "--set", "pilot", "--budget", "1"}, "cannot run"},
		{"abrupt arm on sessions", []string{"--run", "x", "--arms", "A4-abrupt", "--set", "pilot", "--budget", "1"}, "cannot run"},
		{"repeat on sessions", []string{"--run", "x", "--arms", "A0", "--set", "pilot", "--repeat", "2", "--budget", "1"}, "--repeat"},
		{"no repeats", []string{"--run", "x", "--arms", "A0", "--set", "s2", "--repeat", "0", "--budget", "1"}, "--repeat"},
		{"test rule on sessions", []string{"--run", "x", "--arms", "A0", "--set", "pilot", "--task-test-rule", "--budget", "1"}, "--task-test-rule"},
		{"routed thread without routes", []string{"--run", "x", "--arms", "MT-routed", "--set", "s2acceptance", "--budget", "1"}, "MT-routed needs --routes"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out, errb bytes.Buffer
			if code := realMain(c.args(tc.args...), &out, &errb); code == 0 || !strings.Contains(errb.String(), tc.want) {
				t.Errorf("exit %d: %s", code, errb.String())
			}
			if _, err := os.Stat(filepath.Join(c.dir, "runs", "x")); err == nil {
				t.Error("a refused run created its directory")
			}
		})
	}
}

func TestMissingKeyIsRefusedWithoutLeaks(t *testing.T) {
	c := newCLI(t, 8192)
	if err := os.WriteFile(c.env, []byte("OTHER=sk-other-value\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var out, errb bytes.Buffer
	code := realMain(c.args("--run", "x", "--arms", "A0", "--set", "pilot", "--budget", "1"), &out, &errb)
	if code != 1 || !strings.Contains(errb.String(), "OPENAI_API_KEY") || strings.Contains(errb.String(), "sk-other-value") {
		t.Errorf("exit %d: %s", code, errb.String())
	}
}

func TestRepeatReachesTheTaskList(t *testing.T) {
	f, err := parseRunFlags([]string{"--run", "x", "--arms", "A0", "--set", "s2", "--repeat", "2", "--limit-sessions", "1", "--budget", "1"}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	tasks := f.tasks()
	if len(tasks) != 2 || !strings.HasSuffix(tasks[1].ID, "~r2") || tasks[0].ID+"~r2" != tasks[1].ID {
		t.Errorf("tasks: %d", len(tasks))
	}
}

func TestGitCommitMarksChangesUnderTheExperiment(t *testing.T) {
	dir := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-c", "user.name=test", "-c", "user.email=test@invalid", "-c", "commit.gpgsign=false"}, args...)...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	t.Chdir(dir)
	if got := gitCommit(); got != "unknown" {
		t.Skipf("the temp directory is inside a repository: %s", got)
	}
	git("init")
	git("commit", "--allow-empty", "-m", "start")
	clean := gitCommit()
	if clean == "unknown" || strings.HasSuffix(clean, "-dirty") {
		t.Fatalf("a clean checkout reads %q", clean)
	}
	if err := os.WriteFile(filepath.Join(dir, "elsewhere.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := gitCommit(); got != clean {
		t.Errorf("a change outside the experiment reads %q", got)
	}
	sub := filepath.Join(dir, "experiments", "stage0")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "new.go"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := gitCommit(); got != clean+"-dirty" {
		t.Errorf("an untracked file under the experiment reads %q", got)
	}
}

func TestS2AcceptanceIsAnS2Set(t *testing.T) {
	f, err := parseRunFlags([]string{"--run", "x", "--arms", "A0", "--set", "s2acceptance", "--repeat", "5", "--budget", "1"}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := len(f.tasks()), 5*len(s2.Acceptance()); got != want {
		t.Errorf("%d tasks, want %d", got, want)
	}
	if _, _, err := loadSet("s2acceptance", 0, 0); err != nil {
		t.Error(err)
	}
}

func TestResumeRefusesAnotherSet(t *testing.T) {
	prev := report.Manifest{Run: "x", Set: "s2", Commit: "c"}
	next := report.Manifest{Run: "x", Set: "s2acceptance", Commit: "c"}
	if err := checkResume(prev, next, "m"); err == nil || !strings.Contains(err.Error(), "set") {
		t.Errorf("err = %v", err)
	}
	next.Set = "s2"
	if err := checkResume(prev, next, "m"); err != nil {
		t.Error(err)
	}
}

func TestOnlyJobsKeepsTheListedPairsAndRefusesOthers(t *testing.T) {
	task := s2.Tasks()[0]
	other := s2.Tasks()[1]
	jobs := []arms.Job{{Arm: "T1", Task: &task}, {Arm: "A4", Task: &task}, {Arm: "T1", Task: &other}}
	path := filepath.Join(t.TempDir(), "only.json")
	write := func(body string) {
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(`[{"arm":"A4","session":"` + task.ID + `"},{"arm":"T1","session":"` + other.ID + `"}]`)
	kept, err := onlyJobs(jobs, path)
	if err != nil || len(kept) != 2 || kept[0].Arm != "A4" || kept[1].ID() != other.ID {
		t.Fatalf("kept = %+v, err = %v", kept, err)
	}
	write(`[{"arm":"A4","session":"no-such-task"}]`)
	if _, err := onlyJobs(jobs, path); err == nil {
		t.Error("a pair outside the plan was accepted")
	}
}
