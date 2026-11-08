# Stage 0 harness contract

Experiment code under `experiments/stage0/`, published with the Main Thread RFC.
It is not part of the library.
The design it implements is §6, §15 and §18 of the plan, which is not
published (see the note at the top of `prereg.md`). This file records the
concrete choices, so that an evaluator can check them against the code and the
pre-registration. Nothing here changes the Agent API.

## Layout

| Path | Owner | Contents |
|---|---|---|
| `corpus/`, `score/` | Blind author | S1, CBench and the deterministic scorer. Frozen by hash before the pilot |
| `lab/` | Harness | Responses and vLLM transports, ledger, slots, forks at the HTTP boundary |
| `arms/` | Harness | Session loop, A0, A3, A6, A4, T1, the checkpoint clones, the C group, fakes for L0 |
| `report/` | Independent of `arms/` | Paired bootstrap, gate decisions, tables from JSONL |
| `cmd/stage0/` | Harness | CLI that runs arms and writes `runs/<run>/` |
| `s2/` | Blind author | S2 fixture repositories and scoring |

`runs/` is git-ignored; run ledgers, scores and reports are not published.

## Fixed settings

- Cloud: OpenAI `gpt-6-luna` on the Responses API. `store:false`, encrypted
  reasoning replayed, default 30 minute cache tier, buffered model calls
  (`WithModelStreaming(false)`). Declared window: 1,050,000. Main calls and probes send
  no effort field, so they use the API's default for the model (medium). Forks, briefs,
  C0h and C7 use low effort through a `configuration_update` item placed after the
  prefix, which keeps the cache hit. C0 is the existing `Compact` and is left as it ships.
- Local: `Qwen/Qwen3.5-4B` on vLLM in a Linux VM, thinking off, `temperature: 0`.
  Window enforced by the server's `--max-model-len`: 8,192 for the 8K arms,
  32,768 for A3-32K. The client's `ContextConfig.ContextSize` matches.
- Each session and arm starts its system prompt with a unique marker, so no
  session can read another session's cache.
- Budgets: Fork B target 2,500 estimated tokens, limit 7,000 because
  `max_output_tokens` includes reasoning, refresh every 5K of cloud growth. A brief has
  Fork B's target and prompt, and the same limit on the cloud; a local brief has 5,000,
  because thinking is off there. `ReserveRatio` is 0.3 for every agent with a local
  window. `prereg.md` explains why these differ from the plan's proposal, and
  `--fork-refresh`, `--summary-target` and `--never-clear` select other values for the
  sensitivity runs.
- Local summary total: 3,000 tokens in the agent's unit, bytes/4. A summary held outside
  Context and the agent's own summary share it, so a full 7,497 byte `PeerSummary` leaves
  1,126. A brief in A4's Context is not charged to it, which favors A4.
- The prompt offers 6,000 characters for a 2,500 token target, 80% of where the cut fires
  (above 7,499 characters). The host checks each summary and sends back what fails, up to 4
  times (`summary_rewrites`), with its length, the line where the limit falls and the share
  to aim for. A rewrite under 5,500 characters also goes back. A fork answers through
  `submit_summary`, appended after the prefix as a client tool search result, so the fork
  stays in context and in the cache. A cloud brief bounces on its clone. C0, C0h and C7
  bounce from the draft alone (role `shorten`), and a rewrite holding a code the draft lacks
  is refused and uses up its round. A local brief is not rewritten. What is still too long is
  cut from the end at a line boundary, except that C0, whose template ends in the next steps,
  is cut in the middle. Every cut counts as a lossy event and in `summary_trims`. A fork round
  that submits nothing is answered and counted in `fork_strays`.
- An owner holding a summary outside Context reserves that summary's tokens on top of the
  0.3 reserve, capped at 0.75, so a full summary takes window and leaves the raw tail alone.
  At a takeover its own summary is held to what the new summary leaves.
- Each local owner's compression request states its budget in a `[Summary budget]` line of a
  system prompt only that request sees.
- Token counts for local windows come from vLLM's `/tokenize`, because the agent's
  default estimate of bytes/4 understates this corpus.

