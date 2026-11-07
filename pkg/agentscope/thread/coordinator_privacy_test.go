package thread

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/agentscope-ai/agentscope-go/v2/pkg/agentscope/model"
	"github.com/agentscope-ai/agentscope-go/v2/pkg/agentscope/types"
)

// TestCoordinatorPrivacySession runs a session with edge-only inputs and
// private edge work, then checks that no edge-only text reached the cloud
// through a Sync, a Pending result, a Read, an activation input or an
// event page.
func TestCoordinatorPrivacySession(t *testing.T) {
	edge, cloud := edgeCloud()
	edgeErrs := map[string]error{}
	var recordID string
	edge.push(func(r *fakeRun) *ActivationEnd {
		h := r.host()
		recordID = h.RecordEvent(EventRecord{Tool: "read_contacts", Output: "SECRET-E3 alice 555"})
		_, _, edgeErrs["assign"] = r.assign("cloud", "look up SECRET-P1", false)
		_, edgeErrs["report"] = r.report("SECRET-R4", true, "cloud")
		if _, err := h.Publish(r.ctx, PublishRequest{From: Origin{Participant: "edge"}, Refs: []ArtifactRef{{URI: "file:///SECRET-A5"}}}); err != nil {
			t.Errorf("publish: %v", err)
		}
		r.say("Noted SECRET-R2")
		return &ActivationEnd{Reason: types.ReplyCompleted, Text: r.text(), Private: true}
	})
	var cloudErrs []error
	cloud.push(replyWith("Rome plan"), func(r *fakeRun) *ActivationEnd {
		r.pending()
		if _, err := r.read(ReadQuery{}); err != nil {
			cloudErrs = append(cloudErrs, err)
		}
		var all []uint64
		for i := uint64(1); i <= 20; i++ {
			all = append(all, i)
		}
		if _, err := r.read(ReadQuery{Seqs: all}); err != nil {
			cloudErrs = append(cloudErrs, err)
		}
		if got := r.host().EventOutline("cloud", 0); len(got) != 0 {
			t.Errorf("cloud outline = %+v", got)
		}
		if _, err := r.page(recordID); !errors.Is(err, ErrEventNotFound) {
			t.Errorf("cloud page error = %v", err)
		}
		return r.reply("flights booked")
	})
	env := newCoEnv(t, nil, func(cfg *Config) { cfg.Router = zoneRouter() }, edge, cloud)
	env.say("plan a trip to Rome")
	env.idle()
	env.submit(Query{Text: "my passport is SECRET-P1", Disclosure: DiscloseEdgeOnly})
	env.idle()
	env.say("book flights")
	env.idle()

	for _, name := range []string{"assign", "report"} {
		if !errors.Is(edgeErrs[name], errPrivateBrief) {
			t.Errorf("private %s to the cloud: %v", name, edgeErrs[name])
		}
	}
	if len(cloudErrs) > 0 {
		t.Fatalf("cloud reads failed: %v", cloudErrs)
	}
	log := env.log()
	var edgeOnly int
	for i := range log {
		e := &log[i]
		if strings.Contains(renderEntry(e), "SECRET") && e.Visibility != EdgeOnly {
			t.Errorf("#%d %s holds private text but is %s", e.Seq, e.Kind, e.Visibility)
		}
		if e.Visibility == EdgeOnly {
			edgeOnly++
		}
	}
	if edgeOnly != 3 { // the input, the private reply and the artifact
		t.Fatalf("%d edge-only entries", edgeOnly)
	}
	seen := cloud.seenTexts()
	if containsText(seen, "SECRET") {
		t.Fatalf("edge-only text reached the cloud: %q", seen)
	}
	if !containsText(seen, "withheld: edge-only") {
		t.Fatalf("the cloud saw no gap: %q", seen)
	}
	for _, s := range cloud.pendingResults() {
		if containsText(syncTexts(s), "SECRET") {
			t.Fatal("a Pending result held edge-only text")
		}
	}
	env.c.mu.Lock()
	private := env.c.parts["edge"].private
	env.c.mu.Unlock()
	if !private {
		t.Fatal("the edge is not marked private")
	}
}

func TestCoordinatorDisclosurePolicyDeclassifies(t *testing.T) {
	for _, tc := range []struct {
		name string
		fn   func(string) (Visibility, error)
		want Visibility
	}{
		{"shared", func(string) (Visibility, error) { return Shared, nil }, Shared},
		{"edge only", func(string) (Visibility, error) { return EdgeOnly, nil }, EdgeOnly},
		{"error", func(string) (Visibility, error) { return "", errors.New("down") }, EdgeOnly},
	} {
		t.Run(tc.name, func(t *testing.T) {
			edge, cloud := edgeCloud()
			policy := &fakePolicy{fn: tc.fn}
			edge.push(func(r *fakeRun) *ActivationEnd {
				r.say("the weather is fine")
				return &ActivationEnd{Reason: types.ReplyCompleted, Text: r.text(), Private: true,
					Digest: &ToolDigest{Calls: []ToolCallDigest{{Name: "Read", Args: `{"path":"notes.txt"}`}}}}
			})
			env := newCoEnv(t, nil, func(cfg *Config) { cfg.Disclosure = policy }, edge, cloud)
			env.say("weather?")
			env.idle()
			reps := findEntries(env.log(), KindReply, "")
			if len(reps) != 1 || reps[0].Visibility != tc.want {
				t.Fatalf("reply = %+v", reps)
			}
			if len(policy.calls) != 1 || !strings.Contains(policy.calls[0], "notes.txt") {
				t.Fatalf("classified %q; the digest must be classified with the text", policy.calls)
			}
		})
	}
}

func TestCoordinatorTaintPropagation(t *testing.T) {
	edge, cloud := edgeCloud()
	edge.push(func(r *fakeRun) *ActivationEnd {
		r.say("from the web")
		return &ActivationEnd{Reason: types.ReplyCompleted, Text: r.text(), Tainted: true}
	}, func(r *fakeRun) *ActivationEnd {
		seq, err := r.host().Transfer(r.ctx, TransferRequest{From: Origin{Participant: "edge"}, Epoch: r.act.Epoch,
			To: "cloud", Brief: testBrief(), Text: "handing over", Tainted: true})
		if err != nil || seq == 0 {
			t.Errorf("transfer: %v", err)
		}
		return &ActivationEnd{Reason: types.ReplyHandedOff}
	})
	env := newCoEnv(t, nil, nil, edge, cloud)
	env.say("search")
	env.idle()
	env.say("continue in the cloud")
	env.idle()
	log := env.log()
	reps := findEntries(log, KindReply, "")
	if len(reps) < 2 || !reps[0].Tainted || !reps[1].Tainted {
		t.Fatalf("replies = %+v", reps)
	}
	ocs := findEntries(log, KindOwnerChange, "")
	if last := ocs[len(ocs)-1]; last.Owner.Reason != ReasonTransfer || !last.Tainted {
		t.Fatalf("transfer = %+v", last)
	}
	acts := cloud.activations()
	if len(acts) != 1 || acts[0].Sync == nil || acts[0].Sync.Taint == nil || !*acts[0].Sync.Taint {
		t.Fatalf("cloud import taint = %+v", acts[0].Sync)
	}
	if !strings.Contains(strings.Join(syncTexts(acts[0].Sync), "\n"), "(untrusted source)") {
		t.Fatal("the tainted reply is not marked in the projection")
	}
}

