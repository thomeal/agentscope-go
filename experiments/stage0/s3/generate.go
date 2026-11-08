package s3

import (
	"fmt"
	"slices"
	"strings"

	"github.com/agentscope-ai/agentscope-go/v2/experiments/stage0/corpus"
)

// Size targets in estimated tokens (see EstimateTokens). A built record lands
// within a line of its target. The history budget stays under 60K even when the
// last read overshoots it by a whole long record.
const (
	historyMinTokens = 21_000
	historyMaxTokens = 54_000

	shortRecordMin, shortRecordMax   = 540, 900
	mediumRecordMin, mediumRecordMax = 1100, 2000
	longRecordMin, longRecordMax     = 2350, 2900
	localReadMin, localReadMax       = 540, 800
	localUnreadMin, localUnreadMax   = 540, 1250
	recentReadMin, recentReadMax     = 540, 700

	// mustLineTokens is what a record reserves for each mandatory line. It is a
	// constant, not the line's real cost, so the two halves of a pair draw identical
	// filler even though their slot lines differ in length.
	mustLineTokens = 35
)

// distractorOneIn: one body line in this many holds a labelled code. Keeping every
// labelled code from a history in a summary is then far from free.
const distractorOneIn = 6

// olderGap is how many steps must stand between the record that holds a fact and
// the recent steps, so "older" stays true whatever a router counts as recent.
const olderGap = 3

// placement says where the target fact goes in the cloud history.
type placement int

const (
	placeNone            placement = iota // not in the cloud history
	placeRecent                           // recent published text only
	placeRecord                           // one older record only
	placeRecordAndRecent                  // an older record, restated recently
	placeCleared                          // recent text says the value is gone
)

// coded is a label with its code.
type coded struct {
	kind, subject, code string
}

func (c coded) line() string {
	return capitalize(c.kind) + " for the " + c.subject + ": " + c.code
}

// scene holds every labelled value an item might use. All are drawn before anything
// else in the same order for every item, which is what keeps a pair's two histories
// aligned draw for draw.
type scene struct {
	target coded // what the query asks about

	slotAlt     coded // in the older record's slot when the target is not there
	slotKind    coded // always beside that slot: same kind as the target
	slotSubject coded // always beside that slot: same subject as the target
	tailAlt     coded // in the recent statement slot when the target is not there
	tailKind    coded // always in recent text: same kind as the target

	localNote    coded // a LocalOwn statement: same subject as the target
	localAlt     coded // in the unread local record when the target is not there
	localKind    coded // always in the unread local record: same kind
	localSubject coded // always in the unread local record: same subject
}

// gen holds the per-item uniqueness state: no code, label or record id repeats.
type gen struct {
	r       *prng
	codes   map[string]bool
	used    map[[2]int]bool
	recIDs  map[int]bool
	records map[string]string

	plainDeck []plainExchange
}

func newGen(seed int64) *gen {
	return &gen{
		r:       newPRNG(seed, streamItem),
		codes:   map[string]bool{},
		used:    map[[2]int]bool{},
		recIDs:  map[int]bool{},
		records: map[string]string{},
	}
}

func (g *gen) newScene(k, s int) scene {
	g.used[[2]int{k, s}] = true
	sc := scene{target: coded{kinds[k], subjects[s], g.code()}}
	sc.slotAlt = g.dealKind(k)
	sc.slotKind = g.dealKind(k)
	sc.slotSubject = g.dealSubject(s)
	sc.tailAlt = g.dealSubject(s)
	sc.tailKind = g.dealKind(k)
	sc.localNote = g.dealSubject(s)
	sc.localAlt = g.dealKind(k)
	sc.localKind = g.dealKind(k)
	sc.localSubject = g.dealSubject(s)
	return sc
}

