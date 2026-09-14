//! Snapshot identity and structural validation.

use risk_core::evidence::*;
use risk_core::input::*;
use risk_core::snapshot;
use risk_core::units::{Amount, Bps};

fn evidence<T>(value: T, id: &str) -> Evidence<T> {
    Evidence {
        value: Some(value),
        state: State::Present,
        origin: Origin::Observed,
        requirement: Requirement::Required,
        source: "test".into(),
        observed_at: Some(1_000),
        retrieved_at: 1_000,
        coverage: Coverage::Complete,
        evidence_id: id.into(),
        content_hash: "h".into(),
        parent_evidence_ids: vec![],
        scenario_id: None,
    }
}

fn input() -> RiskInput {
    RiskInput {
        schema_version: "v1".into(),
        facility_policy: FacilityPolicy::Observed,
        requested_class: Class::PrototypeEligible,
        assessment_time: 1_000,
        scenario_id: None,
        proposal: Proposal {
            sequence: 5,
            valid_after: 900,
            valid_until: 4_600,
        },
        binding: Binding {
            agent: AgentRef {
                identity_registry_chain_id: 10143,
                identity_registry: "0xreg".into(),
                agent_id: "184".into(),
            },
            operator: "0xop".into(),
            credit_account: "0xacct".into(),
            facility_id: "1".into(),
            account_state_version: 3,
            config_version: 1,
            config_hash: "cfg".into(),
            policy_epoch: 1,
            policy_hash: "pol".into(),
            accepted_sequence: 4,
        },
        context: Context::Origination {
            request: Request {
                request_id: "req-1".into(),
                settlement_asset: "0xusd".into(),
                requested_principal: Amount(5_000_000_000),
                term_seconds: 10 * 86_400,
                max_accepted_apr: Bps(1_500),
                action_envelope_hash: "act".into(),
                intent_hash: "intent".into(),
                intended_action: risk_core::stress::IntendedAction::Hold,
            },
        },
        account: evidence(
            AccountState {
                settlement_balance: Amount(5_000_000_000),
                positions: vec![],
                state_version: 3,
                outstanding_debt: Amount::ZERO,
                facility_limit: Amount(12_000_000_000),
                restricted: false,
            },
            "acct-ev",
        ),
        market: evidence(
            MarketState {
                observed_at: 1_000,
                realized_volatility_bps: Bps(4_000),
                volatility_window_seconds: 86_400,
                liquidity_depth: Amount(50_000_000_000),
            },
            "mkt-ev",
        ),
        history: evidence(
            HistorySummary {
                coverage_complete: true,
                ..Default::default()
            },
            "hist-ev",
        ),
        forecasts: evidence(ForecastSummary::default(), "fc-ev"),
        portfolio: evidence(
            PortfolioContext {
                strategy_id: "spot-v1".into(),
                remaining_strategy_budget: Amount(20_000_000_000),
                remaining_vault_budget: Amount(50_000_000_000),
                available_cash: Amount(50_000_000_000),
            },
            "pf-ev",
        ),
        model_policy: ModelPolicy {
            model_id: "m1".into(),
            model_hash: risk_core::model_hash(),
            policy_id: "p1".into(),
            policy_hash: "policy-hash".into(),
        },
    }
}

// --- Replay identity ---

/// The core replay property: the same input yields the same identity, with
/// no counter, clock, or allocation involved.
#[test]
fn identity_is_reproducible() {
    let a = snapshot::identify(&input()).unwrap();
    let b = snapshot::identify(&input()).unwrap();
    assert_eq!(a, b);
}

/// Any change to the input changes the hash — otherwise a replay could
/// "match" against different evidence.
#[test]
fn any_input_change_changes_hash() {
    let base = snapshot::input_hash(&input()).unwrap();

    let mut changed = input();
    changed.assessment_time = 1_001;
    assert_ne!(snapshot::input_hash(&changed).unwrap(), base);

    let mut changed = input();
    changed.proposal.sequence = 6;
    assert_ne!(
        snapshot::input_hash(&changed).unwrap(),
        base,
        "sequence is inside the hash"
    );

    let mut changed = input();
    changed.binding.config_hash = "other".into();
    assert_ne!(snapshot::input_hash(&changed).unwrap(), base);
}

/// `docs/04`: replacing a simulated observation with an observed one
/// changes lineage and therefore the hash, even at an identical value.
#[test]
fn origin_change_changes_hash_at_same_value() {
    let base = snapshot::input_hash(&input()).unwrap();
    let mut synthetic = input();
    synthetic.market.origin = Origin::Synthetic;
    assert_ne!(snapshot::input_hash(&synthetic).unwrap(), base);
}

/// Model and policy are hashed independently, so swapping either produces a
/// different assessment identity at the same input.
#[test]
fn model_and_policy_are_independent() {
    let ih = "input-hash";
    let base = snapshot::assessment_id("v1", ih, "m", "p");
    assert_ne!(snapshot::assessment_id("v1", ih, "m2", "p"), base);
    assert_ne!(snapshot::assessment_id("v1", ih, "m", "p2"), base);
}

/// Length-prefixing prevents field-boundary collisions.
#[test]
fn tuple_fields_cannot_collide() {
    assert_ne!(
        snapshot::assessment_id("v1", "ab", "c", "p"),
        snapshot::assessment_id("v1", "a", "bc", "p")
    );
}

// --- Structural validation ---

#[test]
fn valid_input_passes() {
    assert_eq!(input().validate(), Ok(()));
}

