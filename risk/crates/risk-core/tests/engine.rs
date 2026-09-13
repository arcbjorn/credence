//! End-to-end evaluation through all nine steps.

use risk_core::assessment::{Outcome, Permission, Reason};
use risk_core::capacity::{BindingConstraint, ForecastPolicy};
use risk_core::engine::{evaluate, Policy};
use risk_core::evidence::*;
use risk_core::input::*;
use risk_core::pricing::{LossAssumption, PricingPolicy};
use risk_core::stress::StressPolicy;
use risk_core::units::{Amount, Bps, Ratio};

fn policy() -> Policy {
    Policy {
        stress: StressPolicy {
            min_price_shock_bps: Bps(3_000),
            volatility_multiple_bps: Bps(15_000),
            max_price_shock_bps: Bps(9_000),
            liquidity_haircut_bps: Bps(2_000),
            execution_cost_bps: Bps(50),
            keeper_fee_bps: Bps(50),
            min_origination_coverage: Ratio::new(6, 5).unwrap(),
        },
        pricing: PricingPolicy {
            base_rate_bps: Bps(1_200),
            max_history_discount_bps: Bps(400),
            max_portfolio_surcharge_bps: Bps(800),
            min_apr_bps: Bps(500),
            max_apr_bps: Bps(4_000),
        },
        forecast: ForecastPolicy {
            min_outcomes: 5,
            max_penalty_bps: Bps(2_000),
            worst_brier_ppm: 250_000,
        },
        account_hard_cap: Amount(12_000_000_000),
        base_history_cap: Amount(10_000_000_000),
        history_growth_per_ppm: 10_000,
        max_evidence_age_seconds: 3_600,
        funding_rate_bps: Bps(450),
        operating_rate_bps: Bps(100),
        min_maintenance_coverage: Ratio::new(23, 20).unwrap(), // 1.15
        loss_assumption: Some(LossAssumption {
            pd_over_term_ppm: 17_000,
            lgd_ppm: 250_000,
        }),
    }
}

fn ev<T>(value: T, id: &str) -> Evidence<T> {
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

fn input(requested: u128, equity: u128) -> RiskInput {
    RiskInput {
        schema_version: "v1".into(),
        facility_policy: FacilityPolicy::Observed,
        requested_class: Class::PrototypeEligible,
        assessment_time: 1_000,
        scenario_id: None,
        proposal: Proposal { sequence: 5, valid_after: 900, valid_until: 4_600 },
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
                requested_principal: Amount(requested),
                term_seconds: 10 * 86_400,
                max_accepted_apr: Bps(2_000),
                action_envelope_hash: "act".into(),
                intent_hash: "intent".into(),
                intended_action: risk_core::stress::IntendedAction::Hold,
            },
        },
        account: ev(
            AccountState {
                settlement_balance: Amount(equity),
                positions: vec![],
                state_version: 3,
                outstanding_debt: Amount::ZERO,
                facility_limit: Amount(50_000_000_000),
                restricted: false,
            },
            "acct",
        ),
        market: ev(
            MarketState {
                observed_at: 1_000,
                realized_volatility_bps: Bps(2_000),
                volatility_window_seconds: 86_400,
                liquidity_depth: Amount(1_000_000_000_000),
            },
            "mkt",
        ),
        history: ev(
            HistorySummary {
                weighted_repayments_ppm: 500_000,
                resolved_defaults: 0,
                policy_breaches: 0,
                coverage_complete: true,
            },
            "hist",
        ),
        forecasts: ev(
            ForecastSummary {
                comparable_resolved: 10,
                mean_brier_ppm: 50_000,
                unresolved: 0,
                unobservable: 0,
            },
            "fc",
        ),
        portfolio: ev(
            PortfolioContext {
                strategy_id: "spot-v1".into(),
                remaining_strategy_budget: Amount(40_000_000_000),
                remaining_vault_budget: Amount(40_000_000_000),
                available_cash: Amount(40_000_000_000),
            },
            "pf",
        ),
        model_policy: ModelPolicy {
            model_id: "m1".into(),
            model_hash: "model-hash".into(),
            policy_id: "p1".into(),
            policy_hash: "policy-hash".into(),
        },
    }
}

