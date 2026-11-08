package lab

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/agentscope-ai/agentscope-go/v2/pkg/agentscope/message"
)

// fakeResponses answers /v1/responses and records the raw bodies it received.
type fakeResponses struct {
	mu     sync.Mutex
	bodies [][]byte
	status string
	code   int
	fails  int
}

func (f *fakeResponses) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	raw, _ := io.ReadAll(r.Body)
	f.mu.Lock()
	f.bodies = append(f.bodies, raw)
	fails := f.fails
	if fails > 0 {
		f.fails--
	}
	status := f.status
	code := f.code
	f.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	if fails > 0 {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":{"message":"slow down"}}`))
		return
	}
	if code != 0 {
		w.WriteHeader(code)
		_, _ = w.Write([]byte(`{"error":{"message":"bad key sk-abc123def"}}`))
		return
	}
	if status == "" {
		status = "completed"
	}
	_, _ = w.Write([]byte(`{"status":"` + status + `","output":[{"type":"reasoning","id":"rs_1","summary":[],"encrypted_content":"enc"},{"type":"message","role":"assistant","content":[{"type":"output_text","text":"hello","annotations":null}]}],"usage":{"input_tokens":1000,"input_tokens_details":{"cached_tokens":900,"cache_write_tokens":90},"output_tokens":50,"output_tokens_details":{"reasoning_tokens":20}}}`))
}

func (f *fakeResponses) last(t *testing.T) map[string]any {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	var body map[string]any
	if err := json.Unmarshal(f.bodies[len(f.bodies)-1], &body); err != nil {
		t.Fatal(err)
	}
	return body
}

func newCloud(t *testing.T, f *fakeResponses, l *Ledger, canaries ...string) *Cloud {
	t.Helper()
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	return NewCloud(Options{Ledger: l, Arm: "T1", Session: "s1", BaseURL: srv.URL, APIKey: "k", Model: "m", Canaries: canaries})
}

func userMsg(text string) []*message.Msg {
	return []*message.Msg{message.NewMsg("u", message.RoleUser, text)}
}

func TestCloudInjectsStatelessFieldsAndRecordsUsage(t *testing.T) {
	f := &fakeResponses{}
	l, _ := NewLedger("r", "", 0)
	c := newCloud(t, f, l)
	m, err := c.ChatModel()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Chat(context.Background(), userMsg("hi")); err != nil {
		t.Fatal(err)
	}
	body := f.last(t)
	if body["store"] != false {
		t.Errorf("store = %v, want false", body["store"])
	}
	inc, _ := body["include"].([]any)
	if len(inc) != 1 || inc[0] != "reasoning.encrypted_content" {
		t.Errorf("include = %v", body["include"])
	}
	rows := l.Rows()
	if len(rows) != 1 {
		t.Fatalf("rows = %d", len(rows))
	}
	r := rows[0]
	if r.Role != RoleMain || r.Input != 1000 || r.Cached != 900 || r.Write != 90 || r.Output != 50 || r.Reasoning != 20 {
		t.Errorf("row = %+v", r)
	}
	want := Luna.Cost(1000, 900, 90, 50)
	if r.CostUSD != want || want <= 0 {
		t.Errorf("cost = %v, want %v", r.CostUSD, want)
	}
}

func TestForkKeepsThePrefixAndDoesNotMoveTheSnapshot(t *testing.T) {
	f := &fakeResponses{}
	c := newCloud(t, f, nil)
	m, _ := c.ChatModel()
	ctx := context.Background()
	if _, err := m.Chat(ctx, userMsg("q1")); err != nil {
		t.Fatal(err)
	}
	snap, ok := c.Snapshot()
	if !ok {
		t.Fatal("no snapshot after a completed main call")
	}
	res, err := c.Fork(ctx, snap, ForkRequest{Role: RoleForkB, Prompt: "SUMMARIZE", Limit: 2000})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Completed || res.Text != "hello" || res.Reasoning != 20 {
		t.Errorf("result = %+v", res)
	}
	f.mu.Lock()
	forkRaw := f.bodies[len(f.bodies)-1]
	f.mu.Unlock()
	same, err := PrefixEqual(snap.Body, forkRaw)
	if err != nil || !same {
		t.Errorf("prefix equal = %v, err %v", same, err)
	}
	body := f.last(t)
	if body["tool_choice"] != "none" || body["max_output_tokens"] != float64(2000) {
		t.Errorf("fork fields: %v %v", body["tool_choice"], body["max_output_tokens"])
	}
	items := body["input"].([]any)
	if last := items[len(items)-1].(map[string]any); last["content"] != "SUMMARIZE" {
		t.Errorf("last item = %v", last)
	}
	if strings.Contains(string(forkRaw), `"annotations":null`) {
		t.Error("nulls in the replayed output were not stripped")
	}
	after, _ := c.Snapshot()
	if !bytes.Equal(after.Body, snap.Body) {
		t.Error("a fork moved the snapshot")
	}
}

