// Package s3 generates the synthetic S3 routing workload for the stage 0
// experiment. A session is served by a large-context cloud assistant and an 8K
// local assistant. Each Item is one routing point: a session history plus one new
// query with a deterministic right answer. The harness runs the query on both
// sides, scores each with Score, and asks routers which side they would have
// chosen. This package supplies only the items; it assumes no router, context
// strategy or model.
//
// Everything is deterministic and offline. Tuning, Acceptance and Pilot return
// byte-identical data on any platform and Go version, because the package owns
// its random number generator and holds its text in interpreted Go strings.
//
// Where an answer lives, by kind. "Published" means the User and Reply fields of
// a step; ToolResult never is. "Recent" means the last RecentSteps steps of
// History, whoever the current owner is.
//
//	followup       recent published text of History. Older record too only when
//	               the query mentions a record, as a distractor.
//	reasoning      nowhere: it must be worked out from the query alone.
//	local_tool     one L- record that LocalOwn has not read. The query names it.
//	cloud_context  one older tool result in History, never published.
//	edge_private   the local side only, with Disclosure EdgeOnly: an unread L-
//	               record the query names, or a LocalOwn message.
//
// At least 40 percent of items sit in state-sensitive pairs: a followup and a
// cloud_context with the same query word for word, the same answer, and histories
// that differ only in where that fact lives. Near-miss labels fill the other
// half's slots, so the recent text of a cloud_context half holds both halves of
// the label, on separate lines, with other codes.
//
// HOST ONLY. Answer, Kind, PairID, ID, Seed and Records must never reach a model
// or a router. ToolResult fields of History must never reach the local side except
// through what a summary carries, and L- records and LocalOwn must never reach the
// cloud side. See Read for the one sanctioned way to serve a record.
package s3

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"

	"github.com/agentscope-ai/agentscope-go/v2/experiments/stage0/corpus"
)

// Kind is what decides an item's route.
type Kind string

// The item kinds. Comments give the expected cheapest correct route; the harness
// measures the real one.
const (
	KindFollowup     Kind = "followup"      // local: recently published
	KindReasoning    Kind = "reasoning"     // cloud: self-contained and hard
	KindLocalTool    Kind = "local_tool"    // local: only its tool reads the record
	KindCloudContext Kind = "cloud_context" // cloud: only in an unpublished tool result
	KindEdgePrivate  Kind = "edge_private"  // local: disclosure rules keep it there
)

// Set names.
const (
	SetTuning     = "tuning"
	SetAcceptance = "acceptance"
)

// Unknown is the answer when the published text says a value no longer exists.
const Unknown = "UNKNOWN"

// RecentSteps is how many steps at the end of History count as recent. A router
// that sees recent history must see at least these, even when Owner is local.
const RecentSteps = 3

// PilotSize is the number of items in Pilot.
const PilotSize = blockItems

// Seeds are ranges far enough apart that no realistic n can make them overlap.
const (
	tuningSeedBase     int64 = 9_100_000
	acceptanceSeedBase int64 = 9_900_000
)

// Item is one routing point. The two items of a pair share a Seed, because their
// histories come from one draw.
type Item struct {
	ID           string
	Set          string // "tuning" or "acceptance"
	Seed         int64
	System       string         // short domain system prompt
	History      []corpus.CStep // scripted cloud-side history, oldest first
	LocalOwn     []corpus.CStep // scripted local-side turns, oldest first
	Owner        corpus.Side    // current owner at the routing point
	Query        string         // the user's new input; does not reveal Kind
	Disclosure   corpus.Disclosure
	Kind         Kind
	PairID       string            // shared by the two halves of a pair, "" otherwise
	Answer       string            // host only, never sent to a model
	Records      map[string]string // id -> record text; host only, served through Read
	AnswerPrompt string            // appended to Query when scoring
}

// Tuning returns n tuning items. See Acceptance.
func Tuning(n int) []Item { return build(SetTuning, tuningSeedBase, n) }