// --- Determinism ---

/// The property the entire design rests on: same input, same pinned policy,
/// same bytes. This is what makes `docs/09` beat 8 checkable.
#[test]
fn evaluation_is_deterministic() {
    let i = input(2_000_000_000, 5_000_000_000);
    let p = policy();

    let first = evaluate(&i, &p);
    for _ in 0..20 {
        assert_eq!(evaluate(&i, &p), first, "evaluation must be reproducible");
    }

    let a = serde_json::to_value(&first).unwrap();
    let b = serde_json::to_value(&evaluate(&i, &p)).unwrap();
    assert_eq!(
        risk_core::snapshot::output_hash(&a).unwrap(),
        risk_core::snapshot::output_hash(&b).unwrap()
    );
}

// --- Approval path ---

#[test]
fn eligible_request_is_approved_exactly() {
    let a = evaluate(&input(2_000_000_000, 5_000_000_000), &policy());
    match &a.outcome {
        Outcome::ApprovedOrigination { requested_principal, approved_principal, terms, .. } => {
            assert_eq!(requested_principal, approved_principal, "exact grant");
            assert_eq!(terms.term_seconds, 10 * 86_400);
            assert!(terms.apr_bps.0 > 0);
        }
        other => panic!("expected approval, got {other:?}"),
    }
    assert_eq!(a.permission(), Permission::Allowed);
    assert!(a.is_executable());
}

/// All three mandatory scenarios accompany the decision (`docs/03`).
#[test]
fn approval_reports_three_stress_scenarios() {
    let a = evaluate(&input(2_000_000_000, 5_000_000_000), &policy());
    assert_eq!(a.stress_results.len(), 3);
}

/// Pricing runs after capacity, and its components are visible.
#[test]
fn approval_carries_priced_economics() {
    let a = evaluate(&input(2_000_000_000, 5_000_000_000), &policy());
    let terms = a.outcome.terms().expect("approved");
    assert!(terms.economics.gross_interest.0 > 0);
    assert!(terms.economics.funding_expense.0 > 0);
    // The declared assumption travels with the figure.
    assert_eq!(terms.economics.assumed_pd_ppm, Some(17_000));
    assert!(terms.economics.scenario_expected_loss.is_some());
}

// --- Denial paths ---

/// A request above capacity is denied and names what bound it — never
/// silently reduced (`docs/03`).
#[test]
fn request_above_capacity_is_denied_with_reason() {
    let a = evaluate(&input(40_000_000_000, 5_000_000_000), &policy());
    match &a.outcome {
        Outcome::Denied { requested_principal, capacity } => {
            assert_eq!(*requested_principal, Some(Amount(40_000_000_000)));
            assert!(capacity.is_some(), "the reader must see what was possible");
        }
        other => panic!("expected denial, got {other:?}"),
    }
    assert!(matches!(a.reasons[0], Reason::CapacityExceeded { .. }));
    assert_eq!(a.permission(), Permission::Blocked);
    assert!(a.outcome.terms().is_none(), "a denial carries no terms");
}

/// `docs/02`: an agent with no equity receives no credit in v0.
#[test]
fn no_equity_is_denied() {
    let a = evaluate(&input(1_000_000_000, 0), &policy());
    assert!(matches!(a.outcome, Outcome::Denied { .. }));
}

#[test]
fn restricted_account_is_denied() {
    let mut i = input(1_000_000_000, 5_000_000_000);
    if let Some(acct) = i.account.value.as_mut() {
        acct.restricted = true;
    }
    let a = evaluate(&i, &policy());
    assert!(matches!(a.reasons[0], Reason::AccountRestricted));
}

