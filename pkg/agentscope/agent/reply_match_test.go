package agent

import (
	"context"
	"testing"
	"time"

	"github.com/agentscope-ai/agentscope-go/v2/pkg/agentscope/event"
	"github.com/agentscope-ai/agentscope-go/v2/pkg/agentscope/message"
	"github.com/agentscope-ai/agentscope-go/v2/pkg/agentscope/model"
	"github.com/agentscope-ai/agentscope-go/v2/pkg/agentscope/permission"
	"github.com/agentscope-ai/agentscope-go/v2/pkg/agentscope/tool"
)

// TestWaitersDropAnswersForAnotherReply: tool-call IDs may repeat across
// replies, so an answer that names another reply (one a canceled reply never
// read) must not answer the waiting reply's call with the same ID. An answer
// without a reply ID still matches, as before.
func TestWaitersDropAnswersForAnotherReply(t *testing.T) {
	a := NewUnifiedAgent("a", "sys", &seamModel{}, WithPermissionContext(permission.NewContext(permission.ModeDefault)))
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	result := func(reply, out string) event.ExternalExecutionResultEvent {
		return event.ExternalExecutionResultEvent{ReplyID: reply, ExecutionResults: []message.ToolResultBlock{{ID: "call-0", Output: out}}}
	}

	go func() {
		a.externalCh <- result("old", "stale")
		a.externalCh <- result("new", "fresh")
	}()
	if got := a.waitForExternalResult(ctx, answerIDs{reply: "new"}, "call-0"); got == nil || got.GetOutputText() != "fresh" {
		t.Fatalf("external result: %+v", got)
	}
	go func() { a.externalCh <- result("", "legacy") }()
	if got := a.waitForExternalResult(ctx, answerIDs{reply: "new"}, "call-0"); got == nil || got.GetOutputText() != "legacy" {
		t.Fatalf("an answer without a reply ID must still match: %+v", got)
	}
}

// TestConfirmWaiterDropsAnotherReplysAnswer is the same for confirmations:
// an approval meant for another reply must not approve this one's call.
func TestConfirmWaiterDropsAnotherReplysAnswer(t *testing.T) {
	a := NewUnifiedAgent("a", "sys", &seamModel{}, WithPermissionContext(permission.NewContext(permission.ModeDefault)))
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	confirm := func(reply string, ok bool) event.UserConfirmResultEvent {
		return event.UserConfirmResultEvent{ReplyID: reply, ConfirmResults: []event.ConfirmResult{{Confirmed: ok, ToolCall: message.ToolCallBlock{ID: "call-0"}}}}
	}
	go func() {
		a.confirmCh <- confirm("old", true)
		a.confirmCh <- confirm("new", false)
	}()
	if ok, _ := a.waitForConfirmation(ctx, answerIDs{reply: "new"}, "call-0"); ok {
		t.Fatal("an approval meant for another reply approved this reply's call")
	}
	if ctx.Err() != nil {
		t.Fatal("the waiters must not have timed out")
	}
}

// TestResumedCallAcceptsTheCheckpointReplyID: a plain checkpoint resume gives
// the reply a new ID, but a host may answer the request it saw before the
// restart, naming the checkpoint's reply ID, before the re-asked request
// arrives. That answer reaches the re-asked call; one naming any other reply
// does not.
func TestResumedCallAcceptsTheCheckpointReplyID(t *testing.T) {
	for _, tc := range []struct {
		name    string
		replyID string
		runs    int
	}{
		{"checkpoint reply", "r-old", 1},
		{"another reply", "r-other", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			echoExecutions = 0
			call := message.ToolCallBlock{Type: "tool_call", ID: "tc_ask", Name: "echo_a", Input: `{}`, State: message.ToolCallAsking}
			a := NewUnifiedAgent("cp-agent", "helpful", &mockChatModel{responses: []model.ChatResponse{
				{Content: []message.ContentBlock{message.TextBlock{Type: "text", Text: "resumed fine"}}, IsLast: true},
			}},
				WithToolkit(tool.NewToolkit(echoToolFixtureA())),
				WithPermissionContext(permission.NewContext(permission.ModeDefault)),
				WithReactConfig(ReactConfig{MaxIters: 4}),
				WithState(&AgentState{
					SchemaVersion: StateSchemaVersion, SessionID: "sess", ReplyID: "r-old",
					Context: []*message.Msg{
						message.UserMsg("user", "do the thing"),
						message.AssistantMsg("cp-agent", []message.ContentBlock{call}),
					},
				}),
				WithStateSaver(newFakeStateSaver()),
			)
			a.SubmitUserConfirm(&event.UserConfirmResultEvent{ReplyID: tc.replyID, ConfirmResults: []event.ConfirmResult{{Confirmed: true, ToolCall: call}}})
			ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
			defer cancel()
			ch, err := a.ReplyStream(ctx, "continue")
			if err != nil {
				t.Fatal(err)
			}
			for range ch {
			}
			if echoExecutions != tc.runs {
				t.Fatalf("the re-asked call ran %d times, want %d", echoExecutions, tc.runs)
			}
		})
	}
}
