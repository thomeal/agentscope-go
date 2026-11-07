package thread

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/agentscope-ai/agentscope-go/v2/pkg/agentscope/event"
	"github.com/agentscope-ai/agentscope-go/v2/pkg/agentscope/types"
)

// These tests reproduce the probes of an independent review of the
// Coordinator, one per finding.

func edgePrivate(env *coEnv, name string) bool {
	env.c.mu.Lock()
	defer env.c.mu.Unlock()
	return env.c.parts[name].private
}

// A chunked import whose later chunk fails has already put edge-only content
// into the edge's context: the edge must stay private, so its next reply is
// not shared with the cloud.
func TestCoordinatorFailedChunkKeepsEdgePrivate(t *testing.T) {
	store := NewMemoryStore(WithClock(fixedClock()))
	mustAppend(t, store, nil, Entry{Kind: KindArtifact, Visibility: EdgeOnly, Content: textBlocks("SECRET-1 " + strings.Repeat("s", 120))})
	for range 4 {
		mustAppend(t, store, nil, Entry{Kind: KindArtifact, Visibility: Shared, Content: textBlocks(strings.Repeat("a", 120))})
	}
	edge := smallEdge()
	_, cloud := edgeCloud()
	var once sync.Once
	edge.onCompact = func() {
		once.Do(func() {
			edge.mu.Lock()
			edge.syncErr = errors.New("checkpoint save failed")
			edge.mu.Unlock()
		})
	}
	env := newCoEnv(t, store, nil, edge, cloud)
	env.submit(Query{Text: "go", Explicit: "edge"})
	env.idle()
	if n := len(edge.activations()); n != 0 {
		t.Fatalf("edge ran %d times; its import was meant to fail", n)
	}
	if !containsText(allImported(edge), "SECRET-1") {
		t.Fatal("the first chunk with the edge-only entry was not applied")
	}
	if !edgePrivate(env, "edge") {
		t.Fatal("the edge holds edge-only content from an applied chunk but is not private")
	}

	edge.mu.Lock()
	edge.syncErr = nil
	edge.mu.Unlock()
	edge.push(replyWith("SECRET-1 is the code"))
	env.submit(Query{Text: "again", Explicit: "edge"})
	env.idle()
	reps := findEntries(env.log(), KindReply, "")
	last := reps[len(reps)-1]
	if last.Origin.Participant != "edge" || last.Visibility != EdgeOnly {
		t.Fatalf("the edge's reply is %+v; it must be edge-only", last)
	}
	env.submit(Query{Text: "and now", Explicit: "cloud"})
	env.idle()
	if containsText(cloud.seenTexts(), "SECRET-1") {
		t.Fatal("edge-only text reached the cloud")
	}
}

// A clear is recorded only once its reset is applied: when Activate fails,
// the edge keeps its privacy and the clear is tried again next time.
func TestCoordinatorClearCommitsOnlyWhenApplied(t *testing.T) {
	store := NewMemoryStore(WithClock(fixedClock()))
	mustAppend(t, store, nil, userInput("plan"))
	mustAppend(t, store, &Guard{}, ownerChange("", "cloud"))
	rep := replyFrom("cloud", "the plan")
	rep.Origin.Path = ""
	mustAppend(t, store, &Guard{Owner: "cloud", Epoch: 1}, rep)
	mustAppend(t, store, nil, Entry{
		Kind: KindSummary, Origin: Origin{Participant: "cloud"}, Visibility: Shared,
		Summary: &SummaryMeta{Target: "edge"}, Coverage: &Coverage{Through: 3}, Content: textBlocks("SUMMARY"),
	}) // #4
	edge, cloud := edgeCloud()
	edge.activateErr = errors.New("model not loaded")
	env := newCoEnv(t, store, nil, edge, cloud)
	env.c.mu.Lock()
	env.c.parts["edge"].raisePrivateLocked()
	env.c.mu.Unlock()

	env.submit(Query{Text: "continue", Explicit: "edge"})
	env.idle()
	env.c.mu.Lock()
	ps := env.c.parts["edge"]
	private, held, from := ps.private, ps.heldCoverage, ps.resetFrom
	env.c.mu.Unlock()
	if !private || held != nil || from != 0 {
		t.Fatalf("after a failed Activate: private %v held %+v resetFrom %d; nothing may change", private, held, from)
	}

	edge.mu.Lock()
	edge.activateErr = nil
	edge.mu.Unlock()
	env.submit(Query{Text: "continue again", Explicit: "edge"})
	env.idle()
	acts := edge.activations()
	if len(acts) != 1 || acts[0].Sync == nil || !acts[0].Sync.Reset {
		t.Fatalf("the clear was not applied: %+v", acts)
	}
	env.c.mu.Lock()
	private, held, from = ps.private, ps.heldCoverage, ps.resetFrom
	env.c.mu.Unlock()
	if private || held == nil || held.Through != 3 || from != 4 {
		t.Fatalf("after the clear: private %v held %+v resetFrom %d", private, held, from)
	}
}

