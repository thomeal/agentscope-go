# Briefs for the second long set

> Local paths are replaced by placeholders; the briefs are otherwise as sent.

The three author briefs of Amendment 20 as sent, then the clarification each author received at 21:27 on 2026-11-08.

## author A2

````text
Write 4 new long, multi-turn coding tasks as test fixtures in a Go repository. This is plain Go fixture work, with no model calls and no network.

**Where to work.** Work only in the git worktree `<worktree long2-a>` (branch `long2/author-a`), and run every command from there. On Windows, set `GOCACHE=<go cache>` for Go commands. This worktree holds only `experiments/stage0/s2/`, `experiments/stage0/corpus/` and the repository's top-level files.

**Stay blind.** Other people will evaluate assistants on what you write, so you must not know how. Concretely:
- Do not read anything outside that worktree. In particular, stay out of `<main checkout>` itself and every other worktree.
- Inside the worktree, read only `experiments/stage0/s2/` and, if a type needs it, `experiments/stage0/corpus/`.
- Do not run `git log`, `git show`, `git diff` against other commits, or anything else that reads history or other branches. Do not change the sparse checkout.
- Do not read `long_a*`, `long_b*` or `long_c*` files: those are an earlier set, and yours must be independent of it. Do not read `long2_b*` or `long2_c*` files either, if any appear.
- If something you need seems to be in a forbidden place, work around it and say so in your report.

**What the workload is.** Read `experiments/stage0/s2/s2.go` first.
- A task is a small Go module plus a script of user turns. Two assistants serve the turns, sharing one workspace: a large-window `cloud` assistant and a small-window `local` one.
- Hidden tests decide the score at the end.
- What makes it a test: the code at the end depends on things settled earlier in the session, on the other side, that later turns never restate.

Read `s2_test.go`, `forgetful_test.go` and `stated_test.go`. Then read one example with its variants: `acc_a02.go` and its matching entries in `acc_a_forgetful_test.go`. Note how `Carry`, `Absent` and the forgetful variants work.

**How long tasks differ.** Your tasks are long sessions, like real multi-day feature work on one small codebase. The exact limits are in `limitsOf` and `TestLongTaskShape` in `s2_test.go`:
- **Turns and owners:** 20 to 32 turns, at least 8 switches of owner and at least 8 turns on each side. Include at least one stretch of 3 or more turns on one side. Use several stretches of different lengths, as real work has, not strict alternation.
- **Size:** 5 to 14 initial files. The finished module, initial files plus solution, holds 18,000 to 36,000 characters, with no single file over 4,500 characters.
- **Growth:** the session has to grow. Most turns should change files that already exist, so an assistant keeps reading and rewriting whole files of 2,000 to 4,500 characters.
  - Include docs in the workspace that a turn asks one side to read, such as notes, a spec, an export format or a policy.
  - Include refactors that touch several files.
  - Avoid a module made of many tiny files.
- **Carries and tests:** at least 6 carries and at least 20 hidden tests. At least half the carries must be needed on the other side 8 or more turns after they were settled.
- **A rule that changes:** include at least one rule that changes mid-session. The user settles something, work goes on, then the user revises it, and only the current rule is right at the end. Treat the revision as its own carry, established at the turn that revises it. Word later turns so they satisfy `Absent` for both.
- **Plausible owners:** `local` is a small model, so give it the simpler, mechanical turns a small model can really do, and give `cloud` the harder design and algorithm turns. A turn on `local` must still be achievable with one or two files in view.
- **Turn size:** each turn is one well-scoped change, under 600 characters, with no declaration over 30 lines in the solution.
- **Determined tests:** every hidden test must follow from what the turns and the workspace docs say. A test that checks a choice no turn or doc states is a defect.