## Session loop (all live arms)

For each scripted turn:

1. If the owner changes: run the arm's switch handling, then run the probe batch
   whose `AfterTurn` is the previous turn on a clone of the new owner. No probe
   ever touches the original state.
2. `Reply(turn.User)` on the owner with role `main`.
3. Run the arm's after-reply step, which is where T1 publishes and forks.

At the end, run the final batch on a clone of the final owner.

Probes use the recipient's tools with at most 3 tool calls. Failures count as
wrong and stay in the denominator. A session error stops that session; the
batches it did not reach score zero. Exposure (whether the fact was actually
seen) is reported separately, not used to filter.

A batch answer that does not parse gets one repair request on the same clone, within
the same tool bound. On the local side the server is held to the answer's JSON schema
with tools off. What still fails is a parse failure. `probe_repairs` counts them.

## Arms

- **A0.** One cloud agent with both record tools. Every turn and probe goes to the cloud.
- **A2.** The real `ConnectivityAwareModel` over both sides, window = local window, because a
  fallback can land on local. It ignores who owns a turn and stays on the cloud until the cloud
  fails, so local takes only the calls that follow a cloud failure. S2 only.
- **A8.** What exists today put together. Routes come from outside, and with `--routes` from a
  classifier that saw the query alone. One history at the cloud's window, so it never compresses here.
  A local call goes out only if its input fits 8K with room for a reply, the managed check before
  dispatch, and otherwise goes to the cloud, which `hard_limit_cloud` counts. The managed path itself
  has no tools or turns and cannot run S2. S2 only.
- **A3-8K, A3-32K.** One agent, one history, both record tools (a single host has
  a single toolkit), window = local window. A switching `ChatModel` picks the
  scripted owner's adapter. Compression always goes to the cloud so that the
  local model's summary quality cannot handicap the baseline. The local owner can
  re-read a cloud record through the tool it shares, so A0 - A3 is conservative.
- **A3-8K-h (recorded).** A3-8K with the handoff text as its compression prompt and the
  sentence of the record rules true of a host. It shows how much of G1 is the prompt.
- **A6.** As A3, with the window declared as the cloud's. A switch to local relies
  on the agent's overflow recovery. Recorded only. It has both tools and a cloud-sized
  window on one history, so it is not comparable with T1 or A4, and a row near A0 does
  not refute G1.
- **A4.** Two agents, no log. At each switch the outgoing owner writes a brief on a
  clone with its own model. The brief's target and limit equal Fork B's. The
  receiver keeps its own context and gets the brief through `Observe`. A brief in Context
  can be rewritten by compression, which `briefs_compressed` counts. A brief cut off by
  its limit is delivered and counted as lossy, unlike a fork, which is discarded: a
  receiver with nothing would handicap the baseline.
- **A4s.** A4 with the newest local brief in the system prompt, where T1 keeps its
  summary: one at a time and out of reach of compression. Context stays, because A4 has
  no log to rebuild it from. A brief to the cloud goes through `Observe` as in A4.
- **T1.** Two agents, an in-memory log and the rules below.
- **T1+brief (secondary).** A clone of T1's receiver that also gets a brief.
- **A4-abrupt (secondary).** A clone of A4's receiver that gets no brief at this switch.
  It keeps the earlier briefs, because only the latest switch is abrupt. T1's planned
  switch makes no model call, so T1's abrupt score equals its planned score.
- **T1+read and A4+read (secondary).** A clone of the receiver whose local can also call
  `event_read`, which reads what the cloud's tools returned. Nothing is pushed, and reads are
  bounded. A cloud batch is the primary's, because the cloud holds its own results.

On S2 there are no probes, so A4-abrupt, A4+read and T1+read run as whole arms with their own cloud
calls. A4-abrupt carries nothing at any switch there, which is harder than its S1 clone.

**What the local model is told.** On S1 every local agent in A4, A4s, T1 and the C group
gets the record rules: call the tool before saying a record was read, the tool reads only
`L-` records, read again if the contents are not visible. T1 and the C group also get the
log rules, because only they have log entries. On S2 the tools are the workspace, so T1 gets
`taskRules`, the log rules for a shared workspace, and A4's local gets `taskPeerRules`, the same
about the workspace with no log.

