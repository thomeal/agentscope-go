package thread

import (
	"context"
	"errors"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/agentscope-ai/agentscope-go/v2/pkg/agentscope/model"
	"github.com/agentscope-ai/agentscope-go/v2/pkg/agentscope/types"
)

func forkEnv(t *testing.T, store Store, mutate func(*Config)) (*coEnv, *fakePart, *fakeForker) {
	t.Helper()
	edge := newFakePart("edge", ZoneEdge)
	cloud := newFakeForker("cloud")
	env := newCoEnv(t, store, func(cfg *Config) {
		cfg.Router = zoneRouter()
		cfg.Summaries.PeerEvery = 100
		if mutate != nil {
			mutate(cfg)
		}
	}, edge, cloud)
	return env, edge, cloud
}

// grow replies text after the participant's input reached tokens.
func grow(tokens int, text string) fakeScript {
	return func(r *fakeRun) *ActivationEnd {
		r.f.setState(func(s *ParticipantState) { s.InputTokens = tokens })
		return r.reply(text)
	}
}

func TestCoordinatorPeerForkAfterGrowth(t *testing.T) {
	store := NewMemoryStore(WithClock(fixedClock()))
	mustAppend(t, store, nil, Entry{Kind: KindArtifact, Visibility: EdgeOnly, Refs: []ArtifactRef{{URI: "file:///diary"}}})
	env, _, cloud := forkEnv(t, store, nil)
	cloud.push(grow(500, "plan ready"), grow(700, "more planning"))
	env.say("plan") // #2, owner_change #3, reply #4
	env.idle()

	sums := findEntries(env.log(), KindSummary, "")
	if len(sums) != 1 {
		t.Fatalf("summaries = %+v", sums)
	}
	s := sums[0]
	if s.Origin.Participant != "cloud" || s.Summary.Target != "edge" || s.Summary.Budget != 200 || s.Summary.Lossy ||
		s.Visibility != Shared || s.Text() != "peer summary text" || !strings.HasPrefix(s.DedupKey, "peer:") {
		t.Fatalf("summary = %+v %+v", s, s.Summary)
	}
	if s.Coverage.Through != 4 || len(s.Coverage.Excluded) != 2 || s.Coverage.Excluded[0] != 1 || s.Coverage.Excluded[1] != 3 {
		t.Fatalf("coverage = %+v", s.Coverage)
	}
	specs := cloud.forkSpecs()
	if len(specs) != 1 {
		t.Fatalf("%d forks", len(specs))
	}
	spec := specs[0]
	if !strings.Contains(spec.Prompt, "for edge,") || !strings.Contains(spec.Prompt, "8000-token") ||
		!strings.Contains(spec.Prompt, "under 200 tokens") || spec.MaxTokens != 512 || spec.MaxRewrites != 1 || spec.Appendix != "" {
		t.Fatalf("spec = %+v", spec)
	}
	if fb, ok := spec.Validate("short"); !ok || fb != "" {
		t.Fatalf("Validate(short) = %q %v", fb, ok)
	}
	if fb, ok := spec.Validate(strings.Repeat("x", 1204)); ok || fb != "Too long (301 tokens). Rewrite it under 200 tokens." {
		t.Fatalf("Validate(long) = %q %v", fb, ok)
	}
	var sawSummary bool
	for _, ev := range env.events() {
		sawSummary = sawSummary || (ev.Kind == EventEntry && ev.Entry.Kind == KindSummary)
	}
	if !sawSummary {
		t.Fatal("the summary was not shown")
	}

	env.say("more")
	env.idle()
	specs = cloud.forkSpecs()
	if len(specs) != 2 || specs[1].Appendix != "Previous summary for edge, update it:\npeer summary text" {
		t.Fatalf("second fork = %+v", specs)
	}
	tot := env.c.Ledger().TotalsByKind()[UsageForkPeer]
	if tot.Calls != 2 || tot.InputTokens != 20 || tot.UnknownCalls != 0 {
		t.Fatalf("fork ledger = %+v", tot)
	}
}

