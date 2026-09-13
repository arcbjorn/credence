#!/usr/bin/env bash
# docs/09 beat 5b: unprompted reassessment.
#
# The agent requests nothing. The market moves, the scheduler notices, and
# a monitoring assessment reaches chain on its own. This is the beat that
# separates "continuous underwriting" from a claim about it.
#
# The trigger must be `price_move`. A `staleness` cycle only proves a clock
# still ticks; it says nothing about the gateway reacting to the market.
set -euo pipefail

RPC=${RPC:-http://127.0.0.1:8546}
ROOT=$(cd "$(dirname "$0")/.." && pwd)
WORK=${WORK:-/tmp/credence-monitor}
DEPLOYER=ac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80
SIGNER=${CREDENCE_SIGNER_KEY:-5de4111afa1a4b94908f83103eb1f1706367c2e68ca870fc3fb9a804cdab365a}
# Well past the 500 bps threshold, so the move is unambiguous on screen.
MOVE_BPS=${MOVE_BPS:-2000}

rm -rf "$WORK" && mkdir -p "$WORK"

cd "$ROOT/contracts"
export DEPLOYER_KEY=0x$DEPLOYER DEPLOYMENT_ID=7
export ATTESTOR_ADDRESS=$(cast wallet address --private-key "0x$SIGNER")
forge script script/Deploy.s.sol --rpc-url "$RPC" --broadcast >/dev/null
MANIFEST="$ROOT/contracts/deployments/31337.json"
addr() { python3 -c "import json;print(json.load(open('$MANIFEST'))['addresses']['$1'])"; }
ORACLE=$(addr oracle);        VAULT=$(addr vault)
MANAGER=$(addr loan_manager); SETTLEMENT=$(addr settlement)
RISKY=$(addr risky);          ADAPTER=$(addr adapter)
PRICE_SOURCE=$(addr price_source)

export AGENT_ADDRESS=0x70997970C51812dc3A010C7d01b50e0d17dc79C8
export SETTLEMENT_TOKEN=$SETTLEMENT VAULT_ADDRESS=$VAULT
export LOAN_MANAGER_ADDRESS=$MANAGER RISKY_ASSET=$RISKY STRATEGY_ADAPTER=$ADAPTER
ACCOUNT=$(forge script script/Scenario.s.sol --rpc-url "$RPC" --broadcast 2>&1 \
  | grep -o 'credit account 0x[0-9a-fA-F]\{40\}' | awk '{print $3}')
echo "credit account $ACCOUNT"

# Leave one loan open: continuous underwriting governs live debt, and a
# repaid loan is untracked, so there would be nothing to reassess.
cd "$ROOT/gateway"
export CREDENCE_OPERATOR_KEY=$DEPLOYER CREDENCE_SIGNER_KEY=$SIGNER
go build -o credence-scenario ./cmd/credence-scenario
go build -o credence-gateway ./cmd/credence-gateway
./credence-scenario -rpc "$RPC" -oracle "$ORACLE" -manager "$MANAGER" \
  -account "$ACCOUNT" -settlement "$SETTLEMENT" -principal 4000000000 \
  -adapter "$ADAPTER" -risky "$RISKY" -price-source "$PRICE_SOURCE" \
  -deploy-bps 10000 -loans 1 -enroll "$WORK/credence.db" >"$WORK/scenario.log" 2>&1
echo "loan open and deployed"

cd "$ROOT/risk"
cargo build --quiet 2>/dev/null || true
RISK_SERVICE_ADDR=127.0.0.1:18110 ./target/debug/credence-risk-service >"$WORK/risk.log" 2>&1 &
RISK_PID=$!
trap 'kill $RISK_PID $GW_PID 2>/dev/null || true' EXIT
for _ in $(seq 40); do
  curl -sf http://127.0.0.1:18110/health >/dev/null 2>&1 && break
  sleep 0.25
done

# Same engine policy the scenario priced against, so the monitoring result
# is comparable to the origination it follows.
python3 - "$MANIFEST" "$WORK" <<'PY'
import json, sys
manifest, work = sys.argv[1], sys.argv[2]
a = json.load(open(manifest))["addresses"]
base = json.load(open("/tmp/gwtest/gateway.json"))
base["addresses"] = {
    "oracle": a["oracle"], "vault": a["vault"],
    "loan_manager": a["loan_manager"], "settlement": a["settlement"],
    "risky": a["risky"], "price_source": a["price_source"],
}
# The declared valuation policy for the test asset. The observed price
# read from the source drives the scheduler; this scales positions for the
# engine, and both must describe the same asset.
base["risky_asset"] = {
    "price": 2000000000, "price_scale": 6,
    "decimals": 18, "haircut_bps": 2000,
}
base["database_path"] = work + "/credence.db"
json.dump(base, open(work + "/gateway.json", "w"), indent=2)
PY

cd "$ROOT/gateway"
./credence-gateway -config "$WORK/gateway.json" >"$WORK/gateway.log" 2>&1 &
GW_PID=$!
# Let the loop settle until a cycle actually LANDS. The reference price is
# only set by a published cycle, so moving the price before one lands would
# leave the scheduler with nothing to measure against.
for _ in $(seq 40); do
  grep -q "permission=.* tx=0x" "$WORK/gateway.log" 2>/dev/null && break
  sleep 0.5
done

echo "=== settling: cycles before the move ==="
grep -c "trigger=" "$WORK/gateway.log" || true

# The move. Nothing asks for a loan; only the price changes.
OLD=$(cast call "$PRICE_SOURCE" 'price()(uint256)' --rpc-url "$RPC" | awk '{print $1}')
NEW=$(python3 -c "print($OLD * (10000 - $MOVE_BPS) // 10000)")
cast send "$PRICE_SOURCE" "setPrice(uint256)" "$NEW" --private-key "0x$DEPLOYER" --rpc-url "$RPC" >/dev/null
echo "price moves $OLD -> $NEW ($MOVE_BPS bps), no agent request"
for _ in $(seq 40); do
  grep -q "trigger=price_move" "$WORK/gateway.log" 2>/dev/null && break
  sleep 0.5
done
sleep 1

kill $GW_PID 2>/dev/null || true
sleep 1

echo "=== price_move cycles ==="
grep "trigger=price_move" "$WORK/gateway.log" || echo "NONE — beat 5b not demonstrated"
echo "=== full log at $WORK/gateway.log ==="

# docs/09 beat 8: replay the decision that just reached chain.
#
# Replay is only meaningful if it can fail, so the retained pair is
# replayed as-is and then with one field altered. A MATCH that cannot
# become a DIFFERENCE proves nothing.
echo "=== beat 8: replay from retained evidence ==="
python3 - "$WORK" <<'PY'
import json, sqlite3, sys
work = sys.argv[1]
c = sqlite3.connect(work + "/credence.db")
row = c.execute(
    "SELECT a.payload, s.payload FROM assessments a"
    " JOIN snapshots s ON s.input_hash = a.input_hash"
    " ORDER BY a.sequence DESC LIMIT 1").fetchone()
if row is None:
    raise SystemExit("nothing retained to replay")
open(work + "/assessment.json", "wb").write(row[0])
open(work + "/input.json", "wb").write(row[1])
json.dump(json.load(open(work + "/gateway.json"))["engine_policy"],
          open(work + "/policy.json", "w"))

# One digit of the position's mark, changed. Everything else identical.
d = json.loads(row[1])
d["account"]["value"]["positions"][0]["price"] = "1900000000"
json.dump(d, open(work + "/tampered.json", "w"))
PY

cd "$ROOT/risk"
./target/debug/credence-risk replay \
  --assessment "$WORK/assessment.json" --input "$WORK/input.json" \
  --policy "$WORK/policy.json"

echo "--- control: one altered field must break the match ---"
if ./target/debug/credence-risk replay \
     --assessment "$WORK/assessment.json" --input "$WORK/tampered.json" \
     --policy "$WORK/policy.json" >/dev/null 2>&1; then
  echo "REPLAY IS VACUOUS: tampered evidence still matched"
  exit 1
fi
echo "tampered evidence correctly rejected"
