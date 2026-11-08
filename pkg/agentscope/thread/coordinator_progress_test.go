package thread

import (
	"testing"

	"github.com/agentscope-ai/agentscope-go/v2/pkg/agentscope/event"
)

func TestPlanText(t *testing.T) {
	plan := func(v map[string]any) event.Event { return event.CustomEvent{Name: planEvent, Value: v} }
	cases := []struct {
		name string
		ev   event.Event
		want string
	}{
		{"other event", event.TextBlockDeltaEvent{Delta: "x"}, ""},
		{"other custom event", event.CustomEvent{Name: "turn_stats"}, ""},
		{"no plan", plan(map[string]any{}), ""},
		{"only empty steps", plan(map[string]any{"plan": []any{map[string]any{"step": " "}, "junk"}}), ""},
		{"steps and explanation", plan(map[string]any{
			"plan": []any{
				map[string]any{"step": "Book the\nflight", "status": "completed"},
				map[string]any{"step": "", "status": "pending"},
				map[string]any{"step": "Book the hotel", "status": "in_progress"},
			},
			"explanation": "  flight\tdone ",
		}), "1. [completed] Book the flight\n2. [in_progress] Book the hotel\nflight done"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := planText(tc.ev); got != tc.want {
				t.Fatalf("planText = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestRestartNoteFromABrief: an owner resuming from a Transfer's brief is
// pointed at the brief, with any joined inputs quoted, and never gets the
// request it may not read.
func TestRestartNoteFromABrief(t *testing.T) {
	in := func(seq uint64, text string) *Entry { return &Entry{Seq: seq, Content: textContent(text)} }
	tail := ". Work done before the restart may or may not have taken effect: check the actual state before you redo anything. Continue the request.]"
	for _, tc := range []struct {
		name string
		with []*Entry
		want string
	}{
		{"alone", nil, "[The session restarted while you were continuing the request handed to you in #4" + tail},
		{"one input", []*Entry{in(6, "Friday")}, `[The session restarted while you were continuing the request handed to you in #4, with the user's message #6 "Friday"` + tail},
		{"two inputs", []*Entry{in(6, "Friday"), in(7, "the invoice")},
			`[The session restarted while you were continuing the request handed to you in #4, with the user's messages #6 "Friday", #7 "the invoice"` + tail},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := &activation{briefSeq: 4, cycleInput: 1, userText: "the door code is 4711", recoverWith: tc.with}
			if got := restartNote(a, ""); got != tc.want {
				t.Fatalf("note:\n%s\nwant\n%s", got, tc.want)
			}
		})
	}
}