/// A rate above the borrower's ceiling denies rather than repricing.
#[test]
fn rate_above_borrower_maximum_denies() {
    let mut i = input(1_000_000_000, 5_000_000_000);
    if let Context::Origination { request } = &mut i.context {
        request.max_accepted_apr = Bps(100); // below any schedule output
    }
    let a = evaluate(&i, &policy());
    assert!(matches!(a.reasons[0], Reason::PriceExceedsBorrowerMaximum { .. }));
    assert!(a.outcome.terms().is_none());
}

// --- Unavailable paths ---

/// Missing required evidence yields Unavailable, never a zero-risk approval.
#[test]
fn missing_required_evidence_is_unavailable() {
    let mut i = input(1_000_000_000, 5_000_000_000);
    i.market.value = None;
    i.market.state = State::Missing;

    let a = evaluate(&i, &policy());
    assert!(matches!(a.outcome, Outcome::Unavailable));
    assert_eq!(a.permission(), Permission::Blocked);
    assert!(!a.is_executable());
    assert!(matches!(a.reasons[0], Reason::EvidenceBlocked(_)));
}

/// Structural failure is distinguishable from a risk denial.
#[test]
fn structural_failure_is_unavailable_not_denied() {
    let mut i = input(1_000_000_000, 5_000_000_000);
    i.proposal.sequence = 1; // not increasing
    let a = evaluate(&i, &policy());
    assert!(matches!(a.outcome, Outcome::Unavailable));
    assert!(matches!(a.reasons[0], Reason::StructurallyInvalid(_)));
}

/// Synthetic input caps the class, so nothing executes from a scenario.
#[test]
fn synthetic_input_caps_class_and_blocks_execution() {
    let mut i = input(2_000_000_000, 5_000_000_000);
    i.facility_policy = FacilityPolicy::Scenario;
    i.scenario_id = Some("run-1".into());
    i.market.origin = Origin::Synthetic;

    let a = evaluate(&i, &policy());
    assert!(a.synthetic_used);
    assert_eq!(a.authorization_class, Class::ScenarioOnly);
    assert!(!a.is_executable(), "scenario evidence cannot move real capital");
}

// --- Monitoring ---

fn monitoring(outstanding: u128, equity: u128) -> RiskInput {
    let mut i = input(0, equity);
    i.context = Context::Monitoring {
        loan: ActiveLoan {
            loan_id: "loan-1".into(),
            settlement_asset: "0xusd".into(),
            original_principal: Amount(outstanding),
            outstanding_principal: Amount(outstanding),
            apr: Bps(1_200),
            originated_at: 500,
            maturity_at: 900_000,
            in_recovery: false,
            interest_collected: Amount::ZERO,
        },
    };
    i
}

/// A healthy account keeps permission, and monitoring quotes no terms.
#[test]
fn healthy_monitoring_allows_risk() {
    let a = evaluate(&monitoring(1_000_000_000, 5_000_000_000), &policy());
    assert!(matches!(a.outcome, Outcome::MonitoringAllowed { .. }));
    assert_eq!(a.permission(), Permission::Allowed);
    assert!(a.outcome.terms().is_none(), "monitoring never carries terms");
    assert!(!a.outcome.is_executable(), "monitoring can never fund a loan");
}

/// A breached account loses permission — the continuous-underwriting beat.
#[test]
fn breached_monitoring_blocks_risk() {
    let a = evaluate(&monitoring(5_000_000_000, 1_000_000_000), &policy());
    assert!(matches!(a.outcome, Outcome::MonitoringBlocked { .. }));
    assert_eq!(a.permission(), Permission::Blocked);
}

/// Monitoring reports its scenarios too, so a reader sees why.
#[test]
fn monitoring_reports_stress_results() {
    let a = evaluate(&monitoring(1_000_000_000, 5_000_000_000), &policy());
    assert_eq!(a.stress_results.len(), 3);
}

// --- Behavioural causality ---

