package thread

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/agentscope-ai/agentscope-go/v2/pkg/agentscope/event"
	"github.com/agentscope-ai/agentscope-go/v2/pkg/agentscope/types"
)

// edgeEraStore holds a session where the edge answered with eight long
// replies (#3–#10), the cloud took over without importing them, and a summary
// written for the edge (#14) excludes them. The edge's cursor is 10.
func edgeEraStore(t *testing.T) Store {
	t.Helper()
	s := NewMemoryStore(WithClock(fixedClock()))
	mustAppend(t, s, nil, userInput("start"))
	mustAppend(t, s, &Guard{}, ownerChange("", "edge"))
	for i := 0; i < 8; i++ {
		e := replyFrom("edge", strings.Repeat("b", 120))
		e.Origin.Path = ""
		e.BasedOn = 1
		mustAppend(t, s, &Guard{Owner: "edge", Epoch: 1}, e)
	}
	mustAppend(t, s, &Guard{Owner: "edge", Epoch: 1}, ownerChange("edge", "cloud"))
	mustAppend(t, s, nil, userInput("next"))
	cloud := replyFrom("cloud", "cloud answer")
	cloud.Origin.Path = ""
	cloud.BasedOn = 12
	mustAppend(t, s, &Guard{Owner: "cloud", Epoch: 2}, cloud)
	mustAppend(t, s, nil, Entry{
		Kind: KindSummary, Origin: Origin{Participant: "cloud"}, Visibility: Shared,
		Summary:  &SummaryMeta{Target: "edge", Budget: 200},
		Coverage: &Coverage{Through: 13, Excluded: []uint64{3, 4, 5, 6, 7, 8, 9, 10}},
		Content:  textBlocks("SUMMARY: the cloud's part"),
	})
	return s
}

// TestCoordinatorClearInRoundsBelowTheCursor: a clear that replays the edge's
// own excluded replies does not fit in one round, so its first round ends
// below the edge's cursor. The reset sets the cursor (it never moves back
// otherwise), so the edge takes the foreground back instead of failing over.
func TestCoordinatorClearInRoundsBelowTheCursor(t *testing.T) {
	edge := smallEdge()
	edge.state.Cursor = 10
	_, cloud := edgeCloud()
	env := newCoEnv(t, edgeEraStore(t), nil, edge, cloud)
	seq := env.submit(Query{Text: "back to the edge", Explicit: "edge"})
	env.idle()

	syncs := edge.appliedSyncs()
	if len(syncs) < 2 || !syncs[0].Reset || syncs[0].Cursor == 0 || syncs[0].Cursor >= 10 {
		t.Fatalf("the clear's first round must end below the edge's cursor: %+v", syncs)
	}
	acts := edge.activations()
	if len(acts) != 1 || acts[0].Trigger.Seq != seq {
		t.Fatalf("the edge must answer #%d itself: %+v", seq, acts)
	}
	for _, e := range env.log() {
		if e.Kind == KindOwnerChange && e.Owner.Reason == ReasonFailover {
			t.Fatalf("the clear failed over: %+v", e.Owner)
		}
	}
	if st := edge.State(); st.Cursor < seq {
		t.Fatalf("cursor after the clear = %d", st.Cursor)
	}
	got := strings.Join(allImported(edge), "\n")
	for seq := 3; seq <= 10; seq++ {
		if !strings.Contains(got, "[#"+strconv.Itoa(seq)+" reply from edge]") {
			t.Fatalf("the clear lost the edge's reply #%d:\n%s", seq, got)
		}
	}
	env.c.mu.Lock()
	from := env.c.parts["edge"].resetFrom
	env.c.mu.Unlock()
	if from != 14 {
		t.Fatalf("resetFrom = %d", from)
	}
}

