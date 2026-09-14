//! Evidence envelopes and the policy that gates authorization on them.
//!
//! Two rules from `docs/04` shape everything here:
//!
//! 1. **An acquisition failure changes availability, not origin.** Observed
//!    evidence is never replaced with synthetic values to keep a facility
//!    running. `Origin` is therefore a property of the observation, fixed at
//!    creation, and there is no method that mutates it.
//!
//! 2. **Missing required evidence yields `Unavailable`, never a zero-risk
//!    approval.** Absence must not become zero. A missing debt figure is
//!    unknown debt; a missing liquidation record is not a clean history.

use serde::{Deserialize, Serialize};

/// Where a value came from. Fixed when the observation is created.
#[derive(Clone, Copy, Debug, PartialEq, Eq, Serialize, Deserialize)]
pub enum Origin {
    Observed,
    Synthetic,
    PolicyAssumption,
}

/// Availability of a value, independent of its origin.
#[derive(Clone, Copy, Debug, PartialEq, Eq, Serialize, Deserialize)]
pub enum State {
    Present,
    Missing,
    Stale,
    Conflicting,
    Unsupported,
}

/// How completely the observation covers its declared scope.
#[derive(Clone, Copy, Debug, PartialEq, Eq, Serialize, Deserialize)]
pub enum Coverage {
    Complete,
    Partial,
    Unknown,
}

/// Whether a field must be present for an executable decision.
#[derive(Clone, Copy, Debug, PartialEq, Eq, Serialize, Deserialize)]
pub enum Requirement {
    /// Failure blocks authorization.
    Required,
    /// Failure omits the feature and awards no benefit for its absence.
    Optional,
}

/// Evidence class of the resulting assessment (`docs/04`).
#[derive(Clone, Copy, Debug, PartialEq, Eq, PartialOrd, Ord, Serialize, Deserialize)]
pub enum Class {
    ScenarioOnly,
    ObservedShadow,
    PrototypeEligible,
}

/// One observation with its lineage.
#[derive(Clone, Debug, PartialEq, Eq, Serialize, Deserialize)]
pub struct Evidence<T> {
    pub value: Option<T>,
    pub state: State,
    pub origin: Origin,
    pub requirement: Requirement,
    pub source: String,
    pub observed_at: Option<u64>,
    pub retrieved_at: u64,
    pub coverage: Coverage,
    pub evidence_id: String,
    pub content_hash: String,
    pub parent_evidence_ids: Vec<String>,
    pub scenario_id: Option<String>,
}

impl<T> Evidence<T> {
    /// Usable for a decision: present, with a value, and covering its scope.
    ///
    /// `Partial` coverage does not qualify. A response can be current *and*
    /// incomplete (`docs/04`), and acting on part of a portfolio is how an
    /// underwriter misses the position that matters.
    pub fn is_usable(&self) -> bool {
        self.state == State::Present
            && self.value.is_some()
            && self.coverage == Coverage::Complete
    }

    /// Blocks authorization: required, and not usable.
    pub fn blocks_authorization(&self) -> bool {
        self.requirement == Requirement::Required && !self.is_usable()
    }

    /// Freshness against a policy bound. Evidence with no observation time
    /// cannot be shown fresh, so it is treated as stale.
    pub fn is_stale(&self, now: u64, max_age_seconds: u64) -> bool {
        match self.observed_at {
            Some(t) => now.saturating_sub(t) > max_age_seconds,
            None => true,
        }
    }
}

/// Why an assessment could not produce executable terms.
#[derive(Clone, Debug, PartialEq, Eq, Serialize, Deserialize)]
pub enum Blocker {
    MissingRequired { evidence_id: String, source: String },
    StaleRequired { evidence_id: String, age_seconds: u64 },
    ConflictingRequired { evidence_id: String },
    UnsupportedRequired { evidence_id: String },
    PartialCoverage { evidence_id: String },
    /// Synthetic input reached a facility whose policy forbids it.
    SyntheticInObservedFacility { evidence_id: String },
}

/// What a facility will accept as evidence.
#[derive(Clone, Copy, Debug, PartialEq, Eq, Serialize, Deserialize)]
pub enum FacilityPolicy {
    /// Accepts synthetic input; results are scenario-only.
    Scenario,
    /// Rejects synthetic input entirely (`docs/04`).
    Observed,
}

