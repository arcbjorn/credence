//! `evaluate` — the engine's entire public surface.
//!
//! Runs the nine steps of `docs/03` in the fixed order and returns a
//! `RiskAssessment`. Pure: no I/O, no clock, no entropy, no dependence on
//! hash-map iteration. Feed it the same input under the same pinned model
//! and policy and it returns the same bytes.
//!
//! The ordering is not stylistic. Structural validation precedes evidence
//! policy, which precedes risk evaluation, so a malformed snapshot never
//! reaches the scoring path and can never be mistaken for a considered
//! denial.

use serde::{Deserialize, Serialize};

use crate::assessment::{CapacityView, Outcome, ProposedTerms, Reason, RiskAssessment};
use crate::capacity::{self, AgentInputs, ForecastPolicy, GrantDecision, LenderInputs};
use crate::evidence::{Blocker, Class, EvidenceCheck};
use crate::input::{Context, ForecastSummary, HistorySummary, PortfolioContext, RiskInput};
use crate::interest;
use crate::pricing::{self, LossAssumption, PricingPolicy};
use crate::snapshot;
use crate::stress::{self, StressPolicy, StressResult};
use crate::units::{coverage_meets, Amount, Bps, Ratio};

/// Everything the engine needs beyond the snapshot. Pinned and hashed.
#[derive(Clone, Copy, Debug, PartialEq, Eq, Serialize, Deserialize)]
pub struct Policy {
    pub stress: StressPolicy,
    pub pricing: PricingPolicy,
    pub forecast: ForecastPolicy,
    /// Hard ceiling on principal for any one account.
    pub account_hard_cap: Amount,
    /// Base facility for an account with no eligible history.
    pub base_history_cap: Amount,
    /// Additional capacity earned per unit of weighted history.
    pub history_growth_per_ppm: u128,
    /// Maximum evidence age for an executable result.
    pub max_evidence_age_seconds: u64,
    pub funding_rate_bps: Bps,
    pub operating_rate_bps: Bps,
    /// Coverage required to keep an existing loan in good standing.
    pub min_maintenance_coverage: Ratio,
    /// Declared loss assumption, if the policy publishes one.
    pub loss_assumption: Option<LossAssumption>,
}

