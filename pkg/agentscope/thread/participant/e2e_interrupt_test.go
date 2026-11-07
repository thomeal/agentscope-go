package participant_test

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/agentscope-ai/agentscope-go/v2/pkg/agentscope/agent"
	"github.com/agentscope-ai/agentscope-go/v2/pkg/agentscope/event"
	"github.com/agentscope-ai/agentscope-go/v2/pkg/agentscope/thread"
	"github.com/agentscope-ai/agentscope-go/v2/pkg/agentscope/thread/participant"
)

// This file covers the end of an interrupted activation, the user's stream
// around a transfer, the privacy of edge tool outputs and restarts, over the
// session of e2e_test.go.

// blockingPolicy is the keyword policy, except that it holds any text with
// marker until release closes: a disclosure check the test can block.
type blockingPolicy struct {
	marker  string
	entered chan struct{}
	release chan struct{}
	enter   sync.Once
	open    sync.Once
}

func newBlockingPolicy(marker string) *blockingPolicy {
	return &blockingPolicy{marker: marker, entered: make(chan struct{}), release: make(chan struct{})}
}

func (p *blockingPolicy) Classify(ctx context.Context, text string) (thread.Visibility, error) {
	if strings.Contains(text, p.marker) {
		p.enter.Do(func() { close(p.entered) })
		<-p.release
	}
	return canaryPolicy{}.Classify(ctx, text)
}

func (p *blockingPolicy) unblock() { p.open.Do(func() { close(p.release) }) }

// interrupted checks that the activation answering log's query ended
// interrupted: one interrupted marker (an unknown_effects marker may follow).
func interrupted(t *testing.T, log []thread.Entry) {
	t.Helper()
	n := 0
	markers := find(log, thread.KindMarker, "")
	for i := range markers {
		if markers[i].Marker.Kind == thread.MarkerInterrupted {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("the activation must end interrupted once:%s", describe(log))
	}
}

func waitClosed(t *testing.T, ch <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(20 * time.Second):
		t.Fatalf("timed out waiting for %s", what)
	}
}

// checkNotDone answers the edge's next request after an interrupted write
// that wrote nothing: the request must say so, whether the agent recorded
// the participant's answer or a note carries it.
func checkNotDone(t *testing.T, tool string) turn {
	return inspect(func(c *wireCall) {
		text := c.text()
		if !strings.Contains(text, tool+" was not done: the reply was interrupted before the call was recorded, so nothing was written.") {
			t.Errorf("the next request does not say the %s was not done:\n%s", tool, text)
		}
		if strings.Contains(text, "External execution timed out") && !strings.Contains(text, "[Your "+tool+" call was interrupted before its answer was recorded.") {
			t.Errorf("the agent recorded no answer and got no note:\n%s", text)
		}
	}, say("EDGE: nothing was sent."))
}

// TestE2EInterruptedTransferIsReportedNotDone interrupts a private edge
// while the Coordinator's disclosure check of its transfer brief blocks. The
// Coordinator refuses the transfer as interrupted, nothing is written, and
// the edge's next request says the transfer was not done.
func TestE2EInterruptedTransferIsReportedNotDone(t *testing.T) {
	policy := newBlockingPolicy("BLOCK-ME")
	defer policy.unblock()
	s := newSession(t, &sessionOptions{disclosure: policy})
	s.edge.script(
		act("", toolCall("door_lock", map[string]any{"code": canary})),
		act("", toolCall(participant.ToolTransfer, map[string]any{"to": "cloud", "goal": "BLOCK-ME plan the trip", "remaining": []any{"plan it"}})),
	)
	q := s.submit(&thread.Query{Text: "Set the door code, then let the cloud plan my trip.", Disclosure: thread.DiscloseShared, Explicit: "edge"})
	waitClosed(t, policy.entered, "the disclosure check of the transfer brief")
	s.c.Interrupt()
	policy.unblock()
	s.settle()

	log := s.since(q)
	if got := find(log, thread.KindOwnerChange, "edge"); len(got) != 0 {
		t.Fatalf("an interrupted transfer wrote an owner change:%s", describe(got))
	}
	interrupted(t, log)

	if owner, _ := s.c.Owner(); owner != "edge" {
		t.Fatalf("owner = %s", owner)
	}
	s.edge.script(checkNotDone(t, participant.ToolTransfer))
	s.submit(&thread.Query{Text: "Did the cloud get it?", Disclosure: thread.DiscloseShared, Explicit: "edge"})
	s.settle()
}

