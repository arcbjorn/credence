//! Stress scenarios and capacity solving.

use risk_core::input::{AccountState, MarketState, Position};
use risk_core::stress::*;
use risk_core::units::{Amount, Bps, Ratio};

fn policy() -> StressPolicy {
    StressPolicy {
        min_price_shock_bps: Bps(3_000),      // 30% floor
        volatility_multiple_bps: Bps(15_000), // 1.5x realized vol
        max_price_shock_bps: Bps(9_000),
        liquidity_haircut_bps: Bps(2_000),
        execution_cost_bps: Bps(50),
        keeper_fee_bps: Bps(50),
        min_origination_coverage: Ratio::new(6, 5).unwrap(), // 1.20
    }
}

fn market(vol_bps: u32) -> MarketState {
    MarketState {
        observed_at: 1_000,
        realized_volatility_bps: Bps(vol_bps),
        volatility_window_seconds: 86_400,
        liquidity_depth: Amount(1_000_000_000_000),
    }
}

fn account(settlement: u128, positions: Vec<Position>, debt: u128) -> AccountState {
    AccountState {
        settlement_balance: Amount(settlement),
        positions,
        state_version: 1,
        outstanding_debt: Amount(debt),
        facility_limit: Amount(100_000_000_000),
        restricted: false,
    }
}

fn eth(units: u128) -> Position {
    Position {
        asset: "ETH".into(),
        amount: Amount(units),
        price: Amount(2_000_000_000), // 2000.000000 in 6dp
        price_scale: 6,
        haircut_bps: Bps(500),
    }
}

// --- Stress floor ---

/// `docs/03`: a declared minimum floor prevents a calm observation window
/// from silently weakening the test. This is the load-bearing property.
#[test]
fn calm_market_still_gets_the_floor_shock() {
    let p = policy();
    // 1% realized vol * 1.5 = 150 bps, far under the 3000 bps floor.
    assert_eq!(effective_price_shock(&p, &market(100)), Bps(3_000));
}

#[test]
fn volatile_market_scales_above_the_floor() {
    let p = policy();
    // 40% vol * 1.5 = 6000 bps.
    assert_eq!(effective_price_shock(&p, &market(4_000)), Bps(6_000));
}

#[test]
fn extreme_volatility_is_capped() {
    let p = policy();
    assert_eq!(effective_price_shock(&p, &market(50_000)), Bps(9_000));
}

// --- Three mandatory scenarios ---

/// All three must be reported side by side (`docs/03`).
#[test]
fn all_three_scenarios_are_evaluated() {
    let acct = account(0, vec![eth(5_000_000_000_000_000_000)], 0);
    let results = evaluate_all(
        acct.settlement_balance,
        &acct.positions,
        Amount(5_000_000_000),
        &market(2_000),
        &policy(),
    )
    .unwrap();

    assert_eq!(results.len(), 3);
    assert_eq!(results[0].shock, Shock::Price);
    assert_eq!(results[1].shock, Shock::Liquidity);
    assert_eq!(results[2].shock, Shock::Combined);
}

/// The combined shock must be at least as severe as either alone —
/// otherwise reporting three scenarios is decorative.
#[test]
fn combined_shock_is_the_most_severe() {
    let acct = account(0, vec![eth(5_000_000_000_000_000_000)], 0);
    let r = evaluate_all(
        acct.settlement_balance,
        &acct.positions,
        Amount(1_000_000_000),
        &market(2_000),
        &policy(),
    )
    .unwrap();

    let price = r[0].recoverable_value.0;
    let liquidity = r[1].recoverable_value.0;
    let combined = r[2].recoverable_value.0;

    assert!(combined <= price, "combined must be at least as severe as price");
    assert!(combined <= liquidity, "combined must be at least as severe as liquidity");
}

/// Settlement balance does not fall under a price shock, but a risky
/// position does.
#[test]
fn price_shock_hits_positions_not_settlement() {
    let p = policy();
    let cash_only = recoverable_value(Amount(10_000_000_000), &[], Shock::Price, Bps(3_000), &p)
        .unwrap();
    let no_shock = recoverable_value(Amount(10_000_000_000), &[], Shock::Liquidity, Bps(3_000), &p)
        .unwrap();
    assert_eq!(cash_only, no_shock, "cash is unaffected by a price shock");
}

// --- Capacity solving ---

/// Brute-force reference, as `docs/03` requires: the binary search must
/// agree with a linear scan over a small range.
fn reference_capacity(
    acct: &AccountState,
    action: &IntendedAction,
    apr: Bps,
    term: u64,
    mkt: &MarketState,
    p: &StressPolicy,
    upper: u128,
    step: u128,
) -> u128 {
    let mut best = 0u128;
    let mut candidate = 0u128;
    while candidate <= upper {
        if principal_is_safe(acct, action, Amount(candidate), apr, term, mkt, p) {
            best = candidate;
        }
        candidate += step;
    }
    best
}

