package lab

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"sync"

	"github.com/agentscope-ai/agentscope-go/v2/pkg/agentscope/model"
)

// Cloud is the OpenAI Responses transport for one participant in one session.
// It keeps the last completed main exchange, which is what a fork replays.
type Cloud struct {
	o Options

	mu       sync.Mutex
	lastBody []byte
	lastResp []byte
	mainSeen int
}

// NewCloud returns a transport. The zero MaxAttempts and Prices get defaults.
func NewCloud(o Options) *Cloud {
	if o.Prices == (Prices{}) {
		o.Prices = Luna
	}
	return &Cloud{o: o}
}

// ChatModel builds the real Responses adapter over this transport.
func (c *Cloud) ChatModel() (model.ChatModel, error) {
	return model.NewOpenAIResponseModel(&model.OpenAIResponseConfig{
		APIKey:     c.o.APIKey,
		BaseURL:    c.o.BaseURL,
		Model:      c.o.Model,
		HTTPClient: &http.Client{Transport: c},
	})
}

// RoundTrip implements http.RoundTripper for adapter traffic.
func (c *Cloud) RoundTrip(r *http.Request) (*http.Response, error) {
	ctx := r.Context()
	raw, err := io.ReadAll(r.Body)
	_ = r.Body.Close()
	if err != nil {
		return nil, err
	}
	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		return nil, fmt.Errorf("lab: request is not JSON: %w", err)
	}
	prepareResponses(body)
	if c.o.CanonicalReplay {
		canonicalReplay(body)
	}
	if limit, ok := ctx.Value(limitKey).(int); ok && limit > 0 {
		body["max_output_tokens"] = limit
	}
	role := roleOf(ctx, body)
	ex, out, err := c.send(ctx, r.URL.String(), r.Header, body, role)
	if err != nil {
		return nil, err
	}
	if role == RoleMain && ex.status == http.StatusOK {
		var parsed map[string]any
		if json.Unmarshal(ex.body, &parsed) == nil && parsed["status"] == "completed" {
			c.mu.Lock()
			c.lastBody, c.lastResp = out, ex.body
			c.mainSeen++
			c.mu.Unlock()
		}
	}
	return rebuilt(r, ex), nil
}

// prepareResponses applies the stateless settings every Responses call needs.
func prepareResponses(body map[string]any) {
	body["store"] = false
	body["include"] = []any{"reasoning.encrypted_content"}
	items, _ := body["input"].([]any)
	for _, it := range items {
		m, ok := it.(map[string]any)
		if !ok || m["type"] != "function_call" {
			continue
		}
		// The adapter sends a call ID as the item id. The API only accepts ids
		// starting with "fc", so calls made by the local model would get a 400
		// when a single shared history returns to the cloud. Dropping the id is
		// deterministic, which keeps the cached prefix stable.
		if id, _ := m["id"].(string); !strings.HasPrefix(id, "fc") {
			delete(m, "id")
		}
	}
}

// localCallPrefix starts the call ids the local server makes.
const localCallPrefix = "chatcmpl"

// canonicalReplay rewrites the arguments of local's calls the way the cloud writes its own:
// compact JSON with sorted keys. Sent as local wrote them, they make the cloud run away on
// its next whole-file write (prereg.md, Amendment 8). The cloud's own calls stay as they
// are, and the rewrite is deterministic, so a cached prefix stays stable. Arguments that do
// not parse stay too, because they have no canonical form.
func canonicalReplay(body map[string]any) {
	items, _ := body["input"].([]any)
	for _, it := range items {
		m, ok := it.(map[string]any)
		if !ok || m["type"] != "function_call" {
			continue
		}
		if id, _ := m["call_id"].(string); !strings.HasPrefix(id, localCallPrefix) {
			continue
		}
		args, _ := m["arguments"].(string)
		if out, ok := canonicalJSON(args); ok {
			m["arguments"] = out
		}
	}
}

