//! Stress scenarios and capacity solving — steps 3 to 5 of `docs/03`.
//!
//! Three shocks are mandatory and reported side by side: price, liquidity,
//! and combined. Volatility scaling supplements them; a **declared minimum
//! floor** stops a calm observation window from silently weakening the test.
//!
//! Capacity is solved on the **post-action** portfolio. A draw adds cash and
//! a debt obligation, and the intended trade changes what is recoverable —
//! sizing from the pre-draw portfolio would systematically over-lend.

use serde::{Deserialize, Serialize};

use crate::input::{AccountState, MarketState, Position};
use crate::interest;
use crate::units::{coverage_meets, Amount, Bps, Ratio};

/// The three mandatory scenarios (`docs/03`).
#[derive(Clone, Copy, Debug, PartialEq, Eq, Serialize, Deserialize)]
pub enum Shock {
    Price,
    Liquidity,
    Combined,
}

impl Shock {
    pub const ALL: [Shock; 3] = [Shock::Price, Shock::Liquidity, Shock::Combined];

    pub fn name(self) -> &'static str {
        match self {
            Shock::Price => "price",
            Shock::Liquidity => "liquidity",
            Shock::Combined => "price+liquidity",
        }
    }
}

/// Versioned stress policy.
#[derive(Clone, Copy, Debug, PartialEq, Eq, Serialize, Deserialize)]
pub struct StressPolicy {
    /// Floor price shock. Applies even in a calm market.
    pub min_price_shock_bps: Bps,
    /// Multiplier on realized volatility, in bps of vol.
    pub volatility_multiple_bps: Bps,
    /// Ceiling, so an extreme reading cannot make capacity absurd.
    pub max_price_shock_bps: Bps,
    /// Extra haircut when liquidity is shocked.
    pub liquidity_haircut_bps: Bps,
    /// Execution cost, deducted once from recoverable value.
    pub execution_cost_bps: Bps,
    /// Bounded keeper fee, included in the recovery budget.
    pub keeper_fee_bps: Bps,
    /// Minimum stressed coverage required to originate.
    pub min_origination_coverage: Ratio,
}

/// One scenario's outcome.
#[derive(Clone, Debug, PartialEq, Eq, Serialize, Deserialize)]
pub struct StressResult {
    pub shock: Shock,
    pub applied_price_shock_bps: Bps,
    /// Recoverable settlement proceeds after shock, haircuts, and costs.
    pub recoverable_value: Amount,
    /// Debt including reserved full-term interest.
    pub debt_with_reserved_interest: Amount,
    pub passes: bool,
}

/// Effective price shock: volatility-scaled, floored, and capped.
///
/// The floor is the load-bearing part. Without it, a quiet observation
/// window would produce a near-zero shock and the stress test would assert
/// nothing (`docs/03`).
pub fn effective_price_shock(policy: &StressPolicy, market: &MarketState) -> Bps {
    let scaled = (market.realized_volatility_bps.0 as u64)
        .saturating_mul(policy.volatility_multiple_bps.0 as u64)
        / 10_000;
    let floored = scaled.max(policy.min_price_shock_bps.0 as u64);
    let capped = floored.min(policy.max_price_shock_bps.0 as u64);
    Bps(capped as u32)
}

/// Recoverable settlement proceeds for one portfolio under one shock.
///
/// Costs are deducted **once**, from recoverable assets — never a second
/// time from the debt side (`docs/03`).
pub fn recoverable_value(
    settlement_balance: Amount,
    positions: &[Position],
    shock: Shock,
    price_shock_bps: Bps,
    // Depth the exit would trade into. Zero means undeclared, which is
    // the worst case rather than an unlimited one.
    depth: Amount,
    policy: &StressPolicy,
) -> Option<Amount> {
    let (apply_price, apply_liquidity) = match shock {
        Shock::Price => (true, false),
        Shock::Liquidity => (false, true),
        Shock::Combined => (true, true),
    };

    // Settlement balance holds its value under a price shock, but still
    // pays execution cost when it must be moved.
    let mut total = settlement_balance;

    for p in positions {
        // Mark to settlement units at the observed price.
        let scale = 10u128.checked_pow(p.price_scale)?;
        let marked = p.amount.0.checked_mul(p.price.0)? / scale;

        let mut value = marked;

        if apply_price {
            let remaining = 10_000u128.checked_sub(price_shock_bps.0 as u128)?;
            value = value.checked_mul(remaining)? / 10_000;
        }

        // The position's own haircut always applies: it is a property of the
        // asset, not of the scenario.
        let after_haircut = 10_000u128.checked_sub(p.haircut_bps.0 as u128)?;
        value = value.checked_mul(after_haircut)? / 10_000;

        if apply_liquidity {
            // Scaled by how much of the market this position represents,
            // rather than a flat rate: a concentrated book is the case
            // the liquidity scenario exists to catch.
            let impact = liquidity_impact_bps(marked, depth, policy.liquidity_haircut_bps);
            let after_liq = 10_000u128.checked_sub(impact.min(10_000))?;
            value = value.checked_mul(after_liq)? / 10_000;
        }

        total = total.checked_add(Amount(value))?;
    }

    // Execution cost and keeper fee, deducted once.
    let cost_bps = (policy.execution_cost_bps.0 as u128) + (policy.keeper_fee_bps.0 as u128);
    let remaining = 10_000u128.checked_sub(cost_bps)?;
    Some(Amount(total.0.checked_mul(remaining)? / 10_000))
}

