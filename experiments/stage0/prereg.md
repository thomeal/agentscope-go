# Stage 0 pre-registration

> **Published copy.** This record was kept on local branches while the benchmark
> ran. For publication, local paths and machine details were replaced and a few
> sentences were reworded for a public reader; no plan, amendment, number or
> decision was changed. Terms used below:
> - "The user" is the owner of this fork, who set the direction; "the maintainer"
>   is whoever ran the harness.
> - "The plan" is the design plan the benchmark was written against. It is not
>   published; the RFC and `docs/design/main-thread.md` describe the design.
> - Run names (`runs/...`, `l3c`, `long-s2` and the like) are local run
>   directories. They are not published; the figures here are copied from
>   their reports.
> - Branch names and commits refer to the local history the runs used. The
>   branch `eval/stage0` holds this harness on top of the final
>   `feat/main-thread`; the local commits named here are not published.

Written before L2 starts. The gates come from §15 and §18 of the plan. Everything below
was fixed on the first five pilot sessions and two CBench items; the rest of the pilot
had not been run at that point. The main text holds no result. Amendment 1 at the end
changes it, and it says what had been seen when it was written.

## Frozen material

| Item | Value |
|---|---|
| `corpus.Hash()` (full generator) | `bafc910e91ef96d98cc6cc2dd584f76ddd4bb997dd604be38d3143df9c7834e2` |
| Pilot hash | `8753d432…`, pinned by `TestHashPinned` |
| CBench pilot hash | `d288a819…`, pinned by `TestHashPinned` |
| `s2.Hash()` | `47d8b0df13a7f35e9d394405fcb5c922194524963f7f9758c07ce90a0d22f38e` |
| `s2.AcceptanceHash()` (Amendment 3) | `4bfe9974fe50d326b358112a36265d074dc4683921f39f18546089bf92788bbf` |
| Scorer | `score/`, unchanged since the freeze |

Pilot: `corpus.Pilot()`, 20 sessions of 20 turns and 10 of 50. CBench pilot:
`corpus.CBenchPilot()`, 10 items. S2: `s2.Tasks()`, 15 tasks.

## Configuration

Cloud: `gpt-6-luna`, Responses API, `store:false`, default 30 minute tier, buffered calls,
declared window 1,050,000. Main calls send no effort field, so they use the API's default
for the model, which is medium.

Local: `Qwen3.5-4B` on vLLM 0.33.0 in a Linux VM, thinking off, `temperature: 0`, one
exact-token counter (`/tokenize`) for every local window, window 8,192 (32,768 for
A3-32K, on a restarted server), `ReserveRatio` 0.3.

Budgets used for the primary analysis:

| Setting | Value |
|---|---|
| Fork B and brief target | 2,500 estimated tokens, trimmed deterministically |
| Fork B refresh | 5K of cloud input growth |
| Local summary total | 3,000 |
| Summary effort | `low`, through a `configuration_update` item after the prefix |
| Fork B and cloud brief limit | 7,000 |
| Local brief limit | 5,000 |
| Fork A and merged limits | 20,000 and 24,000 |
| Clear rule | As in the plan: clear when the newest summary is past local's last activity |

## Deviations from the plan, all decided before L2

1. **Qwen3.5-4B on vLLM instead of an 8B model on llama-server.** The user chose it. vLLM
   rejects a prompt above the window with HTTP 400 in wording `IsContextLimitExceeded`
   recognizes, so A6 needs no translation layer.
2. **Budgets.** The plan proposed a 1,000 token target and a 20K refresh, and left them
   to calibration. On five pilot sessions those gave T1 2 of 66 record facts at local
   takeovers. The values above are the best T1 setting found on those five sessions. They
   are a choice in T1's favor, which makes a G2 failure harder to put down to its
   parameters, though it cannot rule that out. A4 gets the same target, as G2 requires.
   The tuning runs are in `runs/smoke3` and `runs/v-*`.
3. **Handoff prompt.** The first text made the model give up on record facts because they
   could not all fit (948 characters, 4 codes). The frozen text gives an allowance and an
   order to spend it in. Fork B, every brief and C7 share one text.
4. **Log rules for T1.** A short system-prompt section explains imported entries. Without
   it the local model answered a request to read a record by claiming to have read it, with
   no tool call. A4 and A3 have no imported entries and do not get the section.
5. **Probe repair.** The local model sometimes writes broken JSON, and the scorer is strict.
   Every arm gets one repair request; on the local side the server is held to the answer
   schema. The answers are the model's. What still fails counts as a parse failure. The
   scorer is unchanged.
6. **Tool summaries** clip arguments at 200 characters, because a `write_file` call holds a
   whole file. It changes nothing on S1, whose arguments are short.

## Gates

Primary metric: each session's pooled F1–F5 retention. Differences are paired by session,
with a percentile bootstrap over sessions for the 95% CI (`report/`, seed 1, 10,000
resamples). A percentile CI undercovers at small n. Fewer than 10 pairs is INSUFFICIENT,
which is a floor and not a claim of power. F6 and categories are secondary. Failures stay
in the denominator. Pilot gates can reject a clearly failing direction. Confirming a small
gain is for L4.

| Gate | Comparison | Pass | Otherwise |
|---|---|---|---|
| G1 | A0 − A3-8K on S1, S2 for direction | ≥ 10 points | Stop: a single history is enough |
| G2 | T1 − A4 and T1 − A4s on S2's hidden-test score, planned switches (Amendment 3; it read S1 retention before) | CI low > 0 and point estimate ≥ 5 points | Drop Main Thread, use direct handoff, keep routing only |
| G6 | C3 against C0, C4, C6, C7, C8 | §15's terms, below | §15's terms |

G6 in full: C3 − C0 has CI low > 0 and point estimate ≥ 5 points. C3 is not 5 points below
C6. C3's own-fact retention is not 5 points below C8. Lossy events are 0. Fork cache hit is
at least 70%. Fork A + Fork B cost at most 25% of main.

If T1 wins only on abrupt switches, §18 applies: periodic summaries and a log are justified
without the owner and concurrency machinery, and the plan shrinks accordingly.

## Secondary analyses, reported whatever they show

- T1 − A4-abrupt, the abrupt switch comparison.
- T1+brief against T1 and A4.
- Retention by recipient side and by category.
- **G2 sensitivity:** T1 and A4 on the plan's original budgets (20K, 1,000), run on the
  same S2 tasks as a separate run (Amendment 3; it ran on S1 sessions before). G2 is judged
  on the primary configuration. The sensitivity result is reported beside it.
- **G6 without F3:** F3 has a floor in CBench, because 855 code lines cannot fit in any
  summary tried here. Retention over F1, F2, F4 and F5 is reported next to the primary
  metric. It does not replace it in the gate.
- A3-32K, A6, A3-8K-h and C0h, recorded only.
- Switch cost: brief latency against T1's zero model calls, and fork against brief spend.
- Results excluding the five sessions used for tuning.

## Known limits

- The first five pilot sessions and two CBench items informed the configuration, so the
  pilot is not independent of it. L4 on acceptance seeds is.
- S2 runs model-written Go on this machine with key-like variables stripped and no OS
  sandbox. A scorer error is an infrastructure failure, not a zero.
- The corpus's main replies are very short, so "fork cost as a share of main" is higher here
  than it would be with real replies. The report gives absolute cost as well.
- `gpt-6-luna` is the smallest listed model, so C7 tests only prefix reuse, and it may not
  represent stronger models. The stronger-model check comes after L4.
- This machine is not an edge device, and the 8K window is imposed. Retention carries over,
  while first-token latency and energy do not.

## Amendment 1

Written after an independent review of commit `9936f8b` returned FAIL.

**What had been seen.** G1's rows (`runs/l2-g1`: A0 0.998, A3-8K 0.594, 30 sessions), all
C group rows (`runs/l2-c`), the tuning runs above, and two S2 smoke tasks (`runs/smoke-s2`).
G2 had been started in `runs/l2-g2` and was stopped when the review came back. Its rows
were not read and are not used, and its spend counts. So every G2 result that exists comes
after this amendment, and every G6 result comes after results of the unamended harness had
been seen. G6's changes below are declared as such.

**Why the G6 changes stand anyway.** D1 adds a condition the plan already had. D2 lowers
what the prompt offers and leaves the count as it was, so a trim still fails the gate. D3
and D8 fix requests that failed for reasons no arm intended. None of them moved a threshold.

**Changes to what is measured.**

| | Defect | Change |
|---|---|---|
| D1 | G6 had no cost condition | (Fork A + Fork B) / main ≤ 0.25 on C3+A, the arm that runs both |
| D2 | The prompt offered 7,250 characters and the trim fell at 7,497, so most overshoots counted as lossy | The prompt offers 6,000. A trim is still a lossy event, and `summary_trims` shows them apart |
| D3 | The repair's answer schema reached a compression started inside the repair: 16 failed compressions in `l2-c` | The schema applies to the answer only |
| D5 | A scorer failure on S2 counted as a zero, and scoring shared the session's deadline | A null retention, and the scorer has its own deadline |
| D6 | A4's brief sat in Context, where compression can rewrite it, while T1's summary sat out of reach. That favors T1 | New arm A4s holds the newest local brief in the system prompt. **G2 now also requires T1 − A4s to pass the same terms.** `briefs_compressed` counts what A4 lost this way |
| D7 | "Local summaries total 3,000" was not a bound: the peer was counted at chars/3 and the agent's budget at bytes/4, so A4 had 12,000 bytes and T1 with a full summary 2,000 | One unit, bytes/4, and one rule: peer plus own summary ≤ 3,000. Full peer leaves 1,126 tokens, 4,504 bytes. A brief in A4's Context is not charged, which favors A4 |
| D8 | A compression request carried the system prompt with the peer in it, so the keep arms mostly failed to compress (C8: 125 compress rows, 45 overflow, 57 at the length limit, 52 lossy) | The request leaves the peer out, as the plan's separate slot would. The output limit rises from 2,000 to 3,000, the local total |
| D9 | The log rules were coaching on tool use and A4 lacked them, which favors T1 | Split in two. Record rules go to every local agent on S1 in A4, A4s, T1 and the C group. Log rules go only to arms with log entries |
| D10 | C3 − C0 measured the prompt, not the fork | Recorded arm C0h: the same path with the handoff text and low effort. Without F3, C3 − C0 was already near zero in `l2-c` |
| D11 | G1's baseline did not get the tuned summary | Recorded arm A3-8K-h. A0 and A3-8K keep their `l2-g1` rows, which none of these changes touch: their compression goes to the cloud, they have no rules, and their budget was already the full total |

**Changes to the analysis.**

| | Defect | Change |
|---|---|---|
| D4 | The report pooled sets and had no record of budgets, so a sensitivity run could replace rows in silence | It refuses S2 rows beside any other set, tuning rows beside acceptance rows, and runs whose `manifest.json` budgets differ. `l2-g1` predates manifests and is named in the limits |
| D4 | S2 had no direction path | A separate S2 report with A0 − A3-8K and T1 − A4, build failures and scorer failures. It holds no verdict |
| D12 | Registered secondaries had no code | T1+brief − T1, retention by recipient and category, the C group without F3, results excluding the tuning sessions, latency by role |

**Smaller corrections.** A budget stop that a compression swallowed now leaves no row. The
report's Fork USD column is labelled as prefix forks, with a Summary USD column beside it,
because it read 0 for C0 and C7. `HARNESS.md` now states what it left out: A3 and A6 have
both record tools, A4-abrupt keeps earlier briefs, a brief cut off by its limit is delivered
while a fork is discarded, `taskRules` on S2, and the bootstrap's limits.

**What L2 runs now.** `l2b-g2` (A6, A4, A4s, T1), `l2b-g1x` (A3-8K-h), `l2b-c` (C0, C0h, C3,
C4, C6, C7, C8, C3+A), and A3-32K on a restarted server. The G2 sensitivity run on the plan's
budgets goes in a run of its own and a report of its own, which D4 enforces.

## Amendment 2

Written after an independent review of `git diff 9936f8b ad9601e` returned FAIL. It found that
D1 to D12 were in the code, that D1's ratio could not pass for a reason no arm intended, and
that a compression asymmetry remained in G2's T1 − A4 term.

**What had been seen.** Everything Amendment 1 lists, plus `runs/smoke4` and `runs/smoke4c`,
which are the first two tuning sessions and the first two tuning benches. No amended G2 row and
no amended G6 row existed when this was written. `l2-c`'s C3+A ratio of 0.547 had been seen, and
E1 changes how that number is computed, so E1 is declared as a change made after seeing it.

**Why E1 stands anyway.** §15 compares forks with the main thread, and estimates about 20%. In
the C group the main thread is 36 scripted steps that cost nothing and two live calls, so the
ratio compared forks with almost nothing. The threshold stays at 0.25.

**Changes to what is measured.**

