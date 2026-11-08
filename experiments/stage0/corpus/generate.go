package corpus

import (
	"fmt"
	"slices"
	"strings"
)

// Independent PRNG streams, so a session and a benchmark item with equal seeds
// share nothing.
const (
	streamSession uint64 = 1
	streamCBench  uint64 = 2
)

type turnKind int

const (
	kindRead turnKind = iota
	kindDecision
	kindPlain
)

type segKind int

const (
	segLocal segKind = iota
	segLong
	segMedium
	segShort
)

type segment struct {
	owner Side
	kind  segKind
	turns int
}

// Record size targets in estimated tokens. A built record lands within one line
// (about 30 tokens) above its target. Codes and timestamps tokenize worse than
// prose, so the maxima leave room for the workload's limits of 3K for any record
// and 1.5K for a local one to hold under a more pessimistic count as well: the
// tests check both counts.
const (
	longRecordMin, longRecordMax     = 2350, 2550
	mediumRecordMin, mediumRecordMax = 1100, 2000
	shortRecordMin, shortRecordMax   = 520, 900
	localRecordMin, localRecordMax   = 520, 1270
)

// longSegmentReads is how many reads a long segment keeps. Nine reads of at
// least longRecordMin tokens exceed the 20K growth requirement without counting
// any user text or reply.
const longSegmentReads = 9

// Distractor density: one body line in distractorOneIn holds a labelled code.
// Keeping every labelled code line then costs about half of an 8K window at length
// 20, more than all of it at length 50, and about 13 times a 1,000-token summary
// in a benchmark item, so keeping everything is not free. A denser setting would
// push F3 retention under any fixed summary budget toward a floor where
// summarizers can no longer be told apart.
const distractorOneIn = 6

// f4Stage is one statement of a value of an F4 fact, placed on one turn.
type f4Stage struct {
	factID    string
	kind      string
	subject   string
	value     string
	prev      string // the value it replaces, "" at the first statement
	viaRecord bool
}

type turnPlan struct {
	owner  Side
	edge   bool
	kind   turnKind
	target int // record size target for reads
	nFacts int // F3 or F5 facts in the record for reads
	f1     bool
	stages []f4Stage
}

// gen holds the per-item uniqueness state: no code, label or record id repeats.
type gen struct {
	r       *prng
	codes   map[string]bool
	used    map[[2]int]bool
	recIDs  map[string]bool
	facts   map[string]Fact
	order   []string // fact ids in creation order
	qtext   map[string]string
	records map[string]Record

	plainDeck []plainQuestionPair
}

func newGen(seed int64, stream uint64) *gen {
	return &gen{
		r:       newPRNG(seed, stream),
		codes:   map[string]bool{},
		used:    map[[2]int]bool{},
		recIDs:  map[string]bool{},
		facts:   map[string]Fact{},
		qtext:   map[string]string{},
		records: map[string]Record{},
	}
}

// Generate builds one session. It panics on a length outside
// [MinLength, MaxLength] or an unknown set: both are programmer errors, and the
// contract has no error return.
func Generate(seed int64, length int, set string) Session {
	if length < MinLength || length > MaxLength {
		panic(fmt.Sprintf("corpus: length %d outside [%d, %d]", length, MinLength, MaxLength))
	}
	checkSet(set)
	g := newGen(seed, streamSession)
	plans := g.planSession(makeLayout(g.r, length), length)
	s := Session{
		ID:      fmt.Sprintf("%s-%d-L%d", set, seed, length),
		Seed:    seed,
		Set:     set,
		Length:  length,
		System:  systemPrompt,
		Turns:   g.buildTurns(plans),
		Records: g.records,
		Facts:   g.facts,
	}
	s.Probes = g.sessionProbes(&s)
	return s
}

func checkSet(set string) {
	if set != SetTuning && set != SetAcceptance {
		panic(fmt.Sprintf("corpus: unknown set %q", set))
	}
}