**Your files**, all in `experiments/stage0/s2/`:
- `long2_a01.go` to `long2_a04.go`, with functions `long2A01()` to `long2A04()`, using `system(...)`, `cloud(...)`, `local(...)`, `script(...)`, `goMod` and `hiddenName`.
- IDs `s2-long2-a01-<name>` to `s2-long2-a04-<name>`, and `Set: "long"` on each.
- Register the four functions in `long2_a.go`, in `long2A`.
- `long2_a_forgetful_test.go`, with an `init` that fills `forgetful[...]` for every carry, as `acc_a_forgetful_test.go` does.

Do not change any other file, and do not change any test's rules. If a rule seems impossible, say so in your report and do not edit it.

**Your domain:** consumer and personal software. Pick four quite different apps, and not these exact ones: a household budget, a habit tracker, a trip itinerary, an e-book library. Avoid domains that already appear among these existing task IDs:

job-queue, invoice-totals, catalog-listing, settings-file, warehouse-stock, room-bookings, sales-export, member-signup, cache-cleanup, sensor-stats, line-protocol, access-rules, deploy-dry-run, event-timeline, shipping-rates, library-loans, payroll-hours, loyalty-points, coupon-redemption, expense-reports, rent-ledger, gym-waitlist, restaurant-tabs, event-ticketing, parcel-lockers, rate-limiter, log-rotation, url-router, flag-parser, template-renderer, feature-flags, version-compare, glob-matcher, circuit-breaker, dns-records, unit-conversion, csv-merge, downsample, gradebook, recipe-scaling, seating-chart, income-tax, calendar-recurrence, reorder-forecast, text-tokenizer, fleet-servicing, sales-commissions, helpdesk-sla, claims-desk, subtitle-tool, po-catalog, mainframe-extract, mail-digest, wal-store, lease-manager, alert-pipeline, process-supervisor.

**When done,** this must pass from the worktree root, with no failures in your tasks:

`go test ./experiments/stage0/s2/ -run 'TestTaskSet|TestTurnScripts|TestFixtureLimits|TestChangeSizes|TestFixtureSources|TestCarries|TestLongTaskShape|TestEveryTask|TestForgetfulSolutions|TestTotalAmnesia|TestHiddenNamesAreStated|TestDeterminism'`

Also run `gofmt -l` on your files. Do not commit; leave your files in the working tree.

**Report back:**
- the full list of files you wrote;
- for each task: its ID, turns, switches, finished size in characters, carries and hidden tests;
- the final test output line;
- anything you could not satisfy or had to work around, and anything you saw that you were not meant to see.
````

Clarification:

````text
A clarification to your brief: draft only inside your worktree, or in a subfolder of the shared drafting folder named authora. Do not list, open or read any other file or folder in the shared drafting folder; other people's files are kept there. In your report, say whether you saw any shared drafting folder file outside that subfolder, including in a directory listing, and anything you saw from the long2-b or long2-c worktrees.
````

## author B2

````text
Write 4 new long, multi-turn coding tasks as test fixtures in a Go repository. This is plain Go fixture work, with no model calls and no network.

**Where to work.** Work only in the git worktree `<worktree long2-b>` (branch `long2/author-b`), and run every command from there. On Windows, set `GOCACHE=<go cache>` for Go commands. This worktree holds only `experiments/stage0/s2/`, `experiments/stage0/corpus/` and the repository's top-level files.

**Stay blind.** Other people will evaluate assistants on what you write, so you must not know how. Concretely:
- Do not read anything outside that worktree. In particular, stay out of `<main checkout>` itself and every other worktree.
- Inside the worktree, read only `experiments/stage0/s2/` and, if a type needs it, `experiments/stage0/corpus/`.
- Do not run `git log`, `git show`, `git diff` against other commits, or anything else that reads history or other branches. Do not change the sparse checkout.
- Do not read `long_a*`, `long_b*` or `long_c*` files: those are an earlier set, and yours must be independent of it. Do not read `long2_a*` or `long2_c*` files either, if any appear.
- If something you need seems to be in a forbidden place, work around it and say so in your report.