// A clear that imports nothing edge-only lowers the edge's privacy only
// when the edge itself does: a private edge that keeps a plan across the
// clear (it may name what a private run read) stays private, and the
// clear's Sync says to clear the flag either way. A plan never makes an
// edge that was not private private.
func TestCoordinatorClearKeepsAPrivatePlan(t *testing.T) {
	for _, plan := range []bool{false, true} {
		t.Run(fmt.Sprintf("plan=%v", plan), func(t *testing.T) {
			store := NewMemoryStore(WithClock(fixedClock()))
			mustAppend(t, store, nil, userInput("plan"))
			mustAppend(t, store, &Guard{}, ownerChange("", "cloud"))
			rep := replyFrom("cloud", "the plan")
			mustAppend(t, store, &Guard{Owner: "cloud", Epoch: 1}, rep)
			mustAppend(t, store, nil, Entry{
				Kind: KindSummary, Origin: Origin{Participant: "cloud"}, Visibility: Shared,
				Summary: &SummaryMeta{Target: "edge"}, Coverage: &Coverage{Through: 3}, Content: textBlocks("SUMMARY"),
			}) // #4
			edge, cloud := edgeCloud()
			edge.planHeld = plan
			edge.setState(func(s *ParticipantState) { s.Private = true })
			env := newCoEnv(t, store, nil, edge, cloud)
			env.c.mu.Lock()
			env.c.parts["edge"].raisePrivateLocked()
			env.c.mu.Unlock()

			env.submit(Query{Text: "continue", Explicit: "edge"})
			env.idle()
			acts := edge.activations()
			if len(acts) != 1 || acts[0].Sync == nil || !acts[0].Sync.Reset || acts[0].Sync.Private == nil || *acts[0].Sync.Private {
				t.Fatalf("the clear did not say to clear the flag: %+v", acts)
			}
			if got := edgePrivate(env, "edge"); got != plan {
				t.Fatalf("private after the clear = %v, want %v", got, plan)
			}
			if got := edge.State().Private; got != plan {
				t.Fatalf("the edge's own flag = %v, want %v", got, plan)
			}
		})
	}
	t.Run("plan, never private", func(t *testing.T) {
		store := NewMemoryStore(WithClock(fixedClock()))
		mustAppend(t, store, nil, userInput("plan"))
		mustAppend(t, store, &Guard{}, ownerChange("", "cloud"))
		mustAppend(t, store, &Guard{Owner: "cloud", Epoch: 1}, replyFrom("cloud", "the plan"))
		mustAppend(t, store, nil, Entry{
			Kind: KindSummary, Origin: Origin{Participant: "cloud"}, Visibility: Shared,
			Summary: &SummaryMeta{Target: "edge"}, Coverage: &Coverage{Through: 3}, Content: textBlocks("SUMMARY"),
		}) // #4
		edge, cloud := edgeCloud()
		edge.planHeld = true
		env := newCoEnv(t, store, nil, edge, cloud)
		env.submit(Query{Text: "continue", Explicit: "edge"})
		env.idle()
		if acts := edge.activations(); len(acts) != 1 || acts[0].Sync == nil || !acts[0].Sync.Reset {
			t.Fatalf("no clear: %+v", acts)
		}
		if edgePrivate(env, "edge") || edge.State().Private {
			t.Fatal("a clear made an edge that was never private private")
		}
		var replies []Entry
		for _, e := range findEntries(env.log(), KindReply, "") {
			if e.Origin.Participant == "edge" {
				replies = append(replies, e)
			}
		}
		if len(replies) != 1 || replies[0].Visibility != Shared {
			t.Fatalf("edge replies = %+v", replies)
		}
	})
}

// An edge-only input that arrived while the cloud answered is never dropped
// by the cycle of a newer shared input: it starts its own cycle on the edge.
func TestCoordinatorEdgeOnlyInputNotDropped(t *testing.T) {
	edge, cloud := edgeCloud()
	cloud.started = make(chan Activation, 8)
	release := make(chan struct{})
	cloud.push(gate(release, replyWith("plan ready")))
	env := newCoEnv(t, nil, func(cfg *Config) { cfg.Router = zoneRouter() }, edge, cloud)
	env.say("plan")
	waitStarted(t, cloud)
	secret := env.submit(Query{Text: "my PIN is 4242", Disclosure: DiscloseEdgeOnly})
	more := env.submit(Query{Text: "and the hotel?", Disclosure: DiscloseShared})
	close(release)
	env.idle()

	ca := cloud.activations()
	if len(ca) != 2 || ca[1].Trigger.Seq != more {
		t.Fatalf("cloud activations = %+v", ca)
	}
	ea := edge.activations()
	if len(ea) != 1 || ea[0].Trigger.Seq != secret || ea[0].Input != "my PIN is 4242" {
		t.Fatalf("the edge-only input was not answered on the edge: %+v", ea)
	}
	if containsText(cloud.seenTexts(), "4242") {
		t.Fatal("edge-only input reached the cloud")
	}
	checkNotStranded(t, env, map[string]*fakePart{"edge": edge, "cloud": cloud})
}

// Interrupting the routing of a query also ends the older inputs it would
// have carried as context, each with its own marker.
func TestCoordinatorInterruptRoutingEndsContextInputs(t *testing.T) {
	edge, cloud := edgeCloud()
	edge.started = make(chan Activation, 8)
	release := make(chan struct{})
	edge.push(gate(release, replyWith("first done")))
	routing := make(chan struct{}, 4)
	var calls atomic.Int32
	router := &fakeRouter{fn: func(ctx context.Context, _ *RouteRequest) (RouteDecision, error) {
		if calls.Add(1) == 1 {
			return RouteDecision{Target: "edge"}, nil
		}
		routing <- struct{}{}
		<-ctx.Done()
		return RouteDecision{}, ctx.Err()
	}}
	env := newCoEnv(t, nil, func(cfg *Config) { cfg.Router = router }, edge, cloud)
	env.say("first")
	waitStarted(t, edge)
	second := env.say("second")
	third := env.say("third")
	close(release)
	<-routing
	env.c.Interrupt()
	env.idle()
	var based []uint64
	for _, m := range findEntries(env.log(), KindMarker, MarkerInterrupted) {
		based = append(based, m.BasedOn)
	}
	if !reflect.DeepEqual(based, []uint64{second, third}) {
		t.Fatalf("interrupted markers based on %v, want [%d %d]", based, second, third)
	}
	env.c.mu.Lock()
	defer env.c.mu.Unlock()
	if len(env.c.inputs) != 0 {
		t.Fatalf("%d inputs still waiting", len(env.c.inputs))
	}
}

// When the newest input finds no owner, an older one that may go elsewhere
// still gets its own cycle.
func TestCoordinatorOlderInputRoutedAfterNoOwner(t *testing.T) {
	edge, cloud := edgeCloud()
	cloud.started = make(chan Activation, 8)
	release := make(chan struct{})
	cloud.push(gate(release, replyWith("plan ready")))
	var edgeUp atomic.Bool
	edgeUp.Store(true)
	env := newCoEnv(t, nil, func(cfg *Config) {
		cfg.Router = zoneRouter()
		cfg.Available = func(name string) bool { return name != "edge" || edgeUp.Load() }
	}, edge, cloud)
	env.say("plan")
	waitStarted(t, cloud)
	shared := env.submit(Query{Text: "the hotel?", Disclosure: DiscloseShared})
	secret := env.submit(Query{Text: "my PIN", Disclosure: DiscloseEdgeOnly})
	edgeUp.Store(false)
	close(release)
	env.idle()
	if m := findEntries(env.log(), KindMarker, MarkerNoOwner); len(m) != 1 || m[0].BasedOn != secret {
		t.Fatalf("no_owner markers = %+v", m)
	}
	ca := cloud.activations()
	if len(ca) != 2 || ca[1].Trigger.Seq != shared {
		t.Fatalf("the older shared input was not answered: %+v", ca)
	}
}