// makeLayout fixes the owner of every turn. The required long and short cloud
// segments sit between local segments, so each has a switch on both sides. A
// length-50 session also gets a medium segment, whose growth falls between the
// two thresholds, and may open or close on one more short cloud segment.
func makeLayout(r *prng, length int) []segment {
	nLong := 1 + (length-20)/30
	nMedium := 0
	if length >= 35 {
		nMedium = 1 + (length-35)/40
	}
	nShort := 1 + (length-20)/25

	var inner []segment
	used := 0
	add := func(kind segKind, turns int) {
		inner = append(inner, segment{SideCloud, kind, turns})
		used += turns
	}
	for range nLong {
		turns := longSegmentReads + 1
		if length >= 30 {
			turns += r.intn(2)
		}
		add(segLong, turns)
	}
	for range nMedium {
		add(segMedium, r.between(5, 6))
	}
	for range nShort {
		add(segShort, r.between(2, 3))
	}
	shuffled(r, inner)

	localSegs := len(inner) + 1
	remaining := length - used
	opening, closing := false, false
	if remaining-2 >= 2*localSegs {
		switch r.intn(3) {
		case 1:
			opening, remaining = true, remaining-2
		case 2:
			closing, remaining = true, remaining-2
		}
	}
	sizes := make([]int, localSegs)
	for i := range sizes {
		sizes[i] = 2
	}
	for range remaining - 2*localSegs {
		sizes[r.intn(localSegs)]++
	}

	var out []segment
	if opening {
		out = append(out, segment{SideCloud, segShort, 2})
	}
	for i, n := range sizes {
		out = append(out, segment{SideLocal, segLocal, n})
		if i < len(inner) {
			out = append(out, inner[i])
		}
	}
	if closing {
		out = append(out, segment{SideCloud, segShort, 2})
	}
	return out
}

// cloudSegmentKinds scripts one cloud segment. Local turn kinds are dealt
// separately across the whole session.
func (g *gen) cloudSegmentKinds(seg segment) []turnKind {
	out := make([]turnKind, seg.turns) // all kindRead
	switch seg.kind {
	case segLong:
		// Decisions never open a segment, and the reads stay at longSegmentReads.
		slots := make([]int, 0, seg.turns-1)
		for i := 1; i < seg.turns; i++ {
			slots = append(slots, i)
		}
		shuffled(g.r, slots)
		for _, i := range slots[:seg.turns-longSegmentReads] {
			out[i] = kindDecision
		}
	case segMedium:
		out[g.r.between(1, seg.turns-1)] = kindDecision
	case segShort:
		if seg.turns == 3 {
			out[1] = kindDecision
			break
		}
		switch g.r.intn(3) {
		case 0:
			out[1] = kindDecision
		case 1:
			out[0] = kindDecision
		default:
			out[1] = kindPlain
		}
	}
	return out
}

func (g *gen) planSession(layout []segment, length int) []turnPlan {
	plans := make([]turnPlan, 0, length)
	var local []int
	for _, seg := range layout {
		var segKinds []turnKind
		if seg.owner == SideCloud {
			segKinds = g.cloudSegmentKinds(seg)
		}
		for j := range seg.turns {
			p := turnPlan{owner: seg.owner}
			switch {
			case seg.owner == SideLocal:
				local = append(local, len(plans))
				p.target = g.r.between(localRecordMin, localRecordMax)
				p.nFacts = 1
				if g.r.chance(1, 4) {
					p.nFacts = 2
				}
			case segKinds[j] == kindRead:
				p.target, p.nFacts = g.cloudReadShape(seg.kind)
				p.kind = kindRead
			default:
				p.kind = segKinds[j]
			}
			plans = append(plans, p)
		}
	}

	// Local kinds: half reads, a quarter decisions, the rest plain questions.
	n := len(local)
	deck := make([]turnKind, 0, n)
	reads, decisions := (n+1)/2, max(1, n/4)
	for i := range n {
		switch {
		case i < reads:
			deck = append(deck, kindRead)
		case i < reads+decisions:
			deck = append(deck, kindDecision)
		default:
			deck = append(deck, kindPlain)
		}
	}
	shuffled(g.r, deck)
	for i, ti := range local {
		plans[ti].kind = deck[i]
	}

	// About a third of local turns are edge-only. Turn 0 stays shared because it
	// carries an F1 fact, which keeps the first probe from being empty.
	candidates := slices.DeleteFunc(slices.Clone(local), func(t int) bool { return t == 0 })
	shuffled(g.r, candidates)
	for _, ti := range candidates[:(n+1)/3] {
		plans[ti].edge = true
	}

	g.planF1(plans, max(3, length/6))
	g.planF4(plans, max(2, length/8), true)
	return plans
}

