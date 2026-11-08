package arms

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/agentscope-ai/agentscope-go/v2/experiments/stage0/corpus"
	"github.com/agentscope-ai/agentscope-go/v2/experiments/stage0/lab"
	"github.com/agentscope-ai/agentscope-go/v2/pkg/agentscope/agent"
	"github.com/agentscope-ai/agentscope-go/v2/pkg/agentscope/message"
	"github.com/agentscope-ai/agentscope-go/v2/pkg/agentscope/model"
	"github.com/agentscope-ai/agentscope-go/v2/pkg/agentscope/tool"
)

// hostArm is A0, A3 and A6: one agent, one history, both record tools. A single host
// has a single toolkit.
type hostArm struct {
	host   *participant
	shared *sharedHost
}

func newHostArm(arm string, run *sessionRun) (*hostArm, error) {
	e, s := run.env, run.sess
	cloudChat, err := e.cloudTransport(arm, s.ID, privateCodes(s)).ChatModel()
	if err != nil {
		return nil, err
	}
	a := &hostArm{}
	var chat model.ChatModel
	cfg := agent.ContextConfig{ContextSize: CloudWindow}
	system := run.system
	if arm == ArmA0 {
		chat = cloudChat
	} else if arm == ArmA2 {
		// The hybrid the repository has today. It goes to the cloud until the cloud fails and
		// never looks at who owns a turn. A fallback can land on local, so a host has to declare
		// the smaller window, and the cloud then compresses at it too.
		localChat, err := run.localModel(arm)
		if err != nil {
			return nil, err
		}
		chat = e.counted(model.NewConnectivityAwareModel(localChat, cloudChat))
		cfg = localConfig(Window8K, localSummaryTotal)
	} else {
		localChat, err := run.localModel(arm)
		if err != nil {
			return nil, err
		}
		a.shared = &sharedHost{cloud: cloudChat, local: localChat, owner: s.Turns[0].Owner}
		// Left to itself, the agent would derive a summary budget of 1,024 for A3 and
		// 8,192 for A6, which alone overflows the server. The baselines get the total
		// that A4 and T1 have.
		switch arm {
		case ArmA38K:
			chat, cfg = e.counted(a.shared), localConfig(Window8K, localSummaryTotal)
		case ArmA38KH:
			chat, cfg = e.counted(a.shared), localConfig(Window8K, localSummaryTotal)
			cfg.CompressionPrompt = HandoffPrompt
			if run.ws == nil {
				system = run.system + "\n\n" + hostRecordRule
			}
		case ArmA332K:
			chat, cfg = e.counted(a.shared), localConfig(Window32K, localSummaryTotal)
		case ArmA8:
			// What exists today put together: routes come from outside, the history keeps the
			// cloud's window, and local only takes what fits with room for its reply.
			a.shared.limit, a.shared.count = Window8K-taskReplyLimit, e.Tokens.CountMessages
			chat, cfg.SummaryMaxTokens = a.shared, localSummaryTotal
		default:
			chat, cfg.SummaryMaxTokens = a.shared, localSummaryTotal
		}
	}
	tk := tool.NewToolkit(run.hostTools()...)
	state := &agent.AgentState{SessionID: arm + "/" + s.ID}
	a.host = newParticipant(chat, system, tk, cfg, state, &run.tr.c, buildOpts{react: run.react})
	return a, nil
}

func (a *hostArm) owner(corpus.Side) *participant { return a.host }

func (a *hostArm) switchTo(_ context.Context, _, to corpus.Side, _ int) error {
	if a.shared != nil {
		a.shared.owner = to
	}
	return nil
}

func (a *hostArm) afterReply(context.Context, corpus.Turn, replyOut, []toolCall) error { return nil }

func (a *hostArm) secondaries() []*secondary { return nil }

// a4Arm is two agents and no log. Each switch carries a brief. On A4s a local brief
// goes where T1 keeps its summary: in the system prompt, one at a time and out of
// reach of compression. Its context stays, because A4 has no log to rebuild it from.
type a4Arm struct {
	run          *sessionRun
	arm          string
	slot         bool
	noBrief      bool // A4-abrupt on S2: a switch carries nothing
	cloud, local *participant
	abrupt       *secondary
	withRead     *secondary
	delivered    map[corpus.Side]int
}