// TestCoordinatorRecovery: a request the stopped process was answering is
// resumed on its owner. The restarted marker carries the host's note, the
// input quotes the request and warns about unknown effects, the import holds
// what the log says. A task without a final report is reported unfinished,
// task IDs continue, and a second restart finds nothing new.
func TestCoordinatorRecovery(t *testing.T) {
	store := NewMemoryStore(WithClock(fixedClock()))
	mustAppend(t, store, nil, userInput("answered"))
	mustAppend(t, store, &Guard{}, ownerChange("", "cloud"))
	rep := replyFrom("cloud", "done")
	rep.BasedOn = 1
	mustAppend(t, store, &Guard{Owner: "cloud", Epoch: 1}, rep)
	mustAppend(t, store, nil,
		userInput("unanswered"),
		Entry{Kind: KindAssign, Origin: Origin{Participant: "cloud"}, To: []string{"edge"}, Visibility: Shared,
			Task: &Task{ID: "t-7"}, Content: textBlocks("do it")},
		Entry{Kind: KindArtifact, Visibility: EdgeOnly, Refs: []ArtifactRef{{URI: "file:///private"}}},
		Entry{Kind: KindProgress, Origin: Origin{Participant: "cloud"}, Visibility: Shared, BasedOn: 4, Run: "a-1",
			Content: textBlocks("1. [in_progress] look it up"), DedupKey: "progress:a-1:1"},
	)
	edge, cloud := edgeCloud()
	// The edge imported the edge-only artifact: its checkpoint is private.
	edge.setState(func(s *ParticipantState) { s.Cursor, s.Private = 6, true })
	cloud.setState(func(s *ParticipantState) { s.Cursor = 4 })
	var taskID string
	cloud.push(func(r *fakeRun) *ActivationEnd {
		taskID, _, _ = r.assign("edge", "next task", false)
		return r.reply("checked, done now")
	})
	env := newCoEnv(t, store, func(cfg *Config) { cfg.RecoveryNote = "power loss\nat 02:00" }, edge, cloud)
	env.idle()

	log := env.log()
	if got := kindsOf(log[7:9]); !reflect.DeepEqual(got, []string{"marker:restarted", "report"}) {
		t.Fatalf("recovery entries = %v", got)
	}
	mk := log[7]
	if mk.BasedOn != 4 || mk.DedupKey != "restart:4" || !strings.Contains(mk.Marker.Detail, "#4") ||
		!strings.HasSuffix(mk.Marker.Detail, "Host: power loss\nat 02:00") {
		t.Fatalf("restarted marker = %+v %+v", mk, mk.Marker)
	}
	orphan := log[8]
	if orphan.Task == nil || orphan.Task.ID != "t-7" || !reflect.DeepEqual(orphan.To, []string{"cloud"}) || orphan.BasedOn != 5 {
		t.Fatalf("orphan report = %+v", orphan)
	}
	acts := cloud.activations()
	if len(acts) != 1 || !acts[0].Foreground || acts[0].Trigger.Seq != mk.Seq {
		t.Fatalf("cloud activations = %+v", acts)
	}
	wantInput := `[The session restarted while the user's request #4 "unanswered" was being answered (host: power loss at 02:00). ` +
		`Work done before the restart may or may not have taken effect: check the actual state before you redo anything. ` +
		`The latest progress is #7. Continue the request.]`
	if acts[0].Input != wantInput {
		t.Fatalf("resume input:\n%s\nwant\n%s", acts[0].Input, wantInput)
	}
	if imported := strings.Join(syncTexts(acts[0].Sync), "\n"); !strings.Contains(imported, "[#7 progress of cloud]") {
		t.Fatalf("the resumed owner does not see its own progress:\n%s", imported)
	}
	var resumed *Entry
	for i := range log {
		if log[i].Kind == KindReply && log[i].Text() == "checked, done now" {
			resumed = &log[i]
		}
	}
	if resumed == nil || resumed.BasedOn != mk.Seq {
		t.Fatalf("resumed reply = %+v", resumed)
	}
	if taskID != "t-8" {
		t.Fatalf("task id after recovery = %q", taskID)
	}
	if owner, epoch := env.c.Owner(); owner != "cloud" || epoch != 1 {
		t.Fatalf("owner = %s %d", owner, epoch)
	}
	env.c.mu.Lock()
	private := env.c.parts["edge"].private
	waiting := len(env.c.inputs)
	env.c.mu.Unlock()
	if !private || waiting != 0 {
		t.Fatalf("private %v, waiting inputs %d", private, waiting)
	}

	// A second restart finds nothing new to recover.
	edge2, cloud2 := edgeCloud()
	before := len(env.log())
	c2, err := NewCoordinator(t.Context(), Config{Store: store, Participants: []Participant{edge2, cloud2}})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c2.Close() }()
	if after, _ := store.Head(t.Context()); after.LastSeq != uint64(before) {
		t.Fatalf("second recovery appended %d entries", after.LastSeq-uint64(before))
	}
}

