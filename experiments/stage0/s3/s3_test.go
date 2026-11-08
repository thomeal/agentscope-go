package s3

import (
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/agentscope-ai/agentscope-go/v2/experiments/stage0/corpus"
)

// pinnedHash freezes Pilot. Changing any fixture text must change it on purpose.
const pinnedHash = "8612d59d949aeb847837e9f0a5b5f2c7c1d4f8d7de8960989c36719a268d390c"

var (
	pilotOnce sync.Once
	pilotData []Item
	bigOnce   sync.Once
	bigData   []Item
)

// pilot is shared and read-only. Corruption tests take clones.
func pilot(t *testing.T) []Item {
	t.Helper()
	pilotOnce.Do(func() { pilotData = Pilot() })
	return pilotData
}

// bigAcceptance is large enough to exercise every family and form many times.
func bigAcceptance(t *testing.T) []Item {
	t.Helper()
	bigOnce.Do(func() { bigData = Acceptance(180) })
	return bigData
}

func clone(t *testing.T, it Item) Item {
	t.Helper()
	b, err := json.Marshal(it)
	if err != nil {
		t.Fatal(err)
	}
	var out Item
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func report(t *testing.T, p problems) {
	t.Helper()
	const limit = 25
	for i, s := range p {
		if i == limit {
			t.Errorf("... and %d more", len(p)-limit)
			return
		}
		t.Error(s)
	}
}

func dump(t *testing.T, items []Item) string {
	t.Helper()
	b, err := json.Marshal(items)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func firstOf(t *testing.T, items []Item, want func(Item) bool) Item {
	t.Helper()
	for _, it := range items {
		if want(it) {
			return it
		}
	}
	t.Fatal("no item matches")
	return Item{}
}

func ofKind(k Kind) func(Item) bool { return func(it Item) bool { return it.Kind == k } }

func TestDeterministic(t *testing.T) {
	if a, b := dump(t, Pilot()), dump(t, pilot(t)); a != b {
		t.Error("Pilot differs between calls")
	}
	if a, b := dump(t, Tuning(60)), dump(t, pilot(t)); a != b {
		t.Error("Pilot is not Tuning(60)")
	}
	if a, b := dump(t, Acceptance(13)), dump(t, Acceptance(13)); a != b {
		t.Error("Acceptance differs between calls")
	}
	// Building other items first must not disturb a later list: no shared state.
	Tuning(7)
	Acceptance(9)
	if a, b := dump(t, Tuning(60)), dump(t, pilot(t)); a != b {
		t.Error("Tuning depends on what was built before it")
	}
	if Hash() != Hash() {
		t.Error("Hash differs between calls")
	}
	if Tuning(0) != nil || Acceptance(-3) != nil {
		t.Error("a non-positive n returned items")
	}
}

func TestHashPinned(t *testing.T) {
	if got := Hash(); got != pinnedHash {
		t.Errorf("Hash = %s, pinned %s. Pilot changed: update the pin only for an intended corpus change.", got, pinnedHash)
	}
}

func TestPilotValid(t *testing.T) {
	items := pilot(t)
	if len(items) != 60 {
		t.Fatalf("Pilot has %d items", len(items))
	}
	var p problems
	for _, it := range items {
		if it.Set != SetTuning {
			p.add("set", "%s has set %q", it.ID, it.Set)
		}
		p = append(p, validateItem(it)...)
	}
	checkSet(&p, items)
	report(t, p)
}

func TestAcceptanceValid(t *testing.T) {
	items := bigAcceptance(t)
	if len(items) != 180 {
		t.Fatalf("Acceptance(180) has %d items", len(items))
	}
	var p problems
	for _, it := range items {
		if it.Set != SetAcceptance {
			p.add("set", "%s has set %q", it.ID, it.Set)
		}
		p = append(p, validateItem(it)...)
	}
	checkSet(&p, items)
	report(t, p)
}

func TestTuningAndAcceptanceDisjoint(t *testing.T) {
	tune, acc := Tuning(120), bigAcceptance(t)
	seeds, ids, queries, codes := map[int64]bool{}, map[string]bool{}, map[string]bool{}, map[string]bool{}
	for _, it := range tune {
		seeds[it.Seed] = true
		ids[it.ID] = true
		queries[it.Query] = true
		if testCodeRe.MatchString(it.Answer) {
			codes[it.Answer] = true
		}
	}
	for _, it := range acc {
		if seeds[it.Seed] {
			t.Errorf("seed %d is in both sets", it.Seed)
		}
		if ids[it.ID] {
			t.Errorf("id %s is in both sets", it.ID)
		}
		if queries[it.Query] {
			t.Errorf("query %q is in both sets", shorten(it.Query))
		}
		if codes[it.Answer] {
			t.Errorf("an answer of %s is also a tuning answer", it.ID)
		}
	}
}

// TestSeedBasesApart checks the seed ranges cannot meet at any realistic size.
func TestSeedBasesApart(t *testing.T) {
	if gap := acceptanceSeedBase - tuningSeedBase; gap < 100_000 {
		t.Errorf("seed bases are %d apart", gap)
	}
}

func countKinds(items []Item) map[Kind]int {
	out := map[Kind]int{}
	for _, it := range items {
		out[it.Kind]++
	}
	return out
}

func TestKindProportions(t *testing.T) {
	want := map[Kind]int{KindFollowup: 15, KindReasoning: 12, KindLocalTool: 12, KindCloudContext: 18, KindEdgePrivate: 3}
	check := func(name string, items []Item, factor int) {
		got := countKinds(items)
		for k, n := range want {
			if got[k] != n*factor {
				t.Errorf("%s has %d %s, want %d", name, got[k], k, n*factor)
			}
		}
	}
	check("Pilot", pilot(t), 1)
	check("Acceptance(180)", bigAcceptance(t), 3)
}

func pairShare(items []Item) float64 {
	n := 0
	for _, it := range items {
		if it.PairID != "" {
			n++
		}
	}
	return float64(n) / float64(len(items))
}

func TestPairShare(t *testing.T) {
	if s := pairShare(pilot(t)); s < 0.40 {
		t.Errorf("Pilot has %.0f%% of items in pairs", 100*s)
	}
	for _, n := range []int{40, 60, 90, 120, 150, 200} {
		if s := pairShare(Tuning(n)); s < 0.40 {
			t.Errorf("Tuning(%d) has %.0f%% of items in pairs", n, 100*s)
		}
	}
}

// TestAnySizeKeepsPromises checks exact length, unbroken pairs and stable prefixes at
// sizes that cut through blocks and pairs.
func TestAnySizeKeepsPromises(t *testing.T) {
	long := Acceptance(130)
	for _, n := range []int{1, 2, 3, 4, 5, 7, 10, 11, 21, 34, 47, 59, 60, 61, 73, 100, 119, 121} {
		items := Acceptance(n)
		if len(items) != n {
			t.Errorf("Acceptance(%d) has %d items", n, len(items))
			continue
		}
		pairs := map[string]int{}
		for _, it := range items {
			if it.PairID != "" {
				pairs[it.PairID]++
			}
		}
		for id, c := range pairs {
			if c != 2 {
				t.Errorf("Acceptance(%d) holds %d of %s", n, c, id)
			}
		}
		if dump(t, items[:n-1]) != dump(t, long[:n-1]) {
			t.Errorf("Acceptance(%d) is not a prefix of a longer list, apart from its last item", n)
		}
	}
}

// TestProportionsStableAtPartialSizes checks a list cut mid-block stays close to the
// block's mix, so a smaller run is not quietly a different experiment.
func TestProportionsStableAtPartialSizes(t *testing.T) {
	want := map[Kind]float64{KindFollowup: 0.25, KindReasoning: 0.20, KindLocalTool: 0.20, KindCloudContext: 0.30, KindEdgePrivate: 0.05}
	for _, n := range []int{20, 30, 45, 75, 100} {
		items := Tuning(n)
		got := countKinds(items)
		for k, share := range want {
			if d := float64(got[k])/float64(n) - share; d < -0.08 || d > 0.08 {
				t.Errorf("Tuning(%d) has %d %s, %.0f%% off", n, got[k], k, 100*d)
			}
		}
	}
}

func TestPairsAreBalancedAndComplete(t *testing.T) {
	items := pilot(t)
	pairs := map[string]int{}
	for _, it := range items {
		if it.PairID != "" {
			pairs[it.PairID]++
		}
	}
	if len(pairs) != 13 {
		t.Errorf("Pilot has %d pairs, want 13", len(pairs))
	}
}

// TestPairOrderSaysNothing checks a half's position does not give away its kind: the
// followup comes first in some pairs and second in others.
func TestPairOrderSaysNothing(t *testing.T) {
	first := map[Kind]int{}
	seen := map[string]bool{}
	for _, it := range bigAcceptance(t) {
		if it.PairID != "" && !seen[it.PairID] {
			seen[it.PairID] = true
			first[it.Kind]++
		}
	}
	total := first[KindFollowup] + first[KindCloudContext]
	if d := first[KindFollowup] - first[KindCloudContext]; d < -total/2 || d > total/2 {
		t.Errorf("first halves: %d followup, %d cloud_context", first[KindFollowup], first[KindCloudContext])
	}
}

// TestOwnerSaysNothing checks owner is balanced within every kind.
func TestOwnerSaysNothing(t *testing.T) {
	counts := map[Kind]map[corpus.Side]int{}
	for _, it := range bigAcceptance(t) {
		if counts[it.Kind] == nil {
			counts[it.Kind] = map[corpus.Side]int{}
		}
		counts[it.Kind][it.Owner]++
	}
	for k, c := range counts {
		total := c[corpus.SideCloud] + c[corpus.SideLocal]
		if d := c[corpus.SideCloud] - c[corpus.SideLocal]; d < -total/4 || d > total/4 {
			t.Errorf("%s has owners cloud %d, local %d", k, c[corpus.SideCloud], c[corpus.SideLocal])
		}
	}
}

// TestHistorySizeSaysNothing checks every kind spans a similar range of history size.
func TestHistorySizeSaysNothing(t *testing.T) {
	sum, n := map[Kind]int{}, map[Kind]int{}
	lo, hi := map[Kind]int{}, map[Kind]int{}
	for _, it := range bigAcceptance(t) {
		h := historyTokens(it)
		sum[it.Kind] += h
		n[it.Kind]++
		if lo[it.Kind] == 0 || h < lo[it.Kind] {
			lo[it.Kind] = h
		}
		hi[it.Kind] = max(hi[it.Kind], h)
	}
	for k := range n {
		mean := sum[k] / n[k]
		if mean < 30_000 || mean > 46_000 {
			t.Errorf("%s has mean history %d tokens", k, mean)
		}
		// Each kind reaches both halves of the range, so size alone cannot sort them.
		if lo[k] > 30_000 || hi[k] < 42_000 {
			t.Errorf("%s spans only %d to %d tokens", k, lo[k], hi[k])
		}
	}
}

// expectedSide is the route each kind is designed for. The harness measures the real
// one; these tests use it only to show simple cues do not recover the design.
func expectedSide(k Kind) corpus.Side {
	if k == KindReasoning || k == KindCloudContext {
		return corpus.SideCloud
	}
	return corpus.SideLocal
}

func TestDistractorsPresent(t *testing.T) {
	const short, long = 120, 300
	longSimple, shortHard, recordRecent, longCloud := 0, 0, 0, 0
	for _, it := range pilot(t) {
		switch it.Kind {
		case KindFollowup:
			if len(it.Query) >= long {
				longSimple++
			}
			if strings.Contains(strings.ToLower(it.Query), "record") && strings.Contains(PublishedRecent(it), it.Answer) {
				recordRecent++
			}
		case KindCloudContext:
			if len(it.Query) >= long {
				longCloud++
			}
		case KindReasoning:
			if len(it.Query) <= short {
				shortHard++
			}
		}
	}
	if longSimple < 4 {
		t.Errorf("%d long simple followups", longSimple)
	}
	if longCloud < 4 {
		t.Errorf("%d long cloud_context queries", longCloud)
	}
	if shortHard < 5 {
		t.Errorf("%d short reasoning queries", shortHard)
	}
	if recordRecent < 5 {
		t.Errorf("%d record-mentioning queries answered by recent text", recordRecent)
	}
}

// bestAccuracy returns the score of the best of a family of query-only rules, against
// the designed routes, so a test can show a simple cue falls short.
func bestAccuracy(items []Item, rules []func(Item) corpus.Side) float64 {
	best := 0
	for _, rule := range rules {
		right := 0
		for _, it := range items {
			if rule(it) == expectedSide(it.Kind) {
				right++
			}
		}
		best = max(best, right)
	}
	return float64(best) / float64(len(items))
}

func both(cue func(Item) bool) []func(Item) corpus.Side {
	return []func(Item) corpus.Side{
		func(it Item) corpus.Side {
			if cue(it) {
				return corpus.SideCloud
			}
			return corpus.SideLocal
		},
		func(it Item) corpus.Side {
			if cue(it) {
				return corpus.SideLocal
			}
			return corpus.SideCloud
		},
	}
}

// TestSimpleCuesFallShort: no single cue gets close to what recent text can reach. The
// limit sits under the 78 percent that the pairs cap every query-only rule at, so it
// can fail. Constant routes reach exactly half.
func TestSimpleCuesFallShort(t *testing.T) {
	items := pilot(t)
	const limit = 0.75

	always := func(side corpus.Side) []func(Item) corpus.Side {
		return []func(Item) corpus.Side{func(Item) corpus.Side { return side }}
	}
	if got := bestAccuracy(items, always(corpus.SideLocal)); got != 0.5 {
		t.Errorf("always local reaches %.0f%%", 100*got)
	}
	if got := bestAccuracy(items, always(corpus.SideCloud)); got != 0.5 {
		t.Errorf("always cloud reaches %.0f%%", 100*got)
	}

	var lengthRules []func(Item) corpus.Side
	for _, it := range items {
		threshold := len(it.Query)
		lengthRules = append(lengthRules, both(func(x Item) bool { return len(x.Query) >= threshold })...)
	}
	if got := bestAccuracy(items, lengthRules); got > limit {
		t.Errorf("a query length threshold reaches %.0f%%", 100*got)
	}
	for _, word := range []string{"record", "local", "edge", "site", "step", "r-", "l-"} {
		if got := bestAccuracy(items, both(func(x Item) bool { return strings.Contains(strings.ToLower(x.Query), word) })); got > limit {
			t.Errorf("the word %q reaches %.0f%%", word, 100*got)
		}
	}
	if got := bestAccuracy(items, both(func(x Item) bool { return x.Owner == corpus.SideCloud })); got > limit {
		t.Errorf("the owner reaches %.0f%%", 100*got)
	}
	if got := bestAccuracy(items, both(func(x Item) bool {
		return testCodeRe.MatchString(PublishedRecent(x))
	})); got > limit {
		t.Errorf("a code in recent text reaches %.0f%%", 100*got)
	}
}

// TestStateDecidesPairs is the design's central claim, run both ways. No function of
// the query alone can get both halves of a pair, and reading recent text can.
func TestStateDecidesPairs(t *testing.T) {
	items := pilot(t)
	groups := map[string][]Item{}
	for _, it := range items {
		if it.PairID != "" {
			groups[it.PairID] = append(groups[it.PairID], it)
		}
	}
	bagRight, labelRight := 0, 0
	for id, g := range groups {
		if g[0].Query != g[1].Query {
			t.Fatalf("%s: a query-only router could tell the halves apart", id)
		}
		if expectedSide(g[0].Kind) == expectedSide(g[1].Kind) {
			t.Errorf("%s: both halves expect the same route", id)
		}
		for _, it := range g {
			kind, subject, ok := queryLabel(it.Query)
			if !ok {
				t.Fatalf("%s: no label in query", it.ID)
			}
			recent := strings.ToLower(PublishedRecent(it))

			side := corpus.SideCloud
			if statesLabel(recent, kind, subject) {
				side = corpus.SideLocal
			}
			if side == expectedSide(it.Kind) {
				labelRight++
			}

			// A bag-of-words overlap sees both halves of the label in both histories.
			bag := corpus.SideCloud
			if strings.Contains(recent, kind) && strings.Contains(recent, subject) {
				bag = corpus.SideLocal
			}
			if bag == expectedSide(it.Kind) {
				bagRight++
			}
		}
	}
	if labelRight != 2*len(groups) {
		t.Errorf("a recent-text label match got %d of %d halves", labelRight, 2*len(groups))
	}
	if bagRight != len(groups) {
		t.Errorf("a bag of words got %d of %d halves, want exactly one per pair", bagRight, 2*len(groups))
	}
}

func TestRead(t *testing.T) {
	for _, it := range pilot(t) {
		for id, text := range it.Records {
			side, other := corpus.SideCloud, corpus.SideLocal
			if strings.HasPrefix(id, "L-") {
				side, other = corpus.SideLocal, corpus.SideCloud
			}
			if got, ok := Read(it, side, id); !ok || got != text {
				t.Errorf("%s: Read(%s, %s) = %v", it.ID, side, id, ok)
			}
			if got, ok := Read(it, other, id); ok || got != "" {
				t.Errorf("%s: Read(%s, %s) crossed sides", it.ID, other, id)
			}
			if _, ok := Read(it, corpus.Side(""), id); ok {
				t.Errorf("%s: Read with no side served %s", it.ID, id)
			}
			if _, ok := Read(it, corpus.Side("Cloud"), id); ok {
				t.Errorf("%s: Read with a misspelled side served %s", it.ID, id)
			}
			if _, ok := Read(it, side, strings.ToLower(id)); ok {
				t.Errorf("%s: Read served a lower-case id", it.ID)
			}
		}
		for _, side := range []corpus.Side{corpus.SideCloud, corpus.SideLocal} {
			for _, id := range []string{"", "R-", "L-", "R-0", "L-0", "unknown"} {
				if _, ok := Read(it, side, id); ok {
					t.Errorf("%s: Read(%s, %q) served an unknown id", it.ID, side, id)
				}
			}
		}
	}
}

// TestReadByKind checks each kind's record is reachable by the right side only.
func TestReadByKind(t *testing.T) {
	local := firstOf(t, pilot(t), ofKind(KindLocalTool))
	id := localIDRe.FindString(local.Query)
	if text, ok := Read(local, corpus.SideLocal, id); !ok || !strings.Contains(text, local.Answer) {
		t.Errorf("%s: the local side cannot read its answer", local.ID)
	}
	if _, ok := Read(local, corpus.SideCloud, id); ok {
		t.Errorf("%s: the cloud side read a local record", local.ID)
	}

	cloud := firstOf(t, pilot(t), ofKind(KindCloudContext))
	found := false
	for id, text := range cloud.Records {
		if !strings.Contains(text, cloud.Answer) {
			continue
		}
		found = true
		if _, ok := Read(cloud, corpus.SideLocal, id); ok {
			t.Errorf("%s: the local side read the cloud record", cloud.ID)
		}
		if _, ok := Read(cloud, corpus.SideCloud, id); !ok {
			t.Errorf("%s: the cloud side cannot read its record", cloud.ID)
		}
	}
	if !found {
		t.Errorf("%s: no record holds the answer", cloud.ID)
	}
}

func TestScoreAcceptsTheAnswer(t *testing.T) {
	for _, it := range pilot(t) {
		for _, reply := range []string{
			it.Answer,
			strings.ToLower(it.Answer),
			"  " + it.Answer + "  ",
			"\n\t" + it.Answer + "\r\n",
		} {
			if !Score(it, reply) {
				t.Errorf("%s: Score rejected a right reply", it.ID)
			}
		}
	}
}

// TestForgetfulRepliesScoreFalse runs what an assistant that forgot, guessed or padded
// would say. Every near miss in the item's own text counts as wrong.
func TestForgetfulRepliesScoreFalse(t *testing.T) {
	for _, it := range pilot(t) {
		wrong := []string{"", " ", "\n\t", "N/A", "none", "I don't know", "unknown code",
			it.Answer + "0", it.Answer + ".", it.Answer + " or ABC-1234",
			"The answer is " + it.Answer, "**" + it.Answer + "**", "`" + it.Answer + "`",
			it.Answer[:len(it.Answer)-1], "0" + it.Answer, it.Answer + " " + it.Answer,
		}
		if it.Answer != Unknown {
			wrong = append(wrong, Unknown, "unknown", " Unknown ")
		}
		if testCodeRe.MatchString(it.Answer) {
			wrong = append(wrong, "0", "42", strings.ReplaceAll(it.Answer, "-", " "), strings.ReplaceAll(it.Answer, "-", ""))
		}
		seen := map[string]bool{}
		for _, text := range allFacts(it) {
			for _, code := range testCodeRe.FindAllString(text, -1) {
				if code != it.Answer && !seen[code] {
					seen[code] = true
					wrong = append(wrong, code)
				}
			}
		}
		for _, reply := range wrong {
			if Score(it, reply) {
				t.Errorf("%s: Score accepted the wrong reply %q", it.ID, reply)
			}
		}
	}
}

func TestScoreEdges(t *testing.T) {
	if Score(Item{}, "") {
		t.Error("an item with no answer accepted an empty reply")
	}
	if Score(Item{Answer: ""}, "anything") {
		t.Error("an item with no answer accepted a reply")
	}
	// Unicode case folding would let these lookalikes pass.
	if Score(Item{Answer: "K7P-2941"}, string(rune(0x212A))+"7P-2941") {
		t.Error("a Kelvin sign passed for K")
	}
	if Score(Item{Answer: "SAB-2941"}, string(rune(0x017F))+"AB-2941") {
		t.Error("a long s passed for S")
	}
	if !Score(Item{Answer: "42"}, " 42 ") {
		t.Error("a right integer failed")
	}
	if Score(Item{Answer: "42"}, "042") || Score(Item{Answer: "42"}, "42.0") || Score(Item{Answer: "42"}, "+42") {
		t.Error("a differently written integer passed")
	}
	if !Score(Item{Answer: Unknown}, "unknown") {
		t.Error("a right UNKNOWN failed")
	}
}

// TestAnswerDomains covers each kind of answer and shows the UNKNOWN case exists.
func TestAnswerDomains(t *testing.T) {
	var codes, integers, unknown int
	for _, it := range pilot(t) {
		switch {
		case it.Answer == Unknown:
			unknown++
			if it.Kind != KindFollowup {
				t.Errorf("%s of kind %s answers %s", it.ID, it.Kind, Unknown)
			}
		case bareIntegers.MatchString(it.Answer):
			integers++
		default:
			codes++
		}
	}
	if codes == 0 || integers == 0 || unknown == 0 {
		t.Errorf("answers: %d codes, %d integers, %d %s", codes, integers, unknown, Unknown)
	}
}

// TestReasoningFamilies makes sure every family is in use, across enough draws that
// the independent solvers meet a real variety.
func TestReasoningFamilies(t *testing.T) {
	prefixes := []string{"Timer A", "Six steps", "Jobs and hours", "Hosts,", "Tasks "}
	counts := map[string]int{}
	for _, it := range bigAcceptance(t) {
		if it.Kind != KindReasoning {
			continue
		}
		for _, pre := range prefixes {
			if strings.HasPrefix(it.Query, pre) {
				counts[pre]++
			}
		}
	}
	for _, pre := range prefixes {
		if counts[pre] < 6 {
			t.Errorf("family %q has %d items", pre, counts[pre])
		}
	}
}

// TestSolversAgreeOnManyDraws drives both solvers far past what a set holds.
func TestSolversAgreeOnManyDraws(t *testing.T) {
	g := newGen(424242)
	for f := famTimers; f <= famPriority; f++ {
		for range 150 {
			q, a := g.reasoning(f)
			solved, err := solveReasoning(q)
			if err != nil {
				t.Fatalf("family %d: %v", f, err)
			}
			if solved != a {
				t.Fatalf("family %d: the solvers disagree on %q", f, q)
			}
			if strings.Contains(q, a) {
				t.Fatalf("family %d: query %q holds its answer", f, q)
			}
		}
	}
}

// TestVocabulary guards the label matching every other test relies on.
func TestVocabulary(t *testing.T) {
	check := func(name string, list []string) {
		if len(list) != 60 {
			t.Errorf("%s has %d entries", name, len(list))
		}
		for i, a := range list {
			for j, b := range list {
				if i != j && strings.Contains(a, b) {
					t.Errorf("%s: %q contains %q, so labels could be confused", name, a, b)
				}
			}
			if strings.ToLower(a) != a {
				t.Errorf("%s: %q is not lower case", name, a)
			}
		}
	}
	check("kinds", kinds)
	check("subjects", subjects)
}

// TestTargetLabelsDisjoint checks the label deal directly, past any realistic size.
func TestTargetLabelsDisjoint(t *testing.T) {
	seen := map[[2]int]string{}
	for _, set := range []string{SetTuning, SetAcceptance} {
		for n := range labelsPerSet {
			k, s := targetLabel(set, n)
			key := [2]int{k, s}
			if prev, dup := seen[key]; dup {
				t.Fatalf("label %v dealt to %s and %s/%d", key, prev, set, n)
			}
			seen[key] = fmt.Sprintf("%s/%d", set, n)
		}
	}
}

func TestEstimateTokens(t *testing.T) {
	for _, c := range []struct {
		in   string
		want int
	}{
		{"", 0},
		{"abc", 1},
		{"abcdefgh", 3},
		{"K7P-2941", 5},
		{"abcdefgh\nK7P-2941\n", 3 + 5},
		{"see K7P-2941 now", 9},
		{"K7P-294", 2},
		{"node-1234 and more", 5},
	} {
		if got := EstimateTokens(c.in); got != c.want {
			t.Errorf("EstimateTokens(%q) = %d, want %d", c.in, got, c.want)
		}
	}
}

// corruption mutates a clone and names the check that must fire.
type corruption struct {
	name   string
	kind   Kind
	want   string
	mutate func(it *Item)
}

func TestValidatorCatchesCorruption(t *testing.T) {
	lastUser := func(it *Item, extra string) {
		it.History[len(it.History)-1].User += " " + extra
	}
	cases := []corruption{
		{"cloud answer published recently", KindCloudContext, "location", func(it *Item) { lastUser(it, it.Answer) }},
		{"cloud answer in an older reply", KindCloudContext, "location", func(it *Item) { it.History[0].Reply += " " + it.Answer }},
		{"cloud answer in a local turn", KindCloudContext, "location", func(it *Item) { it.LocalOwn[0].User += " " + it.Answer }},
		{"cloud answer missing from its record", KindCloudContext, "location", func(it *Item) {
			for id, text := range it.Records {
				it.Records[id] = strings.ReplaceAll(text, it.Answer, "XXX-0000")
			}
			for i := range it.History {
				it.History[i].ToolResult = strings.ReplaceAll(it.History[i].ToolResult, it.Answer, "XXX-0000")
			}
		}},
		{"followup answer gone from recent text", KindFollowup, "location", func(it *Item) {
			for i := range it.History {
				it.History[i].User = strings.ReplaceAll(it.History[i].User, it.Answer, "XXX-0000")
				it.History[i].Reply = strings.ReplaceAll(it.History[i].Reply, it.Answer, "XXX-0000")
			}
		}},
		{"followup answer in an older step", KindFollowup, "location", func(it *Item) { it.History[0].User += " " + it.Answer }},
		{"local answer in the cloud history", KindLocalTool, "location", func(it *Item) { lastUser(it, it.Answer) }},
		{"local answer in a local turn", KindLocalTool, "location", func(it *Item) { it.LocalOwn[0].Reply += " " + it.Answer }},
		{"local answer in a cloud record", KindLocalTool, "location", func(it *Item) {
			for id := range it.Records {
				if strings.HasPrefix(id, "R-") {
					it.Records[id] += it.Answer
					return
				}
			}
		}},
		{"edge answer in the cloud history", KindEdgePrivate, "location", func(it *Item) { it.History[0].Reply += " " + it.Answer }},
		{"edge disclosure dropped", KindEdgePrivate, "shape", func(it *Item) { it.Disclosure = corpus.Shared }},
		{"followup turned edge only", KindFollowup, "shape", func(it *Item) { it.Disclosure = corpus.EdgeOnly }},
		{"reasoning answer wrong", KindReasoning, "reasoning", func(it *Item) { it.Answer += "1" }},
		{"query holds its answer", KindFollowup, "query", func(it *Item) { it.Query += " " + it.Answer }},
		{"query holds a kind word", KindCloudContext, "query", func(it *Item) { it.Query += " cloud_context" }},
		{"query holds another spelling", KindFollowup, "query", func(it *Item) { it.Query += " Follow-up" }},
		{"history too short", KindFollowup, "size", func(it *Item) { it.History = it.History[len(it.History)-5:] }},
		{"local turns too long", KindLocalTool, "size", func(it *Item) { it.LocalOwn[0].Reply += strings.Repeat("noise line\n", 1000) }},
		{"record too long", KindReasoning, "size", func(it *Item) {
			for id := range it.Records {
				it.Records[id] += strings.Repeat("noise line\n", 3000)
				return
			}
		}},
		{"tool result differs from record", KindReasoning, "step", func(it *Item) {
			for i := range it.History {
				if it.History[i].RecordID != "" {
					it.History[i].ToolResult += "x"
					return
				}
			}
		}},
		{"wrong tool name on the cloud side", KindReasoning, "step", func(it *Item) {
			for i := range it.History {
				if it.History[i].RecordID != "" {
					it.History[i].ToolName = "read_local"
					return
				}
			}
		}},
		{"a record published", KindReasoning, "published", func(it *Item) {
			for _, st := range it.History {
				if code := testCodeRe.FindString(st.ToolResult); code != "" {
					it.History[0].Reply += " " + code
					return
				}
			}
		}},
		{"carriage return", KindReasoning, "shape", func(it *Item) { it.History[0].User += "\r\n" }},
		{"own system prompt", KindCloudContext, "shape", func(it *Item) { it.System = "Answer from older records." }},
	}
	items := pilot(t)
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			// A local_tool query always names a record. For the other kinds take a plain
			// form, whose answer sits in one place only.
			it := clone(t, firstOf(t, items, func(x Item) bool {
				return x.Kind == c.kind && x.Answer != Unknown && (c.kind == KindLocalTool || !mentionsRecord(x.Query))
			}))
			if got := validateItem(it); len(got) != 0 {
				t.Fatalf("clean item already fails: %v", got)
			}
			c.mutate(&it)
			if got := validateItem(it); !got.has(c.want) {
				t.Errorf("no %q problem from %q: %v", c.want, c.name, got)
			}
		})
	}
}

