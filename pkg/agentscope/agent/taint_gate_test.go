package agent

import (
	"context"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/agentscope-ai/agentscope-go/v2/pkg/agentscope/event"
	"github.com/agentscope-ai/agentscope-go/v2/pkg/agentscope/message"
	"github.com/agentscope-ai/agentscope-go/v2/pkg/agentscope/model"
	"github.com/agentscope-ai/agentscope-go/v2/pkg/agentscope/permission"
	"github.com/agentscope-ai/agentscope-go/v2/pkg/agentscope/tool"
)

type taintFixture struct {
	a     *UnifiedAgent
	m     *seamModel
	runs  map[string]*atomic.Int32
	perms *permission.Context
}

// newTaintFixture builds an agent whose model calls the named tool once and
// then answers with text. write and read are top-level tools, fetch lives in
// the "net" group; all three are allowed by rule.
func newTaintFixture(call string, withEngine bool, opts ...AgentOption) *taintFixture {
	f := &taintFixture{runs: map[string]*atomic.Int32{}}
	for _, n := range []string{"write", "read", "fetch"} {
		f.runs[n] = &atomic.Int32{}
	}
	tk := tool.NewToolkit(countingTool("write", f.runs["write"]), countingTool("read", f.runs["read"]))
	tk.AddGroup("net", countingTool("fetch", f.runs["fetch"]))
	f.m = &seamModel{respond: func(i int, _ []*message.Msg, _ *model.CallOptions) (*model.ChatResponse, error) {
		if i == 0 {
			return seamToolCalls(message.ToolCallBlock{ID: "c1", Name: call}), nil
		}
		return seamText("done"), nil
	}}
	all := []AgentOption{WithToolkit(tk)}
	if withEngine {
		f.perms = permission.NewContext(permission.ModeDefault)
		for _, n := range []string{"write", "read", "fetch"} {
			f.perms.AllowRules[n] = []permission.Rule{{ToolName: n, Behavior: permission.BehaviorAllow}}
		}
		all = append(all, WithPermissionContext(f.perms))
	}
	f.a = NewUnifiedAgent("a", "sys", f.m, append(all, opts...)...)
	return f
}

// run replies once, answering every confirmation request with answer, and
// returns the number of requests and the recorded tool result.
func (f *taintFixture) run(t *testing.T, answer bool) (int, message.ToolResultBlock) {
	t.Helper()
	ch, err := f.a.ReplyStream(context.Background(), "go")
	if err != nil {
		t.Fatal(err)
	}
	asks := 0
	for e := range ch {
		if req, ok := e.(event.RequireUserConfirmEvent); ok {
			asks++
			f.a.SubmitUserConfirm(&event.UserConfirmResultEvent{ConfirmResults: []event.ConfirmResult{{Confirmed: answer, ToolCall: req.ToolCalls[0]}}})
		}
	}
	return asks, f.lastResult()
}

// lastResult returns the last tool result recorded in the context.
func (f *taintFixture) lastResult() message.ToolResultBlock {
	var result message.ToolResultBlock
	for _, m := range f.a.state.Context {
		for _, b := range m.GetContentBlocks(message.ContentBlockToolResult) {
			result = b.(message.ToolResultBlock)
		}
	}
	return result
}

func TestTaintGateAsksForAllowedCall(t *testing.T) {
	// Untainted: the allow rule holds and nothing is asked.
	f := newTaintFixture("write", true, WithTaintGate("write", "net"))
	if asks, r := f.run(t, true); asks != 0 || r.State != message.ToolResultSuccess || f.runs["write"].Load() != 1 {
		t.Fatalf("untainted: asks=%d result=%+v", asks, r)
	}

	// Tainted: the same allowed call needs confirmation and runs once confirmed.
	f = newTaintFixture("write", true, WithTaintGate("write", "net"))
	if err := f.a.SetTainted(true); err != nil {
		t.Fatal(err)
	}
	if asks, r := f.run(t, true); asks != 1 || r.State != message.ToolResultSuccess || f.runs["write"].Load() != 1 {
		t.Fatalf("tainted, confirmed: asks=%d result=%+v runs=%d", asks, r, f.runs["write"].Load())
	}
	if rules := f.perms.AllowRules["write"]; len(rules) != 1 {
		t.Fatalf("confirming a gated call must not add rules: %+v", rules)
	}

	// Tainted and refused: the call is denied and does not run.
	f = newTaintFixture("write", true, WithTaintGate("write"))
	if err := f.a.SetTainted(true); err != nil {
		t.Fatal(err)
	}
	if asks, r := f.run(t, false); asks != 1 || r.State == message.ToolResultSuccess || f.runs["write"].Load() != 0 {
		t.Fatalf("tainted, refused: asks=%d result=%+v runs=%d", asks, r, f.runs["write"].Load())
	}

	// A tool outside the gate is not affected.
	f = newTaintFixture("read", true, WithTaintGate("write", "net"))
	if err := f.a.SetTainted(true); err != nil {
		t.Fatal(err)
	}
	if asks, _ := f.run(t, true); asks != 0 || f.runs["read"].Load() != 1 {
		t.Fatalf("ungated tool: asks=%d", asks)
	}
}