// TestCoordinatorRecoveryAfterAFailover: the process stopped after the cloud
// failed over to the edge and before the edge answered. The edge resumes
// the request; its input points at the cloud's failure and at the newest
// progress, its own, which its first import brings back although it wrote
// it past its checkpoint's cursor.
func TestCoordinatorRecoveryAfterAFailover(t *testing.T) {
	store := NewMemoryStore(WithClock(fixedClock()))
	mustAppend(t, store, nil, userInput("plan my day"))
	mustAppend(t, store, &Guard{}, ownerChange("", "cloud"))
	failed := markerEntry(MarkerFailed, 1, "cloud failed: cloud model unreachable")
	failed.DedupKey = "failed:a-1"
	fo := ownerChange("cloud", "edge")
	fo.BasedOn = 1
	fo.Owner.Reason = ReasonFailover
	mustAppend(t, store, nil,
		Entry{Kind: KindProgress, Origin: Origin{Participant: "cloud"}, Visibility: Shared, BasedOn: 1, Run: "a-1",
			Content: textBlocks("1. [in_progress] draft the morning"), DedupKey: "progress:a-1:1"},
		failed)
	mustAppend(t, store, &Guard{Owner: "cloud", Epoch: 1}, fo)
	mustAppend(t, store, nil,
		Entry{Kind: KindProgress, Origin: Origin{Participant: "edge"}, Visibility: EdgeOnly, BasedOn: 5, Run: "a-2",
			Content: textBlocks("1. [in_progress] read the calendar"), DedupKey: "progress:a-2:1"})
	edge, cloud := edgeCloud()
	edge.setState(func(s *ParticipantState) { s.Cursor = 2 })
	edge.push(replyWith("Here is your plan."))
	env := newCoEnv(t, store, nil, edge, cloud)
	env.idle()

	acts := edge.activations()
	if len(acts) != 1 || acts[0].Trigger.Kind != KindMarker || acts[0].Trigger.BasedOn != 5 {
		t.Fatalf("edge activations = %+v", acts)
	}
	in := acts[0].Input
	for _, want := range []string{`request #1 "plan my day"`, "A failure before it is recorded in #4", "The latest progress is #6"} {
		if !strings.Contains(in, want) {
			t.Fatalf("resume input %q lacks %q", in, want)
		}
	}
	imported := strings.Join(syncTexts(acts[0].Sync), "\n")
	for _, want := range []string{"[#3 progress of cloud]", "[#4 marker failed]", "[#6 progress of edge]"} {
		if !strings.Contains(imported, want) {
			t.Fatalf("the edge's import lacks %q:\n%s", want, imported)
		}
	}
	if len(cloud.activations()) != 0 {
		t.Fatal("the cloud ran after the failover")
	}
}

// TestCoordinatorRecoveryStopsAfterRepeatedRestarts: a request whose run
// stopped the process maxRestarts times is parked with a recovered marker on
// its newest restart, which ends it for the next recovery too.
func TestCoordinatorRecoveryStopsAfterRepeatedRestarts(t *testing.T) {
	store := NewMemoryStore(WithClock(fixedClock()))
	mustAppend(t, store, nil, userInput("crashy"))
	mustAppend(t, store, &Guard{}, ownerChange("", "cloud"))
	first := markerEntry(MarkerRestarted, 1, "restarted")
	first.DedupKey = "restart:1"
	mustAppend(t, store, nil, first)
	second := markerEntry(MarkerRestarted, 3, "restarted")
	second.DedupKey = "restart:3"
	mustAppend(t, store, nil, second)
	edge, cloud := edgeCloud()
	env := newCoEnv(t, store, nil, edge, cloud)
	env.idle()
	log := env.log()
	if got := kindsOf(log[4:]); !reflect.DeepEqual(got, []string{"marker:recovered"}) || log[4].BasedOn != 4 || log[4].DedupKey != "recovered:4" {
		t.Fatalf("recovery entries = %v", got)
	}
	if len(cloud.activations()) != 0 {
		t.Fatal("the request ran a third time")
	}
	env.c.mu.Lock()
	parked := len(env.c.inputs) == 1 && env.c.inputs[0].parked && env.c.inputs[0].entry.Seq == 1
	env.c.mu.Unlock()
	if !parked {
		t.Fatal("the request does not wait for the next message")
	}

	// The process stops again with a task left open: the parked request
	// stays ended, and the task's report is written.
	again := NewMemoryStore(WithClock(fixedClock()))
	for _, e := range log {
		e.Seq, e.Epoch = 0, 0
		var g *Guard
		if e.Kind == KindOwnerChange {
			g = &Guard{}
		}
		mustAppend(t, again, g, e)
	}
	mustAppend(t, again, nil, assignTo("edge", "t-9"))
	edge2, cloud2 := edgeCloud()
	env2 := newCoEnv(t, again, nil, edge2, cloud2)
	env2.idle()
	if got := kindsOf(env2.log()[6:]); !reflect.DeepEqual(got, []string{"report"}) {
		t.Fatalf("second recovery wrote %v", got)
	}
	if len(cloud2.activations())+len(edge2.activations()) != 0 {
		t.Fatal("the parked request ran again")
	}
}

// assignTo is a task the cloud assigned to a participant.
func assignTo(to, id string) Entry {
	return Entry{Kind: KindAssign, Origin: Origin{Participant: "cloud"}, To: []string{to}, Visibility: Shared,
		Task: &Task{ID: id}, Content: textBlocks("check the sensors")}
}

// TestCoordinatorRecoveryCountsEveryRequeue: a request whose routing never
// finished gets a restarted marker of its own at every restart, also when
// the recovery writes other entries, and is parked after maxRestarts.
func TestCoordinatorRecoveryCountsEveryRequeue(t *testing.T) {
	store := NewMemoryStore(WithClock(fixedClock()))
	mustAppend(t, store, nil, userInput("route me"))
	first := markerEntry(MarkerRestarted, 1, "restarted")
	first.DedupKey = "requeue:1:1"
	mustAppend(t, store, nil, first, assignTo("edge", "t-7"))
	edge, cloud := edgeCloud()
	blocked := &fakeRouter{fn: func(ctx context.Context, _ *RouteRequest) (RouteDecision, error) {
		<-ctx.Done()
		return RouteDecision{}, ctx.Err()
	}}
	env := newCoEnv(t, store, func(cfg *Config) { cfg.Router = blocked }, edge, cloud)
	log := env.log()
	if got := kindsOf(log[3:]); !reflect.DeepEqual(got, []string{"marker:restarted", "report"}) ||
		log[3].BasedOn != 1 || log[3].DedupKey != "requeue:1:2" {
		t.Fatalf("second recovery wrote %v: %+v", got, log[3:])
	}

	third := NewMemoryStore(WithClock(fixedClock()))
	for _, e := range log {
		e.Seq, e.Epoch = 0, 0
		mustAppend(t, third, nil, e)
	}
	edge3, cloud3 := edgeCloud()
	env3 := newCoEnv(t, third, nil, edge3, cloud3)
	env3.idle()
	log3 := env3.log()
	if got := kindsOf(log3[5:]); !reflect.DeepEqual(got, []string{"marker:recovered"}) || log3[5].BasedOn != 1 {
		t.Fatalf("third recovery wrote %v", got)
	}
	if len(cloud3.activations())+len(edge3.activations()) != 0 {
		t.Fatal("the request was routed a third time")
	}
}

// TestCoordinatorRecoveryLeavesEndedRequests: a request the log already
// ended (interrupted, no owner, recovered before) is not resumed.
func TestCoordinatorRecoveryLeavesEndedRequests(t *testing.T) {
	for _, kind := range []MarkerKind{MarkerInterrupted, MarkerNoOwner, MarkerRecovered} {
		t.Run(string(kind), func(t *testing.T) {
			store := NewMemoryStore(WithClock(fixedClock()))
			mustAppend(t, store, nil, userInput("stop it"))
			mustAppend(t, store, &Guard{}, ownerChange("", "cloud"))
			mustAppend(t, store, nil, markerEntry(kind, 1, "ended"))
			edge, cloud := edgeCloud()
			env := newCoEnv(t, store, nil, edge, cloud)
			env.idle()
			if log := env.log(); len(log) != 3 {
				t.Fatalf("recovery wrote %v", kindsOf(log[3:]))
			}
			if len(cloud.activations())+len(edge.activations()) != 0 {
				t.Fatal("an ended request ran again")
			}
		})
	}
}

