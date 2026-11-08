package arms

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/agentscope-ai/agentscope-go/v2/experiments/stage0/corpus"
	"github.com/agentscope-ai/agentscope-go/v2/experiments/stage0/lab"
	"github.com/agentscope-ai/agentscope-go/v2/pkg/agentscope/agent"
	"github.com/agentscope-ai/agentscope-go/v2/pkg/agentscope/classifier"
	classifieropenai "github.com/agentscope-ai/agentscope-go/v2/pkg/agentscope/classifier/openai"
	"github.com/agentscope-ai/agentscope-go/v2/pkg/agentscope/credential"
	"github.com/agentscope-ai/agentscope-go/v2/pkg/agentscope/event"
	"github.com/agentscope-ai/agentscope-go/v2/pkg/agentscope/message"
	"github.com/agentscope-ai/agentscope-go/v2/pkg/agentscope/model"
	"github.com/agentscope-ai/agentscope-go/v2/pkg/agentscope/permission"
	"github.com/agentscope-ai/agentscope-go/v2/pkg/agentscope/thread"
	mtpart "github.com/agentscope-ai/agentscope-go/v2/pkg/agentscope/thread/participant"
	"github.com/agentscope-ai/agentscope-go/v2/pkg/agentscope/thread/route"
	"github.com/agentscope-ai/agentscope-go/v2/pkg/agentscope/tool"
	"github.com/agentscope-ai/agentscope-go/v2/pkg/agentscope/types"
)

// ArmMTHybrid runs an S2 task on the shipped Main Thread: a thread.Coordinator with an
// edge and a cloud participant from package participant, routed by route.Hybrid with a
// classifier that sees the thread state. The task's own owners are not used: the router
// decides every turn, and the participants may transfer or assign work on their own. The
// prototype T1 is the harness's model of the same design; this arm is the code.
const ArmMTHybrid = "MT-hybrid"

// ArmMTRouted runs the same thread with master's routing left as it is: a host rule gives
// every turn the owner the frozen routes give it (the query-only classifier's choice, local
// only at θ or above), and a repair turn the owner of the last scripted turn, as A3-8K
// does. The router asks no classifier. What A3-8K and this arm share is the first owner of
// every query. The thread as shipped differs in the rest: its contexts and who summarizes
// them, its prompts, tools and per-side workspace views, and its handovers (transfer,
// background tasks, failover), which can move work to the other side within a turn. A
// recovered query carries no operation ID and would fall to the hybrid default; sessions
// here use a fresh memory store and never restart.
const ArmMTRouted = "MT-routed"

// isThreadArm reports whether arm runs on the shipped Coordinator.
func isThreadArm(arm string) bool { return arm == ArmMTHybrid || arm == ArmMTRouted }

// scriptedRoute is ArmMTRouted's host rule. Turn i is submitted as "turn-i"; an index past
// the scripted turns is a repair turn and goes to the last scripted turn's owner.
func scriptedRoute(turns []corpus.Turn) route.Rule {
	return func(in thread.RouteRequest) (string, bool) {
		var i int
		if _, err := fmt.Sscanf(in.OperationID, "turn-%d", &i); err != nil || i < 0 || len(turns) == 0 {
			return "", false
		}
		i = min(i, len(turns)-1)
		if turns[i].Owner == corpus.SideCloud {
			return threadCloud, true
		}
		return threadEdge, true
	}
}

// Participant names and what each one's peers and the router read about it. route adds
// stage 0's text about each side's model and context in front of the description.
const (
	threadEdge  = "edge"
	threadCloud = "cloud"
	threadDesc  = "It can read and edit the workspace files and run the tests."
)

// threadBriefRunes bounds a transfer brief to the edge. Stage 0 gave a brief Fork B's
// target of 2,500 tokens; at about 3.2 characters a token on this corpus that is 8,000.
const threadBriefRunes = 8000