| | Defect | Change |
|---|---|---|
| E1 | (Fork A + Fork B) / main had only two live calls below the line | Each scripted step is priced as the main call a real session would have made: the prefix read from the cache, the step's new tokens written, the reply as output, in estimated tokens at luna's prices. A step with a tool call really makes two calls and is priced as one, which understates the main thread and so works against the gate. The row carries it as `imputed_main_usd`, and rewrites (E5) go on the fork side |
| E2 | The agent charges the whole system prompt to the reserve, so a full PeerSummary used all of it. T1 and A4s kept no raw turns where A4, with its brief in Context, kept its share. That handicapped T1 in T1 − A4 only | The reserve of an arm with a summary outside Context grows by that summary's tokens, as the local tokenizer counts them, capped at 0.75. The tail now matches. The cost is more frequent compression, which is what a summary in that window costs |
| E3 | The compression limit was 3,000 tokens and the budget after a full peer 4,504 bytes, and the prompt gave no number, so a compression could elide and count as lossy | Every local owner's compression request states its budget, at 80% of it, in a `[Summary budget]` line of its compression-only system prompt, capped at 5,700 characters, where a summary meets that request's 3,000 token output limit. An owner with no summary outside Context reaches the cap, as does T1 or A4s with one up to 4,875 bytes, which tells them less than they have. A3's compression goes to the cloud and does not change |
| E5 | The prompt's allowance does not hold: on the five tuning sessions 7 of 24 Fork Bs ran past the cut, up to 14,505 characters. A blind cut in the middle can take any line | The host checks every summary and sends back what fails, up to 4 times, stating its length, the line where the limit falls and the share to aim for. A rewrite under 5,500 characters also goes back, because a model told to shrink can overshoot. What is still too long after that is cut from the end at a line boundary, the least important part under the prompt's own order, and a cut is still a lossy event. C0's template ends in the next steps, so C0 alone is cut in the middle, as the agent's own trim would. A final rewrite under the 5,500 line stands and is not a lossy event; `summary_rewrites` shows the loop ran. A round that submits nothing, by calling another tool or sending an empty summary, is answered and counted in `fork_strays`, and it uses up a round. `summary_rewrites` counts the bounces. A fork stays in its own context through a `submit_summary` tool appended after the prefix, as Codex adds a tool late: on real 51K and 57K forks 98% of the input stayed cached that way, and 0% with the same tool in the top-level list. A cloud brief bounces on its own clone with the same feedback as a user message. C0, C0h and C7 have no context to return to, so they bounce from the draft alone, and a rewrite holding a code the draft lacks is refused and uses up its round. That favors C3 over C0 and C0h a little, and it is what those paths can do. A local brief goes straight to the line cut, counted lossy: the local window cannot hold its context, the draft and the feedback, the 4B model barely shrank a draft on its own, and nothing checks its rewrite. That can cost A4, never T1, and is declared here. Fork A keeps its cut, and only C3+A runs it |
| E6 | An own summary written under the larger budget stayed beside a new summary until the next compression, up to 19,497 bytes in A4s, where T1's clear rule drops it. That favors A4s | At the slot the own summary is held to the budget the new summary leaves. A cut there counts as lossy |

**Changes to the analysis.**

| | Defect | Change |
|---|---|---|
| E4 | `CheckPooling` compared only runs with manifests, so `l2-g1` plus a sensitivity run gave an unmarked G2 verdict, and `l2-c` or `l2-g2` could pair with new rows | A run with no manifest may hold only A0 and A3-8K rows. Budgets are compared with the frozen ones, and a report on anything else says it is not a primary result, in the file and on stdout |

**How E5 was chosen.** On the seven oversize Fork Bs from those five tuning sessions: one rewrite from the draft alone came back as short as 1,141 tokens and the local model barely shrank at all (`runs/smoke5`). Plain feedback in context reached the limit on 3 of 7 within 2 rounds, and one draft grew. The loop above reached it on 6 of 7 within 4 rounds, 3 of them at the first bounce, and kept 350 of 430 facts' codes after any cut, where a line cut alone kept 333. It kept fewer on two: 39 against 52 on the longest draft, and 56 against 61. It cost about $0.0036 per oversize summary. Only tuning sessions were used.

**G6's "lossy 0" after E5.** A bounce is not a lossy event, because the writer chooses what to drop, though it does drop facts. That reads the condition more loosely than a blind count of lost text would. Retention still measures the result, and every G6 lossy condition prints how many times summaries were sent back, so a clean count cannot hide them.

**Declared, with no change.** The first three were in Amendment 1 and it did not state them.

- `ForkAPrompt` went from 6,000 tokens and 18,000 characters to 6,250 and 15,000, for the same
  reason as D2. That changes C4 and C3+A.
- C3's prompt offers 6,000 characters while C0's text gives no number and can fill to the cut.
  That works against C3 in C3 − C0.
- The budgets called "the best T1 setting" were tuned before D2, D7, D8, D9 and this amendment.
  They are kept as they are, so they are no longer the best setting found on the current code.
  Nothing has been tuned since, so they favor T1 less than that section implies.
- E2's reserve grows by the peer summary and ignores the own summary an agent writes later, so T1's
  room for output after a compression can be smaller than A4's: 272 tokens against 915 on a stub.
  That works against T1.

**Corrections to Amendment 1.** The bootstrap's limits are in this file's Gates section and in
the report, not in `HARNESS.md` as it said; `HARNESS.md` now has them too. D4 enforced separate
sensitivity reports only when both runs had manifests; E4 closes that. The cut fires above 7,499
characters.

**Pooling.** A report refuses runs at different commits, and a run with uncommitted changes under
`experiments/stage0` is not a primary result.

**Smaller fixes.** An S2 row with no scorer run now has a null retention whatever the cause. A run
is marked dirty by any change under `experiments/stage0`, and the check no longer depends on the
working directory. A resume at another commit is refused. Tests the review could mutate past now
go through the wire or the real call path: compression on T1, A4s and C8, the cloud having no local
rules, A4s's slot taking only cloud-written briefs, a canceled task still being scored, the
compression limit, and which categories `RetentionWithoutF3` keeps.

**A0 and A3-8K.** Their `l2-g1` rows stay valid: the review confirmed none of this reaches them at
code level, though it could not confirm which commit produced those rows, because they predate
manifests.

## Amendment 3

Made on 2026-11-07, before L3 and before the rest of L2.

**Why.** The person running this work decided that G2 should read whether the task came out right.
S1 retention asks about facts, and most of what T1 lost on S1 was F3: labelled codes from records
that the later work never needed. A lost fact is not a wrong task. S2 is the workload that measures
the task: hidden tests decide its score, and each task depends on something established earlier on
the other side.

| | Before | Now |
|---|---|---|
| G2's primary | T1 − A4 and T1 − A4s on S1 retention | The same two on S2's hidden-test score |
| Terms and the failure action | CI low > 0, point estimate ≥ 5 points; drop Main Thread | Unchanged |
| Beside it | | The share of tasks whose hidden tests all pass, and T1 − A0 |
| S1 on T1 | Decides G2 | A diagnostic by category, with no verdict |
| The S2 report | A direction, no verdict | Holds G2's verdict |
| G3 | §15, not yet run | Reads the same task score as G2 |
| G1 | S1, S2 for direction | Unchanged, and the S2 report states A0 − A3-8K on the task score |
| G6 | C-group retention | Unchanged: CBench has no tasks |

If A0 − A3-8K on the task score comes out below 10 points, the problem S1 showed does not show on
tasks. G1 stays as S1 decided it, and the report has to say that plainly, because the plan rests on
that gap.

**What had been seen.** This change is not blind, so here is everything it could have leaned on:

- `smoke6`, five tuning sessions: retention A4 .642, A4s .651, T1 .583. Without F3, A4 .810 and T1
  .760, driven by F5 and F6.
- `diag-fit`, the same sessions on older code: why F3 codes did not reach local.
- `smoke-s2`, two S2 tasks on older code: hidden tests passed A0 26/27, A4 26/27, A3-8K 24/27, T1
  15/27, with one T1 build failure.
- `l2-g2` was stopped and its results were not read or used.

The two sources point the same way for T1, so this change is not a rescue of T1: the only S2 data
seen was worse for it than the S1 data.

**Arms on S2.** A0, A3-8K, A4, A4s, A4-abrupt, A4+read, T1 and T1+read. A4s is there because G2
needs it. A4-abrupt is there for the abrupt-switch branch of §18. S2 has no probes, so A4-abrupt
and the read arms run as whole arms with their own cloud calls, where S1 has clones of the receiver
at a probe. A4-abrupt carries nothing at a switch.

**The event list.** The plan now lets local read what the cloud's tools returned, on demand and
bounded. `T1+read` and `A4+read` give local an `event_read` tool. The list needs no log, so A4 could
have it too, and that is why `A4+read` is a control.

G2 reads T1. If T1 falls short and T1+read clears the same terms against A4, A4s and A4+read, the
plan keeps Main Thread and the list becomes part of T1. That is two tries at one gate, which raises
the chance of a false pass. So acceptance on held-out tasks runs only the form that passed. On S1,
labels are restated word for word, which flatters a word search, so S1 says little about how well
the list would do on real questions.

**Repeats and the unit.** `--repeat R` runs each task R times. Repeats share a task and are not
independent, so the report averages them: n counts tasks and the CI resamples tasks. A repeat only
reduces the noise inside a task.

**Scale.** The 15 tasks in `s2.Tasks()` were available during tuning, so L3 gives a pilot verdict, as
L2 does on S1. After L3, an author blind to the harness writes held-out tasks, with set
`s2-acceptance`, and their number comes from L3's SD by the formula in `report/`. The report refuses
to pool the two sets. S1's acceptance run no longer has to carry G2: it keeps A0 and A3-8K for G1,
and CBench acceptance keeps G6.

**Deferred.** Handing local the raw tail of the cloud's records, as Claude Code's session memory
compact does, waits behind the event list. Raw tail reach on the generated S1 corpus is an upper
bound: 36% for the last record, 57% for the last six.

**A dropped connection.** `smoke7` lost a T1 session at turn 2, when six attempts over about 16
seconds all failed with EOF. A session error stays in the denominator, so an outage decided that
row. The transport now retries for about 90 seconds.

**`smoke7`, also seen before L3.** Five tuning sessions on code before the review's fixes: A4 .650,
A4+read .726, T1 .545, T1+read .555. T1's numbers carry that lost session. T1+read's clone was also
told the results were not available, which the review found and this amendment fixes, so T1+read's
number says little.

**After the first L3 run.** `l3`, at commit `3a86fc8`, gave T1 .922, A4 .756, A4s .518, A0 .992 and
A3-8K .892, which passes G2. Before reading that as a verdict, the maintainer looked at why A4 lost, because the
plan requires the baseline's best chance. T1's local was told that two assistants share a workspace
and to read its files, and A4's local was told nothing. So A4, A4s, A4-abrupt and A4+read now get
the same sentence, `taskPeerRules`. That helps A4. One side effect: when a task opens on local, T1's
local and A4's now both have their rules from the first turn, where in `l3` T1's had them only from
its first takeover. `l3` stays on record and does not count. `l3b` is L3.

One A4 failure read in full: local wrote `func Backoff(attempt int) time.Duration` where the user
had asked for `int`, did not run the tests, and the hidden tests then failed to build, which scores
the whole task 0. T1's local made the same first draft, ran the tests and fixed it. T1's log shows
the cloud's `run_tests` and its "Tests pass" replies, and S2's system prompt does not ask for tests,
so the log may be coaching local by example. That would be real, but a host could get it with one
line of system prompt. So `--task-test-rule` tells every arm to run the tests. It is a sensitivity,
reported beside G2. If T1's gain disappears under it, the report has to say that the gain was this.

**`l3b` and the test rule.** `l3b`, at `b2ff6b3`: A0 .991, A3-8K .898, T1 .882, A4 .803, A4s .540,
A4-abrupt .218. T1 − A4 +.079 [.003, .159], T1 − A4s +.342 [.181, .500]. So the sentence took T1's
lead over A4 from .166 to .079. T1 − A3-8K −.016 [−.164, .127], and T1 − A0 −.109 [−.238, −.018].
`l3b-tests`, the same with the test rule: A3-8K .928, T1 .865, A4 .819, A4s .449, and T1 − A4 +.046
[−.049, .138]. The rule moved no arm by more than the noise, except that A4s fell, so T1's lead was
not coaching by example. It raised T1's and A3-8K's cost per run by more than half, and it stays off.

**A stale write.** A4s had 18 build failures in 45 runs and A4 had 6, and the two differ only in where
the brief sits. Reading `diag-a4s`, one A4s run per tuning task with a dump: local wrote a whole file
from what it remembered, without reading the file, and the cloud's new function was gone. The hidden
tests then failed to build. Counting whole-file writes over a version the writer had not seen:

| Arm | Runs | Such writes | Runs with any | Their mean score | The other runs' |
|---|---|---|---|---|---|
| A4s | 15 | 20 | 10 | .29 | .91 |
| A4 | 15 | 14 | 9 | .85 | .71 |
| T1 | 15 | 4 | 3 | .65 | .96 |

A4 and T1 here are `diag-s2`, from before the sentence. These counts come from one run each and are
for finding the mechanism, not for inference. T1's log shows local that the cloud wrote the file, so
T1 may have been winning partly by not doing this. A real host does not need a log for that: refusing
the write is a few lines, and Claude Code's tools do it. So from `l3c` on every arm goes through
`s2.View`, and the `write_file` description says to read first. This helps the arms that lost work,
and a baseline should get it. It is a change made after seeing tuning results, as the sentence was,
and it is frozen before any held-out task runs. `l3c` runs A3-8K, A4, A4s and T1 and is L3 from now
on, and `l3` and `l3b` stay on record.

The finding belongs in the plan whatever `l3c` shows: agents that share a workspace need this check.

**The event list on S2.** In `l3`, T1+read made 0 reads in 45 runs, A4+read made 1, and T1+read
scored .071 below T1, CI [-.177, .008]. The workspace is there to read, so local never reached for
the list, and the tool and its rules only took room in an 8K window. These arms are not run again
in `l3b`, so G2 with the event list reads INSUFFICIENT there and `l3` is its record.

**Scale, from `l3`.** T1 − A4 has an SD of .254 across task means, and most of that is noise between
repeats: the SD within a task is .41 per repeat, which leaves about .09 between tasks. So repeats
buy more than tasks here. Acceptance uses 30 held-out tasks with 5 repeats, which gives an SD of about
.21 and a standard error of about .038, so 80% power for a true gain of about .105. The formula's 349
tasks for .05 are out of proportion, so a true gain near .05 will not be confirmed, and the report
has to say so if the CI crosses 0.

