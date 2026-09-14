#!/usr/bin/env bash
# docs/09 beat 4: close the strategy, then repay from the proceeds.
#
# The healthy counterpart to demo-default.sh. The agent borrows, puts the
# whole book to work, holds while interest accrues, then exits the
# position and settles the debt. Nothing is written off.
set -euo pipefail

RPC=${RPC:-http://127.0.0.1:8546}
# Read from the node, not assumed. The deploy script writes its manifest
# keyed by chain id, so a hardcoded 31337 silently reads a stale local
# deployment when pointed at a testnet.
CHAIN_ID=${CHAIN_ID:-$(cast chain-id --rpc-url "$RPC")}
ROOT=$(cd "$(dirname "$0")/.." && pwd)
WORK=${WORK:-/tmp/credence-repay}
# A fresh database each run: a stale one carries forecasts and history
# from a previous chain, which would credit the agent for loans this run
# never made.
rm -rf "$WORK" && mkdir -p "$WORK"
# Anvil account #0 by default. On a public chain, export a funded key:
# this one is published in the anvil banner and anyone can drain it.
DEPLOYER=${CREDENCE_DEPLOYER_KEY:-ac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80}
SIGNER=${CREDENCE_SIGNER_KEY:-5de4111afa1a4b94908f83103eb1f1706367c2e68ca870fc3fb9a804cdab365a}
PRINCIPAL=${PRINCIPAL:-4000000000}
# Two loans: the first closes and repays, the second is left open so the
# monitoring beats have a live position to assess.
LOANS=${LOANS:-2}
# A local chain warps time, so a 10-day term costs nothing to observe. A
# public one cannot: the hold has to be a real wait, so both shrink to
# seconds. Interest is then genuinely accrued over the elapsed term
# rather than asserted over one that never passed.
# The identity the loans are bound to. The defaults are a placeholder
# registry and a made-up agent, which is honest for a local run: no
# ERC-8004 registry exists there. Against a real one, name an agent that
# is actually registered -- publishing feedback about a token id nobody
# holds attributes an outcome to nothing.
IDENTITY_REGISTRY=${IDENTITY_REGISTRY:-0x000000000000000000000000000000000000aec0}
AGENT_ID=${AGENT_ID:-184}
TERM_SECONDS=${TERM_SECONDS:-$((10 * 86400))}
HOLD_SECONDS=${HOLD_SECONDS:-$((5 * 86400))}

# A published key may sign nothing on a chain other people can reach.
. "$ROOT/scripts/lib/keys.sh"
credence_require_private_key "$SIGNER" "$CHAIN_ID" attestor CREDENCE_SIGNER_KEY
credence_require_private_key "$DEPLOYER" "$CHAIN_ID" deployer CREDENCE_DEPLOYER_KEY

# What a reviewer must know before reading any number below.
#
# `docs/11` requires modes to be labelled and `docs/08` requires claimed
# capabilities to be backed by artifacts. Printed as text rather than a
# status pill precisely so it cannot collapse into "green".
cat <<NOTE
--- mode ---
  chain          $CHAIN_ID $(credence_is_local_chain "$CHAIN_ID" 2>/dev/null && echo "(local, time can be warped)" || echo "(public, real elapsed time)")
  evidence       SCENARIO -- synthetic history, honestly labelled
  authorization  Development route, one attestor key
  assets         test tokens; the risky one is named NOT ETH on purpose
  trust          the operator and attestor are TRUSTED, see docs/05
NOTE

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
addr() { python3 -c "import json;print(json.load(open('$MANIFEST'))['addresses']['$1'])"; }
ORACLE=$(addr oracle);             VAULT=$(addr vault)
MANAGER=$(addr loan_manager);      SETTLEMENT=$(addr settlement)
RISKY=$(addr risky);               ADAPTER=$(addr adapter)
PRICE_SOURCE=$(addr price_source)

# The bound operator. It signs nothing in this demo, so it needs no gas,
# but it follows CREDENCE_AGENT_KEY so every demo binds the same operator.
export AGENT_ADDRESS=$(cast wallet address --private-key \
  "0x${CREDENCE_AGENT_KEY:-59c6995e998f97a5a0044966f0945389dc9e86dae88c7a8412f4603b6b78690d}")
export SETTLEMENT_TOKEN=$SETTLEMENT VAULT_ADDRESS=$VAULT
export LOAN_MANAGER_ADDRESS=$MANAGER RISKY_ASSET=$RISKY STRATEGY_ADAPTER=$ADAPTER
ACCOUNT=$(forge script script/Scenario.s.sol --rpc-url "$RPC" --broadcast 2>&1 \
  | grep -o 'credit account 0x[0-9a-fA-F]\{40\}' | awk '{print $3}')
echo "credit account $ACCOUNT"

# The engine prices every loan. Without it the scenario signs fixed terms
# and the sequential-loans row cannot be demonstrated (`docs/11`).
cd "$ROOT/risk"
cargo build --quiet 2>/dev/null || true
RISK_SERVICE_ADDR=127.0.0.1:18111 ./target/debug/credence-risk-service \
  >"$WORK/risk-repay.log" 2>&1 &
RISK_PID=$!
trap 'kill $RISK_PID 2>/dev/null || true' EXIT
for _ in $(seq 40); do
  curl -sf http://127.0.0.1:18111/health >/dev/null 2>&1 && break
  sleep 0.25
done

cd "$ROOT/gateway"
export CREDENCE_OPERATOR_KEY=$DEPLOYER CREDENCE_SIGNER_KEY=$SIGNER
go build -o credence-scenario ./cmd/credence-scenario
./credence-scenario -rpc "$RPC" -oracle "$ORACLE" -manager "$MANAGER" \
  -account "$ACCOUNT" -settlement "$SETTLEMENT" -principal "$PRINCIPAL" \
  -adapter "$ADAPTER" -risky "$RISKY" -price-source "$PRICE_SOURCE" \
  -chain-id "$CHAIN_ID" -deploy-bps 10000 -loans "$LOANS" \
  -term-seconds "$TERM_SECONDS" -hold-seconds "$HOLD_SECONDS" \
  -identity-registry "$IDENTITY_REGISTRY" -agent-id "$AGENT_ID" \
  -enroll "$WORK/scenario.db" -vault "$VAULT" -risk http://127.0.0.1:18111 -policy "$ROOT/config/engine-policy.json"

# `docs/11`: a passing unit test and a correctly wired deployment are
# different claims. This asserts the second against real balances.
echo "=== deployment verification ==="
go build -o credence-verify ./cmd/credence-verify
./credence-verify -rpc "$RPC" -manager "$MANAGER" -account "$ACCOUNT" \
  -settlement "$SETTLEMENT" -owner "$(cast wallet address --private-key "0x$DEPLOYER")" \
  -vault "$VAULT" -risky "$RISKY"

echo "=== vault after repayment ==="
for f in outstandingPrincipal collectedInterest realizedLoss idleCash; do
  printf '%-22s %s\n' "$f" "$(cast call "$VAULT" "$f()(uint256)" --rpc-url "$RPC")"
done