func (g *gen) cloudReadShape(kind segKind) (target, nFacts int) {
	switch kind {
	case segLong:
		return g.r.between(longRecordMin, longRecordMax), g.r.between(1, 3)
	case segMedium:
		return g.r.between(mediumRecordMin, mediumRecordMax), g.r.between(1, 2)
	default:
		return g.r.between(shortRecordMin, shortRecordMax), 1
	}
}

// sharedTurns lists turns whose reply may cross sides: only those may carry
// anything other than F5.
func sharedTurns(plans []turnPlan) []int {
	var out []int
	for i, p := range plans {
		if !p.edge {
			out = append(out, i)
		}
	}
	return out
}

// planF1 spreads constraints evenly across shared turns, starting at the first.
func (g *gen) planF1(plans []turnPlan, n int) {
	shared := sharedTurns(plans)
	plans[shared[0]].f1 = true
	for b := 1; b < n; b++ {
		lo, hi := len(shared)*b/n, len(shared)*(b+1)/n
		plans[shared[lo+g.r.intn(hi-lo)]].f1 = true
	}
}

// planF4 places each F4 fact's statements on shared turns at least two turns
// apart. In session mode every third fact has two superseded values, and the
// first fact crosses a switch, so at least one correction lands on a side that
// did not see the original statement.
func (g *gen) planF4(plans []turnPlan, n int, sessionMode bool) {
	shared := sharedTurns(plans)
	taken := map[int]bool{}
	for f := range n {
		stages := 2
		if sessionMode && f%3 == 2 {
			stages = 3
		}
		turns := g.pickF4Turns(plans, shared, taken, stages, sessionMode && f == 0)
		ki, si := g.pair()
		kind, subject := kinds[ki], subjects[si]

		values := make([]string, stages)
		via := make([]bool, stages)
		for st, ti := range turns {
			values[st] = g.code()
			via[st] = plans[ti].kind == kindRead && plans[ti].owner == SideCloud && g.r.chance(1, 2)
		}
		last := stages - 1
		where := "user"
		if via[last] {
			where = "cloud_tool"
		}
		id := g.addFact(Fact{
			Category:       F4,
			Value:          values[last],
			Superseded:     slices.Clone(values[:last]),
			IntroducedTurn: turns[0],
			SupersededTurn: turns[last],
			Label:          fmt.Sprintf("current %s for the %s", kind, subject),
			Where:          where,
			Kind:           kind,
			Subject:        subject,
		}, qCurrent(kind, subject))
		for st, ti := range turns {
			stage := f4Stage{factID: id, kind: kind, subject: subject, value: values[st], viaRecord: via[st]}
			if st > 0 {
				stage.prev = values[st-1]
			}
			plans[ti].stages = append(plans[ti].stages, stage)
		}
	}
}

func (g *gen) pickF4Turns(plans []turnPlan, shared []int, taken map[int]bool, stages int, mustCross bool) []int {
	limit := len(shared) * 6 / 10
	for range 5000 {
		first := shared[g.r.intn(limit)]
		if taken[first] {
			continue
		}
		turns := []int{first}
		for len(turns) < stages {
			var later []int
			for _, t := range shared {
				if t >= turns[len(turns)-1]+2 && !taken[t] {
					later = append(later, t)
				}
			}
			if len(later) == 0 {
				break
			}
			turns = append(turns, pick(g.r, later))
		}
		if len(turns) < stages {
			continue
		}
		if mustCross && plans[turns[0]].owner == plans[turns[len(turns)-1]].owner {
			continue
		}
		for _, t := range turns {
			taken[t] = true
		}
		return turns
	}
	panic("corpus: no room to place an F4 fact")
}

