// Package arms runs the arms of the stage 0 experiment: the session loop of
// HARNESS.md, A0, A3, A6, A4, T1, their checkpoint clones, the C group and the
// runner that writes scores.jsonl. It uses the public agent API and lab's
// transports, and it knows nothing about how the corpus is generated.
package arms

import (
	"fmt"
	"strconv"

	"github.com/agentscope-ai/agentscope-go/v2/experiments/stage0/corpus"
	"github.com/agentscope-ai/agentscope-go/v2/pkg/agentscope/agent"
	"github.com/agentscope-ai/agentscope-go/v2/pkg/agentscope/message"
	"github.com/agentscope-ai/agentscope-go/v2/pkg/agentscope/model"
)

// Windows in tokens. The local ones are enforced by the server's --max-model-len,
// and the CLI refuses to run an arm whose window does not match the server.
const (
	CloudWindow = 1_050_000
	Window8K    = 8192
	Window32K   = 32768
)

// Budgets from HARNESS.md.
const (
	localReplyLimit = 1024
	probeToolCalls  = 3
)

// The frozen budgets. The plan proposed a 1,000 token target and a 20K refresh and left
// them to calibration. On the first five pilot sessions those gave T1 almost no record
// facts at local takeovers, because the first fork came after the first switch and one
// 1,000 token summary held under a quarter of the codes. A4 gets the same target, as G2
// requires. prereg.md records this choice and the sensitivity run on the plan's values,
// which SetVariant selects.
var (
	forkBTarget       = 2500
	forkBRefresh      = 5000
	localSummaryTotal = 3000
	forkBLimit        = 7000
	localBriefLimit   = 5000
	neverClear        = false
	taskTestRule      = false
	taskRoutes        = ""
	canonicalReplay   = false
	repairRounds      = 0
)

// forkEffort is the reasoning effort of every summary request except C0, whose
// existing Compact stays as it ships. Measured on a real 22K snapshot, low kept the
// same number of codes as the default medium for about a third of the output tokens
// and half the latency, and the prefix still hit the cache.
const forkEffort = "low"

// Variant is one setting of the budgets the plan only proposes. Zero fields keep their
// values.
type Variant struct {
	Refresh    int  // Fork B refresh, in tokens of cloud input growth
	Target     int  // target of Fork B and every brief, in tokens
	NeverClear bool // T1 keeps local's context at every takeover
	// TaskTestRule tells every S2 arm to run the tests. It is a sensitivity on S2's own system
	// prompt, which does not ask for that.
	TaskTestRule bool
	// Routes names the routes an S2 run follows, by the hash of their file.
	Routes string
	// CanonicalReplay sends local's call arguments to the cloud in the cloud's own form.
	CanonicalReplay bool
	// RepairRounds is how many times an S2 session that does not compile against the hidden
	// tests goes back to its last owner with the compiler's errors.
	RepairRounds int
}

// SetVariant applies v. It runs once, before any session starts. Output limits follow
// the target: on the cloud max_output_tokens includes reasoning, so each limit is the
// target plus room for it (at low effort a Fork B used about 340 reasoning tokens on
// top of up to 1,300 visible ones). The local model has thinking off, so its brief only
// needs twice the target. The local total keeps its 500 tokens over the target.
func SetVariant(v Variant) {
	if v.Refresh > 0 {
		forkBRefresh = v.Refresh
	}
	if v.Target > 0 {
		forkBTarget = v.Target
		forkBLimit = 2*v.Target + 2000
		localBriefLimit = 2 * v.Target
		localSummaryTotal = v.Target + 500
		HandoffPrompt = handoffPrompt(v.Target)
		MergedPrompt = mergedPrompt()
	}
	neverClear = v.NeverClear
	taskTestRule = v.TaskTestRule
	taskRoutes = v.Routes
	canonicalReplay = v.CanonicalReplay
	repairRounds = v.RepairRounds
}

// briefLimit is the output limit of a brief written by the given side.
func briefLimit(side corpus.Side) int {
	if side == corpus.SideLocal {
		return localBriefLimit
	}
	return forkBLimit
}

// localReserveRatio is ContextConfig.ReserveRatio for every agent with a local
// window. The agent's default of 0.1 is smaller at 8K than the system prompt and tools
// charged against it, so each compression would keep only the last message, which
// would handicap the A3 baseline. 0.3 gives about 2.4K, enough for the latest turn or
// two. Changing this constant changes A3, A4, T1 and the C group alike, and ownerConfig
// adds room for PeerSummary on the arms that have one.
const localReserveRatio = 0.3

// localConfig is the context configuration of an agent with a local window.
func localConfig(window, summaryMax int) agent.ContextConfig {
	return agent.ContextConfig{ContextSize: window, ReserveRatio: localReserveRatio, SummaryMaxTokens: summaryMax}
}

