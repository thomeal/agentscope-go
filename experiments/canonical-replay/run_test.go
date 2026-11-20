package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestScheduleIsDeterministicAndShuffled(t *testing.T) {
	ids := []int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10}
	a, b := schedule(ids, 4), schedule(ids, 4)
	if len(a) != 660 {
		t.Fatalf("%d jobs, want 660", len(a))
	}
	seen := map[job]bool{}
	for i := range a {
		if a[i] != b[i] {
			t.Fatal("the same seed gave two orders")
		}
		if seen[a[i]] {
			t.Fatalf("job %+v repeats", a[i])
		}
		seen[a[i]] = true
	}
	if c := schedule(ids, 5); c[0] == a[0] && c[1] == a[1] && c[2] == a[2] {
		t.Error("another seed gave the same order")
	}
	head := map[string]int{}
	for _, j := range a[:40] {
		head[j.Variant]++
	}
	if head[libOff] == 0 || head[libOn] == 0 {
		t.Errorf("variants are not interleaved: %v", head)
	}
}

func TestPendingAndFinal(t *testing.T) {
	jobs := []job{{0, libOff, 0}, {0, libOff, 1}, {0, libOn, 0}, {0, libOn, 1}, {1, libOff, 0}}
	e400, canceled := "http 400", errCanceled
	ok := &Outcome{Status: "completed", Tools: []string{}}
	rows := []Row{
		{Prompt: 0, Variant: libOff, I: 0, Outcome: ok},
		{Prompt: 0, Variant: libOff, I: 1, Error: &e400},
		{Prompt: 0, Variant: libOn, I: 0, Error: &e400},
		{Prompt: 0, Variant: libOn, I: 0, Rerun: 1, Outcome: ok},
		{Prompt: 0, Variant: libOn, I: 1, Error: &canceled, HeldUSD: 0.004},
	}
	got := pending(jobs, rows, false)
	if fmt.Sprint(got) != fmt.Sprint([]job{{0, libOn, 1}, {1, libOff, 0}}) {
		t.Errorf("pending = %v", got)
	}
	if got := pending(jobs, rows, true); fmt.Sprint(got) != fmt.Sprint([]job{{0, libOff, 1}}) {
		t.Errorf("pending rerun = %v", got)
	}
	f := final(rows)
	if len(f) != 3 || !f[job{0, libOn, 0}].valid() || f[job{0, libOff, 1}].valid() {
		t.Errorf("final = %+v", f)
	}
	spent := 0.0
	for _, r := range rows {
		spent += r.charged()
	}
	if spent != 0.004 {
		t.Errorf("charged = %v", spent)
	}
}

func TestBudgetHoldsConcurrentReservations(t *testing.T) {
	b := &budget{limit: 0.01}
	for i, want := range []bool{true, true, false} {
		if got := b.reserve(0.004); got != want {
			t.Fatalf("reservation %d: got %v, want %v", i+1, got, want)
		}
	}
	b.settle(0.004, 0.001)
	if !b.reserve(0.004) {
		t.Fatal("settling should free room")
	}
	if math.Abs(b.total()-0.001) > 1e-12 {
		t.Errorf("spent = %v", b.total())
	}
}

func TestCostMatchesStage0Ledger(t *testing.T) {
	rows, err := readLedger(stage0Replay(t, "replay.jsonl", "stage 0's replay ledger is not published"))
	if err != nil || len(rows) == 0 {
		t.Fatalf("ledger: %d rows, %v", len(rows), err)
	}
	n := 0
	for _, r := range rows {
		if r.Outcome == nil {
			continue
		}
		n++
		if got := luna.cost(r.Input, r.Cached, 0, r.Output); math.Abs(got-r.USD) > 1e-12 {
			t.Fatalf("row %+v: cost %v, ledger %v", r, got, r.USD)
		}
	}
	if n < 2900 {
		t.Errorf("only %d priced rows", n)
	}
}

