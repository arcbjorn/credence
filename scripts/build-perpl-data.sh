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
for key, snap in snaps.items():
    if (snap.get("context") or {}).get("kind") == "Origination":
        json.dump(snap, open(sys.argv[2], "w"))
        print(f"  snapshot    {key[:12]} (origination)")
        break
else:
    raise SystemExit("  no origination snapshot in the run")
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
