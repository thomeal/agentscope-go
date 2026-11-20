package main

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/agentscope-ai/agentscope-go/v2/pkg/agentscope/message"
	"github.com/agentscope-ai/agentscope-go/v2/pkg/agentscope/model"
)

// recorded is one line of stage 0's prompts.jsonl: a cloud request as the harness sent it,
// after its transport had applied prepareResponses.
type recorded struct {
	ID          int             `json:"id"`
	Session     string          `json:"session"`
	Group       string          `json:"group"`
	Known       bool            `json:"known_runaway"`
	LocalWrites int             `json:"local_writes"`
	CloudWrites int             `json:"cloud_writes"`
	Req         json.RawMessage `json:"req"`
}

// author is the side that wrote one model response.
type author int

const (
	authorUnknown author = iota
	authorLocal
	authorCloud
)

func (a author) String() string {
	switch a {
	case authorLocal:
		return "local"
	case authorCloud:
		return "cloud"
	}
	return "unknown"
}

type stepKind int

const (
	stepUser     stepKind = iota // a user message
	stepResponse                 // one model response: reasoning, text and tool calls
	stepResults                  // the tool results that answer the response before them
)

// step is one event of the recorded session, in input order.
type step struct {
	kind   stepKind
	text   string                 // stepUser
	author author                 // stepResponse
	blocks []message.ContentBlock // stepResponse and stepResults
}

// conversation is a recorded request taken apart into what produced it: the system
// prompt, the tools, and the session's steps.
type conversation struct {
	model        string
	instructions string
	tools        []model.ToolSchema
	steps        []step
}

// localResponses returns the blocks of every local response, in order. They are what the
// scripted local model answers.
func (c *conversation) localResponses() [][]message.ContentBlock {
	var out [][]message.ContentBlock
	for _, s := range c.steps {
		if s.kind == stepResponse && s.author == authorLocal {
			out = append(out, s.blocks)
		}
	}
	return out
}

// The request fields the recorded bodies carry, and how each is reproduced.
var (
	// reproducedByAdapter are written by main's Responses adapter from the history and
	// the call options.
	reproducedByAdapter = []string{"input", "instructions", "model", "stream", "tools"}
	// reproducedByTransport are written by prepareResponses, which both variants apply.
	reproducedByTransport = []string{"include", "store"}
)

// convert takes a recorded request apart. It fails on anything it cannot reproduce
// exactly, rather than dropping it.
func convert(raw []byte) (*conversation, error) {
	var req map[string]any
	if err := json.Unmarshal(raw, &req); err != nil {
		return nil, fmt.Errorf("request is not JSON: %w", err)
	}
	for k := range req {
		if !slices.Contains(reproducedByAdapter, k) && !slices.Contains(reproducedByTransport, k) {
			return nil, fmt.Errorf("request field %q is not reproduced", k)
		}
	}
	if req["stream"] != false {
		return nil, fmt.Errorf("stream is %v; Chat sends false", req["stream"])
	}
	if req["store"] != false {
		return nil, fmt.Errorf("store is %v; the transport sends false", req["store"])
	}
	if inc, _ := json.Marshal(req["include"]); string(inc) != `["reasoning.encrypted_content"]` {
		return nil, fmt.Errorf("include is %s; the transport sends [\"reasoning.encrypted_content\"]", inc)
	}
	c := &conversation{}
	var ok bool
	if c.model, ok = req["model"].(string); !ok || c.model == "" {
		return nil, fmt.Errorf("model is missing")
	}
	if c.instructions, ok = req["instructions"].(string); !ok {
		return nil, fmt.Errorf("instructions are missing")
	}
	tools, err := convertTools(req["tools"])
	if err != nil {
		return nil, err
	}
	c.tools = tools
	items, ok := req["input"].([]any)
	if !ok {
		return nil, fmt.Errorf("input is not a list")
	}
	if c.steps, err = convertItems(items); err != nil {
		return nil, err
	}
	return c, nil
}

