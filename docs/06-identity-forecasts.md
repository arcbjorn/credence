# 06 · Identity & Forecasts

## Binding

Reference agents by registry chain, address, and agent ID — **never a bare integer.** Bind financing to the tuple present at origination:

```text
(identity_registry_chain_id, identity_registry, agent_id,
 credit_account, operator, facility_id,
 policy_epoch, policy_hash, config_version, config_hash)
```

Policy epoch and configuration version identify pinned records; their hashes commit to record contents. Validate both pairs as specified in [05 · Contracts](05-contracts.md). ERC-8004 supplies **attribution**; Credence supplies **continuity policy**, including handling identity transfer and mutable metadata.

On transfer or configuration mismatch: **freeze new credit, require reassessment.** Existing debt, recovery rights, and the original equity recipient remain explicit in the loan record. **Registry transfer never moves or erases a loan.**

## Anti-farming

Ten instant micro-loans must not approach the weight of real capital held for real duration.

```text
eligible_end_i     = min(repaid_at_i, maturity_at_i)
eligible_seconds_i = max(eligible_end_i - originated_at_i, 0)
weight_i = (principal_i / reference_size)
           * min(eligible_seconds_i, 30 * 86_400) / (30 * 86_400)
```

Apply this successful-repayment weight only to fully repaid loans that did not enter recovery; defaults, recoveries, and breaches remain separate outcome signals. Use actual eligible elapsed time, never the requested contractual duration. Same-block borrow/repay with equal timestamps earns zero weight. A 30-day loan repaid after one day earns one day's exposure, not 30 days'. Per-loan and per-period caps are versioned policy. Evaluate the rational expression in checked fixed-point arithmetic and round credited weight down.

Use chain timestamps for onchain history and explicitly labeled virtual timestamps for synthetic history; virtual age cannot count in an observed-evidence facility. Track overlapping borrowing and known shared operators, but **do not claim complete entity resolution or elimination of Sybil attacks.**

PnL adjusted for external cash flows. **A repayment proves an obligation was settled; it does not prove the strategy generated the repayment.** External top-ups stay visible in the outcome record.

**What proves the duration rule** — added 19 September. The weight is
computed in `gateway/store/derived.go`, not in the engine, and two tests
in `gateway/store/` pin it: `TestSameBlockRepaymentEarnsNothing` is the
anti-farming guarantee, and
`TestRepaymentWeightScalesWithExposureAndDuration` checks the scaling
between them. Run with `go test ./store/` from `gateway/`.

The rule is not theoretical. The 19 September testnet run held each loan
**45 seconds** against the 30-day cap, so each earned roughly
1/57,600th of a full-term loan's credit — and capacity moved from
6,200,000,000 to 6,200,320,000 across three loans. A reader seeing that
flat line should read it as the cap working, not as repricing failing;
[11 · Verification](11-verification.md) records the same effect on the
14 September run.

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

**A and B are release requirements against the selected network's ERC-8004-compatible registry**, with its address and compatibility evidence in the deployment manifest. A local adapter may unblock development, but does not satisfy these acceptance rows or justify an ERC-8004 integration claim. Resolve registry compatibility by the 15 September gate and complete publication and independent reading by 25 September.

### Registry compatibility — resolved 13 September

**ERC-8004 is deployed on Monad testnet.** Verified on chain by
`scripts/probe-testnet.sh`, which is the compatibility evidence this gate
asks for rather than a documentation citation:

| Registry | Address | Evidence |
|---|---|---|
| Identity | `0x8004A818BFB912233c491871b3d84c89A494BD9e` | ERC-721 reporting `AgentIdentity` / `AGENT` |
| Reputation | `0x8004B663056A597Dffe9eCcC1965A193B7388713` | `giveFeedback`, `readFeedback`, `readAllFeedback` selectors present in the deployed implementation |
| Validation | not deployed | Out of scope; this release publishes feedback, not validation |

ERC-8004 publishes two canonical pairs — one shared by every mainnet, one
by every testnet — and the mainnet pair holds no code on Monad testnet.
**Nothing hardcodes either.** `probe-testnet.sh` tries both and reports
which the chain actually has, so pointing at the wrong tier is a loud
failure rather than a silent empty read. Both registries are proxies, so
compatibility is checked against the implementation behind the EIP-1967
slot, not the proxy.

The reader applies the same rule: it refuses an address with no code,
because an absent registry returns the same empty result as an agent with
no history, and those must not look alike.

### Local adapter vs. ERC-8004 — the rewrite, done

**Corrected 14 September.** This section previously opened "The registries
exist and respond. **Credence does not speak to them.**" That was true when
written and is now false: `scripts/demo-erc8004.sh` publishes to the
canonical reputation registry on Monad testnet, and the note at the end of
this section records the evidence. The comparison below is kept because it
is still the reason the two paths are not interchangeable.

| Piece | Local adapter | ERC-8004 |
|---|---|---|
| Publish | `publish((uint256,address,…))` — `0xb1e5ccd9` | `giveFeedback(uint256,int128,uint8,…)` — `0x3c036a7e` |
| Read | `agentEntries(uint256)` — `0x4950dab9` | `readAllFeedback(uint256,address[],…)` — `0xd9d84224` |
| Value | an outcome struct: principal, interest, loss | a signed `int128` with decimals and two string tags |
| Attribution | a single publisher | per-client `clientAddress`, self-feedback refused |

Different selectors in both directions, and a different value model. The
local reader's `agentEntries` call **reverts** against the live registry,
which is why this was a rewrite rather than a configuration change, and why
both paths still exist side by side.

Two gaps that the local path had, and how each was closed:

- **Attribution was supplied, not derived.** The publisher took an agent
  id from its command line and applied it to every loan, without reading
  the registry or checking the binding it came from. **Fixed 14
  September** at the root: the loan now records `agentId` from the
  binding its quote was consumed under, readable afterwards via
  `agentIdOf(loanId)`. Previously the id appeared in no storage slot and
  no event, so the publisher could not have derived it — the flag was a
  missing link in the chain, not laziness.
- **`evidenceRef` was not evidence.** It was the keccak of a loan-number
  string, so the same loan number under different managers produced the
  same reference, and it committed to nothing retrievable. The ERC-8004
  path carries `feedbackURI` plus `feedbackHash` for exactly this and
  uses both, and the loan tag now mixes a `sha256(chainId:manager)`
  deployment digest so two deployments cannot collide.

### The value model, resolved

ERC-8004 stores **one signed `int128` per entry** with two filterable
tags. `docs/06` forbids combining a repayment, a profit, a forecast and
an absence of default into one fact. A composite score would fit the
field and break the rule, so Credence publishes **one entry per signal**,
not one per loan:

| tag1 | value | tag2 |
|---|---|---|
| `credence.outcome` | principal | outcome class |
| `credence.interest` | interest collected | outcome class |
| `credence.loss` | realized loss, **negative** | outcome class |
| `credence.held` | seconds held | outcome class |

All four carry the same `feedbackURI` and `feedbackHash`, so they resolve
to one retained record rather than four disconnected claims. The loss is
negative because the registry's value is signed for that purpose:
publishing it unsigned would read as a gain of the same size to anyone
summing the tag.

### A silent failure mode, found the hard way

**The reputation registry accepts feedback about an agent that does not
exist.** A `giveFeedback` call naming an unregistered token id returns
receipt `status: 1` and writes a row attributed to nothing. It does not
revert.

Found on 14 September when a derivation bug published four signals to
**agent 0** and every transaction reported success. Nothing in the
transaction result distinguished it from a correct publish; the entries
were simply attributed to a token nobody holds. The
`ERC721NonexistentToken` revert seen elsewhere fires only on the
ownership path, not on this one.

Two defences, because an attribution that fails silently is worse than
one that fails:

- The publisher derives the agent from `agentIdOf(loanId)` and **refuses
  to publish when it reads zero** rather than falling back to a default.
- Before sending, it calls `ownerOf` on the identity registry and refuses
  an agent with no owner.

Verified against the deployed registry on Monad testnet, 14 September:

- `giveFeedback(uint256,int128,uint8,string,string,string,string,bytes32)`
  computes to `0x3c036a7e`, matching both the selector recorded above and
  the code behind the registry's EIP-1967 proxy slot.
- A negative value simulates successfully against the live contract.
- Self-feedback reverts `"Self-feedback not allowed"`, and an
  unregistered agent reverts `ERC721NonexistentToken(uint256)`. **Credence
  must therefore be the client, and the borrowing agent a separately
  owned registered identity** -- Credence cannot report on an agent it
  owns.

> **Sub-claims A and B are met as of 14 September**, against the canonical
> registries rather than the local adapter: `scripts/demo-erc8004.sh`
> publishes a resolved obligation to
> `0x8004B663056A597Dffe9eCcC1965A193B7388713` on Monad testnet and
> `tools/reader/read_erc8004.py` reads it back from the chain alone. The
> evidence is recorded in
> [11 · Verification](11-verification.md#independent-reader--erc-8004-14-september).
>
> The local adapter and its reader remain in the tree as the development
> path `docs/08` permits, and `scripts/demo-feedback.sh` still exercises
> them. They do not carry the release claim.
>
> **What the reader still cannot verify is the evidence payload.**
> ERC-8004 returns neither `feedbackURI` nor `feedbackHash` through any
> view, so a consumer cannot retrieve — or even see — the record an entry
> refers to. Provenance is checked; content is not.

**Be precise about what it verifies:** registry provenance — that feedback was published by Credence's declared `clientAddress` — plus any application-level signature over the payload. The standard attributes feedback to a client address and requires consumers to *filter which sources they trust*. It does not make arbitrary registry feedback trustworthy.

The reader demonstrates **attributable, filterable provenance**, not that the content is true. Claim "independently verifiable", never "adopted".

## Forecast commitments

One precisely defined scored event:

```text
Event:       cash-flow-adjusted marked account NAV falls more than 5%
             below its observed peak during the next 20 minutes.
Forecast:    probability in fixed-scale integer units.
Observation: supported account, named price source, fixed sample cadence.
Resolution:  occurred / did not occur / not observable.
```

Twenty minutes is a **demo horizon**, not a claim about fourteen-day forecasting.

NAV is marked assets minus outstanding principal and unpaid accrued interest, normalized for equity deposits and withdrawals via a unitized series — **borrowing adds an asset and an equal liability and creates no profit.** Require positive starting equity; define insolvency as a breach. Sampling cadence is part of the event definition: a sampled maximum drawdown does not claim to observe every intermediate price.

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

**Low in the engine** — the bounded principal cap in [03 · Underwriting](03-underwriting.md), which cannot relax the hard stress floor and does not change APR. The policy maps comparable mean Brier loss to `forecast_penalty_bps`; insufficient eligible resolved observations use its maximum penalty. **High in the demo** — repeated forecast errors visibly constrain subsequent capacity.

Good forecast metrics cannot authorize a loan, prove repayment intent, or remove collateral constraints. Insufficient or unobservable outcomes receive no favorable treatment.

**A single drawdown exceeding a low-probability forecast is an observation, not proof of deception.** The overconfident scenario uses repeated forecasts and a seeded outcome sequence inconsistent with them. Select the scenario before examining displayed results.
