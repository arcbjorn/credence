# 08 · Integrations

## The rule

Five deliverables each: documented capability check, normalized adapter, fixture-based contract test, visible product use, defined failure behavior.

> **An integration is complete only when the submitted environment demonstrates its actual behavior. A logo, an installed SDK, or an architecture box does not satisfy this.**

Status: `Unverified` · `Proven in spike` · `Integrated` · `Accepted` · `Deferred`.

**The baseline must work with zero optional providers.** Each has a defined omission behavior. Required chain access, custody execution, and release identity verification remain dependencies. A fallback for development does not automatically satisfy release acceptance, and no fallback changes the evidence or authorization policy of an existing facility.

## Matrix

| Integration | Capability check | Acceptance | If unavailable |
|---|---|---|---|
| Monad RPC | Chain, tokens, read consistency, transactions, explorer | Full state plus a successful and a reverting transaction | Another RPC; selected-chain deployment still required |
| Execution venue | Assets, custody, approvals, liquidity, pricing, bounded exit | Trade and unwind, proceeds to account | Labeled scenario venue; no external-market claim |
| ERC-8004 | Registry addresses, ABI, binding and feedback permissions | Registered identity, attributable feedback, transfer handling, independent reader on the selected network | Local adapter for development only; integration incomplete and release identity gate remains open |
| Envio | Events, lag, reorg handling, hosting | Reconstruct lifecycle with evidence links | Direct log indexer is baseline. **Synced against Monad testnet over HyperSync, rendered on screen, and rollback demonstrated through a simulated reorg — 18 Sep** |
| Perpl | Supported endpoints on chosen network | A documented field changes an assessment | Feature absent |
| Nansen | Account coverage, quotas | A sourced feature demonstrably affects a decision | Omit, disclose coverage |
| CRE | Deployment access, network and forwarder, consumer path | Valid report accepted; invalid bounds and wrong routes rejected | Separate development-attested baseline; an existing CRE facility blocks new risk and cannot fall back to a development key |
| Kuru | Market creation, quote asset, fills, redemption | Tradable claim with completed redemption | **Feasibility run 19 Sep: creation is permissionless, ~0.133 MON, no blocker.** Fills and redemption remain unbuilt, so it stays omitted from working-product claims |
| Agent wallet | SDK and policy boundary | Agent signs permitted action; prohibited custody fails | Basic runner wallet; no plugin claim |

## Strategy — economic purpose required

"One risky asset, one adapter" says what is *permitted*, not why an agent would *borrow*.

**By 14 September**, specify in one paragraph: the activity, why it needs financing, how the agent decides financing is worthwhile at the offered APR, and how it exits. Without this, demo beat 2 — where the agent accepts or declines terms — has no content.

