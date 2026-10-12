# 05 · Contracts

## Responsibilities

| Contract | Responsibility |
|---|---|
| `CreditVault` | Fixed-epoch deposits, available cash, receivables, collections, realized losses, final settlement |
| `LoanManager` | Origination, aggregate exposure, interest, maturity, repayment, recovery, final resolution |
| `CreditAccount` | Custody, bound operator, supported assets, restricted actions, residual equity return |
| `StrategyAdapter` | One verified route, fixed receiver, bounded approvals, slippage and output checks |
| `RiskOracle` | Origination quotes and monitoring permission, shared sequence, policy binding, evidence class, facility-specific authorization route |
| `PriceSource` | Fresh valuation required for risk-increasing operations and bounded closeout |
| `IdentityAdapter` | Registry references, configuration continuity; authoritative debt stays in `LoanManager` |

**Non-upgradeable.** A changed implementation gets a new deployment manifest and a new assessment domain.

## Vault accounting — fixed epoch

A fixed epoch avoids building instantaneous redemption or fair-value share pricing while loans are outstanding.

```text
Funding ──► Active ──► Resolving ──► Settled
   │           │           │            │
 deposits   lending    no new       pro-rata
 at par     within     lending      withdrawal of
            global cap              ACTUAL final cash
```

Separate ledgers for idle cash, outstanding principal, realized losses, and collected interest. **Uncollected interest is never withdrawable cash.** Integer rounding and residual dust allocation must be specified and tested; a final claimant may take remaining dust via a remaining-cash / remaining-shares calculation.

Lenders have a **defined lockup**. The UI must not offer immediate withdrawals backed by outstanding loans. There is no guaranteed settlement date when assets cannot be recovered.

## Loan state machine

```text
NoLoan ── valid assessment + equity + funding ──► Active
Active ── full repayment ───────────────────────► Repaid
Active ── health breach or maturity ────────────► RecoveryPending
RecoveryPending ── full debt collected ─────────► Recovered
RecoveryPending ── assets exhausted + shortfall ► Defaulted
```

Terminal states leave the loss record permanently attributable to the original binding. A binding with an unresolved default flag receives no new loans. One active loan per account. **Collection accounting is idempotent** across multiple recovery transactions.

**If liquidity disappears and assets remain unsold, the loan stays `RecoveryPending`.** Do not mark it resolved because a timeout elapsed or an optimistic price exists. The epoch cannot claim full settlement while a material receivable is unresolved.

## Origination sequence

1. Agent commits intent and forecast definition **before** funding
2. Go snapshots account, facility, policy, evidence at a specified state
3. Rust returns an `Origination` assessment; only approval contains a grant and proposed terms
4. The facility's required authorization adapter validates and records the assessment
5. Borrower accepts terms and requests the exact approved draw matching its committed request and action envelope
6. `LoanManager` checks binding, account state, sequence, expiry, evidence class, equity, debt, aggregate limits, cash, prices, epoch state
7. **Transaction consumes the quote and creates the debt before any untrusted external interaction**
8. Vault transfers principal only to the bound `CreditAccount`
9. Account executes only the permitted initial action, or holds settlement tokens

Where an initial trade is part of the approval, **fund and trade atomically**; revert everything if post-action checks fail. A quote for a conservative allocation cannot fund a different allocation.

`LoanManager.originateAndExecute` is that path: it consumes the quote, creates the debt, funds the account, and performs the committed action in one transaction. A failing action reverts the draw with it, so no funded loan survives an action that never happened. `originate` remains for a quote whose approved action is to hold settlement tokens, where there is nothing to perform.

Atomicity closes the window a two-transaction origination would leave, in which a borrower could draw and never execute, holding settlement tokens the stress scenarios assume are deployed.

`approved_principal` must equal the positive `requested_principal`; a smaller or larger draw cannot consume that quote. A request above current capacity is denied with no proposed terms. Changing the amount or intended allocation requires a new commitment and assessment. The borrower may decline an approved quote without consuming it.

## Custody rules

