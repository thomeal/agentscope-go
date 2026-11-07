package thread

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/agentscope-ai/agentscope-go/v2/pkg/agentscope/event"
	"github.com/agentscope-ai/agentscope-go/v2/pkg/agentscope/types"
)

// actKind says what started an activation.
type actKind int

const (
	actUser     actKind = iota + 1 // a routed user_input
	actTransfer                    // a Transfer's owner_change
	actFailover                    // a failover owner_change
	actAssign                      // a background task
	actWake                        // a Notify report waking the idle owner
)

var (
	errUnavailable = errors.New("thread: participant is unavailable")
	errNoEnd       = errors.New("thread: activation stream closed without an end item")
)

// activation is one queued or running activation. The fields after the
// first group are guarded by Coordinator.mu.
type activation struct {
	id         string
	part       *partState
	kind       actKind
	trigger    Entry
	foreground bool
	epoch      uint64
	depth      int
	task       *taskRec
	failFrom   string
	failReason string
	userText   string
	// cycleInput is the user_input this activation answers: the trigger of
	// a routed input, passed on through Transfer and failover; 0 for wakes
	// and tasks. Only such an activation fails over.
	cycleInput uint64
	// pred is the activation whose Transfer started this one while events
	// it showed before the Transfer were still unread; this one starts only
	// once they are forwarded.
	pred   *activation
	ctx    context.Context
	cancel context.CancelFunc

	started  bool
	handedTo string // set when a Transfer of this activation sealed it
	limited  bool   // Pending is budgeted
	budget   int    // tokens Pending may still deliver when limited
	// eventsBefore is the TransferRequest.EventsBefore of the Transfer that
	// sealed it; forwarded counts the Event items processed so far. While
	// forwarded < eventsBefore after the Transfer, backlog is open and held
	// keeps the Transfer's entries, emitted once the backlog is forwarded.
	eventsBefore int
	forwarded    int
	backlog      chan struct{}
	held         []Entry
	flushed      bool
	streamDone   bool // its stream ended; nothing more will be forwarded
}

// newActivationLocked creates an activation at the current epoch.
func (c *Coordinator) newActivationLocked(ps *partState, kind actKind, trigger *Entry, foreground bool, depth int) *activation {
	c.nextAct++
	a := &activation{
		id:         fmt.Sprintf("%s-%d", c.runID, c.nextAct),
		part:       ps,
		kind:       kind,
		trigger:    cloneEntry(trigger),
		foreground: foreground,
		epoch:      c.epoch,
		depth:      depth,
	}
	a.ctx, a.cancel = context.WithCancel(c.ctx)
	return a
}

// startForegroundLocked makes a the foreground activation and queues it.
func (c *Coordinator) startForegroundLocked(a *activation) {
	c.fg = a
	c.fgCancel = a.cancel
	c.enqueueActivationLocked(a)
}

// enqueueActivationLocked queues a on its participant: foreground work
// first, then background work, each in arrival order.
func (c *Coordinator) enqueueActivationLocked(a *activation) {
	c.addWorkLocked()
	ps := a.part
	if a.foreground {
		i := 0
		for i < len(ps.queue) && ps.queue[i].foreground {
			i++
		}
		ps.queue = slices.Insert(ps.queue, i, a)
	} else {
		ps.queue = append(ps.queue, a)
	}
	c.kickLocked(ps)
}

// kickLocked starts the participant's next queued activation when it is
// idle.
func (c *Coordinator) kickLocked(ps *partState) {
	if c.closed || ps.running != nil || ps.maint || len(ps.queue) == 0 {
		return
	}
	a := ps.queue[0]
	ps.queue[0] = nil
	ps.queue = ps.queue[1:]
	ps.running = a
	c.wg.Add(1)
	go c.runActivation(a)
}

// holdsForegroundLocked reports whether a is the foreground activation of
// the current owner at the current epoch.
func (c *Coordinator) holdsForegroundLocked(a *activation) bool {
	return a.foreground && c.fg == a && c.owner == a.part.name && c.epoch == a.epoch
}

// idleOwnerLocked reports whether ps is the owner with nothing running or
// queued while the foreground is free.
func (c *Coordinator) idleOwnerLocked(ps *partState) bool {
	return !c.closed && c.owner == ps.name && ps.running == nil && len(ps.queue) == 0 &&
		!ps.maint && c.fg == nil && !c.routing
}

