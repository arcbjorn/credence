//! Credence risk core — deterministic underwriting.
//!
//! This crate is a pure function. It performs no I/O, reads no clock, draws
//! no entropy, and does not depend on hash-map iteration order. The same
//! canonical input under the same pinned model and policy produces the same
//! canonical assessment, byte for byte.
//!
//! That purity is what makes `docs/04`'s replay contract possible, and it
//! is why this crate can be developed and verified offline while contracts
//! are still in flux.

/// The engine's own identity, committed to in every assessment.
///
/// `model_hash` answers "which decision logic produced this?", the way
/// `policy_hash` answers "under which parameters?". It was previously the
/// literal string `"model-hash"` supplied by the gateway, which committed
/// to nothing: the policy was hashed for real while the model identity was
/// decorative, and two different engine builds produced assessments that
/// claimed the same provenance.
///
/// Declared HERE rather than passed in, because only this crate knows what
/// logic it implements. A caller that could choose it could attest that a
/// decision came from a model it did not run. Bump it whenever the
/// decision logic changes in a way that would alter an output.
///
/// Reported by review, 14 September.
pub const MODEL_ID: &str = "credence-risk-core";
pub const MODEL_VERSION: &str = "v1";

/// `MODEL_ID@MODEL_VERSION`, the value assessments carry.
pub fn model_hash() -> String {
    canonical::hash_hex(&serde_json::json!({
        "model_id": MODEL_ID,
        "model_version": MODEL_VERSION,
    }))
    .unwrap_or_default()
}

pub mod assessment;
pub mod canonical;
pub mod capacity;
pub mod engine;
pub mod evidence;
pub mod input;
pub mod interest;
pub mod pricing;
pub mod snapshot;
pub mod stress;
pub mod units;

pub use units::{coverage_meets, Amount, Bps, Ratio};