- Neither agent nor owner withdraws while debt is active or recovering
- No general `call`, `delegatecall`, unrestricted approval, or rescue route bypasses the debt gate
- Targets, selectors, assets, spender, recipient fixed or adapter-constrained; outputs return to the account
- Approvals bounded to the operation and cleaned up
- **Slippage checked against a permitted valuation source, not only caller-supplied `minOut`**
- **A whitelisted venue is not sufficient** — a bad-price trade transfers value to a cooperating counterparty. Test this explicitly
- Execution increments account state version, invalidating stale action assumptions
- After origination, risk-increasing actions require current allowed monitoring permission for that state version, plus independent price, action, and health checks
- Configuration changes require no outstanding debt (v0 rule)

## Authorization envelopes

The development signer uses EIP-712 typed messages. The CRE receiver verifies its expected forwarder and workflow identity, then normalizes its report into the same typed fields. The facility pins **one required adapter route** for the epoch: `Development` or `CRE`. Signer allowlists are scoped within that route; a development key is never a substitute in a CRE facility. Changing an existing facility's route requires a new epoch after its obligations resolve; a separate deployment uses a distinct authorization domain.

Common fields for both assessment kinds:

```text
chain_id, verifying_contract, deployment_id, evidence_policy_id, authorization_route
identity_registry_chain_id, identity_registry, agent_id, operator, credit_account, facility_id
account_state_version, config_version, config_hash, policy_epoch, policy_hash
assessment_kind, assessment_id, assessment_hash, decision, risk_permission
model_hash, input_hash, evidence_root
authorization_class, sequence, valid_after, valid_until
```

Kind-specific fields:

| Kind | Additional fields | Permitted effect |
|---|---|---|
| `Origination` | `request_id`, `intent_hash`, `settlement_asset`, `requested_principal`; on approval only: `approved_principal`, `apr_bps`, `term_seconds`, `required_equity`, `permitted_action_hash` | An approved, eligible quote may be consumed once for the exact requested draw |
| `Monitoring` | `loan_id`, `settlement_asset`; no request, grant, proposed terms, or action authorization | Updates risk permission for the existing loan; can never disburse |

`policy_epoch` identifies the immutable facility policy record, and `policy_hash` must equal that record's hash. `config_version` identifies the binding's configuration record, and `config_hash` must equal its canonical content hash. Both identifiers and hashes are signed and checked; neither pair is an alias for the other. Bind the original identity registry namespace and equity recipient in the loan record as described in [06 · Identity & Forecasts](06-identity-forecasts.md).

Use a **single strictly increasing sequence per binding across both kinds**. Every accepted newer assessment, including `Denied`, `Unavailable`, or monitoring, supersedes unused older quotes. Reject grant fields on a non-approved result and reject them on every monitoring result. Replayed, expired, cross-chain, cross-deployment, wrong-route, wrong-policy, and wrong-account attestations fail. Shadow assessments cannot update an executable facility.

Monitoring results with valid bindings may record `Blocked` permission even when required market evidence is stale or missing. Results with invalid bindings are audit records only. Expired, absent, blocked, or state-version-mismatched monitoring permission prevents risk-increasing actions; repayment and permitted recovery remain open. The initial atomic action is covered by the consumed origination quote. A monitoring approval does not replace a new origination quote after repayment.

**Current contract state is still checked at draw time.** A correctly signed assessment is not permission to ignore a vault cap or an open loan.

### What the account enforces per action

The adapter allowlist says a route is *known*. It says nothing about whether
acting now is safe, which is the engine's judgment. While debt is
outstanding, `execute` additionally requires:

| Check | Why |
|---|---|
| Not in recovery | An owner or agent who could keep trading would race the liquidation they are being recovered from |
| Action matches `permitted_action_hash` | Authorization is for *that* call, not for any call through the same adapter |
| Standing permission, after the first action | The origination assessment authorizes one atomic action; the commitment is consumed, so a repeat is a new risk decision |

Two deliberate exemptions:

- **An account with no debt is unrestricted.** There is no lender exposure
  to protect, and requiring a quote to move one's own equity would make the
  account unusable before its first loan.