// A cloud participant imports what it lacks in full, whatever its window:
// nothing is cut, skipped or compacted on the way in, so its peer summary
// excludes only edge-only entries and entries past its snapshot's cursor.
func TestCoordinatorCloudImportsInFull(t *testing.T) {
	store := NewMemoryStore(WithClock(fixedClock()))
	for i := 1; i <= 5; i++ {
		mustAppend(t, store, nil, Entry{Kind: KindArtifact, Origin: Origin{Participant: "edge"}, Visibility: Shared,
			Content: textBlocks(fmt.Sprintf("EDGE-%d ", i) + strings.Repeat("e", 110))})
	}
	edge := newFakePart("edge", ZoneEdge)
	cloud := newFakeForker("cloud")
	cloud.desc.Window = 400
	cloud.state.InputTokens = 150
	cloud.push(grow(500, "plan ready"))
	env := newCoEnv(t, store, func(cfg *Config) {
		cfg.Router = zoneRouter()
		cfg.Summaries.PeerEvery = 100
	}, edge, cloud)
	env.say("plan") // #6, owner_change #7, reply #8
	env.idle()

	syncs := cloud.appliedSyncs()
	if len(syncs) != 1 {
		t.Fatalf("want the cloud's import in one Sync: %+v", syncs)
	}
	for i := 1; i <= 5; i++ {
		if want := fmt.Sprintf("EDGE-%d %s", i, strings.Repeat("e", 110)); !containsText(syncTexts(&syncs[0]), want) {
			t.Fatalf("the cloud did not import EDGE-%d in full: %q", i, syncTexts(&syncs[0]))
		}
	}
	if n := cloud.compactCount(); n != 0 {
		t.Fatalf("the cloud's import compacted %d times", n)
	}
	sums := findEntries(env.log(), KindSummary, "")
	if len(sums) != 1 {
		t.Fatalf("summaries = %+v", sums)
	}
	if got := sums[0].Coverage.Excluded; !reflect.DeepEqual(got, []uint64{7}) {
		t.Fatalf("coverage excludes %v, want [7]", got)
	}
}

// An unclassified query submitted while the edge is tainted is edge-only in
// every respect: its entry, the running cloud owner's Pending, routing and
// Transfer.
func TestCoordinatorUnclassifiedWhileTaintedIsEdgeOnly(t *testing.T) {
	edge, cloud := edgeCloud()
	edge.setState(func(s *ParticipantState) { s.Tainted = true })
	cloud.started = make(chan Activation, 8)
	release := make(chan struct{})
	var pending []*Sync
	cloud.push(gate(release, func(r *fakeRun) *ActivationEnd {
		pending = append(pending, r.pending())
		return r.reply("planned")
	}))
	var transferErr error
	edge.push(func(r *fakeRun) *ActivationEnd {
		_, transferErr = r.transfer("cloud")
		return r.reply("kept on the device")
	})
	env := newCoEnv(t, nil, func(cfg *Config) {
		cfg.Router = &fakeRouter{fn: func(context.Context, *RouteRequest) (RouteDecision, error) {
			return RouteDecision{Target: "cloud"}, nil // the Coordinator overrides it for an edge-only query
		}}
	}, edge, cloud)
	env.submit(Query{Text: "plan", Disclosure: DiscloseShared})
	waitStarted(t, cloud)
	q := env.submit(Query{Text: "taint-secret"})
	close(release)
	env.idle()

	e, _, _ := env.store.Get(t.Context(), q)
	if e.Visibility != EdgeOnly {
		t.Fatalf("the query is stored %s", e.Visibility)
	}
	if len(pending) != 1 || pending[0] != nil {
		t.Fatalf("the cloud's Pending = %v", pending)
	}
	if containsText(cloud.seenTexts(), "taint-secret") {
		t.Fatal("the query reached the cloud")
	}
	ea := edge.activations()
	if len(ea) != 1 || ea[0].Trigger.Seq != q {
		t.Fatalf("edge activations = %+v", ea)
	}
	if !errors.Is(transferErr, errPrivateBrief) {
		t.Fatalf("transfer of the cycle to the cloud with no disclosure policy: %v", transferErr)
	}
}

// A wake that fails writes its markers and does not fail over: the query
// it would pass on was already answered.
func TestCoordinatorFailedWakeDoesNotFailOver(t *testing.T) {
	edge, cloud := edgeCloud()
	cloud.push(func(r *fakeRun) *ActivationEnd {
		if _, _, err := r.assign("edge", "count the photos", true); err != nil {
			t.Errorf("assign: %v", err)
		}
		return r.reply("ORIGINAL answer")
	}, func(*fakeRun) *ActivationEnd {
		return &ActivationEnd{Reason: types.ReplyError, Err: errors.New("cloud down"), ToolCalls: 1}
	})
	edge.push(replyWith("42 photos"))
	env := newCoEnv(t, nil, func(cfg *Config) { cfg.Router = zoneRouter() }, edge, cloud)
	env.say("ORIGINAL-Q1 please")
	env.idle()

	for _, a := range edge.activations() {
		if strings.Contains(a.Input, "ORIGINAL-Q1") {
			t.Fatalf("the answered query was failed over to the edge: %q", a.Input)
		}
	}
	if n := len(edge.activations()); n != 1 {
		t.Fatalf("edge ran %d times", n)
	}
	log := env.log()
	rep := findEntries(log, KindReport, "")
	if len(rep) != 1 {
		t.Fatalf("reports = %+v", rep)
	}
	if m := findEntries(log, KindMarker, MarkerUnknownEffects); len(m) != 1 || m[0].BasedOn != rep[0].Seq {
		t.Fatalf("unknown_effects = %+v", m)
	}
	for _, oc := range findEntries(log, KindOwnerChange, "") {
		if oc.Owner.Reason == ReasonFailover {
			t.Fatalf("failover after a failed wake: %+v", oc.Owner)
		}
	}
	if owner, _ := env.c.Owner(); owner != "cloud" {
		t.Fatalf("owner = %s", owner)
	}
	checkNotStranded(t, env, map[string]*fakePart{"edge": edge, "cloud": cloud})
}

