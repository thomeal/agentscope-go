package thread

import (
	"context"
	"fmt"
	"strconv"
	"strings"
)

// recover rebuilds the Coordinator's state from the log and the
// participants, then records what the previous process left unfinished:
// a user_input that no reply, marker or owner_change is based on gets a
// recovered marker and waits, undelivered, for the next Submit (its effects
// are unknown, so it is never re-run); a task with no final report gets a
// report to its assigner saying it did not finish. A participant whose
// cursor is past the log head fails it: its checkpoint does not belong to
// this log.
func (c *Coordinator) recover(ctx context.Context) error {
	head, err := c.store.Head(ctx)
	if err != nil {
		return fmt.Errorf("thread: read log head: %w", err)
	}
	entries, err := c.store.Read(ctx, 0, Filter{}, 0)
	if err != nil {
		return fmt.Errorf("thread: read log: %w", err)
	}
	c.owner, c.epoch, c.lastSeq = head.Owner, head.Epoch, head.LastSeq

	states := make(map[string]ParticipantState, len(c.order))
	for _, ps := range c.order {
		st := ps.p.State()
		if st.Cursor > head.LastSeq {
			return fmt.Errorf("thread: participant %s has cursor %d but the log ends at #%d; "+
				"its checkpoint does not belong to this log, or the log lost entries (for example ones written WithoutSync)",
				ps.name, st.Cursor, head.LastSeq)
		}
		states[ps.name] = st
	}

	handled := make(map[uint64]bool)
	reported := make(map[string]bool)
	for i := range entries {
		e := &entries[i]
		if ps := c.parts[e.Origin.Participant]; ps != nil {
			ps.lastActivity = e.Seq
			if sealedOutput(e) {
				ps.lastSealed = e.Seq
			}
		}
		switch e.Kind {
		case KindReply, KindMarker, KindOwnerChange:
			if e.BasedOn != 0 {
				handled[e.BasedOn] = true
			}
		case KindReport:
			// Only the report that ends a task counts, never a progress
			// report about it.
			if e.Task != nil && (strings.HasPrefix(e.DedupKey, taskReportPrefix) || strings.HasPrefix(e.DedupKey, orphanPrefix)) {
				reported[e.Task.ID] = true
			}
		case KindAssign:
			if e.Task != nil {
				if n, ok := strings.CutPrefix(e.Task.ID, "t-"); ok {
					if v, err := strconv.Atoi(n); err == nil {
						c.nextTask = max(c.nextTask, v)
					}
				}
			}
		}
	}

	for _, ps := range c.order {
		st := states[ps.name]
		ps.seen = st.Cursor
		ps.lastInput = st.InputTokens
		for i := range entries {
			e := &entries[i]
			switch {
			case ps.desc.Zone == ZoneEdge && e.Seq <= st.Cursor && e.EffectiveVisibility() == EdgeOnly:
				ps.private = true
			case e.Seq > st.Cursor && e.Kind == KindReport && e.Notify && e.Addressed(ps.name):
				// Reports past the cursor reached no foreground activation;
				// they still wake the owner.
				if ps.desc.Zone.CanRead(e.EffectiveVisibility()) {
					if ps.notify == nil {
						ps.notify = make(map[uint64]*Entry)
					}
					cp := cloneEntry(e)
					ps.notify[e.Seq] = &cp
				}
			}
		}
	}

	var batch []Entry
	var waiting *Entry
	for i := range entries {
		e := &entries[i]
		switch {
		case e.Kind == KindUserInput && !handled[e.Seq]:
			batch = append(batch, markerEntry(MarkerRecovered, e.Seq, fmt.Sprintf(
				"#%d was not answered before the session stopped; its effects are unknown, so it was not run again", e.Seq)))
			batch[len(batch)-1].DedupKey = "recovered:" + strconv.FormatUint(e.Seq, 10)
			waiting = e
		case e.Kind == KindAssign && e.Task != nil && !reported[e.Task.ID] && len(e.To) > 0:
			batch = append(batch, Entry{
				Kind:       KindReport,
				To:         []string{e.Origin.Participant},
				Visibility: e.EffectiveVisibility(),
				BasedOn:    e.Seq,
				Task:       &Task{ID: e.Task.ID},
				Content: textContent(fmt.Sprintf(
					"Task %s did not finish before the session stopped; its effects are unknown.", e.Task.ID)),
				DedupKey: orphanPrefix + e.Task.ID,
			})
		}
	}
	if len(batch) == 0 {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	stored, err := c.appendLocked(batch, nil)
	if err != nil {
		return fmt.Errorf("thread: record recovery: %w", err)
	}
	c.emitEntriesLocked(stored)
	if waiting != nil {
		// Only the newest unanswered input waits; older ones are context.
		// It keeps the disclosure Submit resolved; an entry without one (or
		// with one that contradicts its visibility) falls back to the
		// visibility, edge-only winning. A parked input never starts its
		// own cycle and delivery decides on visibility, so today this keeps
		// the record faithful; it matters once a routing path reads it.
		q := Query{Text: waiting.Text(), Disclosure: waiting.Disclosure}
		switch {
		case waiting.EffectiveVisibility() == EdgeOnly:
			q.Disclosure = DiscloseEdgeOnly
		case q.Disclosure != DiscloseShared && q.Disclosure != DiscloseUnclassified:
			q.Disclosure = DiscloseShared
		}
		in := &inputRec{entry: cloneEntry(waiting), q: q}
		c.inputs = append(c.inputs, in)
		c.parkLocked(in)
	}
	return nil
}

// orphanPrefix starts the dedup key of the report recovery writes for a task
// that never finished.
const orphanPrefix = "orphan:"

// sealedOutput reports whether e is a participant's own sealed output: a
// reply, an owner change it wrote (a Transfer), or the report that ended a
// task it ran.
func sealedOutput(e *Entry) bool {
	switch e.Kind {
	case KindReply, KindOwnerChange:
		return true
	case KindReport:
		return e.Task != nil && strings.HasPrefix(e.DedupKey, taskReportPrefix)
	}
	return false
}
