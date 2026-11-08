package arms

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/agentscope-ai/agentscope-go/v2/experiments/stage0/corpus"
	"github.com/agentscope-ai/agentscope-go/v2/experiments/stage0/lab"
	"github.com/agentscope-ai/agentscope-go/v2/experiments/stage0/s3"
	"github.com/agentscope-ai/agentscope-go/v2/pkg/agentscope/classifier"
)

// stubClassifier records what it was asked and always picks one side.
type stubClassifier struct {
	mu     sync.Mutex
	states []string
	ops    []string
	choice string
	err    error
	// transient fails this many calls with a rate limit before behaving as set.
	transient int
}

func (s *stubClassifier) Classify(_ context.Context, r classifier.Request) (*classifier.Response, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.states = append(s.states, string(r.State))
	s.ops = append(s.ops, r.OperationID)
	in, out := int64(1000), int64(0)
	resp := &classifier.Response{Model: "stub", Attempts: []classifier.Attempt{{Kind: classifier.PhysicalAttempt, Usage: &classifier.Usage{InputTokens: &in, OutputTokens: &out}}}}
	if s.transient > 0 {
		s.transient--
		return resp, errors.New("HTTP 429: rate limit exceeded")
	}
	if s.err != nil {
		return resp, s.err
	}
	resp.Answers = map[string]classifier.Answer{"route": &classifier.ChoiceAnswer{
		Choice: s.choice, Confidence: 0.9, Probabilities: map[string]float64{s.choice: 0.9},
	}}
	return resp, nil
}

func routeItemOf(t *testing.T, kind s3.Kind) s3.Item {
	t.Helper()
	for _, it := range s3.Pilot() {
		if it.Kind == kind {
			return it
		}
	}
	t.Fatalf("no item of kind %s", kind)
	return s3.Item{}
}

func loadRoute(f *fake, it s3.Item) {
	sc := &fakeScript{}
	for _, st := range it.History {
		sc.turns = append(sc.turns, scriptTurn{User: st.User, Tool: st.ToolName, RecordID: st.RecordID, Reply: st.Reply})
	}
	f.scripts[it.ID] = sc
}

func runRoute(t *testing.T, f *fake, env *Env, it s3.Item) Row {
	t.Helper()
	loadRoute(f, it)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	rows, err := env.Exec(ctx, Job{Arm: ArmRoute, Route: &it})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Route == nil {
		t.Fatalf("rows = %+v", rows)
	}
	return rows[0]
}

func TestForkStepFindsTheLastCrossing(t *testing.T) {
	step := func(n int) corpus.CStep { return corpus.CStep{User: strings.Repeat("x", n*4-1)} }
	steps := []corpus.CStep{step(4000), step(2000), step(3000), step(500), step(2000)}
	// Totals: 4000, 6000, 9000, 9500, 11500.
	if got := forkStep(steps, 5000); got != 4 {
		t.Errorf("refresh 5000: %d", got)
	}
	if got := forkStep(steps, 10000); got != 4 {
		t.Errorf("refresh 10000: %d", got)
	}
	if got := forkStep(steps, 20000); got != -1 {
		t.Errorf("refresh 20000: %d", got)
	}
	if got := tokensAfter(steps, 1); got != 5500 {
		t.Errorf("tokens after: %d", got)
	}
}

