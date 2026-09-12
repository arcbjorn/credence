//! Capacity composition, the derived score, and the grant rule.

use risk_core::capacity::*;
use risk_core::units::{Amount, Bps};

fn agent(stress: u128, history: u128, hard: u128, forecast: u128) -> AgentInputs {
    AgentInputs {
        stress_capacity: Amount(stress),
        history_growth_cap: Amount(history),
        account_hard_cap: Amount(hard),
        forecast_cap: Amount(forecast),
    }
}

fn lender(headroom: u128, strategy: u128, vault: u128, cash: u128) -> LenderInputs {
    LenderInputs {
        debt_headroom_capacity: Amount(headroom),
        remaining_strategy_budget: Amount(strategy),
        remaining_vault_budget: Amount(vault),
        available_cash: Amount(cash),
    }
}

// --- The score defect this design exists to prevent ---

/// The exact counterexample from `docs/03`: at a 12,000 hard cap and 9,000
/// agent capacity the score is 750. If vault cash falls to 4,500, the same
/// agent must STILL score 750 — nothing about the borrower changed.
#[test]
fn vault_scarcity_does_not_move_the_score() {
    let a = agent(9_000, 12_000, 12_000, 12_000);

    let flush = compose(a, lender(50_000, 50_000, 50_000, 50_000));
    let drained = compose(a, lender(50_000, 50_000, 50_000, 4_500));

    assert_eq!(flush.agent_capacity, Amount(9_000));
    assert_eq!(drained.agent_capacity, Amount(9_000), "borrower capacity is unchanged");

    let s1 = score(flush.agent_capacity, a.account_hard_cap).unwrap();
    let s2 = score(drained.agent_capacity, a.account_hard_cap).unwrap();
    assert_eq!(s1, 750);
    assert_eq!(s2, 750, "a lender-side constraint must not move a borrower score");

    // The scarcity shows up where it belongs.
    assert_eq!(drained.drawable_capacity, Amount(4_500));
    assert_eq!(drained.drawable_binding, BindingConstraint::AvailableCash);
}

#[test]
fn score_is_monotone_and_bounded() {
    let cap = Amount(12_000);
    assert_eq!(score(Amount::ZERO, cap), Some(300));
    assert_eq!(score(Amount(6_000), cap), Some(600));
    assert_eq!(score(cap, cap), Some(900));
    // Capacity above the cap cannot push the score past its ceiling.
    assert_eq!(score(Amount(99_000), cap), Some(900));
}

/// `docs/03`: omit the score when borrower capacity cannot be evaluated.
#[test]
fn zero_hard_cap_yields_no_score() {
    assert_eq!(score(Amount(5_000), Amount::ZERO), None);
}

// --- Binding constraint reporting ---

#[test]
fn each_constraint_can_bind_and_is_named() {
    let l = lender(99_000, 99_000, 99_000, 99_000);
    assert_eq!(
        compose(agent(1_000, 9_000, 9_000, 9_000), l).agent_binding,
        BindingConstraint::StressCapacity
    );
    assert_eq!(
        compose(agent(9_000, 1_000, 9_000, 9_000), l).agent_binding,
        BindingConstraint::HistoryGrowthCap
    );
    assert_eq!(
        compose(agent(9_000, 9_000, 1_000, 9_000), l).agent_binding,
        BindingConstraint::AccountHardCap
    );
    assert_eq!(
        compose(agent(9_000, 9_000, 9_000, 1_000), l).agent_binding,
        BindingConstraint::ForecastCap
    );
}

#[test]
fn lender_constraints_bind_drawable_only() {
    let a = agent(9_000, 9_000, 12_000, 12_000);
    for (l, expected) in [
        (lender(500, 99_000, 99_000, 99_000), BindingConstraint::DebtHeadroom),
        (lender(99_000, 500, 99_000, 99_000), BindingConstraint::StrategyBudget),
        (lender(99_000, 99_000, 500, 99_000), BindingConstraint::VaultBudget),
        (lender(99_000, 99_000, 99_000, 500), BindingConstraint::AvailableCash),
    ] {
        let r = compose(a, l);
        assert_eq!(r.drawable_binding, expected);
        assert_eq!(r.agent_capacity, Amount(9_000), "agent side is untouched");
    }
}

/// Ties report the earlier constraint in the documented order, so identical
/// inputs always name the same constraint.
#[test]
fn ties_report_deterministically() {
    let r = compose(agent(5_000, 5_000, 5_000, 5_000), lender(99_000, 99_000, 99_000, 99_000));
    assert_eq!(r.agent_binding, BindingConstraint::StressCapacity);
}

