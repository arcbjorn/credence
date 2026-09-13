#!/usr/bin/env bash
# docs/09 beat 4: close the strategy, then repay from the proceeds.
#
# The healthy counterpart to demo-default.sh. The agent borrows, puts the
# whole book to work, holds while interest accrues, then exits the
# position and settles the debt. Nothing is written off.
set -euo pipefail

RPC=${RPC:-http://127.0.0.1:8546}
ROOT=$(cd "$(dirname "$0")/.." && pwd)
DEPLOYER=ac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80
SIGNER=${CREDENCE_SIGNER_KEY:-5de4111afa1a4b94908f83103eb1f1706367c2e68ca870fc3fb9a804cdab365a}
PRINCIPAL=${PRINCIPAL:-4000000000}
# Two loans: the first closes and repays, the second is left open so the
# monitoring beats have a live position to assess.
LOANS=${LOANS:-2}

cd "$ROOT/contracts"
export DEPLOYER_KEY=0x$DEPLOYER
export ATTESTOR_ADDRESS=$(cast wallet address --private-key "0x$SIGNER")
export DEPLOYMENT_ID=7
forge script script/Deploy.s.sol --rpc-url "$RPC" --broadcast >/dev/null
MANIFEST="$ROOT/contracts/deployments/31337.json"
addr() { python3 -c "import json;print(json.load(open('$MANIFEST'))['addresses']['$1'])"; }
ORACLE=$(addr oracle);             VAULT=$(addr vault)
MANAGER=$(addr loan_manager);      SETTLEMENT=$(addr settlement)
RISKY=$(addr risky);               ADAPTER=$(addr adapter)
PRICE_SOURCE=$(addr price_source)

export AGENT_ADDRESS=0x70997970C51812dc3A010C7d01b50e0d17dc79C8
export SETTLEMENT_TOKEN=$SETTLEMENT VAULT_ADDRESS=$VAULT
export LOAN_MANAGER_ADDRESS=$MANAGER RISKY_ASSET=$RISKY STRATEGY_ADAPTER=$ADAPTER
ACCOUNT=$(forge script script/Scenario.s.sol --rpc-url "$RPC" --broadcast 2>&1 \
  | grep -o 'credit account 0x[0-9a-fA-F]\{40\}' | awk '{print $3}')
echo "credit account $ACCOUNT"

cd "$ROOT/gateway"
export CREDENCE_OPERATOR_KEY=$DEPLOYER CREDENCE_SIGNER_KEY=$SIGNER
go build -o credence-scenario ./cmd/credence-scenario
./credence-scenario -rpc "$RPC" -oracle "$ORACLE" -manager "$MANAGER" \
  -account "$ACCOUNT" -settlement "$SETTLEMENT" -principal "$PRINCIPAL" \
  -adapter "$ADAPTER" -risky "$RISKY" -price-source "$PRICE_SOURCE" \
  -deploy-bps 10000 -loans "$LOANS"

echo "=== vault after repayment ==="
for f in outstandingPrincipal collectedInterest realizedLoss idleCash; do
  printf '%-22s %s\n' "$f" "$(cast call "$VAULT" "$f()(uint256)" --rpc-url "$RPC")"
done