/// Evaluate one snapshot.
///
/// Never returns an error: an input that cannot be evaluated produces an
/// `Unavailable` assessment carrying its reasons. A caller that must
/// distinguish "no answer" from "no" reads the outcome, not a `Result` —
/// which keeps both paths on the record with the same identity and hash.
pub fn evaluate(input: &RiskInput, policy: &Policy) -> RiskAssessment {
    let identity = snapshot::identify(input);

    // Step 0 — the policy must BE the policy the snapshot names.
    //
    // Identity is computed from the snapshot's labels, so an unverified
    // policy means changing the pricing schedule changes the offered rate
    // while `policy_hash` and `assessment_id` stay identical. Replay would
    // then "match" two assessments produced under different rules, which
    // is precisely the guarantee `docs/04` claims.
    //
    // A declared label that is not a content hash is left alone: some
    // deployments pin policy by identifier elsewhere. What is refused is a
    // label that claims to be a hash and disagrees with the content.
    // The declared label must BE the hash of the policy supplied. A
    // symbolic label proves nothing about which policy ran: the same
    // label over different pricing produces different terms under an
    // identical assessment id, and replay would "verify" two assessments
    // made under different rules.
    //
    // Comparison is on normalized hex, so `0x`-prefixed and bare forms of
    // the same bytes are the same commitment rather than a mismatch.
    match policy_content_hash(policy) {
        Some(actual) if normalize_hash(&input.model_policy.policy_hash) == actual => {}
        _ => {
            return unavailable(
                input,
                &identity,
                vec![Reason::StructurallyInvalid(format!(
                    "policy content does not match declared policy_hash {}",
                    input.model_policy.policy_hash
                ))],
                vec![],
                Class::ScenarioOnly,
                false,
                policy.max_evidence_age_seconds,
            );
        }
    }

    // Step 1 — structure. A malformed snapshot cannot be evaluated at all.
    if let Err(e) = input.validate() {
        return unavailable(
            input,
            &identity,
            vec![Reason::StructurallyInvalid(format!("{e:?}"))],
            vec![],
            Class::ScenarioOnly,
            false,
            policy.max_evidence_age_seconds,
        );
    }

    // Step 1 (cont.) — evidence policy.
    let mut check = EvidenceCheck::new(
        input.facility_policy,
        input.assessment_time,
        policy.max_evidence_age_seconds,
    );
    check.observe(&input.account);
    check.observe(&input.market);
    check.observe(&input.history);
    check.observe(&input.forecasts);
    check.observe(&input.portfolio);
    let verdict = check.finish();

    let class = verdict.max_class(input.requested_class);

    if !verdict.authorized() {
        let reasons = verdict
            .blockers
            .iter()
            .cloned()
            .map(Reason::EvidenceBlocked)
            .collect();
        return unavailable(
            input,
            &identity,
            reasons,
            verdict.omitted.clone(),
            class,
            verdict.synthetic_used,
            policy.max_evidence_age_seconds,
        );
    }

    // Account and market are Required: a usable verdict guarantees them,
    // and their absence is a blocker rather than something to default.
    // Treat a missing value as unavailable anyway rather than panicking --
    // the service has no panic boundary around evaluation.
    let (account, market) = match (input.account.value.as_ref(), input.market.value.as_ref()) {
        (Some(a), Some(m)) => (a, m),
        _ => {
            return unavailable(
                input,
                &identity,
                vec![Reason::EvidenceBlocked(Blocker::MissingRequired {
                    evidence_id: if input.account.value.is_none() {
                        input.account.evidence_id.clone()
                    } else {
                        input.market.evidence_id.clone()
                    },
                    source: "engine".into(),
                })],
                verdict.omitted.clone(),
                class,
                verdict.synthetic_used,
                policy.max_evidence_age_seconds,
            );
        }
    };

    // The rest may legitimately be omitted (`docs/04`), so evaluation
    // proceeds from a conservative stand-in rather than unwrapping. Each
    // default awards nothing for the absence: no history earns no earned
    // capacity, no forecasts take the full penalty, and no portfolio
    // context leaves no headroom.
    let omitted_history = HistorySummary {
        weighted_repayments_ppm: 0,
        resolved_defaults: 0,
        policy_breaches: 0,
        coverage_complete: false,
    };
    // USABLE, not merely present. Stale evidence often retains its last
    // value; using it while reporting it as omitted would make the
    // explanation and the arithmetic disagree, and would let an agent earn
    // a discount from a reading the engine has already disowned.
    let history = usable_or(
        &input.history,
        &omitted_history,
        input.assessment_time,
        policy.max_evidence_age_seconds,
    );

    let omitted_forecasts = ForecastSummary {
        comparable_resolved: 0,
        mean_brier_ppm: u64::MAX,
        unresolved: 0,
        unobservable: 0,
    };
    let forecasts = usable_or(
        &input.forecasts,
        &omitted_forecasts,
        input.assessment_time,
        policy.max_evidence_age_seconds,
    );

    let omitted_portfolio = PortfolioContext {
        strategy_id: String::new(),
        remaining_strategy_budget: Amount::ZERO,
        remaining_vault_budget: Amount::ZERO,
        available_cash: Amount::ZERO,
    };
    let portfolio = usable_or(
        &input.portfolio,
        &omitted_portfolio,
        input.assessment_time,
        policy.max_evidence_age_seconds,
    );

    match &input.context {
        Context::Monitoring { loan } => {
            // Monitoring evaluates the existing loan. It solves no
            // hypothetical new loan and quotes no terms (`docs/03`).
            let accrued = interest::accrued(
                loan.original_principal,
                loan.apr,
                loan.originated_at,
                input.assessment_time.min(loan.maturity_at),
                loan.maturity_at.saturating_sub(loan.originated_at),
            )
            .unwrap_or(Amount::ZERO);
            let unpaid = accrued.saturating_sub(loan.interest_collected);
            let debt = loan
                .outstanding_principal
                .checked_add(unpaid)
                .unwrap_or(Amount(u128::MAX));

            let results = stress::evaluate_all(
                account.settlement_balance,
                &account.positions,
                debt,
                market,
                &policy.stress,
            )
            .unwrap_or_default();

            let marked = stress::recoverable_value(
                account.settlement_balance,
                &account.positions,
                stress::Shock::Liquidity,
                Bps::ZERO,
                market.liquidity_depth,
                &policy.stress,
            )
            .unwrap_or(Amount::ZERO);

            let healthy = coverage_meets(marked, debt, policy.min_maintenance_coverage);

            // Recovery overrides coverage. It is the keeper's process
            // (`docs/02`, `docs/05`), and an account that could keep
            // acting would be racing the liquidation it is being
            // recovered from. Coverage holding at this instant says
            // nothing about whether recovery should be interrupted.
            let (outcome, reasons) = if healthy && !account.restricted && !loan.in_recovery {
                (
                    Outcome::MonitoringAllowed {
                        loan_id: loan.loan_id.clone(),
                    },
                    vec![Reason::Eligible],
                )
            } else {
                let reason = if loan.in_recovery {
                    Reason::LoanInRecovery
                } else if account.restricted {
                    Reason::AccountRestricted
                } else {
                    Reason::MonitoringCoverageBreach {
                        shock: "marked".into(),
                    }
                };
                (
                    Outcome::MonitoringBlocked {
                        loan_id: loan.loan_id.clone(),
                    },
                    vec![reason],
                )
            };

            assemble(
                input,
                &identity,
                outcome,
                results,
                reasons,
                verdict.omitted,
                class,
                verdict.synthetic_used,
                policy.max_evidence_age_seconds,
            )
        }

        Context::Origination { request } => {
            // Step 2 — request against account policy.
            if account.restricted {
                return denied(
                    input,
                    &identity,
                    Some(request.requested_principal),
                    None,
                    vec![Reason::AccountRestricted],
                    verdict.omitted,
                    class,
                    verdict.synthetic_used,
                    policy.max_evidence_age_seconds,
                );
            }
            if request.requested_principal.0 == 0 {
                return denied(
                    input,
                    &identity,
                    Some(Amount::ZERO),
                    None,
                    vec![Reason::ZeroRequest],
                    verdict.omitted,
                    class,
                    verdict.synthetic_used,
                    policy.max_evidence_age_seconds,
                );
            }

            // Steps 3-5 — stress capacity on the post-action portfolio.
            // The borrower's declared action shapes the portfolio being
            // assessed; assuming a cash hold would understate real risk.
            let action = request.intended_action.clone();
            let indicative_apr = policy.pricing.max_apr_bps;
            let stress_capacity = stress::stress_capacity(
                account,
                &action,
                // Size against the worst rate the schedule can produce, so
                // the reserve is never understated before pricing runs.
                indicative_apr,
                request.term_seconds,
                market,
                &policy.stress,
                // Search above the hard cap so a clamp is distinguishable
                // from a genuine stress limit. Searching only to the cap
                // makes every well-collateralised account report
                // StressCapacity when the cap is what actually binds —
                // naming the wrong constraint in the explanation.
                stress_search_ceiling(policy.account_hard_cap),
            );

            // Step 6 — history, caps.
            // Earned rather than reported: unproven coverage and past
            // adverse outcomes both reduce what the raw weight claims.
            let earned_ppm = capacity::earned_repayment_ppm(
                history.weighted_repayments_ppm,
                history.coverage_complete,
                history.resolved_defaults,
                history.policy_breaches,
            );
            let history_growth_cap = Amount(policy.base_history_cap.0.saturating_add(
                (earned_ppm as u128).saturating_mul(policy.history_growth_per_ppm),
            ));

            // Step 7 — forecast cap.
            let penalty = capacity::forecast_penalty_bps(
                &policy.forecast,
                forecasts.comparable_resolved,
                forecasts.mean_brier_ppm,
            );
            let forecast_cap = capacity::forecast_cap(policy.account_hard_cap, penalty);

            let available_credit = account
                .facility_limit
                .saturating_sub(account.outstanding_debt);
            let headroom = stress::debt_headroom_capacity(
                available_credit,
                indicative_apr,
                request.term_seconds,
            );

            let composed = capacity::compose(
                AgentInputs {
                    stress_capacity,
                    history_growth_cap,
                    account_hard_cap: policy.account_hard_cap,
                    forecast_cap,
                },
                LenderInputs {
                    debt_headroom_capacity: headroom,
                    remaining_strategy_budget: portfolio.remaining_strategy_budget,
                    remaining_vault_budget: portfolio.remaining_vault_budget,
                    available_cash: portfolio.available_cash,
                },
            );
            let score = capacity::score(composed.agent_capacity, policy.account_hard_cap);
            let view = CapacityView::from_result(&composed, score);

            // Evidence for a DENIAL: the portfolio as it stands, which is
            // what a reader needs to see when no draw will happen. An
            // approval replaces this below with the post-draw projection,
            // because that is the portfolio the decision actually
            // authorizes (`docs/04`).
            let results = stress::evaluate_all(
                account.settlement_balance,
                &account.positions,
                account.outstanding_debt,
                market,
                &policy.stress,
            )
            .unwrap_or_default();

            // Step 9a — capacity decision.
            let granted = match capacity::decide_grant(request.requested_principal, &composed) {
                GrantDecision::Approve { principal } => principal,
                GrantDecision::Deny { limiting, capacity } => {
                    return denied(
                        input,
                        &identity,
                        Some(request.requested_principal),
                        Some(view),
                        vec![Reason::CapacityExceeded { limiting, capacity }],
                        verdict.omitted,
                        class,
                        verdict.synthetic_used,
                        policy.max_evidence_age_seconds,
                    );
                }
                GrantDecision::DenyZeroRequest => {
                    return denied(
                        input,
                        &identity,
                        Some(Amount::ZERO),
                        Some(view),
                        vec![Reason::ZeroRequest],
                        verdict.omitted,
                        class,
                        verdict.synthetic_used,
                        policy.max_evidence_age_seconds,
                    );
                }
            };

            // Step 8 — price. Runs after capacity, so a rate can never
            // manufacture eligibility.
            let history_quality_ppm = history.weighted_repayments_ppm.min(1_000_000);
            let portfolio_risk_ppm = portfolio_risk(account, market);
            let breakdown =
                pricing::price(&policy.pricing, history_quality_ppm, portfolio_risk_ppm);

            if let Err(pricing::PricingRefusal::ExceedsBorrowerMaximum {
                offered_bps,
                maximum_bps,
            }) = pricing::check_borrower_maximum(breakdown.apr_bps, request.max_accepted_apr)
            {
                return denied(
                    input,
                    &identity,
                    Some(request.requested_principal),
                    Some(view),
                    vec![Reason::PriceExceedsBorrowerMaximum {
                        offered_bps,
                        maximum_bps,
                    }],
                    verdict.omitted,
                    class,
                    verdict.synthetic_used,
                    policy.max_evidence_age_seconds,
                );
            }

            // Re-verify at the actual rate. The capacity above was sized at
            // the schedule's maximum, so the real rate can only be safer —
            // but the requested portfolio is rechecked exactly, because a
            // capacity estimate does not authorize an allocation (`docs/03`).
            if !stress::principal_is_safe(
                account,
                &action,
                granted,
                breakdown.apr_bps,
                request.term_seconds,
                market,
                &policy.stress,
            ) {
                return denied(
                    input,
                    &identity,
                    Some(request.requested_principal),
                    Some(view),
                    vec![Reason::CapacityExceeded {
                        limiting: composed.agent_binding,
                        capacity: composed.agent_capacity,
                    }],
                    verdict.omitted,
                    class,
                    verdict.synthetic_used,
                    policy.max_evidence_age_seconds,
                );
            }

            // Report the portfolio this grant produces, at the rate it
            // was actually priced at. The same projection the safety
            // recheck just ran, so the displayed cases and the decision
            // describe one portfolio rather than two.
            let results = match stress::project(
                account,
                &action,
                granted,
                breakdown.apr_bps,
                request.term_seconds,
            ) {
                Some(p) => {
                    stress::evaluate_all(p.settlement, &p.positions, p.debt, market, &policy.stress)
                        .unwrap_or(results)
                }
                None => results,
            };

            let econ = pricing::economics(
                granted,
                breakdown.apr_bps,
                request.term_seconds,
                policy.funding_rate_bps,
                policy.operating_rate_bps,
                policy.loss_assumption,
            );
            let Some(economics) = econ else {
                return unavailable(
                    input,
                    &identity,
                    vec![Reason::StructurallyInvalid("economics overflow".into())],
                    verdict.omitted,
                    class,
                    verdict.synthetic_used,
                    policy.max_evidence_age_seconds,
                );
            };

            let outcome = Outcome::ApprovedOrigination {
                requested_principal: request.requested_principal,
                approved_principal: granted,
                terms: ProposedTerms {
                    approved_principal: granted,
                    apr_bps: breakdown.apr_bps,
                    term_seconds: request.term_seconds,
                    required_equity: account.settlement_balance,
                    permitted_action_hash: request.action_envelope_hash.clone(),
                    price_breakdown: breakdown,
                    economics,
                },
                capacity: view,
            };

            assemble(
                input,
                &identity,
                outcome,
                results,
                vec![Reason::Eligible],
                verdict.omitted,
                class,
                verdict.synthetic_used,
                policy.max_evidence_age_seconds,
            )
        }
    }
}