func TestOutcomeOf(t *testing.T) {
	args := `{"content":"` + strings.Repeat("x", 500)
	cut := fmt.Sprintf(`{"status":"incomplete","incomplete_details":{"reason":"max_output_tokens"},"output":[{"type":"reasoning"},{"type":"function_call","name":"write_file","arguments":%q}],"usage":{"input_tokens":1823,"input_tokens_details":{"cached_tokens":0},"output_tokens":8000,"output_tokens_details":{"reasoning_tokens":298}}}`, args)
	o, err := outcomeOf([]byte(cut))
	if err != nil {
		t.Fatal(err)
	}
	r := Row{Outcome: o}
	if !r.runaway() || o.ArgsLen != len(args) || *o.FirstKey != "content" || len([]rune(o.Head)) != 160 || len([]rune(o.Tail)) != 160 || o.Reasoning != 298 {
		t.Errorf("cut reply: %+v", o)
	}
	if math.Abs(o.USD-0.0041823) > 1e-12 {
		t.Errorf("usd = %v", o.USD)
	}
	done := `{"status":"completed","output":[{"type":"function_call","name":"list_files","arguments":"{}"},{"type":"function_call","name":"read_file","arguments":"{\"path\":\"a.go\"}"}],"usage":{"input_tokens":4810,"input_tokens_details":{"cached_tokens":4807},"output_tokens":420}}`
	o, err = outcomeOf([]byte(done))
	if err != nil {
		t.Fatal(err)
	}
	r = Row{Outcome: o}
	if r.runaway() || o.Reason != nil || fmt.Sprint(o.Tools) != "[list_files read_file]" || o.ArgsLen != 17 || *o.FirstKey != "path" || o.Head != "" {
		t.Errorf("completed reply: %+v", o)
	}
	if _, err := outcomeOf([]byte(`{"status":"completed","output":[]}`)); err == nil {
		t.Error("a response without usage was accepted")
	}
}

type scripted struct {
	mu    sync.Mutex
	steps []func() (*http.Response, error)
	calls int
}

func (s *scripted) RoundTrip(r *http.Request) (*http.Response, error) {
	_, _ = io.Copy(io.Discard, r.Body)
	s.mu.Lock()
	defer s.mu.Unlock()
	step := s.steps[min(s.calls, len(s.steps)-1)]
	s.calls++
	return step()
}

func status(code int, body string) func() (*http.Response, error) {
	return func() (*http.Response, error) {
		return &http.Response{StatusCode: code, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}, nil
	}
}

func TestSendRetriesLikeStage0(t *testing.T) {
	ok := `{"status":"completed","output":[],"usage":{"input_tokens":10,"output_tokens":10}}`
	drop := func() (*http.Response, error) { return nil, errors.New("connection reset") }
	cases := []struct {
		name               string
		steps              []func() (*http.Response, error)
		tries, lost, final int
	}{
		{"5xx then 200", []func() (*http.Response, error){status(500, "x"), status(429, "x"), status(200, ok)}, 3, 0, 200},
		{"400 is final", []func() (*http.Response, error){status(400, "bad")}, 1, 0, 400},
		{"drop then 200", []func() (*http.Response, error){drop, status(200, ok)}, 2, 1, 200},
		{"always 503", []func() (*http.Response, error){status(503, "x")}, 9, 0, 503},
	}
	for _, c := range cases {
		base := &scripted{steps: c.steps}
		ct := &cloudTransport{base: base, budget: &budget{limit: 1}, sleep: func(context.Context, time.Duration) error { return nil }}
		req, _ := http.NewRequest(http.MethodPost, "https://offline.invalid/v1/responses", nil)
		ex, err := ct.send(context.Background(), req, []byte(`{}`))
		if err != nil || ex.tries != c.tries || ex.lost != c.lost || ex.status != c.final {
			t.Errorf("%s: tries %d lost %d status %d err %v", c.name, ex.tries, ex.lost, ex.status, err)
		}
	}
}