func TestRouteRunsBothSidesAndScoresEachWorld(t *testing.T) {
	f := newFake(t, modeOracle, Window8K)
	it := routeItemOf(t, s3.KindReasoning)
	f.routeReply = func(side, last string) (string, bool) {
		if !strings.Contains(last, it.Query) {
			return "", false
		}
		if side == lab.SideCloud {
			return it.Answer, true
		}
		return "wrong", true
	}
	env := f.env(0)
	stub := &stubClassifier{choice: "cloud"}
	env.Classifiers = []NamedClassifier{{Name: "stub", C: stub, Cloud: true, Prices: lab.Luna}}
	row := runRoute(t, f, env, it)
	out := row.Route
	if row.Error != "" {
		t.Fatalf("error: %s", row.Error)
	}
	if !out.Cloud.Correct {
		t.Errorf("cloud = %+v", out.Cloud)
	}
	for _, world := range []string{WorldFresh, WorldStale, WorldNone} {
		w := out.Worlds[world]
		if w == nil || w.Local.Correct || w.Local.Reply != "wrong" {
			t.Errorf("%s = %+v", world, w)
		}
	}
	if !out.Worlds[WorldFresh].HasSummary || out.Worlds[WorldNone].HasSummary {
		t.Errorf("summaries: fresh %v, none %v", out.Worlds[WorldFresh].HasSummary, out.Worlds[WorldNone].HasSummary)
	}
	if out.Worlds[WorldFresh].BehindTokens >= refreshFresh {
		t.Errorf("a fresh summary is %d tokens behind", out.Worlds[WorldFresh].BehindTokens)
	}
	if len(out.Decisions) != 4 || len(stub.ops) != 4 {
		t.Errorf("decisions = %d, calls = %d: want R2 once and R3 per world", len(out.Decisions), len(stub.ops))
	}
	if out.Rules[RouterOwner] != string(it.Owner) {
		t.Errorf("rules = %v", out.Rules)
	}
}

func TestRouteWorldsSeeOnlyTheirOwnSummary(t *testing.T) {
	f := newFake(t, modeOracle, Window8K)
	it := routeItemOf(t, s3.KindFollowup)
	f.forkText = func(n int) string { return strings.Repeat("S", 1) + "UMMARY-" + string(rune('0'+n)) }
	f.routeReply = func(_, last string) (string, bool) { return it.Answer, strings.Contains(last, it.Query) }
	env := f.env(0)
	row := runRoute(t, f, env, it)
	if row.Error != "" {
		t.Fatal(row.Error)
	}
	var fresh, stale, none int
	for _, c := range f.captures() {
		if c.Side != lab.SideLocal || c.Kind != kindProbe {
			continue
		}
		has1, has2 := bytes.Contains(c.Raw, []byte("SUMMARY-1")), bytes.Contains(c.Raw, []byte("SUMMARY-2"))
		switch {
		case has1 && has2:
			t.Error("a world holds two summaries")
		case has2:
			fresh++
		case has1:
			stale++
		default:
			none++
		}
	}
	if none != 1 {
		t.Errorf("local requests with no summary = %d", none)
	}
	if fresh+stale != 2 {
		t.Errorf("local requests with a summary = %d (fresh %d, stale %d)", fresh+stale, fresh, stale)
	}
}

func TestWorldLogHidesOtherWorldsSummaries(t *testing.T) {
	r := &routeRun{log: &threadLog{}, peers: map[string]*summary{}}
	r.log.add(kindSummary, "cloud", corpus.Shared, "OLD")
	r.log.add(kindUser, "cloud", corpus.Shared, "hello")
	r.log.add(kindSummary, "cloud", corpus.Shared, "NEW")
	r.peers[WorldStale] = &summary{Text: "OLD"}
	r.peers[WorldFresh] = &summary{Text: "NEW"}
	for world, want := range map[string][2]bool{WorldStale: {true, false}, WorldFresh: {false, true}, WorldNone: {false, false}} {
		got := r.worldLog(world)
		if (got.entries[0].Text == "OLD") != want[0] || (got.entries[2].Text == "NEW") != want[1] {
			t.Errorf("%s: %+v", world, got.entries)
		}
		if got.entries[1].Text != "hello" {
			t.Errorf("%s: a published entry changed", world)
		}
	}
	if r.log.entries[0].Text != "OLD" || r.log.entries[2].Text != "NEW" {
		t.Error("worldLog changed the log")
	}
}

