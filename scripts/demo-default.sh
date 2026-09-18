#!/usr/bin/env bash
# docs/09 beat 7: a lender loss that is realised, not asserted.
#
# A loan deploys its principal into the risky asset, the asset falls far
# enough that unwinding returns less than the principal, and recovery
# collects everything that actually exists. What is missing afterwards is
# recorded as realized loss against the vault.
#
# The fall must be deep enough to break principal, not just interest:
# recovering full principal plus a shortfall of accrued interest is a
# legitimate outcome, but it is not a lender loss.
set -euo pipefail

RPC=${RPC:-http://127.0.0.1:8546}
# Read from the node, not assumed. The deploy script writes its manifest
# keyed by chain id, so a hardcoded 31337 silently reads a stale local
# deployment when pointed at a testnet.
CHAIN_ID=${CHAIN_ID:-$(cast chain-id --rpc-url "$RPC")}
ROOT=$(cd "$(dirname "$0")/.." && pwd)
# anvil #0 owns the deployment and acts as keeper; #1 is the agent.
# Anvil account #0 by default. On a public chain, export a funded key:
# this one is published in the anvil banner and anyone can drain it.
DEPLOYER=${CREDENCE_DEPLOYER_KEY:-ac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80}
AGENT=${CREDENCE_AGENT_KEY:-59c6995e998f97a5a0044966f0945389dc9e86dae88c7a8412f4603b6b78690d}
# The oracle only accepts assessments signed by the attestor it was
# deployed with, so derive that address from the signing key rather than
# pinning it: a literal here would silently drift and every publish would
# fail signature verification.
SIGNER=${CREDENCE_SIGNER_KEY:-5de4111afa1a4b94908f83103eb1f1706367c2e68ca870fc3fb9a804cdab365a}
# Fraction of the asset's value that survives the crash.
#
# Agent equity is the first loss layer, so lenders only lose once the
# fall exceeds it. With 5,000 of equity and 4,000 of principal deployed,
# the whole 9,000 book must fall below 4,000 before principal breaks --
# hence a severe number here. A milder crash is absorbed entirely by the
# agent, which is the protocol working, not a failed demo.
CRASH_BPS=${CRASH_BPS:-1000}
PRINCIPAL=${PRINCIPAL:-4000000000}

# A published key may sign nothing on a chain other people can reach.
. "$ROOT/scripts/lib/keys.sh"
credence_require_private_key "$SIGNER" "$CHAIN_ID" attestor CREDENCE_SIGNER_KEY
credence_require_private_key "$DEPLOYER" "$CHAIN_ID" deployer CREDENCE_DEPLOYER_KEY

cd "$ROOT/contracts"
# Forge replays nonces from its own broadcast cache. A chain that has been
# restarted -- or a different chain reusing the id -- resets those nonces,
# and forge then retries "nonce too low" with no useful output until it
# gives up. Drop this chain's cache so a deploy starts from the node's
# real nonce. Observed against a restarted anvil, 14 September.
rm -rf broadcast/*/"$CHAIN_ID" cache/*.s.sol/"$CHAIN_ID"
export DEPLOYER_KEY=0x$DEPLOYER
export ATTESTOR_ADDRESS=$(cast wallet address --private-key "0x$SIGNER")
export DEPLOYMENT_ID=7
forge script script/Deploy.s.sol --rpc-url "$RPC" --broadcast >/dev/null
MANIFEST="$ROOT/contracts/deployments/$CHAIN_ID.json"
addr() { python3 -c "import json,sys;print(json.load(open('$MANIFEST'))['addresses']['$1'])"; }
ORACLE=$(addr oracle);       VAULT=$(addr vault)
MANAGER=$(addr loan_manager); SETTLEMENT=$(addr settlement)
RISKY=$(addr risky);          VENUE=$(addr venue)
PRICE_SOURCE=$(addr price_source); ADAPTER=$(addr adapter)

# Derived, never a second constant: the account's bound operator is the
# key that signs its actions, so a mismatched pair fails at execute time.
export AGENT_ADDRESS=$(cast wallet address --private-key "0x$AGENT")
export SETTLEMENT_TOKEN=$SETTLEMENT VAULT_ADDRESS=$VAULT
export LOAN_MANAGER_ADDRESS=$MANAGER RISKY_ASSET=$RISKY STRATEGY_ADAPTER=$ADAPTER
ACCOUNT=$(forge script script/Scenario.s.sol --rpc-url "$RPC" --broadcast 2>&1 \
  | grep -o 'credit account 0x[0-9a-fA-F]\{40\}' | awk '{print $3}')
echo "credit account $ACCOUNT"

# Originate and put the whole principal into the risky asset.
cd "$ROOT/gateway"
export CREDENCE_OPERATOR_KEY=$DEPLOYER CREDENCE_SIGNER_KEY=$SIGNER
go build -o credence-scenario ./cmd/credence-scenario
go build -o credence-recover ./cmd/credence-recover
go build -o credence-verify ./cmd/credence-verify
./credence-scenario -rpc "$RPC" -oracle "$ORACLE" -manager "$MANAGER" \
  -account "$ACCOUNT" -settlement "$SETTLEMENT" \
  -principal "$PRINCIPAL" -adapter "$ADAPTER" -risky "$RISKY" \
  -chain-id "$CHAIN_ID" -deploy-bps 10000

# The crash. Venue and independent price move together: this is a market
# move, so the adapter's slippage guard correctly does not block the exit.
NEW_PRICE=$(python3 -c "print(2000000000 * $CRASH_BPS // 10000)")
cast send "$VENUE" "setPrice(uint256)" "$NEW_PRICE" --private-key "0x$DEPLOYER" --rpc-url "$RPC" >/dev/null
cast send "$PRICE_SOURCE" "setPrice(uint256)" "$NEW_PRICE" --private-key "0x$DEPLOYER" --rpc-url "$RPC" >/dev/null
echo "risky asset falls to $NEW_PRICE ($CRASH_BPS bps of mark)"

# Exit the position at the crashed price, realising the shortfall.
RISKY_BAL=$(cast call "$RISKY" 'balanceOf(address)(uint256)' "$ACCOUNT" --rpc-url "$RPC" | awk '{print $1}')
cast send "$ACCOUNT" "approveAdapter(address,address,uint256)" "$RISKY" "$ADAPTER" "$RISKY_BAL" \
  --private-key "0x$AGENT" --rpc-url "$RPC" >/dev/null
cast send "$ACCOUNT" "execute(address,bytes)" "$ADAPTER" \
  "$(cast calldata 'unwind(uint256)' "$RISKY_BAL")" \
  --private-key "0x$AGENT" --rpc-url "$RPC" >/dev/null

DEBT=$(cast call "$MANAGER" "debtDue(uint256)(uint256)" 1 --rpc-url "$RPC" | awk '{print $1}')
CASH=$(cast call "$SETTLEMENT" 'balanceOf(address)(uint256)' "$ACCOUNT" --rpc-url "$RPC" | awk '{print $1}')
echo "after unwind: debt $DEBT against cash $CASH"

# Collect everything that exists (survives-bps 10000): the shortfall is
# what the market actually destroyed, not a number this script chose.
./credence-recover -rpc "$RPC" -manager "$MANAGER" -vault "$VAULT" \
  -account "$ACCOUNT" -settlement "$SETTLEMENT" -loan 1 -chain-id "$CHAIN_ID" -survives-bps 10000

# Retain the vault's ledgers, so the interface can render a REALISED loss.
#
# Without this the lender screen reads `demo-repay`, where nothing ever
# defaults, and its "realized loss" figure is structurally always zero --
# the one screen whose job is showing a lender loss could never show one.
# `docs/09` beat 7 requires the loss to be visible, and `docs/12` lists
# "losses are accounted for" among the claims the release may earn.
#
# The figures come from `realizedLoss()` on the vault, read back after
# recovery settled, not from anything this script computed.
WORK=${WORK:-/tmp/credence-default}
mkdir -p "$WORK"
echo
echo "=== deployment verification ==="
# `-ledgers-only`: the loan has resolved, so the beat-6 checks would be
# asking the wrong question. "Owner withdrawal reverts while debt is open"
# cannot hold once the debt is written off, and reporting it as a failure
# would describe a correctly resolved default as six broken invariants.
./credence-verify -rpc "$RPC" -vault "$VAULT" -ledgers-only \
  -json "$WORK/checks.json"