// threadWorkspaceNote is the host's one statement about the task: both participants work
// on one workspace. What the thread log is, and how to hand work over, is the system
// prompt section the participant package writes.
const threadWorkspaceNote = `[Shared workspace]
The other participant and you share one workspace. Read the workspace files to see their current state.`

// roleClassifier labels the router's classifier calls in the ledger, and roleThreadFork
// the cloud's summary forks, peer summaries and its own notes alike.
const (
	roleClassifier = "classifier"
	roleThreadFork = "thread_fork"
)

// forkTagger books the cloud participant's forks under roleThreadFork. A fork
// (agent.ForkSummary) is the one request the thread sends with tool choice "none"; every
// other call keeps the transport's main role.
type forkTagger struct{ model.ChatModel }

// Chat implements model.ChatModel.
func (f forkTagger) Chat(ctx context.Context, msgs []*message.Msg, opts ...model.CallOption) (*model.ChatResponse, error) {
	var o model.CallOptions
	for _, opt := range opts {
		opt(&o)
	}
	if o.ToolChoice != nil && o.ToolChoice.Mode == "none" {
		ctx = lab.WithRole(ctx, roleThreadFork)
	}
	return f.ChatModel.Chat(ctx, msgs, opts...)
}

// threadArm is one session of ArmMTHybrid or ArmMTRouted.
type threadArm struct {
	run  *sessionRun
	arm  string
	c    *thread.Coordinator
	done chan struct{}

	mu     sync.Mutex
	text   strings.Builder
	counts map[string]int
	errs   []string
}

// threadClassifier is the router's classifier for one session: Env.ThreadClassifier, or
// OpenAI Decisions on the cloud model through a transport that books every call.
func (e *Env) threadClassifier(arm, session string) (classifier.DispatchClassifier, error) {
	if e.ThreadClassifier != nil {
		return e.ThreadClassifier(arm, session)
	}
	return classifieropenai.New(classifieropenai.Config{
		Credential: &credential.OpenAICredential{Key: e.APIKey, URL: strings.TrimRight(e.CloudURL, "/") + "/v1"},
		Model:      e.CloudModel,
		HTTPClient: &http.Client{Transport: &classifierLedger{e: e, arm: arm, session: session, next: http.DefaultTransport}},
	})
}

// classifierLedger books each classifier request as a cloud row of the session, with the
// cache reads and writes the adapter does not return. Decisions charges no output.
type classifierLedger struct {
	e            *Env
	arm, session string
	next         http.RoundTripper
}

// RoundTrip implements http.RoundTripper.
func (t *classifierLedger) RoundTrip(r *http.Request) (*http.Response, error) {
	if t.e.overBudget() {
		return nil, lab.ErrBudget
	}
	begin := time.Now()
	resp, err := t.next.RoundTrip(r)
	row := lab.Row{Arm: t.arm, Session: t.session, Side: lab.SideCloud, Role: roleClassifier, Model: t.e.CloudModel, Attempt: 1}
	if err != nil {
		row.LatencyMS = time.Since(begin).Milliseconds()
		row.Error = scrub(err.Error(), t.e.APIKey)
		row.UsageMissing = true
		t.e.Ledger.Add(row)
		return nil, err
	}
	body, readErr := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	row.HTTP, row.LatencyMS = resp.StatusCode, time.Since(begin).Milliseconds()
	fillClassifierRow(&row, body)
	if readErr != nil {
		row.Error = scrub(readErr.Error(), t.e.APIKey)
	}
	t.e.Ledger.Add(row)
	if readErr != nil {
		return nil, readErr
	}
	resp.Body = io.NopCloser(bytes.NewReader(body))
	return resp, nil
}

