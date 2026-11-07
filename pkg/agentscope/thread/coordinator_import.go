package thread

import (
	"context"
	"fmt"
	"slices"
	"unicode/utf8"

	"github.com/agentscope-ai/agentscope-go/v2/pkg/agentscope/message"
)

// importResult is what prepareImport hands an activation.
type importResult struct {
	sync   *Sync
	budget int // import budget in tokens, 0 when unlimited
	tokens int // tokens of sync's messages
	// clear is set when sync carries a clear's reset: the state to record
	// once the activation's Sync is known to be applied.
	clear *clearCommit
}

// clearCommit is what a clear changes in the Coordinator's view of the
// participant once its reset is applied.
type clearCommit struct {
	seq      uint64    // the summary's seq
	coverage *Coverage // the summary's coverage
	private  bool      // whether the new context holds edge-only material
	gen      uint64    // partState.privGen when the import marked it
}

// importer imports the entries before an activation's trigger into its
// participant. It calls the participant (Sync, Compact, State) and must run
// without the mutex.
//
// Everything it learns about a chunk (edge-only content, entries cut or
// skipped) is recorded before the chunk is sent: a Sync can fail after the
// participant applied it in memory, so its outcome is unknown, and the
// Coordinator must then assume it was applied. Only a clear lowers the
// participant's privacy, and only once its reset is known to be applied.
type importer struct {
	c       *Coordinator
	ps      *partState
	ctx     context.Context
	entries []Entry // seqs 1..trigger-1, entries[i].Seq == i+1
	trigger uint64
	// private is set when something edge-only was indexed or rendered.
	private bool
	// clearOf is the summary of a clear, nil for an incremental import;
	// indexPrivate is set when the clear's index lists edge-only entries.
	clearOf      *Entry
	indexPrivate bool
	// resetTaint is the summary's taint while the first chunk of a clear is
	// projected; nil for an incremental import.
	resetTaint *bool
	reserve    int // tokens a clear's summary and index take
	// applied is the clear whose reset round a Sync already applied, while
	// its later rounds are still being sent: the clear counts as done
	// (resetFrom) only once its last round goes with the activation.
	applied *clearCommit
}

// prepareImport builds the Sync applied before the activation's reply. The
// trigger itself is never in it: it reaches the participant as the input.
// A trigger at or behind the participant's cursor (a wake for a report a
// background activation already imported) imports nothing.
//
// An edge participant is cleared when it returns after another participant
// held the foreground (the clear rule is for a takeover) and the newest
// summary written for it covers entries past its own last activity: its
// context is reset to that summary, an index of the edge-only entries the
// summary excludes, and the projection of everything else it lacks.
// Otherwise the entries between its cursor and the trigger are projected. An
// import that does not fit is applied in chunks with a compaction after
// each; when compaction stops helping the rest is skipped with a message
// naming the range.
func (c *Coordinator) prepareImport(ctx context.Context, a *activation, st ParticipantState) (importResult, error) {
	ps := a.part
	t := a.trigger.Seq
	if t <= 1 || t <= st.Cursor {
		im := &importer{c: c, ps: ps}
		return importResult{budget: im.budgetFor(st, false)}, nil
	}
	entries, err := c.store.Read(ctx, 0, Filter{}, int(t-1))
	if err != nil {
		return importResult{}, fmt.Errorf("thread: read entries for %s: %w", ps.name, err)
	}
	c.mu.Lock()
	lastAct, held, resetFrom := ps.lastActivity, cloneCoverage(ps.heldCoverage), ps.resetFrom
	pending := ps.clearPending != nil
	c.mu.Unlock()

	im := &importer{c: c, ps: ps, ctx: ctx, entries: entries, trigger: t}
	if ps.desc.Zone == ZoneEdge {
		// An unfinished clear is repeated whatever the participant did
		// since (a failed task writes a report in its name).
		if s := newestSummaryFor(entries, ps.name); s != nil && (pending ||
			(s.Coverage.Through > lastAct && s.Seq != resetFrom && foregroundLeft(entries, ps.name, lastAct))) {
			return im.clear(s, st)
		}
	}
	return im.incremental(st, held)
}