**Held-out tasks.** Three authors wrote 10 tasks each in `s2.Acceptance()`, on separate branches and
separate domains, with read access to the task format and its tests only. None of them saw the harness
or a result. One reported seeing, in an index loaded automatically, a single line on the design
direction. `TestHiddenNamesAreStated` now type-checks every task's hidden tests against its solution
and requires each fixture name they use to be stated in a turn or an initial file.

**A screen before the freeze.** A0 runs each held-out task once, on the cloud alone. A task changes
only if a correct implementation that follows its turns would fail it, and any change is stated here.
No other arm runs on these tasks until `s2.AcceptanceHash()` is frozen below. The screen's scores do
not enter a gate.

Two runs of the screen gave A0 .975 and .969, with no build failure. Three tasks failed the same tests
in both, and each was changed:

- `s2-b02`: `TestPruneOldestFirst` accepted only pruning by generation number. With gaps in the
  generations, pruning by count is just as correct and kept everything. It now uses a Keep where the
  two agree.
- `s2-b06`: the turn adding `Has` said nothing about what it means, right after a sentence on anonymous
  traffic, and both runs wrote it as whether anonymous users get the feature. The turn now says.
- `s2-c10`: both runs read "a trailing 's" as a trailing s. Strictly, a correct implementation would
  pass this one, so this goes beyond the rule above. It is a wording fix with no compared arm seen, and
  it makes the turn say the same thing more plainly.

The failures that appeared in only one run were left alone: the other run shows an implementation
that passes. The set is now frozen at the `s2.AcceptanceHash()` in the table at the top.

**`l3c`.** At `f7fea8f`, with the write check, on the tuning tasks: T1 .915, A3-8K .876, A4 .789, A4s
.672. T1 − A4 +.126 [.052, .226], T1 − A4s +.243 [.122, .374], and T1 − A3-8K +.039 [−.071, .192]. The check raised A4s and T1 and did
not raise A4 (.803 before). T1 made one write the check refused, A4 19 and A4s 41, so T1's log
does keep local's view of the files current, and the check does not close the rest of the gap. T1 − A4
went .166, .079, .126 across the three runs, and the rule moved it less than the runs differ, so
these runs do not say how large the true gain is. That is what acceptance is for.

Failures still read in the traces: local writes `func Backoff(attempt int) time.Duration` where the
turn says `int`, and the visible tests still pass, so nothing tells it. That scores 0 for the task
whichever arm does it.

**Review before acceptance.** A fresh reviewer, with no sight of results, read these changes. Its verdict
was fit to run, and these fixes went in first. The A4 rule said a brief was "above" when it never is, which
favored T1, so it now says "if you have". T1's log recorded a refused write as done, so a refused write now
leaves no entry. T1's tool summaries lost the file name for any write over about 190 characters, because
JSON sorts `content` before `path` and the entry is clipped, so the path now comes first. This last one
favors T1. It is a plain defect, and it was fixed before T1 ran on any held-out task. `l3c` ran without
it, so the first numbers for T1 in this form are the held-out ones. Refusals are now counted by side.

**Acceptance.** `s2acceptance`, A0, A3-8K, A4, A4s and T1, `--repeat 5`, on the harness at the commit
that freezes this. With the SD inside a task that `l3c` shows (.31), 30 tasks with 5 repeats have 80%
power for a true T1 − A4 of about .07. A true gain smaller than that may not clear the CI, and the
report has to say so. G2's terms are unchanged. G3 and G4 read this run as previews for Stage 2.

**Unchanged.** `s2.Hash()`, the scorer, `MaxIters` 16, the local reply limit of 3,000, the budgets
and Stage 1.

**`l4-s2`: the held-out verdict.** At `c30ff2b`, `s2acceptance`, five arms, `--repeat 5`, 750 runs with
no error, $6.31. G2 passes, on its terms as they stood before any held-out task ran.

| Arm | Mean score | All pass | Build failed, of 150 | Cloud USD per run |
| --- | --- | --- | --- | --- |
| A0 | .989 | .860 | 0 | .0098 |
| A3-8K | .880 | .393 | 4 | .0115 |
| A4 | .764 | .213 | 17 | .0072 |
| A4s | .776 | .227 | 15 | .0064 |
| T1 | .880 | .413 | 5 | .0071 |

| Comparison | Task score | 95% CI | All pass | 95% CI |
| --- | --- | --- | --- | --- |
| T1 − A4 | +.116 | [.044, .202] | +.200 | [.093, .313] |
| T1 − A4s | +.103 | [.041, .179] | +.187 | [.073, .293] |
| T1 − A3-8K | −.001 | [−.032, .031] | +.020 | [−.087, .127] |
| T1 − A0 | −.110 | [−.146, −.075] | −.447 | [−.573, −.320] |
| A0 − A3-8K | +.109 | [.069, .154] | +.467 | [.320, .607] |

How much the verdict rests on:

- **One task.** `s2-a09` gives T1 +.96 over both, because all ten A4 and A4s runs failed to build. `l4-s2`
  has no dump, so the cause comes from one rerun (`diag-a09`, the first nine tasks, in no verdict). On
  A4s the early-bird rule is told on a local turn, local's brief leaves it out, cloud asks what the rule
  is instead of writing `Price`, and the missing function stops the hidden test file from building, so all
  19 tests score 0. A real user would have answered, so this is harsher than a session would be. A4 passed
  whole in the same rerun, so the loss is not certain on every run. Without this task: T1 − A4 +.087 [.031, .151]
  and T1 − A4s +.074 [.030, .115]. Leaving out any one task keeps the CI low at .030 or above.
- **Build failures.** A build failure scores every test 0, so one missing name costs a whole task. On
  built runs only, T1 − A4 is +.051 [.031, .073] and T1 − A4s +.051 [.029, .073], over 29 tasks. That
  comparison was chosen after seeing the result and is not a verdict.
- **The authors.** T1 − A4 by author: A +.173, B −.015, C +.190. T1 − A4s: A +.193, B +.040, C +.076. Author
  B's tasks show no gain over A4.
- **Power.** The .07 above used the SD inside a task. The SD of differences across tasks here is .228, so
  this run's 80% line was about .116, not .07. The estimate is about that size. A smaller true gain could
  have failed.

What it does not show:

- T1 does not beat one history at 8K on score. It costs .62 as much per run. These sessions are short, and
  on S1, where they run long, A0 − A3-8K is .40, so S2 does not test what T1's cloud keeps over an 8K
  history.
- G3 and G4 read this run as previews, and both fall short. T1 − A0 has a CI low of −.146 against G3's
  −.10. T1's cost against A0 is .725 per run, .815 per score point and 1.51 per run that passes whole, against
  G4's .70. T1 here switches where the script says, not where a router would, so neither is a verdict.
- A0 − A3-8K is above the 10-point line as a point estimate, so the problem S1 showed does show on tasks,
  but that comparison mixes local's window with local's ability. Amendment 4 separates them.

Refused writes: A4 57, A4s 87, T1 6, A3-8K 0, which agrees with `l3c`.

## Amendment 4

Written after `l4-s2`, before any A3-32K row on S2.

**A3-32K on S2.** It is A3-8K on a 32K server, so the window that drives compression is out of the way, and
what is left between it and A0 is local's ability and the change of model between turns. It runs on
`s2acceptance` with `--repeat 5`, as recorded rows in no gate. It comes from a later commit than `l4-s2`,
which adds the arm to `TaskArms` and these rows to the report, and changes nothing the other arms run.

The reading, stated before the run:

- If A0 − A3-32K stays near A0 − A3-8K (.109), the loss is local's ability. No context mechanism closes that
  and only routing can, so G3 cannot be met by more work on the thread.
- If A3-32K comes near A0, the loss is the window, and T1's gap to A0 is context T1 fails to deliver, which
  more work on the thread could close.
- In between, it is both, and the two differences give the shares.

**A3-32K on S1.** The plan's recorded row, on the pilot, while the server is at 32K.

**A2 on S2.** The plan left A2 out of this round. It is the only hybrid the repository has today, so T1
has no comparison with current practice without it. It is the real `ConnectivityAwareModel` with the
8K window a host has to declare, since `ResolveContextSize` takes the smaller member and a fallback can land
on local. The plan says A2 equals A0 while the cloud is up. That ignores the window: the cloud compresses at
8K too, so this run measures it. `s2acceptance`, `--repeat 5`, recorded rows in no gate, written before
any A2 row.

## Amendment 5

Written before any A8 row and before any route exists.

**Why.** The comparison with current practice left out how the repository routes by content, which is a
classifier on the query in `managedtask.Router`, with a per-candidate window check before dispatch.
`managedtask` has no tools, turns or sessions and cannot run S2 as it stands. So this is the closest
thing that can run: that decision and that check, in front of an Agent.

**Routes.** `stage0 route-tasks` asks a classifier about each turn of `s2acceptance` from the query alone,
once, and writes `routes.json` once. The question states what each side costs and can do, and no rule from
the set. A local choice at θ = 0.8 or more goes local, and everything else goes to the cloud, including low
confidence and failed calls, which is the explicit default a managed route needs. 0.8 is the value the
repository's offline example uses, not a calibrated one. Hosted Jev has no key here, so the classifier is
OpenAI Decisions, as a stand-in, and the report has to say so. Every routed arm follows the same file.

**Arms, on `s2acceptance`.**

| Name | Routes | History | Reads as |
|---|---|---|---|
| A8 scripted (`a8-s2`, `--repeat 5`) | The set's own | Cloud's window, local only when it fits | The hard limit alone, at T1's switch points in `l4-s2` |
| A8 routed (`routed-s2`, `--repeat 3`) | The classifier's | Cloud's window, local only when it fits | Current practice, form 1: what does not fit goes to the cloud |
| A3-8K routed (`routed-s2`) | The classifier's | One history compressed at 8K | Current practice, form 2: the window is the smallest member's |
| T1 routed (`routed-s2`) | The classifier's | Main Thread | The candidate under the same routes |

A8's limit is 8,192 less the 3,000 local may reply with, checked before each local call, and a call over it
goes to the cloud. T1 is the same arm as in `l4-s2`, with no state in its router, so the only difference
from the two forms is the context mechanism. A0 is `l4-s2`'s, since it ignores owners. A share of local
calls comes with every row, because cost and score both move with it.

**Reading.** None of this enters a gate or changes G2's verdict. It answers how T1 compares with what exists
now, on score and cloud cost together. If the classifier sends so few turns local that T1 hardly runs, the
routed row says little, and the report has to say that instead of drawing a conclusion.

## Amendment 6

Decided by the user on 2026-11-07, after `l4-s2`, after the previews of G3 and G4 fell short, and after the A2 and A3-32K rows, with the A8 and routed rows still running. This file keeps that order of events. By the user's decision, the final report states the final terms and explains them, without this history. It shows the numbers against A0 anyway, and does not describe these terms as fixed before the results.

- **A0 no longer gates.** G3's and G4's lines against A0 were proposed values with no measured basis. On a device that has to use a local model, A0 is not a choice anyone is making. It stays as the reference at the top of each table.
- **Cost is cloud cost.** Electricity and depreciation depend on assumptions this work cannot support, so G4's second costing is dropped. The calibration in `runs/calib` stays as a raw record.
- **Latency is recorded and does not gate.** It swings with the network: the cloud probe in `runs/calib-cloud` gave a first token at P50 3.94 s and P95 34 s over 40 calls, with no relation to prompt size. A server-side cloud agent would change it. That form is not part of this work.
- **Proposed exit for Stage 2, not yet confirmed.** G0 stays hard. On task score and cloud cost, no scheme that can run with this local window is better on one with a CI clear of 0 and no worse on the other. No other threshold is set.
- **Stage 2's scope.** Compression is background fork only, with no synchronous step first. The agent's existing compression stays only as the fallback for a failed fork or a window about to overflow, and for local, which has no fork. Hybrid comes first, and the other two modes depend on results. Background concurrency moves from Stage 4 into Stage 2, by the user's decision, so Stage 2 also takes on `Assign`, the shared ledger, confirmation routing and resource leases. There is still one foreground owner.
- **Window.** The local window stays at 8K, with no retest at other sizes beyond the A3-32K rows.

## Amendment 7

Written after the A2, A3-32K, A8 and routed rows, while the first replay below was running, before any of its other variants, and before the S1 rows below.

**Rows so far on `s2acceptance`.** Score, whole-task pass, cloud $ per run, median cloud $ per run:

| Arm | n | Score | Whole pass | Mean $ | Median $ | Local calls |
|---|---|---|---|---|---|---|
| A0 | 150 | .989 | .860 | .0098 | .0086 | 0% |
| A2 | 150 | .956 | .713 | .0129 | .0109 | 0% |
| A3-32K | 150 | .910 | .507 | .0082 | .0047 | 48.4% |
| A3-8K | 150 | .880 | .393 | .0115 | .0058 | 49.6% |
| A4 | 150 | .764 | .213 | .0072 | .0062 | 50% |
| A4s | 150 | .776 | .227 | .0064 | .0062 | 50% |
| A8 scripted | 150 | .952 | .713 | .0116 | .0067 | 30.6% |
| T1 | 150 | .880 | .413 | .0071 | .0058 | 49.2% |
| A8 routed | 90 | .877 | .600 | .0253 | .0056 | 45.2% |
| A3-8K routed | 90 | .725 | .233 | .0163 | .0046 | 67.7% |
| T1 routed | 90 | .764 | .200 | .0036 | .0036 | 68.4% |

The classifier chose local on all 238 turns, and 65 fell below θ and went to the cloud by default, so the routes hold 73% local against the script's 50%. It does not separate hard queries, so these rows measure routing by a query-only classifier that always says local. Hosted Jev has no key here and stays unmeasured. `a8-s2` and `routed-s2` carry `b83a494…-dirty` in their manifests, from one untracked `l1/__pycache__/bench.cpython-312.pyc`. The Go binary came from `fa05b5a`, and the routes file's hash starts `57894dd72904`.