// fillClassifierRow reads a Decisions response's usage into row and prices it.
func fillClassifierRow(row *lab.Row, body []byte) {
	var parsed struct {
		Model string `json:"model"`
		Usage *struct {
			Input   int `json:"input_tokens"`
			Output  int `json:"output_tokens"`
			Details struct {
				Cached int `json:"cached_tokens"`
				Write  int `json:"cache_write_tokens"`
			} `json:"input_tokens_details"`
		} `json:"usage"`
	}
	if json.Unmarshal(body, &parsed) != nil || parsed.Usage == nil {
		row.UsageMissing = true
		return
	}
	if parsed.Model != "" {
		row.Model = parsed.Model
	}
	u := parsed.Usage
	row.Input, row.Output, row.Cached, row.Write = u.Input, u.Output, u.Details.Cached, u.Details.Write
	row.Status = "completed"
	row.CostUSD = lab.Luna.Cost(u.Input, u.Details.Cached, u.Details.Write, 0)
}

// newThreadArm builds the two participants, the router and the Coordinator.
func newThreadArm(ctx context.Context, arm string, run *sessionRun) (*threadArm, error) {
	e, s := run.env, run.sess
	edgeDesc := thread.Descriptor{Name: threadEdge, Zone: thread.ZoneEdge, Description: threadDesc,
		Window: Window8K, SummaryBudget: forkBTarget, BriefMaxRunes: threadBriefRunes}
	cloudDesc := thread.Descriptor{Name: threadCloud, Zone: thread.ZoneCloud, Description: threadDesc, Window: CloudWindow}
	descs := []thread.Descriptor{edgeDesc, cloudDesc}
	prompt := func(d thread.Descriptor) string {
		return run.system + "\n\n" + threadWorkspaceNote + "\n\n" + mtpart.SystemPromptSection(d, descs)
	}
	base := func(state *agent.AgentState, tk *tool.Toolkit) []agent.AgentOption {
		opts := []agent.AgentOption{
			agent.WithModelStreaming(false),
			agent.WithPermissionContext(permission.NewContext(permission.ModeBypass)),
			agent.WithToolkit(tk),
			agent.WithState(state),
			agent.WithStateSaver(&memSaver{}),
			// One attempt at this layer, as on every other arm: the transports retry.
			agent.WithModelConfig(agent.ModelConfig{MaxRetries: 1}),
		}
		if run.react.MaxIters > 0 {
			opts = append(opts, agent.WithReactConfig(run.react))
		}
		return opts
	}

	// The edge: the local model with stage 0's window, reserve and summary total.
	localChat, err := run.localModel(arm)
	if err != nil {
		return nil, err
	}
	edgeState := &agent.AgentState{SessionID: arm + "/" + s.ID + "/edge"}
	edgeModel := &watchModel{inner: e.counted(localChat), c: &run.tr.c, state: edgeState}
	edgePrep, err := mtpart.Prepare(edgeDesc, mtpart.PrepareOptions{})
	if err != nil {
		return nil, err
	}
	edgeTk := tool.NewToolkit(append(edgePrep.Tools(), run.rec.workspaceTools(run.ws, corpus.SideLocal)...)...)
	edgeCfg := localConfig(Window8K, localSummaryTotal)
	edgeCfg.ThreadSummaryTokens = localSummaryTotal
	edgeOpts := append(base(edgeState, edgeTk), agent.WithContextConfig(&edgeCfg))
	edgeAgent := agent.NewUnifiedAgent(threadEdge, prompt(edgeDesc), edgeModel, append(edgeOpts, edgePrep.Options()...)...)
	edgeP, err := edgePrep.Participant(mtpart.AgentConfig{Descriptor: edgeDesc, Agent: edgeAgent})
	if err != nil {
		return nil, err
	}

	// The cloud: the Responses adapter over the same transport as every other arm. Its
	// forks go through it too, so they are booked to this session.
	cloudChat, err := e.cloudTransport(arm, s.ID, nil).ChatModel()
	if err != nil {
		return nil, err
	}
	cloudState := &agent.AgentState{SessionID: arm + "/" + s.ID + "/cloud"}
	cloudPrep, err := mtpart.Prepare(cloudDesc, mtpart.PrepareOptions{})
	if err != nil {
		return nil, err
	}
	cloudTk := tool.NewToolkit(append(cloudPrep.Tools(), run.rec.workspaceTools(run.ws, corpus.SideCloud)...)...)
	cloudCfg := agent.ContextConfig{ContextSize: CloudWindow}
	cloudOpts := append(base(cloudState, cloudTk), agent.WithContextConfig(&cloudCfg))
	cloudAgent := agent.NewUnifiedAgent(threadCloud, prompt(cloudDesc), forkTagger{cloudChat}, append(cloudOpts, cloudPrep.Options()...)...)
	cloudP, err := cloudPrep.Participant(mtpart.AgentConfig{Descriptor: cloudDesc, Agent: cloudAgent})
	if err != nil {
		return nil, err
	}

	rcfg := route.Config{Mode: route.Hybrid, Edge: threadEdge, Cloud: threadCloud, Threshold: route.DefaultThreshold}
	if arm == ArmMTRouted {
		rcfg.Rules = []route.Rule{scriptedRoute(s.Turns)}
	} else {
		if rcfg.Classifier, err = e.threadClassifier(arm, s.ID); err != nil {
			return nil, err
		}
		// Amendment 17's hybrid decides over the thread state, history
		// included; the router now sends the query alone by default.
		rcfg.IncludeHistory = true
	}
	router, err := route.New(rcfg)
	if err != nil {
		return nil, err
	}
	c, err := thread.NewCoordinator(ctx, thread.Config{
		Store:        thread.NewMemoryStore(),
		Participants: []thread.Participant{edgeP, cloudP},
		Router:       router,
		// Stage 0's refresh and output limit; the window-share rule keeps its default.
		Summaries: thread.SummaryPolicy{PeerTarget: threadEdge, PeerEvery: forkBRefresh, PeerMaxTokens: forkBLimit},
	})
	if err != nil {
		return nil, err
	}
	a := &threadArm{run: run, arm: arm, c: c, done: make(chan struct{}), counts: map[string]int{}}
	go a.drain()
	return a, nil
}