/// Canonical hash of the policy's CONTENT.
///
/// None when the policy cannot be encoded, which the caller treats as
/// "cannot verify" rather than "verified".
fn policy_content_hash(policy: &Policy) -> Option<String> {
    let value = serde_json::to_value(policy).ok()?;
    crate::canonical::hash_hex(&value).ok()
}

/// A declared policy hash in canonical form: bare, lowercase hex.
///
/// `0x`-prefixed and bare spellings of the same bytes denote the same
/// policy, so they must compare equal. A label that is not hex at all
/// normalizes to itself and simply fails the comparison, which is the
/// intended outcome: an unresolvable label cannot commit to anything.
fn normalize_hash(s: &str) -> String {
    s.strip_prefix("0x").unwrap_or(s).to_ascii_lowercase()
}

/// The evidence's value when it is usable, and the conservative stand-in
/// otherwise.
///
/// `docs/04`: omitted evidence earns no benefit for its absence. Evidence
/// that is Stale or incompletely covered is omitted even when it still
/// carries a value, so the check is usability rather than presence.
fn usable_or<'a, T>(
    ev: &'a crate::evidence::Evidence<T>,
    fallback: &'a T,
    now: u64,
    max_age_seconds: u64,
) -> &'a T {
    // Fresh as well as usable. A stale reading often retains its value,
    // and consuming it would let expired enrichment improve terms while
    // the verdict reports it omitted -- the arithmetic and the
    // explanation must agree (`docs/04`).
    if ev.is_usable() && !ev.is_stale(now, max_age_seconds) {
        ev.value.as_ref().unwrap_or(fallback)
    } else {
        fallback
    }
}

