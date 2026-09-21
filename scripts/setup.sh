#!/usr/bin/env bash
# Fetch what a clean checkout does not carry.
#
# `docs/11` reproducibility row: a clean setup must complete the
# documented scenario. Two things are gitignored build inputs rather than
# source, so a fresh clone cannot compile without them:
#
#   * contracts/lib/forge-std — pinned below, not vendored
#   * contracts/deployments/  — tracked via its README, since vm.writeFile
#     will not create a parent directory
#
# Idempotent: safe to re-run.
set -euo pipefail

ROOT=$(cd "$(dirname "$0")/.." && pwd)

# Pinned. An unpinned dependency makes "clean setup" mean a different
# thing on different days.
FORGE_STD_VERSION=${FORGE_STD_VERSION:-v1.16.2}

if [ ! -d "$ROOT/contracts/lib/forge-std" ]; then
  echo "fetching forge-std $FORGE_STD_VERSION"
  git clone --quiet --depth 1 --branch "$FORGE_STD_VERSION" \
    https://github.com/foundry-rs/forge-std \
    "$ROOT/contracts/lib/forge-std"
else
  echo "forge-std present"
fi

mkdir -p "$ROOT/contracts/deployments"

echo
echo "building"
# NOT `--offline`. This script exists for the clean-checkout case, which
# is precisely the case where solc has never been downloaded: on a fresh
# runner `--offline` fails with "can't install missing solc 0.8.28 in
# offline mode" before anything is built. It only ever succeeded on a
# machine that had already built once, which is the opposite of what the
# reproducibility row checks.
# The three are INDEPENDENT -- no artifact of one feeds another -- so
# they run concurrently. In series they took the sum of the three; the
# wall clock is now the slowest one. Failures are collected rather than
# swallowed: a `wait` that ignored exit codes would report "ready" after
# a compile error.
pids=()
(cd "$ROOT/contracts" && forge build >/dev/null) &
pids+=($!)
(cd "$ROOT/risk" && cargo build --quiet) &
pids+=($!)
(cd "$ROOT/gateway" && go build ./...) &
pids+=($!)

failed=0
for pid in "${pids[@]}"; do
  wait "$pid" || failed=1
done
if [ "$failed" -ne 0 ]; then
  echo "a build failed; see the output above" >&2
  exit 1
fi

echo "ready. run scripts/demo-repay.sh"

# External dependencies, when a target chain is given. The demos deploy
# their own contracts and need none of this, so a local run stays silent;
# a testnet run is told what is actually there before it builds against
# an assumption.
if [ -n "${RPC:-}" ]; then
  echo
  echo "checking external dependencies on $RPC"
  "$ROOT/scripts/probe-testnet.sh" || {
    echo
    echo "note: the demos do not need these. This affects only the"
    echo "ERC-8004 and CRE integrations."
  }
fi
