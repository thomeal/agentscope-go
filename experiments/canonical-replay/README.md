# Canonical replay on Amendment 8's prompts

This program replays stage 0's 11 recorded first-write cloud requests through
`model.ConnectivityAwareModel`, with and without `WithCanonicalReplay()`, against
the OpenAI Responses API. It tests the library option on the measurement that
motivated it. The design, prediction, reading and limits are pre-registered in
[Amendment 22](https://github.com/thomeal/agentscope-go/blob/f7da54b2195f681d0b9adaeb74fd0ce8d909dc18/experiments/stage0/prereg.md#amendment-22-the-librarys-canonical-replay-on-amendment-8s-prompts).

Each recorded request is rebuilt as the history that produced it. The local
model's turns come from a scripted model while the cloud is held unreachable, so
the wrapper's own local path returns them (and, with the option, marks them). The
final request goes to the cloud through the real adapter. Both variants share
stage 0's transport shaping: `store` false, encrypted reasoning included,
`function_call` item ids that do not start with `fc` dropped, and
`max_output_tokens` 8,000. A reply stopped at that cap counts as a runaway.

The run of record used commit `dd5c72a`.

## Data

The input, `prompts.jsonl` (stage 0's recorded requests from
`runs/replay-runaway`), is not published, and neither is Amendment 8's ledger nor
this run's. `check` and `run` need that file.

## Tests

```bash
go test ./experiments/canonical-replay/
```

This runs offline on synthetic fixtures and sends nothing. Four cross-checks skip
without stage 0's material:

- `STAGE0_DIR`, a checkout of `eval/stage0`, lets the test compare the copied
  stage 0 helpers with `experiments/stage0/lab/cloud.go`.
- `STAGE0_REPLAY_DIR`, a directory holding `prompts.jsonl` and `replay.jsonl`,
  runs the fidelity check on the recorded prompts. It also checks prices and the
  reading against Amendment 8's ledger.

## Commands

```bash
go build -o bin/canonical-replay ./experiments/canonical-replay
bin/canonical-replay check  -prompts PROMPTS [-out DIR]
bin/canonical-replay run    -prompts PROMPTS -env KEYFILE [-out DIR]
bin/canonical-replay run    -prompts PROMPTS -env KEYFILE [-out DIR] -rerun
bin/canonical-replay report [-out DIR]
```

- **`check`** builds every outbound body without sending it. It requires
  `lib_off` to equal the recorded request and `lib_on` to equal stage 0's
  argument rewrite.
- **`run`** repeats the check and refuses to start on a difference. It sends
  660 calls (11 prompts × 30 × 2 variants, shuffled) and appends one row per call
  to `DIR/replay.jsonl`. `DIR` defaults to `replay-canonical-lib` in the working
  directory. Running the same command again resumes.
- **`run -rerun`** runs each call that ended in an infra error once more.
- **`report`** gives the reading by prompt with its bootstrap CI.

`KEYFILE` holds an `OPENAI_API_KEY=` line. The key is never printed or written.
Keep the file outside the repository.

The model is fixed to `gpt-6-luna` at `api.openai.com`. This makes live, billable
requests: about $0.35 is expected. The program enforces a $2 hard stop over the
whole ledger, reruns included.