func convertTools(v any) ([]model.ToolSchema, error) {
	list, ok := v.([]any)
	if !ok {
		return nil, fmt.Errorf("tools is not a list")
	}
	var out []model.ToolSchema
	for i, t := range list {
		m, ok := t.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("tool %d is not an object", i)
		}
		if err := onlyKeys(m, "type", "name", "description", "parameters"); err != nil {
			return nil, fmt.Errorf("tool %d: %w", i, err)
		}
		if m["type"] != "function" {
			return nil, fmt.Errorf("tool %d has type %v", i, m["type"])
		}
		name, _ := m["name"].(string)
		desc, ok := m["description"].(string)
		if name == "" || !ok {
			return nil, fmt.Errorf("tool %d needs a name and a description", i)
		}
		params, err := json.Marshal(m["parameters"])
		if err != nil {
			return nil, fmt.Errorf("tool %d parameters: %w", i, err)
		}
		out = append(out, model.ToolSchema{
			Type:     "function",
			Function: model.ToolFunction{Name: name, Description: desc, Parameters: params},
		})
	}
	return out, nil
}

// convertItems groups the input items into steps. A model response is a run of
// reasoning, assistant text and function calls; the function_call_output items after it
// are its results. Local calls carry the local server's call_id prefix and no item id
// (prepareResponses dropped it). Cloud calls carry an fc item id, and reasoning items are
// the cloud's: the local model ran with thinking off. A response with only text takes
// the author of the other responses in its reply.
func convertItems(items []any) ([]step, error) {
	var steps []step
	type call struct {
		blockID, name string
		answered      bool
	}
	calls := map[string]*call{} // by wire call_id
	open := func(a author) (*step, error) {
		if n := len(steps); n > 0 && steps[n-1].kind == stepResponse {
			s := &steps[n-1]
			if a != authorUnknown {
				if s.author != authorUnknown && s.author != a {
					return nil, fmt.Errorf("a %s block follows a %s block in one response", a, s.author)
				}
				s.author = a
			}
			return s, nil
		}
		steps = append(steps, step{kind: stepResponse, author: a})
		return &steps[len(steps)-1], nil
	}
	for i, it := range items {
		m, ok := it.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("item %d is not an object", i)
		}
		if role, has := m["role"]; has {
			if err := onlyKeys(m, "role", "content"); err != nil {
				return nil, fmt.Errorf("item %d: %w", i, err)
			}
			text, ok := m["content"].(string)
			if !ok {
				return nil, fmt.Errorf("item %d: content is not a string", i)
			}
			switch role {
			case "user":
				steps = append(steps, step{kind: stepUser, text: text})
			case "assistant":
				s, err := open(authorUnknown)
				if err != nil {
					return nil, fmt.Errorf("item %d: %w", i, err)
				}
				s.blocks = append(s.blocks, message.TextBlock{Type: "text", Text: text})
			default:
				return nil, fmt.Errorf("item %d has role %v", i, role)
			}
			continue
		}
		switch m["type"] {
		case "reasoning":
			if err := onlyKeys(m, "type", "id", "summary", "content", "encrypted_content"); err != nil {
				return nil, fmt.Errorf("item %d: %w", i, err)
			}
			s, err := open(authorCloud)
			if err != nil {
				return nil, fmt.Errorf("item %d: %w", i, err)
			}
			id, _ := m["id"].(string)
			// The adapter replays Extra["responses_item"] verbatim (nulls stripped); this
			// is how parseResponse stores a reasoning item.
			s.blocks = append(s.blocks, message.ThinkingBlock{
				Type: "thinking", ID: id, Thinking: summaryText(m),
				Extra: map[string]any{"responses_item": m},
			})
		case "function_call":
			if err := onlyKeys(m, "type", "id", "call_id", "name", "arguments"); err != nil {
				return nil, fmt.Errorf("item %d: %w", i, err)
			}
			callID, _ := m["call_id"].(string)
			name, _ := m["name"].(string)
			args, ok := m["arguments"].(string)
			if callID == "" || name == "" || !ok {
				return nil, fmt.Errorf("item %d: function_call needs call_id, name and arguments", i)
			}
			if _, dup := calls[callID]; dup {
				return nil, fmt.Errorf("item %d: call_id %s repeats", i, callID)
			}
			id, hasID := m["id"].(string)
			var blk message.ToolCallBlock
			var a author
			switch {
			case strings.HasPrefix(callID, localCallPrefix):
				if hasID {
					return nil, fmt.Errorf("item %d: local call %s kept an item id", i, callID)
				}
				// As the local server's response was parsed: the call id is the block ID,
				// and the adapter sends it as call_id (and as the id the transport drops).
				a, blk = authorLocal, message.ToolCallBlock{Type: "tool_call", ID: callID, Name: name, Input: args}
			case hasID && strings.HasPrefix(id, "fc"):
				// As parseResponse stores a cloud call: the item id is the block ID and
				// the wire call_id is kept in Extra.
				a, blk = authorCloud, message.ToolCallBlock{
					Type: "tool_call", ID: id, Name: name, Input: args,
					Extra: map[string]any{"call_id": callID},
				}
			default:
				return nil, fmt.Errorf("item %d: call %s is neither local nor the cloud's", i, callID)
			}
			s, err := open(a)
			if err != nil {
				return nil, fmt.Errorf("item %d: %w", i, err)
			}
			s.blocks = append(s.blocks, blk)
			calls[callID] = &call{blockID: blk.ID, name: name}
		case "function_call_output":
			if err := onlyKeys(m, "type", "call_id", "output"); err != nil {
				return nil, fmt.Errorf("item %d: %w", i, err)
			}
			callID, _ := m["call_id"].(string)
			out, ok := m["output"].(string)
			if !ok {
				return nil, fmt.Errorf("item %d: output is not a string", i)
			}
			c := calls[callID]
			if c == nil || c.answered {
				return nil, fmt.Errorf("item %d: output for %s has no open call", i, callID)
			}
			c.answered = true
			if n := len(steps); n == 0 || steps[n-1].kind == stepUser {
				return nil, fmt.Errorf("item %d: output follows no response", i)
			} else if steps[n-1].kind == stepResponse {
				steps = append(steps, step{kind: stepResults})
			}
			s := &steps[len(steps)-1]
			// UnifiedAgent keys a result by the call block's ID; the adapter maps it back
			// to the call's wire call_id.
			s.blocks = append(s.blocks, message.ToolResultBlock{
				Type: "tool_result", ID: c.blockID, Name: c.name, Output: out, State: message.ToolResultSuccess,
			})
		default:
			return nil, fmt.Errorf("item %d has type %v", i, m["type"])
		}
	}
	for id, c := range calls {
		if !c.answered {
			return nil, fmt.Errorf("call %s has no output", id)
		}
	}
	return steps, resolveTextAuthors(steps)
}