// A peer fork needs output sealed past the newest summary's coverage: growth
// from an activation that sealed nothing buys no new summary, and a
// background task's report counts as sealed output.
func TestCoordinatorPeerForkNeedsNewSealedOutput(t *testing.T) {
	env, edge, cloud := forkEnv(t, nil, func(cfg *Config) {
		cfg.Router = &fakeRouter{fn: func(_ context.Context, req *RouteRequest) (RouteDecision, error) {
			if strings.HasPrefix(req.Query, "edge:") {
				return RouteDecision{Target: "edge", Source: RouteRule}, nil
			}
			return RouteDecision{Target: "cloud", Source: RouteRule}, nil
		}}
	})
	cloud.push(grow(500, "plan ready"), grow(900, ""), grow(1500, "it opens at ten"))
	env.say("plan")
	env.idle()
	if n := len(cloud.forkSpecs()); n != 1 {
		t.Fatalf("forks after the first reply = %d", n)
	}

	env.say("again") // grows past PeerEvery, seals nothing
	env.idle()
	if n := len(cloud.forkSpecs()); n != 1 {
		t.Fatalf("an activation that sealed nothing forked again: %d forks", n)
	}

	edge.push(func(r *fakeRun) *ActivationEnd {
		if _, _, err := r.assign("cloud", "look up the opening hours", false); err != nil {
			t.Errorf("assign: %v", err)
		}
		return r.reply("asked the cloud")
	})
	env.say("edge: ask the cloud")
	env.idle()
	reps := findEntries(env.log(), KindReport, "")
	if len(reps) != 1 || reps[0].Origin.Participant != "cloud" {
		t.Fatalf("reports = %+v", reps)
	}
	sums := findEntries(env.log(), KindSummary, "")
	if len(sums) != 2 || sums[1].Coverage.Through != reps[0].Seq {
		t.Fatalf("summaries = %+v, want the second through the report #%d", sums, reps[0].Seq)
	}

	// The edge kept the foreground while the cloud's task ran: the summary
	// that task produced does not reset it, and the edge receives the
	// task's answer itself.
	edge.push(replyWith("it opens at ten, then"))
	env.say("edge: what did the cloud find?")
	env.idle()
	acts := edge.activations()
	last := acts[len(acts)-1].Sync
	if last == nil || last.Reset || last.PeerSummary != nil || !containsText(syncTexts(last), "it opens at ten") {
		t.Fatalf("the edge that kept the foreground was reset or missed the report: %+v %q", last, syncTexts(last))
	}
}

func TestCoordinatorOwnForkAppliedAfterPeer(t *testing.T) {
	env, _, cloud := forkEnv(t, nil, nil)
	cloud.setState(func(s *ParticipantState) { s.Window = 1000 })
	var summaryFirst bool
	cloud.beforeApply = func() { summaryFirst = len(findEntries(env.log(), KindSummary, "")) == 1 }
	cloud.push(grow(700, "big answer"))
	env.say("go")
	env.idle()
	if got := cloud.appliedOwn(); len(got) != 1 || got[0] != "own summary text" {
		t.Fatalf("applied = %v", got)
	}
	if !summaryFirst {
		t.Fatal("the own summary was applied before the peer summary was written")
	}
	specs := cloud.forkSpecs()
	var own *ForkSpec
	for i := range specs {
		if specs[i].Validate == nil {
			own = &specs[i]
		}
	}
	if own == nil || own.Prompt != DefaultOwnPrompt || own.MaxTokens != defaultOwnMaxTokens {
		t.Fatalf("own spec = %+v", own)
	}
	if tot := env.c.Ledger().TotalsByKind()[UsageForkOwn]; tot.Calls != 1 || tot.InputTokens != 20 {
		t.Fatalf("own fork ledger = %+v", tot)
	}
	env.c.mu.Lock()
	last := env.c.parts["cloud"].lastInput
	env.c.mu.Unlock()
	if last != 1000 {
		t.Fatalf("lastInput after the own summary = %d", last)
	}
}