// TestE2EInterruptedAssignIsReportedNotDone is the same for assign_task: the
// disclosure check of the task text blocks, the Coordinator refuses the task
// as interrupted, and no task starts.
func TestE2EInterruptedAssignIsReportedNotDone(t *testing.T) {
	policy := newBlockingPolicy("BLOCK-ME")
	defer policy.unblock()
	s := newSession(t, &sessionOptions{disclosure: policy})
	s.edge.script(
		act("", toolCall("door_lock", map[string]any{"code": canary})),
		act("", toolCall(participant.ToolAssignTask, map[string]any{"to": "cloud", "task": "BLOCK-ME look up the weather"})),
	)
	q := s.submit(&thread.Query{Text: "Set the door code, then have the cloud check the weather.", Disclosure: thread.DiscloseShared, Explicit: "edge"})
	waitClosed(t, policy.entered, "the disclosure check of the task")
	s.c.Interrupt()
	policy.unblock()
	s.settle()

	log := s.since(q)
	if got := find(log, thread.KindAssign, ""); len(got) != 0 {
		t.Fatalf("an interrupted assign wrote a task:%s", describe(got))
	}
	interrupted(t, log)

	if s.master.count("") != 0 {
		t.Fatal("no task may reach the cloud")
	}
	s.edge.script(checkNotDone(t, participant.ToolAssignTask))
	s.submit(&thread.Query{Text: "Is the weather check running?", Disclosure: thread.DiscloseShared, Explicit: "edge"})
	s.settle()
}

// hookedEdge wraps the edge so a hook runs right after each Assign the
// Coordinator committed for it.
type hookedEdge struct {
	*participant.AgentParticipant
	afterAssign func(ctx context.Context, seq uint64)
	host        *hookedHost
}

type hookedHost struct {
	thread.Host
	e *hookedEdge
}

func (h *hookedHost) Assign(ctx context.Context, req thread.AssignRequest) (string, uint64, error) { //nolint:gocritic // the thread.Host contract
	id, seq, err := h.Host.Assign(ctx, req)
	if err == nil && h.e.afterAssign != nil {
		h.e.afterAssign(ctx, seq)
	}
	return id, seq, err
}

func (e *hookedEdge) Bind(h thread.Host) error {
	e.host = &hookedHost{Host: h, e: e}
	return e.AgentParticipant.Bind(e.host)
}

func (e *hookedEdge) Release(thread.Host) { e.AgentParticipant.Release(e.host) }