/// Ceiling for the stress search.
///
/// Deliberately above the hard cap: the search must be able to report a
/// capacity the cap then clamps, or `compose` cannot tell which of the two
/// bound the result.
fn stress_search_ceiling(hard_cap: Amount) -> Amount {
    Amount(hard_cap.0.saturating_mul(4))
}

/// Portfolio risk as the share of marked value held in risky positions.
/// A policy-derived signal, not a measured probability.
fn portfolio_risk(
    account: &crate::input::AccountState,
    _market: &crate::input::MarketState,
) -> u64 {
    let mut risky = 0u128;
    for p in &account.positions {
        let scale = 10u128.pow(p.price_scale);
        risky = risky.saturating_add(p.amount.0.saturating_mul(p.price.0) / scale);
    }
    let total = risky.saturating_add(account.settlement_balance.0);
    if total == 0 {
        return 0;
    }
    ((risky.saturating_mul(1_000_000)) / total) as u64
}

type Identity = Result<snapshot::SnapshotIdentity, snapshot::SnapshotError>;

fn ids(identity: &Identity) -> (String, String) {
    match identity {
        Ok(i) => (i.assessment_id.clone(), i.input_hash.clone()),
        // An input that cannot be encoded cannot be identified. Empty
        // strings are visibly wrong rather than plausibly right.
        Err(_) => (String::new(), String::new()),
    }
}

