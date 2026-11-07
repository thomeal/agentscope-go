package thread

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/agentscope-ai/agentscope-go/v2/pkg/agentscope"
	"github.com/agentscope-ai/agentscope-go/v2/pkg/agentscope/types"
)

// ownResult is an own summary waiting to be applied while its participant
// is idle.
type ownResult struct {
	snap Snapshot
	text string
}

// forkJob is one background summary job of a Forker participant: a peer
// fork, an own fork, or both, on the same snapshot.
type forkJob struct {
	id     string
	ps     *partState
	snap   Snapshot
	growth int // growth when the job started; a successful peer fork subtracts it

	peer     bool
	target   *partState
	budget   int
	coverage Coverage
	tainted  bool
	peerSpec ForkSpec

	own     bool
	ownSpec ForkSpec
}

// afterEnd applies an own summary that waited for the activation to end,
// then decides whether the participant's sealed activation calls for a peer
// or own summary and starts the job. It runs without the mutex.
func (c *Coordinator) afterEnd(a *activation, reason types.ReplyFinishedReason, started bool) {
	ps := a.part
	if ps.forker == nil {
		return
	}
	c.mu.Lock()
	own := ps.pendingOwn
	ps.pendingOwn = nil
	c.mu.Unlock()
	if own != nil {
		c.applyOwn(ps, own)
	}
	if c.cfg.Summaries.Disabled || !started || reason == types.ReplyError {
		return
	}
	job := c.planForks(a)
	if job == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed || ps.forking {
		return
	}
	ps.forking = true
	c.nextJob++
	job.id = fmt.Sprintf("%s-f%d", c.runID, c.nextJob)
	c.addWorkLocked()
	c.wg.Add(1)
	go c.runForkJob(job)
}

// planForks updates the growth counter and returns the job the activation
// calls for, or nil.
func (c *Coordinator) planForks(a *activation) *forkJob {
	ps := a.part
	pol := &c.cfg.Summaries
	st := ps.p.State()

	c.mu.Lock()
	ps.growth += max(0, st.InputTokens-ps.lastInput)
	ps.lastInput = st.InputTokens
	growth, through, handedTo, busy := ps.growth, ps.lastSealed, a.handedTo, ps.forking
	c.mu.Unlock()
	if busy {
		return nil
	}

	target := c.parts[c.edge]
	needPeer := false
	if target != nil && target != ps && through > 0 {
		// A new peer summary only pays for itself when the writer sealed
		// something past the newest one's coverage: growth from work that
		// sealed nothing new would buy the same summary again.
		prev := c.newestSummary(c.ctx, target.name)
		stale := prev == nil || prev.Coverage.Through < through
		needPeer = stale && (growth >= pol.PeerEvery || handedTo == target.name)
	}
	needOwn := st.Window > 0 && float64(st.InputTokens) >= pol.OwnRatio*float64(st.Window)
	if !needPeer && !needOwn {
		return nil
	}
	snap, err := ps.forker.Snapshot()
	if err != nil {
		if !errors.Is(err, ErrNoSnapshot) {
			agentscope.Log().WithError(err).WithField("participant", ps.name).Warn("thread: snapshot for summary fork")
		}
		return nil
	}
	job := &forkJob{ps: ps, snap: snap, growth: growth, tainted: st.Tainted}
	if needPeer {
		if err := c.planPeer(job, target, through); err != nil {
			agentscope.Log().WithError(err).WithField("participant", ps.name).Warn("thread: plan peer summary")
		}
	}
	if needOwn {
		job.own = true
		job.ownSpec = ForkSpec{Prompt: pol.OwnPrompt, MaxTokens: pol.OwnMaxTokens}
	}
	if !job.peer && !job.own {
		return nil
	}
	return job
}

