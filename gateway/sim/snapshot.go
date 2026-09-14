package sim

import (
	"encoding/json"
	"fmt"

	"github.com/arcbjorn/credence/gateway/evidence"
	"github.com/arcbjorn/credence/gateway/snapshot"
)

// snapshot builds what the underwriter is allowed to see at this moment.
//
// This function is the wall. Everything the engine learns about the world
// passes through here, and every field is either an observation at or
// before `obs.At` or a fact about the agent's own ledger. The world's
// shock schedule, its seed, its future prices and its generator version
// are all in scope on the caller's side and none of them appear below.
//
// Marked `SCENARIO` throughout: `docs/07` requires imported virtual
// history to remain synthetic and be accepted only by a scenario
// facility, and `docs/04` caps such a result at `ScenarioOnly`. A
// simulation that labelled its evidence as observed would be claiming a
// provenance it does not have.
func (r *Runner) snapshot(obs Observation, p Proposal) (json.RawMessage, error) {
	now := uint64(obs.At)
	scenario := r.cfg.Pack

	in := snapshot.Input{
		SchemaVersion:  "v1",
		FacilityPolicy: "Scenario",
		RequestedClass: "ScenarioOnly",
		AssessmentTime: now,
		ScenarioID:     &scenario,
		Proposal: snapshot.Proposal{
			Sequence:   uint64(len(r.queue.events) + 1),
			ValidAfter: now - 1,
			ValidUntil: now + 3_600,
		},
	}

	in.Binding = json.RawMessage(fmt.Sprintf(
		`{"agent":{"identity_registry_chain_id":10143,`+
			`"identity_registry":"0x00000000000000000000000000000000000000ae",`+
			`"agent_id":"1"},`+
			`"operator":"0x00000000000000000000000000000000000000a9",`+
			`"credit_account":"0x00000000000000000000000000000000000000ac",`+
			`"facility_id":"1","account_state_version":%d,`+
			`"config_version":1,`+
			`"config_hash":"0x%064x","policy_epoch":1,`+
			`"policy_hash":"0x%064x","accepted_sequence":0}`,
		r.stateVersion(), 1, 2))

	// The action the principal would perform, so the engine sizes the
	// loan against the portfolio it WOULD produce rather than a cash
	// hold. deploy_bps 10000: the whole principal goes into the asset,
	// which is what the runner then actually does.
	action := fmt.Sprintf(
		`{"Acquire":{"asset":"RISKY","price":"%d","price_scale":%d,`+
			`"haircut_bps":500,"deploy_bps":10000}}`,
		obs.Price, obs.PriceScale)

	in.Context = json.RawMessage(fmt.Sprintf(
		`{"kind":"Origination","request":{"request_id":"sim-%d",`+
			`"settlement_asset":"SETTLE","requested_principal":"%d",`+
			`"term_seconds":%d,"max_accepted_apr":%d,`+
			`"action_envelope_hash":"act","intent_hash":"intent",`+
			`"intended_action":%s}}`,
		obs.At, p.Principal, p.TermSeconds, p.MaxAprBps, action))

	in.ModelPolicy = json.RawMessage(fmt.Sprintf(
		`{"model_id":"m1","model_hash":"%s",`+
			`"policy_id":"epoch-1","policy_hash":"%s"}`,
		r.cfg.ModelHash, r.cfg.PolicyHash))

	account := evidence.AccountState{
		SettlementBalance: fmt.Sprintf("%d", r.equity),
		StateVersion:      r.stateVersion(),
		OutstandingDebt:   "0",
		FacilityLimit:     "100000000000",
		Positions:         []evidence.Position{},
	}

	market := evidence.MarketState{
		ObservedAt:              now,
		RealizedVolatilityBps:   uint32(obs.RealizedVolBps),
		VolatilityWindowSeconds: 86_400,
		LiquidityDepth:          fmt.Sprintf("%d", obs.LiquidityDepth),
	}

	var err error
	// Origin SYNTHETIC, not observed. This is generated history and
	// saying otherwise would be the exact misrepresentation `docs/07`
	// warns about -- the engine caps the class at ScenarioOnly because of
	// this label, and a wrong one would let synthetic evidence support an
	// executable result.
	if in.Account, err = snapshot.Present(
		"acct", "sim", snapshot.OriginSynthetic, snapshot.Required,
		now, now, account,
	); err != nil {
		return nil, err
	}
	if in.Market, err = snapshot.Present(
		"market", "sim", snapshot.OriginSynthetic, snapshot.Required,
		now, now, market,
	); err != nil {
		return nil, err
	}

	// The agent's own record, which accumulates as the run proceeds.
	// Weighted by what actually resolved, not by how many loans were
	// attempted (`docs/06`).
	history := map[string]any{
		"weighted_repayments_ppm": r.repaymentPPM(),
		"coverage_complete":       true,
		"resolved_defaults":       r.outcome.Defaulted,
		"policy_breaches":         0,
		"reference_size":          "1000000000",
	}
	if in.History, err = snapshot.Present(
		"history", "sim", snapshot.OriginSynthetic, snapshot.Optional,
		now, now, history,
	); err != nil {
		return nil, err
	}

	forecasts := map[string]any{
		"comparable_resolved": r.outcome.Repaid + r.outcome.Defaulted,
		"mean_brier_ppm":      100_000,
		"unresolved":          0,
		"unobservable":        0,
	}
	if in.Forecasts, err = snapshot.Present(
		"forecasts", "sim", snapshot.OriginSynthetic, snapshot.Optional,
		now, now, forecasts,
	); err != nil {
		return nil, err
	}

	portfolio := map[string]any{"strategy_id": "sim-spot-v1"}
	if in.Portfolio, err = snapshot.Present(
		"portfolio", "sim", snapshot.OriginSynthetic, snapshot.Optional,
		now, now, portfolio,
	); err != nil {
		return nil, err
	}

	if err := in.Validate(); err != nil {
		return nil, fmt.Errorf("sim built an invalid snapshot: %w", err)
	}
	return json.Marshal(in)
}

// stateVersion advances with every resolved loan, mirroring what an
// account's executions would do on chain.
func (r *Runner) stateVersion() uint64 {
	return uint64(r.outcome.Repaid + r.outcome.Recovered + r.outcome.Defaulted)
}

// repaymentPPM is the agent's demonstrated record so far.
//
// Resolved loans only. An agent that has borrowed once and not yet repaid
// has demonstrated nothing, and crediting it would let the first loan
// bootstrap the second.
func (r *Runner) repaymentPPM() uint64 {
	resolved := r.outcome.Repaid + r.outcome.Recovered + r.outcome.Defaulted
	if resolved == 0 {
		return 0
	}
	return uint64(r.outcome.Repaid) * 1_000_000 / uint64(resolved)
}