// canonicalJSON re-encodes one JSON value with sorted keys and no spaces. It keeps numbers
// as written and does not escape HTML, so only the form changes.
func canonicalJSON(s string) (string, bool) {
	dec := json.NewDecoder(strings.NewReader(s))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil || dec.More() {
		return "", false
	}
	var out bytes.Buffer
	enc := json.NewEncoder(&out)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return "", false
	}
	return strings.TrimSuffix(out.String(), "\n"), true
}

func (c *Cloud) send(ctx context.Context, url string, hdr http.Header, body map[string]any, role string) (exchange, []byte, error) {
	if c.o.Ledger != nil && c.o.Ledger.OverBudget() {
		return exchange{}, nil, ErrBudget
	}
	applyEffort(ctx, body)
	out, err := json.Marshal(body)
	if err != nil {
		return exchange{}, nil, err
	}
	ex, err := post(ctx, &c.o, url, hdr, out)
	row := Row{
		Arm: c.o.Arm, Session: c.o.Session, Side: SideCloud, Role: role, Model: c.o.Model,
		HTTP: ex.status, WaitMS: ex.wait, LatencyMS: ex.ms, Attempt: ex.tries,
		CanaryHits: countCanaries(out, c.o.Canaries),
	}
	if err != nil {
		row.Error = err.Error()
		c.record(row)
		return ex, out, err
	}
	fillCloudRow(&row, ex.body, c.o.Prices)
	c.record(row)
	if c.o.Dump != nil {
		c.o.Dump(c.o.Arm, c.o.Session, SideCloud, role, out, ex.body)
	}
	return ex, out, nil
}

// applyEffort inserts the configuration update just before the last input item, which
// is the prompt of a fork or a brief. Everything before it stays byte-identical.
func applyEffort(ctx context.Context, body map[string]any) {
	effort, _ := ctx.Value(effortKey).(string)
	items, _ := body["input"].([]any)
	if effort == "" || len(items) == 0 {
		return
	}
	update := map[string]any{"type": "configuration_update", "reasoning": map[string]any{"effort": effort}}
	last := len(items) - 1
	out := make([]any, 0, len(items)+1)
	out = append(out, items[:last]...)
	out = append(out, update, items[last])
	body["input"] = out
}

func (c *Cloud) record(r Row) {
	if c.o.Ledger != nil {
		c.o.Ledger.Add(r)
	}
}

func fillCloudRow(row *Row, respBody []byte, p Prices) {
	var parsed map[string]any
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		row.Error = "response is not JSON"
		row.UsageMissing = true
		return
	}
	row.Status, _ = parsed["status"].(string)
	if inc, ok := parsed["incomplete_details"].(map[string]any); ok {
		row.Incomplete, _ = inc["reason"].(string)
	}
	if row.HTTP >= 400 {
		row.Error = redact(string(respBody))
		row.Overflow = model.IsContextLimitExceeded(errors.New(string(respBody)))
		return
	}
	u, ok := parsed["usage"].(map[string]any)
	if !ok {
		row.UsageMissing = true
		return
	}
	row.Input, row.Output = intOf(u["input_tokens"]), intOf(u["output_tokens"])
	if d, ok := u["input_tokens_details"].(map[string]any); ok {
		row.Cached, row.Write = intOf(d["cached_tokens"]), intOf(d["cache_write_tokens"])
	}
	if d, ok := u["output_tokens_details"].(map[string]any); ok {
		row.Reasoning = intOf(d["reasoning_tokens"])
	}
	row.CostUSD = p.Cost(row.Input, row.Cached, row.Write, row.Output)
}

// Snapshot is a deep copy of the last completed main exchange.
type Snapshot struct {
	Body   []byte
	Output []any
}