// runActivation runs one activation from import to settlement.
func (c *Coordinator) runActivation(a *activation) {
	defer c.wg.Done()
	defer a.cancel()
	end, started := c.execute(a)
	c.finish(a, end, started)
}

// execute prepares and runs the activation and returns its end. started is
// false when it ended before Activate was called.
func (c *Coordinator) execute(a *activation) (*ActivationEnd, bool) {
	ps := a.part
	if p := a.pred; p != nil {
		// The previous owner's events shown before its Transfer go first.
		select {
		case <-p.backlog:
		case <-a.ctx.Done():
			// Interrupted while waiting: the rest of the backlog is no
			// longer shown, so nothing of the old owner can follow a later
			// owner's events.
			c.mu.Lock()
			c.flushBacklogLocked(p, true)
			c.mu.Unlock()
		}
	}
	if err := a.ctx.Err(); err != nil {
		return &ActivationEnd{Reason: types.ReplyInterrupted, Err: err}, false
	}
	if !c.available(ps.name) {
		return &ActivationEnd{Reason: types.ReplyError, Err: errUnavailable}, false
	}
	if !ps.desc.Zone.CanRead(a.trigger.EffectiveVisibility()) {
		// Never reached through the Coordinator's own paths; kept so an
		// edge-only trigger can never become a cloud input.
		return &ActivationEnd{Reason: types.ReplyError, Err: fmt.Errorf("thread: %s may not read its trigger #%d", ps.name, a.trigger.Seq)}, false
	}
	st := ps.p.State()
	im, err := c.prepareImport(a.ctx, a, st)
	if cerr := a.ctx.Err(); cerr != nil {
		// Interrupted while importing: what was applied stays applied (the
		// participant's cursor moved with it), but the reply never starts.
		return &ActivationEnd{Reason: types.ReplyInterrupted, Err: cerr}, false
	}
	if err != nil {
		return &ActivationEnd{Reason: types.ReplyError, Err: err}, false
	}
	input := c.inputText(a)

	c.mu.Lock()
	a.started = true
	prevSeen := ps.seen
	ps.seen = max(ps.seen, a.trigger.Seq)
	if im.sync != nil {
		ps.seen = max(ps.seen, im.sync.Cursor)
	}
	newSeen := ps.seen
	if im.budget > 0 {
		a.limited = true
		a.budget = im.budget - im.tokens
	}
	c.mu.Unlock()

	ch, err := ps.p.Activate(a.ctx, Activation{
		Epoch:      a.epoch,
		Foreground: a.foreground,
		Trigger:    cloneEntry(&a.trigger),
		Input:      input,
		Sync:       im.sync,
		Depth:      a.depth,
	})
	if err != nil {
		c.mu.Lock()
		if ps.seen == newSeen {
			// Nothing was delivered: entries in that range must still be
			// found by the next check for addressed entries.
			ps.seen = prevSeen
		}
		c.mu.Unlock()
		return &ActivationEnd{Reason: types.ReplyError, Err: fmt.Errorf("thread: activate %s: %w", ps.name, err)}, true
	}
	c.mu.Lock()
	// The reply started, so its Sync was applied: a clear it carried is now
	// in force, and a foreground activation holds everything up to its
	// cursor.
	if im.clear != nil {
		c.commitClearLocked(ps, im.clear, true)
	}
	if c.holdsForegroundLocked(a) {
		through := max(st.Cursor, a.trigger.Seq)
		if im.sync != nil {
			through = max(through, im.sync.Cursor)
		}
		ps.deliveredNotifyLocked(through)
	}
	c.mu.Unlock()

	var end *ActivationEnd
	n := 0
	for ev := range ch {
		switch {
		case ev.End != nil:
			if end == nil {
				end = ev.End
			}
		case ev.Event != nil:
			n++
			c.forwardAgent(a, &ev, n)
		}
	}
	c.mu.Lock()
	// The stream is over: whatever of a backlog was not forwarded never will
	// be, so the Transfer's entries and the new owner may go on.
	a.streamDone = true
	c.flushBacklogLocked(a, true)
	c.mu.Unlock()
	if end == nil {
		end = &ActivationEnd{Reason: types.ReplyError, Err: errNoEnd}
	}
	return end, true
}

