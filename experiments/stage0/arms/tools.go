package arms

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"sync"

	"github.com/agentscope-ai/agentscope-go/v2/experiments/stage0/corpus"
	"github.com/agentscope-ai/agentscope-go/v2/experiments/stage0/s2"
	"github.com/agentscope-ai/agentscope-go/v2/pkg/agentscope/tool"
)

var recordSchema = json.RawMessage(`{"type":"object","properties":{"id":{"type":"string","description":"The record id."}},"required":["id"]}`)

var threadSchema = json.RawMessage(`{"type":"object","properties":{"seq":{"type":"integer","description":"Entry number to read in full."},"before":{"type":"integer","description":"Show the outline of entries older than this number."}}}`)

const threadDescription = `Read this session's log. With no arguments it returns an outline: one line per entry with its number, kind, source and first line, newest entries last. With "seq" it returns that entry's full text. With "before" it returns the outline of the entries older than that number.`

// toolCall is one tool use during a main turn, as T1 publishes it: name and
// arguments, never the result.
type toolCall struct{ Name, Args string }

// toolEvent is one tool use during a main turn with its result: an entry of the event list.
type toolEvent struct {
	Turn             int
	Name, Args, Text string
}

// toolRec records tool use for exposure and enforces the probe bound. One toolRec
// serves all the agents of a session, which run one at a time.
type toolRec struct {
	mu      sync.Mutex
	turn    int
	limit   int
	used    int
	calls   []toolCall
	results map[int][]string
	events  []toolEvent
	// staleWrites counts the writes a workspace refused because the writer had not seen the
	// file's current version.
	staleWrites, staleLocal int
}

func newToolRec() *toolRec {
	return &toolRec{turn: -1, results: map[int][]string{}}
}

// begin starts a main turn (turn >= 0) or a probe (turn < 0 with a limit).
func (r *toolRec) begin(turn, limit int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.turn, r.limit, r.used, r.calls = turn, limit, 0, nil
}

// end stops recording and returns the calls of the turn.
func (r *toolRec) end() []toolCall {
	r.mu.Lock()
	defer r.mu.Unlock()
	calls := r.calls
	r.turn, r.limit, r.used, r.calls = -1, 0, 0, nil
	return calls
}

// wrap builds a tool that goes through the recorder. Its schema never changes,
// because the tool list is part of the cloud's cache prefix.
func (r *toolRec) wrap(name, description string, schema json.RawMessage, fn func(map[string]any) (string, error)) tool.Tool {
	return r.wrapCtx(name, description, schema, func(_ context.Context, input map[string]any) (string, error) {
		return fn(input)
	})
}

// wrapCtx is wrap for a tool that needs the call's context.
func (r *toolRec) wrapCtx(name, description string, schema json.RawMessage, fn func(context.Context, map[string]any) (string, error)) tool.Tool {
	return tool.NewFunctionTool(name, description, schema, func(ctx context.Context, input map[string]any) (out any, err error) {
		// Tools run on a goroutine the agent owns, where a panic would end the run.
		defer func() {
			if rec := recover(); rec != nil {
				out, err = nil, fmt.Errorf("panic in tool %s: %v", name, rec)
			}
		}()
		if err := r.admit(); err != nil {
			return nil, err
		}
		text, err := fn(ctx, input)
		r.record(name, input, text, err)
		if err != nil {
			return nil, err
		}
		return text, nil
	})
}

// admit enforces the bound per call. The agent's own bound is per model round, and
// one round can hold several calls.
func (r *toolRec) admit() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.limit > 0 && r.used >= r.limit {
		return fmt.Errorf("tool call limit reached: at most %d tool calls for this question batch; answer now", r.limit)
	}
	r.used++
	return nil
}

func (r *toolRec) record(name string, input map[string]any, text string, callErr error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	// A refused write did not happen, and T1's log says what the other assistant did.
	if r.turn < 0 || errors.Is(callErr, s2.ErrStaleWrite) {
		return
	}
	args := marshalArgs(input)
	r.calls = append(r.calls, toolCall{Name: name, Args: string(args)})
	if callErr == nil {
		r.results[r.turn] = append(r.results[r.turn], text)
		r.events = append(r.events, toolEvent{Turn: r.turn, Name: name, Args: string(args), Text: text})
	}
}

// marshalArgs renders a call's arguments with the path first. A log entry keeps only the start
// of them, and json.Marshal sorts keys, which puts a whole file's content before its path.
func marshalArgs(input map[string]any) []byte {
	all, err := json.Marshal(input)
	if err != nil {
		return []byte("{}")
	}
	path, ok := input["path"]
	if !ok || len(input) == 1 {
		return all
	}
	rest := maps.Clone(input)
	delete(rest, "path")
	first, err := json.Marshal(path)
	if err != nil {
		return all
	}
	others, err := json.Marshal(rest)
	if err != nil {
		return all
	}
	return slices.Concat([]byte(`{"path":`), first, []byte(","), others[1:])
}

// eventsWhere returns the recorded events that pass keep, oldest first.
func (r *toolRec) eventsWhere(keep func(toolEvent) bool) []toolEvent {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []toolEvent
	for _, e := range r.events {
		if keep(e) {
			out = append(out, e)
		}
	}
	return out
}

// recordTool serves one side's records.
func (r *toolRec) recordTool(side corpus.Side, read func(corpus.Side, string) (string, bool)) tool.Tool {
	name, description := corpus.RecordTool(side)
	return r.wrap(name, description, recordSchema, func(input map[string]any) (string, error) {
		id, _ := input["id"].(string)
		text, ok := read(side, id)
		if !ok {
			return "", fmt.Errorf("no record %q here", id)
		}
		return text, nil
	})
}

// threadTool is thread_read for one viewer.
func (r *toolRec) threadTool(log *threadLog, viewer corpus.Side) tool.Tool {
	return r.wrap("thread_read", threadDescription, threadSchema, func(input map[string]any) (string, error) {
		return log.read(viewer, input)
	})
}
