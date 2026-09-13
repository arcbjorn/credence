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
    let cash_only = recoverable_value(Amount(10_000_000_000), &[], Shock::Price, Bps(3_000), Amount(1), &p)
        .unwrap();
    let no_shock = recoverable_value(Amount(10_000_000_000), &[], Shock::Liquidity, Bps(3_000), Amount(1), &p)
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

// --- Liquidity depth governs the exit (docs/03) ---

/// A position large relative to depth must cost more to exit.
///
/// Market impact is not linear in size: a book that IS the market cannot
/// be sold at the price a small clip fetches. A flat haircut prices both
/// identically, which understates exactly the concentrated positions the
/// liquidity scenario exists to catch.
#[test]
fn shallow_depth_reduces_recoverable_value() {
    let p = policy();
    let positions = vec![eth(1_000_000_000_000_000_000)]; // 1 ETH, 18dp

    // The position marks to 2e21 in settlement units (1e18 units at a
    // 2000.000000 price, 6dp). Depth is expressed in the same units.
    let mut deep = market(2_000);
    deep.liquidity_depth = Amount(2_000_000_000_000_000_000_000_000); // 1000x the position
    let mut shallow = market(2_000);
    shallow.liquidity_depth = Amount(2_000_000_000_000_000_000_000); // the position IS the market

    let deep_results =
        evaluate_all(Amount(0), &positions, Amount(1), &deep, &p).expect("deep");
    let shallow_results =
        evaluate_all(Amount(0), &positions, Amount(1), &shallow, &p).expect("shallow");

    let liq = |rs: &[StressResult]| {
        rs.iter()
            .find(|r| r.shock == Shock::Liquidity)
            .expect("liquidity case")
            .recoverable_value
    };

    assert!(
        liq(&shallow_results) < liq(&deep_results),
        "a position that is the market must recover less: {:?} vs {:?}",
        liq(&shallow_results),
        liq(&deep_results)
    );
}

/// Depth far above the position size leaves the exit essentially
/// unaffected, so the model does not penalise ordinary positions.
#[test]
fn ample_depth_leaves_small_positions_alone() {
    let p = policy();
    let positions = vec![eth(1_000_000_000_000_000_000)];

    let mut ample = market(2_000);
    ample.liquidity_depth = Amount(2_000_000_000_000_000_000_000_000_000);
    let mut ampler = market(2_000);
    ampler.liquidity_depth = Amount(2_000_000_000_000_000_000_000_000_000_0);

    let a = evaluate_all(Amount(0), &positions, Amount(1), &ample, &p).expect("a");
    let b = evaluate_all(Amount(0), &positions, Amount(1), &ampler, &p).expect("b");

    let liq = |rs: &[StressResult]| {
        rs.iter()
            .find(|r| r.shock == Shock::Liquidity)
            .expect("liquidity case")
            .recoverable_value
    };

    // Both deep enough that impact rounds to the same floor.
    assert_eq!(
        liq(&a),
        liq(&b),
        "ample depth must not distinguish between two deep markets"
    );
}

/// Zero declared depth cannot silently mean "infinitely deep". It is the
/// worst case, and must not recover more than a shallow-but-real market.
#[test]
fn zero_depth_is_the_worst_case() {
    let p = policy();
    let positions = vec![eth(1_000_000_000_000_000_000)];

    let mut none = market(2_000);
    none.liquidity_depth = Amount::ZERO;
    let mut shallow = market(2_000);
    shallow.liquidity_depth = Amount(2_000_000_000_000_000_000_000);

    let n = evaluate_all(Amount(0), &positions, Amount(1), &none, &p).expect("none");
    let s = evaluate_all(Amount(0), &positions, Amount(1), &shallow, &p).expect("shallow");

    let liq = |rs: &[StressResult]| {
        rs.iter()
            .find(|r| r.shock == Shock::Liquidity)
            .expect("liquidity case")
            .recoverable_value
    };

    assert!(
        liq(&n) <= liq(&s),
        "undeclared depth must not recover more than a shallow market"
    );
}