func (g *gen) buildTurns(plans []turnPlan) []Turn {
	turns := make([]Turn, len(plans))
	for i, p := range plans {
		turns[i] = g.buildTurn(i, p)
	}
	return turns
}

func (g *gen) buildTurn(i int, p turnPlan) Turn {
	t := Turn{Index: i, Owner: p.owner, Disclosure: Shared}
	if p.edge {
		t.Disclosure = EdgeOnly
	}
	var parts []string
	for _, st := range p.stages {
		t.FactIDs = append(t.FactIDs, st.factID)
		if !st.viaRecord {
			parts = append(parts, g.f4UserLine(st))
		}
	}
	if p.f1 {
		parts = append(parts, g.f1Line(i, &t))
	}
	noted := len(parts) > 0

	var main, reply string
	switch p.kind {
	case kindRead:
		main, reply = g.readTurn(i, p, &t)
	case kindDecision:
		main, reply = g.decisionTurn(i, p, &t)
	default:
		main, reply = g.plainQuestion()
	}
	t.User = strings.Join(append(parts, main), "\n\n")
	if noted {
		reply = "Noted. " + reply
	}
	t.ExpectedReply = reply
	return t
}

// plainQuestion deals from a shuffled deck, so a question repeats only after all
// of them have been used.
func (g *gen) plainQuestion() (q, a string) {
	if len(g.plainDeck) == 0 {
		g.plainDeck = shuffled(g.r, slices.Clone(plainQuestions))
	}
	next := g.plainDeck[0]
	g.plainDeck = g.plainDeck[1:]
	return next.q, next.a
}

func (g *gen) f1Line(i int, t *Turn) string {
	ki, si := g.pair()
	kind, subject := kinds[ki], subjects[si]
	code := g.code()
	t.FactIDs = append(t.FactIDs, g.addFact(Fact{
		Category:       F1,
		Value:          code,
		IntroducedTurn: i,
		SupersededTurn: -1,
		Label:          fmt.Sprintf("%s the user required for the %s", kind, subject),
		Where:          "user",
		Kind:           kind,
		Subject:        subject,
	}, qRequired(kind, subject)))
	return fmt.Sprintf("Constraint from me: the %s for the %s must be %s for the rest of this review.", kind, subject, code)
}

func (g *gen) f4UserLine(st f4Stage) string {
	switch {
	case st.prev == "":
		return fmt.Sprintf("For now, the current %s for the %s is %s.", st.kind, st.subject, st.value)
	case g.r.chance(1, 2):
		return fmt.Sprintf("Correction: %s is no longer valid. The current %s for the %s is now %s.", st.prev, st.kind, st.subject, st.value)
	default:
		return fmt.Sprintf("Correction: the current %s for the %s is now %s. Ignore the earlier value.", st.kind, st.subject, st.value)
	}
}

func f4RecordLine(st f4Stage) string {
	if st.prev == "" {
		return fmt.Sprintf("Current %s for the %s: %s", st.kind, st.subject, st.value)
	}
	return fmt.Sprintf("Revision notice: the current %s for the %s is now %s, replacing any earlier value.", st.kind, st.subject, st.value)
}

