#!/usr/bin/env bash
# Starts vLLM detached from the caller. Usage: start.sh <max-model-len> [port]
LEN="${1:-8192}"
PORT="${2:-8000}"
cd "$(dirname "$0")"
GPU_UTIL="${GPU_UTIL:-0.72}" MAX_SEQS="${MAX_SEQS:-64}" nohup setsid bash serve.sh "$LEN" "$PORT" > "$HOME/stage0/logs/serve-${LEN}.out" 2>&1 < /dev/null &
disown
echo "started pid $!"