func TestForkReportsAnIncompleteResultAsNotCompleted(t *testing.T) {
	f := &fakeResponses{}
	c := newCloud(t, f, nil)
	m, _ := c.ChatModel()
	ctx := context.Background()
	if _, err := m.Chat(ctx, userMsg("q1")); err != nil {
		t.Fatal(err)
	}
	snap, _ := c.Snapshot()
	f.mu.Lock()
	f.status = "incomplete"
	f.mu.Unlock()
	res, err := c.Fork(ctx, snap, ForkRequest{Prompt: "S", Limit: 60})
	if err != nil {
		t.Fatal(err)
	}
	if res.Completed {
		t.Errorf("incomplete fork reported as completed: %+v", res)
	}
}

func TestIncompleteMainCallDoesNotMoveTheSnapshot(t *testing.T) {
	f := &fakeResponses{status: "incomplete"}
	c := newCloud(t, f, nil)
	m, _ := c.ChatModel()
	if _, err := m.Chat(context.Background(), userMsg("q1")); err != nil {
		t.Fatal(err)
	}
	if _, ok := c.Snapshot(); ok {
		t.Error("an incomplete main call became a fork point")
	}
}

func TestProbeAndCompressRolesDoNotMoveTheSnapshot(t *testing.T) {
	f := &fakeResponses{}
	l, _ := NewLedger("r", "", 0)
	c := newCloud(t, f, l)
	m, _ := c.ChatModel()
	if _, err := m.Chat(WithRole(context.Background(), RoleProbe), userMsg("q")); err != nil {
		t.Fatal(err)
	}
	if _, ok := c.Snapshot(); ok {
		t.Error("a probe became a fork point")
	}
	if got := l.Rows()[0].Role; got != RoleProbe {
		t.Errorf("role = %s", got)
	}
}

func TestPrepareDropsOnlyForeignCallIDs(t *testing.T) {
	body := map[string]any{"input": []any{
		map[string]any{"type": "function_call", "id": "call_abc", "call_id": "call_abc"},
		map[string]any{"type": "function_call", "id": "fc_abc", "call_id": "fc_abc"},
		map[string]any{"role": "user", "content": "x"},
	}}
	prepareResponses(body)
	items := body["input"].([]any)
	if _, has := items[0].(map[string]any)["id"]; has {
		t.Error("a foreign id survived")
	}
	if items[0].(map[string]any)["call_id"] != "call_abc" {
		t.Error("call_id was changed")
	}
	if items[1].(map[string]any)["id"] != "fc_abc" {
		t.Error("a native id was dropped")
	}
}

