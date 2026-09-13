// Package config loads and validates gateway configuration.
//
// Two rules shape it:
//
//  1. **Secrets never come from the config file.** The signing key is read
//     from the environment, so a committed config cannot leak one and a
//     deployment cannot accidentally publish with a checked-in key.
//
//  2. **Validation is total and happens once, at startup.** A gateway that
//     starts with a half-valid config fails later, in the middle of a
//     monitoring cycle, where the cause is hardest to see. Every field is
//     checked before any component is constructed.
package config

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"
)

// Private keys are only ever read from the environment.
const (
	// SignerKeyEnv attests assessments. It signs, it never spends.
	SignerKeyEnv = "CREDENCE_SIGNER_KEY"
	// OperatorKeyEnv pays gas. Separate from the attestor so a compromised
	// gateway signer cannot also drain the operator balance.
	OperatorKeyEnv = "CREDENCE_OPERATOR_KEY"
)

type Config struct {
	// Chain
	RPCURL    string    `json:"rpc_url"`
	ChainID   uint64    `json:"chain_id"`
	Addresses Addresses `json:"addresses"`

	// Services
	RiskServiceURL string `json:"risk_service_url"`
	DatabasePath   string `json:"database_path"`

	// Deployment identity
	DeploymentID     uint64 `json:"deployment_id"`
	EvidencePolicyID string `json:"evidence_policy_id"`
	FacilityPolicy   string `json:"facility_policy"`
	RequestedClass   string `json:"requested_class"`
	ScenarioID       string `json:"scenario_id,omitempty"`
	SchemaVersion    string `json:"schema_version"`

	// Policy
	Scheduler  SchedulerPolicy  `json:"scheduler"`
	Evidence   EvidencePolicy   `json:"evidence"`
	Market     MarketPolicy     `json:"market"`
	Wallet     WalletPolicy     `json:"wallet"`
	RiskyAsset RiskyAssetPolicy `json:"risky_asset"`

	// StrategyID names the single supported strategy in v0.
	StrategyID string `json:"strategy_id"`

	// Engine policy, passed through opaquely: the gateway must not reshape
	// what the engine hashes.
	EnginePolicy json.RawMessage `json:"engine_policy"`

	// Timeouts
	RiskTimeout time.Duration `json:"-"`
	RPCTimeout  time.Duration `json:"-"`
	TickPeriod  time.Duration `json:"-"`

	RiskTimeoutMS uint64 `json:"risk_timeout_ms"`
	RPCTimeoutMS  uint64 `json:"rpc_timeout_ms"`
	TickPeriodMS  uint64 `json:"tick_period_ms"`
}

type Addresses struct {
	Oracle      string `json:"oracle"`
	LoanManager string `json:"loan_manager"`
	Vault       string `json:"vault"`
	Settlement  string `json:"settlement"`
	// Risky is optional: a deployment with none supports settlement-only
	// accounts, which cannot demonstrate concentration.
	Risky string `json:"risky,omitempty"`
}

// RiskyAssetPolicy declares how the risky asset is valued.
//
// These are policy inputs, not observations. The prototype has no oracle
// for a test asset, and presenting an invented price as a measurement would
// be exactly the dishonesty `docs/04` forbids.
type RiskyAssetPolicy struct {
	Price      uint64 `json:"price"`
	PriceScale uint32 `json:"price_scale"`
	Decimals   uint32 `json:"decimals"`
	HaircutBps uint32 `json:"haircut_bps"`
}

type SchedulerPolicy struct {
	PriceMoveBps          uint32 `json:"price_move_bps"`
	MaxEvidenceAgeSeconds int64  `json:"max_evidence_age_seconds"`
	RefreshLeadSeconds    int64  `json:"refresh_lead_seconds"`
	HeartbeatSeconds      int64  `json:"heartbeat_seconds"`
}

// MarketPolicy configures the one external evidence source.
type MarketPolicy struct {
	ProviderURL   string `json:"provider_url,omitempty"`
	ClosesField   string `json:"closes_field,omitempty"`
	TimeField     string `json:"time_field,omitempty"`
	WindowSeconds uint64 `json:"window_seconds"`
	// LiquidityDepth is a declared policy input, not an observation: no
	// public feed reports the depth available to this protocol at
	// liquidation.
	LiquidityDepth string `json:"liquidity_depth"`
	// StaticCloses serves scenario facilities only.
	StaticCloses []uint64 `json:"static_closes,omitempty"`
}

// WalletPolicy bounds what a single transaction may cost.
type WalletPolicy struct {
	GasLimitCap    uint64 `json:"gas_limit_cap"`
	GasPriceCapWei uint64 `json:"gas_price_cap_wei"`
}