// Acceptance returns n acceptance items, with seeds, answers and queries disjoint
// from Tuning. Every multiple of PilotSize holds exact kind proportions, and other
// sizes stay close to them. No size splits a pair: where the n-th item would be
// half of one, the next unpaired item takes its place, so only the last item of a
// list can differ from a longer list's item at that index.
func Acceptance(n int) []Item { return build(SetAcceptance, acceptanceSeedBase, n) }

// Pilot returns the 60 tuning items: 15 followup, 12 reasoning, 12 local_tool, 18
// cloud_context and 3 edge_private, with 13 pairs.
func Pilot() []Item { return Tuning(PilotSize) }

// Hash returns the SHA-256 of a canonical JSON dump of Pilot. encoding/json sorts
// map keys, so the dump is canonical for these types.
func Hash() string {
	b, err := json.Marshal(Pilot())
	if err != nil {
		// Item holds only strings, ints and containers of them.
		panic("s3: marshal: " + err.Error())
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// Read is the deterministic record tool. It reports false when the id is unknown
// or belongs to the other side: the cloud reads only R- ids and the local side
// only L- ids.
func Read(it Item, side corpus.Side, id string) (string, bool) {
	prefix := ""
	switch side {
	case corpus.SideCloud:
		prefix = "R-"
	case corpus.SideLocal:
		prefix = "L-"
	default:
		return "", false
	}
	if !strings.HasPrefix(id, prefix) {
		return "", false
	}
	text, ok := it.Records[id]
	return text, ok
}

// Score reports whether a reply is exactly the answer. It tolerates ASCII case and
// surrounding whitespace and nothing else, so extra words, punctuation, markdown,
// a longer value or a list of values all score false.
func Score(it Item, reply string) bool {
	if it.Answer == "" {
		return false
	}
	return foldASCII(strings.TrimSpace(reply)) == foldASCII(it.Answer)
}

// foldASCII upper-cases only ASCII letters. Unicode case folding would let lookalike
// runes such as the Kelvin sign pass for letters in a code.
func foldASCII(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'a' && c <= 'z' {
			b[i] = c - 'a' + 'A'
		}
	}
	return string(b)
}

// PublishedRecent returns the published text of the recent steps of History, oldest
// first: the view a router or the local side gets of what was said lately. It
// never includes a tool result. In a followup item it does include the answer,
// which is the point of a followup.
func PublishedRecent(it Item) string {
	steps := it.History
	if len(steps) > RecentSteps {
		steps = steps[len(steps)-RecentSteps:]
	}
	var b strings.Builder
	for _, st := range steps {
		b.WriteString(st.User)
		b.WriteByte('\n')
		b.WriteString(st.Reply)
		b.WriteByte('\n')
	}
	return b.String()
}

// EstimateTokens is the package's token estimate, counted line by line: 2
// characters per token on a line holding a code, 4 on any other line, rounded up.
// Newlines count as a character.
func EstimateTokens(s string) int {
	n := 0
	for len(s) > 0 {
		line, rest, _ := strings.Cut(s, "\n")
		n += lineTokens(line)
		s = rest
	}
	return n
}

func lineTokens(line string) int {
	chars := len(line) + 1
	if hasCode(line) {
		return (chars + 1) / 2
	}
	return (chars + 3) / 4
}

// hasCode reports whether a line holds something shaped like a code, such as
// K7P-2941.
func hasCode(line string) bool {
	for i := 3; i+4 < len(line); i++ {
		if line[i] == '-' &&
			isCodeChar(line[i-3]) && isCodeChar(line[i-2]) && isCodeChar(line[i-1]) &&
			isDigit(line[i+1]) && isDigit(line[i+2]) && isDigit(line[i+3]) && isDigit(line[i+4]) {
			return true
		}
	}
	return false
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

func isCodeChar(c byte) bool { return isDigit(c) || (c >= 'A' && c <= 'Z') }