// TestE2ECommittedAssignNoteAfterInterrupt interrupts the edge right after
// the Coordinator committed its assign_task, once the agent stopped waiting
// for the answer. The task stands in the log, and the edge's next request
// names the committed entry instead of only "no result".
func TestE2ECommittedAssignNoteAfterInterrupt(t *testing.T) {
	var s *session
	var assignSeq uint64
	edge := &hookedEdge{}
	edge.afterAssign = func(ctx context.Context, seq uint64) {
		assignSeq = seq
		edge.afterAssign = nil
		s.c.Interrupt()
		<-ctx.Done()
		// Wait until the agent has given up on the answer: an empty update
		// fails only while its reply runs.
		deadline := time.Now().Add(10 * time.Second)
		for s.edgeAgent.ApplyThreadUpdate(context.Background(), agent.ThreadUpdate{}) != nil && time.Now().Before(deadline) {
			time.Sleep(time.Millisecond)
		}
	}
	s = newSession(t, &sessionOptions{wrapEdge: func(p *participant.AgentParticipant) thread.Participant {
		edge.AgentParticipant = p
		return edge
	}})
	s.edge.script(act("", toolCall(participant.ToolAssignTask, map[string]any{"to": "cloud", "task": "Look up the weather in Lisbon."})))
	s.master.script(say("TASK: sunny."))
	q := s.submit(&thread.Query{Text: "Have the cloud check the weather.", Disclosure: thread.DiscloseShared, Explicit: "edge"})
	s.settle()

	log := s.since(q)
	task := one(t, find(log, thread.KindAssign, "edge"), "assign")
	if task.Seq != assignSeq {
		t.Fatalf("assign #%d, hook saw #%d", task.Seq, assignSeq)
	}
	interrupted(t, log)

	want := fmt.Sprintf("[Your assign_task call was committed as entry #%d before the reply was interrupted: task %s went to cloud.]", assignSeq, task.Task.ID)
	s.edge.script(inspect(func(c *wireCall) {
		text := c.text()
		if !strings.Contains(text, "External execution timed out") || !strings.Contains(text, want) {
			t.Errorf("the next request must name the committed task %q:\n%s", want, text)
		}
	}, say("EDGE: the cloud is on it.")))
	s.submit(&thread.Query{Text: "Is the weather check running?", Disclosure: thread.DiscloseShared, Explicit: "edge"})
	s.settle()
	if n := len(find(s.log(), thread.KindAssign, "")); n != 1 {
		t.Fatalf("the task must not be assigned again: %d assigns", n)
	}
}

// TestE2ETextBeforeTransferReachesTheUser holds the user's stream while the
// cloud shows text and transfers, with room for one event only. The text the
// transfer seals still reaches the user, before the transfer's entries and
// before anything of the new owner.
func TestE2ETextBeforeTransferReachesTheUser(t *testing.T) {
	gate := make(chan struct{})
	var once sync.Once
	open := func() { once.Do(func() { close(gate) }) }
	defer open()
	s := newSession(t, &sessionOptions{limits: thread.Limits{EventBuffer: 1}, eventGate: gate})
	s.master.script(act("SHOWN-BEFORE: the lamp is a device task.", toolCall(participant.ToolTransfer, map[string]any{
		"to": "edge", "goal": "Turn on the lamp", "remaining": []any{"switch the lamp on"}})))
	s.edge.script(say("EDGE: the lamp is on."))
	q := s.submit(&thread.Query{Text: "Turn on the lamp.", Disclosure: thread.DiscloseShared, Explicit: "cloud"})
	deadline := time.Now().Add(20 * time.Second)
	for len(find(s.since(q), thread.KindOwnerChange, "cloud")) == 0 {
		if time.Now().After(deadline) {
			t.Fatal("the cloud never transferred")
		}
		time.Sleep(time.Millisecond)
	}
	open()
	s.settle()

	transfer := one(t, find(s.since(q), thread.KindOwnerChange, "cloud"), "transfer")
	textAt, transferAt, edgeAt := -1, -1, -1
	for i, ev := range s.events.all() {
		switch {
		case ev.Kind == thread.EventAgent && ev.Participant == "cloud":
			if d, ok := ev.Agent.(event.TextBlockDeltaEvent); ok && textAt < 0 && strings.Contains(d.Delta, "SHOWN-BEFORE") {
				textAt = i
				if !ev.Foreground {
					t.Fatalf("the text before the transfer reached the user as background: %+v", ev)
				}
			}
		case ev.Kind == thread.EventEntry && ev.Entry.Seq == transfer.Seq:
			transferAt = i
		case ev.Kind == thread.EventAgent && ev.Participant == "edge" && edgeAt < 0:
			edgeAt = i
		}
	}
	if textAt < 0 || transferAt < textAt || edgeAt < transferAt {
		t.Fatalf("order: text at %d, transfer at %d, the edge's first event at %d", textAt, transferAt, edgeAt)
	}
}

