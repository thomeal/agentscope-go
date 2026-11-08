package s3

import (
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/agentscope-ai/agentscope-go/v2/experiments/stage0/corpus"
)

// The validators re-derive every promise from the generated data alone: they read
// the fields a harness would read rather than asking the generator what it did.
// They return tagged problems, so corruption tests can show each check fires.

type problems []string

func (p *problems) add(tag, format string, args ...any) {
	*p = append(*p, tag+": "+fmt.Sprintf(format, args...))
}

func (p problems) has(tag string) bool {
	return slices.ContainsFunc(p, func(s string) bool { return strings.HasPrefix(s, tag+": ") })
}

var (
	testCodeRe   = regexp.MustCompile(`\b[A-Z0-9]{3}-[0-9]{4}\b`)
	localIDRe    = regexp.MustCompile(`\bL-[0-9]+\b`)
	cloudIDRe    = regexp.MustCompile(`\bR-[0-9]+\b`)
	bareIntegers = regexp.MustCompile(`^[0-9]+$`)

	// kindWords must not appear in a query, in any spelling.
	kindWords = []string{
		"followup", "follow-up", "follow up",
		"reasoning",
		"local_tool", "local tool", "localtool",
		"cloud_context", "cloud context", "cloudcontext",
		"edge_private", "edge private", "edgeprivate",
	}
)

// Size bounds from the workload description, in EstimateTokens units.
const (
	maxRecentTokens = 250
	maxQueryTokens  = 400
	localWindow     = 8000

	// minStepsAfterRead keeps a cloud_context fact "older" under a router that looks
	// further back than RecentSteps. Written out here, not taken from the generator, so
	// loosening the generator fails this check.
	minStepsAfterRead = 6
)

// occurrences counts a value in each place it could live.
type occurrences struct {
	recentPublished, olderPublished int
	recentTool, olderTool           int
	localPublished, localTool       int
	cloudRecords, localRecords      int
}

func locate(it Item, needle string) occurrences {
	var o occurrences
	cut := len(it.History) - RecentSteps
	for i, st := range it.History {
		pub := strings.Count(st.User, needle) + strings.Count(st.Reply, needle)
		tool := strings.Count(st.ToolResult, needle)
		if i >= cut {
			o.recentPublished += pub
			o.recentTool += tool
		} else {
			o.olderPublished += pub
			o.olderTool += tool
		}
	}
	for _, st := range it.LocalOwn {
		o.localPublished += strings.Count(st.User, needle) + strings.Count(st.Reply, needle)
		o.localTool += strings.Count(st.ToolResult, needle)
	}
	for id, text := range it.Records {
		if strings.HasPrefix(id, "R-") {
			o.cloudRecords += strings.Count(text, needle)
		} else {
			o.localRecords += strings.Count(text, needle)
		}
	}
	return o
}

// queryLabel finds the one kind and subject a query asks about.
func queryLabel(query string) (kind, subject string, ok bool) {
	lower := strings.ToLower(query)
	n := 0
	for _, k := range kinds {
		if strings.Contains(lower, k) {
			kind = k
			n++
		}
	}
	m := 0
	for _, s := range subjects {
		if strings.Contains(lower, "the "+s) {
			subject = s
			m++
		}
	}
	return kind, subject, n == 1 && m == 1
}

func mentionsRecord(query string) bool {
	return strings.Contains(strings.ToLower(query), "record") || cloudIDRe.MatchString(query) || localIDRe.MatchString(query)
}

func historyTokens(it Item) int {
	return stepsTokens(it.History)
}

func stepsTokens(steps []corpus.CStep) int {
	n := 0
	for _, st := range steps {
		n += EstimateTokens(st.User) + EstimateTokens(st.ToolResult) + EstimateTokens(st.Reply)
	}
	return n
}

func validateItem(it Item) problems {
	var p problems
	checkShape(&p, it)
	checkSteps(&p, it)
	checkSizes(&p, it)
	checkQuery(&p, it)
	checkLocation(&p, it)
	checkUnpublished(&p, it)
	return p
}

func allText(it Item) []string {
	out := []string{it.ID, it.Set, it.System, it.Query, it.Answer, it.AnswerPrompt, it.PairID}
	for _, steps := range [][]corpus.CStep{it.History, it.LocalOwn} {
		for _, st := range steps {
			out = append(out, st.User, st.ToolName, st.ToolID, st.ToolResult, st.Reply, st.RecordID)
		}
	}
	for id, text := range it.Records {
		out = append(out, id, text)
	}
	return out
}

