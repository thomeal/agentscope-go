package arms

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/agentscope-ai/agentscope-go/v2/experiments/stage0/corpus"
	"github.com/agentscope-ai/agentscope-go/v2/experiments/stage0/lab"
	"github.com/agentscope-ai/agentscope-go/v2/experiments/stage0/s3"
	"github.com/agentscope-ai/agentscope-go/v2/pkg/agentscope/agent"
	"github.com/agentscope-ai/agentscope-go/v2/pkg/agentscope/classifier"
	"github.com/agentscope-ai/agentscope-go/v2/pkg/agentscope/message"
	"github.com/agentscope-ai/agentscope-go/v2/pkg/agentscope/model"
	"github.com/agentscope-ai/agentscope-go/v2/pkg/agentscope/tool"
)

// ArmRoute is the one arm of the routing experiment. A router never changes what either
// side does: both sides answer every item and each router is scored on what it chose.
const ArmRoute = "S3"

// RouteArms are the arms that run on S3 items.
var RouteArms = []string{ArmRoute}

// The freshness levels of the local summary. Each is a world: the local side answers
// with that summary and a router's state holds that summary.
const (
	WorldFresh = "r5k"
	WorldStale = "r20k"
	WorldNone  = "none"
)

// Routers as they appear in a row. R4, the oracle, is computed from the outcomes.
const (
	RouterOwner  = "R0"
	RouterRules  = "R1"
	RouterRulesS = "R1s"
	RouterQuery  = "R2"
	RouterState  = "R3"
)

const (
	refreshFresh = 5000
	refreshStale = 20000

	// recentSteps is how many published steps a router sees. S3 puts a followup's fact
	// in the last three, so this holds at least those.
	recentSteps = 5
	// routeImportBudget bounds what local imports past its summary, in exact tokens. It
	// is C6's delta budget. With no summary the full log would overflow the window, and
	// the older entries stay readable through thread_read.
	routeImportBudget = c6DeltaBudget
	// routeClassifyTries bounds one decision's calls. The adapters make one attempt and
	// leave retries to the caller, and a rate limit is not a fact about routing.
	routeClassifyTries = 3
)

// routeClassifyBackoff is a var so that a test does not wait for it.
var routeClassifyBackoff = 2 * time.Second

// routeInstructions is the one Choice question. It states the cost of each side and no
// rule from the corpus.
const routeInstructions = `Two assistants serve this session. Decide which one should answer the user's new query. Prefer the local assistant whenever it can answer correctly, because it is cheaper and keeps data on the device. Choose the cloud assistant when the answer needs something only the cloud has, or reasoning too hard for a small model.`

var routeChoices = map[string]string{
	string(corpus.SideCloud): "Large model with the whole session history in context, including the full results of every record it read. It can read records whose ids start with R-. It handles hard reasoning well.",
	string(corpus.SideLocal): "Small model with an 8K window. It has the last published messages of the session, any summary, and its own notes. It can read records whose ids start with L-. It cannot see the results of records the cloud read unless the summary states them.",
}

// NamedClassifier is one classifier under test. Sem bounds its concurrent calls. Cloud
// says the calls leave the machine and cost money at Prices.
type NamedClassifier struct {
	Name   string
	C      classifier.Classifier
	Cloud  bool
	Prices lab.Prices
	Sem    chan struct{}
}

// SideResult is one side's answer to the query.
type SideResult struct {
	Correct bool   `json:"correct"`
	Reply   string `json:"reply"`
	Error   string `json:"error,omitempty"`
	// HoldsAnswer marks a reply that holds the answer and scored wrong. S3's strict score
	// may understate a weak model, and this is how much.
	HoldsAnswer bool `json:"holds_answer,omitempty"`
}

