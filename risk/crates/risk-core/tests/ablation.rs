//! Controlled ablation — the release requirement in `docs/03`.
//!
//! The point is not that three agents get different answers; that could be
//! coincidence. It is that holding everything else constant and varying
//! **one** input changes the terms by a stated amount, and that the binding
//! constraint is named.
//!
//! `docs/03` warns of a specific trap: if stress capacity clamps every
//! scenario, behaviour never binds and the demo shows nothing. So the
//! fixtures here are calibrated for the opposite, and a test asserts that
//! calibration rather than assuming it.

use risk_core::assessment::Outcome;
use risk_core::capacity::{BindingConstraint, ForecastPolicy};
use risk_core::engine::{evaluate, Policy};
use risk_core::evidence::*;
use risk_core::input::*;
use risk_core::pricing::{LossAssumption, PricingPolicy};
use risk_core::stress::{IntendedAction, StressPolicy};
use risk_core::units::{Amount, Bps, Ratio};

/// Canonical content hash of the fixture policy.
fn policy_hash() -> String {
    hash_of(&policy())
}

/// Canonical content hash of any policy.
///
/// A test that VARIES the policy must re-declare this on its input, or
/// the engine refuses the result: an executable answer requires the
/// declared label to be the hash of the policy actually supplied.
fn hash_of(p: &Policy) -> String {
    let value = serde_json::to_value(p).expect("serialize policy");
    risk_core::canonical::hash_hex(&value).expect("hash policy")
}

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
        // Calibrated so history and forecast can bind: the hard cap sits
        // below what a well-collateralised account could stress-support,
        // leaving room for behavioural constraints to be the limiting one.
        account_hard_cap: Amount(12_000_000_000),
        base_history_cap: Amount(4_000_000_000),
        history_growth_per_ppm: 8_000,
        max_evidence_age_seconds: 3_600,
        funding_rate_bps: Bps(450),
        operating_rate_bps: Bps(100),
        min_maintenance_coverage: Ratio::new(23, 20).unwrap(),
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
        source: "ablation".into(),
        observed_at: Some(1_000),
        retrieved_at: 1_000,
        coverage: Coverage::Complete,
        evidence_id: id.into(),
        content_hash: "h".into(),
        parent_evidence_ids: vec![],
        scenario_id: None,
    }
}

/// The baseline every run varies from. One field at a time changes.
fn baseline() -> RiskInput {
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
                identity_registry_chain_id: 31337,
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
                request_id: "req".into(),
                settlement_asset: "0xusd".into(),
                requested_principal: Amount(1_000_000_000),
                term_seconds: 10 * 86_400,
                max_accepted_apr: Bps(2_500),
                action_envelope_hash: "act".into(),
                intent_hash: "intent".into(),
                intended_action: IntendedAction::Hold,
            },
        },
        account: ev(
            AccountState {
                settlement_balance: Amount(20_000_000_000),
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
                weighted_repayments_ppm: 0,
                resolved_defaults: 0,
                policy_breaches: 0,
                coverage_complete: true,
            },
            "hist",
        ),
        forecasts: ev(
            ForecastSummary {
                comparable_resolved: 10,
                mean_brier_ppm: 0,
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
            model_hash: risk_core::model_hash(),
            policy_id: "p1".into(),
            // The REAL content hash of policy(). An executable result
            // requires the declared label to be the hash of the policy
            // actually supplied, so a symbolic one would make every
            // fixture Unavailable.
            policy_hash: policy_hash(),
        },
    }
}

/// Capacity, binding constraint, APR, and score for one run.
struct Observed {
    capacity: u128,
    binding: BindingConstraint,
    apr_bps: u32,
    score: Option<u32>,
}

fn observe(input: &RiskInput, policy: &Policy) -> Observed {
    let a = evaluate(input, policy);
    match &a.outcome {
        Outcome::ApprovedOrigination {
            capacity, terms, ..
        } => Observed {
            capacity: capacity.agent_capacity.0,
            binding: capacity.agent_binding,
            apr_bps: terms.apr_bps.0,
            score: capacity.score,
        },
        Outcome::Denied {
            capacity: Some(c), ..
        } => Observed {
            capacity: c.agent_capacity.0,
            binding: c.agent_binding,
            apr_bps: 0,
            score: c.score,
        },
        other => panic!("expected a priced outcome, got {other:?}"),
    }
}