func checkShape(p *problems, it Item) {
	if it.ID == "" || it.Seed == 0 || (it.Set != SetTuning && it.Set != SetAcceptance) {
		p.add("shape", "id %q seed %d set %q", it.ID, it.Seed, it.Set)
	}
	if it.System != systemPrompt || it.AnswerPrompt != answerPrompt {
		p.add("shape", "%s has its own system or answer prompt, which could reveal its kind", it.ID)
	}
	if it.Owner != corpus.SideCloud && it.Owner != corpus.SideLocal {
		p.add("shape", "%s has owner %q", it.ID, it.Owner)
	}
	if want := it.Kind == KindEdgePrivate; want != (it.Disclosure == corpus.EdgeOnly) {
		p.add("shape", "%s of kind %s has disclosure %q", it.ID, it.Kind, it.Disclosure)
	}
	if it.Disclosure != corpus.Shared && it.Disclosure != corpus.EdgeOnly {
		p.add("shape", "%s has disclosure %q", it.ID, it.Disclosure)
	}
	if !testCodeRe.MatchString(it.Answer) && !bareIntegers.MatchString(it.Answer) && it.Answer != Unknown {
		p.add("shape", "%s has an answer that is not a code, an integer or %s", it.ID, Unknown)
	}
	if bareIntegers.MatchString(it.Answer) && it.Kind != KindReasoning {
		p.add("shape", "%s of kind %s has an integer answer", it.ID, it.Kind)
	}
	if len(testCodeRe.FindAllString(it.System+it.AnswerPrompt, -1)) > 0 {
		p.add("shape", "%s has a code in a fixed prompt", it.ID)
	}
	for _, s := range allText(it) {
		for i := range len(s) {
			if s[i] == '\r' || s[i] >= 0x80 {
				p.add("shape", "%s holds a CR or non-ASCII byte, which would tie the hash to its checkout", it.ID)
				return
			}
		}
	}
}

func checkSteps(p *problems, it Item) {
	if len(it.History) <= RecentSteps || len(it.LocalOwn) == 0 {
		p.add("step", "%s has %d history and %d local steps", it.ID, len(it.History), len(it.LocalOwn))
	}
	toolIDs := map[string]bool{}
	seenRecord := map[string]int{}
	check := func(steps []corpus.CStep, tool, prefix string) {
		for i, st := range steps {
			if st.User == "" || st.Reply == "" {
				p.add("step", "%s step %d has empty text", it.ID, i)
			}
			if st.RecordID == "" {
				if st.ToolName != "" || st.ToolID != "" || st.ToolResult != "" {
					p.add("step", "%s step %d has tool fields without a record", it.ID, i)
				}
				continue
			}
			if st.ToolName != tool || !strings.HasPrefix(st.RecordID, prefix) || st.ToolID == "" || toolIDs[st.ToolID] {
				p.add("step", "%s step %d: tool %q id %q record %q", it.ID, i, st.ToolName, st.ToolID, st.RecordID)
			}
			toolIDs[st.ToolID] = true
			seenRecord[st.RecordID]++
			if it.Records[st.RecordID] != st.ToolResult || st.ToolResult == "" {
				p.add("step", "%s step %d: tool result differs from record %s", it.ID, i, st.RecordID)
			}
			if !strings.Contains(st.User, st.RecordID) {
				p.add("step", "%s step %d does not ask for %s", it.ID, i, st.RecordID)
			}
		}
	}
	check(it.History, "read_record", "R-")
	check(it.LocalOwn, "read_local", "L-")

	unread := 0
	for id := range it.Records {
		switch {
		case strings.HasPrefix(id, "R-"):
			if seenRecord[id] != 1 {
				p.add("step", "%s cloud record %s read %d times", it.ID, id, seenRecord[id])
			}
		case strings.HasPrefix(id, "L-"):
			if seenRecord[id] == 0 {
				unread++
			}
		default:
			p.add("step", "%s has a record id %q on neither side", it.ID, id)
		}
	}
	if unread != 1 {
		p.add("step", "%s has %d unread local records, want 1 in every kind", it.ID, unread)
	}
	nums := map[string]bool{}
	for id := range it.Records {
		if nums[id[2:]] {
			p.add("step", "%s uses record number %s on both sides", it.ID, id[2:])
		}
		nums[id[2:]] = true
	}
}