func TestTaintGateByGroup(t *testing.T) {
	f := newTaintFixture("fetch", true, WithTaintGate("net"))
	if err := f.a.SetTainted(true); err != nil {
		t.Fatal(err)
	}
	if asks, r := f.run(t, true); asks != 1 || r.State != message.ToolResultSuccess || f.runs["fetch"].Load() != 1 {
		t.Fatalf("group gate: asks=%d result=%+v", asks, r)
	}
}

func TestTaintGateKeepsDeny(t *testing.T) {
	f := newTaintFixture("write", true, WithTaintGate("write"))
	f.perms.DenyRules["write"] = []permission.Rule{{ToolName: "write", Behavior: permission.BehaviorDeny}}
	if err := f.a.SetTainted(true); err != nil {
		t.Fatal(err)
	}
	if asks, r := f.run(t, true); asks != 0 || r.State != message.ToolResultDenied || f.runs["write"].Load() != 0 {
		t.Fatalf("deny must stay deny: asks=%d result=%+v", asks, r)
	}
}

func TestTaintGateNeedsEngine(t *testing.T) {
	f := newTaintFixture("write", false, WithTaintGate("write"))
	if err := f.a.SetTainted(true); err != nil {
		t.Fatal(err)
	}
	if asks, r := f.run(t, true); asks != 0 || r.State != message.ToolResultSuccess || f.runs["write"].Load() != 1 {
		t.Fatalf("no engine, no gate: asks=%d result=%+v", asks, r)
	}
}

// replyCountingAsks runs one reply without answering confirmation requests and
// returns how many were asked. A reply that waits for an answer runs into the
// deadline, which fails the test.
func replyCountingAsks(t *testing.T, a *UnifiedAgent) int {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	ch, err := a.ReplyStream(ctx, "go")
	if err != nil {
		t.Fatal(err)
	}
	asks := 0
	for e := range ch {
		if _, ok := e.(event.RequireUserConfirmEvent); ok {
			asks++
		}
	}
	if ctx.Err() != nil {
		t.Fatalf("the reply waited for a confirmation (%d asked)", asks)
	}
	return asks
}

func TestTaintGateDeniesInDontAskMode(t *testing.T) {
	for _, width := range []int{0, 2} {
		t.Run("width "+strconv.Itoa(width), func(t *testing.T) {
			var opts []AgentOption
			if width > 0 {
				opts = append(opts, WithToolConcurrencyLimit(width))
			}
			f := newTaintFixture("write", true, append(opts, WithTaintGate("write"))...)
			f.perms.Mode = permission.ModeDontAsk
			if err := f.a.SetTainted(true); err != nil {
				t.Fatal(err)
			}
			if asks := replyCountingAsks(t, f.a); asks != 0 {
				t.Fatalf("dont_ask mode must never ask, asked %d times", asks)
			}
			r := f.lastResult()
			if r.State != message.ToolResultDenied || !strings.Contains(r.GetOutputText(), "dont_ask") || f.runs["write"].Load() != 0 {
				t.Fatalf("a gated call in dont_ask mode must be denied: result=%+v runs=%d", r, f.runs["write"].Load())
			}

			// Untainted, the allow rule still holds in dont_ask mode.
			f = newTaintFixture("write", true, append(opts, WithTaintGate("write"))...)
			f.perms.Mode = permission.ModeDontAsk
			if asks, r := f.run(t, true); asks != 0 || r.State != message.ToolResultSuccess || f.runs["write"].Load() != 1 {
				t.Fatalf("untainted dont_ask: asks=%d result=%+v", asks, r)
			}
		})
	}
}

