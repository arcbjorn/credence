#!/usr/bin/env bash
# Rest a sell order for notes on the Kuru market.
#
# `docs/12` keeps "a note is tradable debt exposure" unearned because
# every holder so far is this project. The support it names is someone
# independent buying or selling one. This is the project's half of that
# trade. The buyer's half is `scripts/note-buy.sh`, which is a single
# file that runs without this repository, so it can be sent to someone
# who has no access to it.
#
# No order had been placed on this market before: `list-notes-kuru.sh`
# moved the notes by direct transfer. Both halves were run first against
# a local fork of Monad testnet (`anvil --fork-url`), where a 10-note ask
# at 1.00 filled a 5 tCRED market buy for 4.985 notes, which is 5 less
# the market's 30 bps taker fee.
#
# Notes that do not sell stay on the book, and the tCRED from a fill
# lands in the seller's Kuru margin account, not its wallet.
set -euo pipefail

RPC=${RPC:-https://testnet-rpc.monad.xyz}
ROOT=$(cd "$(dirname "$0")/.." && pwd)
CHAIN_ID=$(cast chain-id --rpc-url "$RPC")

# Kuru's margin account on Monad testnet, verified by scripts/probe-kuru.sh.
MARGIN=${KURU_MARGIN_ACCOUNT:-0xd029C2D98ff85D8F64799017fE00a59B1159CE02}

NOTES=${NOTES:-10} # whole notes to offer
PRICE=${PRICE:-1}  # tCRED per note

. "$ROOT/scripts/lib/keys.sh"
KEY=${CREDENCE_AGENT_KEY:-}
[ -n "$KEY" ] || { echo "set CREDENCE_AGENT_KEY (the address that holds the notes)" >&2; exit 1; }
credence_require_private_key "$KEY" "$CHAIN_ID" "note seller" CREDENCE_AGENT_KEY
KEY=0x${KEY#0x}
SELLER=$(cast wallet address --private-key "$KEY")

MANIFEST="$ROOT/contracts/deployments/$CHAIN_ID.json"
[ -f "$MANIFEST" ] || { echo "no manifest for chain $CHAIN_ID" >&2; exit 1; }
KURU=$(python3 -c "
import json, sys
k = json.load(open(sys.argv[1]))['kuru']
print(k['market'], k['note'])" "$MANIFEST")
read -r MARKET NOTE <<<"$KURU"

PARAMS=$(cast call "$MARKET" \
  'getMarketParams()(uint32,uint96,address,uint256,address,uint256,uint32,uint96,uint96,uint256,uint256)' \
  --rpc-url "$RPC" | awk '{print $1}')
HELD=$(cast call "$NOTE" 'balanceOf(address)(uint256)' "$SELLER" --rpc-url "$RPC" | awk '{print $1}')

# Kuru counts price in units of `pricePrecision` per whole quote token and
# size in units of `sizePrecision` per whole base token; the deposit is in
# the note's own decimals. Decimal arithmetic, so PRICE=0.95 is exact.
# A command substitution rather than `< <(...)`, so a refusal below stops
# the script instead of leaving these empty.
ORDER=$(python3 - "$PARAMS" "$NOTES" "$PRICE" "$HELD" <<'PY'
import sys
from decimal import Decimal
p = sys.argv[1].split()
price_prec, size_prec, base_dec, tick, min_size, max_size = (
    int(p[0]), int(p[1]), int(p[3]), int(p[6]), int(p[7]), int(p[8]))
notes, price, held = Decimal(sys.argv[2]), Decimal(sys.argv[3]), int(sys.argv[4])
price_units = price * price_prec
size = notes * size_prec
raw = notes * 10**base_dec
if price_units != int(price_units) or int(price_units) % tick or price_units <= 0:
    sys.exit(f"PRICE {price} is not a whole number of ticks")
if size != int(size) or not (min_size <= size <= max_size):
    sys.exit(f"NOTES {notes} is outside the market's order size limits")
if raw > held:
    sys.exit(f"seller holds {held / 10**base_dec} notes, fewer than {notes}")
print(int(price_units), int(size), int(raw), held / 10**base_dec)
PY
)
read -r PRICE_UNITS SIZE RAW HELD_NOTES <<<"$ORDER"

cat <<INFO
--- note ask ---
  chain    $CHAIN_ID
  market   $MARKET
  seller   $SELLER (holds $HELD_NOTES notes)
  offer    $NOTES notes at $PRICE tCRED each
INFO

cast send "$NOTE" 'approve(address,uint256)' "$MARGIN" "$RAW" \
  --private-key "$KEY" --rpc-url "$RPC" >/dev/null
cast send "$MARGIN" 'deposit(address,address,uint256)' "$SELLER" "$NOTE" "$RAW" \
  --private-key "$KEY" --rpc-url "$RPC" >/dev/null
# Post-only, so the order rests on the book rather than crossing a bid.
# With --json, cast reports a revert on stdout, so capture it and show it.
if ! OUT=$(cast send "$MARKET" 'addSellOrder(uint32,uint96,bool)' "$PRICE_UNITS" "$SIZE" true \
  --private-key "$KEY" --rpc-url "$RPC" --json 2>&1); then
  echo "the order reverted; the notes are in the margin account at $MARGIN" >&2
  echo "$OUT" | tail -3 >&2
  exit 1
fi
TX=$(echo "$OUT" | python3 -c 'import json, sys; print(json.load(sys.stdin)["transactionHash"])')

ASK=$(cast call "$MARKET" 'bestBidAsk()(uint256,uint256)' --rpc-url "$RPC" | sed -n 2p | awk '{print $1}')
echo "  best ask $(python3 -c "print($ASK / 10**18)") tCRED"
echo "  order tx $TX"
echo
echo "Send scripts/note-buy.sh to the buyer. They need Foundry and some"
echo "testnet MON for gas, and they mint their own tCRED."
