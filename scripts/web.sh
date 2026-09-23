#!/usr/bin/env bash
#
# Everything the interface needs, from a clean checkout to a running page.
#
# The web build depends on two GENERATED artifacts that a clone does not
# carry, and on evidence that only the demos produce:
#
#   web/src/engine/    the risk engine compiled to WebAssembly
#   web/public/data/   what the demo runs retained
#
# Without both, every screen loads and renders nothing. That is a poor way
# to meet a project, so this runs the whole path in order and says which
# step it is on.
#
# Idempotent, and each step is skippable when its output already exists:
#
#   scripts/web.sh              # everything, reusing what is already there
#   scripts/web.sh --fresh      # re-run the demos even if evidence exists
#   scripts/web.sh --no-serve   # build only, do not start the dev server
set -euo pipefail

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
RPC=${RPC:-http://127.0.0.1:8546}
FRESH=0
SERVE=1

for arg in "$@"; do
  case "$arg" in
    --fresh)    FRESH=1 ;;
    --no-serve) SERVE=0 ;;
    -h|--help)
      sed -n '3,19p' "${BASH_SOURCE[0]}" | sed 's/^# \{0,1\}//'
      exit 0 ;;
    *)
      echo "unknown option $arg" >&2
      exit 1 ;;
  esac
done

step() { printf '\n\033[1m==> %s\033[0m\n' "$1"; }
have() { command -v "$1" >/dev/null 2>&1; }

# --- prerequisites, named before anything is attempted ---------------
#
# Checked up front rather than failing three minutes in: a missing
# toolchain should cost a second, not a partial build.
step "checking the toolchain"
missing=0
for tool in cargo rustup go forge cast pnpm node; do
  if have "$tool"; then
    printf '  %-8s ok\n' "$tool"
  else
    printf '  %-8s MISSING\n' "$tool"
    missing=1
  fi
done

if ! have wasm-bindgen; then
  echo "  wasm-bindgen MISSING -- installing (this takes a few minutes)"
  cargo install wasm-bindgen-cli --version 0.2.128
fi

if [ "$missing" -eq 1 ]; then
  cat >&2 <<'MSG'

Install what is missing first:
  rust + cargo   https://rustup.rs
  go             https://go.dev/dl
  forge, cast    https://getfoundry.sh
  pnpm, node     https://pnpm.io/installation
MSG
  exit 1
fi

if ! rustup target list --installed 2>/dev/null | grep -q wasm32-unknown-unknown; then
  echo "  adding the wasm32-unknown-unknown target"
  rustup target add wasm32-unknown-unknown
fi

# --- a chain -----------------------------------------------------------
#
# The demos deploy their own contracts, so they need a node. Nothing else
# starts one, and the failure without it is an opaque RPC error rather
# than "there is no chain".
step "checking for a chain at $RPC"
ANVIL_PID=""
if cast chain-id --rpc-url "$RPC" >/dev/null 2>&1; then
  echo "  a node is already listening"
else
  case "$RPC" in
    *127.0.0.1*|*localhost*)
      echo "  none found; starting anvil on port 8546"
      anvil --port 8546 --silent &
      ANVIL_PID=$!
      # Started here, so stopped here. Leaving a stray node behind would
      # make the next run reuse a chain whose state nobody remembers.
      trap 'kill $ANVIL_PID 2>/dev/null || true' EXIT
      for _ in $(seq 40); do
        cast chain-id --rpc-url "$RPC" >/dev/null 2>&1 && break
        sleep 0.25
      done
      ;;
    *)
      echo "  no node at $RPC, and it is not local -- not starting one" >&2
      exit 1 ;;
  esac
fi

# --- build the backends ------------------------------------------------
step "building contracts, engine and gateway"
bash "$ROOT/scripts/setup.sh"

