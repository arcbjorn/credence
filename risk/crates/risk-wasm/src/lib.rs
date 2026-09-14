//! The underwriter, compiled for a browser.
//!
//! `docs/09` beat 8 asks a reader to confirm that a retained assessment
//! reproduces. Until now that required running a Rust binary, which means
//! the claim was only checkable by someone who could build the repository.
//!
//! This crate exposes the SAME engine to a web page. It is deliberately a
//! thin boundary: every function here parses JSON, calls `risk_core`, and
//! serializes the answer. No judgment, no defaults, no convenience
//! fallbacks -- a viewer that "helpfully" filled in a missing field would
//! be reporting a result the engine never produced.
//!
//! **Why this is safe to trust.** `risk-core` is pure: no I/O, no clock,
//! no entropy (`docs/01`). The same input and policy therefore produce the
//! same assessment in a browser as in the CLI, and the workspace forbids
//! `unsafe_code`, so the boundary below is ordinary safe Rust.

use risk_core::assessment::RiskAssessment;
use risk_core::engine::{evaluate as core_evaluate, Policy};
use risk_core::input::RiskInput;
use risk_core::replay as core_replay;
use serde::Serialize;
use wasm_bindgen::prelude::*;

/// What a replay produced, in the shape the replay screen renders.
///
/// `matched` and `differences` are both reported. A UI that showed only a
/// boolean could not explain a mismatch, and `docs/04` requires the fields
/// responsible rather than a bare "not equal".
#[derive(Serialize)]
pub struct ReplayReport {
    pub matched: bool,
    pub assessment_id: String,
    pub input_hash: String,
    pub differences: Vec<DifferenceView>,
}

#[derive(Serialize)]
pub struct DifferenceView {
    pub field: String,
    pub retained: String,
    pub replayed: String,
}

/// Evaluate a snapshot under a policy.
///
/// Takes and returns JSON strings rather than JS objects: the canonical
/// encoding is byte-sensitive, and round-tripping through a JS object
/// would risk a number losing precision on the way in.
#[wasm_bindgen]
pub fn evaluate(input_json: &str, policy_json: &str) -> Result<String, JsError> {
    let input: RiskInput =
        serde_json::from_str(input_json).map_err(|e| JsError::new(&format!("parse input: {e}")))?;
    let policy: Policy = serde_json::from_str(policy_json)
        .map_err(|e| JsError::new(&format!("parse policy: {e}")))?;

    let assessment = core_evaluate(&input, &policy);
    serde_json::to_string(&assessment)
        .map_err(|e| JsError::new(&format!("serialize assessment: {e}")))
}

/// Replay a retained assessment against its retained input.
///
/// A model or policy mismatch is an ERROR, not a difference: replaying
/// under a different model is not a replay that disagreed, it is a replay
/// that never happened.
#[wasm_bindgen]
pub fn replay(
    assessment_json: &str,
    input_json: &str,
    policy_json: &str,
) -> Result<JsValue, JsError> {
    let original: RiskAssessment = serde_json::from_str(assessment_json)
        .map_err(|e| JsError::new(&format!("parse assessment: {e}")))?;
    let input: RiskInput =
        serde_json::from_str(input_json).map_err(|e| JsError::new(&format!("parse input: {e}")))?;
    let policy: Policy = serde_json::from_str(policy_json)
        .map_err(|e| JsError::new(&format!("parse policy: {e}")))?;

    let result = core_replay::replay(&original, &input, &policy)
        .map_err(|e| JsError::new(&e.to_string()))?;

    let report = ReplayReport {
        matched: result.is_match(),
        assessment_id: result.replayed.assessment_id.clone(),
        input_hash: result.replayed.input_hash.clone(),
        differences: result
            .differences
            .iter()
            .map(|d| DifferenceView {
                field: d.field.clone(),
                retained: d.retained.clone(),
                replayed: d.replayed.clone(),
            })
            .collect(),
    };

    serde_wasm_bindgen::to_value(&report)
        .map_err(|e| JsError::new(&format!("serialize report: {e}")))
}

/// The model hash this build carries.
///
/// The viewer shows it beside every replay: a MATCH means nothing if the
/// reader cannot tell which engine produced it.
#[wasm_bindgen]
pub fn model_hash() -> String {
    risk_core::model_hash()
}
