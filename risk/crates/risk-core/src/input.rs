//! `RiskInput` — the engine's only input.
//!
//! Two properties matter more than the field list:
//!
//! 1. **The kinds are mutually exclusive.** An origination carries a
//!    borrower request; a monitoring assessment carries an active loan.
//!    `docs/04` states monitoring "needs no borrower request and cannot
//!    carry an origination quote". Modelled as an enum so the invalid
//!    combination cannot be constructed, rather than validated after the
//!    fact and forgotten at one call site.
//!
//! 2. **Time and sequence are inputs, not reads.** `docs/06` forbids clock
//!    access in the engine, so the gateway proposes both and the engine
//!    validates them.

use serde::{Deserialize, Serialize};

use crate::evidence::{Class, Evidence, FacilityPolicy};
use crate::units::{Amount, Bps};

/// Identifies an agent. Never a bare integer (`docs/06`).
#[derive(Clone, Debug, PartialEq, Eq, Serialize, Deserialize)]
pub struct AgentRef {
    pub identity_registry_chain_id: u64,
    pub identity_registry: String,
    pub agent_id: String,
}

/// The tuple financing attaches to.
#[derive(Clone, Debug, PartialEq, Eq, Serialize, Deserialize)]
pub struct Binding {
    pub agent: AgentRef,
    pub operator: String,
    pub credit_account: String,
    pub facility_id: String,
    pub account_state_version: u64,
    pub config_version: u64,
    pub config_hash: String,
    pub policy_epoch: u64,
    pub policy_hash: String,
    /// Highest sequence the chain has accepted for this binding.
    pub accepted_sequence: u64,
}

/// A borrower's request. Origination only.
#[derive(Clone, Debug, PartialEq, Eq, Serialize, Deserialize)]
pub struct Request {
    pub request_id: String,
    pub settlement_asset: String,
    pub requested_principal: Amount,
    pub term_seconds: u64,
    /// The borrower declines above this rate; the engine must not price past it.
    pub max_accepted_apr: Bps,
    pub action_envelope_hash: String,
    pub intent_hash: String,
    /// What the principal will be used for. Capacity is solved on the
    /// **post-action** portfolio, so this is an input to the decision, not
    /// a note attached to it (`docs/03`).
    pub intended_action: crate::stress::IntendedAction,
}

/// An outstanding obligation. Monitoring only.
#[derive(Clone, Debug, PartialEq, Eq, Serialize, Deserialize)]
pub struct ActiveLoan {
    pub loan_id: String,
    pub settlement_asset: String,
    pub original_principal: Amount,
    pub outstanding_principal: Amount,
    /// Fixed at origination. A monitoring result never reprices it.
    pub apr: Bps,
    pub originated_at: u64,
    pub maturity_at: u64,
    pub in_recovery: bool,
    pub interest_collected: Amount,
}

/// What this assessment is for. The two contexts cannot coexist.
#[derive(Clone, Debug, PartialEq, Eq, Serialize, Deserialize)]
#[serde(tag = "kind")]
pub enum Context {
    Origination { request: Request },
    Monitoring { loan: ActiveLoan },
}

impl Context {
    pub fn is_origination(&self) -> bool {
        matches!(self, Context::Origination { .. })
    }
}

/// One held asset.
#[derive(Clone, Debug, PartialEq, Eq, Serialize, Deserialize)]
pub struct Position {
    pub asset: String,
    pub amount: Amount,
    /// Price in settlement-asset base units, scaled by `price_scale`.
    pub price: Amount,
    pub price_scale: u32,
    /// Fraction of value assumed unrecoverable at liquidation, in bps.
    pub haircut_bps: Bps,
}

/// Account state. Coverage must be complete for an executable decision.
#[derive(Clone, Debug, PartialEq, Eq, Serialize, Deserialize)]
pub struct AccountState {
    pub settlement_balance: Amount,
    pub positions: Vec<Position>,
    pub state_version: u64,
    /// All debt in this facility, not only the loan under assessment.
    pub outstanding_debt: Amount,
    pub facility_limit: Amount,
    pub restricted: bool,
}

/// Market conditions backing the stress scenarios.
#[derive(Clone, Debug, PartialEq, Eq, Serialize, Deserialize)]
pub struct MarketState {
    pub observed_at: u64,
    /// Realized volatility over a declared window, in bps.
    pub realized_volatility_bps: Bps,
    pub volatility_window_seconds: u64,
    /// Depth available for liquidation, in settlement-asset units.
    pub liquidity_depth: Amount,
}

/// Repayment history, already weighted by exposure and duration (`docs/06`).
#[derive(Clone, Debug, Default, PartialEq, Eq, Serialize, Deserialize)]
pub struct HistorySummary {
    /// Sum of `(principal / reference) * min(eligible_seconds / 30d, 1)`.
    pub weighted_repayments_ppm: u64,
    pub resolved_defaults: u32,
    pub policy_breaches: u32,
    /// False when history could not be fully enumerated. An incomplete
    /// history is not a clean one.
    pub coverage_complete: bool,
}

/// Forecast record. Insufficient samples take the maximum penalty.
#[derive(Clone, Debug, Default, PartialEq, Eq, Serialize, Deserialize)]
pub struct ForecastSummary {
    pub comparable_resolved: u32,
    pub mean_brier_ppm: u64,
    pub unresolved: u32,
    pub unobservable: u32,
}

