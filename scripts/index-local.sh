#!/usr/bin/env bash
# Run the Envio indexer against the local demo chain.
#
# Anvil redeploys the loan manager at a fresh address every run, so the
# config carries an empty address list for 31337 and this script writes
# the current one from the deployment manifest before starting. A
# hardcoded address would index a contract that no longer exists and
# report an empty lifecycle as a successful sync -- the failure mode the
# manifest exists to prevent everywhere else in this repository.
#
# Prerequisites: a chain with demo data on it (`scripts/demo-repay.sh`)
# and Docker, which HyperIndex uses for its Postgres.
set -euo pipefail

ROOT=$(cd "$(dirname "$0")/.." && pwd)
RPC=${RPC:-http://127.0.0.1:8546}
CHAIN_ID=$(cast chain-id --rpc-url "$RPC")
MANIFEST="$ROOT/contracts/deployments/$CHAIN_ID.json"

if [ ! -f "$MANIFEST" ]; then
  echo "no manifest at $MANIFEST -- run scripts/demo-repay.sh first" >&2
  exit 1
fi

MANAGER=$(python3 -c \
  "import json;print(json.load(open('$MANIFEST'))['addresses']['loan_manager'])")
echo "indexing LoanManager $MANAGER on chain $CHAIN_ID"

# Rewrite only the 31337 address list, leaving the deployed testnet entry
# untouched: editing the file in place keeps one config rather than two
# that can disagree.
python3 - "$ROOT/indexer/config.yaml" "$CHAIN_ID" "$MANAGER" <<'PY'
import re
import sys

path, chain_id, manager = sys.argv[1], sys.argv[2], sys.argv[3]
text = open(path).read()

# Find the chain block for this id, then the address list inside it.
chain = re.search(rf"(  - id: {re.escape(chain_id)}\n(?:.*\n)*?)(?=  - id: |\Z)", text)
if chain is None:
    raise SystemExit(f"no chain {chain_id} in {path}")

block = chain.group(1)
updated = re.sub(
    r"(        address:)(?: \[\]|\n(?:          - .*\n)+)",
    rf"\1\n          - {manager}\n",
    block,
    count=1,
)
if updated == block:
    raise SystemExit(f"could not rewrite the address list for chain {chain_id}")

open(path, "w").write(text[: chain.start(1)] + updated + text[chain.end(1) :])
PY

cd "$ROOT/indexer"
pnpm install --frozen-lockfile >/dev/null
pnpm codegen

if ! docker info >/dev/null 2>&1; then
  echo >&2
  echo "Docker is not responding. HyperIndex runs Postgres in it." >&2
  echo "Start Docker Desktop, then re-run this script." >&2
  exit 1
fi

# `envio dev` serves GraphQL on :8080 and keeps syncing.
exec pnpm dev
