# 03 · Underwriting

![Underwriting order](diagrams/underwriting-order.svg)

## Origination evaluation order — fixed

1. Validate binding, units, coverage, freshness, supported assets
2. Validate origination kind, request, and intended action against account policy
3. Mark value; compute recoverable values under **three named shocks**
4. Reserve maximum full-term interest, execution cost, bounded keeper fee
5. Compute maximum safe draw on the **post-action** portfolio
6. Apply history, configuration continuity, concentration, utilization, hard caps
7. Apply the bounded forecast cap, using the conservative default when outcomes are insufficient
8. Price with the versioned policy schedule
9. Approve, deny, or mark unavailable with binding constraints and evidence references

**Step 3 is mandatory in three forms** — price shock, liquidity shock, and combined price + liquidity shock — restoring the original comparison. Volatility scaling supplements these; it never replaces them, and a **declared minimum stress floor** prevents a calm observation window from silently weakening the test. Report all three side by side.

**Step 5's post-action framing is essential.** A draw adds both cash and a debt obligation, and the intended trade changes recoverability. Computing capacity from the pre-draw portfolio is insufficient.

```text
forecast_cap = floor(account_hard_cap * (10_000 - forecast_penalty_bps) / 10_000)
agent_capacity = min(stress_capacity, history_growth_cap, account_hard_cap,
                     forecast_cap)
drawable_capacity = min(agent_capacity, debt_headroom_capacity,
                        remaining_strategy_budget, remaining_vault_budget,
                        available_cash)
```

These are principal amounts for an **origination** assessment under the declared action and term. `stress_capacity` includes the post-action concentration constraints. `debt_headroom_capacity` is the largest principal whose principal plus reserved full-term interest fits `available_credit`. Strategy and vault budgets are likewise converted to remaining principal capacity after reserving existing obligations and the new loan's full-term interest. Costs are deducted once from recoverable assets, not again from debt.

Use bounded monotone search where the permitted action has a monotone feasible range, verified against a slow reference implementation. Recheck the exact requested portfolio; a capacity estimate does not authorize a different allocation. With an active or recovering loan, an inactive epoch, a pause, or an independent account restriction, `drawable_capacity` is zero. If required evidence is unavailable, capacity is absent rather than an invented zero-risk amount. A new eligible origination assessment may supersede a prior blocked assessment; the prior result alone is not a permanent denial.

**With complete required evidence, v0 grants the exact requested amount or denies the request.** For an otherwise eligible request with `0 < requested_principal <= drawable_capacity`, `approved_principal = requested_principal`. Otherwise return `Denied` with no proposed terms and explain the limiting capacity when known. An agent wanting a smaller loan submits a new intent and request; there is no implicit partial fill. A request for 1,000 with capacity 5,000 grants 1,000; a request for 6,000 is denied.

Monitoring assessments evaluate the existing loan and current account policy, without solving or quoting a hypothetical new loan. See **Continuous underwriting** below and [04 · Evidence Contract](04-evidence-contract.md).

## The five quantities

Distinct everywhere. The vision conflated them.

```text
facility_limit       binding's total debt ceiling under the pinned facility policy
requested_principal  what the agent asked for
approved_principal   exact principal granted; absent unless origination is approved
outstanding_debt     outstanding principal + unpaid accrued interest
available_credit     max(facility_limit - outstanding_debt, 0)   (headroom)
```

**`available_credit` is debt headroom, not drawable cash.** The separate `drawable_capacity` accounts for reserved interest, borrower capacity, lender liquidity, and origination gates. The one-active-loan rule can block a draw while headroom is positive; vault budget can block it too. Never present headroom as immediately borrowable. The demo's changing behavioral limit is `agent_capacity`; it does not change the pinned `facility_limit`.

## Liquidation cost scales with depth

The liquidity haircut is **not** a flat rate. Market impact grows with the
square root of position size relative to available depth, which is the
standard form for liquidation cost:

```
impact_bps = liquidity_haircut_bps × √(position_value / liquidity_depth)
```

| Position vs depth | Impact |
|---|---|
| size = depth | the full declared haircut |
| size = depth / 4 | half of it |
| size = depth / 100 | a tenth of it |

A flat haircut misprices both ends: it over-penalises a small clip that
barely moves the market, and under-penalises a book that *is* the market
and cannot be sold at the observed price. Concentrated positions are
exactly what the liquidity scenario exists to catch.

**A declared depth of zero means undeclared, never infinite.** An
undeclared market takes the maximum impact.

Computed with an integer square root (Newton's method on `u128`), so no
floating-point path produces a value used in authorization.

> **Declare the depth the venue actually has.** A depth larger than the
> venue's real liquidity makes every exit look costless — the same failure
> as omitting the haircut entirely.

## Forecast penalty

The versioned policy declares a positive integer `min_forecast_outcomes`, a maximum penalty with `0 < max_forecast_penalty_bps < 10_000`, and a table mapping comparable mean Brier loss to `forecast_penalty_bps` between zero and that maximum. Higher loss cannot reduce the penalty. With sufficient eligible resolved observations, use that table; otherwise use the **maximum penalty**, including when outcomes are missing or unobservable. No forecast credit is earned merely because observations are absent.

The penalty affects **only `forecast_cap`**, not APR or stress assumptions. Good observations can remove some or all of it; every other eligibility and capacity constraint still applies. Publish the table, sample requirement, selected penalty, and resulting cap with the assessment. These are policy assumptions, not a calibration claim. History may still bind below the conservative forecast cap, allowing a new account to receive its secured base loan.

## Pricing

A **versioned APR policy schedule**: base rate, bounded history discount, portfolio surcharge, minimum and maximum rate. Frozen with demo fixtures after behavioral testing. **Experimental terms, not empirically estimated fair prices.**

Where an assumed PD is displayed, report it **separately** from the policy APR and run a consistency check:

```text
scenario_expected_loss = PD_over_term × LGD_assumption × exposure_at_default
term_interest          = principal × APR × term_seconds / seconds_per_year
```

All three on the **same horizon**. Price the term, then annualize for display — compounding a 30-day PD to annual overstates low grades and breaks the curve at high ones.

Label funding expense, operating expense, uncollected interest, expected loss, and gross interest separately. **Gross interest minus expected loss is not lender profit** — that needs a probability-weighted cash-flow model.

**Do not make every borrower acceptable by raising APR.** Return `Denied` when complete evidence establishes that the request breaches capacity, action policy, term limits, or the borrower's maximum APR. Return `Unavailable` when required evidence is missing, stale, conflicting beyond tolerance, or unsupported. Missing optional enrichment is omitted without a clean-history benefit.

The agent separately accepts or declines offered terms using its retained expected-benefit estimate on the same horizon after costs. The engine does not require a return forecast or treat an agent's benefit estimate as evidence of repayment capacity. This preserves the one scored forecast dimension in [06 · Identity & Forecasts](06-identity-forecasts.md).

## Reconciliation examples

Explanatory arithmetic and test-vector candidates — **not calibration.**

| Example | Calculation | Result |
|---|---|---|
| Original rate example | 10,000 × 9.8% × 30/365 | 80.55 gross interest |
| Original PD, zero recovery | 10,000 × 1.7% × 100% | 170.00 expected loss |
| Same PD, 50% LGD | 10,000 × 1.7% × 50% | 85.00 expected loss |
| Spread at requested ceiling | (18.7% − 9.1%) × 14/365 | 0.3682% over 14 days |
| Spread at granted rate | (18.7% − 8.9%) × 14/365 | 0.3759% over 14 days |

The first three show why a recovery assumption matters, and why the original pairing was inconsistent: **9.8% APR earns 80.55 while a 1.7% PD implies 170.00 of expected loss.** The lender loses in expectation.

The last two correct the original's unit error. Both rates are from the vision and both are correct — 9.1% is the ceiling the agent *requests*, 8.9% is what is *granted*. Either way a 14-day trade at 18.7% annualized gross yields an **annualized spread of 9.6–9.8%, which is 0.37–0.38% over the term — about $37 on $10,000** — not a 9.6% period return.

## Interest convention

```text
accrual_end = recovery_started_at if recovery_has_started else now
elapsed     = min(max(accrual_end - originated_at, 0), term_seconds)
interest_accrued = ceil(principal_base_units × apr_bps × elapsed
                        / (10_000 × 365 × 86_400))
interest_due = interest_accrued - interest_already_collected
debt_due     = principal_outstanding + interest_due
```

Simple fixed APR, 365-day year, chain-time seconds, overflow-safe arithmetic. Accrual stops at maturity or recovery entry, whichever comes first; snapshot on entering recovery so partial collections need no new schedule. No late penalty, no compounding. Early full repayment pays accrued interest only.

**An assessment update never reprices an outstanding fixed-rate loan.** Future loans use the new terms.

Reserve maximum full-term interest when sizing capacity. Price from the pre-draw assessment, verify the reserve covers the final APR, re-solve if it does not. Enforce maximum term and borrower-specified maximum APR at draw time.

## Score — derived, not invented

```text
agent_capacity = min(stress_capacity, history_growth_cap, account_hard_cap,
                     forecast_cap)
score          = 300 + floor(600 * agent_capacity / account_hard_cap)  # 300–900
```

**`agent_capacity` deliberately excludes every lender-side term** — vault budget, strategy budget, available cash — and the size of the request. Scoring a liquidity-constrained amount would let a borrower-quality number move when nothing about the borrower changed: at a 12,000 cap and 9,000 capacity the score is 750, but if vault cash fell to 4,500 the same agent would score 525. Vault scarcity belongs in `drawable_capacity`; contractual debt headroom remains `available_credit`. Require a positive `account_hard_cap`; omit the score when borrower capacity cannot be evaluated.

It is a **facility-relative capacity index, not portable borrower quality** — scaled by *this* facility's hard cap, so it is not comparable across facilities. Label it that way on screen. **It is not a credit rating.** Never show a letter grade: A/AAA implies a calibrated default scale that does not exist.

Always show the binding constraint beside the score. The score alone hides *why* the limit is what it is.

## Factors — measured, not attributed

Re-run the pure engine with each feature set to a **declared policy reference value** — never with evidence removed. Removing positions or valuation triggers the evidence policy, which returns `Unavailable`: the capacity is then undefined, not higher, and the comparison is meaningless. **Every counterfactual must have complete, valid inputs yielding a defined `agent_capacity`.** Approval and lender liquidity do not determine whether this borrower-capacity comparison exists.

| Factor | Counterfactual reference | Agent capacity | Δ capacity |
|---|---|---|---|
| 37 repayments, exposure-weighted | zero-history baseline | 6,400 | −2,600 |
| Low historical drawdown | policy median drawdown | 7,800 | −1,200 |
| Zero liquidations | policy baseline rate | 8,300 | −700 |
| ETH concentration | diversified reference | 10,100 | +1,100 |
| Leverage increase | policy reference leverage | 9,600 | +600 |

*Illustrative against 9,000 agent capacity and a 12,000 hard cap, expressed in settlement-token units. `Δ capacity = counterfactual agent_capacity - actual agent_capacity`. Every reference value is versioned policy, published with the panel. Counterfactual allocations use only the supported settlement and risky assets.*

**Deltas do not sum to the score.** `agent_capacity` is a `min()` over competing constraints, so effects are non-additive — a factor that is not binding contributes nothing until it becomes binding. Label each row as the change in capacity under that reference, not as score points or a decomposition. Genuine missing-data behavior is tested separately.

This is stronger than a weighted panel: each value is a measured consequence a judge can reproduce with `credence risk assess`.

## Behavioral underwriting must be visibly causal

`min()` creates a product risk that outranks every integration question. If stress capacity is 3,000 while history cap improves from 4,000 to 6,000, capacity stays 3,000 — **the history improved and the product delivered nothing.** Often correct; fatal if it is the only outcome demonstrated, because then agent behavior is decorative and this is a competent collateralized lender rather than Credence.

### Controlled ablation — release requirement

| Run | Held constant | Varied | Must show |
|---|---|---|---|
| A→B | Equity, positions, market, strategy, liquidity | Eligible repayment history only | Exact change in limit and APR attributable to history |
| A→C | Same, plus history | Forecast evidence only | Exact change attributable to forecast quality |
| A→D | Same | Portfolio risk raised | Hard constraints override both; which binds, by how much |

Every run reports the **binding constraint by name**. **Fixtures must be calibrated so history and forecast bind in A→B and A→C** — if stress capacity clamps every scenario, the demo cannot show behavior mattering, and the fixtures are wrong.

### Three sequential loans — release requirement

The ablation proves history *would* change terms. It does not show credit **compounding**, which is the original thesis. The performer completes at least three sequential loans in one run, unchanged policy and model hash, `agent_capacity` recomputed after each:

| Loan | History at request | Limit | APR | Binding constraint |
|---|---|---|---|---|
| 1 | none | base | base | history cap |
| 2 | loan 1 repaid | higher | lower | history cap |
| 3 | loans 1–2 repaid | higher still | lower still | **stress capacity** |

The constraint switching by loan 3 is the `min()` story told honestly: history opens the facility until portfolio risk becomes the limit. Cheap in virtual time. **Sequential, not concurrent** — one active loan per account is unchanged.

#### How the fixtures are calibrated

The switch is a property of the numbers, not of the narrative, so the
scenario's constants are chosen to produce it:

| Quantity | Value | Why |
|---|---|---|
| Agent equity | 10,000 | Sets stress capacity: the loan deploys equity *and* principal into the risky asset, and stress capacity lands near **0.82× equity** under this policy |
| `base_history_cap` | 6,200 | Below stress capacity at loan 1, so **history binds first** |
| `history_growth_per_ppm` | 10,000 | Each repayment adds ~1.33e9 of cap, so two repayments carry the history cap past stress |
| Forecast cap | 9,600 at maximum penalty | Stays **above** stress, so it never masks the switch |

Observed:

```
loan 1: apr=1200bps capacity=6200000000 binding=HistoryGrowthCap
loan 2: apr=1147bps capacity=7533330000 binding=HistoryGrowthCap
loan 3: apr=1094bps capacity=8117138556 binding=StressCapacity
```

Two traps worth naming, because both silently defeat the demonstration:

- **A new account carries the maximum forecast penalty.** One forecast per
  loan never reaches `min_outcomes`, so the forecast cap stays at 9,600
  throughout. That is the intended behaviour — this document already says
  history may bind below a conservative forecast cap — but it means the
  forecast cap must sit *above* stress capacity, or it binds instead and
  no switch is visible.
- **Raising equity does not help on its own.** Stress capacity scales with
  equity, so more equity moves the target rather than reaching it. The
  lever is `base_history_cap` relative to stress. Equity matters only for
  a second reason: the requested principal must fit under loan 1's
  capacity, or the request is denied outright rather than sized down.
- **The action's declared `deploy_bps` changes the ratio.** Deploying
  equity alongside principal is a riskier book than principal alone, so
  stress capacity falls from ~1.29× equity to ~0.82×. Calibrating against
  a projection that understates the deployment produces numbers that stop
  working the moment the projection is corrected.

## Continuous underwriting

The original's climax is a shock changing risk **while a loan is open and the agent requested nothing**. Origination checks, recovery thresholds, and post-repayment reassessment do not require that behavior — a system could pass the whole demo script without it.

**Release requirement:**

> While a loan is active and the agent submits no new request, a market observation changes. The Go scheduler detects it, triggers reassessment, Rust produces a new assessment, and the updated risk state reaches the chain within a declared interval.

| Property | Specification |
|---|---|
| Owner | Go scheduler — owns reassessment timing; contracts independently check chain time |
| Trigger | Price move beyond policy threshold, or evidence staleness |
| Interval | Declared per deployment in the gateway config's `scheduler` block; measured in the latency report |

**What "declared" means, concretely — recorded 19 September.** Four
documents referred to "the declared interval" and none said what it was,
which makes a bound a reader cannot check. As `scripts/demo-monitor.sh`
runs it:

| Setting | Value | What it bounds |
|---|---|---|
| `price_move_bps` | 500 | A 5% move from the price the standing permission was computed against queues a reassessment |
| `max_evidence_age_seconds` | 300 | Required evidence older than this cannot back a standing permission |
| `refresh_lead_seconds` | 60 | Reassessment is queued this far BEFORE evidence goes stale, so permission is refreshed rather than lapsing |
| `heartbeat_seconds` | 600 | The floor: permission may stand untouched no longer than this, even in a quiet market |

So the worst case for an unprompted update is the heartbeat, **600
seconds**, and a 5% price move cuts it to the next scheduler tick. These
are the demo's values; `config/gateway.example.json` carries the same
scheduler block with `max_evidence_age_seconds` at 3600, and a deployment
may declare its own.

The scheduler is **fail-closed**, which is what makes the bound
meaningful rather than aspirational: if it stops, permission ages out and
risk-increasing actions are refused on chain. Nothing here can extend
permission by being unavailable. Fourteen tests in `gateway/scheduler/`
pin each trigger, including `TestHeartbeatRefreshesStandingPermission`
and `TestStalenessTriggersBeforeExpiry`.

**Measured, 19 September.** From a `demo-monitor.sh` run, polling at
800ms:

```
02:15:30  trigger=staleness   permission=Allowed  tx=0x2ce36f57…
02:15:31  trigger=price_move  permission=Blocked  tx=0xb752fc7f…
```

The price move produced a **`Blocked` monitoring result on chain about a
second after the observation changed**, against a 600-second worst case.
The trigger is `price_move`, not `staleness`: a staleness cycle would
only prove a clock runs, where this shows an observation changing a
standing permission. Both lines carry a transaction hash, so the update
reached the chain rather than a log.
| Effect | Refreshes or blocks risk permission for subsequent risk-increasing actions; future draws still require a new origination assessment |
| Explicitly not | Repricing the outstanding loan, or forcing recovery on its own |
| Failure | Scheduler offline → evidence stales → risk-increasing actions blocked, repayment open. **Fail-closed, never fail-silent.** |

**Changing what an agent may borrow next is not the same as recovering what it already owes.** Continuous underwriting governs permission; recovery governs exposure.

Use `assessment_kind = Monitoring` with the active `loan_id` and its fixed terms, not a fabricated borrower request. Evaluate current marked health, stressed coverage against existing debt and remaining contractual interest, evidence freshness, and account restrictions. The monitoring thresholds are pinned policy; they do not reprice the loan. An `Approved` monitoring result permits only consideration of subsequent risk-increasing actions subject to contract checks. `Denied` or `Unavailable` blocks them. All monitoring results contain **no proposed loan terms** and cannot fund a loan.

The accepted update shares the binding's assessment sequence and supersedes unused quotes. Onchain authorization requires a current allowed risk state bound to the current account state version; expired or absent permission blocks risk increases even if the scheduler is offline. Repayment and permitted recovery do not require an allowed monitoring result. An update with an invalid binding is retained for diagnosis but cannot change another binding's chain state.

## Presentation discipline

Lead with eligibility, limit, APR, stressed coverage, and binding constraints. Display sample size and data origin before any aggregate statistic. Do not invent universal grades.
