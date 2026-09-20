#!/usr/bin/env bash
# Drive the real Perpl providers against the real venue.
#
# `scripts/probe-perpl.sh` establishes what the API does. This proves the
# GATEWAY'S OWN CODE consumes it: the same `market.PerplProvider` and
# `market.PerplDepthProvider` the scheduler would build, pointed at
# app.perpl.xyz, producing the `evidence.MarketState` the engine reads.
#
# `docs/08` requires that "an integration is complete only when the
# submitted environment demonstrates its actual behavior". Unit tests run
# against httptest fixtures I wrote, so they prove the parsing matches my
# belief about the venue -- not that my belief is right. This closes that
# gap, and it is the check that would catch Perpl changing its response
# shape under us.
#
# Read-only and unauthenticated: no key, no funds, no transaction.
set -euo pipefail

ROOT=$(cd "$(dirname "$0")/.." && pwd)
BASE=${PERPL_BASE:-https://app.perpl.xyz/api}
MARKET=${PERPL_MARKET:-20}

echo "=== gateway market providers against $BASE ==="
echo

cd "$ROOT/gateway"
go run ./cmd/credence-perplcheck \
  -base "$BASE" \
  -market "$MARKET" \
  "$@"