// WorldResult is what local did with one summary.
type WorldResult struct {
	Local        SideResult `json:"local"`
	HasSummary   bool       `json:"has_summary"`
	SummaryChars int        `json:"summary_chars"`
	// BehindTokens is how much of the history the summary does not cover.
	BehindTokens int `json:"behind_tokens"`
	Imported     int `json:"imported"`
	Dropped      int `json:"imports_dropped"`
}

// RouteDecision is one classifier's answer, kept in full so that a threshold can be
// applied afterwards.
type RouteDecision struct {
	Router        string             `json:"router"`
	World         string             `json:"world,omitempty"`
	Classifier    string             `json:"classifier"`
	Choice        string             `json:"choice,omitempty"`
	Confidence    float64            `json:"confidence"`
	Probabilities map[string]float64 `json:"probabilities,omitempty"`
	// LatencyMS and InputTokens describe the last attempt. Every attempt has a ledger row.
	LatencyMS   int64 `json:"latency_ms"`
	StateBytes  int   `json:"state_bytes"`
	InputTokens int64 `json:"input_tokens"`
	// CanaryHits counts local-only text in the state this classifier saw.
	CanaryHits int    `json:"canary_hits,omitempty"`
	Error      string `json:"error,omitempty"`
}

// RouteOutcome is one item's result. Kind, PairID and Seed are host data, written so the
// report can break results down and resample pairs together. No router ever saw them.
type RouteOutcome struct {
	Kind          string                  `json:"kind"`
	PairID        string                  `json:"pair_id,omitempty"`
	Seed          int64                   `json:"seed"`
	Owner         string                  `json:"owner"`
	Disclosure    string                  `json:"disclosure"`
	CloudEligible bool                    `json:"cloud_eligible"`
	Cloud         SideResult              `json:"cloud"`
	Worlds        map[string]*WorldResult `json:"worlds"`
	Rules         map[string]string       `json:"rules"`
	Decisions     []RouteDecision         `json:"decisions"`
}

// routeRun is one item.
type routeRun struct {
	env       *Env
	it        s3.Item
	system    string
	rec       *toolRec
	tr        *tracker
	log       *threadLog
	transport *lab.Cloud
	cloud     *participant
	peers     map[string]*summary
	covered   map[string]int // history index a world's summary reaches
	replies   map[int]string
	out       *RouteOutcome
}

// PlanRoute orders S3 jobs. Items are independent, so no shuffle is needed.
func PlanRoute(items []s3.Item) []Job {
	jobs := make([]Job, 0, len(items))
	for i := range items {
		jobs = append(jobs, Job{Arm: ArmRoute, Route: &items[i]})
	}
	return jobs
}

// opaqueID names an item to anything outside the host. Item IDs run in order, and a pair's
// two halves sit side by side, so the ID itself says something the corpus keeps host-only.
func opaqueID(run, id string) string {
	sum := sha256.Sum256([]byte(run + "/" + id))
	return hex.EncodeToString(sum[:6])
}

func newRouteOutcome(it s3.Item) *RouteOutcome {
	return &RouteOutcome{
		Kind: string(it.Kind), PairID: it.PairID, Seed: it.Seed,
		Owner: string(it.Owner), Disclosure: string(it.Disclosure),
		CloudEligible: it.Disclosure != corpus.EdgeOnly,
		Worlds:        map[string]*WorldResult{},
		Rules:         routeRules(it),
		Decisions:     []RouteDecision{},
	}
}

func (e *Env) execRoute(ctx context.Context, it s3.Item) ([]Row, error) {
	r := &routeRun{
		env: e, it: it,
		system:  marker(e.Run, ArmRoute, opaqueID(e.Run, it.ID)) + "\n" + it.System,
		rec:     newToolRec(),
		tr:      newTracker(ArmRoute, it.ID, "s3-"+it.Set, len(it.History), nil),
		log:     &threadLog{},
		peers:   map[string]*summary{},
		covered: map[string]int{},
		replies: map[int]string{},
		out:     newRouteOutcome(it),
	}
	begin := time.Now()
	err := r.run(ctx)
	if isBudget(err) || e.overBudget() {
		return nil, lab.ErrBudget
	}
	if err != nil {
		r.tr.err = err
		r.tr.c.SessionErrors++
	}
	r.tr.elapsed = time.Since(begin)
	row := r.tr.row(e, 0, 0)
	row.Route = r.out
	return []Row{row}, nil
}