func TestEdgeOnlyQueryReachesNeitherCloudNorClassifier(t *testing.T) {
	f := newFake(t, modeOracle, Window8K)
	it := routeItemOf(t, s3.KindEdgePrivate)
	f.routeReply = func(_, last string) (string, bool) { return it.Answer, strings.Contains(last, it.Query) }
	env := f.env(0)
	stub := &stubClassifier{choice: "cloud"}
	env.Classifiers = []NamedClassifier{{Name: "stub", C: stub, Cloud: true, Prices: lab.Luna}}
	row := runRoute(t, f, env, it)
	out := row.Route
	if out.CloudEligible || out.Cloud.Correct {
		t.Errorf("cloud side = %+v, eligible %v", out.Cloud, out.CloudEligible)
	}
	if len(stub.ops) != 0 {
		t.Errorf("a classifier saw an edge_only query: %v", stub.ops)
	}
	for _, name := range []string{RouterOwner, RouterRules, RouterRulesS} {
		if out.Rules[name] != string(corpus.SideLocal) {
			t.Errorf("%s = %s", name, out.Rules[name])
		}
	}
	needleText := needle(it.Query)
	for _, c := range f.captures() {
		if c.Side == lab.SideCloud && bytes.Contains(c.Raw, needleText) {
			t.Fatal("the query reached the cloud")
		}
	}
	if row.CanaryHits != 0 {
		t.Errorf("canary hits = %d", row.CanaryHits)
	}
	if !out.Worlds[WorldFresh].Local.Correct {
		t.Errorf("local = %+v", out.Worlds[WorldFresh].Local)
	}
}

func TestRouteStateHoldsNoHostFieldAndNoLocalData(t *testing.T) {
	f := newFake(t, modeOracle, Window8K)
	// A local_tool item has an L- record that is the answer's home.
	it := routeItemOf(t, s3.KindLocalTool)
	f.routeReply = func(_, last string) (string, bool) { return it.Answer, strings.Contains(last, it.Query) }
	env := f.env(0)
	stub := &stubClassifier{choice: "local"}
	env.Classifiers = []NamedClassifier{{Name: "stub", C: stub, Cloud: true, Prices: lab.Luna}}
	runRoute(t, f, env, it)
	if len(stub.states) != 4 {
		t.Fatalf("states = %d", len(stub.states))
	}
	forbidden := []string{it.Answer, string(it.Kind), it.ID, `"seed"`, `"kind"`, `"answer"`, `"pair`}
	for _, code := range routeCanaries(it) {
		forbidden = append(forbidden, code)
	}
	for _, state := range stub.states {
		for _, bad := range forbidden {
			if strings.Contains(state, bad) {
				t.Errorf("the state holds %q", bad)
			}
		}
		if !strings.Contains(state, `"query"`) {
			t.Errorf("state = %.100s", state)
		}
	}
	if strings.Contains(stub.states[0], `"session"`) {
		t.Error("R2 saw the session")
	}
	if !strings.Contains(stub.states[1], `"recent"`) || !strings.Contains(stub.states[1], `"owner"`) {
		t.Errorf("R3 state = %.200s", stub.states[1])
	}
}

func TestRouteClassifierFailureIsRecordedAndCharged(t *testing.T) {
	f := newFake(t, modeOracle, Window8K)
	it := routeItemOf(t, s3.KindFollowup)
	f.routeReply = func(_, last string) (string, bool) { return it.Answer, strings.Contains(last, it.Query) }
	env := f.env(0)
	stub := &stubClassifier{err: errors.New("boom sk-secret-value")}
	env.Classifiers = []NamedClassifier{{Name: "stub", C: stub, Cloud: true, Prices: lab.Luna}}
	row := runRoute(t, f, env, it)
	if row.Error != "" {
		t.Fatalf("a failed classifier ended the item: %s", row.Error)
	}
	for _, d := range row.Route.Decisions {
		if d.Error == "" || d.Choice != "" || strings.Contains(d.Error, "sk-secret") {
			t.Errorf("decision = %+v", d)
		}
	}
	if len(stub.ops) != 4 {
		t.Errorf("a permanent failure was tried again: %d calls for 4 decisions", len(stub.ops))
	}
	charged := 0.0
	for _, lr := range env.Ledger.Rows() {
		if lr.Arm == "cls:stub" {
			charged += lr.CostUSD
			if lr.Role != "classifier" || lr.Side != lab.SideCloud {
				t.Errorf("row = %+v", lr)
			}
		}
	}
	if charged <= 0 {
		t.Error("the classifier's usage was not charged")
	}
}

