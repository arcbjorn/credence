# 05 · Contracts

## Responsibilities

| Contract | Responsibility |
|---|---|
| `CreditVault` | Fixed-epoch deposits, available cash, receivables, collections, realized losses, final settlement |
| `LoanManager` | Origination, aggregate exposure, interest, maturity, repayment, recovery, final resolution |
| `CreditAccount` | Custody, bound operator, supported assets, restricted actions, residual equity return |
| `StrategyAdapter` | One verified route, fixed receiver, bounded approvals, slippage and output checks |
| `RiskOracle` | Accepted assessment state, sequence, policy binding, evidence class, authorization adapters |
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
3. Rust returns assessment and proposed terms
4. Authorization adapter validates and records
5. Borrower requests a draw matching the signed request and action envelope
6. `LoanManager` checks binding, account state, sequence, expiry, evidence class, equity, debt, aggregate limits, cash, prices, epoch state
7. **Transaction consumes the quote and creates the debt before any untrusted external interaction**
8. Vault transfers principal only to the bound `CreditAccount`
9. Account executes only the permitted initial action, or holds settlement tokens

Where an initial trade is part of the approval, **fund and trade atomically**; revert everything if post-action checks fail. A quote for a conservative allocation cannot fund a different allocation.

## Custody rules

- Neither agent nor owner withdraws while debt is active or recovering
- No general `call`, `delegatecall`, unrestricted approval, or rescue route bypasses the debt gate
- Targets, selectors, assets, spender, recipient fixed or adapter-constrained; outputs return to the account
- Approvals bounded to the operation and cleaned up
- **Slippage checked against a permitted valuation source, not only caller-supplied `minOut`**
- **A whitelisted venue is not sufficient** — a bad-price trade transfers value to a cooperating counterparty. Test this explicitly
- Execution increments account state version, invalidating stale action assumptions
- Configuration changes require no outstanding debt (v0 rule)

## Authorization envelope

```text
chain_id, verifying_contract, deployment_id, evidence_policy_id
identity_registry, agent_id, operator, credit_account, facility_id
account_state_version, config_hash, policy_epoch
request_id, intent_hash, settlement_asset, principal, term, apr
permitted_action_hash, model_hash, input_hash, evidence_root
authorization_class, sequence, valid_after, valid_until
```

EIP-712 typed envelope. Strictly increasing sequence per binding. A quote is consumed at most once; newer assessments supersede unused quotes. Replayed, expired, cross-chain, cross-deployment, wrong-policy, and wrong-account attestations fail.

**Current contract state is still checked at draw time.** A correctly signed assessment is not permission to ignore a vault cap or an open loan.

## Invariants — must hold

1. **Aggregate, not per-loan.** `outstanding_debt + new_draw ≤ credit_limit`
2. **Replay protection.** Nonce consumed once
3. **Expiry.** `valid_until` enforced onchain
4. **Configuration binding.** Reverts on `policy_hash` or `config_version` mismatch
5. **Supersession.** A new assessment invalidates prior unconsumed ones
6. **Signer plurality.** Any authorized signer — protocol key from day one, CRE added without rework
7. **Pause.** Halts new originations; never blocks repayment or liquidation

## Trust assumptions

The baseline trusts the development attestor and price source within deployed contract limits. **Hashes, provenance labels, and reproducible computation do not prove an operator supplied complete or truthful evidence.** Make that trust visible rather than calling the path trustless.

Agents and their proposed actions are untrusted. External venues, tokens, and adapters are security-critical dependencies; a supported-asset list is not proof they cannot fail.

Pin adapters and risk limits for the active epoch. An administrator may pause new risk but **must not replace an adapter or weaken valuation rules around an outstanding loan.** Materially changed policies deploy in a new epoch after existing obligations resolve.

**Increasing the number of independently authorized signers does not automatically strengthen verification.**
