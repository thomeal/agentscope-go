package corpus

import (
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// The validators re-derive every promise from the generated data alone: they
// parse rules, records and prompts rather than asking the generator what it did.
// They return tagged problems, so corruption tests can show each check fires.

type problems []string

func (p *problems) add(tag, format string, args ...any) {
	*p = append(*p, tag+": "+fmt.Sprintf(format, args...))
}

func (p problems) has(tag string) bool {
	return slices.ContainsFunc(p, func(s string) bool { return strings.HasPrefix(s, tag+": ") })
}

var (
	candidateRe = regexp.MustCompile(`(?m)^- ([A-Z0-9]{3}-[0-9]{4}) \((.+) ([0-9]+)\)$`)
	ruleRe      = regexp.MustCompile(`Rule: choose the candidate with the (highest|lowest) (.+)\. State the chosen code in your reply\.`)

	// Every question must fit one of the forms, so F6 cannot stand out by wording.
	questionForms = []*regexp.Regexp{
		regexp.MustCompile(`^Which .+ did the user require for the .+\?$`),
		regexp.MustCompile(`^Which .+ was chosen in the decision about the .+\?$`),
		regexp.MustCompile(`^What is the current .+ for the .+\?$`),
		regexp.MustCompile(`^What .+ does record R-[0-9]+ give for the .+\?$`),
		regexp.MustCompile(`^What .+ does local record L-[0-9]+ give for the .+\?$`),
	}
	recordRefRe = regexp.MustCompile(`\b[RL]-[0-9]+\b`)
)

// deriveDecision applies the rule stated in a user message to its candidates.
func deriveDecision(user string) (string, bool) {
	rule := ruleRe.FindStringSubmatch(user)
	cands := candidateRe.FindAllStringSubmatch(user, -1)
	if rule == nil || len(cands) < 2 {
		return "", false
	}
	chosen, best := "", 0
	for _, c := range cands {
		if c[2] != rule[2] {
			return "", false
		}
		v, err := strconv.Atoi(c[3])
		if err != nil {
			return "", false
		}
		if chosen == "" || (rule[1] == "highest" && v > best) || (rule[1] == "lowest" && v < best) {
			chosen, best = c[1], v
		}
	}
	return chosen, true
}

func labelPhrase(f Fact) string {
	return strings.ToLower(f.Kind + " for the " + f.Subject)
}

// firstAskable recomputes when a fact's newest value exists. It deliberately does
// not call the generator's readyTurn: a bug there would otherwise blind this check
// too, as a mutation test once showed.
func firstAskable(f Fact) int {
	return max(f.IntroducedTurn, f.SupersededTurn)
}

var pessimisticRe = regexp.MustCompile(`[A-Za-z]+|[0-9]+|[^\sA-Za-z0-9]`)

// pessimisticTokens is a stand-in for a real tokenizer, which this offline test
// cannot run. It charges more than a real one usually would: a letter run costs
// one token per 5 letters, a digit run one per 3 digits, each punctuation mark
// one. It bounds sizes from above, where EstimateTokens may fall short on codes
// and timestamps.
func pessimisticTokens(s string) int {
	n := 0
	for _, run := range pessimisticRe.FindAllString(s, -1) {
		switch c := run[0]; {
		case c >= '0' && c <= '9':
			n += (len(run) + 2) / 3
		case (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z'):
			n += (len(run) + 4) / 5
		default:
			n++
		}
	}
	return n
}

func isASCII(s string) bool {
	for i := range len(s) {
		if s[i] >= 0x80 {
			return false
		}
	}
	return true
}

// checkLabels applies to sessions and benchmark items alike.
func checkLabels(p *problems, facts map[string]Fact) {
	seen := map[string]string{}
	for key, f := range facts {
		if key != f.ID {
			p.add("label", "fact key %s holds id %s", key, f.ID)
		}
		if f.Label == "" || len(Codes(f.Label)) > 0 || strings.Contains(f.Label, f.Value) {
			p.add("label", "%s has an empty or code-bearing label %q", f.ID, f.Label)
		}
		phrase := labelPhrase(f)
		if other, dup := seen[phrase]; dup {
			p.add("label", "%s and %s share the label %q", f.ID, other, phrase)
		}
		seen[phrase] = f.ID
	}
}

// checkF6Absent shows F6 is unanswerable: neither its label nor the word UNKNOWN
// appears anywhere an assistant could have read it, distractors included.
func checkF6Absent(p *problems, facts map[string]Fact, material []string) {
	joined := strings.ToLower(strings.Join(material, "\n"))
	if strings.Contains(strings.ToUpper(joined), Unknown) {
		p.add("f6-leak", "%q appears in session material", Unknown)
	}
	for _, f := range facts {
		if f.Category != F6 {
			continue
		}
		if f.Value != Unknown || f.IntroducedTurn != -1 || f.SupersededTurn != -1 || f.Where != "" {
			p.add("f6-shape", "%s is not shaped like an F6 fact: %+v", f.ID, f)
		}
		if strings.Contains(joined, labelPhrase(f)) {
			p.add("f6-leak", "%s label %q appears in session material", f.ID, labelPhrase(f))
		}
	}
}

// checkBatch applies to every batch in sessions and benchmark items.
func checkBatch(p *problems, b ProbeBatch, facts map[string]Fact, seenIDs map[string]bool) {
	if n := len(b.Questions); n == 0 || n > MaxBatchQuestions {
		p.add("batch-size", "batch after turn %d has %d questions", b.AfterTurn, n)
	}
	nF6 := 0
	inBatch := map[string]bool{}
	for _, q := range b.Questions {
		f, ok := facts[q.FactID]
		if !ok {
			p.add("batch-answer", "%s asks about unknown fact %q", q.ID, q.FactID)
			continue
		}
		if seenIDs[q.ID] {
			p.add("batch-ids", "question id %s repeats", q.ID)
		}
		seenIDs[q.ID] = true
		if inBatch[q.FactID] {
			p.add("batch-ids", "fact %s asked twice in one batch", q.FactID)
		}
		inBatch[q.FactID] = true
		if q.Category != f.Category || q.Answer != f.Value || !slices.Equal(q.Stale, f.Superseded) {
			p.add("batch-answer", "%s disagrees with fact %s", q.ID, f.ID)
		}
		if q.Category == F6 {
			nF6++
			if q.Answer != Unknown {
				p.add("batch-answer", "F6 %s answers %q", q.ID, q.Answer)
			}
		}
		if q.Category == F5 && b.Recipient != SideLocal {
			p.add("batch-f5", "%s is F5 in a %s batch", q.ID, b.Recipient)
		}
		if len(Codes(q.Text)) > 0 {
			p.add("batch-leak", "%s text holds a code: %q", q.ID, q.Text)
		}
		if !slices.ContainsFunc(questionForms, func(re *regexp.Regexp) bool { return re.MatchString(q.Text) }) {
			p.add("batch-form", "%s fits no question form: %q", q.ID, q.Text)
		}
		if !strings.Contains(b.Prompt, q.ID+": "+q.Text+"\n") {
			p.add("batch-prompt", "prompt omits %s", q.ID)
		}
		if f.Category != F6 && strings.Contains(b.Prompt, f.Value) {
			p.add("batch-leak", "prompt reveals the answer to %s", q.ID)
		}
		for _, old := range f.Superseded {
			if strings.Contains(b.Prompt, old) {
				p.add("batch-leak", "prompt reveals a superseded value of %s", q.ID)
			}
		}
	}
	if nF6 == 0 {
		p.add("batch-f6", "batch after turn %d has no F6 question", b.AfterTurn)
	}
	if len(Codes(b.Prompt)) > 0 {
		p.add("batch-leak", "prompt holds code-shaped text: %v", Codes(b.Prompt))
	}
	for _, want := range []string{"at most 3 tool calls", "only JSON", Unknown, `{"answers":[{"id":"`, `"answer":"`} {
		if !strings.Contains(b.Prompt, want) {
			p.add("batch-prompt", "prompt lacks %q", want)
		}
	}
	if b.Recipient == SideCloud && strings.Contains(b.Prompt, "local record") {
		p.add("batch-leak", "cloud prompt names a local record")
	}
}

type cloudSegment struct {
	start, end int
	bounded    bool
	growth     int // by EstimateTokens
	worst      int // by pessimisticTokens
}

// cloudSegments measures each run of cloud turns over user text and record text
// plus the canned reply. A segment is bounded when a switch sits on both sides.
func cloudSegments(s Session) []cloudSegment {
	var out []cloudSegment
	for i := 0; i < len(s.Turns); {
		if s.Turns[i].Owner != SideCloud {
			i++
			continue
		}
		seg := cloudSegment{start: i}
		for i < len(s.Turns) && s.Turns[i].Owner == SideCloud {
			t := s.Turns[i]
			seg.growth += EstimateTokens(t.User) + EstimateTokens(t.ExpectedReply)
			seg.worst += pessimisticTokens(t.User) + pessimisticTokens(t.ExpectedReply)
			if t.ReadRecord != "" {
				seg.growth += EstimateTokens(s.Records[t.ReadRecord].Text)
				seg.worst += pessimisticTokens(s.Records[t.ReadRecord].Text)
			}
			i++
		}
		seg.end = i - 1
		seg.bounded = seg.start > 0 && i < len(s.Turns)
		out = append(out, seg)
	}
	return out
}

func sessionMaterial(s Session) []string {
	out := []string{s.System}
	for _, t := range s.Turns {
		out = append(out, t.User, t.ExpectedReply)
	}
	for _, r := range s.Records {
		out = append(out, r.Text)
	}
	return out
}

func validateSession(s Session) problems {
	var p problems
	checkSessionShape(&p, s)
	checkSessionRecords(&p, s)
	checkSessionCodes(&p, s)
	checkSessionFacts(&p, s)
	checkLabels(&p, s.Facts)
	checkF6Absent(&p, s.Facts, sessionMaterial(s))
	checkSessionProbes(&p, s)
	checkSessionCanaries(&p, s)
	return p
}

func checkSessionShape(p *problems, s Session) {
	if len(s.Turns) != s.Length {
		p.add("structure", "%d turns for length %d", len(s.Turns), s.Length)
	}
	switches, local, edge := 0, 0, 0
	for i, t := range s.Turns {
		if t.Index != i {
			p.add("structure", "turn %d has index %d", i, t.Index)
		}
		if i > 0 && s.Turns[i-1].Owner != t.Owner {
			switches++
		}
		if t.Owner == SideCloud && t.Disclosure != Shared {
			p.add("edge", "cloud turn %d is %s", i, t.Disclosure)
		}
		if t.Owner == SideLocal {
			local++
			if t.Disclosure == EdgeOnly {
				edge++
			}
		}
		if t.Disclosure == EdgeOnly {
			for _, id := range t.FactIDs {
				if s.Facts[id].Category != F5 {
					p.add("edge", "edge-only turn %d introduces %s fact %s", i, s.Facts[id].Category, id)
				}
			}
		}
		if t.ReadRecord != "" {
			if r, ok := s.Records[t.ReadRecord]; !ok || r.Side != t.Owner {
				p.add("structure", "turn %d reads %q, which its owner cannot read", i, t.ReadRecord)
			}
			if !strings.Contains(t.User, t.ReadRecord) {
				p.add("structure", "turn %d does not name record %s", i, t.ReadRecord)
			}
		}
	}
	if switches < 3 {
		p.add("switches", "only %d switches", switches)
	}
	// Turn 0 carries an F1 fact on a shared turn, so even the first batch has
	// something to ask whichever side receives it.
	if t := s.Turns[0]; t.Disclosure != Shared || !slices.ContainsFunc(t.FactIDs, func(id string) bool { return s.Facts[id].Category == F1 }) {
		p.add("structure", "turn 0 is %s with facts %v, want a shared turn with an F1 fact", t.Disclosure, t.FactIDs)
	}
	if local > 0 {
		if ratio := float64(edge) / float64(local); ratio < 0.2 || ratio > 0.45 {
			p.add("edge", "edge-only ratio %.2f over %d local turns", ratio, local)
		}
	}

	// The long threshold takes the lower count and the short one the higher, so
	// each holds however the tokens are counted.
	long, short := false, false
	for _, seg := range cloudSegments(s) {
		if seg.bounded && seg.growth > 20000 {
			long = true
		}
		if seg.bounded && seg.worst < 3500 && seg.end-seg.start+1 <= 3 {
			short = true
		}
	}
	if !long {
		p.add("long-segment", "no cloud segment between two switches grows past 20K")
	}
	if !short {
		p.add("short-segment", "no cloud segment between two switches stays well under 5K")
	}
}

func checkSessionRecords(p *problems, s Session) {
	read := map[string]bool{}
	for _, t := range s.Turns {
		if t.ReadRecord != "" {
			read[t.ReadRecord] = true
		}
	}
	for id, r := range s.Records {
		n, worst := EstimateTokens(r.Text), pessimisticTokens(r.Text)
		if n < 500 || worst > 3000 || (r.Side == SideLocal && worst > 1500) {
			p.add("record-size", "%s record %s has %d tokens, %d pessimistically", r.Side, id, n, worst)
		}
		if !read[id] {
			p.add("record-size", "record %s is never read", id)
		}
		if id != r.ID {
			p.add("structure", "record key %s holds id %s", id, r.ID)
		}
	}
	for _, text := range sessionMaterial(s) {
		if !isASCII(text) {
			p.add("ascii", "non-ASCII text: %.40q", text)
		}
	}
	// Keeping everything must not be free: facts must be a small part of what a
	// record shows, and each one comes with its two near misses.
	allCodes, recordFacts := 0, 0
	for id, r := range s.Records {
		codes := len(Codes(r.Text))
		allCodes += codes
		own := 0
		for _, fid := range r.FactIDs {
			if s.Facts[fid].Category != F4 {
				own++
			}
		}
		recordFacts += own
		if codes < 3*own {
			p.add("distractors", "record %s has %d codes for %d facts", id, codes, own)
		}
	}
	if allCodes < 6*recordFacts {
		p.add("distractors", "records hold %d codes for %d facts", allCodes, recordFacts)
	}
}

func checkSessionCodes(p *problems, s Session) {
	superseded := map[string]bool{}
	for _, f := range s.Facts {
		for _, old := range f.Superseded {
			superseded[old] = true
		}
	}
	sources := map[string]int{}
	note := func(text string) {
		for _, c := range Codes(text) {
			sources[c]++
		}
	}
	for _, r := range s.Records {
		note(r.Text)
	}
	for _, t := range s.Turns {
		note(t.User)
	}
	for code, n := range sources {
		// A correction may name the value it retires, so that value can appear twice.
		if n > 1 && !(superseded[code] && n == 2) {
			p.add("code-unique", "code %s appears %d times", code, n)
		}
	}
}

func checkSessionFacts(p *problems, s Session) {
	users := make([]string, len(s.Turns))
	replies := make([]string, len(s.Turns))
	for i, t := range s.Turns {
		users[i], replies[i] = t.User, t.ExpectedReply
	}
	allUser := strings.Join(users, "\n")
	allReply := strings.Join(replies, "\n")

	turnHas := func(f Fact, turn int) bool {
		return turn >= 0 && turn < len(s.Turns) && slices.Contains(s.Turns[turn].FactIDs, f.ID)
	}
	recordAt := func(turn int) Record { return s.Records[s.Turns[turn].ReadRecord] }

	f4Total, f4Crossing, f4Chained := 0, 0, 0
	for _, f := range s.Facts {
		if f.Category != F4 {
			continue
		}
		f4Total++
		if f.IntroducedTurn >= 0 && f.SupersededTurn >= 0 && f.IntroducedTurn < len(s.Turns) && f.SupersededTurn < len(s.Turns) &&
			s.Turns[f.IntroducedTurn].Owner != s.Turns[f.SupersededTurn].Owner {
			f4Crossing++
		}
		if len(f.Superseded) >= 2 {
			f4Chained++
		}
	}
	if f4Total > 0 && f4Crossing == 0 {
		p.add("f4-cross", "none of %d F4 facts is corrected on the other side", f4Total)
	}
	if f4Total >= 3 && f4Chained == 0 {
		p.add("f4-chain", "none of %d F4 facts has two superseded values", f4Total)
	}

	for id, f := range s.Facts {
		if f.Category == F6 {
			continue
		}
		if f.IntroducedTurn < 0 || f.IntroducedTurn >= len(s.Turns) || !turnHas(f, f.IntroducedTurn) {
			p.add("structure", "%s is not listed on its introducing turn %d", id, f.IntroducedTurn)
			continue
		}
		t := s.Turns[f.IntroducedTurn]
		if f.Category != F4 && (f.SupersededTurn != -1 || len(f.Superseded) != 0) {
			p.add("f4", "%s %s carries supersession data", f.Category, id)
		}
		if f.Category != F5 && t.Disclosure == EdgeOnly {
			p.add("edge", "%s %s comes from an edge-only turn", f.Category, id)
		}
		switch f.Category {
		case F1:
			if f.Where != "user" || !strings.Contains(t.User, f.Value) || !strings.Contains(strings.ToLower(t.User), labelPhrase(f)) {
				p.add("f1", "%s is not answerable from its user message", id)
			}
		case F2:
			got, ok := deriveDecision(t.User)
			if f.Where != "reply" || !ok || got != f.Value || !strings.Contains(t.ExpectedReply, f.Value) {
				p.add("f2", "%s: rule gives %q, fact says %q", id, got, f.Value)
			}
		case F3:
			r := recordAt(f.IntroducedTurn)
			if f.Where != "cloud_tool" || t.Owner != SideCloud || r.Side != SideCloud ||
				!slices.Contains(r.FactIDs, id) ||
				!strings.Contains(r.Text, "\n"+refLine(f.Kind, f.Subject, f.Value)+"\n") {
				p.add("f3", "%s is not labelled in the cloud record %s", id, r.ID)
			}
			if !strings.Contains(f.Label, r.ID) {
				p.add("f3", "%s label does not name record %s", id, r.ID)
			}
			if strings.Contains(allUser, f.Value) || strings.Contains(allReply, f.Value) {
				p.add("f3-leak", "%s appears in a user message or expected reply", id)
			}
		case F4:
			checkF4(p, s, f, turnHas, recordAt)
		case F5:
			switch f.Where {
			case "local_tool":
				r := recordAt(f.IntroducedTurn)
				if t.Owner != SideLocal || r.Side != SideLocal || !slices.Contains(r.FactIDs, id) ||
					!strings.Contains(r.Text, "\n"+refLine(f.Kind, f.Subject, f.Value)+"\n") {
					p.add("f5", "%s is not labelled in the local record %s", id, r.ID)
				}
			case "edge_reply":
				got, ok := deriveDecision(t.User)
				if t.Disclosure != EdgeOnly || t.Owner != SideLocal || !ok || got != f.Value || !strings.Contains(t.ExpectedReply, f.Value) {
					p.add("f5", "%s is not a rule-derived edge-only decision", id)
				}
			default:
				p.add("f5", "%s has Where %q", id, f.Where)
			}
			if strings.Contains(allReply, f.Value) && f.Where == "local_tool" {
				p.add("f5", "%s from a local record appears in an expected reply", id)
			}
		}
	}
}

func checkF4(p *problems, s Session, f Fact, turnHas func(Fact, int) bool, recordAt func(int) Record) {
	if len(f.Superseded) == 0 {
		p.add("f4", "%s has no superseded value", f.ID)
		return
	}
	if f.SupersededTurn < f.IntroducedTurn+2 || f.SupersededTurn >= len(s.Turns) || !turnHas(f, f.SupersededTurn) {
		p.add("f4", "%s has correction turn %d after introduction %d", f.ID, f.SupersededTurn, f.IntroducedTurn)
		return
	}
	t := s.Turns[f.SupersededTurn]
	if s.Turns[f.IntroducedTurn].Disclosure != Shared || t.Disclosure != Shared {
		p.add("edge", "%s is stated on an edge-only turn", f.ID)
	}
	switch f.Where {
	case "user":
		if !strings.Contains(t.User, f.Value) {
			p.add("f4", "%s newest value is missing from the user message of turn %d", f.ID, t.Index)
		}
	case "cloud_tool":
		if !strings.Contains(recordAt(t.Index).Text, f.Value) {
			p.add("f4", "%s newest value is missing from the record at turn %d", f.ID, t.Index)
		}
	default:
		p.add("f4", "%s has Where %q", f.ID, f.Where)
	}
	// Before its correction turn, nothing holds the newest value but the older
	// values have all been stated, so "stale" means something a side could have
	// seen.
	var before strings.Builder
	for _, prior := range s.Turns[:t.Index] {
		before.WriteString(prior.User)
		if prior.ReadRecord != "" {
			before.WriteString(s.Records[prior.ReadRecord].Text)
		}
	}
	if strings.Contains(before.String(), f.Value) {
		p.add("f4", "%s newest value appears before its correction", f.ID)
	}
	for _, old := range f.Superseded {
		if !strings.Contains(before.String(), old) {
			p.add("f4", "%s superseded value %s was never stated", f.ID, old)
		}
	}
}

func checkSessionProbes(p *problems, s Session) {
	type expected struct {
		after     int
		recipient Side
		reason    string
	}
	var want []expected
	for i, t := range s.Turns {
		switch {
		case i == len(s.Turns)-1:
			want = append(want, expected{i, t.Owner, "end"})
		case s.Turns[i+1].Owner != t.Owner:
			want = append(want, expected{i, s.Turns[i+1].Owner, "switch"})
		}
	}
	if len(want) != len(s.Probes) {
		p.add("probe-schedule", "%d batches, want %d", len(s.Probes), len(want))
	}
	seenIDs := map[string]bool{}
	prev := -1
	for i, b := range s.Probes {
		if i < len(want) && (b.AfterTurn != want[i].after || b.Recipient != want[i].recipient || b.Reason != want[i].reason) {
			p.add("probe-schedule", "batch %d is {%d %s %s}, want %+v", i, b.AfterTurn, b.Recipient, b.Reason, want[i])
		}
		checkBatch(p, b, s.Facts, seenIDs)
		checkBatchTiming(p, s, b)
		checkBatchMix(p, s, b, prev)
		prev = b.AfterTurn
	}
}

// checkBatchTiming shows a batch asks only what already exists, and names only
// records the recipient's session has already read.
func checkBatchTiming(p *problems, s Session, b ProbeBatch) {
	read := map[string]bool{}
	for _, t := range s.Turns[:min(b.AfterTurn+1, len(s.Turns))] {
		if t.ReadRecord != "" {
			read[t.ReadRecord] = true
		}
	}
	for _, q := range b.Questions {
		f := s.Facts[q.FactID]
		if f.Category != F6 && firstAskable(f) > b.AfterTurn {
			p.add("batch-early", "%s asks %s after turn %d, before it exists at turn %d", q.ID, f.ID, b.AfterTurn, firstAskable(f))
		}
		for _, ref := range recordRefRe.FindAllString(q.Text, -1) {
			if !read[ref] {
				p.add("batch-early", "%s names record %s before it is read", q.ID, ref)
			}
		}
	}
}

// checkBatchMix shows batches mix categories and mix old with recent facts,
// whenever the facts available at that point allow it.
func checkBatchMix(p *problems, s Session, b ProbeBatch, prev int) {
	categories := map[Category]bool{}
	asked, oldAsked, newAsked := 0, false, false
	for _, q := range b.Questions {
		f := s.Facts[q.FactID]
		if f.Category == F6 {
			continue
		}
		asked++
		categories[f.Category] = true
		if firstAskable(f) > prev {
			newAsked = true
		} else {
			oldAsked = true
		}
	}
	if asked == 0 {
		p.add("batch-empty", "batch after turn %d holds only F6 questions", b.AfterTurn)
	}
	if asked >= 6 && len(categories) < 3 {
		p.add("batch-mix", "batch after turn %d spans only %d categories", b.AfterTurn, len(categories))
	}
	if asked < 2 {
		return
	}
	oldAvail, newAvail := false, false
	for _, f := range s.Facts {
		if f.Category == F6 || firstAskable(f) > b.AfterTurn || (f.Category == F5 && b.Recipient != SideLocal) {
			continue
		}
		if firstAskable(f) > prev {
			newAvail = true
		} else {
			oldAvail = true
		}
	}
	if (oldAvail && !oldAsked) || (newAvail && !newAsked) {
		p.add("batch-mix", "batch after turn %d: old asked %v of avail %v, new asked %v of avail %v", b.AfterTurn, oldAsked, oldAvail, newAsked, newAvail)
	}
}

func checkSessionCanaries(p *problems, s Session) {
	canaries := s.Canaries()
	if !slices.IsSorted(canaries) {
		p.add("canary", "canaries are not sorted")
	}
	in := map[string]bool{}
	for _, c := range canaries {
		in[c] = true
	}
	for _, r := range s.Records {
		for _, c := range Codes(r.Text) {
			if in[c] != (r.Side == SideLocal) {
				p.add("canary", "code %s in %s record %s has canary=%v", c, r.Side, r.ID, in[c])
			}
		}
	}
	for _, t := range s.Turns {
		for _, c := range Codes(t.User + "\n" + t.ExpectedReply) {
			if in[c] != (t.Disclosure == EdgeOnly) {
				p.add("canary", "code %s in %s turn %d has canary=%v", c, t.Disclosure, t.Index, in[c])
			}
		}
	}
	for id, f := range s.Facts {
		if f.Category != F6 && in[f.Value] != (f.Category == F5) {
			p.add("canary", "%s fact %s has canary=%v", f.Category, id, in[f.Value])
		}
	}
}

func cbenchMaterial(c CBench) []string {
	out := []string{c.System}
	for _, st := range append(slices.Clone(c.History), c.LocalOwn...) {
		out = append(out, st.User, st.ToolResult, st.Reply)
	}
	return out
}

func stepTokens(steps []CStep) int {
	n := 0
	for _, st := range steps {
		n += EstimateTokens(st.User) + EstimateTokens(st.ToolResult) + EstimateTokens(st.Reply)
	}
	return n
}

func validateCBench(c CBench) problems {
	var p problems
	if n := stepTokens(c.History); n < 95000 || n > 105000 {
		p.add("cbench-size", "history has %d tokens", n)
	}
	if n := stepTokens(c.LocalOwn); n < 2400 || n > 3600 {
		p.add("cbench-size", "local history has %d tokens", n)
	}
	checkCBenchSteps(&p, c.History, "read_record", 3000)
	checkCBenchSteps(&p, c.LocalOwn, "read_local", 1500)
	for _, text := range cbenchMaterial(c) {
		if !isASCII(text) {
			p.add("ascii", "non-ASCII text: %.40q", text)
		}
	}
	checkLabels(&p, c.Facts)
	checkF6Absent(&p, c.Facts, cbenchMaterial(c))
	checkCBenchFacts(&p, c)
	checkCBenchSpread(&p, c)
	checkCBenchProbes(&p, c)
	checkCBenchCanaries(&p, c)
	return p
}

func checkCBenchSteps(p *problems, steps []CStep, tool string, maxTokens int) {
	ids := map[string]bool{}
	for i, st := range steps {
		if st.ToolName == "" {
			if st.ToolID != "" || st.ToolResult != "" || st.RecordID != "" {
				p.add("cbench-step", "step %d has tool data without a tool", i)
			}
			continue
		}
		if st.ToolName != tool || st.ToolID == "" || ids[st.ToolID] || !strings.Contains(st.ToolResult, st.RecordID) || !strings.Contains(st.User, st.RecordID) {
			p.add("cbench-step", "step %d has a bad tool call: %s %s %s", i, st.ToolName, st.ToolID, st.RecordID)
		}
		ids[st.ToolID] = true
		if n, worst := EstimateTokens(st.ToolResult), pessimisticTokens(st.ToolResult); n < 500 || worst > maxTokens {
			p.add("record-size", "step %d tool result has %d tokens, %d pessimistically", i, n, worst)
		}
	}
}

func checkCBenchFacts(p *problems, c CBench) {
	var historyUser, historyReply, historyTool, localAll strings.Builder
	for _, st := range c.History {
		historyUser.WriteString(st.User)
		historyReply.WriteString(st.Reply)
		historyTool.WriteString(st.ToolResult)
	}
	for _, st := range c.LocalOwn {
		localAll.WriteString(st.User + st.ToolResult + st.Reply)
	}
	for id, f := range c.Facts {
		if f.Category == F6 {
			continue
		}
		steps := c.History
		if f.Category == F5 {
			steps = c.LocalOwn
		}
		if f.IntroducedTurn < 0 || f.IntroducedTurn >= len(steps) {
			p.add("structure", "%s has out-of-range turn %d", id, f.IntroducedTurn)
			continue
		}
		st := steps[f.IntroducedTurn]
		switch f.Category {
		case F1:
			if !strings.Contains(st.User, f.Value) {
				p.add("f1", "%s is not in its user message", id)
			}
		case F2:
			got, ok := deriveDecision(st.User)
			if !ok || got != f.Value || !strings.Contains(st.Reply, f.Value) {
				p.add("f2", "%s: rule gives %q, fact says %q", id, got, f.Value)
			}
		case F3:
			if !strings.Contains(st.ToolResult, "\n"+refLine(f.Kind, f.Subject, f.Value)+"\n") {
				p.add("f3", "%s is not labelled in step %d", id, f.IntroducedTurn)
			}
			if strings.Contains(historyUser.String(), f.Value) || strings.Contains(historyReply.String(), f.Value) {
				p.add("f3-leak", "%s appears in a user message or canned reply", id)
			}
		case F4:
			if len(f.Superseded) == 0 || f.SupersededTurn < f.IntroducedTurn+2 || f.SupersededTurn >= len(steps) {
				p.add("f4", "%s has a bad correction: %+v", id, f)
				continue
			}
			last := steps[f.SupersededTurn]
			if !strings.Contains(last.User+last.ToolResult, f.Value) {
				p.add("f4", "%s newest value is missing from step %d", id, f.SupersededTurn)
			}
		case F5:
			if !strings.Contains(localAll.String(), f.Value) {
				p.add("f5", "%s is not in the local history", id)
			}
			if strings.Contains(historyUser.String()+historyTool.String()+historyReply.String(), f.Value) {
				p.add("f5", "%s appears in the cloud history", id)
			}
		}
	}
	sources := map[string]int{}
	for _, text := range cbenchMaterial(c) {
		for _, code := range Codes(text) {
			sources[code]++
		}
	}
	superseded := map[string]bool{}
	for _, f := range c.Facts {
		for _, old := range f.Superseded {
			superseded[old] = true
		}
	}
	twice := map[string]bool{}
	for _, f := range c.Facts {
		if f.Category == F2 || f.Where == "edge_reply" {
			twice[f.Value] = true // the rule's candidate, then the canned reply
		}
	}
	for code, n := range sources {
		if n > 1 && !(n == 2 && (superseded[code] || twice[code])) {
			p.add("code-unique", "code %s appears %d times", code, n)
		}
	}
}

// checkCBenchSpread shows F3 facts reach both ends of the history and every
// tenth of it, measured in tokens so a long step cannot hide a gap.
func checkCBenchSpread(p *problems, c CBench) {
	total := stepTokens(c.History)
	position := make([]int, len(c.History))
	running := 0
	for i, st := range c.History {
		position[i] = running
		running += EstimateTokens(st.User) + EstimateTokens(st.ToolResult) + EstimateTokens(st.Reply)
	}
	deciles := make([]int, 10)
	facts, f3 := 0, 0
	first, last := false, false
	thirds := map[Category]*[3]int{F1: {}, F2: {}}
	for _, f := range c.Facts {
		if f.Category != F6 {
			facts++
		}
		if counts, ok := thirds[f.Category]; ok && f.IntroducedTurn >= 0 && len(c.History) > 0 {
			counts[min(f.IntroducedTurn*3/len(c.History), 2)]++
		}
		if f.Category != F3 {
			continue
		}
		f3++
		if f.IntroducedTurn < 0 || f.IntroducedTurn >= len(position) {
			p.add("cbench-spread", "%s points outside the history", f.ID)
			continue
		}
		deciles[min(position[f.IntroducedTurn]*10/total, 9)]++
		first = first || f.IntroducedTurn == 0
		last = last || f.IntroducedTurn == len(c.History)-1
	}
	if f3 < 40 {
		p.add("cbench-spread", "only %d F3 facts", f3)
	}
	if facts < 40 {
		p.add("cbench-spread", "only %d facts in total", facts)
	}
	if !first || !last {
		p.add("cbench-spread", "F3 facts miss an end of the history: first %v, last %v", first, last)
	}
	for i, n := range deciles {
		if n < 3 {
			p.add("cbench-spread", "tenth %d of the history holds only %d F3 facts", i, n)
		}
	}
	// User-stated facts and decisions must not all be old or all be recent.
	for cat, counts := range thirds {
		for i, n := range counts {
			if n == 0 {
				p.add("cbench-spread", "third %d of the history holds no %s fact", i, cat)
			}
		}
	}
	codes := 0
	for _, st := range c.History {
		codes += len(Codes(st.ToolResult))
	}
	if codes < 8*f3 {
		p.add("distractors", "history holds %d codes for %d F3 facts", codes, f3)
	}
}

func checkCBenchProbes(p *problems, c CBench) {
	seenIDs := map[string]bool{}
	asked := map[string]int{}
	categories := map[Category]bool{}
	for _, b := range c.Probes {
		if b.Recipient != SideLocal || b.AfterTurn != len(c.History)-1 || b.Reason != "end" {
			p.add("probe-schedule", "batch is {%d %s %s}", b.AfterTurn, b.Recipient, b.Reason)
		}
		checkBatch(p, b, c.Facts, seenIDs)
		for _, q := range b.Questions {
			asked[q.FactID]++
			categories[q.Category] = true
		}
	}
	for _, cat := range []Category{F1, F2, F3, F4, F5, F6} {
		if !categories[cat] {
			p.add("batch-mix", "no %s question in any batch", cat)
		}
	}
	for id := range c.Facts {
		if asked[id] != 1 {
			p.add("batch-mix", "fact %s asked %d times", id, asked[id])
		}
	}
}

func checkCBenchCanaries(p *problems, c CBench) {
	in := map[string]bool{}
	for _, code := range c.Canaries {
		in[code] = true
	}
	for _, st := range c.LocalOwn {
		for _, code := range Codes(st.User + "\n" + st.ToolResult + "\n" + st.Reply) {
			if !in[code] {
				p.add("canary", "local code %s is not a canary", code)
			}
		}
	}
	for _, st := range c.History {
		for _, code := range Codes(st.User + "\n" + st.ToolResult + "\n" + st.Reply) {
			if in[code] {
				p.add("canary", "cloud history holds canary %s", code)
			}
		}
	}
}
