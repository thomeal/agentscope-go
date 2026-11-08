package agent

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/agentscope-ai/agentscope-go/v2/pkg/agentscope/event"
	"github.com/agentscope-ai/agentscope-go/v2/pkg/agentscope/message"
	"github.com/agentscope-ai/agentscope-go/v2/pkg/agentscope/model"
	"github.com/agentscope-ai/agentscope-go/v2/pkg/agentscope/permission"
	"github.com/agentscope-ai/agentscope-go/v2/pkg/agentscope/protocol"
	"github.com/agentscope-ai/agentscope-go/v2/pkg/agentscope/tool"
)

// stateHook records the loop states the tool hooks saw.
type stateHook struct {
	dispatchHook
	states []protocol.LoopState
}

func (h *stateHook) BeforeToolExec(state protocol.LoopState, iter int, name string) {
	h.dispatchHook.BeforeToolExec(state, iter, name)
	h.states = append(h.states, state)
}

func (h *stateHook) AfterToolExec(state protocol.LoopState, iter int, name string, err error) {
	h.dispatchHook.AfterToolExec(state, iter, name, err)
	h.states = append(h.states, state)
}

// A call restored from a checkpoint and run on resume gets the loop hooks,
// in StateAct, as on the ordinary tool path.
func TestRestoredCallsRunTheLoopHooks(t *testing.T) {
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
	sensor := tool.NewFunctionTool("sensor", "reads the heart rate", nil,
		func(context.Context, map[string]any) (any, error) { return "heart rate 142 bpm", nil })
	hook := &stateHook{}
	a := NewUnifiedAgent("a", "sys", mock,
		WithToolkit(tool.NewToolkit(sensor)),
		WithPermissionContext(permission.NewContext(permission.ModeDefault)),
		WithReactConfig(ReactConfig{MaxIters: 4}),
		WithState(restored), WithStateSaver(newFakeStateSaver()),
		WithLoopHooks(hook))
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
}
