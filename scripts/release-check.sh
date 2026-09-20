#!/usr/bin/env bash
# Re-run every acceptance row and record what it produced.
#
# `docs/10` commits to a pinned baseline tagged on 22 September, with all
# applicable acceptance rows re-run before every merge. This is that
# re-run, in one command, so tagging is mechanical rather than a scramble
# on the day.
#
# It writes a dated record to `releases/` and fails if any row fails. The
# record is the point: a tag whose evidence lives only in a terminal
# somebody closed is a tag nobody can check.
#
# Rows needing a public chain, Docker or funded keys are SKIPPED and named
# as skipped. `docs/11` is explicit that a row without evidence is named
# pending rather than assumed, and the same rule applies here: a skip is
# reported, never silently counted as a pass.
set -uo pipefail

ROOT=$(cd "$(dirname "$0")/.." && pwd)
STAMP=$(date -u +%Y-%m-%dT%H:%M:%SZ)
DAY=$(date -u +%Y-%m-%d)
OUT=${RELEASE_OUT:-$ROOT/releases/$DAY.md}
RPC=${RPC:-http://127.0.0.1:8546}

mkdir -p "$(dirname "$OUT")"

pass=0
fail=0
skip=0
lines=()

# record <status> <row> <detail>
record() {
  local status=$1 row=$2 detail=$3
  case "$status" in
    PASS) pass=$((pass + 1)) ;;
    FAIL) fail=$((fail + 1)) ;;
    SKIP) skip=$((skip + 1)) ;;
  esac
  printf '  %-4s %-34s %s\n' "$status" "$row" "$detail"
  lines+=("| $status | $row | $detail |")
}

# run <row> <directory> <command...>
#
# Captures output so a failure can be read afterwards rather than
# scrolling past. Records PASS on exit 0 and FAIL otherwise.
run() {
  local row=$1 dir=$2
  shift 2
  local log
  log=$(mktemp)
  if (cd "$dir" && "$@") >"$log" 2>&1; then
    record PASS "$row" "$(tail -1 "$log" | cut -c1-72)"
  else
    record FAIL "$row" "see ${log}"
    return 1
  fi
  rm -f "$log"
}

echo "=== release check, $STAMP ==="
echo

# --- Suites, which need nothing but the toolchain ----------------------

echo "suites"
run "Contract invariants" "$ROOT/contracts" forge test || true
run "Engine" "$ROOT/risk" cargo test -p risk-core || true
run "Gateway" "$ROOT/gateway" go test -race -count=1 ./... || true
run "CRE workflow" "$ROOT/cre" go test -count=1 ./underwrite/ || true
run "Indexer fold" "$ROOT/indexer" pnpm test || true
run "Cited doc counts" "$ROOT" bash scripts/check-docs.sh || true

# --- Gates CI enforces -------------------------------------------------

echo
echo "gates"
run "Solidity format" "$ROOT/contracts" forge fmt --check || true
run "Rust format" "$ROOT/risk" cargo fmt --check || true

