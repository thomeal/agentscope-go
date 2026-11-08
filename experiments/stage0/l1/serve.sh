#!/usr/bin/env bash
# Starts vLLM for stage 0 inside WSL. Usage: serve.sh <max-model-len> [port]
# Runs in the foreground, logging to ~/stage0/logs. The model lives in H:\Models.
set -euo pipefail

LEN="${1:-8192}"
PORT="${2:-8000}"
MODEL=/mnt/h/Models/Qwen3.5-4B
LOG="$HOME/stage0/logs/vllm-${LEN}.log"

# shellcheck disable=SC1091
. "$HOME/stage0/venv/bin/activate"
exec vllm serve "$MODEL" \
  --served-model-name Qwen3.5-4B \
  --host 127.0.0.1 --port "$PORT" \
  --max-model-len "$LEN" \
  --language-model-only \
  --gpu-memory-utilization "${GPU_UTIL:-0.72}" \
  --enable-auto-tool-choice --tool-call-parser qwen3_coder \
  --reasoning-parser qwen3 \
  --default-chat-template-kwargs '{"enable_thinking": false}' \
  --max-num-seqs "${MAX_SEQS:-64}" \
  --disable-log-requests \
  2>&1 | tee "$LOG"
