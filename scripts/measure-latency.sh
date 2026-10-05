#!/usr/bin/env bash
# Measure the three latencies `docs/09` asks for, on the deployment the
# manifest names:
#
#   request to confirmation   a transaction sent -> its receipt
#   event to index            that receipt -> the Envio index showing it
#   input to assessment       a snapshot sent to the engine -> its answer
#
# `docs/09` is specific about where: "in the submitted deployment", and
# "do not attribute offchain provider speed to the chain". So this deploys
# nothing. It adds one credit account to the facility the manifest already
# names, the way `demo-cohort.sh` adds its agents, and runs LOANS short
# loans through `credence-scenario` exactly as the demos do. The existing
# loans are not touched.
#
# The prototype runs unmodified. `scripts/lib/latency-proxy.py` sits in
# front of the RPC and the risk service and timestamps what crosses them,
# so every figure is a request the prototype made, measured on this
# machine. The record lists every transaction hash, so each figure can be
# checked on chain.
#
#   bash scripts/measure-latency.sh                      # local anvil
#   RPC=https://testnet-rpc.monad.xyz \
#   CREDENCE_DEPLOYER_KEY=... CREDENCE_SIGNER_KEY=... CREDENCE_AGENT_KEY=... \
#     bash scripts/measure-latency.sh                    # Monad testnet
#
# The event-to-index interval needs the indexer running against the same
# chain: `scripts/index-local.sh` for anvil, `scripts/index-testnet.sh` for
# Monad testnet. Without one, that section is recorded as skipped.
set -euo pipefail