// forwardAgent shows the n-th event of an activation to the user: every
// event of the activation holding the foreground, and of one whose Transfer
// moved the foreground the events it showed before the Transfer
// (TransferRequest.EventsBefore). Of other activations only confirmation
// requests pass, with Foreground false. It waits for room in the outbox
// first; the foreground check and the queueing are one step, so once the
// epoch moves nothing else of the old activation is queued.
func (c *Coordinator) forwardAgent(a *activation, ev *ActivationEvent, n int) {
	confirm := ev.Event.GetEventType() == event.EventRequireUserConfirm
	for {
		c.mu.Lock()
		if c.closed {
			c.mu.Unlock()
			return
		}
		if len(c.outbox) < c.cfg.Limits.EventBuffer {
			fg := c.holdsForegroundLocked(a) || (a.backlog != nil && !a.flushed && n <= a.eventsBefore)
			if fg || confirm {
				c.enqueueLocked(&Event{Kind: EventAgent, Participant: a.part.name, Path: ev.Path, Epoch: a.epoch, Foreground: fg, Agent: ev.Event})
			}
			a.forwarded = n
			c.flushBacklogLocked(a, false)
			c.mu.Unlock()
			return
		}
		space := c.space
		c.mu.Unlock()
		select {
		case <-space:
		case <-c.ctx.Done():
			return
		}
	}
}

// flushBacklogLocked ends the backlog of a transferred activation once the
// events it showed before its Transfer are forwarded, or at once when force
// is set: it emits the Transfer's entries and lets the new owner start.
func (c *Coordinator) flushBacklogLocked(a *activation, force bool) {
	if a.backlog == nil || a.flushed || (!force && a.forwarded < a.eventsBefore) {
		return
	}
	a.flushed = true
	if c.open == a {
		c.open = nil
	}
	held := a.held
	a.held = nil
	c.emitEntriesLocked(held)
	close(a.backlog)
}

// commitClearLocked records a clear whose reset the participant applied:
// the summary it now holds, and its privacy, lowered only when nothing set
// it since the import marked it. done marks the clear finished (resetFrom),
// so the same summary is not applied again; it is set once every round of
// the clear reached the participant.
func (c *Coordinator) commitClearLocked(ps *partState, cc *clearCommit, done bool) {
	if ps.privGen == cc.gen {
		ps.private = cc.private
	}
	ps.heldCoverage = cloneCoverage(cc.coverage)
	if done {
		ps.resetFrom = cc.seq
		ps.clearPending = nil
	} else {
		ps.clearPending = cc
	}
}

// deliveredNotifyLocked drops the Notify reports up to through from the
// reports waiting for a foreground activation of ps.
func (ps *partState) deliveredNotifyLocked(through uint64) {
	for seq := range ps.notify {
		if seq <= through {
			delete(ps.notify, seq)
		}
	}
}

// inputText is the text the participant replies to.
func (c *Coordinator) inputText(a *activation) string {
	t := &a.trigger
	switch a.kind {
	case actTransfer:
		var from string
		var brief *Brief
		if t.Owner != nil {
			from, brief = t.Owner.From, t.Owner.Brief
		}
		return "[The foreground was handed to you by " + from + "]\n" + brief.Render() + "\nContinue with the user from here."
	case actFailover:
		return fmt.Sprintf("[%s failed: %s. Continue the user's request below.]\n%s", a.failFrom, a.failReason, a.userText)
	case actAssign:
		id, origin := "", t.Origin.Participant
		if t.Task != nil {
			id = t.Task.ID
		}
		return fmt.Sprintf("[Task %s from %s]\n%s\nYour final answer is reported back to %s automatically.", id, origin, t.Text(), origin)
	case actWake:
		return renderEntry(t)
	default:
		return t.Text()
	}
}

// endReason normalizes an end's reason.
func endReason(end *ActivationEnd) types.ReplyFinishedReason {
	switch end.Reason {
	case types.ReplyCompleted, types.ReplyExceedMaxIters, types.ReplyInterrupted, types.ReplyHandedOff, types.ReplyError:
		return end.Reason
	}
	if end.Err != nil {
		return types.ReplyError
	}
	return types.ReplyCompleted
}

func isFailure(r types.ReplyFinishedReason) bool {
	return r == types.ReplyError || r == types.ReplyInterrupted
}

// sealFacts is what sealing needs from calls made without the mutex.
type sealFacts struct {
	vis         Visibility
	avail       map[string]bool
	edgeTainted bool
}

