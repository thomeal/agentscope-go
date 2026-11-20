package main

import (
	"bytes"
	"context"
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sirupsen/logrus"

	"github.com/agentscope-ai/agentscope-go/v2/pkg/agentscope/message"
)

// The cross-checks read stage 0's material and skip without it.
//
// STAGE0_DIR is a checkout of thomeal/agentscope-go at eval/stage0, which holds stage 0's
// source. STAGE0_REPLAY_DIR is a directory holding Amendment 8's prompts.jsonl and
// replay.jsonl (stage 0's runs/replay-runaway); that data is not published.
const (
	stage0DirEnv    = "STAGE0_DIR"
	stage0ReplayEnv = "STAGE0_REPLAY_DIR"
)

// stage0Cloud returns stage 0's lab/cloud.go, or skips.
func stage0Cloud(t *testing.T) string {
	t.Helper()
	dir := os.Getenv(stage0DirEnv)
	path := filepath.Join(dir, "experiments", "stage0", "lab", "cloud.go")
	if _, err := os.Stat(path); dir == "" || err != nil {
		t.Skip("stage 0 source not found (set STAGE0_DIR)")
	}
	return path
}

// stage0Replay returns a file of Amendment 8's replay data, or skips saying why.
func stage0Replay(t *testing.T, name, unpublished string) string {
	t.Helper()
	dir := os.Getenv(stage0ReplayEnv)
	path := filepath.Join(dir, name)
	if _, err := os.Stat(path); dir == "" || err != nil {
		t.Skipf("%s; to run this check, set %s to a directory holding %s", unpublished, stage0ReplayEnv, name)
	}
	return path
}

var testTools = []any{
	map[string]any{"type": "function", "name": "list_files", "description": "List files.",
		"parameters": map[string]any{"type": "object", "properties": map[string]any{}, "additionalProperties": false}},
	map[string]any{"type": "function", "name": "read_file", "description": "Read one file.",
		"parameters": map[string]any{"type": "object", "properties": map[string]any{"path": map[string]any{"type": "string"}}, "required": []any{"path"}, "additionalProperties": false}},
	map[string]any{"type": "function", "name": "write_file", "description": "Replace a whole file.",
		"parameters": map[string]any{"type": "object", "properties": map[string]any{"path": map[string]any{"type": "string"}, "content": map[string]any{"type": "string"}}, "required": []any{"path", "content"}, "additionalProperties": false}},
	map[string]any{"type": "function", "name": "run_tests", "description": "Run the tests.",
		"parameters": map[string]any{"type": "object", "properties": map[string]any{}, "additionalProperties": false}},
}

func fc(callID, id, name, args string) map[string]any {
	m := map[string]any{"type": "function_call", "call_id": callID, "name": name, "arguments": args}
	if id != "" {
		m["id"] = id
	}
	return m
}

func out(callID, output string) map[string]any {
	return map[string]any{"type": "function_call_output", "call_id": callID, "output": output}
}

func msg(role, text string) map[string]any { return map[string]any{"role": role, "content": text} }

func reasoning(id, enc string, summary ...string) map[string]any {
	parts := []any{}
	for _, s := range summary {
		parts = append(parts, map[string]any{"type": "summary_text", "text": s})
	}
	return map[string]any{"type": "reasoning", "id": id, "encrypted_content": enc, "summary": parts, "content": []any{}}
}

// syntheticItems is a recorded session with every item shape the 11 prompts hold: two user
// messages in a row, local parallel calls, local text with a call, a local final text, the
// cloud's reasoning, parallel calls and text, and a request ending on the cloud's results.
func syntheticItems() []any {
	return []any{
		msg("user", "[Previous context summary]: earlier work"),
		msg("user", "Add Foo."),
		fc("chatcmpl-tool-1", "", "read_file", `{"path": "a.go"}`),
		fc("chatcmpl-tool-2", "", "read_file", `{"path": "b.go"}`),
		out("chatcmpl-tool-1", "package a"),
		out("chatcmpl-tool-2", "package b"),
		msg("assistant", "I see."),
		fc("chatcmpl-tool-3", "", "write_file", `{"path": "a.go", "content": "package a\n\nfunc Foo() bool { return 1 < 2 && true }\n"}`),
		out("chatcmpl-tool-3", "wrote a.go (51 bytes)"),
		fc("chatcmpl-tool-4", "", "run_tests", `{}`),
		out("chatcmpl-tool-4", "ok"),
		msg("assistant", "Done."),
		msg("user", "Add Bar."),
		reasoning("rs_1", "enc1"),
		fc("fc_1", "fc_1", "list_files", `{}`),
		fc("fc_2", "fc_2", "read_file", `{"path":"a.go"}`),
		out("fc_1", "a.go\nb.go"),
		out("fc_2", "package a <&>"),
		reasoning("rs_2", "enc2", "planning"),
		msg("assistant", "Bar needs care."),
		msg("user", "Add Baz."),
		reasoning("rs_3", "enc3"),
		fc("fc_3", "fc_3", "read_file", `{"path":"b.go"}`),
		out("fc_3", "package b"),
	}
}

