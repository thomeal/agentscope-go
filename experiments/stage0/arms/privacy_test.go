package arms

import (
	"slices"
	"testing"

	"github.com/agentscope-ai/agentscope-go/v2/experiments/stage0/corpus"
)

func TestPrivateCodesCoverEdgeOnlyTurnsAndSkipSharedRecords(t *testing.T) {
	s := corpus.Session{
		Records: map[string]corpus.Record{
			"L-1": {ID: "L-1", Side: corpus.SideLocal, Text: "token AAA-1111"},
			"L-2": {ID: "L-2", Side: corpus.SideLocal, Text: "token BBB-2222"},
			"L-3": {ID: "L-3", Side: corpus.SideLocal, Text: "token FFF-6666"},
		},
		Turns: []corpus.Turn{
			{Owner: corpus.SideLocal, Disclosure: corpus.EdgeOnly, ReadRecord: "L-1", User: "note CCC-3333"},
			{Owner: corpus.SideLocal, Disclosure: corpus.Shared, ReadRecord: "L-2", User: "note DDD-4444"},
			{Owner: corpus.SideLocal, Disclosure: corpus.EdgeOnly, ExpectedReply: "chose EEE-5555"},
		},
	}
	got := privateCodes(s)
	want := []string{"AAA-1111", "CCC-3333", "EEE-5555"}
	if !slices.Equal(got, want) {
		t.Errorf("private codes = %v, want %v", got, want)
	}
}

func TestPrivateCodesIsANonEmptySubsetOfCanariesOnThePilot(t *testing.T) {
	for _, s := range corpus.Pilot() {
		private := privateCodes(s)
		if len(private) == 0 {
			t.Fatalf("%s has no private codes, so a zero scan would prove nothing", s.ID)
		}
		wide := s.Canaries()
		for _, c := range private {
			if !slices.Contains(wide, c) {
				t.Errorf("%s: %s is private but not a canary", s.ID, c)
			}
		}
	}
}