// --- The calibration the whole exercise depends on ---

/// `docs/03`: if stress capacity clamps every scenario, the demo cannot
/// show behaviour mattering and the fixtures are wrong. This asserts the
/// fixtures leave room for behavioural constraints to bind.
#[test]
fn fixtures_leave_room_for_behaviour_to_bind() {
    let p = policy();
    let base = observe(&baseline(), &p);
    assert_ne!(
        base.binding,
        BindingConstraint::StressCapacity,
        "stress clamps the baseline, so history and forecast can never bind — \
         the fixtures are miscalibrated, not the engine"
    );
}

// --- A→B: history alone ---

/// Vary only eligible repayment history. Capacity must rise and the rate
/// must fall, with the change attributable to nothing else.
#[test]
fn history_alone_changes_capacity_and_rate() {
    let p = policy();

    let mut strong = baseline();
    if let Some(h) = strong.history.value.as_mut() {
        h.weighted_repayments_ppm = 1_000_000;
    }

    let a = observe(&baseline(), &p);
    let b = observe(&strong, &p);

    assert!(
        b.capacity > a.capacity,
        "history must raise capacity: {} -> {}",
        a.capacity,
        b.capacity
    );
    assert!(
        b.apr_bps < a.apr_bps,
        "history must lower the rate: {} -> {} bps",
        a.apr_bps,
        b.apr_bps
    );
    assert_eq!(
        a.binding,
        BindingConstraint::HistoryGrowthCap,
        "history must be what binds before it improves"
    );
}

/// The improvement is monotone: more history never yields less capacity.
#[test]
fn history_improvement_is_monotone() {
    let p = policy();
    let mut previous = 0u128;
    for ppm in (0..=1_000_000).step_by(100_000) {
        let mut input = baseline();
        if let Some(h) = input.history.value.as_mut() {
            h.weighted_repayments_ppm = ppm;
        }
        let observed = observe(&input, &p);
        assert!(
            observed.capacity >= previous,
            "capacity fell as history improved at {ppm} ppm"
        );
        previous = observed.capacity;
    }
}

// --- A→C: forecast evidence alone ---

/// Vary only forecast quality. `docs/03`: insufficient eligible outcomes
/// take the maximum penalty, so removing them must cost capacity.
#[test]
fn forecast_evidence_alone_changes_capacity() {
    let p = policy();

    // Give history enough room that forecast is free to bind.
    let mut good = baseline();
    if let Some(h) = good.history.value.as_mut() {
        h.weighted_repayments_ppm = 1_000_000;
    }

    let mut sparse = good.clone();
    if let Some(f) = sparse.forecasts.value.as_mut() {
        f.comparable_resolved = 0;
    }

    let with_forecasts = observe(&good, &p);
    let without = observe(&sparse, &p);

    assert!(
        without.capacity < with_forecasts.capacity,
        "absent forecast outcomes must cost capacity: {} -> {}",
        with_forecasts.capacity,
        without.capacity
    );
    assert_eq!(
        without.binding,
        BindingConstraint::ForecastCap,
        "the forecast cap must be named as what bound the result"
    );
}

/// A worse Brier score costs capacity even with enough samples.
#[test]
fn forecast_quality_is_monotone() {
    let p = policy();
    let mut previous = u128::MAX;
    for brier in (0..=250_000).step_by(50_000) {
        let mut input = baseline();
        if let Some(h) = input.history.value.as_mut() {
            h.weighted_repayments_ppm = 1_000_000;
        }
        if let Some(f) = input.forecasts.value.as_mut() {
            f.mean_brier_ppm = brier;
        }
        let observed = observe(&input, &p);
        assert!(
            observed.capacity <= previous,
            "capacity rose as forecasts worsened at {brier} ppm"
        );
        previous = observed.capacity;
    }
}

// --- A→D: hard constraints override behaviour ---