**Answered 13 September** in [07 · Simulation](07-simulation.md#what-the-strategy-does-and-why-it-is-financed):
spot acquisition of one risky asset at a whitelisted venue, financed
because the agent's edge is timing and selection rather than capital, and
lendable because the position is custodied where the keeper can reach it.
Every claim there is enforced by code — bounded adapter approvals, exact
amount or deny, de-risking exempt from fresh permission, recovery only on
maturity or withdrawn permission.

### Assets, venue, and valuation — settled 13 September

Due 14 September. Working default taken: named test assets and an isolated
scenario venue.

| Decision | Choice | Why |
|---|---|---|
| Settlement | `TestSettlementToken`, **6 decimals**, named *Credence Test Settlement (NOT USDC)* | The name carries the disclaimer, so a block explorer cannot make it look like the real thing |
| Risky asset | `TestRiskyAsset`, **18 decimals**, named *Credence Test Risky Asset (NOT ETH)* | A decimal mismatch against settlement is deliberate: it exercises the rescaling that a same-decimals pair would hide |
| Venue | `SpotVenue`, seeded with **1,000 risky units** | Isolated and operator-priced, so a bad fill can be produced on demand and the slippage guard is testable rather than asserted |
| Mark | `FixedPriceSource` at **2000.000000** (6dp), read on chain | An *observed* price, not a policy constant. Positions mark to it, so a market move changes a decision rather than only a trigger |
| Declared depth | **2,000,000,000,000** settlement units | The venue's real liquidity. Declaring more would make every exit look costless — the same failure as omitting the haircut |

The price source is separate from the venue on purpose. A whitelisted
venue is not evidence of a fair price, so the adapter checks its fill
against an independent source and reverts when they disagree beyond
tolerance. Moving both together is a market move; moving one is a bad
fill, and the two must be distinguishable.

> **Everything here is a test asset on a scenario venue**, labelled as
> such in its own token name. No claim is made about production liquidity,
> and the declared depth is the venue's actual holding rather than an
> aspiration.

### Why the basis trade is out of v0

Two common arguments against it are **wrong**, and the record matters because it changes what to check at the venue gate.

*Wrong: "margin sits off-chain, outside the custody domain."* Perpl is fully onchain on Monad, with a real Exchange contract on mainnet and testnet, and `createAccount(uint256)` is callable **onchain with collateral and no API authentication**. A contract can open and fund the account.

*Wrong: "the perp leg is unrecoverable, so the failure path cannot cover it."* Two documented facts rescue custody:

- **Withdrawals are never permitted via an API key, regardless of scope.** A trade-scope key cannot move margin out. Withdrawal stays onchain, gated by `CreditAccount`. **The owner-cannot-withdraw invariant holds.**
- Perpl's own liquidation engine closes undercollateralized positions and returns the bulk of remaining margin to the trader — landing **inside** the custody domain if the trader is `CreditAccount`.

So a workable shape exists: `CreditAccount` owns the Perpl account; an offchain keeper holds a trade-scope key it can use to close the hedge but never to withdraw. The cost is a **downgrade, not a break** — recovery timing depends on a keeper acting rather than being contract-enforced.

**The actual blocker is the calendar.** Six unresolved unknowns, all on the critical path:

1. Does `createAccount` succeed when called from a contract?
2. May `target_profile` in a delegated enrollment be a contract address?
3. Does enrollment accept EIP-1271? *(unmentioned in the docs — assume no)*
4. Can a keeper key close positions reliably under stress?
5. How is a perp position marked onchain for coverage math?
6. How does isolated margin compose with account coverage?

**Partial spike, 12 September — unknowns 1–3 probed against testnet.** Exchange proxy `0x1964…80cc` resolves to implementation `0xbcbd…adbb` (128 KB).

| # | Question | Result | Evidence |
|---|---|---|---|
| 1 | `createAccount` callable from a contract? | **Likely yes, unproven** | Selector `cab13915` present. A contract caller and an EOA caller returned the *identical* revert `0xcfe73bb0(caller, amount)` — a balance/allowance error, not caller-type rejection. No EOA-only guard surfaced. Proof needs a funded contract transaction. |
| 2 | May `target_profile` be a contract address? | **Unresolved** | Docs define it only as "the delegated account address". No onchain operator functions (`isOperator`, `setOperator`) exist in the implementation; delegation is validated server-side by an undocumented mechanism. Needs a live enrollment attempt. |
| 3 | Enrollment accepts EIP-1271? | **No — treat as closed** | `isValidSignature(bytes32,bytes)` absent from the implementation. Enrollment requires a secp256k1 EIP-712 wallet signature; EIP-1271 and ERC-6492 are unmentioned. A contract cannot sign enrollment. |

Unknown 2 is now load-bearing and is **not resolvable from public documentation or chain state.** Unknowns 4–6 are build questions this spike did not touch.

Spot-only carries **one** unknown: which venue. Eleven days to the baseline, one builder, custody plus recovery is the entire foundation. **Six critical-path unknowns do not fit; one does.**

**Retained as strategy adapter #2** with a bounded 2-day spike after the baseline is pinned, resolving unknowns 1–3. The venue capability to check is **contract-callable trading and a contract-callable forced unwind** — not "is it onchain", which Perpl already is. An exchange's own liquidation mechanism does not supply Credence's required recovery *timing*.

## CRE — what it proves

Consensus across independent nodes on a workflow that fetches your endpoint means **N nodes agreeing on the same fabricated response.** Consensus protects workflow *execution*, not input *truth*.

Two adapters, separate authorization domains: development EIP-712, and a CRE receiver validating expected forwarder and workflow identity. **Each facility pins its required route for the epoch.** A facility claiming CRE verification requires its CRE path for both origination and monitoring; a development key cannot remain an equally powerful alternative. The baseline development facility is separately bound. Adapter authentication precedes normalization into the common fields in [05 · Contracts](05-contracts.md).

| Level | What was established |
|---|---|
| Development attestation | A trusted signer approved the payload |
| CRE policy-checked report | The deployed workflow checked binding, freshness, allowed identifiers, explicit bounds |
| CRE recomputed assessment | The workflow ran pinned model logic on the committed input. **Claim only if demonstrated** |

**A returned `model_hash` string does not prove that model ran.**

The first useful workflow performs five named checks:

1. Independently read account and facility state and match the identity, configuration, policy, and state-version binding.
2. Check the assessment kind: origination matches its committed request; monitoring matches the existing loan and contains no origination terms.
3. Validate permitted identifiers and explicit arithmetic and policy bounds, including amount, APR, term, and risk permission where applicable.
4. Validate input and evidence commitments against the retained payloads.
5. Check sequence and validity windows; required evidence freshness is necessary for an allowed result. A stale-evidence monitoring report may only publish blocked permission.

State which upstream data remain trusted — **identical responses from one project-controlled API do not establish independent evidence.** These checks do not establish model recomputation.

Contracts retain their own exposure, epoch, state, and expiry checks because state changes after workflow execution. Prove rejection of at least one fabricated out-of-bounds assessment; **do not claim this establishes the truth of every in-bounds assessment.**

Request deployment access during milestone one. Preserve the distinction between local simulation and a deployed workflow.

### Availability — resolved 13 September

**CRE supports Monad testnet.** Verified on chain, not from documentation:

| Item | Value |
|---|---|
| Chain name | `monad-testnet` |
| KeystoneForwarder | `0xF8344CFd5c43616a4366C34E3EEE75af79a74482` — 8,592 bytes deployed |
| MockKeystoneForwarder | `0xB9F79d863261869B234c481D1f9A7af84AeAd192` (simulation only) |

Access is **gated but self-service**: `cre account access` from the CLI
submits a request with a one-line use-case description, answered by email.
Simulation needs no approval — `cre workflow simulate` runs immediately,
and only `cre workflow deploy` waits on it.

That matters for the claim ceiling. This document already distinguishes a
**CRE policy-checked report** from a **recomputed assessment**; the further
distinction is between a *simulated* workflow and a *deployed* one. Only a
deployed workflow supports an integration claim, and the forwarder address
above is what the receiver contract must validate `msg.sender` against.

Run `scripts/probe-testnet.sh` to re-verify.

### CRE — built, simulated, not yet delivered on chain

**Corrected 14 September.** This section previously said there was no
receiver, no forwarder authentication, no workflow, and no SDK. All four
now exist, and leaving that text standing would let a reader quote this
document against `docs/11`.

What exists:

| Piece | State |
|---|---|
| `CREReceiver.sol` | Deployed on Monad testnet, 21 tests |
| `cre/underwrite` | Compiles to WASM, runs under `cre workflow simulate` |
| `credence-assessd` | Serves the assessments the workflow reads |
| CRE CLI v1.33.0 | Installed, authenticated |

The receiver validates `msg.sender` against the forwarder above,
implements ERC-165, checks the workflow owner and id, requires the loan to
be enrolled by the operator, and consumes each report once.

#### What this still is not

Being precise, because "CRE integration" reads broader than what holds:

- **No DON-signed report has reached the live receiver.** The simulator's
  `Written: 1` means the write path executed and was accepted in
  simulation; it is not a transaction. Deploy access is pending.
- **The workflow is a monitoring transport.** It asks
  `credence-assessd` — a project-controlled HTTP service — for an
  assessment and reaches consensus on the reply. That is N nodes agreeing
  on one source, which is weaker than N nodes independently reading chain
  state. The checks this document specifies for a CRE claim (independent
  account and facility reads, bounds, evidence commitments) are performed
  by the ENGINE against a gateway-built snapshot, not by the workflow.
- **Origination cannot use CRE.** A monitoring report carries no grant by
  construction, so a CRE-pinned facility can monitor but cannot fund. A
  CRE-only facility is not possible in v0.
- **`acceptFromRoute` is unsigned by design**, authenticated by caller
  rather than by EIP-712. Anyone the operator allowlists for `Route.CRE`
  can record a full assessment. That is the trust model `docs/05` states,
  and it means a mistaken `setSigner(Route.CRE, eoa, true)` is a funding
  door.
- **`credence-assessd` is credentialed but still a single source.**
  Since 14 September it requires `Authorization: Bearer <token>`, and
  refuses to start on a non-loopback listener without one; the workflow
  reads that token from CRE's secret store rather than from its committed
  config. That closes "anyone who can reach the port can drive the
  permission oracle". It does not change the paragraph above it: N nodes
  still agree on one project-controlled reply.
- **The metadata layout was observed on the MOCK forwarder**, not the
  production Keystone one. If production metadata is not exactly 64 bytes
  with the owner at `[42:62]`, every report reverts — loudly, which is the
  right failure, but untested against the real path.

> The honest sentence: **CRE authentication is built and simulated. No CRE
> facility has originated or monitored on chain.**

## Envio — lifecycle from the log, 18 September

`indexer/` is a HyperIndex project over the `LoanManager` event log.

### Why it is not a bolt-on

The gateway reads loan **structs** over `eth_call`. That answers "what is
true now" and cannot answer "how did it get here": a struct read at
maturity shows a repaid loan, not when recovery opened, how many
collections it took, or what each one recovered. The contract keeps
running totals and two timestamps — enough to report an outcome, not
enough to reconstruct the path to it. Those intermediate facts exist only
in the log.

> Stated carefully, because an earlier draft of this section overstated
> it. Held duration **is** derivable from a struct: the contract retains
> `originatedAt` and `resolvedAt` on purpose, and `credence-publish`
> computes `held` from exactly those. The indexer's claim is narrower and
> still true — the *sequence* is not recoverable from state, only the
> endpoints.

So the indexer is the only component that can reconstruct a lifecycle,
which is the `docs/11` row it serves — "replay an indexed loan after
cursor rollback or simulated reorganization". `rollback_on_reorg` is set
rather than left to default.

### The capability check, run rather than read

| Item | Result |
|---|---|
| HyperSync supports Monad testnet | **Yes.** `chain_id` 10143, tier GOLD, confirmed against `chains.hyperquery.xyz/active_chains` |
| Public RPC `eth_getLogs` range | **Capped at 100 blocks** (`-32614`), which is the plainest argument for HyperSync: the manager deployed at block 62,425,291, so reconstructing its lifecycle over that RPC is ~620,000 sequential requests |
| Manager deploy block | 62,425,291, found by bisecting `cast code` because the log range cap makes a scan impractical |
| anvil | No HyperSync endpoint, so 31337 syncs over RPC. HyperSync serves the chain it actually supports; the throwaway local chain is read directly |

### What is verified, and what is not

- **The fold is tested.** `indexer/src/lifecycle.ts` holds the whole of
  the reasoning — what a `Loan` row looks like after each event — as pure
  functions with no Envio context, and `lifecycle.test.ts` pins them
  against what `docs/05` requires: repeated collections accumulate rather
  than overwrite, a collection is not terminal, and collected plus
  written off accounts for the whole principal. Eleven tests. Reverting the
  accumulation to an assignment fails the collections one.
- **Handlers typecheck against the generated schema**, so an entity field
  that does not exist is a build error rather than a silent null.
- **A full sync has now been run, 18 September.** Against the local chain
  the `demo-default.sh` run left behind — deliberately the default path
  rather than the simple repayment, because it exercises recovery,
  partial collection and write-off. Envio reported `All chains are caught
  up` and `The indexer is ready`, and the stored rows were read back from
  Postgres:

  ```
     id    |  status   | principalRepaid | principalLoss | heldSeconds
   31337-1 | Defaulted |      1400000000 |    2600000000 |      432002

   Originated  b30/i4 · RecoveryOpened b36/i1 · Collected b37/i3 · Defaulted b38/i2
  ```

  `principalRepaid + principalLoss` is exactly the 4,000,000,000
  principal, and `principalLoss` matches the `realized_loss=2600000000`
  the demo printed from chain state — derived here from the log alone,
  with no `eth_call`. The GraphQL API serves the same rows including the
  `events` relation, and the `Defaulted` row's transaction hash resolves
  on chain to block 38 against the manager the manifest names, so an
  indexed row is checkable without trusting the indexer.

- **`scripts/index-local.sh` is not yet reliable.** The sync above was
  produced by running `envio dev` directly. The script performs the same
  steps and works up to the point of launching it, but its redirected
  output swallows envio's own logging, so a run that stalls is
  indistinguishable from one that is working. Four environment
  collisions are handled in it and each was hit for real on this
  machine — Postgres port 5433 held by another project, IPv4 8080 held
  by an unrelated server while Docker publishes on IPv6, a leftover
  indexer on 9898, and codegen running against a different config than
  the indexer. **Treat the script as a record of those traps rather than
  a one-command path**, and run `envio dev -r --config config.local.yaml`
  from `indexer/` when it misbehaves.

- **A screen reads it — the "Indexed" view, added 18 September.** It
  renders the loan beside the ordered event log that produced it, and
  prints the conservation check in words:
  `1,400 recovered + 2,600 written off = 4,000 principal`. Deliberately
  separate from "Loan lifecycle", which renders what the gateway
  retained: merging them would hide which source each figure came from,
  and the point of this one is that the figures came from the log alone.

  It follows the same rule as every other screen. `scripts/build-indexed-data.sh`
  reads the rows the indexer stored into the bundle, and the page renders
  the bundle — a screen querying a local GraphQL endpoint would show
  nothing in the submitted static folder. The bundle step is the only
  optional one, because it is the only one needing Docker; absent it, the
  screen says the sync has not been run and gives the command, rather
  than rendering an empty lifecycle that reads like a loan with no
  events.

- **The export is tested, and the tests caught a real defect.**
  `web/src/lib/indexed.test.ts` pins that amounts stay strings, that a
  resolved loan reconciles, that every loan opens with `Originated`, and
  that events are in block order. The ordering test failed on first run:
  the export cast `blockNumber` to text before sorting, so Postgres
  ordered it lexicographically and block 6 landed after block 14 —
  origination rendered last. Fixed by sorting on the numeric column and
  casting only in the projection.

### The deployed chain, over HyperSync — 18 September

With an API token in `indexer/.env`, `scripts/index-testnet.sh` indexes
**Monad testnet itself**, not a local double. What it read:

```
   id    | status | principal  | principalRepaid | interestCollected | heldSeconds
 10143-1 | Repaid | 4000000000 |      4000000000 |               625 |          41
 10143-2 | Active | 4000000000 |               0 |                 0 |
```

Both loans on the manager the manifest names. Loan 1 repaid in full;
loan 2 is open, and carries no resolution rather than a zeroed one. The
41-second hold is the `HOLD_SECONDS=45` the testnet run used — the same
short hold `docs/11` records as the reason the repricing story does not
reproduce there.

**Checked against the chain rather than trusted.** The indexed `Repaid`
row's transaction hash resolves on Monad testnet to block 62,425,571,
status success, `to` the manifest's loan manager `0x40dc8Dd6…`. A reader
can repeat that with `cast receipt` and no Credence service.

**~1.1 million blocks in about 2.5 seconds**, from the manager's deploy
block to the head. The same range over the public RPC is impossible in
practice: it caps `eth_getLogs` at 100 blocks, so this is ~11,000
sequential requests. That gap is the integration's argument.

### Reorg handling, demonstrated — 18 September

`docs/11` asks for a replay after a cursor rollback or a simulated
reorganization. `scripts/verify-reorg.sh` performs one and asserts the
outcome:

```
=== before ===
  loan 1 is Repaid, 3 event(s) indexed
=== reorganising ===
  Repaid is in block 11, head is 16 -- rewinding 6 block(s)
  block 11 was replaced (hash changed)
=== waiting for the rollback ===
  loan 1 is Active again -- the withdrawn repayment was withdrawn
  chain holds 2 log(s) for this manager
=== replaying the repayment ===
  loan 1 is Repaid again, now from block 22 (was 11)

PASS  indexed state followed the chain through a reorganisation
```

**A terminal outcome is inverted, which is the strongest version of this
test.** `Repaid` is the state a lender acts on: an indexer that does not
roll back keeps serving a repayment the chain no longer has, and looks
identical to a correct one until exactly that moment. The indexer's own
log names the range it discarded — `fromBlock: 16, toBlock: 6,
rollbackedEvents: 2` — and block 6 is precisely the last block the
reorganization left intact.

Both directions are checked. Rolling back alone would pass if the
indexer simply dropped everything, so the replay asserts the repayment
returns **at the new block 22 rather than the reorganized-away block
11**, and the event count is compared against `eth_getLogs` on the same
manager.

The mechanism is `anvil_reorg`, so this is anvil-only by construction and
the script refuses any other chain id — asking a public chain to
reorganize would be asking it to rewrite someone else's history.

> The honest sentence: **the indexer syncs the deployed chain, its output
> reconciles against that chain transaction by transaction, a screen
> renders it, and it follows the chain through a reorganization.** The
> reorg check runs against anvil; no reorganization has been observed on
> Monad testnet, because one cannot be requested.

## Kuru — gated extension

**Feasibility experiment by 15 September:** bounded test of market creation for an existing token, settlement-token quote, fill, holder redemption. Produces an estimate; commits nothing.

### Feasibility — slipped, then run: 19 September

**It was not run by 15 September, and nothing recorded that.** Recorded
here rather than left implicit, for the same reason `docs/10` records the
screen-shell slip: an unflagged slip on a dated gate is exactly what a
schedule exists to surface. It was run on 19 September, four days late.

Every line below was established **on chain**, not from documentation, and `scripts/probe-kuru.sh` re-runs the whole check. It sends no transaction and creates no market, so a reader needs no funded key.

| Contract | Address | Bytes |
|---|---|---|
| Router | `0x7EFbE105Ca7415dE98F96622173458ac1c054630` | 141 (EIP-1967 proxy) |
| Router implementation | `0xaaa0f0c4d49d09ef33ae758d88afab810ecbe1ed` | 10,389 |
| Margin account | `0xd029C2D98ff85D8F64799017fE00a59B1159CE02` | 141 |
| Kuru deployer | `0xDacd06372cEb638640c9D8466A023b7362324e1A` | 6,703 |
| Forwarder | `0x681bB1508E14433b148a2549ba2726454aDc9BB4` | 141 |

`TRUSTED_FORWARDER()` on the Router returns the forwarder address above,
so the documentation and the chain agree, and
`orderBookImplementation()` returns
`0x72caE0a99C19B574e8a6De558F43fc1D019c9374`.

**The ABI was confirmed against the bytecode, not taken on trust.** 24
selectors were extracted from the deployed implementation; ten did not
resolve in the public signature database, and the signature the SDK
*prose* documents (`deployMarket(...)`) matches none of them. The
published ABI names `deployProxy` with eleven parameters instead, whose
selector `ce186ec3` **is** in the extracted set, as is
`computeAddress` at `1444ccb6`. The prose describes an SDK wrapper; the
contract takes `deployProxy`.

#### What the experiment answered

| Question | Answer |
|---|---|
| Is market creation permissionless? | **Yes.** `deployProxy` simulates successfully from an ordinary unprivileged address |
| Cost to create a market | **1,301,114 gas ≈ 0.133 MON** at the observed 102 gwei |
| Does it accept our assets? | **Yes** — simulated with the deployment's own `TestRiskyAsset` / `TestSettlementToken`, returning market `0x3a4DEFF4029FEEc71b8AA93109DEfbE831e1cB30` |

One trap worth recording: `deployProxy` reverts with **empty revert data**
when handed addresses that hold no code. That reads exactly like an
access-control rejection and is not one — the first attempt used
placeholder addresses `0x1`/`0x2` and looked like a permission gate. Real
ERC20s succeed from the same caller.

> **The 25 September gate's feasibility condition is met: no blocker.**
> The venue is live, creation is permissionless, the cost is negligible,
> and it accepts the deployment's own tokens.
>
> **That is not the same as passing the gate.** The gate also requires the
> baseline to demonstrate custody restrictions, repayment, recovery and
> loss accounting — which it does — and the 2 October gate then requires
> **origination, market transfer, collection, redemption and a tested
> default haircut**. None of that exists: `NoteSeries` is not built, and
> what this experiment priced is *creating a market*, not *tokenising a
> receivable*. `docs/08` states that **no late baseline contract rewrite
> is justified to include Kuru**, and a `NoteSeries` with its own
> redemption waterfall is precisely such a rewrite. The remaining decision
> is a scope decision, not a feasibility one.

**Structure.** A separate `NoteSeries` funding route for one loan in an isolated deployment. **Never tokenize a receivable belonging to the baseline pooled vault** — each `LoanManager` binds to its allowed funding source. The series defines settlement asset, target principal, borrower, term, rate, maturity, recovery waterfall, and redemption method before funding. Fixed supply issued at origination; notes are a pro-rata claim on that loan's actual net collections. Early repayment pays accrued interest only — **the note does not promise unearned full-term interest.** Default can redeem below principal; pending recovery means pending redemption.

**The same loan must never appear as both a redeemable pooled-vault asset and an independently redeemable note claim.** Notes are not eligible collateral in this release.

### Market feedback stays informational

Show fundamental assessment and market-implied pricing **separately**. Do not convert note price into real-world PD without explicit models for recovery, timing, interest, and liquidity premiums.

**Why.** The note market is two participants you control. That does not imply zero displayed depth — it implies the market is **controlled, with observations lacking independent validation.** A price nothing independent produced cannot move executable credit.

**What would close it is unsettled.** A depth-based gate is a *candidate* policy requiring adversarial evaluation:

```text
CANDIDATE — unproven:
  observed depth   >= a declared multiple of the influence it can exert
  participants     >= 3 independent, none project-controlled
  observation age  <= freshness bound
  influence        <= bounded fraction of limit, tighten-only, rate-limited
```

**Do not claim a depth threshold makes manipulation unprofitable.** Comparing cost-to-cross-the-book against a financing benefit does not establish an attacker's economic loss: trading notional is not money irreversibly spent, true cost depends on the executable curve and the unwind, displayed liquidity can vanish or belong to cooperating participants, three addresses are not three independent actors, and the payoff can sit outside the affected facility. **Tightening is itself exploitable** — it can force sales benefiting an attacker's other positions.

The honest statement: **the market feedback loop is a meaningful future capability whose economic design is outstanding.** The counterfactual panel demonstrates the *mechanism* against synthetic prices; it does not show the mechanism is safe to connect.

**Gates.** 25 Sep — proceed only if the baseline demonstrates custody restrictions, repayment, recovery, and loss accounting, and feasibility showed no blocker. 2 Oct — must demonstrate origination, market transfer, collection, redemption, and a tested default haircut, or be excluded from the recorded demo. **No late baseline contract rewrite is justified to include Kuru.**

## Bounties

**Track: Onchain Finance & Trading** ($30,000, split three ways). The track explicitly names undercollateralised lending priced on onchain credit history.

Pursue only where the integration already serves the demo. Fits below are assessed against each bounty's **stated requirement**, not its sponsor name.

### Pursue — already on the spine

| Sponsor | Prize | Requirement | Why it fits |
|---|---|---|---|
| Envio | $1,000 | HyperIndex/HyperSync/HyperRPC powering a **core feature** | Lifecycle reconstruction from indexed events is a core feature, not a bolt-on |
| Chainlink | $3,000 | CRE workflow as an orchestration layer — **"build, simulate, or deploy"** | Simulation qualifies, so the attestation path can enter even if deployment access never lands |

### Pursue — a view or adapter over committed work

| Sponsor | Prize | Requirement | What it costs |
|---|---|---|---|
| Perpl | $3,000 | Real-time analytics, risk-monitoring, or portfolio intelligence **focused on Perpl** | A **Perpl market-risk panel**: feed public orderbook depth, funding rates, and realized volatility into the same three named stress scenarios, displayed as a Perpl-focused view. The engine already consumes exactly these inputs. Perpl's market-data endpoints are public — orderbook and funding over WebSocket, funding and candles over REST, no authentication. |
| MetaMask | $2,500 | A **plugin** giving the Agent Wallet a new trading superpower via its plugin architecture | A thin TypeScript plugin over the existing `/v1` product API, exposing the runner's tools as `mm` commands — assess, borrow, health, repay. Plugins are small packages (`@metamask/agent-sdk`; see the `ens` and `sample` examples). **The superpower is credit**: a MetaMask agent that can borrow, which no other plugin offers. 1–2 days, after the baseline. |

Both are additive. Neither touches `risk-core`, the contracts, or the schedule before 22 September.

### Conditional — genuine fit, gated

| Sponsor | Prize | Requirement | Condition |
|---|---|---|---|
| Kuru | $5,000 | **New assets and markets** — a new tradable class *plus the infrastructure to make it viable* | `NoteSeries` tokenized agent debt is literally a new asset class. Behind the 25 Sep and 2 Oct gates above. |
| Nansen | $5,000 | A product experience **beyond exposing raw data** | Provenance-tagged enrichment that demonstrably changes an underwriting decision |

### Pursue last — vault hedging automation

**Perpl API ($5,000)** — *"production-ready trading bot or automation system on Perpl."*

The custody constraint that removes the basis trade (D14) binds `CreditAccount`, which cannot sign Ed25519. It does **not** bind the offchain services, which already hold keys by design. And the plan already carries an unanswered risk:

```text
CreditVault lends against agents holding the risky asset
        ↓
agent defaults → recovery unwinds into a falling market
        ↓
lenders absorb the shortfall (02: realized LGD spans 0%–88%)
        ↓
an offchain service shorts perps on Perpl to hedge the vault's
   aggregate risky-asset exposure
```

Why this qualifies where the basis trade does not:

| | Basis trade | Vault hedge |
|---|---|---|
| Who trades | `CreditAccount` — cannot sign | Offchain service — already holds keys |
| Custody domain | Broken | Untouched |
| Recovery path | Becomes keeper-dependent | Unchanged, contract-enforced |
| Position marked onchain for coverage | Required | **Not needed** — vault-level, not account-level |
| Unknowns 4–6 above | All apply | **None apply** |

The six unknowns exist because the perp leg must sit *inside* a credit account and be marked for coverage math. A vault-level hedge sits entirely outside that boundary.

**Constraints.** Genuinely additional scope: a service holding its own keys and managing its own margin. Starts only after the 22 September baseline is pinned, and its failure must leave the demo unchanged. **For v0 it runs in scenario mode against the synthetic population**, demonstrating the mechanism — not managing real capital. A hedging bot that loses money is a new loss source, so it carries its own risk framing and its own labeled evidence.

**Rank it last** — after Envio, CRE, the Perpl panel, the MetaMask plugin, and Kuru. Take it only if time remains.

### Do not chase

Everything else — Agora (mobile apps), Cleanverse, Aurora, Privy, Dynamic, Alchemy, Hunyuan, KIMI, Qwen, Mera — requires a product shape this release does not have.

**Check eligibility, not fit:** Monad Foundation's *Best Community Team Project* ($5,000) turns on whether the team qualifies as a Metropolis community supporter. Confirm during milestone one.

### Expectation

| Outcome | Cumulative |
|---|---|
| Spine only — Envio + CRE | $1k–$4k |
| Plus the Perpl panel and MetaMask plugin | $6.5k–$9.5k |
| Kuru clears its gates | $11.5k–$14.5k |
| Plus Nansen | ~$19.5k |
| Plus the vault hedge | ~$24.5k |

**Sequencing rule:** none of these may start before the 22 September baseline is pinned. Each is a leaf — a view, an adapter, or a gated extension — and a failure in any one must leave the demo unchanged.

**Bounties reward eligible submissions at judges' discretion. They are not expected revenue or a guaranteed return on integration work.** An integration that would not otherwise earn a place in the demo is not worth its bounty.
