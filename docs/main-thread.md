# Main Thread: one session shared by a cloud and an edge agent

The experimental `thread` packages let a cloud agent and an edge (on-device)
agent serve one continuous user session. Each keeps its own context, model and
tools. They share an append-only log, the Main Thread, that holds only what
was published: the user's input, the text the user actually saw, which tools
ran (names and targets, never outputs), summaries one side wrote for the
other's window, ownership changes, background tasks, reports, artifacts and
markers. A Coordinator is the single writer of that log.

| Package | Role |
|---|---|
| [`thread`](../pkg/agentscope/thread/) | Log entries and stores, projection into a participant's context, the Coordinator, the ledger, the cloud tool-output list, leases, and the contracts in `runtime.go` |
| [`thread/participant`](../pkg/agentscope/thread/participant/) | `AgentParticipant` (one `UnifiedAgent`), `TeamParticipant` (a cloud master with sub-agents through `pipeline.TeamPipeline`), and the thread tools |
| [`thread/route`](../pkg/agentscope/thread/route/) | The router: three modes, rules and an optional classifier (Jev, Piccolo through the Jev adapter, OpenAI Decisions or the offline fake) |

Every exported identifier in these packages is EXPERIMENTAL and may change
without notice. The runtime is covered by offline tests with scripted models
only. It has not been measured against live models in this form: nothing here
establishes task quality, cloud cost, latency or energy benefits.

## Run the offline example

```bash
go run ./examples/main_thread
go test -count=1 ./pkg/agentscope/thread/... ./examples/main_thread
```

The example runs a short scripted session (a shared query on the cloud with a
sub-agent, a transfer to the edge, an edge-only query that stays on the edge,
a background task with a report) and prints the log, the owner and the ledger.

## Concepts

**Participants.** Each participant has a `thread.Descriptor`: name, zone
(`edge` or `cloud`), context window, the budget of a summary written for it,
the longest brief it accepts, its tool groups and a description. Peers see
each other's descriptors in their system prompt
(`participant.SystemPromptSection`).

**One owner at a time.** Exactly one participant owns the foreground. Only the
owner's foreground activation reaches the user. Every owner change increments
the epoch; reply and owner-change writes carry a guard `{owner, epoch}` that
the store checks, so output from a participant that lost the foreground can
never be written as a reply. It is kept as a report with a
`rejected_output` marker instead.

**Visibility.** Every entry is `shared` or `edge_only`. The cloud zone never
reads `edge_only` entries: its imports show a gap marker such as
`[#101–#102 withheld: edge-only entries]` instead, its `thread_read` leaves
them out, and the hosted classifier never sees them.

**Cursors.** Each participant's agent state holds the last entry it imported
(`ThreadCursor`, schema version 3 checkpoints). The cursor and the imported
content change in one state update, including the entry that triggers a reply
(`agent.WithThreadInput`).

The invariants the Coordinator keeps are listed in
[`thread/doc.go`](../pkg/agentscope/thread/doc.go).

## How a query flows

1. `Coordinator.Submit` appends the user input with the visibility the host's
   disclosure decision gives it (`DiscloseShared`, `DiscloseEdgeOnly` or
   `DiscloseUnclassified`). An unclassified input submitted while the edge is
   tainted is stored and handled as edge-only.
2. If an activation is running, the input is steering for the current owner and
   arrives at its next safe boundary (after a tool step, before the next model
   call). Routing only happens at a query boundary.