RPC=${RPC:-http://127.0.0.1:8546}
ROOT=$(cd "$(dirname "$0")/.." && pwd)
CHAIN_ID=$(cast chain-id --rpc-url "$RPC")
LOANS=${LOANS:-3}
PRINCIPAL=${PRINCIPAL:-1500000000}
# Hasura publishes on IPv6. When something else holds the IPv4 literal,
# "localhost" can reach that instead (see scripts/index-local.sh).
GRAPHQL_DEFAULT=http://localhost:8080/v1/graphql
if lsof -nP -iTCP:8080 -sTCP:LISTEN 2>/dev/null | grep -q "TCP 127.0.0.1:8080"; then
  GRAPHQL_DEFAULT='http://[::1]:8080/v1/graphql'
fi
GRAPHQL=${GRAPHQL:-$GRAPHQL_DEFAULT}
DAY=$(date -u +%Y-%m-%d)
OUT=${LATENCY_OUT:-$ROOT/releases/latency-$DAY.md}
WORK=$(mktemp -d /tmp/credence-latency.XXXXXX)

DEPLOYER=${CREDENCE_DEPLOYER_KEY:-ac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80}
SIGNER=${CREDENCE_SIGNER_KEY:-5de4111afa1a4b94908f83103eb1f1706367c2e68ca870fc3fb9a804cdab365a}
. "$ROOT/scripts/lib/keys.sh"
# A local chain warps time, so a hold costs nothing, and it must cover the
# 1,200-second forecast horizon `credence-scenario` commits there. A public
# chain cannot warp; the scenario shortens the horizon to the hold, so ten
# real seconds do, inside a ten-minute term.
if credence_is_local_chain "$CHAIN_ID"; then
  TERM_SECONDS=${TERM_SECONDS:-86400}
  HOLD_SECONDS=${HOLD_SECONDS:-1200}
else
  TERM_SECONDS=${TERM_SECONDS:-600}
  HOLD_SECONDS=${HOLD_SECONDS:-10}
fi
credence_require_private_key "$SIGNER" "$CHAIN_ID" attestor CREDENCE_SIGNER_KEY
credence_require_private_key "$DEPLOYER" "$CHAIN_ID" deployer CREDENCE_DEPLOYER_KEY
DEPLOYER=${DEPLOYER#0x}
SIGNER=${SIGNER#0x}

MANIFEST="$ROOT/contracts/deployments/$CHAIN_ID.json"
[ -f "$MANIFEST" ] || { echo "no manifest for chain $CHAIN_ID: deploy first" >&2; exit 1; }
addr() { python3 -c "import json;print(json.load(open('$MANIFEST'))['addresses']['$1'])"; }
erc8004() { python3 -c "import json;print((json.load(open('$MANIFEST')).get('erc8004') or {}).get('$1',''))"; }
ORACLE=$(addr oracle)
VAULT=$(addr vault)
MANAGER=$(addr loan_manager)
SETTLEMENT=$(addr settlement)
RISKY=$(addr risky)
ADAPTER=$(addr adapter)
PRICE_SOURCE=$(addr price_source)
IDENTITY_REGISTRY=$(erc8004 identity_registry)
AGENT_ID=${AGENT_ID:-$(erc8004 agent_id)}
IDENTITY_REGISTRY=${IDENTITY_REGISTRY:-0x000000000000000000000000000000000000aec0}
AGENT_ID=${AGENT_ID:-184}
if ! credence_is_local_chain "$CHAIN_ID" && [ "$AGENT_ID" = 184 ]; then
  echo "refusing to bind loans to placeholder agent 184 on chain $CHAIN_ID" >&2
  exit 1
fi

# The index interval needs an indexer on THIS chain and THIS manager. One
# watching another deployment would never show these loans, and every
# sample would time out rather than fail.
INDEX_URL=$GRAPHQL
indexed=$(curl -s -m 5 -X POST "$GRAPHQL" -H 'content-type: application/json' \
  -d "{\"query\":\"{Loan(where:{chainId:{_eq:$CHAIN_ID}},limit:1){id}}\"}" 2>/dev/null || true)
if ! echo "$indexed" | grep -q "\"id\":\"$CHAIN_ID-"; then
  echo "no indexer serving chain $CHAIN_ID at $GRAPHQL: event-to-index will be skipped" >&2
  INDEX_URL=""
fi

cat <<INFO
--- latency ---
  chain     $CHAIN_ID ($RPC)
  manager   $MANAGER
  loans     $LOANS of $PRINCIPAL, held ${HOLD_SECONDS}s each, on a NEW account
  index     ${INDEX_URL:-skipped}
INFO

# Nothing below may fail silently, and nothing may outlive the run.
PIDS=()
trap 'for p in "${PIDS[@]:-}"; do [ -n "$p" ] && kill "$p" 2>/dev/null; done; true' EXIT

cd "$ROOT/risk"
cargo build --quiet --bin credence-risk-service
RISK_SERVICE_ADDR=127.0.0.1:18115 ./target/debug/credence-risk-service >"$WORK/risk.log" 2>&1 &
PIDS+=($!)
for _ in $(seq 40); do
  curl -sf http://127.0.0.1:18115/health >/dev/null 2>&1 && break
  sleep 0.25
done

OBS="$WORK/observations.jsonl"
python3 "$ROOT/scripts/lib/latency-proxy.py" \
  --rpc "$RPC" --rpc-port 18545 --risk http://127.0.0.1:18115 --risk-port 18116 \
  --graphql "$INDEX_URL" --manager "$MANAGER" --chain-id "$CHAIN_ID" \
  --originated-topic "$(cast keccak 'Originated(uint256,address,uint256)')" \
  --repaid-topic "$(cast keccak 'Repaid(uint256,uint256,uint256)')" \
  --out "$OBS" >"$WORK/proxy.log" 2>&1 &
PIDS+=($!)
for _ in $(seq 100); do
  grep -q "ready" "$WORK/proxy.log" 2>/dev/null && break
  sleep 0.2
done
grep -q "ready" "$WORK/proxy.log" || { cat "$WORK/proxy.log" >&2; exit 1; }
PROXY=http://127.0.0.1:18545

# One more account on the live facility. `--slow` sends each transaction
# after the previous one confirms, so no sample waits behind another.
cd "$ROOT/contracts"
export DEPLOYER_KEY=0x$DEPLOYER
export AGENT_ADDRESS=$(cast wallet address --private-key \
  "0x${CREDENCE_AGENT_KEY:-59c6995e998f97a5a0044966f0945389dc9e86dae88c7a8412f4603b6b78690d}")
export SETTLEMENT_TOKEN=$SETTLEMENT VAULT_ADDRESS=$VAULT
export LOAN_MANAGER_ADDRESS=$MANAGER RISKY_ASSET=$RISKY STRATEGY_ADAPTER=$ADAPTER
export AGENT_EQUITY=${AGENT_EQUITY:-4000000000}
export AGENT_RISKY_EQUITY=${AGENT_RISKY_EQUITY:-2000000000000000000}
if ! forge script script/AddAccount.s.sol --rpc-url "$PROXY" --broadcast --slow \
  >"$WORK/account.log" 2>&1; then
  tail -30 "$WORK/account.log" >&2
  exit 1
fi
ACCOUNT=$(grep -o 'credit account 0x[0-9a-fA-F]\{40\}' "$WORK/account.log" | awk '{print $3}')
[ -n "$ACCOUNT" ] || { tail -30 "$WORK/account.log" >&2; exit 1; }
echo "  account   $ACCOUNT"

cd "$ROOT/gateway"
go build -o "$WORK/credence-scenario" ./cmd/credence-scenario
# -enroll is not optional: the forecast the engine requires before it
# prices a loan is committed to that database.
CREDENCE_OPERATOR_KEY=$DEPLOYER CREDENCE_SIGNER_KEY=$SIGNER \
  "$WORK/credence-scenario" -rpc "$PROXY" -oracle "$ORACLE" -manager "$MANAGER" \
  -account "$ACCOUNT" -settlement "$SETTLEMENT" -principal "$PRINCIPAL" \
  -adapter "$ADAPTER" -risky "$RISKY" -price-source "$PRICE_SOURCE" \
  -chain-id "$CHAIN_ID" -deploy-bps 10000 -loans "$LOANS" \
  -term-seconds "$TERM_SECONDS" -hold-seconds "$HOLD_SECONDS" \
  -identity-registry "$IDENTITY_REGISTRY" -agent-id "$AGENT_ID" \
  -enroll "$WORK/latency.db" \
  -vault "$VAULT" -risk http://127.0.0.1:18116 -policy "$ROOT/config/engine-policy.json" \
  >"$WORK/loans.log" 2>&1 || {
  tail -30 "$WORK/loans.log" >&2
  exit 1
}
grep -oE 'loan [0-9]+: engine (terms|declined).*' "$WORK/loans.log" | sed 's/^/  /' || true
# A decline is the engine working, but it is not a latency sample: say so
# rather than writing a record with no loans in it.
if grep -q 'engine declined' "$WORK/loans.log"; then
  echo "the engine declined a loan, so this run measured nothing" >&2
  exit 1
fi

# The scenario always leaves its last loan open. Close it the way the
# scenario closes the others -- unwind the position through the account's
# de-risking action, then repay from the proceeds -- so the run leaves no
# new debt on the deployment. If that fails, say so and keep the record.
OPEN=$(cast call "$MANAGER" 'activeLoan(address)(uint256)' "$ACCOUNT" --rpc-url "$RPC" | awk '{print $1}')
if [ "$OPEN" != 0 ]; then
  HELD=$(cast call "$RISKY" 'balanceOf(address)(uint256)' "$ACCOUNT" --rpc-url "$RPC" | awk '{print $1}')
  if { [ "$HELD" = 0 ] || {
    cast send "$ACCOUNT" 'approveAdapter(address,address,uint256)' "$RISKY" "$ADAPTER" "$HELD" \
      --private-key "0x$DEPLOYER" --rpc-url "$PROXY" >/dev/null &&
      cast send "$ACCOUNT" 'execute(address,bytes)' "$ADAPTER" \
        "$(cast calldata 'unwind(uint256)' "$HELD")" \
        --private-key "0x$DEPLOYER" --rpc-url "$PROXY" >/dev/null
  }; } 2>"$WORK/close.err" &&
    cast send "$MANAGER" 'repay(uint256)' "$OPEN" \
      --private-key "0x$DEPLOYER" --rpc-url "$PROXY" >/dev/null 2>>"$WORK/close.err"; then
    echo "  repaid    loan $OPEN, the one the scenario leaves open"
  else
    echo "  loan $OPEN left open: $(tail -1 "$WORK/close.err")" >&2
  fi
fi

# Let the last receipts and index rows land. The proxy writes an
# index_wait for every loan event it sees confirmed, and one index or
# index_timeout when that watch ends, so the run waits for exactly the
# events that happened.
sleep 2
for _ in $(seq 1300); do
  waits=$(grep -c '"kind": "index_wait"' "$OBS" 2>/dev/null || true)
  done_=$(grep -cE '"kind": "index(_timeout)?"' "$OBS" 2>/dev/null || true)
  [ "${done_:-0}" -ge "${waits:-0}" ] && break
  sleep 0.5
done

mkdir -p "$(dirname "$OUT")"
cp "$OBS" "${OUT%.md}.jsonl"
python3 "$ROOT/scripts/lib/latency-report.py" "$OBS" "$OUT" "$ROOT" \
  date="$DAY" stamp="$(date -u +%Y-%m-%dT%H:%M:%SZ)" chain="$CHAIN_ID" rpc="$RPC" \
  manager="$MANAGER" account="$ACCOUNT" loans="$LOANS" principal="$PRINCIPAL" \
  hold="${HOLD_SECONDS}s" index="${INDEX_URL:-skipped}"
sed -n '/^## Request/,/^## Transactions/p' "$OUT" | sed '$d'
