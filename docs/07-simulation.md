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
| agent declines | 0 | 0 |
| principal lent | 28.93e9 | 10.84e9 |
| principal lost | **0** | **549.7e6** (5.1%) |
| borrower equity left | 1.83e9 | **0** |

**Rewritten 14 September, twice.** The figures above replace an earlier
table, and both corrections are worth recording because the first version
supported a claim the packs did not.

The earlier table reported 8% and 41% principal loss and concluded that
**"the unnamed shock cost five times more."** Two defects produced those
numbers:

- The runner deployed only the BORROWED principal and left the agent's
  equity in settlement, then recovered from the risky sale alone. Equity
  absorbed nothing, so losses were charged to lenders that the borrower's
  own capital should have taken first. Reverting the fix reproduced it: a
  2.4e9 lender loss beside 10e9 of untouched equity.
- The exit charged a hardcoded 500bps liquidity haircut while the shipped
  policy declares 2000. The world let positions out at a quarter of the
  modelled cost.

#### What these packs do and do not show

They show that **agent equity is the first loss layer, and it works**:
the development set loses nothing and ends with 1.83e9 of borrower capital
intact; the evaluation set's unnamed shock exhausts equity to zero and
only then takes 5.1% of principal from lenders.

They do **not** isolate the cost of the unnamed shock. The two sets differ
in seed, volatility, book depth, drift and loan count as well as in shock
schedule, so the difference between them is not attributable to the shock
alone. A controlled ablation -- one world, one schedule, the unnamed shock
present or absent -- is what would support that claim, and it has not been
run. The earlier "five times more" sentence did not have it either.

**There are no agent declines in either pack.** An earlier version of this
document reported 653 and built a paragraph on the agent refusing terms it
could not justify. That figure counted engine REFUSALS -- `Denied` on
stress capacity, mostly after a book had already fallen -- not the agent
exercising its own judgment. The agent's ability to decline is real and
tested (`TestAgentDeclinesTermsItCannotJustify`), but these two sealed
runs do not exercise it, and the table now says 0 because that is what
happened.

**There is no keeper.** An earlier sentence described the unnamed shock as
arriving "through the level where a keeper would have exited". Nothing in
this harness exits a position early: open loans are marked at maturity or
at the world's end. What the evaluation shows is a mark-to-market loss,
not a failed liquidation.

**One placement error, kept in the record.** The evaluation's unnamed
shock was first scheduled for day 62, by which point the agent had stopped
borrowing. The harness reported it as unexercised rather than counting it,
which is how the mistake surfaced; it now falls at day 20. A shock nothing
is exposed to tests nothing, and a harness that counted it would have
overstated coverage.

**Two generator defects, also corrected 14 September.** Both were found by
review rather than by the harness:

- `DriftBpsPerDay / stepsPerDay` was integer division, so 15 bps/day over
  24 hourly steps truncated to **zero**. Both worlds had no drift at all,
  while the evaluation set documented itself as "a market that drifts
  against the agent". Drift is now carried as a remainder.
- A shock's label stuck to every later observation, so a loan opened weeks
  afterwards counted as having "exercised" it -- the same overcount
  already fixed for idle accounts, one layer down.

**Do not claim default-model validation from a small synthetic population.** Report what was exercised: failure-path coverage, monotonic constraints, response latency, replay equality, realized losses within declared scenarios.

Measure what the implemented collection path realizes across several recovery outcomes, **including near-zero recovery.**

## Agent runner

The agent lives outside the risk core with structured tools: inspect account, propose allocation, estimate forecast, request or decline terms, execute, repay. Every action validated against deterministic rules.

One model-backed agent supplies a retained proposal and acceptance trace, shown live or as an explicitly labeled replay in the demo. The runner records model identifier, prompt and configuration hash, tool calls, proposed forecast, expected-benefit estimate and costs on the loan horizon, and decision. **Credentials never enter the risk snapshot or browser.**

Use prerecorded decisions or a deterministic policy agent for regression runs, and **display replay mode explicitly.** A replayed trace demonstrates system behavior; it must not be presented as a fresh model decision.

**The agent must be able to decline** when expected benefit, on the correct horizon after costs, does not justify the offered terms. **The demo must not force an approval to advance the script.**

> **As built, 14 September.** There is **no model-backed agent**. The
> section above is the specification; what exists is the deterministic
> policy agent it permits for regression runs — `sim.MomentumAgent`, a
> momentum rule with a lookback, a leverage fraction and a maximum
> acceptable APR. No model identifier, no prompt hash, no tool-call trace,
> because there is no model in the loop.
>
> It does decline, and `TestAgentDeclinesTermsItCannotJustify` pins that:
> offered an absurd rate, it refuses rather than advancing the script.
> Neither sealed pack happens to contain a decline — see the note above.
>
> A reader should take the paragraphs above as what a model-backed runner
> would have to record, not as a description of this repository.

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