#[allow(clippy::too_many_arguments)]
fn assemble(
    input: &RiskInput,
    identity: &Identity,
    outcome: Outcome,
    stress_results: Vec<StressResult>,
    reasons: Vec<Reason>,
    omitted: Vec<String>,
    class: Class,
    synthetic_used: bool,
    max_evidence_age_seconds: u64,
) -> RiskAssessment {
    let (assessment_id, input_hash) = ids(identity);

    // Permission may not outlive the evidence behind it (`docs/04`). The
    // requested window is a ceiling, not a grant: whichever comes first,
    // the request's own expiry or the moment the oldest required
    // observation goes stale, ends the authorization.
    // Only an EXECUTABLE result is bounded by its evidence. A revocation
    // authorizes nothing, so shortening its window cannot make anything
    // safer -- it only risks producing a blocked result that expired
    // before it could be delivered, which the oracle rejects outright and
    // which therefore never revokes anything.
    let grants = matches!(
        outcome,
        Outcome::ApprovedOrigination { .. } | Outcome::MonitoringAllowed { .. }
    );
    let valid_until = if grants {
        match input.required_evidence_deadline(max_evidence_age_seconds) {
            Some(deadline) => input.proposal.valid_until.min(deadline),
            // No required observation time to reason from. Nothing can
            // vouch for freshness, so the window closes at the assessment
            // itself rather than standing open on an unknown basis.
            None => input.proposal.valid_until.min(input.assessment_time),
        }
    } else {
        input.proposal.valid_until
    };
    RiskAssessment {
        schema_version: input.schema_version.clone(),
        assessment_id,
        input_hash,
        model_hash: input.model_policy.model_hash.clone(),
        policy_hash: input.model_policy.policy_hash.clone(),
        assessment_time: input.assessment_time,
        sequence: input.proposal.sequence,
        valid_after: input.proposal.valid_after,
        valid_until,
        authorization_class: class,
        outcome,
        stress_results,
        reasons,
        omitted_evidence: omitted,
        synthetic_used,
    }
}

#[allow(clippy::too_many_arguments)]
fn denied(
    input: &RiskInput,
    identity: &Identity,
    requested: Option<Amount>,
    capacity: Option<CapacityView>,
    reasons: Vec<Reason>,
    omitted: Vec<String>,
    class: Class,
    synthetic_used: bool,
    max_evidence_age_seconds: u64,
) -> RiskAssessment {
    assemble(
        input,
        identity,
        Outcome::Denied {
            requested_principal: requested,
            capacity,
        },
        vec![],
        reasons,
        omitted,
        class,
        synthetic_used,
        max_evidence_age_seconds,
    )
}

fn unavailable(
    input: &RiskInput,
    identity: &Identity,
    reasons: Vec<Reason>,
    omitted: Vec<String>,
    class: Class,
    synthetic_used: bool,
    max_evidence_age_seconds: u64,
) -> RiskAssessment {
    assemble(
        input,
        identity,
        Outcome::Unavailable,
        vec![],
        reasons,
        omitted,
        class,
        synthetic_used,
        max_evidence_age_seconds,
    )
}