**What the workload is.** Read `experiments/stage0/s2/s2.go` first.
- A task is a small Go module plus a script of user turns. Two assistants serve the turns, sharing one workspace: a large-window `cloud` assistant and a small-window `local` one.
- Hidden tests decide the score at the end.
- What makes it a test: the code at the end depends on things settled earlier in the session, on the other side, that later turns never restate.

Read `s2_test.go`, `forgetful_test.go` and `stated_test.go`. Then read one example with its variants: `acc_b02.go` and its matching entries in `acc_b_forgetful_test.go`. Note how `Carry`, `Absent` and the forgetful variants work.

**How long tasks differ.** Your tasks are long sessions, like real multi-day feature work on one small codebase. The exact limits are in `limitsOf` and `TestLongTaskShape` in `s2_test.go`:
- **Turns and owners:** 20 to 32 turns, at least 8 switches of owner and at least 8 turns on each side. Include at least one stretch of 3 or more turns on one side. Use several stretches of different lengths, as real work has, not strict alternation.
- **Size:** 5 to 14 initial files. The finished module, initial files plus solution, holds 18,000 to 36,000 characters, with no single file over 4,500 characters.
- **Growth:** the session has to grow. Most turns should change files that already exist, so an assistant keeps reading and rewriting whole files of 2,000 to 4,500 characters.
  - Include docs in the workspace that a turn asks one side to read, such as notes, a spec, an export format or a policy.
  - Include refactors that touch several files.
  - Avoid a module made of many tiny files.
- **Carries and tests:** at least 6 carries and at least 20 hidden tests. At least half the carries must be needed on the other side 8 or more turns after they were settled.
- **A rule that changes:** include at least one rule that changes mid-session. The user settles something, work goes on, then the user revises it, and only the current rule is right at the end. Treat the revision as its own carry, established at the turn that revises it. Word later turns so they satisfy `Absent` for both.
- **Plausible owners:** `local` is a small model, so give it the simpler, mechanical turns a small model can really do, and give `cloud` the harder design and algorithm turns. A turn on `local` must still be achievable with one or two files in view.
- **Turn size:** each turn is one well-scoped change, under 600 characters, with no declaration over 30 lines in the solution.
- **Determined tests:** every hidden test must follow from what the turns and the workspace docs say. A test that checks a choice no turn or doc states is a defect.

**Your files**, all in `experiments/stage0/s2/`:
- `long2_b01.go` to `long2_b04.go`, with functions `long2B01()` to `long2B04()`, using `system(...)`, `cloud(...)`, `local(...)`, `script(...)`, `goMod` and `hiddenName`.
- IDs `s2-long2-b01-<name>` to `s2-long2-b04-<name>`, and `Set: "long"` on each.
- Register the four functions in `long2_b.go`, in `long2B`.
- `long2_b_forgetful_test.go`, with an `init` that fills `forgetful[...]` for every carry, as `acc_b_forgetful_test.go` does.

Do not change any other file, and do not change any test's rules. If a rule seems impossible, say so in your report and do not edit it.

**Your domain:** rules engines, games and simulations. Pick four quite different ones, and not these exact ones: a tournament bracket, a board-game referee, a traffic simulation, a card-game scorer. Avoid domains that already appear among these existing task IDs:

job-queue, invoice-totals, catalog-listing, settings-file, warehouse-stock, room-bookings, sales-export, member-signup, cache-cleanup, sensor-stats, line-protocol, access-rules, deploy-dry-run, event-timeline, shipping-rates, library-loans, payroll-hours, loyalty-points, coupon-redemption, expense-reports, rent-ledger, gym-waitlist, restaurant-tabs, event-ticketing, parcel-lockers, rate-limiter, log-rotation, url-router, flag-parser, template-renderer, feature-flags, version-compare, glob-matcher, circuit-breaker, dns-records, unit-conversion, csv-merge, downsample, gradebook, recipe-scaling, seating-chart, income-tax, calendar-recurrence, reorder-forecast, text-tokenizer, fleet-servicing, sales-commissions, helpdesk-sla, claims-desk, subtitle-tool, po-catalog, mainframe-extract, mail-digest, wal-store, lease-manager, alert-pipeline, process-supervisor.