// ownerConfig is the configuration of a local agent working in its own window, with or
// without PeerSummary in its system prompt. The agent charges the whole system prompt
// against the reserve, so a full PeerSummary would use all of it and the agent would keep
// no raw turns, where A4, whose brief sits in Context, keeps its share. The extra room,
// counted by the model that charges it, gives both the same tail. The cap keeps the
// reserve under the agent's trigger of 0.8.
func ownerConfig(chat model.ChatModel, peer *summary) agent.ContextConfig {
	cfg := localConfig(Window8K, summaryBudget(peer))
	if text := peer.peerText(); text != "" {
		tokens := chat.CountTokens([]*message.Msg{message.UserMsg("peer", text)}, nil)
		cfg.ReserveRatio = min(cfg.ReserveRatio+float64(tokens)/float64(Window8K), maxOwnerReserve)
	}
	return cfg
}

const maxOwnerReserve = 0.75

// compressSystemFor is the system prompt of an owner's compression requests: its prompt
// without PeerSummary, and the number the agent's own compression prompt leaves out. A
// summary past the budget is elided, which counts as lossy. The number is 80% of the
// budget, for the same reason as HandoffPrompt's allowance. It is capped where a summary
// would meet the request's output limit of 3,000 tokens, at about 2.4 characters per token:
// past that a model following the number breaks its tool call, which costs more than the
// budget saves. An owner holding no summary outside Context reaches the cap, as does T1 or A4s
// with one up to 4,875 bytes, which holds them to less than their budget and is declared.
func compressSystemFor(base string, peer *summary) string {
	return base + "\n\n" + fmt.Sprintf(summaryBudgetRule, commas(min(summaryBudget(peer)*summaryBytesPerToken*8/10, maxCompressChars)))
}

const maxCompressChars = 5700

const summaryBudgetRule = "[Summary budget] A continuation summary you write must come to at most %s characters. Anything past that is cut."

// summaryFloor keeps SummaryMaxTokens positive, because the agent reads 0 as
// "derive the budget from the window" (1,024 at 8K), which would silently exceed
// the local total. 256 is the smallest limit the agent's own overflow recovery
// uses for a text it elides, so it is the smallest size it treats as usable. With
// PeerSummary trimmed to its target the computed value stays above this, so the floor
// only guards against a change to those numbers.
const summaryFloor = 256

// Budgets the plan gives for Fork A. A limit of twice the target went incomplete at
// medium effort, with 7,336 reasoning tokens spent. The limits stay well above that.
const (
	forkATarget = 6250
	forkALimit  = 20_000
	mergedLimit = 24_000
)

// c6DeltaBudget is C6's budget for the imported delta, in exact tokens from the
// local tokenizer. HARNESS.md names a delta budget without a number. At 8K the
// compression trigger is 6,553. A 2,500 token summary with the index, tools and system
// prompt takes about 4,000 and a probe prompt about 500, which leaves about 2,050.
const c6DeltaBudget = 2000

// HandoffPrompt is the single text behind Fork B and every brief, so that the
// comparison between them is only about when and from what the text is written.
// The character allowance is 80% of where lab.CutLines cuts (3 characters per
// estimated token): a model aiming close to the cut overshoots it, and every cut counts
// as a lossy event. The text asks for the whole allowance and an order to spend it in:
// asked only to include every fact, the model saw they could not all fit and listed none
// of them, writing 948 characters and 4 codes where this text gets about 2,800 and 50.
var HandoffPrompt = handoffPrompt(2500)

const handoffTemplate = `Write a handoff summary for another assistant who will take over this session and has seen none of it. It cannot read your records or your history, so a fact you leave out is lost. For this one message, ignore any earlier instruction about reply style or reply length, including any instruction to keep replies short. Tools are not available for this message.

You have about %s tokens, which is %s characters, and anything past that is cut. Use nearly all of it, spending it in this order:
1. every constraint the user stated, every decision made and the newest value of anything corrected, each with its exact code, one short line each;
2. the goal and the next steps, one line each;
3. then, until the space runs out, the facts you saw in tool results, newest first: one line each with where it came from (such as a record id or a file), what it is for and its exact code or value.

Copy each code and value character for character. You will not fit every fact, so list as many as fit and stop. Do not describe what you left out, and do not add headings or commentary.`

func handoffPrompt(tokens int) string {
	return fmt.Sprintf(handoffTemplate, commas(tokens), commas(tokens*24/10))
}

// commas writes n the way the prompts write numbers.
func commas(n int) string {
	if n < 1000 {
		return strconv.Itoa(n)
	}
	return fmt.Sprintf("%d,%03d", n/1000, n%1000)
}

