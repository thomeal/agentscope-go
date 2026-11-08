# Main Thread: one session shared by a cloud and an edge agent

The experimental `thread` packages let a cloud agent and an edge (on-device)
agent serve one continuous user session. Each keeps its own context, model and
tools. They share an append-only log, the Main Thread, that holds only what
was published: the user's input, the text the user actually saw, which tools
ran (each call's name and arguments, cut at 200 characters, never outputs),
summaries one side wrote for the other's window, ownership changes, background
tasks and their progress, reports, artifacts and markers. A Coordinator is the
single writer of that log.

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

The example runs a short scripted session (a query on the cloud with a
sub-agent, a transfer to the edge, a query the user names for the edge, a
background task with a report) and prints the log, the owner and the ledger.

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

**No privacy classes.** Every participant may read every entry, and the
cloud imports the edge's entries like any others. Keeping some data on the
device is future work; see
[Main Thread privacy](design/main-thread-privacy.md).

**Cursors.** Each participant's agent state holds the last entry it imported
(`ThreadCursor`, schema version 3 checkpoints). The cursor and the imported
content change in one state update, including the entry that triggers a reply
(`agent.WithThreadInput`).

The invariants the Coordinator keeps are listed in
[`thread/doc.go`](../pkg/agentscope/thread/doc.go).

## How a query flows

1. `Coordinator.Submit` appends the user input.
2. If an activation is running, the input is steering for the current owner and
   arrives at its next safe boundary (after a tool step, before the next model
   call). Routing only happens at a query boundary.
3. Otherwise the router picks the owner. A change appends an owner change
   based on the input.
4. The Coordinator imports what the owner has not seen (see "Context and
   summaries"), then starts its reply. Events stream to the user.
5. When the reply ends, the visible text and the tool digest are sealed into a
   reply entry. An interrupted reply gets an `interrupted` marker. A failed
   one gets a `failed` marker with the error, `unknown_effects` when it had
   called tools, then one failover
   to another participant that may take the query, or `no_owner`. The entry
   that ends the run (the `failed` or `interrupted` marker, else the reply)
   lists the inputs it took besides its trigger (`Entry.Steered`): the
   steering and the older inputs its cycle carried as context. An input that found no owner waits parked: it does
   not count toward `Limits.MaxPendingInputs`, at most that many are kept (the
   oldest beyond that is retired and stays in the log as context), and a
   later cycle imports it as context.

### Failover

A user message never changes the route while an activation runs: it steers
the running owner. The failover is the one automatic handover. When the
owner fails, the Coordinator picks another participant that may take the
query and queues its activation; if that participant is running a
background task, the failover waits behind the task instead of interrupting
it. The new owner's input is one line:

```
[cloud failed: <reason> (#41). You now answer the user: continue their request #37 "<the request, at most 120 characters>" from where cloud left off. What cloud did is in the summary and the entries above; its latest progress is #40.]
```

The import that goes with it (a clear with the newest peer summary for the
edge, an incremental import otherwise) brings the `failed` marker and the
failed run's last progress, so the new owner reads the error and the task
state instead of being told them at length. The new owner also takes the
inputs that steered the failed run. Without a new owner
the request ends with `no_owner`, and its steering with it. A failed wake
does not fail over (its report was already answered); the inputs that
steered it are queued again.

## Routing modes and the classifier layer

`route.New(route.Config{Mode: ...})`:

| Mode | Home | The classifier decides |
|---|---|---|
| `local_based` | edge | go to the cloud only for stronger reasoning or cloud-only tools |
| `cloud_based` | cloud | go to the edge only for simple requests or device-only tools and data |
| `hybrid` | none | best participant per query; low confidence stays with the current owner |

The decision order is fixed: availability, a participant the user named,
tool groups only one side holds, host rules, the classifier, then the mode's
default. A classifier error, an invalid
answer, confidence below the threshold or a call that runs out of
`route.Config.Timeout` (default 10 s) falls through to the default; routing
never blocks the user on the classifier.

The classifier reads a fixed-size state, so it fits a small model's prompt
(Piccolo's 8,192-token row): the query (cut to 1,500 tokens), the summary the
cloud wrote for the edge window, recent published entries as text with the
names of the tools each reply used (arguments and outputs never appear) and
the participants. The hybrid question is stage 0's word for word, apart from
the participant names. The participant descriptions are stage 0's too, with
tool calls in place of the benchmark's records and the host's description in
place of its record-ID line. The question carries no warning about the
state's content: a state that steers the classifier can cost quality or
money. The state holds the query and recent entries whatever the
classifier's endpoint, so a hosted classifier sees them.
`route.BuildState` returns the exact state for logs.

The three windows involved are therefore bounded separately: the cloud's own
window (own notes from 10,000 tokens, installed at its compaction line), the
edge's 8K window (peer summary and budgeted imports) and the
classifier's prompt (fixed state budget).

## Handing the foreground over

The `transfer` tool (always called alone; `agent.WithExclusiveTools`) takes a
structured brief: goal, done, remaining, constraints and references. The
Coordinator appends the caller's visible text and the owner change in one
guarded write, ends the caller's reply with `types.ReplyHandedOff` (no further
model call; `OnReply` middleware cannot swallow it) and starts the target with
the brief. It never waits for a summary fork. Unlike a sub-agent call, nothing
returns to the caller: the other side continues with the user.

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

**Shared workspace.** When the participants' agents work on one workspace,
give each its own `tool.ReadCache` with `SetRejectStaleWrites(true)` through
`agent.WithReadCache`. A whole-file `Write` from a copy the agent read before
the other side changed the file is then refused, and the agent reads it
again; see [stale writes](tools.md#stale-writes-on-a-shared-workspace-experimental).
This includes a team's master and members: a `TeamPipeline` member runs on
the pipeline's context, not inside its master's tools, so a member without a
cache of its own has neither the read-first guard nor this check. Stage 0's
harness had a similar check, which also refused writes to files never read;
on the short tasks it refused 6 Main Thread writes.

**Progress.** An agent with `agent.WithPlanTool` reports its plan with
`update_plan`; each update becomes a `progress` entry of its run, and the
call stays out of the reply's tool digest. The plan is injected ahead of the
history on every call, so each update makes the history miss the prompt
cache on the next one. `agent.WithPlanAfterLatestInput` puts it right after
the newest user message instead, so a plan update re-sends only the current
turn uncached. Imported entries, notes and boundary input are user messages
too, and each one moves the plan, so the steps since the previous input miss
the cache once; a participant that imports often and rarely updates its plan
may cache better with the default. Its effect on answer quality has not been
measured.
A projection shows only the newest progress of a run that is still going;
once the run replied, transferred, reported, answered late or was
interrupted, its progress drops out. A failed run keeps its last progress
visible, which is what a failover or a recovery points at.

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

* **Peer summary (for the edge).** At the end of a cloud reply, when what a
  handover would bring the edge (its newest peer summary plus the entries
  after it, plus what the reply added) reaches `SummaryPolicy.PeerRatio` of
  the edge's window (default 0.8), so the edge can always take over within
  its window, failover included; also when the cloud hands the foreground to
  the edge, or when its own notes need a covering summary (below). A positive
  `PeerEvery` adds a refresh every that many tokens of growth; it defaults to
  20,000 only when the edge's window is unknown. The fork writes a summary
  for the edge's window whenever the newest one is outdated, within the
  edge's `SummaryBudget` or, when that is zero, 5/6 of the edge's local
  summary total (2,560 tokens of an 8,192-token window; see below), and appends it as a `summary` entry with its coverage:
  through the cloud's last sealed entry, excluding entries written by others
  after the cloud's cursor. The cloud imports what it
  lacks in full, so everything else up to its cursor is in the fork's snapshot.
  `ForkGuard` (participant option) runs the host's guardrail rules on every
  summary a fork writes, since a fork does not pass through the reply loop.
* **Own notes (for the cloud).** These follow Claude Code's session memory.
  The first notes are forked once the cloud's input reaches
  `SummaryPolicy.OwnInit` (default 10,000 tokens), then updated whenever the
  input grew `OwnEvery` (default 5,000) since the last update; a compaction
  lowers that baseline. The first fork writes the notes in fixed sections
  (task, current state, files and identifiers, decisions, errors and fixes,
  next steps). An update gets the current notes and answers only with
  FIND/REPLACE edits (a section rewritten whole is one edit) or with
  `NO CHANGES`, as Claude Code's memory update may only edit its notes file;
  an answer that is neither, such as complete notes or a remark about them,
  never replaces the notes. An answer that is not edits, an edit that does
  not apply, or notes over `OwnMaxTokens` are sent back for one rewrite, and
  an update that still fails leaves the notes as they were.
  The notes are not applied when written: the Coordinator offers them to the
  cloud (`Forker.OfferOwnSummary`, `agent.OfferSummary`), and the agent
  installs them without a model call when its context reaches its own
  compaction line, keeping a recent tail with intact tool pairs and
  everything after the notes' snapshot. The offer is saved with the agent's
  state, so a team master rebuilt for the next activation still holds it,
  and notes that finish after the next activation built its master are
  offered to that master, which matches them by message ID.
  Notes are offered only once a peer summary covers their snapshot: once they
  replace the history, its tool work could no longer reach the edge. Near the
  compaction line (input plus `OwnEvery` reaching it) the peer fork runs on
  the same snapshot. When an offer no longer matches the context, or the
  context is still at the compaction line once the notes are in, the agent's
  synchronous compression runs, as Claude Code falls back from its
  session-memory compaction.
* **Clear rule (edge handover).** When the edge is about to run after another
  participant held the foreground since the edge last acted, and the newest
  peer summary is newer than the edge's last activity, the edge's context is
  replaced in one update that goes with the activation: the peer summary and
  the entries the summary does not cover, including the edge's own when the
  summary excludes them. The edge does not
  need its older context: the summary speaks for it. The update is not
  chunked or compacted on the way in: what outgrows the window is the edge
  agent's own compression's job, as in any reply; nor is what reaches the edge
  during that reply (steering input, reports). A clear whose activation fails
  is not recorded, and the next handover clears again: an agent may have
  applied it in memory without saving it, and applying it again is harmless,
  since it carries everything up to the new trigger. An edge that
  kept the foreground (for example while the cloud ran a background task for
  it) is not cleared; it imports incrementally.
* **Incremental import.** Otherwise the participant imports the entries past
  its cursor. A cloud participant imports them in full; an edge import stays
  within a share of its window (`ImportPolicy.EdgeShare`).
* **Chunked import (edge fallback).** When an edge import does not fit (no
  newer peer summary exists, for example because the cloud is unavailable),
  the Coordinator imports in chunks and compacts between them with
  `agent.CompactStrict`, which leaves the state unchanged on any failure.
  After `ImportPolicy.MaxRounds` it moves on and tells the participant
  which entries to read with `thread_read`.

* **Local summary budget.** The edge's own summary comes from its agent's
  synchronous compression in the reply loop; nothing forks on the edge.
  `agent.ContextConfig.ThreadSummaryTokens` is the budget the edge's own
  summary shares with the peer summary it holds. Zero (the default) sizes it
  from the window, 3/8 of it (3,072 tokens of an 8,192-token window; stage 0
  used 3,000), applied while a peer summary is held. A positive value fixes
  it, a negative one turns it off, and `UnifiedAgent.SetThreadSummaryTokens`
  changes it at run time. The own summary gets what the peer summary leaves,
  a newly arrived peer summary elides an older own summary to that share at
  once, and the peer summary no longer eats the recent tail's
  `ReserveRatio` share. The peer summary's own budget, when the edge's
  descriptor leaves `SummaryBudget` at zero, is 5/6 of that total (stage 0:
  2,500 of 3,000).

The event list is opt-in (stage 0 measured no gain from it): with
`CaptureOutputs` set on a participant and `event_read` installed on its peer,
that participant's tool outputs are kept outside the log and every context,
and the peer in the other zone reads them on demand with `event_read`
(outline, search, page; every read is capped). Nothing is pushed, and a
participant is never offered records from its own zone.

## Wiring

The example is the complete reference; the shape is:

```go
store := thread.NewMemoryStore() // or thread.OpenFileStore(path)

edgeDesc := thread.Descriptor{Name: "edge", Zone: thread.ZoneEdge, Window: 8192,
    SummaryBudget: 1024, ToolGroups: []string{"device"},
    Description: "Small on-device model with the device tools."}
prep, err := participant.Prepare(edgeDesc, participant.PrepareOptions{})
// handle err
edgeAgent := agent.NewUnifiedAgent("edge", edgePrompt, edgeModel,
    append(prep.Options(),
        agent.WithToolkit(tool.NewToolkit(append(prep.Tools(), deviceTools...)...)),
        agent.WithPermissionContext(permission.NewContext(permission.ModeDefault)),
        agent.WithContextConfig(&agent.ContextConfig{ContextSize: 8192}))...)
edge, err := prep.Participant(participant.AgentConfig{Descriptor: edgeDesc, Agent: edgeAgent})

cloud, err := participant.NewTeamParticipant(participant.TeamConfig{
    Descriptor: cloudDesc, Master: buildMaster, Members: members,
    Load: loadMasterState}) // reads the checkpoint buildMaster restores from

router, err := route.New(route.Config{Mode: route.Hybrid, Edge: "edge", Cloud: "cloud",
    Classifier: jevClassifier})

coord, err := thread.NewCoordinator(ctx, thread.Config{
    Store: store, Participants: []thread.Participant{edge, cloud}, Router: router})
defer coord.Close()

go func() { for ev := range coord.Events() { render(ev) } }() // must be drained
_, err = coord.Submit(ctx, thread.Query{Text: "…"})
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
come from the log, and participants' cursors from their agent checkpoints.

Cycles run one at a time, so the stopped process was answering one turn:
one cycle and the inputs that steered it or waited for it. A routing owner
change is based on its input, but the router writes one only when the owner
changes, and a run writes nothing before it ends unless it reports
progress. So the turn holds every open input that no entry is based on,
wherever it stands in the log (an input sent while the previous reply was
still streaming, or right after an interrupt, ran after that reply or
marker), and the open requests with an entry after the newest finished
request (such as an input skipped by an earlier cycle that got its own
cycle afterwards). The owner resumes the turn's requests as one: the
newest of them that an entry is based on (an owner change, progress, a
marker), else the newest, with the others joined to it. An
input a failed or interrupted run listed (`Entry.Steered`) goes with that
run's request: it is joined when the request is resumed and ends when the
request ended without an answer.

The owner gets a `restarted` marker and an input that quotes the request
and the inputs joined to it, the host's `Config.RecoveryNote` (why the
process stopped, on one line), the failure before the restart if any, and
the latest progress. The input also warns that work done before the restart
may or may not have taken effect, so the owner checks the actual state
before redoing anything. A request a Transfer handed to the owner is resumed
there like any other, and the input quotes it. If the owner is gone, the
turn's routing never finished: its request is routed again behind a
`requeue:` marker that counts the restart, the turn's other inputs that
nothing is based on are queued again as Submit would queue them, and one
that something is based on gets a `recovered` marker. An open request whose
entries all precede the newest finished one gets a `recovered` marker and is
not re-run. A run that ends normally
with no text and no tool calls writes no reply, so recovery cannot tell it
finished; such a request is taken as in flight. After two restarts of the same
request it gets a `recovered` marker on its newest restart and waits for
the next message, and the inputs that would have joined it get `recovered`
markers too. Every marker has a dedup key of its own, so repeated restarts
are each recorded. A background task left without a report gets a report
saying it did not finish, which points at its last progress. A restored
participant re-imports its own entries past its checkpoint, its live progress
included. A participant accepts one Coordinator at a
time: `Coordinator.Close` releases the participants from this package
(`thread.Releaser`), so a new Coordinator over the reopened log can bind them
again. A graceful `Close` seals a running reply as interrupted instead of
leaving it for recovery.

## Limits

* Offline tests with scripted models only; no live measurement of this runtime.
* One Coordinator process per log; `FileStore` takes no file lock and keeps the
  whole log in memory.
* The Coordinator does not persist which peer summary a participant holds;
  after a restart the next clear decides from the log.
* **Evaluation to do: local compression.** The edge compacts synchronously in
  its reply loop. Whether an asynchronous, continuously updated summary (the
  Claude Code session-memory style the cloud's forks follow) is stable enough
  on a weak local model has not been evaluated; compare the two on task
  correctness and cloud cost before changing the edge's default.
* The cloud's own notes are scheduled at the end of each reply; Claude Code
  checks after every model response, so a long tool-heavy reply here can
  reach the compaction line before its notes are due, and the agent then
  compresses synchronously.
* Resuming a request after a restart cannot tell which of its tool calls took
  effect; the resumed owner is told to check the actual state.
* Providers that report no usage show up as unknown usage; the agent's
  per-call event reports zeros in that case, which the participant treats as
  unknown.
* A settle failure after a normal end (a note or a checkpoint the participant
  could not save) is logged; the Coordinator reports end errors only for
  failed ends.
* The loop bridge (`UnifiedAgentRunner.LoopOptions`) cannot hand the
  foreground over or ask the user; use the Coordinator.
* No privacy boundary: the cloud reads the edge's entries, briefs, reports
  and recorded tool outputs, and a hosted classifier reads the query and
  recent entries. Do not give the edge data that must stay on the device.
  Untrusted tool output is not tracked either: nothing gates a write after a
  web fetch beyond the host's permission rules. The design for both is in
  [Main Thread privacy](design/main-thread-privacy.md). Logs and checkpoints
  written by earlier, unreleased builds that had privacy classes still load:
  their edge-only entries are then read like any others, and a saved thread
  index is dropped.
