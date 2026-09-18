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

# Write a LOCAL config rather than editing the committed one.
#
# An earlier version rewrote `indexer/config.yaml` in place and restored
# it from git on exit. The restore never ran: the script ends in `exec`,
# which replaces the shell and discards its EXIT trap, so every run left
# the repository dirty with an anvil address and a `skip:` flag. The
# generated file is gitignored, and the committed config stays the
# deployed truth.
CONFIG=config.local.yaml
cp "$ROOT/indexer/config.yaml" "$ROOT/indexer/$CONFIG"
python3 - "$ROOT/indexer/$CONFIG" "$CHAIN_ID" "$MANAGER" <<'PY'
import sys

path, chain_id, manager = sys.argv[1], sys.argv[2], sys.argv[3]
lines = open(path).read().splitlines()

# Line-based rather than a regex over the block. The regex version
# required each address line to end in a newline, so it rewrote a fresh
# `address: []` and then failed on its own output when the list had
# become the last line of the file -- "could not rewrite the address
# list", on the second run only. Scanning lines has no such edge.
start = next(
    (i for i, ln in enumerate(lines) if ln.strip() == f"- id: {chain_id}"), None
)
if start is None:
    raise SystemExit(f"no chain {chain_id} in {path}")

# The chain block runs to the next `- id:` at the same indent, or to EOF.
indent = len(lines[start]) - len(lines[start].lstrip())
end = len(lines)
for i in range(start + 1, len(lines)):
    ln = lines[i]
    if ln.strip().startswith("- id: ") and len(ln) - len(ln.lstrip()) == indent:
        end = i
        break

addr = next(
    (i for i in range(start, end) if lines[i].strip().startswith("address:")), None
)
if addr is None:
    raise SystemExit(f"no address key for chain {chain_id} in {path}")

# Drop whatever the list currently holds: `[]`, or one or more entries.
tail = addr + 1
while tail < end and lines[tail].strip().startswith("- 0x"):
    tail += 1

pad = " " * (len(lines[addr]) - len(lines[addr].lstrip()))
lines[addr:tail] = [f"{pad}address:", f"{pad}  - {manager}"]

open(path, "w").write("\n".join(lines) + "\n")
PY

cd "$ROOT/indexer"
pnpm install --frozen-lockfile >/dev/null

if ! docker info >/dev/null 2>&1; then
  echo >&2
  echo "Docker is not responding. HyperIndex runs Postgres in it." >&2
  echo "Start Docker Desktop, then re-run this script." >&2
  echo >&2
  echo "If it hangs rather than refusing, the Linux VM is wedged: the" >&2
  echo "socket accepts connections and never answers. Quit Docker" >&2
  echo "Desktop and reopen it." >&2
  exit 1
fi

# Envio decides its Postgres is already running by probing the PORT, but
# Hasura reaches it by CONTAINER NAME. Anything else listening on 5433 --
# another project's database, say -- makes envio skip creating
# `envio-postgres`, and Hasura then fails every query with "could not
# translate host name envio-postgres". Observed exactly that, 18
# September: the sync hung on "Waiting for Hasura" and exited.
#
# So the port is checked rather than assumed, and moved when taken.
ENVIO_PG_PORT=${ENVIO_PG_PORT:-5433}
if lsof -iTCP:"$ENVIO_PG_PORT" -sTCP:LISTEN >/dev/null 2>&1 &&
  ! docker ps --format '{{.Names}}' | grep -qx envio-postgres; then
  for candidate in 5440 5441 5442 5443; do
    if ! lsof -iTCP:"$candidate" -sTCP:LISTEN >/dev/null 2>&1; then
      echo "port $ENVIO_PG_PORT is taken by something that is not" \
        "envio-postgres; using $candidate"
      ENVIO_PG_PORT=$candidate
      break
    fi
  done
fi
export ENVIO_PG_PORT

# Hasura's port, with the same problem one layer up. Docker publishes
# 8080 on IPv6 (`*:8080`), and a process already holding IPv4
# `127.0.0.1:8080` does not stop it binding -- but envio then polls
# `localhost:8080`, resolves IPv4 first, and reads the OTHER server's 404
# as "Hasura is not up yet". It waits forever and exits. Observed 18
# September against an unrelated local server.
#
# So probe the endpoint envio will actually use, and fall back to the
# IPv6 literal when the IPv4 listener is somebody else's.
if [ -z "${HASURA_GRAPHQL_ENDPOINT:-}" ]; then
  HASURA_GRAPHQL_ENDPOINT=http://localhost:8080/v1/metadata
  # Is the IPv4 listener somebody else's? Docker's published port answers
  # on IPv6, so if an IPv4 process holds 8080 and it is not Hasura,
  # address Hasura by its IPv6 literal instead of racing name
  # resolution. Checked by asking: Hasura answers /healthz with 200 and
  # serves /v1/metadata; an unrelated server 404s.
  if lsof -nP -iTCP:8080 -sTCP:LISTEN 2>/dev/null | grep -q "TCP 127.0.0.1:8080"; then
    echo "something else holds IPv4 127.0.0.1:8080; using [::1] for Hasura"
    HASURA_GRAPHQL_ENDPOINT='http://[::1]:8080/v1/metadata'
  fi
