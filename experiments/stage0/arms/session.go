package arms

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/agentscope-ai/agentscope-go/v2/experiments/stage0/corpus"
	"github.com/agentscope-ai/agentscope-go/v2/experiments/stage0/lab"
	"github.com/agentscope-ai/agentscope-go/v2/experiments/stage0/s2"
	"github.com/agentscope-ai/agentscope-go/v2/experiments/stage0/score"
	"github.com/agentscope-ai/agentscope-go/v2/pkg/agentscope/agent"
	"github.com/agentscope-ai/agentscope-go/v2/pkg/agentscope/classifier"
	"github.com/agentscope-ai/agentscope-go/v2/pkg/agentscope/message"
	"github.com/agentscope-ai/agentscope-go/v2/pkg/agentscope/model"
	"github.com/agentscope-ai/agentscope-go/v2/pkg/agentscope/tool"
)

// Env is what every arm shares in one run.
type Env struct {
	Run                  string
	Ledger               *lab.Ledger
	CloudURL, CloudModel string
	APIKey               string
	LocalURL, LocalModel string
	CloudSlots           *lab.Slots
	LocalSlots           *lab.Slots
	Tokens               *lab.Tokenizer

	// Trace, when set, observes internal steps. Tests use it to check branch behavior.
	Trace func(TraceEvent)
	// Dump, when set, sees every request and response body. See lab.Options.Dump.
	Dump func(arm, session, side, role string, req, resp []byte)
	// Classifiers are the routers under test on S3. Other workloads ignore them.
	Classifiers []NamedClassifier
	// ThreadClassifier, when set, builds ArmMTHybrid's classifier for one session. Nil
	// uses OpenAI Decisions on CloudModel, booked to the session.
	ThreadClassifier func(arm, session string) (classifier.DispatchClassifier, error)
}

// TraceEvent is one internal step. State is live: read it inside the callback.
type TraceEvent struct {
	Kind   string // switch_begin, switch_end, fork_b, clear, keep, brief
	Turn   int
	Growth int
	Before int
	After  int
	State  *agent.AgentState
	System string
}

func (e *Env) trace(ev TraceEvent) {
	if e.Trace != nil {
		e.Trace(ev)
	}
}

func (e *Env) cloudTransport(arm, session string, canaries []string) *lab.Cloud {
	return lab.NewCloud(lab.Options{
		Ledger: e.Ledger, Arm: arm, Session: session,
		BaseURL: e.CloudURL, APIKey: e.APIKey, Model: e.CloudModel,
		Slots: e.CloudSlots, Canaries: canaries, Dump: e.Dump,
		CanonicalReplay: canonicalReplay,
	})
}

// localChat builds the local adapter. Its JSON helper already makes three attempts
// on 429 and 5xx, so the transport makes one each; otherwise an outage would cost
// eighteen. Each attempt still gets its own ledger row.
func (e *Env) localChat(arm, session string) (model.ChatModel, error) {
	return e.localChatLimit(arm, session, localReplyLimit)
}

func (e *Env) localChatLimit(arm, session string, limit int) (model.ChatModel, error) {
	l := lab.NewLocal(lab.Options{
		Ledger: e.Ledger, Arm: arm, Session: session,
		BaseURL: e.LocalURL, Model: e.LocalModel,
		Slots: e.LocalSlots, MaxAttempts: 1, Dump: e.Dump,
	}, limit)
	return l.ChatModel()
}

// overBudget is the check after a session. The agent swallows a failed compression call
// and truncates, so a budget stop there can end a session with no error at all, and a
// row written after it would hold truncated work paid for by a request that never ran.
func (e *Env) overBudget() bool {
	return e.Ledger != nil && e.Ledger.OverBudget()
}

// counted applies the exact token counter that every local window uses.
func (e *Env) counted(m model.ChatModel) model.ChatModel {
	return &lab.CountingModel{ChatModel: m, Tokens: e.Tokens}
}

// Counters are the events a row reports. The first eight are the contract's; the
// rest make failed steps visible.
type Counters struct {
	Clears, Keeps, ForkB, ForkFailed, Compress, OverflowRecoveries, LossyEvents, SessionErrors int
	ForkA, ForkMerged, ForkSkipped                                                             int
	BriefFailed, BriefIncomplete, ProbeErrors, ProbeRepairs, EmptyReplies, MaxIters            int
	// SummaryRewrites counts the times a summary was sent back. SummaryTrims counts summaries
	// cut at their target, each of which is also a lossy
	// event. BriefsCompressed counts briefs a two-agent arm's compression folded away.
	SummaryRewrites, SummaryTrims, BriefsCompressed, ForkStrays, EventReads int
	// HardLimitCloud counts local calls that went to the cloud because they did not fit.
	HardLimitCloud int
	// StaleWrites counts S2 writes refused for overwriting a version the writer had not seen,
	// and StaleLocal those by a local agent.
	StaleWrites, StaleLocal int
}