// foregroundLeft reports whether a participant other than name held the
// foreground after seq: the owner in force at seq, or the target of a later
// owner change, is another participant. A participant that kept the
// foreground since its last activity is not taken over, whatever summaries
// a background task of another participant produced meanwhile.
func foregroundLeft(entries []Entry, name string, seq uint64) bool {
	owner := ""
	for i := range entries {
		e := &entries[i]
		if e.Kind != KindOwnerChange || e.Owner == nil {
			continue
		}
		if e.Seq <= seq {
			owner = e.Owner.To
			continue
		}
		if e.Owner.To != name {
			return true
		}
	}
	return owner != "" && owner != name
}

// clear builds the reset import from summary s.
func (im *importer) clear(s *Entry, st ParticipantState) (importResult, error) {
	var shared []uint64
	var hidden []Entry
	for _, seq := range s.Coverage.Excluded {
		e := im.entry(seq)
		if e == nil {
			continue
		}
		if e.EffectiveVisibility() == EdgeOnly {
			hidden = append(hidden, cloneEntry(e))
		} else {
			shared = append(shared, seq)
		}
	}
	index := EdgeIndex(hidden, im.c.cfg.Import.IndexRunes)
	im.clearOf = s
	im.indexPrivate = len(hidden) > 0
	taint := s.Tainted
	im.resetTaint = &taint
	sum := cloneEntry(s)
	im.reserve = estimateText(s.Text()) + estimateText(index)
	first := &Sync{Reset: true, PeerSummary: &sum, Index: &index}
	// Rendering "the summary plus the shared entries it excludes": the
	// edge-only ones it excludes count as covered, because the index stands
	// for them.
	covered := &Coverage{Through: s.Coverage.Through, Excluded: shared}
	return im.run(st, first, covered, true, 0)
}

// incremental imports the entries between the cursor and the trigger.
func (im *importer) incremental(st ParticipantState, held *Coverage) (importResult, error) {
	after := st.Cursor
	if after+1 >= im.trigger {
		return importResult{budget: im.budgetFor(st, false)}, nil
	}
	return im.run(st, nil, held, false, after)
}

// mark records what a chunk about to be sent brings: edge-only content
// raises the participant's privacy, and cut entries join its unseen ones.
// It returns the privacy generation after the mark.
func (im *importer) mark(private bool, cut []uint64) uint64 {
	c := im.c
	c.mu.Lock()
	defer c.mu.Unlock()
	if private {
		im.ps.raisePrivateLocked()
	}
	for _, seq := range cut {
		im.ps.addUnseenLocked(seq, seq)
	}
	return im.ps.privGen
}

// commitFor returns what the clear's reset changes once applied; gen is the
// privacy generation right after the reset chunk was marked.
func (im *importer) commitFor(gen uint64) *clearCommit {
	return &clearCommit{seq: im.clearOf.Seq, coverage: cloneCoverage(im.clearOf.Coverage), private: im.private, gen: gen}
}

// run projects, and when the projection does not fit applies chunks with a
// compaction after each. first carries the clear's fields for the first
// chunk.
func (im *importer) run(st ParticipantState, first *Sync, covered *Coverage, includeOwn bool, after uint64) (importResult, error) {
	pol := im.c.cfg.Import
	falses := 0
	for round := 0; ; round++ {
		budget := im.budgetFor(st, first != nil)
		proj := Project(im.entries[min(int(after), len(im.entries)):], ProjectionSpec{
			Participant: im.ps.name,
			Zone:        im.ps.desc.Zone,
			After:       after,
			Covered:     covered,
			MaxTokens:   budget,
			IncludeOwn:  includeOwn,
		})
		s, private := im.chunk(first, &proj)
		if !proj.Remaining {
			// The last chunk goes with the activation: marked now, before
			// Activate sends it.
			gen := im.mark(private, proj.Cut)
			res := importResult{budget: budget, tokens: countTokens(s.Messages)}
			if !s.Empty() {
				res.sync = s
			}
			if first != nil {
				res.clear = im.commitFor(gen)
			} else {
				res.clear = im.applied
			}
			return res, nil
		}
		if round >= pol.MaxRounds && first == nil {
			return im.skip(st, after), nil
		}
		gen := im.mark(private, proj.Cut)
		if err := im.ps.p.Sync(im.ctx, *s); err != nil {
			return importResult{}, fmt.Errorf("thread: import into %s: %w", im.ps.name, err)
		}
		if first != nil {
			// The reset is applied: the context now holds the summary, and
			// the later chunks raise the privacy again when they need to.
			// The clear is not done yet: if a later round fails, the next
			// activation clears again, because the reset dropped the
			// participant's own entries above this round's cursor and only
			// a clear sends own entries.
			cc := im.commitFor(gen)
			im.c.mu.Lock()
			im.c.commitClearLocked(im.ps, cc, false)
			im.c.mu.Unlock()
			im.applied = cc
		}
		after = proj.Through
		first = nil
		ok, err := im.ps.p.Compact(im.ctx, "")
		switch {
		case err != nil:
			return im.skip(im.ps.p.State(), after), nil
		case !ok:
			falses++
			if falses >= 2 {
				return im.skip(im.ps.p.State(), after), nil
			}
		default:
			falses = 0
		}
		st = im.ps.p.State()
	}
}