#[test]
fn binary_search_agrees_with_reference() {
    let acct = account(5_000_000_000, vec![], 0);
    let p = policy();
    let m = market(2_000);
    let action = IntendedAction::Hold;
    let upper = 20_000_000_000u128;
    let step = 100_000_000u128;

    let searched = stress_capacity(&acct, &action, Bps(1_200), 10 * 86_400, &m, &p, Amount(upper));
    let reference = reference_capacity(&acct, &action, Bps(1_200), 10 * 86_400, &m, &p, upper, step);

    // The search is exact; the reference is granular, so it can only be
    // lower — but never by more than one step.
    assert!(
        searched.0 >= reference && searched.0 < reference + step,
        "searched {} vs reference {} (step {})",
        searched.0,
        reference,
        step
    );
}

/// Safety is monotone: if a principal is safe, every smaller one is too.
/// Binary search is only valid because of this.
#[test]
fn safety_is_monotone_in_principal() {
    let acct = account(5_000_000_000, vec![], 0);
    let p = policy();
    let m = market(2_000);
    let cap = stress_capacity(
        &acct, &IntendedAction::Hold, Bps(1_200), 10 * 86_400, &m, &p, Amount(50_000_000_000),
    );

    assert!(principal_is_safe(&acct, &IntendedAction::Hold, cap, Bps(1_200), 10 * 86_400, &m, &p));
    assert!(!principal_is_safe(
        &acct, &IntendedAction::Hold, Amount(cap.0 + 1), Bps(1_200), 10 * 86_400, &m, &p
    ));
}

/// The post-action portfolio matters: acquiring a shocked asset yields less
/// capacity than holding cash, at identical inputs.
#[test]
fn acquiring_risk_reduces_capacity_versus_holding() {
    let acct = account(5_000_000_000, vec![], 0);
    let p = policy();
    let m = market(2_000);
    let upper = Amount(50_000_000_000);

    let holding = stress_capacity(&acct, &IntendedAction::Hold, Bps(1_200), 864_000, &m, &p, upper);
    let acquiring = stress_capacity(
        &acct,
        &IntendedAction::Acquire {
            asset: "ETH".into(),
            price: Amount(2_000_000_000),
            price_scale: 6,
            haircut_bps: Bps(500),
        },
        Bps(1_200),
        864_000,
        &m,
        &p,
        upper,
    );

    assert!(
        acquiring.0 < holding.0,
        "deploying into a shocked asset must reduce capacity: {} vs {}",
        acquiring.0,
        holding.0
    );
}

/// An account with no equity supports no draw (`docs/02`).
#[test]
fn no_equity_yields_no_capacity() {
    let acct = account(0, vec![], 0);
    let cap = stress_capacity(
        &acct,
        &IntendedAction::Acquire {
            asset: "ETH".into(),
            price: Amount(2_000_000_000),
            price_scale: 6,
            haircut_bps: Bps(500),
        },
        Bps(1_200),
        864_000,
        &market(2_000),
        &policy(),
        Amount(10_000_000_000),
    );
    assert_eq!(cap, Amount::ZERO);
}

/// Existing debt reduces what can be added.
#[test]
fn existing_debt_reduces_capacity() {
    let p = policy();
    let m = market(2_000);
    let clean = stress_capacity(
        &account(10_000_000_000, vec![], 0),
        &IntendedAction::Hold, Bps(1_200), 864_000, &m, &p, Amount(50_000_000_000),
    );
    let levered = stress_capacity(
        &account(10_000_000_000, vec![], 3_000_000_000),
        &IntendedAction::Hold, Bps(1_200), 864_000, &m, &p, Amount(50_000_000_000),
    );
    assert!(levered.0 < clean.0);
}

// --- Debt headroom ---

/// Headroom must fit principal AND reserved interest, or the facility limit
/// is breached as soon as interest accrues.
#[test]
fn headroom_reserves_full_term_interest() {
    let available = Amount(5_000_000_000);
    let cap = debt_headroom_capacity(available, Bps(1_200), 10 * 86_400);

    assert!(cap.0 < available.0, "capacity must leave room for interest");

    let reserve = risk_core::interest::full_term(cap, Bps(1_200), 10 * 86_400).unwrap();
    assert!(cap.0 + reserve.0 <= available.0, "principal plus reserve must fit");

    // One unit more must not fit.
    let next = Amount(cap.0 + 1);
    let next_reserve = risk_core::interest::full_term(next, Bps(1_200), 10 * 86_400).unwrap();
    assert!(next.0 + next_reserve.0 > available.0, "capacity must be maximal");
}

#[test]
fn zero_headroom_yields_zero_capacity() {
    assert_eq!(debt_headroom_capacity(Amount::ZERO, Bps(1_200), 864_000), Amount::ZERO);
}

/// At zero APR there is no reserve, so all headroom is usable.
#[test]
fn zero_apr_uses_all_headroom() {
    let available = Amount(5_000_000_000);
    assert_eq!(debt_headroom_capacity(available, Bps::ZERO, 864_000), available);
}
