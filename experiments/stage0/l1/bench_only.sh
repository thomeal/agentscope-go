#!/usr/bin/env bash
cd "$(dirname "$0")"
PY="$HOME/stage0/venv/bin/python"
OUT="$HOME/stage0/logs/l1-$(date +%H%M%S)"
mkdir -p "$OUT"
"$PY" - <<'PYEOF'
import checks
checks.check_window("http://127.0.0.1:8000", "Qwen3.5-4B")
PYEOF
"$PY" bench.py --levels "${LEVELS:-1,2,4,8,16,24,32,48}" --rounds "${ROUNDS:-3}" --input "${INPUT:-6000}" --output "${OUTPUT:-256}" | tee "$OUT/bench.jsonl"
nvidia-smi --query-gpu=memory.used,utilization.gpu,power.draw --format=csv,noheader
echo "output in $OUT"
