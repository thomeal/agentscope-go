package lab

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/agentscope-ai/agentscope-go/v2/pkg/agentscope/message"
	"github.com/agentscope-ai/agentscope-go/v2/pkg/agentscope/model"
)

// Overheads added to tokenized text for chat framing. They were measured against
// vLLM's reported prompt_tokens for Qwen3.5-4B and err slightly high.
const (
	perMessageOverhead = 6
	perRequestOverhead = 12
)

// Tokenizer counts exact tokens through vLLM's /tokenize, with a cache. The
// agent's default estimate is bytes/4, while random codes run at about 1.7
// characters per token, so an 8K window would otherwise overflow. Every 8K arm
// uses this counter, which gives the baselines their best chance.
type Tokenizer struct {
	base, model string
	client      *http.Client

	mu    sync.Mutex
	cache map[string]int
}

// NewTokenizer returns a counter for one server. It shares its cache across
// sessions, which keeps repeated histories cheap.
func NewTokenizer(base, modelName string) *Tokenizer {
	return &Tokenizer{
		base: strings.TrimRight(base, "/"), model: modelName,
		client: &http.Client{Timeout: 2 * time.Minute},
		cache:  map[string]int{},
	}
}

// Count returns the token count of text. It falls back to the conservative
// EstimateTokens if the server cannot answer, so a failed count cannot silently
// understate the context.
func (t *Tokenizer) Count(ctx context.Context, text string) int {
	if text == "" {
		return 0
	}
	t.mu.Lock()
	n, ok := t.cache[text]
	t.mu.Unlock()
	if ok {
		return n
	}
	n, err := t.remote(ctx, text)
	if err != nil {
		return EstimateTokens(text)
	}
	t.mu.Lock()
	t.cache[text] = n
	t.mu.Unlock()
	return n
}

func (t *Tokenizer) remote(ctx context.Context, text string) (int, error) {
	body, err := json.Marshal(map[string]any{"model": t.model, "prompt": text, "add_special_tokens": false})
	if err != nil {
		return 0, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.base+"/tokenize", bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := t.client.Do(req)
	if err != nil {
		return 0, err
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, err
	}
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("lab: tokenize returned HTTP %d", resp.StatusCode)
	}
	var parsed struct {
		Count int `json:"count"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return 0, err
	}
	return parsed.Count, nil
}

// CountMessages counts a request the way the chat template will roughly render
// it: every block's text and tool arguments, plus framing and tool schemas.
func (t *Tokenizer) CountMessages(ctx context.Context, msgs []*message.Msg, tools []model.ToolSchema) int {
	total := perRequestOverhead
	for _, m := range msgs {
		total += perMessageOverhead
		for _, b := range m.GetContentBlocks() {
			switch blk := b.(type) {
			case message.TextBlock:
				total += t.Count(ctx, blk.Text)
			case message.ToolCallBlock:
				total += perMessageOverhead + t.Count(ctx, blk.Name) + t.Count(ctx, blk.Input)
			case message.ToolResultBlock:
				total += perMessageOverhead + t.Count(ctx, resultText(blk))
			}
		}
	}
	if len(tools) > 0 {
		raw, err := json.Marshal(tools)
		if err == nil {
			total += t.Count(ctx, string(raw))
		}
	}
	return total
}

func resultText(blk message.ToolResultBlock) string {
	switch out := blk.Output.(type) {
	case string:
		return out
	case []message.ContentBlock:
		var sb strings.Builder
		for _, c := range out {
			if tb, ok := c.(message.TextBlock); ok {
				sb.WriteString(tb.Text)
			}
		}
		return sb.String()
	}
	raw, err := json.Marshal(blk.Output)
	if err != nil {
		return ""
	}
	return string(raw)
}

// CountingModel wraps a ChatModel and replaces only its token counter.
type CountingModel struct {
	model.ChatModel
	Tokens *Tokenizer
}

// CountTokens implements model.ChatModel with exact counts.
func (m *CountingModel) CountTokens(msgs []*message.Msg, tools []model.ToolSchema) int {
	return m.Tokens.CountMessages(context.Background(), msgs, tools)
}