/// `docs/03`'s ablation requirement in miniature: holding everything else
/// constant, better history must change the terms.
#[test]
fn history_alone_changes_the_offered_rate() {
    let p = policy();

    let mut poor = input(1_000_000_000, 5_000_000_000);
    if let Some(h) = poor.history.value.as_mut() {
        h.weighted_repayments_ppm = 0;
    }
    let mut strong = input(1_000_000_000, 5_000_000_000);
    if let Some(h) = strong.history.value.as_mut() {
        h.weighted_repayments_ppm = 1_000_000;
    }

    let poor_apr = evaluate(&poor, &p).outcome.terms().unwrap().apr_bps;
    let strong_apr = evaluate(&strong, &p).outcome.terms().unwrap().apr_bps;
    assert!(strong_apr.0 < poor_apr.0, "history must move the rate: {strong_apr:?} vs {poor_apr:?}");
}

/// Insufficient forecast outcomes take the maximum penalty, which must be
/// visible in reduced capacity rather than silently ignored.
#[test]
fn insufficient_forecasts_reduce_capacity() {
    let p = policy();
    let mut sparse = input(1_000_000_000, 5_000_000_000);
    if let Some(f) = sparse.forecasts.value.as_mut() {
        f.comparable_resolved = 0;
    }

    let full = evaluate(&input(1_000_000_000, 5_000_000_000), &p);
    let thin = evaluate(&sparse, &p);

    let full_cap = match &full.outcome {
        Outcome::ApprovedOrigination { capacity, .. } => capacity.agent_capacity,
        _ => panic!("expected approval"),
    };
    let thin_cap = match &thin.outcome {
        Outcome::ApprovedOrigination { capacity, .. } => capacity.agent_capacity,
        _ => panic!("expected approval"),
    };
    assert!(thin_cap.0 <= full_cap.0, "missing forecasts must not increase capacity");
}

/// Every approval names the constraint that bound it (`docs/03`).
#[test]
fn capacity_view_names_its_binding_constraint() {
    let a = evaluate(&input(1_000_000_000, 5_000_000_000), &policy());
    if let Outcome::ApprovedOrigination { capacity, .. } = &a.outcome {
        assert!(matches!(
            capacity.agent_binding,
            BindingConstraint::StressCapacity
                | BindingConstraint::HistoryGrowthCap
                | BindingConstraint::AccountHardCap
                | BindingConstraint::ForecastCap
        ));
        assert!(capacity.score.is_some());
    } else {
        panic!("expected approval");
    }
}

/// Guards against a vacuous suite: the approval tests must sit inside a real
/// capacity boundary, not in a range so wide that nothing could be denied.
#[test]
fn approval_boundary_is_real() {
    let p = policy();
    let mut approved_max = 0u128;
    let mut denied_min = u128::MAX;

    for principal in (500_000_000u128..=12_000_000_000).step_by(500_000_000) {
        match evaluate(&input(principal, 5_000_000_000), &p).outcome {
            Outcome::ApprovedOrigination { .. } => approved_max = approved_max.max(principal),
            Outcome::Denied { .. } => denied_min = denied_min.min(principal),
            other => panic!("unexpected outcome at {principal}: {other:?}"),
        }
    }

    assert!(approved_max > 0, "some request must be approvable");
    assert!(denied_min < u128::MAX, "some request must be denied");
    assert!(
        approved_max < denied_min,
        "approval and denial must not interleave: max approved {approved_max}, min denied {denied_min}"
    );
}

#[test]
#[ignore]
fn report_capacity_boundary() {
    let p = policy();
    for principal in (1_000_000_000u128..=8_000_000_000).step_by(1_000_000_000) {
        let a = evaluate(&input(principal, 5_000_000_000), &p);
        let label = match &a.outcome {
            Outcome::ApprovedOrigination { capacity, terms, .. } => format!(
                "APPROVED  apr={}bps agent_cap={} bound_by={:?} score={:?}",
                terms.apr_bps.0, capacity.agent_capacity.0, capacity.agent_binding, capacity.score
            ),
            Outcome::Denied { capacity, .. } => format!(
                "DENIED    capacity={:?}",
                capacity.as_ref().map(|c| c.drawable_capacity.0)
            ),
            o => format!("{o:?}"),
        };
        println!("{:>13} -> {label}", principal);
    }
}

