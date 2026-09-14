//! Pricing schedule and term economics.

use risk_core::pricing::*;
use risk_core::units::{Amount, Bps};

fn policy() -> PricingPolicy {
    PricingPolicy {
        base_rate_bps: Bps(1_200),
        max_history_discount_bps: Bps(400),
        max_portfolio_surcharge_bps: Bps(800),
        min_apr_bps: Bps(500),
        max_apr_bps: Bps(4_000),
    }
}

/// A clean record earns the full discount; a risky book pays the surcharge.
#[test]
fn history_discounts_and_portfolio_surcharges() {
    let p = policy();

    let best = price(&p, 1_000_000, 0);
    assert_eq!(best.apr_bps, Bps(800), "1200 base - 400 discount");

    let worst = price(&p, 0, 1_000_000);
    assert_eq!(worst.apr_bps, Bps(2_000), "1200 base + 800 surcharge");

    let neutral = price(&p, 0, 0);
    assert_eq!(neutral.apr_bps, p.base_rate_bps);
}

/// Better history never raises the rate; more portfolio risk never lowers it.
#[test]
fn pricing_is_monotone_in_both_inputs() {
    let p = policy();
    let mut previous = u32::MAX;
    for q in (0..=1_000_000).step_by(100_000) {
        let apr = price(&p, q, 0).apr_bps.0;
        assert!(apr <= previous, "better history must not raise the rate");
        previous = apr;
    }
    let mut previous = 0u32;
    for r in (0..=1_000_000).step_by(100_000) {
        let apr = price(&p, 0, r).apr_bps.0;
        assert!(apr >= previous, "more risk must not lower the rate");
        previous = apr;
    }
}

/// The schedule's floor and ceiling are reported as clamps, so a displayed
/// rate is never mistaken for the sum of its components.
#[test]
fn clamping_is_reported() {
    let mut p = policy();
    p.max_history_discount_bps = Bps(2_000); // would take 1200 below the floor
    let b = price(&p, 1_000_000, 0);
    assert_eq!(b.apr_bps, p.min_apr_bps);
    assert!(b.clamped);

    let unclamped = price(&policy(), 500_000, 0);
    assert!(!unclamped.clamped);
}

/// `docs/03`: do not make every borrower acceptable by raising APR. A rate
/// above the borrower's declared ceiling is a refusal, not a reprice.
#[test]
fn rate_above_borrower_maximum_is_refused() {
    assert_eq!(
        check_borrower_maximum(Bps(2_000), Bps(1_500)),
        Err(PricingRefusal::ExceedsBorrowerMaximum {
            offered_bps: Bps(2_000),
            maximum_bps: Bps(1_500),
        })
    );
    assert_eq!(check_borrower_maximum(Bps(1_400), Bps(1_500)), Ok(()));
    // Exactly at the ceiling is acceptable.
    assert_eq!(check_borrower_maximum(Bps(1_500), Bps(1_500)), Ok(()));
}

/// Gross interest matches the interest module exactly — the same figure the
/// contract accrues (`docs/11` cross-language reconciliation).
#[test]
fn gross_interest_matches_interest_module() {
    let e = economics(
        Amount(5_000_000_000),
        Bps(1_200),
        10 * 86_400,
        Bps(450),
        Bps(100),
        None,
    )
    .unwrap();
    assert_eq!(e.gross_interest, Amount(16_438_357));
    assert!(
        e.scenario_expected_loss.is_none(),
        "no assumption, no figure"
    );
}

/// EL = PD_over_term * LGD * exposure, all on the loan's horizon.
#[test]
fn expected_loss_uses_term_horizon() {
    let e = economics(
        Amount(10_000_000_000),
        Bps(980),
        30 * 86_400,
        Bps(450),
        Bps(100),
        Some(LossAssumption {
            pd_over_term_ppm: 17_000,
            lgd_ppm: 250_000,
        }),
    )
    .unwrap();

    // 10,000 * 1.7% * 25% = 42.50
    assert_eq!(e.scenario_expected_loss, Some(Amount(42_500_000)));
    // The assumptions travel with the figure so it is never read as an estimate.
    assert_eq!(e.assumed_pd_ppm, Some(17_000));
    assert_eq!(e.assumed_lgd_ppm, Some(250_000));
}

/// The docs' inconsistent pairing, reproduced: 9.8% APR over 30 days earns
/// 80.55 while a 1.7% PD at full loss implies 170.00. Expenses and loss are
/// reported separately precisely so this is visible rather than netted away.
#[test]
fn components_are_reported_separately_not_netted() {
    let e = economics(
        Amount(10_000_000_000),
        Bps(980),
        30 * 86_400,
        Bps(450),
        Bps(100),
        Some(LossAssumption {
            pd_over_term_ppm: 17_000,
            lgd_ppm: 1_000_000,
        }),
    )
    .unwrap();

    assert_eq!(e.gross_interest, Amount(80_547_946));
    assert_eq!(e.scenario_expected_loss, Some(Amount(170_000_000)));
    assert!(
        e.scenario_expected_loss.unwrap().0 > e.gross_interest.0,
        "the inconsistency must remain visible, not be summed away"
    );
    // Funding and operating expenses are separate line items.
    assert!(e.funding_expense.0 > 0 && e.operating_expense.0 > 0);
}
