// Package gatewayrt holds construction shared by the gateway binaries.
//
// `credence-gateway` assesses and publishes on the development route;
// `credence-assessd` only assesses, for the CRE route where a DON
// publishes instead. They are separate processes on purpose -- `docs/05`
// pins one authorization route per facility epoch -- but they read the
// same store, the same chain, and the same evidence.
//
// Shared rather than duplicated because these are the pieces where a
// divergence would be silent: two spellings of the binding query would
// answer differently after a schema change, and two policy hashes would
// let one process attest under a policy the other never ran.
package gatewayrt

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"

	"github.com/arcbjorn/credence/gateway/attest"
	"github.com/arcbjorn/credence/gateway/canonical"
	"github.com/arcbjorn/credence/gateway/chain"
	"github.com/arcbjorn/credence/gateway/config"
	"github.com/arcbjorn/credence/gateway/evidence"
	"github.com/arcbjorn/credence/gateway/market"
	"github.com/arcbjorn/credence/gateway/store"
)

// ModelHash is the identity of the risk engine that produces assessments.
//
// Must equal `risk_core::model_hash()`, which the engine verifies: an
// assessment naming a model the engine did not run attests to provenance
// nobody can check. Previously the literal "model-hash", which committed
// to nothing while the policy beside it was hashed for real.
//
//	cargo run -p risk-core --example model_hash
//
// Reported by review, 14 September.
const ModelHash = "eb1f28871b21718a9db5a3e65fcdca22fc1680e9f369d8adb25869966461a1a3"

// LoadBinding reads the on-chain identity an assessment binds to.
func LoadBinding(
	ctx context.Context,
	db *store.Store,
	bindingKey string,
	accountStateVersion uint64,
) (attest.Binding, error) {
	var (
		registryChainID            uint64
		registry, agentID          string
		operator, creditAccount    string
		facilityID                 string
		configVersion, policyEpoch uint64
		configHash, policyHash     string
	)
	err := db.DB().QueryRowContext(ctx,
		`SELECT a.identity_registry_chain_id, a.identity_registry, a.agent_id,
		        b.operator, b.credit_account, b.facility_id,
		        b.config_version, b.config_hash, b.policy_epoch, b.policy_hash
		 FROM bindings b JOIN agents a ON a.agent_key = b.agent_key
		 WHERE b.binding_key = ?`, bindingKey,
	).Scan(&registryChainID, &registry, &agentID, &operator, &creditAccount,
		&facilityID, &configVersion, &configHash, &policyEpoch, &policyHash)
	if err != nil {
		return attest.Binding{}, fmt.Errorf("unknown binding %q: %w", bindingKey, err)
	}

	registryAddr, err := ParseAddress(registry)
	if err != nil {
		return attest.Binding{}, fmt.Errorf("binding registry address: %w", err)
	}
	operatorAddr, err := ParseAddress(operator)
	if err != nil {
		return attest.Binding{}, fmt.Errorf("binding operator address: %w", err)
	}
	accountAddr, err := ParseAddress(creditAccount)
	if err != nil {
		return attest.Binding{}, fmt.Errorf("binding credit account: %w", err)
	}
	cfgHash, err := ParseHash(configHash)
	if err != nil {
		return attest.Binding{}, fmt.Errorf("binding config hash: %w", err)
	}
	polHash, err := ParseHash(policyHash)
	if err != nil {
		return attest.Binding{}, fmt.Errorf("binding policy hash: %w", err)
	}

	agent, ok := new(big.Int).SetString(agentID, 10)
	if !ok {
		return attest.Binding{}, fmt.Errorf("agent id %q is not numeric", agentID)
	}
	facility, ok := new(big.Int).SetString(facilityID, 10)
	if !ok {
		return attest.Binding{}, fmt.Errorf("facility id %q is not numeric", facilityID)
	}

	// AccountStateVersion is supplied by the CALLER, not read here.
	//
	// It must be the version the engine actually assessed, because three
	// places compare it and they must agree:
	//
	//   * the engine refuses a snapshot whose binding and account evidence
	//     disagree (`AccountStateVersionMismatch`);
	//   * the oracle stores it as the version permission applies to;
	//   * `riskAllowedFor` compares that against the account's CURRENT
	//     version at execution time.
	//
	// An earlier fix read the WATCHED version from the store. That is the
	// version permission was last bound to, which is frozen at enrolment
	// -- correct for the first cycle and wrong for every one after the
	// account executes again, at which point the engine rejects every
	// assessment and monitoring deadlocks. Reported by review,
	// 14 September; the first fix traded a silent failure for a loud one.
	return attest.Binding{
		IdentityRegistryChainID: new(big.Int).SetUint64(registryChainID),
		IdentityRegistry:        registryAddr,
		AgentID:                 agent,
		Operator:                operatorAddr,
		CreditAccount:           accountAddr,
		FacilityID:              facility,
		AccountStateVersion:     accountStateVersion,
		ConfigVersion:           configVersion,
		ConfigHash:              cfgHash,
		PolicyEpoch:             policyEpoch,
		PolicyHash:              polHash,
	}, nil
}