func TestRouteRulesAndLabels(t *testing.T) {
	item := func(query string, owner corpus.Side, d corpus.Disclosure, recent string) s3.Item {
		return s3.Item{Query: query, Owner: owner, Disclosure: d, History: []corpus.CStep{{User: recent, Reply: "ok"}}}
	}
	cases := []struct {
		name        string
		it          s3.Item
		r0, r1, r1s corpus.Side
	}{
		{"edge only", item("anything", corpus.SideCloud, corpus.EdgeOnly, ""), corpus.SideLocal, corpus.SideLocal, corpus.SideLocal},
		{"local id", item("read L-123 for me", corpus.SideCloud, corpus.Shared, ""), corpus.SideCloud, corpus.SideLocal, corpus.SideLocal},
		{"cloud id", item("read R-123 for me", corpus.SideLocal, corpus.Shared, ""), corpus.SideLocal, corpus.SideCloud, corpus.SideCloud},
		{"plain query", item("what is it", corpus.SideCloud, corpus.Shared, "nothing"), corpus.SideCloud, corpus.SideCloud, corpus.SideCloud},
		{"the literal rule ignores the owner", item("what is it", corpus.SideLocal, corpus.Shared, "nothing"), corpus.SideLocal, corpus.SideLocal, corpus.SideCloud},
		{"label as a sentence", item("what is the restore point for the ledger service?", corpus.SideCloud, corpus.Shared, "Note for the review, the restore point for the ledger service is AAA-1111."), corpus.SideCloud, corpus.SideCloud, corpus.SideLocal},
		{"label beats a named record", item("what restore point does record R-123 give for the ledger service?", corpus.SideCloud, corpus.Shared, "Restore point for the ledger service: AAA-1111."), corpus.SideCloud, corpus.SideCloud, corpus.SideLocal},
		{"revoked label", item("give me the restore point for the ledger service", corpus.SideCloud, corpus.Shared, "The restore point for the ledger service was revoked and nothing has replaced it."), corpus.SideCloud, corpus.SideCloud, corpus.SideLocal},
		{"label with no value", item("what is the restore point for the ledger service?", corpus.SideCloud, corpus.Shared, "Which restore point for the ledger service do we hold?"), corpus.SideCloud, corpus.SideCloud, corpus.SideCloud},
		{"label in recent text", item("what is the restore point for the ledger service?", corpus.SideCloud, corpus.Shared, "Restore point for the ledger service: AAA-1111."), corpus.SideCloud, corpus.SideCloud, corpus.SideLocal},
		{"near miss", item("what is the restore point for the ledger service?", corpus.SideCloud, corpus.Shared, "Restore point for the batch runner: AAA-1111.\nKey rotation ticket for the ledger service: BBB-2222."), corpus.SideCloud, corpus.SideCloud, corpus.SideCloud},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := routeRules(c.it)
			if got[RouterOwner] != string(c.r0) || got[RouterRules] != string(c.r1) || got[RouterRulesS] != string(c.r1s) {
				t.Errorf("rules = %v", got)
			}
		})
	}
}

func TestBudgetImportsKeepsTheNewest(t *testing.T) {
	f := newFake(t, modeOracle, Window8K)
	env := f.env(0)
	r := &routeRun{env: env}
	log := &threadLog{}
	for _, text := range []string{"oldest", "middle", "newest"} {
		log.add(kindUser, "cloud", corpus.Shared, text+" "+strings.Repeat("x", routeImportBudget*4/3))
	}
	msgs := log.imports(0, corpus.SideLocal)
	kept, dropped := r.budgetImports(context.Background(), msgs)
	if dropped != 1 || len(kept) != 2 {
		t.Fatalf("kept %d, dropped %d", len(kept), dropped)
	}
	txt := kept[len(kept)-1].GetTextContent("")
	if txt == nil || !strings.Contains(*txt, "newest") {
		t.Errorf("last kept = %v", txt)
	}
}

func TestRouteRowHasNoRetention(t *testing.T) {
	raw, err := json.Marshal(Row{Arm: ArmRoute, Session: "x", Route: &RouteOutcome{Kind: "followup"}})
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if v, ok := got["retention"]; !ok || v != nil {
		t.Errorf("retention = %v", v)
	}
	if got["route"] == nil {
		t.Errorf("row lost its route: %s", raw)
	}
}

// designedRoute is S3's own statement of where each kind belongs.
var designedRoute = map[s3.Kind]corpus.Side{
	s3.KindFollowup:     corpus.SideLocal,
	s3.KindReasoning:    corpus.SideCloud,
	s3.KindLocalTool:    corpus.SideLocal,
	s3.KindCloudContext: corpus.SideCloud,
	s3.KindEdgePrivate:  corpus.SideLocal,
}

