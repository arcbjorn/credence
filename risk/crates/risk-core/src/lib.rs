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

pub mod canonical;
pub mod capacity;
pub mod evidence;
pub mod input;
pub mod snapshot;
pub mod stress;
pub mod interest;
pub mod units;

pub use units::{coverage_meets, Amount, Bps, Ratio};