func TestCanonicalReplayRewritesOnlyLocalCalls(t *testing.T) {
	const (
		localArgs  = `{"path": "a.go", "content": "if a<b && c {}\n", "n": 1.50}`
		wantLocal  = `{"content":"if a<b && c {}\n","n":1.50,"path":"a.go"}`
		cloudArgs  = `{"path":"b.go", "content":"x"}`
		brokenArgs = `{"path": "c.go", "content": "cut`
	)
	send := func(on bool) []any {
		t.Helper()
		f := &fakeResponses{}
		srv := httptest.NewServer(f)
		defer srv.Close()
		l, _ := NewLedger("r", "", 0)
		c := NewCloud(Options{Ledger: l, Arm: "A8", Session: "s1", BaseURL: srv.URL, APIKey: "k", Model: "m", CanonicalReplay: on})
		body, err := json.Marshal(map[string]any{"model": "m", "input": []any{
			map[string]any{"type": "function_call", "call_id": "chatcmpl-tool-1", "name": "write_file", "arguments": localArgs},
			map[string]any{"type": "function_call", "call_id": "fc_2", "name": "write_file", "arguments": cloudArgs},
			map[string]any{"type": "function_call", "call_id": "chatcmpl-tool-3", "name": "write_file", "arguments": brokenArgs},
			map[string]any{"role": "user", "content": "x"},
		}})
		if err != nil {
			t.Fatal(err)
		}
		req, err := http.NewRequest(http.MethodPost, srv.URL+"/v1/responses", bytes.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		resp, err := c.RoundTrip(req)
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		items, _ := f.last(t)["input"].([]any)
		return items
	}
	args := func(items []any, i int) string {
		s, _ := items[i].(map[string]any)["arguments"].(string)
		return s
	}
	on := send(true)
	if got := args(on, 0); got != wantLocal {
		t.Errorf("local call = %s, want %s", got, wantLocal)
	}
	if got := args(on, 1); got != cloudArgs {
		t.Errorf("the cloud's own call changed: %s", got)
	}
	if got := args(on, 2); got != brokenArgs {
		t.Errorf("arguments that do not parse changed: %s", got)
	}
	off := send(false)
	if got := args(off, 0); got != localArgs {
		t.Errorf("without the option, local call = %s", got)
	}
}

func TestBudgetStopRefusesFurtherCloudCalls(t *testing.T) {
	f := &fakeResponses{}
	l, _ := NewLedger("r", "", 0.0000001)
	c := newCloud(t, f, l)
	m, _ := c.ChatModel()
	ctx := context.Background()
	if _, err := m.Chat(ctx, userMsg("q")); err != nil {
		t.Fatal(err)
	}
	if !l.OverBudget() {
		t.Fatal("budget not crossed")
	}
	_, err := m.Chat(ctx, userMsg("q2"))
	if err == nil || !strings.Contains(err.Error(), ErrBudget.Error()) {
		t.Errorf("err = %v, want the budget stop", err)
	}
	if !errors.Is(ErrBudget, ErrBudget) {
		t.Fatal("sentinel")
	}
}

func TestCanaryScanCountsOutboundCodes(t *testing.T) {
	f := &fakeResponses{}
	l, _ := NewLedger("r", "", 0)
	c := newCloud(t, f, l, "K7P-1111", "K7P-2222")
	m, _ := c.ChatModel()
	if _, err := m.Chat(context.Background(), userMsg("the code is K7P-2222")); err != nil {
		t.Fatal(err)
	}
	if got := l.Rows()[0].CanaryHits; got != 1 {
		t.Errorf("canary hits = %d, want 1", got)
	}
}

func TestRetriesTransientFailuresAndCountsAttempts(t *testing.T) {
	f := &fakeResponses{fails: 2}
	l, _ := NewLedger("r", "", 0)
	c := newCloud(t, f, l)
	m, _ := c.ChatModel()
	if _, err := m.Chat(context.Background(), userMsg("q")); err != nil {
		t.Fatal(err)
	}
	if got := l.Rows()[0].Attempt; got != 3 {
		t.Errorf("attempts = %d, want 3", got)
	}
}

func TestErrorBodyIsRedactedInTheLedger(t *testing.T) {
	f := &fakeResponses{code: http.StatusBadRequest}
	l, _ := NewLedger("r", "", 0)
	c := newCloud(t, f, l)
	m, _ := c.ChatModel()
	if _, err := m.Chat(context.Background(), userMsg("q")); err == nil {
		t.Fatal("want an error")
	}
	if got := l.Rows()[0].Error; strings.Contains(got, "sk-abc") || !strings.Contains(got, "sk-***") {
		t.Errorf("ledger error = %q", got)
	}
}

func TestSlotsBoundInFlightAndHonorCancellation(t *testing.T) {
	s := NewSlots(1)
	ctx := context.Background()
	if _, err := s.acquire(ctx); err != nil {
		t.Fatal(err)
	}
	short, cancel := context.WithTimeout(ctx, 20*time.Millisecond)
	defer cancel()
	if _, err := s.acquire(short); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("err = %v", err)
	}
	s.release()
	if _, err := s.acquire(ctx); err != nil {
		t.Errorf("slot not released: %v", err)
	}
	var none *Slots
	if _, err := none.acquire(ctx); err != nil {
		t.Error(err)
	}
	none.release()
}

func TestLocalInjectsServerArgsAndLimit(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &got)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"x","object":"chat.completion","model":"m","choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"ok"}}],"usage":{"prompt_tokens":70,"completion_tokens":7,"total_tokens":77}}`))
	}))
	defer srv.Close()
	led, _ := NewLedger("r", "", 0)
	l := NewLocal(Options{Ledger: led, Arm: "T1", Session: "s", BaseURL: srv.URL, Model: "m"}, 800)
	m, err := l.ChatModel()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Chat(WithLimit(context.Background(), 300), userMsg("q")); err != nil {
		t.Fatal(err)
	}
	if got["max_completion_tokens"] != float64(300) {
		t.Errorf("limit = %v", got["max_completion_tokens"])
	}
	kw, _ := got["chat_template_kwargs"].(map[string]any)
	if kw["enable_thinking"] != false {
		t.Errorf("thinking args = %v", got["chat_template_kwargs"])
	}
	r := led.Rows()[0]
	if r.Side != SideLocal || r.Input != 70 || r.Output != 7 || r.CostUSD != 0 {
		t.Errorf("row = %+v", r)
	}
}

