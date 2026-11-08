#!/usr/bin/env python3
"""First-token latency and energy against prompt size, with nothing else on the GPU.

One request at a time, in pairs: a new prompt (cold) and the same prefix again (warm). Power comes from
nvidia-smi every 200 ms, and a request's energy is the sum over its span, with and without the idle draw.
Run it on an idle server. Standard library only. The API key is read from the env file and never printed.
"""
import argparse
import concurrent.futures as cf
import json
import os
import random
import statistics
import subprocess
import threading
import time
import urllib.error
import urllib.request

import bench


class Power:
    """Samples GPU power in the background."""

    def __init__(self):
        self.samples = []
        self.proc = subprocess.Popen(
            ["nvidia-smi", "--query-gpu=power.draw,utilization.gpu", "--format=csv,noheader,nounits", "-lms", "200"],
            stdout=subprocess.PIPE, text=True)
        threading.Thread(target=self._read, daemon=True).start()

    def _read(self):
        for line in self.proc.stdout:
            try:
                watts, util = [float(x) for x in line.strip().split(",")[:2]]
            except ValueError:
                continue
            self.samples.append((time.time(), watts, util))

    def between(self, start, end):
        return [s for s in self.samples if start <= s[0] <= end]

    def joules(self, start, end, idle=0.0):
        rows = self.between(start, end)
        if not rows:
            return None
        return sum(max(w - idle, 0.0) for _, w, _ in rows) * (end - start) / len(rows)

    def stop(self):
        self.proc.terminate()


def timed(fn, power, idle):
    start = time.time()
    result = fn()
    end = time.time()
    time.sleep(0.4)  # the last samples of this span arrive after it ends
    result["span"] = end - start
    result["joules"] = power.joules(start, end)
    result["joules_net"] = power.joules(start, end, idle)
    return result


def local(args, power, out):
    bench.calibrate(args.base, args.model)
    begin = time.time()
    time.sleep(args.idle_seconds)
    rows = power.between(begin, time.time())
    idle = statistics.median(w for _, w, _ in rows)
    out.write(json.dumps({"kind": "idle", "watts": idle, "samples": len(rows)}) + "\n")
    out.flush()
    print("idle draw %.1f W over %d samples" % (idle, len(rows)), flush=True)
    session = 900000
    for size in [int(x) for x in args.sizes.split(",")]:
        for rep in range(args.reps):
            session += 1
            for state, round_no in (("cold", 0), ("warm", 1)):
                r = timed(lambda: bench.one(args.base, args.model, session, round_no, size, args.output, True), power, idle)
                r.update(kind="local", size=size, rep=rep, state=state)
                out.write(json.dumps(r) + "\n")
                out.flush()
        print("local size %d done" % size, flush=True)
    for conc in [int(x) for x in args.levels.split(",")]:
        size = 3000
        session += 1000
        base_session = session
        jobs = [(base_session + s, r) for r in range(3) for s in range(conc)]
        start = time.time()
        with cf.ThreadPoolExecutor(conc) as pool:
            results = list(pool.map(lambda j: bench.one(args.base, args.model, j[0], j[1], size, args.output, True), jobs))
        end = time.time()
        time.sleep(0.4)
        ok = [r for r in results if "error" not in r]
        rec = {"kind": "load", "concurrency": conc, "wall": end - start, "requests": len(ok),
               "in": sum(r["in"] for r in ok), "out": sum(r["out"] for r in ok), "cached": sum(r["cached"] for r in ok),
               "joules": power.joules(start, end), "joules_net": power.joules(start, end, idle),
               "ttft_p50": statistics.median(r["ttft"] for r in ok) if ok else None,
               "mean_watts": statistics.mean(w for _, w, _ in power.between(start, end))}
        out.write(json.dumps(rec) + "\n")
        out.flush()
        print("load %d: %.0f W, ttft p50 %.2f" % (conc, rec["mean_watts"], rec["ttft_p50"] or 0), flush=True)


