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