type EvidencePolicy struct {
	MaxSkewSeconds  uint64 `json:"max_skew_seconds"`
	ValiditySeconds uint64 `json:"validity_seconds"`
}

// Load reads, defaults, and validates a configuration file.
func Load(path string) (*Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	var cfg Config
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	// Reject unknown fields: a typo in a policy threshold would otherwise
	// be silently ignored and the default used instead.
	dec.DisallowUnknownFields()
	if err := dec.Decode(&cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	cfg.applyDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func (c *Config) applyDefaults() {
	if c.RiskTimeoutMS == 0 {
		c.RiskTimeoutMS = 5_000
	}
	if c.RPCTimeoutMS == 0 {
		c.RPCTimeoutMS = 10_000
	}
	if c.TickPeriodMS == 0 {
		c.TickPeriodMS = 15_000
	}
	if c.SchemaVersion == "" {
		c.SchemaVersion = "v1"
	}
	if c.StrategyID == "" {
		c.StrategyID = "spot-v1"
	}
	if c.Market.ClosesField == "" {
		c.Market.ClosesField = "closes"
	}
	if c.Market.TimeField == "" {
		c.Market.TimeField = "observed_at"
	}
	if c.RequestedClass == "" {
		c.RequestedClass = "PrototypeEligible"
	}
	c.RiskTimeout = time.Duration(c.RiskTimeoutMS) * time.Millisecond
	c.RPCTimeout = time.Duration(c.RPCTimeoutMS) * time.Millisecond
	c.TickPeriod = time.Duration(c.TickPeriodMS) * time.Millisecond
}

// Validate checks everything before any component is built.
func (c *Config) Validate() error {
	if c.RPCURL == "" {
		return fmt.Errorf("config: rpc_url is required")
	}
	if c.RiskServiceURL == "" {
		return fmt.Errorf("config: risk_service_url is required")
	}
	if c.DatabasePath == "" {
		return fmt.Errorf("config: database_path is required")
	}
	if c.ChainID == 0 {
		return fmt.Errorf("config: chain_id is required")
	}

	for name, addr := range map[string]string{
		"oracle":       c.Addresses.Oracle,
		"loan_manager": c.Addresses.LoanManager,
		"settlement":   c.Addresses.Settlement,
	} {
		if err := validateAddress(addr); err != nil {
			return fmt.Errorf("config: addresses.%s: %w", name, err)
		}
	}

	switch c.FacilityPolicy {
	case "Observed", "Scenario":
	default:
		return fmt.Errorf("config: facility_policy must be Observed or Scenario, got %q",
			c.FacilityPolicy)
	}
	// A scenario facility must name its scenario, or synthetic evidence
	// would have nothing to attribute it to (`docs/04`).
	if c.FacilityPolicy == "Scenario" && c.ScenarioID == "" {
		return fmt.Errorf("config: scenario_id is required for a Scenario facility")
	}
	if c.FacilityPolicy == "Observed" && c.ScenarioID != "" {
		return fmt.Errorf("config: scenario_id must be empty for an Observed facility")
	}

	switch c.RequestedClass {
	case "ScenarioOnly", "ObservedShadow", "PrototypeEligible":
	default:
		return fmt.Errorf("config: requested_class %q is not recognised", c.RequestedClass)
	}

	if err := validateBytes32(c.EvidencePolicyID); err != nil {
		return fmt.Errorf("config: evidence_policy_id: %w", err)
	}
	// A JSON null decodes to the four bytes "null", not an empty slice, so
	// a length check alone would let an explicit null through to the engine.
	if len(c.EnginePolicy) == 0 || string(c.EnginePolicy) == "null" {
		return fmt.Errorf("config: engine_policy is required")
	}

	// The scheduler's fail-closed guarantee depends on these relationships:
	// refresh must lead expiry, or permission lapses before it is renewed.
	if c.Scheduler.MaxEvidenceAgeSeconds <= 0 {
		return fmt.Errorf("config: scheduler.max_evidence_age_seconds must be positive")
	}
	if c.Scheduler.RefreshLeadSeconds <= 0 {
		return fmt.Errorf("config: scheduler.refresh_lead_seconds must be positive")
	}
	if c.Scheduler.RefreshLeadSeconds >= c.Scheduler.MaxEvidenceAgeSeconds {
		return fmt.Errorf(
			"config: refresh_lead_seconds (%d) must be less than max_evidence_age_seconds (%d), "+
				"or permission expires before it can be renewed",
			c.Scheduler.RefreshLeadSeconds, c.Scheduler.MaxEvidenceAgeSeconds)
	}
	if c.Scheduler.HeartbeatSeconds <= 0 {
		return fmt.Errorf("config: scheduler.heartbeat_seconds must be positive")
	}

	// A declared risky asset needs a complete valuation policy, or the
	// engine would mark a real position at zero and see no concentration.
	if c.Addresses.Risky != "" {
		if err := validateAddress(c.Addresses.Risky); err != nil {
			return fmt.Errorf("config: addresses.risky: %w", err)
		}
		if c.RiskyAsset.Price == 0 {
			return fmt.Errorf("config: risky_asset.price is required when a risky asset is set")
		}
		if c.RiskyAsset.Decimals == 0 {
			return fmt.Errorf("config: risky_asset.decimals is required when a risky asset is set")
		}
		if c.RiskyAsset.HaircutBps >= 10_000 {
			return fmt.Errorf("config: risky_asset.haircut_bps must be below 10000")
		}
	}

	// Both caps must be set: an unbounded gas limit or price turns a
	// routine monitoring update into an unbounded cost.
	if c.Wallet.GasLimitCap == 0 {
		return fmt.Errorf("config: wallet.gas_limit_cap must be positive")
	}
	if c.Wallet.GasPriceCapWei == 0 {
		return fmt.Errorf("config: wallet.gas_price_cap_wei must be positive")
	}

	if c.Market.WindowSeconds == 0 {
		return fmt.Errorf("config: market.window_seconds must be positive")
	}
	if c.Market.LiquidityDepth == "" {
		return fmt.Errorf("config: market.liquidity_depth is required")
	}
	// An observed facility cannot fall back to a static series: that would
	// be synthetic evidence inside an executable decision (`docs/04`).
	if c.FacilityPolicy == "Observed" && c.Market.ProviderURL == "" {
		return fmt.Errorf("config: market.provider_url is required for an Observed facility")
	}
	if c.FacilityPolicy == "Observed" && len(c.Market.StaticCloses) > 0 {
		return fmt.Errorf("config: market.static_closes is not permitted for an Observed facility")
	}
	if c.FacilityPolicy == "Scenario" && c.Market.ProviderURL == "" &&
		len(c.Market.StaticCloses) < 2 {
		return fmt.Errorf("config: a Scenario facility needs a provider or at least two static closes")
	}
	if err := validateAddress(c.Addresses.Vault); err != nil {
		return fmt.Errorf("config: addresses.vault: %w", err)
	}

	if c.Evidence.ValiditySeconds == 0 {
		return fmt.Errorf("config: evidence.validity_seconds must be positive")
	}
	// An assessment must not outlive the evidence behind it, or the chain
	// would hold permission the gateway no longer believes in.
	if c.Evidence.ValiditySeconds > uint64(c.Scheduler.MaxEvidenceAgeSeconds) {
		return fmt.Errorf(
			"config: validity_seconds (%d) must not exceed max_evidence_age_seconds (%d), "+
				"or an assessment outlives the evidence supporting it",
			c.Evidence.ValiditySeconds, c.Scheduler.MaxEvidenceAgeSeconds)
	}

	return nil
}

// SignerKey reads the signing key from the environment.
//
// Never from the config file: a committed config must not be able to carry
// a key, and a misconfigured deployment must fail loudly rather than sign
// with something unintended.
func SignerKey() ([]byte, error) {
	return readKey(SignerKeyEnv)
}

func readKey(env string) ([]byte, error) {
	raw := os.Getenv(env)
	if raw == "" {
		return nil, fmt.Errorf("%s is not set", env)
	}
	key, err := hex.DecodeString(strings.TrimPrefix(raw, "0x"))
	if err != nil {
		return nil, fmt.Errorf("%s is not hex: %w", env, err)
	}
	if len(key) != 32 {
		return nil, fmt.Errorf("%s must be 32 bytes, got %d", env, len(key))
	}
	return key, nil
}

// OperatorKey reads the gas-paying key from the environment.
func OperatorKey() ([]byte, error) {
	return readKey(OperatorKeyEnv)
}

func validateAddress(addr string) error {
	b, err := hex.DecodeString(strings.TrimPrefix(addr, "0x"))
	if err != nil {
		return fmt.Errorf("not hex: %w", err)
	}
	if len(b) != 20 {
		return fmt.Errorf("expected 20 bytes, got %d", len(b))
	}
	return nil
}

func validateBytes32(v string) error {
	b, err := hex.DecodeString(strings.TrimPrefix(v, "0x"))
	if err != nil {
		return fmt.Errorf("not hex: %w", err)
	}
	if len(b) != 32 {
		return fmt.Errorf("expected 32 bytes, got %d", len(b))
	}
	return nil
}
