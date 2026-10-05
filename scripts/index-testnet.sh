#!/usr/bin/env bash
# Index the DEPLOYED chain — Monad testnet, 10143 — over HyperSync.
#
# The counterpart to `scripts/index-local.sh`, which indexes whatever
# anvil the demos just deployed. This one reads the chain the deployment
# manifest names, which is the only version of this that supports the
# `docs/08` claim: an integration is complete when the submitted
# environment demonstrates its actual behaviour, and a local chain is not
# that environment.
#
# Needs ENVIO_API_TOKEN in `indexer/.env` (free, envio.dev/app/api-tokens).
# HyperSync refuses without one, and the local script skips 10143 for
# exactly that reason.
#
# Observed 18 September: ~1.1M blocks from the manager's deploy block to
# the chain head, fetched in about 2.5 seconds.
set -euo pipefail

ROOT=$(cd "$(dirname "$0")/.." && pwd)
CONFIG=config.testnet.yaml

cd "$ROOT/indexer"

if [ ! -f .env ] || ! grep -q '^ENVIO_API_TOKEN=.' .env; then
  echo "no ENVIO_API_TOKEN in indexer/.env" >&2
  echo "HyperSync needs one: https://envio.dev/app/api-tokens" >&2
  echo "Copy .env.example to .env and fill it in." >&2
  exit 1
fi
set -a
# shellcheck disable=SC1091
. ./.env
set +a

if ! docker info >/dev/null 2>&1; then
  echo "Docker is not responding. HyperIndex runs Postgres in it." >&2
  exit 1
fi

# The committed config names the testnet manager by hand, and a redeploy
# moves the manifest without touching it. That happened on 19 September:
# this indexed the previous manager for two weeks, and nothing failed,
# because an index of the wrong contract is still a well-formed index.
INDEXED=$(python3 - <<'PY'
lines = open("config.yaml").read().splitlines()
for i, line in enumerate(lines):
    if line.strip() == "- id: 10143":
        for nxt in lines[i + 1:]:
            if nxt.strip().startswith("- 0x"):
                print(nxt.strip()[2:].lower())
                break
        break
PY
)
DEPLOYED=$(python3 -c "import json;print(json.load(open('$ROOT/contracts/deployments/10143.json'))['addresses']['loan_manager'].lower())")
if [ "$INDEXED" != "$DEPLOYED" ]; then
  echo "indexer/config.yaml indexes $INDEXED on 10143," >&2
  echo "but the manifest's loan_manager is $DEPLOYED." >&2
  echo "Point the config at the manifest's manager, with start_block set to" >&2
  echo "the block its code first appears at." >&2
  exit 1
fi

# Skip the local chain: this run is about the deployed one. Generated
# rather than committed, for the same reason config.local.yaml is.
cp config.yaml "$CONFIG"
python3 - "$CONFIG" <<'PY'
import sys

path = sys.argv[1]
lines = open(path).read().splitlines()
for i, line in enumerate(lines):
    if line.strip() == "- id: 31337":
        pad = " " * (len(line) - len(line.lstrip()) + 2)
        if lines[i + 1].strip() != "skip: true":
            lines.insert(i + 1, f"{pad}skip: true")
        break
open(path, "w").write("\n".join(lines) + "\n")
PY

# The same four environment collisions `index-local.sh` documents. See
# that script for why each one is here; the causes are identical.
ENVIO_PG_PORT=${ENVIO_PG_PORT:-5433}
if lsof -iTCP:"$ENVIO_PG_PORT" -sTCP:LISTEN >/dev/null 2>&1 &&
  ! docker ps --format '{{.Names}}' | grep -qx envio-postgres; then
  for candidate in 5440 5441 5442 5443; do
    if ! lsof -iTCP:"$candidate" -sTCP:LISTEN >/dev/null 2>&1; then
      ENVIO_PG_PORT=$candidate
      break
    fi
  done
fi
export ENVIO_PG_PORT

if [ -z "${HASURA_GRAPHQL_ENDPOINT:-}" ]; then
  HASURA_GRAPHQL_ENDPOINT=http://localhost:8080/v1/metadata
  if lsof -nP -iTCP:8080 -sTCP:LISTEN 2>/dev/null | grep -q "TCP 127.0.0.1:8080"; then
    HASURA_GRAPHQL_ENDPOINT='http://[::1]:8080/v1/metadata'
  fi
fi
export HASURA_GRAPHQL_ENDPOINT

if leftover=$(lsof -ti :9898 2>/dev/null) && [ -n "$leftover" ]; then
  for pid in $leftover; do
    if ps -p "$pid" -o command= 2>/dev/null | grep -q "envio"; then
      kill -9 "$pid" 2>/dev/null || true
    fi
  done
fi

# Always a fresh database: envio regenerates its Postgres password each
# run while the volume keeps the old one.
if docker ps -a --format '{{.Names}}' | grep -qxE 'envio-postgres|envio-hasura'; then
  echo "clearing the previous indexer database"
  docker rm -f envio-postgres envio-hasura >/dev/null 2>&1 || true
  docker volume rm envio-postgres-data >/dev/null 2>&1 || true
fi

./node_modules/.bin/envio codegen --config "$CONFIG"

# Containers first, then index. When envio has to create them itself it
# reports "ready" and then hangs without binding its port -- so the first
# invocation exists only to bring Postgres and Hasura up, and is stopped
# once BOTH answer. Killing on Hasura alone is too early: envio creates
# it first, and Postgres is still being created when the signal lands.
wait_for_containers() {
  local i
  for i in $(seq 120); do
    if docker exec envio-postgres pg_isready -U postgres >/dev/null 2>&1 &&
      docker ps --format '{{.Names}}\t{{.Status}}' |
      grep -q "^envio-hasura.*healthy"; then
      return 0
    fi
    sleep 2
  done
  return 1
}

if ! wait_for_containers; then
  echo "starting Postgres and Hasura"
  ./node_modules/.bin/envio dev -r --config "$CONFIG" >/dev/null 2>&1 &
  starter=$!
  if ! wait_for_containers; then
    echo "Postgres and Hasura did not come up in four minutes." >&2
    kill "$starter" 2>/dev/null || true
    exit 1
  fi
  kill "$starter" 2>/dev/null || true
  wait "$starter" 2>/dev/null || true
  sleep 2
fi

echo "indexing Monad testnet (10143) over HyperSync"
exec ./node_modules/.bin/envio dev -r --config "$CONFIG"
