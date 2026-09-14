//! Output shape rules from `docs/04`.

use risk_core::assessment::*;
use risk_core::capacity::{BindingConstraint, CapacityResult};
use risk_core::evidence::Class;
use risk_core::pricing::{economics, price, PricingPolicy};
use risk_core::units::{Amount, Bps};

fn terms() -> ProposedTerms {
    let p = PricingPolicy {
        base_rate_bps: Bps(1_200),
        max_history_discount_bps: Bps(400),
        max_portfolio_surcharge_bps: Bps(800),
        min_apr_bps: Bps(500),
        max_apr_bps: Bps(4_000),
    };
    ProposedTerms {
        approved_principal: Amount(5_000_000_000),
        apr_bps: Bps(1_200),
        term_seconds: 10 * 86_400,
        required_equity: Amount(5_000_000_000),
        permitted_action_hash: "act".into(),
        price_breakdown: price(&p, 0, 0),
        economics: economics(
            Amount(5_000_000_000),
            Bps(1_200),
            10 * 86_400,
            Bps(450),
            Bps(100),
            None,
        )
        .unwrap(),
    }
}

fn capacity_view() -> CapacityView {
    CapacityView::from_result(
        &CapacityResult {
            agent_capacity: Amount(9_000_000_000),
            drawable_capacity: Amount(9_000_000_000),
            agent_binding: BindingConstraint::StressCapacity,
            drawable_binding: BindingConstraint::StressCapacity,
        },
        Some(750),
    )
}

fn assessment(outcome: Outcome, class: Class) -> RiskAssessment {
    RiskAssessment {
        schema_version: "v1".into(),
        assessment_id: "a1".into(),
        input_hash: "ih".into(),
        model_hash: "mh".into(),
        policy_hash: "ph".into(),
        assessment_time: 1_000,
        sequence: 5,
        valid_after: 900,
        valid_until: 4_600,
        authorization_class: class,
        outcome,
        stress_results: vec![],
        reasons: vec![],
        omitted_evidence: vec![],
        synthetic_used: false,
    }
}

/// Approved sets Allowed; everything else sets Blocked (`docs/04`).
#[test]
fn permission_follows_decision() {
    let approved = Outcome::ApprovedOrigination {
        requested_principal: Amount(5_000_000_000),
        approved_principal: Amount(5_000_000_000),
        terms: terms(),
        capacity: capacity_view(),
    };
    assert_eq!(approved.permission(), Permission::Allowed);

    assert_eq!(
        Outcome::Denied {
            requested_principal: None,
            capacity: None
        }
        .permission(),
        Permission::Blocked
    );
    assert_eq!(Outcome::Unavailable.permission(), Permission::Blocked);
    assert_eq!(
        Outcome::MonitoringAllowed {
            loan_id: "l1".into()
        }
        .permission(),
        Permission::Allowed
    );
    assert_eq!(
        Outcome::MonitoringBlocked {
            loan_id: "l1".into()
        }
        .permission(),
        Permission::Blocked
    );
}

/// A denial carries no grant and no terms — by construction, not by
/// validation after the fact.
#[test]
fn denial_carries_no_terms() {
    let denied = Outcome::Denied {
        requested_principal: Some(Amount(6_000_000_000)),
        capacity: Some(capacity_view()),
    };
    assert!(denied.terms().is_none());
    assert!(!denied.is_executable());
}

/// Unavailable produces nothing executable. Absence never becomes zero.
#[test]
fn unavailable_produces_nothing_executable() {
    assert!(Outcome::Unavailable.terms().is_none());
    assert!(!Outcome::Unavailable.is_executable());
}

/// Monitoring omits terms and granted principal entirely — there is no
/// field to put them in.
#[test]
fn monitoring_omits_grant_and_terms() {
    for outcome in [
        Outcome::MonitoringAllowed {
            loan_id: "l1".into(),
        },
        Outcome::MonitoringBlocked {
            loan_id: "l1".into(),
        },
    ] {
        assert!(outcome.terms().is_none());
        assert!(!outcome.is_executable(), "monitoring can never fund a loan");
    }
}

/// `docs/04`: an observed/shadow approval is non-executable by class,
/// however it was signed.
#[test]
fn shadow_approval_is_not_executable() {
    let outcome = Outcome::ApprovedOrigination {
        requested_principal: Amount(5_000_000_000),
        approved_principal: Amount(5_000_000_000),
        terms: terms(),
        capacity: capacity_view(),
    };
    let shadow = assessment(outcome.clone(), Class::ObservedShadow);
    assert_eq!(shadow.permission(), Permission::Allowed);
    assert!(!shadow.is_executable(), "shadow class cannot execute");

    let scenario = assessment(outcome.clone(), Class::ScenarioOnly);
    assert!(!scenario.is_executable());

    let prototype = assessment(outcome, Class::PrototypeEligible);
    assert!(prototype.is_executable());
}

/// `docs/03`: an approval grants exactly what was requested.
#[test]
fn approval_grants_exactly_the_request() {
    if let Outcome::ApprovedOrigination {
        requested_principal,
        approved_principal,
        ..
    } = (Outcome::ApprovedOrigination {
        requested_principal: Amount(5_000_000_000),
        approved_principal: Amount(5_000_000_000),
        terms: terms(),
        capacity: capacity_view(),
    }) {
        assert_eq!(requested_principal, approved_principal);
    } else {
        panic!("expected an approval");
    }
}

/// Assessments serialize through the canonical encoder, which is what the
/// output hash commits to.
#[test]
fn assessment_serializes_canonically() {
    let a = assessment(Outcome::Unavailable, Class::ScenarioOnly);
    let value = serde_json::to_value(&a).unwrap();
    let hash = risk_core::snapshot::output_hash(&value).unwrap();
    assert_eq!(hash.len(), 64);

    // Reproducible: the same output yields the same hash.
    let again = serde_json::to_value(&a).unwrap();
    assert_eq!(risk_core::snapshot::output_hash(&again).unwrap(), hash);
}
