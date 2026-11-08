// Package corpus generates the synthetic S1 evaluation workload for the stage 0
// experiment: long operations-troubleshooting sessions served alternately by a
// large-window cloud assistant and an 8K local assistant, with probe batches
// that measure which facts the current owner can still produce after a switch.
//
// Everything here is deterministic and offline. Generate, GenerateCBench, Pilot
// and Acceptance return byte-identical data for a seed, on any platform and Go
// version, because the package owns its random number generator (see prng).
// The corpus assumes no particular context strategy.
//
// Token counts use EstimateTokens (4 ASCII characters per token), counted over
// user text and tool results only where a threshold matters. Replies come from
// a live model at run time and only add to the growth figures.
package corpus

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"regexp"
	"slices"
)

// Side names one of the two assistants.
type Side string

// The two assistants.
const (
	SideCloud Side = "cloud"
	SideLocal Side = "local"
)

// Disclosure marks whether a turn's reply may be shared across sides.
type Disclosure string

// Disclosure values. Cloud turns are always Shared.
const (
	Shared   Disclosure = "shared"
	EdgeOnly Disclosure = "edge_only"
)

// Category is a fact category.
type Category string

// Fact categories, as defined by workload S1.
const (
	F1 Category = "F1" // a constraint the user stated
	F2 Category = "F2" // a decision in the owner's reply, derived by applying a rule
	F3 Category = "F3" // exists only in a cloud tool result
	F4 Category = "F4" // a value corrected later; only the newest is right
	F5 Category = "F5" // the local side's own fact
	F6 Category = "F6" // never mentioned; the only right answer is UNKNOWN
)

// Unknown is the only correct answer to an F6 question.
const Unknown = "UNKNOWN"

// Set names accepted by Generate and GenerateCBench.
const (
	SetTuning     = "tuning"
	SetAcceptance = "acceptance"
)

// Supported session lengths for Generate. The lower bound is what a long cloud
// segment plus at least three switches needs; the upper bound is what the
// label vocabulary can serve without reusing a label.
const (
	MinLength = 20
	MaxLength = 100
)

// Seeds are ranges far enough apart that no realistic n can make them overlap.
const (
	pilotSeedBase            int64 = 7_100_000
	acceptanceSeedBase       int64 = 7_900_000
	cbenchPilotSeedBase      int64 = 8_100_000
	cbenchAcceptanceSeedBase int64 = 8_900_000
)

// Record is a deterministic tool result. FactIDs lists the facts it carries;
// every other code in Text is a distractor.
type Record struct {
	ID      string
	Side    Side
	Text    string
	FactIDs []string
}

// Turn is one scripted user turn.
//
// ExpectedReply is an addition to the base contract: a canned correct reply
// that restates nothing the user asked to keep out of it. It exists for tests,
// dry runs and Exposure checks. Live runs use the model's actual reply.
type Turn struct {
	Index         int
	Owner         Side
	User          string
	Disclosure    Disclosure
	ReadRecord    string   // record id the user asks to read, "" if none
	FactIDs       []string // facts introduced in this turn
	ExpectedReply string
}

// Fact is one scoreable value.
//
// For F4, IntroducedTurn is the first statement of any value, Value is the
// newest, Superseded holds the older values oldest first, and SupersededTurn is
// the turn that stated the newest value. Probes ask an F4 fact only from
// SupersededTurn onwards, so its answer never depends on when it is asked.
//
// F6 facts have IntroducedTurn and SupersededTurn -1, Where "" and Value
// Unknown. In a CBench, IntroducedTurn indexes History, or LocalOwn when Where
// is "local_tool" or "edge_reply".
//
// Kind and Subject are additions to the base contract: the two halves of the
// label, which is unique across a session.
type Fact struct {
	ID             string
	Category       Category
	Value          string
	Superseded     []string
	IntroducedTurn int
	SupersededTurn int // -1 if none
	Label          string
	Where          string // "user", "reply", "cloud_tool", "local_tool", "edge_reply"
	Kind           string
	Subject        string
}

// Question is one probe question. Stale holds the superseded values of an F4
// fact, for counting stale answers separately.
type Question struct {
	ID       string
	Text     string
	Category Category
	FactID   string
	Answer   string
	Stale    []string
}

// ProbeBatch is one batch of questions addressed to one side.
type ProbeBatch struct {
	AfterTurn int
	Recipient Side
	Reason    string // "switch" or "end"
	Questions []Question
	Prompt    string
}

// Session is one complete S1 session.
type Session struct {
	ID      string
	Seed    int64
	Set     string // "tuning" or "acceptance"
	Length  int
	System  string // short domain system prompt, no session marker
	Turns   []Turn
	Records map[string]Record
	Facts   map[string]Fact
	Probes  []ProbeBatch
}

// CStep is one scripted exchange in a component benchmark. RecordID is an
// addition to the base contract: the id argument of the scripted tool call, ""
// when the step has none.
type CStep struct {
	User       string
	ToolName   string
	ToolID     string
	ToolResult string
	Reply      string
	RecordID   string
}