/// Deploying into a risky asset must make the stress engine bind — the
/// post-action framing of `docs/03` exists for exactly this case. Without
/// it, a suite that only ever holds cash never exercises the constraint
/// that matters most.
#[test]
fn deploying_into_risk_makes_stress_bind() {
    let p = policy();
    let mut i = input(3_000_000_000, 5_000_000_000);
    if let Context::Origination { request } = &mut i.context {
        request.intended_action = risk_core::stress::IntendedAction::Acquire {
            asset: "ETH".into(),
            price: Amount(2_000_000_000),
            price_scale: 6,
            haircut_bps: Bps(500),
        };
    }

    let a = evaluate(&i, &p);
    match &a.outcome {
        Outcome::ApprovedOrigination { capacity, .. } => {
            assert_eq!(
                capacity.agent_binding,
                BindingConstraint::StressCapacity,
                "deploying must make stress the binding constraint"
            );
        }
        Outcome::Denied { capacity, .. } => {
            let c = capacity.as_ref().expect("capacity reported on denial");
            assert_eq!(c.agent_binding, BindingConstraint::StressCapacity);
        }
        other => panic!("unexpected: {other:?}"),
    }
}

/// Holding cash is genuinely safer than deploying, so it must yield more
/// capacity at identical inputs.
#[test]
fn holding_yields_more_capacity_than_deploying() {
    let p = policy();
    let holding = evaluate(&input(1_000_000_000, 5_000_000_000), &p);

    let mut deploying_input = input(1_000_000_000, 5_000_000_000);
    if let Context::Origination { request } = &mut deploying_input.context {
        request.intended_action = risk_core::stress::IntendedAction::Acquire {
            asset: "ETH".into(),
            price: Amount(2_000_000_000),
            price_scale: 6,
            haircut_bps: Bps(500),
        };
    }
    let deploying = evaluate(&deploying_input, &p);

    let cap = |a: &risk_core::assessment::RiskAssessment| match &a.outcome {
        Outcome::ApprovedOrigination { capacity, .. } => capacity.agent_capacity.0,
        Outcome::Denied { capacity, .. } => capacity.as_ref().unwrap().agent_capacity.0,
        o => panic!("unexpected: {o:?}"),
    };
    assert!(
        cap(&deploying) < cap(&holding),
        "deploying must reduce capacity: {} vs {}",
        cap(&deploying),
        cap(&holding)
    );
}

/// Emits a runnable fixture pack: snapshot, policy, and the assessment they
/// produce. The CLI is tested against these, and the demo replays them.
/// Regenerate with:
///   cargo test --test engine -- --ignored emit_fixture_pack
#[test]
#[ignore]
fn emit_fixture_pack() {
    use std::fs;
    let root = std::path::Path::new(env!("CARGO_MANIFEST_DIR"))
        .ancestors()
        .nth(3)
        .unwrap()
        .join("fixtures/performer");
    fs::create_dir_all(&root).unwrap();

    let i = input(2_000_000_000, 5_000_000_000);
    let p = policy();
    let a = evaluate(&i, &p);

    for (name, value) in [
        ("input.json", serde_json::to_value(&i).unwrap()),
        ("policy.json", serde_json::to_value(&p).unwrap()),
        ("assessment.json", serde_json::to_value(&a).unwrap()),
    ] {
        fs::write(root.join(name), serde_json::to_string_pretty(&value).unwrap() + "\n").unwrap();
    }
}