func (c *Counters) toMap() map[string]int {
	return map[string]int{
		"clears": c.Clears, "keeps": c.Keeps, "fork_b": c.ForkB, "fork_failed": c.ForkFailed,
		"compress": c.Compress, "overflow_recoveries": c.OverflowRecoveries,
		"lossy_events": c.LossyEvents, "session_errors": c.SessionErrors,
		"fork_a": c.ForkA, "fork_merged": c.ForkMerged, "fork_skipped": c.ForkSkipped,
		"brief_failed": c.BriefFailed, "brief_incomplete": c.BriefIncomplete,
		"probe_errors": c.ProbeErrors, "probe_repairs": c.ProbeRepairs,
		"empty_replies": c.EmptyReplies, "max_iters": c.MaxIters,
		"summary_rewrites": c.SummaryRewrites, "summary_trims": c.SummaryTrims, "fork_strays": c.ForkStrays, "event_reads": c.EventReads,
		"hard_limit_cloud": c.HardLimitCloud, "stale_writes": c.StaleWrites, "stale_local": c.StaleLocal, "briefs_compressed": c.BriefsCompressed,
	}
}

// Row is one line of scores.jsonl.
type Row struct {
	Arm        string                     `json:"arm"`
	Session    string                     `json:"session"`
	Set        string                     `json:"set"`
	Length     int                        `json:"length"`
	Retention  float64                    `json:"retention"`
	ByCategory map[corpus.Category][2]int `json:"by_category"`
	// ByRecipient splits ByCategory by the side that answered, which is where a
	// handoff method shows: the cloud keeps its whole history in every two-agent arm.
	ByRecipient   map[corpus.Side]map[corpus.Category][2]int `json:"by_recipient"`
	F6Correct     int                                        `json:"f6_correct"`
	F6Total       int                                        `json:"f6_total"`
	Stale         int                                        `json:"stale"`
	ParseFailures int                                        `json:"parse_failures"`
	Batches       int                                        `json:"batches"`
	Questions     int                                        `json:"questions"`
	ExposureSeen  int                                        `json:"exposure_seen"`
	ExposureTotal int                                        `json:"exposure_total"`
	OwnRetention  *float64                                   `json:"own_retention"`
	Counters      map[string]int                             `json:"counters"`
	CanaryHits    int                                        `json:"canary_hits"`
	Seconds       float64                                    `json:"seconds"`
	CostUSD       float64                                    `json:"cost_usd"`
	Error         string                                     `json:"error"`

	// ImputedMainUSD is what the scripted steps of a C-group row would have cost as real
	// main calls. It exists so that a fork's cost can be compared with a main thread.
	ImputedMainUSD float64 `json:"imputed_main_usd,omitempty"`

	// S2 is set on rows of S2 tasks, whose Retention is the hidden-test score.
	S2 *S2Outcome `json:"s2,omitempty"`
	// Route is set on rows of S3 items, which have no retention.
	Route *RouteOutcome `json:"route,omitempty"`
	// ThreadErrors are the first errors a thread arm's Coordinator reported. The thread
	// recovers from them by itself (a failed activation fails over), so they are not a
	// session error.
	ThreadErrors []string `json:"thread_errors,omitempty"`
}

// MarshalJSON writes a null retention when the S2 scorer itself failed, because such a row
// says nothing about the arm and a zero would read as a task that failed its tests, and on
// S3 rows, which measure no retention.
func (r Row) MarshalJSON() ([]byte, error) {
	type plain Row
	if r.Route == nil && (r.S2 == nil || r.S2.Infra == "") {
		return json.Marshal(plain(r))
	}
	return json.Marshal(struct {
		plain
		Retention *float64 `json:"retention"`
	}{plain: plain(r)})
}

// tracker is one row in progress.
type tracker struct {
	arm, session, set string
	length            int
	batches           []corpus.ProbeBatch
	results           []*score.BatchResult
	c                 Counters
	err               error
	elapsed           time.Duration
}

func newTracker(arm, session, set string, length int, batches []corpus.ProbeBatch) *tracker {
	return &tracker{arm: arm, session: session, set: set, length: length, batches: batches, results: make([]*score.BatchResult, len(batches))}
}