// finish seals the end, releases leases, runs summary bookkeeping and
// settles the participant.
func (c *Coordinator) finish(a *activation, end *ActivationEnd, started bool) {
	ps := a.part
	reason := endReason(end)

	var f sealFacts
	for {
		c.mu.Lock()
		holding := c.holdsForegroundLocked(a)
		private := c.privateLocked(ps, a, end.Private)
		c.mu.Unlock()
		f.vis = c.visibility(c.ctx, ps, private, c.sealContent(a, end, reason, holding))
		if holding && reason == types.ReplyError {
			f.avail = c.availability()
			f.edgeTainted = c.edgeTainted()
		}
		c.mu.Lock()
		if private || !c.privateLocked(ps, a, end.Private) {
			break
		}
		// The participant turned private while the policy was asked: decide
		// again, never seal under the older answer.
		c.mu.Unlock()
	}

	// The mutex is held from here.
	if ps.desc.Zone == ZoneEdge && end.Private {
		ps.raisePrivateLocked()
	}
	if a.kind == actWake && isFailure(reason) {
		// A wake that failed leaves its report for the owner's next
		// foreground activation instead of waking it again in this cycle.
		c.noWakeLocked(a.trigger.Seq)
	}
	switch {
	case a.foreground && c.holdsForegroundLocked(a):
		c.sealForegroundLocked(a, end, reason, &f)
	case a.foreground:
		c.sealLateLocked(a, end, &f)
		if c.fg == a {
			// Only a Transfer moves the foreground away from a running
			// activation, and it also replaces c.fg; release it regardless so
			// the slot can never stay taken.
			c.fg, c.fgCancel = nil, nil
			c.maybeStartCycleLocked()
		}
	default:
		c.sealTaskLocked(a, end, reason, &f)
	}
	c.mu.Unlock()

	if c.cfg.Leases != nil {
		c.cfg.Leases.ReleaseAll(ps.name)
	}
	c.afterEnd(a, reason, started)
	c.settle(a)
}

// sealContent is the text whose visibility the end's write needs.
func (c *Coordinator) sealContent(a *activation, end *ActivationEnd, reason types.ReplyFinishedReason, holding bool) string {
	switch {
	case a.foreground && holding:
		return joinNonEmpty(end.Text, end.Digest.Render())
	case a.foreground:
		return end.Text
	case a.task != nil:
		return joinNonEmpty(taskContent(a, end, reason, c.hideTaskError(a)), end.Digest.Render())
	}
	return ""
}

// hideTaskError reports whether a failed task's error text must stay out of
// its report: an edge error is not passed to a cloud assigner, because its
// text may name private data.
func (c *Coordinator) hideTaskError(a *activation) bool {
	if a.task == nil || a.part.desc.Zone != ZoneEdge {
		return false
	}
	as := c.parts[a.task.assigner]
	return as != nil && as.desc.Zone == ZoneCloud
}

func joinNonEmpty(parts ...string) string {
	var keep []string
	for _, p := range parts {
		if p != "" {
			keep = append(keep, p)
		}
	}
	return strings.Join(keep, "\n")
}

// privateLocked reports whether what ps writes now may hold edge-only
// material.
func (c *Coordinator) privateLocked(ps *partState, a *activation, extra bool) bool {
	if ps.desc.Zone != ZoneEdge {
		return false
	}
	trigger := a != nil && a.trigger.EffectiveVisibility() == EdgeOnly
	return ps.private || extra || trigger
}

// visibility applies the write rule: cloud writes are shared; edge writes
// are shared unless private, in which case the disclosure policy decides
// (an error, or no policy, keeps them edge-only). Never call it with the
// mutex held.
func (c *Coordinator) visibility(ctx context.Context, ps *partState, private bool, content string) Visibility {
	if ps.desc.Zone != ZoneEdge || !private {
		return Shared
	}
	if content == "" {
		// Nothing is written from empty content; no need to ask the policy.
		return EdgeOnly
	}
	v, ok := c.classify(ctx, content)
	if !ok {
		return EdgeOnly
	}
	return v
}