// buildItem draws one item. For a pair it runs twice with one seed and two
// placements: every draw is the same in both runs, and only which lines hold the
// target differs.
func buildItem(u unit, kind Kind, place placement, slot int) Item {
	g := newGen(u.seed)
	k, s := targetLabel(u.set, u.number)
	sc := g.newScene(k, s)

	stated := statedForm(u)
	history, slotID := g.buildHistory(sc, place, stated)
	noteTarget := kind == KindEdgePrivate && u.form == formPlain
	localOwn := g.buildLocalOwn(sc, noteTarget)

	unreadHolds := kind == KindLocalTool || (kind == KindEdgePrivate && !noteTarget)
	unreadID := g.buildUnreadLocal(sc, unreadHolds)

	it := Item{
		ID:           fmt.Sprintf("s3-%s-%05d", u.set, slot),
		Set:          u.set,
		Seed:         u.seed,
		System:       systemPrompt,
		History:      history,
		LocalOwn:     localOwn,
		Owner:        u.owner,
		Disclosure:   corpus.Shared,
		Kind:         kind,
		Answer:       sc.target.code,
		Records:      g.records,
		AnswerPrompt: answerPrompt,
	}
	switch kind {
	case KindReasoning:
		it.Query, it.Answer = g.reasoning(u.family)
	case KindEdgePrivate:
		it.Disclosure = corpus.EdgeOnly
		it.Query = g.query(u.form, sc.target, unreadID, corpus.SideLocal)
	case KindLocalTool:
		it.Query = g.query(u.form, sc.target, unreadID, corpus.SideLocal)
	default:
		it.Query = g.query(u.form, sc.target, slotID, corpus.SideCloud)
	}
	if place == placeCleared {
		it.Answer = Unknown
	}
	return it
}

// statedForm is how a recent statement is worded. A value restated from a record
// comes back in a reply, which is the natural way to restate one; the others go by
// a fixed-per-item draw, so both halves of a pair use the same shape.
type statementShape int

const (
	statedByUser statementShape = iota
	statedInReply
	statedEither
)

func statedForm(u unit) statementShape {
	if u.form == formRecordWord || (u.typ == unitFollowup && u.form == formNamed) {
		return statedInReply
	}
	if u.form == formCleared {
		return statedByUser
	}
	return statedEither
}

type stepKind int

const (
	stepRead stepKind = iota
	stepStatement
	stepDecision
	stepPlain
)

type stepPlan struct {
	kind   stepKind
	target int  // record size target for reads
	slot   bool // this read holds the record slot
}

// planOlder scripts everything before the recent steps: mostly reads, so a history's
// size comes from tool results. The first step is always a read and the slot read
// is uniform over the eligible ones, so head or tail truncation has no built-in
// advantage.
func (g *gen) planOlder() []stepPlan {
	budget := g.r.between(historyMinTokens, historyMaxTokens)
	target := g.recordTarget()
	plans := []stepPlan{{kind: stepRead, target: target}}
	for spent := target + 30; spent < budget; {
		switch d := g.r.intn(100); {
		case d < 72:
			t := g.recordTarget()
			plans = append(plans, stepPlan{kind: stepRead, target: t})
			spent += t + 30
		case d < 84:
			plans = append(plans, stepPlan{kind: stepStatement})
			spent += 40
		case d < 92:
			plans = append(plans, stepPlan{kind: stepDecision})
			spent += 110
		default:
			plans = append(plans, stepPlan{kind: stepPlain})
			spent += 30
		}
	}
	var eligible []int
	for i := range len(plans) - olderGap {
		if plans[i].kind == stepRead {
			eligible = append(eligible, i)
		}
	}
	plans[pick(g.r, eligible)].slot = true
	return plans
}

func (g *gen) recordTarget() int {
	switch d := g.r.intn(100); {
	case d < 40:
		return g.r.between(shortRecordMin, shortRecordMax)
	case d < 75:
		return g.r.between(mediumRecordMin, mediumRecordMax)
	default:
		return g.r.between(longRecordMin, longRecordMax)
	}
}