func TestCoordinatorOwnSummaryWaitsForIdle(t *testing.T) {
	env, _, cloud := forkEnv(t, nil, nil)
	c := env.c
	ps := c.parts["cloud"]
	c.mu.Lock()
	ps.running = &activation{part: ps}
	c.mu.Unlock()
	c.finishOwn(&forkJob{ps: ps, snap: fakeSnap{}}, &ForkOutput{Text: "own text", Calls: 1}, nil)
	if got := cloud.appliedOwn(); len(got) != 0 {
		t.Fatalf("applied while busy: %v", got)
	}
	c.mu.Lock()
	pending := ps.pendingOwn != nil
	ps.running = nil
	c.mu.Unlock()
	if !pending {
		t.Fatal("the result was not kept for later")
	}
	c.afterEnd(&activation{part: ps}, types.ReplyError, true)
	if got := cloud.appliedOwn(); len(got) != 1 || got[0] != "own text" {
		t.Fatalf("applied = %v", got)
	}

	// A stale snapshot drops the summary.
	cloud.fmu.Lock()
	cloud.applyErr = ErrStale
	cloud.fmu.Unlock()
	cloud.setState(func(s *ParticipantState) { s.InputTokens = 5 })
	c.finishOwn(&forkJob{ps: ps, snap: fakeSnap{}}, &ForkOutput{Text: "stale text", Calls: 1}, nil)
	c.mu.Lock()
	last, maint, kept := ps.lastInput, ps.maint, ps.pendingOwn
	c.mu.Unlock()
	if last == 5 || maint || kept != nil {
		t.Fatalf("stale summary: lastInput %d maint %v pending %v", last, maint, kept)
	}
	// An error or an empty text applies nothing.
	c.finishOwn(&forkJob{ps: ps, snap: fakeSnap{}}, &ForkOutput{}, errors.New("fork failed"))
	c.finishOwn(&forkJob{ps: ps, snap: fakeSnap{}}, &ForkOutput{Text: "  "}, nil)
	if got := cloud.appliedOwn(); len(got) != 2 {
		t.Fatalf("applied = %v", got)
	}
	env.idle()
}

// TestCoordinatorOwnSummaryFinishedBeforeSettle covers an own summary that
// finished after afterEnd looked for one but before its activation settled:
// settle applies it, instead of leaving it for the end of the next
// activation while the Coordinator reports idle.
func TestCoordinatorOwnSummaryFinishedBeforeSettle(t *testing.T) {
	env, _, cloud := forkEnv(t, nil, nil)
	c := env.c
	ps := c.parts["cloud"]
	a := &activation{part: ps}
	c.mu.Lock()
	ps.running = a
	c.addWorkLocked() // the activation's unit, which settle releases
	c.mu.Unlock()
	c.finishOwn(&forkJob{ps: ps, snap: fakeSnap{}}, &ForkOutput{Text: "own text", Calls: 1}, nil)
	if got := cloud.appliedOwn(); len(got) != 0 {
		t.Fatalf("applied while running: %v", got)
	}
	c.settle(a)
	env.idle()
	if got := cloud.appliedOwn(); len(got) != 1 || got[0] != "own text" {
		t.Fatalf("applied = %v", got)
	}
	c.mu.Lock()
	maint, kept := ps.maint, ps.pendingOwn
	c.mu.Unlock()
	if maint || kept != nil {
		t.Fatalf("after settle: maint %v pending %v", maint, kept)
	}
}

// TestCoordinatorWakeAfterOwnSummary covers a Notify report that reaches
// the idle owner while its own summary is applied: it cannot wake the owner
// then, so ending the maintenance does.
func TestCoordinatorWakeAfterOwnSummary(t *testing.T) {
	env, edge, cloud := forkEnv(t, nil, nil)
	env.say("go")
	env.idle()
	c := env.c
	ps := c.parts["cloud"]
	if owner, _ := c.Owner(); owner != "cloud" {
		t.Fatalf("owner = %s", owner)
	}
	c.mu.Lock()
	ps.maint = true
	c.addWorkLocked()
	c.mu.Unlock()
	edge.mu.Lock()
	h := edge.host
	edge.mu.Unlock()
	rep, err := h.Report(context.Background(), ReportRequest{From: Origin{Participant: "edge"}, Text: "the scan finished", To: []string{"cloud"}, Notify: true})
	if err != nil {
		t.Fatal(err)
	}
	if n := len(cloud.activations()); n != 1 {
		t.Fatalf("woken during maintenance: %d activations", n)
	}
	c.endMaint(ps)
	env.idle()
	acts := cloud.activations()
	if len(acts) != 2 || acts[1].Trigger.Seq != rep || acts[1].Trigger.Kind != KindReport {
		t.Fatalf("the report did not wake the owner after maintenance: %+v", acts)
	}
}