**Runaway replies, found while reading these rows.** Some cloud calls reply with the full 128,000 output tokens the model allows, status `incomplete`, about $0.064 each. The harness sets no `max_output_tokens` on cloud main calls, as the repository's default does not. By scheme, runaways over cloud main calls:

| History the cloud is sent | Arms | Runaways | Calls | Rate |
|---|---|---|---|---|
| Holds local's tool calls | A3-32K, A3-8K, A8, A8 routed, A3-8K routed | 47 | 11,163 | .42% |
| Does not | A0, A2, A4, A4s, T1, T1 routed | 2 | 19,374 | .01% |

Most fall on the cloud's first write after local has worked. The one in a dump (`l3c`, `s2-14-event-timeline`) is a single `write_file` of 410,668 characters: a sound file, then the same code repeated with junk between. Runs mostly recover, because a truncated call gets a parse error and the next call is ordinary, so the cost is the main damage. But the truncated reply stays in the history: in A8 routed the next prompts reach about 133,000 and 262,000 tokens, every later local call spills to the cloud, and further runaways follow. So the mean cost of the single-history arms is partly this effect, and the means are not comparable until it is explained. Mean and median are both reported from here on.

**The likely difference.** The cloud writes `{"content":…,"path":…}`, compact, in every arm. Local writes `{"path": …, "content": …}` with spaces, in most calls, so the cloud sees calls that close straight after the content while its own habit leaves `path` still to write. This is a guess to test.

**Replay.** `runs/replay-runaway` re-sends real requests from `l3c` A3-8K and `s2a-screen2` A0 with `max_output_tokens` 8,000, far above any ordinary reply in any ledger, and counts replies stopped there. Prompts are chosen by shape with a fixed seed: the cloud's first write after local (no cloud write in the request yet, at least two of local's) and, for A0, a write with at least three of its own. The one prompt known to have run away is reported apart, because it was chosen by its outcome.

| Variant | What changes | Reads as |
|---|---|---|
| exact | Nothing | The rate from a request as sent |
| ids | Local's `chatcmpl-tool-…` call ids renamed `call_…` | The id format |
| cloud_style | Local's arguments rewritten compact, keys sorted, content first | The argument form, and a possible fix |
| no_local_calls | Local's calls and their results removed | Whether local's calls are the cause at all |

Variants run shuffled together. If `exact` alone gives too few runaways to compare, the variants are not run on that basis, and the finding stays a correlation across schemes. Either way the correlation stands as measured, and it does not settle whether the effect would appear with other models.

**S1.** T1, A4 and A4s on the S1 pilot at the current code, in `runs/l4-s1`, with their clones. `l2-g2` holds older rows from code that has since changed. This is the open condition on T1 for long sessions: on S1 A0 is .998, A3-32K .893, A3-8K .594, and T1 has no current row. It is a diagnostic with no verdict, read by fact category, as Amendment 3 set out for S1.

## Amendment 8

Written after the replay of Amendment 7, before any row with its fix, and with `l4-s1` still running.

**The replay's result.** 11 prompts on the first-write shape, 30 replays each, shuffled together, stopped at 8,000 output tokens. The largest ordinary reply in the whole replay was 7,171.

| Variant | Runaways | Calls | Rate | Against exact, by prompt |
|---|---|---|---|---|
| exact | 27 | 330 | 8.18% | |
| ids | 21 | 329 | 6.38% | −1.8 points [−6.4, +3.1] |
| cloud_style | 0 | 330 | 0% | −8.2 points [−13.6, −3.6] |
| no_local_calls | 2 | 330 | .61% | −7.6 points [−12.4, −3.3] |

An earlier pass of the same kind gave 22 of 330 on those prompts, and 0 of 300 on ten all-cloud prompts with at least three of the cloud's own writes. Every runaway is a `write_file`. So the cause is the form in which local's arguments are replayed, not the id format and not the presence of local's work in the history. This is on `gpt-6-luna` and one local model, and it does not establish the same for other pairs.

Two more variants, also 11 prompts by 30, split the form: `compact_only` removes the spaces and keeps local's key order, and `sorted_spaced` sorts the keys and keeps the spaces. They only describe the mechanism, and nothing below depends on them. Added after they ran: `exact` 21 of 330, `compact_only` 23 of 329, `sorted_spaced` 0 of 330, a difference of −6.4 points [−9.7, −3.0] against `exact`. So the cause is key order, and spacing does nothing. The prompts agree: in every pass, every runaway came on the eight prompts where local wrote `path` first, and the three where local wrote `content` first, as the cloud does, gave none at all. The cloud always writes `content` first, so it reads calls that close straight after the content while it still has `path` to write.

**What this does to the earlier reading.** The single-history arms' mean cost in Amendment 7 is inflated by a defect that one normalization removes, so "T1 is much cheaper" cannot stand on those means. Their runs without a runaway give a first estimate, with the selection that implies:

| Arm | Score | Mean $ | Mean $ without runaway runs |
|---|---|---|---|
| A3-8K | .880 | .0115 | .0069 |
| A3-32K | .910 | .0082 | .0055 |
| A8 scripted | .952 | .0116 | .0075 |
| A8 routed | .877 | .0253 | .0056 |
| A3-8K routed | .725 | .0163 | .0047 |
| T1 | .880 | .0071 | .0064 |
| T1 routed | .764 | .0036 | .0036 |

**Rerun.** `stage0 run --canonical-replay` rewrites local's arguments at the transport, as `cloud_style` did, and leaves the cloud's own calls alone. `fix-s2` runs A8 and A3-8K with `--repeat 5`, and `fix-routed` runs A8 and A3-8K on the same routes file with `--repeat 3`, all on `s2acceptance`. T1, A4, A4s, A0 and A2 send the cloud no local calls, so the flag changes nothing there, and their rows stand. A3-32K would need the 32K server, which the user's decision on the window does not call for, so its figure stays the estimate above.

**Reading.** These rows replace the earlier ones as the comparison of T1 with single-history schemes. The earlier ones stay as what the repository does today without the fix. The exit rule of Amendment 6 is read on the fixed rows. Single-history schemes send the cloud everything local saw: on S1 canary hits per session are A3-8K 81, A3-32K 225, A0 690, and T1 0 in `l2-g2`. Where private data must stay on the device, that is a separate failure of a hard constraint which no cost figure offsets, and S2 cannot show it, since it holds no private data.

## Amendment 9

Written after `l4-s1` and before any long task exists, by a question from the user: what does T1 deliver on long sessions, given what S1 says about facts?

**What is known.** `l4-s1`, at the current code, S1 pilot, retention and cloud $ per session: T1 .644, $.0367; A4 .681, $.0249; A4s .702, $.0262; A3-8K .594, $.0233; A3-32K .893, $.0198; A0 .998, $.0223. T1 − A4 is −3.6 points [−6.8, −.6] and T1 − A4s is −5.7 [−8.9, −2.8]. Canary hits over the 30 sessions: T1 0, A4 6,985, A4s 6,045. Fork B is 59% of T1's cost there and 17% on S2, because S1's main replies average 26 tokens and every fork is sent back for length about once. S1 has no deliverable, so none of this says how well a long session's work turns out.

Splitting `l4-s2` by A0's peak prompt, a split made after seeing those rows, ten tasks to a band (peak about 10K, 15K and 22K):

| T1 against | Short | Middle | Long |
|---|---|---|---|
| A4 | −.7 [−7.9, +5.4] | +17.3 [+4.5, +36.4] | +18.3 [+7.0, +31.8] |
| A4s | +6.0 [−2.5, +14.1] | +18.4 [+6.6, +36.9] | +6.5 [−1.9, +13.9] |
| A3-8K, before the fix | −3.9 [−8.6, +.3] | +3.6 [−.9, +9.1] | .0 [−5.6, +5.9] |
| A8, before the fix | −8.4 [−14.1, −2.9] | −3.0 [−8.5, +1.6] | −10.3 [−17.4, −2.8] |

**The long set.** `s2.Long()`, set `s2long`: 12 tasks by three authors, four each, written blind to the harness, `prereg.md`, the runs and every result, in the format of S2. 20 to 32 turns, at least 8 switches and 8 turns a side, with stretches of three or more turns on one side. The finished module holds 18,000 to 36,000 characters, no file over 4,500, so one file still fits an 8K window with room to work, and whole-file writes make the session grow. At least 6 carried constraints and 20 hidden tests, and at least half the carries are needed on the other side 8 or more turns after they were settled. Rules can change mid-session, and the current one is what counts. `TestLongTaskShape` and the existing rules on tasks check all of this, including that forgetting each carry fails its tests. The hash is frozen before any model sees a task.

**Run.** `long-s2`: A0, A4, A3-8K, A8 and T1, `--repeat 2`, `--canonical-replay`, at the set's own switch points, on the 8K server. The flag changes nothing for A0, A4 and T1, which send the cloud no local calls. About $10, an estimate that assumes A0 peaks near 60K. Length is checked first by A0's peak prompt on these same rows, so a set that turns out too short is reported as such and not extended after seeing scores.

**Reading.** No gate. It answers the user's question: task score and whole-task pass on long sessions, with cloud cost, mean and median. The exit rule of Amendment 6 is read here too, on the schemes this window can run. n is 12 tasks, so only a large difference can clear its CI, and a difference that does not clear it is not evidence of no difference.

**Decided by the user on 2026-11-07, while this was being written: fact retention is no longer read, and only the final deliverable counts.** What follows from that:

- Retention no longer decides or supports anything, in any gate or report. S1 and CBench rows stay on file, and the only thing still read from them is the canary count, because privacy is a hard constraint and that count is not retention. S1's costs are not read either, since its replies are not real work.
- `l5-c` (CBench acceptance for G6) and `l5-g1` (S1 acceptance for G1) were running and were stopped at 41 and 17 rows, $.91 and $.39. They are not read.
- G1 is read on task score: A0 − A3-8K on `s2acceptance` is 10.9 points, and the long set adds to it.
- G6 has no reading left. Its gain was in retention. Background fork is the user's decision, and the choices it was meant to settle (merged requests, published-only, a small model) keep the plan's defaults and stand as not validated on deliverables.
- Clearing local's context at a takeover and the event list are the two design choices that can change a deliverable, so the long set reads them on deliverables: T1+read joins `long-s2` as a whole arm, and `long-s2-keep` runs T1 alone with `--never-clear`. This adds about $4 to the estimate.
- S3 scores a router by whether a side can answer a question from the facts it holds, which is the same kind of measure, so it is not run and G7 is not read on it. Routing is judged by what a route does to a deliverable, as `routed-s2` does.

## Amendment 10

Written after `fix-routed` and `fix-s2b`, and before any long task has run. Rows are on `s2acceptance`, with local's arguments replayed in the cloud's form where the arm sends them. A0, A2, T1, A4 and A4s are unchanged, since they send the cloud no local calls.

`fix-s2` stalled at 145 of 300 rows, when an interrupt meant for the two stopped runs reached it. It was stopped by process ID and completed as `fix-s2b` at `e4bca1d`, with the finished rows carried over. The commits differ only in code for the long set. Scores come from `fix-s2b` and costs from both ledgers, without the rows of the ten sessions left unfinished, which ran again in full. From now on a run is stopped by process ID, never by stopping a script that shares its console.

| Arm | n | Score | Whole pass | Mean $ | Median $ | Local calls | Runaways |
|---|---|---|---|---|---|---|---|
| A0 | 150 | .989 | .860 | .0098 | .0086 | 0% | 0 |
| A2 | 150 | .956 | .713 | .0129 | .0109 | 0% | 0 |
| A8 fixed | 150 | .971 | .753 | .0069 | .0061 | 30.6% | 0 |
| A3-8K fixed | 150 | .875 | .373 | .0070 | .0057 | 50.3% | 0 |
| T1 | 150 | .880 | .413 | .0071 | .0058 | 49.2% | 1 |
| A4 | 150 | .764 | .213 | .0072 | .0062 | 50% | 1 |
| A4s | 150 | .776 | .227 | .0064 | .0062 | 50% | 0 |
| A8 routed fixed | 90 | .847 | .589 | .0051 | .0049 | 48.0% | 0 |
| A3-8K routed fixed | 90 | .756 | .256 | .0043 | .0039 | 69.5% | 0 |
| T1 routed | 90 | .764 | .200 | .0036 | .0036 | 68.4% | 0 |

Paired by task, score then whole pass in points, and the cost ratio:

| Comparison | Score | Whole pass | Cost ratio |
|---|---|---|---|
| T1 − A8 fixed | −9.1 [−12.8, −5.5] | −34.0 [−47.3, −22.0] | 1.02 [.87, 1.27] |
| T1 − A3-8K fixed | +.4 [−2.9, +3.8] | +4.0 [−4.7, +13.3] | 1.01 [.84, 1.25] |
| T1 − A4 | +11.6 [+4.5, +20.4] | +20.0 [+9.3, +31.3] | .99 [.79, 1.25] |
| T1 − A4s | +10.3 [+4.1, +18.1] | +18.7 [+7.3, +29.3] | 1.10 [.92, 1.37] |
| A8 fixed − A0 | −1.9 [−3.4, −.5] | −10.7 [−21.3, +.7] | .71 [.64, .78] |
| T1 routed − A8 routed fixed | −8.3 [−20.4, +5.0] | −38.9 [−54.4, −22.2] | .71 [.54, .87] |
| T1 routed − A3-8K routed fixed | +.8 [−7.7, +9.8] | −5.6 [−13.3, +2.2] | .84 [.73, .95] |

**Reading.** On these tasks T1 fails the exit rule of Amendment 6 against A8 fixed: A8 is better on score with a CI clear of 0 and no worse on cost. The earlier view that T1 was the best scheme for an edge deployment rested on means inflated by the runaways, so it does not stand on S2. G2's verdict against A4 and A4s stands, and it does not make T1 better than the other single-history schemes. T1 equals A3-8K on score, whole pass and cost at the same share of local calls.

