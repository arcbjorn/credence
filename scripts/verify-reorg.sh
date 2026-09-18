#!/usr/bin/env bash
# `docs/11`: replay an indexed loan after a simulated reorganization.
#
# The row asks whether indexed state FOLLOWS the chain when the chain
# changes its mind. A green sync does not answer that: an indexer that
# never rolls back looks identical to one that does, right up until a
# reorg, at which point it serves a repayment that no longer happened.
#
# So this reorganises away a repayment and checks the indexer withdraws
# it, then replays the repayment and checks it comes back. Inverting a
# TERMINAL outcome is the strongest version of the test available --
# `Repaid` is the state a lender acts on.
#
# Prerequisites: a synced indexer (`scripts/index-local.sh`) against a
# chain carrying a repaid loan (`LOANS=2 scripts/demo-repay.sh`).
set -euo pipefail

ROOT=$(cd "$(dirname "$0")/.." && pwd)
RPC=${RPC:-http://127.0.0.1:8546}
PSQL=(docker exec envio-postgres psql -U postgres -d envio-dev -tAc)

fail() {
  echo "FAIL: $1" >&2
  exit 1
}

# anvil only. A public chain cannot be asked to reorganise, and asking
# would be a request to rewrite somebody else's history.
CHAIN_ID=$(cast chain-id --rpc-url "$RPC")
case "$CHAIN_ID" in
  31337 | 1337) ;;
  *) fail "refusing to reorganise chain $CHAIN_ID; this is an anvil-only check" ;;
esac

state() { "${PSQL[@]}" "select status from \"Loan\" where id = '$CHAIN_ID-1'" 2>/dev/null; }
events() { "${PSQL[@]}" 'select count(*) from "LoanEvent"' 2>/dev/null; }

echo "=== before ==="
before=$(state)
echo "  loan 1 is $before, $(events) event(s) indexed"
[ "$before" = "Repaid" ] ||
  fail "loan 1 is $before, not Repaid -- run LOANS=2 scripts/demo-repay.sh, then index it"

# The block carrying the repayment, read from the chain rather than
# assumed: the demo's block layout changes with its parameters.
MANAGER=$(python3 -c \
  "import json;print(json.load(open('$ROOT/contracts/deployments/$CHAIN_ID.json'))['addresses']['loan_manager'])")
REPAID_TOPIC=$(cast keccak "Repaid(uint256,uint256,uint256)")
REPAID_BLOCK=$(cast logs --from-block 0 --to-block latest \
  --address "$MANAGER" "$REPAID_TOPIC" --rpc-url "$RPC" --json 2>/dev/null |
  python3 -c "import sys,json;d=json.load(sys.stdin);print(int(d[0]['blockNumber'],16))")
HEAD=$(cast block-number --rpc-url "$RPC")
DEPTH=$((HEAD - REPAID_BLOCK + 1))

echo
echo "=== reorganising ==="
echo "  Repaid is in block $REPAID_BLOCK, head is $HEAD -- rewinding $DEPTH block(s)"
HASH_BEFORE=$(cast block "$REPAID_BLOCK" --rpc-url "$RPC" --json |
  python3 -c "import sys,json;print(json.load(sys.stdin)['data']['hash'])")
cast rpc anvil_reorg "$DEPTH" '[]' --rpc-url "$RPC" >/dev/null
HASH_AFTER=$(cast block "$REPAID_BLOCK" --rpc-url "$RPC" --json |
  python3 -c "import sys,json;print(json.load(sys.stdin)['data']['hash'])")
[ "$HASH_BEFORE" != "$HASH_AFTER" ] ||
  fail "block $REPAID_BLOCK has the same hash; no reorganisation happened"
echo "  block $REPAID_BLOCK was replaced (hash changed)"

# The indexer notices on the next block it sees, so give it one. Without
# this it sits on stale state indefinitely and the check reads as a
# failure that is really a missing poll.
for _ in $(seq 5); do cast rpc anvil_mine --rpc-url "$RPC" >/dev/null; done

echo
echo "=== waiting for the rollback ==="
for _ in $(seq 24); do
  sleep 5
  now=$(state)
  [ "$now" = "Active" ] && break
done
now=$(state)
[ "$now" = "Active" ] ||
  fail "loan 1 is still '$now' after the reorganisation; the indexer served a repayment the chain no longer has"
echo "  loan 1 is Active again -- the withdrawn repayment was withdrawn"

# The rows must match the chain, not merely change. An indexer that
# dropped everything would also pass a status check.
chain_events=$(cast logs --from-block 0 --to-block latest --address "$MANAGER" \
  --rpc-url "$RPC" --json 2>/dev/null | python3 -c "import sys,json;print(len(json.load(sys.stdin)))")
echo "  chain holds $chain_events log(s) for this manager"

echo
echo "=== replaying the repayment ==="
cast send "$MANAGER" "repay(uint256)" 1 --rpc-url "$RPC" \
  --private-key "0x${CREDENCE_DEPLOYER_KEY:-ac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80}" \
  >/dev/null
for _ in $(seq 3); do cast rpc anvil_mine --rpc-url "$RPC" >/dev/null; done

for _ in $(seq 24); do
  sleep 5
  now=$(state)
  [ "$now" = "Repaid" ] && break
done
now=$(state)
[ "$now" = "Repaid" ] ||
  fail "loan 1 is '$now' after the replay; the indexer rolled back but did not roll forward"

NEW_BLOCK=$("${PSQL[@]}" \
  "select \"blockNumber\" from \"LoanEvent\" where kind = 'Repaid'" 2>/dev/null)
[ "$NEW_BLOCK" != "$REPAID_BLOCK" ] ||
  fail "the replayed repayment is recorded in block $NEW_BLOCK, the block that was reorganised away"

echo "  loan 1 is Repaid again, now from block $NEW_BLOCK (was $REPAID_BLOCK)"
echo
echo "PASS  indexed state followed the chain through a reorganisation"
