# 11 · Verification

## Risk-core

- Golden fixtures for three scenarios, reviewed against hand-calculated cases
- Same input, model, and policy produce the same canonical output across repeated runs
- Safe rounding and overflow at token-decimal and integer boundaries
- **Worse recovery assumptions cannot increase permitted debt** when all else is fixed
- Missing required evidence never produces executable terms
- Synthetic origin survives derived-feature transformations
- Forecasts with unresolved outcomes receive no success credit
- Capacity search agrees with a slow reference evaluator and respects every binding constraint
- Amount, rate, and term arithmetic reconciles across Go, Rust, frontend, and Solidity

## Contract invariants

Foundry unit plus invariant and fuzz tests. **Test observable failures, not private helper mirrors.**

- Debt plus a new draw never exceeds account, facility, or global cap
- A repeated draw, duplicate report, or transaction retry cannot duplicate an economic effect
- Wrong chain, contract, account, asset, action, policy, configuration, or evidence domain fails authorization
- Expired or superseded quotes cannot originate loans
- Owners, agents, adapters, and rescue functions cannot remove assets while debt remains
- **A bad-price trade through an otherwise approved venue is rejected**
- Reentrant callbacks cannot alter debt or accounting before checks settle
- Pauses preserve repayment and permitted recovery
- **Healthy-account checks do not lock an unhealthy account out of a valid risk-reducing recovery action**
- Stale data blocks new risk without inventing a zero liquidation value
- Lender withdrawals are bounded by actual settled cash; collections and losses cannot be double-counted
- Identity transfer leaves existing debt and recovery rights intact
- **Unavailable liquidity produces `RecoveryPending`, not fictitious repayment**

## Integration and operational

- Normalize a recorded provider response; verify units, coverage, provenance
- Exercise timeouts, partial responses, unexpected asset IDs, stale and conflicting prices
- Restart Go during a submitted transaction; reconcile without resubmitting an already-funded loan
- Replay an indexed loan after cursor rollback or simulated reorganization
- **Run the demo with no optional APIs and confirm the scenario baseline still works**
- Restore retained SQLite data and reproduce a selected historical assessment
- Reject invalid CRE report paths and bindings; distinguish simulated from deployed execution
- Test note ownership transfer, collection, holder redemption, default payout conservation

## Release acceptance

Nothing ships without evidence in every row.

| Requirement | Evidence required |
|---|---|
| Performer lifecycle | Account, request, assessment, execution, repayment, next assessment linked |
| **Three sequential loans** | Same policy and model hash; limit rises, APR falls, binding constraint switches from history to stress capacity |
| **Behavioral ablation** | Controlled runs varying only history, then only forecast evidence, each naming its binding constraint and published reference values |
| **Continuous underwriting** | Unprompted reassessment while a loan is open, reaching chain within the declared interval |
| Concentration override | Favorable history plus a current-risk denial with cited inputs |
| **Three stress cases** | Price, liquidity, and combined shocks reported side by side |
| Forecast evaluation | Commit-before-funding, repeated resolved outcomes under a defined rule |
| Custody protection | Prohibited withdrawal rejected by contract |
| Default handling | Realized proceeds, principal loss, unpaid interest, lender allocation reconcile |
| Replay | Exact canonical assessment reproduced from retained evidence |
| Missing / stale data | Risk-increasing authorization blocked while repayment remains possible |
| **Independent reputation reader** | Standalone script verifies registry provenance with no Credence endpoint |
| Integration honesty | Claimed capabilities backed by artifacts; modes labeled |
| Reproducibility | Clean setup completes the documented scenario without private data |

## Operational measurement

Engine runtime, input size, provider lag, event reconciliation lag, transaction status. One scripted command runs the offline scenario suite; another starts the demo environment. Document required credentials and optional integrations explicitly.

Persist SQLite on durable storage and take a consistent backup **before recording the demonstration.**
