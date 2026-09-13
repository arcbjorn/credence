package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/arcbjorn/credence/gateway/config"
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