// unreachedAnswer grades as every question wrong without counting a parse failure,
// because the model never got to answer.
const unreachedAnswer = `{"answers":[]}`

// row pools the batches. A batch that was never reached scores zero and stays in the
// denominator.
func (t *tracker) row(e *Env, seen, total int) Row {
	graded := make([]score.BatchResult, len(t.batches))
	for i, b := range t.batches {
		if t.results[i] != nil {
			graded[i] = *t.results[i]
		} else {
			graded[i] = score.Batch(b.Questions, unreachedAnswer)
		}
	}
	s := score.Session(t.session, graded)
	r := Row{
		Arm: t.arm, Session: t.session, Set: t.set, Length: t.length,
		Retention: s.Retention, ByCategory: s.ByCategory,
		F6Correct: s.F6Correct, F6Total: s.F6Total, Stale: s.Stale,
		ParseFailures: s.ParseFailures, Batches: s.Batches, Questions: s.Questions,
		ExposureSeen: seen, ExposureTotal: total,
		Counters: t.c.toMap(), Seconds: t.elapsed.Seconds(),
	}
	r.ByRecipient = map[corpus.Side]map[corpus.Category][2]int{}
	for i, b := range t.batches {
		into := r.ByRecipient[b.Recipient]
		if into == nil {
			into = map[corpus.Category][2]int{}
			r.ByRecipient[b.Recipient] = into
		}
		for cat, counts := range graded[i].ByCategory {
			sum := into[cat]
			sum[0] += counts[0]
			sum[1] += counts[1]
			into[cat] = sum
		}
	}
	if own := s.ByCategory[corpus.F5]; own[1] > 0 {
		v := float64(own[0]) / float64(own[1])
		r.OwnRetention = &v
	}
	if t.err != nil {
		r.Error = scrub(t.err.Error(), e.APIKey)
	}
	if e.Ledger != nil {
		e.Ledger.Scan(func(lr *lab.Row) bool {
			if lr.Arm == t.arm && lr.Session == t.session {
				r.CanaryHits += lr.CanaryHits
				if lr.Side == lab.SideCloud {
					r.CostUSD += lr.CostUSD
				}
			}
			return true
		})
	}
	return r
}

var keyShape = regexp.MustCompile(`sk-[A-Za-z0-9_\-*]+`)

// scrub removes the API key from text that may be written to a run directory. A
// provider's error body can quote a key.
func scrub(text, key string) string {
	if key != "" {
		text = strings.ReplaceAll(text, key, "sk-***")
	}
	text = keyShape.ReplaceAllString(text, "sk-***")
	if len(text) > 500 {
		text = text[:500]
	}
	return strings.ToValidUTF8(text, "")
}

// secondary is a checkpoint clone that writes its own row: its own tracker, and
// transports that label the ledger with its own arm name.
type secondary struct {
	tr   *tracker
	chat map[corpus.Side]model.ChatModel
	// tk marks a read clone: local's tools plus the event list. shared lists the batches
	// whose result is the primary's, which the loop fills in once they exist.
	tk     *tool.Toolkit
	shared []int
}

// sessionRun is what one session's arm works with.
type sessionRun struct {
	env    *Env
	arm    string
	sess   corpus.Session
	system string
	tr     *tracker
	rec    *toolRec
	start  time.Time
	spent  time.Duration // time inside secondaries, kept out of the primary's seconds

	// replies holds the published text by turn or step index, for exposure.
	replies map[int]string

	// The S2 settings. On S1 ws is nil, and the zero values mean the S1 defaults.
	ws         *s2.Workspace
	replyLimit int
	react      agent.ReactConfig
	rules      string
	localExtra string
}

// localModel builds the local adapter with this run's reply limit.
func (r *sessionRun) localModel(arm string) (model.ChatModel, error) {
	if r.replyLimit > 0 {
		return r.env.localChatLimit(arm, r.sess.ID, r.replyLimit)
	}
	return r.env.localChat(arm, r.sess.ID)
}

// logText is the explanation of log entries for this run's workload.
func (r *sessionRun) logText() string {
	if r.rules != "" {
		return r.rules
	}
	return s1LogRules
}

// localBase is the system prompt of a local agent with no log and no summary. S2's
// tools are the workspace, so the record text is about S1 alone.
func (r *sessionRun) localBase() string {
	if r.ws != nil {
		return r.system + r.localExtra
	}
	return r.system + "\n\n" + recordRules
}