// twoAgents builds the cloud and local participants of a two-agent arm.
func twoAgents(run *sessionRun, arm string, cloudTk, localTk *tool.Toolkit) (*participant, *participant, *lab.Cloud, error) {
	e, s := run.env, run.sess
	transport := e.cloudTransport(arm, s.ID, privateCodes(s))
	cloudChat, err := transport.ChatModel()
	if err != nil {
		return nil, nil, nil, err
	}
	localChat, err := run.localModel(arm)
	if err != nil {
		return nil, nil, nil, err
	}
	b := buildOpts{react: run.react}
	cloud := newParticipant(cloudChat, run.system, cloudTk, agent.ContextConfig{ContextSize: CloudWindow},
		&agent.AgentState{SessionID: arm + "/" + s.ID + "/cloud"}, &run.tr.c, b)
	counted := e.counted(localChat)
	local := newParticipant(counted, run.localBase(), localTk, ownerConfig(counted, nil),
		&agent.AgentState{SessionID: arm + "/" + s.ID + "/local"}, &run.tr.c,
		buildOpts{react: run.react, compressSystem: compressSystemFor(run.localBase(), nil)})
	return cloud, local, transport, nil
}

// newSecondary builds the transports that label a clone's calls with its own arm. The
// system prompt comes from the clone's state, so it keeps reading the primary's cache.
func newSecondary(run *sessionRun, arm string) (*secondary, error) {
	e, s := run.env, run.sess
	cloudChat, err := e.cloudTransport(arm, s.ID, privateCodes(s)).ChatModel()
	if err != nil {
		return nil, err
	}
	localChat, err := e.localChat(arm, s.ID)
	if err != nil {
		return nil, err
	}
	return &secondary{
		tr: newTracker(arm, s.ID, s.Set, s.Length, s.Probes),
		chat: map[corpus.Side]model.ChatModel{
			corpus.SideCloud: cloudChat,
			corpus.SideLocal: e.counted(localChat),
		},
	}, nil
}

func newA4Arm(run *sessionRun, arm string) (*a4Arm, error) {
	cloudTk := tool.NewToolkit(run.sideTools(corpus.SideCloud)...)
	localTools := run.sideTools(corpus.SideLocal)
	if arm == ArmA4Read {
		localTools = append(localTools, run.eventReader(&run.tr.c))
	}
	localTk := tool.NewToolkit(localTools...)
	cloud, local, _, err := twoAgents(run, arm, cloudTk, localTk)
	if err != nil {
		return nil, err
	}
	a := &a4Arm{run: run, arm: arm, slot: arm == ArmA4S, noBrief: arm == ArmA4Abr, cloud: cloud, local: local, delivered: map[corpus.Side]int{}}
	// The abrupt clone exists to answer probes, and S2 has none. A4s is recorded next to
	// A4, so one abrupt row is enough.
	if arm == ArmA4 && len(run.sess.Probes) > 0 {
		if a.abrupt, err = newSecondary(run, ArmA4Abr); err != nil {
			return nil, err
		}
		if a.withRead, err = newReadSecondary(run, ArmA4Read, nil); err != nil {
			return nil, err
		}
	}
	return a, nil
}

// newReadSecondary is a clone whose local also has the event list. extra are the arm's other
// local tools, which the clone keeps.
func newReadSecondary(run *sessionRun, arm string, extra []tool.Tool) (*secondary, error) {
	sec, err := newSecondary(run, arm)
	if err != nil {
		return nil, err
	}
	tools := append(run.sideTools(corpus.SideLocal), extra...)
	sec.tk = tool.NewToolkit(append(tools, run.eventReader(&sec.tr.c))...)
	return sec, nil
}

func (a *a4Arm) owner(side corpus.Side) *participant {
	if side == corpus.SideLocal {
		return a.local
	}
	return a.cloud
}

func (a *a4Arm) switchTo(ctx context.Context, from, to corpus.Side, batch int) error {
	if a.noBrief {
		return nil
	}
	outgoing, receiver := a.owner(from), a.owner(to)
	text, ok, err := a.run.writeBrief(ctx, outgoing, from, outgoing.chat, &a.run.tr.c, a.arm)
	if err != nil {
		return err
	}
	// The abrupt clone is the receiver before this brief. Earlier briefs stay in its
	// context, because only the latest switch is abrupt.
	if a.abrupt != nil {
		if err := a.run.probeSecondary(ctx, a.abrupt, receiver, to, batch, nil); err != nil {
			return err
		}
	}
	if a.slot && to == corpus.SideLocal {
		return a.slotBrief(ctx, from, text, ok)
	}
	before := len(receiver.state.Context)
	if ok {
		if err := receiver.agent.Observe(ctx, []*message.Msg{briefMsg(from, text)}); err != nil {
			return err
		}
		a.delivered[to]++
	}
	a.run.env.trace(TraceEvent{Kind: "brief", Before: before, After: len(receiver.state.Context), State: receiver.state})
	return a.run.readClone(ctx, a.withRead, receiver, to, batch)
}

