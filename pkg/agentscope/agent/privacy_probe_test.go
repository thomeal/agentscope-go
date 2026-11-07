package agent

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/agentscope-ai/agentscope-go/v2/pkg/agentscope/event"
	"github.com/agentscope-ai/agentscope-go/v2/pkg/agentscope/message"
	"github.com/agentscope-ai/agentscope-go/v2/pkg/agentscope/model"
	"github.com/agentscope-ai/agentscope-go/v2/pkg/agentscope/permission"
	"github.com/agentscope-ai/agentscope-go/v2/pkg/agentscope/protocol"
	"github.com/agentscope-ai/agentscope-go/v2/pkg/agentscope/tool"
	"github.com/agentscope-ai/agentscope-go/v2/pkg/agentscope/types"
)

// snapSaver records every saved state: whether it holds the marker text and
// whether it is private.
type snapSaver struct {
	*fakeStateSaver
	marker string

	mu    sync.Mutex
	snaps []snap
}

type snap struct{ holds, private bool }

func newSnapSaver(marker string) *snapSaver {
	return &snapSaver{fakeStateSaver: newFakeStateSaver(), marker: marker}
}

func (s *snapSaver) SaveState(ctx context.Context, id string, st *AgentState) error {
	holds := false
	for _, m := range st.Context {
		if m == nil {
			continue
		}
		for _, b := range m.Content {
			if r, ok := b.(message.ToolResultBlock); ok && strings.Contains(r.GetOutputText(), s.marker) {
				holds = true
			}
		}
	}
	s.mu.Lock()
	s.snaps = append(s.snaps, snap{holds: holds, private: st.Private})
	s.mu.Unlock()
	return s.fakeStateSaver.SaveState(ctx, id, st)
}

// check fails unless some saved state holds the marker and every one that
// does is private.
func (s *snapSaver) check(t *testing.T) {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	held := 0
	for i, sn := range s.snaps {
		if sn.holds {
			held++
			if !sn.private {
				t.Fatalf("checkpoint %d holds the private reading but not the flag (snapshots %+v)", i, s.snaps)
			}
		}
	}
	if held == 0 {
		t.Fatalf("no checkpoint holds the reading: %+v", s.snaps)
	}
}

// privateHook marks the run private once the sensor tool has run, as the
// thread participant's loop hook does. It records the loop states the tool
// hooks saw.
type privateHook struct {
	dispatchHook
	private atomic.Bool
	states  []protocol.LoopState
}

func (h *privateHook) BeforeToolExec(state protocol.LoopState, iter int, name string) {
	h.dispatchHook.BeforeToolExec(state, iter, name)
	h.states = append(h.states, state)
}

func (h *privateHook) AfterToolExec(state protocol.LoopState, iter int, name string, err error) {
	h.dispatchHook.AfterToolExec(state, iter, name, err)
	h.states = append(h.states, state)
	if name == "sensor" {
		h.private.Store(true)
	}
}

func sensorTool() tool.Tool {
	return tool.NewFunctionTool("sensor", "reads the heart rate", nil,
		func(context.Context, map[string]any) (any, error) { return "heart rate 142 bpm", nil })
}

// A private tool's result and the flag land in the same checkpoint: the
// probe is asked before the tool step's checkpoint, after the loop hook
// marked the run.
func TestPrivacyProbeMarksTheToolStepCheckpoint(t *testing.T) {
	m := &seamModel{respond: func(call int, _ []*message.Msg, _ *model.CallOptions) (*model.ChatResponse, error) {
		if call == 0 {
			return seamToolCalls(message.ToolCallBlock{ID: "c1", Name: "sensor"}), nil
		}
		return seamText("done"), nil
	}}
	saver := newSnapSaver("142 bpm")
	hook := &privateHook{}
	var asked atomic.Int32
	a := NewUnifiedAgent("a", "sys", m, WithToolkit(tool.NewToolkit(sensorTool())), WithStateSaver(saver),
		WithLoopHooks(hook), WithPrivacyProbe(func() bool { asked.Add(1); return hook.private.Load() }))
	a.state.SessionID = "s"
	if v := a.ThreadView(); v.Private {
		t.Fatal("a new agent is private")
	}
	ch, err := a.ReplyStream(context.Background(), "how am I doing")
	if err != nil {
		t.Fatal(err)
	}
	if end := replyEnd(drain(t, ch)); end == nil || end.FinishedReason != types.ReplyCompleted {
		t.Fatalf("reply end = %+v", end)
	}
	saver.check(t)
	if asked.Load() == 0 {
		t.Fatal("the probe was never asked")
	}
	if v := a.ThreadView(); !v.Private {
		t.Fatal("the agent is not private after the reading")
	}
	if st := saver.states["s"]; st == nil || !st.Private || st.SchemaVersion != 3 {
		t.Fatalf("saved state = %+v", st)
	}
}

