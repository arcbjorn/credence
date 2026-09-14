#!/usr/bin/env bash
# docs/06 sub-claims A and B: publish attributable feedback, then read it
# back with no Credence code involved.
#
# The reader is deliberately a separate program in a different language
# that imports nothing from this repository. Sharing an encoder would make
# the check circular -- the reader would agree with the publisher for being
# the same code.
#
# Runs against a LOCAL ADAPTER. `docs/08` permits one for development and
# is explicit that it does not satisfy the release identity gate, which
# requires the selected network's ERC-8004-compatible registry.
set -euo pipefail

RPC=${RPC:-http://127.0.0.1:8546}
# Read from the node, not assumed. The deploy script writes its manifest
# keyed by chain id, so a hardcoded 31337 silently reads a stale local
# deployment when pointed at a testnet.
CHAIN_ID=${CHAIN_ID:-$(cast chain-id --rpc-url "$RPC")}
ROOT=$(cd "$(dirname "$0")/.." && pwd)
# Anvil account #0 by default. On a public chain, export a funded key:
# this one is published in the anvil banner and anyone can drain it.
DEPLOYER=${CREDENCE_DEPLOYER_KEY:-ac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80}
# The adapter only accepts this address, and the gateway publishes with
# DEPLOYER. Derive it so overriding the key cannot authorize one address
# while publishing from another.
PUBLISHER=$(cast wallet address --private-key "0x$DEPLOYER")
AGENT=${AGENT:-184}

# A run to publish outcomes from. Loans must have RESOLVED: an in-flight
# loan is not an outcome.
LOANS=${LOANS:-4} "$ROOT/scripts/demo-repay.sh" >/dev/null 2>&1
echo "loans run and repaid"

MANIFEST="$ROOT/contracts/deployments/$CHAIN_ID.json"
MANAGER=$(python3 -c "import json;print(json.load(open('$MANIFEST'))['addresses']['loan_manager'])")

cd "$ROOT/contracts"
# Forge replays nonces from its own broadcast cache. A chain that has been
# restarted -- or a different chain reusing the id -- resets those nonces,
# and forge then retries "nonce too low" with no useful output until it
# gives up. Drop this chain's cache so a deploy starts from the node's
# real nonce. Observed against a restarted anvil, 14 September.
rm -rf broadcast/*/"$CHAIN_ID" cache/*.s.sol/"$CHAIN_ID"
REGISTRY=$(forge create src/FeedbackRegistry.sol:FeedbackRegistry \
  --rpc-url "$RPC" --private-key "0x$DEPLOYER" --broadcast \
  --constructor-args "$PUBLISHER" 2>&1 | grep "Deployed to:" | awk '{print $3}')
echo "feedback registry $REGISTRY"

echo
echo "=== A: publish resolved obligations ==="
cd "$ROOT/gateway"
go build -o credence-publish ./cmd/credence-publish
CREDENCE_OPERATOR_KEY=$DEPLOYER ./credence-publish \
  -rpc "$RPC" -manager "$MANAGER" -registry "$REGISTRY" -agent "$AGENT" -chain-id "$CHAIN_ID"

# Publishing twice must not double-count: the registry refuses a second
# entry for the same loan (`docs/06` queues updates idempotently).
echo
echo "=== A: republish is idempotent ==="
CREDENCE_OPERATOR_KEY=$DEPLOYER ./credence-publish \
  -rpc "$RPC" -manager "$MANAGER" -registry "$REGISTRY" -agent "$AGENT" -chain-id "$CHAIN_ID"

echo
echo "=== B: read it back, from outside the repo ==="
# Copied out of the tree so the run cannot accidentally depend on
# anything beside it.
READER=$(mktemp -d)/read_feedback.py
cp "$ROOT/tools/reader/read_feedback.py" "$READER"
cd "$(dirname "$READER")"
python3 "$READER" --rpc "$RPC" --registry "$REGISTRY" --agent "$AGENT"
