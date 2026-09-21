#!/usr/bin/env bash
# Read this deployment's reputation from the canonical ERC-8004 registry,
# the way a stranger would.
#
# `docs/11` asks for an INDEPENDENT reader: the claim is that a third
# party can price this agent's history without trusting Credence, so the
# check cannot run through Credence's own gateway. It makes one
# `eth_call` against the registry named in the deployment manifest and
# decodes the reply.
#
# Read-only: no key, no funds, no transaction. That is the point -- a row
# that only a funded operator could check would be evidence about the
# operator rather than about the registry.
#
# The obligations are published by `scripts/demo-erc8004.sh`, which DOES
# need funded keys. This script only reads what that already wrote, so it
# stays runnable at every release check rather than once per funded run.
set -euo pipefail

ROOT=$(cd "$(dirname "$0")/.." && pwd)
RPC=${RPC:-https://testnet-rpc.monad.xyz}
CHAIN_ID=${CHAIN_ID:-10143}
MANIFEST="$ROOT/contracts/deployments/$CHAIN_ID.json"

[ -f "$MANIFEST" ] || {
  echo "no manifest at $MANIFEST" >&2
  exit 1
}

read -r REGISTRY AGENT CLIENT <<<"$(python3 -c "
import json, sys
d = json.load(open('$MANIFEST')).get('erc8004')
if not d:
    sys.exit('manifest has no erc8004 block -- run scripts/demo-erc8004.sh')
print(d['reputation_registry'], d['agent_id'], d['credence_client'])
")"

echo "=== ERC-8004 reputation, read independently ==="
echo "  chain     $CHAIN_ID"
echo "  registry  $REGISTRY"
echo "  agent     $AGENT"
echo

# The five-argument form. `probe-testnet.sh` records that the prose
# documents a shorter one that the deployed registry does not have: the
# wrong arity reverts with EMPTY data, which reads like a missing agent
# and is not one.
DATA=$(cast calldata "readAllFeedback(uint256,address[],string,string,bool)" \
  "$AGENT" "[]" "" "" false)

RAW=$(cast call "$REGISTRY" --data "$DATA" --rpc-url "$RPC")
[ -n "$RAW" ] && [ "$RAW" != "0x" ] || {
  echo "FAIL  the registry returned nothing for agent $AGENT" >&2
  exit 1
}

echo "$RAW" >"$ROOT/.reputation.hex"
trap 'rm -f "$ROOT/.reputation.hex"' EXIT

# Decoded by the project's own decoder, which `decode_test.go` pins
# against a response the live registry really returned.
cd "$ROOT/gateway"
go run ./cmd/credence-reputation \
  -hex "$ROOT/.reputation.hex" \
  -client "$CLIENT" \
  -agent "$AGENT"
