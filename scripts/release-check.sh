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
# Reading the registry needs no key and no funds -- that is the property
# the row asserts -- so it runs here rather than waiting for a funded
# operator. PUBLISHING to it does need funds; `scripts/demo-erc8004.sh`
# does that, and this only reads what it already wrote.
if (cd "$ROOT" && bash scripts/verify-reputation.sh) >/tmp/rc-reputation.log 2>&1; then
  sig=$(grep -oE '[0-9]+ signal\(s\) from this client across [0-9]+ obligation' \
    /tmp/rc-reputation.log | head -1)
  record PASS "Independent reputation reader" "${sig:-read the registry}"
else
  # A public chain this project does not control. An outage is a FAIL of
  # this row and not of the release: the record names what was reachable.
  record FAIL "Independent reputation reader" "see /tmp/rc-reputation.log"
fi

# The indexed lifecycle needs Docker, a synced indexer and a local chain.
# It is attempted rather than announced: a row that only ever printed
# instructions was indistinguishable from one nobody had ever run.
if ! docker info >/dev/null 2>&1; then
  record SKIP "Indexed lifecycle" "Docker not responding"
elif ! curl -s -o /dev/null -m 5 http://localhost:8080/healthz; then
  # localhost, not 127.0.0.1: Docker publishes this port on IPv6 and the
  # IPv4 literal can miss a healthy Hasura (see scripts/index-local.sh).
  record SKIP "Indexed lifecycle" "no indexer at :8080 -- run scripts/index-local.sh"