// slotBrief rebuilds the local agent with the newest brief in its system prompt. A
// failed brief leaves the last one in place, as a failed fork does for T1.
func (a *a4Arm) slotBrief(ctx context.Context, from corpus.Side, text string, ok bool) error {
	if !ok {
		return nil
	}
	st, err := a.local.snapshot(ctx)
	if err != nil {
		return err
	}
	base := a.run.localBase()
	system := base + "\n\n" + fmt.Sprintf(briefHeader+"\n%s", from, text)
	peer := &summary{Text: text}
	boundOwnSummary(st, peer, &a.run.tr.c)
	a.local = newParticipant(a.local.chat, system, a.local.tk, ownerConfig(a.local.chat, peer), st, &a.run.tr.c, buildOpts{react: a.run.react, compressSystem: compressSystemFor(base, peer)})
	a.run.env.trace(TraceEvent{Kind: "brief", Before: len(st.Context), After: len(st.Context), State: st, System: system})
	return nil
}

// afterReply states how many delivered briefs compression has folded away. A brief in
// Context can be rewritten or lost that way, which T1's summary and A4s's brief cannot.
func (a *a4Arm) afterReply(context.Context, corpus.Turn, replyOut, []toolCall) error {
	lost := 0
	for _, side := range []corpus.Side{corpus.SideCloud, corpus.SideLocal} {
		lost += a.delivered[side] - countBriefs(a.owner(side).state.Context)
	}
	a.run.tr.c.BriefsCompressed = lost
	return nil
}

func countBriefs(msgs []*message.Msg) int {
	n := 0
	for _, m := range msgs {
		if txt := m.GetTextContent(""); txt != nil && strings.HasPrefix(*txt, briefPrefix) {
			n++
		}
	}
	return n
}

func (a *a4Arm) secondaries() []*secondary {
	if a.abrupt == nil {
		return nil
	}
	return []*secondary{a.abrupt, a.withRead}
}

// t1Arm is two agents, an in-memory log and the T1 rules of HARNESS.md.
type t1Arm struct {
	run          *sessionRun
	arm          string
	log          *threadLog
	transport    *lab.Cloud
	cloud, local *participant
	withBrief    *secondary
	withRead     *secondary

	peer          *summary
	lastForkInput int
	cloudCursor   int
	localCursor   int
	localLast     int
}

func newT1Arm(run *sessionRun, arm string) (*t1Arm, error) {
	log := &threadLog{}
	cloudTk := tool.NewToolkit(append(run.sideTools(corpus.SideCloud), run.rec.threadTool(log, corpus.SideCloud))...)
	localTools := append(run.sideTools(corpus.SideLocal), run.rec.threadTool(log, corpus.SideLocal))
	if arm == ArmT1Read {
		localTools = append(localTools, run.eventReader(&run.tr.c))
	}
	localTk := tool.NewToolkit(localTools...)
	cloud, local, transport, err := twoAgents(run, arm, cloudTk, localTk)
	if err != nil {
		return nil, err
	}
	a := &t1Arm{run: run, arm: arm, log: log, transport: transport, cloud: cloud, local: local}
	// The brief clone exists to answer probes, and S2 has none.
	if len(run.sess.Probes) > 0 {
		if a.withBrief, err = newSecondary(run, ArmT1Brief); err != nil {
			return nil, err
		}
		if a.withRead, err = newReadSecondary(run, ArmT1Read, []tool.Tool{run.rec.threadTool(log, corpus.SideLocal)}); err != nil {
			return nil, err
		}
	}
	return a, nil
}

func (a *t1Arm) owner(side corpus.Side) *participant {
	if side == corpus.SideLocal {
		return a.local
	}
	return a.cloud
}

func (a *t1Arm) switchTo(ctx context.Context, from, to corpus.Side, batch int) error {
	outgoing := a.owner(from)
	a.log.add(kindOwnerChange, sourceHarness, corpus.Shared, fmt.Sprintf("%s -> %s", from, to))
	var err error
	if to == corpus.SideLocal {
		err = a.takeLocal(ctx)
	} else {
		err = a.takeCloud(ctx)
	}
	if err != nil {
		return err
	}
	if a.withBrief == nil {
		return nil
	}
	if err := a.briefClone(ctx, outgoing, from, to, batch); err != nil {
		return err
	}
	return a.run.readClone(ctx, a.withRead, a.owner(to), to, batch)
}

