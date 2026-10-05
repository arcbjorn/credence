#!/usr/bin/env python3
"""Summarise latency-proxy observations into a dated record.

usage: latency-report.py <observations.jsonl> <record.md> <root> key=value...

The key=value pairs are the run's parameters and go into the header table
as given. Transactions are named from the compiled ABIs in contracts/out,
so a reader sees `repay`, not a selector.
"""

import glob
import json
import statistics
import sys


def names(root):
    sel = {}
    for path in glob.glob(f"{root}/contracts/out/*.sol/*.json"):
        try:
            ids = json.load(open(path)).get("methodIdentifiers") or {}
        except ValueError:
            continue
        for sig, h in ids.items():
            sel.setdefault("0x" + h, sig.split("(")[0])
    return sel


def fmt(ms):
    return f"{ms / 1000:.2f} s" if ms >= 1000 else f"{ms:.0f} ms"


def row(label, values):
    return (f"| {label} | {len(values)} | {fmt(statistics.median(values))} "
            f"| {fmt(min(values))} | {fmt(max(values))} |")


def main():
    src, out, root = sys.argv[1], sys.argv[2], sys.argv[3]
    params = dict(arg.split("=", 1) for arg in sys.argv[4:])
    obs = [json.loads(line) for line in open(src) if line.strip()]
    sel = names(root)

    rtt = next((o["ms"] for o in obs if o["kind"] == "rtt"), [])
    txs = [o for o in obs if o["kind"] == "tx"]
    for t in txs:
        t["name"] = "create" if not t["to"] else sel.get(t["selector"], t["selector"])
    index = [o for o in obs if o["kind"] == "index"]
    engine = [o for o in obs if o["kind"] == "engine"]
    lost = [o for o in obs if o["kind"] in ("send_error", "index_timeout")]
    failed = [t for t in txs if t["status"] != 1]

    head = "| Call | n | median | min | max |\n|---|---|---|---|---|"
    lines = [
        f"# Latency — {params.get('date', '')}",
        "",
        f"Produced by `scripts/measure-latency.sh` at {params.get('stamp', '')}.",
        "",
        "| Field | Value |",
        "|---|---|",
    ]
    for key in ("chain", "rpc", "manager", "account", "loans", "principal", "hold"):
        if key in params:
            lines.append(f"| {key.capitalize()} | `{params[key]}` |")
    if rtt:
        lines.append(f"| Round trip, this machine to the RPC | median {fmt(statistics.median(rtt))} (n={len(rtt)}) |")
    lines += [
        "",
        "Every interval is wall-clock time on the machine that ran the",
        "prototype, taken at the boundary its requests crossed. Each chain",
        "and index figure includes up to one round trip to the RPC, the line",
        "above. With samples this small the minimum and maximum are shown",
        "instead of percentiles.",
        "",
        "## Request to confirmation",
        "",
        "From `eth_sendRawTransaction` reaching the RPC boundary to the",
        "transaction's receipt first being returned.",
        "",
        head,
    ]
    by = {}
    for t in txs:
        by.setdefault(t["name"], []).append(t["ms"])
    for name in sorted(by, key=lambda n: -len(by[n])):
        lines.append(row(f"`{name}`", by[name]))
    if txs:
        lines.append(row("**all**", [t["ms"] for t in txs]))
    lines += ["", "## Event to index", "",
              "From the receipt first being returned to the loan's row in the",
              "Envio index showing the event.", ""]
    if index:
        lines.append(head.replace("Call", "Event"))
        for event in ("Originated", "Repaid"):
            v = [o["ms"] for o in index if o["event"] == event]
            if v:
                lines.append(row(event, v))
        lines.append(row("**all**", [o["ms"] for o in index]))
    elif params.get("index", "skipped") == "skipped":
        lines.append("**SKIP** — no indexer was serving this chain, so nothing was measured.")
    else:
        lines.append("**None observed** — the indexer was reachable, but no loan event was seen.")
    lines += ["", "## Input to assessment", "",
              "From a snapshot reaching the risk service to its assessment",
              "being returned. Off chain: this is the engine and its HTTP",
              "transport on this machine, not the chain.", ""]
    if engine:
        lines.append(head.replace("Call", "Request"))
        lines.append(row("`POST /v1/assess`", [o["ms"] for o in engine]))
        sizes = [o["bytes_in"] for o in engine]
        lines += ["", f"Snapshot size: median {statistics.median(sizes):,.0f} bytes, "
                      f"max {max(sizes):,} bytes."]
    lines += ["", "## Transactions", "",
              "Every transaction the run sent, so each figure can be checked on",
              "chain.", "",
              "| Block | Call | Gas | Confirmation | Status | Hash |",
              "|---|---|---|---|---|---|"]
    for t in sorted(txs, key=lambda t: t["block"]):
        lines.append(f"| {t['block']} | `{t['name']}` | {t['gas']:,} | {fmt(t['ms'])} "
                     f"| {'ok' if t['status'] == 1 else '**reverted**'} | `{t['tx']}` |")
    if failed or lost:
        lines += ["", f"**{len(failed)} reverted, {len(lost)} not observed.** "
                      "These are counted above and listed here rather than dropped."]
        for o in lost:
            lines.append(f"- `{json.dumps(o)}`")
    open(out, "w").write("\n".join(lines) + "\n")
    print(f"record: {out}")


if __name__ == "__main__":
    main()