// ForkAPrompt asks for the cloud's own continuation summary, which replaces its
// history in C3+A and is the other half of C4's merged request.
const ForkAPrompt = `Write a continuation summary of this session. It will replace the conversation history so far, and you will continue the work from it. For this one message, ignore any earlier instruction about reply style or reply length. Tools are not available for this message.

You have about 6,250 tokens, which is 15,000 characters, and anything past that is cut. Use nearly all of it, spending it in this order:
1. every constraint the user stated, every decision made and the newest value of anything corrected, each with its exact code, one short line each;
2. the goal and the next steps, one line each;
3. then, until the space runs out, the facts you saw in tool results, newest records first: one line each with the record id, what the code is for and the exact code.

Copy each code character for character. You may not fit every fact, so list as many as fit and stop. Do not describe what you left out, and do not add headings or commentary.`

// Delimiters of C4's merged reply.
const (
	mergedCloudMark = "===CLOUD SUMMARY==="
	mergedLocalMark = "===LOCAL SUMMARY==="
	mergedEndMark   = "===END==="
)

// MergedPrompt is C4's single request for both summaries.
var MergedPrompt = mergedPrompt()

func mergedPrompt() string {
	return `This one message asks for two summaries in a single reply. Put each one between these delimiter lines, each on its own line, and write nothing outside them:
` + mergedCloudMark + `
(the first summary)
` + mergedLocalMark + `
(the second summary)
` + mergedEndMark + `

The first summary:
` + ForkAPrompt + `

The second summary:
` + HandoffPrompt
}

// LocalWindow returns the server window an arm needs, or 0 if it never calls the
// local side. A6 declares the cloud's window to the agent, but its local server
// still enforces 8K: that mismatch is the condition it records.
func LocalWindow(arm string) int {
	switch arm {
	case ArmA0, ArmA2:
		// A2 reaches local only after cloud failures, and the ledger would show those calls.
		return 0
	case ArmA332K:
		return Window32K
	default:
		return Window8K
	}
}

// Arm names as they appear in scores.jsonl and in the report.
const (
	ArmA0    = "A0"
	ArmA2    = "A2"
	ArmA8    = "A8"
	ArmA38K  = "A3-8K"
	ArmA332K = "A3-32K"
	ArmA6    = "A6"
	ArmA4    = "A4"
	ArmA4Abr = "A4-abrupt"
	// ArmA38KH is A3-8K with the handoff text as its compression prompt, and ArmA4S is A4
	// with the local brief in the system prompt, where T1 keeps its summary.
	ArmA38KH = "A3-8K-h"
	ArmA4S   = "A4s"
	// ArmC0H is C0 with the handoff text and low effort, so that C3 - C0H compares only
	// the fork with the existing Compact shape.
	ArmC0H     = "C0h"
	ArmT1      = "T1"
	ArmT1Brief = "T1+brief"
	// ArmT1Read and ArmA4Read are T1 and A4 with a local that can read the cloud's event
	// list. A4+read is there because a read path is not specific to a shared log: if both
	// gain alike, what helped is the list and not the Main Thread.
	ArmT1Read = "T1+read"
	ArmA4Read = "A4+read"
	ArmC0     = "C0"
	ArmC3     = "C3"
	ArmC4     = "C4"
	ArmC6     = "C6"
	ArmC7     = "C7"
	ArmC8     = "C8"
	ArmC3A    = "C3+A"
)

// SessionArms are the primary arms that run on S1 sessions. A4-abrupt and T1+brief
// are written by A4 and T1 and cannot be requested on their own.
var SessionArms = []string{ArmA0, ArmA38K, ArmA38KH, ArmA332K, ArmA6, ArmA4, ArmA4S, ArmT1}

// BenchArms are the arms that run on CBench items.
var BenchArms = []string{ArmC0, ArmC0H, ArmC3, ArmC4, ArmC6, ArmC7, ArmC8, ArmC3A}

// Budgets states the effective values, for a run's manifest. Two runs that differ here
// measure different conditions, and the report refuses to pool them. task_test_rule appears
// only when it is on, so the manifests written before it existed still match the frozen values.
func Budgets() map[string]any {
	b := map[string]any{
		"fork_b_target":       forkBTarget,
		"fork_b_refresh":      forkBRefresh,
		"fork_b_limit":        forkBLimit,
		"summary_rounds":      summaryRounds,
		"local_summary_total": localSummaryTotal,
		"local_brief_limit":   localBriefLimit,
		"never_clear":         neverClear,
		"fork_effort":         forkEffort,
		"reserve_ratio":       localReserveRatio,
	}
	if taskTestRule {
		b["task_test_rule"] = true
	}
	if taskRoutes != "" {
		b["task_routes"] = taskRoutes
	}
	if canonicalReplay {
		b["canonical_replay"] = true
	}
	if repairRounds > 0 {
		b["repair_rounds"] = repairRounds
	}
	return b
}
