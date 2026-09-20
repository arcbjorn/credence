#!/usr/bin/env bash
# The Perpl feasibility experiment, in the same form as `probe-kuru.sh`.
#
# It answers three questions and nothing else: is the market data public,
# what shape does it arrive in, and does it carry the two fields the risk
# engine actually consumes -- realized volatility and liquidation depth.
# Every call here is an unauthenticated GET, so a reader with no key and
# no funded account can re-run the whole thing.
#
# `docs/08`'s rule is that a capability check runs against the actual
# intended environment rather than being read from documentation. Three
# things here would have been got wrong by reading alone, which is the
# whole reason the rule exists:
#
#   * Resolution is an INTEGER NUMBER OF SECONDS. The TradingView-style
#     strings the candle type suggests (`1h`, `1d`, `1H`) do not return a
#     400 -- they return **HTTP 500**, which reads like an outage and is
#     not one. `3600` on the identical URL returns 200.
#   * A 400 from `/candles/60/...` is a CANDLE-COUNT cap, not a rejected
#     resolution. 60s over 7 days is 10,080 candles and fails; the same
#     resolution over 6 hours is 361 candles and succeeds. Concluding
#     "one-minute candles are unsupported" from the first result would
#     have been a wrong answer.
#   * Market ids are NOT stable across networks. ETH is 20 on mainnet and
#     32 on testnet, and mainnet has no BTC market at all, so a testnet id
#     used against mainnet 404s or silently reads a different asset.
#
# The depth finding is the one that changes the system. `gateway/market`
# carried a comment saying no public feed reports the depth available at
# liquidation, so depth was declared policy rather than observation. The
# `book` endpoint below is exactly such a feed, and `perpl.go` now reads
# it. This script stays a check of the VENUE; `scripts/verify-perpl.sh`
# checks that the gateway's own providers consume it correctly.
set -euo pipefail

BASE=${PERPL_BASE:-https://app.perpl.xyz/api}
MARKET=${PERPL_MARKET:-20} # ETH on mainnet; see the id warning above.

NOW=$(($(date +%s) * 1000)) # Perpl timestamps are MILLISECONDS.

echo "=== Perpl capability check, $BASE ==="
echo

# get <label> <path> -- prints status and size, leaves the body in /tmp.
BODY=$(mktemp)
get() {
  local label=$1 path=$2 code
  code=$(curl -s -m 20 -o "$BODY" -w "%{http_code}" "$BASE/$path")
  printf '  %-34s HTTP %-4s %7s bytes\n' "$label" "$code" "$(wc -c <"$BODY" | tr -d ' ')"
  [ "$code" = "200" ]
}

echo "--- is it public? ---"
get "context (no auth)" "v1/pub/context" ||
  { echo "  FAIL: context is not readable without a key" >&2; exit 1; }

python3 - "$BODY" "$MARKET" <<'PY'
import json, sys
doc = json.load(open(sys.argv[1]))
want = int(sys.argv[2])
markets = doc["markets"]
rows = markets.values() if isinstance(markets, dict) else markets
m = next((r for r in rows if r.get("id") == want), None)
if m is None:
    ids = sorted((r.get("id"), r.get("symbol")) for r in rows)
    raise SystemExit(f"  FAIL: no market {want} here. Available: {ids}")
cfg = m["config"]
print(f"    market {want} is {m['symbol']}, open={cfg['is_open']}")
print(f"    price_decimals={cfg['price_decimals']} size_decimals={cfg['size_decimals']}")
# Settlement token decimals: the engine works in settlement units, so a
# depth figure is meaningless without them.
tok = doc["tokens"][0]
print(f"    settlement {tok['symbol']} decimals={tok['decimals']}")
PY

echo
echo "--- resolution is seconds, not a TradingView string ---"
FROM=$((NOW - 21600000)) # 6 hours
for r in 1h 3600; do
  get "candles resolution '$r'" "v1/market-data/$MARKET/candles/$r/$FROM-$NOW" || true
done
echo "    '1h' returns 500, '3600' returns 200 -- the string form is not"
echo "    merely unsupported, it errors in a way that looks like an outage."

echo
echo "--- 400 is a candle-count cap, not a bad resolution ---"
WEEK=$((NOW - 604800000))
get "60s over 7 days (10080 candles)" "v1/market-data/$MARKET/candles/60/$WEEK-$NOW" || true
get "60s over 6 hours (361 candles)" "v1/market-data/$MARKET/candles/60/$FROM-$NOW" || true

echo
echo "--- the two fields the engine consumes ---"
get "candles -> realized volatility" "v1/market-data/$MARKET/candles/3600/$FROM-$NOW" ||
  { echo "  FAIL: no candles" >&2; exit 1; }
python3 - "$BODY" <<'PY'
import json, sys
d = json.load(open(sys.argv[1]))
c = d["d"]
print(f"    {len(c)} candles, resolution {d['r']}s")
print(f"    shape {sorted(c[0])}  (t is ms; v is a STRING)")
closes = [x["c"] for x in c]
# The same mean-absolute-return measure gateway/market uses, so the probe
# reports the number the gateway would actually compute rather than a
# different statistic that happens to be easier here.
bps = [abs(b - a) * 10_000 // a for a, b in zip(closes, closes[1:]) if a]
print(f"    realized volatility {sum(bps)//len(bps)} bps over {len(bps)} returns")
PY

get "book -> liquidation depth" "v1/market-data/$MARKET/book" ||
  { echo "  FAIL: no book" >&2; exit 1; }
python3 - "$BODY" <<'PY'
import json, sys
d = json.load(open(sys.argv[1]))
bid, ask = d["bid"], d["ask"]
print(f"    {len(bid)} bid levels, {len(ask)} ask levels")
print(f"    level shape {sorted(bid[0])}  (p price, s size, o orders)")
# Depth is summed on the BID side: liquidating collateral SELLS into bids.
# Summing both sides, or the asks, would report depth that cannot absorb
# the trade the stress scenario actually models.
top = bid[0]["p"]
notional = sum(l["p"] * l["s"] for l in bid)
print(f"    top bid {top}, total bid notional {notional} (price*size units)")
PY

rm -f "$BODY"

echo
echo "PASS  Perpl market data is public, and carries both engine inputs"
echo
echo "  Candles give realized volatility. The book gives depth on the side"
echo "  a liquidation would actually trade into. gateway/market once"
echo "  declared depth as policy because no public feed reported it; this"
echo "  endpoint is that feed, and gateway/market/perpl.go now reads it."
echo
echo "  This probe checks the VENUE. To check that the gateway's own"
echo "  providers consume it correctly, run scripts/verify-perpl.sh."