// Snapshot returns the fork point, or false before the first completed call.
func (c *Cloud) Snapshot() (*Snapshot, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.lastBody == nil {
		return nil, false
	}
	var resp map[string]any
	if err := json.Unmarshal(c.lastResp, &resp); err != nil {
		return nil, false
	}
	output, _ := resp["output"].([]any)
	return &Snapshot{Body: append([]byte(nil), c.lastBody...), Output: output}, true
}

// LastInputTokens returns the input tokens of the last completed main call.
func (c *Cloud) LastInputTokens() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	var resp map[string]any
	if json.Unmarshal(c.lastResp, &resp) != nil {
		return 0
	}
	u, _ := resp["usage"].(map[string]any)
	return intOf(u["input_tokens"])
}

// ForkRequest asks for one summary on top of a snapshot.
type ForkRequest struct {
	Role   string
	Prompt string
	Limit  int
}

// ForkResult reports one fork. A fork that did not complete has Completed set
// to false and must not be applied, whatever text it returned.
type ForkResult struct {
	Text      string
	Status    string
	Completed bool
	Input     int
	Cached    int
	Write     int
	Output    int
	Reasoning int
}

// Fork replays the snapshot plus one user message. The prefix up to the last
// main request is byte-identical, tools stay in place and tool_choice "none"
// stops the fork from calling them. It never touches the snapshot.
func (c *Cloud) Fork(ctx context.Context, snap *Snapshot, req ForkRequest) (ForkResult, error) {
	var body map[string]any
	if err := json.Unmarshal(snap.Body, &body); err != nil {
		return ForkResult{}, err
	}
	input, _ := body["input"].([]any)
	for _, it := range snap.Output {
		input = append(input, stripNulls(it))
	}
	input = append(input, map[string]any{"role": "user", "content": req.Prompt})
	body["input"] = input
	body["tool_choice"] = "none"
	if req.Limit > 0 {
		body["max_output_tokens"] = req.Limit
	}
	role := req.Role
	if role == "" {
		role = RoleForkB
	}
	hdr := http.Header{
		"Content-Type":  {"application/json"},
		"Authorization": {"Bearer " + c.o.APIKey},
	}
	url := strings.TrimRight(c.o.BaseURL, "/") + "/v1/responses"
	ex, _, err := c.send(ctx, url, hdr, body, role)
	if err != nil {
		return ForkResult{}, err
	}
	if ex.status >= 400 {
		return ForkResult{}, fmt.Errorf("lab: fork returned HTTP %d: %s", ex.status, redact(string(ex.body)))
	}
	return parseFork(ex.body)
}

// SubmitTool is the tool a checked fork answers through.
const SubmitTool = "submit_summary"

const (
	submitSearchID = "submit_tool"
	submitAsk      = "\n\nWhen it is ready, submit it with " + SubmitTool + " instead of replying with it."
)

// submitToolItems add the tool after the prefix, as a client tool search result, which is
// how Codex adds a tool late. Measured on real forks of 51K and 57K tokens, 98% of the
// input stays cached this way. The same tool at the end of the top-level list caches
// nothing, because that list comes before the input.
func submitToolItems() []any {
	tool := map[string]any{
		"type": "function", "name": SubmitTool,
		"description": "Submit the finished summary. The host measures it and replies whether it was accepted. If it is rejected, fix it and submit the whole summary again.",
		"strict":      true,
		"parameters": map[string]any{
			"type":                 "object",
			"properties":           map[string]any{"summary": map[string]any{"type": "string", "description": "The complete summary"}},
			"required":             []any{"summary"},
			"additionalProperties": false,
		},
	}
	return []any{
		map[string]any{"type": "tool_search_call", "call_id": submitSearchID, "execution": "client", "status": "completed", "arguments": map[string]any{"query": "submit summary"}},
		map[string]any{"type": "tool_search_output", "call_id": submitSearchID, "execution": "client", "status": "completed", "tools": []any{tool}},
	}
}

// Check decides whether a submitted text stands, and says why not. first is true for the
// first submission of a fork.
type Check func(text string, first bool) (ok bool, feedback string)

