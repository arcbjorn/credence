#!/usr/bin/env bash
# Check the hand-written protobuf codec against the reference implementation.
#
# `risk-service` encodes protobuf in ~200 lines of dependency-free Rust
# rather than pulling `tonic`/`prost` (see `src/protobuf.rs` for why). A
# codec checked only against its own decoder agrees with itself about a
# layout both may have wrong -- the same trap `gateway/erc8004`'s decoder
# avoids by pinning against a response the live registry really returned.
#
# So this encodes a message in Rust and decodes it with `protoc`, which
# is the specification's own implementation. If the two ever disagree,
# this fails.
#
# Needs `protoc` on PATH. Without it the check SKIPS rather than passes:
# an unavailable reference implementation proves nothing.
set -euo pipefail

ROOT=$(cd "$(dirname "$0")/.." && pwd)

if ! command -v protoc >/dev/null 2>&1; then
  echo "SKIP  protoc is not installed; the reference decoder is unavailable"
  exit 0
fi

echo "=== the schema compiles ==="
protoc --proto_path="$ROOT/schemas/proto" \
  --descriptor_set_out=/dev/null "$ROOT/schemas/proto/risk.proto"
echo "  risk.proto is valid"

WORK=$(mktemp -d)
trap 'rm -rf "$WORK"' EXIT

# A payload with an escape sequence in it: the bytes the engine hashes
# are canonical JSON, and a codec that "helpfully" re-encoded them would
# break replay rather than fail loudly.
cat >"$WORK/interop.rs" <<'RS'
fn main() {
    let mut buf = Vec::new();
    risk_service::protobuf::put_bytes(&mut buf, 1, b"{\"snapshot\":true,\"e\":\"\\u00e9\"}");
    risk_service::protobuf::put_bytes(&mut buf, 2, b"{\"policy\":1}");
    print!("{}", buf.iter().map(|b| format!("{b:02x}")).collect::<String>());
}
RS

mkdir -p "$ROOT/risk/crates/risk-service/examples"
cp "$WORK/interop.rs" "$ROOT/risk/crates/risk-service/examples/protobuf_interop.rs"
trap 'rm -rf "$WORK" "$ROOT/risk/crates/risk-service/examples"' EXIT

echo
echo "=== Rust encodes ==="
(cd "$ROOT/risk" && cargo run -q -p risk-service --example protobuf_interop) >"$WORK/wire.hex"
echo "  $(wc -c <"$WORK/wire.hex" | tr -d ' ') hex chars"

python3 -c "
import binascii, sys
open('$WORK/wire.bin','wb').write(binascii.unhexlify(open('$WORK/wire.hex').read().strip()))
"

echo
echo "=== protoc decodes ==="
OUT=$(protoc --proto_path="$ROOT/schemas/proto" \
  --decode=credence.risk.v1.AssessRequest \
  "$ROOT/schemas/proto/risk.proto" <"$WORK/wire.bin")
echo "$OUT" | sed 's/^/  /'

# The escape must survive VERBATIM. protoc prints it back escaped, so a
# codec that decoded \u00e9 into a literal byte would show the difference
# here rather than silently changing what the engine hashes.
echo "$OUT" | grep -q 'snapshot' || {
  echo "FAIL  protoc did not recover the input field" >&2
  exit 1
}
echo "$OUT" | grep -q 'u00e9' || {
  echo "FAIL  the escape sequence did not survive the round trip" >&2
  exit 1
}

echo
echo "PASS  the hand-written codec agrees with protoc"
echo
echo "  Encoded by risk-service, decoded by the reference implementation."
echo "  A codec checked only against its own decoder proves nothing."