Score follows the share of local calls more closely than the context mechanism does, with A4 and A4s as the exception: 0%, 31%, 50% and 69% give about .99, .97, .88 and .76. A8 reaches its 31% by sending a call to the cloud once the history no longer fits, so local does the early turns and the cloud does the later ones with everything in view. That is why it is not a fair comparison at equal local work.

What is left for T1 is not shown by these rows. On long sessions A8's local share should fall and its cost should move toward A0's, which is a prediction for the long set to test. A single history also sends the cloud all that local saw, which S1's canary counts show and S2 cannot, and it has no way to serve an over-window turn with the cloud down, which follows from its definition and was not measured on deliverables. If T1 does not hold up on the long set, it is worth building only where privacy or offline use is required, and that is a change of direction to take to the user.

## Amendment 11

Written before any model has seen a long task.

**The long set is frozen** at `ab80952089cc26d136357d8517f01c7e12c9b6fb07a5f16f1c46789860d6dcbc`: 12 tasks, four from each of three authors, 27 to 32 turns, 10 to 18 switches, 13 to 17 cloud turns and 13 to 18 local turns. They all pass the rule tests in `s2_test.go`, including that forgetting a carry fails its tests.

**What the authors could see.** They were kept away from the harness, these notes, the runs and the results, with three leaks, which are all small and none of which shows how a task is scored:

- The startup git status put the five newest commit subjects in front of each of them, which name arms such as A8 and mention classifier routes.
- Their worktrees were cut from `main`, so each fast-forwarded to reach `s2/`. That brought the forbidden directories into the worktree, and each reports not opening them.
- One ran `TestWorkloadSummary` unfiltered and saw some carry descriptions from other tasks in the package.

No model has run these tasks, so what the rule tests cannot catch, such as a hidden test that a turn does not determine, would show up here for the first time. A hidden test that every arm fails is that kind of defect, and it is reported and not dropped.

**Runs.** On the 8K server, all with `--canonical-replay` and `--repeat 2`:

- `long-s2`: A0, A4, A3-8K, A8, T1, T1+read, at the set's own switch points.
- `long-s2-keep`: T1 with `--never-clear`.
- `long-routed`: A8, A3-8K, T1, on routes the OpenAI Decisions stand-in wrote for these tasks at theta .8 in `routes-long`. This is new since Amendment 9. It is the closest runnable form of today's online path on long sessions, and it reads routing on deliverables, which S3 no longer does. It adds about $3. Hosted Jev and Piccolo stay unmeasured.

## Amendment 12

Written before any long task has a score.

**A8 is not a candidate.** The user's decision on 2026-11-07: it has no production value, because once a history passes the window, nearly every later call goes to the cloud, and no one would build that for an edge deployment. It was not requested: the user asked that the online Jev and the context-window hard limit be taken into account, and the maintainer built A8 from that. Calling it today's online path overstated it: `managedtask.Router` and `tierrouter.ChatRouter` refuse tools, so no shipping path does this on a task with tools. $5.75 went to it before it was stopped.

This withdraws the reading in Amendment 10 that T1 fails the exit rule against A8. The rows stay above as a record. A8 scores high for the same reason A0 does, by doing more of the work in the cloud, and A0 does not gate either. A8 and its routed form are out of the comparisons and of every report.

**Runs.** The long runs of Amendment 11 change as follows. A8 leaves them, and they are renamed because they were started twice before a row was scored:

- `long-s2` and `long-routed` were stopped by process ID to remove A8, then `long-s2b`, `long-s2-keep` and `long-routedb` to move their Go cache. None has a score. Their ledgers count toward spend and nothing else.
- `long-main`: A0, A4, A3-8K, T1, T1+read. `long-keep`: T1 with `--never-clear`. `long-route`: A3-8K, T1, on `routes-long`.

**Go cache.** Each agent's test call and each score runs `go test` in a new scratch directory, through `goTest` in `s2/workspace.go`, and the runs left about 14 GiB in the default cache with 8 GiB free on the system drive. These runs set `GOCACHE` to `runs/.gocache` on another drive, which `goEnv` passes through. Nothing else about them changes.

## Amendment 13

Written after the fault below was found, and before any row from the reruns exists.

**The fault.** The application that launched the runs restarted at 07:03:17 UTC. `long-main`, `long-keep`, `long-route` and `diag-long-build` kept running, but from about then on every row came back as a build failure with no compile output, 56 rows on all arms in a row, A0 included, which had none before. The scorer read a `go test` that printed nothing as a build failure, so each scored zero. Polling for 15 seconds caught no `go.exe` under those runs, against 21 sightings in 25 seconds before, so the go command seems to die at once. Why is not established. A likely cause is that the console those processes belonged to went away with the app. It was found by reading `diag-long-build`, whose rows held no compile errors, and A0 failing on tasks in a pattern set by time and not by task.

**Rows.** A row counts only if its session's last call came before 07:03:00 UTC. That keeps 57 rows: 37 in `long-main`, 7 in `long-keep`, 13 in `long-route`, carried into `long-main2`, `long-keep2` and `long-route2`, which run the remaining sessions. The rest are void, and so is `diag-long-build`. Costs of carried rows come from the old ledgers, and void spend, $6.93, counts toward the total only.

**Scorer.** A build failure with no output is now the scorer's own error, an `infra` row that says nothing about the arm, and it is not read. A real compile error always prints. New rows keep the compile errors, so whether a task failed because its code is broken or because a hidden test uses a name the code lacks can be read from them. Carried rows do not have them. The hidden tests, their weights and the frozen hashes do not change.

**Runs.** Started with a hidden console of their own, so they do not depend on the app's. A watcher stops them after three `infra` rows.

## Amendment 14

Decided by the user on 2026-11-07, while the long runs were going and before any of the below ran: once they finish, the sessions that ended with a compile error are continued, and the results are organized separately.

**What continuing means here.** `execTask` deletes a session's workspace and context when it is scored, so a finished session cannot be picked up where it stopped. Each such pair runs again, the same arm, task and repeat, with `--repair-rounds 3` and `--only`, in `repair-main`, `repair-keep` and `repair-route`. If a session does not compile against the hidden tests when its scripted turns end, the compiler's errors go to the owner of the last turn as one more turn, and the task is scored again, at most three times. Compile errors are the only trigger, so a session that compiles and fails tests gets no round.

**What the model sees.** The compiler's lines, with the position in the hidden file replaced by "acceptance check". They name what the code lacks, and `TestHiddenNamesAreStated` holds that the turns already state those names. No hidden test body reaches the model.

**Reading, three ways, kept apart:**

1. As scored: every row of `long-main2`, `long-keep2` and `long-route2`, with no repair. This stays the primary reading.
2. What a repair does: among rerun sessions that failed to compile at the first score, the score before and after, and how many then compile.
3. With repair: rows that compiled as they were, plus the rerun's final score for rows that did not.

The third is not a clean measure. A rerun is a second try, so it can compile with no repair at all, and a scheme with more failures gets more second tries. Those are counted and reported, and the third reading is not used without the second beside it. The cost of the rounds is read from ledger rows after `first.at`. The reruns' own cost is reported apart, and counts toward no scheme's cost in the first reading.

## Amendment 15

Written after the first reruns of Amendment 14, before the rest of them ran.

**The local server died** at 10:04:33 UTC with `CUDA error: unknown error`. Fourteen seconds earlier Windows logged low virtual memory, with a `fixture.test.exe` at 10.7 GB and the Linux VM running vLLM at 27.8 GB, and two more test binaries reached 45 GB and 58 GB in the next four minutes. A test binary is code a model wrote, built by `go test` in a session's workspace, and nothing capped its memory. The user spotted the 15 GB process.

**Rows.** 47 of the 101 rerun sessions ended on a session error, all on the dead server. They are void. The 54 that finished before it died stand. The second reruns of those 47, `repair-main2`, `repair-keep2` and `repair-route2`, were stopped by process ID before any row was scored and are void as well. They run again as `repair-main3`, `repair-keep3` and `repair-route3`.

**The cap.** Every `go test` the harness runs, for an agent's test tool and for scoring, now runs in a Windows job object: 2 GiB a process, 4 GiB for the command and all it starts, and everything in it killed when the command ends. A binary over the cap fails with "fatal error: out of memory", which prints, so it reads as the code failing. Without the cap such a test failed on its timeout instead, so a row's score does not depend on which of the two stopped it, and the earlier rows stand.

## Amendment 16

The long set's results, read the three ways of Amendment 14. 24 rows a scheme, 12 tasks with two repeats, paired by task, 95% bootstrap CIs. Score is the mean hidden-test score; cost is cloud cost a session.

**1. As scored** (`long-main2`, `long-keep2`, `long-route2`, with the rows carried from before the fault; no `infra` row and no session error):

| Arm | Score | No compile | Score when it compiled | Mean $ | Local calls |
|---|---|---|---|---|---|
| A0 | .966 | 0 | .966 | .1159 | 0% |
| T1 | .527 | 10 | .903 | .1100 | 54.4% |
| T1+read | .560 | 9 | .897 | .1225 | 55.8% |
| T1 `--never-clear` | .520 | 10 | .891 | .1146 | 54.9% |
| A4 | .300 | 15 | .799 | .0911 | 56.1% |
| A3-8K | .119 | 20 | .714 | .1042 | 48.2% |
| T1 routed | .220 | 18 | .879 | .0356 | 85.7% |
| A3-8K routed | .100 | 19 | .479 | .0616 | 80.8% |

| Comparison | Score | Cost ratio |
|---|---|---|
| T1 − A3-8K | +40.8 [+13.5, +66.4] | 1.06 [.88, 1.27] |
| T1 − A4 | +22.7 [−2.7, +46.1] | 1.21 [1.07, 1.40] |
| T1 − A0 | −43.9 [−66.6, −22.0] | .95 [.75, 1.21] |
| T1+read − T1 | +3.3 [−18.6, +27.2] | 1.11 [.93, 1.32] |
| T1 never-clear − T1 | −.7 [−19.8, +18.5] | 1.04 [.90, 1.19] |
| T1 routed − A3-8K routed | +12.0 [−8.2, +33.2] | .58 [.46, .72] |
| T1 routed − T1 | −30.7 [−57.9, −.4] | .32 [.23, .44] |

Whole-task pass is .375 for A0 and 0 for every other arm, so it is not read. Five hidden tests in three tasks were passed by no row. An audit against the turns found two of the five defective, both in c03: `TestNoResolvedWithoutNotice` contradicts the literal reading of turn 13, and `TestAddSilenceUnknownRule` checks a rule no turn states and conflicts with `TestParseSilenceBareNumberIsMinutes`. The other three (`TestWriteMultiLine`, `TestPagerTextIsCut`, `TestNextDelaySeesAStableRun`) are determined by the turns. The two cost every compiled c03 row the same two of 63 tests, about .3 points on a scheme's mean, and are kept.

New rows held their compile errors. About half were code that does not compile (a missing import, a name declared twice, an unused variable), and half code that compiles but lacks one to three names the turns asked for.

**2. What a repair round does.** Rerun sessions that did not compile when their scripted turns ended, and what up to three rounds of the compiler's errors to the last owner did:

| Arm | Reruns | Compiled with no round | Did not compile | Compiled after rounds | Score after rounds | Cloud $ of the rounds, each |
|---|---|---|---|---|---|---|
| A3-8K | 20 | 4 | 16 | 7 | .284 | .011 |
| T1 | 10 | 4 | 6 | 5 | .684 | .001 |
| T1+read | 9 | 6 | 3 | 2 | .565 | .000 |
| T1 never-clear | 10 | 6 | 4 | 2 | .375 | .001 |
| A4 | 15 | 6 | 9 | 4 | .342 | .002 |
| A3-8K routed | 19 | 1 | 18 | 6 | .195 | .008 |
| T1 routed | 18 | 3 | 15 | 3 | .160 | .000 |

29 of 71 compiled after the rounds. The last owner was local on these tasks, so the rounds cost the cloud almost nothing. A missing import or a duplicate name was usually fixed. A missing function mostly was not: local's window no longer held the turn that asked for it.

**3. With repair.** Rows that compiled keep their score, the rest take the rerun's final score:

| Arm | Score | Mean $ |
|---|---|---|
| A0 | .966 | .1159 |
| T1 | .848 | .1080 |
| T1+read | .820 | .1254 |
| T1 never-clear | .805 | .1085 |
| A4 | .612 | .0912 |
| A3-8K | .423 | .1108 |
| T1 routed | .416 | .0331 |
| A3-8K routed | .280 | .0684 |

| Comparison | Score | Cost ratio |
|---|---|---|
| T1 − A3-8K | +42.5 [+21.8, +60.8] | .97 [.76, 1.21] |
| T1 − A4 | +23.6 [+13.4, +34.5] | 1.18 [1.00, 1.43] |
| T1 − A0 | −11.8 [−20.0, −5.2] | .93 [.73, 1.16] |
| T1+read − T1 | −2.7 [−12.9, +8.3] | 1.16 [1.03, 1.32] |
| T1 never-clear − T1 | −4.3 [−17.7, +8.2] | 1.01 [.85, 1.15] |
| T1 routed − A3-8K routed | +13.6 [−12.0, +38.1] | .48 [.39, .60] |
| T1 routed − T1 | −43.2 [−65.5, −20.7] | .31 [.22, .42] |

This reading includes second tries: of the rerun sessions, 30 compiled with no round at all, and for T1 those scored .900. A scheme with more failures had more second tries, so it is read beside the second.