- **Risk-*reducing* entry points need no permission.** Invariant 3
  constrains actions that increase risk. A gate that blocked de-risking
  would trap an account in the position a reassessment is telling it to
  close. These are registered by selector, by the operator — never by the
  agent, since mislabelling a risk-increasing call would exempt it
  entirely.

The binding the account checks against is pinned by the loan manager at
origination, from the assessment it consumed. The account never trusts a
caller-supplied binding: the standing-quote key covers only the stable
identity (registry, agent, account, facility), so a quote additionally
commits to the hash of the **complete** signed binding. Without that,
rewriting the unsigned fields — operator, configuration, policy, or a
bumped state version — would let a stale quote fund.

## Invariants — must hold

1. **Aggregate, not per-loan.** At origination, outstanding obligations plus the new principal and reserved full-term interest fit the **global debt budget** — the budget bounds what the loan will owe, not merely what it draws today. Lower subsequent risk capacity blocks new risk without altering existing debt.

   **Split honestly:** `globalCap` is the only limit the contracts hold. `facility_limit`, the account cap and the coverage ratios are the ENGINE's (there is no `max_ltv` field in `Terms`, on either side of the boundary), and they reach chain as an approved principal inside a signed envelope rather than as numbers the contracts re-derive. A quote that ignored them would be refused by the engine, not by `LoanManager`.

   That is a deliberate prototype split — the engine judges, the contracts enforce custody and envelopes — but it means the contracts do not independently check the engine's arithmetic.
2. **Replay protection.** Nonce consumed once
3. **Expiry.** `valid_until` enforced onchain
4. **Configuration binding.** Reverts if either policy epoch/hash or configuration version/hash fails to match its pinned record
5. **Supersession.** A new assessment of either kind invalidates prior unconsumed quotes; monitoring cannot originate a loan
6. **Authorization route.** Only the facility's pinned route may publish executable assessments; a CRE facility rejects development attestations
7. **Pause.** Halts new originations; never blocks repayment or liquidation
8. **Collection is bounded.** Recovery takes only what the loan is owed, and only the appointed keeper takes it. The surplus is the borrower's residual equity
9. **Resolution before write-off.** A default requires every registered collateral asset exhausted, not merely the settlement token. Unsold positions leave the loan `RecoveryPending`
10. **The receivable is the obligation.** Recovery marks principal *and* frozen interest unresolved, so the epoch cannot settle while either is still being collected
11. **Configuration is immutable for an obligation's life.** A facility's route cannot be re-pinned while it has live loans, and an execution route cannot be *added* to an account that owes. Revocation stays open, because withdrawing a route only reduces what an account can do
12. **Recovery needs a trigger.** Maturity passed, or risk permission withdrawn. Seizing a healthy, unmatured position would make every loan callable at will, which is not the instrument the borrower agreed to
13. **The executable class is pinned per deployment.** An oracle acts only on the class its deployment declares, by exact match. A scenario deployment issues and executes `ScenarioOnly`; a production one requires `PrototypeEligible` and refuses scenario evidence. An assessment of any other class is accepted as a record and stores no quote

### What proves each one

Run from `contracts/`:

| # | Invariant | Test |
|---|---|---|
| 1 | Aggregate debt budget | `forge test --match-test globalCap` |
| 2 | Replay protection | `test_replayedSequenceRejected` |
| 3 | Expiry | `test_permissionExpires` |
| 4 | Configuration binding | `test_unpinnedFacilityRejected` |
| 5 | Supersession | `test_monitoringSupersedesUnusedQuote` |
| 6 | Authorization route | `test_presentedRouteMustMatchPinned`, `forge test --match-test CREFacility` |
| 7 | Pause | `test_pauseBlocksOriginationNotRepayment` |
| 8 | Collection is bounded | `forge test --match-test ownerHasNoBypass`, and the close-out ordering test in `Lifecycle.t.sol` |
| 9 | Resolution before write-off | `test_cannotDefaultWhileAssetsRemain` |
| 10 | The receivable is the obligation | `test_settleBlockedWhileReceivableOpen` |
| 11 | Configuration is immutable for an obligation's life | `test_AdapterCannotBeAddedUnderALiveLoan` **and** `test_AdapterCanAlwaysBeRevoked` |
| 12 | Recovery needs a trigger | the three `LoanNotRecoverable` cases in `Lifecycle.t.sol` |
| 13 | The executable class is pinned | `test_ScenarioDeploymentExecutesScenarioClass`, `test_ScenarioDeploymentRefusesOtherClasses`, `test_PrototypeDeploymentRefusesScenarioClass` |