// buildHistory scripts the cloud side and returns it with the id of the slot record.
func (g *gen) buildHistory(sc scene, place placement, stated statementShape) ([]corpus.CStep, string) {
	slotLine := sc.slotAlt.line()
	if place == placeRecord || place == placeRecordAndRecent {
		slotLine = sc.target.line()
	}
	tailFact := sc.tailAlt
	if place == placeRecent || place == placeRecordAndRecent {
		tailFact = sc.target
	}

	var steps []corpus.CStep
	slotID := ""
	for _, p := range g.planOlder() {
		switch p.kind {
		case stepRead:
			var must []string
			if p.slot {
				must = []string{slotLine, sc.slotKind.line(), sc.slotSubject.line()}
			}
			st := g.readStep(len(steps), corpus.SideCloud, p.target, must)
			if p.slot {
				slotID = st.RecordID
			}
			steps = append(steps, st)
		case stepStatement:
			steps = append(steps, g.olderStatementStep())
		case stepDecision:
			steps = append(steps, g.decisionStep())
		default:
			steps = append(steps, g.plainStep())
		}
	}
	return append(steps, g.buildTail(len(steps), sc, tailFact, place, stated)...), slotID
}

// buildTail scripts the recent steps in random order. Every item's tail has the same
// three shapes: a statement in the fact's slot, a near miss sharing the target's kind,
// and a plain exchange or a read of an unrelated record.
func (g *gen) buildTail(base int, sc scene, fact coded, place placement, stated statementShape) []corpus.CStep {
	useRead := g.r.chance(1, 3)
	readTarget := g.r.between(recentReadMin, recentReadMax)
	order := g.r.perm(RecentSteps)

	// Draw both statement shapes now, in one order, whatever the placement.
	factShape := g.drawShape(stated)
	otherShape := g.drawShape(statedEither)
	factTemplate, otherTemplate := g.r.intn(len(userStatements)), g.r.intn(len(userStatements))
	factRequest, otherRequest := g.r.intn(len(replyRequests)), g.r.intn(len(replyRequests))

	out := make([]corpus.CStep, RecentSteps)
	for i, which := range order {
		switch which {
		case 0:
			if place == placeCleared {
				out[i] = corpus.CStep{
					User:  fmt.Sprintf(clearedStatement, sc.target.kind, sc.target.subject),
					Reply: "Noted.",
				}
				continue
			}
			out[i] = statementStep(fact, factShape, factTemplate, factRequest)
		case 1:
			out[i] = statementStep(sc.tailKind, otherShape, otherTemplate, otherRequest)
		default:
			if useRead {
				out[i] = g.readStep(base+i, corpus.SideCloud, readTarget, nil)
			} else {
				out[i] = g.plainStep()
			}
		}
	}
	return out
}

func (g *gen) drawShape(stated statementShape) statementShape {
	inReply := g.r.chance(1, 2)
	switch {
	case stated != statedEither:
		return stated
	case inReply:
		return statedInReply
	}
	return statedByUser
}

func statementStep(c coded, shape statementShape, template, request int) corpus.CStep {
	if shape == statedInReply {
		return corpus.CStep{
			User:  fmt.Sprintf(replyRequests[request], c.kind, c.subject),
			Reply: c.line() + ".",
		}
	}
	return corpus.CStep{
		User:  fmt.Sprintf(userStatements[template], c.kind, c.subject, c.code),
		Reply: "Noted.",
	}
}

