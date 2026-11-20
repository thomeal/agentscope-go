package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"strings"

	"github.com/agentscope-ai/agentscope-go/v2/pkg/agentscope/message"
)

// writerRankKey is the mark canonical replay stores on a local call (model package,
// unexported there).
const writerRankKey = "agentscope_writer_rank"

// offlineAPI answers every request with an empty completed response and never dials.
type offlineAPI struct{ requests int }

func (o *offlineAPI) RoundTrip(r *http.Request) (*http.Response, error) {
	o.requests++
	_, _ = io.Copy(io.Discard, r.Body)
	_ = r.Body.Close()
	body := []byte(`{"id":"resp_offline","status":"completed","output":[],"usage":{"input_tokens":0,"output_tokens":0}}`)
	return &http.Response{
		StatusCode: http.StatusOK, Status: "200 OK", Proto: "HTTP/1.1", ProtoMajor: 1, ProtoMinor: 1,
		Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(bytes.NewReader(body)),
		ContentLength: int64(len(body)), Request: r,
	}, nil
}

// checkResult is the offline fidelity check for one prompt.
type checkResult struct {
	Prompt  int               `json:"prompt"`
	Session string            `json:"session"`
	Hashes  map[string]string `json:"hashes"` // variant -> outbound body sha256
	// OffDiffs lists every difference between lib_off's outbound body and the recorded
	// exact body after the same shaping; OnDiffs the same for lib_on against the recorded
	// body after stage 0's canonicalReplay.
	OffDiffs []string `json:"off_diffs"`
	OnDiffs  []string `json:"on_diffs"`
	// Problems are failed checks on the calls, the history and the plumbing.
	Problems []string `json:"problems"`
	// Counts of calls in the request.
	LocalCalls, CloudCalls, LocalRewritten int
	LocalResponses, Blocked                int
}

func (c checkResult) ok() bool {
	return len(c.OffDiffs) == 0 && len(c.OnDiffs) == 0 && len(c.Problems) == 0
}

// checkPrompt builds both variants' outbound bodies without sending anything and compares
// them with the recorded request.
func checkPrompt(p recorded) (checkResult, map[string][]byte, error) {
	res := checkResult{Prompt: p.ID, Session: p.Session, Hashes: map[string]string{}}
	bodies := map[string][]byte{}
	histories := map[string][]*message.Msg{}
	for _, v := range variants {
		api := &offlineAPI{}
		ct := &cloudTransport{base: api, budget: &budget{limit: 1e9}}
		c := &capture{key: jobKey{p.ID, v}, keepBody: true}
		out := replayOne(withCapture(context.Background(), c), p.Req, v, "offline-check", "https://offline.invalid", ct)
		if out.err != nil {
			return res, nil, fmt.Errorf("prompt %d %s: %w", p.ID, v, out.err)
		}
		if api.requests != 1 || c.requests != 1 || out.passed != 1 {
			res.Problems = append(res.Problems, fmt.Sprintf("%s: %d requests reached the API, want 1", v, api.requests))
		}
		if out.blocked < 1 && out.expected > 0 {
			res.Problems = append(res.Problems, fmt.Sprintf("%s: the cloud was never tried while local turns were produced", v))
		}
		res.LocalResponses, res.Blocked = out.expected, out.blocked
		bodies[v], res.Hashes[v], histories[v] = c.body, c.sha, out.history
	}

	exact, err := expectedExact(p.Req)
	if err != nil {
		return res, nil, err
	}
	style := deepCopy(exact)
	canonicalReplay(style)
	bodies["recorded_exact"], _ = json.Marshal(exact)
	bodies["recorded_cloud_style"], _ = json.Marshal(style)

	res.OffDiffs = diffJSON(bodies[libOff], exact)
	res.OnDiffs = diffJSON(bodies[libOn], style)
	if len(res.OffDiffs) == 0 && !bytes.Equal(bodies[libOff], bodies["recorded_exact"]) {
		res.Problems = append(res.Problems, "lib_off equals the recorded body as JSON but not as bytes")
	}
	if len(res.OnDiffs) == 0 && !bytes.Equal(bodies[libOn], bodies["recorded_cloud_style"]) {
		res.Problems = append(res.Problems, "lib_on equals the cloud_style body as JSON but not as bytes")
	}
	res.Problems = append(res.Problems, checkCalls(&res, p.Req, bodies[libOn])...)
	res.Problems = append(res.Problems, checkHistories(p.Req, histories)...)
	return res, bodies, nil
}