func TestReadKey(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "env")
	_ = os.WriteFile(path, []byte("OTHER=1\nOPENAI_API_KEY=\"sk-test-123\"\n"), 0o600)
	if k, err := readKey(path); err != nil || k != "sk-test-123" {
		t.Errorf("key %q err %v", k, err)
	}
	_ = os.WriteFile(path, []byte("OPENAI_KEY=sk-test-123\n"), 0o600)
	if _, err := readKey(path); err == nil || strings.Contains(err.Error(), "sk-test") {
		t.Errorf("err = %v", err)
	}
}

// fakeAPI stands in for the Responses API. It checks the shaping on every request, runs
// away on lib_off's prompts other than 1, answers HTTP 400 once, and counts requests.
type fakeAPI struct {
	t        *testing.T
	requests atomic.Int32
	fail400  atomic.Int32 // requests left to answer with a 400
	bad      atomic.Int32
}

func (f *fakeAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.requests.Add(1)
	raw, _ := io.ReadAll(r.Body)
	var body map[string]any
	_ = json.Unmarshal(raw, &body)
	if r.Header.Get("Authorization") != "Bearer test-key" || body["max_output_tokens"] != float64(8000) || body["store"] != false {
		f.bad.Add(1)
	}
	for _, it := range body["input"].([]any) {
		m := it.(map[string]any)
		if id, ok := m["id"].(string); ok && m["type"] == "function_call" && !strings.HasPrefix(id, "fc") {
			f.bad.Add(1)
		}
	}
	if f.fail400.Add(-1) >= 0 {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"bad"}}`))
		return
	}
	spaced := bytes.Contains(raw, []byte(`{\"path\": \"a.go\", `))
	if spaced && !bytes.Contains(raw, []byte(`Add Foo 1.`)) {
		_, _ = w.Write([]byte(`{"status":"incomplete","incomplete_details":{"reason":"max_output_tokens"},"output":[{"type":"function_call","name":"write_file","arguments":"{\"content\":\"package a"}],"usage":{"input_tokens":1000,"output_tokens":8000}}`))
		return
	}
	_, _ = w.Write([]byte(`{"status":"completed","output":[{"type":"function_call","name":"write_file","arguments":"{\"content\":\"package a\",\"path\":\"a.go\"}"}],"usage":{"input_tokens":1000,"input_tokens_details":{"cached_tokens":900},"output_tokens":300}}`))
}

func syntheticPrompts(t *testing.T) []recorded {
	var out []recorded
	for id := range 11 {
		items := syntheticItems()
		items[1] = msg("user", fmt.Sprintf("Add Foo %d.", id))
		out = append(out, recorded{ID: id, Session: fmt.Sprintf("s-%d", id), Group: "shared", Known: id == 0, Req: syntheticRequest(t, items)})
	}
	return out
}

