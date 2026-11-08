#!/usr/bin/env bash
# Runs the L1 checks, then the throughput sweep, against a running vLLM.
cd "$(dirname "$0")"
PY="$HOME/stage0/venv/bin/python"
OUT="$HOME/stage0/logs/l1-$(date +%H%M%S)"
mkdir -p "$OUT"
"$PY" checks.py | tee "$OUT/checks.jsonl"
"$PY" bench.py --levels "${LEVELS:-1,2,4,8,16,24,32,48}" --rounds "${ROUNDS:-3}" | tee "$OUT/bench.jsonl"
nvidia-smi --query-gpu=memory.used,utilization.gpu,power.draw --format=csv,noheader
echo "output in $OUT"