/// Integer square root, by Newton's method.
///
/// Exact and terminating on every `u128`: the iteration is monotonically
/// decreasing once started above the root, so it converges without a
/// floating-point step. `docs/04` forbids a float path producing a value
/// used in authorization, and this feeds the liquidity haircut.
fn isqrt(n: u128) -> u128 {
    if n < 2 {
        return n;
    }
    // Seed from the bit length rather than from n itself: the obvious
    // `(n + 1) / 2` overflows on u128::MAX, which is reachable whenever a
    // saturating multiply above hits the ceiling. A panic here would take
    // the HTTP service down, since evaluation has no panic boundary.
    let mut x = 1u128 << ((128 - n.leading_zeros()).div_ceil(2));
    loop {
        // Both halves are <= x, so the sum cannot overflow.
        let y = (x / 2) + (n / x / 2);
        if y >= x {
            return x;
        }
        x = y;
    }
}

/// Extra haircut from exiting a position into finite depth, in bps.
///
/// Market impact grows with the square root of size relative to depth --
/// the standard form for liquidation cost, and the reason a flat haircut
/// misprices both ends: a small clip barely moves the market, while a
/// position that IS the market cannot be sold at the observed price.
///
/// `impact = base * sqrt(size / depth)`, computed in bps throughout:
///
///   size == depth      -> the full base haircut
///   size == depth/4    -> half of it
///   size == depth/100  -> a tenth of it
///
/// Depth of zero means undeclared, never infinite. An undeclared market is
/// the worst case, so it takes the maximum impact rather than none.
///
/// Capped at 10,000 bps: a position cannot recover less than nothing.
fn liquidity_impact_bps(marked_value: u128, depth: Amount, base_bps: Bps) -> u128 {
    let base = base_bps.0 as u128;
    if marked_value == 0 {
        return 0;
    }
    if depth.0 == 0 {
        // Undeclared depth is the WORST case, not a middling one. Anything
        // less than the maximum would mean failing to declare depth
        // recovers more than declaring a nearly empty market -- rewarding
        // the omission and inverting the conservative reading.
        return 10_000;
    }

    // ratio_bps = size / depth, in bps. Saturates when the position dwarfs
    // the market, which is exactly when the impact should be maximal.
    let ratio_bps = marked_value
        .saturating_mul(10_000)
        .checked_div(depth.0)
        .unwrap_or(10_000);

    // sqrt in bps space: sqrt(r/10_000) * 10_000 == sqrt(r * 10_000).
    let scaled = isqrt(ratio_bps.saturating_mul(10_000));

    base.saturating_mul(scaled) / 10_000
}

/// Evaluate all three scenarios against a candidate post-action portfolio.
pub fn evaluate_all(
    settlement_balance: Amount,
    positions: &[Position],
    debt_with_reserved_interest: Amount,
    market: &MarketState,
    policy: &StressPolicy,
) -> Option<Vec<StressResult>> {
    let price_shock = effective_price_shock(policy, market);
    let mut out = Vec::with_capacity(3);

    for shock in Shock::ALL {
        let recoverable = recoverable_value(
            settlement_balance,
            positions,
            shock,
            price_shock,
            market.liquidity_depth,
            policy,
        )?;
        out.push(StressResult {
            shock,
            applied_price_shock_bps: price_shock,
            recoverable_value: recoverable,
            debt_with_reserved_interest,
            passes: coverage_meets(
                recoverable,
                debt_with_reserved_interest,
                policy.min_origination_coverage,
            ),
        });
    }
    Some(out)
}

/// The intended use of a draw, needed to build the post-action portfolio.
#[derive(Clone, Debug, PartialEq, Eq, Serialize, Deserialize)]
pub enum IntendedAction {
    /// Principal stays as settlement tokens.
    Hold,
    /// Principal buys the named asset at the given price.
    Acquire {
        asset: String,
        price: Amount,
        price_scale: u32,
        haircut_bps: Bps,
    },
}