**When done,** this must pass from the worktree root, with no failures in your tasks:

`go test ./experiments/stage0/s2/ -run 'TestTaskSet|TestTurnScripts|TestFixtureLimits|TestChangeSizes|TestFixtureSources|TestCarries|TestLongTaskShape|TestEveryTask|TestForgetfulSolutions|TestTotalAmnesia|TestHiddenNamesAreStated|TestDeterminism'`

Also run `gofmt -l` on your files. Do not commit; leave your files in the working tree.

**Report back:**
- the full list of files you wrote;
- for each task: its ID, turns, switches, finished size in characters, carries and hidden tests;
- the final test output line;
- anything you could not satisfy or had to work around, and anything you saw that you were not meant to see.
````

Clarification:

````text
A clarification to your brief, about the shared drafting folder: draft only in your own subfolder there (authorb). Do not list, open or read any other file or folder in the shared drafting folder; other people's files are kept there. In your report, say whether you saw any shared drafting folder file outside your subfolder, including in a directory listing.
````

## author C2

````text
Write 4 new long, multi-turn coding tasks as test fixtures in a Go repository. This is plain Go fixture work, with no model calls and no network.

**Where to work.** Work only in the git worktree `<worktree long2-c>` (branch `long2/author-c`), and run every command from there. On Windows, set `GOCACHE=<go cache>` for Go commands. This worktree holds only `experiments/stage0/s2/`, `experiments/stage0/corpus/` and the repository's top-level files.

**Stay blind.** Other people will evaluate assistants on what you write, so you must not know how. Concretely:
- Do not read anything outside that worktree. In particular, stay out of `<main checkout>` itself and every other worktree.
- Inside the worktree, read only `experiments/stage0/s2/` and, if a type needs it, `experiments/stage0/corpus/`.
- Do not run `git log`, `git show`, `git diff` against other commits, or anything else that reads history or other branches. Do not change the sparse checkout.
- Do not read `long_a*`, `long_b*` or `long_c*` files: those are an earlier set, and yours must be independent of it. Do not read `long2_a*` or `long2_b*` files either, if any appear.
- If something you need seems to be in a forbidden place, work around it and say so in your report.

**What the workload is.** Read `experiments/stage0/s2/s2.go` first.
- A task is a small Go module plus a script of user turns. Two assistants serve the turns, sharing one workspace: a large-window `cloud` assistant and a small-window `local` one.
- Hidden tests decide the score at the end.
- What makes it a test: the code at the end depends on things settled earlier in the session, on the other side, that later turns never restate.

Read `s2_test.go`, `forgetful_test.go` and `stated_test.go`. Then read one example with its variants: `acc_c02.go` and its matching entries in `acc_c_forgetful_test.go`. Note how `Carry`, `Absent` and the forgetful variants work.

**How long tasks differ.** Your tasks are long sessions, like real multi-day feature work on one small codebase. The exact limits are in `limitsOf` and `TestLongTaskShape` in `s2_test.go`:
- **Turns and owners:** 20 to 32 turns, at least 8 switches of owner and at least 8 turns on each side. Include at least one stretch of 3 or more turns on one side. Use several stretches of different lengths, as real work has, not strict alternation.
- **Size:** 5 to 14 initial files. The finished module, initial files plus solution, holds 18,000 to 36,000 characters, with no single file over 4,500 characters.
- **Growth:** the session has to grow. Most turns should change files that already exist, so an assistant keeps reading and rewriting whole files of 2,000 to 4,500 characters.
  - Include docs in the workspace that a turn asks one side to read, such as notes, a spec, an export format or a policy.
  - Include refactors that touch several files.
  - Avoid a module made of many tiny files.
