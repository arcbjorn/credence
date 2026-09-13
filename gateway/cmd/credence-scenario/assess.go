package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"

	"github.com/arcbjorn/credence/gateway/attest"
	"github.com/arcbjorn/credence/gateway/canonical"
	"github.com/arcbjorn/credence/gateway/chain"
	"github.com/arcbjorn/credence/gateway/evidence"
	"github.com/arcbjorn/credence/gateway/riskclient"
	"github.com/arcbjorn/credence/gateway/store"
	"github.com/arcbjorn/credence/gateway/snapshot"
)

// originationTerms asks the engine what this request is worth.
//
// The scenario used to sign fixed terms — a constant APR, zero required
// equity — which made the three-sequential-loans release row unprovable:
// identical terms every time say nothing about credit compounding, however
// many loans run. `docs/11` requires the limit to rise and the APR to fall
// under one policy and model hash, and only the engine can produce that.
//
// Returns the engine's raw assessment alongside the terms, so the caller
// can retain exactly what was decided rather than a reconstruction.
func originationTerms(
	ctx context.Context,
	risk *riskclient.Client,
	reader *chain.Reader,
	rpc *chain.RPC,
	account, settlement string,
	bindingJSON json.RawMessage,
	sequence uint64,
	principal uint64,
	termSeconds uint64,
	history snapshot.Evidence,
	forecasts snapshot.Evidence,
	policy json.RawMessage,
	intendedAcquire *AcquireAction,
) (*assessedTerms, json.RawMessage, error) {
	block, err := rpc.LatestBlock(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("pin block: %w", err)
	}
	at := evidence.BlockRef{
		Number: block.Number, Hash: block.Hash, Timestamp: block.Timestamp,
	}

	acct, err := reader.AccountAt(ctx, account, at)
	if err != nil {
		return nil, nil, fmt.Errorf("read account: %w", err)
	}

	in := snapshot.Input{
		SchemaVersion:  "v1",
		FacilityPolicy: "Scenario",
		RequestedClass: "PrototypeEligible",
		AssessmentTime: block.Timestamp,
		ScenarioID:     scenarioID(),
		Binding:        bindingJSON,
		ModelPolicy:    modelPolicy(),
		Proposal: snapshot.Proposal{
			Sequence:   sequence,
			ValidAfter: block.Timestamp,
			ValidUntil: block.Timestamp + 3600,
		},
	}

	// The action the loan will ACTUALLY take. Assessing a cash hold while
	// funding an acquisition sizes the loan against a portfolio that will
	// not exist a block later -- the engine would be answering a different
	// question than the one the draw poses (`docs/03` sizes on the
	// post-action portfolio).
	action := `"Hold"`
	if intendedAcquire != nil {
		action = fmt.Sprintf(
			`{"Acquire":{"asset":"%s","price":"%d","price_scale":%d,"haircut_bps":%d}}`,
			intendedAcquire.Asset, intendedAcquire.Price,
			intendedAcquire.PriceScale, intendedAcquire.HaircutBps)
	}

	in.Context = json.RawMessage(fmt.Sprintf(
		`{"kind":"Origination","request":{"request_id":"req-%d",`+
			`"settlement_asset":"%s","requested_principal":"%d",`+
			`"term_seconds":%d,"max_accepted_apr":4000,`+
			`"action_envelope_hash":"act","intent_hash":"intent",`+
			`"intended_action":%s}}`,
		sequence, settlement, principal, termSeconds, action))

	if in.Account, err = snapshot.Present(
		"acct", "chain", snapshot.OriginObserved, snapshot.Required,
		block.Timestamp, block.Timestamp, acct,
	); err != nil {
		return nil, nil, err
	}

	// A scenario facility declares its market rather than observing one,
	// and the declaration is labelled as such by the facility policy.
	if in.Market, err = snapshot.Present(
		"mkt", "declared", snapshot.OriginSynthetic, snapshot.Required,
		block.Timestamp, block.Timestamp, scenarioMarket(block.Timestamp),
	); err != nil {
		return nil, nil, err
	}

	in.History = history

	// From the database this run actually writes, not a fixed pair. A
	// constant sample makes the forecast penalty independent of the
	// forecasts, which is precisely what `docs/06` scores.
	in.Forecasts = forecasts

	if in.Portfolio, err = snapshot.Present(
		"pf", "derived", snapshot.OriginObserved, snapshot.Required,
		block.Timestamp, block.Timestamp,
		map[string]any{
			"strategy_id":               "spot-v1",
			"remaining_strategy_budget": "96000000000",
			"remaining_vault_budget":    "96000000000",
			"available_cash":            "46000000000",
		},
	); err != nil {
		return nil, nil, err
	}

	if err := in.Validate(); err != nil {
		return nil, nil, fmt.Errorf("snapshot: %w", err)
	}

	inputJSON, err := json.Marshal(in)
	if err != nil {
		return nil, nil, err
	}
	raw, err := risk.Assess(ctx, inputJSON, policy)
	if err != nil {
		return nil, nil, fmt.Errorf("assess: %w", err)
	}

	var doc struct {
		Outcome struct {
			Decision          string `json:"decision"`
			ApprovedPrincipal string `json:"approved_principal"`
			Terms             struct {
				AprBps         uint32 `json:"apr_bps"`
				TermSeconds    uint64 `json:"term_seconds"`
				RequiredEquity string `json:"required_equity"`
			} `json:"terms"`
			Capacity struct {
				AgentCapacity string `json:"agent_capacity"`
				AgentBinding  string `json:"agent_binding"`
			} `json:"capacity"`
		} `json:"outcome"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, nil, fmt.Errorf("decode assessment: %w", err)
	}
	if doc.Outcome.Decision != "ApprovedOrigination" {
		// A denial is a legitimate answer, not a failure. The caller
		// reports it rather than signing terms the engine refused.
		return nil, raw, nil
	}

	approved, ok := new(big.Int).SetString(doc.Outcome.ApprovedPrincipal, 10)
	if !ok {
		return nil, nil, fmt.Errorf("approved principal %q", doc.Outcome.ApprovedPrincipal)
	}
	equity, ok := new(big.Int).SetString(doc.Outcome.Terms.RequiredEquity, 10)
	if !ok {
		equity = big.NewInt(0)
	}

	return &assessedTerms{
		ApprovedPrincipal: approved,
		AprBps:            doc.Outcome.Terms.AprBps,
		TermSeconds:       doc.Outcome.Terms.TermSeconds,
		RequiredEquity:    equity,
		AgentCapacity:     doc.Outcome.Capacity.AgentCapacity,
		AgentBinding:      doc.Outcome.Capacity.AgentBinding,
		InputJSON:         inputJSON,
	}, raw, nil
}

// AcquireAction describes a draw that buys a risky asset rather than
// holding cash.
type AcquireAction struct {
	Asset      string
	Price      uint64
	PriceScale uint32
	HaircutBps uint32
}

type assessedTerms struct {
	ApprovedPrincipal *big.Int
	AprBps            uint32
	TermSeconds       uint64
	RequiredEquity    *big.Int
	AgentCapacity     string
	AgentBinding      string
	InputJSON         json.RawMessage
}

func scenarioID() *string {
	s := "demo-4b"
	return &s
}

func modelPolicy() json.RawMessage {
	return json.RawMessage(
		`{"model_id":"m1","model_hash":"model-hash",` +
			`"policy_id":"p1","policy_hash":"policy-hash"}`)
}

// scenarioMarket is the declared market for a scenario facility. Depth is
// the venue's real liquidity: 1,000 risky units at 2000.000000.
func scenarioMarket(observedAt uint64) map[string]any {
	return map[string]any{
		"observed_at":               observedAt,
		"realized_volatility_bps":   2000,
		"volatility_window_seconds": 3600,
		"liquidity_depth":           "2000000000000",
	}
}

// historyEvidence is the repayment record the engine prices against.
//
// Weighted by exposure and duration (`docs/03`), which here is one
// completed loan's worth per repayment — enough for the sequential-loans
// row to show history accumulating without inventing a richer record than
// the scenario actually produced.
func historyEvidence(observedAt uint64, repayments []repayment) snapshot.Evidence {
	// `docs/06`: weight = (principal / reference) * min(eligible, 30d)/30d,
	// summed. A flat per-repayment credit would let ten instant micro-loans
	// approach the weight of real capital held for real duration, which is
	// exactly the anti-farming property the formula exists to provide.
	const referenceSize = 5_000_000_000 // one reference loan
	const maxEligible = 30 * 86_400

	var weighted uint64
	for _, r := range repayments {
		eligible := r.EligibleSeconds
		if eligible > maxEligible {
			eligible = maxEligible
		}
		// Integer throughout: ppm of reference, scaled by duration share.
		sizeShare := r.Principal * 1_000_000 / referenceSize
		weighted += sizeShare * eligible / maxEligible
	}
	if weighted > 1_000_000 {
		weighted = 1_000_000
	}
	ev, err := snapshot.Present(
		"hist", "derived", snapshot.OriginObserved, snapshot.Required,
		observedAt, observedAt,
		map[string]any{
			"weighted_repayments_ppm": weighted,
			"resolved_defaults":       0,
			"policy_breaches":         0,
			"coverage_complete":       true,
		},
	)
	if err != nil {
		// A snapshot that cannot be built is Missing, which blocks rather
		// than silently crediting a clean record.
		return snapshot.Missing("hist", "derived",
			snapshot.OriginObserved, snapshot.Required, observedAt)
	}
	return ev
}

// repayment is one settled loan, as observed rather than assumed.
type repayment struct {
	Principal       uint64
	EligibleSeconds uint64
}

// bindingSnapshotJSON renders the binding in the shape the engine reads.
//
// Distinct from the ABI encoding: the contract takes eleven flat words,
// while the engine takes a nested agent reference. Both describe the same
// binding, so they are built from one source.
func bindingSnapshotJSON(b attest.Binding, acceptedSequence uint64) (json.RawMessage, error) {
	return json.Marshal(map[string]any{
		"agent": map[string]any{
			"identity_registry_chain_id": b.IdentityRegistryChainID.Uint64(),
			"identity_registry":          addressHex(b.IdentityRegistry),
			"agent_id":                   b.AgentID.String(),
		},
		"operator":              addressHex(b.Operator),
		"credit_account":        addressHex(b.CreditAccount),
		"facility_id":           b.FacilityID.String(),
		"account_state_version": b.AccountStateVersion,
		"config_version":        b.ConfigVersion,
		"config_hash":           hashHex(b.ConfigHash),
		"policy_epoch":          b.PolicyEpoch,
		"policy_hash":           hashHex(b.PolicyHash),
		"accepted_sequence":     acceptedSequence,
	})
}

// commitForecast records a drawdown-probability forecast before funding.
//
// The event `docs/06` defines: cash-flow-adjusted marked account NAV falls
// more than 5% below its observed peak within 20 minutes. One quantity is
// scored this release, chosen because it resolves precisely.
//
// The probability declines across a run as the agent accumulates history,
// which is what makes repeated forecasts scoreable rather than a single
// observation -- `docs/06` is explicit that one drawdown exceeding a
// low-probability forecast is an observation, not proof of deception.
func commitForecast(
	ctx context.Context,
	dbPath string,
	binding attest.Binding,
	chainKey string,
	loanIndex int,
	committedAt uint64,
) error {
	db, err := store.Open(ctx, dbPath)
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}
	defer db.Close()
	reg := store.NewRegistry(db)

	if err := registerBindingFor(ctx, reg, binding, chainKey); err != nil {
		return err
	}

	// Declining confidence in a drawdown as the record builds.
	probability := uint64(300_000)
	if loanIndex > 1 {
		probability = 300_000 / uint64(loanIndex)
	}

	payload, err := json.Marshal(map[string]any{
		"event": "cash-flow-adjusted marked account NAV falls more than 5% " +
			"below its observed peak during the next 20 minutes",
		"probability_ppm":  probability,
		"horizon_seconds":  1_200,
		"observation":      "supported account, named price source, fixed cadence",
		"early_exit_rule":  "scored on close; final unitized equity carries the horizon",
		"committed_before": "disbursement",
	})
	if err != nil {
		return err
	}

	return reg.CommitForecast(ctx, store.Forecast{
		ForecastID:      fmt.Sprintf("%s:forecast:%d", chainKey, loanIndex),
		BindingKey:      chainKey,
		EventDefinition: "nav_drawdown_5pct_20m",
		ProbabilityPPM:  probability,
		HorizonSeconds:  1_200,
		CommittedAt:     int64(committedAt),
		Payload:         payload,
	})
}

// resolveForecast records what the horizon actually showed.
//
// Called after the loan closes, so the outcome is observed rather than
// assumed. A run that never resolves leaves the commitment counted as
// unresolved, which takes the engine's maximum forecast penalty.
func resolveForecast(
	ctx context.Context,
	dbPath, chainKey string,
	loanIndex int,
	occurred bool,
	resolvedAt uint64,
) error {
	db, err := store.Open(ctx, dbPath)
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}
	defer db.Close()

	resolution := store.DidNotOccur
	if occurred {
		resolution = store.Occurred
	}
	forecastID := fmt.Sprintf("%s:forecast:%d", chainKey, loanIndex)
	return store.NewRegistry(db).ResolveForecast(
		ctx, forecastID+":outcome", forecastID, resolution, int64(resolvedAt), nil)
}

// forecastEvidence summarises the forecasts this run actually committed.
//
// Read from the store rather than declared, so the forecast penalty
// responds to the predictions that were made. A fixed sample makes the
// penalty independent of the forecasts, which is what `docs/06` scores.
//
// An unreadable store yields Missing rather than a clean-looking default:
// absence of evidence is not evidence of accuracy.
func forecastEvidence(
	ctx context.Context,
	dbPath, bindingKey string,
	observedAt uint64,
) snapshot.Evidence {
	missing := snapshot.Missing(
		"fc", "derived", snapshot.OriginObserved, snapshot.Required, observedAt)
	if dbPath == "" {
		return missing
	}

	db, err := store.Open(ctx, dbPath)
	if err != nil {
		return missing
	}
	defer db.Close()

	raw, err := store.NewDerived(db).Forecasts(ctx, bindingKey)
	if err != nil {
		return missing
	}
	// DecodeStrict, not json.Unmarshal: the latter produces float64 for
	// every number, which the canonical encoder rejects -- and the
	// rejection would surface as Missing evidence rather than as the
	// decoding bug it is.
	summary, err := canonical.DecodeStrict(raw)
	if err != nil {
		return missing
	}

	ev, err := snapshot.Present(
		"fc", "derived", snapshot.OriginObserved, snapshot.Required,
		observedAt, observedAt, summary)
	if err != nil {
		return missing
	}
	return ev
}