func checkSizes(p *problems, it Item) {
	if n := historyTokens(it); n < 20_000 || n > 60_000 {
		p.add("size", "%s history is %d tokens", it.ID, n)
	}
	maxLocal := 0
	for id, text := range it.Records {
		n := EstimateTokens(text)
		if n < 500 || n > 3000 {
			p.add("size", "%s record %s is %d tokens", it.ID, id, n)
		}
		if strings.HasPrefix(id, "L-") {
			maxLocal = max(maxLocal, n)
		}
	}
	local := stepsTokens(it.LocalOwn)
	if local > 2000 {
		p.add("size", "%s local history is %d tokens", it.ID, local)
	}
	if n := EstimateTokens(PublishedRecent(it)); n > maxRecentTokens {
		p.add("size", "%s recent published text is %d tokens", it.ID, n)
	}
	if n := EstimateTokens(it.Query); n > maxQueryTokens {
		p.add("size", "%s query is %d tokens", it.ID, n)
	}
	// The local side must fit its own turns, the recent text, the query and one more
	// read, with room to spare for whatever else a strategy keeps.
	need := local + maxLocal + EstimateTokens(PublishedRecent(it)) + EstimateTokens(it.Query) + EstimateTokens(it.System)
	if need > localWindow*3/4 {
		p.add("size", "%s needs %d of a %d local window", it.ID, need, localWindow)
	}
}

func checkQuery(p *problems, it Item) {
	lower := strings.ToLower(it.Query)
	if it.Query == "" {
		p.add("query", "%s has an empty query", it.ID)
	}
	for _, w := range kindWords {
		if strings.Contains(lower, w) {
			p.add("query", "%s query holds the kind word %q", it.ID, w)
		}
	}
	if strings.Contains(it.Query, it.Answer) || strings.Contains(lower, strings.ToLower(it.Answer)) {
		p.add("query", "%s query holds its answer", it.ID)
	}
	if testCodeRe.MatchString(it.Query) {
		p.add("query", "%s query holds a code", it.ID)
	}
	if strings.Contains(it.Query, Unknown) {
		p.add("query", "%s query holds %s", it.ID, Unknown)
	}
	switch it.Kind {
	case KindReasoning:
		solved, err := solveReasoning(it.Query)
		switch {
		case err != nil:
			p.add("reasoning", "%s: %v", it.ID, err)
		case solved != it.Answer:
			p.add("reasoning", "%s: the independent solver got a different answer", it.ID)
		}
		if cloudIDRe.MatchString(it.Query) || localIDRe.MatchString(it.Query) {
			p.add("reasoning", "%s is not self-contained", it.ID)
		}
	default:
		if _, _, ok := queryLabel(it.Query); !ok {
			p.add("query", "%s query does not name exactly one label", it.ID)
		}
	}
}

// checkLocation holds each kind to where the package promises its answer lives.
func checkLocation(p *problems, it Item) {
	o := locate(it, it.Answer)
	zero := func(what string, n int) {
		if n != 0 {
			p.add("location", "%s of kind %s has its answer in %s (%d times)", it.ID, it.Kind, what, n)
		}
	}
	want := func(what string, n, exact int) {
		if n != exact {
			p.add("location", "%s of kind %s has its answer in %s %d times, want %d", it.ID, it.Kind, what, n, exact)
		}
	}
	switch it.Kind {
	case KindFollowup:
		want("recent published text", o.recentPublished, 1)
		zero("older published text", o.olderPublished)
		zero("a recent tool result", o.recentTool)
		zero("local published text", o.localPublished)
		zero("a local tool result", o.localTool)
		zero("a local record", o.localRecords)
		// The record-mentioning distractor keeps the value in the record it read.
		if it.Answer != Unknown && mentionsRecord(it.Query) {
			want("an older tool result", o.olderTool, 1)
			want("a cloud record", o.cloudRecords, 1)
		} else {
			zero("an older tool result", o.olderTool)
			zero("a cloud record", o.cloudRecords)
		}
	case KindCloudContext:
		want("an older tool result", o.olderTool, 1)
		want("a cloud record", o.cloudRecords, 1)
		zero("recent published text", o.recentPublished)
		zero("older published text", o.olderPublished)
		zero("a recent tool result", o.recentTool)
		zero("local published text", o.localPublished)
		zero("a local tool result", o.localTool)
		zero("a local record", o.localRecords)
		for i, st := range it.History {
			if strings.Contains(st.ToolResult, it.Answer) && len(it.History)-1-i < minStepsAfterRead {
				p.add("location", "%s reads its record only %d steps before the end", it.ID, len(it.History)-1-i)
			}
		}
	case KindLocalTool:
		want("a local record", o.localRecords, 1)
		zero("recent published text", o.recentPublished)
		zero("older published text", o.olderPublished)
		zero("a recent tool result", o.recentTool)
		zero("an older tool result", o.olderTool)
		zero("local published text", o.localPublished)
		zero("a local tool result", o.localTool)
		zero("a cloud record", o.cloudRecords)
		checkNamedLocal(p, it)
	case KindEdgePrivate:
		zero("recent published text", o.recentPublished)
		zero("older published text", o.olderPublished)
		zero("a recent tool result", o.recentTool)
		zero("an older tool result", o.olderTool)
		zero("a cloud record", o.cloudRecords)
		if mentionsRecord(it.Query) {
			want("a local record", o.localRecords, 1)
			zero("local published text", o.localPublished)
			zero("a local tool result", o.localTool)
			checkNamedLocal(p, it)
		} else {
			want("local published text", o.localPublished, 1)
			zero("a local record", o.localRecords)
		}
	case KindReasoning:
		// An integer can turn up in a log line by chance. A code cannot.
		if testCodeRe.MatchString(it.Answer) && o != (occurrences{}) {
			p.add("location", "%s of kind reasoning has its code answer in the session: %+v", it.ID, o)
		}
		if len(it.Records) == 0 {
			p.add("location", "%s has no records", it.ID)
		}
	default:
		p.add("location", "%s has unknown kind %q", it.ID, it.Kind)
	}
	checkAnswerLines(p, it)
}

