#!/usr/bin/env bash
# Deploy to a public testnet and verify the result on chain.
#
# Separate from the demo scripts because a public chain differs in ways a
# local one hides: blocks are not instant, time cannot be warped, and a
# half-funded deployer strands a partial deployment. This checks the
# balance BEFORE spending any of it, so an underfunded run costs nothing.
set -euo pipefail

RPC=${RPC:-https://testnet-rpc.monad.xyz}
ROOT=$(cd "$(dirname "$0")/.." && pwd)
KEYFILE=${CREDENCE_KEYFILE:-$HOME/.credence/deployer.key}

if [ -n "${CREDENCE_DEPLOYER_KEY:-}" ]; then
  DEPLOYER=$CREDENCE_DEPLOYER_KEY
elif [ -f "$KEYFILE" ]; then
  DEPLOYER=$(tr -d '[:space:]' < "$KEYFILE")
else
  echo "No deployer key. Set CREDENCE_DEPLOYER_KEY or create $KEYFILE" >&2
  exit 1
fi

ADDR=$(cast wallet address --private-key "0x$DEPLOYER")
CHAIN_ID=$(cast chain-id --rpc-url "$RPC")
GAS_PRICE=$(cast gas-price --rpc-url "$RPC")
BALANCE=$(cast balance "$ADDR" --rpc-url "$RPC")

# Measured from a full local run (23 txs, 8.8M gas for deploy + scenario),
# doubled for headroom. Not a guess: scripts/deploy-testnet.sh README
# records how the figure was obtained.
NEEDED_GAS=${NEEDED_GAS:-20000000}
NEEDED=$(python3 -c "print($NEEDED_GAS * $GAS_PRICE)")

echo "chain     $CHAIN_ID"
echo "deployer  $ADDR"
python3 - <<PY
b=$BALANCE; n=$NEEDED; g=$GAS_PRICE
print(f"balance   {b/1e18:.4f} MON")
print(f"gas price {g/1e9:.1f} gwei")
print(f"needed    {n/1e18:.4f} MON (for {$NEEDED_GAS:,} gas)")
PY

if [ "$(python3 -c "print(1 if $BALANCE < $NEEDED else 0)")" = "1" ]; then
  cat >&2 <<MSG

Underfunded. Nothing was deployed and nothing was spent.

Fund $ADDR with testnet MON, then re-run:
  https://faucet.monad.xyz
MSG
  exit 1
fi

cd "$ROOT/contracts"
export DEPLOYER_KEY=0x$DEPLOYER
SIGNER=${CREDENCE_SIGNER_KEY:-5de4111afa1a4b94908f83103eb1f1706367c2e68ca870fc3fb9a804cdab365a}
export ATTESTOR_ADDRESS=$(cast wallet address --private-key "0x$SIGNER")
export DEPLOYMENT_ID=${DEPLOYMENT_ID:-7}

echo
echo "deploying..."
forge script script/Deploy.s.sol --rpc-url "$RPC" --broadcast --slow

MANIFEST="$ROOT/contracts/deployments/$CHAIN_ID.json"
echo
echo "=== verifying deployment on chain ==="
python3 - "$MANIFEST" <<'PY' > /tmp/credence-addrs.txt
import json,sys
m=json.load(open(sys.argv[1]))
for k,v in m['addresses'].items(): print(k,v)
PY

FAIL=0
while read -r name addr; do
  CODE=$(cast code "$addr" --rpc-url "$RPC" 2>/dev/null || echo 0x)
  # An address with no code is a failed deployment, not an empty contract.
  if [ -z "$CODE" ] || [ "$CODE" = "0x" ]; then
    printf '[FAIL] %-16s %s has no code\n' "$name" "$addr"; FAIL=1
  else
    printf '[PASS] %-16s %s (%s bytes)\n' "$name" "$addr" "$(( (${#CODE} - 2) / 2 ))"
  fi
done < /tmp/credence-addrs.txt

echo
if [ "$FAIL" = "1" ]; then echo "deployment INCOMPLETE"; exit 1; fi
echo "all contracts verified on chain $CHAIN_ID"
echo "manifest: $MANIFEST"