// chunk turns a projection into a Sync and reports whether the Sync brings
// edge-only content into an edge participant (rendered entries, or a clear's
// index of edge-only entries).
func (im *importer) chunk(first *Sync, proj *Projection) (*Sync, bool) {
	s := &Sync{Messages: proj.Messages, Cursor: proj.Through}
	private := false
	if first != nil {
		s.Reset, s.PeerSummary, s.Index = first.Reset, first.PeerSummary, first.Index
		taint := proj.Tainted
		if im.resetTaint != nil {
			taint = taint || *im.resetTaint
		}
		s.Taint = &taint
		private = im.indexPrivate
	} else if proj.Tainted {
		taint := true
		s.Taint = &taint
	}
	if im.ps.desc.Zone == ZoneEdge && renderedEdgeOnly(proj.Messages, im.entry) {
		private = true
	}
	im.private = im.private || private
	return s, private
}

// skip moves the cursor past the rest of the import with one message that
// names the range, so nothing is lost and nothing is imported again. The
// range is recorded as unseen before the message is sent.
func (im *importer) skip(st ParticipantState, after uint64) importResult {
	last := im.trigger - 1
	s := &Sync{Cursor: last}
	if after < last {
		im.c.mu.Lock()
		im.ps.addUnseenLocked(after+1, last)
		im.c.mu.Unlock()
		var text string
		if after+1 == last {
			text = fmt.Sprintf("[#%d not imported: read it with thread_read]", last)
		} else {
			text = fmt.Sprintf("[#%d–#%d not imported: read them with thread_read]", after+1, last)
		}
		// Like a gap, the notice names only seqs; it carries the visibility
		// of the entries it stands for.
		vis := Shared
		for seq := after + 1; seq <= last; seq++ {
			if e := im.entry(seq); e == nil || e.EffectiveVisibility() == EdgeOnly {
				vis = EdgeOnly
				break
			}
		}
		s.Messages = []*message.Msg{projectedMsg(text, last, GapKind, vis)}
	}
	// A clear whose reset round was applied is done with the skip notice,
	// which names the rest (own entries included) for thread_read.
	return importResult{sync: s, budget: im.budgetFor(st, false), tokens: countTokens(s.Messages), clear: im.applied}
}

// budgetFor is Share x Window - InputTokens, at least 1, or 0 (no limit)
// when the window is unknown. For a clear's first chunk the context is about
// to be emptied, so only the summary and index count.
func (im *importer) budgetFor(st ParticipantState, reset bool) int {
	w := st.Window
	if w <= 0 {
		w = im.ps.desc.Window
	}
	if w <= 0 {
		return 0
	}
	share := im.c.cfg.Import.CloudShare
	if im.ps.desc.Zone == ZoneEdge {
		share = im.c.cfg.Import.EdgeShare
	}
	used := st.InputTokens
	if reset {
		used = im.reserve
	}
	return max(int(share*float64(w))-used, 1)
}

// entry returns the entry with seq, nil when it is not in range.
func (im *importer) entry(seq uint64) *Entry {
	if seq == 0 || seq > uint64(len(im.entries)) {
		return nil
	}
	e := &im.entries[seq-1]
	if e.Seq != seq {
		return nil
	}
	return e
}