func marker(run, arm, session string) string {
	return fmt.Sprintf("[stage0 run=%s arm=%s session=%s]", run, arm, session)
}

// armImpl is one arm's rules within the common loop.
type armImpl interface {
	owner(side corpus.Side) *participant
	switchTo(ctx context.Context, from, to corpus.Side, batch int) error
	afterReply(ctx context.Context, turn corpus.Turn, out replyOut, calls []toolCall) error
	secondaries() []*secondary
}

// loop is the session loop of HARNESS.md. Its error is the session error; the
// batches it did not reach stay nil and score zero. A workload with no probes, which is
// S2, only switches and replies.
func (r *sessionRun) loop(ctx context.Context, a armImpl) error {
	probing := len(r.sess.Probes) > 0
	batchAt := map[int]int{}
	for i, b := range r.sess.Probes {
		batchAt[b.AfterTurn] = i
	}
	turns := r.sess.Turns
	for i, turn := range turns {
		if i > 0 && turn.Owner != turns[i-1].Owner {
			idx, ok := batchAt[i-1]
			if probing && (!ok || r.sess.Probes[idx].Recipient != turn.Owner) {
				return fmt.Errorf("corpus: no batch for the new owner after turn %d", i-1)
			}
			r.env.trace(TraceEvent{Kind: "switch_begin", Turn: i - 1})
			if err := a.switchTo(ctx, turns[i-1].Owner, turn.Owner, idx); err != nil {
				return fmt.Errorf("switch after turn %d: %w", i-1, err)
			}
			r.env.trace(TraceEvent{Kind: "switch_end", Turn: i - 1})
			if probing {
				if err := r.probePrimary(ctx, a.owner(turn.Owner), idx); err != nil {
					return fmt.Errorf("probe after turn %d: %w", i-1, err)
				}
			}
		}
		r.rec.begin(i, 0)
		out, err := a.owner(turn.Owner).reply(lab.WithRole(ctx, lab.RoleMain), turn.User)
		calls := r.rec.end()
		if err != nil {
			return fmt.Errorf("turn %d: %w", i, err)
		}
		if out.All == "" {
			r.tr.c.EmptyReplies++
		}
		r.replies[i] = out.All
		if err := a.afterReply(ctx, turn, out, calls); err != nil {
			return fmt.Errorf("after turn %d: %w", i, err)
		}
	}
	if !probing {
		return nil
	}
	last := len(turns) - 1
	idx, ok := batchAt[last]
	if !ok || r.sess.Probes[idx].Recipient != turns[last].Owner {
		return fmt.Errorf("corpus: no final batch for the owner after turn %d", last)
	}
	if err := r.probePrimary(ctx, a.owner(turns[last].Owner), idx); err != nil {
		return fmt.Errorf("final probe: %w", err)
	}
	// Nothing switches at the end, so a clone there would see the primary's state
	// with no brief to add or withhold. Its result is the primary's, at no cost.
	// A read clone still differs there, because it can read what the primary cannot.
	for _, sec := range a.secondaries() {
		if sec.tk == nil {
			sec.tr.results[idx] = r.tr.results[idx]
			continue
		}
		if err := r.readClone(ctx, sec, a.owner(turns[last].Owner), turns[last].Owner, idx); err != nil {
			return fmt.Errorf("final read probe: %w", err)
		}
	}
	return nil
}

// probe runs one batch on a clone with at most probeToolCalls tool calls. The model
// gets one round beyond those to answer with the tools still in place, which keeps
// the cached prefix. Only budget and cancellation are errors: any other failure
// counts as wrong and stays in the denominator.
func (r *sessionRun) probe(ctx context.Context, p *participant, chat model.ChatModel, c *Counters, b corpus.ProbeBatch, extra []*message.Msg) (*score.BatchResult, error) {
	clone, err := p.clone(ctx, chat, c, buildOpts{react: agent.ReactConfig{MaxIters: probeToolCalls + 1}})
	if err != nil {
		return nil, err
	}
	if len(extra) > 0 {
		if err := clone.agent.Observe(ctx, extra); err != nil {
			return nil, err
		}
	}
	r.rec.begin(-1, probeToolCalls)
	defer r.rec.end()
	probeCtx := lab.WithRole(ctx, lab.RoleProbe)
	out, err := clone.reply(probeCtx, b.Prompt)
	if err != nil {
		if isBudget(err) || ctx.Err() != nil {
			return nil, err
		}
		c.ProbeErrors++
		res := score.Batch(b.Questions, "")
		return &res, nil
	}
	res := score.Batch(b.Questions, out.Final)
	if res.Parsed {
		return &res, nil
	}
	// A malformed reply would zero the whole batch, which measures JSON rather than
	// memory. Every arm gets one chance to fix the form, on the same clone and within
	// the same tool bound, and the local server is held to the form: a plain second
	// request repeated the same broken object word for word. What still fails stays
	// a parse failure.
	c.ProbeRepairs++
	out, err = clone.reply(lab.WithAnswerSchema(probeCtx, answerSchema(b.Questions)), repairPrompt)
	if err != nil {
		if isBudget(err) || ctx.Err() != nil {
			return nil, err
		}
		return &res, nil
	}
	repaired := score.Batch(b.Questions, out.Final)
	return &repaired, nil
}