// planPeer fills the peer fork of job: its coverage, recorded before the
// fork, and its spec. The coverage excludes edge-only entries, entries of
// others past the snapshot's cursor, and entries the writer consumed without
// seeing them in full (skipped by an import, or cut by a projection): a
// clear drops what a summary covers, so the summary must not claim them.
func (c *Coordinator) planPeer(job *forkJob, target *partState, through uint64) error {
	pol := &c.cfg.Summaries
	entries, err := c.store.Read(c.ctx, 0, Filter{}, int(through))
	if err != nil {
		return err
	}
	c.mu.Lock()
	unseen := slices.Clone(job.ps.unseen)
	c.mu.Unlock()
	cursor := job.snap.Cursor()
	cov := Coverage{Through: through}
	for i := range entries {
		e := &entries[i]
		if e.EffectiveVisibility() == EdgeOnly || (e.Seq > cursor && e.Origin.Participant != job.ps.name) ||
			inRanges(unseen, e.Seq) {
			cov.Excluded = append(cov.Excluded, e.Seq)
		}
	}
	budget := target.desc.SummaryBudget
	if budget <= 0 {
		budget = defaultPeerBudget
	}
	maxTokens := pol.PeerMaxTokens
	if maxTokens <= 0 {
		maxTokens = max(2*budget, 512)
	}
	window := "unknown"
	if target.desc.Window > 0 {
		window = strconv.Itoa(target.desc.Window)
	}
	prompt := strings.NewReplacer("{target}", target.name, "{window}", window, "{budget}", strconv.Itoa(budget)).Replace(pol.PeerPrompt)
	var appendix string
	if prev := c.newestSummary(c.ctx, target.name); prev != nil && prev.EffectiveVisibility() == Shared {
		appendix = "Previous summary for " + target.name + ", update it:\n" + prev.Text()
	}
	job.peer = true
	job.target = target
	job.budget = budget
	job.coverage = cov
	job.peerSpec = ForkSpec{
		Prompt:      prompt,
		Appendix:    appendix,
		MaxTokens:   maxTokens,
		Validate:    peerValidator(budget),
		MaxRewrites: pol.MaxRewrites,
	}
	return nil
}

// peerValidator accepts a summary of at most 1.5 x budget estimated tokens.
func peerValidator(budget int) func(string) (string, bool) {
	return func(text string) (string, bool) {
		n := estimateText(text)
		if float64(n) <= 1.5*float64(budget) {
			return "", true
		}
		return fmt.Sprintf("Too long (%d tokens). Rewrite it under %d tokens.", n, budget), false
	}
}

// runForkJob runs the job's forks concurrently, appends the peer summary,
// then applies the own summary once the peer fork settled.
func (c *Coordinator) runForkJob(job *forkJob) {
	defer c.wg.Done()
	ps := job.ps
	var peerOut, ownOut ForkOutput
	var peerErr, ownErr error
	var wg sync.WaitGroup
	if job.peer {
		wg.Go(func() { peerOut, peerErr = ps.forker.Fork(c.ctx, job.snap, job.peerSpec) })
	}
	if job.own {
		wg.Go(func() { ownOut, ownErr = ps.forker.Fork(c.ctx, job.snap, job.ownSpec) })
	}
	wg.Wait()
	if job.peer {
		c.finishPeer(job, &peerOut, peerErr)
	}
	if job.own {
		c.finishOwn(job, &ownOut, ownErr)
	}
	c.mu.Lock()
	ps.forking = false
	c.doneWorkLocked()
	c.mu.Unlock()
}

// finishPeer records the peer fork and appends its summary.
func (c *Coordinator) finishPeer(job *forkJob, out *ForkOutput, err error) {
	ps := job.ps
	c.recordForkUsage(ps.name, UsageForkPeer, out, err)
	if err != nil {
		agentscope.Log().WithError(err).WithField("participant", ps.name).Warn("thread: peer summary fork")
		return
	}
	text := strings.TrimSpace(out.Text)
	if text == "" {
		agentscope.Log().WithField("participant", ps.name).Warn("thread: peer summary fork returned no text")
		return
	}
	if out.Rejected {
		text = middleCut(text, job.budget)
	}
	cov := job.coverage
	e := Entry{
		Kind:       KindSummary,
		Origin:     Origin{Participant: ps.name},
		Visibility: Shared,
		Summary:    &SummaryMeta{Target: job.target.name, Budget: job.budget, Lossy: out.Rejected},
		Coverage:   &cov,
		Tainted:    job.tainted,
		Content:    textContent(text),
		DedupKey:   "peer:" + job.id,
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return
	}
	if c.commitLocked(ps.name, []Entry{e}, nil) != nil {
		ps.growth = max(0, ps.growth-job.growth)
	}
}