- **Carries and tests:** at least 6 carries and at least 20 hidden tests. At least half the carries must be needed on the other side 8 or more turns after they were settled.
- **A rule that changes:** include at least one rule that changes mid-session. The user settles something, work goes on, then the user revises it, and only the current rule is right at the end. Treat the revision as its own carry, established at the turn that revises it. Word later turns so they satisfy `Absent` for both.
- **Plausible owners:** `local` is a small model, so give it the simpler, mechanical turns a small model can really do, and give `cloud` the harder design and algorithm turns. A turn on `local` must still be achievable with one or two files in view.
- **Turn size:** each turn is one well-scoped change, under 600 characters, with no declaration over 30 lines in the solution.
- **Determined tests:** every hidden test must follow from what the turns and the workspace docs say. A test that checks a choice no turn or doc states is a defect.

**Your files**, all in `experiments/stage0/s2/`:
- `long2_c01.go` to `long2_c04.go`, with functions `long2C01()` to `long2C04()`, using `system(...)`, `cloud(...)`, `local(...)`, `script(...)`, `goMod` and `hiddenName`.
- IDs `s2-long2-c01-<name>` to `s2-long2-c04-<name>`, and `Set: "long"` on each.
- Register the four functions in `long2_c.go`, in `long2C`.
- `long2_c_forgetful_test.go`, with an `init` that fills `forgetful[...]` for every carry, as `acc_c_forgetful_test.go` does.

Do not change any other file, and do not change any test's rules. If a rule seems impossible, say so in your report and do not edit it.

**Your domain:** science, engineering and analytics tools. Pick four quite different ones, and not these exact ones: lab sample tracking, survey statistics, a bill of materials, sensor calibration. Avoid domains that already appear among these existing task IDs:

job-queue, invoice-totals, catalog-listing, settings-file, warehouse-stock, room-bookings, sales-export, member-signup, cache-cleanup, sensor-stats, line-protocol, access-rules, deploy-dry-run, event-timeline, shipping-rates, library-loans, payroll-hours, loyalty-points, coupon-redemption, expense-reports, rent-ledger, gym-waitlist, restaurant-tabs, event-ticketing, parcel-lockers, rate-limiter, log-rotation, url-router, flag-parser, template-renderer, feature-flags, version-compare, glob-matcher, circuit-breaker, dns-records, unit-conversion, csv-merge, downsample, gradebook, recipe-scaling, seating-chart, income-tax, calendar-recurrence, reorder-forecast, text-tokenizer, fleet-servicing, sales-commissions, helpdesk-sla, claims-desk, subtitle-tool, po-catalog, mainframe-extract, mail-digest, wal-store, lease-manager, alert-pipeline, process-supervisor.

**When done,** this must pass from the worktree root, with no failures in your tasks:

`go test ./experiments/stage0/s2/ -run 'TestTaskSet|TestTurnScripts|TestFixtureLimits|TestChangeSizes|TestFixtureSources|TestCarries|TestLongTaskShape|TestEveryTask|TestForgetfulSolutions|TestTotalAmnesia|TestHiddenNamesAreStated|TestDeterminism'`

Also run `gofmt -l` on your files. Do not commit; leave your files in the working tree.

**Report back:**
- the full list of files you wrote;
- for each task: its ID, turns, switches, finished size in characters, carries and hidden tests;
- the final test output line;
- anything you could not satisfy or had to work around, and anything you saw that you were not meant to see.
````

Clarification:

````text
A clarification to your brief: draft only inside your worktree, or in a subfolder of the shared drafting folder named authorc. Do not list, open or read any other file or folder in the shared drafting folder; other people's files are kept there. In your report, say whether you saw any shared drafting folder file outside that subfolder, including in a directory listing, and anything you saw in the long2-a worktree, where your session started.
````