func (r *routeRun) run(ctx context.Context) error {
	for _, st := range r.it.LocalOwn {
		r.log.addTurn(corpus.SideLocal, corpus.EdgeOnly, st.User, stepCalls(st), st.Reply)
	}
	if err := r.buildCloud(ctx); err != nil {
		return err
	}
	if err := r.answerCloud(ctx); err != nil {
		return err
	}
	for _, world := range []string{WorldFresh, WorldStale, WorldNone} {
		if err := r.answerLocal(ctx, world); err != nil {
			return err
		}
	}
	return r.classify(ctx)
}

// stepTokens is S3's own estimate of one step, which is what a refresh is counted in.
func stepTokens(st corpus.CStep) int {
	return s3.EstimateTokens(st.User) + s3.EstimateTokens(st.Reply) + s3.EstimateTokens(st.ToolResult)
}

// forkStep returns the history index where the last Fork B of a refresh would fire: the
// step that crossed the last multiple of refresh. It returns -1 when none did.
func forkStep(steps []corpus.CStep, refresh int) int {
	total, at := 0, -1
	for i, st := range steps {
		before := total / refresh
		total += stepTokens(st)
		if total/refresh > before {
			at = i
		}
	}
	return at
}

// tokensAfter is how much of the history lies past index k.
func tokensAfter(steps []corpus.CStep, k int) int {
	n := 0
	for _, st := range steps[k+1:] {
		n += stepTokens(st)
	}
	return n
}

// routeCanaries are the codes of the local records and of LocalOwn, which must never
// reach the cloud. S3 keeps them apart by construction, and this checks that it stays so.
func routeCanaries(it s3.Item) []string {
	seen := map[string]bool{}
	add := func(text string) {
		for _, code := range corpus.Codes(text) {
			seen[code] = true
		}
	}
	for id, text := range it.Records {
		if strings.HasPrefix(id, "L-") {
			add(text)
		}
	}
	for _, st := range it.LocalOwn {
		add(st.User + "\n" + st.ToolResult + "\n" + st.Reply)
	}
	out := make([]string, 0, len(seen))
	for code := range seen {
		out = append(out, code)
	}
	slices.Sort(out)
	return out
}

func (r *routeRun) readRecord(side corpus.Side, id string) (string, bool) {
	return s3.Read(r.it, side, id)
}

