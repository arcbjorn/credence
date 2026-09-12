# 06 · Identity & Forecasts

## Binding

Reference agents by registry chain, address, and agent ID — **never a bare integer.** Bind financing to the tuple present at origination:

```text
(identity, credit_account, operator, policy_hash, config_version)
```

ERC-8004 supports transferable identities and mutable metadata, and its specification acknowledges Sybil exposure. It supplies **attribution**; Credence supplies **continuity policy**.

On transfer or configuration mismatch: **freeze new credit, require reassessment.** Existing debt, recovery rights, and the original equity recipient remain explicit in the loan record. **Registry transfer never moves or erases a loan.**

## Anti-farming

Ten instant micro-loans must not approach the weight of real capital held for real duration.

```text
weight_i = (principal_i / reference_size) × min(term_days_i / 30, 1.0)
```

Per-loan and per-period caps. Same-block borrow/repay contributes negligible duration. Distinguish scenario time from chain time. Track overlapping borrowing and known shared operators, but **do not claim complete entity resolution or elimination of Sybil attacks.**

PnL adjusted for external cash flows. **A repayment proves an obligation was settled; it does not prove the strategy generated the repayment.** External top-ups stay visible in the outcome record.

## Published feedback

Attributable events with loan IDs, amounts, durations, outcome classes, evidence references. Contract state is authoritative even if registry publication lags. Queue updates idempotently.

**A repayment, a profitable strategy, a reliable forecast, and absence of default are separate signals.** Do not combine them into one fact. Keep append-only local history; record corrections as new versions rather than rewriting assessments.

### Independent readability

| Sub-claim | Status |
|---|---|
| A. Credence publishes standardized signed feedback | **Ship** |
| B. A third party reads and verifies it without asking Credence | **Ship** — reader library + script, ~1 day |
| C. Another protocol consumes it in production | **Cannot ship** — needs a second team to adopt |

Demonstrate A and B with a standalone script printing an agent's obligation history using no Credence endpoint.

**Be precise about what it verifies:** registry provenance — that feedback was published by Credence's declared `clientAddress` — plus any application-level signature over the payload. The standard attributes feedback to a client address and requires consumers to *filter which sources they trust*. It does not make arbitrary registry feedback trustworthy.

The reader demonstrates **attributable, filterable provenance**, not that the content is true. Claim "independently verifiable", never "adopted".

## Forecast commitments

One or two precisely defined events:

```text
Event:       cash-flow-adjusted marked account NAV falls more than 5%
             below its observed peak during the next 20 minutes.
Forecast:    probability in fixed-scale integer units.
Observation: supported account, named price source, fixed sample cadence.
Resolution:  occurred / did not occur / not observable.
```

Twenty minutes is a **demo horizon**, not a claim about fourteen-day forecasting.

NAV is marked assets minus principal and accrued interest, normalized for equity deposits and withdrawals via a unitized series — **borrowing adds an asset and an equal liability and creates no profit.** Require positive starting equity; define insolvency as a breach. Sampling cadence is part of the event definition: a sampled maximum drawdown does not claim to observe every intermediate price.

Commit before disbursement, storing retrievable payload **and** hash. Score every eligible commitment including early exits; after strategy closure preserve final unitized equity as settlement cash through the remaining horizon. **Disclose the early-exit rule before forecasting.**

### Scoring

Brier loss `(p − y)²`; lower is better. Aggregate only comparable events, showing sample size, event frequency, horizon, unresolved observations, and provenance.

**Do not label `1 − mean_brier/0.25` as universal calibration.** A 0.5 forecast is not the correct reference for every event frequency. Report raw Brier and, with sufficient comparable data, a separately defined baseline and reliability bins. Predictive accuracy and calibration are related but different properties.

### Scope, stated honestly

The original commits four quantities — expected gross return, expected net return, maximum drawdown, duration — and compares each against actuals. **This release scores one:** a drawdown-event probability, chosen because it resolves precisely. Predicted-versus-actual **return** and **duration** are retained roadmap items. Requested loan duration survives as a term; *predicted* duration is a separate forecast this release does not evaluate.

### Three loans do not establish calibration

The sequential-loan requirement supplies a **lifecycle demonstration, not a forecast sample.** Three observations permit an average, not a reliability claim.

**Separate acceptance criteria.** Lifecycle completeness is satisfied by three loans. Any calibration statement requires a declared minimum sample, reliability bins, and a stated baseline — and without that sample, report raw Brier with its `n` and make no aggregate claim.

### Weighting

**Low in the engine** — a bounded penalty that cannot relax the hard stress floor. **High in the demo** — the agent caught by its own committed forecast is the most memorable beat available.

Good forecast metrics cannot authorize a loan, prove repayment intent, or remove collateral constraints. Insufficient or unobservable outcomes receive no favorable treatment.

**A single drawdown exceeding a low-probability forecast is an observation, not proof of deception.** The overconfident scenario uses repeated forecasts and a seeded outcome sequence inconsistent with them. Select the scenario before examining displayed results.