// resolveTextAuthors gives a text-only response the author of its reply, the responses
// between the same two user messages. A reply that is all text has no evidence of its
// author and is an error.
func resolveTextAuthors(steps []step) error {
	start := 0
	for start < len(steps) {
		end := start
		for end < len(steps) && steps[end].kind != stepUser {
			end++
		}
		var who author
		for _, s := range steps[start:end] {
			if s.kind != stepResponse || s.author == authorUnknown {
				continue
			}
			if who != authorUnknown && who != s.author {
				return fmt.Errorf("one reply has responses from both sides")
			}
			who = s.author
		}
		for i := start; i < end; i++ {
			if steps[i].kind == stepResponse && steps[i].author == authorUnknown {
				if who == authorUnknown {
					return fmt.Errorf("a reply holds only text, so its author is unknown")
				}
				steps[i].author = who
			}
		}
		start = end + 1
	}
	return nil
}

func summaryText(item map[string]any) string {
	parts, _ := item["summary"].([]any)
	var texts []string
	for _, p := range parts {
		if pm, ok := p.(map[string]any); ok {
			if t, ok := pm["text"].(string); ok {
				texts = append(texts, t)
			}
		}
	}
	return strings.Join(texts, "\n")
}

func onlyKeys(m map[string]any, allowed ...string) error {
	for k := range m {
		if !slices.Contains(allowed, k) {
			return fmt.Errorf("field %q is not reproduced", k)
		}
	}
	return nil
}
