package arms

import (
	"slices"

	"github.com/agentscope-ai/agentscope-go/v2/experiments/stage0/corpus"
)

// privateCodes returns the codes the outbound scan looks for: those in the user
// text and expected reply of edge_only turns, and in records read on those turns.
//
// corpus.Session.Canaries is wider. It also holds every code of a local record read
// on a shared turn, whose reply the rules allow to be published, so a hit there is
// not a leak. A shared reply that repeats a code from an earlier edge_only turn
// still counts, because the rules do not allow that.
func privateCodes(s corpus.Session) []string {
	set := map[string]bool{}
	for _, t := range s.Turns {
		if t.Disclosure != corpus.EdgeOnly {
			continue
		}
		for _, c := range corpus.Codes(t.User + "\n" + t.ExpectedReply) {
			set[c] = true
		}
		if t.ReadRecord != "" {
			for _, c := range corpus.Codes(s.Records[t.ReadRecord].Text) {
				set[c] = true
			}
		}
	}
	out := make([]string, 0, len(set))
	for c := range set {
		out = append(out, c)
	}
	slices.Sort(out)
	return out
}
