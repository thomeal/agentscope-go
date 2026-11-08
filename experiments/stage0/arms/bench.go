package arms

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/agentscope-ai/agentscope-go/v2/experiments/stage0/corpus"
	"github.com/agentscope-ai/agentscope-go/v2/experiments/stage0/lab"
	"github.com/agentscope-ai/agentscope-go/v2/experiments/stage0/score"
	"github.com/agentscope-ai/agentscope-go/v2/pkg/agentscope/agent"
	"github.com/agentscope-ai/agentscope-go/v2/pkg/agentscope/message"
	"github.com/agentscope-ai/agentscope-go/v2/pkg/agentscope/tool"
)

// C3+A's Fork A keeps a tail of the history, as the plan specifies: at least this
// many tokens and text messages.
const (
	forkATailTokens = 10_000
	forkATailTexts  = 5
)

// compactReserve makes the agent's Compact summarize the whole history. A ratio this
// small rounds the reserve down to 0 tokens, while 0 itself would be replaced by the
// default of 0.1 and keep up to 105K tokens out of the summary at this window.
const compactReserve = 1e-7

// benchRun is one C-group arm on one item. All arms start from the same scripted
// history, differing only in the marker that keeps their caches apart.
type benchRun struct {
	sessionRun
	item      corpus.CBench
	log       *threadLog
	transport *lab.Cloud
	cloud     *participant
	traced    []corpus.CStep // the history as it happened, for C7's rendering
	live      map[int]bool   // steps a model ran, as opposed to scripted ones
	prefix    int            // estimated tokens a main call at this point would resend
	imputed   float64        // USD the scripted steps would have cost as main calls
}

func (e *Env) execBench(ctx context.Context, arm string, item corpus.CBench) ([]Row, error) {
	b := &benchRun{
		sessionRun: sessionRun{
			env: e, arm: arm,
			system: marker(e.Run, arm, item.ID) + "\n" + item.System,
			tr:     newTracker(arm, item.ID, "c-"+item.Set, len(item.History), item.Probes),
			rec:    newToolRec(),
			start:  time.Now(),

			replies: map[int]string{},
		},
		item: item,
		log:  &threadLog{},
		live: map[int]bool{},
	}
	err := b.run(ctx)
	if isBudget(err) || e.overBudget() {
		return nil, lab.ErrBudget
	}
	if err != nil {
		b.tr.err = err
		b.tr.c.SessionErrors++
	}
	b.tr.elapsed = time.Since(b.start)
	seen, total := b.exposure()
	row := b.tr.row(e, seen, total)
	row.ImputedMainUSD = b.imputed
	return []Row{row}, nil
}

// forkPoint is the history index of the live main call, at about 80%.
func forkPoint(n int) int { return n * 8 / 10 }

func (b *benchRun) run(ctx context.Context) error {
	k := forkPoint(len(b.item.History))
	for _, st := range b.item.LocalOwn {
		b.log.addTurn(corpus.SideLocal, corpus.EdgeOnly, st.User, stepCalls(st), st.Reply)
	}
	if err := b.startCloud(k); err != nil {
		return err
	}
	if err := b.liveCall(ctx, k); err != nil {
		return err
	}
	peer, deltaFrom, err := b.summarize(ctx, k)
	if err != nil {
		return err
	}
	// Without a summary local gets the delta alone. Importing the whole history would
	// turn a failed method into a richer context than the working ones get.
	coverage := b.log.last()
	if peer != nil {
		coverage = peer.Coverage
	}
	return b.answer(ctx, peer, coverage, deltaFrom)
}

func stepCalls(st corpus.CStep) []toolCall {
	if st.ToolName == "" {
		return nil
	}
	return []toolCall{{Name: st.ToolName, Args: toolArgs(st.RecordID)}}
}

// startCloud seeds the state with History[:k] and logs those steps.
func (b *benchRun) startCloud(k int) error {
	item := b.item
	b.transport = b.env.cloudTransport(b.arm, item.ID, item.Canaries)
	chat, err := b.transport.ChatModel()
	if err != nil {
		return err
	}
	b.prefix = lab.EstimateTokens(b.system)
	for i, st := range item.History[:k] {
		b.log.addTurn(corpus.SideCloud, corpus.Shared, st.User, stepCalls(st), st.Reply)
		b.note(i, st)
	}
	tk := tool.NewToolkit(b.rec.recordTool(corpus.SideCloud, item.Read), b.rec.threadTool(b.log, corpus.SideCloud))
	state := &agent.AgentState{SessionID: b.arm + "/" + item.ID + "/cloud", Context: seedMsgs(item.History[:k])}
	b.cloud = newParticipant(chat, b.system, tk, agent.ContextConfig{ContextSize: CloudWindow}, state, &b.tr.c, buildOpts{})
	return nil
}