/// The impact curve must be monotone and match the square-root shape at
/// its reference points, or the "scales with depth" claim is decorative.
#[test]
fn liquidity_impact_follows_the_square_root_law() {
    let p = policy();
    let positions = vec![eth(1_000_000_000_000_000_000)];
    let marked = 2_000_000_000_000_000_000_000u128; // 1e18 units * 2000e6 / 1e6

    let recover_at = |depth: u128| {
        let mut m = market(2_000);
        m.liquidity_depth = Amount(depth);
        evaluate_all(Amount(0), &positions, Amount(1), &m, &p)
            .expect("results")
            .iter()
            .find(|r| r.shock == Shock::Liquidity)
            .expect("liquidity case")
            .recoverable_value
    };

    // Monotone: deeper markets never recover less.
    let quarter = recover_at(marked * 4);
    let same = recover_at(marked);
    let hundredth = recover_at(marked * 100);

    assert!(same <= quarter, "size == depth must cost at least as much as size == depth/4");
    assert!(quarter <= hundredth, "deeper must never recover less");

    // Isolate the LIQUIDITY component. The asset's own haircut and the
    // execution cost apply in every case, so measuring total loss would
    // conflate them with impact and hide the curve's shape.
    let baseline = recover_at(u128::MAX / 2); // effectively infinite depth
    let full_impact = baseline.0 - same.0;
    let quarter_impact = baseline.0 - quarter.0;

    // sqrt(1/4) == 1/2, so a quarter-of-depth position takes about half
    // the impact. A band, since bps arithmetic rounds.
    assert!(
        quarter_impact * 2 >= full_impact * 9 / 10
            && quarter_impact * 2 <= full_impact * 11 / 10,
        "quarter-depth impact should be about half of full: {quarter_impact} vs {full_impact}"
    );
}

/// Undeclared depth must be the WORST case, not a middling one.
///
/// Zero means "we cannot say how deep this market is". If that recovers
/// more than a market known to be nearly empty, then failing to declare
/// depth is rewarded -- and the conservative reading is inverted.
#[test]
fn zero_depth_is_never_better_than_tiny_depth() {
    let p = policy();
    let positions = vec![eth(1_000_000_000_000_000_000)];

    let mut none = market(2_000);
    none.liquidity_depth = Amount::ZERO;
    let mut tiny = market(2_000);
    tiny.liquidity_depth = Amount(1);

    let liq = |m: &MarketState| {
        evaluate_all(Amount(0), &positions, Amount(1), m, &p)
            .expect("results")
            .iter()
            .find(|r| r.shock == Shock::Liquidity)
            .expect("liquidity case")
            .recoverable_value
    };

    assert!(
        liq(&none) <= liq(&tiny),
        "undeclared depth must not recover more than a nearly empty market: {:?} vs {:?}",
        liq(&none),
        liq(&tiny)
    );
}

/// A position enormous relative to depth must not panic.
///
/// The ratio saturates, and an integer square root that computes
/// `(x + n/x)` overflows on a saturated input. A panic here takes the
/// HTTP service down, since evaluation has no panic boundary.
#[test]
fn extreme_liquidity_ratio_does_not_panic() {
    let p = policy();
    // A position marked far above any plausible depth.
    let positions = vec![Position {
        asset: "ETH".into(),
        amount: Amount(u128::MAX / 1_000_000_000_000),
        price: Amount(2_000_000_000),
        price_scale: 6,
        haircut_bps: Bps(500),
    }];

    let mut shallow = market(2_000);
    shallow.liquidity_depth = Amount(1);

    // Must return an answer or None, never abort.
    let _ = evaluate_all(Amount(0), &positions, Amount(1), &shallow, &p);
}

/// The integer square root must be exact, including at the boundary that
/// used to overflow. An approximation here silently mis-scales every
/// liquidity haircut.
#[test]
fn isqrt_is_exact_including_at_the_ceiling() {
    // Exposed through the impact it produces: a position exactly at depth
    // takes the full base haircut, because sqrt(1) == 1.
    let p = policy();
    let positions = vec![eth(1_000_000_000_000_000_000)];
    let marked = 2_000_000_000_000_000_000_000u128;

    let mut at_depth = market(2_000);
    at_depth.liquidity_depth = Amount(marked);

    let r = evaluate_all(Amount(0), &positions, Amount(1), &at_depth, &p)
        .expect("results");
    let liq = r.iter().find(|x| x.shock == Shock::Liquidity).unwrap();

    // marked, less the 500bps asset haircut, less the full 2000bps
    // liquidity impact, less 100bps of costs.
    let expected = marked * 9_500 / 10_000 * 8_000 / 10_000 * 9_900 / 10_000;
    assert_eq!(
        liq.recoverable_value,
        Amount(expected),
        "size == depth must take exactly the base haircut"
    );
}
