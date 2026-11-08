package arms

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/agentscope-ai/agentscope-go/v2/experiments/stage0/corpus"
	"github.com/agentscope-ai/agentscope-go/v2/experiments/stage0/lab"
	"github.com/agentscope-ai/agentscope-go/v2/experiments/stage0/report"
	"github.com/agentscope-ai/agentscope-go/v2/pkg/agentscope/message"
	"github.com/agentscope-ai/agentscope-go/v2/pkg/agentscope/model"
)

type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

func readRows(t *testing.T, path string) []Row {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var out []Row
	for line := range strings.SplitSeq(string(raw), "\n") {
		var r Row
		if json.Unmarshal([]byte(line), &r) == nil && r.Arm != "" {
			out = append(out, r)
		}
	}
	return out
}

func smallSessions(n int) []corpus.Session {
	out := make([]corpus.Session, 0, n)
	for i := range n {
		out = append(out, corpus.Generate(shortSeed+int64(i), 20, corpus.SetTuning))
	}
	return out
}

func TestPlanCoversEveryPairInASeededOrder(t *testing.T) {
	sessions := smallSessions(6)
	arms := []string{ArmA0, ArmA38K, ArmT1}
	a := Plan(arms, sessions, nil, 42)
	b := Plan(arms, sessions, nil, 42)
	if len(a) != 18 {
		t.Fatalf("%d jobs", len(a))
	}
	first := map[string]bool{}
	seen := map[string]bool{}
	for i, j := range a {
		if b[i].Arm != j.Arm || b[i].ID() != j.ID() {
			t.Fatal("same seed, different order")
		}
		seen[pairKey(j.Arm, j.ID())] = true
		if i%3 == 0 {
			first[j.Arm] = true
		}
	}
	if len(seen) != 18 {
		t.Errorf("%d distinct pairs", len(seen))
	}
	if len(first) < 2 {
		t.Errorf("the same arm always leads: %v", first)
	}
	c := Plan(arms, sessions, nil, 43)
	same := true
	for i := range a {
		if a[i].Arm != c[i].Arm {
			same = false
		}
	}
	if same {
		t.Error("the seed has no effect")
	}
}

func stubExec(n *atomic.Int32) func(context.Context, Job) ([]Row, error) {
	return func(_ context.Context, j Job) ([]Row, error) {
		n.Add(1)
		return []Row{{Arm: j.Arm, Session: j.ID(), Retention: 0.5}}, nil
	}
}

func TestRunnerResumeSkipsFinishedPairs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "scores.jsonl")
	jobs := Plan([]string{ArmA0, ArmT1}, smallSessions(2), nil, 1)
	var n atomic.Int32
	var out syncBuffer
	r := &Runner{ScoresPath: path, Workers: 2, Out: &out, Exec: stubExec(&n)}
	sum, err := r.Run(context.Background(), jobs[:3])
	if err != nil {
		t.Fatal(err)
	}
	if sum.Finished != 3 || n.Load() != 3 {
		t.Fatalf("summary %+v, calls %d", sum, n.Load())
	}
	// A crash mid-write leaves a torn last line, which is not a finished pair.
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(`{"arm":"A0","session":"torn`); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()

	n.Store(0)
	sum, err = r.Run(context.Background(), jobs)
	if err != nil {
		t.Fatal(err)
	}
	if sum.Skipped != 3 || sum.Finished != 1 || n.Load() != 1 {
		t.Errorf("summary %+v, calls %d", sum, n.Load())
	}
	if rows := readRows(t, path); len(rows) != 4 {
		t.Errorf("%d rows after resume", len(rows))
	}
	n.Store(0)
	if sum, err = r.Run(context.Background(), jobs); err != nil || sum.Skipped != 4 || n.Load() != 0 {
		t.Errorf("third run: %+v, calls %d, %v", sum, n.Load(), err)
	}
}