// expectedExact is the recorded request as Amendment 8's exact variant sent it: the body
// stage 0's transport sent, plus the output cap. prepareResponses is applied again; on a
// recorded body it changes nothing.
func expectedExact(raw []byte) (map[string]any, error) {
	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		return nil, err
	}
	prepareResponses(body)
	body["max_output_tokens"] = maxOutputTokens
	// Round-trip so numbers have the type the outbound body decodes to.
	b, _ := json.Marshal(body)
	var out map[string]any
	return out, json.Unmarshal(b, &out)
}

// checkCalls compares every call's arguments in lib_on's outbound body with the recording:
// local calls must byte-equal stage 0's rewrite, and the cloud's calls must be untouched.
func checkCalls(res *checkResult, recordedRaw, onBody []byte) []string {
	var problems []string
	recCalls := callArgs(recordedRaw)
	onCalls := callArgs(onBody)
	for id, rec := range recCalls {
		got, ok := onCalls[id]
		if !ok {
			problems = append(problems, fmt.Sprintf("call %s is missing from lib_on", id))
			continue
		}
		if strings.HasPrefix(id, localCallPrefix) {
			res.LocalCalls++
			want, canon := canonicalJSON(rec)
			if !canon {
				want = rec
			}
			if got != want {
				problems = append(problems, fmt.Sprintf("local call %s: lib_on arguments differ from stage 0's rewrite", id))
			}
			if got != rec {
				res.LocalRewritten++
			}
		} else {
			res.CloudCalls++
			if got != rec {
				problems = append(problems, fmt.Sprintf("cloud call %s: lib_on changed its arguments", id))
			}
		}
	}
	if len(onCalls) != len(recCalls) {
		problems = append(problems, fmt.Sprintf("lib_on has %d calls, the recording %d", len(onCalls), len(recCalls)))
	}
	return problems
}

func callArgs(raw []byte) map[string]string {
	var body struct {
		Input []map[string]any `json:"input"`
	}
	_ = json.Unmarshal(raw, &body)
	out := map[string]string{}
	for _, it := range body.Input {
		if it["type"] == "function_call" {
			id, _ := it["call_id"].(string)
			args, _ := it["arguments"].(string)
			out[id] = args
		}
	}
	return out
}

// checkHistories checks the stored history: lib_on marks every local call and no cloud
// call, lib_off marks nothing, and in both the stored arguments are as recorded (the
// rewrite is only in the copy sent to the cloud).
func checkHistories(recordedRaw []byte, histories map[string][]*message.Msg) []string {
	var problems []string
	rec := callArgs(recordedRaw)
	for _, v := range variants {
		for _, m := range histories[v] {
			for _, b := range m.Content {
				call, ok := b.(message.ToolCallBlock)
				if !ok {
					continue
				}
				wire := call.ID
				if id, ok := call.Extra["call_id"].(string); ok {
					wire = id
				}
				local := strings.HasPrefix(wire, localCallPrefix)
				rank, marked := call.Extra[writerRankKey]
				switch {
				case v == libOn && local && (!marked || rank != 1):
					problems = append(problems, fmt.Sprintf("%s: local call %s is not marked rank 1", v, wire))
				case (v == libOff || !local) && marked:
					problems = append(problems, fmt.Sprintf("%s: call %s is marked", v, wire))
				}
				if call.Input != rec[wire] {
					problems = append(problems, fmt.Sprintf("%s: stored arguments of %s were changed", v, wire))
				}
			}
		}
	}
	return problems
}

// diffJSON lists every difference between the outbound bytes and the expected body, by
// JSON path.
func diffJSON(got []byte, want map[string]any) []string {
	var g any
	if err := json.Unmarshal(got, &g); err != nil {
		return []string{"outbound body is not JSON"}
	}
	var out []string
	diffValue("$", g, want, &out)
	return out
}