Invariant 11 needs **both** tests, and the pair is the point: adding a
route under a live loan reverts, revoking one never does. A single test
of the revert would look like coverage while leaving the asymmetry —
"withdrawing a route only reduces what an account can do" — unproven.

### Why the class is scoped, not a threshold

`docs/04` describes scenario fallback as starting a **separately bound
scenario deployment**. Its evidence is honestly synthetic, so its
assessments are honestly `ScenarioOnly` — and a deployment that refused to
act on them would make the fallback useless, while signing
`PrototypeEligible` instead would claim an authorization the engine never
granted.

The comparison is an exact match rather than a ranking, because
`ObservedShadow` is not "less executable" than `PrototypeEligible`; it is a
different kind of record. A scenario deployment must no more execute shadow
results than a production one executes scenario results.

## Trust assumptions

The baseline trusts the development attestor and price source within deployed contract limits. **Hashes, provenance labels, and reproducible computation do not prove an operator supplied complete or truthful evidence.** Make that trust visible rather than calling the path trustless.

Agents and their proposed actions are untrusted. External venues, tokens, and adapters are security-critical dependencies; a supported-asset list is not proof they cannot fail.

Pin adapters and risk limits for the active epoch. An administrator may pause new risk but **must not replace an adapter or weaken valuation rules around an outstanding loan.** Materially changed policies deploy in a new epoch after existing obligations resolve.

**Increasing the number of independently authorized signers does not automatically strengthen verification.**

### Time, and who controls it

Every deadline these contracts enforce is measured against
`block.timestamp`: quote and permission expiry, loan maturity, the
12-hour recovery grace, and the price-age bound. A block proposer can
shift that by a small amount, and `forge lint` flags each comparison for
exactly that reason.

**Accepted, and why.** The windows are hours, not seconds — a 3600s
permission window, a 12-hour grace, a loan term in days. A few seconds of
proposer drift cannot change *who* may act, only *when* by a margin far
inside the window's own tolerance. The comparisons are annotated in place
rather than rewritten, each with the same reasoning.

What this does mean: **no deadline here should ever be tightened to
seconds without revisiting it.** A short window is where timestamp drift
stops being noise and starts being a lever, and the annotations would then
be documenting a real weakness rather than an immaterial one.

Note also that assessment validity is anchored to CHAIN time rather than
the gateway's wall clock, for the same reason in reverse: the oracle
judges against `block.timestamp`, so a window measured off a node's own
clock would be wrong by whatever the two disagree by.

### What the operator can do — stated for borrowers

One key pins the facility's route, allowlists signers, pauses new risk,
appoints recovery keepers, and configures the price source. That is a
prototype trade-off, not a design for production, and the concentration
matters most where it meets the recovery trigger.

**An explicit `Blocked` assessment opens recovery immediately.** That is
the instrument working: a risk decision was made and acted on.

**Silence does not, for `recoveryGraceSeconds` (12 hours).** Treating a
merely-lapsed permission the same as a withdrawn one would let an operator
who also controls the attestor seize a healthy, unmatured position by
withholding updates, making the covenant "keep the attestor answering"
rather than "stay solvent". The two are distinguished on chain by
`permissionWithdrawn`.

Past the grace window a lapse still opens recovery: an obligation nobody
is assessing cannot stay open indefinitely. So the honest statement of
the borrower's covenant is:

> Stay solvent, and expect recovery if the attestor either denies you or
> stops assessing you for twelve hours.

The second clause is a real obligation on the *lender's* side of the
arrangement, and a borrower should read it before drawing.