// finishOwn records the own fork and applies its summary while the
// participant is idle, or keeps it for the end of its current activation.
func (c *Coordinator) finishOwn(job *forkJob, out *ForkOutput, err error) {
	ps := job.ps
	c.recordForkUsage(ps.name, UsageForkOwn, out, err)
	if err != nil {
		agentscope.Log().WithError(err).WithField("participant", ps.name).Warn("thread: own summary fork")
		return
	}
	text := strings.TrimSpace(out.Text)
	if text == "" {
		return
	}
	own := &ownResult{snap: job.snap, text: text}
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return
	}
	if ps.running != nil || ps.maint {
		ps.pendingOwn = own
		c.mu.Unlock()
		return
	}
	ps.maint = true
	c.addWorkLocked()
	c.mu.Unlock()

	c.applyOwn(ps, own)
	c.endMaint(ps)
}

// endMaint ends the own-summary maintenance of an idle participant. It
// starts the participant's next queued activation, or else schedules what
// was addressed to it meanwhile: a Notify report could not wake it while
// the summary was applied. It runs without the mutex.
func (c *Coordinator) endMaint(ps *partState) {
	cur := ps.p.State().Cursor
	c.mu.Lock()
	defer c.mu.Unlock()
	ps.maint = false
	c.kickLocked(ps)
	if ps.running == nil {
		c.checkAddressedLocked(ps, cur)
	}
	c.doneWorkLocked()
}

// applyOwn applies an own summary; a stale snapshot drops it. It runs
// without the mutex while the participant is idle.
func (c *Coordinator) applyOwn(ps *partState, own *ownResult) {
	err := ps.forker.ApplyOwnSummary(c.ctx, own.snap, own.text)
	switch {
	case errors.Is(err, ErrStale):
		agentscope.Log().WithField("participant", ps.name).Debug("thread: own summary is stale; dropped")
		return
	case err != nil:
		agentscope.Log().WithError(err).WithField("participant", ps.name).Warn("thread: apply own summary")
		return
	}
	st := ps.p.State()
	c.mu.Lock()
	ps.lastInput = st.InputTokens
	c.mu.Unlock()
}

// recordForkUsage records a fork's calls: one entry with its usage when it
// is known, otherwise one entry with nil usage per call made.
func (c *Coordinator) recordForkUsage(name string, kind UsageKind, out *ForkOutput, err error) {
	now := c.cfg.Clock()
	switch {
	case out.Usage != nil:
		c.ledger.Record(LedgerEntry{Participant: name, Kind: kind, Usage: out.Usage, Time: now})
	case out.Calls > 0:
		for range out.Calls {
			c.ledger.Record(LedgerEntry{Participant: name, Kind: kind, Time: now})
		}
	case err == nil:
		c.ledger.Record(LedgerEntry{Participant: name, Kind: kind, Time: now})
	}
}

// middleCut keeps the head and tail of text within budget estimated tokens,
// marking the cut.
func middleCut(text string, budget int) string {
	const mark = "\n[…]\n"
	maxRunes := budget * 4
	r := []rune(text)
	if len(r) <= maxRunes {
		return text
	}
	keep := maxRunes - utf8.RuneCountInString(mark)
	if keep <= 0 {
		return string(r[:max(maxRunes, 0)])
	}
	head := keep / 2
	tail := keep - head
	return string(r[:head]) + mark + string(r[len(r)-tail:])
}