3. Otherwise the router picks the owner. A change appends an owner change.
4. The Coordinator imports what the owner has not seen (see "Context and
   summaries"), then starts its reply. Events stream to the user.
5. When the reply ends, the visible text and the tool digest are sealed into a
   reply entry. An interrupted reply gets an `interrupted` marker; a failed one
   after tool calls gets `unknown_effects`, then one failover to another
   participant that may take the query, or `no_owner`. An input that found no
   owner waits parked: it does not count toward `Limits.MaxPendingInputs`,
   at most that many are kept (the oldest beyond that is retired and stays in
   the log as context), and a later cycle imports it as context.

## Routing modes and the classifier layer

`route.New(route.Config{Mode: ...})`:

| Mode | Home | The classifier decides |
|---|---|---|
| `local_based` | edge | go to the cloud only for stronger reasoning or cloud-only tools |
| `cloud_based` | cloud | go to the edge only for simple requests or device-only tools and data |
| `hybrid` | none | best participant per query; low confidence stays with the current owner |

The decision order is fixed: availability and disclosure (an edge-only query
never reaches the cloud; neither does an unclassified one while the edge is
tainted), a participant the user named, tool groups only one side holds, host
rules, the classifier, then the mode's default. A classifier error, an invalid
answer or confidence below the threshold falls through to the default; routing
never blocks the user on the classifier.

The classifier reads a fixed-size state, so it fits a small model's prompt
(Piccolo's 8,192-token row): the query (cut to 1,500 tokens), the summary the
cloud wrote for the edge window, recent published entries as text with their
tool digests (tool calls and outputs never appear) and the participants. A
hosted classifier only sees shared entries and is not called for an edge-only
query. A classifier configured as on-device (`Hosted: false`) is accepted only
when its endpoint is an `http`/`https` URL on `localhost` or a loopback
address, or the offline fake; anything else needs `Hosted: true`.
`route.BuildState` returns the exact state for logs.

The three windows involved are therefore bounded separately: the cloud's own
window (own summary at 0.6), the edge's 8K window (peer summary, index and
budgeted imports) and the classifier's prompt (fixed state budget).

## Handing the foreground over

The `transfer` tool (always called alone; `agent.WithExclusiveTools`) takes a
structured brief: goal, done, remaining, constraints and references. The
Coordinator appends the caller's visible text and the owner change in one
guarded write, ends the caller's reply with `types.ReplyHandedOff` (no further
model call; `OnReply` middleware cannot swallow it) and starts the target with
the brief. It never waits for a summary fork. Unlike a sub-agent call, nothing
returns to the caller: the other side continues with the user.

A transfer is refused when the user's request is edge-only and the target is in
the cloud, and when the brief comes from an edge context holding edge-only
material and the host's `DisclosurePolicy` does not clear it. The model gets
the reason and may rewrite the brief or continue itself.

## Background work

`assign_task` starts a task on another participant in the background; its
events are not shown to the user, except confirmation requests. Its final
answer becomes a `report` addressed to the assigner. A report with `notify`
reaches a running recipient at its next boundary, or wakes the idle owner
(at most `Limits.MaxWakesPerQuery` wakes per query; task depth is limited by
`Limits.MaxAssignDepth`). Nothing addressed to a participant is left unread:
after every activation the Coordinator checks for entries past the
participant's cursor. `participant.Leased` wraps tools that touch shared
resources (for example a file path) so two participants cannot use one at the
same time.

## A cloud master with sub-agents

`participant.TeamParticipant` runs the cloud side as a master with members
through `pipeline.TeamPipeline`. Each activation starts a team run whose leader
factory rebuilds the master from its persisted state (the factory installs the
TeamAssign tool, the thread tools and the participant's agent options).
`TeamConfig.Load` reads the master's persisted state so a participant rebuilt
after a restart reports the right cursor before its first activation; without
it the participant supports only a new session. Members
never see the thread and never get thread tools; their text never becomes a
reply entry, but their tool calls appear in the digest as `member/tool` and
their usage is in the ledger.

## Context and summaries

Summaries are written by the cloud with a forked request: the cloud's last
request plus one appended instruction, with the same system prompt, tools and
options and an output cap, so a provider with prefix caching can reuse the
cached prefix. Whether it does, and what that saves, has not been measured for
this runtime. The fork sends tool choice none by default; some providers
(Anthropic) invalidate cached message blocks when the tool choice changes, so
`ForkRequest.KeepToolChoice` (participant option `KeepForkToolChoice`) keeps
the main request's tool choice and asks the model to rewrite if it calls a
tool. The main thread is never touched by a fork
(`agent.WithForkCapture`, `ForkSnapshot`, `ForkSummary`).

* **Peer summary (for the edge).** After every `SummaryPolicy.PeerEvery`
  tokens of growth (default 20,000), or when the cloud hands the foreground to
  the edge with an outdated summary, a fork writes a summary for the edge's
  window (its `SummaryBudget`, about 1K tokens) and appends it as a `summary`
  entry with its coverage: through the cloud's last sealed entry, excluding
  edge-only entries and entries the cloud never imported.
* **Own summary (for the cloud).** When the cloud's input reaches 0.6 of its
  window, a fork on the same snapshot writes the cloud's own summary. It is
  applied between replies, only after the peer fork settled, keeping a recent
  tail with intact tool pairs (`ApplySummary`). Compression of the cloud is
  never synchronous on its main thread.
* **Clear rule (edge takeover).** When the edge is about to run after another
  participant held the foreground since the edge last acted, and the newest
  peer summary is newer than the edge's last activity, the edge's context is
  replaced: the peer summary, a short index of the edge-only entries the
  summary cannot hold (readable in full with `thread_read`), and the entries
  after the summary. The edge does not need its older context: the summary
  speaks for it. An edge that kept the foreground (for example while the cloud
  ran a background task for it) is not cleared; it imports incrementally. A
  summary's coverage excludes edge-only entries and every entry the cloud
  consumed without seeing in full (skipped or cut imports), so a clear never
  drops what the summary does not hold. A clear that does not fit in one round
  is applied in chunks; its first round resets the context and sets the
  cursor, which may then be below the old one (`ThreadUpdate.Cursor`). The
  clear counts as done only when its last round goes with the activation; if
  a later round fails, the participant's next import clears again (also after
  a failed background task), because only a clear brings back its own
  earlier replies.