Each agent reaches the workspace through its own `s2.View`. `write_file` replaces a whole file, so
a View refuses a write over a version that agent has not seen, and the agent can read the file and
try again. A single host has one View, and each side of a two-agent arm has its own. The `write_file`
description says to read an existing file first. `stale_writes` counts the refusals. The cloud gets
none of them. This is coaching: it changes what the small model does and the answers stay
the model's.

## T1 rules

Log entries carry seq, kind (`user`, `reply`, `tool_summary`, `summary`,
`owner_change`), source and visibility. A turn's user message and reply take the
turn's disclosure. Tool summaries hold the tool name and arguments clipped to 200
characters, never the result, and take the same visibility.

**Fork B.** At the end of a cloud reply, when the cloud's input tokens have grown
by at least 5K since the last Fork B. No fork is forced at a switch. The request
appends the previous `PeerSummary` if there is one. A fork that did not complete
is discarded and the old summary stays. The text is trimmed deterministically to
2,500 estimated tokens, and the entry records its coverage: everything up to the
snapshot, excluding `edge_only` seqs.

**Cloud to local.** No model call.

1. If the newest summary covers past local's last activity seq, start local from
   an empty state with that summary as `PeerSummary` (the clear branch).
2. Otherwise keep local's context, drop imported messages whose `thread_seq` the
   summary now covers, and adopt the summary if there is one.
3. Import shared entries past the coverage with `Observe`, as plain text carrying
   `thread_seq` in `Msg.Metadata`.
4. Rebuild the local agent with the rules, `PeerSummary` and the `edge_only` index in
   the system prompt, and `SummaryMaxTokens = 3,000 − PeerSummary bytes/4`. A compression
   request carries the same system prompt without `PeerSummary`, standing in for the
   plan's separate slot, which compression never sees.

**Local to cloud.** No model call. Import shared entries past the cloud's cursor
with `Observe`. `edge_only` entries never leave.

**`thread_read`.** A normal tool. Outline mode returns seq, kind, source and first
line; entry mode returns one entry's full text. Local sees everything. Cloud sees
shared entries only.

**Privacy check.** Every outbound cloud body is scanned for the codes in `edge_only`
turns: their user text, expected reply and any record they read. T1 must have 0
hits. `Session.Canaries()` is wider and is not used for this, because it includes
local records read on `shared` turns, whose replies the rules allow to be published.

## C group

Every method starts from the same scripted history on the same cloud prefix. One
live main call makes the fork point, and local answers the probes. The fork point
sits at about 80% of the history, and the rest is the delta.

| Arm | Summary | Delta | Local context |
|---|---|---|---|
| C0 | Existing `Compact` shape on the cloud, trimmed to 2,500 | Published text | Cleared |
| C0h | C0 with the handoff text and low effort, so only the path differs from C3. Recorded | Published text | Cleared |
| C3 | Fork B | Published text | Cleared |
| C4 | One merged request yielding both summaries, local part used | Published text | Cleared |
| C6 | Fork B | Published text plus tool results, newest first, cut to the delta budget | Cleared |
| C7 | Cold request with the same model and the rendered trace, no reuse of the main prefix | Published text | Cleared |
| C8 | Fork B | Published text | Kept (`LocalOwn` stays) |
| C3+A | C3, then apply Fork A on the same snapshot, continue, and Fork B again | Published text | Cleared |

`gpt-6-luna` is already the smallest listed model, so C7 tests only the reuse of
the prefix and not a cheaper model. The report states this limit. C3 - C0 compares the
prompt, the budget and the path together. C3 - C0h compares the path alone.

## Output of a run

`runs/<run>/manifest.json` states the set, the commit and the effective budgets. A run
that resumes must have the budgets it started with. The report refuses to pool runs whose
budgets differ, S2 rows with any other set, and tuning rows with acceptance rows. A run
with no manifest is named in the report's limits.
`runs/<run>/ledger.jsonl` holds one `lab.Row` per model call.
`runs/<run>/scores.jsonl` holds one object per arm and session:

```json
{"arm":"T1","session":"s1-20-0001","set":"tuning","length":20,
 "retention":0.82,"by_category":{"F1":[8,10]},"f6_correct":4,"f6_total":5,
 "stale":1,"parse_failures":0,"batches":4,"questions":38,
 "exposure_seen":30,"exposure_total":32,
 "own_retention":0.7,
 "counters":{"clears":2,"keeps":1,"fork_b":2,"fork_failed":0,"compress":3,"overflow_recoveries":0,"lossy_events":0,"session_errors":0},
 "canary_hits":0,"seconds":412.5,"cost_usd":0.21,"error":""}
```

`by_recipient` splits `by_category` by the side that answered. On S2, `retention` is the
hidden-test score and `s2` holds passed, total, `build_failed` and any scorer failure. A
scorer failure writes a null `retention`, because that row says nothing about the arm. The
scorer has its own deadline, so a session that ran out of time is still scored. A budget
stop leaves no row for the session it interrupted. A repeat of an S2 task has the session id
`<task>~r<k>`, and the first run keeps the plain id, so a later run can add repeats.
`own_retention` is the same pooled ratio over F5 only. `lossy_events` counts
compressions that fell back to truncation and summaries that were elided or
discarded. The report groups rows by arm, pairs them by session and reads spend and
cache rates from the ledger.

## Levels

- **L0, offline.** All arms on loopback fakes. An oracle model scores 100%, a null
  model scores 0 outside F6, and a fork's prefix matches the snapshot byte for byte.
- **L1, calibration.** vLLM throughput at 1, 2, 4, 8, 16, 32 slots with realistic
  prompts; overflow behaviour at 8K; tool-call success; a capability check that
  asks Qwen3.5-4B about facts in a short context. These fix the slot counts.
- **L2, pilot.** `corpus.Pilot()` and `corpus.CBenchPilot()`, then G1, G6 and privacy. T1's rows on
  S1 are a diagnostic. `prereg.md` is committed before L2 starts.
- **L3, pilot of G2.** S2: A0, A3-8K, A4, A4s, A4-abrupt, A4+read, T1 and T1+read on `--set s2
  --repeat R`. One shared workspace, no probes, no clones, `MaxIters` 16 and a local reply limit of
  3,000 on every arm. Its report is separate and holds G2's verdict. n counts tasks and the CI
  resamples tasks, so repeats only reduce the noise inside a task.
- **L4, acceptance.** Frozen configuration. S2's held-out tasks for G2, their number from L3's SD.
  `corpus.CBenchAcceptance(n)` for G6, `n` from L2's SD. `corpus.Acceptance(n)` with A0 and A3-8K
  for G1.

## Runs in one report

A report refuses runs with different budgets or different commits, except that a run with no
manifest may hold A0 and A3-8K rows. A run with uncommitted changes under `experiments/stage0` or an
unknown commit is reported as not a primary result.

## Routing on S3

`stage0 run --arms S3 --set s3pilot|s3acceptance --classifiers decisions[,piccolo,jev]` runs each item
once, in shadow, and writes one row with both sides' answers, each world's local answer and every
router's decision. Classifier calls go to the ledger as arm `cls:<name>`, role `classifier`. Nothing
a corpus keeps host-only reaches a model or a classifier: items are named to them by an opaque hash,
and the state holds the query and session data only. The report has one G7 per classifier, the
threshold grid and a breakdown by kind, and refuses to pool S3 rows with any other set. `prereg.md`
has the design.

## Reading a CI

The bootstrap is percentile, 10,000 resamples, seed 1: at session level, on S2 at task level, and on
S3 at seed level. A
percentile CI undercovers at small n. The floor of 10 pairs is a floor and not a claim of power.

## Concurrency

Session parallelism is the one knob. The local side is the bottleneck, so the slot
count is set from L1 where total throughput stops rising, and the number of
parallel sessions is raised until local slots stay busy without the queue growing.
`wait_ms` in the ledger shows whether either side is starving the other. The cloud
side retries on 429 outside a slot, so a backoff never holds capacity.
