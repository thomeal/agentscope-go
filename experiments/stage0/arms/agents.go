package arms

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/agentscope-ai/agentscope-go/v2/experiments/stage0/corpus"
	"github.com/agentscope-ai/agentscope-go/v2/experiments/stage0/lab"
	"github.com/agentscope-ai/agentscope-go/v2/pkg/agentscope/agent"
	"github.com/agentscope-ai/agentscope-go/v2/pkg/agentscope/event"
	"github.com/agentscope-ai/agentscope-go/v2/pkg/agentscope/message"
	"github.com/agentscope-ai/agentscope-go/v2/pkg/agentscope/model"
	"github.com/agentscope-ai/agentscope-go/v2/pkg/agentscope/permission"
	"github.com/agentscope-ai/agentscope-go/v2/pkg/agentscope/tool"
	"github.com/agentscope-ai/agentscope-go/v2/pkg/agentscope/types"
)

// agentName is the same everywhere, so a seeded message merges and replays exactly
// as one the agent wrote itself.
const agentName = "assistant"

// structuredToolName is the tool the agent's compression path offers on every rung
// of its fallback ladder. model.structuredOutputToolName is private.
const structuredToolName = "generate_structured_output"

// isCompressCall recognizes the agent's compression by its call options. lab's own
// check looks for a named tool_choice, but the adapters send the forced rung as the
// string "required" and the other rungs without a choice, so only the option list
// identifies all of them.
func isCompressCall(opts []model.CallOption) bool {
	var o model.CallOptions
	for _, opt := range opts {
		opt(&o)
	}
	return isCompressTools(o.Tools)
}

func isCompressTools(tools []model.ToolSchema) bool {
	return len(tools) == 1 && tools[0].Function.Name == structuredToolName
}

// truncationNotice and elisionMarker are the agent's own texts for a compression
// that fell back to truncation and for a summary it elided. TestNullModelCountsLossyCompression
// fails if the first one drifts.
const (
	truncationNotice = "<system-info>Some earlier messages were truncated for limited context.</system-info>"
	elisionMarker    = "<<<TRUNCATED: "
)

// watchModel is the outermost model of every agent. It tags compression calls,
// counts them, and turns a panic into a call error, because model calls run on a
// goroutine the agent owns and a panic there would end the whole run.
type watchModel struct {
	inner   model.ChatModel
	c       *Counters
	state   *agent.AgentState
	noTools bool
	// compressSystem, when set, is the system prompt of a compression request. The
	// plan keeps PeerSummary in a slot of its own that compression never sees, and the
	// prototype holds it in the system prompt, which the agent would otherwise send.
	compressSystem string

	open   bool
	before string
}

// Chat implements model.ChatModel.
func (w *watchModel) Chat(ctx context.Context, msgs []*message.Msg, opts ...model.CallOption) (resp *model.ChatResponse, err error) {
	defer func() {
		if r := recover(); r != nil {
			resp, err = nil, fmt.Errorf("panic in model call: %v", r)
		}
	}()
	if isCompressCall(opts) {
		if !w.open {
			w.open, w.before = true, w.state.Summary
		}
		ctx = lab.WithRole(ctx, lab.RoleCompress)
		return w.inner.Chat(ctx, w.forCompression(msgs), opts...)
	}
	w.settle()
	if w.noTools {
		// Same as a fork: the tools stay in the request, so the prefix is intact,
		// and the model cannot call them.
		opts = append(append([]model.CallOption(nil), opts...), model.WithToolChoice(&model.ToolChoice{Mode: "none"}))
	}
	return w.inner.Chat(ctx, msgs, opts...)
}

// ChatStream implements model.ChatModel. Every agent here is buffered.
func (w *watchModel) ChatStream(context.Context, []*message.Msg, ...model.CallOption) (<-chan model.ChatResponse, error) {
	return nil, model.ErrStreamNotSupported
}

// CountTokens implements model.ChatModel.
func (w *watchModel) CountTokens(msgs []*message.Msg, tools []model.ToolSchema) int {
	if isCompressTools(tools) {
		msgs = w.forCompression(msgs)
	}
	return w.inner.CountTokens(msgs, tools)
}

// forCompression replaces the system message of a compression request.
func (w *watchModel) forCompression(msgs []*message.Msg) []*message.Msg {
	if w.compressSystem == "" || len(msgs) == 0 || msgs[0].Role != message.RoleSystem {
		return msgs
	}
	out := slices.Clone(msgs)
	out[0] = message.SystemMsg("system", w.compressSystem)
	return out
}

