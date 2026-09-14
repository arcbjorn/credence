# 07 · Simulation

> **Status, 14 September: built.** `gateway/sim` generates the world,
> `credence-sim` runs the evaluation, and two sealed packs live in
> `fixtures/sim/`.
>
> What is NOT claimed: this is a synthetic population, and this document
> forbids claiming default-model validation from one. The packs report
> what was exercised -- coverage, realized figures, declines -- and carry
> no score, because a number that looked like a grade would invite exactly
> that claim.
>
> The virtual clock never advances live chain time. `scripts/` still drive
> a real chain, with `evm_increaseTime` locally and real waits on a public
> network; the simulation is a separate evaluation, not a replacement.

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

### What the evaluation found — 14 September

Both sets ran against the live engine, one policy and one model hash
throughout. The packs are sealed in `fixtures/sim/` and verify on load; an
edited one fails.

| | development | evaluation |
|---|---|---|
| seed | `0xC0FFEE` | `0xBADBEEF` |
| shocks | the three NAMED scenarios | two named, one **unnamed** |
| originated | 12 | 3 |
| declined | 0 | 653 |
| principal recovered | 84% | **58%** |
| principal lost | 8% | **41%** |

**The unnamed shock cost five times more.** The model stresses to the
scenarios it names -- price, liquidity, combined -- and
`gap-through-the-exit` supplies a price fall and a liquidity collapse
arriving together, through the level where a keeper would have exited.
Realized loss went from 8% to 41% of principal.

That is the difference this document asks to be demonstrated rather than
asserted: **expected stress protection is not guaranteed recovery.**

The 653 declines matter too. The agent must be able to refuse terms it
cannot justify, and the demo must not force an approval to advance. In a
market drifting against it, the agent mostly declined -- and the run
records that as an outcome rather than retrying until something was
approved.

**One placement error, kept in the record.** The evaluation's unnamed
shock was first scheduled for day 62, by which point the agent had stopped
borrowing. The harness reported it as unexercised rather than counting it,
which is how the mistake surfaced; it now falls at day 20. A shock nothing
is exposed to tests nothing, and a harness that counted it would have
overstated coverage.

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