// answerSchema is the wire form of a batch answer, with exactly its question ids.
func answerSchema(qs []corpus.Question) map[string]any {
	ids := make([]any, len(qs))
	for i, q := range qs {
		ids[i] = q.ID
	}
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"required":             []any{"answers"},
		"properties": map[string]any{
			"answers": map[string]any{
				"type":     "array",
				"minItems": len(qs),
				"maxItems": len(qs),
				"items": map[string]any{
					"type":                 "object",
					"additionalProperties": false,
					"required":             []any{"id", "answer"},
					"properties": map[string]any{
						"id":     map[string]any{"type": "string", "enum": ids},
						"answer": map[string]any{"type": "string"},
					},
				},
			},
		},
	}
}

// repairPrompt asks for a malformed batch answer again.
const repairPrompt = `Your last reply was not valid JSON in the required form. Reply again with only the JSON object, one entry for every question id in the form {"id":"q1","answer":"<code or UNKNOWN>"}, and keep your answers the same.`

func (r *sessionRun) probePrimary(ctx context.Context, p *participant, idx int) error {
	res, err := r.probe(ctx, p, p.chat, &r.tr.c, r.sess.Probes[idx], nil)
	if err != nil {
		return err
	}
	r.tr.results[idx] = res
	return nil
}

// probeSecondary runs a batch for a secondary arm and keeps its time apart.
func (r *sessionRun) probeSecondary(ctx context.Context, sec *secondary, p *participant, to corpus.Side, idx int, extra []*message.Msg) error {
	begin := time.Now()
	defer func() {
		d := time.Since(begin)
		sec.tr.elapsed += d
		r.spent += d
	}()
	res, err := r.probe(ctx, p, sec.chat[to], &sec.tr.c, r.sess.Probes[idx], extra)
	if err != nil {
		return err
	}
	sec.tr.results[idx] = res
	return nil
}

// writeBrief has the outgoing owner write a brief on a clone with its own model. It
// has Fork B's target and limit and cannot call tools. Only budget and cancellation
// are errors. A failed brief is counted and delivers nothing. A brief cut off by the
// limit is counted and delivered, unlike a fork: discarding it would leave the
// receiver with nothing and handicap the baseline.
func (r *sessionRun) writeBrief(ctx context.Context, from *participant, side corpus.Side, chat model.ChatModel, c *Counters, arm string) (string, bool, error) {
	clone, err := from.clone(ctx, chat, c, buildOpts{react: agent.ReactConfig{MaxIters: 1}, noTools: true})
	if err != nil {
		return "", false, err
	}
	r.rec.begin(-1, 0)
	briefCtx := lab.WithEffort(lab.WithLimit(lab.WithRole(ctx, lab.RoleBrief), briefLimit(side)), forkEffort)
	out, err := clone.reply(briefCtx, HandoffPrompt)
	r.rec.end()
	if err != nil {
		if isBudget(err) || ctx.Err() != nil {
			return "", false, err
		}
		c.BriefFailed++
		c.LossyEvents++
		return "", false, nil
	}
	if strings.TrimSpace(out.Final) == "" {
		c.BriefFailed++
		c.LossyEvents++
		return "", false, nil
	}
	if s := lastStatus(r.env.Ledger, arm, r.tr.session, lab.RoleBrief); s == "incomplete" || s == "length" {
		c.BriefIncomplete++
		c.LossyEvents++
	}
	// A local brief goes straight to the cut. The local window cannot hold its context, the
	// draft and the feedback, and from the draft alone the 4B model barely shrank it
	// (`runs/smoke5`, three rewrites at 3,100 to 3,300 tokens), while its rewrites have
	// nothing to check them against. A rewrite gone wrong would hurt A4 alone.
	text := out.Final
	if side == corpus.SideCloud {
		truncated := func() bool {
			s := lastStatus(r.env.Ledger, arm, r.tr.session, lab.RoleBrief)
			return s == "incomplete" || s == "length"
		}
		if text, err = bounceOnClone(briefCtx, clone, out.Final, truncated, c); err != nil {
			return "", false, err
		}
	}
	text, trimmed := lab.CutLines(text, forkBTarget)
	if trimmed {
		c.LossyEvents++
		c.SummaryTrims++
	}
	return text, true, nil
}