// TestCoordinatorRecoveryRoutesUnroutedRequests: requests whose routing
// never finished (there is no owner yet) ran nothing. They are routed again
// as Submit would route them: the newest with a restarted marker that counts
// the restart, the older one as its context, which the reply records as
// answered.
func TestCoordinatorRecoveryRoutesUnroutedRequests(t *testing.T) {
	store := NewMemoryStore(WithClock(fixedClock()))
	mustAppend(t, store, nil, userInput("old"), userInput("newest"))
	edge, cloud := edgeCloud()
	edge.push(replyWith("hi"))
	cloud.push(replyWith("hi"))
	env := newCoEnv(t, store, nil, edge, cloud)
	env.idle()
	log := env.log()
	if got := kindsOf(log[2:3]); !reflect.DeepEqual(got, []string{"marker:restarted"}) || log[2].BasedOn != 2 || log[2].DedupKey != "requeue:2:1" {
		t.Fatalf("recovery entries = %v", kindsOf(log))
	}
	if m := findEntries(log, KindMarker, MarkerRecovered); len(m) != 0 {
		t.Fatalf("an input that never ran was marked as not run again: %+v", m)
	}
	var routed bool
	for _, a := range append(edge.activations(), cloud.activations()...) {
		if a.Trigger.Seq == 2 && a.Input == "newest" {
			routed = true
		}
	}
	if !routed {
		t.Fatal("the unrouted request was not routed again")
	}
	replies := findEntries(log, KindReply, "")
	if len(replies) != 1 || replies[0].BasedOn == 1 || !slices.Equal(replies[0].Steered, []uint64{1}) {
		t.Fatalf("the reply must record the older input it carried as context: %+v", replies)
	}
}

// TestCoordinatorRecoveryJoinsSteering: the user steered the running
// request, and the process stopped before the reply was sealed, so nothing
// records the steering. The request is resumed with the steering message
// quoted after it; neither gets a recovered marker, and the resumed reply
// records the steering as answered. A later recovery would not take it for a
// request of its own.
func TestCoordinatorRecoveryJoinsSteering(t *testing.T) {
	store := NewMemoryStore(WithClock(fixedClock()))
	mustAppend(t, store, nil, userInput("plan my trip"))
	oc := ownerChange("", "cloud")
	oc.BasedOn = 1
	mustAppend(t, store, &Guard{}, oc)
	mustAppend(t, store, nil, userInput("use metric units"))
	edge, cloud := edgeCloud()
	cloud.push(replyWith("planned, in metric"))
	env := newCoEnv(t, store, nil, edge, cloud)
	env.idle()
	log := env.log()
	if m := findEntries(log, KindMarker, MarkerRecovered); len(m) != 0 {
		t.Fatalf("recovered markers = %+v", m)
	}
	restarted := findEntries(log, KindMarker, MarkerRestarted)
	if len(restarted) != 1 || restarted[0].BasedOn != 1 || !strings.Contains(restarted[0].Marker.Detail, "while #1, #3 was being answered") {
		t.Fatalf("restarted markers = %+v", restarted)
	}
	acts := cloud.activations()
	want := `[The session restarted while the user's messages #1 "plan my trip", #3 "use metric units" were being answered.`
	if len(acts) != 1 || !strings.HasPrefix(acts[0].Input, want) {
		t.Fatalf("cloud activations = %+v", acts)
	}
	replies := findEntries(log, KindReply, "")
	if len(replies) != 1 || !slices.Equal(replies[0].Steered, []uint64{3}) {
		t.Fatalf("the resumed reply must record the steering: %+v", replies)
	}
}

// TestCoordinatorRecoveryRequeuesWhatTheOwnerCannotRead: an edge-only input
// sent while the cloud answered was waiting for a cycle of its own; it does
// not go with the cloud's request but is routed again after it. The cloud
// resumes its request with the restart warning even when nothing in the log
// is based on it yet (an owner change without a base stands for a log
// whose cloud run wrote nothing).
func TestCoordinatorRecoveryRequeuesWhatTheOwnerCannotRead(t *testing.T) {
	for _, based := range []bool{true, false} {
		t.Run(fmt.Sprintf("based=%v", based), func(t *testing.T) {
			store := NewMemoryStore(WithClock(fixedClock()))
			mustAppend(t, store, nil, userInput("send the email to Bob"))
			oc := ownerChange("", "cloud")
			if based {
				oc.BasedOn = 1
			}
			mustAppend(t, store, &Guard{}, oc)
			private := userInput("my passport number is X")
			private.Visibility, private.Disclosure = EdgeOnly, DiscloseEdgeOnly
			mustAppend(t, store, nil, private)
			edge, cloud := edgeCloud()
			cloud.push(replyWith("sent"))
			edge.push(replyWith("noted"))
			env := newCoEnv(t, store, nil, edge, cloud)
			env.idle()
			acts := cloud.activations()
			if len(acts) != 1 || strings.Contains(acts[0].Input, "passport") ||
				!strings.HasPrefix(acts[0].Input, `[The session restarted while the user's request #1 "send the email to Bob"`) {
				t.Fatalf("cloud activations = %+v", acts)
			}
			if acts := edge.activations(); len(acts) != 1 || acts[0].Trigger.Seq != 3 && acts[0].Trigger.BasedOn != 3 {
				t.Fatalf("the edge-only input was not routed again: %+v", acts)
			}
			log := env.log()
			if m := findEntries(log, KindMarker, MarkerRecovered); len(m) != 0 {
				t.Fatalf("recovered markers = %+v", m)
			}
			for _, r := range findEntries(log, KindReply, "") {
				if len(r.Steered) != 0 {
					t.Fatalf("a reply took the other request for steering: %+v", r)
				}
			}
		})
	}
}

