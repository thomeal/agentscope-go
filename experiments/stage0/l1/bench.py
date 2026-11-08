#!/usr/bin/env python3
"""L1 throughput sweep against the local vLLM server.

Prompts have the shape the harness produces: a tool schema, about 6K tokens of
synthetic operations text with a per-session prefix shared across rounds, and a
short reply. Standard library only. No network access beyond the given base URL.
"""
import argparse
import concurrent.futures as cf
import json
import random
import statistics
import time
import urllib.error
import urllib.request

VOCAB = "host service latency cache retry shard index queue worker config deploy rollback region volume kernel thread buffer socket timeout replica".split()
TOOLS = [{"type": "function", "function": {"name": "read_local", "description": "Read one local record by id.",
          "parameters": {"type": "object", "properties": {"id": {"type": "string"}}, "required": ["id"]}}}]


TOKENS_PER_WORD = 3.0


def calibrate(base, model):
    """Measures tokens per word on the server, so prompts have the size asked for."""
    global TOKENS_PER_WORD
    sample = " ".join(f"{random.Random(i).choice(VOCAB)}-{random.Random(i).randint(0, 9999)}" for i in range(2000))
    req = urllib.request.Request(base + "/tokenize", data=json.dumps({"model": model, "prompt": sample}).encode(),
                                 headers={"Content-Type": "application/json"})
    with urllib.request.urlopen(req, timeout=60) as resp:
        TOKENS_PER_WORD = json.load(resp)["count"] / 2000


def text(rng, tokens):
    return " ".join(f"{rng.choice(VOCAB)}-{rng.randint(0, 9999)}" for _ in range(int(tokens / TOKENS_PER_WORD)))


def one(base, model, session, round_no, input_tokens, output_tokens, tools):
    rng = random.Random(session)
    prefix = text(rng, int(input_tokens * 0.8))
    tail = text(random.Random(session * 1000 + round_no), int(input_tokens * 0.2))
    body = {
        "model": model,
        "messages": [
            {"role": "system", "content": f"[session-{session}] You are an operations assistant."},
            {"role": "user", "content": prefix},
            {"role": "user", "content": tail + "\nSummarize the notable items in about 200 words."},
        ],
        "max_completion_tokens": output_tokens,
        "temperature": 0,
        "chat_template_kwargs": {"enable_thinking": False},
        "stream": True,
        "stream_options": {"include_usage": True},
    }
    if tools:
        body["tools"] = TOOLS
    req = urllib.request.Request(base + "/v1/chat/completions", data=json.dumps(body).encode(),
                                 headers={"Content-Type": "application/json"})
    start = time.time()
    first = None
    usage = {}
    try:
        with urllib.request.urlopen(req, timeout=900) as resp:
            for raw in resp:
                line = raw.decode("utf-8", "ignore").strip()
                if not line.startswith("data:") or line.endswith("[DONE]"):
                    continue
                chunk = json.loads(line[5:])
                if chunk.get("usage"):
                    usage = chunk["usage"]
                choices = chunk.get("choices") or []
                if first is None and choices and choices[0].get("delta", {}).get("content"):
                    first = time.time()
    except urllib.error.HTTPError as err:
        return {"error": f"HTTP {err.code}: {err.read()[:200]!r}"}
    end = time.time()
    out = usage.get("completion_tokens", 0)
    gen = (end - first) if first else 0
    return {"ttft": (first or end) - start, "total": end - start, "out": out,
            "in": usage.get("prompt_tokens", 0), "cached": (usage.get("prompt_tokens_details") or {}).get("cached_tokens", 0),
            "tps": (out - 1) / gen if gen > 0 and out > 1 else 0}


def sweep(base, model, conc, rounds, input_tokens, output_tokens, tools):
    jobs = [(s, r) for r in range(rounds) for s in range(conc)]
    start = time.time()
    with cf.ThreadPoolExecutor(conc) as pool:
        results = list(pool.map(lambda j: one(base, model, j[0] + 1000 * conc, j[1], input_tokens, output_tokens, tools), jobs))
    wall = time.time() - start
    ok = [r for r in results if "error" not in r]
    errs = [r["error"] for r in results if "error" in r]
    p = lambda xs, q: sorted(xs)[min(len(xs) - 1, int(len(xs) * q))] if xs else 0
    return {
        "concurrency": conc, "requests": len(results), "errors": len(errs), "first_error": errs[0] if errs else "",
        "wall_s": round(wall, 1), "output_tps_total": round(sum(r["out"] for r in ok) / wall, 1),
        "input_tps_total": round(sum(r["in"] for r in ok) / wall, 1),
        "per_stream_tps_p50": round(statistics.median([r["tps"] for r in ok]), 1) if ok else 0,
        "ttft_p50": round(p([r["ttft"] for r in ok], 0.5), 2), "ttft_p95": round(p([r["ttft"] for r in ok], 0.95), 2),
        "total_p50": round(p([r["total"] for r in ok], 0.5), 2), "total_p95": round(p([r["total"] for r in ok], 0.95), 2),
        "avg_in": round(statistics.mean([r["in"] for r in ok])) if ok else 0,
        "avg_cached": round(statistics.mean([r["cached"] for r in ok])) if ok else 0,
        "avg_out": round(statistics.mean([r["out"] for r in ok])) if ok else 0,
    }


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--base", default="http://127.0.0.1:8000")
    ap.add_argument("--model", default="Qwen3.5-4B")
    ap.add_argument("--levels", default="1,2,4,8,16,24,32,48,64")
    ap.add_argument("--rounds", type=int, default=3)
    ap.add_argument("--input", type=int, default=6000)
    ap.add_argument("--output", type=int, default=256)
    ap.add_argument("--no-tools", action="store_true")
    args = ap.parse_args()
    calibrate(args.base, args.model)
    print(json.dumps({"tokens_per_word": TOKENS_PER_WORD}), flush=True)
    for conc in [int(x) for x in args.levels.split(",")]:
        print(json.dumps(sweep(args.base, args.model, conc, args.rounds, args.input, args.output, not args.no_tools)), flush=True)


if __name__ == "__main__":
    main()