func TestLocalHoldsAnswersToTheSchemaAndNeverACompression(t *testing.T) {
	var got []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(raw, &body)
		got = append(got, body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"x","object":"chat.completion","model":"m","choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"ok"}}],"usage":{"prompt_tokens":70,"completion_tokens":7,"total_tokens":77}}`))
	}))
	defer srv.Close()
	l := NewLocal(Options{Arm: "T1", Session: "s", BaseURL: srv.URL, Model: "m"}, 800)
	m, err := l.ChatModel()
	if err != nil {
		t.Fatal(err)
	}
	ctx := WithAnswerSchema(WithRole(context.Background(), RoleProbe), map[string]any{"type": "object"})
	if _, err := m.Chat(ctx, userMsg("q")); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Chat(WithRole(ctx, RoleCompress), userMsg("q")); err != nil {
		t.Fatal(err)
	}
	if got[0]["response_format"] == nil || got[0]["tool_choice"] != "none" {
		t.Errorf("the answer is not held to the schema: %v", got[0])
	}
	if got[1]["response_format"] != nil || got[1]["tool_choice"] == "none" {
		t.Errorf("the compression inside a repair got the answer's form: %v", got[1])
	}
	if got[0]["max_completion_tokens"] != 800.0 || got[1]["max_completion_tokens"] != 3000.0 {
		t.Errorf("limits = %v, %v: a compression's limit must clear the summary total", got[0]["max_completion_tokens"], got[1]["max_completion_tokens"])
	}
}