func diffValue(path string, got, want any, out *[]string) {
	switch w := want.(type) {
	case map[string]any:
		g, ok := got.(map[string]any)
		if !ok {
			*out = append(*out, fmt.Sprintf("%s: got %s, want an object", path, short(got)))
			return
		}
		keys := map[string]bool{}
		for k := range w {
			keys[k] = true
		}
		for k := range g {
			keys[k] = true
		}
		names := make([]string, 0, len(keys))
		for k := range keys {
			names = append(names, k)
		}
		sort.Strings(names)
		for _, k := range names {
			gv, gok := g[k]
			wv, wok := w[k]
			switch {
			case !gok:
				*out = append(*out, fmt.Sprintf("%s.%s: missing, want %s", path, k, short(wv)))
			case !wok:
				*out = append(*out, fmt.Sprintf("%s.%s: extra field %s", path, k, short(gv)))
			default:
				diffValue(path+"."+k, gv, wv, out)
			}
		}
	case []any:
		g, ok := got.([]any)
		if !ok {
			*out = append(*out, fmt.Sprintf("%s: got %s, want a list", path, short(got)))
			return
		}
		if len(g) != len(w) {
			*out = append(*out, fmt.Sprintf("%s: %d items, want %d", path, len(g), len(w)))
		}
		for i := range min(len(g), len(w)) {
			diffValue(fmt.Sprintf("%s[%d]", path, i), g[i], w[i], out)
		}
	default:
		if !reflect.DeepEqual(got, want) {
			*out = append(*out, fmt.Sprintf("%s: got %s, want %s", path, short(got), short(want)))
		}
	}
}

func short(v any) string {
	b, _ := json.Marshal(v)
	s := string(b)
	if len(s) > 120 {
		s = s[:120] + "..."
	}
	return s
}

func deepCopy(m map[string]any) map[string]any {
	b, _ := json.Marshal(m)
	var out map[string]any
	_ = json.Unmarshal(b, &out)
	return out
}

// loadPrompts reads prompts.jsonl and keeps Amendment 8's first-write prompts (group
// "shared"), by id.
func loadPrompts(path string) ([]recorded, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var out []recorded
	for n, line := range bytes.Split(data, []byte("\n")) {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var r recorded
		if err := json.Unmarshal(line, &r); err != nil {
			return nil, fmt.Errorf("%s line %d: %w", path, n+1, err)
		}
		if r.Group == "shared" {
			out = append(out, r)
		}
	}
	slices.SortFunc(out, func(a, b recorded) int { return a.ID - b.ID })
	return out, nil
}

// checkAll runs the offline check on every prompt. With dir set, it writes the bodies and
// a summary there. It returns the outbound body hash of every prompt and variant.
func checkAll(prompts []recorded, dir string, w io.Writer) (map[jobKey]string, bool, error) {
	hashes := map[jobKey]string{}
	all := true
	var results []checkResult
	if dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, false, err
		}
	}
	for _, p := range prompts {
		res, bodies, err := checkPrompt(p)
		if err != nil {
			return nil, false, err
		}
		results = append(results, res)
		for v, h := range res.Hashes {
			hashes[jobKey{p.ID, v}] = h
		}
		status := "ok"
		if !res.ok() {
			status, all = "DIFFERS", false
		}
		fmt.Fprintf(w, "prompt %2d %-26s %s  local responses %d, local calls %d (%d rewritten), cloud calls %d, blocked cloud tries %d\n",
			p.ID, p.Session, status, res.LocalResponses, res.LocalCalls, res.LocalRewritten, res.CloudCalls, res.Blocked)
		for _, d := range res.OffDiffs {
			fmt.Fprintf(w, "    lib_off vs exact: %s\n", d)
		}
		for _, d := range res.OnDiffs {
			fmt.Fprintf(w, "    lib_on vs cloud_style: %s\n", d)
		}
		for _, d := range res.Problems {
			fmt.Fprintf(w, "    problem: %s\n", d)
		}
		if dir != "" {
			for name, b := range bodies {
				if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("p%02d-%s.json", p.ID, name)), b, 0o644); err != nil {
					return nil, false, err
				}
			}
		}
	}
	if dir != "" {
		b, _ := json.MarshalIndent(results, "", "  ")
		if err := os.WriteFile(filepath.Join(dir, "summary.json"), b, 0o644); err != nil {
			return nil, false, err
		}
	}
	return hashes, all, nil
}