// A wake that fails does not fail over, so a user message that steered it
// is queued again and gets a cycle of its own instead of being lost. The
// failed marker records it, and recovery would leave it open.
func TestCoordinatorFailedWakeQueuesItsSteering(t *testing.T) {
	edge, cloud := edgeCloud()
	cloud.started = make(chan Activation, 8)
	release := make(chan struct{})
	cloud.push(func(r *fakeRun) *ActivationEnd {
		if _, _, err := r.assign("edge", "count the photos", true); err != nil {
			t.Errorf("assign: %v", err)
		}
		return r.reply("asked the edge")
	}, gate(release, func(r *fakeRun) *ActivationEnd {
		r.pending()
		return &ActivationEnd{Reason: types.ReplyError, Err: errors.New("cloud down")}
	}), replyWith("the album is sorted"))
	edge.push(replyWith("42 photos"))
	env := newCoEnv(t, nil, func(cfg *Config) { cfg.Router = zoneRouter() }, edge, cloud)
	env.say("count my photos")
	waitStarted(t, cloud)
	wake := waitStarted(t, cloud)
	steer := env.say("then sort the album")
	close(release)
	env.idle()

	if wake.Trigger.Kind != KindReport {
		t.Fatalf("second cloud activation = %+v", wake)
	}
	log := env.log()
	failed := findEntries(log, KindMarker, MarkerFailed)
	if len(failed) != 1 || !slices.Equal(failed[0].Steered, []uint64{steer}) {
		t.Fatalf("failed markers = %+v", failed)
	}
	acts := cloud.activations()
	if len(acts) != 3 || acts[2].Trigger.Seq != steer || acts[2].Input != "then sort the album" {
		t.Fatalf("cloud activations = %+v", acts)
	}
	chains := map[uint64]*reqChain{steer: {root: &Entry{Seq: steer}, leaf: steer, last: steer}}
	recordChainEntry(chains, &failed[0], nil)
	if chains[steer].done() {
		t.Fatal("recovery takes a failed wake's steering for answered")
	}
}

// A wake that cannot start leaves its report for the owner's next
// foreground activation; it does not retry the wake in the same cycle.
func TestCoordinatorWakeThatCannotStartWaits(t *testing.T) {
	edge, cloud := edgeCloud()
	var cloudDown atomic.Bool
	cloud.push(func(r *fakeRun) *ActivationEnd {
		if _, _, err := r.assign("edge", "scan", true); err != nil {
			t.Errorf("assign: %v", err)
		}
		return r.reply("scanning")
	}, replyWith("next answer"))
	edge.push(func(r *fakeRun) *ActivationEnd {
		cloudDown.Store(true)
		return r.reply("SCAN-RESULT")
	})
	env := newCoEnv(t, nil, func(cfg *Config) {
		cfg.Router = zoneRouter()
		cfg.Available = func(name string) bool { return name != "cloud" || !cloudDown.Load() }
	}, edge, cloud)
	env.say("scan please")
	env.idle()
	if n := len(cloud.activations()); n != 1 {
		t.Fatalf("cloud ran %d times while unavailable", n)
	}
	if n := len(edge.activations()); n != 1 {
		t.Fatalf("edge ran %d times; a failed wake must not fail over", n)
	}
	env.c.mu.Lock()
	waiting := len(env.c.parts["cloud"].notify)
	wakes := env.c.cycle.wakes
	env.c.mu.Unlock()
	if waiting != 1 {
		t.Fatalf("%d reports wait for the cloud", waiting)
	}
	if wakes != 1 {
		t.Fatalf("the owner was woken %d times for one report; a failed wake must not be retried in the cycle", wakes)
	}

	cloudDown.Store(false)
	env.say("what did the scan find?")
	env.idle()
	ca := cloud.activations()
	if len(ca) != 2 || !containsText(syncTexts(ca[1].Sync), "SCAN-RESULT") {
		t.Fatalf("the report did not reach the owner's next activation: %+v", ca)
	}
	env.c.mu.Lock()
	waiting = len(env.c.parts["cloud"].notify)
	env.c.mu.Unlock()
	if waiting != 0 {
		t.Fatalf("%d reports still wait after a foreground activation imported them", waiting)
	}
}

// reportSignal closes a channel once a task report from a participant is
// appended.
type reportSignal struct {
	Store
	from string
	once sync.Once
	done chan struct{}
}

func (s *reportSignal) Append(ctx context.Context, entries []Entry, g *Guard) (uint64, error) {
	seq, err := s.Store.Append(ctx, entries, g)
	for i := range entries {
		if e := &entries[i]; err == nil && e.Kind == KindReport && e.Task != nil && e.Origin.Participant == s.from {
			s.once.Do(func() { close(s.done) })
		}
	}
	return seq, err
}

// A Notify report meant for the idle owner is not consumed by the owner's
// own background activation: the owner is still woken to answer it.
func TestCoordinatorNotifyReportWakesOwnerAfterBackground(t *testing.T) {
	edge, cloud := edgeCloud()
	store := &reportSignal{Store: NewMemoryStore(WithClock(fixedClock())), from: "cloud", done: make(chan struct{})}
	bRunning := make(chan struct{})
	var bPending *Sync
	edge.push(func(r *fakeRun) *ActivationEnd {
		if _, _, err := r.assign("cloud", "task A", true); err != nil {
			t.Errorf("assign A: %v", err)
		}
		return r.reply("asked the cloud")
	}, func(r *fakeRun) *ActivationEnd { // background task B
		close(bRunning)
		select {
		case <-store.done:
		case <-time.After(testTimeout):
			t.Error("report A was never appended")
		}
		bPending = r.pending()
		return r.reply("B result")
	}, replyWith("handled A"))
	cloud.push(func(r *fakeRun) *ActivationEnd { // background task A
		if _, _, err := r.assign("edge", "task B", false); err != nil {
			t.Errorf("assign B: %v", err)
		}
		<-bRunning
		return r.reply("A result")
	})
	env := newCoEnv(t, store, nil, edge, cloud)
	env.submit(Query{Text: "go", Explicit: "edge"})
	env.idle()

	if bPending != nil && containsText(syncTexts(bPending), "A result") {
		t.Fatalf("report A was delivered into background task B: %q", syncTexts(bPending))
	}
	acts := edge.activations()
	if len(acts) != 3 || !acts[2].Foreground || acts[2].Trigger.Kind != KindReport || !strings.Contains(acts[2].Input, "A result") {
		t.Fatalf("the idle owner was not woken by report A: %d activations %+v", len(acts), acts)
	}
	checkNotStranded(t, env, map[string]*fakePart{"edge": edge, "cloud": cloud})
}