/// `docs/03`'s central claim: a perfect record does not survive a dangerous
/// book. Raise portfolio risk and stress must become the binding
/// constraint, overriding history and forecast alike.
#[test]
fn portfolio_risk_overrides_perfect_behaviour() {
    let p = policy();

    let mut concentrated = baseline();
    if let Some(h) = concentrated.history.value.as_mut() {
        h.weighted_repayments_ppm = 1_000_000; // flawless record
    }
    if let Some(a) = concentrated.account.value.as_mut() {
        // Thin equity, already levered, and what remains sits in a single
        // shocked asset. This is the shape `docs/07`'s concentrated agent
        // has: a flawless record against a dangerous book.
        a.settlement_balance = Amount(50_000_000);
        a.outstanding_debt = Amount(9_000_000_000);
        a.positions = vec![Position {
            asset: "ETH".into(),
            amount: Amount(2_000_000),
            price: Amount(2_000_000_000),
            price_scale: 6,
            haircut_bps: Bps(500),
        }];
    }
    if let Context::Origination { request } = &mut concentrated.context {
        request.requested_principal = Amount(8_000_000_000);
        // Deploying into the same asset compounds the concentration, which
        // is what the post-action framing exists to catch.
        request.intended_action = IntendedAction::Acquire {
            asset: "ETH".into(),
            price: Amount(2_000_000_000),
            price_scale: 6,
            deploy_bps: Bps(10_000),
            haircut_bps: Bps(500),
        };
    }

    let observed = observe(&concentrated, &p);
    assert_eq!(
        observed.binding,
        BindingConstraint::StressCapacity,
        "portfolio risk must override a flawless record"
    );

    // And the outcome is a denial, not a quietly reduced grant.
    let a = evaluate(&concentrated, &p);
    assert!(
        matches!(a.outcome, Outcome::Denied { .. }),
        "a request beyond stressed capacity is denied, never shrunk"
    );
}

// --- Attribution ---

/// Every run names its binding constraint, which is what makes the
/// comparison an attribution rather than an anecdote.
#[test]
fn every_run_names_its_binding_constraint() {
    let p = policy();
    let mut seen = std::collections::HashSet::new();

    // Baseline: history binds.
    seen.insert(observe(&baseline(), &p).binding);

    // Strong history, sparse forecasts: forecast binds.
    let mut sparse = baseline();
    if let Some(h) = sparse.history.value.as_mut() {
        h.weighted_repayments_ppm = 1_000_000;
    }
    if let Some(f) = sparse.forecasts.value.as_mut() {
        f.comparable_resolved = 0;
    }
    seen.insert(observe(&sparse, &p).binding);

    // Concentrated book: stress binds. Thin equity, heavy existing debt,
    // and what remains sits in one shocked asset.
    let mut risky = baseline();
    if let Some(h) = risky.history.value.as_mut() {
        h.weighted_repayments_ppm = 1_000_000;
    }
    if let Some(a) = risky.account.value.as_mut() {
        a.settlement_balance = Amount(50_000_000);
        a.outstanding_debt = Amount(9_000_000_000);
        a.positions = vec![Position {
            asset: "ETH".into(),
            amount: Amount(2_000_000),
            price: Amount(2_000_000_000),
            price_scale: 6,
            haircut_bps: Bps(500),
        }];
    }
    seen.insert(observe(&risky, &p).binding);

    assert!(
        seen.len() >= 3,
        "three different constraints must be demonstrable, saw {seen:?}"
    );
}

/// The score tracks agent capacity, so a behavioural change is visible in
/// the number the interface leads with.
#[test]
fn score_moves_with_behaviour() {
    let p = policy();

    let mut strong = baseline();
    if let Some(h) = strong.history.value.as_mut() {
        h.weighted_repayments_ppm = 1_000_000;
    }

    let a = observe(&baseline(), &p);
    let b = observe(&strong, &p);

    let (before, after) = (a.score.expect("score"), b.score.expect("score"));
    assert!(
        after > before,
        "score must rise with history: {before} -> {after}"
    );
}