func TestCoordinatorForkWithoutSnapshot(t *testing.T) {
	env, _, cloud := forkEnv(t, nil, nil)
	cloud.snapErrs = []error{ErrNoSnapshot, errors.New("broken")}
	cloud.push(grow(500, "one"), grow(600, "two"), grow(700, "three"))
	for _, q := range []string{"a", "b", "c"} {
		env.say(q)
		env.idle()
	}
	if n := len(cloud.forkSpecs()); n != 1 {
		t.Fatalf("%d forks; want one after two snapshot failures", n)
	}
}

func TestCoordinatorForkFailureAndLossySummary(t *testing.T) {
	env, _, cloud := forkEnv(t, nil, nil)
	cloud.forkErr = errors.New("rate limited")
	cloud.peerOut = ForkOutput{Calls: 2}
	cloud.push(grow(500, "one"), grow(1000, "two"))
	env.say("a")
	env.idle()
	if n := len(findEntries(env.log(), KindSummary, "")); n != 0 {
		t.Fatalf("%d summaries after a failed fork", n)
	}
	if tot := env.c.Ledger().TotalsByKind()[UsageForkPeer]; tot.Calls != 2 || tot.UnknownCalls != 2 {
		t.Fatalf("failed fork ledger = %+v", tot)
	}

	cloud.fmu.Lock()
	cloud.forkErr = nil
	cloud.peerOut = ForkOutput{Text: strings.Repeat("y", 4000), Rejected: true, Calls: 2, Usage: &model.ChatUsage{InputTokens: 7}}
	cloud.fmu.Unlock()
	env.say("b")
	env.idle()
	sums := findEntries(env.log(), KindSummary, "")
	if len(sums) != 1 || !sums[0].Summary.Lossy {
		t.Fatalf("summaries = %+v", sums)
	}
	text := sums[0].Text()
	if n := utf8.RuneCountInString(text); n > 800 || !strings.Contains(text, "[…]") {
		t.Fatalf("lossy summary has %d runes: %.40q", n, text)
	}
}

func TestCoordinatorPeerForkOnHandoff(t *testing.T) {
	for _, disabled := range []bool{false, true} {
		env, edge, cloud := forkEnv(t, nil, func(cfg *Config) {
			cfg.Summaries.PeerEvery = 1 << 30
			cfg.Summaries.Disabled = disabled
		})
		var oc uint64
		cloud.push(func(r *fakeRun) *ActivationEnd {
			r.say("over to you")
			var err error
			if oc, err = r.transfer("edge"); err != nil {
				t.Errorf("transfer: %v", err)
			}
			return &ActivationEnd{Reason: types.ReplyHandedOff}
		})
		edge.push(replyWith("hello"))
		env.say("go")
		env.idle()
		sums := findEntries(env.log(), KindSummary, "")
		if disabled {
			if len(sums) != 0 {
				t.Fatalf("summaries while disabled: %+v", sums)
			}
			continue
		}
		if len(sums) != 1 || sums[0].Coverage.Through != oc {
			t.Fatalf("handoff summary = %+v, transfer #%d", sums, oc)
		}
	}
}

func TestMiddleCut(t *testing.T) {
	if got := middleCut("short", 10); got != "short" {
		t.Fatalf("short text cut: %q", got)
	}
	got := middleCut(strings.Repeat("a", 50)+strings.Repeat("z", 50), 10)
	if utf8.RuneCountInString(got) != 40 || !strings.HasPrefix(got, "aaaa") || !strings.HasSuffix(got, "zzzz") {
		t.Fatalf("cut = %q", got)
	}
	if got := middleCut("abcdefgh", 1); got != "abcd" {
		t.Fatalf("tiny budget cut = %q", got)
	}
}