// TestCoordinatorRecoveryResumesASkippedInput: an edge-only input skipped by
// a cloud cycle got its own cycle after the cloud answered, and the process
// stopped while the edge answered it. That cycle is the turn in flight,
// although a newer request was answered, so the edge resumes it, also in a
// log whose owner change names no request.
func TestCoordinatorRecoveryResumesASkippedInput(t *testing.T) {
	for _, based := range []bool{true, false} {
		t.Run(fmt.Sprintf("based=%v", based), func(t *testing.T) {
			store := NewMemoryStore(WithClock(fixedClock()))
			secret := userInput("my private note")
			secret.Visibility, secret.Disclosure = EdgeOnly, DiscloseEdgeOnly
			mustAppend(t, store, nil, secret, userInput("plan my trip"))
			toCloud := ownerChange("", "cloud")
			toCloud.BasedOn = 2
			mustAppend(t, store, &Guard{}, toCloud)
			rep := replyFrom("cloud", "planned")
			rep.BasedOn = 2
			mustAppend(t, store, &Guard{Owner: "cloud", Epoch: 1}, rep)
			toEdge := ownerChange("cloud", "edge")
			if based {
				toEdge.BasedOn = 1
			}
			mustAppend(t, store, &Guard{Owner: "cloud", Epoch: 1}, toEdge)
			edge, cloud := edgeCloud()
			edge.push(replyWith("noted"))
			env := newCoEnv(t, store, nil, edge, cloud)
			env.idle()
			log := env.log()
			if m := findEntries(log, KindMarker, MarkerRecovered); len(m) != 0 {
				t.Fatalf("recovered markers = %+v", m)
			}
			acts := edge.activations()
			if len(acts) != 1 || len(cloud.activations()) != 0 {
				t.Fatalf("edge activations = %+v, cloud = %+v", acts, cloud.activations())
			}
			restarted := findEntries(log, KindMarker, MarkerRestarted)
			if len(restarted) != 1 || restarted[0].BasedOn != 1 || restarted[0].DedupKey != "restart:1" || restarted[0].Visibility != EdgeOnly {
				t.Fatalf("restarted markers = %+v", restarted)
			}
			if !strings.Contains(acts[0].Input, `request #1 "my private note"`) {
				t.Fatalf("edge input = %q", acts[0].Input)
			}
		})
	}
}

// TestCoordinatorRecoveryJoinsAnEarlierRequest: a request recovery routed
// again at an earlier restart was waiting while a newer one ran. Both are
// the turn in flight: the newer one, which the router sent to the cloud, is
// resumed with the earlier one joined to it and its progress, and neither is
// marked as not run again.
func TestCoordinatorRecoveryJoinsAnEarlierRequest(t *testing.T) {
	store := NewMemoryStore(WithClock(fixedClock()))
	mustAppend(t, store, nil, userInput("book the hotel"))
	requeued := markerEntry(MarkerRestarted, 1, "restarted")
	requeued.DedupKey = "requeue:1:1"
	mustAppend(t, store, nil, requeued,
		Entry{Kind: KindProgress, Origin: Origin{Participant: "cloud"}, Visibility: Shared, BasedOn: 1, Run: "a-0",
			Content: textBlocks("1. [in_progress] compare hotels"), DedupKey: "progress:a-0:1"},
		userInput("and a car"))
	oc := ownerChange("", "cloud")
	oc.BasedOn = 4
	mustAppend(t, store, &Guard{}, oc)
	edge, cloud := edgeCloud()
	cloud.push(replyWith("booked both"))
	env := newCoEnv(t, store, nil, edge, cloud)
	env.idle()
	log := env.log()
	if m := findEntries(log, KindMarker, MarkerRecovered); len(m) != 0 {
		t.Fatalf("recovered markers = %+v", m)
	}
	restarted := findEntries(log, KindMarker, MarkerRestarted)
	if len(restarted) != 2 || restarted[1].DedupKey != "restart:4" || !strings.Contains(restarted[1].Marker.Detail, "while #4, #1 was being answered") {
		t.Fatalf("restarted markers = %+v", restarted)
	}
	acts := cloud.activations()
	want := `[The session restarted while the user's messages #1 "book the hotel", #4 "and a car" were being answered.`
	if len(acts) != 1 || !strings.HasPrefix(acts[0].Input, want) || !strings.Contains(acts[0].Input, "The latest progress is #3") {
		t.Fatalf("cloud activations = %+v", acts)
	}
	replies := findEntries(log, KindReply, "")
	if len(replies) != 1 || !slices.Equal(replies[0].Steered, []uint64{1}) {
		t.Fatalf("replies = %+v", replies)
	}
}

// TestCoordinatorRecoveryJoinsAFailedRunsSteering: the edge's run failed
// after the user steered it, and the process stopped before the failover
// was written. The failed marker records the steering, so it goes with the
// request the edge resumes.
func TestCoordinatorRecoveryJoinsAFailedRunsSteering(t *testing.T) {
	store := NewMemoryStore(WithClock(fixedClock()))
	mustAppend(t, store, nil, userInput("plan my trip"))
	oc := ownerChange("", "edge")
	oc.BasedOn = 1
	mustAppend(t, store, &Guard{}, oc)
	mustAppend(t, store, nil, userInput("use metric units"))
	failed := markerEntry(MarkerFailed, 1, "edge failed: its activation ended with an error")
	failed.DedupKey, failed.Steered = "failed:a-1", []uint64{3}
	mustAppend(t, store, nil, failed)
	edge, cloud := edgeCloud()
	edge.push(replyWith("planned, in metric"))
	env := newCoEnv(t, store, nil, edge, cloud)
	env.idle()
	acts := edge.activations()
	want := `[The session restarted while the user's messages #1 "plan my trip", #3 "use metric units" were being answered.`
	if len(acts) != 1 || !strings.HasPrefix(acts[0].Input, want) || !strings.Contains(acts[0].Input, "A failure before it is recorded in #4") {
		t.Fatalf("edge activations = %+v", acts)
	}
	replies := findEntries(env.log(), KindReply, "")
	if len(replies) != 1 || !slices.Equal(replies[0].Steered, []uint64{3}) {
		t.Fatalf("replies = %+v", replies)
	}
}

// TestCoordinatorRecoveryResumesTheNextRequest: the user sent a request while
// the previous reply was still streaming, or right after interrupting it.
// It waited, ran on the same owner once that run ended (the router wrote no
// owner change), and the process stopped before it wrote anything. It is
// resumed with the restart warning, not run again as a new request.
func TestCoordinatorRecoveryResumesTheNextRequest(t *testing.T) {
	for _, end := range []string{"reply", "interrupted"} {
		t.Run(end, func(t *testing.T) {
			store := NewMemoryStore(WithClock(fixedClock()))
			mustAppend(t, store, nil, userInput("first"))
			oc := ownerChange("", "cloud")
			oc.BasedOn = 1
			mustAppend(t, store, &Guard{}, oc)
			mustAppend(t, store, nil, userInput("send the email to Bob"))
			if end == "reply" {
				rep := replyFrom("cloud", "answer to first")
				rep.BasedOn, rep.DedupKey = 1, "reply:a-1"
				mustAppend(t, store, &Guard{Owner: "cloud", Epoch: 1}, rep)
			} else {
				mk := markerEntry(MarkerInterrupted, 1, "cloud was interrupted while answering #1")
				mk.DedupKey = "interrupted:a-1"
				mustAppend(t, store, nil, mk)
			}
			edge, cloud := edgeCloud()
			cloud.push(replyWith("sent once"))
			env := newCoEnv(t, store, nil, edge, cloud)
			env.idle()
			acts := cloud.activations()
			if len(acts) != 1 || !strings.HasPrefix(acts[0].Input, `[The session restarted while the user's request #3 "send the email to Bob"`) {
				t.Fatalf("cloud activations = %+v", acts)
			}
			if len(edge.activations()) != 0 {
				t.Fatal("the request ran on the edge too")
			}
			if m := findEntries(env.log(), KindMarker, MarkerRecovered); len(m) != 0 {
				t.Fatalf("recovered markers = %+v", m)
			}
		})
	}
}