// TestE2EPrivateEdgeOutputsStayOnTheEdge lets the cloud read edge tool
// outputs. The edge's private tool output, and every output after it in the
// same activation, is edge-only: the cloud's event_read never returns them.
// (An output captured while the agent already ran ahead into a private tool
// is edge-only too, so the shared output comes from an earlier activation.)
func TestE2EPrivateEdgeOutputsStayOnTheEdge(t *testing.T) {
	s := newSession(t, &sessionOptions{cloudReadsEdge: true})
	s.edge.script(
		act("", toolCall("device_control", map[string]any{"device": "hall light", "state": "on"})),
		say("The hall light is on."),
	)
	s.submit(&thread.Query{Text: "Turn the hall light on.", Disclosure: thread.DiscloseShared, Explicit: "edge"})
	s.settle()
	s.edge.script(
		act("", toolCall("door_lock", map[string]any{"code": canary})),
		act("", toolCall("device_control", map[string]any{"device": "bedroom fan", "state": "off"})),
		say("Done."),
	)
	s.submit(&thread.Query{Text: "Set my door code, then turn the fan off.", Disclosure: thread.DiscloseShared, Explicit: "edge"})
	s.settle()
	if got := s.evList.Outline(thread.ZoneCloud, true, 0); len(got) != 1 || !strings.Contains(got[0].FirstLine, "hall light") {
		t.Fatalf("the cloud may read only the output before the private one: %+v", got)
	}

	s.master.script(
		act("", toolCall(participant.ToolEventRead, map[string]any{"action": "outline"})),
		inspect(func(c *wireCall) {
			if !strings.Contains(c.text(), "hall light is now on") {
				t.Errorf("the outline misses the shared output:\n%s", c.text())
			}
		}, act("", toolCall(participant.ToolEventRead, map[string]any{"action": "search", "query": "door"}))),
		act("", toolCall(participant.ToolEventRead, map[string]any{"action": "page", "id": "ev-2"})),
		act("", toolCall(participant.ToolEventRead, map[string]any{"action": "page", "id": "ev-3"})),
		say("CLOUD: the hall light is on."),
	)
	s.submit(&thread.Query{Text: "What did the devices do?", Disclosure: thread.DiscloseShared, Explicit: "cloud"})
	s.settle()
	for i, c := range s.master.requests("") {
		for _, secret := range []string{canary, "front door code", "bedroom fan is now off"} {
			if strings.Contains(c.raw, secret) {
				t.Fatalf("master request %d holds %q:\n%s", i, secret, c.text())
			}
		}
	}
}

// TestE2EAgentParticipantRestart restarts a session whose edge agent the
// host restores from its checkpoint. The rebuilt participant reports the
// restored cursor, and the edge's next request holds the earlier question
// and its own answer once each.
func TestE2EAgentParticipantRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "thread.jsonl")
	first, err := thread.OpenFileStore(path)
	if err != nil {
		t.Fatal(err)
	}
	s := newSession(t, &sessionOptions{store: first, persistEdge: true})
	s.edge.script(say("EDGE-ANSWER-1: the lamp is on."))
	q := s.submit(&thread.Query{Text: "Q-EDGE-1: turn on the lamp.", Disclosure: thread.DiscloseShared, Explicit: "edge"})
	s.settle()
	if st := s.edgeP.State(); st.Cursor < q {
		t.Fatalf("edge state after the first query = %+v", st)
	}
	cursor := s.edgeP.State().Cursor
	if err := s.c.Close(); err != nil {
		t.Fatal(err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}

	second, err := thread.OpenFileStore(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = second.Close() })
	s.rebuild(second)
	if st := s.edgeP.State(); st.Cursor != cursor {
		t.Fatalf("the restored edge reports cursor %d, want %d", st.Cursor, cursor)
	}
	s.edge.script(inspect(func(c *wireCall) {
		text := c.text()
		for _, once := range []string{"Q-EDGE-1: turn on the lamp.", "EDGE-ANSWER-1: the lamp is on."} {
			if n := strings.Count(text, once); n != 1 {
				t.Errorf("the request after the restart holds %q %d times:\n%s", once, n, text)
			}
		}
	}, say("EDGE-ANSWER-2: it is still on.")))
	s.submit(&thread.Query{Text: "Q-EDGE-2: is the lamp still on?", Disclosure: thread.DiscloseShared, Explicit: "edge"})
	s.settle()
}