// checkNamedLocal: the query names the record that holds the answer, and no step on
// either side has read it.
func checkNamedLocal(p *problems, it Item) {
	ids := localIDRe.FindAllString(it.Query, -1)
	if len(ids) != 1 {
		p.add("location", "%s names %d local records", it.ID, len(ids))
		return
	}
	if !strings.Contains(it.Records[ids[0]], it.Answer) {
		p.add("location", "%s names a local record without its answer", it.ID)
	}
	for _, st := range it.LocalOwn {
		if st.RecordID == ids[0] {
			p.add("location", "%s already read its local record", it.ID)
		}
	}
}

// checkAnswerLines: wherever the answer sits, its own line carries the label the query
// asks about, so it is the right value for the question and not a stray copy.
func checkAnswerLines(p *problems, it Item) {
	if it.Kind == KindReasoning {
		return
	}
	kind, subject, ok := queryLabel(it.Query)
	if !ok {
		return
	}
	found := 0
	for _, text := range allFacts(it) {
		for _, line := range strings.Split(text, "\n") {
			if !strings.Contains(line, it.Answer) {
				continue
			}
			found++
			lower := strings.ToLower(line)
			if !strings.Contains(lower, kind) || !strings.Contains(lower, subject) {
				p.add("location", "%s has its answer on a line without its label", it.ID)
			}
		}
	}
	if found == 0 {
		p.add("location", "%s has its answer nowhere", it.ID)
	}
}

func allFacts(it Item) []string {
	var out []string
	for _, steps := range [][]corpus.CStep{it.History, it.LocalOwn} {
		for _, st := range steps {
			out = append(out, st.User, st.Reply)
		}
	}
	for _, text := range it.Records {
		out = append(out, text)
	}
	return out
}

// checkUnpublished: nothing read from a record reaches published text. The one
// exception is a followup restating its own answer.
func checkUnpublished(p *problems, it Item) {
	published := map[string]bool{}
	for _, steps := range [][]corpus.CStep{it.History, it.LocalOwn} {
		for _, st := range steps {
			for _, c := range testCodeRe.FindAllString(st.User+"\n"+st.Reply, -1) {
				published[c] = true
			}
		}
	}
	for id, text := range it.Records {
		for _, c := range testCodeRe.FindAllString(text, -1) {
			if published[c] && !(it.Kind == KindFollowup && c == it.Answer) {
				p.add("published", "%s publishes %s from record %s", it.ID, c, id)
			}
		}
	}
	for _, steps := range [][]corpus.CStep{it.History, it.LocalOwn} {
		for _, st := range steps {
			if st.RecordID != "" && testCodeRe.MatchString(st.User+st.Reply) {
				p.add("published", "%s restates a code in the read of %s", it.ID, st.RecordID)
			}
		}
	}
}