// summaryRounds is how many times a summary past its limit can be sent back. On seven real
// oversize Fork Bs, three passed at the first bounce and six within four, and sending a
// summary back keeps more facts than cutting it. What is still too long after this many
// rounds is cut at a line boundary, which counts as lossy.
const summaryRounds = 4

// summaryCut is the longest summary that fits Fork B's target, in characters. summaryAim
// is what a bounce asks for, under the cut because a model cannot count exactly.
// rewriteFloor is the least a rewritten summary may hold: a model told to shrink can
// overshoot by half and drop facts that would have fit.
func summaryCut() int   { return 3*forkBTarget - 1 }
func summaryAim() int   { return forkBTarget * 272 / 100 }
func rewriteFloor() int { return forkBTarget * 22 / 10 }

// checkSummary is the host's validator. The feedback gives what a model cannot measure for
// itself: the length, the line where the limit falls and the share to aim for. A first
// draft can be short, because a short session has little to say. A rewrite cannot.
func checkSummary(text string, first bool) (bool, string) {
	n := utf8.RuneCountInString(text)
	if n > summaryCut() {
		lines := strings.Split(text, "\n")
		total, at := 0, len(lines)-1
		for i, line := range lines {
			total += utf8.RuneCountInString(line) + 1
			if total > summaryCut() {
				at = i
				break
			}
		}
		line := []rune(lines[at])
		return false, fmt.Sprintf("Rejected: %s characters, and the limit is %s. Line %d of %d is where the limit falls, and everything from it on would be lost: %q. Write the whole summary again at about %d%% of this length. Do not add facts. Shorten long lines to one fact each first, then drop the oldest and least useful facts.",
			commas(n), commas(summaryCut()+1), at+1, len(lines), string(line[:min(len(line), 80)]), 100*summaryAim()/n)
	}
	if !first && n < rewriteFloor() {
		return false, fmt.Sprintf("Rejected: %s characters, which leaves %s unused, and facts you dropped would fit. Write the whole summary again with as many of them restored as fit, staying under %s characters.",
			commas(n), commas(summaryCut()-n), commas(summaryCut()+1))
	}
	return true, ""
}

// shortenSystem and shortenOpening make the request of a rewrite that works from the draft
// alone: C0, C0h and C7, which have no fork to return to. It always starts from the
// original draft, so a rewrite that dropped too much can get it back.
const (
	shortenSystem  = "You shorten handoff summaries. You never add a fact."
	shortenOpening = "The handoff summary below came to "
)

func shortenPrompt(draft string, percent, last int, refused bool) string {
	var sb strings.Builder
	n := utf8.RuneCountInString(draft)
	fmt.Fprintf(&sb, shortenOpening+"%s characters, and the limit is %s. Write it again at about %d%% of its length, which is %s characters. Shorten long lines to one fact each first, then drop the oldest and least useful facts. Keep each code and value character for character, add nothing, and do not describe what you removed. Return only the result.",
		commas(n), commas(summaryCut()+1), percent, commas(n*percent/100))
	if last > 0 {
		fmt.Fprintf(&sb, " Your last attempt came to %s characters, so aim at the length given here.", commas(last))
	}
	if refused {
		sb.WriteString(" Your last attempt held a code that is not in this text, so it was refused. Copy codes only from this text.")
	}
	sb.WriteString("\n\n")
	sb.WriteString(draft)
	return sb.String()
}

// rewriter sends one shortening request to the model that wrote the text.
type rewriter func(ctx context.Context, prompt string) (string, error)

func cloudRewriter(cloud *lab.Cloud) rewriter {
	return func(ctx context.Context, prompt string) (string, error) {
		res, err := cloud.Cold(lab.WithEffort(ctx, forkEffort), lab.RoleShorten, shortenSystem, prompt, forkBLimit)
		if err != nil {
			return "", err
		}
		if !res.Completed {
			return "", errors.New("the rewrite did not complete")
		}
		return res.Text, nil
	}
}