else
  # The behavioural rows above redeploy the manager at a FRESH address,
  # so an indexer synced before this run is watching a contract that no
  # longer exists. That is a stale indexer, not a broken one, and
  # reporting it as a failure would make the release record accuse the
  # code of something the harness did.
  #
  # Detected by comparing what the indexer indexed against what the
  # manifest now says.
  #
  # With RESYNC_INDEXER=1 the check restores the precondition itself:
  # a repaid loan 1 and an indexer on the current manager. That is not
  # "rebuilding what is under test" -- the indexer is a precondition like
  # the anvil chain, and the thing being tested is whether indexed state
  # FOLLOWS A REORG, which the re-sync does not touch. It is opt-in only
  # because it costs a demo run plus a sync, and the default path should
  # stay fast enough to run often.
  indexed_mgr=$(curl -s -m 5 -X POST http://localhost:8080/v1/graphql \
    -H 'content-type: application/json' \
    -d '{"query":"{Loan(limit:1){id}}"}' 2>/dev/null | grep -o '"id":"[^"]*"' | head -1)
  live_mgr=$(python3 -c "
import json
try:
    print(json.load(open('$ROOT/contracts/deployments/31337.json'))['addresses']['loan_manager'].lower())
except Exception:
    print('')
" 2>/dev/null)

  stale=""
  if [ -z "$indexed_mgr" ]; then
    stale="indexer holds no loans"
  elif [ -n "$live_mgr" ] && ! echo "$indexed_mgr" | grep -qi "${live_mgr#0x}"; then
    stale="indexer is on a superseded manager"
  fi

  # The behavioural rows leave loan 1 Defaulted (demo-default.sh ran
  # last), and verify-reorg.sh needs it Repaid. So the re-sync redoes the
  # repayment demo first, then indexes THAT deployment.
  if [ -n "$stale" ] && [ "${RESYNC_INDEXER:-0}" = "1" ]; then
    echo "  re-syncing the indexer (RESYNC_INDEXER=1)"
    : >/tmp/rc-resync.log
    if (cd "$ROOT" && LOANS=2 bash scripts/demo-repay.sh) >>/tmp/rc-resync.log 2>&1; then
      # index-local.sh ends in `exec envio dev`, which never returns, so
      # it runs in the background and readiness is read from its log.
      (cd "$ROOT" && bash scripts/index-local.sh) >>/tmp/rc-resync.log 2>&1 &
      for _ in $(seq 40); do
        sleep 10
        if grep -q 'indexer is ready' /tmp/rc-resync.log 2>/dev/null; then
          stale=""
          break
        fi
      done
    fi
    [ -n "$stale" ] && stale="$stale (re-sync failed, see /tmp/rc-resync.log)"
  fi

  if [ -n "$stale" ]; then
    record SKIP "Indexed lifecycle" "$stale -- re-run with RESYNC_INDEXER=1"
  elif (cd "$ROOT" && bash scripts/verify-reorg.sh) >/tmp/rc-reorg.log 2>&1; then
    blk=$(grep -oE 'now from block [0-9]+ \(was [0-9]+\)' /tmp/rc-reorg.log | head -1)
    record PASS "Indexed lifecycle" "reorg survived, ${blk:-replayed}"
  else
    record FAIL "Indexed lifecycle" "see /tmp/rc-reorg.log"
  fi
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

# The panel capture needs a web bundle to substitute into, so it is only
# attempted when one exists. A missing bundle is a SKIP rather than a
# failure: `scripts/build-web-data.sh` is a separate step.
if [ -f "$ROOT/web/public/data/repay.json" ]; then
  if (cd "$ROOT" && bash scripts/build-perpl-data.sh) >/tmp/rc-perplpanel.log 2>&1; then
    src=$(grep -oE 'snapshot    [a-f0-9]+' /tmp/rc-perplpanel.log | awk '{print $2}')
    record PASS "Perpl risk panel" "captured against snapshot ${src:-?}"
  else
    record FAIL "Perpl risk panel" "see /tmp/rc-perplpanel.log"
  fi
else
  record SKIP "Perpl risk panel" "run scripts/build-web-data.sh first"
fi

# The transport gate (`docs/10`, 6 Oct): the protobuf codec must agree
# with the reference implementation, and the frozen artifacts must be
# untouched. SKIPs without protoc rather than passing -- an unavailable
# reference decoder proves nothing.
if (cd "$ROOT" && bash scripts/verify-protobuf.sh) >/tmp/rc-proto.log 2>&1; then
  if grep -q '^SKIP' /tmp/rc-proto.log; then
    record SKIP "Protobuf transport" "protoc not installed"
  else
    record PASS "Protobuf transport" "codec agrees with protoc"
  fi
else
  record FAIL "Protobuf transport" "see /tmp/rc-proto.log"
fi

# --- Is the remote build green? ----------------------------------------
#
# Everything above re-runs the gates locally. None of it asks whether CI
# AGREES, and on 21 September that gap let a baseline tag land on a red
# build: `web` had been failing for two commits because a test required a
# fixture that depends on a third-party venue, which no local run could
# have shown -- locally the file was always present.
#
# Reported, never enforced by exit code: a tag is a human decision, and a
# release check that refused to produce a record because GitHub was slow
# would be worse than one that says plainly what it found.
echo
echo "remote"
if ! command -v gh >/dev/null 2>&1; then
  record SKIP "CI on this commit" "gh not installed"
elif ! git -C "$ROOT" rev-parse --abbrev-ref '@{upstream}' >/dev/null 2>&1; then
  record SKIP "CI on this commit" "no upstream branch"
else
  SHA=$(git -C "$ROOT" rev-parse HEAD)
  CI=$(gh run list --limit 20 \
    --json headSha,status,conclusion \
    --jq ".[] | select(.headSha==\"$SHA\") | \"\(.status) \(.conclusion // \"pending\")\"" \
    2>/dev/null | head -1 | sed 's/[[:space:]]*$//')
  case "$CI" in
    "completed success") record PASS "CI on this commit" "green on ${SHA:0:8}" ;;
    "completed "*) record FAIL "CI on this commit" "${CI#completed } on ${SHA:0:8}" ;;
    "") record SKIP "CI on this commit" "no run for ${SHA:0:8} yet -- push first" ;;
    *) record SKIP "CI on this commit" "$CI on ${SHA:0:8}" ;;
  esac
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