// TestCoordinatorParkedInputsDoNotBlockSubmit: edge-only queries that found no
// owner while the edge was down wait parked; they must not fill
// MaxPendingInputs and refuse every later query, and at most
// MaxPendingInputs of them are kept.
func TestCoordinatorParkedInputsDoNotBlockSubmit(t *testing.T) {
	edge, cloud := edgeCloud()
	env := newCoEnv(t, nil, func(cfg *Config) {
		cfg.Limits.MaxPendingInputs = 3
		cfg.Available = func(name string) bool { return name != "edge" }
	}, edge, cloud)
	var private []uint64
	for i := 0; i < 5; i++ {
		private = append(private, env.submit(Query{Text: "private " + strconv.Itoa(i), Disclosure: DiscloseEdgeOnly}))
		env.idle()
	}
	if m := findEntries(env.log(), KindMarker, MarkerNoOwner); len(m) != len(private) {
		t.Fatalf("no_owner markers = %d", len(m))
	}
	env.c.mu.Lock()
	var kept []uint64
	for _, r := range env.c.inputs {
		if !r.parked {
			t.Errorf("input #%d waits unparked", r.entry.Seq)
		}
		kept = append(kept, r.entry.Seq)
	}
	env.c.mu.Unlock()
	if want := private[2:]; !slices.Equal(kept, want) {
		t.Fatalf("parked inputs = %v, want the newest three %v", kept, want)
	}

	env.say("a shared question")
	env.idle()
	if n := len(cloud.activations()); n != 1 {
		t.Fatalf("the shared query must reach the cloud: %d activations", n)
	}
	if containsText(cloud.seenTexts(), "private") {
		t.Fatal("a parked edge-only input reached the cloud")
	}
}

// TestCoordinatorEntriesWaitBehindAnOpenBacklog: while a Transfer's backlog
// is open, its entries wait for the events shown before it; entries committed
// meanwhile wait behind them, so entry events stay in log order.
func TestCoordinatorEntriesWaitBehindAnOpenBacklog(t *testing.T) {
	edge, cloud := edgeCloud()
	env := newCoEnv(t, nil, nil, edge, cloud)
	reply := Entry{Seq: 10, Kind: KindReply, Origin: Origin{Participant: "edge"}, Visibility: Shared, Content: textBlocks("before the transfer")}
	later := markerEntry(MarkerNoOwner, 9, "committed while the backlog is open")
	later.Seq = 11
	c := env.c
	c.mu.Lock()
	a := &activation{backlog: make(chan struct{}), held: []Entry{reply}, eventsBefore: 2}
	c.open = a
	c.emitEntriesLocked([]Entry{later})
	c.flushBacklogLocked(a, true)
	c.mu.Unlock()
	var seqs []uint64
	for _, ev := range env.events() {
		if ev.Kind == EventEntry {
			seqs = append(seqs, ev.Entry.Seq)
		}
	}
	if !slices.Equal(seqs, []uint64{10, 11}) {
		t.Fatalf("entry events in order %v, want [10 11]", seqs)
	}
	if c.open != nil {
		t.Fatal("the flush must close the open backlog")
	}
}