// buildLocalOwn scripts the local side: one or two reads of small records, one
// statement and one or two plain exchanges, about 2K tokens at most. The statement
// holds the target only for an edge_private item that keeps its fact here.
func (g *gen) buildLocalOwn(sc scene, noteTarget bool) []corpus.CStep {
	note := sc.localNote
	if noteTarget {
		note = sc.target
	}
	plans := []stepPlan{{kind: stepRead, target: g.r.between(localReadMin, localReadMax)}, {kind: stepStatement}, {kind: stepPlain}}
	if g.r.chance(1, 2) {
		plans = append(plans, stepPlan{kind: stepRead, target: g.r.between(localReadMin, localReadMax)})
	}
	if g.r.chance(1, 2) {
		plans = append(plans, stepPlan{kind: stepPlain})
	}
	shuffled(g.r, plans)

	steps := make([]corpus.CStep, 0, len(plans))
	for _, p := range plans {
		switch p.kind {
		case stepRead:
			steps = append(steps, g.readStep(len(steps), corpus.SideLocal, p.target, nil))
		case stepStatement:
			steps = append(steps, corpus.CStep{
				User:  fmt.Sprintf("Site note for this device: the %s for the %s is %s.", note.kind, note.subject, note.code),
				Reply: "Noted.",
			})
		default:
			steps = append(steps, g.plainStep())
		}
	}
	return steps
}

// buildUnreadLocal adds the local record no step has read. Every item has one, so the
// shape of Records says nothing about the kind. It holds the target only when holds
// is true.
func (g *gen) buildUnreadLocal(sc scene, holds bool) string {
	line := sc.localAlt.line()
	if holds {
		line = sc.target.line()
	}
	id := g.recordID(corpus.SideLocal)
	target := g.r.between(localUnreadMin, localUnreadMax)
	g.records[id] = g.buildRecord(id, corpus.SideLocal, target, []string{line, sc.localKind.line(), sc.localSubject.line()})
	return id
}

// query words the new input. A named form refers to the record bare or with the word
// "record", at random. Pairs get here with identical draws, so identical text.
func (g *gen) query(form formClass, t coded, recordID string, side corpus.Side) string {
	bare := g.r.chance(1, 2)
	ref := recordID
	if !bare {
		ref = "record " + recordID
		if side == corpus.SideLocal {
			ref = "local record " + recordID
		}
	}
	switch form {
	case formRecordWord:
		return fmt.Sprintf(pick(g.r, recordWordForms), t.kind, t.subject)
	case formLong:
		return fmt.Sprintf(pick(g.r, longForms), t.kind, t.subject)
	case formNamed:
		return fmt.Sprintf(pick(g.r, namedForms), t.kind, t.subject, ref)
	case formNamedLong:
		return fmt.Sprintf(pick(g.r, namedLongForms), t.kind, t.subject, ref)
	default:
		return fmt.Sprintf(pick(g.r, plainForms), t.kind, t.subject)
	}
}

func (g *gen) readStep(index int, side corpus.Side, target int, must []string) corpus.CStep {
	id := g.recordID(side)
	text := g.buildRecord(id, side, target, must)
	g.records[id] = text

	tool, prefix, ref := "read_record", "call", "record "+id
	if side == corpus.SideLocal {
		tool, prefix, ref = "read_local", "local_call", "local record "+id
	}
	return corpus.CStep{
		User:       fmt.Sprintf(pick(g.r, readTemplates), ref, tool),
		ToolName:   tool,
		ToolID:     fmt.Sprintf("%s_%04d", prefix, index),
		ToolResult: text,
		Reply:      fmt.Sprintf("Read %s. Confirmed.", id),
		RecordID:   id,
	}
}

func (g *gen) olderStatementStep() corpus.CStep {
	c := g.deal()
	return corpus.CStep{
		User:  fmt.Sprintf("Constraint from me: the %s for the %s must be %s for the rest of this review.", c.kind, c.subject, c.code),
		Reply: "Noted.",
	}
}

// decisionStep gives candidate codes and a rule, and replies with the chosen one.
func (g *gen) decisionStep() corpus.CStep {
	c := g.deal()
	rule := pick(g.r, decisionRules)
	n := g.r.between(3, 4)
	seen := map[int]bool{}
	lines := make([]string, 0, n)
	chosen, best := "", 0
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
	return corpus.CStep{
		User: fmt.Sprintf("Decision needed about the %s. Candidates, each %s %s for the %s:\n%s\nRule: choose the candidate with the %s %s. State the chosen code in your reply.",
			c.subject, article(c.kind), c.kind, c.subject, strings.Join(lines, "\n"), word, rule.attr),
		Reply: fmt.Sprintf("Chosen %s for the %s: %s.", c.kind, c.subject, chosen),
	}
}

