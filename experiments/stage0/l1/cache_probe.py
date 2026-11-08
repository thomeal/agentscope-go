"""L1 cloud checks the smoke runs cannot cover: a fork on a 150K prefix and the default
tier's TTL. Writes one JSON line per request to the given file. Never prints the key.

Usage: cache_probe.py <env file> <out.jsonl> [wait_minutes ...]
"""
import json
import random
import string
import sys
import time
import urllib.request

ENV, OUT = sys.argv[1], sys.argv[2]
WAITS = [float(x) for x in sys.argv[3:]] or [31.0]
key = next(l.split("=", 1)[1].strip().strip('"') for l in open(ENV, encoding="utf-8") if l.startswith("OPENAI_API_KEY="))

rng = random.Random(20261107)
marker = "[stage0 l1 cache probe %d]" % int(time.time())


def line():
    code = "".join(rng.choices(string.ascii_uppercase + string.digits, k=3)) + "-" + "".join(rng.choices(string.digits, k=4))
    return "Log entry %06d for the build farm reports code %s and %d percent load." % (rng.randrange(10**6), code, rng.randrange(100))


# About 150K tokens across twenty tool-free messages, so the prefix has a realistic shape.
history = []
for i in range(20):
    history.append({"role": "user", "content": "Record %d:\n" % i + "\n".join(line() for _ in range(220))})
    history.append({"role": "assistant", "content": "Noted record %d." % i})
base = {"model": "gpt-6-luna", "store": False, "instructions": marker + " You are a careful assistant. Keep replies short.", "include": ["reasoning.encrypted_content"]}


def call(tag, tail, limit):
    body = dict(base)
    body["input"] = history + tail
    body["max_output_tokens"] = limit
    req = urllib.request.Request("https://api.openai.com/v1/responses", data=json.dumps(body).encode(),
                                 headers={"Authorization": "Bearer " + key, "Content-Type": "application/json"})
    t = time.time()
    try:
        resp = json.load(urllib.request.urlopen(req, timeout=600))
    except urllib.error.HTTPError as e:
        row = {"tag": tag, "http": e.code, "error": e.read().decode()[:300]}
    else:
        u = resp["usage"]
        d = u.get("input_tokens_details", {})
        row = {"tag": tag, "http": 200, "status": resp["status"], "secs": round(time.time() - t, 1),
               "input": u["input_tokens"], "cached": d.get("cached_tokens"), "write": d.get("cache_write_tokens"),
               "output": u["output_tokens"], "reasoning": u.get("output_tokens_details", {}).get("reasoning_tokens")}
    row["ts"] = time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime())
    with open(OUT, "a", encoding="utf-8") as f:
        f.write(json.dumps(row) + "\n")
    print(row, flush=True)
    return row


ask = [{"role": "user", "content": "Reply with the single word ok."}]
call("main-first", ask, 200)
time.sleep(5)
fork = [{"role": "user", "content": "Summarize the build farm load in one sentence."}]
call("fork-150k", fork, 2000)
for _ in range(4):
    call("fork-150k-again", fork, 2000)
for wait in WAITS:
    time.sleep(wait * 60)
    call("after-%gm" % wait, fork, 2000)