/// Vault-wide context. Per-agent limits alone do not address many agents
/// following one strategy (`docs/03`).
#[derive(Clone, Debug, PartialEq, Eq, Serialize, Deserialize)]
pub struct PortfolioContext {
    pub strategy_id: String,
    pub remaining_strategy_budget: Amount,
    pub remaining_vault_budget: Amount,
    pub available_cash: Amount,
}

/// Pinned model and policy identity.
#[derive(Clone, Debug, PartialEq, Eq, Serialize, Deserialize)]
pub struct ModelPolicy {
    pub model_id: String,
    pub model_hash: String,
    pub policy_id: String,
    pub policy_hash: String,
}

/// Sequence and validity the gateway proposes; the engine validates them.
#[derive(Clone, Debug, PartialEq, Eq, Serialize, Deserialize)]
pub struct Proposal {
    pub sequence: u64,
    pub valid_after: u64,
    pub valid_until: u64,
}

/// The complete input.
#[derive(Clone, Debug, PartialEq, Eq, Serialize, Deserialize)]
pub struct RiskInput {
    pub schema_version: String,
    pub facility_policy: FacilityPolicy,
    pub requested_class: Class,
    /// Explicit — the engine never reads a clock.
    pub assessment_time: u64,
    pub scenario_id: Option<String>,
    pub proposal: Proposal,
    pub binding: Binding,
    pub context: Context,
    pub account: Evidence<AccountState>,
    pub market: Evidence<MarketState>,
    pub history: Evidence<HistorySummary>,
    pub forecasts: Evidence<ForecastSummary>,
    pub portfolio: Evidence<PortfolioContext>,
    pub model_policy: ModelPolicy,
}

/// Structural problems detected before any risk evaluation.
#[derive(Clone, Debug, PartialEq, Eq, Serialize, Deserialize)]
pub enum InputError {
    /// `docs/04`: reject duplicate evidence IDs.
    DuplicateEvidenceId(String),
    /// The proposed sequence must exceed what the chain already accepted.
    SequenceNotIncreasing { accepted: u64, proposed: u64 },
    /// A window that has already closed cannot authorize anything.
    InvalidValidityWindow { valid_after: u64, valid_until: u64 },
    /// Assessment time outside its own declared window.
    AssessmentTimeOutsideWindow { time: u64, valid_after: u64, valid_until: u64 },
    /// The account the binding names is not the account described.
    AccountStateVersionMismatch { binding: u64, account: u64 },
    /// A scenario identity is required when scenario evidence is in play.
    ScenarioIdMissing,
    /// The snapshot declares a schema this engine does not implement.
    UnsupportedSchemaVersion(String),
}

/// Schema majors this engine understands.
///
/// Checked rather than trusted: a snapshot written against a future schema
/// may carry fields with different meanings, and evaluating it anyway
/// would produce a confident answer to a question that was never asked.
pub const SUPPORTED_SCHEMA_VERSIONS: &[&str] = &["v1"];

impl RiskInput {
    /// Structural validation, before any risk logic runs.
    ///
    /// These are not risk judgments — they are reasons the input cannot be
    /// evaluated at all. Keeping them separate means a malformed snapshot
    /// never reaches the scoring path and cannot be confused with a denial.
    pub fn validate(&self) -> Result<(), InputError> {
        if !SUPPORTED_SCHEMA_VERSIONS.contains(&self.schema_version.as_str()) {
            return Err(InputError::UnsupportedSchemaVersion(
                self.schema_version.clone(),
            ));
        }

        let mut ids = Vec::new();
        for id in [
            &self.account.evidence_id,
            &self.market.evidence_id,
            &self.history.evidence_id,
            &self.forecasts.evidence_id,
            &self.portfolio.evidence_id,
        ] {
            if ids.contains(&id) {
                return Err(InputError::DuplicateEvidenceId(id.clone()));
            }
            ids.push(id);
        }

        if self.proposal.sequence <= self.binding.accepted_sequence {
            return Err(InputError::SequenceNotIncreasing {
                accepted: self.binding.accepted_sequence,
                proposed: self.proposal.sequence,
            });
        }

        if self.proposal.valid_until <= self.proposal.valid_after {
            return Err(InputError::InvalidValidityWindow {
                valid_after: self.proposal.valid_after,
                valid_until: self.proposal.valid_until,
            });
        }

        if self.assessment_time < self.proposal.valid_after
            || self.assessment_time >= self.proposal.valid_until
        {
            return Err(InputError::AssessmentTimeOutsideWindow {
                time: self.assessment_time,
                valid_after: self.proposal.valid_after,
                valid_until: self.proposal.valid_until,
            });
        }

        // Only checked when the account is actually present: a missing
        // account is an evidence blocker, not a structural mismatch.
        if let Some(account) = &self.account.value {
            if account.state_version != self.binding.account_state_version {
                return Err(InputError::AccountStateVersionMismatch {
                    binding: self.binding.account_state_version,
                    account: account.state_version,
                });
            }
        }

        if self.facility_policy == FacilityPolicy::Scenario && self.scenario_id.is_none() {
            return Err(InputError::ScenarioIdMissing);
        }

        Ok(())
    }
}