// buildCloud walks the cloud through the history. The scripted steps are seeded, and each
// fork point is one live call on a real prefix, as in the C group, so a fork reuses it.
// The walk is one line, so each later call writes only what grew.
func (r *routeRun) buildCloud(ctx context.Context) error {
	it := r.it
	r.transport = r.env.cloudTransport(ArmRoute, it.ID, routeCanaries(it))
	chat, err := r.transport.ChatModel()
	if err != nil {
		return err
	}
	tk := tool.NewToolkit(r.rec.recordTool(corpus.SideCloud, r.readRecord), r.rec.threadTool(r.log, corpus.SideCloud))
	cfg := agent.ContextConfig{ContextSize: CloudWindow}
	st := &agent.AgentState{SessionID: ArmRoute + "/" + it.ID + "/cloud"}
	points := map[int][]string{}
	for world, refresh := range map[string]int{WorldFresh: refreshFresh, WorldStale: refreshStale} {
		if k := forkStep(it.History, refresh); k >= 0 {
			points[k] = append(points[k], world)
		}
	}
	steps := make([]int, 0, len(points))
	for k := range points {
		steps = append(steps, k)
	}
	slices.Sort(steps)

	pos := 0
	var prev *summary
	for _, k := range steps {
		st.Context = append(st.Context, r.seed(pos, k)...)
		r.cloud = newParticipant(chat, r.system, tk, cfg, st, &r.tr.c, buildOpts{})
		r.rec.begin(k, 0)
		out, err := r.cloud.reply(lab.WithRole(ctx, lab.RoleMain), it.History[k].User)
		calls := r.rec.end()
		if err != nil {
			return fmt.Errorf("live call at step %d: %w", k, err)
		}
		// The log takes the scripted reply. A live reply could restate the record it just read,
		// and S3's tool results reach local only through a summary.
		r.log.addTurn(corpus.SideCloud, corpus.Shared, it.History[k].User, calls, it.History[k].Reply)
		r.replies[k] = out.All
		if snap, ok := r.transport.Snapshot(); ok {
			next, err := forkB(ctx, r.transport, snap, r.log, prev, &r.tr.c)
			if err != nil {
				return err
			}
			if next != nil {
				prev = next
				for _, world := range points[k] {
					r.peers[world], r.covered[world] = next, k
				}
			}
		}
		if st, err = r.cloud.snapshot(ctx); err != nil {
			return err
		}
		pos = k + 1
	}
	st.Context = append(st.Context, r.seed(pos, len(it.History))...)
	r.cloud = newParticipant(chat, r.system, tk, cfg, st, &r.tr.c, buildOpts{})
	return nil
}

// seed logs the scripted steps in [from, to) and returns their messages.
func (r *routeRun) seed(from, to int) []*message.Msg {
	steps := r.it.History[from:to]
	for i, st := range steps {
		r.log.addTurn(corpus.SideCloud, corpus.Shared, st.User, stepCalls(st), st.Reply)
		r.replies[from+i] = st.Reply
	}
	return seedMsgs(steps)
}

func (r *routeRun) queryText() string {
	return r.it.Query + "\n\n" + r.it.AnswerPrompt
}

// answerCloud asks the cloud, unless disclosure rules forbid sending it the query.
func (r *routeRun) answerCloud(ctx context.Context) error {
	if !r.out.CloudEligible {
		return nil
	}
	clone, err := r.cloud.clone(ctx, r.cloud.chat, &r.tr.c, buildOpts{react: agent.ReactConfig{MaxIters: probeToolCalls + 1}})
	if err != nil {
		return err
	}
	res, err := r.ask(ctx, clone)
	r.out.Cloud = res
	return err
}

// ask runs the query with the same tool bound a probe has. Only budget and cancellation
// are errors, because any other failure counts as a wrong answer.
func (r *routeRun) ask(ctx context.Context, p *participant) (SideResult, error) {
	r.rec.begin(-1, probeToolCalls)
	defer r.rec.end()
	out, err := p.reply(lab.WithRole(ctx, lab.RoleProbe), r.queryText())
	if err != nil {
		if isBudget(err) || ctx.Err() != nil {
			return SideResult{}, err
		}
		r.tr.c.ProbeErrors++
		return SideResult{Error: scrub(err.Error(), r.env.APIKey)}, nil
	}
	correct := s3.Score(r.it, out.Final)
	return SideResult{
		Correct: correct, Reply: scrub(out.Final, r.env.APIKey),
		HoldsAnswer: !correct && strings.Contains(strings.ToLower(out.Final), strings.ToLower(r.it.Answer)),
	}, nil
}

// worldLog is the log as this world knows it. Summaries of other worlds are blanked, or
// thread_read would hand the stale world a fresher summary.
func (r *routeRun) worldLog(world string) *threadLog {
	out := &threadLog{entries: slices.Clone(r.log.entries)}
	var keep string
	if p := r.peers[world]; p != nil {
		keep = p.Text
	}
	for i := range out.entries {
		if e := &out.entries[i]; e.Kind == kindSummary && e.Text != keep {
			e.Text = "(not available)"
		}
	}
	return out
}

