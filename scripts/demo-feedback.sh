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
ROOT=$(cd "$(dirname "$0")/.." && pwd)
DEPLOYER=ac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80
PUBLISHER=0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266
AGENT=${AGENT:-184}

# A run to publish outcomes from. Loans must have RESOLVED: an in-flight
# loan is not an outcome.
LOANS=${LOANS:-4} "$ROOT/scripts/demo-repay.sh" >/dev/null 2>&1
echo "loans run and repaid"

MANIFEST="$ROOT/contracts/deployments/31337.json"
MANAGER=$(python3 -c "import json;print(json.load(open('$MANIFEST'))['addresses']['loan_manager'])")

cd "$ROOT/contracts"
REGISTRY=$(forge create src/FeedbackRegistry.sol:FeedbackRegistry \
  --rpc-url "$RPC" --private-key "0x$DEPLOYER" --broadcast \
  --constructor-args "$PUBLISHER" 2>&1 | grep "Deployed to:" | awk '{print $3}')
echo "feedback registry $REGISTRY"

echo
echo "=== A: publish resolved obligations ==="
cd "$ROOT/gateway"
go build -o credence-publish ./cmd/credence-publish
CREDENCE_OPERATOR_KEY=$DEPLOYER ./credence-publish \
  -rpc "$RPC" -manager "$MANAGER" -registry "$REGISTRY" -agent "$AGENT"

# Publishing twice must not double-count: the registry refuses a second
# entry for the same loan (`docs/06` queues updates idempotently).
echo
echo "=== A: republish is idempotent ==="
CREDENCE_OPERATOR_KEY=$DEPLOYER ./credence-publish \
  -rpc "$RPC" -manager "$MANAGER" -registry "$REGISTRY" -agent "$AGENT"

echo
echo "=== B: read it back, from outside the repo ==="
# Copied out of the tree so the run cannot accidentally depend on
# anything beside it.
READER=$(mktemp -d)/read_feedback.py
cp "$ROOT/tools/reader/read_feedback.py" "$READER"
cd "$(dirname "$READER")"
python3 "$READER" --rpc "$RPC" --registry "$REGISTRY" --agent "$AGENT"