// TestCoordinatorRecoveryKeepsTheEdgePrivate: after a restart the
// Coordinator takes the edge's privacy from its checkpoint (Private), not
// from the log: a run's private reading is saved with the flag, and the log
// shows neither. The edge was answering a shared request when the process
// stopped; resumed, its reply stays edge-only (there is no disclosure
// policy) exactly when the checkpoint is private, whatever progress the run
// left in the log.
func TestCoordinatorRecoveryKeepsTheEdgePrivate(t *testing.T) {
	for _, private := range []bool{true, false} {
		for _, progress := range []bool{true, false} {
			t.Run(fmt.Sprintf("private=%v/progress=%v", private, progress), func(t *testing.T) {
				store := NewMemoryStore(WithClock(fixedClock()))
				mustAppend(t, store, nil, userInput("how am I doing"))
				oc := ownerChange("", "edge")
				oc.BasedOn = 1
				mustAppend(t, store, &Guard{}, oc)
				if progress {
					mustAppend(t, store, nil, Entry{Kind: KindProgress, Origin: Origin{Participant: "edge"}, Visibility: EdgeOnly, BasedOn: 1,
						Run: "a-1", Content: textBlocks("1. [completed] read heart rate: 142 bpm\n2. [in_progress] answer"),
						DedupKey: "progress:a-1:1"})
				}
				edge, cloud := edgeCloud()
				edge.setState(func(s *ParticipantState) { s.Cursor, s.Private = 1, private })
				edge.push(replyWith("Your heart rate is 142 bpm."))
				env := newCoEnv(t, store, nil, edge, cloud)
				env.idle()
				want := Shared
				if private || progress {
					// The resumed run imports its own edge-only progress.
					want = EdgeOnly
				}
				replies := findEntries(env.log(), KindReply, "")
				if len(replies) != 1 || replies[0].Visibility != want {
					t.Fatalf("replies = %+v", replies)
				}
			})
		}
	}
	// The cloud answers after the restart, and the edge's checkpoint is
	// private (its cut-off background task read a sensor): the edge stays
	// private although nothing in the log says so.
	t.Run("cut-off task", func(t *testing.T) {
		store := NewMemoryStore(WithClock(fixedClock()))
		mustAppend(t, store, nil, userInput("check my vitals in the background"))
		oc := ownerChange("", "cloud")
		oc.BasedOn = 1
		mustAppend(t, store, &Guard{}, oc)
		as := assignTo("edge", "t-1")
		as.BasedOn = 1
		mustAppend(t, store, nil, as)
		rep := replyFrom("cloud", "started a task on the edge")
		rep.BasedOn, rep.DedupKey = 1, "reply:a-1"
		mustAppend(t, store, &Guard{Owner: "cloud", Epoch: 1}, rep)
		edge, cloud := edgeCloud()
		edge.setState(func(s *ParticipantState) { s.Cursor, s.Private = 3, true })
		cloud.setState(func(s *ParticipantState) { s.Cursor = 4 })
		env := newCoEnv(t, store, nil, edge, cloud)
		env.idle()
		if !edgePrivate(env, "edge") {
			t.Fatal("the edge's private checkpoint did not make it private")
		}
	})
	// A clear lowered the edge's privacy before the stop: the edge-only
	// entries it imported earlier no longer count, so its replies after the
	// restart are shared again.
	t.Run("cleared before the stop", func(t *testing.T) {
		store := NewMemoryStore(WithClock(fixedClock()))
		mustAppend(t, store, nil, userInput("hi"))
		oc := ownerChange("", "edge")
		oc.BasedOn = 1
		mustAppend(t, store, &Guard{}, oc)
		mustAppend(t, store, nil, Entry{Kind: KindArtifact, Visibility: EdgeOnly, Refs: []ArtifactRef{{URI: "file:///private"}}})
		rep := replyFrom("edge", "hello")
		rep.BasedOn, rep.DedupKey = 1, "reply:a-1"
		mustAppend(t, store, &Guard{Owner: "edge", Epoch: 1}, rep)
		edge, cloud := edgeCloud()
		edge.setState(func(s *ParticipantState) { s.Cursor = 4 })
		edge.push(replyWith("you are welcome"))
		env := newCoEnv(t, store, nil, edge, cloud)
		env.idle()
		if edgePrivate(env, "edge") {
			t.Fatal("the edge turned private although its checkpoint is not")
		}
		env.say("thanks")
		env.idle()
		replies := findEntries(env.log(), KindReply, "")
		if len(replies) != 2 || replies[1].Visibility != Shared {
			t.Fatalf("replies = %+v", replies)
		}
	})
	// A graceful Close sealed the edge's run while an input still waited:
	// the checkpoint is not private, so its replies stay shared.
	t.Run("queued input at a graceful close", func(t *testing.T) {
		store := NewMemoryStore(WithClock(fixedClock()))
		mustAppend(t, store, nil, userInput("hi"))
		oc := ownerChange("", "edge")
		oc.BasedOn = 1
		mustAppend(t, store, &Guard{}, oc)
		mustAppend(t, store, nil, userInput("and the weather?"))
		rep := replyFrom("edge", "hello")
		rep.BasedOn, rep.DedupKey = 1, "reply:a-1"
		mustAppend(t, store, &Guard{Owner: "edge", Epoch: 1}, rep)
		edge, cloud := edgeCloud()
		edge.setState(func(s *ParticipantState) { s.Cursor = 1 })
		edge.push(replyWith("sunny"), replyWith("you are welcome"))
		env := newCoEnv(t, store, nil, edge, cloud)
		env.idle()
		env.say("thanks")
		env.idle()
		if edgePrivate(env, "edge") {
			t.Fatal("the edge turned private although its checkpoint is not")
		}
		replies := findEntries(env.log(), KindReply, "")
		if len(replies) != 3 || replies[1].Visibility != Shared || replies[2].Visibility != Shared {
			t.Fatalf("replies = %+v", replies)
		}
	})
}