// takeLocal is "Cloud to local": no model call. The clear branch fires only when the
// newest summary is past local's last activity, so a short cloud segment does not
// cost local its context.
func (a *t1Arm) takeLocal(ctx context.Context) error {
	var st *agent.AgentState
	kind := "keep"
	if !neverClear && a.peer != nil && a.peer.Coverage > a.localLast {
		kind = "clear"
		st = &agent.AgentState{SessionID: a.local.state.SessionID}
		a.localCursor = a.peer.Coverage
		a.run.tr.c.Clears++
	} else {
		var err error
		if st, err = a.local.snapshot(ctx); err != nil {
			return err
		}
		if a.peer != nil {
			st.Context = dropCovered(st.Context, a.peer.Coverage)
			a.localCursor = max(a.localCursor, a.peer.Coverage)
			boundOwnSummary(st, a.peer, &a.run.tr.c)
		}
		a.run.tr.c.Keeps++
	}
	cfg := ownerConfig(a.local.chat, a.peer)
	rules, index := a.run.logText(), a.log.edgeIndex()
	system := localSystem(a.run.system, rules, a.peer, index)
	before := len(st.Context)
	a.local = newParticipant(a.local.chat, system, a.local.tk, cfg, st, &a.run.tr.c, buildOpts{
		react:          a.run.react,
		compressSystem: compressSystemFor(localSystem(a.run.system, rules, nil, index), a.peer),
	})
	a.run.env.trace(TraceEvent{Kind: kind, Before: before, State: st, System: system})
	if err := a.local.agent.Observe(ctx, a.log.imports(a.localCursor, corpus.SideLocal)); err != nil {
		return err
	}
	a.localCursor = a.log.last()
	return nil
}

// takeCloud is "Local to cloud": no model call and no rebuild, so the cached prefix
// stays intact. edge_only entries never leave, because imports filters them out.
func (a *t1Arm) takeCloud(ctx context.Context) error {
	if err := a.cloud.agent.Observe(ctx, a.log.imports(a.cloudCursor, corpus.SideCloud)); err != nil {
		return err
	}
	a.cloudCursor = a.log.last()
	return nil
}

// briefClone is T1+brief: the receiver, after T1's own handling, on a clone that also
// gets a brief from the outgoing owner. It never touches T1's state.
func (a *t1Arm) briefClone(ctx context.Context, outgoing *participant, from, to corpus.Side, batch int) error {
	sec := a.withBrief
	begin := time.Now()
	text, ok, err := a.run.writeBrief(ctx, outgoing, from, sec.chat[from], &sec.tr.c, ArmT1Brief)
	d := time.Since(begin)
	sec.tr.elapsed += d
	a.run.spent += d
	if err != nil {
		return err
	}
	var extra []*message.Msg
	if ok {
		extra = []*message.Msg{briefMsg(from, text)}
	}
	return a.run.probeSecondary(ctx, sec, a.owner(to), to, batch, extra)
}

func (a *t1Arm) afterReply(ctx context.Context, turn corpus.Turn, out replyOut, calls []toolCall) error {
	seq := a.log.addTurn(turn.Owner, turn.Disclosure, turn.User, calls, out.All)
	if turn.Owner == corpus.SideLocal {
		a.localLast = seq
		return nil
	}
	return a.maybeFork(ctx, turn.Index)
}

// maybeFork is Fork B: at the end of a cloud reply, once the cloud's input has grown
// by at least forkBRefresh since the last one that completed. A failed fork leaves the mark
// where it was, so the next cloud reply tries again.
func (a *t1Arm) maybeFork(ctx context.Context, turn int) error {
	input := a.transport.LastInputTokens()
	growth := input - a.lastForkInput
	if growth < forkBRefresh {
		return nil
	}
	// An incomplete main call does not move the snapshot, so coverage would claim
	// more than the fork can see. Wait for the next completed reply.
	if lastStatus(a.run.env.Ledger, a.arm, a.run.tr.session, lab.RoleMain) != "completed" {
		a.run.tr.c.ForkSkipped++
		return nil
	}
	snap, ok := a.transport.Snapshot()
	if !ok {
		a.run.tr.c.ForkSkipped++
		return nil
	}
	next, err := forkB(ctx, a.transport, snap, a.log, a.peer, &a.run.tr.c)
	if err != nil {
		return err
	}
	if next == nil {
		return nil
	}
	a.peer = next
	a.lastForkInput = input
	a.run.env.trace(TraceEvent{Kind: "fork_b", Turn: turn, Growth: growth})
	return nil
}

func (a *t1Arm) secondaries() []*secondary {
	if a.withBrief == nil {
		return nil
	}
	return []*secondary{a.withBrief, a.withRead}
}