// A Notify report that a background activation of the owner imported as
// context (its trigger came after the report) still wakes the owner: wakes
// follow the reports no foreground activation received, not the cursor.
func TestCoordinatorNotifyReportImportedByBackgroundStillWakes(t *testing.T) {
	edge, cloud := edgeCloud()
	bAssigned := make(chan struct{})
	edge.push(func(r *fakeRun) *ActivationEnd {
		if _, _, err := r.assign("cloud", "task A", true); err != nil {
			t.Errorf("assign A: %v", err)
		}
		return r.reply("asked the cloud")
	}, func(r *fakeRun) *ActivationEnd { // background task X, running while report A arrives
		if _, _, err := r.assign("cloud", "task C", false); err != nil {
			t.Errorf("assign C: %v", err)
		}
		select {
		case <-bAssigned:
		case <-time.After(testTimeout):
			t.Error("task B was never assigned")
		}
		return r.reply("X result")
	}, replyWith("B result"), replyWith("handled A"))
	cloud.push(func(r *fakeRun) *ActivationEnd { // background task A
		if _, _, err := r.assign("edge", "task X", false); err != nil {
			t.Errorf("assign X: %v", err)
		}
		return r.reply("A result")
	}, func(r *fakeRun) *ActivationEnd { // background task C, after report A
		if _, _, err := r.assign("edge", "task B", false); err != nil {
			t.Errorf("assign B: %v", err)
		}
		close(bAssigned)
		return r.reply("C result")
	})
	env := newCoEnv(t, nil, func(cfg *Config) { cfg.Limits.MaxAssignDepth = 5 }, edge, cloud)
	env.submit(Query{Text: "go", Explicit: "edge"})
	env.idle()

	acts := edge.activations()
	if len(acts) != 4 {
		t.Fatalf("edge ran %d times: %+v", len(acts), acts)
	}
	b, wake := acts[2], acts[3]
	if b.Foreground || !containsText(syncTexts(b.Sync), "A result") {
		t.Fatalf("background task B did not import report A as context: %+v", b)
	}
	if !wake.Foreground || wake.Trigger.Kind != KindReport || wake.Trigger.Seq >= b.Trigger.Seq || !strings.Contains(wake.Input, "A result") {
		t.Fatalf("the idle owner was not woken by report A: %+v", wake)
	}
	checkNotStranded(t, env, map[string]*fakePart{"edge": edge, "cloud": cloud})
}

// A background activation receives through Pending the reports of tasks it
// assigned itself, and they do not wake the owner afterwards.
func TestCoordinatorBackgroundPendingGetsOwnTaskReport(t *testing.T) {
	edge, cloud := edgeCloud()
	store := &reportSignal{Store: NewMemoryStore(WithClock(fixedClock())), from: "edge", done: make(chan struct{})}
	var got *Sync
	cloud.push(func(r *fakeRun) *ActivationEnd { // background task from the edge
		if _, _, err := r.assign("edge", "subtask", true); err != nil {
			t.Errorf("assign: %v", err)
		}
		select {
		case <-store.done:
		case <-time.After(testTimeout):
			t.Error("the subtask report was never appended")
		}
		got = r.pending()
		return r.reply("task done")
	})
	edge.push(func(r *fakeRun) *ActivationEnd {
		if _, _, err := r.assign("cloud", "task", false); err != nil {
			t.Errorf("assign: %v", err)
		}
		return r.reply("asked")
	}, replyWith("SUBTASK-RESULT"))
	env := newCoEnv(t, store, func(cfg *Config) { cfg.Limits.MaxAssignDepth = 3 }, edge, cloud)
	env.submit(Query{Text: "go", Explicit: "edge"})
	env.idle()
	if got == nil || !containsText(syncTexts(got), "SUBTASK-RESULT") {
		t.Fatalf("the assigning activation did not receive its task's report: %+v", got)
	}
	if n := len(cloud.activations()); n != 1 {
		t.Fatalf("cloud ran %d times; the report was already delivered", n)
	}
}

// Interrupting the owner while its Transfer waits for the disclosure policy
// commits nothing: no owner change, no answer from the target.
func TestCoordinatorInterruptDuringTransferCommitsNothing(t *testing.T) {
	edge, cloud := edgeCloud()
	inPolicy := make(chan struct{}, 1)
	release := make(chan struct{})
	policy := &fakePolicy{fn: func(string) (Visibility, error) {
		select {
		case inPolicy <- struct{}{}:
		default:
		}
		<-release
		return Shared, nil
	}}
	var transferErr error
	edge.push(func(r *fakeRun) *ActivationEnd {
		r.say("handing over")
		_, transferErr = r.host().Transfer(r.ctx, TransferRequest{From: Origin{Participant: "edge"}, Epoch: r.act.Epoch,
			To: "cloud", Brief: testBrief(), Text: r.text(), Private: true})
		return &ActivationEnd{Reason: types.ReplyInterrupted, Text: r.text()}
	})
	env := newCoEnv(t, nil, func(cfg *Config) { cfg.Disclosure = policy }, edge, cloud)
	q := env.submit(Query{Text: "go", Explicit: "edge"})
	<-inPolicy
	env.c.Interrupt()
	close(release)
	env.idle()

	if !errors.Is(transferErr, ErrInterrupted) || !errors.Is(transferErr, context.Canceled) {
		t.Fatalf("Transfer after Interrupt = %v, want ErrInterrupted wrapping context.Canceled", transferErr)
	}
	for _, oc := range findEntries(env.log(), KindOwnerChange, "") {
		if oc.Owner.Reason == ReasonTransfer {
			t.Fatalf("the transfer was committed after the interrupt: %+v", oc.Owner)
		}
	}
	if n := len(cloud.activations()); n != 0 {
		t.Fatalf("cloud ran %d times", n)
	}
	if m := findEntries(env.log(), KindMarker, MarkerInterrupted); len(m) != 1 || m[0].BasedOn != q {
		t.Fatalf("interrupted markers = %+v", m)
	}
}

