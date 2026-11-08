#!/usr/bin/env bash
# Builds the vLLM environment on uv's managed Python. The system Python on this
# WSL distro has no headers, and triton compiles a C extension against them.
set -euo pipefail
export PATH="$HOME/.local/bin:$PATH"
cd "$HOME/stage0"
rm -rf venv
uv python install 3.12
uv venv --python-preference only-managed --python 3.12 venv
. venv/bin/activate
UV_HTTP_TIMEOUT=600 uv pip install -q vllm "huggingface_hub[cli]" 2>&1 | tail -n 3
python -c 'import sysconfig,os;p=sysconfig.get_paths()["include"];print("headers",p,os.path.exists(p+"/Python.h"))'
python -c 'import vllm;print("vllm",vllm.__version__)' 2>&1 | tail -n 1
