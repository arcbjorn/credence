//! Simple fixed-APR interest on a 365-day year.
//!
//! Per `docs/03`: accrual stops at original maturity or entry into recovery,
//! whichever comes first. No late penalty, no compounding. An assessment
//! update never reprices an outstanding fixed-rate loan.

use crate::units::{Amount, Bps};

pub const SECONDS_PER_YEAR: u128 = 365 * 86_400;
const BPS_DENOM: u128 = 10_000;

/// Interest accrued from origination to `accrual_end`, rounded **up**.
///
/// Obligations round up (`docs/04`), so the borrower never benefits from
/// truncation. Elapsed time is clamped to the contractual term: accrual
/// stops at maturity even if repayment comes later.
pub fn accrued(
    principal: Amount,
    apr: Bps,
    originated_at: u64,
    accrual_end: u64,
    term_seconds: u64,
) -> Option<Amount> {
    let elapsed = accrual_end
        .saturating_sub(originated_at)
        .min(term_seconds) as u128;

    let numer = principal
        .0
        .checked_mul(apr.0 as u128)?
        .checked_mul(elapsed)?;
    let denom = BPS_DENOM.checked_mul(SECONDS_PER_YEAR)?;

    Some(Amount(numer.div_ceil(denom)))
}

/// Maximum interest reservable for a full term.
///
/// `docs/03` requires reserving full-term interest when sizing capacity —
/// conservative relative to early repayment, which pays accrued only.
pub fn full_term(principal: Amount, apr: Bps, term_seconds: u64) -> Option<Amount> {
    accrued(principal, apr, 0, term_seconds, term_seconds)
}

/// Total owed: outstanding principal plus interest not yet collected.
pub fn debt_due(
    principal_outstanding: Amount,
    accrued_total: Amount,
    already_collected: Amount,
) -> Option<Amount> {
    let unpaid = accrued_total.checked_sub(already_collected)?;
    principal_outstanding.checked_add(unpaid)
}
