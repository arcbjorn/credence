#!/usr/bin/env bash
# A market, not a borrower: several agents against one facility.
#
# Every other demo drives ONE credit account, so every screen that speaks
# of a market was rendering a single point with real chain history beside
# assessed profiles that never borrowed. The claim on the landing page is
# "one point per agent"; until this ran there was one agent.
#
# Four agents share a facility and differ in the two things an agent can
# actually differ in here: how much equity it posts, and how long a
# repayment record it builds. Each gets its own `CreditAccount`, its own
# originations priced by the engine, and its own row on chain.
#
# They are NOT four variations of one story. The cohort is chosen so the
# engine has to say something different about each:
#
#   sable    thin equity, one loan        -- small, unproven, cheap to serve
#   ember    mid equity, a short record   -- the discount starting to bite
#   cinder   deep equity, a long record   -- where the ceiling stops being history
#   ash      mid equity, borrows and stops -- an open position at the end
#
# Read it beside `demo-growth.sh`, which is one agent over time; this is
# several agents at one time.
set -euo pipefail

RPC=${RPC:-http://127.0.0.1:8546}
CHAIN_ID=${CHAIN_ID:-$(cast chain-id --rpc-url "$RPC")}
ROOT=$(cd "$(dirname "$0")/.." && pwd)
WORK=${WORK:-/tmp/credence-cohort}
rm -rf "$WORK" && mkdir -p "$WORK"

DEPLOYER=${CREDENCE_DEPLOYER_KEY:-ac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80}
SIGNER=${CREDENCE_SIGNER_KEY:-5de4111afa1a4b94908f83103eb1f1706367c2e68ca870fc3fb9a804cdab365a}
TERM_SECONDS=${TERM_SECONDS:-$((10 * 86400))}
HOLD_SECONDS=${HOLD_SECONDS:-$((5 * 86400))}
IDENTITY_REGISTRY=${IDENTITY_REGISTRY:-}
AGENT_ID=${AGENT_ID:-}

# name:equity:risky_equity:principal:loans
#
# Equity is the first loss layer and bounds what the account may draw, so
# it is the lever that separates a small agent from a large one. Loans is
# how much record it accumulates before the run ends.
COHORT=${COHORT:-"sable:4000000000:2000000000000000000:1500000000:1
ember:9000000000:4000000000000000000:3000000000:3
cinder:16000000000:8000000000000000000:5000000000:6
ash:9000000000:4000000000000000000:2500000000:2"}

. "$ROOT/scripts/lib/keys.sh"
credence_require_private_key "$SIGNER" "$CHAIN_ID" attestor CREDENCE_SIGNER_KEY
credence_require_private_key "$DEPLOYER" "$CHAIN_ID" deployer CREDENCE_DEPLOYER_KEY

cat <<NOTE
--- mode ---
  chain          $CHAIN_ID $(credence_is_local_chain "$CHAIN_ID" 2>/dev/null && echo "(local, time can be warped)" || echo "(public, real elapsed time)")
  evidence       SCENARIO -- synthetic history, honestly labelled
  authorization  Development route, one attestor key
  assets         test tokens; the risky one is named NOT ETH on purpose
  trust          the operator and attestor are TRUSTED, see docs/05
  cohort         $(echo "$COHORT" | wc -l | tr -d ' ') agents on ONE facility
NOTE

cd "$ROOT/contracts"
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

erc8004() {
  python3 -c "import json;d=json.load(open('$MANIFEST')).get('erc8004') or {};print(d.get('$1',''))"
}
IDENTITY_REGISTRY=${IDENTITY_REGISTRY:-$(erc8004 identity_registry)}
AGENT_ID=${AGENT_ID:-$(erc8004 agent_id)}
IDENTITY_REGISTRY=${IDENTITY_REGISTRY:-0x000000000000000000000000000000000000aec0}
AGENT_ID=${AGENT_ID:-184}
if ! credence_is_local_chain "$CHAIN_ID" 2>/dev/null; then
  if [ "$AGENT_ID" = "184" ]; then
    echo "refusing to bind loans to placeholder agent 184 on chain $CHAIN_ID" >&2
    echo "record an erc8004 block in $MANIFEST, or set AGENT_ID" >&2
    exit 1
  fi
fi

export AGENT_ADDRESS=$(cast wallet address --private-key \
  "0x${CREDENCE_AGENT_KEY:-59c6995e998f97a5a0044966f0945389dc9e86dae88c7a8412f4603b6b78690d}")
export SETTLEMENT_TOKEN=$SETTLEMENT VAULT_ADDRESS=$VAULT
export LOAN_MANAGER_ADDRESS=$MANAGER RISKY_ASSET=$RISKY STRATEGY_ADAPTER=$ADAPTER

# The engine prices every loan for every agent.
cd "$ROOT/risk"
cargo build --quiet 2>/dev/null || true
RISK_SERVICE_ADDR=127.0.0.1:18114 ./target/debug/credence-risk-service \
  >"$WORK/risk-cohort.log" 2>&1 &
RISK_PID=$!
trap 'kill $RISK_PID 2>/dev/null || true' EXIT
for _ in $(seq 40); do
  curl -sf http://127.0.0.1:18114/health >/dev/null 2>&1 && break
  sleep 0.25
done

cd "$ROOT/gateway"
export CREDENCE_OPERATOR_KEY=$DEPLOYER CREDENCE_SIGNER_KEY=$SIGNER
go build -o credence-scenario ./cmd/credence-scenario

# The facility, funded once. Deep enough that the LENDER's budget is not
# what limits the last agent in the cohort -- otherwise the run would be
# demonstrating a small vault rather than four different borrowers.
export LENDER_DEPOSIT=${LENDER_DEPOSIT:-200000000000}

first=1
index=0
while IFS=: read -r NAME EQUITY RISKY_EQUITY PRINCIPAL LOANS; do
  [ -n "$NAME" ] || continue
  index=$((index + 1))
  echo
  echo "=== $NAME (equity $EQUITY, $LOANS loan(s) of $PRINCIPAL) ==="

  cd "$ROOT/contracts"
  export AGENT_EQUITY=$EQUITY AGENT_RISKY_EQUITY=$RISKY_EQUITY
  LOG="$WORK/$NAME-account.log"
  # The FIRST agent funds and activates the vault; the rest attach to the
  # facility it created. `Scenario` calls `activate()`, which requires
  # `Phase.Funding`, so running it per agent would revert on the second.
  if [ "$first" -eq 1 ]; then
    SCRIPT=script/Scenario.s.sol
    first=0
  else
    SCRIPT=script/AddAccount.s.sol
  fi
  if ! forge script "$SCRIPT" --rpc-url "$RPC" --broadcast --slow >"$LOG" 2>&1; then
    echo "  $NAME: the account script failed:" >&2
    tail -30 "$LOG" >&2
    exit 1
  fi
  ACCOUNT=$(grep -o 'credit account 0x[0-9a-fA-F]\{40\}' "$LOG" | awk '{print $3}')
  if [ -z "$ACCOUNT" ]; then
    echo "  $NAME: the script ran but printed no credit account:" >&2
    tail -30 "$LOG" >&2
    exit 1
  fi
  echo "  account   $ACCOUNT"

  # One database per agent. A shared one would pool their histories, and
  # the whole point is that each agent is judged on its OWN record.
  cd "$ROOT/gateway"
  ./credence-scenario -rpc "$RPC" -oracle "$ORACLE" -manager "$MANAGER" \
    -account "$ACCOUNT" -settlement "$SETTLEMENT" -principal "$PRINCIPAL" \
    -adapter "$ADAPTER" -risky "$RISKY" -price-source "$PRICE_SOURCE" \
    -chain-id "$CHAIN_ID" -deploy-bps 10000 -loans "$LOANS" \
    -term-seconds "$TERM_SECONDS" -hold-seconds "$HOLD_SECONDS" \
    -identity-registry "$IDENTITY_REGISTRY" -agent-id "$AGENT_ID" \
    -enroll "$WORK/$NAME.db" -vault "$VAULT" \
    -risk http://127.0.0.1:18114 -policy "$ROOT/config/engine-policy.json" \
    >"$WORK/$NAME-loans.log" 2>&1 || {
    echo "  $NAME: origination failed:" >&2
    tail -30 "$WORK/$NAME-loans.log" >&2
    exit 1
  }
  grep -oE 'loan [0-9]+: engine terms .*' "$WORK/$NAME-loans.log" |
    sed 's/^/  /' || true

  # Record the account beside the database, so the bundler can attribute
  # each agent's assessments without re-deriving them from a log.
  printf '%s\n' "$ACCOUNT" > "$WORK/$NAME.account"
done <<< "$COHORT"

echo
echo "=== the cohort, as the engine priced it ==="
python3 - "$WORK" <<'PY'
import glob, json, os, sqlite3, sys

work = sys.argv[1]
print(f"  {'agent':<9} {'loans':>5} {'first APR':>10} {'last APR':>9} "
      f"{'capacity':>14}  bound by")

rows = []
for db in sorted(glob.glob(os.path.join(work, "*.db"))):
    name = os.path.splitext(os.path.basename(db))[0]
    c = sqlite3.connect(f"file:{db}?mode=ro", uri=True)
    c.row_factory = sqlite3.Row
    try:
        got = [dict(r) for r in c.execute(
            "SELECT sequence, payload FROM assessments ORDER BY sequence")]
    except sqlite3.Error:
        got = []
    finally:
        c.close()

    priced = []
    for r in got:
        p = r["payload"]
        if isinstance(p, (bytes, bytearray)):
            p = p.decode("utf-8", "replace")
        p = json.loads(p)
        out = p.get("outcome") or {}
        terms, cap = out.get("terms") or {}, out.get("capacity") or {}
        if terms.get("apr_bps") is not None:
            priced.append((terms["apr_bps"], cap))
    if not priced:
        continue
    rows.append((name, priced))
    first, last = priced[0], priced[-1]
    print(f"  {name:<9} {len(priced):>5} {first[0]/100:>9.2f}% "
          f"{last[0]/100:>8.2f}% "
          f"{int(last[1].get('agent_capacity', 0))/1e9:>13.3f}B  "
          f"{last[1].get('agent_binding','')}")

# The run is only worth keeping if the agents actually DIFFER. Four rows
# that agree would be one agent rendered four times, which is exactly the
# thing this script exists to stop the globe from doing.
if len(rows) > 1:
    shapes = {
        (len(p), p[-1][0], p[-1][1].get("agent_capacity"))
        for _, p in rows
    }
    if len(shapes) < len(rows):
        print("\n  NOTE: two agents produced the same answer. The cohort "
              "is meant to span the space, not repeat a point.")
    else:
        print(f"\n  {len(rows)} agents, {len(shapes)} distinct outcomes")
PY
