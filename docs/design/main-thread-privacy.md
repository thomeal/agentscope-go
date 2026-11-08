# Main Thread privacy: future work

Status: design note. Nothing here is implemented. The Main Thread runtime
([guide](../main-thread.md)) has no privacy classes and no taint tracking:
every participant reads every entry, and the cloud imports what the edge
wrote like anything else.

This note records what the runtime should eventually protect, what stage 0
showed, why a first implementation was taken out before merge, and the
direction a systematic design should take.

## The requirement

A cloud agent and an edge agent sharing one session raise two separate needs.

**Confidentiality: data that stays on the device.** A user tells the edge a
door code, the edge reads a health record or a local file, a device tool
returns a sensor log. Some of this must never reach the cloud, by any path:
the log entries the cloud imports, a transfer brief, a report or task, a
summary, the routing classifier's state, the tool-output list, a failover or
restart note, or an error message that quotes the data.

**Integrity: content that must not steer actions.** A web page, a mail or a
search result can carry instructions. A participant that has read such
content should not take writing or device actions on its own authority, and
the concern travels: an edge that imports a report written from a web page
holds that content too.

Both needs reach every path that moves content between participants. That is
what makes them hard to bolt on later, and also why a partial design is
worse than none: a host would rely on it.

## What stage 0 showed

Stage 0 compared Main Thread with schemes a host could run instead, on a
local 4B model with an 8K window and a hosted cloud model. It measured its
own harness implementation of the scheme, not the runtime in this
repository, so its results are results for the scheme. Two of them favored
Main Thread.

**Privacy.** On synthetic sessions holding private records, every request
body sent to the cloud was scanned for the codes of device-only turns: their
user text, their expected reply and any record they read.

| Scheme | Private codes found in cloud requests |
|---|---|
| Main Thread | 0 (in both runs) |
| One shared history, 8K window | 81 per session |
| One shared history, 32K window | 225 per session |
| Everything in the cloud | 690 per session |
| Direct summary handoff | 6,985 over 30 sessions |
| Direct handoff, brief kept in the system prompt | 6,045 over 30 sessions |

Every other scheme sends the cloud what the edge saw. Main Thread kept device
data out because its log carries what was published, not each side's
context. The labels in stage 0 were the benchmark's: every turn came marked
shared or device-only. So the result shows the log can hold the line when the
labels are right. It does not show that a host can produce those labels.

**Long tasks.** On the long coding tasks, scored by hidden tests with up to
three rounds of compile repair, Main Thread scored .848 against .423 for one
shared 8K history (+42.5 points, 95% CI [+21.8, +60.8]) at the same cloud
cost (ratio .97), and .612 for direct summary handoff (+23.6 [+13.4, +34.5])
at about a fifth more cloud cost. The lead over the shared history is clear
in every reading. The lead over handoff is clear only with the repair
rounds: as scored, without them, it is +22.7 [−2.7, +46.1]. On the short
tasks Main Thread tied the shared history and beat handoff by +11.6 points at
the same cost. Those task sets hold no private data, so this comparison does
not depend on any privacy mechanism.

**Everything in the cloud.** Sending everything to the cloud scored higher
on both task sets: .966 against .848 on the long tasks with repair (−11.8
[−20.0, −5.2]) at about the same cloud cost, and .989 against .880 on the
short tasks (−11.0 [−14.6, −7.5]), where Main Thread cost 28% less. Stage 0
set this scheme aside because it sends the cloud all private data. Without a
privacy design, a host that needs no data kept on the device can run it, and
against it stage 0 shows Main Thread a lower cloud cost on short tasks, not a
higher score. Serving a turn while the cloud is down is the other argument
for an edge agent; it follows from the design and was not measured on task
results.

Removing the implementation keeps the comparison with the shared history and
handoff, and gives up the privacy result until a design like the one below
exists.

## The first implementation, and why it was removed

A first implementation was built and reviewed, then taken out before merge.
It is kept for reference on the branch `archive/main-thread-privacy-taint`
(commit `ed9d558`). It worked like this:

* every entry was `shared` or `edge_only`, and could be marked `tainted`;
* `Submit` took a per-query disclosure, and an unclassified query was
  treated as device-only while the edge was tainted;
* the cloud's imports showed gap markers instead of device-only entries, an
  edge clear carried an index of the device-only entries the summary could
  not hold, and summaries recorded which entries they left out;
* the router removed the cloud for device-only queries and refused a
  classifier endpoint off the device unless it was declared hosted;
* an edge whose context held device-only material wrote device-only entries
  unless a host `DisclosurePolicy` cleared the text, and the agent saved that
  privacy in its checkpoint through a probe asked before every save;