// settle closes a compression episode and classifies it from the state it left. The
// agent reports a fallback only in its log, and an episode can span several rungs
// of the structured-output ladder.
func (w *watchModel) settle() {
	if !w.open {
		return
	}
	w.open = false
	w.c.Compress++
	now := w.state.Summary
	if now == w.before || now == truncationNotice || strings.Contains(now, elisionMarker) {
		w.c.LossyEvents++
	}
}

// sharedHost is the switching model behind A3 and A6: one history, routed by the
// scripted owner. Compression always goes to the cloud, so the local model's
// summary quality cannot handicap the baseline.
type sharedHost struct {
	cloud, local model.ChatModel
	owner        corpus.Side
	// limit, when set, is the most input a local call may hold, the check a managed deployment
	// makes before dispatch. What does not fit goes to the cloud, and spilled counts those calls.
	limit   int
	count   func(context.Context, []*message.Msg, []model.ToolSchema) int
	spilled atomic.Int64
}

// Chat implements model.ChatModel.
func (s *sharedHost) Chat(ctx context.Context, msgs []*message.Msg, opts ...model.CallOption) (*model.ChatResponse, error) {
	if s.owner == corpus.SideCloud || isCompressCall(opts) || s.overLimit(ctx, msgs, opts) {
		return s.cloud.Chat(ctx, msgs, opts...)
	}
	return s.local.Chat(ctx, msgs, opts...)
}

func (s *sharedHost) overLimit(ctx context.Context, msgs []*message.Msg, opts []model.CallOption) bool {
	if s.limit <= 0 {
		return false
	}
	var o model.CallOptions
	for _, opt := range opts {
		opt(&o)
	}
	if s.count(ctx, msgs, o.Tools) <= s.limit {
		return false
	}
	s.spilled.Add(1)
	return true
}

// ChatStream implements model.ChatModel.
func (s *sharedHost) ChatStream(context.Context, []*message.Msg, ...model.CallOption) (<-chan model.ChatResponse, error) {
	return nil, model.ErrStreamNotSupported
}

// CountTokens implements model.ChatModel. Windows that are local wrap this host in
// lab.CountingModel, so this byte estimate only serves A6's cloud-sized window.
func (s *sharedHost) CountTokens(msgs []*message.Msg, tools []model.ToolSchema) int {
	return s.cloud.CountTokens(msgs, tools)
}

// memSaver is an in-memory agent.StateSaver.
type memSaver struct {
	mu sync.Mutex
	m  map[string][]byte
}

// SaveState implements agent.StateSaver.
func (s *memSaver) SaveState(_ context.Context, id string, st *agent.AgentState) error {
	raw, err := json.Marshal(st)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.m == nil {
		s.m = map[string][]byte{}
	}
	s.m[id] = raw
	return nil
}

// LoadState implements agent.StateSaver.
func (s *memSaver) LoadState(_ context.Context, id string) (*agent.AgentState, error) {
	s.mu.Lock()
	raw, ok := s.m[id]
	s.mu.Unlock()
	if !ok {
		return nil, fmt.Errorf("no state saved for %s", id)
	}
	var st agent.AgentState
	if err := json.Unmarshal(raw, &st); err != nil {
		return nil, err
	}
	return &st, nil
}

// ListSessions implements agent.StateSaver.
func (s *memSaver) ListSessions(context.Context) ([]agent.SessionInfo, error) { return nil, nil }

// DeleteSession implements agent.StateSaver.
func (s *memSaver) DeleteSession(context.Context, string) error { return nil }

// participant is one agent and everything needed to clone or rebuild it.
type participant struct {
	system string
	chat   model.ChatModel
	tk     *tool.Toolkit
	cfg    agent.ContextConfig
	c      *Counters
	react  agent.ReactConfig
	// compressSystem is watchModel.compressSystem, kept for clones.
	compressSystem string

	state *agent.AgentState
	saver *memSaver
	agent *agent.UnifiedAgent
	watch *watchModel
}

type buildOpts struct {
	react          agent.ReactConfig
	noTools        bool
	noSaver        bool
	compressSystem string
}

func newParticipant(chat model.ChatModel, system string, tk *tool.Toolkit, cfg agent.ContextConfig, state *agent.AgentState, c *Counters, b buildOpts) *participant {
	p := &participant{system: system, chat: chat, tk: tk, cfg: cfg, c: c, react: b.react, state: state, compressSystem: b.compressSystem}
	p.watch = &watchModel{inner: chat, c: c, state: state, noTools: b.noTools, compressSystem: b.compressSystem}
	opts := []agent.AgentOption{
		agent.WithModelStreaming(false),
		agent.WithPermissionContext(permission.NewContext(permission.ModeBypass)),
		agent.WithToolkit(tk),
		agent.WithContextConfig(&cfg),
		agent.WithState(state),
		// One attempt at this layer. The cloud transport already retries 429 and
		// 5xx, and a second layer would multiply them.
		agent.WithModelConfig(agent.ModelConfig{MaxRetries: 1}),
	}
	if b.react.MaxIters > 0 {
		opts = append(opts, agent.WithReactConfig(b.react))
	}
	if !b.noSaver {
		p.saver = &memSaver{}
		opts = append(opts, agent.WithStateSaver(p.saver))
	}
	p.agent = agent.NewUnifiedAgent(agentName, system, p.watch, opts...)
	return p
}