/// Optional evidence that is Missing must be omitted, never unwrapped.
///
/// `docs/04`: an optional feature's absence is recorded and no benefit is
/// awarded for it. The evidence check returns a usable verdict in that
/// case, so evaluation must not assume every value is present -- and a
/// panic here takes the HTTP service down with it, since the server has no
/// panic boundary around evaluation.
#[test]
fn optional_missing_evidence_is_omitted_not_unwrapped() {
    let mut i = input(5_000_000_000, 5_000_000_000);
    i.forecasts.requirement = Requirement::Optional;
    i.forecasts.state = State::Missing;
    i.forecasts.value = None;

    let out = evaluate(&i, &policy());
    assert!(
        out.omitted_evidence.iter().any(|e| e == "fc"),
        "the omission must be visible in the output, got {:?}",
        out.omitted_evidence
    );
}

/// Optional evidence that is Stale must not still contribute.
///
/// Listing it in omitted_evidence while its retained value continues to
/// lower the APR would report an omission that did not happen -- the
/// explanation and the arithmetic must agree.
#[test]
fn optional_stale_history_does_not_improve_terms() {
    let fresh = input(5_000_000_000, 5_000_000_000);
    let baseline = evaluate(&fresh, &policy());

    let mut stale = input(5_000_000_000, 5_000_000_000);
    stale.history.requirement = Requirement::Optional;
    stale.history.state = State::Stale;
    // The value is retained: this is exactly the case where a stale
    // reading is still sitting in the struct.
    let out = evaluate(&stale, &policy());

    assert!(
        out.omitted_evidence.iter().any(|e| e == "hist"),
        "stale optional history must be reported omitted, got {:?}",
        out.omitted_evidence
    );

    let apr = |a: &risk_core::assessment::RiskAssessment| match &a.outcome {
        Outcome::ApprovedOrigination { terms, .. } => Some(terms.apr_bps),
        _ => None,
    };
    if let (Some(base), Some(got)) = (apr(&baseline), apr(&out)) {
        assert!(
            got >= base,
            "omitted history must not earn the discount it would have: {:?} vs {:?}",
            got,
            base
        );
    }
}

/// A snapshot declaring an unknown schema must be refused, not evaluated.
///
/// A future schema may reuse field names with different meanings, so
/// evaluating it produces a confident answer to a question that was never
/// asked. Refusing is the only honest response.
#[test]
fn unknown_schema_version_is_refused() {
    let mut i = input(5_000_000_000, 5_000_000_000);
    i.schema_version = "v999".into();

    assert!(
        matches!(
            i.validate(),
            Err(risk_core::input::InputError::UnsupportedSchemaVersion(_))
        ),
        "an unknown schema major must not validate"
    );
}

/// Permission must not outlive the evidence it rests on.
///
/// `docs/04`: authorization is only as good as the observations behind it.
/// A request may ask for a long window, but required evidence that goes
/// stale before then ends the permission first -- otherwise the chain
/// keeps honouring a grant whose basis has expired.
#[test]
fn validity_is_bounded_by_required_evidence_freshness() {
    let mut i = input(5_000_000_000, 5_000_000_000);
    // The request asks to stay valid far beyond the freshness bound.
    i.proposal.valid_until = 5_000;
    // Market data was observed just before the assessment.
    i.market.observed_at = Some(941);

    let mut p = policy();
    p.max_evidence_age_seconds = 60;

    let out = evaluate(&i, &p);

    // 941 + 60 = 1001, far short of the requested 5000.
    assert_eq!(
        out.valid_until, 1_001,
        "permission must expire with its oldest required evidence"
    );
}

/// The requested window still governs when it is the tighter of the two:
/// the bound is a ceiling, not a replacement.
#[test]
fn validity_still_honours_a_shorter_request() {
    let mut i = input(5_000_000_000, 5_000_000_000);
    i.proposal.valid_until = 1_010;
    i.market.observed_at = Some(1_000);

    let mut p = policy();
    p.max_evidence_age_seconds = 3_600;

    let out = evaluate(&i, &p);
    assert_eq!(out.valid_until, 1_010, "a shorter request must not be extended");
}