// classify asks the disclosure policy; ok is false when there is none or it
// failed. A failure after ctx ended is not reported: the caller's write is
// abandoned anyway.
func (c *Coordinator) classify(ctx context.Context, content string) (Visibility, bool) {
	if c.cfg.Disclosure == nil {
		return EdgeOnly, false
	}
	v, err := c.cfg.Disclosure.Classify(ctx, content)
	if err != nil {
		if ctx.Err() == nil {
			c.mu.Lock()
			c.failLocked("", fmt.Errorf("thread: disclosure policy: %w", err))
			c.mu.Unlock()
		}
		return EdgeOnly, false
	}
	if v == Shared {
		return Shared, true
	}
	return EdgeOnly, true
}

// sealForegroundLocked seals the end of the activation holding the
// foreground and releases the foreground.
func (c *Coordinator) sealForegroundLocked(a *activation, end *ActivationEnd, reason types.ReplyFinishedReason, f *sealFacts) {
	ps := a.part
	t := a.trigger.Seq
	var batch []Entry
	if strings.TrimSpace(end.Text) != "" || cloneDigest(end.Digest) != nil {
		batch = append(batch, Entry{
			Kind:       KindReply,
			Origin:     Origin{Participant: ps.name},
			Visibility: f.vis,
			BasedOn:    t,
			Content:    textContent(end.Text),
			Digest:     cloneDigest(end.Digest),
			Tainted:    end.Tainted,
			DedupKey:   "reply:" + a.id,
		})
	}
	if reason == types.ReplyInterrupted {
		batch = append(batch, markerEntry(MarkerInterrupted, t, fmt.Sprintf("%s was interrupted while answering #%d", ps.name, t)))
	}
	if isFailure(reason) && end.ToolCalls > 0 {
		batch = append(batch, unknownEffects(ps.name, end.ToolCalls, t))
	}
	if len(batch) > 0 {
		stored := c.commitLocked(ps.name, batch, &Guard{Owner: ps.name, Epoch: a.epoch})
		for i := range stored {
			if e := &stored[i]; e.Kind == KindReply {
				ps.lastSealed = e.Seq
				if e.Visibility == EdgeOnly {
					ps.raisePrivateLocked()
				}
			}
		}
	}
	c.fg = nil
	c.fgCancel = nil
	if reason == types.ReplyError && !c.closed && c.answersCycleLocked(a) {
		c.failoverLocked(a, end, f)
	}
	c.maybeStartCycleLocked()
}

// answersCycleLocked reports whether a answers the current cycle's user
// input. Only such an activation fails over: a failed wake, or a transfer
// started by one, would otherwise hand the next participant a query that
// was already answered.
func (c *Coordinator) answersCycleLocked(a *activation) bool {
	return a.cycleInput != 0 && a.cycleInput == c.cycle.input
}

func unknownEffects(who string, calls int, basedOn uint64) Entry {
	return markerEntry(MarkerUnknownEffects, basedOn, fmt.Sprintf(
		"%s ran %d tool call(s) for #%d without a recorded result; check the current state before repeating them", who, calls, basedOn))
}

// failoverLocked moves the foreground away from a failed owner, once per
// query cycle, to another available participant that may take the cycle's
// input. Without one it writes a no_owner marker.
func (c *Coordinator) failoverLocked(a *activation, end *ActivationEnd, f *sealFacts) {
	from := a.part
	t := a.trigger.Seq
	var to *partState
	if !c.cycle.failedOver {
		for _, ps := range c.order {
			if ps != from && f.avail[ps.name] && mayTake(ps.desc.Zone, c.cycle.disclosure, f.edgeTainted) {
				to = ps
				break
			}
		}
	}
	if to == nil {
		c.commitLocked("", []Entry{markerEntry(MarkerNoOwner, t,
			fmt.Sprintf("%s failed and no other participant may take the request", from.name))}, nil)
		return
	}
	c.cycle.failedOver = true
	oc := Entry{
		Kind:       KindOwnerChange,
		Visibility: Shared,
		BasedOn:    t,
		Owner:      &OwnerChange{From: from.name, To: to.name, Reason: ReasonFailover},
		DedupKey:   "failover:" + a.id,
	}
	stored := c.commitLocked("", []Entry{oc}, &Guard{Owner: c.owner, Epoch: c.epoch})
	if len(stored) != 1 {
		return
	}
	na := c.newActivationLocked(to, actFailover, &stored[0], true, 0)
	na.failFrom = from.name
	na.failReason = failReason(from, to, end.Err)
	na.userText = c.cycle.text
	na.cycleInput = a.cycleInput
	c.startForegroundLocked(na)
}

