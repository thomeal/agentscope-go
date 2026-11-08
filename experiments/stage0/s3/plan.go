package s3

import (
	"fmt"

	"github.com/agentscope-ai/agentscope-go/v2/experiments/stage0/corpus"
)

// PRNG streams, so a block's plan and an item with equal seeds share nothing.
const (
	streamBlock uint64 = 1
	streamItem  uint64 = 2
	streamOrder uint64 = 3
)

// unitType is one entry in a block's plan. A pair unit makes two items.
type unitType int

const (
	unitPair unitType = iota
	unitFollowup
	unitReasoning
	unitLocalTool
	unitCloud
	unitEdge
	numUnitTypes
)

// A block is the repeating shape of a set: 60 items in 47 units. 13 pairs give
// 26 of 60 items in pairs (43 percent), with 15 followup, 12 reasoning, 12
// local_tool, 18 cloud_context and 3 edge_private.
var unitQuota = [numUnitTypes]int{
	unitPair:      13,
	unitFollowup:  2,
	unitReasoning: 12,
	unitLocalTool: 12,
	unitCloud:     5,
	unitEdge:      3,
}

const (
	blockItems = 60
	blockUnits = 47

	// scheduleWindow bounds how far shuffling moves a unit from its evenly spread
	// position. It keeps the order unpredictable and a partial block's proportions
	// close to the whole block's.
	scheduleWindow = 8
)

// formClass is the shape of a query.
type formClass int

const (
	formPlain      formClass = iota // short, no record mentioned
	formRecordWord                  // short, mentions "the record" without an id
	formLong                        // long and simple, no record mentioned
	formNamed                       // short, names a record by id
	formNamedLong                   // long and simple, names a record by id
	formCleared                     // short; recent text says the value is gone
)

// family is a kind of reasoning problem.
type family int

const (
	famTimers   family = iota // short: congruences
	famOrders                 // short: counting valid orders
	famSchedule               // medium: a constrained schedule
	famTable                  // long: multi-step deduction over a table
	famPriority               // medium: a dependency order
)

// formDecks deal each unit type's query forms across a block. Long simple queries
// and record-mentioning queries sit in pairs, so neither cue can settle a pair.
var formDecks = [numUnitTypes][]formClass{
	unitPair: {
		formPlain, formPlain, formPlain, formPlain, formPlain,
		formRecordWord, formRecordWord, formRecordWord, formRecordWord,
		formLong, formLong, formLong, formLong,
	},
	unitFollowup: {formNamed, formCleared},
	unitLocalTool: {
		formNamed, formNamed, formNamed, formNamed,
		formNamed, formNamed, formNamed, formNamed,
		formNamedLong, formNamedLong, formNamedLong, formNamedLong,
	},
	unitCloud: {formNamed, formNamed, formNamedLong, formPlain, formPlain},
	unitEdge:  {formNamed, formPlain, formNamedLong},
}

// familyDeck deals reasoning problems across a block: half are short, so query
// length alone does not reveal them.
var familyDeck = []family{
	famTimers, famTimers, famTimers,
	famOrders, famOrders, famOrders,
	famSchedule, famSchedule,
	famTable, famTable,
	famPriority, famPriority,
}

// unit is one planned entry: everything an item's draw needs besides the seed.
type unit struct {
	set    string
	typ    unitType
	number int // position across the set; fixes the seed and the target label
	slot   int // first item position across the set; fixes the item id
	seed   int64
	form   formClass
	family family
	owner  corpus.Side
}

func build(set string, base int64, n int) []Item {
	if n <= 0 {
		return nil
	}
	out := make([]Item, 0, n)
	for block := 0; len(out) < n; block++ {
		for _, u := range planBlock(set, base, block) {
			room := n - len(out)
			if room == 0 {
				break
			}
			// A pair never splits. The last place goes to the next unpaired unit.
			if u.typ == unitPair && room == 1 {
				continue
			}
			out = append(out, u.items()...)
		}
	}
	return out
}

