package corpus

import (
	"fmt"
	"slices"
	"strings"
)

// MaxBatchQuestions is the upper limit on questions in one probe batch.
const MaxBatchQuestions = 10

// slotOrder deals a batch's fact questions. F3 gets the most slots because
// records hold the most facts, which keeps repeats of the scarcer categories
// down. Every category with an eligible fact is reached within the first six
// slots.
var slotOrder = []Category{F3, F5, F1, F3, F2, F4, F3, F5, F3, F1, F2, F4, F3, F5, F1, F2, F4}

// readyTurn is the first turn after which a fact's newest value exists.
func readyTurn(f Fact) int {
	if f.SupersededTurn > f.IntroducedTurn {
		return f.SupersededTurn
	}
	return f.IntroducedTurn
}

// sessionProbes builds one batch after every switch, addressed to the new owner,
// and one at the end, addressed to the final owner.
func (g *gen) sessionProbes(s *Session) []ProbeBatch {
	asked := map[string]int{}
	seq := 0
	prev := -1
	var out []ProbeBatch
	for i, t := range s.Turns {
		recipient, reason := t.Owner, ""
		switch {
		case i == len(s.Turns)-1:
			reason = "end"
		case s.Turns[i+1].Owner != t.Owner:
			recipient, reason = s.Turns[i+1].Owner, "switch"
		default:
			continue
		}
		out = append(out, g.sessionBatch(s, i, recipient, reason, prev, asked, &seq))
		prev = i
	}
	return out
}

func (g *gen) sessionBatch(s *Session, after int, recipient Side, reason string, prev int, asked map[string]int, seq *int) ProbeBatch {
	var eligible []Fact
	for _, id := range g.order {
		f := g.facts[id]
		if f.Category == F6 || readyTurn(f) > after {
			continue
		}
		if f.Category == F5 && recipient != SideLocal {
			continue
		}
		eligible = append(eligible, f)
	}
	nF6 := 1
	if len(eligible) >= MaxBatchQuestions-1 && g.r.chance(1, 3) {
		nF6 = 2
	}
	chosen := pickFacts(eligible, MaxBatchQuestions-nF6, prev, asked)
	for _, f := range chosen {
		asked[f.ID]++
	}

	var cloudRead, localRead []string
	for _, t := range s.Turns[:after+1] {
		switch {
		case t.ReadRecord == "":
		case t.Owner == SideCloud:
			cloudRead = append(cloudRead, t.ReadRecord)
		default:
			localRead = append(localRead, t.ReadRecord)
		}
	}
	qs := g.questions(chosen, nF6, recipient, cloudRead, localRead, seq)
	return ProbeBatch{AfterTurn: after, Recipient: recipient, Reason: reason, Questions: qs, Prompt: batchPrompt(qs)}
}

// pickFacts fills a batch, alternating within each category between facts new
// since the previous batch and older ones. Within either group it takes the
// least-asked fact first, so every fact tends to be asked before any repeats.
func pickFacts(eligible []Fact, want, prev int, asked map[string]int) []Fact {
	chosen := map[string]bool{}
	recentNext := map[Category]bool{F1: true, F2: true, F3: true, F4: true, F5: true}
	var out []Fact
	for len(out) < want {
		added := false
		for _, cat := range slotOrder {
			if len(out) == want {
				break
			}
			f, ok := pickOne(eligible, cat, recentNext[cat], prev, asked, chosen)
			if !ok {
				continue
			}
			recentNext[cat] = !recentNext[cat]
			chosen[f.ID] = true
			out = append(out, f)
			added = true
		}
		if !added {
			break
		}
	}
	return out
}

func pickOne(eligible []Fact, cat Category, recent bool, prev int, asked map[string]int, chosen map[string]bool) (Fact, bool) {
	if f, ok := pickFrom(eligible, cat, recent, prev, asked, chosen); ok {
		return f, true
	}
	return pickFrom(eligible, cat, !recent, prev, asked, chosen)
}

func pickFrom(eligible []Fact, cat Category, recent bool, prev int, asked map[string]int, chosen map[string]bool) (Fact, bool) {
	var best Fact
	found := false
	for _, f := range eligible {
		if f.Category != cat || chosen[f.ID] || (readyTurn(f) > prev) != recent {
			continue
		}
		switch {
		case !found, asked[f.ID] < asked[best.ID]:
			best, found = f, true
		case asked[f.ID] > asked[best.ID]:
		case recent && readyTurn(f) > readyTurn(best), !recent && readyTurn(f) < readyTurn(best):
			best = f
		}
	}
	return best, found
}

// questions assembles a batch in shuffled order, so a question's position says
// nothing about its category. Ids run through the whole item, so a repeated
// fact still gets a new id.
func (g *gen) questions(facts []Fact, nF6 int, recipient Side, cloudRead, localRead []string, seq *int) []Question {
	qs := make([]Question, 0, len(facts)+nF6)
	for _, f := range facts {
		qs = append(qs, Question{
			Text:     g.qtext[f.ID],
			Category: f.Category,
			FactID:   f.ID,
			Answer:   f.Value,
			Stale:    slices.Clone(f.Superseded),
		})
	}
	for range nF6 {
		qs = append(qs, g.f6Question(recipient, cloudRead, localRead))
	}
	shuffled(g.r, qs)
	for i := range qs {
		*seq++
		qs[i].ID = fmt.Sprintf("q%d", *seq)
	}
	return qs
}

// f6Question asks about a label no text in this item has used. It runs after
// every turn and record is built, so an unused label stays unused. Each one uses
// a question form that real facts use, naming only records already read, and a
// local record only to the local side.
func (g *gen) f6Question(recipient Side, cloudRead, localRead []string) Question {
	ki, si := g.pair()
	kind, subject := kinds[ki], subjects[si]

	forms := []func() string{
		func() string { return qRequired(kind, subject) },
		func() string { return qChosen(kind, subject) },
		func() string { return qCurrent(kind, subject) },
	}
	if len(cloudRead) > 0 {
		cloud := func() string { return qRecord(kind, pick(g.r, cloudRead), subject) }
		forms = append(forms, cloud, cloud, cloud)
	}
	if recipient == SideLocal && len(localRead) > 0 {
		forms = append(forms, func() string { return qLocalRecord(kind, pick(g.r, localRead), subject) })
	}
	text := pick(g.r, forms)()

	id := g.addFact(Fact{
		Category:       F6,
		Value:          Unknown,
		IntroducedTurn: -1,
		SupersededTurn: -1,
		Label:          fmt.Sprintf("%s for the %s, never mentioned", kind, subject),
		Kind:           kind,
		Subject:        subject,
	}, text)
	return Question{Text: text, Category: F6, FactID: id, Answer: Unknown}
}

// batchPrompt renders a batch. It never contains a code: the answer format uses
// a placeholder.
func batchPrompt(qs []Question) string {
	var b strings.Builder
	b.WriteString("Status check before we continue. Answer every question below from what you know about this session.\n")
	b.WriteString("You may use your tools if they help, with at most 3 tool calls in total.\n")
	b.WriteString("Your final output must be only JSON in exactly this form, with one entry for every question id:\n")
	fmt.Fprintf(&b, `{"answers":[{"id":"%s","answer":"<code or UNKNOWN>"}]}`, qs[0].ID)
	b.WriteString("\nEach answer is the exact code asked for. Answer UNKNOWN when you do not know it.\n\nQuestions:\n")
	for _, q := range qs {
		fmt.Fprintf(&b, "%s: %s\n", q.ID, q.Text)
	}
	return b.String()
}