// codeShape is what a fact's code looks like. A rewrite from the draft alone has no source
// for a code the draft lacks, so one that appears is made up.
var codeShape = regexp.MustCompile(`\b[0-9A-Z]{2,}-[0-9]{3,}\b`)

func addsCodes(draft, out string) bool {
	for _, code := range codeShape.FindAllString(out, -1) {
		if !strings.Contains(draft, code) {
			return true
		}
	}
	return false
}

// fitSummary is the bounce loop for a writer with no context to return to. It returns the
// last attempt it accepted, which the caller cuts if it is still too long. A failed rewrite
// ends the loop, and one holding a made-up code uses up its round and is asked for again, as
// a fork's rejected submission would be. Only budget and cancellation are errors.
func fitSummary(ctx context.Context, rewrite rewriter, draft string, c *Counters) (string, error) {
	attempt, last := draft, 0
	n := utf8.RuneCountInString(draft)
	percent := rewriteTarget(100*summaryAim()/max(n, 1), n)
	refused := false
	for round := range summaryRounds {
		ok, _ := checkSummary(attempt, round == 0)
		if ok {
			return attempt, nil
		}
		c.SummaryRewrites++
		out, err := rewrite(ctx, shortenPrompt(draft, percent, last, refused))
		if err != nil {
			if isBudget(err) || ctx.Err() != nil {
				return "", err
			}
			return attempt, nil
		}
		out = strings.TrimSpace(out)
		if out == "" {
			return attempt, nil
		}
		if refused = addsCodes(draft, out); refused {
			continue
		}
		attempt = out
		last = utf8.RuneCountInString(attempt)
		percent = rewriteTarget(percent*summaryAim()/max(last, 1), n)
	}
	return attempt, nil
}

// rewriteTarget keeps a stated share of a draft of n characters inside what the check
// accepts. A writer that came up short can be asked for more, but a target past the limit
// would be refused if it were obeyed.
func rewriteTarget(percent, n int) int {
	if n <= 0 {
		return 100
	}
	lo, hi := (100*rewriteFloor()+n-1)/n, 100*summaryCut()/n
	return max(min(percent, hi, 100), lo, 1)
}

// bounceOnClone is the bounce loop for a cloud brief, on the clone that wrote it, which has
// the history. It is Fork B's loop as far as a clone with no tools allows: the same check
// and the same feedback, as a user message and not a tool result. As on a fork, a rewrite that
// did not complete leaves the earlier text standing.
func bounceOnClone(ctx context.Context, clone *participant, draft string, truncated func() bool, c *Counters) (string, error) {
	text := draft
	for round := range summaryRounds {
		ok, feedback := checkSummary(text, round == 0)
		if ok {
			return text, nil
		}
		c.SummaryRewrites++
		out, err := clone.reply(ctx, feedback)
		if err != nil {
			if isBudget(err) || ctx.Err() != nil {
				return "", err
			}
			return text, nil
		}
		next := strings.TrimSpace(out.Final)
		if next == "" || truncated() {
			return text, nil
		}
		text = next
	}
	return text, nil
}

// lastStatus returns the status of the newest ledger row for a role. The adapters do
// not report truncation, so the ledger is the only place that knows.
func lastStatus(l *lab.Ledger, arm, session, role string) string {
	status := ""
	l.Scan(func(r *lab.Row) bool {
		if r.Arm == arm && r.Session == session && r.Role == role {
			status = r.Status
			return false
		}
		return true
	})
	return status
}

// briefHeader opens every brief, in Context or in a system prompt. briefPrefix is the
// part of it that identifies one.
const (
	briefHeader = briefPrefix + "%s assistant]"
	briefPrefix = "[Handoff brief from the "
)

func briefMsg(from corpus.Side, text string) *message.Msg {
	return message.UserMsg("handoff", fmt.Sprintf(briefHeader+"\n%s", from, text))
}

// forkB runs one Fork B. A fork that did not complete is discarded and counted, and
// it returns a nil summary; only budget and cancellation are errors. The caller keeps
// its old summary. The fork never touches the snapshot.
func forkB(ctx context.Context, cloud *lab.Cloud, snap *lab.Snapshot, log *threadLog, prev *summary, c *Counters) (*summary, error) {
	prompt := HandoffPrompt
	if prev != nil {
		prompt += fmt.Sprintf("\n\nYour previous summary, through log entry #%d, follows. Carry forward what is still true and replace what was corrected.\n%s", prev.Coverage, prev.Text)
	}
	res, err := cloud.ForkSubmit(lab.WithEffort(ctx, forkEffort), snap, lab.ForkRequest{Role: lab.RoleForkB, Prompt: prompt, Limit: forkBLimit}, checkSummary, summaryRounds)
	// A fork that failed still went through its rounds, so these count either way.
	c.SummaryRewrites += res.Bounces
	c.ForkStrays += res.Strays
	if err != nil {
		if isBudget(err) || ctx.Err() != nil {
			return nil, err
		}
		c.ForkFailed++
		c.LossyEvents++
		return nil, nil
	}
	if !res.Completed || strings.TrimSpace(res.Text) == "" {
		c.ForkFailed++
		c.LossyEvents++
		return nil, nil
	}
	c.ForkB++
	return applySummary(log, res.Text, c), nil
}

