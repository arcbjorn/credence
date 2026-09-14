//! Step 9 — the assessment output.
//!
//! `docs/04` constrains the *shape* as strictly as the values:
//!
//! - An eligible origination: `Approved`, `Allowed`, and
//!   `approved_principal == requested_principal`, with terms.
//! - A policy rejection: `Denied`, `Blocked`, **no grant, no terms**.
//! - Monitoring: always omits request fields, granted principal, terms, and
//!   the hypothetical new-loan capacity and score fields.
//! - Missing required evidence: `Unavailable`, `Blocked`, no executable
//!   terms. **Absence must never become zero.**
//!
//! Modelled so the invalid combinations cannot be constructed: terms live
//! inside the `Approved` variant, and monitoring has no field to put them
//! in. The oracle rejects malformed messages too (`docs/05`), but a shape
//! that cannot be built never reaches it.

use serde::{Deserialize, Serialize};

use crate::capacity::{BindingConstraint, CapacityResult};
use crate::evidence::{Blocker, Class};
use crate::pricing::{EconomicsBreakdown, PriceBreakdown};
use crate::stress::StressResult;
use crate::units::{Amount, Bps};

#[derive(Clone, Copy, Debug, PartialEq, Eq, Serialize, Deserialize)]
pub enum Permission {
    Allowed,
    Blocked,
}

/// Terms exist only on an approval — there is no field for them elsewhere.
#[derive(Clone, Debug, PartialEq, Eq, Serialize, Deserialize)]
pub struct ProposedTerms {
    pub approved_principal: Amount,
    pub apr_bps: Bps,
    pub term_seconds: u64,
    pub required_equity: Amount,
    pub permitted_action_hash: String,
    pub price_breakdown: PriceBreakdown,
    pub economics: EconomicsBreakdown,
}

/// Why a decision came out as it did. Stable codes, not prose.
#[derive(Clone, Debug, PartialEq, Eq, Serialize, Deserialize)]
pub enum Reason {
    EvidenceBlocked(Blocker),
    StructurallyInvalid(String),
    CapacityExceeded {
        limiting: BindingConstraint,
        capacity: Amount,
    },
    ZeroRequest,
    PriceExceedsBorrowerMaximum {
        offered_bps: Bps,
        maximum_bps: Bps,
    },
    AccountRestricted,
    /// Recovery is in progress. The keeper owns the position until it
    /// resolves; permission to act would race the liquidation.
    LoanInRecovery,
    /// One active loan per account, and a binding with an unresolved
    /// default gets no new credit (`docs/05`).
    ///
    /// **Enforced on chain, not here.** The loan manager refuses a second
    /// draw outright and the oracle refuses a defaulted binding; the
    /// engine cannot, because `outstanding_debt` in its snapshot is
    /// facility-wide rather than per account -- denying on it would refuse
    /// every borrower whenever any other agent in the facility owed
    /// anything.
    ///
    /// Kept in the vocabulary so a reader decoding an assessment knows
    /// what the class means, and so the gap is stated rather than implied
    /// by an unused name. Reported by review, 14 September.
    ActiveLoanPresent,
    UnresolvedDefault,
    /// Monitoring: the existing account no longer meets monitoring policy.
    MonitoringCoverageBreach {
        shock: String,
    },
    Eligible,
}

/// Borrower-capacity view. Absent on monitoring, which quotes no new loan.
#[derive(Clone, Debug, PartialEq, Eq, Serialize, Deserialize)]
pub struct CapacityView {
    pub agent_capacity: Amount,
    pub drawable_capacity: Amount,
    pub agent_binding: BindingConstraint,
    pub drawable_binding: BindingConstraint,
    /// `None` when borrower capacity cannot be evaluated (`docs/03`).
    pub score: Option<u32>,
}

impl CapacityView {
    pub fn from_result(result: &CapacityResult, score: Option<u32>) -> Self {
        Self {
            agent_capacity: result.agent_capacity,
            drawable_capacity: result.drawable_capacity,
            agent_binding: result.agent_binding,
            drawable_binding: result.drawable_binding,
            score,
        }
    }
}

/// The outcome. Each variant carries exactly the fields its shape permits.
#[derive(Clone, Debug, PartialEq, Eq, Serialize, Deserialize)]
#[serde(tag = "decision")]
// The approved variant is much larger than a denial, and clippy suggests
// boxing it. Declined deliberately: `Outcome` is the type the engine and
// both language bindings pattern-match on, and a `Box` in one arm would
// complicate every reader of it to save stack on a value that is
// constructed once per assessment. The grant and its terms are
// inseparable from the approval (`docs/05`), which is why they sit inline.
#[allow(clippy::large_enum_variant)]
pub enum Outcome {
    /// Origination approved. Grant and terms are inseparable from it.
    ApprovedOrigination {
        requested_principal: Amount,
        approved_principal: Amount,
        terms: ProposedTerms,
        capacity: CapacityView,
    },
    /// Policy rejection. No grant, no terms, by construction.
    Denied {
        requested_principal: Option<Amount>,
        capacity: Option<CapacityView>,
    },
    /// Required evidence insufficient. Known values may still be shown for
    /// diagnosis, but nothing executable is produced.
    Unavailable,
    /// Monitoring permitted risk-increasing actions to be considered.
    MonitoringAllowed { loan_id: String },
    /// Monitoring revoked permission.
    MonitoringBlocked { loan_id: String },
}

impl Outcome {
    /// `docs/04`: Approved sets Allowed; Denied and Unavailable set Blocked.
    pub fn permission(&self) -> Permission {
        match self {
            Outcome::ApprovedOrigination { .. } | Outcome::MonitoringAllowed { .. } => {
                Permission::Allowed
            }
            _ => Permission::Blocked,
        }
    }

    pub fn is_executable(&self) -> bool {
        matches!(self, Outcome::ApprovedOrigination { .. })
    }

    pub fn terms(&self) -> Option<&ProposedTerms> {
        match self {
            Outcome::ApprovedOrigination { terms, .. } => Some(terms),
            _ => None,
        }
    }
}

/// The complete assessment.
#[derive(Clone, Debug, PartialEq, Eq, Serialize, Deserialize)]
pub struct RiskAssessment {
    pub schema_version: String,
    pub assessment_id: String,
    pub input_hash: String,
    pub model_hash: String,
    pub policy_hash: String,
    pub assessment_time: u64,
    pub sequence: u64,
    pub valid_after: u64,
    pub valid_until: u64,
    /// Capped at `ScenarioOnly` whenever synthetic input contributed.
    pub authorization_class: Class,
    pub outcome: Outcome,
    /// All three mandatory scenarios, reported side by side. Present even on
    /// a denial, because the reader needs to see what bound the decision.
    pub stress_results: Vec<StressResult>,
    pub reasons: Vec<Reason>,
    /// Optional features omitted, named so absence is visible rather than
    /// implying a clean result (`docs/04`).
    pub omitted_evidence: Vec<String>,
    pub synthetic_used: bool,
}

impl RiskAssessment {
    pub fn permission(&self) -> Permission {
        self.outcome.permission()
    }

    /// Executable only when approved AND the class permits it. A shadow
    /// approval is non-executable regardless of how it was signed.
    pub fn is_executable(&self) -> bool {
        self.outcome.is_executable() && self.authorization_class == Class::PrototypeEligible
    }
}