# Every script must at least parse. A quoting error inside an echo is
# invisible until the line runs, and the line that runs last is the one
# nobody waits for -- a broken `probe-perpl.sh` was committed on 20
# September for exactly that reason, caught by eye rather than by a gate.
syntax_bad=""
for f in "$ROOT"/scripts/*.sh "$ROOT"/scripts/lib/*.sh; do
  bash -n "$f" 2>/dev/null || syntax_bad="$syntax_bad $(basename "$f")"
done
if [ -n "$syntax_bad" ]; then
  record FAIL "Script syntax" "does not parse:$syntax_bad"
else
  record PASS "Script syntax" "$(ls "$ROOT"/scripts/*.sh "$ROOT"/scripts/lib/*.sh | wc -l | tr -d ' ') scripts parse"
fi

# `forge lint` exits 0 even with warnings, so the gate is the grep that
# CI uses rather than the exit code.
lint_out=$(cd "$ROOT/contracts" && forge lint 2>&1)
if echo "$lint_out" | grep -qE '^warning\['; then
  record FAIL "Solidity lint" "$(echo "$lint_out" | grep -cE '^warning\[') warning(s)"
else
  record PASS "Solidity lint" "0 warnings"
fi

# --- Behavioural rows, which need a chain ------------------------------

echo
echo "behaviour"
if ! cast chain-id --rpc-url "$RPC" >/dev/null 2>&1; then
  record SKIP "Performer lifecycle" "no chain at $RPC"
  record SKIP "Three sequential loans" "no chain at $RPC"
  record SKIP "Continuous underwriting" "no chain at $RPC"
  record SKIP "Replay" "no chain at $RPC"
  record SKIP "Default handling" "no chain at $RPC"
  record SKIP "Note transfer and redemption" "no chain at $RPC"
else
  CHAIN_ID=$(cast chain-id --rpc-url "$RPC")

  if (cd "$ROOT" && LOANS=3 bash scripts/demo-repay.sh) >/tmp/rc-repay.log 2>&1; then
    checks=$(grep -c '^\[PASS' /tmp/rc-repay.log || echo 0)
    terms=$(grep -oE 'apr=[0-9]+bps' /tmp/rc-repay.log | tr '\n' ' ')
    record PASS "Performer lifecycle" "$checks on-chain checks passed"
    record PASS "Three sequential loans" "${terms:-no terms captured}"
  else
    record FAIL "Performer lifecycle" "see /tmp/rc-repay.log"
    record FAIL "Three sequential loans" "see /tmp/rc-repay.log"
  fi

  if (cd "$ROOT" && bash scripts/demo-monitor.sh) >/tmp/rc-monitor.log 2>&1; then
    trig=$(grep -oE 'trigger=[a-z_]+' /tmp/rc-monitor.log | sort -u | tr '\n' ' ')
    record PASS "Continuous underwriting" "${trig:-no trigger captured}"
    if grep -q '^MATCH' /tmp/rc-monitor.log &&
      grep -q 'tampered evidence correctly rejected' /tmp/rc-monitor.log; then
      record PASS "Replay" "MATCH, and a tampered control rejected"
    else
      record FAIL "Replay" "no MATCH or no tamper control"
    fi
  else
    record FAIL "Continuous underwriting" "see /tmp/rc-monitor.log"
    record FAIL "Replay" "see /tmp/rc-monitor.log"
  fi

  if (cd "$ROOT" && bash scripts/demo-default.sh) >/tmp/rc-default.log 2>&1; then
    loss=$(grep -oE 'realized_loss=[0-9]+' /tmp/rc-default.log | head -1)
    record PASS "Default handling" "${loss:-no loss captured}"
  else
    record FAIL "Default handling" "see /tmp/rc-default.log"
  fi

  if (cd "$ROOT" && bash scripts/demo-notes.sh) >/tmp/rc-notes.log 2>&1; then
    paid=$(grep -oE 'buyer received  [0-9]+' /tmp/rc-notes.log | awk '{print $3}')
    record PASS "Note transfer and redemption" "buyer redeemed ${paid:-?}"
  else
    record FAIL "Note transfer and redemption" "see /tmp/rc-notes.log"
  fi
fi

# --- Rows that need something this machine may not have ----------------

echo
echo "external"
if [ -n "${CREDENCE_DEPLOYER_KEY:-}" ] && [ "${RPC}" != "http://127.0.0.1:8546" ]; then
  record SKIP "Independent reputation reader" "run scripts/demo-erc8004.sh against 10143"
else
  record SKIP "Independent reputation reader" "needs funded keys and a public chain"
fi

if docker info >/dev/null 2>&1; then
  record SKIP "Indexed lifecycle" "run scripts/index-local.sh, then verify-reorg.sh"
else
  record SKIP "Indexed lifecycle" "Docker not responding"
fi

# Perpl needs the public internet and nothing else -- no key, no funds, no
# transaction -- so it runs here rather than being skipped. A venue outage
# is recorded as a FAIL of this row and not of the release: the record
# names what was reachable at the time it was produced.
if (cd "$ROOT" && bash scripts/verify-perpl.sh) >/tmp/rc-perpl.log 2>&1; then
  depth=$(grep -oE '"liquidity_depth": "[0-9]+"' /tmp/rc-perpl.log | head -1 | grep -oE '[0-9]+')
  vol=$(grep -oE '"realized_volatility_bps": [0-9]+' /tmp/rc-perpl.log | grep -oE '[0-9]+')
  record PASS "Perpl market observation" "vol=${vol:-?}bps depth=${depth:-?}"
else
  record FAIL "Perpl market observation" "see /tmp/rc-perpl.log"
fi

# --- The record --------------------------------------------------------

COMMIT=$(git -C "$ROOT" rev-parse HEAD)
SHORT=$(git -C "$ROOT" rev-parse --short HEAD)
DIRTY=$(git -C "$ROOT" status --porcelain | wc -l | tr -d ' ')

{
  echo "# Release check — $DAY"
  echo
  echo "Produced by \`scripts/release-check.sh\` at $STAMP."
  echo
  echo "| Field | Value |"
  echo "|---|---|"
  echo "| Commit | \`$COMMIT\` |"
  echo "| Working tree | $([ "$DIRTY" = "0" ] && echo "clean" || echo "**$DIRTY uncommitted file(s)**") |"
  echo "| Foundry | $(forge --version 2>/dev/null | head -1) |"
  echo "| Chain | $(cast chain-id --rpc-url "$RPC" 2>/dev/null || echo "none at $RPC") |"
  echo "| Result | **$pass passed, $fail failed, $skip skipped** |"
  echo
  echo "| Status | Row | Detail |"
  echo "|---|---|---|"
  printf '%s\n' "${lines[@]}"
  echo
  echo "A **SKIP** is a row this run could not reach, never a row that"
  echo "passed. \`docs/11\` names rows without evidence as pending rather"
  echo "than assumed, and the same rule applies here: the skips above are"
  echo "what a reader must still check by hand."
} >"$OUT"

echo
echo "=== $pass passed, $fail failed, $skip skipped ==="
echo "record: $OUT"

[ "$fail" -eq 0 ] || exit 1