// drain reads the Coordinator's events until Close: the foreground reply text, routes,
// entries and errors.
func (a *threadArm) drain() {
	defer close(a.done)
	for ev := range a.c.Events() {
		a.observe(&ev)
	}
}

func (a *threadArm) observe(ev *thread.Event) {
	a.mu.Lock()
	defer a.mu.Unlock()
	switch ev.Kind {
	case thread.EventRoute:
		if d := ev.Route; d != nil {
			a.counts["route_"+d.Target]++
			a.counts["route_by_"+string(d.Source)]++
			a.counts["classifier_calls"] += d.ClassifierCalls
		}
	case thread.EventEntry:
		if en := ev.Entry; en != nil {
			a.counts["entry_"+string(en.Kind)]++
			if en.Owner != nil {
				a.counts["owner_"+string(en.Owner.Reason)+"_to_"+en.Owner.To]++
			}
			if en.Marker != nil {
				a.counts["marker_"+string(en.Marker.Kind)]++
			}
		}
	case thread.EventError:
		a.counts["thread_errors"]++
		if ev.Err != nil && len(a.errs) < 5 {
			a.errs = append(a.errs, ev.Err.Error())
		}
	case thread.EventAgent:
		if !ev.Foreground {
			return
		}
		switch e := ev.Agent.(type) {
		case event.ModelCallStartEvent:
			if a.text.Len() > 0 {
				a.text.WriteByte('\n')
			}
		case event.TextBlockDeltaEvent:
			a.text.WriteString(e.Delta)
		case event.ReplyEndEvent:
			switch e.FinishedReason {
			case types.ReplyError:
				a.counts["reply_errors"]++
			case types.ReplyExceedMaxIters:
				a.counts["max_iters"]++
			}
		}
	}
}