* **Incremental import.** Otherwise the participant imports the entries past
  its cursor, within a share of its window (`ImportPolicy`).
* **Chunked import.** When even that does not fit (for example the cloud is
  unavailable, so no fresh peer summary exists, or the cloud returns after a
  long edge stretch), the Coordinator imports in chunks and compacts between
  them with `agent.CompactStrict`, which leaves the state unchanged on any
  failure. After `ImportPolicy.MaxRounds` it moves on and tells the participant
  which entries to read with `thread_read`.

The cloud's tool outputs are kept outside the log and every context in the
event list. The edge reads them on demand with `event_read` (outline, search,
page). Nothing is pushed. The cloud reads edge tool outputs only when the host
sets `Config.CloudReadsEdge`, and never an edge-only record (outputs of
private tools, or recorded while the edge context was private).

## Privacy and taint

* An edge participant's writes are `edge_only` while its context holds
  edge-only material, unless the host's `DisclosurePolicy` classifies the text
  (including the tool digest) as shareable. A clear that imports nothing
  edge-only resets this.
* Tools listed as tainting (web fetch, search, mail) mark the activation and
  its entries `Tainted`; importing a tainted entry taints the importer. With
  `agent.WithTaintGate` and a permission engine, a tainted agent's allowed
  calls to gated tools (by default the four writing thread tools) need the
  user's confirmation. In `ModeDontAsk`, and in the loop bridge, which cannot
  ask the user, gated calls are denied instead. The participant's own check
  for a write in the same step as a tainting tool is coarser: once the gate
  list names any tool besides the thread tools, every thread write counts as
  gated there.
* Failover from a failed edge activation to the cloud never carries the edge's
  error text.

## Wiring

The example is the complete reference; the shape is:

```go
store := thread.NewMemoryStore() // or thread.OpenFileStore(path)

edgeDesc := thread.Descriptor{Name: "edge", Zone: thread.ZoneEdge, Window: 8192,
    SummaryBudget: 1024, ToolGroups: []string{"device"},
    Description: "Small on-device model with the device tools and the user's private data."}
prep, err := participant.Prepare(edgeDesc, participant.PrepareOptions{})
// handle err
edgeAgent := agent.NewUnifiedAgent("edge", edgePrompt, edgeModel,
    append(prep.Options(),
        agent.WithToolkit(tool.NewToolkit(append(prep.Tools(), deviceTools...)...)),
        agent.WithPermissionContext(permission.NewContext(permission.ModeDefault)),
        agent.WithContextConfig(&agent.ContextConfig{ContextSize: 8192}))...)
edge, err := prep.Participant(participant.AgentConfig{Descriptor: edgeDesc, Agent: edgeAgent,
    PrivateTools: []string{"read_sensor"}})

cloud, err := participant.NewTeamParticipant(participant.TeamConfig{
    Descriptor: cloudDesc, Master: buildMaster, Members: members,
    Load: loadMasterState, // reads the checkpoint buildMaster restores from
    TaintingTools: []string{"web_fetch"}})

router, err := route.New(route.Config{Mode: route.Hybrid, Edge: "edge", Cloud: "cloud",
    Classifier: jevClassifier, Hosted: true})

coord, err := thread.NewCoordinator(ctx, thread.Config{
    Store: store, Participants: []thread.Participant{edge, cloud}, Router: router,
    Disclosure: policy})
defer coord.Close()

go func() { for ev := range coord.Events() { render(ev) } }() // must be drained
_, err = coord.Submit(ctx, thread.Query{Text: "…", Disclosure: thread.DiscloseShared})
err = coord.WaitIdle(ctx)
```

