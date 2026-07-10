#!/usr/bin/env python3
"""Aggregate bench-compaction results + judgments into a comparison report."""
import json
import glob
import os
from collections import defaultdict

DIR = os.path.dirname(os.path.abspath(__file__))
SIZES = ["S", "M", "L", "XL"]


def load(pattern):
    out = {}
    for f in glob.glob(os.path.join(DIR, pattern)):
        with open(f) as fh:
            d = json.load(fh)
        out[d["session_id"]] = d
    return out


def pct(values, p):
    values = sorted(values)
    if not values:
        return 0
    k = min(len(values) - 1, int(round(p / 100 * (len(values) - 1))))
    return values[k]


def main():
    results = load("results/*.json")
    judgments = load("judgments/*.json")
    models = sorted({r["model"] for res in results.values() for r in res["runs"]})

    perf = defaultdict(lambda: defaultdict(list))  # model -> metric -> [values]
    perf_by_size = defaultdict(lambda: defaultdict(list))  # (model,size) -> metric
    quality = defaultdict(lambda: defaultdict(list))
    wins = defaultdict(int)
    errors = defaultdict(list)
    n_judged = 0

    for sid, res in results.items():
        for r in res["runs"]:
            m = r["model"]
            if r.get("error"):
                errors[m].append((sid, r["error"]))
                continue
            perf[m]["total_s"].append(r["total_seconds"])
            perf[m]["ttft_s"].append(r["ttft_seconds"])
            perf[m]["cost"].append(r["cost_usd"])
            perf[m]["out_tok"].append(r["output_tokens"])
            perf[m]["in_tok"].append(r["input_tokens"])
            perf[m]["reasoning_tok"].append(r.get("reasoning_tokens", 0))
            if r["total_seconds"] > 0:
                perf[m]["tok_per_s"].append(r["output_tokens"] / r["total_seconds"])
            perf_by_size[(m, res["size"])]["total_s"].append(r["total_seconds"])
            perf_by_size[(m, res["size"])]["cost"].append(r["cost_usd"])
            perf_by_size[(m, res["size"])]["sum_len"].append(len(r["summary"]))

    judge_cost = 0.0
    for sid, j in judgments.items():
        if j.get("error"):
            continue
        n_judged += 1
        judge_cost += j.get("cost_usd", 0)
        if j.get("best"):
            wins[j["best"]] += 1
        for model, s in j["scores"].items():
            for dim in ("coverage", "accuracy", "continuation", "no_hallucination"):
                quality[model][dim].append(s[dim])
            quality[model]["overall"].append(
                (s["coverage"] + s["accuracy"] + s["continuation"] + s["no_hallucination"]) / 4
            )

    print(f"# Compaction model comparison — {len(results)} sessions, {n_judged} judged")
    print(f"Judge: claude-opus-4.8 (cost ${judge_cost:.2f})\n")

    print("## Speed & cost (successful runs)")
    hdr = f"{'model':<14}{'n':>4}{'med s':>8}{'p90 s':>8}{'med ttft':>10}{'tok/s':>8}{'med $':>10}{'total $':>10}"
    print(hdr)
    for m in models:
        p = perf[m]
        n = len(p["total_s"])
        print(
            f"{m:<14}{n:>4}{pct(p['total_s'],50):>8.1f}{pct(p['total_s'],90):>8.1f}"
            f"{pct(p['ttft_s'],50):>10.1f}{pct(p['tok_per_s'],50):>8.0f}"
            f"{pct(p['cost'],50):>10.5f}{sum(p['cost']):>10.4f}"
        )

    print("\n## Median latency (s) by session size")
    print(f"{'model':<14}" + "".join(f"{s:>8}" for s in SIZES))
    for m in models:
        print(f"{m:<14}" + "".join(f"{pct(perf_by_size[(m,s)]['total_s'],50):>8.1f}" for s in SIZES))

    print("\n## Total cost ($) by session size")
    print(f"{'model':<14}" + "".join(f"{s:>10}" for s in SIZES))
    for m in models:
        print(f"{m:<14}" + "".join(f"{sum(perf_by_size[(m,s)]['cost']):>10.4f}" for s in SIZES))

    print("\n## Median summary length (chars) by session size")
    print(f"{'model':<14}" + "".join(f"{s:>8}" for s in SIZES))
    for m in models:
        print(f"{m:<14}" + "".join(f"{pct(perf_by_size[(m,s)]['sum_len'],50):>8.0f}" for s in SIZES))

    print("\n## Quality (opus judge, 1-10)")
    print(f"{'model':<14}{'coverage':>10}{'accuracy':>10}{'contin.':>10}{'no-halluc':>11}{'overall':>9}{'wins':>6}")
    for m in models:
        q = quality[m]
        avg = lambda k: sum(q[k]) / len(q[k]) if q[k] else 0
        print(
            f"{m:<14}{avg('coverage'):>10.2f}{avg('accuracy'):>10.2f}{avg('continuation'):>10.2f}"
            f"{avg('no_hallucination'):>11.2f}{avg('overall'):>9.2f}{wins[m]:>6}"
        )

    if any(errors.values()):
        print("\n## Errors")
        for m in models:
            for sid, e in errors[m]:
                print(f"  {m} {sid}: {e[:120]}")


if __name__ == "__main__":
    main()