// SubmitResult reports a checked fork. Completed means Text is a whole submission, and
// Accepted that it passed the check. A completed result that was not accepted is the last
// submission, which the caller has to bring within its limit.
type SubmitResult struct {
	Text      string
	Completed bool
	Accepted  bool
	Bounces   int
	// Strays counts rounds that made no submission: a call to another tool, or an empty
	// summary. The fork inherits the session's tools, so a model can still call one.
	Strays int
}

// strayFeedback answers a call that submitted nothing.
const strayFeedback = "Not accepted: nothing was submitted. No other tool is available here. Submit the whole summary with " + SubmitTool + "."

// ForkSubmit is a fork that answers through SubmitTool. The host checks each submission
// and sends a rejected one back up to maxBounces times, in the fork's own context, so the
// model can choose again with the history in front of it. Every round is a fork row in
// the ledger, and at most maxBounces rounds follow the first. Once a submission exists, a
// later round's failure only ends the loop, and only a budget stop or a cancellation is an
// error then.
func (c *Cloud) ForkSubmit(ctx context.Context, snap *Snapshot, req ForkRequest, check Check, maxBounces int) (SubmitResult, error) {
	var body map[string]any
	if err := json.Unmarshal(snap.Body, &body); err != nil {
		return SubmitResult{}, err
	}
	input, _ := body["input"].([]any)
	for _, it := range snap.Output {
		input = append(input, stripNulls(it))
	}
	input = append(input, submitToolItems()...)
	input = append(input, map[string]any{"role": "user", "content": req.Prompt + submitAsk})
	body["input"] = input
	body["tool_choice"] = "required"
	if req.Limit > 0 {
		body["max_output_tokens"] = req.Limit
	}
	role := req.Role
	if role == "" {
		role = RoleForkB
	}
	hdr := http.Header{
		"Content-Type":  {"application/json"},
		"Authorization": {"Bearer " + c.o.APIKey},
	}
	url := strings.TrimRight(c.o.BaseURL, "/") + "/v1/responses"
	var out SubmitResult
	round := ctx
	for used := 0; ; used++ {
		ex, _, err := c.send(round, url, hdr, body, role)
		if err == nil && ex.status >= 400 {
			err = fmt.Errorf("lab: fork returned HTTP %d: %s", ex.status, redact(string(ex.body)))
		}
		if err != nil {
			if out.Completed && !errors.Is(err, ErrBudget) && ctx.Err() == nil {
				return out, nil
			}
			return out, err
		}
		reply, err := parseSubmit(ex.body)
		if err != nil {
			if out.Completed {
				return out, nil
			}
			return out, err
		}
		if reply.callID == "" && len(reply.strays) == 0 {
			return out, nil
		}
		feedback := strayFeedback
		if reply.callID != "" {
			out.Text, out.Completed = reply.text, true
			ok, why := check(reply.text, out.Bounces == 0)
			if ok {
				out.Accepted = true
				return out, nil
			}
			feedback = why
		}
		if used == maxBounces {
			if reply.callID == "" {
				out.Strays++
			}
			return out, nil
		}
		next, _ := body["input"].([]any)
		next = append(slices.Clone(next), reply.items...)
		if reply.callID != "" {
			out.Bounces++
			next = append(next, map[string]any{"type": "function_call_output", "call_id": reply.callID, "output": feedback})
		} else {
			out.Strays++
		}
		for _, id := range reply.strays {
			next = append(next, map[string]any{"type": "function_call_output", "call_id": id, "output": strayFeedback})
		}
		body["input"] = next
		// The first round inserted the effort before its prompt, and it still holds.
		round = context.WithValue(ctx, effortKey, "")
	}
}

// submitReply is a checked fork's reply. A call ID means a whole, nonempty submission.
// Strays are every other call in the reply, each of which needs an answer before the
// next request. With neither, the reply did not complete or called nothing.
type submitReply struct {
	text, callID string
	strays       []string
	items        []any
}