// Assign, Report and Publish with a canceled context write nothing.
func TestCoordinatorCanceledWritesCommitNothing(t *testing.T) {
	edge, cloud := edgeCloud()
	errs := map[string]error{}
	var before, after int
	var env *coEnv
	edge.push(func(r *fakeRun) *ActivationEnd {
		h := r.host()
		before = len(env.log())
		ctx := canceledCtx()
		_, _, errs["assign"] = h.Assign(ctx, AssignRequest{From: Origin{Participant: "edge"}, To: "cloud", Text: "x"})
		_, errs["report"] = h.Report(ctx, ReportRequest{From: Origin{Participant: "edge"}, Text: "x", To: []string{"cloud"}})
		_, errs["publish"] = h.Publish(ctx, PublishRequest{From: Origin{Participant: "edge"}, Refs: []ArtifactRef{{URI: "x"}}})
		_, errs["transfer"] = h.Transfer(ctx, TransferRequest{From: Origin{Participant: "edge"}, Epoch: r.act.Epoch, To: "cloud", Brief: testBrief()})
		after = len(env.log())
		var none context.Context
		if _, err := h.Report(none, ReportRequest{From: Origin{Participant: "edge"}, Text: "no context", To: []string{"cloud"}}); err != nil {
			t.Errorf("Report without a context: %v", err)
		}
		return r.reply("done")
	})
	env = newCoEnv(t, nil, nil, edge, cloud)
	env.submit(Query{Text: "go", Explicit: "edge"})
	env.idle()
	for name, err := range errs {
		if !errors.Is(err, ErrInterrupted) || !errors.Is(err, context.Canceled) {
			t.Errorf("%s with a canceled context: %v", name, err)
		}
	}
	if len(errs) != 4 || after != before {
		t.Fatalf("errors %v; %d entries written", errs, after-before)
	}
}