func (b *benchRun) note(i int, st corpus.CStep) {
	b.replies[i] = st.Reply
	b.traced = append(b.traced, st)
	added := lab.EstimateTokens(st.User) + lab.EstimateTokens(st.ToolResult)
	reply := lab.EstimateTokens(st.Reply)
	if !b.live[i] {
		// A scripted step made no request. A real session would have made at least one,
		// reading the prefix from the cache and writing what is new. A step with a tool
		// call makes two, so this understates the main thread a little.
		b.imputed += lab.Luna.Cost(b.prefix+added, b.prefix, added, reply)
	}
	b.prefix += added + reply
}

// liveCall replays one scripted user message live. It makes the fork point, so the
// snapshot holds a real response on the real prefix.
func (b *benchRun) liveCall(ctx context.Context, i int) error {
	st := b.item.History[i]
	b.live[i] = true
	b.rec.begin(i, 0)
	out, err := b.cloud.reply(lab.WithRole(ctx, lab.RoleMain), st.User)
	calls := b.rec.end()
	if err != nil {
		return fmt.Errorf("live main call at step %d: %w", i, err)
	}
	if out.All == "" {
		b.tr.c.EmptyReplies++
	}
	b.log.addTurn(corpus.SideCloud, corpus.Shared, st.User, calls, out.All)
	live := corpus.CStep{User: st.User, Reply: out.All}
	if len(calls) > 0 {
		live.ToolName = calls[0].Name
		live.ToolResult = strings.Join(b.rec.results[i], "\n")
		live.RecordID = st.RecordID
	}
	b.note(i, live)
	return nil
}

// summarize runs the arm's summary method at the fork point. It returns the summary
// and the history index where the delta starts. A failed method is counted and
// returns a nil summary, so local starts from the delta alone and the row shows why.
func (b *benchRun) summarize(ctx context.Context, k int) (*summary, int, error) {
	if b.arm == ArmC3A {
		return b.forkAndApply(ctx, k)
	}
	var (
		peer *summary
		err  error
	)
	switch b.arm {
	case ArmC0, ArmC0H:
		peer, err = b.compactSummary(ctx)
	case ArmC3, ArmC6, ArmC8:
		snap, ok := b.transport.Snapshot()
		if !ok {
			return nil, 0, errors.New("no completed main call to fork from")
		}
		peer, err = forkB(ctx, b.transport, snap, b.log, nil, &b.tr.c)
	case ArmC4:
		peer, err = b.mergedSummary(ctx)
	case ArmC7:
		peer, err = b.coldSummary(ctx)
	default:
		err = errors.New("unknown bench arm " + b.arm)
	}
	return peer, k + 1, err
}