// budgetImports keeps the newest imports that fit the budget, in their own order.
func (r *routeRun) budgetImports(ctx context.Context, msgs []*message.Msg) ([]*message.Msg, int) {
	used, start := 0, len(msgs)
	for start > 0 {
		txt := msgs[start-1].GetTextContent("")
		n := 0
		if txt != nil {
			n = r.env.Tokens.Count(ctx, *txt)
		}
		if used+n > routeImportBudget {
			break
		}
		used += n
		start--
	}
	return msgs[start:], start
}

// answerLocal builds the local agent of one world and asks it. LocalOwn stays in its
// context in every world, because this experiment is about routing and C8 is about clearing.
func (r *routeRun) answerLocal(ctx context.Context, world string) error {
	w := &WorldResult{}
	r.out.Worlds[world] = w
	log := r.worldLog(world)
	peer := r.peers[world]
	coverage := 0
	if peer != nil {
		w.HasSummary, w.SummaryChars = true, len([]rune(peer.Text))
		w.BehindTokens = tokensAfter(r.it.History, r.covered[world])
		coverage = peer.Coverage
	} else {
		w.BehindTokens = tokensAfter(r.it.History, -1)
	}
	chat, err := r.env.localChat(ArmRoute, r.it.ID)
	if err != nil {
		return err
	}
	tk := tool.NewToolkit(r.rec.recordTool(corpus.SideLocal, r.readRecord), r.rec.threadTool(log, corpus.SideLocal))
	index := log.edgeIndex()
	counted := r.env.counted(chat)
	p := newParticipant(counted, localSystem(r.system, s1LogRules, peer, index), tk,
		ownerConfig(counted, peer),
		&agent.AgentState{SessionID: ArmRoute + "/" + r.it.ID + "/local/" + world, Context: seedMsgs(r.it.LocalOwn)},
		&r.tr.c,
		buildOpts{react: agent.ReactConfig{MaxIters: probeToolCalls + 1}, compressSystem: compressSystemFor(localSystem(r.system, s1LogRules, nil, index), peer), noSaver: true})
	imports, dropped := r.budgetImports(ctx, log.imports(coverage, corpus.SideLocal))
	w.Imported, w.Dropped = len(imports), dropped
	if err := p.agent.Observe(ctx, imports); err != nil {
		return err
	}
	res, err := r.ask(ctx, p)
	w.Local = res
	return err
}

// routeRules are the routers with no classifier. Every router obeys the disclosure rule
// first, so a query marked edge_only never reaches the cloud by any route.
func routeRules(it s3.Item) map[string]string {
	owner := string(it.Owner)
	hard := ""
	switch {
	case it.Disclosure == corpus.EdgeOnly:
		hard = string(corpus.SideLocal)
	case localIDShape.MatchString(it.Query):
		hard = string(corpus.SideLocal)
	case cloudIDShape.MatchString(it.Query):
		hard = string(corpus.SideCloud)
	}
	rules := owner
	if hard != "" {
		rules = hard
	}
	// The literal rule ignores the owner. A label stated in recent text beats an R-n in the
	// query, because S3 names records whose content the conversation has already restated.
	literal := string(corpus.SideCloud)
	if hard == string(corpus.SideLocal) || labelInRecent(it.Query, s3.PublishedRecent(it)) {
		literal = string(corpus.SideLocal)
	}
	ownerOnly := owner
	if it.Disclosure == corpus.EdgeOnly {
		ownerOnly = string(corpus.SideLocal)
	}
	return map[string]string{RouterOwner: ownerOnly, RouterRules: rules, RouterRulesS: literal}
}

var (
	localIDShape = regexp.MustCompile(`\bL-\d+`)
	cloudIDShape = regexp.MustCompile(`\bR-\d+`)
	wordShape    = regexp.MustCompile(`[a-z0-9]+`)
)