// planBlock fixes the units of one block from the base and block index alone.
func planBlock(set string, base int64, block int) []unit {
	r := newPRNG(base+int64(block), streamBlock)
	order := blockSchedule(r)

	var forms [numUnitTypes][]formClass
	var owners [numUnitTypes][]corpus.Side
	for t := range numUnitTypes {
		forms[t] = shuffled(r, append([]formClass(nil), formDecks[t]...))
		owners[t] = ownerDeck(r, unitQuota[t])
	}
	families := shuffled(r, append([]family(nil), familyDeck...))

	out := make([]unit, 0, blockUnits)
	var dealt [numUnitTypes]int
	slot := block * blockItems
	for pos, t := range order {
		u := unit{
			set:    set,
			typ:    t,
			number: block*blockUnits + pos,
			slot:   slot,
			owner:  owners[t][dealt[t]],
		}
		u.seed = base + int64(u.number)
		if t == unitReasoning {
			u.family = families[dealt[t]]
		} else {
			u.form = forms[t][dealt[t]]
		}
		dealt[t]++
		slot++
		if t == unitPair {
			slot++
		}
		out = append(out, u)
	}
	return out
}

// blockSchedule spreads each unit type evenly through a block by stride scheduling,
// then shuffles within small windows, so kind proportions hold at any size and an
// index says little about a kind.
func blockSchedule(r *prng) []unitType {
	var done [numUnitTypes]int
	out := make([]unitType, 0, blockUnits)
	for range blockUnits {
		best := -1
		for t, q := range unitQuota {
			if done[t] == q {
				continue
			}
			// Lowest pass value (2*done+1)/quota wins, compared without division.
			if best < 0 || (2*done[t]+1)*unitQuota[best] < (2*done[best]+1)*q {
				best = t
			}
		}
		done[best]++
		out = append(out, unitType(best))
	}
	for lo := 0; lo < len(out); lo += scheduleWindow {
		shuffled(r, out[lo:min(lo+scheduleWindow, len(out))])
	}
	return out
}

// ownerDeck gives a unit type a balanced mix of owners, so Owner carries nothing
// about Kind. With an odd count the extra one lands on a random side.
func ownerDeck(r *prng, n int) []corpus.Side {
	first, second := corpus.SideCloud, corpus.SideLocal
	if r.chance(1, 2) {
		first, second = second, first
	}
	out := make([]corpus.Side, n)
	for i := range out {
		out[i] = first
		if i%2 == 1 {
			out[i] = second
		}
	}
	return shuffled(r, out)
}

// items builds the unit's one or two items. A pair's halves take a random order, so
// which of two adjacent ids is which half says nothing.
func (u unit) items() []Item {
	switch u.typ {
	case unitPair:
		place := placeRecent
		if u.form == formRecordWord {
			place = placeRecordAndRecent
		}
		pairID := fmt.Sprintf("pair-%s-%05d", u.set, u.number)
		first, second := 0, 1
		if newPRNG(u.seed, streamOrder).chance(1, 2) {
			first, second = 1, 0
		}
		follow := buildItem(u, KindFollowup, place, u.slot+first)
		cloud := buildItem(u, KindCloudContext, placeRecord, u.slot+second)
		follow.PairID, cloud.PairID = pairID, pairID
		if first == 0 {
			return []Item{follow, cloud}
		}
		return []Item{cloud, follow}
	case unitFollowup:
		if u.form == formCleared {
			return []Item{buildItem(u, KindFollowup, placeCleared, u.slot)}
		}
		return []Item{buildItem(u, KindFollowup, placeRecordAndRecent, u.slot)}
	case unitCloud:
		return []Item{buildItem(u, KindCloudContext, placeRecord, u.slot)}
	case unitReasoning:
		return []Item{buildItem(u, KindReasoning, placeNone, u.slot)}
	case unitLocalTool:
		return []Item{buildItem(u, KindLocalTool, placeNone, u.slot)}
	case unitEdge:
		return []Item{buildItem(u, KindEdgePrivate, placeNone, u.slot)}
	}
	panic(fmt.Sprintf("s3: unknown unit type %d", u.typ))
}

// Target labels are dealt without repeats. Tuning takes the labels whose kind and
// subject indices sum to an even number and acceptance those with an odd sum, so a
// query cannot recur across the sets. 1009 is coprime with 1800, so a set repeats no
// target label within its first 1800 units, about 38 blocks.
const (
	labelsPerSet = 1800
	labelStride  = 1009
	labelOffset  = 211
)

func targetLabel(set string, number int) (kind, subject int) {
	bit := 0
	if set == SetAcceptance {
		bit = 1
	}
	idx := (number*labelStride + labelOffset) % labelsPerSet
	subject = idx % len(subjects)
	kind = 2*(idx/len(subjects)) + (subject+bit)%2
	return kind, subject
}