Agents need a permission context whenever the writing thread tools are
installed. Pass host loop hooks through `PrepareOptions.LoopHooks`
(`agent.WithLoopHooks` replaces earlier hooks). Answer confirmation requests
from `Events()` with `Coordinator.SubmitUserConfirm`. Show only events with
`Foreground` set as the reply: background tasks and late output also appear
in the stream, marked `Foreground: false`. `Interrupt` cancels the foreground
activation; a transfer, task, report or artifact its tools had not committed
yet is refused (`thread.ErrInterrupted`), nothing is written and the model is
told so. A write that committed just before the interrupt reaches the agent's
context as a note before its next activation, so the agent and the log agree.
Its late answer is never taken for a later call: the agent matches external
results and confirmations by reply ID as well as tool-call ID, because some
providers (Gemini) repeat tool-call IDs across replies. If an interrupt reaches
the new owner of a Transfer while the old owner's pre-transfer events still
wait to be shown, the rest of them are dropped from the event stream (the
Transfer's reply entry still carries the whole text), so nothing of an old
owner follows a later owner's events. While those events wait, the new owner
does not start and new entry events wait behind the Transfer's; they flow
when the user reads, when the old owner's stream ends or on `Interrupt`.

A custom `thread.Participant` must fail `Activate` when it cannot apply the
activation's Sync (or does not know whether it did); returning a channel means
the Sync was applied, and the Coordinator then records a clear's reset.

## Accounting

`Coordinator.Ledger()` records every model call by participant, path (member)
and kind: `main`, `fork_peer`, `fork_own`, `classifier`, `compaction`. Unknown
usage stays unknown: a call whose usage the provider did not report is counted
as unknown, and totals give no cost when any call is unknown or unpriceable.

## Durability and recovery

`thread.FileStore` writes one JSON line per append batch and fsyncs it (and
the directory when it creates the file); a torn final line is dropped on open,
anything else malformed is an error. After an fsync failure the batch may or
may not be durable, so the store refuses further writes. Duplicate
writes with the same dedup key are idempotent. On restart the owner and epoch
come from the log, participants' cursors from their agent checkpoints, and a
user input left without an answer gets a `recovered` marker. It is not re-run,
because its effects are unknown; a background task left without a report gets a
report saying it did not finish. A participant accepts one Coordinator at a
time: `Coordinator.Close` releases the participants from this package
(`thread.Releaser`), so a new Coordinator over the reopened log can bind them
again. A graceful `Close` seals a running reply as interrupted instead of
leaving it for recovery.

## Limits

* Offline tests with scripted models only; no live measurement of this runtime.
* One Coordinator process per log; `FileStore` takes no file lock and keeps the
  whole log in memory.
* The Coordinator does not persist which peer summary a participant holds,
  which entries the cloud consumed without seeing in full, nor a clear left
  unfinished by a failed round; after a restart it assumes none of them, so a
  summary written after a restart can claim entries skipped before it, and an
  edge whose clear failed just before a restart may miss its own earlier
  replies until the next clear.
* Steering inputs delivered mid-reply leave no trace on the reply entry; after
  a restart one can receive a harmless `recovered` marker.
* Providers that report no usage show up as unknown usage; the agent's
  per-call event reports zeros in that case, which the participant treats as
  unknown.
* A gated tool called in the same step as a tainting tool is refused once and
  asks the user when called again, because the agent's taint changes at step
  boundaries. With a gate list beyond the thread tools, this applies to every
  thread write (see "Privacy and taint").
* A settle failure after a normal end (a note or a checkpoint the participant
  could not save) is logged; the Coordinator reports end errors only for
  failed ends.
* The loop bridge (`UnifiedAgentRunner.LoopOptions`) cannot hand the
  foreground over or ask the user; use the Coordinator.
* The privacy guarantee covers what this runtime sends: the log, imports,
  briefs, reports, the classifier state and the event list. It does not cover
  tools a host gives the cloud that reach edge data by other means.