// TestCoordinatorInterruptedSuccessorEndsTheBacklog: when the owner a Transfer
// handed to is interrupted while it waits for the old owner's pre-transfer
// events, the rest of them are no longer shown, so nothing of the old owner
// can follow a later owner's events. The Transfer's reply entry still
// carries the whole text.
func TestCoordinatorInterruptedSuccessorEndsTheBacklog(t *testing.T) {
	edge, cloud := edgeCloud()
	edge.buf = 16
	var said []string
	for i := 0; i < 10; i++ {
		said = append(said, "d"+strconv.Itoa(i))
	}
	transferred := make(chan struct{})
	edge.push(func(r *fakeRun) *ActivationEnd {
		for _, s := range said {
			r.say(s)
		}
		if _, err := r.host().Transfer(r.ctx, TransferRequest{From: Origin{Participant: "edge"}, Epoch: r.act.Epoch,
			To: "cloud", Brief: testBrief(), Text: r.text(), EventsBefore: len(said)}); err != nil {
			t.Errorf("transfer: %v", err)
		}
		close(transferred)
		return &ActivationEnd{Reason: types.ReplyHandedOff}
	})
	cloud.push(replyWith("cloud answer"))
	store := NewMemoryStore()
	c, err := NewCoordinator(context.Background(), Config{
		Store: store, Participants: []Participant{edge, cloud}, Limits: Limits{EventBuffer: 1},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close() }()
	if _, err := c.Submit(context.Background(), Query{Text: "go", Explicit: "edge"}); err != nil {
		t.Fatal(err)
	}
	// The user reads nothing yet, so at most the first few deltas fit in the
	// buffers when the Transfer commits; the rest wait in the backlog.
	<-transferred
	c.Interrupt()
	// Wait until the interrupted successor has given up (its marker is in
	// the log) before the user starts reading.
	deadline := time.Now().Add(testTimeout)
	for len(findEntries(readAll(t, store), KindMarker, MarkerInterrupted)) == 0 {
		if time.Now().After(deadline) {
			t.Fatal("the interrupted successor never ended")
		}
		time.Sleep(time.Millisecond)
	}
	if _, err := c.Submit(context.Background(), Query{Text: "next", Explicit: "cloud"}); err != nil {
		t.Fatal(err)
	}
	var edgeDeltas, order []string
	reply := ""
	for ev := range c.Events() {
		if ev.Kind == EventAgent {
			d := ev.Agent.(event.TextBlockDeltaEvent).Delta
			order = append(order, ev.Participant)
			if ev.Participant == "edge" {
				edgeDeltas = append(edgeDeltas, d)
			}
		}
		if ev.Kind == EventEntry && ev.Entry.Kind == KindReply && ev.Participant == "edge" {
			reply = ev.Entry.Text()
		}
		if ev.Kind == EventEntry && ev.Entry.Kind == KindReply && ev.Participant == "cloud" {
			break
		}
	}
	if len(edgeDeltas) >= len(said) || !slices.Equal(edgeDeltas, said[:len(edgeDeltas)]) {
		t.Fatalf("the edge deltas shown after the interrupt = %q; the backlog must end with the wait", edgeDeltas)
	}
	if last := order[len(order)-1]; last != "cloud" || slices.Index(order, "cloud") != len(edgeDeltas) {
		t.Fatalf("an old owner's event followed the new owner's: %q", order)
	}
	if reply != strings.Join(said, "") {
		t.Fatalf("the transfer's reply entry = %q", reply)
	}
}

func readAll(t *testing.T, s Store) []Entry {
	t.Helper()
	got, err := s.Read(context.Background(), 0, Filter{}, 0)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

// TestCoordinatorRecoveryKeepsTheDisclosure: a waiting input restored after a
// restart keeps the disclosure Submit resolved; an entry without one falls
// back to its visibility.
func TestCoordinatorRecoveryKeepsTheDisclosure(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   Entry
		want Disclosure
	}{
		{"unclassified", Entry{Kind: KindUserInput, Visibility: Shared, Disclosure: DiscloseUnclassified}, DiscloseUnclassified},
		{"shared", Entry{Kind: KindUserInput, Visibility: Shared, Disclosure: DiscloseShared}, DiscloseShared},
		{"no disclosure", Entry{Kind: KindUserInput, Visibility: Shared}, DiscloseShared},
		{"edge only", Entry{Kind: KindUserInput, Visibility: EdgeOnly, Disclosure: DiscloseEdgeOnly}, DiscloseEdgeOnly},
		{"contradicting", Entry{Kind: KindUserInput, Visibility: EdgeOnly, Disclosure: DiscloseShared}, DiscloseEdgeOnly},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := NewMemoryStore(WithClock(fixedClock()))
			tc.in.Content = textBlocks("a question")
			mustAppend(t, store, nil, tc.in)
			edge, cloud := edgeCloud()
			env := newCoEnv(t, store, nil, edge, cloud)
			env.c.mu.Lock()
			defer env.c.mu.Unlock()
			if len(env.c.inputs) != 1 || env.c.inputs[0].q.Disclosure != tc.want || !env.c.inputs[0].parked {
				t.Fatalf("restored inputs = %+v, want disclosure %q", env.c.inputs, tc.want)
			}
		})
	}
	// Submit records the resolved disclosure.
	edge, cloud := edgeCloud()
	env := newCoEnv(t, nil, nil, edge, cloud)
	seq := env.submit(Query{Text: "hello"})
	env.idle()
	if e, _, _ := env.store.Get(context.Background(), seq); e.Disclosure != DiscloseUnclassified {
		t.Fatalf("stored disclosure = %q", e.Disclosure)
	}
}

