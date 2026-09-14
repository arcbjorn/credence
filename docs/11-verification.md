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
- A CRE facility rejects development attestations for both origination and monitoring; provider failure cannot change its route — `forge test --match-test CREFacility`
- Monitoring updates cannot originate loans or carry loan terms; blocked updates supersede unused quotes, while expired permission blocks risk increases without blocking repayment
- Grants and draws match the exact committed request amount; partial or larger draws fail
- Expired or superseded quotes cannot originate loans
- Owners, agents, adapters, and rescue functions cannot remove assets while debt remains
- **A bad-price trade through an otherwise approved venue is rejected**
- Reentrant callbacks cannot alter debt or accounting before checks settle — `forge test --match-contract Reentrancy` repays and collects through a settlement token that calls back mid-transfer
- Pauses preserve repayment and permitted recovery
- **Healthy-account checks do not lock an unhealthy account out of a valid risk-reducing recovery action**
- Stale data blocks new risk without inventing a zero liquidation value
- Lender withdrawals are bounded by actual settled cash; collections and losses cannot be double-counted
- Identity transfer leaves existing debt and recovery rights intact — `forge test --match-test IdentityTransfer`
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

## Evidence status — 13 September

Where each row stands against the **baseline**, and the command that
reproduces it. Rows without evidence are named as pending rather than
assumed; a green suite is not evidence for a behavioural claim.

| Row | Status | Reproduce |
|---|---|---|
| Performer lifecycle | evidence | `scripts/demo-repay.sh` |
| **Three sequential loans** | evidence | `LOANS=3 scripts/demo-repay.sh` — one policy and model hash; limit rises, APR falls, binding constraint switches from history to stress capacity at loan 3 |
| Behavioral ablation | evidence | `cargo test -p risk-core --test ablation` (A→B, A→C, A→D, each naming its binding constraint) |
| **Continuous underwriting** | evidence | `scripts/demo-monitor.sh` |
| Concentration override | evidence | `cargo test -p risk-core --test ablation concentration` — perfect history, zero capacity, bound by `StressCapacity` |
| **Three stress cases** | evidence | reported in every assessment; `cargo test -p risk-core --test stress` |
| Forecast evaluation | evidence | `LOANS=4 scripts/demo-repay.sh` commits before funding and scores on resolution; `go test ./store/ -run Brier` pins the arithmetic |
| Custody protection | evidence | `forge test --match-test ownerHasNoBypass` |
| Default handling | evidence | `scripts/demo-default.sh` |
| Replay | evidence | `scripts/demo-monitor.sh` (beat 8, with its tampering control) |
| Missing / stale data | evidence | `forge test --match-test ExecuteRequiresStandingPermission`; engine blocks on stale required evidence |
| **Independent reputation reader** | **not met** | `scripts/demo-feedback.sh` exercises a LOCAL adapter whose ABI is incompatible with ERC-8004 in both directions. Porting is a rewrite, not a config change; see [06 · Identity](06-identity-forecasts.md#what-is-not-integrated) |
| Integration honesty | evidence | scenario facility and test assets labelled; `TestRiskyAsset` is named *NOT ETH* |
| Reproducibility | evidence | the three scripts run from a clean chain |

Observed on 13 September for the sequential-loans row, one policy and
model hash throughout, only demonstrated history varying:

```
loan 1: apr=1200bps capacity=6200000000 binding=HistoryGrowthCap
loan 2: apr=1147bps capacity=7533330000 binding=HistoryGrowthCap
loan 3: apr=1094bps capacity=8117138556 binding=StressCapacity
```

The limit rises, the APR falls, and the binding constraint **switches from
history to stress capacity at loan 3** — the `min()` story told honestly:
history opens the facility until portfolio risk becomes the limit.

Only `base_history_cap` was tuned to produce this; the calibration and the
two traps that defeat it are recorded in
[03 · Underwriting](03-underwriting.md#how-the-fixtures-are-calibrated).

Forecast commitments retained by that run, every commitment timestamped
before its resolution:

```
forecast:1  p=300000ppm  DidNotOccur  brier= 90000   (0.30-0)^2
forecast:2  p=150000ppm  DidNotOccur  brier= 22500   (0.15-0)^2
forecast:3  p=100000ppm  DidNotOccur  brier= 10000   (0.10-0)^2
forecast:4  p= 75000ppm  unresolved   -
```

Three observations permit an average, **not a reliability claim**
(`docs/06`). No calibration statement is made from this sample.

### Independent reader — what is and is not demonstrated

> **Corrected 14 September.** An earlier version of this section implied
> the reader would port to a real registry by changing an address. It will
> not: the local adapter and ERC-8004 share no selectors, and the value
> model differs. What follows describes the LOCAL adapter only.

`tools/reader/read_feedback.py` prints an agent's obligation history from
a JSON-RPC endpoint alone. It imports only the Python standard library and
nothing from this repository: sharing an encoder with the gateway would
make the check circular, since the reader would agree with the publisher
for being the same code. Verified by running it from a directory outside
the repo.

```
agent 184: 3 published obligation(s)
  loan 1  Repaid  principal=4000000000  interest=6575358  loss=0  held=432001s
  loan 2  Repaid  principal=4000000000  interest=6137001  loss=0  held=432001s
  loan 3  Repaid  principal=4000000000  interest=5698644  loss=0  held=432001s
  repaid=3  recovered=0  defaulted=0
```

Interest declines across the three because the engine repriced each loan
as history accumulated — the same run the sequential-loans row reports.
Publication is idempotent: a second run reports `already published` and
adds nothing, so a retried queue cannot double-count an outcome.

Signals stay separate (`docs/06`): repayments, defaults, interest, and
loss are reported independently and never collapsed into a score.

> **This runs against a local adapter, and that does not satisfy the
> release row.** `docs/08` permits one for development while the identity
> gate stays open; the row requires publication to the selected network's
> ERC-8004-compatible registry, with its address and compatibility
> evidence in the deployment manifest. The registry compatibility gate is
> **15 September**. The reader is written against an ABI layout the real
> registry shares, so it should port rather than be rewritten — but that
> claim is untested until a registry exists to test it against.

## Operational measurement

Engine runtime, input size, provider lag, event reconciliation lag, transaction status. One scripted command runs the offline scenario suite; another starts the demo environment. Document required credentials and optional integrations explicitly.

Persist SQLite on durable storage and take a consistent backup **before recording the demonstration.**
