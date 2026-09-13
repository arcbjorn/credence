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
(cd "$ROOT/contracts" && forge build --offline >/dev/null)
(cd "$ROOT/risk" && cargo build --quiet)
(cd "$ROOT/gateway" && go build ./...)

echo "ready. run scripts/demo-repay.sh"