// TestCoordinatorFailedClearRoundKeepsOwnReplies: a clear whose reset round
// was applied but whose later round failed is not done. The reset dropped
// the edge's own replies above the round's cursor, and only a clear sends
// own entries, so the next takeover clears again instead of importing
// incrementally from the lowered cursor.
func TestCoordinatorFailedClearRoundKeepsOwnReplies(t *testing.T) {
	edge := smallEdge()
	edge.state.Cursor = 10
	var once sync.Once
	edge.onCompact = func() {
		once.Do(func() {
			edge.mu.Lock()
			edge.syncErr = errors.New("checkpoint save failed")
			edge.mu.Unlock()
		})
	}
	_, cloud := edgeCloud()
	env := newCoEnv(t, edgeEraStore(t), nil, edge, cloud)
	env.submit(Query{Text: "back to the edge", Explicit: "edge"})
	env.idle()
	if n := len(edge.activations()); n != 0 {
		t.Fatalf("the failed clear must not start a reply: %d", n)
	}
	first := len(edge.appliedSyncs())

	edge.mu.Lock()
	edge.syncErr = nil
	edge.mu.Unlock()
	env.submit(Query{Text: "edge again", Explicit: "edge"})
	env.idle()
	again := edge.appliedSyncs()[first:]
	if len(again) == 0 || !again[0].Reset {
		t.Fatalf("the next takeover must clear again: %+v", again)
	}
	var got []string
	for i := range again {
		got = append(got, syncTexts(&again[i])...)
	}
	joined := strings.Join(got, "\n")
	for n := 3; n <= 10; n++ {
		if !strings.Contains(joined, "[#"+strconv.Itoa(n)+" reply from edge]") {
			t.Fatalf("the edge's own reply #%d was lost after the failed clear:\n%s", n, joined)
		}
	}
	if n := len(edge.activations()); n != 1 {
		t.Fatalf("the edge must answer after the second clear: %d activations", n)
	}
}

// TestCoordinatorFailedBackgroundClearKeepsOwnReplies is the same for a clear
// inside a background task: the failed task's report is written in the
// edge's name and moves its activity past the summary, yet the unfinished
// clear is still repeated at the next takeover.
func TestCoordinatorFailedBackgroundClearKeepsOwnReplies(t *testing.T) {
	edge := smallEdge()
	edge.state.Cursor = 10
	var once sync.Once
	edge.onCompact = func() {
		once.Do(func() {
			edge.mu.Lock()
			edge.syncErr = errors.New("checkpoint save failed")
			edge.mu.Unlock()
		})
	}
	_, cloud := edgeCloud()
	cloud.push(func(r *fakeRun) *ActivationEnd {
		if _, _, err := r.assign("edge", "check the photos", false); err != nil {
			t.Errorf("assign: %v", err)
		}
		return r.reply("assigned")
	})
	env := newCoEnv(t, edgeEraStore(t), nil, edge, cloud)
	env.submit(Query{Text: "next question", Explicit: "cloud"})
	env.idle()
	syncs := edge.appliedSyncs()
	if n := len(edge.activations()); n != 0 || len(syncs) != 1 || !syncs[0].Reset {
		t.Fatalf("want the task's clear to fail after its reset round: %d activations, syncs %+v", n, syncs)
	}
	var failed bool
	for _, e := range env.log() {
		if e.Kind == KindReport && e.Origin.Participant == "edge" {
			failed = true
		}
	}
	if !failed {
		t.Fatal("the failed task must be reported in the edge's name")
	}

	edge.mu.Lock()
	edge.syncErr = nil
	edge.mu.Unlock()
	env.submit(Query{Text: "edge again", Explicit: "edge"})
	env.idle()
	again := edge.appliedSyncs()[1:]
	if len(again) == 0 || !again[0].Reset {
		t.Fatalf("the next takeover must clear again: %+v", again)
	}
	var got []string
	for i := range again {
		got = append(got, syncTexts(&again[i])...)
	}
	joined := strings.Join(got, "\n")
	for n := 3; n <= 10; n++ {
		if !strings.Contains(joined, "[#"+strconv.Itoa(n)+" reply from edge]") {
			t.Fatalf("the edge's own reply #%d was lost after the failed background clear:\n%s", n, joined)
		}
	}
	env.c.mu.Lock()
	pending := env.c.parts["edge"].clearPending
	env.c.mu.Unlock()
	if pending != nil {
		t.Fatal("the finished clear must not stay pending")
	}
}
