//! Step 8 — pricing with a versioned policy schedule.
//!
//! **Experimental terms, not empirically estimated fair prices** (`docs/03`).
//! The schedule is frozen with the demo fixtures; nothing here claims
//! calibration.
//!
//! The rule that shapes the design: *do not make every borrower acceptable
//! by raising APR*. Pricing runs **after** capacity, and a rate that would
//! exceed the borrower's declared maximum produces a denial rather than a
//! quietly repriced offer. APR is an output of eligibility, never a lever
//! that manufactures it.

use serde::{Deserialize, Serialize};

use crate::interest;
use crate::units::{Amount, Bps};

/// Versioned APR schedule.
#[derive(Clone, Copy, Debug, PartialEq, Eq, Serialize, Deserialize)]
pub struct PricingPolicy {
    pub base_rate_bps: Bps,
    /// Largest discount demonstrated history can earn.
    pub max_history_discount_bps: Bps,
    /// Largest surcharge current portfolio risk can add.
    pub max_portfolio_surcharge_bps: Bps,
    pub min_apr_bps: Bps,
    pub max_apr_bps: Bps,
}

/// Each component named separately, so a reader can see what moved the rate.
#[derive(Clone, Copy, Debug, PartialEq, Eq, Serialize, Deserialize)]
pub struct PriceBreakdown {
    pub base_rate_bps: Bps,
    pub history_discount_bps: Bps,
    pub portfolio_surcharge_bps: Bps,
    pub apr_bps: Bps,
    /// True when the schedule hit its floor or ceiling, so the displayed
    /// rate is a clamp rather than the sum of its parts.
    pub clamped: bool,
}

/// Price one request.
///
/// `history_quality_ppm` and `portfolio_risk_ppm` are normalized to
/// [0, 1_000_000]; both are policy-derived, not measured probabilities.
pub fn price(
    policy: &PricingPolicy,
    history_quality_ppm: u64,
    portfolio_risk_ppm: u64,
) -> PriceBreakdown {
    let hq = history_quality_ppm.min(1_000_000);
    let pr = portfolio_risk_ppm.min(1_000_000);

    let discount = (policy.max_history_discount_bps.0 as u64) * hq / 1_000_000;
    let surcharge = (policy.max_portfolio_surcharge_bps.0 as u64) * pr / 1_000_000;

    let raw = (policy.base_rate_bps.0 as i64) - (discount as i64) + (surcharge as i64);
    let bounded = raw
        .max(policy.min_apr_bps.0 as i64)
        .min(policy.max_apr_bps.0 as i64);

    PriceBreakdown {
        base_rate_bps: policy.base_rate_bps,
        history_discount_bps: Bps(discount as u32),
        portfolio_surcharge_bps: Bps(surcharge as u32),
        apr_bps: Bps(bounded as u32),
        clamped: bounded != raw,
    }
}

/// Expenses and income, each labelled.
///
/// `docs/03`: **gross interest minus expected loss is not lender profit** —
/// that needs a probability-weighted cash-flow model. These fields are
/// reported separately and never summed into a profit claim.
#[derive(Clone, Copy, Debug, PartialEq, Eq, Serialize, Deserialize)]
pub struct EconomicsBreakdown {
    pub gross_interest: Amount,
    pub funding_expense: Amount,
    pub operating_expense: Amount,
    /// Present only when the policy declares a PD assumption.
    pub scenario_expected_loss: Option<Amount>,
    /// The assumption behind that figure, carried alongside so it is never
    /// read as an estimate.
    pub assumed_pd_ppm: Option<u64>,
    pub assumed_lgd_ppm: Option<u64>,
}

/// Optional loss assumption. Both values are declared policy inputs.
#[derive(Clone, Copy, Debug, PartialEq, Eq, Serialize, Deserialize)]
pub struct LossAssumption {
    /// Probability of default over the loan term, in ppm.
    pub pd_over_term_ppm: u64,
    /// Loss given default, in ppm. A named assumption — custody creates a
    /// recovery route, not a recovery rate (`docs/02`).
    pub lgd_ppm: u64,
}

/// Compute term economics. All quantities share the loan's horizon, so no
/// annualization distorts the comparison (`docs/03`).
pub fn economics(
    principal: Amount,
    apr: Bps,
    term_seconds: u64,
    funding_rate_bps: Bps,
    operating_rate_bps: Bps,
    loss: Option<LossAssumption>,
) -> Option<EconomicsBreakdown> {
    let gross_interest = interest::full_term(principal, apr, term_seconds)?;
    let funding_expense = interest::full_term(principal, funding_rate_bps, term_seconds)?;
    let operating_expense = interest::full_term(principal, operating_rate_bps, term_seconds)?;

    let (expected_loss, pd, lgd) = match loss {
        Some(l) => {
            // EL = PD_over_term * LGD * exposure, all on the same horizon.
            let el = principal
                .0
                .checked_mul(l.pd_over_term_ppm as u128)?
                .checked_div(1_000_000)?
                .checked_mul(l.lgd_ppm as u128)?
                .checked_div(1_000_000)?;
            (Some(Amount(el)), Some(l.pd_over_term_ppm), Some(l.lgd_ppm))
        }
        None => (None, None, None),
    };

    Some(EconomicsBreakdown {
        gross_interest,
        funding_expense,
        operating_expense,
        scenario_expected_loss: expected_loss,
        assumed_pd_ppm: pd,
        assumed_lgd_ppm: lgd,
    })
}

/// Why pricing refused, distinct from a capacity denial.
#[derive(Clone, Copy, Debug, PartialEq, Eq, Serialize, Deserialize)]
pub enum PricingRefusal {
    /// The scheduled rate exceeds what the borrower declared acceptable.
    /// A denial, never a silent reprice.
    ExceedsBorrowerMaximum { offered_bps: Bps, maximum_bps: Bps },
}

/// Check the priced rate against the borrower's declared ceiling.
pub fn check_borrower_maximum(
    offered: Bps,
    borrower_max: Bps,
) -> Result<(), PricingRefusal> {
    if offered.0 > borrower_max.0 {
        return Err(PricingRefusal::ExceedsBorrowerMaximum {
            offered_bps: offered,
            maximum_bps: borrower_max,
        });
    }
    Ok(())
}