// labelShape finds a label with its status in a lowercased line: "restore point for the
// ledger service: aaa-1111", "the restore point for the ledger service is aaa-1111", or "the
// restore point for the ledger service was revoked", where the answer is UNKNOWN.
var labelShape = regexp.MustCompile(`([a-z ]+?) for the ([a-z0-9 -]+?)(?:(?::| is) [0-9a-z]{2,}-[0-9]{3,}| was )`)

// labelInRecent says whether the query asks for a label that recent text states with a
// value: the same kind of thing for the same subject on one line. S3 plants near misses
// that share one and not the other, so both have to match.
func labelInRecent(query, recent string) bool {
	q := " " + joinWords(query) + " "
	for line := range strings.SplitSeq(strings.ToLower(recent), "\n") {
		for _, m := range labelShape.FindAllStringSubmatch(line, -1) {
			kind := strings.TrimPrefix(joinWords(m[1]), "the ")
			subject := joinWords(m[2])
			if kind != "" && subject != "" && strings.Contains(q, " "+kind+" ") && strings.Contains(q, " "+subject+" ") {
				return true
			}
		}
	}
	return false
}

func joinWords(text string) string {
	return strings.Join(wordShape.FindAllString(strings.ToLower(text), -1), " ")
}

// routeRecent is the published tail as a router sees it: user text, reply text and the
// tool's name. It reads the log, so a live reply stands where it replaced a scripted one.
func (r *routeRun) routeRecent() []map[string]string {
	var out []map[string]string
	var current map[string]string
	for _, e := range r.log.entries {
		if e.Visibility != corpus.Shared {
			continue
		}
		switch e.Kind {
		case kindUser:
			current = map[string]string{"user": e.Text}
			out = append(out, current)
		case kindToolSummary:
			if current != nil {
				name, _, _ := strings.Cut(e.Text, " ")
				current["tool"] = name
			}
		case kindReply:
			if current != nil {
				current["reply"] = e.Text
			}
		}
	}
	if len(out) > recentSteps {
		out = out[len(out)-recentSteps:]
	}
	return out
}

// routeState is what a router may see. It holds no host field. With session set it adds
// the summary, the published tail and what each participant has, which is shared data and
// nothing from the local side, so the same state can go to a hosted classifier.
func (r *routeRun) routeState(withSession bool, world string) (json.RawMessage, error) {
	state := map[string]any{"query": r.it.Query}
	if withSession {
		session := map[string]any{
			"owner":  string(r.it.Owner),
			"recent": r.routeRecent(),
			"participants": map[string]any{
				string(corpus.SideCloud): map[string]any{"window_tokens": CloudWindow, "tools": []string{"read_record"}},
				string(corpus.SideLocal): map[string]any{"window_tokens": Window8K, "tools": []string{"read_local", "thread_read"}},
			},
		}
		if p := r.peers[world]; p != nil {
			session["summary"] = p.Text
			session["summary_behind_tokens"] = tokensAfter(r.it.History, r.covered[world])
		}
		state["session"] = session
	}
	return json.Marshal(state)
}

// classify asks every classifier about this item, once with the query alone and once per
// world with the session. An edge_only item has one possible route, so no classifier sees
// it: the rule comes first in the plan's order, and a hosted classifier is an outbound call.
func (r *routeRun) classify(ctx context.Context) error {
	if !r.out.CloudEligible {
		return nil
	}
	for _, cl := range r.env.Classifiers {
		if err := r.decide(ctx, cl, RouterQuery, "", false); err != nil {
			return err
		}
		for _, world := range []string{WorldFresh, WorldStale, WorldNone} {
			if err := r.decide(ctx, cl, RouterState, world, true); err != nil {
				return err
			}
		}
	}
	return nil
}