// readTurn builds a record and the request to read it without restating it.
// Each fact line comes with two near misses, one sharing its kind and one its
// subject, so holding on to a code without its full label is not enough.
func (g *gen) readTurn(i int, p turnPlan, t *Turn) (main, reply string) {
	id := g.recordID(p.owner)
	var lines, recFacts []string
	for _, st := range p.stages {
		if st.viaRecord {
			lines = append(lines, f4RecordLine(st))
			recFacts = append(recFacts, st.factID)
		}
	}
	for range p.nFacts {
		ki, si := g.pair()
		kind, subject := kinds[ki], subjects[si]
		code := g.code()
		lines = append(lines, refLine(kind, subject, code))
		ki2, si2 := g.pairWithKind(ki)
		lines = append(lines, refLine(kinds[ki2], subjects[si2], g.code()))
		ki3, si3 := g.pairWithSubject(si)
		lines = append(lines, refLine(kinds[ki3], subjects[si3], g.code()))

		fact := Fact{
			Value:          code,
			IntroducedTurn: i,
			SupersededTurn: -1,
			Kind:           kind,
			Subject:        subject,
		}
		var q string
		if p.owner == SideCloud {
			fact.Category, fact.Where = F3, "cloud_tool"
			fact.Label = fmt.Sprintf("%s given by record %s for the %s", kind, id, subject)
			q = qRecord(kind, id, subject)
		} else {
			fact.Category, fact.Where = F5, "local_tool"
			fact.Label = fmt.Sprintf("%s given by local record %s for the %s", kind, id, subject)
			q = qLocalRecord(kind, id, subject)
		}
		factID := g.addFact(fact, q)
		recFacts = append(recFacts, factID)
		t.FactIDs = append(t.FactIDs, factID)
	}
	g.records[id] = Record{
		ID:      id,
		Side:    p.owner,
		Text:    g.buildRecord(id, p.owner, p.target, lines),
		FactIDs: recFacts,
	}
	t.ReadRecord = id

	name, _ := RecordTool(p.owner)
	subject := "record " + id
	if p.owner == SideLocal {
		subject = "local record " + id
	}
	return fmt.Sprintf(pick(g.r, readTemplates), subject, name), fmt.Sprintf("Read %s. Confirmed.", id)
}

// decisionTurn gives candidate codes and a rule. The chosen code is in the user
// text only as one unmarked candidate, so stating it takes applying the rule.
func (g *gen) decisionTurn(i int, p turnPlan, t *Turn) (main, reply string) {
	ki, si := g.pair()
	kind, subject := kinds[ki], subjects[si]
	rule := pick(g.r, decisionRules)

	n := g.r.between(3, 4)
	seen := map[int]bool{}
	lines := make([]string, 0, n)
	var chosen string
	best := 0
	for range n {
		v := g.r.between(rule.lo, rule.hi)
		for seen[v] {
			v = g.r.between(rule.lo, rule.hi)
		}
		seen[v] = true
		code := g.code()
		lines = append(lines, fmt.Sprintf("- %s (%s %d)", code, rule.attr, v))
		if chosen == "" || (rule.highest && v > best) || (!rule.highest && v < best) {
			chosen, best = code, v
		}
	}
	word := "lowest"
	if rule.highest {
		word = "highest"
	}
	main = fmt.Sprintf("Decision needed about the %s. Candidates, each %s %s for the %s:\n%s\nRule: choose the candidate with the %s %s. State the chosen code in your reply.",
		subject, article(kind), kind, subject, strings.Join(lines, "\n"), word, rule.attr)

	category, where := F2, "reply"
	if p.edge {
		category, where = F5, "edge_reply"
	}
	t.FactIDs = append(t.FactIDs, g.addFact(Fact{
		Category:       category,
		Value:          chosen,
		IntroducedTurn: i,
		SupersededTurn: -1,
		Label:          fmt.Sprintf("%s chosen in the decision about the %s", kind, subject),
		Where:          where,
		Kind:           kind,
		Subject:        subject,
	}, qChosen(kind, subject)))
	return main, fmt.Sprintf("Chosen %s for the %s: %s.", kind, subject, chosen)
}

type recLine struct {
	text    string
	stamped bool
}

const stampLen = len("00:00:00 ")