/// `docs/11` release row: a favourable history plus a current-risk denial.
///
/// The concentrated account has a PERFECT repayment record — 1,000,000 ppm
/// — and still gets nothing, because its existing book cannot survive the
/// stress scenarios. Behaviour opens a facility; it does not override
/// portfolio risk, and the binding constraint must say so by name.
#[test]
fn concentration_overrides_perfect_history() {
    let p = policy();
    let mut input = baseline();
    if let Some(h) = input.history.value.as_mut() {
        h.weighted_repayments_ppm = 1_000_000;
    }
    if let Some(a) = input.account.value.as_mut() {
        a.settlement_balance = Amount(50_000_000);
        a.outstanding_debt = Amount(9_000_000_000);
        a.positions = vec![Position {
            asset: "ETH".into(),
            amount: Amount(2_000_000),
            price: Amount(2_000_000_000),
            price_scale: 6,
            haircut_bps: Bps(500),
        }];
    }
    if let Context::Origination { request } = &mut input.context {
        request.intended_action = IntendedAction::Acquire {
            asset: "ETH".into(),
            price: Amount(2_000_000_000),
            price_scale: 6,
            deploy_bps: Bps(10_000),
            haircut_bps: Bps(500),
        };
    }
    let o = observe(&input, &p);

    assert_eq!(
        o.capacity, 0,
        "a book that cannot survive stress earns no capacity"
    );
    assert_eq!(
        o.binding,
        BindingConstraint::StressCapacity,
        "the denial must name current portfolio risk, not history"
    );

    // The control: the SAME history on an unconcentrated account is
    // approved. Without it this test would pass for any account at all,
    // and would not show concentration doing the work.
    let mut clean = baseline();
    if let Some(h) = clean.history.value.as_mut() {
        h.weighted_repayments_ppm = 1_000_000;
    }
    let c = observe(&clean, &p);
    assert!(
        c.capacity > 0,
        "the same history without concentration must still earn capacity"
    );
}

// --- the comparison the demo opens on ---

/// Emit three agent profiles assessed by the real engine.
///
/// `docs/09` beat 1: "Introduce three accounts and the financing problem —
/// different histories and exposures, clear scenario labels." Beat 5 adds
/// the sharpest of the three: an account with a GOOD record denied by its
/// CURRENT portfolio, which is `docs/03`'s central claim.
///
/// That claim was proven here, in Rust, and nowhere a reader could see it.
/// This writes the three assessments to a fixture the interface renders,
/// so the comparison shows real engine output rather than a retelling.
///
/// Assessed under the SHIPPED policy, not this file's local one: the
/// screen sits beside `docs/11`'s published figures, and a comparison
/// computed under different parameters would invite exactly the
/// apples-to-oranges reading the ablation discipline exists to prevent.
///
/// Regenerate with:
///   cargo test --test ablation -- --ignored emit_comparison
#[test]
#[ignore]
fn emit_comparison() {
    let p = shipped_policy();
    let declared = hash_of(&p);

    // One variable at a time, from one baseline. That is the whole
    // discipline: three accounts that differ in several ways at once
    // would show a difference without attributing it.
    let profile = |name: &str, blurb: &str, build: &dyn Fn(&mut RiskInput)| {
        let mut input = baseline();
        input.model_policy.policy_hash = declared.clone();
        build(&mut input);
        let assessment = evaluate(&input, &p);
        serde_json::json!({
            "name": name,
            "blurb": blurb,
            "input": input,
            "assessment": assessment,
        })
    };

    let profiles = vec![
        profile(
            "Unproven",
            "No demonstrated record. Capacity is bounded by history, \
             because there is none to lend against.",
            &|i: &mut RiskInput| {
                if let Some(h) = i.history.value.as_mut() {
                    h.weighted_repayments_ppm = 0;
                }
            },
        ),
        profile(
            "Proven",
            "A strong repayment record against a clean book. History \
             opens the facility.",
            &|i: &mut RiskInput| {
                if let Some(h) = i.history.value.as_mut() {
                    h.weighted_repayments_ppm = 1_000_000;
                }
            },
        ),
        profile(
            "Concentrated",
            "The same flawless record, against a dangerous book: thin \
             equity, already levered, the rest in one shocked asset. \
             Stress overrides the record.",
            &|i: &mut RiskInput| {
                if let Some(h) = i.history.value.as_mut() {
                    h.weighted_repayments_ppm = 1_000_000;
                }
                if let Some(a) = i.account.value.as_mut() {
                    a.settlement_balance = Amount(50_000_000);
                    a.outstanding_debt = Amount(9_000_000_000);
                    a.positions = vec![Position {
                        asset: "ETH".into(),
                        amount: Amount(2_000_000),
                        price: Amount(2_000_000_000),
                        price_scale: 6,
                        haircut_bps: Bps(500),
                    }];
                }
                if let Context::Origination { request } = &mut i.context {
                    request.requested_principal = Amount(8_000_000_000);
                    request.intended_action = IntendedAction::Acquire {
                        asset: "ETH".into(),
                        price: Amount(2_000_000_000),
                        price_scale: 6,
                        deploy_bps: Bps(10_000),
                        haircut_bps: Bps(500),
                    };
                }
            },
        ),
    ];

    let doc = serde_json::json!({
        "_comment": "docs/09 beat 1 and 5. Three profiles from ONE baseline, \
                     one variable at a time, assessed by the real engine under \
                     config/engine-policy.json. Regenerate with: cargo test \
                     --test ablation -- --ignored emit_comparison",
        "policy_hash": declared,
        "model_hash": risk_core::model_hash(),
        "profiles": profiles,
    });

    let path = std::path::Path::new(env!("CARGO_MANIFEST_DIR"))
        .ancestors()
        .nth(3)
        .expect("repo root")
        .join("fixtures/demo/comparison.json");
    std::fs::write(&path, serde_json::to_string_pretty(&doc).unwrap() + "\n")
        .unwrap_or_else(|e| panic!("write {}: {e}", path.display()));
}