func TestResumeTreatsAPrimaryRowAsTheWholePair(t *testing.T) {
	// A secondary's row alone does not finish a pair, because Exec writes it first.
	path := filepath.Join(t.TempDir(), "scores.jsonl")
	s := smallSessions(1)
	var n atomic.Int32
	exec := func(_ context.Context, j Job) ([]Row, error) {
		n.Add(1)
		return []Row{{Arm: ArmT1Brief, Session: j.ID()}, {Arm: j.Arm, Session: j.ID()}}, nil
	}
	r := &Runner{ScoresPath: path, Workers: 1, Out: &syncBuffer{}, Exec: exec}
	if err := os.WriteFile(path, []byte(`{"arm":"T1+brief","session":"`+s[0].ID+`"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Run(context.Background(), Plan([]string{ArmT1}, s, nil, 1)); err != nil {
		t.Fatal(err)
	}
	if n.Load() != 1 {
		t.Errorf("%d executions", n.Load())
	}
}

func TestBudgetStopHaltsTheRun(t *testing.T) {
	f := newFake(t, modeOracle, Window8K)
	// One call on this fake crosses this stop, so the next one is refused.
	env := f.env(1e-9)
	sessions := smallSessions(4)
	for _, s := range sessions {
		f.loadSession(s)
	}
	var out syncBuffer
	path := filepath.Join(t.TempDir(), "scores.jsonl")
	r := &Runner{Env: env, ScoresPath: path, Workers: 1, Timeout: time.Minute, Out: &out}
	sum, err := r.Run(context.Background(), Plan([]string{ArmA0}, sessions, nil, 1))
	if err != nil {
		t.Fatal(err)
	}
	if !sum.BudgetStop {
		t.Fatalf("no budget stop: %+v", sum)
	}
	if !strings.Contains(out.String(), "ERROR budget stop") {
		t.Errorf("output: %s", out.String())
	}
	if rows := readRows(t, path); len(rows) != 0 {
		t.Errorf("%d rows after a stop on the first session", len(rows))
	}
	if calls := len(f.captures()); calls != 1 {
		t.Errorf("%d calls reached the provider after the stop", calls)
	}
}

func TestBudgetCrossedDuringForkOrCompressionStopsTheSession(t *testing.T) {
	for _, tc := range []struct {
		arm  string
		item bool
	}{{ArmC0, true}, {ArmC3, true}, {ArmT1, false}, {ArmA38K, false}} {
		t.Run(tc.arm, func(t *testing.T) {
			f := newFake(t, modeOracle, Window8K)
			env := f.env(0.0005)
			s := shortSession()
			b := benchItem()
			f.loadSession(s)
			f.loadBench(b)
			env.Ledger.SetSpent(0.0005)
			job := Job{Arm: tc.arm, Session: &s}
			if tc.item {
				job = Job{Arm: tc.arm, Bench: &b}
			}
			_, err := env.Exec(context.Background(), job)
			if !errors.Is(err, lab.ErrBudget) {
				t.Errorf("err = %v", err)
			}
		})
	}
}

func TestPanickingSessionBecomesAnErrorRow(t *testing.T) {
	path := filepath.Join(t.TempDir(), "scores.jsonl")
	sessions := smallSessions(2)
	var out syncBuffer
	r := &Runner{ScoresPath: path, Workers: 1, Out: &out, Exec: func(_ context.Context, j Job) ([]Row, error) {
		if j.ID() == sessions[0].ID {
			panic("boom sk-secret-value")
		}
		return []Row{{Arm: j.Arm, Session: j.ID(), Retention: 1}}, nil
	}}
	sum, err := r.Run(context.Background(), Plan([]string{ArmA0}, sessions, nil, 1))
	if err != nil {
		t.Fatal(err)
	}
	if sum.Finished != 2 || sum.Errors != 1 {
		t.Fatalf("summary %+v", sum)
	}
	var bad Row
	for _, row := range readRows(t, path) {
		if row.Session == sessions[0].ID {
			bad = row
		}
	}
	if !strings.Contains(bad.Error, "panic: boom") || strings.Contains(bad.Error, "secret") {
		t.Errorf("error = %q", bad.Error)
	}
	if bad.Retention != 0 || bad.Questions == 0 || bad.Counters["session_errors"] != 1 {
		t.Errorf("row = %+v", bad)
	}
	if !strings.Contains(out.String(), "ERROR "+ArmA0+" "+sessions[0].ID) {
		t.Errorf("output: %s", out.String())
	}
}

type panicModel struct{ fixedModel }

func (*panicModel) Chat(context.Context, []*message.Msg, ...model.CallOption) (*model.ChatResponse, error) {
	panic("model boom")
}

func TestToolAndModelPanicsBecomeErrors(t *testing.T) {
	rec := newToolRec()
	panicky := rec.wrap("t", "d", recordSchema, func(map[string]any) (string, error) { panic("tool boom") })
	resp, err := panicky.Execute(context.Background(), map[string]any{})
	if err == nil {
		raw, merr := json.Marshal(resp)
		if merr != nil || !strings.Contains(string(raw), "tool boom") {
			t.Errorf("tool panic: %s, %v", raw, merr)
		}
	} else if !strings.Contains(err.Error(), "tool boom") {
		t.Errorf("tool panic: %v", err)
	}
	w := &watchModel{inner: &panicModel{}, c: &Counters{}}
	if _, err := w.Chat(context.Background(), nil); err == nil || !strings.Contains(err.Error(), "model boom") {
		t.Errorf("model panic: %v", err)
	}
}

func TestTimeoutBecomesAnErrorRow(t *testing.T) {
	f := newFake(t, modeOracle, Window8K)
	f.onRequest = func() { time.Sleep(40 * time.Millisecond) }
	env := f.env(0)
	s := smallSessions(1)
	f.loadSession(s[0])
	path := filepath.Join(t.TempDir(), "scores.jsonl")
	r := &Runner{Env: env, ScoresPath: path, Workers: 1, Timeout: 100 * time.Millisecond, Out: &syncBuffer{}}
	sum, err := r.Run(context.Background(), Plan([]string{ArmA0}, s, nil, 1))
	if err != nil {
		t.Fatal(err)
	}
	if sum.Errors != 1 || sum.Finished != 1 {
		t.Fatalf("summary %+v", sum)
	}
	rows := readRows(t, path)
	if len(rows) != 1 || rows[0].Error == "" || rows[0].Questions == 0 {
		t.Errorf("rows = %+v", rows)
	}
}

func TestCancelledRunWritesNoRowForInterruptedWork(t *testing.T) {
	path := filepath.Join(t.TempDir(), "scores.jsonl")
	ctx, cancel := context.WithCancel(context.Background())
	r := &Runner{ScoresPath: path, Workers: 1, Out: &syncBuffer{}, Exec: func(ctx context.Context, j Job) ([]Row, error) {
		cancel()
		<-ctx.Done()
		return []Row{{Arm: j.Arm, Session: j.ID()}}, nil
	}}
	if _, err := r.Run(ctx, Plan([]string{ArmA0}, smallSessions(1), nil, 1)); err != nil {
		t.Fatal(err)
	}
	if rows := readRows(t, path); len(rows) != 0 {
		t.Errorf("%d rows from an interrupted session", len(rows))
	}
}

func TestCheckLocalWindowRefusesMismatch(t *testing.T) {
	f := newFake(t, modeOracle, Window8K)
	ctx := context.Background()
	if err := CheckLocalWindow(ctx, f.srv.URL, "qwen", []string{ArmA38K, ArmA6, ArmT1, ArmC3}); err != nil {
		t.Errorf("matching window refused: %v", err)
	}
	if err := CheckLocalWindow(ctx, f.srv.URL, "qwen", []string{ArmA332K}); err == nil || !strings.Contains(err.Error(), "max_model_len") {
		t.Errorf("A3-32K on an 8K server: %v", err)
	}
	if err := CheckLocalWindow(ctx, f.srv.URL, "other", []string{ArmA38K}); err == nil {
		t.Error("wrong model accepted")
	}
	if err := CheckLocalWindow(ctx, "http://127.0.0.1:1", "qwen", []string{ArmA0}); err != nil {
		t.Errorf("A0 needs no local server: %v", err)
	}
}

func TestKeyNeverReachesRunOutput(t *testing.T) {
	key := "sk-proj-abcDEF123_xyz"
	if got := scrub("bad key "+key+" and sk-other123", key); strings.Contains(got, key) || strings.Contains(got, "sk-other123") {
		t.Errorf("scrub = %q", got)
	}
	var b bytes.Buffer
	w := &ScrubWriter{W: &b, Key: key}
	line := "failed with " + key + "\n"
	n, err := w.Write([]byte(line))
	if err != nil || n != len(line) {
		t.Errorf("write = %d, %v", n, err)
	}
	if strings.Contains(b.String(), key) {
		t.Errorf("wrote %q", b.String())
	}
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	if err := os.WriteFile(path, []byte("A=1\nOPENAI_API_KEY="+key+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := ReadAPIKey(path)
	if err != nil || got != key {
		t.Errorf("read = %q, %v", got, err)
	}
	if err := os.WriteFile(path, []byte("OTHER="+key+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadAPIKey(path); err == nil || strings.Contains(err.Error(), key) {
		t.Errorf("missing key error: %v", err)
	}
}

func TestKeyDoesNotReachTheLedgerOrScores(t *testing.T) {
	f := newFake(t, modeOracle, Window8K)
	dir := t.TempDir()
	ledger, err := lab.NewLedger("test", filepath.Join(dir, "ledger.jsonl"), 0)
	if err != nil {
		t.Fatal(err)
	}
	env := f.env(0)
	env.Ledger = ledger
	env.APIKey = "sk-very-secret-key-123"
	s := smallSessions(1)
	f.loadSession(s[0])
	r := &Runner{Env: env, ScoresPath: filepath.Join(dir, "scores.jsonl"), Workers: 1, Out: &syncBuffer{}}
	if _, err := r.Run(context.Background(), Plan([]string{ArmA0}, s, nil, 1)); err != nil {
		t.Fatal(err)
	}
	if err := ledger.Close(); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"ledger.jsonl", "scores.jsonl"} {
		raw, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(raw, []byte(env.APIKey)) {
			t.Errorf("%s holds the key", name)
		}
	}
}

func TestReportReadsRowsFromTheRunner(t *testing.T) {
	f := newFake(t, modeOracle, Window8K)
	root := t.TempDir()
	dir := filepath.Join(root, "run")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	ledger, err := lab.NewLedger("run", filepath.Join(dir, "ledger.jsonl"), 0)
	if err != nil {
		t.Fatal(err)
	}
	env := f.env(0)
	env.Ledger = ledger
	sessions := smallSessions(2)
	for _, s := range sessions {
		f.loadSession(s)
	}
	b := benchItem()
	f.loadBench(b)
	r := &Runner{Env: env, ScoresPath: filepath.Join(dir, "scores.jsonl"), Workers: 2, Out: &syncBuffer{}}
	jobs := Plan([]string{ArmA0, ArmA38K, ArmA6, ArmA4, ArmT1}, sessions, nil, 1)
	jobs = append(jobs, Plan(BenchArms, nil, []corpus.CBench{b}, 1)...)
	if _, err := r.Run(context.Background(), jobs); err != nil {
		t.Fatal(err)
	}
	if err := ledger.Close(); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(report.Manifest{Run: "run", Set: "test", Budgets: Budgets()})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, report.ManifestFile), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(root, "out")
	var stdout bytes.Buffer
	if err := report.Main([]string{"--runs", dir, "--out", out}, &stdout); err != nil {
		t.Fatalf("report: %v\n%s", err, stdout.String())
	}
	t.Log(stdout.String())
	if !strings.Contains(stdout.String(), "Privacy PASS") {
		t.Errorf("report output: %s", stdout.String())
	}
	entries, err := os.ReadDir(out)
	if err != nil || len(entries) == 0 {
		t.Fatalf("report wrote %d files, %v", len(entries), err)
	}
}
