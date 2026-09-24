#!/usr/bin/env bash
# Buy Credence notes on Kuru, Monad testnet, with your own wallet.
#
# A single file on purpose: it needs Foundry's `cast` and nothing from the
# Credence repository, so it can be handed to someone who has never seen
# it. That person is the point. Every note holder so far is the project
# itself, and `docs/12` keeps "a note is tradable debt exposure" unearned
# until someone independent buys or sells one.
#
#   export BUYER_KEY=<your private key>
#   bash note-buy.sh               # spends 5 tCRED
#   SPEND=20 bash note-buy.sh
#
# It signs three transactions and nothing else:
#   1. mint SPEND tCRED to your address. tCRED is a test token that anyone
#      can mint, and it is worth nothing;
#   2. approve the market to take that tCRED;
#   3. market-buy notes, reverting if the fill returns less than it
#      should at the current best ask (SLIPPAGE_BPS, default 100).
#
# The key stays on your machine. A fresh one is fine: `cast wallet new`,
# then send it a little MON from https://faucet.monad.xyz for gas.
set -euo pipefail

RPC=${RPC:-https://testnet-rpc.monad.xyz}
# From the `kuru` block of contracts/deployments/10143.json.
MARKET=${MARKET:-0x874160CaFa8B1C4D4Ffbd8f1121c8d7491cdE38E}
NOTE=${NOTE:-0xbeAF9CB5Ac98659A0e44d6118ba5Fd02d43E631F}
TCRED=${TCRED:-0x6b3597BFE78eAcf33d0C3D1bC5d12feD9e09c131}
SPEND=${SPEND:-5}
SLIPPAGE_BPS=${SLIPPAGE_BPS:-100}

command -v cast >/dev/null || {
  echo "install Foundry first: curl -L https://foundry.paradigm.xyz | bash && foundryup" >&2
  exit 1
}
KEY=${BUYER_KEY:-}
[ -n "$KEY" ] || { echo "set BUYER_KEY to your private key" >&2; exit 1; }
KEY=0x${KEY#0x}
BUYER=$(cast wallet address --private-key "$KEY")

[ "$(cast chain-id --rpc-url "$RPC")" = 10143 ] || { echo "$RPC is not Monad testnet" >&2; exit 1; }
[ "$(cast balance "$BUYER" --rpc-url "$RPC")" != 0 ] || {
  echo "$BUYER has no MON for gas: get some from https://faucet.monad.xyz" >&2
  exit 1
}

# The second value is the best ask, as tCRED per note scaled by 1e18.
# Zero means nothing is on offer.
ASK=$(cast call "$MARKET" 'bestBidAsk()(uint256,uint256)' --rpc-url "$RPC" | sed -n 2p | awk '{print $1}')
[ "$ASK" != 0 ] || { echo "no notes are on offer right now: ask the seller to run note-ask.sh" >&2; exit 1; }

PARAMS=$(cast call "$MARKET" \
  'getMarketParams()(uint32,uint96,address,uint256,address,uint256,uint32,uint96,uint96,uint256,uint256)' \
  --rpc-url "$RPC" | awk '{print $1}')
PRICE_PREC=$(echo "$PARAMS" | sed -n 1p)
NOTE_DEC=$(echo "$PARAMS" | sed -n 4p)
TCRED_DEC=$(echo "$PARAMS" | sed -n 6p)
TAKER_BPS=$(echo "$PARAMS" | sed -n 10p)

# The buy amount is counted in `pricePrecision` units per tCRED; the mint
# and approval in tCRED's own decimals. The floor is what SPEND buys at
# the best ask after the taker fee, less SLIPPAGE_BPS, in raw note units:
# a fill that has to walk up the book reverts instead of overpaying.
PREC_DIGITS=$((${#PRICE_PREC} - 1))
QUOTE_UNITS=$(cast parse-units "$SPEND" "$PREC_DIGITS")
SPEND_RAW=$(cast parse-units "$SPEND" "$TCRED_DEC")
MIN_OUT=$(awk -v s="$SPEND" -v a="$ASK" -v d="$NOTE_DEC" -v f="$TAKER_BPS" -v k="$SLIPPAGE_BPS" \
  'BEGIN { printf "%d", s / (a / 1e18) * (1 - f / 1e4) * (1 - k / 1e4) * 10^d }')

before=$(cast call "$NOTE" 'balanceOf(address)(uint256)' "$BUYER" --rpc-url "$RPC" | awk '{print $1}')
cat <<INFO
--- note buy ---
  buyer     $BUYER
  spend     $SPEND tCRED at a best ask of $(awk -v a="$ASK" 'BEGIN { print a / 1e18 }') tCRED per note
  at least  $(awk -v m="$MIN_OUT" -v d="$NOTE_DEC" 'BEGIN { print m / 10^d }') notes, or the buy reverts
INFO

cast send "$TCRED" 'mint(address,uint256)' "$BUYER" "$SPEND_RAW" \
  --private-key "$KEY" --rpc-url "$RPC" >/dev/null
cast send "$TCRED" 'approve(address,uint256)' "$MARKET" "$SPEND_RAW" \
  --private-key "$KEY" --rpc-url "$RPC" >/dev/null
# With --json, cast reports a revert on stdout, so it is captured and
# shown rather than piped straight into the parse and lost.
if ! OUT=$(cast send "$MARKET" 'placeAndExecuteMarketBuy(uint96,uint256,bool,bool)' \
  "$QUOTE_UNITS" "$MIN_OUT" false false \
  --private-key "$KEY" --rpc-url "$RPC" --json 2>&1); then
  echo >&2
  echo "the buy reverted, so no tCRED was spent. The usual cause is fewer notes" >&2
  echo "on offer at the best ask than SPEND would buy: try a smaller SPEND." >&2
  echo "$OUT" | tail -3 >&2
  exit 1
fi
TX=$(echo "$OUT" | sed -nE 's/.*"transactionHash":"(0x[0-9a-fA-F]{64})".*/\1/p')

after=$(cast call "$NOTE" 'balanceOf(address)(uint256)' "$BUYER" --rpc-url "$RPC" | awk '{print $1}')
got=$(awk -v a="$after" -v b="$before" -v d="$NOTE_DEC" 'BEGIN { print (a - b) / 10^d }')
[ "$after" != "$before" ] || { echo "FAIL: the buy went through but no notes arrived" >&2; exit 1; }

echo
echo "  bought    $got notes"
echo "  tx        https://testnet.monadexplorer.com/tx/$TX"
echo
echo "Send the seller your address and that link."