**Reading.** On long sessions T1 delivers more than A3-8K, with the CI clear in every reading and the cost the same. Against A4 the gain is +22.7 as scored with a CI that crosses 0, and +23.6 with a clear CI when compile errors are repaired, while T1 costs about a fifth more. On the short set T1 beat A4 by +11.6 [+4.5, +20.4] at the same cost, and tied A3-8K. T1 saved no cloud cost against A0 on long sessions (cost ratio about .95), against 28% on the short set, because the cloud side still carries the long history and Fork B adds about a quarter. The event list and keeping local's context at a takeover show no gain, so the plan's defaults stand: clear, and no event list. Routing on the stand-in classifier cut cost by about two thirds at a large loss of score.

**The exit rule of Amendment 6**, among the schemes this window can run that meet the hard constraints: by S1's canary counts, T1 is the only one that kept private data from the cloud (0 hits, against thousands for A3-8K and A4), so no other scheme qualifies. Leaving that constraint aside, A4 is cheaper with a clear CI but delivers less, clearly on the short set and on the long set once compile errors are repaired, so no scheme is better on one measure and no worse on the other, and T1 passes. The rule is still the proposal of Amendment 6 and not yet confirmed by the user.

## Stage 1: routing on S3

Written before any S3 result exists. S3 was frozen in `4bd4cc3`, written blind to the harness and
to the routers.

**What runs.** Each item runs once, in shadow: no router changes what either side does. The cloud
walks the item's history with live calls at the fork points, then answers the query. Local answers
it three times, once per world: `r5k` with the summary from the last 5K refresh, `r20k` with the
one from the last 20K refresh, and `none` with no summary. An edge_only query is logged, never
goes to the cloud or to a classifier, and counts as cloud-ineligible.

**Routers.**

| Router | Sees | Decides |
|---|---|---|
| R0 | Owner | Stays with the owner |
| R1 | Query text | `L-n` goes local, `R-n` goes cloud, otherwise the owner |
| R1s | Query and recent text | A literal rule that ignores the owner: `L-n` local, local when one recent line states a value or a revocation for the same kind and subject as the query, even if the query names an `R-n`, otherwise cloud. Recorded only: it agrees with every designed route on the pilot and on 180 acceptance items, which is what a literal rule can do on this corpus |
| R2 | Query | A classifier, once per item |
| R3 | Query plus session state: owner, the last 5 steps, participants, the world's summary as it is, how far behind it is | A classifier, once per item and world |
| R4 | Both answers | The oracle |

Every router comes after the hard rule: edge_only goes local. A classifier below its threshold, or
one that failed, stays with the owner.

**Classifiers.** They are a factor, and each gets its own G7. OpenAI Decisions runs now. Piccolo-4B
runs locally if it deploys beside vLLM. Hosted Jev runs with `--classifiers jev
--jev-model NAME` if `TYPESAFE_API_KEY` appears in `.env`. It has no price here, so its attempts show
$0. A report states any of them it did not measure. A call is tried up
to 3 times on a retryable error with a growing wait, each attempt charged.

**Utility and regret.** A right answer is worth 1. A cloud call costs λ = 0.25, with 0, 0.1, 0.5 and
1 as sensitivity. An ineligible cloud route is worth −λ, and no router can take one. The oracle takes
the better side, and local on a tie. Regret is the oracle's utility less the router's. Missed-answer
and needless-cloud rates are reported apart, by kind, with the switch rate, classifier latency and
state size. Scoring is `s3.Score`, as strict as its author wrote it. The report counts wrong answers
that still hold the answer, by side, and local-only text reaching the cloud or a classifier's state.

**G7.** Per classifier, in world `r5k`: R3's regret at least 20% below both R2's and R1's, and on an
acceptance set the 95% CI of each absolute difference above 0. A pilot is too small to clear that CI,
so there the verdict rests on the point estimate and the CI is printed. The bootstrap resamples
seeds, so a pair's two halves stay together. Fewer than 10 items, or more than 10% failed decisions on
either router in a comparison, over the eligible items, is INSUFFICIENT. A comparator with no regret to reduce fails the gate. The failure action is the plan's:
drop state injection and route on the query and rules alone.

**Thresholds.** R2 and R3 each get a θ per classifier, chosen on the pilot from {0.5, 0.6, 0.7, 0.8,
0.9, 0.95} for the lowest regret at λ = 0.25, a tie going to the higher. R3's comes from `r5k` and
applies to every world. They are frozen for acceptance with `--theta decisions/R2=…,decisions/R3=…`.
An acceptance report refuses a missing threshold, and any report refuses a name that matches nothing.
0.8 is the starting value on the pilot. The pilot report prints the grid; on an acceptance set it is
for reading only.

**Limits, stated now.**

- S3 restates labels word for word, so query-only routing tops out at 78.3% of the designed routes
  and a literal rule reaches 100%. S3 shows whether a router can use state, not how much state helps
  in real sessions. R1s is there to keep that visible.
- Strict scoring may understate a weak local model. The pilot checks local's false-negative rate
  before acceptance runs.
- Local keeps `LocalOwn` in every world and imports at most 2,000 tokens past its summary, so this is
  not T1's clear rule.
- Each world has at most two forks on one line of history, where a long session has many.
- R3's state holds shared data only, the same for every classifier. A locally deployed classifier
  could also see local notes, which the plan allows and this does not test.

**Cost.** About $0.8 for the pilot and $2.5 for 180 acceptance items, both against the cloud budget.

## Amendment 17: the final comparison on OpenAI Decisions

Written on 2026-11-08, before any run below, at the commit that adds it on the local branch `eval/final`.

**Question.** The user asked for the final, end-to-end comparison of today's practice on `main` with the shipped Main Thread and its hybrid routing, both on OpenAI Decisions with `gpt-6-luna`. Hosted Jev has no key here and Piccolo is not deployed. No public or repository evaluation of either as a router exists to stand in for them.

**Schemes.**

- **Master** is A3-8K on the frozen query-only routes: one agent, one history compressed at 8K, and per turn the owner the classifier chose from the query alone, local only at θ ≥ 0.8. `main`'s routers that take a classifier, `managedtask.Router` and `tierrouter.ChatRouter`, refuse tool calls, so no code on `main` routes a session with tools. This is the harness's arrangement of `main`'s parts, as in stage 0. The routes are `routes-decisions` for `s2acceptance` (sha256 `57894dd7…`, 173 of 238 turns local) and `routes-long` for `s2long` (sha256 `e30debf3…`, 301 of 361 local). Compression goes to the cloud, as in stage 0, which favors this scheme.
- **Hybrid** is `MT-hybrid`: `eval/final` is `experiments/stage0` merged with `feat/main-thread` at `98e2067`. A `thread.Coordinator` runs an edge and a cloud participant from package `participant`, and `route.Hybrid` picks the owner of every turn with Decisions over the thread state (`route.BuildState`, default budget). It uses θ 0.8. Below that, the query stays with the current owner, and the first one goes to the edge. The task's own owners are not used. The participants may transfer and assign background tasks on their own.
  - Edge: Qwen3.5-4B, 8K window, reserve 0.3, `ThreadSummaryTokens` 3,000, its own compression synchronous in its reply loop.
  - Cloud: `gpt-6-luna` on Responses, window 1,050,000.
  - Peer summary for the edge: target 2,500, refreshed every 5,000 tokens of cloud growth and at the default window share of 0.8, output limit 7,000.
  - The cloud's own notes keep their defaults (first at 10K, then every 5K).
  - A transfer brief to the edge may hold 8,000 runes. The event list is off.
  - Forks run at the API's default effort, because the code sets none. Stage 0's prototype used low.
  - The host prompt adds one note: the two participants share one workspace.

**Common to both.**

- Both run on the 8K server with `--repair-rounds 3`. After the scripted turns, a session that does not compile gets the compiler's errors as one more user message, at most three times. Master sends them to the owner of the last scripted turn. The hybrid sends them through its router.
- `--canonical-replay` is set. It reaches master only: the hybrid sends no local call to the cloud.
- The task test rule is off.

**Runs.**

- `final-short`: master and hybrid on `s2acceptance`, `--repeat 3`, `--routes` from `routes-decisions`, 90 rows each.
- `final-long`: the same on `s2long`, `--repeat 2`, `--routes` from `routes-long`, 24 rows each.
- The hard stop is $12 over the smoke runs and both runs.

**Smoke, before them, and not read.** The hybrid runs on the first `s2` tuning task twice. The first run uses Decisions. The second uses `--mt-route cloud`, so that the cloud path and its forks run. Tuning tasks are in neither set. Those rows only check that the arm works.

**Measures.**

- **Primary.**
  - Hidden-test score after the repair rounds.
  - Cloud cost per session, with classifier calls and repair rounds.
- **Master's classifier cost.** Its classifier calls were made once per task when the routes were written. A deployment makes them in every session, so each master session is charged its task's share of the routes ledgers: $0.0057 for the 238 short turns and $0.0091 for the 361 long ones in all.
- **Secondary.** The score when the scripted turns ended, before any repair.
- **Statistics.**
  - Scores are paired by task, with a task's repeats averaged into one unit.
  - CIs are 95% percentile bootstraps: 10,000 resamples, seed 1.
  - Score is read as hybrid minus master, in points. Cost is read as the ratio hybrid over master.

**Reading.**

- A scheme is better when it is clearly better on one measure and not clearly worse on the other.
  - Clearly better on score: the difference's CI lies above 0. Clearly better on cost: the ratio's CI lies below 1.
  - Clearly worse on score: the CI lies wholly below 0. Clearly worse on cost: the CI lies wholly above 1.
- Otherwise there is no clear difference, and both CIs are stated.
- The short and long sets are read apart, as in stage 0, and neither is pooled.

**Reported beside them, and not read as gates.**

- Routing: the share of local main calls, routes by source and target, and the classifier's cost on its own.
- Thread events: transfers by direction, background tasks and their reports, peer summaries, failed markers and thread errors.
- Repairs: repair rounds and their cost, and compile failures with their messages.

**Limits, stated now.**

- **The classifier is a stand-in.** In stage 0, Decisions chose local on every one of the 599 turns it routed. Its confidence barely separated the long turns the authors gave the cloud from those they gave local: 20% of the first went to the cloud against 14% of the second. Both schemes will be local-heavy. The comparison mostly reads the two context mechanisms under local-heavy routing, not the quality of routing.
- **The two classifiers see different input.** The hybrid's sees the thread state, while master's sees the query alone. That difference is part of what is compared.
- **The hybrid's forks may cost more.** They run at the default effort, so they may cost more than the prototype's did.
- **One configuration.** The runs use one local model, one window and one cloud model.

## Amendment 18: the hybrid sends what it is unsure of to the cloud

Written on 2026-11-08, before any run below. `final-short` had just finished and `final-long` was still running. The change was decided after about 70 of the 90 hybrid rows of `final-short` had been seen, and a few of `final-long`. The finished `final-short` has not been scored yet.

**What was seen.**

- At 43 short hybrid rows, the hybrid kept 98% of its routed turns on the edge: 6 of 356 went to the cloud.
- Master's routes send 27% of the scripted turns to the cloud.
- The difference is the default.
  - Master keeps a turn local only when the classifier picks local at θ 0.8 or above.
  - The hybrid kept a turn it could not decide with the current owner, and the first one went to the edge.
  - 216 of those 356 turns fell to that default. With the old default, a session that started on the edge almost never left it.
- At about 70 rows, the short reading was −22 points, CI [−32, −13], at a cost ratio of 0.21.

**The change.** The maintainer then set one rule for both schemes: when the classifier is not sure, the query goes to the cloud. `feat/main-thread` commit `05fdd29` changes the hybrid's default to match.

- These now send the query to the cloud, whoever owns the session:
  - a low-confidence answer;
  - a lag over `MaxLag`;
  - a classifier failure or timeout;
  - no classifier at all.
- The edge takes it only when the cloud is not a candidate.
- The hybrid question, the threshold and every other setting of Amendment 17 are unchanged. `local_based`, `cloud_based` and the Coordinator's no-router default are not touched.

**Runs.**

- Amendment 17's two runs go on to the end unchanged.
  - Their master rows are the master measurement here.
  - Their hybrid rows measure the old default. They are reported beside the results, and are not read as the hybrid.
- `eval/final` merges `feat/main-thread` at `05fdd29` (merge `d64e97e`). The binary is built on a clean tree at that merge. Master's code does not change: the diff touches only `thread/route`, which master does not use.
- `final2-short`: `MT-hybrid` alone on `s2acceptance`, `--repeat 3`, 90 rows. Flags as in `final-short`:
  - `--routes` from `routes-decisions`, which the hybrid ignores but the manifest records;
  - `--canonical-replay`;
  - `--repair-rounds 3`.
- `final2-long`: the same on `s2long`, `--repeat 2`, 24 rows, with `--routes` from `routes-long`.
- The hard stop is $10 over the smoke run and both runs: $3 for the short run and $7 for the long one.

**Smoke, before them, and not read.** The hybrid runs once on the first `s2` tuning task with Decisions. It checks that turns the classifier cannot decide now go to the cloud.

**Measures, statistics and reading.**

- Measures, statistics and reading are Amendment 17's. Master's rows come from `final-short` and `final-long`, and the hybrid's from `final2-short` and `final2-long`.
- Rows are paired by task across runs, as stage 0 compared `A0` from `l4-s2` against later runs.

**Reported beside them, and not read.**

- Amendment 17's diagnostics.
- The old-default hybrid against master, and against the new default.

**Limits, besides Amendment 17's.**

- **The schemes ran apart.** The two schemes' rows come from different processes and times on the same server. The local model samples, so rows from different runs are exchangeable, but the server's load differs. `final2-*` overlap the end of `final-long`.
- **The change came after some results.** It was made after partial results were seen. The rule is the maintainer's, and it is the one master already follows. It was not tuned on these rows.

## Amendment 19: the Main Thread on master's routes

