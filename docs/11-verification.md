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
- Capacity 5,000 plus a request for 1,000 grants exactly 1,000; a request for 6,000 is denied without terms
- Changing only vault cash changes `drawable_capacity`, not `agent_capacity`, score, factor deltas, or contractual debt headroom
- Missing required evidence returns `Unavailable`; missing optional enrichment is omitted; neither silently changes evidence origin
- Higher Brier loss cannot reduce the forecast penalty; insufficient eligible outcomes receive the maximum penalty, and the cap never increases APR or relaxes stress coverage
- Counterfactual deltas equal reference minus actual `agent_capacity`, in settlement-token units
- Same-block repayment earns zero duration weight; early repayment uses actual eligible elapsed seconds rather than contractual term
- Exact reciprocal LTV and coverage comparisons agree at the threshold and handle zero recoverable value without displaying zero risk

## Contract invariants

Foundry unit plus invariant and fuzz tests. **Test observable failures, not private helper mirrors.**

- Principal fits the account cap; outstanding obligations plus a new draw and reserved full-term interest fit facility and global debt caps
- A repeated draw, duplicate report, or transaction retry cannot duplicate an economic effect
- Wrong chain, contract, account, asset, action, policy, configuration, or evidence domain fails authorization
- Policy epoch/hash and configuration version/hash each match their pinned records; swapping either identifier or hash is rejected
- A CRE facility rejects development attestations for both origination and monitoring; provider failure cannot change its route
- Monitoring updates cannot originate loans or carry loan terms; blocked updates supersede unused quotes, while expired permission blocks risk increases without blocking repayment
- Grants and draws match the exact committed request amount; partial or larger draws fail
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
- If the note extension is included, test ownership transfer, collection, holder redemption, and default payout conservation

## Release acceptance

The final release requires evidence in every row below. A tagged development baseline may leave explicitly identified release rows pending, as scheduled in [10 · Schedule](10-schedule.md). Optional integration checks apply only to included capabilities; they cannot waive a required row.

| Requirement | Evidence required |
|---|---|
| Performer lifecycle | Account, request, assessment, execution, repayment, next assessment linked |
| **Three sequential loans** | Same policy and model hash; limit rises, APR falls, binding constraint switches from history to stress capacity |
| **Behavioral ablation** | Controlled runs varying only history, then only forecast evidence, each naming its binding constraint and published reference values |
| **Continuous underwriting** | Unprompted `Monitoring` update for an open loan, with no origination terms, reaching chain within the declared interval; blocked and expired permission prevent new risk |
| Concentration override | Favorable history plus a current-risk denial with cited inputs |
| **Three stress cases** | Price, liquidity, and combined shocks reported side by side |
| Forecast evaluation | Commit-before-funding, repeated resolved outcomes under a defined rule |
| Custody protection | Prohibited withdrawal rejected by contract |
| Default handling | Realized proceeds, principal loss, unpaid interest, lender allocation reconcile |
| Replay | Exact canonical assessment reproduced from retained evidence |
| Missing / stale data | Risk-increasing authorization blocked while repayment remains possible |
| **Independent reputation reader** | Publication and standalone reader verify provenance on the selected network's ERC-8004-compatible registry, with no Credence endpoint; a local-only adapter does not pass |
| Integration honesty | Claimed capabilities backed by artifacts; modes labeled |
| Reproducibility | Clean setup completes the documented scenario without private data |

## Operational measurement

Engine runtime, input size, provider lag, event reconciliation lag, transaction status. One scripted command runs the offline scenario suite; another starts the demo environment. Document required credentials and optional integrations explicitly.

Persist SQLite on durable storage and take a consistent backup **before recording the demonstration.**