// R1s exists to show what a literal rule can do on this corpus, which the author measured
// at 100%. A lower number here would understate that and flatter R3.
func TestLiteralRuleMatchesTheDesignedRoutes(t *testing.T) {
	for name, items := range map[string][]s3.Item{"pilot": s3.Pilot(), "acceptance": s3.Acceptance(180)} {
		for _, it := range items {
			want, ok := designedRoute[it.Kind]
			if !ok {
				t.Fatalf("%s: kind %s has no designed route", name, it.Kind)
			}
			if got := routeRules(it)[RouterRulesS]; got != string(want) {
				t.Errorf("%s %s (%s): R1s = %s, want %s\nquery: %s", name, it.ID, it.Kind, got, want, it.Query)
			}
		}
	}
}

func TestHostFieldsReachNoModelAndNoClassifier(t *testing.T) {
	for _, kind := range []s3.Kind{s3.KindFollowup, s3.KindLocalTool, s3.KindReasoning} {
		t.Run(string(kind), func(t *testing.T) {
			f := newFake(t, modeOracle, Window8K)
			it := routeItemOf(t, kind)
			f.routeReply = func(_, last string) (string, bool) { return it.Answer, strings.Contains(last, it.Query) }
			env := f.env(0)
			stub := &stubClassifier{choice: "local"}
			env.Classifiers = []NamedClassifier{{Name: "stub", C: stub, Cloud: true, Prices: lab.Luna}}
			row := runRoute(t, f, env, it)
			if row.Error != "" {
				t.Fatal(row.Error)
			}
			host := []string{it.ID, fmt.Sprint(it.Seed)}
			if it.PairID != "" {
				host = append(host, it.PairID)
			}
			// "reasoning" is also an API field, so that one kind cannot be scanned for.
			if it.Kind != s3.KindReasoning {
				host = append(host, string(it.Kind))
			}
			seen := 0
			for _, c := range f.captures() {
				seen++
				for _, bad := range host {
					if bytes.Contains(c.Raw, []byte(bad)) {
						t.Errorf("a %s request holds %q", c.Kind, bad)
					}
				}
			}
			if seen == 0 || len(stub.ops) == 0 {
				t.Fatalf("captured %d requests and %d operations", seen, len(stub.ops))
			}
			for i, op := range stub.ops {
				for _, bad := range host {
					if strings.Contains(op, bad) || strings.Contains(stub.states[i], bad) {
						t.Errorf("classifier call %d holds %q: %s", i, bad, op)
					}
				}
			}
		})
	}
}

func TestRouteClassifierRetriesATransientFailure(t *testing.T) {
	old := routeClassifyBackoff
	routeClassifyBackoff = time.Millisecond
	defer func() { routeClassifyBackoff = old }()

	run := func(transient int) (Row, *stubClassifier, *Env) {
		f := newFake(t, modeOracle, Window8K)
		it := routeItemOf(t, s3.KindFollowup)
		f.routeReply = func(_, last string) (string, bool) { return it.Answer, strings.Contains(last, it.Query) }
		env := f.env(0)
		stub := &stubClassifier{choice: "local", transient: transient}
		env.Classifiers = []NamedClassifier{{Name: "stub", C: stub, Cloud: true, Prices: lab.Luna, Sem: make(chan struct{}, 1)}}
		return runRoute(t, f, env, it), stub, env
	}
	attempts := func(env *Env) (n int) {
		for _, lr := range env.Ledger.Rows() {
			if lr.Arm == "cls:stub" {
				n++
			}
		}
		return n
	}

	row, stub, env := run(2)
	if row.Route.Decisions[0].Error != "" || row.Route.Decisions[0].Choice != "local" {
		t.Errorf("recovered decision = %+v", row.Route.Decisions[0])
	}
	if len(stub.ops) != 6 || attempts(env) != 6 {
		t.Errorf("calls %d, charged %d, want 3 + 3 others", len(stub.ops), attempts(env))
	}

	row, stub, env = run(routeClassifyTries)
	if row.Route.Decisions[0].Error == "" || row.Route.Decisions[0].Choice != "" {
		t.Errorf("exhausted decision = %+v", row.Route.Decisions[0])
	}
	if len(stub.ops) != routeClassifyTries+3 || attempts(env) != routeClassifyTries+3 {
		t.Errorf("calls %d, charged %d", len(stub.ops), attempts(env))
	}
}