// Text shown before a Transfer reaches the user even when the Coordinator
// read it from the activation only after the commit: in order, before the
// Transfer's entries and before the new owner's events.
func TestCoordinatorTransferForwardsEventsShownBefore(t *testing.T) {
	edge, cloud := edgeCloud()
	edge.buf = 8
	transferred := make(chan struct{})
	edge.push(func(r *fakeRun) *ActivationEnd {
		for _, s := range []string{"a", "b", "c"} {
			r.say(s)
		}
		if _, err := r.host().Transfer(r.ctx, TransferRequest{From: Origin{Participant: "edge"}, Epoch: r.act.Epoch,
			To: "cloud", Brief: testBrief(), Text: r.text(), EventsBefore: 3}); err != nil {
			t.Errorf("transfer: %v", err)
		}
		close(transferred)
		r.say("late")
		return &ActivationEnd{Reason: types.ReplyHandedOff}
	})
	cloud.push(replyWith("cloud answer"))
	c, err := NewCoordinator(context.Background(), Config{
		Store: NewMemoryStore(), Participants: []Participant{edge, cloud}, Limits: Limits{EventBuffer: 1},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close() }()
	if _, err := c.Submit(context.Background(), Query{Text: "go", Explicit: "edge"}); err != nil {
		t.Fatal(err)
	}
	<-transferred // the user has read nothing yet
	var seen []string
	for ev := range c.Events() {
		switch {
		case ev.Kind == EventAgent:
			d := ev.Agent.(event.TextBlockDeltaEvent).Delta
			if ev.Participant == "edge" && !ev.Foreground {
				t.Errorf("edge delta %q is not marked foreground", d)
			}
			seen = append(seen, ev.Participant+":"+d)
		case ev.Kind == EventEntry && ev.Entry.Kind == KindReply:
			seen = append(seen, "reply:"+ev.Entry.Text())
		}
		if ev.Kind == EventEntry && ev.Entry.Kind == KindReply && ev.Participant == "cloud" {
			break
		}
	}
	want := []string{"edge:a", "edge:b", "edge:c", "reply:abc", "cloud:cloud answer", "reply:cloud answer"}
	if !reflect.DeepEqual(seen, want) {
		t.Fatalf("the user saw %q, want %q", seen, want)
	}
}

// A cloud reader never sees an edge-only event record, whatever the
// CloudReadsEdge switch says.
func TestEventListEdgeOnlyRecords(t *testing.T) {
	l := NewEventList(EventListOptions{})
	private := edgeRecord("Read", "diary.txt", "SECRET diary")
	private.Visibility = EdgeOnly
	odd := edgeRecord("Read", "odd.txt", "SECRET odd")
	odd.Visibility = "secret" // unknown values count as edge-only
	unmarked := edgeRecord("Read", "notes.txt", "SECRET unmarked")
	unmarked.Visibility = "" // an unmarked edge record is edge-only
	pid := l.Add(private)
	oid := l.Add(odd)
	l.Add(unmarked)
	sid := l.Add(edgeRecord("Camera", "front", "jpeg"))
	if got := ids(l.Outline(ZoneCloud, true, 0)); !reflect.DeepEqual(got, []string{sid}) {
		t.Fatalf("cloud outline = %v, want only %s", got, sid)
	}
	if got := ids(l.Search(ZoneCloud, true, "SECRET", 0)); len(got) != 0 {
		t.Fatalf("cloud search found %v", got)
	}
	for _, id := range []string{pid, oid} {
		if _, _, err := l.Page(ZoneCloud, true, id, 0, 0); !errors.Is(err, ErrEventNotFound) {
			t.Fatalf("cloud paged edge-only %s: %v", id, err)
		}
	}
	if text, _, err := l.Page(ZoneCloud, true, sid, 0, 0); err != nil || text != "jpeg" {
		t.Fatalf("cloud page of a shared record = %q %v", text, err)
	}
	cloudRec := cloudRecord("Read", "a", "x")
	cloudRec.Visibility = EdgeOnly
	cid := l.Add(cloudRec)
	if text, _, err := l.Page(ZoneEdge, false, cid, 0, 0); err != nil || text != "x" {
		t.Fatalf("edge page of an edge-only record = %q %v", text, err)
	}
}

// RecordEvent stores an edge record as edge-only when the caller says so or
// when the Coordinator knows the edge's context is private.
func TestCoordinatorRecordEventVisibility(t *testing.T) {
	edge, cloud := edgeCloud()
	var ids []string
	edge.push(func(r *fakeRun) *ActivationEnd {
		h := r.host()
		ids = append(ids,
			h.RecordEvent(EventRecord{Tool: "camera", Output: "open output", Visibility: Shared}),
			h.RecordEvent(EventRecord{Tool: "sensor", Output: "PRIVATE-TOOL", Visibility: EdgeOnly}))
		return r.reply("ok")
	}, func(r *fakeRun) *ActivationEnd {
		ids = append(ids, r.host().RecordEvent(EventRecord{Tool: "contacts", Output: "PRIVATE-CONTEXT", Visibility: Shared}))
		return r.reply("ok")
	})
	var outline []EventSummary
	var pageErrs []error
	cloud.push(func(r *fakeRun) *ActivationEnd {
		outline = r.host().EventOutline("cloud", 0)
		for _, id := range ids {
			_, err := r.page(id)
			pageErrs = append(pageErrs, err)
		}
		return r.reply("seen")
	})
	env := newCoEnv(t, nil, func(cfg *Config) { cfg.CloudReadsEdge = true }, edge, cloud)
	env.submit(Query{Text: "look", Explicit: "edge"})
	env.idle()
	env.submit(Query{Text: "private", Disclosure: DiscloseEdgeOnly})
	env.idle()
	env.submit(Query{Text: "shared", Explicit: "cloud"})
	env.idle()
	if len(outline) != 1 || outline[0].Tool != "camera" {
		t.Fatalf("cloud outline = %+v", outline)
	}
	if len(pageErrs) != 3 || pageErrs[0] != nil || !errors.Is(pageErrs[1], ErrEventNotFound) || !errors.Is(pageErrs[2], ErrEventNotFound) {
		t.Fatalf("cloud page errors = %v", pageErrs)
	}
	if containsText(cloud.seenTexts(), "PRIVATE") {
		t.Fatal("an edge-only record reached the cloud")
	}
}

// Recovery counts only the report that ended a task, restores lastSealed
// from task reports and keeps Notify reports past the cursor waiting.
func TestCoordinatorRecoveryTaskReports(t *testing.T) {
	store := NewMemoryStore(WithClock(fixedClock()))
	assign := func(id string) Entry {
		return Entry{Kind: KindAssign, Origin: Origin{Participant: "cloud"}, To: []string{"edge"}, Visibility: Shared,
			Task: &Task{ID: id}, Content: textBlocks("do " + id)}
	}
	taskReport := func(id, key string, notify bool) Entry {
		return Entry{Kind: KindReport, Origin: Origin{Participant: "edge"}, To: []string{"cloud"}, Visibility: Shared,
			Task: &Task{ID: id, Notify: notify}, Notify: notify, Content: textBlocks("about " + id), DedupKey: key}
	}
	mustAppend(t, store, nil,
		assign("t-1"),                         // #1
		taskReport("t-1", "", false),          // #2: progress only
		assign("t-2"),                         // #3
		taskReport("t-2", "report:x-1", true), // #4: the final report, Notify
		Entry{Kind: KindArtifact, Origin: Origin{Participant: "edge"}, Visibility: Shared, Refs: []ArtifactRef{{URI: "f"}}}, // #5
	)
	edge, cloud := edgeCloud()
	cloud.setState(func(s *ParticipantState) { s.Cursor = 3 })
	env := newCoEnv(t, store, nil, edge, cloud)
	orphans := findEntries(env.log(), KindReport, "")
	var ids []string
	for _, o := range orphans {
		if strings.HasPrefix(o.DedupKey, orphanPrefix) {
			ids = append(ids, o.Task.ID)
		}
	}
	if !reflect.DeepEqual(ids, []string{"t-1"}) {
		t.Fatalf("orphan reports for %v, want only t-1", ids)
	}
	env.c.mu.Lock()
	defer env.c.mu.Unlock()
	if got := env.c.parts["edge"].lastSealed; got != 4 {
		t.Fatalf("edge lastSealed = %d, want the task report #4", got)
	}
	if n := env.c.parts["edge"].lastActivity; n != 5 {
		t.Fatalf("edge lastActivity = %d", n)
	}
	if _, ok := env.c.parts["cloud"].notify[4]; !ok || len(env.c.parts["cloud"].notify) != 1 {
		t.Fatalf("cloud notify = %v, want #4 waiting", env.c.parts["cloud"].notify)
	}
}

// A participant whose cursor is past the log head cannot be recovered over
// that log.
func TestCoordinatorRecoveryRejectsCursorPastHead(t *testing.T) {
	store := NewMemoryStore(WithClock(fixedClock()))
	mustAppend(t, store, nil, userInput("one"), userInput("two"))
	edge, cloud := edgeCloud()
	edge.setState(func(s *ParticipantState) { s.Cursor = 9 })
	_, err := NewCoordinator(t.Context(), Config{Store: store, Participants: []Participant{edge, cloud}})
	if err == nil {
		t.Fatal("NewCoordinator accepted a cursor past the log head")
	}
	for _, want := range []string{"edge", "cursor 9", "#2"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q does not name %q", err, want)
		}
	}
}

// A failed Activate gives back the range it marked as seen.
func TestCoordinatorActivateErrorRestoresSeen(t *testing.T) {
	edge, cloud := edgeCloud()
	edge.activateErr = errors.New("model not loaded")
	env := newCoEnv(t, nil, nil, edge, cloud)
	env.say("hi")
	env.idle()
	env.c.mu.Lock()
	seen := env.c.parts["edge"].seen
	env.c.mu.Unlock()
	if seen != 0 {
		t.Fatalf("edge seen = %d after a failed Activate, want 0", seen)
	}
}

// failOwnerChange fails the first append that holds an owner change.
type failOwnerChange struct {
	Store
	failed atomic.Bool
}