/// `docs/04`: reject duplicate evidence IDs.
#[test]
fn duplicate_evidence_ids_rejected() {
    let mut i = input();
    i.market.evidence_id = "acct-ev".into();
    assert_eq!(
        i.validate(),
        Err(InputError::DuplicateEvidenceId("acct-ev".into()))
    );
}

/// Account and market may not be labelled Optional.
///
/// The evidence gate blocks on stale REQUIRED evidence and merely omits
/// stale optional evidence -- but the engine reads account and market
/// unconditionally. An optional label would therefore let a stale account
/// be reported as omitted while still supporting an executable approval,
/// with the label alone deciding whether staleness blocks (`docs/04`).
///
/// Reported by review, 14 September.
#[test]
fn core_evidence_cannot_be_optional() {
    let mut i = input();
    i.account.requirement = Requirement::Optional;
    assert_eq!(
        i.validate(),
        Err(InputError::CoreEvidenceNotRequired("account".into()))
    );

    let mut i = input();
    i.market.requirement = Requirement::Optional;
    assert_eq!(
        i.validate(),
        Err(InputError::CoreEvidenceNotRequired("market".into()))
    );
}

/// Enrichment may still be optional: only the two the engine consumes
/// unconditionally are pinned.
#[test]
fn enrichment_may_remain_optional() {
    let mut i = input();
    i.history.requirement = Requirement::Optional;
    i.forecasts.requirement = Requirement::Optional;
    i.portfolio.requirement = Requirement::Optional;
    assert!(i.validate().is_ok());
}

/// The proposed sequence must exceed what the chain already accepted.
#[test]
fn non_increasing_sequence_rejected() {
    let mut i = input();
    i.proposal.sequence = 4;
    assert!(matches!(
        i.validate(),
        Err(InputError::SequenceNotIncreasing { .. })
    ));
}

#[test]
fn inverted_validity_window_rejected() {
    let mut i = input();
    i.proposal.valid_until = i.proposal.valid_after;
    assert!(matches!(
        i.validate(),
        Err(InputError::InvalidValidityWindow { .. })
    ));
}

#[test]
fn assessment_time_outside_window_rejected() {
    let mut i = input();
    i.assessment_time = 5_000;
    assert!(matches!(
        i.validate(),
        Err(InputError::AssessmentTimeOutsideWindow { .. })
    ));
}

/// The binding must describe the account being assessed.
#[test]
fn account_state_version_mismatch_rejected() {
    let mut i = input();
    if let Some(a) = i.account.value.as_mut() {
        a.state_version = 9;
    }
    assert!(matches!(
        i.validate(),
        Err(InputError::AccountStateVersionMismatch { .. })
    ));
}

/// A missing account is an evidence blocker, not a structural mismatch —
/// the two failure kinds must stay distinguishable.
#[test]
fn missing_account_is_not_a_structural_error() {
    let mut i = input();
    i.account.value = None;
    i.account.state = State::Missing;
    assert_eq!(
        i.validate(),
        Ok(()),
        "absence is judged by evidence policy, not structure"
    );
}

/// A scenario facility must name its scenario.
#[test]
fn scenario_facility_requires_scenario_id() {
    let mut i = input();
    i.facility_policy = FacilityPolicy::Scenario;
    assert_eq!(i.validate(), Err(InputError::ScenarioIdMissing));

    i.scenario_id = Some("run-1".into());
    assert_eq!(i.validate(), Ok(()));
}

/// Monitoring carries an active loan and no request — the two contexts are
/// mutually exclusive by construction (`docs/04`).
#[test]
fn monitoring_context_carries_loan_not_request() {
    let mut i = input();
    i.context = Context::Monitoring {
        loan: ActiveLoan {
            loan_id: "loan-1".into(),
            settlement_asset: "0xusd".into(),
            original_principal: Amount(5_000_000_000),
            outstanding_principal: Amount(5_000_000_000),
            apr: Bps(1_200),
            originated_at: 500,
            maturity_at: 900_000,
            in_recovery: false,
            interest_collected: Amount::ZERO,
        },
    };
    assert_eq!(i.validate(), Ok(()));
    assert!(!i.context.is_origination());
    // Different context, different hash.
    assert_ne!(
        snapshot::input_hash(&i).unwrap(),
        snapshot::input_hash(&input()).unwrap()
    );
}

/// Emits the canonical snapshot and its hash to `schemas/v1/` so the Go
/// gateway can prove it computes the same `input_hash`. Run with:
///   cargo test --test snapshot -- --ignored emit_snapshot_vector
#[test]
#[ignore]
fn emit_snapshot_vector() {
    use std::fs;
    let i = input();
    let bytes = snapshot::encode_input(&i).unwrap();
    let identity = snapshot::identify(&i).unwrap();

    let doc = serde_json::json!({
        "_comment": "Canonical RiskInput snapshot vector (docs/04). Rust emits it; Go must compute the same input_hash from the same canonical bytes. Regenerate with: cargo test --test snapshot -- --ignored emit_snapshot_vector",
        "canonical": String::from_utf8(bytes).unwrap(),
        "input_hash": identity.input_hash,
        "assessment_id": identity.assessment_id,
        "input": i,
    });
    let root = std::path::Path::new(env!("CARGO_MANIFEST_DIR"))
        .ancestors()
        .nth(3)
        .unwrap()
        .join("schemas/v1/snapshot-vector.json");
    fs::write(root, serde_json::to_string_pretty(&doc).unwrap() + "\n").unwrap();
}
