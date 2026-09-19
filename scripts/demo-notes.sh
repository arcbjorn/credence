#!/usr/bin/env bash
# `docs/09` beat 9: transfer and redeem a note.
#
# The instrument half of the Kuru gate. A `NoteSeries` is deployed and
# funded, its notes are TRANSFERRED to a second holder, and that holder
# redeems against what the loan actually collected. Nothing here is
# simulated: every figure printed below is read back from chain.
#
# What this does NOT do is list the notes on Kuru. `scripts/probe-kuru.sh`
# establishes that creating a market is permissionless and costs about
# 0.133 MON, but a market with two participants the project controls is
# not price discovery, and `docs/08` keeps that signal informational. The
# listing is a separate step behind the 2 October gate.
#
# Runs against anvil by default. On a public chain export funded keys.
set -euo pipefail

RPC=${RPC:-http://127.0.0.1:8546}
CHAIN_ID=${CHAIN_ID:-$(cast chain-id --rpc-url "$RPC")}
ROOT=$(cd "$(dirname "$0")/.." && pwd)

DEPLOYER=${CREDENCE_DEPLOYER_KEY:-ac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80}
# The second holder: whoever buys the note is who gets paid. Anvil #1 by
# default, so the transfer moves value between distinct addresses.
BUYER_KEY=${CREDENCE_AGENT_KEY:-59c6995e998f97a5a0044966f0945389dc9e86dae88c7a8412f4603b6b78690d}

. "$ROOT/scripts/lib/keys.sh"
credence_require_private_key "$DEPLOYER" "$CHAIN_ID" deployer CREDENCE_DEPLOYER_KEY

OPERATOR=$(cast wallet address --private-key "0x$DEPLOYER")
BUYER=$(cast wallet address --private-key "0x$BUYER_KEY")

PRINCIPAL=${PRINCIPAL:-4000000000}
TERM=${TERM_SECONDS:-864000}
APR_BPS=${APR_BPS:-1200}
# What the borrower actually returns. Default is a clean repayment; set
# COLLECTED below PRINCIPAL to see a note redeem under face value.
COLLECTED=${COLLECTED:-$PRINCIPAL}
INTEREST=${INTEREST:-6575343}

cat <<NOTE
--- mode ---
  chain        $CHAIN_ID $(credence_is_local_chain "$CHAIN_ID" 2>/dev/null && echo "(local)" || echo "(public)")
  instrument   NoteSeries -- one loan, fixed supply, redeemed on collections
  market       NONE. Notes are transferable here; nothing is listed or traded
  assets       test tokens; the settlement one is named NOT USDC on purpose
NOTE

cd "$ROOT/contracts"
rm -rf broadcast/*/"$CHAIN_ID" cache/*.s.sol/"$CHAIN_ID"

echo
echo "=== deploying the series ==="
SETTLEMENT=$(forge create src/TestSettlementToken.sol:TestSettlementToken \
  --rpc-url "$RPC" --private-key "0x$DEPLOYER" --broadcast \
  2>&1 | grep "Deployed to:" | awk '{print $3}')

# The operator stands in for the borrower here: this demo exercises the
# instrument, not the full loan lifecycle, which demo-repay.sh covers.
SERIES=$(forge create src/NoteSeries.sol:NoteSeries \
  --rpc-url "$RPC" --private-key "0x$DEPLOYER" --broadcast \
  --constructor-args "$SETTLEMENT" "$OPERATOR" "$OPERATOR" \
  "$PRINCIPAL" "$TERM" "$APR_BPS" 6 \
  2>&1 | grep "Deployed to:" | awk '{print $3}')

echo "  settlement  $SETTLEMENT"
echo "  series      $SERIES"
echo "  holder      $OPERATOR (subscribes)"
echo "  buyer       $BUYER (receives the notes)"

# The manager is bound once and cannot be repointed, so a series cannot be
# aimed at a different obligation after its notes are sold. Here the
# operator acts as the manager to drive the funding interface directly.
cast send "$SERIES" "setLoanManager(address)" "$OPERATOR" \
  --private-key "0x$DEPLOYER" --rpc-url "$RPC" >/dev/null

echo
echo "=== funding ==="
cast send "$SETTLEMENT" "mint(address,uint256)" "$OPERATOR" \
  "$((PRINCIPAL + INTEREST))" --private-key "0x$DEPLOYER" --rpc-url "$RPC" >/dev/null
cast send "$SETTLEMENT" "approve(address,uint256)" "$SERIES" "$PRINCIPAL" \
  --private-key "0x$DEPLOYER" --rpc-url "$RPC" >/dev/null
cast send "$SERIES" "subscribe(uint256)" "$PRINCIPAL" \
  --private-key "0x$DEPLOYER" --rpc-url "$RPC" >/dev/null
cast send "$SERIES" "activate()" \
  --private-key "0x$DEPLOYER" --rpc-url "$RPC" >/dev/null

SUPPLY=$(cast call "$SERIES" "totalSupply()(uint256)" --rpc-url "$RPC" | awk '{print $1}')
echo "  notes issued  $SUPPLY (fixed supply, one per settlement unit)"

echo
echo "=== the loan draws, then returns what it returns ==="
cast send "$SERIES" "lend(address,uint256,uint256)" "$OPERATOR" "$PRINCIPAL" 0 \
  --private-key "0x$DEPLOYER" --rpc-url "$RPC" >/dev/null
echo "  lent          $PRINCIPAL"

cast send "$SETTLEMENT" "transfer(address,uint256)" "$SERIES" \
  "$((COLLECTED + INTEREST))" --private-key "0x$DEPLOYER" --rpc-url "$RPC" >/dev/null
cast send "$SERIES" "recordPrincipalRepaid(address,uint256)" "$OPERATOR" "$COLLECTED" \
  --private-key "0x$DEPLOYER" --rpc-url "$RPC" >/dev/null
cast send "$SERIES" "recordInterestCollected(address,uint256)" "$OPERATOR" "$INTEREST" \
  --private-key "0x$DEPLOYER" --rpc-url "$RPC" >/dev/null

SHORTFALL=$((PRINCIPAL - COLLECTED))
if [ "$SHORTFALL" -gt 0 ]; then
  cast send "$SERIES" "recordLoss(address,uint256)" "$OPERATOR" "$SHORTFALL" \
    --private-key "0x$DEPLOYER" --rpc-url "$RPC" >/dev/null
  echo "  collected     $COLLECTED  (written off $SHORTFALL)"
else
  echo "  collected     $COLLECTED  plus interest $INTEREST"
fi

echo
echo "=== the claim changes hands BEFORE redemption ==="
cast send "$SERIES" "transfer(address,uint256)" "$BUYER" "$SUPPLY" \
  --private-key "0x$DEPLOYER" --rpc-url "$RPC" >/dev/null
echo "  seller holds  $(cast call "$SERIES" "balanceOf(address)(uint256)" "$OPERATOR" --rpc-url "$RPC" | awk '{print $1}')"
echo "  buyer holds   $(cast call "$SERIES" "balanceOf(address)(uint256)" "$BUYER" --rpc-url "$RPC" | awk '{print $1}')"

echo
echo "=== redemption ==="
cast send "$SERIES" "openRedemption()" \
  --private-key "0x$DEPLOYER" --rpc-url "$RPC" >/dev/null

BEFORE=$(cast call "$SETTLEMENT" "balanceOf(address)(uint256)" "$BUYER" --rpc-url "$RPC" | awk '{print $1}')
cast send "$SERIES" "redeem()" --private-key "0x$BUYER_KEY" --rpc-url "$RPC" >/dev/null
AFTER=$(cast call "$SETTLEMENT" "balanceOf(address)(uint256)" "$BUYER" --rpc-url "$RPC" | awk '{print $1}')
PAID=$((AFTER - BEFORE))

echo "  buyer received  $PAID"
echo "  face value      $PRINCIPAL"
python3 - "$PAID" "$PRINCIPAL" <<'PY'
import sys
paid, face = int(sys.argv[1]), int(sys.argv[2])
if paid > face:
    print(f"  -> ABOVE face by {paid - face}: the collected interest reached the holder")
elif paid < face:
    print(f"  -> BELOW face by {face - paid}: the note bore the loss, which is the instrument working")
else:
    print("  -> exactly face value")
PY

# The seller must get nothing: the claim moved with the notes.
if cast send "$SERIES" "redeem()" --private-key "0x$DEPLOYER" --rpc-url "$RPC" \
  >/dev/null 2>&1; then
  echo "  FAIL: the seller redeemed after transferring the claim" >&2
  exit 1
fi
echo "  seller redeems  nothing (NothingToRedeem) -- the claim moved"

LEFT=$(cast call "$SERIES" "idleCash()(uint256)" --rpc-url "$RPC" | awk '{print $1}')
echo "  left in series  $LEFT"
[ "$LEFT" = "0" ] || { echo "  FAIL: cash stranded in the contract" >&2; exit 1; }

echo
echo "PASS  a note was funded, transferred, and redeemed against actual collections"
echo "      (no market: nothing was listed or traded -- see docs/08)"