Written on 2026-11-08, before any run below. `final2-short` had 44 of its 90 rows. `final2-long` and `final-long` were still running.

**What had been seen.** On the 44 short rows, the hybrid with the cloud as its default sent 84% of its routed turns to the cloud. It read +16 points over master, CI [+10, +24], at a cost ratio of 2.57.

**Question.** The maintainer asked for one more set, once Amendment 18's runs end: the Main Thread with master's routing policy left as it is.

**Scheme: `MT-routed`.** The thread, the participants and every setting are `MT-hybrid`'s (Amendments 17 and 18), with one exception: the router asks no classifier.

- A host rule (`route.Config.Rules`) gives each scripted turn the owner the frozen routes give it. These are the files master follows: the query-only classifier's choice, local only at θ 0.8 or above.
- A repair turn goes to the owner of the last scripted turn, as master sends it.
- What the two schemes share is the first owner of every query, repairs included. Everything else is the thread as shipped:
  - **Contexts and who summarizes them.**
    - Master keeps one shared history compressed at 8K, and the cloud writes its summaries as paid calls.
    - `MT-routed` keeps a context per side. The edge compresses its own context with the local model, synchronously, at no cloud cost. The cloud writes the edge's peer summaries and its own notes.
  - **Prompts, tools and views.**
    - The thread adds a workspace note, the participant prompt section and the thread tools.
    - Each side has its own workspace view. A whole-file write is refused when this side has not seen the file's current content, including a file the other side created or rewrote.
  - **Handovers.** Each of these can move work to the other side within a turn, where master's turn stays where the route put it:
    - a participant may `transfer` the foreground when it judges the other side better placed;
    - a participant may assign a background task to the other side;
    - a foreground run that ends in error fails over once to the other side. If that run fails too, the request ends without an owner, and the session goes on with its next turn and its repairs. In master the same error is a session error: the remaining turns and the repairs do not run.

**Classifier cost.** As for master, each session is charged its task's share of the routes ledgers.

**Runs.**

- They start after `final2-short`, `final2-long` and `final-long` end. The binary is built on a clean tree at the commit that adds this amendment.
- `final3-short`: `MT-routed` on `s2acceptance`, `--repeat 3`, `--routes` from `routes-decisions`, 90 rows.
- `final3-long`: the same on `s2long`, `--repeat 2`, `--routes` from `routes-long`, 24 rows.
- Common flags: `--canonical-replay` (it reaches master only) and `--repair-rounds 3`.
- The hard stop is $6 over the smoke run and both runs: $2 for the short run and $4 for the long one.

**Smoke, before them, and not read.** `MT-routed` runs once on the first `s2` tuning task, with its authored owners: the routes files hold no tuning task. It checks that the rule routes turns to both sides.

**Measures, statistics and reading.**

- These are Amendment 17's, `MT-routed` against master. Master's rows come from `final-short` and `final-long`.
- Rows are paired by task across runs, as in Amendment 18.

**Reported beside them, and not read.**

- `MT-routed` against the hybrid of Amendment 18, and Amendment 17's diagnostics.
- For both thread arms and master:
  - the realized share of local main calls;
  - transfers by direction;
  - failovers by direction;
  - background tasks assigned;
  - master's session errors.
- A sensitivity reading of score and cost against master. It leaves out every task where any session of either scheme had a failover or a session error.

**Void rows, decided now.** If the local server stops answering during a run, as in Amendment 15, the affected rows are void and rerun:
- for master, the session-error rows;
- for `MT-routed`, the rows whose sessions failed over while the server was down.
The ledger's local errors and the monitor's server checks show when that was.

**Limits.**

- Amendment 18's limits apply: the runs ran apart, at different server load.
- **The routes favor master's mechanism.** They were written by a classifier that saw only the query, for a single shared history. A thread that owns its context per side may have wanted other owners.
- **The handovers are part of what is compared.** A difference may come from the context mechanism or from work moving sides. The counts above show how often work moved, but they cannot split the difference between the two.
- **Stage 0 ran this design as a prototype.** T1 was not this code.
  - It ran on the same short routes in `routed-s2`, without repair rounds.
  - It ran on the long routes in `long-route` and `long-route2`.
  - In `repair-route*` (Amendment 14), the long-route sessions that ended on a compile error were rerun from scratch with repair rounds.

## Results of Amendments 17 to 19

Written on 2026-11-08, after every run below had finished.

**The comparison Amendment 17 asked for.** The shipped Main Thread with hybrid routing, run against master, reads no clear difference on both sets: it scores clearly higher at clearly higher cloud cost. The one "better" below belongs to `MT-routed`, which Amendment 19 added on master's frozen routes. That is not the shipped router.

**Runs.**

- No row has a session error, an infra failure or a thread error.
- No session of either thread arm failed over, and no thread wrote a failed marker. No master session ended in a session error.
- Local calls returned only 200 or 400 (context overflow), so the server never went down.
- The sensitivity readings therefore equal the primary ones, and no row is void.

| Run | Scheme | Rows | Cloud spend |
|---|---|---|---|
| `final-short` | master and the hybrid with the old default | 90 + 90 | $0.60 |
| `final-long` | master and the hybrid with the old default | 24 + 24 | $2.43 |
| `final2-short` | the hybrid, cloud default (Amendment 18) | 90 | $1.27 |
| `final2-long` | the hybrid, cloud default | 24 | $4.25 |
| `final3-short` | `MT-routed` (Amendment 19) | 90 | $0.59 |
| `final3-long` | `MT-routed` | 24 | $0.90 |
| four smoke runs | not read | | $0.02 |

Total: $10.06. Each amendment's spend stayed under its hard stop:

| Amendment | Spend | Hard stop |
|---|---|---|
| 17 | $3.04 | $12 |
| 18 | $5.52 | $10 |
| 19 | $1.49 | $6 |

Every manifest records `"fork_effort": "low"`. That is the stage 0 prototype arms' constant, and the thread arms do not read it: their forks ran at the API's default effort, as Amendment 17 says.

**Primary readings.** Each scheme is read against master on the measures and rule of Amendment 17:

- score after repairs, the scheme minus master, in points;
- cloud cost per session, as a ratio, with classifier calls included.

CIs are 95%, over 30 short tasks and 12 long ones.

| Set | Scheme | Score | Cost ratio | Reading |
|---|---|---|---|---|
| short | hybrid | +16.5 [+10.0, +24.1] | 2.58 [1.93, 3.51] | no clear difference |
| short | `MT-routed` | −3.1 [−10.1, +3.8] | 1.23 [0.68, 2.05] | no clear difference |
| long | hybrid | +59.8 [+43.9, +73.6] | 2.38 [1.73, 3.32] | no clear difference |
| long | `MT-routed` | +4.9 [−17.5, +30.3] | 0.51 [0.41, 0.63] | **`MT-routed` better** |

- **The hybrid** is clearly better on score and clearly worse on cost, on both sets. Under the rule neither scheme is better: the hybrid buys score with cloud calls.
- **`MT-routed`, short set:** no clear difference from master on either measure.
- **`MT-routed`, long set:** clearly cheaper, and cheaper on all 12 tasks, with no clear difference in score. The rule reads that as better. The score side is weak:
  - The CI's lower bound, −17.5 points, allows a loss of three quarters of master's .234.
  - `MT-routed` scored 0 in both repeats on 7 of the 12 tasks, master on 3.
  - Its mean rests on three tasks near .9 (a02 .951, b03 .877, b04 .941).
  - 16 of its 24 sessions did not compile after the repairs, against master's 14.

**Levels.**

| Scheme | Short: score | Short: before repairs | Short: $ per session | Long: score | Long: before repairs | Long: $ per session |
|---|---|---|---|---|---|---|
| master | .805 | .743 | .0054 | .234 | .110 | .0744 |
| hybrid | .970 | .959 | .0141 | .832 | .562 | .1772 |
| `MT-routed` | .774 | .653 | .0067 | .283 | .231 | .0382 |

**Secondary: before repairs.** Score is taken when the scripted turns ended, and is 0 for a session that did not compile then. That score is Amendment 17's secondary measure. The cost ratios beside it are not pre-registered:

- the maintainer asked for them after the results, and they are not read;
- their cost leaves out the calls made after that point, the repair rounds;
- classifier charges are kept.

| Set | Scheme | Score | Cost ratio |
|---|---|---|---|
| short | hybrid | +21.6 [+12.3, +31.8] | 3.06 [2.43, 3.90] |
| short | `MT-routed` | −9.0 [−19.6, +1.6] | 1.46 [0.85, 2.32] |
| long | hybrid | +45.3 [+29.1, +60.7] | 2.55 [1.85, 3.53] |
| long | `MT-routed` | +12.1 [−5.5, +33.0] | 0.57 [0.45, 0.70] |

**Where the long saving comes from.**

- Cloud compression is $0.0526 of master's $0.0744 per long session.
  - The harness sends master's compression to the cloud on purpose, so that the local model's summaries cannot handicap it. Amendment 17 says this favors master.
  - No master compressing on the edge was run. The long "better" holds against master with cloud compression.
- `MT-routed`'s edge compresses with the local model.
- Its cloud keeps an uncompressed context, so a cloud main call costs more:
  - `MT-routed`: $0.00081 a call, over 823 calls, $0.0278 a session;
  - master: $0.00050 a call, over 1,021 calls, $0.0211 a session.
- Its forks cost $0.0096 a session.
- This is an accounting split, not a causal one.

**Routing, reported beside the readings.**

- **Master's routes** send 27% of the short scripted turns and 17% of the long ones to the cloud. Master's local share of main calls is .701 on the short set and .839 on the long one.
- **The hybrid's classifier is unsure on most turns under both defaults.** It sees the thread state. These are the shares of turns where its answer reached θ:

  | Default | Short | Long |
  |---|---|---|
  | cloud | 17% (120 of 715) | 24% (181 of 742) |
  | old: the current owner, the first query to the edge | 41% (309 of 753) | 30% (231 of 781) |

  Every other turn fell to the default.
  - **Cloud default.** 84% of short turns and 76% of long ones went to the cloud. The local share of main calls was .160 and .391.
    - Amendment 17 expected both schemes to be local-heavy. That failed for this hybrid. Its comparison with master mostly shows how much more it sends to the cloud.
  - **Old default.** 4% of short turns and 10% of long ones went to the cloud. The hybrid then read:
    - short: −20.0 [−29.1, −10.4], at a cost ratio of 0.27 [0.16, 0.43];
    - long: −1.2 [−21.2, +22.3], at 0.37 [0.18, 0.58].
  - Those rows measure the old default, and Amendment 18 says they are not read as the hybrid. Taken alone, the long pair would meet the rule's "better".
  - Old default against the cloud default: short −36.5 [−44.1, −29.4] points at a cost ratio of 0.10 [0.07, 0.15], long −61.1 [−84.9, −27.9] at 0.16 [0.06, 0.31]. The default decides almost all of the hybrid's routing.
- **`MT-routed` follows master's routes.**
  - Short: 26% of its turns went to the cloud, repairs included. All 753 were routed by the rule, and its local share of main calls was .726.
  - Long: 16% (121 of 772), all by the rule, with a local share of .881.
- **The classifier's own cost per session.**
  - Hybrid: $0.00097 short and $0.0089 long.
  - Master and `MT-routed` were each charged $0.00019 and $0.00076.
- **Handovers.**
  - Transfers: the hybrid made 1 to the cloud and 3 to the edge on the short set, and none on the long set. `MT-routed` made 2 to the cloud on the short set and 1 on the long set.
  - Report entries: hybrid 3 short and 1 long; `MT-routed` 18 short and 39 long. They count every `report`, the foreground's intermediate results included. The Coordinator does not emit assignments to the host, so the background tasks assigned were not counted.
  - Owner changes by the router: 190 hybrid short, 141 hybrid long, 320 `MT-routed` short, 197 `MT-routed` long.
- **Summaries.**
  - Peer summaries: hybrid 202 short and 299 long; `MT-routed` 60 and 94.
  - The hybrid's forks, peer summaries and the cloud's own notes, cost $0.0023 a short session and $0.039 a long one: 17% and 22% of its cost.
  - `MT-routed`'s forks cost $0.0006 and $0.0096.

**Repairs.**

| Scheme | Repair rounds, short | Repair rounds, long | Repair cost a session, short | Repair cost a session, long |
|---|---|---|---|---|
| master | 17 | 55 | $0.00085 | $0.0079 |
| hybrid | 1 | 20 | <$0.00001 | $0.0079 |
| `MT-routed` | 39 | 50 | $0.00002 | $0.0006 |

Sessions that still did not build after the repairs:

| Set | master | hybrid | `MT-routed` |
|---|---|---|---|
| short | 2% | 0% | 8% |
| long | 14 of 24 | 3 of 24 | 16 of 24 |

Compile errors when the scripted turns ended, with sessions counted once per kind and a session possibly showing several. Two caveats apply:

- **The counts are lower bounds.** The stored compiler output is cut at 500 characters. It was cut in 13 of 20 master long sessions, 4 of 10 hybrid long, 12 of 18 `MT-routed` long and 2 of 21 `MT-routed` short.
- **"Other" is narrow.** It counts only sessions where none of the listed kinds appeared.

| Scheme | Short sessions | Short errors | Long sessions | Long errors |
|---|---|---|---|---|
| master | 10 | 6 type mismatches, 2 undefined names, 1 unused import, 2 other | 20 | 14 type mismatches, 10 undefined names, 3 redeclarations, 2 unused imports, 2 unused variables |
| hybrid | 1 | 1 unused import | 10 | 4 type mismatches, 4 redeclarations, 3 undefined names, 2 unused variables, 1 other |
| `MT-routed` | 21 | 10 type mismatches, 8 undefined names, 3 syntax errors, 1 unused import, 1 unused variable | 18 | 10 undefined names, 6 type mismatches, 5 redeclarations, 2 syntax errors, 1 unused variable |

