# 07 · Simulation

> **Status, 14 September: not built.** This document describes a design,
> not something in the repository. There is no `gateway/sim`, no virtual
> clock, no fixture pack, and no world generator separated from the
> underwriter.
>
> What exists instead: the demo scripts advance an isolated local chain
> with `evm_increaseTime`, and against a public chain they wait in real
> time (`holdUntilChainTime`). That is enough to show a lifecycle, and it
> is what `docs/11` reports. It is **not** the independent-population
> evaluation this document describes, and no claim anywhere should present
> it as one.
>
> Reported by review, 14 September. Kept rather than deleted because the
> design is still the right one and the schedule may reach it; what was
> wrong was reading as though it had been implemented.

There is no independent borrower population or repayment history yet. The 32-day build cannot produce the multi-month arcs the thesis describes. **Build the synthetic population deliberately and say so.** A model-backed runner can make real proposals while operating on this synthetic economic evidence.

Presented correctly — evaluating an underwriting model against a synthetic population, which is what a risk team actually does — this is more convincing than thin real data, because it exercises failure paths real data could not surface in time.

## Virtual clock

Deterministic queue ordered by virtual timestamp, scenario sequence number, and stable event ID. Pinned generator version and seed.

Each run records generator version, seed, input distribution, schedule, agent configuration, model and policy hashes, and all generated events. **A fixture pack is immutable once used in a recorded demonstration.**

Concurrency may fetch independent providers, but scenario events apply in deterministic order. **The simulator manipulates only scenario state; it never advances live chain timestamps.**

Local-chain tests may advance their isolated test clock explicitly. Public-network demonstrations use real chain timestamps for interest, quote expiry, and maturity; use declared short loan terms or recorded transaction traces when needed. Imported virtual history remains synthetic and is accepted only by a scenario facility. Do not present virtual elapsed time as the age of a public-chain loan or its forecast window.

Events include market moves, liquidity changes, agent decisions, funding, execution, repayments, top-ups, stale-data periods, forecast resolution, and failed recovery.

## Independent outcome generation

```text
World generator          Underwriter
─────────────────        ─────────────────
prices                   sees only evidence
execution costs   ──X──► available at that
fills                    point in time
outcomes                 (no future events,
hidden parameters        no hidden params)
```

Use a **development scenario set** and a **separate evaluation set** with different seeds and shock schedules. Include a shock **outside** the model's named scenarios — this demonstrates the difference between expected stress protection and guaranteed recovery.

**Do not claim default-model validation from a small synthetic population.** Report what was exercised: failure-path coverage, monotonic constraints, response latency, replay equality, realized losses within declared scenarios.

Measure what the implemented collection path realizes across several recovery outcomes, **including near-zero recovery.**

## Agent runner

The agent lives outside the risk core with structured tools: inspect account, propose allocation, estimate forecast, request or decline terms, execute, repay. Every action validated against deterministic rules.

One model-backed agent supplies a retained proposal and acceptance trace, shown live or as an explicitly labeled replay in the demo. The runner records model identifier, prompt and configuration hash, tool calls, proposed forecast, expected-benefit estimate and costs on the loan horizon, and decision. **Credentials never enter the risk snapshot or browser.**

Use prerecorded decisions or a deterministic policy agent for regression runs, and **display replay mode explicitly.** A replayed trace demonstrates system behavior; it must not be presented as a fresh model decision.

**The agent must be able to decline** when expected benefit, on the correct horizon after costs, does not justify the offered terms. **The demo must not force an approval to advance the script.**

## What the strategy does, and why it is financed

**The activity.** The agent acquires a single risky asset at a whitelisted
spot venue, holds it for the loan's term, and sells it back. One asset,
one venue, no leverage beyond the loan itself. `StrategyAdapter` is the
only route: it pulls a bounded approval, trades, and returns the proceeds
to the credit account, so the account is always the counterparty and the
output cannot be redirected.

**Why it needs credit.** The agent's edge is in *timing and selection*,
not capital. Its own equity bounds position size, so a correct call on a
small book earns proportionally little. Borrowing lets it take the
position its analysis actually supports — which is the ordinary reason any
trading desk uses financing, and the reason lenders can be paid for it.

**Why a lender would fund it.** The position is custodied where the lender
can reach it. The agent never touches the principal: borrowed funds land
in a `CreditAccount` that refuses withdrawal while debt is open, binding
the owner as strictly as the agent. If the position deteriorates, the
keeper can exit it without the agent's cooperation. The lender is exposed
to the asset's price, not to the agent's honesty.

**Accept / decline logic.** The engine sizes the loan against the
*post-draw* portfolio under three stress scenarios, and grants the
requested amount or denies — never a partial fill. It declines when
stressed coverage fails, when the account is already concentrated, when
required evidence is missing or stale, or when the price the schedule
produces exceeds the borrower's stated maximum. Good history lowers the
rate and raises the limit; it never overrides a current-risk denial.

**Exit.** Three ways, in decreasing order of agency:

1. The agent closes the position and repays from the proceeds.
2. Monitoring withdraws permission, and the agent must unwind — a
   risk-*reducing* action, so it needs no fresh quote.
3. Maturity passes or permission is withdrawn, and the keeper opens
   recovery, sells the collateral, and collects up to the obligation. What
   the market fails to return is a realized lender loss.

> **This is a prototype strategy on a scenario venue.** It is chosen
> because it is simple enough to verify end to end — one asset, one venue,
> a price source that can be moved — not because spot directional trading
> is the intended production use. The financing machinery is the claim;
> the strategy is the smallest thing that exercises it honestly.

## Three scenarios

| Scenario | Initial condition | Required behavior |
|---|---|---|
| **Performer** · 184 | Sufficient equity, supported allocation, no eligible repayment history at loan 1, reasonable forecast | Bounded approval, execution, repayment, documented history update, improved next terms; **three sequential loans** |
| **Concentrated** · 207 | Good prior repayments, excessive current risky exposure | Denial or lower cap **despite favorable history**; attempted additional risk fails |
| **Overconfident** · 311 | Repeated understated probabilities, adverse outcomes | Forecast penalty, **no invented fraud claim**, recovery after health or maturity trigger, visible realized lender loss |

Variants test stale data, new identities, mismatched configuration, unavailable liquidity, and a prohibited withdrawal.

**No agent ID receives special underwriting logic.** One engine, one policy, three inputs.

## Synthetic market activity

**Market activity generated by the project remains synthetic economic evidence even when transactions settle on a public chain.** Two controlled participants placing orders is a controlled market whose observations have no independent validation — not organic demand, and not independent price discovery.

Store series terms, remaining maturity, best quotes, observed depth, last fill, timestamps, and known participant origin. **A last trade is not automatically a liquid executable price.**