// failReason explains a failure to the participant taking over. An edge
// error is not passed to the cloud: its text may name private data.
func failReason(from, to *partState, err error) string {
	switch {
	case errors.Is(err, errUnavailable):
		return "it is unavailable"
	case err == nil, from.desc.Zone == ZoneEdge && to.desc.Zone == ZoneCloud:
		return "its activation ended with an error"
	}
	return truncateRunes(oneLine(err.Error()), 200)
}

// sealLateLocked keeps text an activation showed after it lost the
// foreground as a report to the current owner, with a rejected_output
// marker. It never writes a reply.
func (c *Coordinator) sealLateLocked(a *activation, end *ActivationEnd, f *sealFacts) {
	if strings.TrimSpace(end.Text) == "" {
		return
	}
	ps := a.part
	t := a.trigger.Seq
	rep := Entry{
		Kind:       KindReport,
		Origin:     Origin{Participant: ps.name},
		To:         []string{c.owner},
		Visibility: f.vis,
		BasedOn:    t,
		Content:    textContent(end.Text),
		Tainted:    end.Tainted,
		DedupKey:   "late:" + a.id,
	}
	mk := markerEntry(MarkerRejected, t, fmt.Sprintf("late output from %s after the foreground moved to %s; kept as a report", ps.name, c.owner))
	c.commitLocked(ps.name, []Entry{rep, mk}, nil)
}

// sealTaskLocked reports a background task's result to its assigner.
func (c *Coordinator) sealTaskLocked(a *activation, end *ActivationEnd, reason types.ReplyFinishedReason, f *sealFacts) {
	tk := a.task
	if tk == nil {
		return
	}
	ps := a.part
	batch := []Entry{{
		Kind:       KindReport,
		Origin:     Origin{Participant: ps.name},
		To:         []string{tk.assigner},
		Visibility: f.vis,
		BasedOn:    tk.seq,
		Task:       &Task{ID: tk.id, Notify: tk.notify},
		Notify:     tk.notify,
		Content:    textContent(taskContent(a, end, reason, c.hideTaskError(a))),
		Digest:     cloneDigest(end.Digest),
		Tainted:    end.Tainted,
		DedupKey:   taskReportPrefix + a.id,
	}}
	if isFailure(reason) && end.ToolCalls > 0 {
		batch = append(batch, unknownEffects(ps.name, end.ToolCalls, tk.seq))
	}
	stored := c.commitLocked(ps.name, batch, nil)
	if len(stored) == 0 {
		return
	}
	// The report is the participant's own sealed output: a summary fork
	// taken after this activation covers through it.
	ps.lastSealed = stored[0].Seq
	if stored[0].Visibility == EdgeOnly {
		ps.raisePrivateLocked()
	}
	if tk.notify {
		c.notifyLocked(c.parts[tk.assigner], &stored[0])
	}
}

// taskReportPrefix starts the dedup key of the report that ends a task.
const taskReportPrefix = "report:"

// taskContent is a task's report text. hideErr replaces the error text with
// a generic reason.
func taskContent(a *activation, end *ActivationEnd, reason types.ReplyFinishedReason, hideErr bool) string {
	id := ""
	if a.task != nil {
		id = a.task.id
	}
	text := strings.TrimSpace(end.Text)
	if !isFailure(reason) && text != "" {
		return end.Text
	}
	why := "it ended with no answer"
	switch {
	case errors.Is(end.Err, errUnavailable):
		why = "the participant is unavailable"
	case reason == types.ReplyInterrupted:
		why = "it was interrupted"
	case end.Err != nil && !hideErr:
		why = truncateRunes(oneLine(end.Err.Error()), 200)
	case end.Err != nil, reason == types.ReplyError:
		why = "it ended with an error"
	}
	msg := fmt.Sprintf("Task %s failed: %s", id, why)
	if text != "" {
		msg += "\nPartial output:\n" + end.Text
	}
	return msg
}

// notifyLocked records a committed Notify report for a recipient that may
// read it and wakes the recipient when it is the idle owner. A report no
// foreground activation of the recipient has received keeps waking it later
// (see checkAddressedLocked).
func (c *Coordinator) notifyLocked(ps *partState, rep *Entry) {
	if ps == nil || !ps.desc.Zone.CanRead(rep.EffectiveVisibility()) {
		return
	}
	if ps.notify == nil {
		ps.notify = make(map[uint64]*Entry)
	}
	cp := cloneEntry(rep)
	ps.notify[rep.Seq] = &cp
	c.wakeLocked(ps, rep)
}

