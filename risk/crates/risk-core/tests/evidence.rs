//! Evidence policy tests, keyed to the table in `docs/04`.

use risk_core::evidence::*;

fn ev<T>(value: Option<T>, state: State, origin: Origin, requirement: Requirement) -> Evidence<T> {
    Evidence {
        value,
        state,
        origin,
        requirement,
        source: "test".into(),
        observed_at: Some(1_000),
        retrieved_at: 1_000,
        coverage: Coverage::Complete,
        evidence_id: "e1".into(),
        content_hash: "h".into(),
        parent_evidence_ids: vec![],
        scenario_id: None,
    }
}

fn check() -> EvidenceCheck {
    EvidenceCheck::new(FacilityPolicy::Observed, 1_000, 3_600)
}

/// Missing required evidence blocks authorization. Absence must never be
/// read as zero.
#[test]
fn missing_required_blocks() {
    let mut c = check();
    c.observe(&ev::<u64>(
        None,
        State::Missing,
        Origin::Observed,
        Requirement::Required,
    ));
    let v = c.finish();
    assert!(!v.authorized());
    assert!(matches!(v.blockers[0], Blocker::MissingRequired { .. }));
}

/// Stale required market observation blocks risk-increasing action.
#[test]
fn stale_required_blocks() {
    let mut c = EvidenceCheck::new(FacilityPolicy::Observed, 10_000, 3_600);
    // Present and valued, but observed long ago.
    let mut e = ev(
        Some(1u64),
        State::Present,
        Origin::Observed,
        Requirement::Required,
    );
    e.observed_at = Some(1_000);
    c.observe(&e);
    let v = c.finish();
    assert!(
        !v.authorized(),
        "an old observation is stale even when Present"
    );
    assert!(matches!(v.blockers[0], Blocker::StaleRequired { .. }));
}

#[test]
fn conflicting_required_blocks() {
    let mut c = check();
    c.observe(&ev(
        Some(1u64),
        State::Conflicting,
        Origin::Observed,
        Requirement::Required,
    ));
    assert!(!c.finish().authorized());
}

/// Partial coverage is not complete evidence: a response can be current and
/// still omit the position that matters.
#[test]
fn partial_coverage_blocks_required() {
    let mut c = check();
    let mut e = ev(
        Some(1u64),
        State::Present,
        Origin::Observed,
        Requirement::Required,
    );
    e.coverage = Coverage::Partial;
    c.observe(&e);
    let v = c.finish();
    assert!(!v.authorized());
    assert!(matches!(v.blockers[0], Blocker::PartialCoverage { .. }));
}

/// Missing optional enrichment is omitted and earns no clean-history
/// benefit. It must not block.
#[test]
fn missing_optional_omits_without_blocking() {
    let mut c = check();
    c.observe(&ev::<u64>(
        None,
        State::Missing,
        Origin::Observed,
        Requirement::Optional,
    ));
    let v = c.finish();
    assert!(v.authorized(), "optional absence must not block");
    assert_eq!(v.omitted, vec!["e1".to_string()]);
}

/// An observed facility rejects synthetic input outright, even when the
/// value is present, fresh, and complete.
#[test]
fn synthetic_rejected_in_observed_facility() {
    let mut c = EvidenceCheck::new(FacilityPolicy::Observed, 1_000, 3_600);
    c.observe(&ev(
        Some(1u64),
        State::Present,
        Origin::Synthetic,
        Requirement::Required,
    ));
    let v = c.finish();
    assert!(!v.authorized());
    assert!(matches!(
        v.blockers[0],
        Blocker::SyntheticInObservedFacility { .. }
    ));
}

/// The same rule applies to optional enrichment — origin is checked before
/// requirement, so synthetic cannot slip in as a "harmless" extra.
#[test]
fn synthetic_optional_also_rejected_in_observed_facility() {
    let mut c = EvidenceCheck::new(FacilityPolicy::Observed, 1_000, 3_600);
    c.observe(&ev(
        Some(1u64),
        State::Present,
        Origin::Synthetic,
        Requirement::Optional,
    ));
    assert!(!c.finish().authorized());
}

/// A scenario facility accepts synthetic input, but the result is capped at
/// ScenarioOnly no matter what class was requested.
#[test]
fn synthetic_caps_class_in_scenario_facility() {
    let mut c = EvidenceCheck::new(FacilityPolicy::Scenario, 1_000, 3_600);
    c.observe(&ev(
        Some(1u64),
        State::Present,
        Origin::Synthetic,
        Requirement::Required,
    ));
    let v = c.finish();
    assert!(v.authorized(), "scenario facility accepts synthetic input");
    assert!(v.synthetic_used);
    assert_eq!(v.max_class(Class::PrototypeEligible), Class::ScenarioOnly);
}

/// Fully observed evidence keeps the class it asked for.
#[test]
fn observed_evidence_keeps_requested_class() {
    let mut c = check();
    c.observe(&ev(
        Some(1u64),
        State::Present,
        Origin::Observed,
        Requirement::Required,
    ));
    let v = c.finish();
    assert!(v.authorized());
    assert!(!v.synthetic_used);
    assert_eq!(
        v.max_class(Class::PrototypeEligible),
        Class::PrototypeEligible
    );
}

/// A policy assumption is not synthetic evidence: it is a declared input,
/// so it does not cap the class.
#[test]
fn policy_assumption_does_not_cap_class() {
    let mut c = check();
    c.observe(&ev(
        Some(1u64),
        State::Present,
        Origin::PolicyAssumption,
        Requirement::Required,
    ));
    let v = c.finish();
    assert!(v.authorized());
    assert_eq!(
        v.max_class(Class::PrototypeEligible),
        Class::PrototypeEligible
    );
}

/// Evidence with no observation time cannot be shown fresh.
#[test]
fn unknown_observation_time_is_stale() {
    let mut e = ev(
        Some(1u64),
        State::Present,
        Origin::Observed,
        Requirement::Required,
    );
    e.observed_at = None;
    assert!(e.is_stale(1_000, 3_600));
}

/// Every blocker is reported, not just the first — the explanation needs to
/// name all of them.
#[test]
fn all_blockers_are_recorded() {
    let mut c = check();
    let mut a = ev::<u64>(
        None,
        State::Missing,
        Origin::Observed,
        Requirement::Required,
    );
    a.evidence_id = "missing".into();
    let mut b = ev(
        Some(1u64),
        State::Conflicting,
        Origin::Observed,
        Requirement::Required,
    );
    b.evidence_id = "conflicting".into();
    c.observe(&a);
    c.observe(&b);
    assert_eq!(c.finish().blockers.len(), 2);
}