func (s *failOwnerChange) Append(ctx context.Context, entries []Entry, g *Guard) (uint64, error) {
	for i := range entries {
		if entries[i].Kind == KindOwnerChange && s.failed.CompareAndSwap(false, true) {
			return 0, errors.New("disk full")
		}
	}
	return s.Store.Append(ctx, entries, g)
}

// An input whose owner change cannot be recorded stays parked, so the next
// owner's activation still receives it.
func TestCoordinatorUnrecordedOwnerChangeParksInput(t *testing.T) {
	store := &failOwnerChange{Store: NewMemoryStore(WithClock(fixedClock()))}
	edge, cloud := edgeCloud()
	edge.push(replyWith("answered both"))
	env := newCoEnv(t, store, nil, edge, cloud)
	first := env.submit(Query{Text: "first", Explicit: "edge"})
	env.idle()
	env.c.mu.Lock()
	parked := len(env.c.inputs) == 1 && env.c.inputs[0].parked && env.c.inputs[0].entry.Seq == first
	env.c.mu.Unlock()
	if n := len(edge.activations()); n != 0 || !parked {
		t.Fatalf("after the failed owner change: %d activations, input parked %v", n, parked)
	}

	env.submit(Query{Text: "second", Explicit: "edge"})
	env.idle()
	acts := edge.activations()
	if len(acts) != 1 || !containsText(syncTexts(acts[0].Sync), fmt.Sprintf("[#%d user]", first)) {
		t.Fatalf("the parked input did not reach the next owner: %+v", acts)
	}
	env.c.mu.Lock()
	defer env.c.mu.Unlock()
	if len(env.c.inputs) != 0 {
		t.Fatalf("%d inputs still waiting", len(env.c.inputs))
	}
}

// An edge write the caller marks private is edge-only and makes the edge
// private at once: the reply sealed after it is edge-only too.
func TestCoordinatorPrivateWriteMakesEdgePrivate(t *testing.T) {
	for name, write := range map[string]func(Host) error{
		"report": func(h Host) error {
			_, err := h.Report(t.Context(), ReportRequest{From: Origin{Participant: "edge"}, Text: "PRIVATE note", Private: true})
			return err
		},
		"publish": func(h Host) error {
			_, err := h.Publish(t.Context(), PublishRequest{From: Origin{Participant: "edge"}, Refs: []ArtifactRef{{URI: "file:///diary"}}, Private: true})
			return err
		},
	} {
		t.Run(name, func(t *testing.T) {
			edge, cloud := edgeCloud()
			edge.push(func(r *fakeRun) *ActivationEnd {
				if err := write(r.host()); err != nil {
					t.Errorf("%s: %v", name, err)
				}
				return r.reply("about what I wrote")
			})
			env := newCoEnv(t, nil, nil, edge, cloud)
			env.submit(Query{Text: "go", Explicit: "edge"})
			env.idle()
			if !edgePrivate(env, "edge") {
				t.Fatal("the edge is not private after an edge-only write")
			}
			for _, e := range env.log() {
				if e.Origin.Participant == "edge" && e.Visibility != EdgeOnly {
					t.Fatalf("#%d %s from the edge is %s", e.Seq, e.Kind, e.Visibility)
				}
			}
		})
	}
}

// A clear the edge cannot apply leaves the Coordinator's view as it was: the
// edge stays private, holds no new coverage and is cleared again at the next
// takeover, with the whole import in that one Sync.
func TestCoordinatorFailedClearKeepsTheView(t *testing.T) {
	store := NewMemoryStore(WithClock(fixedClock()))
	mustAppend(t, store, nil, userInput("plan"))
	mustAppend(t, store, &Guard{}, ownerChange("", "cloud"))
	rep := replyFrom("cloud", "the plan")
	rep.Origin.Path = ""
	mustAppend(t, store, &Guard{Owner: "cloud", Epoch: 1}, rep)
	mustAppend(t, store, nil, Entry{
		Kind: KindSummary, Origin: Origin{Participant: "cloud"}, Visibility: Shared,
		Summary: &SummaryMeta{Target: "edge"}, Coverage: &Coverage{Through: 3}, Content: textBlocks("SUMMARY"),
	}) // #4
	for i := 1; i <= 8; i++ {
		mustAppend(t, store, nil, Entry{Kind: KindArtifact, Visibility: Shared,
			Content: textBlocks(fmt.Sprintf("CHUNK-%d ", i) + strings.Repeat("b", 300))})
	}
	edge := smallEdge()
	edge.syncErr = errors.New("checkpoint save failed")
	_, cloud := edgeCloud()
	cloud.push(replyWith("taken over"))
	env := newCoEnv(t, store, nil, edge, cloud)
	env.c.mu.Lock()
	env.c.parts["edge"].raisePrivateLocked()
	env.c.mu.Unlock()

	env.submit(Query{Text: "continue", Explicit: "edge"})
	env.idle()
	if n := len(edge.activations()); n != 0 || len(edge.appliedSyncs()) != 0 {
		t.Fatalf("want nothing applied and no reply: %d activations, syncs %+v", n, edge.appliedSyncs())
	}
	env.c.mu.Lock()
	ps := env.c.parts["edge"]
	private, held, from := ps.private, ps.heldCoverage, ps.resetFrom
	env.c.mu.Unlock()
	if !private || held != nil || from != 0 {
		t.Fatalf("after the failed clear: private %v held %+v resetFrom %d", private, held, from)
	}

	edge.mu.Lock()
	edge.syncErr = nil
	edge.mu.Unlock()
	edge.push(replyWith("back"))
	env.submit(Query{Text: "continue again", Explicit: "edge"})
	env.idle()
	again := edge.appliedSyncs()
	if len(again) != 1 || !again[0].Reset {
		t.Fatalf("the next takeover must clear again in one Sync: %+v", again)
	}
	texts := syncTexts(&again[0])
	if !containsText(texts, "CHUNK-1") || !containsText(texts, "CHUNK-8") {
		t.Fatalf("the clear must bring the whole import: %q", texts)
	}
	env.c.mu.Lock()
	private, from = ps.private, ps.resetFrom
	env.c.mu.Unlock()
	if private || from != 4 {
		t.Fatalf("after the clear: private %v resetFrom %d", private, from)
	}
}