/// Does `principal` remain safe under every mandatory scenario?
///
/// Builds the **post-action** portfolio: the draw adds cash, the intended
/// action converts it, and the debt grows by principal plus reserved
/// full-term interest.
/// The portfolio a draw would produce: settlement, positions, and debt as
/// they stand AFTER the principal lands and the declared action runs.
///
/// Extracted so the capacity search and the reported stress cases describe
/// the same portfolio. Reporting the pre-draw book alongside a positive
/// grant shows a debt-free account being stressed against no debt, which
/// is evidence for a loan that will never exist (`docs/04`).
pub struct ProjectedPortfolio {
    pub settlement: Amount,
    pub positions: Vec<Position>,
    pub debt: Amount,
}

/// None when the projection cannot be computed — an overflow, or a price
/// of zero. The caller must treat that as unsafe, never as unchanged.
pub fn project(
    account: &AccountState,
    action: &IntendedAction,
    principal: Amount,
    apr: Bps,
    term_seconds: u64,
) -> Option<ProjectedPortfolio> {
    let reserved = interest::full_term(principal, apr, term_seconds)?;
    let debt = account
        .outstanding_debt
        .checked_add(principal)
        .and_then(|d| d.checked_add(reserved))?;

    let mut positions = account.positions.clone();
    let settlement = match action {
        IntendedAction::Hold => account.settlement_balance.checked_add(principal)?,
        IntendedAction::Acquire {
            asset,
            price,
            price_scale,
            haircut_bps,
        } => {
            if price.0 == 0 {
                return None;
            }
            let scale = 10u128.checked_pow(*price_scale)?;
            let units = principal.0.checked_mul(scale).map(|v| v / price.0)?;
            positions.push(Position {
                asset: asset.clone(),
                amount: Amount(units),
                price: *price,
                price_scale: *price_scale,
                haircut_bps: *haircut_bps,
            });
            account.settlement_balance
        }
    };

    Some(ProjectedPortfolio { settlement, positions, debt })
}

pub fn principal_is_safe(
    account: &AccountState,
    action: &IntendedAction,
    principal: Amount,
    apr: Bps,
    term_seconds: u64,
    market: &MarketState,
    policy: &StressPolicy,
) -> bool {
    let Some(p) = project(account, action, principal, apr, term_seconds) else {
        return false;
    };
    match evaluate_all(p.settlement, &p.positions, p.debt, market, policy) {
        Some(results) => results.iter().all(|r| r.passes),
        None => false,
    }
}

/// Largest safe principal, by bounded binary search.
///
/// Safety is monotone in principal — more debt against the same collateral
/// never becomes safer — so binary search is exact here. `docs/03` requires
/// verifying it against a slow reference implementation, which the tests do.
pub fn stress_capacity(
    account: &AccountState,
    action: &IntendedAction,
    apr: Bps,
    term_seconds: u64,
    market: &MarketState,
    policy: &StressPolicy,
    upper_bound: Amount,
) -> Amount {
    let safe = |p: Amount| {
        principal_is_safe(account, action, p, apr, term_seconds, market, policy)
    };

    if !safe(Amount(1)) {
        return Amount::ZERO;
    }
    if safe(upper_bound) {
        return upper_bound;
    }

    let (mut lo, mut hi) = (1u128, upper_bound.0);
    while lo < hi {
        let mid = lo + (hi - lo + 1) / 2;
        if safe(Amount(mid)) {
            lo = mid;
        } else {
            hi = mid - 1;
        }
    }
    Amount(lo)
}

/// Largest principal whose principal plus reserved interest fits headroom.
///
/// Not simply `available_credit`: the reserve must fit too, or the facility
/// limit is breached the moment interest accrues (`docs/03`).
pub fn debt_headroom_capacity(
    available_credit: Amount,
    apr: Bps,
    term_seconds: u64,
) -> Amount {
    if available_credit.0 == 0 {
        return Amount::ZERO;
    }
    let fits = |p: Amount| match interest::full_term(p, apr, term_seconds) {
        Some(reserve) => match p.checked_add(reserve) {
            Some(total) => total.0 <= available_credit.0,
            None => false,
        },
        None => false,
    };

    if fits(available_credit) {
        return available_credit;
    }
    let (mut lo, mut hi) = (0u128, available_credit.0);
    while lo < hi {
        let mid = lo + (hi - lo + 1) / 2;
        if fits(Amount(mid)) {
            lo = mid;
        } else {
            hi = mid - 1;
        }
    }
    Amount(lo)
}