// snapshot returns a deep copy of the agent's state, through the checkpoint path
// that a resume would use.
func (p *participant) snapshot(ctx context.Context) (*agent.AgentState, error) {
	if err := p.agent.SaveCheckpoint(ctx); err != nil {
		return nil, err
	}
	return agent.LoadCheckpoint(ctx, p.saver, p.state.SessionID)
}

// clone builds a throwaway agent on a copy of the state. It may use another
// transport and other counters, which is how a secondary arm gets its own rows.
func (p *participant) clone(ctx context.Context, chat model.ChatModel, c *Counters, b buildOpts) (*participant, error) {
	st, err := p.snapshot(ctx)
	if err != nil {
		return nil, fmt.Errorf("clone: %w", err)
	}
	b.noSaver = true
	b.compressSystem = p.compressSystem
	return newParticipant(chat, p.system, p.tk, p.cfg, st, c, b), nil
}

// replyOut is one finished reply. Final is the text of the last model round only,
// which is what a probe's parser must see: a sentence before a tool call is not part
// of the answer. All is everything the user would have read.
type replyOut struct {
	Final, All string
}

// reply drains ReplyStream. It is Reply plus what Reply hides: per-round text and
// the agent's recovery events.
func (p *participant) reply(ctx context.Context, input string) (replyOut, error) {
	ch, err := p.agent.ReplyStream(ctx, input)
	if err != nil {
		return replyOut{}, err
	}
	var final, all strings.Builder
	var replyErr error
	for evt := range ch {
		switch e := evt.(type) {
		case event.ModelCallStartEvent:
			final.Reset()
			if all.Len() > 0 {
				all.WriteByte('\n')
			}
		case event.TextBlockDeltaEvent:
			final.WriteString(e.Delta)
			all.WriteString(e.Delta)
		case event.CustomEvent:
			if e.Name == "context_overflow_recovery" {
				p.c.OverflowRecoveries++
			}
		case event.ReplyEndEvent:
			switch e.FinishedReason {
			case types.ReplyError:
				if e.Error != nil {
					replyErr = errors.New(e.Error.Message)
				} else {
					replyErr = errors.New("reply failed")
				}
			case types.ReplyExceedMaxIters:
				p.c.MaxIters++
			}
		}
	}
	// On cancellation the stream can close before the core has stopped, so nothing
	// that core writes may be read here. Otherwise closure follows the core.
	if err := ctx.Err(); err != nil {
		return replyOut{}, err
	}
	p.watch.settle()
	if replyErr != nil {
		return replyOut{}, replyErr
	}
	return replyOut{Final: final.String(), All: strings.TrimSpace(all.String())}, nil
}

// isBudget recognizes the hard stop. The agent flattens a failed model call into a
// message string, so errors.Is alone cannot see it.
func isBudget(err error) bool {
	return err != nil && (errors.Is(err, lab.ErrBudget) || strings.Contains(err.Error(), lab.ErrBudget.Error()))
}

// seedMsgs renders scripted steps in the shape the agent itself produces: a user
// message, then one assistant message holding the call, its result and the text.
func seedMsgs(steps []corpus.CStep) []*message.Msg {
	out := make([]*message.Msg, 0, 2*len(steps))
	for _, st := range steps {
		out = append(out, message.UserMsg(agentName, st.User))
		var blocks []message.ContentBlock
		if st.ToolName != "" {
			blocks = append(blocks,
				message.ToolCallBlock{Type: "tool_call", ID: st.ToolID, Name: st.ToolName, Input: toolArgs(st.RecordID), State: message.ToolCallFinished},
				message.ToolResultBlock{Type: "tool_result", ID: st.ToolID, Name: st.ToolName, Output: st.ToolResult, State: message.ToolResultSuccess},
			)
		}
		blocks = append(blocks, message.TextBlock{Type: "text", Text: st.Reply})
		out = append(out, message.AssistantMsg(agentName, blocks))
	}
	return out
}

func toolArgs(recordID string) string {
	raw, err := json.Marshal(map[string]any{"id": recordID})
	if err != nil {
		// A string map cannot fail to marshal.
		panic(err)
	}
	return string(raw)
}