// AccountFor returns the credit account a binding names.
//
// Split out so a caller can read the account's LIVE state version before
// building the binding -- the version the engine assessed, which is what
// the oracle must store.
func AccountFor(
	ctx context.Context,
	db *store.Store,
	bindingKey string,
) (string, error) {
	var account string
	err := db.DB().QueryRowContext(ctx,
		`SELECT credit_account FROM bindings WHERE binding_key = ?`,
		bindingKey).Scan(&account)
	if err != nil {
		return "", fmt.Errorf("unknown binding %q: %w", bindingKey, err)
	}
	return account, nil
}

// NewRegistryStateVersion reads the watched account state version,
// reporting zero for a binding with no active loan.
func NewRegistryStateVersion(
	ctx context.Context,
	db *store.Store,
	bindingKey string,
) (uint64, error) {
	v, err := store.NewRegistry(db).WatchedStateVersion(ctx, bindingKey)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return 0, nil
		}
		return 0, fmt.Errorf("watched state version: %w", err)
	}
	return v, nil
}

// BuildMarketReader constructs the market source for this facility.
//
// A scenario facility may use a configured series, because synthetic
// evidence is permitted there and the engine caps the result at
// ScenarioOnly. An observed facility must have a real provider: falling
// back to a static series would feed invented data into an executable
// decision, which `docs/04` forbids outright.
func BuildMarketReader(cfg *config.Config, rpc *chain.RPC) (evidence.MarketReader, error) {
	if cfg.Market.ProviderURL != "" {
		provider := market.NewHTTPProvider(
			cfg.Market.ProviderURL,
			cfg.Market.ClosesField,
			cfg.Market.TimeField,
			cfg.RPCTimeout,
		)
		return market.NewReader(provider, cfg.Market.WindowSeconds,
			cfg.Market.LiquidityDepth), nil
	}

	if cfg.FacilityPolicy != "Scenario" {
		return nil, fmt.Errorf(
			"config: market.provider_url is required for an Observed facility; " +
				"a static series would be synthetic evidence in an executable decision")
	}
	// A scenario series is stamped with CHAIN time, not the gateway's
	// clock. The collector measures skew against the pinned block, so a
	// wall-clock stamp reads as stale by however far the two have drifted
	// -- which a scenario that advances chain time guarantees.
	return market.NewReader(
		market.StaticProvider{
			Closes: cfg.Market.StaticCloses,
			ObservedAt: func() uint64 {
				b, err := rpc.LatestBlock(context.Background())
				if err != nil {
					// Zero reads as maximally stale, which blocks. That is
					// the right failure: an unreachable chain means the
					// observation cannot be placed in time at all.
					return 0
				}
				return b.Timestamp
			},
		},
		cfg.Market.WindowSeconds, cfg.Market.LiquidityDepth), nil
}