* a taint gate asked the user to confirm writing tools once the agent had
  read output of a tool listed as tainting;
* failover withheld the edge's error text, and crash recovery pointed at a
  transfer's brief instead of quoting a device-only request.

The review rounds and the design discussion found these problems:

1. **No source for the labels.** Everything downstream was as good as the
   label on each query and each write, and nothing produced good labels. The
   library shipped no disclosure policy; the example used a keyword match.
   Stage 0 used labels from the benchmark.
2. **Privacy was a sticky flag on a whole context.** Once the edge's context
   held any device-only material, everything it wrote was device-only unless
   the policy cleared it: replies, progress, plans and briefs alike. A plan
   held across a clear kept the edge private after the private material was
   gone, and recovery had to trust a checkpointed flag over the log. The rule
   matched neither the plan (edge replies device-only by default) nor stage 0
   (each reply follows its input's label).
3. **It cut across the whole runtime.** The agent checkpoint, the import and
   clear rules, summaries and their coverage, the router, the tool-output
   list, failover, crash recovery and the transfer rules all had their own
   visibility checks. Each new feature had to reason about privacy again, and
   review kept finding leaks at new seams: one round found that a restart
   quoting the inputs it joined could quote a device-only request to the
   cloud.
4. **Taint was coarse.** Tools were tainting by name in an opt-in list, and
   writes were gated by name. A write in the same step as a tainting call was
   refused once, then asked about. With a gate list beyond the thread tools,
   every thread write counted as gated. Hosts that cannot ask the user (the
   loop bridge, the don't-ask mode) got denials. Taint spread through whole
   entries and never wore off.
5. **The guarantee had a narrow scope.** It covered what the runtime sends.
   A tool the host gives the cloud could still reach device data another way.

## Direction: labels that travel with the data

A systematic design should move the decision from contexts to content, and
the enforcement to one place.

**Labels on content, set at the source.** Each piece of content carries a
label with two parts: where it may go (device or shared) and whether it can
be trusted (trusted or untrusted). Labels are set where the content enters:

* user input by the host's interface (an explicit choice) or by an on-device
  classifier, whose error rates must be measured before anyone relies on it;
* tool results by the tool's declaration (a device tool's output stays on
  the device, a web fetch is untrusted), not by a host's name list;
* records and files by their store.

**Propagation by derivation.** Content a model writes takes the combination
of the labels it could have read: device if any input was device-only,
untrusted if any was untrusted. This is the information-flow rule used in
recent agent-security work, such as FIDES (Costa et al., 2025,
[arXiv:2505.23643](https://arxiv.org/abs/2505.23643)) and CaMeL
(Debenedetti et al., 2025, [arXiv:2503.18813](https://arxiv.org/abs/2503.18813)). Without a way to lower a label, it creeps the way
the sticky flag did. So the design needs explicit lowering points:

* the user approves a brief or a reply for the cloud;
* a local model writes a sanitized summary, and the host's policy endorses
  it;
* the log records each lowering as an entry with its provenance, so review
  and recovery can see it.

A summary of untrusted content stays untrusted unless it is endorsed the same
way.

**One enforcement point.** Features carry labels and do not check them. A
single egress filter checks every block that leaves the device: Coordinator
projections to cloud participants, the classifier client, the tool-output
list and any transport the host adds. Failover notes, restart notes and
error texts are built from labeled content, so they pass the same filter
without rules of their own.

**Integrity by capability.** A tool declares whether it writes, controls a
device or only reads. An agent whose context holds untrusted content needs
confirmation for writing and device capabilities, and the rule is the same
wherever a call runs. A host that cannot ask the user gets a defined
fallback, set by the host.

**State lives with the content.** Labels ride on message blocks and log
entries, so a restored agent has them without a separate flag, and a clear
or a compaction drops a label only when it drops the labeled content.

## Open questions

* Where labels come from on a real device, and how often a local classifier
  gets them wrong. Measure it on held-out sessions before trusting it.
* The unit of a label: content block, message or log entry.
* How the cloud continues a task whose material it cannot see: briefs,
  lowered summaries, and what that costs in task correctness.
* Prompt caching: redacting content changes the prefixes the cloud caches.
* Evaluation: stage 0's harness, which scans every cloud request for device
  codes, plus task correctness and cloud cost, with labels from a realistic
  source instead of the benchmark's.

## Until then

Do not give the edge data that must stay on the device while a cloud
participant shares its session. Rely on the host's permission rules for
writing and device tools; the runtime does not track untrusted content.
