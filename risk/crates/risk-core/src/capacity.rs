//! Capacity composition and the derived score — steps 6 to 7 of `docs/03`.
//!
//! The split between `agent_capacity` and `drawable_capacity` is not
//! cosmetic. Scoring a liquidity-constrained amount would let a
//! borrower-quality number move when nothing about the borrower changed:
//! at a 12,000 hard cap and 9,000 capacity the score is 750, but if vault
//! cash fell to 4,500 the same agent would score 525.
//!
//! So `agent_capacity` excludes **every lender-side term**, and vault
//! scarcity appears only in `drawable_capacity`.

use serde::{Deserialize, Serialize};

use crate::units::{Amount, Bps};

/// Versioned forecast policy (`docs/03`).
#[derive(Clone, Copy, Debug, PartialEq, Eq, Serialize, Deserialize)]
pub struct ForecastPolicy {
    /// Eligible resolved observations required to use the scoring table.
    pub min_outcomes: u32,
    /// Penalty ceiling. Strictly between 0 and 10_000.
    pub max_penalty_bps: Bps,
    /// Mean Brier loss (ppm) at or above which the maximum penalty applies.
    pub worst_brier_ppm: u64,
}

/// Forecast penalty in bps of the hard cap.
///
/// Insufficient eligible outcomes take the **maximum** penalty, including
/// when outcomes are missing or unobservable. No forecast credit is earned
/// merely because observations are absent (`docs/03`).
///
/// Monotone by construction: higher loss can never reduce the penalty.
pub fn forecast_penalty_bps(
    policy: &ForecastPolicy,
    comparable_resolved: u32,
    mean_brier_ppm: u64,
) -> Bps {
    if comparable_resolved < policy.min_outcomes {
        return policy.max_penalty_bps;
    }
    if policy.worst_brier_ppm == 0 {
        return Bps::ZERO;
    }
    let clamped = mean_brier_ppm.min(policy.worst_brier_ppm);
    let scaled = (policy.max_penalty_bps.0 as u64)
        .saturating_mul(clamped)
        / policy.worst_brier_ppm;
    Bps(scaled as u32)
}

/// `forecast_cap = floor(hard_cap * (10_000 - penalty) / 10_000)`
pub fn forecast_cap(account_hard_cap: Amount, penalty_bps: Bps) -> Amount {
    let remaining = 10_000u128.saturating_sub(penalty_bps.0 as u128);
    Amount(account_hard_cap.0.saturating_mul(remaining) / 10_000)
}

/// Repayment weight an agent has actually demonstrated.
///
/// Three corrections to the raw weight, all of them `docs/04` requirements
/// that a reported-but-unused field would quietly drop:
///
///   * **Unproven coverage halves it.** History the gateway could not
///     fully enumerate is not a clean record; it is an unknown one, and an
///     unknown record must not earn what a verified one does.
///   * **Resolved defaults and policy breaches subtract.** An agent that
///     defaulted and repaid has demonstrated something real about its risk,
///     and reporting that outcome while ignoring it describes an agent the
///     engine never considered.
///   * The result floors at zero rather than wrapping.
///
/// Deliberately coarse. `docs/12` records that the precise penalty for a
/// past resolved default needs a published policy; until then the rule is
/// visible, conservative, and stated rather than absent.
pub fn earned_repayment_ppm(
    weighted_repayments_ppm: u64,
    coverage_complete: bool,
    resolved_defaults: u32,
    policy_breaches: u32,
) -> u64 {
    let mut earned = weighted_repayments_ppm;
    if !coverage_complete {
        earned /= 2;
    }
    // Each adverse outcome costs a fixed slice of demonstrated history.
    const PENALTY_PER_OUTCOME_PPM: u64 = 50_000;
    let adverse = (resolved_defaults as u64).saturating_add(policy_breaches as u64);
    earned.saturating_sub(adverse.saturating_mul(PENALTY_PER_OUTCOME_PPM))
}

/// Which constraint bound the result. Always reported beside the number:
/// a capacity without its binding constraint hides *why* it is what it is.
#[derive(Clone, Copy, Debug, PartialEq, Eq, Hash, Serialize, Deserialize)]
pub enum BindingConstraint {
    StressCapacity,
    HistoryGrowthCap,
    AccountHardCap,
    ForecastCap,
    DebtHeadroom,
    StrategyBudget,
    VaultBudget,
    AvailableCash,
}