// failedWith is a failed marker of run id based on seq that lists the
// inputs the run took.
func failedWith(seq uint64, id string, steered ...uint64) Entry {
	mk := markerEntry(MarkerFailed, seq, "failed: its activation ended with an error")
	mk.DedupKey, mk.Steered = "failed:"+id, steered
	return mk
}

// TestCoordinatorRecoveryMovesSteeringWithItsRequest: an input a failed run
// took follows that run's request after a crash: it is routed again on its
// own when the resuming owner may not read it, gets a recovered marker when
// the request is parked, and is queued with the request when the request is
// routed again. Steered lists that point back at each other or at entries
// that are not requests change nothing.
func TestCoordinatorRecoveryMovesSteeringWithItsRequest(t *testing.T) {
	t.Run("unreadable by the new owner", func(t *testing.T) {
		store := NewMemoryStore(WithClock(fixedClock()))
		mustAppend(t, store, nil, userInput("plan my trip"))
		toEdge := ownerChange("", "edge")
		toEdge.BasedOn = 1
		mustAppend(t, store, &Guard{}, toEdge)
		private := userInput("my passport number is X")
		private.Visibility, private.Disclosure = EdgeOnly, DiscloseEdgeOnly
		mustAppend(t, store, nil, private, failedWith(1, "a-1", 3, 2))
		mustAppend(t, store, &Guard{Owner: "edge", Epoch: 1}, Entry{Kind: KindOwnerChange, Visibility: Shared, BasedOn: 1,
			Owner: &OwnerChange{From: "edge", To: "cloud", Reason: ReasonFailover}, DedupKey: "failover:a-1"})
		edge, cloud := edgeCloud()
		cloud.push(replyWith("planned"))
		edge.push(replyWith("noted"))
		env := newCoEnv(t, store, nil, edge, cloud)
		env.idle()
		restarted := findEntries(env.log(), KindMarker, MarkerRestarted)
		if len(restarted) != 1 || restarted[0].BasedOn != 5 || strings.Contains(restarted[0].Marker.Detail, "#3") {
			t.Fatalf("restarted markers = %+v", restarted)
		}
		if acts := cloud.activations(); len(acts) != 1 || strings.Contains(acts[0].Input, "passport") {
			t.Fatalf("cloud activations = %+v", acts)
		}
		if acts := edge.activations(); len(acts) != 1 || acts[0].Trigger.Seq != 3 {
			t.Fatalf("edge activations = %+v", acts)
		}
		if containsText(cloud.seenTexts(), "passport") {
			t.Fatal("the edge-only steering reached the cloud")
		}
	})
	t.Run("request parked", func(t *testing.T) {
		store := NewMemoryStore(WithClock(fixedClock()))
		mustAppend(t, store, nil, userInput("crashy"))
		oc := ownerChange("", "cloud")
		oc.BasedOn = 1
		mustAppend(t, store, &Guard{}, oc)
		first := markerEntry(MarkerRestarted, 1, "restarted")
		first.DedupKey = "restart:1"
		mustAppend(t, store, nil, first)
		second := markerEntry(MarkerRestarted, 3, "restarted")
		second.DedupKey = "restart:3"
		mustAppend(t, store, nil, second, userInput("faster please"), failedWith(4, "a-3", 5))
		edge, cloud := edgeCloud()
		env := newCoEnv(t, store, nil, edge, cloud)
		env.idle()
		log := env.log()
		if got := kindsOf(log[6:]); !reflect.DeepEqual(got, []string{"marker:recovered", "marker:recovered"}) ||
			log[6].BasedOn != 4 || log[7].BasedOn != 5 {
			t.Fatalf("recovery wrote %v: %+v", got, log[6:])
		}
		if len(cloud.activations())+len(edge.activations()) != 0 {
			t.Fatal("a parked request or its steering ran")
		}
	})
	t.Run("request routed again", func(t *testing.T) {
		store := NewMemoryStore(WithClock(fixedClock()))
		mustAppend(t, store, nil, userInput("plan my trip"), userInput("in May"), failedWith(1, "a-1", 2))
		edge, cloud := edgeCloud()
		edge.push(replyWith("planned for May"))
		cloud.push(replyWith("planned for May"))
		env := newCoEnv(t, store, nil, edge, cloud)
		env.idle()
		log := env.log()
		if log[3].Kind != KindMarker || log[3].DedupKey != "requeue:1:1" {
			t.Fatalf("recovery wrote %+v", log[3])
		}
		replies := findEntries(log, KindReply, "")
		if len(replies) != 1 || replies[0].BasedOn != 2 || !slices.Equal(replies[0].Steered, []uint64{1}) {
			t.Fatalf("replies = %+v", replies)
		}
	})
	t.Run("steered lists that loop", func(t *testing.T) {
		store := NewMemoryStore(WithClock(fixedClock()))
		mustAppend(t, store, nil, userInput("a"), userInput("b"), failedWith(1, "a-1", 2, 99), failedWith(2, "a-2", 1))
		edge, cloud := edgeCloud()
		edge.push(replyWith("done"))
		cloud.push(replyWith("done"))
		env := newCoEnv(t, store, nil, edge, cloud)
		env.idle()
		if m := findEntries(env.log(), KindMarker, MarkerRestarted); len(m) != 1 || m[0].BasedOn != 1 {
			t.Fatalf("restarted markers = %+v", m)
		}
	})
}

// TestCoordinatorRecoveryEndsSteeringWithItsRun: the user interrupted a run
// they had steered; the interrupted marker records the steering, so neither
// message is taken up again after a restart.
func TestCoordinatorRecoveryEndsSteeringWithItsRun(t *testing.T) {
	store := NewMemoryStore(WithClock(fixedClock()))
	mustAppend(t, store, nil, userInput("plan my trip"))
	oc := ownerChange("", "cloud")
	oc.BasedOn = 1
	mustAppend(t, store, &Guard{}, oc)
	mustAppend(t, store, nil, userInput("actually, stop"))
	stop := markerEntry(MarkerInterrupted, 1, "cloud was interrupted while answering #1")
	stop.DedupKey, stop.Steered = "interrupted:a-1", []uint64{3}
	mustAppend(t, store, nil, stop)
	edge, cloud := edgeCloud()
	env := newCoEnv(t, store, nil, edge, cloud)
	env.idle()
	if log := env.log(); len(log) != 4 {
		t.Fatalf("recovery wrote %v", kindsOf(log[4:]))
	}
	if len(cloud.activations())+len(edge.activations()) != 0 {
		t.Fatal("an interrupted run's messages ran again")
	}
}

