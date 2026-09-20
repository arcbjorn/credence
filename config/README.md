# Gateway configurations

| File | Facility | Market source |
|---|---|---|
| `gateway.example.json` | Scenario | a declared static series |
| `gateway.perpl.example.json` | Observed | Perpl candles and orderbook |

A **Scenario** facility may use synthetic market evidence, because the
engine caps its result at `ScenarioOnly`. An **Observed** facility may
not: a static series inside an executable decision is exactly the
synthetic evidence `docs/04` forbids, so the loader refuses it.

## The Perpl block

```json
"perpl": {
  "base_url": "https://app.perpl.xyz/api",
  "market_id": 20,
  "resolution_seconds": 60,
  "price_decimals": 2,
  "size_decimals": 3,
  "settlement_decimals": 6,
  "observe_depth": true
}
```

Four fields are easy to get wrong, and each is refused at load rather
than at 3am in a scheduler log:

- **`resolution_seconds` is an integer number of seconds.** The venue
  answers HTTP **500** to the TradingView-style strings (`1h`), which
  reads like an outage rather than a bad request.
- **It must not exceed `evidence.max_skew_seconds`.** A candle is stamped
  at its *open*, so the newest hourly bar reads up to 3600s old on a
  perfectly live feed, and every cycle would be `Stale`.
- **`window_seconds / resolution_seconds` must stay under ~1000.** Beyond
  that the venue answers HTTP 400.
- **`market_id` is not stable across networks.** ETH is 20 on mainnet and
  32 on testnet; mainnet has no BTC market at all.

The decimals come from the venue's own `/v1/pub/context`
(`markets[id].config.price_decimals`, `.size_decimals`, and the
settlement token's `decimals`). They are declared rather than fetched so
a venue that rescales cannot silently change what past decisions meant.

Run `bash scripts/probe-perpl.sh` to print the current values, and
`bash scripts/verify-perpl.sh` to drive the gateway's own providers
against the live venue. Both are read-only and need no key.

`liquidity_depth` stays **required** even with `observe_depth` on: it is
the fallback used when the venue is unreachable. An unobserved depth can
only make the engine lend less, so the fallback cannot be exploited by
taking the venue down.
