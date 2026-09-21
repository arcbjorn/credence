#!/usr/bin/env bash
# Capture live Perpl market data into the web bundle.
#
# `docs/09` requires every displayed number to come from a retained
# fixture or a transaction-derived record, and `scripts/build-web-data.sh`
# enforces it structurally: the run produced the file, or the screen
# renders nothing. A page that fetched Perpl itself would show nothing in
# the submitted static folder, and "works while the venue is up" is not
# evidence.
#
# So this writes what the venue actually served, alongside the snapshot
# the browser re-evaluates it against. Re-run it to refresh the panel.
#
# Read-only and unauthenticated: no key, no funds, no transaction.
set -euo pipefail

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
OUT="$ROOT/web/public/data/perpl.json"
RUN=${RUN:-repay}

BASE=${PERPL_BASE:-https://app.perpl.xyz/api}
MARKET=${PERPL_MARKET:-20}
SYMBOL=${PERPL_SYMBOL:-ETH}

# 60s candles over 6 hours: 360 bars, under the venue's ~1000 cap, and
# fresh enough for a realistic skew budget. A coarser resolution reads up
# to a full candle stale, because a candle is stamped at its OPEN.
RESOLUTION=${PERPL_RESOLUTION:-60}
WINDOW=${PERPL_WINDOW:-21600}

SOURCE="$ROOT/web/public/data/$RUN.json"
[ -f "$SOURCE" ] || {
  echo "no $SOURCE -- run scripts/build-web-data.sh first" >&2
  exit 1
}

# The panel re-evaluates a REAL retained snapshot rather than a shape
# invented here: what it shows is the effect of today's market on a
# decision that actually happened.
SNAP=$(mktemp)
trap 'rm -f "$SNAP"' EXIT
python3 - "$SOURCE" "$SNAP" <<'PY'
import json, sys

run = json.load(open(sys.argv[1]))
snaps = run.get("snapshots") or {}

# An origination snapshot, because the stress scenarios size a principal
# there. A monitoring snapshot would stress an existing book instead,
# which is a different and less legible question for this panel.
#
# Among those, one whose assessment was bound by StressCapacity.
# Capacity is a min() over several constraints and only the stress term
# reads market evidence, so on a history-bound snapshot the panel shows
# two identical capacities and proves nothing about the market. `snaps`
# is a dict with no meaningful order, so without this the choice is
# arbitrary -- and loan 1 IS history-bound, which is what the 21
# September build picked.
origination = {
    k: v for k, v in snaps.items()
    if (v.get("context") or {}).get("kind") == "Origination"
}
if not origination:
    raise SystemExit("  no origination snapshot in the run")

# Which snapshot each assessment consumed, and what bound it.
bound = {}
for a in run.get("assessments") or []:
    cap = ((a.get("payload") or {}).get("outcome") or {}).get("capacity") or {}
    if cap.get("agent_binding") == "StressCapacity":
        bound[a.get("input_hash")] = True

key = next((k for k in origination if bound.get(k)), None)
why = "stress-bound"
if key is None:
    # Still usable: the page reads the binding constraint and says so
    # rather than claiming the market had no effect.
    key = sorted(origination)[0]
    why = "no stress-bound origination; the panel will name the binding"

json.dump(origination[key], open(sys.argv[2], "w"))
print(f"  snapshot    {key[:12]} ({why})")
PY

cd "$ROOT/gateway"
go run ./cmd/credence-perplpanel \
  -base "$BASE" \
  -market "$MARKET" \
  -symbol "$SYMBOL" \
  -resolution "$RESOLUTION" \
  -window "$WINDOW" \
  -snapshot "$SNAP" \
  -snapshot-label "$RUN" \
  -out "$OUT"

echo
echo "PASS  the Perpl panel has live data"
echo
echo "  The page evaluates both snapshots with the same WebAssembly engine"
echo "  the gateway uses, so the scenarios it shows are the engine's own"
echo "  output. Only the market evidence differs between the two columns."
