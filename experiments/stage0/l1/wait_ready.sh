#!/usr/bin/env bash
# Waits until vLLM answers, or reports why it did not. Usage: wait_ready.sh <log> [port]
LOG="$1"
PORT="${2:-8000}"
sleep 5
for i in $(seq 1 200); do
  if curl -sf -m 2 "http://127.0.0.1:${PORT}/v1/models" >/dev/null; then
    echo "READY after $((i * 3))s"
    break
  fi
  if ! pgrep -f "[v]llm serve" >/dev/null; then
    echo "DIED"
    break
  fi
  sleep 3
done
grep -nE "Application startup|ERROR|Error|Traceback|KV cache|Maximum concurrency|max_model_len|error:" "$LOG" | tail -n 14 | cut -c1-220
tail -n 3 "$LOG" | cut -c1-220
nvidia-smi --query-gpu=memory.used,memory.free,utilization.gpu --format=csv,noheader