// ask submits one user message and waits until the thread is idle again: the reply,
// any transfer, background task and summary it caused. The router picks the owner.
func (a *threadArm) ask(ctx context.Context, index int, text string) error {
	r := a.run
	r.rec.begin(index, 0)
	defer r.rec.end()
	a.mu.Lock()
	a.text.Reset()
	a.mu.Unlock()
	if _, err := a.c.Submit(ctx, thread.Query{OperationID: fmt.Sprintf("turn-%d", index), Text: text}); err != nil {
		return fmt.Errorf("turn %d: %w", index, err)
	}
	if err := a.c.WaitIdle(ctx); err != nil {
		return fmt.Errorf("turn %d: %w", index, err)
	}
	// A cloud call refused by the hard stop fails over to the edge inside the thread, so
	// the stop has to end the session here or the row would hold work it never paid for.
	if r.env.overBudget() {
		return lab.ErrBudget
	}
	a.mu.Lock()
	reply := strings.TrimSpace(a.text.String())
	a.mu.Unlock()
	if reply == "" {
		r.tr.c.EmptyReplies++
	}
	r.replies[index] = reply
	return nil
}

// play runs the session's scripted user messages in order.
func (a *threadArm) play(ctx context.Context) error {
	for i, turn := range a.run.sess.Turns {
		if err := a.ask(ctx, i, turn.User); err != nil {
			return err
		}
	}
	return nil
}

// repair sends the compiler's errors as one more user message. It goes through the
// router like any other, so it reaches whoever the thread would give it to.
func (a *threadArm) repair(ctx context.Context, output string, index int) error {
	if err := a.ask(ctx, index, repairText(output)); err != nil {
		return fmt.Errorf("repair turn %d: %w", index, err)
	}
	return nil
}

// close stops the Coordinator and folds what it saw into the row's counters.
func (a *threadArm) close(row *Row) {
	_ = a.c.Close()
	<-a.done
	for kind, t := range a.c.Ledger().TotalsByKind() {
		a.counts["thread_calls_"+string(kind)] = t.Calls
	}
	if row == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if row.Counters == nil {
		row.Counters = map[string]int{}
	}
	for k, v := range a.counts {
		row.Counters["mt_"+k] = v
	}
	row.Counters["max_iters"] += a.counts["max_iters"]
	if len(a.errs) > 0 && row.Error == "" {
		row.ThreadErrors = make([]string, 0, len(a.errs))
		for _, e := range a.errs {
			row.ThreadErrors = append(row.ThreadErrors, scrub(e, a.run.env.APIKey))
		}
	}
}

// sessionCloudCost is what the ledger holds for one session on the cloud side, the
// classifier's calls included.
func sessionCloudCost(e *Env, arm, session string) float64 {
	var usd float64
	if e.Ledger == nil {
		return 0
	}
	e.Ledger.Scan(func(lr *lab.Row) bool {
		if lr.Arm == arm && lr.Session == session && lr.Side == lab.SideCloud {
			usd += lr.CostUSD
		}
		return true
	})
	return usd
}

// FixedThreadRoute is a classifier for smoke runs that sends every query to target ("edge"
// or "cloud") at confidence 0.95, so one side's path can be exercised on purpose. The CLI
// allows it on the tuning set only.
func FixedThreadRoute(target string) (func(arm, session string) (classifier.DispatchClassifier, error), error) {
	other := threadCloud
	switch target {
	case threadEdge:
	case threadCloud:
		other = threadEdge
	default:
		return nil, fmt.Errorf("fixed route %q: want %q or %q", target, threadEdge, threadCloud)
	}
	return func(string, string) (classifier.DispatchClassifier, error) {
		return classifier.NewFake(&classifier.FakeConfig{Response: classifier.Response{
			Model: "fixed-route",
			Answers: map[string]classifier.Answer{route.QuestionID: &classifier.ChoiceAnswer{
				Choice: target, Confidence: 0.95,
				Probabilities: map[string]float64{target: 0.95, other: 0.05},
			}},
		}}), nil
	}, nil
}