/// Borrower-side inputs. No lender-side term appears here.
#[derive(Clone, Copy, Debug, PartialEq, Eq, Serialize, Deserialize)]
pub struct AgentInputs {
    pub stress_capacity: Amount,
    pub history_growth_cap: Amount,
    pub account_hard_cap: Amount,
    pub forecast_cap: Amount,
}

/// Lender-side and facility limits.
#[derive(Clone, Copy, Debug, PartialEq, Eq, Serialize, Deserialize)]
pub struct LenderInputs {
    pub debt_headroom_capacity: Amount,
    pub remaining_strategy_budget: Amount,
    pub remaining_vault_budget: Amount,
    pub available_cash: Amount,
}

#[derive(Clone, Debug, PartialEq, Eq, Serialize, Deserialize)]
pub struct CapacityResult {
    pub agent_capacity: Amount,
    pub drawable_capacity: Amount,
    pub agent_binding: BindingConstraint,
    pub drawable_binding: BindingConstraint,
}

/// Compose both capacities and name what bound each.
///
/// When several constraints tie, the earlier one in `docs/03`'s listed order
/// is reported, so the same inputs always name the same constraint.
pub fn compose(agent: AgentInputs, lender: LenderInputs) -> CapacityResult {
    let agent_pairs = [
        (agent.stress_capacity, BindingConstraint::StressCapacity),
        (agent.history_growth_cap, BindingConstraint::HistoryGrowthCap),
        (agent.account_hard_cap, BindingConstraint::AccountHardCap),
        (agent.forecast_cap, BindingConstraint::ForecastCap),
    ];
    let (agent_capacity, agent_binding) = min_with_reason(&agent_pairs);

    let drawable_pairs = [
        (agent_capacity, agent_binding),
        (lender.debt_headroom_capacity, BindingConstraint::DebtHeadroom),
        (lender.remaining_strategy_budget, BindingConstraint::StrategyBudget),
        (lender.remaining_vault_budget, BindingConstraint::VaultBudget),
        (lender.available_cash, BindingConstraint::AvailableCash),
    ];
    let (drawable_capacity, drawable_binding) = min_with_reason(&drawable_pairs);

    CapacityResult {
        agent_capacity,
        drawable_capacity,
        agent_binding,
        drawable_binding,
    }
}

fn min_with_reason(pairs: &[(Amount, BindingConstraint)]) -> (Amount, BindingConstraint) {
    let mut best = pairs[0];
    for &(amount, reason) in &pairs[1..] {
        if amount.0 < best.0 .0 {
            best = (amount, reason);
        }
    }
    best
}

/// Facility-relative capacity index, 300–900.
///
/// A monotone restatement of `agent_capacity` — it introduces no parameter
/// and no assumption, and a reader can invert it. Deliberately **not** a
/// credit rating: it is scaled by *this* facility's hard cap, so it is not
/// comparable across facilities.
///
/// Returns `None` when the hard cap is zero, because borrower capacity
/// cannot be evaluated (`docs/03`).
pub fn score(agent_capacity: Amount, account_hard_cap: Amount) -> Option<u32> {
    if account_hard_cap.0 == 0 {
        return None;
    }
    let capped = agent_capacity.0.min(account_hard_cap.0);
    let scaled = capped.saturating_mul(600) / account_hard_cap.0;
    Some(300 + scaled as u32)
}

/// v0 grants the exact requested amount or denies (`docs/03`).
#[derive(Clone, Debug, PartialEq, Eq, Serialize, Deserialize)]
pub enum GrantDecision {
    Approve { principal: Amount },
    Deny { limiting: BindingConstraint, capacity: Amount },
    DenyZeroRequest,
}

/// No implicit partial fill: a request above capacity is denied, not shrunk.
/// An agent wanting less submits a new intent and request.
pub fn decide_grant(requested: Amount, capacity: &CapacityResult) -> GrantDecision {
    if requested.0 == 0 {
        return GrantDecision::DenyZeroRequest;
    }
    if requested.0 <= capacity.drawable_capacity.0 {
        GrantDecision::Approve { principal: requested }
    } else {
        GrantDecision::Deny {
            limiting: capacity.drawable_binding,
            capacity: capacity.drawable_capacity,
        }
    }
}