/// Incomplete history is not clean history.
///
/// `docs/04`: coverage that cannot be proven must not earn the capacity a
/// verified record would. An agent whose outcomes were never fully
/// enumerated has not demonstrated the repayment behaviour the weight
/// claims, so the earned portion cannot be taken at face value.
#[test]
fn incomplete_history_does_not_earn_full_capacity() {
    // A low base cap and a perfect forecast record, so the HISTORY cap is
    // the binding constraint. Otherwise the forecast cap dominates and the
    // comparison says nothing about history at all.
    let mut p = policy();
    p.base_history_cap = Amount(1_000_000_000);
    p.forecast.min_outcomes = 0;
    p.forecast.worst_brier_ppm = 1_000_000;

    let mut complete = input(5_000_000_000, 5_000_000_000);
    if let Some(h) = complete.history.value.as_mut() {
        h.weighted_repayments_ppm = 500_000;
        h.coverage_complete = true;
    }
    if let Some(f) = complete.forecasts.value.as_mut() {
        f.comparable_resolved = 10;
        f.mean_brier_ppm = 0;
    }
    let with_coverage = evaluate(&complete, &p);

    let mut partial = complete.clone();
    if let Some(h) = partial.history.value.as_mut() {
        h.coverage_complete = false;
    }
    let without = evaluate(&partial, &p);

    let cap = |a: &risk_core::assessment::RiskAssessment| match &a.outcome {
        Outcome::ApprovedOrigination { capacity, .. } => Some(capacity.agent_capacity),
        Outcome::Denied { capacity, .. } => capacity.as_ref().map(|c| c.agent_capacity),
        _ => None,
    };

    match (cap(&with_coverage), cap(&without)) {
        (Some(full), Some(reduced)) => assert!(
            reduced < full,
            "unproven coverage must not earn the same capacity: {reduced:?} vs {full:?}"
        ),
        other => panic!("expected capacity on both results, got {other:?}"),
    }
}

/// Past defaults and policy breaches must bear on the decision.
///
/// Reporting them and then ignoring them means the fields describe an
/// agent the engine never actually considered.
#[test]
fn resolved_defaults_reduce_capacity() {
    let mut p = policy();
    p.base_history_cap = Amount(1_000_000_000);
    p.forecast.min_outcomes = 0;
    p.forecast.worst_brier_ppm = 1_000_000;

    let mut clean = input(5_000_000_000, 5_000_000_000);
    if let Some(h) = clean.history.value.as_mut() {
        h.weighted_repayments_ppm = 500_000;
    }
    if let Some(f) = clean.forecasts.value.as_mut() {
        f.comparable_resolved = 10;
        f.mean_brier_ppm = 0;
    }
    let baseline = evaluate(&clean, &p);

    let mut scarred = clean.clone();
    if let Some(h) = scarred.history.value.as_mut() {
        h.resolved_defaults = 5;
        h.policy_breaches = 5;
    }
    let out = evaluate(&scarred, &p);

    let cap = |a: &risk_core::assessment::RiskAssessment| match &a.outcome {
        Outcome::ApprovedOrigination { capacity, .. } => Some(capacity.agent_capacity),
        Outcome::Denied { capacity, .. } => capacity.as_ref().map(|c| c.agent_capacity),
        _ => None,
    };

    match (cap(&baseline), cap(&out)) {
        (Some(clean_cap), Some(scarred_cap)) => assert!(
            scarred_cap < clean_cap,
            "defaults and breaches must reduce capacity: {scarred_cap:?} vs {clean_cap:?}"
        ),
        other => panic!("expected capacity on both results, got {other:?}"),
    }
}

