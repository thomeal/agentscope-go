#!/usr/bin/env python3
"""L1 behaviour checks against the local vLLM server: the window, the overflow
error, tool calls, foreign tool call ids, and a capability baseline. Standard
library only. Prints one JSON object per check."""
import argparse
import concurrent.futures as cf
import json
import random
import urllib.error
import urllib.request

TOOLS = [{"type": "function", "function": {"name": "read_local", "description": "Read one local record by id.",
          "parameters": {"type": "object", "properties": {"id": {"type": "string"}}, "required": ["id"]}}}]


def post(base, body, path="/v1/chat/completions"):
    req = urllib.request.Request(base + path, data=json.dumps(body).encode(), headers={"Content-Type": "application/json"})
    try:
        with urllib.request.urlopen(req, timeout=600) as resp:
            return resp.status, json.load(resp)
    except urllib.error.HTTPError as err:
        return err.code, {"raw": err.read().decode("utf-8", "ignore")}


def chat(base, model, messages, **kw):
    body = {"model": model, "messages": messages, "temperature": 0, "chat_template_kwargs": {"enable_thinking": False}}
    body.update(kw)
    return post(base, body)


def filler(tokens, per_word=7):
    # Comfortably wrong on purpose is not useful here: size by the server's count.
    rng = random.Random(tokens)
    return " ".join(f"item-{rng.randint(0, 99999)}" for _ in range(tokens // per_word))


def check_window(base, model):
    out = []
    for target in (6000, 7800, 8192, 9000, 16000):
        status, data = chat(base, model, [{"role": "user", "content": filler(target) + "\nReply with OK."}], max_completion_tokens=16)
        row = {"target": target, "status": status}
        if status == 200:
            row["prompt_tokens"] = data["usage"]["prompt_tokens"]
            row["finish"] = data["choices"][0]["finish_reason"]
        else:
            row["error"] = data.get("raw", "")[:300]
        out.append(row)
    print(json.dumps({"check": "window", "rows": out}), flush=True)


def check_tools(base, model, n=40):
    def one(i):
        rid = f"L-{i:03d}"
        status, data = chat(base, model, [
            {"role": "system", "content": "You are an operations assistant. Use tools when asked."},
            {"role": "user", "content": f"Call read_local with id {rid}."},
        ], tools=TOOLS, max_completion_tokens=200)
        if status != 200:
            return {"ok": False, "why": f"http {status}"}
        msg = data["choices"][0]["message"]
        calls = msg.get("tool_calls") or []
        if not calls:
            return {"ok": False, "why": "no call", "content": (msg.get("content") or "")[:120]}
        args = json.loads(calls[0]["function"]["arguments"] or "{}")
        return {"ok": calls[0]["function"]["name"] == "read_local" and args.get("id") == rid, "id": calls[0].get("id", "")}
    with cf.ThreadPoolExecutor(8) as pool:
        rows = list(pool.map(one, range(n)))
    ok = sum(1 for r in rows if r["ok"])
    print(json.dumps({"check": "tool_calls", "ok": ok, "n": n, "sample_id": next((r.get("id") for r in rows if r.get("id")), ""),
                      "failures": [r for r in rows if not r["ok"]][:3]}), flush=True)


def check_foreign_ids(base, model):
    # The cloud's item ids start with fc_. A shared history replays them here.
    out = []
    for cid in ("fc_abc123", "call_abc123", "abc"):
        status, data = chat(base, model, [
            {"role": "user", "content": "Read record L-1."},
            {"role": "assistant", "content": "", "tool_calls": [{"id": cid, "type": "function", "function": {"name": "read_local", "arguments": "{\"id\":\"L-1\"}"}}]},
            {"role": "tool", "tool_call_id": cid, "content": "RECORD L-1. The release code is K7P-4242."},
            {"role": "user", "content": "What is the release code? One short sentence."},
        ], tools=TOOLS, max_completion_tokens=60)
        row = {"id": cid, "status": status}
        if status == 200:
            row["content"] = (data["choices"][0]["message"].get("content") or "")[:80]
        else:
            row["error"] = data.get("raw", "")[:200]
        out.append(row)
    print(json.dumps({"check": "foreign_ids", "rows": out}), flush=True)


def check_capability(base, model, n=40):
    # Baseline: the fact is right there in a short context. If this is low, the
    # local model cannot answer at all and context strategy is not the issue.
    rng = random.Random(7)

    def code():
        return f"{rng.choice('ABCDEFGH')}{rng.randint(0, 9)}{rng.choice('PQRSTUVW')}-{rng.randint(1000, 9999)}"

    facts = [(f"service-{i}", code()) for i in range(n)]
    def one(i):
        block = "\n".join(f"The rollback token for {name} is {val}." for name, val in random.Random(i).sample(facts, 8) + [facts[i]])
        prompt = (f"Records:\n{block}\n\nAnswer as JSON only: {{\"answers\":[{{\"id\":\"q1\",\"answer\":\"<code or UNKNOWN>\"}}]}}\n"
                  f"q1: What is the rollback token for {facts[i][0]}?")
        status, data = chat(base, model, [{"role": "user", "content": prompt}], max_completion_tokens=80)
        if status != 200:
            return False
        return facts[i][1] in (data["choices"][0]["message"].get("content") or "")
    def unknown(i):
        prompt = ("Records:\nThe rollback token for service-a is A1P-1000.\n\nAnswer as JSON only: {\"answers\":[{\"id\":\"q1\",\"answer\":\"<code or UNKNOWN>\"}]}\n"
                  f"q1: What is the rollback token for service-z{i}? If it is not in the records, answer UNKNOWN.")
        status, data = chat(base, model, [{"role": "user", "content": prompt}], max_completion_tokens=80)
        return status == 200 and "UNKNOWN" in (data["choices"][0]["message"].get("content") or "")
    with cf.ThreadPoolExecutor(8) as pool:
        known = list(pool.map(one, range(n)))
        unk = list(pool.map(unknown, range(20)))
    print(json.dumps({"check": "capability", "known_ok": sum(known), "known_n": n, "unknown_ok": sum(unk), "unknown_n": 20}), flush=True)


def check_thinking(base, model):
    status, data = chat(base, model, [{"role": "user", "content": "What is 17 * 23? Reply with the number."}], max_completion_tokens=200)
    msg = data["choices"][0]["message"] if status == 200 else {}
    print(json.dumps({"check": "thinking_off", "status": status, "content": (msg.get("content") or "")[:80],
                      "reasoning": str(msg.get("reasoning") or msg.get("reasoning_content") or "")[:80],
                      "tokens": data.get("usage", {}).get("completion_tokens")}), flush=True)


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--base", default="http://127.0.0.1:8000")
    ap.add_argument("--model", default="Qwen3.5-4B")
    args = ap.parse_args()
    check_thinking(args.base, args.model)
    check_window(args.base, args.model)
    check_tools(args.base, args.model)
    check_foreign_ids(args.base, args.model)
    check_capability(args.base, args.model)


if __name__ == "__main__":
    main()