// TestCoordinatorRecoverySkipsSteeredInputs: an input a reply lists as
// steered into its run is answered; the newest unanswered one is resumed.
func TestCoordinatorRecoverySkipsSteeredInputs(t *testing.T) {
	store := NewMemoryStore(WithClock(fixedClock()))
	mustAppend(t, store, nil, userInput("old"))
	oc := ownerChange("", "cloud")
	oc.BasedOn = 1
	mustAppend(t, store, &Guard{}, oc)
	mustAppend(t, store, nil, userInput("asked"), userInput("steered"))
	rep := replyFrom("cloud", "answered both")
	rep.BasedOn = 3
	rep.Steered = []uint64{4}
	mustAppend(t, store, &Guard{Owner: "cloud", Epoch: 1}, rep)
	mustAppend(t, store, nil, userInput("newest"))
	edge, cloud := edgeCloud()
	cloud.push(replyWith("done"))
	env := newCoEnv(t, store, nil, edge, cloud)
	env.idle()
	log := env.log()
	var recovered []uint64
	for _, e := range log[6:] {
		if e.Kind == KindMarker {
			recovered = append(recovered, e.BasedOn)
		}
	}
	if !reflect.DeepEqual(recovered, []uint64{1, 6}) {
		t.Fatalf("markers based on %v, want the old input's and the newest's", recovered)
	}
	if acts := cloud.activations(); len(acts) != 1 || !strings.Contains(acts[0].Input, `#6 "newest"`) {
		t.Fatalf("cloud activations = %+v", acts)
	}
}

func TestCoordinatorHostCalls(t *testing.T) {
	edge, cloud := edgeCloud()
	type result struct {
		name string
		err  error
	}
	var results []result
	check := func(name string, err error) { results = append(results, result{name, err}) }
	edge.push(func(r *fakeRun) *ActivationEnd {
		h := r.host()
		id := h.RecordEvent(EventRecord{Origin: Origin{Participant: "cloud"}, Zone: ZoneCloud, Tool: "camera", Output: "jpeg bytes", Visibility: Shared})
		h.RecordUsage(LedgerEntry{Participant: "cloud", Kind: UsageMain, Usage: &model.ChatUsage{InputTokens: 3}})
		if d := h.Descriptors(); len(d) != 2 || d[0].Name != "edge" || d[1].Name != "cloud" {
			t.Errorf("descriptors = %+v", d)
		}
		if o, e := h.Owner(); o != "edge" || e != 1 {
			t.Errorf("owner = %s %d", o, e)
		}
		_, err := h.Read(r.ctx, "cloud", ReadQuery{})
		check("read as another", err)
		_, _, err = h.EventPage("cloud", id, 0, 0)
		check("page as another", err)
		if h.EventOutline("cloud", 0) != nil || h.EventSearch("cloud", "", 0) != nil {
			t.Error("event lists answered another reader")
		}
		_, err = h.Pending(r.ctx, "cloud", 0)
		check("pending as another", err)
		_, err = h.Publish(r.ctx, PublishRequest{From: Origin{Participant: "edge"}})
		check("publish without refs", err)
		_, err = h.Publish(r.ctx, PublishRequest{From: Origin{Participant: "cloud"}, Refs: []ArtifactRef{{URI: "x"}}})
		check("publish as another", err)
		_, err = h.Report(r.ctx, ReportRequest{From: Origin{Participant: "edge"}, To: []string{"cloud"}})
		check("empty report", err)
		_, err = h.Report(r.ctx, ReportRequest{From: Origin{Participant: "edge"}, Text: "x", To: []string{"nobody"}})
		check("report to unknown", err)
		_, err = h.Report(r.ctx, ReportRequest{From: Origin{Participant: "edge"}, Text: "x", To: []string{"edge"}})
		check("report to self", err)
		_, err = h.Report(r.ctx, ReportRequest{From: Origin{Participant: "cloud"}, Text: "x"})
		check("report as another", err)
		if _, err := h.Report(r.ctx, ReportRequest{From: Origin{Participant: "edge"}, Text: "fyi", To: []string{"cloud", "cloud"}, TaskID: "t-9"}); err != nil {
			t.Errorf("report: %v", err)
		}
		if _, err := h.Publish(r.ctx, PublishRequest{From: Origin{Participant: "edge"}, Refs: []ArtifactRef{{URI: "file:///plan.md"}},
			Text: "the plan", Private: true}); err != nil {
			t.Errorf("publish: %v", err)
		}
		// The edge-only artifact made the edge private: a report to the cloud
		// now needs the disclosure policy, and there is none.
		_, err = h.Report(r.ctx, ReportRequest{From: Origin{Participant: "edge"}, Text: "fyi", To: []string{"cloud"}})
		check("private report to the cloud", err)
		got, err := h.Read(r.ctx, "edge", ReadQuery{Seqs: []uint64{1, 99}, Kinds: []Kind{KindUserInput}, Limit: 1})
		if err != nil || len(got) != 1 || got[0].Seq != 1 {
			t.Errorf("read seqs = %+v %v", got, err)
		}
		return r.reply("done")
	})
	cloud.push(func(r *fakeRun) *ActivationEnd {
		h := r.host()
		if got := h.EventOutline("cloud", 0); len(got) != 1 || got[0].Tool != "camera" || got[0].Origin.Participant != "edge" {
			t.Errorf("cloud outline = %+v", got)
		}
		if got := h.EventSearch("cloud", "jpeg", 0); len(got) != 1 {
			t.Errorf("cloud search = %+v", got)
		}
		if text, _, err := h.EventPage("cloud", "ev-1", 0, 0); err != nil || text != "jpeg bytes" {
			t.Errorf("cloud page = %q %v", text, err)
		}
		got, err := r.read(ReadQuery{Seqs: []uint64{4, 1}})
		if err != nil || len(got) != 1 || got[0].Seq != 1 {
			t.Errorf("cloud read = %+v %v", got, err)
		}
		return r.reply("seen")
	})
	env := newCoEnv(t, nil, func(cfg *Config) { cfg.CloudReadsEdge = true }, edge, cloud)
	env.say("hello") // #1, owner_change #2; the edge reports #3 and publishes #4
	env.idle()
	env.submit(Query{Text: "shared", Explicit: "cloud"})
	env.idle()

	for _, r := range results {
		if r.err == nil {
			t.Errorf("%s: no error", r.name)
		}
	}
	if len(results) != 10 {
		t.Fatalf("%d checks ran", len(results))
	}
	if tot := env.c.Ledger().Totals()["edge"]; tot.Calls != 1 || tot.InputTokens != 3 {
		t.Fatalf("usage recorded as %+v", env.c.Ledger().Totals())
	}
	log := env.log()
	arts := findEntries(log, KindArtifact, "")
	reps := findEntries(log, KindReport, "")
	if len(arts) != 1 || arts[0].Seq != 4 || arts[0].Visibility != EdgeOnly || len(reps) != 1 || !reflect.DeepEqual(reps[0].To, []string{"cloud"}) ||
		reps[0].Task == nil || reps[0].Task.ID != "t-9" || reps[0].Visibility != Shared {
		t.Fatalf("artifact %+v report %+v", arts, reps)
	}
}
