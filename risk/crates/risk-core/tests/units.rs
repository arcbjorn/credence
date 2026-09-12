//! Exact-arithmetic tests, checked against the worked numbers in `docs/02`
//! and `docs/03`. Every expected value here is hand-calculated in the docs,
//! not produced by this code.

use risk_core::interest;
use risk_core::{coverage_meets, Amount, Bps, Ratio};

/// `docs/03` worked loss path: 5,000 borrowed at 12% for ten days accrues
/// approximately 16.44 — debt approximately 5,016.44.
#[test]
fn worked_loss_path_interest_matches_docs() {
    // 6-decimal settlement token: 5,000.000000
    let principal = Amount(5_000_000_000);
    let ten_days = 10 * 86_400;

    let accrued = interest::full_term(principal, Bps(1_200), ten_days).unwrap();

    // 5_000_000_000 * 1200 * 864000 / (10000 * 31_536_000) = 16_438_356.16...
    // Obligations round up.
    assert_eq!(accrued, Amount(16_438_357));

    let debt = interest::debt_due(principal, accrued, Amount::ZERO).unwrap();
    assert_eq!(debt, Amount(5_016_438_357));
}

/// `docs/03`: 10,000 at 9.8% for 30 days earns 80.55 gross interest.
/// This is the figure that proved the original pairing inconsistent.
#[test]
fn original_rate_example_matches_docs() {
    let accrued = interest::full_term(Amount(10_000_000_000), Bps(980), 30 * 86_400).unwrap();
    // 80.547945... rounds up to 80.547946
    assert_eq!(accrued.0 / 1_000_000, 80);
    assert_eq!(accrued, Amount(80_547_946));
}

/// Accrual stops at maturity — a loan repaid late owes no extra interest.
/// `docs/03`: no late penalty, no compounding.
#[test]
fn accrual_stops_at_maturity() {
    let p = Amount(5_000_000_000);
    let term = 10 * 86_400;

    let at_maturity = interest::accrued(p, Bps(1_200), 0, term, term).unwrap();
    let long_after = interest::accrued(p, Bps(1_200), 0, term * 5, term).unwrap();

    assert_eq!(at_maturity, long_after);
}

/// Early repayment pays accrued only, never unearned full-term interest.
#[test]
fn early_repayment_pays_less_than_full_term() {
    let p = Amount(5_000_000_000);
    let term = 10 * 86_400;

    let one_day = interest::accrued(p, Bps(1_200), 0, 86_400, term).unwrap();
    let full = interest::full_term(p, Bps(1_200), term).unwrap();

    assert!(one_day < full);
}

/// `docs/04`: at minimum stressed coverage 6/5, max_ltv is exactly 5/6.
/// A rounded 83.33% must never be enforced as a different limit.
#[test]
fn max_ltv_is_exact_reciprocal_of_coverage() {
    let min_coverage = Ratio::new(6, 5).unwrap();
    let max_ltv = min_coverage.recip().unwrap();

    assert_eq!(max_ltv, Ratio::new(5, 6).unwrap());

    // Exactly at the threshold: 5,000 debt against 6,000 value is 6/5 coverage.
    assert!(coverage_meets(Amount(6_000), Amount(5_000), min_coverage));
    // One unit short fails.
    assert!(!coverage_meets(Amount(5_999), Amount(5_000), min_coverage));
}

/// `docs/04`: zero recoverable value against positive debt fails coverage —
/// it must not display as zero risk.
#[test]
fn zero_value_with_debt_fails_coverage() {
    let threshold = Ratio::new(6, 5).unwrap();
    assert!(!coverage_meets(Amount::ZERO, Amount(1), threshold));
    // Zero debt trivially passes.
    assert!(coverage_meets(Amount::ZERO, Amount::ZERO, threshold));
}

/// Ratio comparison is exact under values that would lose precision in f64.
#[test]
fn ratio_comparison_is_exact() {
    let a = Ratio::new(1_000_000_007, 1_000_000_009).unwrap();
    let b = Ratio::new(1_000_000_008, 1_000_000_010).unwrap();
    // Cross-multiplying: 1000000007*1000000010 vs 1000000008*1000000009
    // = 1000000017000000070 vs 1000000017000000072  -> a < b
    assert_eq!(a.cmp_ratio(b), core::cmp::Ordering::Less);
}

/// Overflow returns None rather than wrapping. Authorization amounts must
/// never silently wrap.
#[test]
fn overflow_is_checked_not_wrapped() {
    assert_eq!(Amount(u128::MAX).checked_add(Amount(1)), None);
    assert_eq!(Amount(0).checked_sub(Amount(1)), None);
    assert_eq!(interest::full_term(Amount(u128::MAX), Bps(10_000), 86_400), None);
}

/// Headroom saturates at zero; debt exceeding the limit is not negative.
#[test]
fn headroom_saturates_at_zero() {
    assert_eq!(Amount(100).saturating_sub(Amount(500)), Amount::ZERO);
}

/// Amounts serialize as decimal strings, never as JSON numbers —
/// `docs/04` requires this so no consumer parses them as f64.
#[test]
fn amounts_serialize_as_strings() {
    let json = serde_json::to_string(&Amount(5_016_438_357)).unwrap();
    assert_eq!(json, "\"5016438357\"");

    let back: Amount = serde_json::from_str(&json).unwrap();
    assert_eq!(back, Amount(5_016_438_357));
}