// buildRecord fills a record to its token target with filler and distractor
// lines. mustHave lines go in at uniform random positions, so head or tail
// truncation has no built-in advantage.
func (g *gen) buildRecord(id string, side Side, target int, mustHave []string) string {
	title, types := "RECORD", cloudRecordTypes
	if side == SideLocal {
		title, types = "LOCAL RECORD", localRecordTypes
	}
	header := fmt.Sprintf("%s %s\nType: %s\nSynthetic data. Each reference line names what its code is for.\n\n", title, id, pick(g.r, types))
	footer := fmt.Sprintf("END OF %s %s\n", title, id)

	budget := target*4 - len(header) - len(footer)
	for _, l := range mustHave {
		budget -= len(l) + 1
	}
	var body []recLine
	for budget > 0 {
		var l recLine
		if g.r.chance(1, distractorOneIn) {
			ki, si := g.pair()
			l.text = refLine(kinds[ki], subjects[si], g.code())
		} else {
			l.text = pick(g.r, fillers)(g.r, pick(g.r, subjects))
			l.stamped = true
			budget -= stampLen
		}
		budget -= len(l.text) + 1
		body = append(body, l)
	}
	for _, m := range mustHave {
		body = slices.Insert(body, g.r.intn(len(body)+1), recLine{text: m})
	}

	var b strings.Builder
	b.WriteString(header)
	clock := g.r.between(0, 20*3600)
	for _, l := range body {
		if l.stamped {
			clock += g.r.between(5, 170)
			fmt.Fprintf(&b, "%02d:%02d:%02d ", clock/3600%24, clock/60%60, clock%60)
		}
		b.WriteString(l.text)
		b.WriteByte('\n')
	}
	b.WriteString(footer)
	return b.String()
}

func (g *gen) addFact(f Fact, question string) string {
	f.ID = fmt.Sprintf("f%03d", len(g.order)+1)
	g.facts[f.ID] = f
	g.order = append(g.order, f.ID)
	g.qtext[f.ID] = question
	return f.ID
}

func (g *gen) code() string {
	for {
		var b [8]byte
		for i := range 3 {
			b[i] = codeAlphabet[g.r.intn(len(codeAlphabet))]
		}
		b[3] = '-'
		for i := 4; i < 8; i++ {
			b[i] = byte('0' + g.r.intn(10))
		}
		c := string(b[:])
		if !g.codes[c] {
			g.codes[c] = true
			return c
		}
	}
}

// recordID is non-sequential, so a model cannot walk future record ids.
func (g *gen) recordID(side Side) string {
	prefix := "R"
	if side == SideLocal {
		prefix = "L"
	}
	for {
		id := fmt.Sprintf("%s-%d", prefix, g.r.between(100, 999))
		if !g.recIDs[id] {
			g.recIDs[id] = true
			return id
		}
	}
}

// pair deals a label no earlier text has used.
func (g *gen) pair() (kind, subject int) {
	for range 64 {
		k, s := g.r.intn(len(kinds)), g.r.intn(len(subjects))
		if !g.used[[2]int{k, s}] {
			g.used[[2]int{k, s}] = true
			return k, s
		}
	}
	start := g.r.intn(len(kinds) * len(subjects))
	for i := range len(kinds) * len(subjects) {
		v := (start + i) % (len(kinds) * len(subjects))
		k, s := v/len(subjects), v%len(subjects)
		if !g.used[[2]int{k, s}] {
			g.used[[2]int{k, s}] = true
			return k, s
		}
	}
	panic("corpus: label vocabulary exhausted")
}

// pairWithKind deals an unused label of the same kind, or any unused label when
// that kind has none left.
func (g *gen) pairWithKind(k int) (kind, subject int) {
	start := g.r.intn(len(subjects))
	for i := range len(subjects) {
		s := (start + i) % len(subjects)
		if !g.used[[2]int{k, s}] {
			g.used[[2]int{k, s}] = true
			return k, s
		}
	}
	return g.pair()
}

// pairWithSubject deals an unused label for the same subject, or any unused
// label when that subject has none left.
func (g *gen) pairWithSubject(s int) (kind, subject int) {
	start := g.r.intn(len(kinds))
	for i := range len(kinds) {
		k := (start + i) % len(kinds)
		if !g.used[[2]int{k, s}] {
			g.used[[2]int{k, s}] = true
			return k, s
		}
	}
	return g.pair()
}
