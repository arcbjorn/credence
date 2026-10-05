#!/usr/bin/env bash
# The central claim, as a single run: a limit that is earned.
#
# `README`: "the multiplier and the price are set by what it has
# demonstrated rather than by what it posts." Every other demo shows that
# in passing -- `demo-repay.sh` originates a handful of loans and the rate
# drifts down while nobody is looking at it. Nothing in the bundle was
# ABOUT the drift, so the one thing the project claims had no run of its
# own and no screen could plot it.
#
# This is that run. One agent borrows and repays the same principal
# repeatedly, and nothing else changes: same policy, same market, same
# asset, same term. The only moving input is the agent's own record. So
# the APR it is quoted and the capacity it is granted move for exactly one
# reason, and a reader can attribute the change without taking anyone's
# word for it.
#
# Read it beside `demo-default.sh`, which is the same claim from the other
# end: a record that goes bad costs the lender real money.
set -euo pipefail

RPC=${RPC:-http://127.0.0.1:8546}
# Read from the node, not assumed. The deploy script writes its manifest
# keyed by chain id, so a hardcoded 31337 silently reads a stale local
# deployment when pointed at a testnet.
CHAIN_ID=${CHAIN_ID:-$(cast chain-id --rpc-url "$RPC")}
ROOT=$(cd "$(dirname "$0")/.." && pwd)
WORK=${WORK:-/tmp/credence-growth}
# A fresh database each run: a stale one carries forecasts and history
# from a previous chain, which would credit the agent for loans this run
# never made -- and this is the one run whose whole subject is history.
rm -rf "$WORK" && mkdir -p "$WORK"
# Anvil account #0 by default. On a public chain, export a funded key:
# this one is published in the anvil banner and anyone can drain it.
DEPLOYER=${CREDENCE_DEPLOYER_KEY:-ac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80}
SIGNER=${CREDENCE_SIGNER_KEY:-5de4111afa1a4b94908f83103eb1f1706367c2e68ca870fc3fb9a804cdab365a}
# Deliberately modest and CONSTANT.
#
# The point is that the same request is priced differently as the record
# grows. Raising the principal each time would move the rate too -- the
# stress term is a function of the book -- and the two causes could not be
# told apart afterwards.
PRINCIPAL=${PRINCIPAL:-3000000000}
# Long enough for the curve to have a shape. Eight points show the
# discount compounding and then flattening as it reaches the policy's
# clamp, which is the honest picture: the discount is bounded, and a
# reader should see the bound rather than an extrapolation.
LOANS=${LOANS:-8}
TERM_SECONDS=${TERM_SECONDS:-$((10 * 86400))}
HOLD_SECONDS=${HOLD_SECONDS:-$((5 * 86400))}
IDENTITY_REGISTRY=${IDENTITY_REGISTRY:-}
AGENT_ID=${AGENT_ID:-}

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
  holding constant  principal, term, market, policy, asset
  varying           the agent's own repayment record, and nothing else
NOTE

cd "$ROOT/contracts"
# Forge replays nonces from its own broadcast cache. A chain that has been
# restarted -- or a different chain reusing the id -- resets those nonces,
# and forge then retries "nonce too low" with no useful output until it
# gives up. Drop this chain's cache so a deploy starts from the node's
# real nonce.
rm -rf broadcast/*/"$CHAIN_ID" cache/*.s.sol/"$CHAIN_ID"
export DEPLOYER_KEY=0x$DEPLOYER
export ATTESTOR_ADDRESS=$(cast wallet address --private-key "0x$SIGNER")
export DEPLOYMENT_ID=7
. "$ROOT/scripts/lib/manifest.sh"
MANIFEST_KEEP=$(credence_manifest_snapshot \
  "$ROOT/contracts/deployments/$CHAIN_ID.json")
forge script script/Deploy.s.sol --rpc-url "$RPC" --broadcast --slow >/dev/null
credence_manifest_restore "$ROOT/contracts/deployments/$CHAIN_ID.json" \
  "$MANIFEST_KEEP"
MANIFEST="$ROOT/contracts/deployments/$CHAIN_ID.json"
addr() { python3 -c "import json;print(json.load(open('$MANIFEST'))['addresses']['$1'])"; }
ORACLE=$(addr oracle);             VAULT=$(addr vault)
MANAGER=$(addr loan_manager);      SETTLEMENT=$(addr settlement)
RISKY=$(addr risky);               ADAPTER=$(addr adapter)
PRICE_SOURCE=$(addr price_source)

# Resolve the identity from the manifest when it records one, so a chain
# with real registries cannot silently bind loans to the placeholder.
erc8004() {
  python3 -c "import json;d=json.load(open('$MANIFEST')).get('erc8004') or {};print(d.get('$1',''))"
}
IDENTITY_REGISTRY=${IDENTITY_REGISTRY:-$(erc8004 identity_registry)}
AGENT_ID=${AGENT_ID:-$(erc8004 agent_id)}
IDENTITY_REGISTRY=${IDENTITY_REGISTRY:-0x000000000000000000000000000000000000aec0}
AGENT_ID=${AGENT_ID:-184}
# Loans bound to an unregistered agent cannot be published as that
# agent's history, and the failure is invisible until a reader reports
# zero obligations. Fail here instead, where the cause is legible.
if ! credence_is_local_chain "$CHAIN_ID" 2>/dev/null; then
  if [ "$AGENT_ID" = "184" ]; then
    echo "refusing to bind loans to placeholder agent 184 on chain $CHAIN_ID" >&2
    echo "record an erc8004 block in $MANIFEST, or set AGENT_ID" >&2
    exit 1
  fi
fi
echo "identity       agent $AGENT_ID at $IDENTITY_REGISTRY"

export AGENT_ADDRESS=$(cast wallet address --private-key \
  "0x${CREDENCE_AGENT_KEY:-59c6995e998f97a5a0044966f0945389dc9e86dae88c7a8412f4603b6b78690d}")
export SETTLEMENT_TOKEN=$SETTLEMENT VAULT_ADDRESS=$VAULT
export LOAN_MANAGER_ADDRESS=$MANAGER RISKY_ASSET=$RISKY STRATEGY_ADAPTER=$ADAPTER
SCENARIO_LOG="$WORK/scenario-deploy.log"
if ! forge script script/Scenario.s.sol --rpc-url "$RPC" --broadcast --slow \
  >"$SCENARIO_LOG" 2>&1; then
  echo "the scenario script failed:" >&2
  tail -40 "$SCENARIO_LOG" >&2
  exit 1
fi
ACCOUNT=$(grep -o 'credit account 0x[0-9a-fA-F]\{40\}' "$SCENARIO_LOG" | awk '{print $3}')
if [ -z "$ACCOUNT" ]; then
  echo "the scenario script ran but printed no credit account:" >&2
  tail -40 "$SCENARIO_LOG" >&2
  exit 1
fi
echo "credit account $ACCOUNT"

# The engine prices every loan. Without it the scenario signs fixed terms,
# and a run whose entire subject is the engine repricing would be a
# sequence of identical numbers.
cd "$ROOT/risk"
cargo build --quiet 2>/dev/null || true
RISK_SERVICE_ADDR=127.0.0.1:18113 ./target/debug/credence-risk-service \
  >"$WORK/risk-growth.log" 2>&1 &
RISK_PID=$!
trap 'kill $RISK_PID 2>/dev/null || true' EXIT
for _ in $(seq 40); do
  curl -sf http://127.0.0.1:18113/health >/dev/null 2>&1 && break
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
  -enroll "$WORK/scenario.db" -vault "$VAULT" \
  -risk http://127.0.0.1:18113 -policy "$ROOT/config/engine-policy.json"

# `docs/11`: a passing unit test and a correctly wired deployment are
# different claims. This asserts the second against real balances.
echo "=== deployment verification ==="
go build -o credence-verify ./cmd/credence-verify
./credence-verify -rpc "$RPC" -manager "$MANAGER" -account "$ACCOUNT" \
  -settlement "$SETTLEMENT" -owner "$(cast wallet address --private-key "0x$DEPLOYER")" \
  -vault "$VAULT" -risky "$RISKY" -json "$WORK/checks.json"

# The curve itself, read back from what was retained.
#
# Printed here because a reader running the script should see the claim
# land in the terminal rather than having to open the interface to find
# out whether it did. The same rows are what `/earned` plots.
echo
echo "=== what the record earned ==="
python3 - "$WORK/scenario.db" <<'PY'
import json, sqlite3, sys

db = sqlite3.connect(f"file:{sys.argv[1]}?mode=ro", uri=True)
db.row_factory = sqlite3.Row
rows = [dict(r) for r in db.execute(
    "SELECT sequence, payload FROM assessments ORDER BY sequence")]
db.close()

print(f"  {'loan':>4}  {'APR':>8}  {'capacity':>14}  bound by")
first = last = None
for r in rows:
    p = r["payload"]
    if isinstance(p, (bytes, bytearray)):
        p = p.decode("utf-8", "replace")
    p = json.loads(p)
    out = p.get("outcome") or {}
    terms = out.get("terms") or {}
    cap = out.get("capacity") or {}
    apr = terms.get("apr_bps")
    if apr is None:
        continue
    if first is None:
        first = apr
    last = apr
    print(f"  {r['sequence']:>4}  {apr/100:>7.2f}%  "
          f"{int(cap.get('agent_capacity', 0))/1e9:>13.3f}B  "
          f"{cap.get('agent_binding', '')}")

# State the delta rather than leaving it to be eyeballed. If the rate did
# NOT fall, this run failed to demonstrate its own subject, and saying so
# is more useful than printing a table that looks fine.
if first is not None and last is not None:
    if last < first:
        print(f"\n  the same request repriced from {first/100:.2f}% to "
              f"{last/100:.2f}% on record alone "
              f"({(first - last)/100:.2f} points earned)")
    else:
        print(f"\n  NOTE: the rate did not fall ({first} -> {last} bps). "
              f"This run demonstrates nothing about earned credit.")
PY