func TestPairCheckCatchesCorruption(t *testing.T) {
	var a, b Item
	for _, it := range pilot(t) {
		if it.PairID != "" && !mentionsRecord(it.Query) {
			for _, other := range pilot(t) {
				if other.PairID == it.PairID && other.ID != it.ID {
					a, b = it, other
				}
			}
			break
		}
	}
	if a.ID == "" {
		t.Fatal("no plain pair found")
	}
	var clean problems
	checkPair(&clean, a, b)
	if len(clean) != 0 {
		t.Fatalf("clean pair fails: %v", clean)
	}
	follow := func(x, y *Item) *Item {
		if x.Kind == KindFollowup {
			return x
		}
		return y
	}
	cloud := func(x, y *Item) *Item {
		if x.Kind == KindCloudContext {
			return x
		}
		return y
	}
	cases := []struct {
		name   string
		mutate func(x, y *Item)
	}{
		{"one word differs", func(x, y *Item) { y.Query += " please" }},
		{"answers differ", func(x, y *Item) { y.Answer = "XXX-0000" }},
		{"owners differ", func(x, y *Item) {
			if x.Owner == corpus.SideCloud {
				x.Owner = corpus.SideLocal
			} else {
				x.Owner = corpus.SideCloud
			}
		}},
		{"local histories differ", func(x, y *Item) { x.LocalOwn[0].Reply += "." }},
		{"one history longer", func(x, y *Item) { x.History = x.History[1:] }},
		{"followup half no longer recent", func(x, y *Item) {
			f := follow(x, y)
			for i := range f.History {
				f.History[i].User = strings.ReplaceAll(f.History[i].User, f.Answer, "XXX-0000")
				f.History[i].Reply = strings.ReplaceAll(f.History[i].Reply, f.Answer, "XXX-0000")
			}
		}},
		{"cloud half published too", func(x, y *Item) {
			c := cloud(x, y)
			c.History[len(c.History)-1].Reply += " " + c.Answer
		}},
		{"followup half in a tool result too", func(x, y *Item) {
			f := follow(x, y)
			for i := range f.History {
				if f.History[i].RecordID != "" {
					f.History[i].ToolResult += f.Answer
					return
				}
			}
		}},
		{"different code counts in recent text", func(x, y *Item) {
			x.History[len(x.History)-1].Reply += " ZZZ-9999"
		}},
		{"cloud half loses its near misses", func(x, y *Item) {
			kind, _, _ := queryLabel(x.Query)
			c := cloud(x, y)
			for i := len(c.History) - RecentSteps; i < len(c.History); i++ {
				c.History[i].User = strings.ReplaceAll(strings.ToLower(c.History[i].User), kind, "thing")
				c.History[i].Reply = strings.ReplaceAll(strings.ToLower(c.History[i].Reply), kind, "thing")
			}
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			x, y := clone(t, a), clone(t, b)
			c.mutate(&x, &y)
			var got problems
			checkPair(&got, x, y)
			if !got.has("pair") {
				t.Errorf("no pair problem from %q", c.name)
			}
		})
	}
}