// renderedEdgeOnly reports whether a projected message renders an edge-only
// entry. It trusts the message's visibility key and looks the entry up only
// when the key is missing.
func renderedEdgeOnly(msgs []*message.Msg, lookup func(uint64) *Entry) bool {
	for _, m := range msgs {
		if kind, _ := m.Metadata[KindMetadataKey].(string); kind == GapKind {
			continue
		}
		if v, ok := MsgVisibility(m); ok {
			if v == EdgeOnly {
				return true
			}
			continue
		}
		seq, _ := m.Metadata[SeqMetadataKey].(uint64)
		if e := lookup(seq); e != nil && e.EffectiveVisibility() == EdgeOnly {
			return true
		}
	}
	return false
}

func countTokens(msgs []*message.Msg) int {
	n := 0
	for _, m := range msgs {
		n += estimateTokens(m)
	}
	return n
}

func estimateText(s string) int {
	if s == "" {
		return 0
	}
	return (utf8.RuneCountInString(s) + 3) / 4
}

// pending is Host.Pending: the projection of the entries up to the newest one
// deliverable to the running participant, as a Sync. Deliverable are a
// steering user input or a Notify report addressed to it while it holds the
// foreground, and, to a background activation, the Notify reports of tasks
// that activation assigned itself: a report meant for the idle owner must
// wake a foreground activation, not vanish into a background one. The budget
// is the one its import left, less what Pending already delivered; State is
// not called here because the participant may be inside its own boundary
// callback. Everything the Sync brings is recorded before it is returned.
func (c *Coordinator) pending(ctx context.Context, ps *partState, cursor uint64) (*Sync, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	a := ps.running
	if c.closed || a == nil || !a.started {
		return nil, nil
	}
	entries, err := c.store.Read(ctx, cursor, Filter{}, 0)
	if err != nil {
		return nil, fmt.Errorf("thread: read pending entries for %s: %w", ps.name, err)
	}
	fgOwner := c.holdsForegroundLocked(a)
	zone := ps.desc.Zone
	x := -1
	for i := range entries {
		e := &entries[i]
		if !zone.CanRead(e.EffectiveVisibility()) {
			continue
		}
		switch e.Kind {
		case KindUserInput:
			if fgOwner && c.undeliveredLocked(e.Seq) {
				x = i
			}
		case KindReport:
			if e.Notify && e.Addressed(ps.name) && (fgOwner || c.ownTaskReportLocked(a, e)) {
				x = i
			}
		}
	}
	if x < 0 {
		return nil, nil
	}
	budget := 0
	if a.limited {
		budget = max(a.budget, 1)
	}
	window := entries[:x+1]
	proj := Project(window, ProjectionSpec{
		Participant: ps.name,
		Zone:        zone,
		After:       cursor,
		Covered:     ps.heldCoverage,
		MaxTokens:   budget,
	})
	if proj.Through <= cursor {
		return nil, nil
	}
	if a.limited {
		a.budget -= countTokens(proj.Messages)
	}
	ps.seen = max(ps.seen, proj.Through)
	if fgOwner {
		c.inputs = slices.DeleteFunc(c.inputs, func(r *inputRec) bool {
			return r.entry.Seq <= proj.Through && zone.CanRead(r.entry.EffectiveVisibility())
		})
		ps.deliveredNotifyLocked(proj.Through)
	} else {
		// Reports of its own tasks reached the activation they were meant
		// for; any other Notify report it saw as context still waits for a
		// foreground activation.
		for i := range window {
			if e := &window[i]; e.Seq <= proj.Through && c.ownTaskReportLocked(a, e) {
				delete(ps.notify, e.Seq)
			}
		}
	}
	for _, seq := range proj.Cut {
		ps.addUnseenLocked(seq, seq)
	}
	lookup := func(seq uint64) *Entry {
		if seq <= cursor || seq > cursor+uint64(len(window)) {
			return nil
		}
		return &window[seq-cursor-1]
	}
	if zone == ZoneEdge && renderedEdgeOnly(proj.Messages, lookup) {
		ps.raisePrivateLocked()
	}
	s := &Sync{Messages: proj.Messages, Cursor: proj.Through}
	if proj.Tainted {
		taint := true
		s.Taint = &taint
	}
	return s, nil
}

// ownTaskReportLocked reports whether e reports on a task activation a
// assigned itself.
func (c *Coordinator) ownTaskReportLocked(a *activation, e *Entry) bool {
	if e.Kind != KindReport || e.Task == nil {
		return false
	}
	tk := c.tasks[e.BasedOn]
	return tk != nil && tk.id == e.Task.ID && tk.assignerAct == a.id
}