// compactSummary is C0: the agent's existing Compact on a clone, so the request has
// its real forced-schema shape with no reuse of the prefix, and the template's text is
// what gets trimmed. C0h asks through the same path with the handoff text at low effort,
// which leaves only the path as the difference from C3.
func (b *benchRun) compactSummary(ctx context.Context) (*summary, error) {
	st, err := b.cloud.snapshot(ctx)
	if err != nil {
		return nil, err
	}
	cfg := agent.ContextConfig{ContextSize: CloudWindow, ReserveRatio: compactReserve}
	if b.arm == ArmC0H {
		cfg.CompressionPrompt = HandoffPrompt
		ctx = lab.WithEffort(ctx, forkEffort)
	}
	clone := newParticipant(b.cloud.chat, b.system, b.cloud.tk, cfg, st, &b.tr.c, buildOpts{noSaver: true})
	lossyBefore := b.tr.c.LossyEvents
	_, err = clone.agent.Compact(ctx)
	clone.watch.settle()
	if isBudget(err) {
		return nil, err
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	// The agent swallows a failed compression call and truncates, so a budget stop
	// can reach here as a plain truncation.
	if b.env.Ledger.OverBudget() {
		return nil, lab.ErrBudget
	}
	if err != nil || st.Summary == "" || st.Summary == truncationNotice {
		b.tr.c.ForkFailed++
		if b.tr.c.LossyEvents == lossyBefore {
			b.tr.c.LossyEvents++
		}
		return nil, nil
	}
	text, err := fitSummary(ctx, cloudRewriter(b.transport), st.Summary, &b.tr.c)
	if err != nil {
		return nil, err
	}
	// C0's template ends in the next steps, and the agent's own trim keeps them by cutting
	// the middle. C0h's text puts the least important part last, so the end goes.
	cut := lab.CutLines
	if b.arm == ArmC0 {
		cut = lab.TrimTokens
	}
	return applySummaryCut(b.log, text, cut, &b.tr.c), nil
}

// checkMerged reads the part that is used, because the cloud section is not delivered. A reply
// with no local part is sent back too.
func checkMerged(text string, first bool) (bool, string) {
	local, ok := localSection(text)
	if !ok {
		return false, "Rejected: the reply must hold both summaries, between the delimiters the request gave."
	}
	return checkSummary(local, first)
}

// mergedSummary is C4: one fork that yields both summaries. Only the local section is
// used; the cloud section exists to pay what a merged request would pay.
func (b *benchRun) mergedSummary(ctx context.Context) (*summary, error) {
	snap, ok := b.transport.Snapshot()
	if !ok {
		return nil, errors.New("no completed main call to fork from")
	}
	res, err := b.transport.ForkSubmit(lab.WithEffort(ctx, forkEffort), snap, lab.ForkRequest{Role: lab.RoleMerged, Prompt: MergedPrompt, Limit: mergedLimit}, checkMerged, summaryRounds)
	if err != nil {
		if isBudget(err) || ctx.Err() != nil {
			return nil, err
		}
		b.tr.c.ForkFailed++
		b.tr.c.LossyEvents++
		return nil, nil
	}
	text, ok := localSection(res.Text)
	if !res.Completed || !ok {
		b.tr.c.ForkFailed++
		b.tr.c.LossyEvents++
		return nil, nil
	}
	b.tr.c.ForkMerged++
	b.tr.c.SummaryRewrites += res.Bounces
	b.tr.c.ForkStrays += res.Strays
	return applySummary(b.log, text, &b.tr.c), nil
}

// localSection cuts the local summary out of a merged reply. A missing delimiter is a
// failure rather than something to guess around.
func localSection(text string) (string, bool) {
	_, rest, found := strings.Cut(text, mergedLocalMark)
	if !found {
		return "", false
	}
	body, _, _ := strings.Cut(rest, mergedEndMark)
	body = strings.TrimSpace(body)
	return body, body != ""
}

// coldSummary is C7: a stateless request with the rendered trace. The same model and
// prompt, with no reuse of the main prefix.
func (b *benchRun) coldSummary(ctx context.Context) (*summary, error) {
	user := renderTrace(b.traced) + "\n\n" + HandoffPrompt
	res, err := b.transport.Cold(lab.WithEffort(ctx, forkEffort), lab.RoleSmall, b.system, user, forkBLimit)
	if err != nil {
		if isBudget(err) || ctx.Err() != nil {
			return nil, err
		}
		b.tr.c.ForkFailed++
		b.tr.c.LossyEvents++
		return nil, nil
	}
	if !res.Completed || strings.TrimSpace(res.Text) == "" {
		b.tr.c.ForkFailed++
		b.tr.c.LossyEvents++
		return nil, nil
	}
	b.tr.c.ForkB++
	text, err := fitSummary(ctx, cloudRewriter(b.transport), res.Text, &b.tr.c)
	if err != nil {
		return nil, err
	}
	return applySummary(b.log, text, &b.tr.c), nil
}

func renderTrace(steps []corpus.CStep) string {
	var sb strings.Builder
	sb.WriteString("Here is the trace of a session so far.\n")
	for _, st := range steps {
		fmt.Fprintf(&sb, "\nUser: %s\n", st.User)
		if st.ToolName != "" {
			fmt.Fprintf(&sb, "Tool call: %s %s\nTool result: %s\n", st.ToolName, toolArgs(st.RecordID), st.ToolResult)
		}
		fmt.Fprintf(&sb, "Assistant: %s\n", st.Reply)
	}
	return sb.String()
}

// forkAndApply is C3+A. Fork B and Fork A run on the same snapshot, and Fork B
// finishes before Fork A replaces the history, as the plan requires. Work then
// continues to about half of the remaining history and Fork B runs again with the
// first summary appended. What it tests is whether facts survive the replacement.
func (b *benchRun) forkAndApply(ctx context.Context, k int) (*summary, int, error) {
	snap, ok := b.transport.Snapshot()
	if !ok {
		return nil, 0, errors.New("no completed main call to fork from")
	}
	first, err := forkB(ctx, b.transport, snap, b.log, nil, &b.tr.c)
	if err != nil {
		return nil, 0, err
	}
	res, err := b.transport.Fork(lab.WithEffort(ctx, forkEffort), snap, lab.ForkRequest{Role: lab.RoleForkA, Prompt: ForkAPrompt, Limit: forkALimit})
	if err != nil && (isBudget(err) || ctx.Err() != nil) {
		return nil, 0, err
	}
	st, err2 := b.cloud.snapshot(ctx)
	if err2 != nil {
		return nil, 0, err2
	}
	if err != nil || !res.Completed || strings.TrimSpace(res.Text) == "" {
		// Nothing to apply, so the cloud keeps its full history and the row says so.
		b.tr.c.ForkFailed++
		b.tr.c.LossyEvents++
	} else {
		text, trimmed := lab.CutLines(res.Text, forkATarget)
		if trimmed {
			b.tr.c.LossyEvents++
			b.tr.c.SummaryTrims++
		}
		b.tr.c.ForkA++
		st.Summary, st.Context = text, historyTail(st.Context)
		b.prefix = lab.EstimateTokens(b.system) + lab.EstimateTokens(text) + b.cloud.chat.CountTokens(st.Context, nil)
	}

	n := len(b.item.History)
	m := k + (n-1-k)/2
	if m <= k && k+1 < n {
		m = k + 1
	}
	if m > k {
		for i := k + 1; i < m; i++ {
			s := b.item.History[i]
			b.log.addTurn(corpus.SideCloud, corpus.Shared, s.User, stepCalls(s), s.Reply)
			b.note(i, s)
			st.Context = append(st.Context, seedMsgs([]corpus.CStep{s})...)
		}
		b.cloud = newParticipant(b.cloud.chat, b.system, b.cloud.tk, b.cloud.cfg, st, &b.tr.c, buildOpts{})
		if err := b.liveCall(ctx, m); err != nil {
			return nil, 0, err
		}
		snap, ok = b.transport.Snapshot()
		if !ok {
			return nil, 0, errors.New("no completed main call to fork from")
		}
		second, err := forkB(ctx, b.transport, snap, b.log, first, &b.tr.c)
		if err != nil {
			return nil, 0, err
		}
		if second != nil {
			first = second
		}
	} else {
		m = k
	}
	return first, m + 1, nil
}

// historyTail keeps the newest messages until both minimums are met. A merged
// assistant message holds its calls and results, so cutting between messages cannot
// split a tool pair.
func historyTail(msgs []*message.Msg) []*message.Msg {
	tokens, texts := 0, 0
	start := len(msgs)
	for start > 0 && (tokens < forkATailTokens || texts < forkATailTexts) {
		start--
		tokens += msgTokens(msgs[start])
		if txt := msgs[start].GetTextContent(""); txt != nil && *txt != "" {
			texts++
		}
	}
	return append([]*message.Msg(nil), msgs[start:]...)
}

func msgTokens(m *message.Msg) int {
	n := 0
	for _, blk := range m.GetContentBlocks() {
		switch v := blk.(type) {
		case message.TextBlock:
			n += lab.EstimateTokens(v.Text)
		case message.ToolCallBlock:
			n += lab.EstimateTokens(v.Input)
		case message.ToolResultBlock:
			if s, ok := v.Output.(string); ok {
				n += lab.EstimateTokens(s)
			}
		}
	}
	return n
}

// answer builds the local agent per the arm's row and runs every batch on a clone.
// The delta is scripted, past the fork point, so it is the same in every arm.
func (b *benchRun) answer(ctx context.Context, peer *summary, coverage, deltaFrom int) error {
	item := b.item
	delta := item.History[min(deltaFrom, len(item.History)):]
	for i, st := range delta {
		b.log.addTurn(corpus.SideCloud, corpus.Shared, st.User, stepCalls(st), st.Reply)
		b.note(deltaFrom+i, st)
	}
	b.log.add(kindOwnerChange, sourceHarness, corpus.Shared, fmt.Sprintf("%s -> %s", corpus.SideCloud, corpus.SideLocal))

	chat, err := b.env.localChat(b.arm, item.ID)
	if err != nil {
		return err
	}
	tk := tool.NewToolkit(b.rec.recordTool(corpus.SideLocal, item.Read), b.rec.threadTool(b.log, corpus.SideLocal))
	state := &agent.AgentState{SessionID: b.arm + "/" + item.ID + "/local"}
	if b.arm == ArmC8 {
		state.Context = seedMsgs(item.LocalOwn)
		b.tr.c.Keeps++
	} else {
		b.tr.c.Clears++
	}
	counted := b.env.counted(chat)
	cfg := ownerConfig(counted, peer)
	index := b.log.edgeIndex()
	system := localSystem(b.system, s1LogRules, peer, index)
	local := newParticipant(counted, system, tk, cfg, state, &b.tr.c, buildOpts{
		compressSystem: compressSystemFor(localSystem(b.system, s1LogRules, nil, index), peer),
	})

	imports := b.log.imports(coverage, corpus.SideLocal)
	if b.arm == ArmC6 {
		imports = append(imports, b.deltaResults(ctx, imports, delta)...)
	}
	if err := local.agent.Observe(ctx, imports); err != nil {
		return err
	}
	for i := range item.Probes {
		res, err := b.probe(ctx, local, local.chat, &b.tr.c, item.Probes[i], nil)
		if err != nil {
			return fmt.Errorf("probe batch %d: %w", i, err)
		}
		b.tr.results[i] = res
	}
	return nil
}

// deltaResults is C6's addition: tool results of the delta, newest first, in what the
// budget leaves after the published text, which C6 keeps exactly as C3 does. The last
// result that fits only partly is cut at its tail.
func (b *benchRun) deltaResults(ctx context.Context, published []*message.Msg, delta []corpus.CStep) []*message.Msg {
	used := 0
	for _, m := range published {
		if txt := m.GetTextContent(""); txt != nil {
			used += b.env.Tokens.Count(ctx, *txt)
		}
	}
	var out []*message.Msg
	for i := len(delta) - 1; i >= 0 && used < c6DeltaBudget; i-- {
		st := delta[i]
		if st.ToolName == "" {
			continue
		}
		text := fmt.Sprintf("[tool result from cloud: %s %s]\n%s", st.ToolName, toolArgs(st.RecordID), st.ToolResult)
		n := b.env.Tokens.Count(ctx, text)
		if used+n > c6DeltaBudget {
			if text = cutToTokens(ctx, b.env.Tokens, text, c6DeltaBudget-used); text != "" {
				out = append(out, message.UserMsg("thread", text))
			}
			break
		}
		out = append(out, message.UserMsg("thread", text))
		used += n
	}
	return out
}

// cutToTokens keeps a head of text that fits budget. Token density varies with the
// codes in a record, so it measures rather than assumes a ratio.
func cutToTokens(ctx context.Context, tk *lab.Tokenizer, text string, budget int) string {
	const marker = "\n[...cut to the delta budget...]"
	runes := []rune(text)
	n := tk.Count(ctx, text)
	if n <= 0 {
		return ""
	}
	keep := len(runes) * budget / n
	for keep > 0 {
		out := string(runes[:keep]) + marker
		if tk.Count(ctx, out) <= budget {
			return out
		}
		keep = keep * 9 / 10
	}
	return ""
}

// exposure reuses the scorer's rule on the item's two histories. Scripted steps count
// as seen, because the owner did see them, and a live step counts by what the live
// reply and tools actually carried.
func (b *benchRun) exposure() (seen, total int) {
	item := b.item
	var cloudFacts, localFacts = map[string]corpus.Fact{}, map[string]corpus.Fact{}
	for id, f := range item.Facts {
		if f.Where == "local_tool" || f.Where == "edge_reply" {
			localFacts[id] = f
		} else {
			cloudFacts[id] = f
		}
	}
	cloudResults := map[int][]string{}
	for i, st := range item.History {
		if b.live[i] {
			cloudResults[i] = b.rec.results[i]
		} else if _, reached := b.replies[i]; reached && st.ToolName != "" {
			cloudResults[i] = []string{st.ToolResult}
		}
	}
	localReplies, localResults := map[int]string{}, map[int][]string{}
	for i, st := range item.LocalOwn {
		localReplies[i] = st.Reply
		if st.ToolName != "" {
			localResults[i] = []string{st.ToolResult}
		}
	}
	for _, x := range [][]score.Exposure{
		score.ExposureOf(pseudoSession(item.History, cloudFacts), b.replies, cloudResults),
		score.ExposureOf(pseudoSession(item.LocalOwn, localFacts), localReplies, localResults),
	} {
		total += len(x)
		for _, e := range x {
			if e.Seen {
				seen++
			}
		}
	}
	return seen, total
}

func pseudoSession(steps []corpus.CStep, facts map[string]corpus.Fact) corpus.Session {
	turns := make([]corpus.Turn, len(steps))
	for i, st := range steps {
		turns[i] = corpus.Turn{Index: i, User: st.User}
	}
	return corpus.Session{Turns: turns, Facts: facts}
}