// syntheticRequest is the body stage 0's transport would have sent for items.
func syntheticRequest(t *testing.T, items []any, mutate ...func(map[string]any)) []byte {
	t.Helper()
	req := map[string]any{
		"model": "gpt-6-luna", "instructions": "You are a coding assistant.", "stream": false,
		"store": false, "include": []any{"reasoning.encrypted_content"}, "tools": testTools, "input": items,
	}
	for _, m := range mutate {
		m(req)
	}
	b, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestConvertSyntheticSession(t *testing.T) {
	conv, err := convert(syntheticRequest(t, syntheticItems()))
	if err != nil {
		t.Fatal(err)
	}
	type want struct {
		kind   stepKind
		author author
		blocks int
	}
	wants := []want{
		{stepUser, 0, 0}, {stepUser, 0, 0},
		{stepResponse, authorLocal, 2}, {stepResults, 0, 2},
		{stepResponse, authorLocal, 2}, {stepResults, 0, 1},
		{stepResponse, authorLocal, 1}, {stepResults, 0, 1},
		{stepResponse, authorLocal, 1}, // "Done.", a text-only response in a local reply
		{stepUser, 0, 0},
		{stepResponse, authorCloud, 3}, {stepResults, 0, 2},
		{stepResponse, authorCloud, 2}, // reasoning then text
		{stepUser, 0, 0},
		{stepResponse, authorCloud, 2}, {stepResults, 0, 1},
	}
	if len(conv.steps) != len(wants) {
		t.Fatalf("got %d steps, want %d", len(conv.steps), len(wants))
	}
	for i, w := range wants {
		s := conv.steps[i]
		if s.kind != w.kind || (w.kind == stepResponse && s.author != w.author) || len(s.blocks) != w.blocks {
			t.Errorf("step %d: kind %d author %s blocks %d, want kind %d author %s blocks %d", i, s.kind, s.author, len(s.blocks), w.kind, w.author, w.blocks)
		}
	}
	if n := len(conv.localResponses()); n != 4 {
		t.Errorf("local responses = %d, want 4", n)
	}
	// A cloud call keeps its item id as the block ID and its call_id in Extra; a local call's
	// block ID is its call_id; a result is keyed by its call's block ID.
	cloud := conv.steps[10].blocks[1].(message.ToolCallBlock)
	if cloud.ID != "fc_1" || cloud.Extra["call_id"] != "fc_1" {
		t.Errorf("cloud call block = %+v", cloud)
	}
	local := conv.steps[2].blocks[0].(message.ToolCallBlock)
	if local.ID != "chatcmpl-tool-1" || local.Extra != nil || local.Input != `{"path": "a.go"}` {
		t.Errorf("local call block = %+v", local)
	}
	res := conv.steps[3].blocks[1].(message.ToolResultBlock)
	if res.ID != "chatcmpl-tool-2" || res.Name != "read_file" || res.Output != "package b" {
		t.Errorf("result block = %+v", res)
	}
	think := conv.steps[12].blocks[0].(message.ThinkingBlock)
	if think.Thinking != "planning" || think.Extra["responses_item"].(map[string]any)["encrypted_content"] != "enc2" {
		t.Errorf("reasoning block = %+v", think)
	}
	if len(conv.tools) != 4 || conv.tools[2].Function.Name != "write_file" {
		t.Errorf("tools = %+v", conv.tools)
	}
}

func TestConvertRejectsWhatItCannotReproduce(t *testing.T) {
	items := syntheticItems
	cases := map[string][]byte{
		"top-level field": syntheticRequest(t, items(), func(r map[string]any) { r["reasoning"] = map[string]any{"effort": "low"} }),
		"store true":      syntheticRequest(t, items(), func(r map[string]any) { r["store"] = true }),
		"no include":      syntheticRequest(t, items(), func(r map[string]any) { delete(r, "include") }),
		"stream":          syntheticRequest(t, items(), func(r map[string]any) { r["stream"] = true }),
		"tool field": syntheticRequest(t, items(), func(r map[string]any) {
			r["tools"] = []any{map[string]any{"type": "function", "name": "x", "description": "", "parameters": map[string]any{}, "strict": true}}
		}),
		"item type":        syntheticRequest(t, append(items(), map[string]any{"type": "web_search_call"})),
		"item field":       syntheticRequest(t, append(items()[:2], map[string]any{"role": "user", "content": "x", "name": "n"})),
		"list content":     syntheticRequest(t, append(items()[:2], map[string]any{"role": "user", "content": []any{}})),
		"orphan output":    syntheticRequest(t, append(items(), out("fc_9", "x"))),
		"unanswered call":  syntheticRequest(t, append(items(), fc("fc_9", "fc_9", "run_tests", "{}"))),
		"local item id":    syntheticRequest(t, []any{msg("user", "x"), fc("chatcmpl-tool-9", "chatcmpl-tool-9", "run_tests", "{}"), out("chatcmpl-tool-9", "ok")}),
		"call_..._id":      syntheticRequest(t, []any{msg("user", "x"), fc("call_9", "", "run_tests", "{}"), out("call_9", "ok")}),
		"text-only reply":  syntheticRequest(t, []any{msg("user", "x"), msg("assistant", "hello")}),
		"mixed response":   syntheticRequest(t, []any{msg("user", "x"), reasoning("rs_9", "e"), fc("chatcmpl-tool-9", "", "run_tests", "{}"), out("chatcmpl-tool-9", "ok")}),
		"repeated call_id": syntheticRequest(t, []any{msg("user", "x"), fc("fc_9", "fc_9", "run_tests", "{}"), out("fc_9", "ok"), fc("fc_9", "fc_9", "run_tests", "{}"), out("fc_9", "ok")}),
	}
	for name, raw := range cases {
		if _, err := convert(raw); err == nil {
			t.Errorf("%s: convert accepted it", name)
		}
	}
}

// TestSyntheticFidelity runs the offline check on the synthetic session: lib_off must send
// the recorded body byte for byte, lib_on must equal stage 0's cloud_style rewrite.
func TestSyntheticFidelity(t *testing.T) {
	p := recorded{ID: 3, Session: "synthetic", Group: "shared", Req: syntheticRequest(t, syntheticItems())}
	res, bodies, err := checkPrompt(p)
	if err != nil {
		t.Fatal(err)
	}
	if !res.ok() {
		t.Fatalf("off %v\non %v\nproblems %v", res.OffDiffs, res.OnDiffs, res.Problems)
	}
	if res.LocalCalls != 4 || res.CloudCalls != 3 || res.LocalRewritten != 3 {
		t.Errorf("calls: local %d (%d rewritten), cloud %d", res.LocalCalls, res.LocalRewritten, res.CloudCalls)
	}
	on := callArgs(bodies[libOn])
	if got := on["chatcmpl-tool-3"]; got != `{"content":"package a\n\nfunc Foo() bool { return 1 < 2 && true }\n","path":"a.go"}` {
		t.Errorf("lib_on local write = %s", got)
	}
	if got := callArgs(bodies[libOff])["chatcmpl-tool-3"]; !strings.HasPrefix(got, `{"path": "a.go", `) {
		t.Errorf("lib_off local write = %s", got)
	}
	// The local calls' item ids were dropped by the shared shaping, the cloud's kept.
	var body struct {
		Input []map[string]any `json:"input"`
	}
	_ = json.Unmarshal(bodies[libOff], &body)
	for _, it := range body.Input {
		if it["type"] != "function_call" {
			continue
		}
		id, has := it["id"].(string)
		if strings.HasPrefix(it["call_id"].(string), "fc") != (has && id == it["call_id"]) {
			t.Errorf("item id handling: %v", it)
		}
	}
}

// TestFidelityDetectsDifferences makes sure the comparison is not vacuous.
func TestFidelityDetectsDifferences(t *testing.T) {
	want, err := expectedExact(syntheticRequest(t, syntheticItems()))
	if err != nil {
		t.Fatal(err)
	}
	altered := deepCopy(want)
	altered["input"].([]any)[6].(map[string]any)["content"] = "I saw."
	altered["tools"].([]any)[0].(map[string]any)["description"] = "List the files."
	delete(altered, "store")
	b, _ := json.Marshal(altered)
	diffs := diffJSON(b, want)
	if len(diffs) != 3 {
		t.Fatalf("diffs = %v", diffs)
	}
}

func TestVariantPlumbing(t *testing.T) {
	raw := syntheticRequest(t, syntheticItems())
	for _, v := range variants {
		api := &offlineAPI{}
		c := &capture{key: jobKey{0, v}, keepBody: true}
		res := replayOne(withCapture(context.Background(), c), raw, v, "k", "https://offline.invalid", &cloudTransport{base: api, budget: &budget{limit: 1}})
		if res.err != nil {
			t.Fatalf("%s: %v", v, res.err)
		}
		if api.requests != 1 || res.passed != 1 || res.blocked < 1 || res.served != 4 {
			t.Errorf("%s: api %d passed %d blocked %d served %d", v, api.requests, res.passed, res.blocked, res.served)
		}
		marked := 0
		for _, m := range res.history {
			for _, b := range m.Content {
				if call, ok := b.(message.ToolCallBlock); ok {
					if _, ok := call.Extra[writerRankKey]; ok {
						marked++
					}
				}
			}
		}
		want := map[string]int{libOff: 0, libOn: 4}[v]
		if marked != want {
			t.Errorf("%s: %d marked calls, want %d", v, marked, want)
		}
		// One assistant message per reply, as UnifiedAgent stores it.
		roles := ""
		for _, m := range res.history {
			roles += string(m.Role[0])
		}
		if roles != "suuauaua" {
			t.Errorf("%s: history roles %q", v, roles)
		}
	}
	if res := replayOne(context.Background(), raw, "other", "k", "https://offline.invalid", &offlineAPI{}); res.err == nil {
		t.Error("an unknown variant was accepted")
	}
}

func TestShapeDropsOnlyNonFCIDs(t *testing.T) {
	raw := []byte(`{"input":[{"type":"function_call","id":"fc_1","call_id":"fc_1"},{"type":"function_call","id":"chatcmpl-tool-2","call_id":"chatcmpl-tool-2"},{"type":"function_call","id":"call_3","call_id":"call_3"},{"type":"message","id":"msg_4"}],"model":"m"}`)
	b, err := shape(raw)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"include":["reasoning.encrypted_content"],"input":[{"call_id":"fc_1","id":"fc_1","type":"function_call"},{"call_id":"chatcmpl-tool-2","type":"function_call"},{"call_id":"call_3","type":"function_call"},{"id":"msg_4","type":"message"}],"max_output_tokens":8000,"model":"m","store":false}`
	if string(b) != want {
		t.Errorf("shape =\n%s\nwant\n%s", b, want)
	}
}

// TestStage0CopiesAreVerbatim compares the copied helpers with stage 0's source.
func TestStage0CopiesAreVerbatim(t *testing.T) {
	path := stage0Cloud(t)
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	mine, err := os.ReadFile("stage0ref.go")
	if err != nil {
		t.Fatal(err)
	}
	// A checkout may have either line ending; the comparison is of the text.
	lf := func(b []byte) []byte { return bytes.ReplaceAll(b, []byte("\r\n"), []byte("\n")) }
	theirs, ours := decls(t, lf(src)), decls(t, lf(mine))
	for _, name := range []string{"prepareResponses", "localCallPrefix", "canonicalReplay", "canonicalJSON"} {
		if theirs[name] == "" || theirs[name] != ours[name] {
			t.Errorf("%s differs from %s", name, path)
		}
	}
}

func decls(t *testing.T, src []byte) map[string]string {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "", src, parser.ParseComments)
	if err != nil {
		t.Fatal(err)
	}
	text := func(n ast.Node, doc *ast.CommentGroup) string {
		start := n.Pos()
		if doc != nil {
			start = doc.Pos()
		}
		return string(src[fset.Position(start).Offset:fset.Position(n.End()).Offset])
	}
	out := map[string]string{}
	for _, d := range f.Decls {
		switch d := d.(type) {
		case *ast.FuncDecl:
			out[d.Name.Name] = text(d, d.Doc)
		case *ast.GenDecl:
			for _, s := range d.Specs {
				if vs, ok := s.(*ast.ValueSpec); ok && len(vs.Names) == 1 {
					out[vs.Names[0].Name] = text(d, d.Doc)
				}
			}
		}
	}
	return out
}

// TestRecordedPromptsFidelity is the offline check on the real prompts.
func TestRecordedPromptsFidelity(t *testing.T) {
	prompts, err := loadPrompts(stage0Replay(t, "prompts.jsonl", "stage 0's recorded prompts are not published"))
	if err != nil {
		t.Fatal(err)
	}
	if err := checkSet(prompts); err != nil {
		t.Fatal(err)
	}
	var log strings.Builder
	hashes, ok, err := checkAll(prompts, "", &log)
	if err != nil {
		t.Fatal(err)
	}
	if !ok || len(hashes) != 22 {
		t.Fatalf("fidelity check failed:\n%s", log.String())
	}
}

func TestMain(m *testing.M) {
	logrus.SetLevel(logrus.ErrorLevel)
	os.Exit(m.Run())
}