/// A judgment over one snapshot's evidence.
#[derive(Clone, Debug, Default, PartialEq, Eq, Serialize, Deserialize)]
pub struct EvidenceVerdict {
    pub blockers: Vec<Blocker>,
    /// Optional features omitted. Recorded so the explanation can say the
    /// feature is absent rather than implying a clean result.
    pub omitted: Vec<String>,
    /// True once any synthetic input has contributed.
    pub synthetic_used: bool,
}

impl EvidenceVerdict {
    pub fn authorized(&self) -> bool {
        self.blockers.is_empty()
    }

    /// Highest class this evidence can support.
    ///
    /// Synthetic input caps the result at `ScenarioOnly` regardless of how
    /// it was signed — `docs/04`: any synthetic input that affects terms or
    /// risk permission requires a scenario domain.
    pub fn max_class(&self, requested: Class) -> Class {
        if self.synthetic_used {
            Class::ScenarioOnly
        } else {
            requested
        }
    }
}

/// Accumulates evidence judgments for one snapshot.
pub struct EvidenceCheck {
    policy: FacilityPolicy,
    now: u64,
    max_age_seconds: u64,
    verdict: EvidenceVerdict,
}

impl EvidenceCheck {
    pub fn new(policy: FacilityPolicy, now: u64, max_age_seconds: u64) -> Self {
        Self {
            policy,
            now,
            max_age_seconds,
            verdict: EvidenceVerdict::default(),
        }
    }

    /// Judge one field, recording any blocker it introduces.
    pub fn observe<T>(&mut self, ev: &Evidence<T>) {
        // Origin is checked first and independently of availability: a
        // synthetic value in an observed facility is a policy violation even
        // when the value is present and fresh.
        if ev.origin == Origin::Synthetic {
            self.verdict.synthetic_used = true;
            if self.policy == FacilityPolicy::Observed {
                self.verdict
                    .blockers
                    .push(Blocker::SyntheticInObservedFacility {
                        evidence_id: ev.evidence_id.clone(),
                    });
                return;
            }
        }

        if ev.requirement == Requirement::Optional {
            // Freshness is COMPUTED, not read from a label. Evidence
            // marked Present but observed long ago is expired enrichment,
            // and consuming it would let an old reading improve terms
            // while the engine reports nothing omitted (`docs/04`).
            let expired = ev.is_stale(self.now, self.max_age_seconds);
            if !ev.is_usable() || expired {
                // Omitted, with no benefit awarded for the absence.
                self.verdict.omitted.push(ev.evidence_id.clone());
            }
            return;
        }

        match ev.state {
            State::Missing => self.verdict.blockers.push(Blocker::MissingRequired {
                evidence_id: ev.evidence_id.clone(),
                source: ev.source.clone(),
            }),
            State::Stale => self.verdict.blockers.push(Blocker::StaleRequired {
                evidence_id: ev.evidence_id.clone(),
                age_seconds: ev
                    .observed_at
                    .map_or(u64::MAX, |t| self.now.saturating_sub(t)),
            }),
            State::Conflicting => self.verdict.blockers.push(Blocker::ConflictingRequired {
                evidence_id: ev.evidence_id.clone(),
            }),
            State::Unsupported => self.verdict.blockers.push(Blocker::UnsupportedRequired {
                evidence_id: ev.evidence_id.clone(),
            }),
            State::Present => {
                if ev.value.is_none() {
                    self.verdict.blockers.push(Blocker::MissingRequired {
                        evidence_id: ev.evidence_id.clone(),
                        source: ev.source.clone(),
                    });
                } else if ev.coverage != Coverage::Complete {
                    self.verdict.blockers.push(Blocker::PartialCoverage {
                        evidence_id: ev.evidence_id.clone(),
                    });
                } else if ev.is_stale(self.now, self.max_age_seconds) {
                    // Freshness is enforced even when the provider reported
                    // the value as Present — a current fetch of an old
                    // observation is still an old observation.
                    self.verdict.blockers.push(Blocker::StaleRequired {
                        evidence_id: ev.evidence_id.clone(),
                        age_seconds: ev
                            .observed_at
                            .map_or(u64::MAX, |t| self.now.saturating_sub(t)),
                    });
                }
            }
        }
    }

    pub fn finish(self) -> EvidenceVerdict {
        self.verdict
    }
}