func TestRunStopsResumesAndReruns(t *testing.T) {
	api := &fakeAPI{t: t}
	api.fail400.Store(1)
	srv := httptest.NewServer(api)
	defer srv.Close()
	dir := t.TempDir()
	cfg := runConfig{
		prompts: syntheticPrompts(t), outDir: dir, apiKey: "test-key", baseURL: srv.URL,
		limit: 0.02, workers: 4, seed: 4, base: http.DefaultTransport, timeout: 20 * time.Second,
		sleep: func(context.Context, time.Duration) error { return nil },
	}
	var log bytes.Buffer
	err := run(context.Background(), cfg, &log)
	if err == nil || !strings.Contains(err.Error(), whyHardStop) {
		t.Fatalf("first run: %v\n%s", err, log.String())
	}
	rows, _ := readLedger(filepath.Join(dir, "replay.jsonl"))
	spent := 0.0
	for _, r := range rows {
		spent += r.charged()
	}
	if len(rows) == 0 || len(rows) >= 660 || spent > cfg.limit {
		t.Fatalf("first run wrote %d rows for $%v", len(rows), spent)
	}

	cfg.limit = 2
	if err := run(context.Background(), cfg, &log); err != nil {
		t.Fatalf("second run: %v", err)
	}
	cfg.rerun = true
	if err := run(context.Background(), cfg, &log); err != nil {
		t.Fatalf("rerun: %v", err)
	}
	if err := run(context.Background(), cfg, &log); err != nil {
		t.Fatalf("second rerun: %v", err)
	}
	rows, _ = readLedger(filepath.Join(dir, "replay.jsonl"))
	if len(rows) != 661 || int(api.requests.Load()) != 661 || api.bad.Load() != 0 {
		t.Fatalf("rows %d, requests %d, badly shaped %d", len(rows), api.requests.Load(), api.bad.Load())
	}
	fin := final(rows)
	reruns := 0
	for _, r := range rows {
		if r.Rerun > 0 {
			reruns++
			if !r.valid() {
				t.Errorf("rerun row is not valid: %+v", r)
			}
		}
	}
	if len(fin) != 660 || reruns != 1 {
		t.Fatalf("final %d, reruns %d", len(fin), reruns)
	}
	cells, prompts := tally(rows)
	if c := cells[jobKey{0, libOff}]; c.runaways != 30 || c.valid != 30 {
		t.Errorf("prompt 0 lib_off = %+v", c)
	}
	if c := cells[jobKey{1, libOff}]; c.runaways != 0 {
		t.Errorf("prompt 1 lib_off = %+v", c)
	}
	for _, id := range prompts {
		if c := cells[jobKey{id, libOn}]; c.runaways != 0 || c.valid != 30 {
			t.Errorf("prompt %d lib_on = %+v", id, c)
		}
	}
	var rep bytes.Buffer
	report(&rep, rows, nil, 1, 1000)
	if !strings.Contains(rep.String(), "confirmed: the CI lies below 0") {
		t.Errorf("report:\n%s", rep.String())
	}
	for _, name := range []string{"replay.jsonl", "manifest.jsonl"} {
		b, _ := os.ReadFile(filepath.Join(dir, name))
		if bytes.Contains(b, []byte("test-key")) {
			t.Errorf("%s holds the key", name)
		}
	}
}

func TestRunRefusesABodyThatDiffersFromTheCheck(t *testing.T) {
	api := &fakeAPI{t: t}
	srv := httptest.NewServer(api)
	defer srv.Close()
	ct := &cloudTransport{base: http.DefaultTransport, budget: &budget{limit: 1}, expect: map[jobKey]string{{0, libOff}: "not-the-hash"}}
	p := syntheticPrompts(t)[0]
	c := &capture{key: jobKey{0, libOff}}
	res := replayOne(withCapture(context.Background(), c), p.Req, libOff, "test-key", srv.URL, ct)
	if res.err == nil || api.requests.Load() != 0 || !strings.HasPrefix(c.err, "outbound body differs") {
		t.Fatalf("err %v, requests %d, capture %q", res.err, api.requests.Load(), c.err)
	}
}

// TestReportReproducesAmendment8 reads Amendment 8's ledger with this program's reading:
// cloud_style against exact on the second pass gave −8.2 points [−13.6, −3.6].
func TestReportReproducesAmendment8(t *testing.T) {
	old, err := readLedger(stage0Replay(t, "replay.jsonl", "stage 0's replay ledger is not published"))
	if err != nil || len(old) == 0 {
		t.Fatalf("ledger: %d rows, %v", len(old), err)
	}
	var rows []Row
	for _, r := range old {
		if r.Seed != 2 || r.Group != "shared" {
			continue
		}
		switch r.Variant {
		case "exact":
			r.Variant = libOff
		case "cloud_style":
			r.Variant = libOn
		default:
			continue
		}
		rows = append(rows, r)
	}
	cells, prompts := tally(rows)
	p := byPrompt(cells, prompts, libOn, libOff, 1, 10000)
	t.Logf("cloud_style − exact: %+.2f [%+.2f, %+.2f] on %d prompts", p.mean, p.lo, p.hi, len(p.diffs))
	if len(p.diffs) != 11 || math.Abs(p.mean-(-8.18)) > 0.01 || math.Abs(p.lo-(-13.6)) > 0.6 || math.Abs(p.hi-(-3.6)) > 0.6 {
		t.Errorf("got %+.2f [%+.2f, %+.2f]", p.mean, p.lo, p.hi)
	}
}
