package model

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/agentscope-ai/agentscope-go/v2/pkg/agentscope/message"
)

func responsesText(resp *ChatResponse) string {
	var sb strings.Builder
	for _, b := range resp.Content {
		if tb, ok := b.(message.TextBlock); ok {
			sb.WriteString(tb.Text)
		}
	}
	return sb.String()
}

func TestOpenAIResponseChatReportsStatus(t *testing.T) {
	output := `"output":[{"type":"message","content":[{"type":"output_text","text":"partial answer"}]}],"usage":{"input_tokens":4,"output_tokens":9}`
	for _, tc := range []struct {
		name, body, wantErr string
	}{
		{"incomplete", `{"id":"r1","status":"incomplete","incomplete_details":{"reason":"max_output_tokens"},` + output + `}`, "response incomplete: max_output_tokens"},
		{"failed", `{"id":"r1","status":"failed","error":{"code":"server_error","message":"upstream failed"},` + output + `}`, "response failed: upstream failed"},
		{"completed", `{"id":"r1","status":"completed",` + output + `}`, ""},
		{"no status", `{"id":"r1",` + output + `}`, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := newResponsesStreamModel(t, func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprint(w, tc.body)
			})
			resp, err := m.Chat(context.Background(), []*message.Msg{message.UserMsg("u", "hi")})
			if err != nil {
				t.Fatalf("Chat: %v", err)
			}
			if responsesText(resp) != "partial answer" || resp.Usage == nil || resp.Usage.OutputTokens != 9 {
				t.Fatalf("content and usage must be kept: %+v", resp)
			}
			switch {
			case tc.wantErr == "" && resp.Error != nil:
				t.Fatalf("unexpected status error: %v", resp.Error)
			case tc.wantErr != "" && (resp.Error == nil || !strings.Contains(resp.Error.Error(), tc.wantErr)):
				t.Fatalf("Error = %v, want it to contain %q", resp.Error, tc.wantErr)
			}
		})
	}
}

func TestOpenAIResponseStreamReportsStatus(t *testing.T) {
	for _, tc := range []struct {
		name, terminal, wantErr string
	}{
		{"incomplete", `{"type":"response.incomplete","response":{"id":"r1","status":"incomplete","incomplete_details":{"reason":"max_output_tokens"},"usage":{"input_tokens":3,"output_tokens":5}}}`, "response incomplete: max_output_tokens"},
		{"incomplete without object", `{"type":"response.incomplete"}`, "response incomplete"},
		{"failed", `{"type":"response.failed","response":{"id":"r1","status":"failed","error":{"code":"server_error","message":""}}}`, "response failed: server_error"},
		{"completed", `{"type":"response.completed","response":{"id":"r1","status":"completed","usage":{"input_tokens":3,"output_tokens":5}}}`, ""},
		{"completed without status", `{"type":"response.completed","response":{"id":"r1"}}`, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := newResponsesStreamModel(t, func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				fmt.Fprint(w, sseDeltaLine("partial "))
				fmt.Fprint(w, sseDeltaLine("answer"))
				fmt.Fprint(w, "data: "+tc.terminal+"\n")
			})
			ch, err := m.ChatStream(context.Background(), []*message.Msg{message.UserMsg("u", "hi")})
			if err != nil {
				t.Fatalf("ChatStream: %v", err)
			}
			var final *ChatResponse
			for r := range ch {
				if r.IsLast {
					cp := r
					final = &cp
				}
			}
			if final == nil {
				t.Fatal("no final response")
			}
			if responsesText(final) != "partial answer" {
				t.Fatalf("accumulated content must be kept: %+v", final)
			}
			switch {
			case tc.wantErr == "" && final.Error != nil:
				t.Fatalf("unexpected status error: %v", final.Error)
			case tc.wantErr != "" && (final.Error == nil || !strings.Contains(final.Error.Error(), tc.wantErr)):
				t.Fatalf("Error = %v, want it to contain %q", final.Error, tc.wantErr)
			}
		})
	}
}