/// The policy the demos actually run, read from disk rather than mirrored.
fn shipped_policy() -> Policy {
    let path = std::path::Path::new(env!("CARGO_MANIFEST_DIR"))
        .ancestors()
        .nth(3)
        .expect("repo root")
        .join("config/engine-policy.json");
    let raw =
        std::fs::read_to_string(&path).unwrap_or_else(|e| panic!("read {}: {e}", path.display()));
    serde_json::from_str(&raw).unwrap_or_else(|e| panic!("parse {}: {e}", path.display()))
}

/// The committed comparison fixture must still be what the engine says.
///
/// `docs/09` beat 1 opens the demo on these three profiles, and the
/// interface renders the fixture rather than recomputing. A fixture that
/// drifted from the engine would put figures on screen that the engine no
/// longer produces — the precise failure the replay screen exists to rule
/// out, one layer up.
///
/// Regenerate with: cargo test --test ablation -- --ignored emit_comparison
#[test]
fn comparison_fixture_matches_the_engine() {
    let path = std::path::Path::new(env!("CARGO_MANIFEST_DIR"))
        .ancestors()
        .nth(3)
        .expect("repo root")
        .join("fixtures/demo/comparison.json");
    let raw = match std::fs::read_to_string(&path) {
        Ok(r) => r,
        // Not generated in this checkout: the emitter is ignored by
        // default, so absence is not a failure.
        Err(_) => return,
    };
    let doc: serde_json::Value = serde_json::from_str(&raw).expect("parse fixture");

    let p = shipped_policy();
    assert_eq!(
        doc["policy_hash"].as_str().unwrap(),
        hash_of(&p),
        "fixture was emitted under a different policy; regenerate it"
    );

    for profile in doc["profiles"].as_array().expect("profiles") {
        let input: RiskInput =
            serde_json::from_value(profile["input"].clone()).expect("parse input");
        let replayed = evaluate(&input, &p);
        let retained: risk_core::assessment::RiskAssessment =
            serde_json::from_value(profile["assessment"].clone()).expect("parse assessment");

        let diffs = risk_core::replay::diff(&retained, &replayed);
        assert!(
            diffs.is_empty(),
            "profile {} no longer reproduces: {:?}",
            profile["name"],
            diffs
        );
    }
}