func parseSubmit(raw []byte) (submitReply, error) {
	var out submitReply
	var parsed map[string]any
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return out, err
	}
	if parsed["status"] != "completed" {
		return out, nil
	}
	output, _ := parsed["output"].([]any)
	for _, it := range output {
		out.items = append(out.items, stripNulls(it))
		m, _ := it.(map[string]any)
		if m["type"] != "function_call" {
			continue
		}
		id, _ := m["call_id"].(string)
		if m["name"] == SubmitTool && out.callID == "" {
			args, _ := m["arguments"].(string)
			var call struct {
				Summary string `json:"summary"`
			}
			if json.Unmarshal([]byte(args), &call) == nil && strings.TrimSpace(call.Summary) != "" {
				out.text, out.callID = call.Summary, id
				continue
			}
		}
		out.strays = append(out.strays, id)
	}
	return out, nil
}

func parseFork(raw []byte) (ForkResult, error) {
	var parsed map[string]any
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return ForkResult{}, err
	}
	var row Row
	row.HTTP = http.StatusOK
	fillCloudRow(&row, raw, Prices{})
	res := ForkResult{
		Status: row.Status, Completed: row.Status == "completed",
		Input: row.Input, Cached: row.Cached, Write: row.Write, Output: row.Output, Reasoning: row.Reasoning,
	}
	var text strings.Builder
	items, _ := parsed["output"].([]any)
	for _, it := range items {
		m, _ := it.(map[string]any)
		if m["type"] != "message" {
			continue
		}
		parts, _ := m["content"].([]any)
		for _, p := range parts {
			pm, _ := p.(map[string]any)
			if t, ok := pm["text"].(string); ok {
				text.WriteString(t)
			}
		}
	}
	res.Text = text.String()
	return res, nil
}

// Cold sends one stateless request outside any session prefix. C7 uses it.
func (c *Cloud) Cold(ctx context.Context, role, system, user string, limit int) (ForkResult, error) {
	body := map[string]any{
		"model":        c.o.Model,
		"instructions": system,
		"input":        []any{map[string]any{"role": "user", "content": user}},
	}
	prepareResponses(body)
	if limit > 0 {
		body["max_output_tokens"] = limit
	}
	hdr := http.Header{
		"Content-Type":  {"application/json"},
		"Authorization": {"Bearer " + c.o.APIKey},
	}
	url := strings.TrimRight(c.o.BaseURL, "/") + "/v1/responses"
	ex, _, err := c.send(ctx, url, hdr, body, role)
	if err != nil {
		return ForkResult{}, err
	}
	if ex.status >= 400 {
		return ForkResult{}, fmt.Errorf("lab: request returned HTTP %d: %s", ex.status, redact(string(ex.body)))
	}
	return parseFork(ex.body)
}

func stripNulls(v any) any {
	switch x := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, val := range x {
			if val != nil {
				out[k] = stripNulls(val)
			}
		}
		return out
	case []any:
		out := make([]any, 0, len(x))
		for _, e := range x {
			out = append(out, stripNulls(e))
		}
		return out
	}
	return v
}

// PrefixEqual reports whether fork's input begins with the exact items of the
// snapshot's request. L0 uses it to check the prefix byte by byte.
func PrefixEqual(snapBody, forkBody []byte) (bool, error) {
	var a, b struct {
		Input []json.RawMessage `json:"input"`
	}
	if err := json.Unmarshal(snapBody, &a); err != nil {
		return false, err
	}
	if err := json.Unmarshal(forkBody, &b); err != nil {
		return false, err
	}
	if len(b.Input) < len(a.Input) {
		return false, nil
	}
	for i := range a.Input {
		if !bytes.Equal(a.Input[i], b.Input[i]) {
			return false, nil
		}
	}
	return true, nil
}
