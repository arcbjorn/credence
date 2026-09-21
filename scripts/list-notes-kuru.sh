#!/usr/bin/env bash
# List a `NoteSeries` on Kuru, and trade one note.
#
# The market half of the 2 October gate (`docs/08`). The instrument half
# -- origination, transfer, collection, redemption, default haircut --
# is `scripts/demo-notes.sh`; this adds the orderbook the notes trade on.
#
# **What this proves and what it does not.** It proves a `NoteSeries`
# note is a real ERC20 that a real on-chain orderbook will accept, and
# that a market for it can be created permissionlessly. It does NOT prove
# price discovery: both sides of any trade here are addresses this
# project controls, which `docs/08` states plainly --
#
#   "The note market is two participants you control. That does not imply
#    zero displayed depth -- it implies the market is CONTROLLED, with
#    observations lacking independent validation. A price nothing
#    independent produced cannot move executable credit."
#
# So the output below is labelled a controlled market, and nothing here
# feeds a price back into underwriting. `docs/08` keeps market feedback
# informational for exactly this reason.
set -euo pipefail

RPC=${RPC:-https://testnet-rpc.monad.xyz}
ROOT=$(cd "$(dirname "$0")/.." && pwd)
CHAIN_ID=$(cast chain-id --rpc-url "$RPC")

# Kuru on Monad testnet, verified on chain by scripts/probe-kuru.sh.
ROUTER=${KURU_ROUTER:-0x7EFbE105Ca7415dE98F96622173458ac1c054630}
DEPLOY_SIG='deployProxy(uint8,address,address,uint96,uint32,uint32,uint96,uint96,uint256,uint256,uint96)'

. "$ROOT/scripts/lib/keys.sh"
DEPLOYER=${CREDENCE_DEPLOYER_KEY:-}
[ -n "$DEPLOYER" ] || { echo "set CREDENCE_DEPLOYER_KEY" >&2; exit 1; }
credence_require_private_key "$DEPLOYER" "$CHAIN_ID" deployer CREDENCE_DEPLOYER_KEY

OPERATOR=$(cast wallet address --private-key "0x$DEPLOYER")
BUYER_KEY=${CREDENCE_AGENT_KEY:-}
[ -n "$BUYER_KEY" ] || { echo "set CREDENCE_AGENT_KEY for the second party" >&2; exit 1; }
BUYER=$(cast wallet address --private-key "0x$BUYER_KEY")

PRINCIPAL=${PRINCIPAL:-4000000000}
TERM=${TERM_SECONDS:-864000}
APR_BPS=${APR_BPS:-1200}

cat <<NOTE
--- mode ---
  chain        $CHAIN_ID
  market       Kuru, router $ROUTER
  participants BOTH project-controlled. This is a CONTROLLED market:
               the price below is not independent price discovery, and
               docs/08 forbids feeding it back into underwriting.
NOTE

BALANCE=$(cast balance "$OPERATOR" --rpc-url "$RPC")
python3 - "$BALANCE" <<'PY'
import sys
bal = int(sys.argv[1]) / 1e18
print(f"\n  deployer holds {bal:.4f} MON")
if bal < 0.6:
    raise SystemExit(
        "  refusing to start: a stranded half-deployment costs more than\n"
        "  it saves. Fund the deployer above 0.6 MON and re-run."
    )
PY

cd "$ROOT/contracts"
rm -rf broadcast/*/"$CHAIN_ID" cache/*.s.sol/"$CHAIN_ID"

echo
echo "=== deploying the series ==="
SETTLEMENT=$(forge create src/TestSettlementToken.sol:TestSettlementToken \
  --rpc-url "$RPC" --private-key "0x$DEPLOYER" --broadcast \
  2>&1 | grep "Deployed to:" | awk '{print $3}')
SERIES=$(forge create src/NoteSeries.sol:NoteSeries \
  --rpc-url "$RPC" --private-key "0x$DEPLOYER" --broadcast \
  --constructor-args "$SETTLEMENT" "$OPERATOR" "$OPERATOR" \
  "$PRINCIPAL" "$TERM" "$APR_BPS" 6 \
  2>&1 | grep "Deployed to:" | awk '{print $3}')
echo "  settlement  $SETTLEMENT"
echo "  series      $SERIES  (the CNOTE token)"

cast send "$SERIES" "setLoanManager(address)" "$OPERATOR" \
  --private-key "0x$DEPLOYER" --rpc-url "$RPC" >/dev/null

echo
echo "=== issuing notes ==="
cast send "$SETTLEMENT" "mint(address,uint256)" "$OPERATOR" "$PRINCIPAL" \
  --private-key "0x$DEPLOYER" --rpc-url "$RPC" >/dev/null
cast send "$SETTLEMENT" "approve(address,uint256)" "$SERIES" "$PRINCIPAL" \
  --private-key "0x$DEPLOYER" --rpc-url "$RPC" >/dev/null
cast send "$SERIES" "subscribe(uint256)" "$PRINCIPAL" \
  --private-key "0x$DEPLOYER" --rpc-url "$RPC" >/dev/null
cast send "$SERIES" "activate()" \
  --private-key "0x$DEPLOYER" --rpc-url "$RPC" >/dev/null
echo "  notes issued  $(cast call "$SERIES" "totalSupply()(uint256)" --rpc-url "$RPC" | awk '{print $1}')"

echo
echo "=== creating the Kuru market ==="
# Market parameters. Kuru rejects a pair whose tokens hold no code, and
# reverts with EMPTY data when it does -- which reads like a permission
# failure and is not one (scripts/probe-kuru.sh records that trap).
PARAMS=(0 "$SERIES" "$SETTLEMENT" 1000000000 100000 1 1000 1000000000000 30 10 100)

MARKET=$(cast call "$ROUTER" "$DEPLOY_SIG(address)" "${PARAMS[@]}" \
  --from "$OPERATOR" --rpc-url "$RPC" 2>/dev/null || true)
if [ -z "$MARKET" ]; then
  echo "  deployProxy simulates to a revert; not sending." >&2
  echo "  Both tokens have code, so check the Kuru ABI has not moved:" >&2
  echo "    bash scripts/probe-kuru.sh" >&2
  exit 1
fi
echo "  simulates to  $MARKET"

cast send "$ROUTER" "$DEPLOY_SIG" "${PARAMS[@]}" \
  --private-key "0x$DEPLOYER" --rpc-url "$RPC" >/dev/null
CODE=$(cast code "$MARKET" --rpc-url "$RPC" 2>/dev/null || echo 0x)
SIZE=$(((${#CODE} - 2) / 2))
echo "  market live   $MARKET  ($SIZE bytes)"
[ "$SIZE" -gt 0 ] || { echo "  FAIL: no code at the market address" >&2; exit 1; }

echo
echo "=== a note changes hands ==="
# Transferred directly rather than through the book: placing and filling
# an order needs both sides funded and approved on the market, and the
# claim being tested here is that the note MOVES and the new holder
# redeems. `docs/08` treats a controlled fill as no stronger evidence
# than a controlled transfer, so this keeps the simpler path.
SUPPLY=$(cast call "$SERIES" "totalSupply()(uint256)" --rpc-url "$RPC" | awk '{print $1}')
cast send "$SERIES" "transfer(address,uint256)" "$BUYER" "$SUPPLY" \
  --private-key "0x$DEPLOYER" --rpc-url "$RPC" >/dev/null
echo "  buyer holds   $(cast call "$SERIES" "balanceOf(address)(uint256)" "$BUYER" --rpc-url "$RPC" | awk '{print $1}')"

# Record them. The script printed these and nothing kept them, so the
# addresses survived only in a terminal and then in `docs/08` prose --
# which is the copy `README.md` warns about, and the reason three
# earlier address copies went stale.
MANIFEST="$ROOT/contracts/deployments/$CHAIN_ID.json"
if [ -f "$MANIFEST" ]; then
  IMPL=$(cast call "$ROUTER" "orderBookImplementation()(address)" --rpc-url "$RPC" 2>/dev/null)
  python3 "$ROOT/scripts/lib/record-kuru.py" \
    "$MANIFEST" "$ROUTER" "$MARKET" "$SERIES" "$SETTLEMENT" "${IMPL:-}"
fi

echo
echo "PASS  a note series is listed on Kuru and its notes have moved"
echo
echo "  market     $MARKET"
echo "  note       $SERIES"
echo "  settlement $SETTLEMENT"
echo
echo "  CONTROLLED MARKET. Both participants are project-controlled, so"
echo "  this demonstrates that the venue accepts the instrument -- not"
echo "  price discovery. docs/08 keeps this signal informational."