/// Reported stress must describe the portfolio the draw produces.
///
/// `docs/04`: every displayed number comes from what was actually
/// assessed. Reporting the pre-draw book beside a positive grant shows a
/// debt-free account stressed against zero debt — evidence for a loan that
/// will never exist.
#[test]
fn reported_stress_describes_the_post_draw_portfolio() {
    let i = input(5_000_000_000, 5_000_000_000);
    let out = evaluate(&i, &policy());

    let granted = match &out.outcome {
        Outcome::ApprovedOrigination { approved_principal, .. } => *approved_principal,
        other => panic!("expected an approval, got {other:?}"),
    };
    assert!(granted.0 > 0, "this test needs a positive grant");

    assert!(!out.stress_results.is_empty(), "stress cases must be reported");
    for r in &out.stress_results {
        assert!(
            r.debt_with_reserved_interest.0 >= granted.0,
            "reported debt {:?} must include the granted principal {:?}",
            r.debt_with_reserved_interest,
            granted
        );
    }
}

/// A loan in recovery must not be granted permission to act.
///
/// Recovery is the keeper's process (`docs/02`, `docs/05`). An account
/// that could keep taking risk-increasing actions would be racing the
/// liquidation it is being recovered from — and coverage holding at this
/// instant says nothing about whether recovery should be interrupted.
#[test]
fn recovery_blocks_monitoring_permission() {
    // Healthy by coverage: without the recovery rule this is Allowed.
    let mut i = monitoring(1_000_000_000, 5_000_000_000);
    if let Context::Monitoring { loan } = &mut i.context {
        loan.in_recovery = true;
    }

    let a = evaluate(&i, &policy());
    assert!(
        matches!(a.outcome, Outcome::MonitoringBlocked { .. }),
        "a loan in recovery must be blocked, got {:?}",
        a.outcome
    );
    assert_eq!(a.permission(), Permission::Blocked);
}

/// `docs/11` release row: three sequential loans under one policy and
/// model must show credit compounding — the limit rises, the APR falls,
/// and the binding constraint moves off history.
///
/// This is the behavioural claim the whole system exists to make. If the
/// same inputs plus more history produce identical terms, "credit that
/// compounds" is decoration.
#[test]
fn sequential_loans_compound_credit() {
    let p = policy();

    // Same account, same market, same policy. Only demonstrated history
    // differs, which is the controlled-ablation discipline of `docs/03`.
    let run = |weighted_ppm: u64| {
        let mut i = input(2_000_000_000, 5_000_000_000);
        if let Some(h) = i.history.value.as_mut() {
            h.weighted_repayments_ppm = weighted_ppm;
            h.coverage_complete = true;
        }
        evaluate(&i, &p)
    };

    let first = run(0);
    let second = run(200_000);
    let third = run(600_000);

    let terms = |a: &risk_core::assessment::RiskAssessment| match &a.outcome {
        Outcome::ApprovedOrigination { terms, capacity, .. } => {
            Some((terms.apr_bps, capacity.agent_capacity, capacity.agent_binding))
        }
        other => panic!("expected approval, got {other:?}"),
    };

    let (apr1, cap1, bind1) = terms(&first).unwrap();
    let (apr2, cap2, _) = terms(&second).unwrap();
    let (apr3, cap3, bind3) = terms(&third).unwrap();

    // Capacity rises while history is what limits it, then PLATEAUS when
    // another cap takes over. The plateau is the point, not a defect: it
    // is why the binding constraint must be reported beside the number.
    assert!(cap2 > cap1, "capacity must rise while history binds: {cap1:?} -> {cap2:?}");
    assert!(cap3 >= cap2, "capacity must never fall as history accumulates");

    // APR falls throughout: pricing keeps responding to demonstrated
    // history after capacity has stopped.
    assert!(apr2 < apr1 && apr3 < apr2, "APR must fall: {apr1:?} {apr2:?} {apr3:?}");

    // The switch `docs/09` beat 4b exists to show.
    assert_eq!(bind1, BindingConstraint::HistoryGrowthCap, "history binds first");
    assert_eq!(bind3, BindingConstraint::ForecastCap, "another cap takes over");
}
