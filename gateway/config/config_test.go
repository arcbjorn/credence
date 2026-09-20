package config_test

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/arcbjorn/credence/gateway/config"
	"github.com/arcbjorn/credence/gateway/market"
)

func write(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "gateway.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}

const valid = `{
  "rpc_url": "https://rpc.example",
  "chain_id": 10143,
  "addresses": {
    "oracle": "0x1111111111111111111111111111111111111111",
    "loan_manager": "0x2222222222222222222222222222222222222222",
    "vault": "0x3333333333333333333333333333333333333333",
    "settlement": "0x4444444444444444444444444444444444444444"
  },
  "risk_service_url": "http://127.0.0.1:8081",
  "database_path": "/tmp/x.db",
  "deployment_id": 7,
  "evidence_policy_id": "0x2c3744f9aecea30351bf13f04f46cadfdb5a8a04e9533c7e35c392992564a882",
  "facility_policy": "Observed",
  "schema_version": "v1",
  "scheduler": {
    "price_move_bps": 500,
    "max_evidence_age_seconds": 300,
    "refresh_lead_seconds": 60,
    "heartbeat_seconds": 600
  },
  "evidence": { "max_skew_seconds": 60, "validity_seconds": 300 },
  "wallet": { "gas_limit_cap": 5000000, "gas_price_cap_wei": 500000000000 },
  "market": {
    "provider_url": "http://127.0.0.1:9000/candles",
    "window_seconds": 3600,
    "liquidity_depth": "50000000000"
  },
  "engine_policy": {}
}`

func TestValidConfigLoads(t *testing.T) {
	cfg, err := config.Load(write(t, valid))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.RiskTimeout == 0 || cfg.TickPeriod == 0 {
		t.Fatal("durations must be defaulted")
	}
	if cfg.RequestedClass != "PrototypeEligible" {
		t.Fatalf("class default = %q", cfg.RequestedClass)
	}
}

// A typo in a policy threshold must not be silently ignored, or the
// deployment would run with a default nobody chose.
func TestUnknownFieldIsRejected(t *testing.T) {
	body := strings.Replace(valid, `"chain_id": 10143,`,
		`"chain_id": 10143, "chian_id": 1,`, 1)
	if _, err := config.Load(write(t, body)); err == nil {
		t.Fatal("an unknown field must be rejected")
	}
}

// The scheduler's fail-closed guarantee depends on refresh leading expiry.
func TestRefreshLeadMustPrecedeExpiry(t *testing.T) {
	body := strings.Replace(valid, `"refresh_lead_seconds": 60`,
		`"refresh_lead_seconds": 300`, 1)
	_, err := config.Load(write(t, body))
	if err == nil {
		t.Fatal("a lead equal to expiry must be rejected")
	}
	if !strings.Contains(err.Error(), "expires before it can be renewed") {
		t.Fatalf("the error must explain why: %v", err)
	}
}

// An assessment must not outlive the evidence behind it.
func TestValidityMustNotExceedEvidenceAge(t *testing.T) {
	body := strings.Replace(valid, `"validity_seconds": 300`,
		`"validity_seconds": 3000`, 1)
	_, err := config.Load(write(t, body))
	if err == nil || !strings.Contains(err.Error(), "outlives the evidence") {
		t.Fatalf("expected a validity refusal, got %v", err)
	}
}

// A scenario facility must name its scenario, or synthetic evidence has
// nothing to attribute it to.
func TestScenarioFacilityRequiresScenarioID(t *testing.T) {
	body := strings.Replace(valid, `"facility_policy": "Observed"`,
		`"facility_policy": "Scenario"`, 1)
	if _, err := config.Load(write(t, body)); err == nil {
		t.Fatal("a scenario facility without an id must be rejected")
	}
}

// And an observed facility must not carry one.
func TestObservedFacilityRejectsScenarioID(t *testing.T) {
	body := strings.Replace(valid, `"schema_version": "v1"`,
		`"schema_version": "v1", "scenario_id": "run-1"`, 1)
	if _, err := config.Load(write(t, body)); err == nil {
		t.Fatal("an observed facility must not carry a scenario id")
	}
}

func TestMalformedAddressIsRejected(t *testing.T) {
	body := strings.Replace(valid,
		`"oracle": "0x1111111111111111111111111111111111111111"`,
		`"oracle": "0xabc"`, 1)
	if _, err := config.Load(write(t, body)); err == nil {
		t.Fatal("a short address must be rejected")
	}
}

func TestMissingEnginePolicyIsRejected(t *testing.T) {
	body := strings.Replace(valid, `"engine_policy": {}`, `"engine_policy": null`, 1)
	if _, err := config.Load(write(t, body)); err == nil {
		t.Fatal("a missing engine policy must be rejected")
	}
}

// The signing key never comes from the config file.
func TestSignerKeyComesFromEnvironment(t *testing.T) {
	t.Setenv(config.SignerKeyEnv, "")
	if _, err := config.SignerKey(); err == nil {
		t.Fatal("an unset key must be an error")
	}

	t.Setenv(config.SignerKeyEnv, strings.Repeat("11", 32))
	key, err := config.SignerKey()
	if err != nil {
		t.Fatalf("valid key: %v", err)
	}
	if len(key) != 32 {
		t.Fatalf("key length %d", len(key))
	}
}

func TestShortSignerKeyIsRejected(t *testing.T) {
	t.Setenv(config.SignerKeyEnv, "1122")
	if _, err := config.SignerKey(); err == nil {
		t.Fatal("a short key must be rejected")
	}
}

func TestNonHexSignerKeyIsRejected(t *testing.T) {
	t.Setenv(config.SignerKeyEnv, strings.Repeat("zz", 32))
	if _, err := config.SignerKey(); err == nil {
		t.Fatal("a non-hex key must be rejected")
	}
}

// docs/04: an observed facility must not run on synthetic evidence. A
// static series would be exactly that, inside an executable decision.
func TestObservedFacilityRejectsStaticCloses(t *testing.T) {
	body := strings.Replace(valid, `"window_seconds": 3600`,
		`"window_seconds": 3600, "static_closes": [100, 101]`, 1)
	_, err := config.Load(write(t, body))
	if err == nil || !strings.Contains(err.Error(), "static_closes") {
		t.Fatalf("expected a static_closes refusal, got %v", err)
	}
}

// And it must have a real provider rather than falling back.
func TestObservedFacilityRequiresProvider(t *testing.T) {
	body := strings.Replace(valid,
		`"provider_url": "http://127.0.0.1:9000/candles",`, "", 1)
	_, err := config.Load(write(t, body))
	if err == nil || !strings.Contains(err.Error(), "provider_url") {
		t.Fatalf("expected a provider refusal, got %v", err)
	}
}

// A scenario facility may use a static series, because synthetic evidence
// is permitted there and the engine caps the result at ScenarioOnly.
func TestScenarioFacilityAcceptsStaticCloses(t *testing.T) {
	body := strings.Replace(valid, `"facility_policy": "Observed"`,
		`"facility_policy": "Scenario", "scenario_id": "run-1"`, 1)
	body = strings.Replace(body,
		`"provider_url": "http://127.0.0.1:9000/candles",`,
		`"static_closes": [10000, 10100, 10201],`, 1)

	if _, err := config.Load(write(t, body)); err != nil {
		t.Fatalf("a scenario facility with a static series must load: %v", err)
	}
}

// A scenario facility with neither a provider nor a usable series has no
// market evidence at all.
func TestScenarioFacilityNeedsSomeMarketSource(t *testing.T) {
	body := strings.Replace(valid, `"facility_policy": "Observed"`,
		`"facility_policy": "Scenario", "scenario_id": "run-1"`, 1)
	body = strings.Replace(body,
		`"provider_url": "http://127.0.0.1:9000/candles",`, "", 1)

	if _, err := config.Load(write(t, body)); err == nil {
		t.Fatal("a scenario facility with no market source must be rejected")
	}
}

func TestLiquidityDepthIsRequired(t *testing.T) {
	body := strings.Replace(valid, `"liquidity_depth": "50000000000"`,
		`"liquidity_depth": ""`, 1)
	if _, err := config.Load(write(t, body)); err == nil {
		t.Fatal("liquidity depth must be declared")
	}
}

// An unbounded gas limit or price turns a routine monitoring update into an
// unbounded cost, so both caps are required rather than defaulted.
func TestWalletCapsAreRequired(t *testing.T) {
	for _, field := range []string{`"gas_limit_cap": 5000000`, `"gas_price_cap_wei": 500000000000`} {
		body := strings.Replace(valid, field,
			strings.Split(field, ":")[0]+": 0", 1)
		if _, err := config.Load(write(t, body)); err == nil {
			t.Fatalf("a zero cap must be rejected: %s", field)
		}
	}
}

// The attestor signs and the operator spends. Reading them from separate
// variables means a leaked attestation key cannot also move funds.
func TestOperatorKeyIsSeparateFromSigner(t *testing.T) {
	t.Setenv(config.SignerKeyEnv, strings.Repeat("11", 32))
	t.Setenv(config.OperatorKeyEnv, "")
	if _, err := config.OperatorKey(); err == nil {
		t.Fatal("an unset operator key must be an error even when the signer is set")
	}

	t.Setenv(config.OperatorKeyEnv, strings.Repeat("22", 32))
	operator, err := config.OperatorKey()
	if err != nil {
		t.Fatalf("operator key: %v", err)
	}
	signer, _ := config.SignerKey()
	if string(operator) == string(signer) {
		t.Fatal("the two keys must be read from different variables")
	}
}

// --- Perpl ------------------------------------------------------------

const perplBlock = `"perpl": {
      "base_url": "https://app.perpl.xyz/api",
      "market_id": 20,
      "resolution_seconds": 60,
      "price_decimals": 2,
      "size_decimals": 3,
      "settlement_decimals": 6,
      "observe_depth": true
    },`

// withPerpl swaps the generic provider for a Perpl block.
//
// It also widens the window to 6 hours. `valid` uses 3600s, which at this
// resolution is a single candle -- correctly refused, but that is a
// different test than the one each caller here is making.
//
// The resolution is 60s rather than hourly because `valid` sets a 60s skew
// budget, and a candle is stamped at its OPEN: anything coarser is Stale
// on arrival. 60s over 6 hours is 360 candles, under the venue's cap.
func withPerpl(body string) string {
	body = strings.Replace(body,
		`"provider_url": "http://127.0.0.1:9000/candles",`, perplBlock, 1)
	return strings.Replace(body, `"window_seconds": 3600`, `"window_seconds": 21600`, 1)
}

// Perpl observes a real venue, so it satisfies an Observed facility on its
// own -- the whole point of building it.
func TestPerplSatisfiesAnObservedFacility(t *testing.T) {
	if _, err := config.Load(write(t, withPerpl(valid))); err != nil {
		t.Fatalf("a Perpl-backed observed facility must load: %v", err)
	}
}

// Two providers would make the source of a published number ambiguous
// after the fact. The evidence contract cannot tolerate that: a reader
// re-deriving a decision must know which feed produced it.
func TestPerplAndGenericProviderConflict(t *testing.T) {
	body := strings.Replace(valid,
		`"provider_url": "http://127.0.0.1:9000/candles",`,
		`"provider_url": "http://127.0.0.1:9000/candles",`+perplBlock, 1)
	_, err := config.Load(write(t, body))
	if err == nil || !strings.Contains(err.Error(), "not both") {
		t.Fatalf("expected a conflict refusal, got %v", err)
	}
}

// A window/resolution pair the venue would reject with HTTP 400 must fail
// at startup, where the message gets read -- not on every cycle after.
func TestPerplOverCapWindowIsRefusedAtLoad(t *testing.T) {
	body := withPerpl(valid)
	// 60s resolution with a 3600s window is fine; widen the window to a
	// week to exceed the candle cap.
	body = strings.Replace(body, `"resolution_seconds": 3600`, `"resolution_seconds": 60`, 1)
	body = strings.Replace(body, `"window_seconds": 21600`, `"window_seconds": 604800`, 1)

	_, err := config.Load(write(t, body))
	if err == nil || !strings.Contains(err.Error(), "cap") {
		t.Fatalf("expected an over-cap refusal, got %v", err)
	}
}

// A window too short to hold two candles can never produce a return.
func TestPerplWindowMustHoldTwoCandles(t *testing.T) {
	// A 60s resolution with a 90s window: not even two candles.
	body := strings.Replace(withPerpl(valid),
		`"window_seconds": 21600`, `"window_seconds": 90`, 1)

	_, err := config.Load(write(t, body))
	if err == nil || !strings.Contains(err.Error(), "fewer than two") {
		t.Fatalf("expected a two-candle refusal, got %v", err)
	}
}

// Declared depth stays REQUIRED even when Perpl observes it, because it is
// the fallback used when the venue is unreachable. Dropping it would turn
// a provider outage into a blocked facility.
func TestPerplStillRequiresDeclaredDepthAsFallback(t *testing.T) {
	body := strings.Replace(withPerpl(valid),
		`"liquidity_depth": "50000000000"`, `"liquidity_depth": ""`, 1)
	if _, err := config.Load(write(t, body)); err == nil {
		t.Fatal("declared depth is the fallback and must stay required")
	}
}

// config duplicates the candle cap to stay a leaf package. If the two ever
// drift, a config that loads would be rejected by the venue at runtime --
// exactly the failure the up-front check exists to prevent.
func TestPerplCandleCapMatchesTheMarketPackage(t *testing.T) {
	// A window one candle over the market package's cap must be refused,
	// which is only true if config's copy of the constant agrees.
	over := uint64(market.MaxPerplCandles+1) * 60
	body := withPerpl(valid)
	body = strings.Replace(body, `"resolution_seconds": 3600`, `"resolution_seconds": 60`, 1)
	body = strings.Replace(body, `"window_seconds": 21600`,
		`"window_seconds": `+strconv.FormatUint(over, 10), 1)

	if _, err := config.Load(write(t, body)); err == nil {
		t.Fatalf("config's cap disagrees with market.MaxPerplCandles (%d)",
			market.MaxPerplCandles)
	}
}

// A candle is stamped at its OPEN, so the newest one is up to a full
// resolution old even on a live feed. A resolution coarser than the skew
// budget makes every cycle Stale and the facility never authorizes.
//
// Found against the live venue, not in a fixture: hourly candles read 32
// minutes old. See scripts/verify-perpl.sh.
func TestPerplResolutionMustFitTheSkewBudget(t *testing.T) {
	body := strings.Replace(withPerpl(valid),
		`"resolution_seconds": 60`, `"resolution_seconds": 3600`, 1)
	_, err := config.Load(write(t, body))
	if err == nil || !strings.Contains(err.Error(), "max_skew_seconds") {
		t.Fatalf("expected a skew-budget refusal, got %v", err)
	}
}
