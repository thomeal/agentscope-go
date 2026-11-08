package corpus

import (
	"fmt"
)

// Benchmark shape. 40 reads of about 2.45K tokens plus their text come to about
// 100K tokens. Every read holds an F3 fact and every third read holds two, which
// gives 54 F3 facts from the first step to the last. At roughly 15 tokens for a
// label and code, the 71 cloud facts alone would fill a 1,000-token summary even
// if a summarizer could somehow tell them from the distractors.
const (
	cbenchReads             = 40
	cbenchDecisions         = 6
	cbenchF1                = 6
	cbenchF4                = 5
	cbenchRecordMin         = longRecordMin
	cbenchRecordMax         = longRecordMax
	cbenchLocalMin          = 700
	cbenchLocalMax          = 950
	cbenchQuestionsPerBatch = MaxBatchQuestions - 1 // one slot in each batch is F6
)

// GenerateCBench builds one component-benchmark item. Every F1-F5 fact is asked
// exactly once, across as many batches as that takes, all addressed to the local
// side after the whole history. It panics on an unknown set.
func GenerateCBench(seed int64, set string) CBench {
	checkSet(set)
	g := newGen(seed, streamCBench)

	history := g.buildTurns(g.planCBenchHistory())
	cloudRecords := readRecords(history)
	local := g.buildTurns(g.planCBenchLocal())
	localRecords := readRecords(local)

	c := CBench{
		ID:       fmt.Sprintf("cbench-%s-%d", set, seed),
		Seed:     seed,
		Set:      set,
		System:   systemPrompt,
		History:  g.steps(history, "call"),
		LocalOwn: g.steps(local, "local_call"),
		Facts:    g.facts,
	}
	c.Probes = g.cbenchProbes(len(history)-1, cloudRecords, localRecords)

	canaries := map[string]bool{}
	for _, st := range c.LocalOwn {
		for _, code := range Codes(st.User + "\n" + st.ToolResult + "\n" + st.Reply) {
			canaries[code] = true
		}
	}
	c.Canaries = sortedKeys(canaries)
	return c
}

func (g *gen) planCBenchHistory() []turnPlan {
	total := cbenchReads + cbenchDecisions
	plans := make([]turnPlan, total)
	for i := range plans {
		plans[i].owner = SideCloud
	}
	// One decision per evenly sized bucket. The first and last steps stay reads,
	// so F3 facts reach both ends of the history.
	for d := range cbenchDecisions {
		lo := 1 + (total-2)*d/cbenchDecisions
		hi := 1 + (total-2)*(d+1)/cbenchDecisions - 1
		plans[g.r.between(lo, hi)].kind = kindDecision
	}
	read := 0
	for i := range plans {
		if plans[i].kind != kindRead {
			continue
		}
		plans[i].target = g.r.between(cbenchRecordMin, cbenchRecordMax)
		plans[i].nFacts = 1
		if read%3 == 0 {
			plans[i].nFacts = 2
		}
		read++
	}
	g.planF1(plans, cbenchF1)
	g.planF4(plans, cbenchF4, false)
	return plans
}

// planCBenchLocal scripts about 3K tokens of local history: three reads and two
// edge-only decisions holding F5 facts, plus one plain question.
func (g *gen) planCBenchLocal() []turnPlan {
	read := func() turnPlan {
		return turnPlan{owner: SideLocal, edge: true, kind: kindRead, target: g.r.between(cbenchLocalMin, cbenchLocalMax), nFacts: 2}
	}
	decision := turnPlan{owner: SideLocal, edge: true, kind: kindDecision}
	plain := turnPlan{owner: SideLocal, edge: true, kind: kindPlain}
	return shuffled(g.r, []turnPlan{read(), read(), read(), decision, decision, plain})
}

func readRecords(turns []Turn) []string {
	var out []string
	for _, t := range turns {
		if t.ReadRecord != "" {
			out = append(out, t.ReadRecord)
		}
	}
	return out
}

func (g *gen) steps(turns []Turn, idPrefix string) []CStep {
	out := make([]CStep, len(turns))
	for i, t := range turns {
		st := CStep{User: t.User, Reply: t.ExpectedReply}
		if t.ReadRecord != "" {
			st.ToolName, _ = RecordTool(t.Owner)
			st.ToolID = fmt.Sprintf("%s_%04d", idPrefix, i)
			st.ToolResult = g.records[t.ReadRecord].Text
			st.RecordID = t.ReadRecord
		}
		out[i] = st
	}
	return out
}

// cbenchProbes asks every fact once. Facts are dealt category by category, so
// each batch holds a similar mix.
func (g *gen) cbenchProbes(after int, cloudRecords, localRecords []string) []ProbeBatch {
	var facts []Fact
	for _, cat := range []Category{F3, F5, F1, F2, F4} {
		var group []Fact
		for _, id := range g.order {
			if f := g.facts[id]; f.Category == cat {
				group = append(group, f)
			}
		}
		facts = append(facts, shuffled(g.r, group)...)
	}
	n := (len(facts) + cbenchQuestionsPerBatch - 1) / cbenchQuestionsPerBatch
	groups := make([][]Fact, n)
	for i, f := range facts {
		groups[i%n] = append(groups[i%n], f)
	}
	seq := 0
	out := make([]ProbeBatch, 0, n)
	for _, group := range groups {
		qs := g.questions(group, 1, SideLocal, cloudRecords, localRecords, &seq)
		out = append(out, ProbeBatch{AfterTurn: after, Recipient: SideLocal, Reason: "end", Questions: qs, Prompt: batchPrompt(qs)})
	}
	return out
}
