package lab

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"

	"github.com/agentscope-ai/agentscope-go/v2/pkg/agentscope/model"
)

// compressLimit is the output limit of the agent's own compression calls. It matches
// the local summary total, and vLLM caps any limit at the remaining window.
const compressLimit = 3000

// Local is the vLLM Chat Completions transport for one participant in one
// session. The window is enforced by the server's --max-model-len.
type Local struct {
	o          Options
	maxTokens  int
	serverArgs map[string]any
}

// NewLocal returns a transport. maxTokens caps replies that set no limit of
// their own; 0 leaves that to the server.
func NewLocal(o Options, maxTokens int) *Local {
	return &Local{
		o: o, maxTokens: maxTokens,
		serverArgs: map[string]any{
			"chat_template_kwargs": map[string]any{"enable_thinking": false},
			"temperature":          0,
		},
	}
}

// ChatModel builds the real OpenAI-compatible adapter over this transport.
func (l *Local) ChatModel() (model.ChatModel, error) {
	return model.NewOpenAIChatModel(model.OpenAIConfig{
		APIKey:     "local",
		BaseURL:    l.o.BaseURL,
		Model:      l.o.Model,
		HTTPClient: &http.Client{Transport: l},
	})
}

// RoundTrip implements http.RoundTripper for adapter traffic.
func (l *Local) RoundTrip(r *http.Request) (*http.Response, error) {
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
	maps.Copy(body, l.serverArgs)
	role := roleOf(ctx, body)
	limit := l.maxTokens
	if role == RoleCompress && limit < compressLimit {
		limit = compressLimit
	}
	if v, ok := ctx.Value(limitKey).(int); ok && v > 0 {
		limit = v
	}
	if limit > 0 {
		delete(body, "max_tokens")
		body["max_completion_tokens"] = limit
	}
	// A compression can start inside a repair's reply. It has its own forced tool, and
	// the answer form would break it, so the schema holds for the answer alone.
	if schema, ok := ctx.Value(schemaKey).(map[string]any); ok && role != RoleCompress {
		body["response_format"] = map[string]any{
			"type":        "json_schema",
			"json_schema": map[string]any{"name": "answers", "schema": schema, "strict": true},
		}
		body["tool_choice"] = "none"
	}
	out, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	ex, err := post(ctx, &l.o, r.URL.String(), r.Header, out)
	row := Row{
		Arm: l.o.Arm, Session: l.o.Session, Side: SideLocal, Role: role, Model: l.o.Model,
		HTTP: ex.status, WaitMS: ex.wait, LatencyMS: ex.ms, Attempt: ex.tries,
	}
	if err != nil {
		row.Error = err.Error()
		l.record(row)
		return nil, err
	}
	fillLocalRow(&row, ex.body)
	l.record(row)
	if l.o.Dump != nil {
		l.o.Dump(l.o.Arm, l.o.Session, SideLocal, role, out, ex.body)
	}
	return rebuilt(r, ex), nil
}

func (l *Local) record(r Row) {
	if l.o.Ledger != nil {
		l.o.Ledger.Add(r)
	}
}

func fillLocalRow(row *Row, respBody []byte) {
	if row.HTTP >= 400 {
		row.Error = redact(string(respBody))
		row.Overflow = model.IsContextLimitExceeded(errors.New(string(respBody)))
		return
	}
	var parsed map[string]any
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		row.Error = "response is not JSON"
		row.UsageMissing = true
		return
	}
	if cs, ok := parsed["choices"].([]any); ok && len(cs) > 0 {
		if c, ok := cs[0].(map[string]any); ok {
			row.Status, _ = c["finish_reason"].(string)
		}
	}
	u, ok := parsed["usage"].(map[string]any)
	if !ok {
		row.UsageMissing = true
		return
	}
	row.Input, row.Output = intOf(u["prompt_tokens"]), intOf(u["completion_tokens"])
	if d, ok := u["prompt_tokens_details"].(map[string]any); ok {
		row.Cached = intOf(d["cached_tokens"])
	}
}