def read_key(path):
    for line in open(path, encoding="utf-8"):
        line = line.strip()
        if line.startswith("OPENAI_API_KEY="):
            return line.split("=", 1)[1].strip().strip('"').strip("'")
    raise SystemExit("no OPENAI_API_KEY in the env file")


def cloud_once(key, model, text, max_output):
    body = {"model": model, "input": [{"role": "user", "content": text + "\nSummarize the notable items in about 100 words."}],
            "stream": True, "store": False, "max_output_tokens": max_output}
    req = urllib.request.Request("https://api.openai.com/v1/responses", data=json.dumps(body).encode(),
                                 headers={"Content-Type": "application/json", "Authorization": "Bearer " + key})
    start = time.time()
    first_any = first_text = None
    usage = {}
    status = ""
    with urllib.request.urlopen(req, timeout=300) as resp:
        for raw in resp:
            line = raw.decode("utf-8", "ignore").strip()
            if not line.startswith("data:"):
                continue
            ev = json.loads(line[5:])
            kind = ev.get("type", "")
            if first_any is None and kind.endswith(".delta"):
                first_any = time.time()
            if first_text is None and kind == "response.output_text.delta":
                first_text = time.time()
            # A reply cut off by its limit ends as incomplete, and still reports usage.
            if kind in ("response.completed", "response.incomplete"):
                usage = ev["response"].get("usage") or {}
                status = ev["response"].get("status", "")
    end = time.time()
    details = usage.get("input_tokens_details") or {}
    return {"ttft": first_text - start if first_text else None, "first_delta": first_any - start if first_any else None,
            "total": end - start, "status": status,
            "in": usage.get("input_tokens", 0), "cached": details.get("cached_tokens", 0), "out": usage.get("output_tokens", 0)}


def cloud(args, out):
    key = read_key(args.env)
    sid = random.randrange(10 ** 9)
    for size in [int(x) for x in args.cloud_sizes.split(",")]:
        for rep in range(args.cloud_reps):
            sid += 1
            text = bench.text(random.Random(sid), size)
            for state in ("cold", "warm"):
                r = None
                for attempt in range(4):
                    try:
                        r = cloud_once(key, args.cloud_model, "[" + str(sid) + "] " + text, args.cloud_output)
                        break
                    except (urllib.error.URLError, OSError) as err:
                        print("attempt %d failed: %s" % (attempt + 1, err), flush=True)
                        time.sleep(5 * (attempt + 1))
                if r is None:
                    continue
                r.update(kind="cloud", size=size, rep=rep, state=state)
                out.write(json.dumps(r) + "\n")
                out.flush()
                time.sleep(1)
        print("cloud size %d done" % size, flush=True)


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--base", default="http://127.0.0.1:8000")
    ap.add_argument("--model", default="Qwen3.5-4B")
    ap.add_argument("--sizes", default="1000,2000,4000,6000")
    ap.add_argument("--reps", type=int, default=8)
    ap.add_argument("--output", type=int, default=256)
    ap.add_argument("--levels", default="8,28")
    ap.add_argument("--idle-seconds", type=int, default=30)
    ap.add_argument("--cloud-model", default="gpt-6-luna")
    ap.add_argument("--cloud-sizes", default="2000,8000,16000,32000")
    ap.add_argument("--cloud-reps", type=int, default=5)
    ap.add_argument("--cloud-output", type=int, default=1500, help="room for reasoning as well as the reply")
    ap.add_argument("--only-cloud", action="store_true")
    ap.add_argument("--env", required=True)
    ap.add_argument("--out", required=True)
    args = ap.parse_args()
    os.makedirs(args.out, exist_ok=True)
    with open(os.path.join(args.out, "calib.jsonl"), "a", encoding="utf-8") as out:
        if args.only_cloud:
            bench.calibrate(args.base, args.model)
        else:
            power = Power()
            local(args, power, out)
            power.stop()
        cloud(args, out)
    print("done", flush=True)


if __name__ == "__main__":
    main()