// A call restored from a checkpoint and run on resume gets the loop hooks
// too, so its result is saved with the flag.
func TestPrivacyProbeCoversRestoredCalls(t *testing.T) {
	mock := &mockChatModel{responses: []model.ChatResponse{
		{Content: []message.ContentBlock{message.TextBlock{Type: "text", Text: "you are fine"}}, IsLast: true},
	}}
	restored := &AgentState{
		SchemaVersion: StateSchemaVersion,
		SessionID:     "sess-sensor",
		Context: []*message.Msg{
			message.UserMsg("user", "how am I doing"),
			message.AssistantMsg("a", []message.ContentBlock{
				message.ToolCallBlock{Type: "tool_call", ID: "tc_ask", Name: "sensor", Input: `{}`, State: message.ToolCallAsking},
			}),
		},
	}
	saver := newSnapSaver("142 bpm")
	hook := &privateHook{}
	a := NewUnifiedAgent("a", "sys", mock,
		WithToolkit(tool.NewToolkit(sensorTool())),
		WithPermissionContext(permission.NewContext(permission.ModeDefault)),
		WithReactConfig(ReactConfig{MaxIters: 4}),
		WithState(restored), WithStateSaver(saver),
		WithLoopHooks(hook), WithPrivacyProbe(hook.private.Load))
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	ch, err := a.ReplyStream(ctx, "continue")
	if err != nil {
		t.Fatal(err)
	}
	confirmed := false
	for evt := range ch {
		if ce, ok := evt.(event.RequireUserConfirmEvent); ok && !confirmed {
			confirmed = true
			a.SubmitUserConfirm(&event.UserConfirmResultEvent{
				ConfirmResults: []event.ConfirmResult{{Confirmed: true, ToolCall: ce.ToolCalls[0]}},
			})
		}
	}
	if !confirmed {
		t.Fatal("the restored call was not re-prompted")
	}
	if got := strings.Join(hook.calls, ","); got != "before:sensor,after:sensor" {
		t.Fatalf("hooks around the restored call = %s", got)
	}
	for _, st := range hook.states {
		if st != protocol.StateAct {
			t.Fatalf("the hooks saw the restored call in state %v, want %v", st, protocol.StateAct)
		}
	}
	saver.check(t)
}

// ThreadUpdate.Private sets and clears the flag; a held plan keeps it set,
// and a reset alone changes nothing.
func TestThreadUpdatePrivate(t *testing.T) {
	ctx := context.Background()
	yes, no := true, false
	a := NewUnifiedAgent("a", "sys", &seamModel{})
	a.mu.Lock()
	a.state.Plan = []tool.PlanItem{{Step: "check the weather", Status: tool.PlanStatusCompleted}}
	a.mu.Unlock()
	apply := func(u ThreadUpdate) bool {
		t.Helper()
		if err := a.ApplyThreadUpdate(ctx, u); err != nil {
			t.Fatal(err)
		}
		return a.ThreadView().Private
	}
	if apply(ThreadUpdate{Reset: true, Private: &no}) {
		t.Fatal("a clear made a never-private agent that holds a plan private")
	}
	a.mu.Lock()
	a.state.Plan = nil
	a.mu.Unlock()
	if !apply(ThreadUpdate{Private: &yes}) {
		t.Fatal("Private=true did not set the flag")
	}
	if !apply(ThreadUpdate{Reset: true}) {
		t.Fatal("a reset alone cleared the flag")
	}
	if apply(ThreadUpdate{Reset: true, Private: &no}) {
		t.Fatal("Private=false did not clear the flag")
	}
	a.mu.Lock()
	a.state.Plan = []tool.PlanItem{{Step: "read the heart rate", Status: tool.PlanStatusCompleted}}
	a.mu.Unlock()
	apply(ThreadUpdate{Private: &yes})
	if !apply(ThreadUpdate{Reset: true, Private: &no}) {
		t.Fatal("a clear lowered the flag while the agent holds a plan")
	}
	a.mu.Lock()
	a.state.Plan = nil
	a.mu.Unlock()
	if apply(ThreadUpdate{Reset: true, Private: &no}) {
		t.Fatal("the flag stayed set after the plan was dropped")
	}

	// The flag alone raises the schema and survives a JSON round trip.
	raw, err := json.Marshal(&AgentState{SessionID: "s", Private: true})
	if err != nil {
		t.Fatal(err)
	}
	var back AgentState
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatal(err)
	}
	if !back.Private || back.checkpointSchemaVersion() != 3 {
		t.Fatalf("round trip = %+v (schema %d)", back, back.checkpointSchemaVersion())
	}
}
