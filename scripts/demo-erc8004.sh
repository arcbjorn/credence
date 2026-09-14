#!/usr/bin/env bash
# docs/06 sub-claims A and B, against the ERC-8004 standard.
#
# The counterpart to demo-feedback.sh, which exercises the LOCAL adapter.
# This one speaks the actual standard: `giveFeedback` / `readAllFeedback`,
# one signed int128 per entry, per-client attribution, and no on-chain
# idempotency.
#
# Against a public chain, point RPC, REPUTATION_REGISTRY and
# IDENTITY_REGISTRY at the canonical deployment and the agent must be
# registered there. With none of those set it deploys a faithful double
# locally (contracts/src/MockERC8004.sol) whose selectors match the real
# registry exactly, so the path can be exercised without a funded key.
set -euo pipefail

RPC=${RPC:-http://127.0.0.1:8546}
CHAIN_ID=${CHAIN_ID:-$(cast chain-id --rpc-url "$RPC")}
ROOT=$(cd "$(dirname "$0")/.." && pwd)
DEPLOYER=${CREDENCE_DEPLOYER_KEY:-ac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80}
CLIENT=$(cast wallet address --private-key "0x$DEPLOYER")

# The agent's owner must NOT be the publisher: ERC-8004 refuses
# self-feedback, which is why Credence reports as a separate client.
AGENT_OWNER_KEY=${CREDENCE_AGENT_KEY:-59c6995e998f97a5a0044966f0945389dc9e86dae88c7a8412f4603b6b78690d}
AGENT_OWNER=$(cast wallet address --private-key "0x$AGENT_OWNER_KEY")

LOANS=${LOANS:-2}

echo "=== ERC-8004 feedback, end to end ==="
echo "chain     $CHAIN_ID"
echo "client    $CLIENT   (publishes)"
echo "agent own $AGENT_OWNER   (owns the identity)"
echo

# A published key may sign nothing on a chain other people can reach.
. "$ROOT/scripts/lib/keys.sh"
credence_require_private_key "$DEPLOYER" "$CHAIN_ID" deployer CREDENCE_DEPLOYER_KEY
credence_require_private_key "$AGENT_OWNER_KEY" "$CHAIN_ID" \
  "agent owner" CREDENCE_AGENT_KEY

cd "$ROOT/contracts"
rm -rf broadcast/*/"$CHAIN_ID" cache/*.s.sol/"$CHAIN_ID"

if [ -n "${REPUTATION_REGISTRY:-}" ] && [ -n "${IDENTITY_REGISTRY:-}" ]; then
  # A real deployment. The agent must already be registered there: this
  # script will not mint an identity on a public registry as a side
  # effect of running a demo.
  REPUTATION=$REPUTATION_REGISTRY
  IDENTITY=$IDENTITY_REGISTRY
  AGENT_ID=${AGENT_ID:?set AGENT_ID to a registered agent on this registry}
  echo "using the canonical registries on this chain"
else
  # A local double is only ever acceptable on a local chain.
  #
  # Without this, forgetting the two environment variables against a
  # public RPC would deploy a mock registry there and publish to it --
  # producing a green run, real transactions, and a claim about ERC-8004
  # that rests on a contract this script deployed a moment earlier. The
  # whole point of the canonical registries is that Credence does not
  # control them. Reported by review, 14 September.
  case "$RPC" in
    *127.0.0.1*|*localhost*|*0.0.0.0*) ;;
    *)
      echo "refusing to deploy a local ERC-8004 double against $RPC" >&2
      echo "set REPUTATION_REGISTRY, IDENTITY_REGISTRY and AGENT_ID to the" >&2
      echo "canonical deployment (see contracts/deployments/<chainId>.json)" >&2
      exit 1
      ;;
  esac
  echo "no registry given; deploying a local double"
  IDENTITY=$(forge create src/MockERC8004.sol:MockIdentityRegistry \
    --rpc-url "$RPC" --private-key "0x$DEPLOYER" --broadcast \
    2>&1 | grep "Deployed to:" | awk '{print $3}')
  REPUTATION=$(forge create src/MockERC8004.sol:MockReputationRegistry \
    --rpc-url "$RPC" --private-key "0x$DEPLOYER" --broadcast \
    --constructor-args "$IDENTITY" 2>&1 | grep "Deployed to:" | awk '{print $3}')
  AGENT_ID=${AGENT_ID:-1830}
  # Minted to the agent's own key, never to the publisher: an identity the
  # client owned could not receive feedback at all.
  cast send "$IDENTITY" "registerTo(address,uint256)" "$AGENT_OWNER" "$AGENT_ID" \
    --private-key "0x$DEPLOYER" --rpc-url "$RPC" >/dev/null
fi

echo "identity   $IDENTITY"
echo "reputation $REPUTATION"
echo "agent id   $AGENT_ID"
echo

# Confirm the topology the standard requires, before any loan runs.
OWNER_ON_CHAIN=$(cast call "$IDENTITY" "ownerOf(uint256)(address)" "$AGENT_ID" --rpc-url "$RPC")
echo "agent $AGENT_ID is owned by $OWNER_ON_CHAIN"
if [ "$(echo "$OWNER_ON_CHAIN" | tr 'A-Z' 'a-z')" = "$(echo "$CLIENT" | tr 'A-Z' 'a-z')" ]; then
  echo "the publisher owns this agent: ERC-8004 refuses self-feedback" >&2
  exit 1
fi
echo

# Loans to publish outcomes from. Bound to the REAL agent id, so the
# attribution the publisher derives names a registered identity.
echo "=== running loans bound to agent $AGENT_ID ==="
IDENTITY_REGISTRY=$IDENTITY AGENT_ID=$AGENT_ID LOANS=$LOANS \
  "$ROOT/scripts/demo-repay.sh" >/dev/null 2>&1
echo "loans run"

MANIFEST="$ROOT/contracts/deployments/$CHAIN_ID.json"
MANAGER=$(python3 -c "import json;print(json.load(open('$MANIFEST'))['addresses']['loan_manager'])")
echo "manager $MANAGER"
echo

echo "=== A: publish resolved obligations ==="
cd "$ROOT/gateway"
go build -o credence-publish ./cmd/credence-publish
CREDENCE_OPERATOR_KEY=$DEPLOYER ./credence-publish \
  -rpc "$RPC" -manager "$MANAGER" -registry "$REPUTATION" \
  -identity-registry "$IDENTITY" -erc8004 -chain-id "$CHAIN_ID"

# ERC-8004 does NOT refuse a duplicate: feedbackIndex auto-increments, so
# a retried queue would double-count every outcome. The guarantee has to
# come from the publisher reading back what it already wrote.
echo
echo "=== A: republish must add nothing ==="
CREDENCE_OPERATOR_KEY=$DEPLOYER ./credence-publish \
  -rpc "$RPC" -manager "$MANAGER" -registry "$REPUTATION" \
  -identity-registry "$IDENTITY" -erc8004 -chain-id "$CHAIN_ID"

echo
echo "=== B: read it back, from outside the repo ==="
# Copied out of the tree so the run cannot accidentally depend on
# anything beside it.
READER=$(mktemp -d)/read_erc8004.py
cp "$ROOT/tools/reader/read_erc8004.py" "$READER"
cd "$(dirname "$READER")"
python3 "$READER" --rpc "$RPC" --registry "$REPUTATION" \
  --identity "$IDENTITY" --agent "$AGENT_ID" --client "$CLIENT" \
  --manager "$MANAGER" --expect-chain "$CHAIN_ID"
