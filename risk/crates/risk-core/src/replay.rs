//! Re-evaluation and comparison of a retained assessment.
//!
//! `docs/04` requires a replay to report the fields responsible for a
//! difference rather than a bare mismatch, and `docs/11` treats MATCH as
//! evidence only when a tampered input can produce a DIFFERENCE.
//!
//! **This lives in the core, not in a caller.** It was written in
//! `risk-cli` and moved here on 14 September, when a second caller (the
//! browser, via `risk-wasm`) needed it. Two implementations of "did this
//! assessment reproduce?" is exactly the drift the replay screen exists to
//! rule out: a viewer that agreed with a retained assessment for a reason
//! the CLI did not share would be worse than no viewer at all.

use crate::assessment::{CapacityView, Outcome, RiskAssessment};
use crate::engine::{evaluate, Policy};
use crate::input::RiskInput;
use crate::snapshot;

/// Why a replay could not be attempted at all.
///
/// Distinct from a difference: a replay under a different model or policy
/// is not a replay that disagreed, it is a replay that never happened, and
/// reporting it as a mismatch would send the reader looking for drift that
/// is not there.
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum ReplayError {
    ModelMismatch { assessment: String, input: String },
    PolicyMismatch { assessment: String, input: String },
}

impl core::fmt::Display for ReplayError {
    fn fmt(&self, f: &mut core::fmt::Formatter<'_>) -> core::fmt::Result {
        match self {
            ReplayError::ModelMismatch { assessment, input } => write!(
                f,
                "model mismatch: assessment used {assessment}, input pins {input}"
            ),
            ReplayError::PolicyMismatch { assessment, input } => write!(
                f,
                "policy mismatch: assessment used {assessment}, input pins {input}"
            ),
        }
    }
}

/// One field that did not reproduce.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Difference {
    pub field: String,
    pub retained: String,
    pub replayed: String,
}

impl core::fmt::Display for Difference {
    fn fmt(&self, f: &mut core::fmt::Formatter<'_>) -> core::fmt::Result {
        write!(
            f,
            "{}: retained={} replayed={}",
            self.field, self.retained, self.replayed
        )
    }
}

/// The result of replaying one retained assessment.
pub struct Replay {
    pub replayed: RiskAssessment,
    pub differences: Vec<Difference>,
}

impl Replay {
    /// A replay matches when nothing differs.
    pub fn is_match(&self) -> bool {
        self.differences.is_empty()
    }
}

/// Re-evaluate `input` under `policy` and compare against `original`.
pub fn replay(
    original: &RiskAssessment,
    input: &RiskInput,
    policy: &Policy,
) -> Result<Replay, ReplayError> {
    if original.model_hash != input.model_policy.model_hash {
        return Err(ReplayError::ModelMismatch {
            assessment: original.model_hash.clone(),
            input: input.model_policy.model_hash.clone(),
        });
    }
    if original.policy_hash != input.model_policy.policy_hash {
        return Err(ReplayError::PolicyMismatch {
            assessment: original.policy_hash.clone(),
            input: input.model_policy.policy_hash.clone(),
        });
    }

    let replayed = evaluate(input, policy);
    let differences = diff(original, &replayed);
    Ok(Replay {
        replayed,
        differences,
    })
}

/// Field-level comparison. A bare "not equal" would leave the operator
/// guessing which input drifted.
pub fn diff(a: &RiskAssessment, b: &RiskAssessment) -> Vec<Difference> {
    let mut out = Vec::new();
    let mut cmp = |name: &str, x: String, y: String| {
        if x != y {
            out.push(Difference {
                field: name.to_string(),
                retained: x,
                replayed: y,
            });
        }
    };

    cmp(
        "assessment_id",
        a.assessment_id.clone(),
        b.assessment_id.clone(),
    );
    cmp("input_hash", a.input_hash.clone(), b.input_hash.clone());
    cmp(
        "schema_version",
        a.schema_version.clone(),
        b.schema_version.clone(),
    );
    cmp("sequence", a.sequence.to_string(), b.sequence.to_string());
    cmp(
        "authorization_class",
        format!("{:?}", a.authorization_class),
        format!("{:?}", b.authorization_class),
    );
    // Compare the outcome by its decisive fields rather than dumping the
    // whole struct -- an operator needs to see WHAT differs, not scroll
    // past two near-identical debug renderings.
    cmp("decision", decision_label(a), decision_label(b));
    cmp("granted", granted(a), granted(b));
    cmp("apr_bps", apr(a), apr(b));
    cmp("agent_capacity", agent_capacity(a), agent_capacity(b));
    cmp("binding_constraint", binding(a), binding(b));
    cmp("score", score(a), score(b));
    cmp(
        "synthetic_used",
        a.synthetic_used.to_string(),
        b.synthetic_used.to_string(),
    );

    if out.is_empty() {
        // Catch anything the named fields miss, so a silent divergence in
        // an unlisted field still surfaces.
        let ja = serde_json::to_value(a).unwrap_or_default();
        let jb = serde_json::to_value(b).unwrap_or_default();
        if let (Ok(ha), Ok(hb)) = (snapshot::output_hash(&ja), snapshot::output_hash(&jb)) {
            if ha != hb {
                out.push(Difference {
                    field: "output_hash".to_string(),
                    retained: ha,
                    replayed: hb,
                });
            }
        }
    }
    out
}

pub fn decision_label(a: &RiskAssessment) -> String {
    match &a.outcome {
        Outcome::ApprovedOrigination { .. } => "approved".into(),
        Outcome::Denied { .. } => "denied".into(),
        Outcome::Unavailable => "unavailable".into(),
        Outcome::MonitoringAllowed { .. } => "monitoring_allowed".into(),
        Outcome::MonitoringBlocked { .. } => "monitoring_blocked".into(),
    }
}

pub fn granted(a: &RiskAssessment) -> String {
    match &a.outcome {
        Outcome::ApprovedOrigination {
            approved_principal, ..
        } => approved_principal.0.to_string(),
        _ => "-".into(),
    }
}

pub fn apr(a: &RiskAssessment) -> String {
    a.outcome
        .terms()
        .map_or("-".into(), |t| t.apr_bps.0.to_string())
}

pub fn capacity_view(a: &RiskAssessment) -> Option<&CapacityView> {
    match &a.outcome {
        Outcome::ApprovedOrigination { capacity, .. } => Some(capacity),
        Outcome::Denied { capacity, .. } => capacity.as_ref(),
        _ => None,
    }
}

pub fn agent_capacity(a: &RiskAssessment) -> String {
    capacity_view(a).map_or("-".into(), |c| c.agent_capacity.0.to_string())
}

pub fn binding(a: &RiskAssessment) -> String {
    capacity_view(a).map_or("-".into(), |c| format!("{:?}", c.agent_binding))
}

pub fn score(a: &RiskAssessment) -> String {
    capacity_view(a)
        .and_then(|c| c.score)
        .map_or("-".into(), |s| s.to_string())
}
