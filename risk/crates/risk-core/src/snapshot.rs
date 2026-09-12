//! Snapshot hashing and identity derivation.
//!
//! `docs/04`: derive `assessment_id` from the canonical tuple
//! `(schema_version, input_hash, model_hash, policy_hash)`. Sequence and
//! time proposals are inside the input hash, so **the engine allocates no
//! IDs, reads no counter, and chooses no timestamp**. Feed it the same
//! input twice and you get the same identity — which is what makes the
//! replay claim in `docs/09` beat 8 checkable rather than asserted.

use serde_json::Value;
use sha2::{Digest, Sha256};

use crate::canonical;
use crate::input::RiskInput;

#[derive(Debug, PartialEq, Eq)]
pub enum SnapshotError {
    Encode(canonical::Error),
    Serialize(String),
}

impl From<canonical::Error> for SnapshotError {
    fn from(e: canonical::Error) -> Self {
        SnapshotError::Encode(e)
    }
}

/// Canonical bytes of an input. These are what gets persisted — `docs/04`
/// requires storing exact bytes, not a row that could later be
/// re-serialized differently.
pub fn encode_input(input: &RiskInput) -> Result<Vec<u8>, SnapshotError> {
    let value: Value =
        serde_json::to_value(input).map_err(|e| SnapshotError::Serialize(e.to_string()))?;
    Ok(canonical::encode(&value)?)
}

/// `input_hash`: SHA-256 over the canonical encoding.
pub fn input_hash(input: &RiskInput) -> Result<String, SnapshotError> {
    let bytes = encode_input(input)?;
    Ok(hex(&Sha256::digest(&bytes)))
}

/// `assessment_id`, derived from the canonical tuple.
///
/// Deriving rather than allocating means two gateways processing the same
/// snapshot agree on identity without coordinating, and a replay is
/// recognisable as the same assessment rather than a new one.
pub fn assessment_id(
    schema_version: &str,
    input_hash: &str,
    model_hash: &str,
    policy_hash: &str,
) -> String {
    let mut hasher = Sha256::new();
    // Length-prefixed so ("ab","c") and ("a","bc") cannot collide.
    for part in [schema_version, input_hash, model_hash, policy_hash] {
        hasher.update((part.len() as u64).to_be_bytes());
        hasher.update(part.as_bytes());
    }
    hex(&hasher.finalize())
}

/// `assessment_hash`: commits to the complete canonical output, which is
/// what a signature is taken over.
pub fn output_hash(output: &Value) -> Result<String, SnapshotError> {
    let bytes = canonical::encode(output)?;
    Ok(hex(&Sha256::digest(&bytes)))
}

/// Identity of one assessment, derived entirely from its inputs.
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct SnapshotIdentity {
    pub input_hash: String,
    pub assessment_id: String,
}

pub fn identify(input: &RiskInput) -> Result<SnapshotIdentity, SnapshotError> {
    let ih = input_hash(input)?;
    let id = assessment_id(
        &input.schema_version,
        &ih,
        &input.model_policy.model_hash,
        &input.model_policy.policy_hash,
    );
    Ok(SnapshotIdentity {
        input_hash: ih,
        assessment_id: id,
    })
}

fn hex(bytes: &[u8]) -> String {
    bytes.iter().map(|b| format!("{b:02x}")).collect()
}