# --- evidence ----------------------------------------------------------
#
# The screens render what these runs retained. `demo-repay` supplies the
# loans, forecasts and on-chain checks; `demo-monitor` supplies the
# snapshot the replay screen reproduces; `demo-growth` supplies the
# controlled sequence the earned-rate screen plots; `demo-cohort`
# supplies the several agents the globe renders as a market.
NEED_DEMOS=$FRESH
if [ ! -f /tmp/credence-repay/scenario.db ] ||
  [ ! -f /tmp/credence-monitor/credence.db ] ||
  [ ! -f /tmp/credence-growth/scenario.db ] ||
  [ -z "$(ls /tmp/credence-cohort/*.db 2>/dev/null)" ]; then
  NEED_DEMOS=1
fi

if [ "$NEED_DEMOS" -eq 1 ]; then
  step "running the demos (this is the evidence the screens show)"
  LOANS=5 bash "$ROOT/scripts/demo-repay.sh" > /tmp/credence-web-repay.log 2>&1 || {
    echo "  demo-repay failed; see /tmp/credence-web-repay.log" >&2
    tail -20 /tmp/credence-web-repay.log >&2
    exit 1
  }
  echo "  demo-repay:  $(grep -c '^\[PASS' /tmp/credence-web-repay.log) checks passed on chain"

  bash "$ROOT/scripts/demo-monitor.sh" > /tmp/credence-web-monitor.log 2>&1 || {
    echo "  demo-monitor failed; see /tmp/credence-web-monitor.log" >&2
    tail -20 /tmp/credence-web-monitor.log >&2
    exit 1
  }
  echo "  demo-monitor: replay $(grep -oE '^MATCH|^DIFFERENCE' /tmp/credence-web-monitor.log | head -1)"

  # The defaulted epoch. Without it the lender screen's "realized loss"
  # can only ever read zero, which demonstrates nothing: `docs/09` beat 7
  # asks for a loss that is realised rather than asserted.
  bash "$ROOT/scripts/demo-default.sh" > /tmp/credence-web-default.log 2>&1 || {
    echo "  demo-default failed; see /tmp/credence-web-default.log" >&2
    tail -20 /tmp/credence-web-default.log >&2
    exit 1
  }
  echo "  demo-default: $(grep -oE 'realized_loss=[0-9]+' /tmp/credence-web-default.log | head -1)"

  # The controlled sequence: one agent, one principal, only the record
  # moving. Without it `/earned` has nothing to plot, and the project's
  # central claim stays spread across loans that differ in several ways
  # at once -- suggestive rather than attributable.
  bash "$ROOT/scripts/demo-growth.sh" > /tmp/credence-web-growth.log 2>&1 || {
    echo "  demo-growth failed; see /tmp/credence-web-growth.log" >&2
    tail -20 /tmp/credence-web-growth.log >&2
    exit 1
  }
  echo "  demo-growth: $(grep -oE 'repriced from [0-9.]+% to [0-9.]+%' /tmp/credence-web-growth.log | head -1)"

  # Several agents on ONE facility. Without it every globe renders a
  # single point beside assessed profiles that never borrowed, while the
  # landing copy says "one point per agent".
  bash "$ROOT/scripts/demo-cohort.sh" > /tmp/credence-web-cohort.log 2>&1 || {
    echo "  demo-cohort failed; see /tmp/credence-web-cohort.log" >&2
    tail -20 /tmp/credence-web-cohort.log >&2
    exit 1
  }
  echo "  demo-cohort: $(grep -oE '[0-9]+ agents, [0-9]+ distinct outcomes' /tmp/credence-web-cohort.log | head -1)"
else
  step "reusing the evidence already in /tmp (pass --fresh to re-run)"
fi

# --- the two generated artifacts ---------------------------------------
step "compiling the risk engine to WebAssembly"
bash "$ROOT/scripts/build-wasm.sh"

step "bundling the retained evidence"
bash "$ROOT/scripts/build-web-data.sh"

# --- the app -----------------------------------------------------------
step "installing web dependencies"
(cd "$ROOT/web" && pnpm install --silent)

if [ "$SERVE" -eq 0 ]; then
  step "building the static site"
  (cd "$ROOT/web" && pnpm build)
  echo
  echo "built. serve it with any static server:"
  echo "  cd web/dist && python3 -m http.server 5173"
  exit 0
fi

step "starting the dev server"
echo "  the chain stays up while this runs; ctrl-c stops both"
echo
cd "$ROOT/web" && pnpm dev
