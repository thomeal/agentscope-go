#!/usr/bin/env bash
# Tuning variants on the first five pilot sessions, before pre-registration.
# Usage: variants.sh <stage0 binary>
set -u
BIN="$1"
run() {
  name="$1"; arms="$2"; shift 2
  "$BIN" run --run "$name" --arms "$arms" --set pilot --limit-sessions 5 --workers 10 \
    --local-slots 12 --cloud-slots 6 --budget 20 --spent 1.2 "$@" 2>&1 \
    | sed -E 's/sk-[A-Za-z0-9_-]+/sk-***/g' | tail -1 | sed "s/^/$name: /"
}
run v-r5k-clear T1 --fork-refresh 5000 &
run v-r5k-keep T1 --fork-refresh 5000 --never-clear &
run v-r5k-t2500-clear T1,A4 --fork-refresh 5000 --summary-target 2500 &
run v-r5k-t2500-keep T1 --fork-refresh 5000 --summary-target 2500 --never-clear &
wait