**`MT-routed` against the hybrid, reported beside them.**

- short: −19.5 [−28.2, −11.7] points, at a cost ratio of 0.48 [0.27, 0.76];
- long: −54.9 [−82.7, −20.0], at 0.22 [0.16, 0.30].

**What this says.**

- **The shipped hybrid against master** reads no clear difference on both sets.
  - As built, its classifier is unsure on most turns, so its default decides where it runs.
  - With the cloud as the default, it scores far higher than master, at 2.4 to 2.6 times master's cloud cost.
  - With the old default, it cost 27% and 37% of master's. It scored lower on the short set, with no clear difference on the long one.
- **`MT-routed` on master's own routes:**
  - Short set: no clear difference on either measure. The score is −3.1 [−10.1, +3.8], and the cost 1.23 [0.68, 2.05], which is 23% dearer at the point estimate.
  - Long set: about half the cloud cost, with no clear difference in score. The saving is mostly master's cloud compression, and the score CI is wide on 12 tasks.
- **Long scores are low** for master and `MT-routed`: .234 and .283. Most of their long sessions still fail to compile after the repairs.
  - Master: 10 of its 14 failed only in the hidden test file. Its own code compiled, but its API did not match the tests.
  - `MT-routed`: 11 of its 16 had errors in their own files, one of them a file without a package clause, as in one of master's.
- **Limits:**
  - The schemes ran in different processes and at different times. These are the ledger spans, in UTC:
    - `final-short`: 08:57 to 09:40;
    - `final-long`: 08:57 to 11:13;
    - `final2`: 09:46 to 11:17, overlapping most of `final-long`;
    - `final3`: 11:17 to 12:29.
  - The cloud default was set after partial results had been seen.
  - The routes favor master's single history.
  - The classifier is a stand-in.

**Note after the results.** `eval/final` then merged `feat/main-thread` at `464c648` (merge `a08fbfd`). From that commit the router sends its classifier the query and the participants alone unless `route.Config.IncludeHistory` is set. `MT-hybrid` sets it, so its classifier input matches the runs above: 46 classifier requests on 3 tasks were the same before and after the merge, apart from the run-to-run order of tool names within a reply. `MT-routed` and master have no thread classifier and are unaffected.

## Amendment 20: the Main Thread on master's routes, on new long tasks

Written on 2026-11-08, after the results of Amendments 17 to 19 and before any of the new tasks exists.

**Why.** On the long set, `MT-routed` led master by 12.1 points [−5.5, +33.0] before repairs, at 0.57 of the cost. Stage 0's routed prototype led by 12.0 on the same 12 tasks. That lead was noticed on those 12 tasks, so it is checked on new ones. The user chose new tasks over more repeats on the old ones. More repeats would narrow the noise within a task, but would not show whether the lead holds on other tasks. The user also noted that not every task has a compiler to drive repairs, so the score before repairs is a primary measure here.

**The new set.** `s2.Long2()`, set `s2long2`: 12 tasks, four from each of three new authors.
- The rules are the long set's (Amendment 9). The tasks have `Set: "long"`, so `limitsOf`, `TestLongTaskShape` and every other rule test apply unchanged.
- The authors are kept from the harness, these notes, the runs and the results. Three things changed from the first set:
  - their worktrees are sparse checkouts that hold only `s2/`, `corpus/` and the repository's top-level files, so the harness, these notes and the runs are not on disk;
  - they may not read the first long set or each other's files;
  - the brief adds that every hidden test must follow from what the turns and the workspace docs say.
- **Known exposures.** Every author's session opened with context the host injects:
  - an automatically loaded notes index, with one line that summarized stage 0's results as two wins, long tasks and privacy;
  - the git status, whose newest commit subjects name the final comparison, the hybrid and the router;
  - the root `CLAUDE.md`, which describes the Main Thread.

  None of these shows how a task is scored. Also:
  - `CHANGELOG.md`, which mentions the stage 0 experiments, is on disk in each worktree. The brief does not allow reading it.
  - All three sessions started in the first author's worktree, and two of them moved to their own.
  - One author drafted in a shared drafting folder, which also holds the scoring scripts and the results write-up. The authors were then told to stay in their own subfolders.

  The freeze note lists what each author reports having seen.
- The three briefs are committed with the freeze, in `experiments/stage0/long2-briefs.md`.
- The domains differ from the first set's: consumer and personal software; rules engines, games and simulations; science, engineering and analytics tools.
- The set's hash is frozen in a note under this amendment before any model sees a task, and `TestLong2IsFrozen` pins the count and the hash.
- A task that fails the rule tests goes back to its author, not to the maintainer, before the freeze. If an author still cannot deliver four passing tasks, the set is frozen with fewer, and the note says so.

**Routes.** `routes-long2`: `route-tasks` on `s2long2` with the OpenAI Decisions stand-in at θ .8, as `routes-long` was made for the long set. It is written once, and both schemes follow it. Each session is charged its task's share of the routes ledger, as in Amendment 17.

**Run.** `final4-long`: master (A3-8K on the routes) and `MT-routed` in one process, on `s2long2`, `--repeat 2`, `--routes` from `routes-long2`, `--canonical-replay`, `--repair-rounds 3`, `--workers 10`, `--budget 6` with `--spent` set to the routes' spend. That is 48 rows. The binary is built on a clean tree at the commit that adds the freeze note. Running both schemes at once removes the "ran apart" limit of Amendments 18 and 19.

**Spend.** About $2.70 expected, from the long set's costs a session ($0.074 for master, $0.038 for `MT-routed`). The hard stop is $6 over the routes and the run.

**Primary readings,** `MT-routed` against master on `s2long2`. Statistics are Amendment 17's: per-task means over the two repeats, paired by task, 95% bootstrap CIs over the 12 tasks.

- **Before repairs.** Score is taken when the scripted turns end, and is 0 for a session that does not compile then. Cost leaves out the repair rounds and keeps the classifier charge. This is the user's question.
  - **The lead is confirmed** if the score CI lies above 0.
  - **The lead is reversed** if the CI lies below 0: it does not hold on these tasks.
  - **Otherwise it is not confirmed.** That is not evidence that there is no lead.
  - **"Not confirmed" is the most likely outcome even if the lead is real.** On the first set's rows, the standard error of the mean difference is about 10.3 points. So the CI lies above 0 about 22% of the time if the true lead is +12.1, 12% at +8 and 7% at +5. About half of that error is noise between repeats. `--repeat 4` would raise the 22% to about 28%, and 24 new tasks to about 38%. The user chose 12 new tasks, so this run can confirm a lead but cannot rule one out.
- **After repairs,** on Amendment 17's measures: score after repairs, and the whole session's cloud cost.

Both are read under Amendment 17's rule: a scheme is better when it is clearly better on one measure and not clearly worse on the other.

The cost side of that rule is close to settled before the run. On the first set, `MT-routed` was cheaper on all 12 tasks, with a before-repair cost ratio of 0.57 [0.45, 0.70], and most of the gap is master's cloud compression. So "`MT-routed` better" under the rule needs little more than a score CI that is not wholly below 0. It is not new evidence of a score lead. Only the confirmation criterion above speaks to the score.

**Reported beside them, and not read.**

- **Both long sets pooled.** That is 24 tasks:
  - the first set from `final-long` (master) and `final3-long` (`MT-routed`);
  - the new set from `final4-long`.
  
  The first set is where the lead was noticed, so the pooled estimate is not a test.
- Amendment 19's diagnostics: local share of main calls, transfers, failovers, report entries, master's session errors, repairs and compile errors.
- Amendment 19's sensitivity reading, which leaves out every task where any session of either scheme had a failover or a session error.
- Hidden tests that fail in every session of both schemes that compiled. These are reported as possible task defects and are not dropped, as in Amendment 11.

**Void, infra and missing rows.**
- The void-row rule is Amendment 19's: if the local server stops answering, the affected rows are void. Infra rows are not read, as in Amendment 13.
- Void and infra rows are rerun once, as the same pairs, in `final4-long-r`. The rerun row replaces the original.
- If the hard stop ends the run early, the rows already written are kept, and the result is reported as incomplete.
- A task left without a valid row for either scheme is reported. The readings cover the tasks both schemes have, and say how many that is.

**Limits.**

- n is 12 tasks, so only a large difference clears its CI (see the chance of confirming above).
- **These are different tasks, not the same kind again.** New authors, new domains and a changed brief make this a check on a different population of tasks. A null result cannot tell noise from a lead that depends on the kind of task. The pooled 24 tasks mix the two populations, two commits, and runs made apart.
- **The routes favor master's mechanism,** as Amendment 19 says. They come from a stand-in classifier that saw only the query, for one shared history, and they are a single draw.
- **One configuration,** as in Amendment 17: one local model, one 8K window and one cloud model.
- Master's compression goes to the cloud, as in Amendment 17.
- The authors are models, as before.

### Freeze note

Written at 22:45 on 2026-11-08, before any model has seen a task.

**The second long set is frozen** at `238247008249cee92547f386099210260b77b350a72d7875b87880c18ed2449d`.
- **Size and shape:** 12 tasks, four from each author. They have 25 to 32 turns, 13 to 23 switches, 11 to 17 cloud turns and 13 to 17 local turns, 7 to 11 carries and 36 to 50 hidden tests.
- **Tests:** every task passes the rule tests in `s2_test.go`, including that forgetting a carry fails its tests. `TestLong2IsFrozen` pins the count and the hash. No task went back to its author.
- **The tasks:**
  - **Author A:** `s2-long2-a01-plant-care`, `a02-flashcards`, `a03-podcast-player`, `a04-address-book`.
  - **Author B:** `s2-long2-b01-elevator-bank`, `b02-skirmish-rules`, `b03-greenhouse-rules`, `b04-wildfire-grid`.
  - **Author C:** `s2-long2-c01-read-trimming`, `c02-tensile-tests`, `c03-reaction-planner`, `c04-quake-catalog`.
- **The briefs** and the later clarification are in `experiments/stage0/long2-briefs.md`.

**Timing.**
- This amendment was committed at 21:28:42 (`f9ed05d`).
- Author B began drafting b01 in the shared drafting folder at 21:27:11, and the first task file in a worktree dates from 21:30:09.
- The maintainer did not open the drafts. The editor's diagnostics did show some of their file and type names in the maintainer's session, after the plan above was set.

**What each author reports having seen,** beyond the exposures listed above:

- **Author A.**
  - A listing of `s2/`, which showed the first set's file names.
  - Output of `TestWorkloadSummary`, the leak Amendment 11 recorded. For `s2-long-c02`, `c03` and `c04` it showed turns, owner strings, hidden-test counts and every carry description. For `s2-long-a03` and `s2-a03` it showed summary lines and a few carry descriptions.
  - Notices naming the other authors' background tasks and their output paths. A did not open them.
  - Nothing in the shared drafting folder.
- **Author B.**
  - The first author's git status: file names only.
  - One listing of the shared drafting folder root, before the clarification. It showed file names that include `results17.md`, `amendment18.md` and the scoring scripts. B opened none of them.
  - B created and then removed four empty folders there, and from then on drafted only in its own subfolder.
  - Notices naming the other authors' tasks.
- **Author C.**
  - The first author's git status: file names only.
  - A listing of `s2/`.
  - A `grep` for test function names that scanned the first set's forgetful files, with those matches filtered out.
  - Nothing in the shared drafting folder.
- **Authors B and C** ran a Python interpreter from outside the worktree, with `-I`, and read no files there.

None of this shows how a task is scored. A path audit of the authors' full transcripts is reported with the results. The review of Amendment 20 audited them up to 21:28, and found B's only calls outside its subfolder to be the creation and removal of those folders.

## Amendment 21: the final comparison is void

Written on 2026-11-08 at 23:50, on the user's decision.

**Every reading against master in Amendments 17 to 20 is void.** That includes the readings in "Results of Amendments 17 to 19". Master is A3-8K, which sends every context compression to the cloud. That arrangement was the maintainer's choice in stage 0's harness ("so that the local model's summary quality cannot handicap the baseline", `HARNESS.md`). It is not what code on `main` does: `UnifiedAgent` compresses with its own model, which in a session routed by turn is the current owner's.

The arrangement raised master's cloud cost, because its compression was most of that cost. It also raised the quality of master's summaries. So master was not today's practice, on either measure. Amendment 17 noted that the arrangement "favors this scheme", but it called master today's practice and that difference was not confirmed with the user before the runs.

`final4-long` was stopped by process ID on the user's word. It had 22 rows and $0.887 of cloud spend; the routes cost another $0.009. Its rows are not read.

**What stands:**
- the second long set, frozen at `fb12f05`;
- `routes-long2`;
- the router's `IncludeHistory` switch.

**Before any new comparison,** every part of the master arm will be listed with its source: code on `main`, the user's decision, or the maintainer's choice. The user confirms each part the maintainer chose before any spend.

**The user's decision after Amendment 21, 2026-11-08.** No comparison against `main` will be run.
- **What `main` offers for a session with tools.** Its routers refuse tool calls (`tierrouter`, `managedtask`). So for a session that uses tools on both sides, its only cloud and local component is `ConnectivityAwareModel`:
  - while the cloud is reachable, every call goes to the cloud;
  - the history is compressed at the smaller window, and the cloud writes the summaries;
  - the local model takes over only after cloud failures.
- **The user's reading.** `main` has no scheme that puts a tool-using session's work on the edge. The shipped Main Thread is therefore the scheme to go forward with.
- **No measurement backs this choice.** Main Thread has not been measured as better than any `main` scheme: every final reading against master is void.
- **Stage 0's single-history baselines also compressed in the cloud.** So stage 0's cost comparisons against them are biased in the same way. Its score comparisons favored the baselines, so a Main Thread lead in score there is conservative.