// TestTaintGateAsksInAnsweringModes pins the deliberate choice for the modes
// that can answer an ask: the gate asks there, like an explicit ask rule.
func TestTaintGateAsksInAnsweringModes(t *testing.T) {
	for _, mode := range []permission.PermissionMode{permission.ModeDefault, permission.ModeAcceptEdits, permission.ModeBypass} {
		t.Run(string(mode), func(t *testing.T) {
			f := newTaintFixture("write", true, WithTaintGate("write"))
			f.perms.Mode = mode
			if err := f.a.SetTainted(true); err != nil {
				t.Fatal(err)
			}
			if asks, r := f.run(t, true); asks != 1 || r.State != message.ToolResultSuccess || f.runs["write"].Load() != 1 {
				t.Fatalf("mode %s: asks=%d result=%+v runs=%d", mode, asks, r, f.runs["write"].Load())
			}
		})
	}
	// Explore mode denies the non-read-only call before the gate; deny stays deny.
	f := newTaintFixture("write", true, WithTaintGate("write"))
	f.perms.Mode = permission.ModeExplore
	if err := f.a.SetTainted(true); err != nil {
		t.Fatal(err)
	}
	if asks, r := f.run(t, true); asks != 0 || r.State != message.ToolResultDenied || f.runs["write"].Load() != 0 {
		t.Fatalf("explore: asks=%d result=%+v", asks, r)
	}
}

// TestTaintGateKeepsGatedCallOutOfParallelBatch: a gated call waits for a
// confirmation, so it must run on the sequential path. In a parallel batch
// its worker has no event channel, the request would never reach the host
// and the reply would wait for an answer nobody was asked for.
func TestTaintGateKeepsGatedCallOutOfParallelBatch(t *testing.T) {
	for _, width := range []int{0, 2} {
		t.Run("width "+strconv.Itoa(width), func(t *testing.T) {
			var fetchRuns, readRuns atomic.Int32
			fetch := tool.NewFunctionTool("fetch", "fetch", nil, func(context.Context, map[string]any) (any, error) {
				fetchRuns.Add(1)
				return "fetched", nil
			})
			read := tool.NewFunctionTool("read", "read", nil, func(context.Context, map[string]any) (any, error) {
				readRuns.Add(1)
				return "read", nil
			})
			fetch.ConcurrencySafe, read.ConcurrencySafe = true, true
			m := &seamModel{respond: func(call int, _ []*message.Msg, _ *model.CallOptions) (*model.ChatResponse, error) {
				if call == 0 {
					return seamToolCalls(message.ToolCallBlock{ID: "f", Name: "fetch"}, message.ToolCallBlock{ID: "r", Name: "read"}), nil
				}
				return seamText("done"), nil
			}}
			perms := permission.NewContext(permission.ModeDefault)
			for _, n := range []string{"fetch", "read"} {
				perms.AllowRules[n] = []permission.Rule{{ToolName: n, Behavior: permission.BehaviorAllow}}
			}
			opts := []AgentOption{WithToolkit(tool.NewToolkit(fetch, read)), WithPermissionContext(perms), WithTaintGate("fetch")}
			if width > 0 {
				opts = append(opts, WithToolConcurrencyLimit(width))
			}
			a := NewUnifiedAgent("a", "sys", m, opts...)
			if err := a.SetTainted(true); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			ch, err := a.ReplyStream(ctx, "go")
			if err != nil {
				t.Fatal(err)
			}
			asks := 0
			for e := range ch {
				if req, ok := e.(event.RequireUserConfirmEvent); ok {
					asks++
					a.SubmitUserConfirm(&event.UserConfirmResultEvent{ConfirmResults: []event.ConfirmResult{{Confirmed: true, ToolCall: req.ToolCalls[0]}}})
				}
			}
			if ctx.Err() != nil {
				t.Fatal("the gated call waited for a confirmation the host never saw")
			}
			if asks != 1 || fetchRuns.Load() != 1 || readRuns.Load() != 1 {
				t.Fatalf("asks=%d fetch=%d read=%d", asks, fetchRuns.Load(), readRuns.Load())
			}
		})
	}
}

func TestTaintGateLoopBridgeFailsClosed(t *testing.T) {
	f := newTaintFixture("write", true, WithTaintGate("write"))
	exec := &toolExecutorAdapter{agent: f.a, orchestrator: tool.NewOrchestrator(tool.OrchestratorConfig{Toolkit: f.a.toolkit, PermEngine: f.a.engine})}
	call := message.ToolCallBlock{Type: "tool_call", ID: "c1", Name: "write", Input: `{}`}

	resp, err := exec.Execute(context.Background(), call)
	if err != nil || resp.State == message.ToolResultError || f.runs["write"].Load() != 1 {
		t.Fatalf("untainted bridge call: %+v %v", resp, err)
	}
	if err := f.a.SetTainted(true); err != nil {
		t.Fatal(err)
	}
	resp, err = exec.Execute(context.Background(), call)
	if err != nil || resp.State != message.ToolResultError || !strings.Contains(responseText(resp), "untrusted input") || f.runs["write"].Load() != 1 {
		t.Fatalf("tainted bridge call must fail closed: %+v %v", resp, err)
	}
}