func TestSetCheckCatchesCorruption(t *testing.T) {
	items := make([]Item, 0, 8)
	for _, it := range pilot(t)[:8] {
		items = append(items, clone(t, it))
	}
	var clean problems
	checkSet(&clean, items[:0])
	if len(clean) != 0 {
		t.Fatalf("empty set fails: %v", clean)
	}

	dup := slices.Clone(items)
	dup[1].ID = dup[0].ID
	var p problems
	checkSet(&p, dup)
	if !p.has("set") {
		t.Error("duplicate ids passed")
	}

	reuse := slices.Clone(items)
	reuse[1].Query = reuse[0].Query
	reuse[1].PairID, reuse[0].PairID = "", ""
	p = nil
	checkSet(&p, reuse)
	if !p.has("set") {
		t.Error("a reused query outside a pair passed")
	}

	half := slices.Clone(items)
	half[0].PairID = "pair-lone"
	half[0].Kind = KindFollowup
	p = nil
	checkSet(&p, half)
	if !p.has("pair") {
		t.Error("half a pair passed")
	}
}

// TestReportStats logs the measured shape of the corpus, for the frozen report. Run
// with -v.
func TestReportStats(t *testing.T) {
	describe := func(name string, items []Item) {
		var hist, records, local, recent, query []int
		var flatHist []int
		pairs := map[string]bool{}
		for _, it := range items {
			hist = append(hist, historyTokens(it))
			flat := 0
			for _, st := range it.History {
				flat += (len(st.User) + len(st.ToolResult) + len(st.Reply)) / 4
			}
			flatHist = append(flatHist, flat)
			local = append(local, stepsTokens(it.LocalOwn))
			recent = append(recent, EstimateTokens(PublishedRecent(it)))
			query = append(query, len(it.Query))
			for _, text := range it.Records {
				records = append(records, EstimateTokens(text))
			}
			if it.PairID != "" {
				pairs[it.PairID] = true
			}
		}
		for _, s := range [][]int{hist, flatHist, records, local, recent, query} {
			sort.Ints(s)
		}
		rng := func(s []int) string { return fmt.Sprintf("%d..%d (median %d)", s[0], s[len(s)-1], s[len(s)/2]) }
		t.Logf("%s: %d items, kinds %v, pairs %d (%.1f%% of items)", name, len(items), countKinds(items), len(pairs), 100*pairShare(items))
		t.Logf("  history tokens %s; at a flat 4 chars %s", rng(hist), rng(flatHist))
		t.Logf("  record tokens %s over %d records", rng(records), len(records))
		t.Logf("  local turn tokens %s", rng(local))
		t.Logf("  recent published tokens %s", rng(recent))
		t.Logf("  query chars %s", rng(query))
	}
	describe("Pilot", pilot(t))
	describe("Acceptance(180)", bigAcceptance(t))
	t.Logf("Hash %s", Hash())
}