// Each world's R3 reads its own summary. Without this R3's three rows could all be the same
// decision, and the summary's freshness would look like it does nothing.
func TestR3StateHoldsTheWorldsSummary(t *testing.T) {
	f := newFake(t, modeOracle, Window8K)
	it := routeItemOf(t, s3.KindFollowup)
	f.forkText = func(n int) string { return "SUMMARY-" + string(rune('0'+n)) }
	f.routeReply = func(_, last string) (string, bool) { return it.Answer, strings.Contains(last, it.Query) }
	env := f.env(0)
	stub := &stubClassifier{choice: "local"}
	env.Classifiers = []NamedClassifier{{Name: "stub", C: stub, Cloud: true, Prices: lab.Luna}}
	row := runRoute(t, f, env, it)
	if row.Error != "" {
		t.Fatal(row.Error)
	}
	want := map[string][2]bool{WorldStale: {true, false}, WorldFresh: {false, true}, WorldNone: {false, false}}
	seen := 0
	for i, op := range stub.ops {
		for world, w := range want {
			if !strings.Contains(op, "/R3/"+world+"/") {
				continue
			}
			seen++
			if strings.Contains(stub.states[i], "SUMMARY-1") != w[0] || strings.Contains(stub.states[i], "SUMMARY-2") != w[1] {
				t.Errorf("%s state = %.300s", world, stub.states[i])
			}
		}
	}
	if seen != 3 {
		t.Errorf("R3 calls = %d", seen)
	}
}

// The second fork builds on the first summary, as a real refresh does. A fork that started
// over would make r5k and r20k differ by more than their age.
func TestForkChainGivesTheNextForkThePreviousSummary(t *testing.T) {
	f := newFake(t, modeOracle, Window8K)
	it := routeItemOf(t, s3.KindFollowup)
	f.forkText = func(n int) string { return "SUMMARY-" + string(rune('0'+n)) }
	f.routeReply = func(_, last string) (string, bool) { return it.Answer, strings.Contains(last, it.Query) }
	env := f.env(0)
	row := runRoute(t, f, env, it)
	if row.Error != "" {
		t.Fatal(row.Error)
	}
	forks := 0
	for _, c := range f.captures() {
		if c.Side != lab.SideCloud || c.Kind != kindFork {
			continue
		}
		forks++
		if holds := bytes.Contains(c.Raw, []byte("SUMMARY-1")); holds != (forks > 1) {
			t.Errorf("fork %d holds the first summary: %v", forks, holds)
		}
	}
	if forks != 2 {
		t.Errorf("forks = %d", forks)
	}
}

// A live reply could restate the record just read, and S3's tool results reach local only
// through a summary.
func TestForkStepLogsTheScriptedReply(t *testing.T) {
	f := newFake(t, modeOracle, Window8K)
	it := routeItemOf(t, s3.KindFollowup)
	const restated = "RESTATED-RECORD-TEXT"
	f.mainText = func() string { return restated }
	f.routeReply = func(_, last string) (string, bool) { return it.Answer, strings.Contains(last, it.Query) }
	env := f.env(0)
	stub := &stubClassifier{choice: "local"}
	env.Classifiers = []NamedClassifier{{Name: "stub", C: stub, Cloud: true, Prices: lab.Luna}}
	row := runRoute(t, f, env, it)
	if row.Error != "" {
		t.Fatal(row.Error)
	}
	cloud := 0
	for _, c := range f.captures() {
		if c.Side == lab.SideCloud && bytes.Contains(c.Raw, []byte(restated)) {
			cloud++
		}
		if c.Side == lab.SideLocal && bytes.Contains(c.Raw, []byte(restated)) {
			t.Fatalf("a live reply reached local: %.200s", c.Raw)
		}
	}
	if cloud == 0 {
		t.Fatal("the fake never restated, so this proves nothing")
	}
	for _, state := range stub.states {
		if strings.Contains(state, restated) {
			t.Fatalf("a live reply reached a classifier: %.200s", state)
		}
	}
}
