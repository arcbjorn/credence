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
# Read from the node, not assumed. The deploy script writes its manifest
# keyed by chain id, so a hardcoded 31337 silently reads a stale local
# deployment when pointed at a testnet.
CHAIN_ID=${CHAIN_ID:-$(cast chain-id --rpc-url "$RPC")}
ROOT=$(cd "$(dirname "$0")/.." && pwd)
WORK=${WORK:-/tmp/credence-monitor}
# Anvil account #0 by default. On a public chain, export a funded key:
# this one is published in the anvil banner and anyone can drain it.
DEPLOYER=${CREDENCE_DEPLOYER_KEY:-ac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80}
SIGNER=${CREDENCE_SIGNER_KEY:-5de4111afa1a4b94908f83103eb1f1706367c2e68ca870fc3fb9a804cdab365a}
# Well past the 500 bps trigger threshold, and deep enough that the
# COVERAGE verdict flips decisively rather than by a hair. A 20% fall
# trips the trigger but leaves coverage well above the 1.15 floor, so the
# reassessment would correctly still read Allowed -- true, but it
# demonstrates nothing about permission being withdrawn. With 10,000 of
# equity absorbing the fall, the move has to be deep to reach lenders.
MOVE_BPS=${MOVE_BPS:-7000}

rm -rf "$WORK" && mkdir -p "$WORK"

# A published key may sign nothing on a chain other people can reach.
. "$ROOT/scripts/lib/keys.sh"
credence_require_private_key "$SIGNER" "$CHAIN_ID" attestor CREDENCE_SIGNER_KEY
credence_require_private_key "$DEPLOYER" "$CHAIN_ID" deployer CREDENCE_DEPLOYER_KEY

cd "$ROOT/contracts"
# Forge replays nonces from its own broadcast cache. A chain that has been
# restarted -- or a different chain reusing the id -- resets those nonces,
# and forge then retries "nonce too low" with no useful output until it
# gives up. Drop this chain's cache so a deploy starts from the node's
# real nonce. Observed against a restarted anvil, 14 September.
rm -rf broadcast/*/"$CHAIN_ID" cache/*.s.sol/"$CHAIN_ID"
export DEPLOYER_KEY=0x$DEPLOYER DEPLOYMENT_ID=7
export ATTESTOR_ADDRESS=$(cast wallet address --private-key "0x$SIGNER")
forge script script/Deploy.s.sol --rpc-url "$RPC" --broadcast >/dev/null
MANIFEST="$ROOT/contracts/deployments/$CHAIN_ID.json"
addr() { python3 -c "import json;print(json.load(open('$MANIFEST'))['addresses']['$1'])"; }
ORACLE=$(addr oracle);        VAULT=$(addr vault)
MANAGER=$(addr loan_manager); SETTLEMENT=$(addr settlement)
RISKY=$(addr risky);          ADAPTER=$(addr adapter)
PRICE_SOURCE=$(addr price_source)

# The bound operator. It signs nothing in this demo, so it needs no gas,
# but it follows CREDENCE_AGENT_KEY so every demo binds the same operator.
export AGENT_ADDRESS=$(cast wallet address --private-key \
  "0x${CREDENCE_AGENT_KEY:-59c6995e998f97a5a0044966f0945389dc9e86dae88c7a8412f4603b6b78690d}")
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
  -chain-id "$CHAIN_ID" -deploy-bps 10000 -loans 1 -enroll "$WORK/credence.db" >"$WORK/scenario.log" 2>&1
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
python3 - "$MANIFEST" "$WORK" "$ROOT" "$CHAIN_ID" "$RPC" <<'PY'
import json, sys
manifest, work, root = sys.argv[1], sys.argv[2], sys.argv[3]
a = json.load(open(manifest))["addresses"]
base = json.load(open(root + "/config/gateway.example.json"))
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
# The node this run actually targets. Leaving the template's RPC here
# points the gateway at a different chain than everything else, and every
# read returns empty rather than failing loudly.
base["rpc_url"] = sys.argv[5]
base["chain_id"] = int(sys.argv[4])
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

# Say this before a reviewer asks. `docs/02` invariant 3 reads as though
# the account reverts on a coverage breach; it does not compute coverage
# at all, and claiming otherwise is the kind of gap a reviewer finds.
cat <<'NOTE'

--- what "health" means here ---
  CreditAccount computes no LTV and no coverage ratio. Those are the
  engine's. What reaches chain is the oracle's permission bit, so a
  risk-increasing call reverts when permission is absent, expired, or
  bound to a different account state version.

  A breach therefore surfaces at the NEXT assessment, not at the moment
  of the trade -- the window between them is the monitoring interval you
  just watched close. What the contract guarantees is that the account
  cannot keep increasing risk, not that it was stopped mid-trade.
NOTE

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