func TestLocalMarksContextOverflow(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"This model's maximum context length is 8192 tokens."}}`))
	}))
	defer srv.Close()
	led, _ := NewLedger("r", "", 0)
	l := NewLocal(Options{Ledger: led, BaseURL: srv.URL, Model: "m"}, 0)
	m, _ := l.ChatModel()
	if _, err := m.Chat(context.Background(), userMsg("q")); err == nil {
		t.Fatal("want an error")
	}
	if !led.Rows()[0].Overflow {
		t.Errorf("row = %+v", led.Rows()[0])
	}
}

// submitServer answers a checked fork from a script of submissions.
func submitServer(t *testing.T, replies []string, statuses map[int]string) (*httptest.Server, *[]map[string]any) {
	t.Helper()
	var mu sync.Mutex
	var bodies []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(raw, &body)
		mu.Lock()
		n := len(bodies)
		bodies = append(bodies, body)
		mu.Unlock()
		status := "completed"
		if s, ok := statuses[n]; ok {
			status = s
		}
		args, _ := json.Marshal(map[string]string{"summary": replies[min(n, len(replies)-1)]})
		resp, _ := json.Marshal(map[string]any{
			"id": "r", "status": status, "usage": map[string]any{"input_tokens": 100, "output_tokens": 10},
			"output": []any{
				map[string]any{"type": "reasoning", "id": "rs_1", "summary": []any{}, "encrypted_content": "enc", "content": nil},
				map[string]any{"type": "function_call", "id": fmt.Sprintf("fc_%d", n), "call_id": fmt.Sprintf("call_%d", n), "name": SubmitTool, "arguments": string(args), "status": "completed"},
			},
		})
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(resp)
	}))
	t.Cleanup(srv.Close)
	return srv, &bodies
}

func TestForkSubmitBouncesUntilTheCheckPasses(t *testing.T) {
	snapBody, _ := json.Marshal(map[string]any{"model": "m", "instructions": "s", "tools": []any{map[string]any{"type": "function", "name": "read"}}, "input": []any{map[string]any{"role": "user", "content": "q"}}})
	snap := &Snapshot{Body: snapBody, Output: []any{map[string]any{"type": "message", "role": "assistant", "content": "a"}}}
	reject := func(text string, first bool) (bool, string) {
		return len(text) <= 3, fmt.Sprintf("too long %d %v", len(text), first)
	}

	srv, bodies := submitServer(t, []string{"aaaaaa", "bbbbb", "cc"}, nil)
	c := NewCloud(Options{BaseURL: srv.URL, APIKey: "k", Model: "m", Arm: "T1", Session: "s"})
	ctx := WithEffort(context.Background(), "low")
	res, err := c.ForkSubmit(ctx, snap, ForkRequest{Role: RoleForkB, Prompt: "SUM", Limit: 500}, reject, 4)
	if err != nil {
		t.Fatal(err)
	}
	if res.Text != "cc" || !res.Accepted || !res.Completed || res.Bounces != 2 {
		t.Errorf("result = %+v", res)
	}
	if len(*bodies) != 3 {
		t.Fatalf("%d requests", len(*bodies))
	}
	for i, b := range *bodies {
		if b["tool_choice"] != "required" || b["max_output_tokens"] != float64(500) {
			t.Errorf("request %d: %v %v", i, b["tool_choice"], b["max_output_tokens"])
		}
		tools, _ := b["tools"].([]any)
		if len(tools) != 1 {
			t.Errorf("request %d changed the top-level tools: %v", i, tools)
		}
		raw, _ := json.Marshal(b["input"])
		if got := strings.Count(string(raw), `"configuration_update"`); got != 1 {
			t.Errorf("request %d has %d effort updates", i, got)
		}
		if !strings.Contains(string(raw), `"tool_search_output"`) || !strings.Contains(string(raw), SubmitTool) {
			t.Errorf("request %d lacks the appended tool", i)
		}
	}
	last, _ := json.Marshal((*bodies)[2]["input"])
	for _, want := range []string{"too long 6 true", "too long 5 false", "call_0", "call_1", `"encrypted_content":"enc"`} {
		if !strings.Contains(string(last), want) {
			t.Errorf("the last request lacks %q", want)
		}
	}
	// The prefix through the snapshot is untouched, in order.
	items, _ := (*bodies)[2]["input"].([]any)
	if m, _ := items[0].(map[string]any); m["content"] != "q" {
		t.Errorf("first item = %v", items[0])
	}
	if m, _ := items[1].(map[string]any); m["content"] != "a" {
		t.Errorf("second item = %v", items[1])
	}

	// Rounds run out with the last submission in hand.
	srv, bodies = submitServer(t, []string{"aaaaaa"}, nil)
	c = NewCloud(Options{BaseURL: srv.URL, APIKey: "k", Model: "m"})
	res, err = c.ForkSubmit(context.Background(), snap, ForkRequest{Prompt: "SUM"}, reject, 2)
	if err != nil || res.Accepted || !res.Completed || res.Bounces != 2 || len(*bodies) != 3 {
		t.Errorf("rounds out: %+v, %v, %d requests", res, err, len(*bodies))
	}

	// A first round that did not complete has nothing to apply.
	srv, _ = submitServer(t, []string{"cc"}, map[int]string{0: "incomplete"})
	c = NewCloud(Options{BaseURL: srv.URL, APIKey: "k", Model: "m"})
	if res, err = c.ForkSubmit(context.Background(), snap, ForkRequest{Prompt: "SUM"}, reject, 2); err != nil || res.Completed {
		t.Errorf("incomplete first: %+v, %v", res, err)
	}

	// A later round that did not complete leaves the submission before it.
	srv, _ = submitServer(t, []string{"aaaaaa", "cc"}, map[int]string{1: "incomplete"})
	c = NewCloud(Options{BaseURL: srv.URL, APIKey: "k", Model: "m"})
	res, err = c.ForkSubmit(context.Background(), snap, ForkRequest{Prompt: "SUM"}, reject, 2)
	if err != nil || !res.Completed || res.Accepted || res.Text != "aaaaaa" {
		t.Errorf("incomplete later: %+v, %v", res, err)
	}
}

func TestCutLinesDropsWholeLinesFromTheEnd(t *testing.T) {
	if got, did := CutLines("short", 100); did || got != "short" {
		t.Errorf("a text that fits: %q, %v", got, did)
	}
	text := "first\n" + strings.Repeat("y", 40) + "\nlast line"
	got, did := CutLines(text, 15) // 44 characters fit
	if !did || got != "first" {
		t.Errorf("got %q, %v", got, did)
	}
	got, did = CutLines(strings.Repeat("z", 100), 15)
	if !did || got != strings.Repeat("z", 44) {
		t.Errorf("one long line: %d chars, %v", len(got), did)
	}
	if EstimateTokens(got) > 15 {
		t.Errorf("the cut is still over: %d", EstimateTokens(got))
	}
}

func TestTrimTokensKeepsHeadAndTail(t *testing.T) {
	text := "HEAD" + strings.Repeat("x", 9000) + "TAIL"
	out, trimmed := TrimTokens(text, 1000)
	if !trimmed || !strings.HasPrefix(out, "HEAD") || !strings.HasSuffix(out, "TAIL") {
		t.Fatalf("trimmed=%v head=%q", trimmed, out[:8])
	}
	if EstimateTokens(out) > 1000 {
		t.Errorf("estimate %d over budget", EstimateTokens(out))
	}
	if same, did := TrimTokens("short", 1000); did || same != "short" {
		t.Errorf("short text changed: %q", same)
	}
}

// scriptedFork answers each request with the next reply. A reply is a raw body and a status.
func scriptedFork(t *testing.T, replies []func(n int) (int, string)) (*httptest.Server, *[]map[string]any) {
	t.Helper()
	var mu sync.Mutex
	var bodies []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(raw, &body)
		mu.Lock()
		n := len(bodies)
		bodies = append(bodies, body)
		mu.Unlock()
		status, resp := replies[min(n, len(replies)-1)](n)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(resp))
	}))
	t.Cleanup(srv.Close)
	return srv, &bodies
}

func forkReply(calls ...map[string]any) func(int) (int, string) {
	return func(int) (int, string) {
		out := []any{}
		for _, c := range calls {
			out = append(out, c)
		}
		resp, _ := json.Marshal(map[string]any{"id": "r", "status": "completed", "usage": map[string]any{"input_tokens": 100, "output_tokens": 10}, "output": out})
		return 200, string(resp)
	}
}

func submitCall(id, text string) map[string]any {
	args, _ := json.Marshal(map[string]string{"summary": text})
	return map[string]any{"type": "function_call", "id": "fc_" + id, "call_id": id, "name": SubmitTool, "arguments": string(args)}
}

func otherCall(id string) map[string]any {
	return map[string]any{"type": "function_call", "id": "fc_" + id, "call_id": id, "name": "read_record", "arguments": `{"id":"R-1"}`}
}

func TestForkSubmitAnswersAStrayCallAndGoesOn(t *testing.T) {
	snapBody, _ := json.Marshal(map[string]any{"model": "m", "instructions": "s", "tools": []any{map[string]any{"type": "function", "name": "read_record"}}, "input": []any{map[string]any{"role": "user", "content": "q"}}})
	snap := &Snapshot{Body: snapBody, Output: []any{map[string]any{"type": "message", "role": "assistant", "content": "a"}}}
	pass := func(string, bool) (bool, string) { return true, "" }

	srv, bodies := scriptedFork(t, []func(int) (int, string){forkReply(otherCall("s1")), forkReply(submitCall("s2", "the summary"))})
	c := NewCloud(Options{BaseURL: srv.URL, APIKey: "k", Model: "m", Arm: "T1", Session: "s"})
	res, err := c.ForkSubmit(context.Background(), snap, ForkRequest{Prompt: "SUM"}, pass, 4)
	if err != nil {
		t.Fatal(err)
	}
	if res.Text != "the summary" || !res.Accepted || res.Strays != 1 || res.Bounces != 0 {
		t.Errorf("result = %+v", res)
	}
	last, _ := json.Marshal((*bodies)[1]["input"])
	if !strings.Contains(string(last), `"call_id":"s1"`) || !strings.Contains(string(last), "Not accepted") {
		t.Errorf("the stray call was not answered: %s", last)
	}

	// A submission beside a stray call: both get an answer, or the next request is a 400.
	reject := func(text string, first bool) (bool, string) { return text == "short", "too long" }
	srv, bodies = scriptedFork(t, []func(int) (int, string){forkReply(submitCall("a", "long"), otherCall("b")), forkReply(submitCall("c", "short"))})
	c = NewCloud(Options{BaseURL: srv.URL, APIKey: "k", Model: "m", Arm: "T1", Session: "s"})
	res, err = c.ForkSubmit(context.Background(), snap, ForkRequest{Prompt: "SUM"}, reject, 4)
	if err != nil || res.Text != "short" {
		t.Fatalf("%+v, %v", res, err)
	}
	last, _ = json.Marshal((*bodies)[1]["input"])
	for _, want := range []string{"too long", "Not accepted"} {
		if !strings.Contains(string(last), want) {
			t.Errorf("no %q in %s", want, last)
		}
	}

	// A fork that only ever strays is a failed fork, and the rounds are bounded.
	srv, bodies = scriptedFork(t, []func(int) (int, string){forkReply(otherCall("s"))})
	c = NewCloud(Options{BaseURL: srv.URL, APIKey: "k", Model: "m", Arm: "T1", Session: "s"})
	res, err = c.ForkSubmit(context.Background(), snap, ForkRequest{Prompt: "SUM"}, pass, 2)
	if err != nil || res.Completed || len(*bodies) != 3 || res.Strays != 3 {
		t.Errorf("%d requests: %+v, %v", len(*bodies), res, err)
	}

	// An empty summary submits nothing.
	srv, _ = scriptedFork(t, []func(int) (int, string){forkReply(submitCall("e", "  ")), forkReply(submitCall("f", "real"))})
	c = NewCloud(Options{BaseURL: srv.URL, APIKey: "k", Model: "m", Arm: "T1", Session: "s"})
	res, err = c.ForkSubmit(context.Background(), snap, ForkRequest{Prompt: "SUM"}, pass, 2)
	if err != nil || res.Text != "real" || res.Strays != 1 {
		t.Errorf("%+v, %v", res, err)
	}
}

func TestForkSubmitKeepsAnEarlierSubmissionWhenALaterRoundFails(t *testing.T) {
	snapBody, _ := json.Marshal(map[string]any{"model": "m", "instructions": "s", "tools": []any{}, "input": []any{map[string]any{"role": "user", "content": "q"}}})
	snap := &Snapshot{Body: snapBody, Output: []any{map[string]any{"type": "message", "role": "assistant", "content": "a"}}}
	reject := func(string, bool) (bool, string) { return false, "too long" }
	incomplete, _ := json.Marshal(map[string]any{"id": "r", "status": "incomplete", "usage": map[string]any{"input_tokens": 100, "output_tokens": 10}, "output": []any{}})
	cases := map[string]func(int) (int, string){
		"http error": func(int) (int, string) { return 400, `{"error":{"message":"bad"}}` },
		"not json":   func(int) (int, string) { return 200, "not json" },
		"incomplete": func(int) (int, string) { return 200, string(incomplete) },
		"empty":      forkReply(submitCall("e", "")),
		"no tool":    forkReply(map[string]any{"type": "message", "role": "assistant", "content": []any{}}),
	}
	for name, later := range cases {
		t.Run(name, func(t *testing.T) {
			srv, _ := scriptedFork(t, []func(int) (int, string){forkReply(submitCall("a", "first draft")), later})
			c := NewCloud(Options{BaseURL: srv.URL, APIKey: "k", Model: "m", Arm: "T1", Session: "s"})
			res, err := c.ForkSubmit(context.Background(), snap, ForkRequest{Prompt: "SUM"}, reject, 1)
			if err != nil {
				t.Fatal(err)
			}
			if res.Text != "first draft" || !res.Completed || res.Accepted {
				t.Errorf("result = %+v", res)
			}
		})
	}
}

// An earlier submission does not excuse a budget stop or a cancellation, which are the caller's
// to see.
func TestForkSubmitStillReportsCancellationAfterASubmission(t *testing.T) {
	snapBody, _ := json.Marshal(map[string]any{"model": "m", "instructions": "s", "tools": []any{}, "input": []any{map[string]any{"role": "user", "content": "q"}}})
	snap := &Snapshot{Body: snapBody}
	reject := func(string, bool) (bool, string) { return false, "too long" }
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	later := func(int) (int, string) {
		cancel()
		return 400, `{"error":{"message":"bad"}}`
	}
	srv, _ := scriptedFork(t, []func(int) (int, string){forkReply(submitCall("a", "first draft")), later})
	c := NewCloud(Options{BaseURL: srv.URL, APIKey: "k", Model: "m", Arm: "T1", Session: "s"})
	res, err := c.ForkSubmit(ctx, snap, ForkRequest{Prompt: "SUM"}, reject, 1)
	if err == nil {
		t.Fatalf("no error, result %+v", res)
	}
}

// A stray call is not a submission, so the first submission after one is still a first draft,
// which may be short.
func TestForkSubmitCountsFirstBySubmissionNotByRound(t *testing.T) {
	snapBody, _ := json.Marshal(map[string]any{"model": "m", "instructions": "s", "tools": []any{}, "input": []any{map[string]any{"role": "user", "content": "q"}}})
	snap := &Snapshot{Body: snapBody}
	var firsts []bool
	check := func(_ string, first bool) (bool, string) {
		firsts = append(firsts, first)
		return true, ""
	}
	srv, _ := scriptedFork(t, []func(int) (int, string){forkReply(otherCall("s1")), forkReply(submitCall("a", "draft"))})
	c := NewCloud(Options{BaseURL: srv.URL, APIKey: "k", Model: "m", Arm: "T1", Session: "s"})
	if _, err := c.ForkSubmit(context.Background(), snap, ForkRequest{Prompt: "SUM"}, check, 4); err != nil {
		t.Fatal(err)
	}
	if len(firsts) != 1 || !firsts[0] {
		t.Errorf("first flags = %v", firsts)
	}
}

// Two submissions in one reply: the first stands and the second is answered as a stray.
func TestForkSubmitTakesTheFirstOfTwoSubmissions(t *testing.T) {
	snapBody, _ := json.Marshal(map[string]any{"model": "m", "instructions": "s", "tools": []any{}, "input": []any{map[string]any{"role": "user", "content": "q"}}})
	snap := &Snapshot{Body: snapBody}
	pass := func(string, bool) (bool, string) { return true, "" }
	srv, _ := scriptedFork(t, []func(int) (int, string){forkReply(submitCall("a", "one"), submitCall("b", "two"))})
	c := NewCloud(Options{BaseURL: srv.URL, APIKey: "k", Model: "m", Arm: "T1", Session: "s"})
	res, err := c.ForkSubmit(context.Background(), snap, ForkRequest{Prompt: "SUM"}, pass, 1)
	if err != nil {
		t.Fatal(err)
	}
	if res.Text != "one" {
		t.Errorf("result = %+v", res)
	}
}

// A session that errors stays in the denominator, so the retries have to outlast a short
// outage. One of about 16 seconds took a session in a tuning run.
func TestRetriesOutlastAShortOutage(t *testing.T) {
	total, d := time.Duration(0), 500*time.Millisecond
	for range (&Options{}).attempts() - 1 {
		total += d
		d = nextBackoff(d)
	}
	if total < 60*time.Second || total > 3*time.Minute {
		t.Errorf("the retries wait %v", total)
	}
	if d > backoffCap {
		t.Errorf("the backoff reached %v", d)
	}
}

// eofThen drops the connection n times and then hands over.
type eofThen struct {
	n    int
	next http.RoundTripper
}

func (e *eofThen) RoundTrip(r *http.Request) (*http.Response, error) {
	if e.n > 0 {
		e.n--
		return nil, io.EOF
	}
	return e.next.RoundTrip(r)
}

func TestRetriesADroppedConnection(t *testing.T) {
	srv, _ := scriptedFork(t, []func(int) (int, string){forkReply(submitCall("a", "draft"))})
	snapBody, _ := json.Marshal(map[string]any{"model": "m", "instructions": "s", "tools": []any{}, "input": []any{map[string]any{"role": "user", "content": "q"}}})
	l, _ := NewLedger("r", "", 0)
	c := NewCloud(Options{BaseURL: srv.URL, APIKey: "k", Model: "m", Arm: "T1", Session: "s", Ledger: l, Next: &eofThen{n: 2, next: http.DefaultTransport}})
	pass := func(string, bool) (bool, string) { return true, "" }
	res, err := c.ForkSubmit(context.Background(), &Snapshot{Body: snapBody}, ForkRequest{Prompt: "SUM"}, pass, 1)
	if err != nil || res.Text != "draft" {
		t.Fatalf("res %+v, err %v", res, err)
	}
	if got := l.Rows()[0].Attempt; got != 3 {
		t.Errorf("attempts = %d, want 3", got)
	}
}

func TestForkSubmitStillReportsTheBudgetStopAfterASubmission(t *testing.T) {
	srv, _ := scriptedFork(t, []func(int) (int, string){forkReply(submitCall("a", "draft")), forkReply(submitCall("b", "second"))})
	snapBody, _ := json.Marshal(map[string]any{"model": "m", "instructions": "s", "tools": []any{}, "input": []any{map[string]any{"role": "user", "content": "q"}}})
	l, _ := NewLedger("r", "", 0.000000001)
	c := NewCloud(Options{BaseURL: srv.URL, APIKey: "k", Model: "m", Arm: "T1", Session: "s", Ledger: l})
	reject := func(string, bool) (bool, string) { return false, "too long" }
	res, err := c.ForkSubmit(context.Background(), &Snapshot{Body: snapBody}, ForkRequest{Prompt: "SUM"}, reject, 1)
	if !errors.Is(err, ErrBudget) {
		t.Fatalf("res %+v, err %v", res, err)
	}
}