// CBench is one component-benchmark item: a fixed cloud history, a small local
// history, and probes addressed to the local side after the whole history.
type CBench struct {
	ID       string
	Seed     int64
	Set      string
	System   string
	History  []CStep // about 100K tokens of scripted cloud history
	LocalOwn []CStep // about 3K tokens of scripted local history holding F5 facts
	Probes   []ProbeBatch
	Facts    map[string]Fact
	Canaries []string
}

// Pilot returns the 30 tuning sessions: 20 of length 20 and 10 of length 50.
func Pilot() []Session {
	out := make([]Session, 0, 30)
	for i := range 30 {
		out = append(out, Generate(pilotSeedBase+int64(i), mixLength(i), SetTuning))
	}
	return out
}

// Acceptance returns n acceptance sessions in the same 2:1 mix as Pilot, with
// seeds disjoint from Pilot. Each item depends only on its index, so
// Acceptance(k)[:j] equals Acceptance(j) for j <= k.
func Acceptance(n int) []Session {
	if n <= 0 {
		return nil
	}
	out := make([]Session, 0, n)
	for i := range n {
		out = append(out, Generate(acceptanceSeedBase+int64(i), mixLength(i), SetAcceptance))
	}
	return out
}

// mixLength gives the 2:1 mix of length 20 and length 50 by index alone.
func mixLength(i int) int {
	if i%3 == 2 {
		return 50
	}
	return 20
}

// CBenchPilot returns the 10 tuning benchmark items.
func CBenchPilot() []CBench {
	out := make([]CBench, 0, 10)
	for i := range 10 {
		out = append(out, GenerateCBench(cbenchPilotSeedBase+int64(i), SetTuning))
	}
	return out
}

// CBenchAcceptance returns n acceptance benchmark items with seeds disjoint from
// CBenchPilot and the same stable prefix property as Acceptance.
func CBenchAcceptance(n int) []CBench {
	if n <= 0 {
		return nil
	}
	out := make([]CBench, 0, n)
	for i := range n {
		out = append(out, GenerateCBench(cbenchAcceptanceSeedBase+int64(i), SetAcceptance))
	}
	return out
}

// RecordTool returns the record tool for a side: read_record for cloud and
// read_local for local. Both take one parameter, {"id": string}.
func RecordTool(side Side) (name, description string) {
	if side == SideLocal {
		return "read_local", `Read a local-side record by id. Takes one string parameter, "id". Returns the full record text, or an error when the id is unknown here.`
	}
	return "read_record", `Read an operations record by id. Takes one string parameter, "id". Returns the full record text, or an error when the id is unknown here.`
}

// Read is the deterministic record tool. It reports false when the id is unknown
// or belongs to the other side.
func (s Session) Read(side Side, id string) (string, bool) {
	r, ok := s.Records[id]
	if !ok || r.Side != side {
		return "", false
	}
	return r.Text, true
}

// Read serves a benchmark's record tool from its scripted steps. It reports
// false when the id is unknown or belongs to the other side.
func (c CBench) Read(side Side, id string) (string, bool) {
	steps := c.History
	if side == SideLocal {
		steps = c.LocalOwn
	}
	if id == "" {
		return "", false
	}
	for _, st := range steps {
		if st.RecordID == id {
			return st.ToolResult, true
		}
	}
	return "", false
}

// Canaries returns every code that may appear only in edge-only or local-private
// material, sorted: all codes in local records (distractors included) and all
// codes in the user text and expected reply of edge-only turns. Codes never
// repeat across labels, so finding one in outbound material is a real leak of
// that material.
func (s Session) Canaries() []string {
	set := map[string]bool{}
	for _, r := range s.Records {
		if r.Side == SideLocal {
			for _, c := range Codes(r.Text) {
				set[c] = true
			}
		}
	}
	for _, t := range s.Turns {
		if t.Disclosure != EdgeOnly {
			continue
		}
		for _, c := range Codes(t.User + "\n" + t.ExpectedReply) {
			set[c] = true
		}
	}
	return sortedKeys(set)
}

func sortedKeys(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

var codePattern = regexp.MustCompile(`\b[A-Z0-9]{3}-[0-9]{4}\b`)

// Codes returns every code-shaped string in text, in order of appearance.
func Codes(text string) []string {
	return codePattern.FindAllString(text, -1)
}

// EstimateTokens is the corpus's token estimate: 1 token per 4 ASCII characters,
// rounded up.
func EstimateTokens(s string) int {
	return (len(s) + 3) / 4
}

// Hash returns the SHA-256 of a canonical JSON dump of Pilot and CBenchPilot.
// encoding/json sorts map keys, so the dump is canonical for these types.
func Hash() string {
	return hashJSON(struct {
		Pilot  []Session
		CBench []CBench
	}{Pilot(), CBenchPilot()})
}

// PilotHash returns the SHA-256 of a canonical JSON dump of Pilot alone.
func PilotHash() string { return hashJSON(Pilot()) }

// CBenchHash returns the SHA-256 of a canonical JSON dump of CBenchPilot alone.
func CBenchHash() string { return hashJSON(CBenchPilot()) }

func hashJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		// These types hold only strings, ints and containers of them.
		panic("corpus: marshal: " + err.Error())
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