/// drawable never exceeds agent capacity.
#[test]
fn drawable_never_exceeds_agent_capacity() {
    let r = compose(agent(3_000, 9_000, 9_000, 9_000), lender(99_000, 99_000, 99_000, 99_000));
    assert_eq!(r.drawable_capacity, Amount(3_000));
    assert!(r.drawable_capacity.0 <= r.agent_capacity.0);
}

// --- Forecast penalty ---

fn fpolicy() -> ForecastPolicy {
    ForecastPolicy {
        min_outcomes: 5,
        max_penalty_bps: Bps(2_000),
        worst_brier_ppm: 250_000, // 0.25 = maximum hedging
    }
}

/// `docs/03`: insufficient eligible outcomes take the MAXIMUM penalty. No
/// forecast credit is earned merely because observations are absent.
#[test]
fn insufficient_outcomes_take_maximum_penalty() {
    assert_eq!(forecast_penalty_bps(&fpolicy(), 0, 0), Bps(2_000));
    assert_eq!(forecast_penalty_bps(&fpolicy(), 4, 0), Bps(2_000));
}

/// A perfect record with enough samples earns no penalty.
#[test]
fn good_forecasts_remove_the_penalty() {
    assert_eq!(forecast_penalty_bps(&fpolicy(), 10, 0), Bps::ZERO);
}

/// Monotone: higher Brier loss can never reduce the penalty.
#[test]
fn penalty_is_monotone_in_brier_loss() {
    let p = fpolicy();
    let mut previous = 0u32;
    for loss in (0..=300_000).step_by(25_000) {
        let penalty = forecast_penalty_bps(&p, 10, loss).0;
        assert!(penalty >= previous, "penalty fell as loss rose: {penalty} < {previous}");
        previous = penalty;
    }
    assert_eq!(previous, 2_000, "loss beyond the worst case is clamped");
}

#[test]
fn forecast_cap_applies_the_penalty() {
    assert_eq!(forecast_cap(Amount(12_000), Bps(2_000)), Amount(9_600));
    assert_eq!(forecast_cap(Amount(12_000), Bps::ZERO), Amount(12_000));
}

/// The penalty affects only the forecast cap — never APR, never the stress
/// floor. A penalised agent can still borrow within its stressed capacity.
#[test]
fn penalty_binds_through_forecast_cap_only() {
    let hard = Amount(12_000);
    let penalised = forecast_cap(hard, Bps(2_000));
    let r = compose(
        agent(5_000, 12_000, hard.0, penalised.0),
        lender(99_000, 99_000, 99_000, 99_000),
    );
    // Stress still binds below the penalised cap, so the agent is not
    // blocked by forecast quality alone.
    assert_eq!(r.agent_capacity, Amount(5_000));
    assert_eq!(r.agent_binding, BindingConstraint::StressCapacity);
}

// --- Grant rule ---

/// `docs/03`: a request for 1,000 with capacity 5,000 grants exactly 1,000.
#[test]
fn request_within_capacity_is_granted_exactly() {
    let c = compose(agent(5_000, 9_000, 9_000, 9_000), lender(99_000, 99_000, 99_000, 99_000));
    assert_eq!(
        decide_grant(Amount(1_000), &c),
        GrantDecision::Approve { principal: Amount(1_000) }
    );
}

/// A request for 6,000 against 5,000 capacity is denied — never shrunk.
/// There is no implicit partial fill.
#[test]
fn request_above_capacity_is_denied_not_reduced() {
    let c = compose(agent(5_000, 9_000, 9_000, 9_000), lender(99_000, 99_000, 99_000, 99_000));
    assert_eq!(
        decide_grant(Amount(6_000), &c),
        GrantDecision::Deny {
            limiting: BindingConstraint::StressCapacity,
            capacity: Amount(5_000),
        }
    );
}

#[test]
fn exact_capacity_is_grantable() {
    let c = compose(agent(5_000, 9_000, 9_000, 9_000), lender(99_000, 99_000, 99_000, 99_000));
    assert!(matches!(decide_grant(Amount(5_000), &c), GrantDecision::Approve { .. }));
}

#[test]
fn zero_request_is_denied() {
    let c = compose(agent(5_000, 9_000, 9_000, 9_000), lender(99_000, 99_000, 99_000, 99_000));
    assert_eq!(decide_grant(Amount::ZERO, &c), GrantDecision::DenyZeroRequest);
}

/// The denial names the lender-side constraint when that is what bound.
#[test]
fn denial_names_the_lender_constraint() {
    let c = compose(agent(9_000, 9_000, 12_000, 12_000), lender(99_000, 99_000, 99_000, 2_000));
    assert_eq!(
        decide_grant(Amount(5_000), &c),
        GrantDecision::Deny {
            limiting: BindingConstraint::AvailableCash,
            capacity: Amount(2_000),
        }
    );
}