func (r *routeRun) decide(ctx context.Context, cl NamedClassifier, router, world string, withSession bool) error {
	state, err := r.routeState(withSession, world)
	if err != nil {
		return err
	}
	d := RouteDecision{Router: router, World: world, Classifier: cl.Name, StateBytes: len(state)}
	if cl.Cloud && r.env.overBudget() {
		return lab.ErrBudget
	}
	if cl.Sem != nil {
		select {
		case cl.Sem <- struct{}{}:
			defer func() { <-cl.Sem }()
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	for _, canary := range routeCanaries(r.it) {
		d.CanaryHits += strings.Count(string(state), canary)
	}
	var (
		resp    *classifier.Response
		callErr error
	)
	for try := 1; ; try++ {
		begin := time.Now()
		resp, callErr = cl.C.Classify(ctx, classifier.Request{
			OperationID: opaqueID(r.env.Run, r.it.ID) + "/" + router + "/" + world + "/" + cl.Name,
			State:       state,
			Questions:   map[string]classifier.Question{"route": &classifier.ChoiceQuestion{Instructions: routeInstructions, Choices: routeChoices}},
		})
		d.LatencyMS, d.InputTokens = time.Since(begin).Milliseconds(), 0
		if ctx.Err() != nil {
			return ctx.Err()
		}
		r.charge(cl, resp, callErr, &d, try)
		if callErr == nil || try == routeClassifyTries || !model.IsRetryableError(callErr) {
			break
		}
		select {
		case <-time.After(routeClassifyBackoff * time.Duration(try)):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if callErr != nil {
		d.Error = scrub(callErr.Error(), r.env.APIKey)
	} else if a, ok := resp.Answers["route"].(*classifier.ChoiceAnswer); ok {
		d.Choice, d.Confidence, d.Probabilities = a.Choice, a.Confidence, a.Probabilities
	} else {
		d.Error = "no choice in the answers"
	}
	r.out.Decisions = append(r.out.Decisions, d)
	return nil
}

// charge writes the classifier's attempt to the ledger, so its cost and failures sit with
// everything else. A failed call that reports no usage shows as an error row.
func (r *routeRun) charge(cl NamedClassifier, resp *classifier.Response, callErr error, d *RouteDecision, try int) {
	chargeClassifier(r.env, r.it.ID, cl, resp, callErr, d, try)
}

func chargeClassifier(e *Env, session string, cl NamedClassifier, resp *classifier.Response, callErr error, d *RouteDecision, try int) {
	row := lab.Row{
		Arm: "cls:" + cl.Name, Session: session, Side: lab.SideLocal, Role: "classifier",
		LatencyMS: d.LatencyMS, Attempt: try, HTTP: 200, Status: "completed",
	}
	if cl.Cloud {
		row.Side = lab.SideCloud
	}
	if callErr != nil {
		row.HTTP, row.Status = 0, ""
		row.Error = scrub(callErr.Error(), e.APIKey)
	}
	row.UsageMissing = true
	if resp != nil {
		row.Model = resp.Model
		for _, att := range resp.Attempts {
			if att.Usage == nil || att.Usage.InputTokens == nil {
				continue
			}
			row.UsageMissing = false
			row.Input = int(*att.Usage.InputTokens)
			if att.Usage.OutputTokens != nil {
				row.Output = int(*att.Usage.OutputTokens)
			}
			row.CostUSD = cl.Prices.Cost(row.Input, 0, 0, row.Output)
			d.InputTokens = int64(row.Input)
		}
	}
	e.Ledger.Add(row)
}

// errNoClassifier is why a route run refuses to start with nothing to evaluate.
var errNoClassifier = errors.New("a route run needs at least one classifier")

// CheckRouteEnv refuses a route run that could not produce an R2 or R3 row.
func CheckRouteEnv(e *Env) error {
	if len(e.Classifiers) == 0 {
		return errNoClassifier
	}
	return nil
}
