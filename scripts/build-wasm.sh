#!/usr/bin/env bash
#
# Compile the risk engine for the browser.
#
# `docs/09` beat 8 asks a reader to confirm a retained assessment
# reproduces. This puts the ACTUAL engine in the page, so the check is the
# same computation the CLI performs rather than a second implementation
# that could drift from it.
#
# Output lands in web/src/engine/, which is gitignored: it is a build
# artifact of risk/crates/risk-wasm, and committing it would create a
# second copy of the engine that could fall behind the first.
set -euo pipefail

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
OUT="$ROOT/web/src/engine"

if ! command -v wasm-bindgen >/dev/null 2>&1; then
  echo "wasm-bindgen not found. Install it with:" >&2
  echo "  cargo install wasm-bindgen-cli --version 0.2.128" >&2
  exit 1
fi

if ! rustup target list --installed 2>/dev/null | grep -q wasm32-unknown-unknown; then
  echo "adding the wasm32-unknown-unknown target"
  rustup target add wasm32-unknown-unknown
fi

echo "building risk-wasm"
cargo build --manifest-path "$ROOT/risk/Cargo.toml" \
  -p risk-wasm --release --target wasm32-unknown-unknown

rm -rf "$OUT"
mkdir -p "$OUT"

# `--target web` emits an ES module with no bundler assumptions, which is
# what Vite wants and what a plain <script type="module"> can also load.
wasm-bindgen \
  "$ROOT/risk/target/wasm32-unknown-unknown/release/risk_wasm.wasm" \
  --out-dir "$OUT" --target web

# The model hash is part of the claim: a MATCH means nothing if a reader
# cannot tell which engine produced it. Record it beside the artifact so
# the page can show it without instantiating the module first.
#
# Asked of the ENGINE rather than scraped from source. `model_hash()` is
# computed from the model id and version, so there is no 64-hex literal to
# grep for -- an earlier version of this script looked for one, found
# nothing, and failed silently under `set -e`.
MODEL_HASH=$(cd "$ROOT/risk" && cargo run --quiet --release --bin credence-risk -- model-hash 2>/dev/null || true)
if [ -z "$MODEL_HASH" ]; then
  # The CLI has no such subcommand in every build; fall back to node,
  # which can ask the artifact that was just produced.
  MODEL_HASH=$(cd "$OUT" && node --input-type=module -e '
    import init, { model_hash } from "./risk_wasm.js";
    import fs from "node:fs";
    await init({ module_or_path: fs.readFileSync("./risk_wasm_bg.wasm") });
    process.stdout.write(model_hash());
  ' 2>/dev/null || true)
fi
if [ -z "$MODEL_HASH" ]; then
  echo "could not determine the model hash" >&2
  exit 1
fi
SIZE=$(wc -c < "$OUT/risk_wasm_bg.wasm" | tr -d ' ')
cat > "$OUT/build.json" <<JSON
{
  "model_hash": "${MODEL_HASH}",
  "wasm_bytes": ${SIZE},
  "built_at": "$(date -u +%Y-%m-%dT%H:%M:%SZ)"
}
JSON

echo
echo "engine written to web/src/engine/"
echo "  wasm      $(du -h "$OUT/risk_wasm_bg.wasm" | cut -f1)"
echo "  gzipped   $(gzip -c "$OUT/risk_wasm_bg.wasm" | wc -c | tr -d ' ') bytes"