// plainStep deals from a shuffled deck, so an exchange repeats only after all of
// them have been used.
func (g *gen) plainStep() corpus.CStep {
	if len(g.plainDeck) == 0 {
		g.plainDeck = shuffled(g.r, slices.Clone(plainExchanges))
	}
	next := g.plainDeck[0]
	g.plainDeck = g.plainDeck[1:]
	return corpus.CStep{User: next.q, Reply: next.a}
}

type recLine struct {
	text    string
	stamped bool
}

const stampLen = len("00:00:00 ")

// buildRecord fills a record to its token target with filler and distractor lines.
// Mandatory lines go in at uniform random positions, so head or tail truncation has
// no built-in advantage.
func (g *gen) buildRecord(id string, side corpus.Side, target int, must []string) string {
	title, types := "RECORD", cloudRecordTypes
	if side == corpus.SideLocal {
		title, types = "LOCAL RECORD", localRecordTypes
	}
	header := fmt.Sprintf("%s %s\nType: %s\nSynthetic data. Each reference line names what its code is for.\n\n", title, id, pick(g.r, types))
	footer := fmt.Sprintf("END OF %s %s\n", title, id)

	budget := target - EstimateTokens(header) - EstimateTokens(footer) - len(must)*mustLineTokens
	var body []recLine
	for budget > 0 {
		var l recLine
		if g.r.chance(1, distractorOneIn) {
			l.text = g.deal().line()
			budget -= lineTokens(l.text)
		} else {
			l.text = pick(g.r, fillers)(g.r, pick(g.r, subjects))
			l.stamped = true
			budget -= (stampLen + len(l.text) + 1 + 3) / 4
		}
		body = append(body, l)
	}
	for _, m := range must {
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

// recordID is non-sequential, so a model cannot walk other record ids, and a number
// is never used on both sides.
func (g *gen) recordID(side corpus.Side) string {
	prefix := "R"
	if side == corpus.SideLocal {
		prefix = "L"
	}
	for {
		n := g.r.between(100, 999)
		if !g.recIDs[n] {
			g.recIDs[n] = true
			return fmt.Sprintf("%s-%d", prefix, n)
		}
	}
}

// deal gives a label no earlier text in this item has used, with a fresh code.
func (g *gen) deal() coded {
	for range 64 {
		k, s := g.r.intn(len(kinds)), g.r.intn(len(subjects))
		if !g.used[[2]int{k, s}] {
			return g.take(k, s)
		}
	}
	start := g.r.intn(len(kinds) * len(subjects))
	for i := range len(kinds) * len(subjects) {
		v := (start + i) % (len(kinds) * len(subjects))
		k, s := v/len(subjects), v%len(subjects)
		if !g.used[[2]int{k, s}] {
			return g.take(k, s)
		}
	}
	panic("s3: label vocabulary exhausted")
}

// dealKind gives an unused label of the same kind.
func (g *gen) dealKind(k int) coded {
	start := g.r.intn(len(subjects))
	for i := range len(subjects) {
		s := (start + i) % len(subjects)
		if !g.used[[2]int{k, s}] {
			return g.take(k, s)
		}
	}
	panic("s3: no subject left for a kind")
}

// dealSubject gives an unused label for the same subject.
func (g *gen) dealSubject(s int) coded {
	start := g.r.intn(len(kinds))
	for i := range len(kinds) {
		k := (start + i) % len(kinds)
		if !g.used[[2]int{k, s}] {
			return g.take(k, s)
		}
	}
	panic("s3: no kind left for a subject")
}

func (g *gen) take(k, s int) coded {
	g.used[[2]int{k, s}] = true
	return coded{kinds[k], subjects[s], g.code()}
}