// applySummary cuts a finished summary to Fork B's target and writes its log entry.
// Its coverage is everything logged so far, minus the edge_only seqs.
func applySummary(log *threadLog, text string, c *Counters) *summary {
	return applySummaryCut(log, text, lab.CutLines, c)
}

// applySummaryCut is applySummary with the cut named. A summary whose prompt ends in what
// matters most cannot lose its end, so C0's template is cut in the middle.
func applySummaryCut(log *threadLog, text string, cut func(string, int) (string, bool), c *Counters) *summary {
	trimmed, did := cut(text, forkBTarget)
	if did {
		c.LossyEvents++
		c.SummaryTrims++
	}
	coverage := log.last()
	log.add(kindSummary, string(corpus.SideCloud), corpus.Shared, trimmed)
	return &summary{Text: trimmed, Coverage: coverage, Excluded: log.edgeSeqs(coverage)}
}

// execSession runs one primary arm on one session and returns its rows, secondaries
// first so that the primary's row marks the pair as finished. The only error is the
// budget stop, whose session must not leave a row.
func (e *Env) execSession(ctx context.Context, arm string, s corpus.Session) ([]Row, error) {
	run := &sessionRun{
		env: e, arm: arm, sess: s,
		system: marker(e.Run, arm, s.ID) + "\n" + s.System,
		tr:     newTracker(arm, s.ID, s.Set, s.Length, s.Probes),
		rec:    newToolRec(),
		start:  time.Now(),

		replies: map[int]string{},
	}
	impl, err := newArm(arm, run)
	if err == nil {
		err = run.loop(ctx, impl)
	}
	if isBudget(err) || e.overBudget() {
		return nil, lab.ErrBudget
	}
	var secs []*secondary
	if impl != nil {
		secs = impl.secondaries()
	}
	if err != nil {
		run.tr.err = err
		run.tr.c.SessionErrors++
		for _, sec := range secs {
			sec.tr.err = err
			sec.tr.c.SessionErrors++
		}
	}
	run.tr.elapsed = time.Since(run.start) - run.spent
	// A cloud batch on a read clone is the primary's, whether or not the session reached its
	// end. Filling these only at the end would score a clone 0 where its primary scored.
	for _, sec := range secs {
		for _, i := range sec.shared {
			sec.tr.results[i] = run.tr.results[i]
		}
	}

	exposure := score.ExposureOf(s, run.replies, run.rec.results)
	seen := 0
	for _, x := range exposure {
		if x.Seen {
			seen++
		}
	}
	rows := make([]Row, 0, len(secs)+1)
	for _, sec := range secs {
		rows = append(rows, sec.tr.row(e, seen, len(exposure)))
	}
	return append(rows, run.tr.row(e, seen, len(exposure))), nil
}

// newArm builds an arm's agents. A construction error becomes a session error.
func newArm(arm string, run *sessionRun) (armImpl, error) {
	var (
		impl armImpl
		err  error
	)
	if run.ws == nil && (arm == ArmA4Abr || arm == ArmA4Read || arm == ArmT1Read) {
		return nil, errors.New(arm + " is a clone at a probe on a session set, and a whole arm only on S2")
	}
	switch arm {
	case ArmA0, ArmA2, ArmA8, ArmA38K, ArmA38KH, ArmA332K, ArmA6:
		var a *hostArm
		if a, err = newHostArm(arm, run); err == nil {
			impl = a
		}
	case ArmA4, ArmA4S, ArmA4Abr, ArmA4Read:
		var a *a4Arm
		if a, err = newA4Arm(run, arm); err == nil {
			impl = a
		}
	case ArmT1, ArmT1Read:
		var a *t1Arm
		if a, err = newT1Arm(run, arm); err == nil {
			impl = a
		}
	default:
		err = errors.New("unknown session arm " + arm)
	}
	return impl, err
}