// noWakeLocked keeps the report seq from waking its recipient again in this
// query cycle.
func (c *Coordinator) noWakeLocked(seq uint64) {
	if c.cycle.noWake == nil {
		c.cycle.noWake = make(map[uint64]bool)
	}
	c.cycle.noWake[seq] = true
}

// wakeLocked starts a foreground activation of the idle owner for a Notify
// report it may read, within the cycle's wake limit. The trigger may be
// below the owner's cursor, when a background activation of the owner
// imported the report as context: the wake still answers it.
func (c *Coordinator) wakeLocked(ps *partState, rep *Entry) {
	if ps == nil || !c.idleOwnerLocked(ps) || !ps.desc.Zone.CanRead(rep.EffectiveVisibility()) || c.cycle.noWake[rep.Seq] {
		return
	}
	if c.cycle.wakes >= c.cfg.Limits.MaxWakesPerQuery {
		return
	}
	c.cycle.wakes++
	c.startForegroundLocked(c.newActivationLocked(ps, actWake, rep, true, 0))
}

// newestNotifyLocked returns the newest Notify report waiting for a
// foreground activation of ps that may still wake it in this cycle, or nil.
func (c *Coordinator) newestNotifyLocked(ps *partState) *Entry {
	var newest *Entry
	for seq, e := range ps.notify {
		if (newest == nil || seq > newest.Seq) && !c.cycle.noWake[seq] {
			newest = e
		}
	}
	return newest
}

// queueTaskLocked queues the background activation of a task.
func (c *Coordinator) queueTaskLocked(tk *taskRec, assign *Entry) {
	ps := c.parts[tk.assignee]
	if ps == nil || tk.queued {
		return
	}
	tk.queued = true
	a := c.newActivationLocked(ps, actAssign, assign, false, tk.depth)
	a.task = tk
	c.enqueueActivationLocked(a)
}

// settle frees the participant and makes sure nothing addressed to it or
// to the owner is stranded, then starts its next queued activation.
func (c *Coordinator) settle(a *activation) {
	ps := a.part
	cur := ps.p.State().Cursor
	c.mu.Lock()
	owner := c.parts[c.owner]
	c.mu.Unlock()
	var ownerCur uint64
	if owner != nil && owner != ps {
		ownerCur = owner.p.State().Cursor
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	ps.running = nil
	c.maybeStartCycleLocked()
	c.checkAddressedLocked(ps, cur)
	if owner != nil && owner != ps {
		c.checkAddressedLocked(owner, ownerCur)
	}
	c.kickLocked(ps)
	// An own summary that finished after afterEnd looked for one, while
	// the activation was still running, is applied now that the
	// participant is idle; it is not left for the end of its next
	// activation.
	if own := ps.pendingOwn; own != nil && ps.running == nil && !ps.maint && !c.closed {
		ps.pendingOwn = nil
		ps.maint = true
		c.addWorkLocked()
		c.wg.Add(1)
		go func() {
			defer c.wg.Done()
			c.applyOwn(ps, own)
			c.endMaint(ps)
		}()
	}
	c.doneWorkLocked()
}

// checkAddressedLocked schedules what is addressed to ps: tasks past its
// cursor not yet queued, and, while ps is the idle owner, a wake for the
// newest Notify report no foreground activation of ps has received (a
// background activation of ps may have imported it, moving the cursor past
// it, without anyone answering it). Other entries wait for its next import.
func (c *Coordinator) checkAddressedLocked(ps *partState, cur uint64) {
	if c.closed {
		return
	}
	cur = max(cur, ps.seen)
	entries, err := c.store.Read(c.bg, cur, Filter{To: ps.name, Kinds: []Kind{KindAssign}}, 0)
	if err != nil {
		c.failLocked(ps.name, fmt.Errorf("thread: read entries addressed to %s: %w", ps.name, err))
		return
	}
	for i := range entries {
		e := &entries[i]
		if tk := c.tasks[e.Seq]; tk != nil && !tk.queued && ps.desc.Zone.CanRead(e.EffectiveVisibility()) {
			c.queueTaskLocked(tk, e)
		}
	}
	if !c.idleOwnerLocked(ps) {
		return
	}
	if rep := c.newestNotifyLocked(ps); rep != nil {
		c.wakeLocked(ps, rep)
	}
}