fi
export HASURA_GRAPHQL_ENDPOINT
echo "hasura at $HASURA_GRAPHQL_ENDPOINT"

# The indexer's own metrics port. A previous run that was interrupted
# rather than stopped keeps holding it, and the next run then dies with
# "Port 9898 is already in use" AFTER reporting that storage is ready --
# which reads like a sync that simply produced nothing. Clear our own
# leftovers; refuse to kill anything else.
if leftover=$(lsof -ti :9898 2>/dev/null) && [ -n "$leftover" ]; then
  for pid in $leftover; do
    if ps -p "$pid" -o command= 2>/dev/null | grep -q "envio"; then
      echo "stopping a leftover indexer on port 9898 (pid $pid)"
      kill -9 "$pid" 2>/dev/null || true
    else
      echo "port 9898 is held by something that is not envio (pid $pid)." >&2
      echo "Set ENVIO_INDEXER_PORT to a free port and re-run." >&2
      exit 1
    fi
  done
fi

# HyperSync needs an Envio API token, and the config names two chains:
# 10143 over HyperSync and 31337 over RPC. Envio initialises BOTH before
# indexing either, so with no token a purely local run dies on the
# testnet chain it was never going to reach -- "An Envio API token is
# required for using HyperSync as a data-source".
#
# With a token, index both. Without one, skip 10143 and say so, rather
# than failing on a chain this script does not need.
if [ -z "${ENVIO_API_TOKEN:-}" ]; then
  echo "no ENVIO_API_TOKEN; indexing the local chain only"
  echo "  (10143 needs one: https://envio.dev/app/api-tokens)"
  python3 - "$ROOT/indexer/$CONFIG" <<'PY'
import sys

path = sys.argv[1]
lines = open(path).read().splitlines()
for i, line in enumerate(lines):
    if line.strip() == "- id: 10143":
        pad = " " * (len(line) - len(line.lstrip()) + 2)
        if lines[i + 1].strip() != "skip: true":
            lines.insert(i + 1, f"{pad}skip: true")
        break
open(path, "w").write("\n".join(lines) + "\n")
PY
fi

# Codegen against the SAME config the indexer will run. Generating from
# the committed `config.yaml` while running `config.local.yaml` bakes in
# a different chain list than the one being indexed.
./node_modules/.bin/envio codegen --config "$CONFIG"

# Bring Postgres and Hasura up FIRST, and only then start indexing.
#
# This is the one that cost the most to find. When `envio dev` has to
# create its own containers, it reports "Waiting for Hasura ... ready"
# and then sits forever without binding its port or creating a table --
# no error, no log line, just a process that never proceeds. Run again
# once the containers are up and it syncs in under a second.
#
# So the containers are started by a throwaway invocation whose hang is
# expected and ignored, and the real run follows against a warm
# environment.

# Start from an empty database every time.
#
# Envio generates a fresh Postgres password on each run, but the data
# volume outlives the container and keeps the OLD one. A container
# revived against that volume rejects envio's new credentials -- "password
# authentication failed for user postgres" -- while `docker ps` still
# reports it up and healthy, and `pg_isready` still says yes, because
# `docker exec` connects over the local socket and never presents a
# password at all. Neither check sees the failure envio hits over TCP.
#
# Rather than detect it, avoid it: the volume holds nothing worth keeping
# because this script re-indexes from scratch with `-r` regardless, and
# the local chain it indexes is itself thrown away on every demo run.
# Hasura goes too — it pools against the database that just disappeared.
if docker ps -a --format '{{.Names}}' | grep -qxE 'envio-postgres|envio-hasura'; then
  echo "clearing the previous indexer database"
  docker rm -f envio-postgres envio-hasura >/dev/null 2>&1 || true
  docker volume rm envio-postgres-data >/dev/null 2>&1 || true
fi

if ! docker ps --format '{{.Names}}' | grep -qx envio-postgres ||
  ! docker ps --format '{{.Names}}' | grep -qx envio-hasura; then
  echo "starting Postgres and Hasura (the first run only brings them up)"
  ./node_modules/.bin/envio dev -r --config "$CONFIG" >/dev/null 2>&1 &
  starter=$!
  # BOTH containers, and Postgres answering -- not just Hasura healthy.
  # envio creates Hasura first, so waiting on it alone kills the starter
  # while Postgres is still being created, and the next step then finds
  # no database at all.
  for _ in $(seq 90); do
    if docker ps --format '{{.Names}}\t{{.Status}}' |
      grep -q "^envio-hasura.*healthy" &&
      docker exec envio-postgres pg_isready -U postgres >/dev/null 2>&1; then
      break
    fi
    sleep 2
  done
  kill "$starter" 2>/dev/null || true
  wait "$starter" 2>/dev/null || true
  # envio leaves the node process behind when killed this way.
  pkill -f "envio/bin.mjs" 2>/dev/null || true
  sleep 1
fi

# The binary directly rather than `pnpm exec`: under the pnpm wrapper the
# child is never spawned at all, which looks identical to the hang above.
exec ./node_modules/.bin/envio dev -r --config "$CONFIG"