func BindingLookup(
	db *store.Store, cfg *config.Config,
) func(string) (evidence.BindingInfo, bool) {
	registry := store.NewRegistry(db)
	return func(bindingKey string) (evidence.BindingInfo, bool) {
		ctx := context.Background()

		binding, err := registry.Binding(ctx, bindingKey)
		if err != nil {
			return evidence.BindingInfo{}, false
		}

		var (
			registryChainID uint64
			registryAddr    string
			agentID         string
		)
		err = db.DB().QueryRowContext(ctx,
			`SELECT identity_registry_chain_id, identity_registry, agent_id
			 FROM agents WHERE agent_key = ?`, binding.AgentKey,
		).Scan(&registryChainID, &registryAddr, &agentID)
		if err != nil {
			return evidence.BindingInfo{}, false
		}

		// The state version the permission is bound to. Read rather than
		// defaulted: the engine compares it against the account's live
		// version, so a placeholder zero blocks every cycle on an account
		// that has executed even once.
		stateVersion, err := registry.WatchedStateVersion(ctx, bindingKey)
		if err != nil {
			return evidence.BindingInfo{}, false
		}

		// The accepted sequence is read from chain at collection time, so
		// it is left at zero here and the collector supplies the proposal.
		raw, err := json.Marshal(map[string]any{
			"agent": map[string]any{
				"identity_registry_chain_id": registryChainID,
				"identity_registry":          registryAddr,
				"agent_id":                   agentID,
			},
			"operator":              binding.Operator,
			"credit_account":        binding.CreditAccount,
			"facility_id":           binding.FacilityID,
			"account_state_version": stateVersion,
			"config_version":        binding.ConfigVersion,
			"config_hash":           binding.ConfigHash,
			"policy_epoch":          binding.PolicyEpoch,
			"policy_hash":           binding.PolicyHash,
			"accepted_sequence":     0,
		})
		if err != nil {
			return evidence.BindingInfo{}, false
		}

		// `policy_hash` is the CONTENT hash of the engine policy this
		// deployment runs, not the binding's registry identifier. The
		// engine verifies it against the policy it is handed and refuses
		// an executable result on a mismatch, so a symbolic or
		// registry-scoped value here would block every assessment.
		//
		// The binding's own policy identity is not lost: it travels in
		// the signed envelope's binding, and `policy_id` names the epoch
		// it belongs to. They answer different questions -- which policy
		// RAN, and which policy record the binding is scoped to.
		modelPolicy, err := json.Marshal(map[string]any{
			"model_id":    "m1",
			"model_hash":  ModelHash,
			"policy_id":   fmt.Sprintf("epoch-%d", binding.PolicyEpoch),
			"policy_hash": PolicyContentHash(cfg.EnginePolicy),
		})
		if err != nil {
			return evidence.BindingInfo{}, false
		}

		// Derive the oracle's key from the same identity fields the
		// contract uses. A local identifier would read an unrelated slot.
		chainBinding, err := ChainBindingFrom(
			registryChainID, registryAddr, agentID, binding)
		if err != nil {
			return evidence.BindingInfo{}, false
		}

		return evidence.BindingInfo{
			Raw:         raw,
			ModelPolicy: modelPolicy,
			Account:     binding.CreditAccount,
			ChainKey:    attest.ChainKeyHex(chainBinding),
		}, true
	}
}

func ParseAddress(s string) (attest.Address, error) {
	var a attest.Address
	b, err := hex.DecodeString(Trim0x(s))
	if err != nil || len(b) != 20 {
		return a, fmt.Errorf("expected 20 hex bytes")
	}
	copy(a[:], b)
	return a, nil
}

// BindingLookup builds the binding the engine consumes.
//
// Every field comes from the registry. A placeholder would be hashed into
// the snapshot and signed, binding a decision to an identity that does not
// exist — so an incomplete record is a lookup failure, not a default.
// PolicyContentHash is the canonical hash of the engine policy.
//
// The engine verifies a snapshot's declared policy_hash against the
// policy it is handed, so this is what must be declared. An unhashable
// policy yields an empty string, which the engine then refuses -- the
// right failure, since it cannot commit to a policy it could not encode.
func PolicyContentHash(policy json.RawMessage) string {
	decoded, err := canonical.DecodeStrict(policy)
	if err != nil {
		return ""
	}
	h, err := canonical.HashHex(decoded)
	if err != nil {
		return ""
	}
	return h
}

func ParseHash(s string) (attest.Hash, error) {
	var h attest.Hash
	b, err := hex.DecodeString(Trim0x(s))
	if err != nil || len(b) != 32 {
		return h, fmt.Errorf("expected 32 hex bytes")
	}
	copy(h[:], b)
	return h, nil
}

// ChainBindingFrom assembles the fields the oracle keys on.
func ChainBindingFrom(
	registryChainID uint64,
	registryAddr, agentID string,
	binding store.Binding,
) (attest.Binding, error) {
	registry, err := ParseAddress(registryAddr)
	if err != nil {
		return attest.Binding{}, err
	}
	account, err := ParseAddress(binding.CreditAccount)
	if err != nil {
		return attest.Binding{}, err
	}
	agent, ok := new(big.Int).SetString(agentID, 10)
	if !ok {
		return attest.Binding{}, fmt.Errorf("agent id %q is not numeric", agentID)
	}
	facility, ok := new(big.Int).SetString(binding.FacilityID, 10)
	if !ok {
		return attest.Binding{}, fmt.Errorf("facility id %q is not numeric", binding.FacilityID)
	}
	return attest.Binding{
		IdentityRegistryChainID: new(big.Int).SetUint64(registryChainID),
		IdentityRegistry:        registry,
		AgentID:                 agent,
		CreditAccount:           account,
		FacilityID:              facility,
	}, nil
}

func Trim0x(s string) string {
	if len(s) >= 2 && s[:2] == "0x" {
		return s[2:]
	}
	return s
}
