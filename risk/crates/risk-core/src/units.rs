//! Integer money and exact ratios.
//!
//! Per `docs/04`: token amounts are integer base units; no floating-point
//! path produces an amount used in authorization. Every operation here is
//! checked — overflow returns `None` rather than wrapping or panicking.

use serde::{Deserialize, Serialize};

/// Token amount in integer base units, serialized as a decimal string.
///
/// `u128` holds any realistic balance: 18-decimal tokens reach only ~1e38
/// at supply levels far beyond anything this protocol touches.
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq, PartialOrd, Ord, Serialize, Deserialize)]
#[serde(transparent)]
pub struct Amount(#[serde(with = "amount_str")] pub u128);

impl Amount {
    pub const ZERO: Self = Amount(0);

    pub fn checked_add(self, other: Self) -> Option<Self> {
        self.0.checked_add(other.0).map(Amount)
    }

    pub fn checked_sub(self, other: Self) -> Option<Self> {
        self.0.checked_sub(other.0).map(Amount)
    }

    /// Saturating difference — for headroom, where negative means zero.
    pub fn saturating_sub(self, other: Self) -> Self {
        Amount(self.0.saturating_sub(other.0))
    }
}

mod amount_str {
    use serde::{Deserialize, Deserializer, Serializer};

    pub fn serialize<S: Serializer>(v: &u128, s: S) -> Result<S::Ok, S::Error> {
        s.serialize_str(&v.to_string())
    }

    pub fn deserialize<'de, D: Deserializer<'de>>(d: D) -> Result<u128, D::Error> {
        let s = String::deserialize(d)?;
        s.parse().map_err(serde::de::Error::custom)
    }
}

/// Annual rate in basis points. 10_000 bps = 100%.
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq, PartialOrd, Ord, Serialize, Deserialize)]
#[serde(transparent)]
pub struct Bps(pub u32);

impl Bps {
    pub const ZERO: Self = Bps(0);
    pub const ONE_HUNDRED_PCT: Self = Bps(10_000);
}

/// Exact ratio as integer numerator and denominator.
///
/// `docs/04`: coverage and LTV thresholds are exact integer ratios; rounded
/// percentages are display-only. At minimum stressed coverage `6/5`,
/// `max_ltv = 5/6` — never a rounded 83.33% enforced as a different limit.
#[derive(Clone, Copy, Debug, PartialEq, Eq, Serialize, Deserialize)]
pub struct Ratio {
    pub num: u64,
    pub den: u64,
}

impl Ratio {
    /// Returns `None` for a zero denominator rather than constructing an
    /// invalid ratio that would fail later at an unrelated call site.
    pub fn new(num: u64, den: u64) -> Option<Self> {
        (den != 0).then_some(Ratio { num, den })
    }

    /// Reciprocal. `None` when the numerator is zero.
    pub fn recip(self) -> Option<Self> {
        Self::new(self.den, self.num)
    }

    /// Compare by cross-multiplication in `u128` — exact, no division,
    /// no precision loss.
    pub fn cmp_ratio(self, other: Self) -> core::cmp::Ordering {
        let lhs = self.num as u128 * other.den as u128;
        let rhs = other.num as u128 * self.den as u128;
        lhs.cmp(&rhs)
    }

    /// `amount * self`, rounded down. `None` on overflow.
    pub fn mul_floor(self, amount: Amount) -> Option<Amount> {
        amount
            .0
            .checked_mul(self.num as u128)
            .map(|v| Amount(v / self.den as u128))
    }

    /// `amount * self`, rounded up. Used where obligations round up.
    pub fn mul_ceil(self, amount: Amount) -> Option<Amount> {
        let prod = amount.0.checked_mul(self.num as u128)?;
        let den = self.den as u128;
        Some(Amount(prod.div_ceil(den)))
    }
}

/// Coverage comparison: does `value / debt` meet `threshold`?
///
/// Zero debt trivially passes. A positive debt against zero recoverable
/// value fails — `docs/04` requires this to fail coverage rather than
/// display as zero risk.
pub fn coverage_meets(value: Amount, debt: Amount, threshold: Ratio) -> bool {
    if debt.0 == 0 {
        return true;
    }
    // value/debt >= num/den  <=>  value*den >= num*debt
    let lhs = value.0.saturating_mul(threshold.den as u128);
    let rhs = (threshold.num as u128).saturating_mul(debt.0);
    lhs >= rhs
}