// checkPair holds two items to the state-sensitive pair contract. Everything a
// query-only router sees is identical, and the histories differ only in where the
// fact lives.
func checkPair(p *problems, a, b Item) {
	follow, cloud := a, b
	if a.Kind == KindCloudContext {
		follow, cloud = b, a
	}
	if follow.Kind != KindFollowup || cloud.Kind != KindCloudContext {
		p.add("pair", "%s and %s are %s and %s", a.ID, b.ID, a.Kind, b.Kind)
		return
	}
	if follow.Query != cloud.Query {
		p.add("pair", "%s and %s have different query text", a.ID, b.ID)
	}
	if follow.Answer != cloud.Answer {
		p.add("pair", "%s and %s ask about different values", a.ID, b.ID)
	}
	if follow.Owner != cloud.Owner || follow.Seed != cloud.Seed || follow.Set != cloud.Set || follow.Disclosure != cloud.Disclosure {
		p.add("pair", "%s and %s differ beyond the fact's location", a.ID, b.ID)
	}
	if !slices.Equal(follow.LocalOwn, cloud.LocalOwn) {
		p.add("pair", "%s and %s have different local histories", a.ID, b.ID)
	}
	if len(follow.History) != len(cloud.History) {
		p.add("pair", "%s and %s have %d and %d history steps", a.ID, b.ID, len(follow.History), len(cloud.History))
		return
	}
	for i := range follow.History {
		f, c := follow.History[i], cloud.History[i]
		if f.RecordID != c.RecordID || f.ToolName != c.ToolName || f.ToolID != c.ToolID {
			p.add("pair", "%s and %s differ in tool shape at step %d", a.ID, b.ID, i)
		}
	}
	if d := historyTokens(follow) - historyTokens(cloud); d < -100 || d > 100 {
		p.add("pair", "%s and %s differ by %d history tokens, which a router could read", a.ID, b.ID, d)
	}
	// The difference is where the fact lives.
	if !strings.Contains(PublishedRecent(follow), follow.Answer) {
		p.add("pair", "%s does not state the fact recently", follow.ID)
	}
	if strings.Contains(PublishedRecent(cloud), cloud.Answer) {
		p.add("pair", "%s states the fact recently", cloud.ID)
	}
	if locate(cloud, cloud.Answer).olderTool != 1 {
		p.add("pair", "%s does not keep the fact in an older tool result", cloud.ID)
	}
	if !mentionsRecord(follow.Query) && locate(follow, follow.Answer).olderTool != 0 {
		p.add("pair", "%s keeps the fact in a tool result too", follow.ID)
	}
	// Simple state cues must not settle it: both recent texts hold the same number of
	// codes, and the cloud half holds both halves of the label on different lines.
	if len(testCodeRe.FindAllString(PublishedRecent(follow), -1)) != len(testCodeRe.FindAllString(PublishedRecent(cloud), -1)) {
		p.add("pair", "%s and %s have different code counts in recent text", a.ID, b.ID)
	}
	kind, subject, ok := queryLabel(follow.Query)
	if !ok {
		return
	}
	recent := strings.ToLower(PublishedRecent(cloud))
	if !strings.Contains(recent, kind) || !strings.Contains(recent, subject) {
		p.add("pair", "%s recent text lacks a near miss on the label", cloud.ID)
	}
	if statesLabel(recent, kind, subject) {
		p.add("pair", "%s recent text states the label", cloud.ID)
	}
	if !statesLabel(strings.ToLower(PublishedRecent(follow)), kind, subject) {
		p.add("pair", "%s recent text does not state the label", follow.ID)
	}
}

// statesLabel reports whether one line carries both halves of a label and a code.
func statesLabel(lowerText, kind, subject string) bool {
	for _, line := range strings.Split(lowerText, "\n") {
		if strings.Contains(line, kind) && strings.Contains(line, subject) {
			return true
		}
	}
	return false
}

// checkSet holds a whole list to the promises that span items.
func checkSet(p *problems, items []Item) {
	ids := map[string]bool{}
	queries := map[string][]Item{}
	pairs := map[string][]Item{}
	for _, it := range items {
		if ids[it.ID] {
			p.add("set", "duplicate id %s", it.ID)
		}
		ids[it.ID] = true
		queries[it.Query] = append(queries[it.Query], it)
		if it.PairID != "" {
			pairs[it.PairID] = append(pairs[it.PairID], it)
			if it.Kind != KindFollowup && it.Kind != KindCloudContext {
				p.add("set", "%s of kind %s has a pair id", it.ID, it.Kind)
			}
		}
	}
	for q, group := range queries {
		switch {
		case len(group) == 1 && group[0].PairID == "":
		case len(group) == 2 && group[0].PairID != "" && group[0].PairID == group[1].PairID:
		default:
			p.add("set", "query text %q is shared by %d items outside one pair", shorten(q), len(group))
		}
	}
	for id, group := range pairs {
		if len(group) != 2 {
			p.add("pair", "%s has %d items", id, len(group))
			continue
		}
		checkPair(p, group[0], group[1])
	}
}

func shorten(s string) string {
	if len(s) > 60 {
		return s[:60] + "..."
	}
	return s
}
