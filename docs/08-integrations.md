# 08 · Integrations

## The rule

Five deliverables each: documented capability check, normalized adapter, fixture-based contract test, visible product use, defined failure behavior.

> **An integration is complete only when the released environment demonstrates its actual behavior. A logo, an installed SDK, or an architecture box does not satisfy this.**

Status: `Unverified` · `Proven in spike` · `Integrated` · `Accepted` · `Deferred`.

**The baseline must work with zero optional providers.** Each has a defined omission behavior. Required chain access, custody execution, and release identity verification remain dependencies. A fallback for development does not automatically satisfy release acceptance, and no fallback changes the evidence or authorization policy of an existing facility.

## Matrix

| Integration | Capability check | Acceptance | If unavailable |
|---|---|---|---|
| Monad RPC | Chain, tokens, read consistency, transactions, explorer | Full state plus a successful and a reverting transaction | Another RPC; selected-chain deployment still required |
| Execution venue | Assets, custody, approvals, liquidity, pricing, bounded exit | Trade and unwind, proceeds to account | Labeled scenario venue; no external-market claim |
| ERC-8004 | Registry addresses, ABI, binding and feedback permissions | Registered identity, attributable feedback, transfer handling, independent reader on the selected network | Local adapter for development only; integration incomplete and release identity gate remains open |
| Envio | Events, lag, reorg handling, hosting | Reconstruct lifecycle with evidence links | Direct log indexer is baseline. **Synced against Monad testnet over HyperSync, rendered on screen, and rollback demonstrated through a simulated reorg** |
| Perpl | Supported endpoints on chosen network | A documented field changes an assessment | Feature absent |
| Nansen | Account coverage, quotas | A sourced feature demonstrably affects a decision | Omit, disclose coverage |
| CRE | Deployment access, network and forwarder, consumer path | Valid report accepted; invalid bounds and wrong routes rejected | Separate development-attested baseline; an existing CRE facility blocks new risk and cannot fall back to a development key |
| Kuru | Market creation, quote asset, fills, redemption | Tradable claim with completed redemption | **Listed on testnet 20 Sep** — market `0x874160Ca…`, `verifiedMarket() = true`, notes transferred. Redemption is tested off-market (`demo-notes.sh`). **Both participants are project-controlled**, so this stays out of working-product claims |
| Agent wallet | SDK and policy boundary | Agent signs permitted action; prohibited custody fails | Basic runner wallet; no plugin claim |

## Strategy — economic purpose required

"One risky asset, one adapter" says what is *permitted*, not why an agent would *borrow*.

The strategy states the activity, why it needs financing, how the agent decides financing is worthwhile at the offered APR, and how it exits. Without this, docs/09 step 2 — where the agent proposes an allocation and a financing ceiling — has no content.

[07 · Simulation](07-simulation.md#what-the-strategy-does-and-why-it-is-financed) states it:
spot acquisition of one risky asset at a whitelisted venue, financed
because the agent's edge is timing and selection rather than capital, and
lendable because the position is custodied where the keeper can reach it.
Every claim there is enforced by code — bounded adapter approvals, exact
amount or deny, de-risking exempt from fresh permission, recovery only on
maturity or withdrawn permission.

### Assets, venue, and valuation

Named test assets on an isolated scenario venue.

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

Two common arguments against it are **wrong**, and the distinction changes what the venue capability check must test.

*Wrong: "margin sits off-chain, outside the custody domain."* Perpl is fully onchain on Monad, with a real Exchange contract on mainnet and testnet, and `createAccount(uint256)` is callable **onchain with collateral and no API authentication**. A contract can open and fund the account.

*Wrong: "the perp leg is unrecoverable, so the failure path cannot cover it."* Two documented facts rescue custody:

- **Withdrawals are never permitted via an API key, regardless of scope.** A trade-scope key cannot move margin out. Withdrawal stays onchain, gated by `CreditAccount`. **The owner-cannot-withdraw invariant holds.**
- Perpl's own liquidation engine closes undercollateralized positions and returns the bulk of remaining margin to the trader — landing **inside** the custody domain if the trader is `CreditAccount`.

So a workable shape exists: `CreditAccount` owns the Perpl account; an offchain keeper holds a trade-scope key it can use to close the hedge but never to withdraw. The cost is a **downgrade, not a break** — recovery timing depends on a keeper acting rather than being contract-enforced.

**The blocker is six unresolved unknowns:**

1. Does `createAccount` succeed when called from a contract?
2. May `target_profile` in a delegated enrollment be a contract address?
3. Does enrollment accept EIP-1271? *(unmentioned in the docs — assume no)*
4. Can a keeper key close positions reliably under stress?
5. How is a perp position marked onchain for coverage math?
6. How does isolated margin compose with account coverage?

**Unknowns 1–3, probed against testnet on 12 September.** Exchange proxy `0x1964…80cc` resolves to implementation `0xbcbd…adbb` (128 KB).

| # | Question | Result | Evidence |
|---|---|---|---|
| 1 | `createAccount` callable from a contract? | **Likely yes, unproven** | Selector `cab13915` present. A contract caller and an EOA caller returned the *identical* revert `0xcfe73bb0(caller, amount)` — a balance/allowance error, not caller-type rejection. No EOA-only guard surfaced. Proof needs a funded contract transaction. |
| 2 | May `target_profile` be a contract address? | **Unresolved** | Docs define it only as "the delegated account address". No onchain operator functions (`isOperator`, `setOperator`) exist in the implementation; delegation is validated server-side by an undocumented mechanism. Needs a live enrollment attempt. |
| 3 | Enrollment accepts EIP-1271? | **No — treat as closed** | `isValidSignature(bytes32,bytes)` absent from the implementation. Enrollment requires a secp256k1 EIP-712 wallet signature; EIP-1271 and ERC-6492 are unmentioned. A contract cannot sign enrollment. |

Unknown 2 is load-bearing and is **not resolvable from public documentation or chain state.** Unknowns 4–6 are build questions the probe did not touch.

Spot-only carries **one** unknown: which venue.

**Retained as a candidate strategy adapter #2, outside v0.** The venue capability to check is **contract-callable trading and a contract-callable forced unwind** — not "is it onchain", which Perpl already is. An exchange's own liquidation mechanism does not supply Credence's required recovery *timing*.

**Vault-level hedging — not built.** The custody constraint that excludes the basis trade binds `CreditAccount`, which cannot sign Ed25519; it does not bind the offchain services, which already hold keys. An offchain service shorting perps against the vault's aggregate risky-asset exposure would leave custody and the contract-enforced recovery path unchanged, needs no onchain position marking, and touches none of unknowns 4–6. It would also be a new loss source with its own risk framing.

## CRE — what it proves

Consensus across independent nodes on a workflow that fetches a single endpoint means **N nodes agreeing on the same fabricated response.** Consensus protects workflow *execution*, not input *truth*.

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

Preserve the distinction between local simulation and a deployed workflow.

### Availability on Monad testnet

**CRE supports Monad testnet.** Verified on chain, not from documentation:

| Item | Value |
|---|---|
| Chain name | `monad-testnet` |
| KeystoneForwarder | `0xF8344CFd5c43616a4366C34E3EEE75af79a74482` — 8,592 bytes deployed |
| MockKeystoneForwarder | `0xB9F79d863261869B234c481D1f9A7af84AeAd192` (simulation only) |

Deploy access is **gated, and not self-service**. `cre account access`
submits a request from the CLI; it is not an approval. The response
(18 September) was an invitation to **discuss commercial conditions for
workflow deployment** under a mutual NDA.

Simulation needs no approval — `cre workflow simulate` runs immediately,
and only `cre workflow deploy` requires the commercial agreement.

**Deploy access is unavailable for this release.** A signed MNDA and a
commercial agreement are disproportionate for a prototype that `docs/12`
forbids from claiming third-party adoption. Development attestation
remains the funding route, and CRE status is described as simulated.

Beyond the distinction between a **CRE policy-checked report** and a
**recomputed assessment**, there is one between a *simulated* workflow and
a *deployed* one. Only a deployed workflow supports an integration claim.
The forwarder address above is what the receiver contract must validate
`msg.sender` against.

Run `scripts/probe-testnet.sh` to re-verify.

### CRE — built and simulated, not delivered on chain

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

#### What this is not

- **No DON-signed report has reached the live receiver.** The simulator's
  `Written: 1` means the write path executed and was accepted in
  simulation; it is not a transaction. Deploy access is unavailable.
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
  It requires `Authorization: Bearer <token>`, and refuses to start on a
  non-loopback listener without one; the workflow reads that token from
  CRE's secret store rather than from its committed config. That closes
  "anyone who can reach the port can drive the permission oracle". It does
  not change the point above: N nodes still agree on one
  project-controlled reply.
- **The metadata layout was observed on the MOCK forwarder**, not the
  production Keystone one. If production metadata is not exactly 64 bytes
  with the owner at `[42:62]`, every report reverts — loudly, which is the
  right failure, but untested against the real path.

> **CRE authentication is built and simulated. No CRE facility has
> originated or monitored on chain.**

## Envio — lifecycle from the log

`indexer/` is a HyperIndex project over the `LoanManager` event log.

### Why state reads are not enough

The gateway reads loan **structs** over `eth_call`. That answers "what is
true now" and cannot answer "how did it get here": a struct read at
maturity shows a repaid loan, not when recovery opened, how many
collections it took, or what each one recovered. The contract keeps
running totals and two timestamps — enough to report an outcome, not
enough to reconstruct the path to it. Those intermediate facts exist only
in the log.

> Held duration **is** derivable from a struct: the contract retains
> `originatedAt` and `resolvedAt` on purpose, and `credence-publish`
> computes `held` from exactly those. The *sequence* is not recoverable
> from state, only the endpoints.

So the indexer is the only component that can reconstruct a lifecycle,
which is the `docs/11` row it serves — "replay an indexed loan after
cursor rollback or simulated reorganization". `rollback_on_reorg` is set
rather than left to default.

### Capability check

| Item | Result |
|---|---|
| HyperSync supports Monad testnet | **Yes.** `chain_id` 10143, tier GOLD, confirmed against `chains.hyperquery.xyz/active_chains` |
| Public RPC `eth_getLogs` range | **Capped at 100 blocks** (`-32614`), which is the plainest argument for HyperSync: the replaced manager deployed at block 62,425,291, so reconstructing its lifecycle over that RPC is ~620,000 sequential requests |
| Manager deploy block | 63,805,994 for the current manager, 62,425,291 for the one it replaced; both found by bisecting `cast code` because the log range cap makes a scan impractical |
| anvil | No HyperSync endpoint, so 31337 syncs over RPC. HyperSync serves the chain it supports; the throwaway local chain is read directly |

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
- **A full sync, run 18 September**, against the local chain a
  `demo-default.sh` run leaves behind — the default scenario rather than
  the simple repayment, because it exercises recovery, partial collection
  and write-off. Envio reported `All chains are caught up` and `The
  indexer is ready`, and the stored rows were read back from Postgres:

  ```
     id    |  status   | principalRepaid | principalLoss | heldSeconds
   31337-1 | Defaulted |      1400000000 |    2600000000 |      432002

   Originated  b30/i4 · RecoveryOpened b36/i1 · Collected b37/i3 · Defaulted b38/i2
  ```

  `principalRepaid + principalLoss` is exactly the 4,000,000,000
  principal, and `principalLoss` matches the `realized_loss=2600000000`
  the scenario printed from chain state — derived here from the log alone,
  with no `eth_call`. The GraphQL API serves the same rows including the
  `events` relation, and the `Defaulted` row's transaction hash resolves
  on chain to block 38 against the manager the manifest names, so an
  indexed row is checkable without trusting the indexer.

- **`scripts/index-local.sh` is the one-command path.** It handles four
  environment collisions: Postgres port 5433 already in use, IPv4 8080
  held by another server while Docker publishes on IPv6, a leftover
  indexer on 9898, and codegen running against a different config than
  the indexer. It ends in `envio dev`, which prints `The indexer is ready`
  once synced; `scripts/release-check.sh` and the interface build both
  wait for that line.

- **A screen reads it — the "Indexed" view.** It
  renders the loan beside the ordered event log that produced it, and
  prints the conservation check in words:
  `1,400 recovered + 2,600 written off = 4,000 principal`. Deliberately
  separate from "Loan lifecycle", which renders what the gateway
  retained: merging them would hide which source each figure came from,
  and the point of this one is that the figures came from the log alone.

  It follows the same rule as every other screen. `scripts/build-indexed-data.sh`
  reads the rows the indexer stored into the bundle, and the page renders
  the bundle — a screen querying a local GraphQL endpoint would show
  nothing in the published static build. The bundle step is the only
  optional one, because it is the only one needing Docker; absent it, the
  screen says the sync has not been run and gives the command, rather
  than rendering an empty lifecycle that reads like a loan with no
  events.

- **The export is tested.** `web/src/lib/indexed.test.ts` pins that
  amounts stay strings, that a resolved loan reconciles, that every loan
  opens with `Originated`, and that events are in block order. The export
  sorts on the numeric `blockNumber` column and casts to text only in the
  projection; a text sort would place block 14 before block 6 and render
  origination last.

### The deployed chain, over HyperSync

With an API token in `indexer/.env`, `scripts/index-testnet.sh` indexes
**Monad testnet itself**, not a local double. `indexer/config.yaml` names
the manifest's loan manager from block 63,805,994, and the script refuses
to start when the config and the manifest disagree. Run on 5 October, the
index holds loans `10143-1` to `10143-6` on that manager: 1, 2 and 4 to 6
`Repaid`, 3 `Active`, matching `status()` on chain.

The 18 September sync of the replaced manager `0x40dc8Dd6…` read:

```
   id    | status | principal  | principalRepaid | interestCollected | heldSeconds
 10143-1 | Repaid | 4000000000 |      4000000000 |               625 |          41
 10143-2 | Active | 4000000000 |               0 |                 0 |
```

Loan 1 repaid in full; loan 2 is open, and carries no resolution rather
than a zeroed one. The 41-second hold is the `HOLD_SECONDS=45` the
testnet run used — the same short hold `docs/11` records as the reason
the repricing story does not reproduce there.

**Checked against the chain rather than trusted.** The indexed `Repaid`
row's transaction hash resolves on Monad testnet to block 62,425,571,
status success, `to` that loan manager `0x40dc8Dd6…`. A reader can repeat
that with `cast receipt` and no Credence service.

**~1.1 million blocks in about 2.5 seconds**, from that manager's deploy
block to the head. The same range over the public RPC is impossible in
practice: it caps `eth_getLogs` at 100 blocks, so this is ~11,000
sequential requests.

### Reorg handling

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

> **The indexer syncs the deployed chain, its output reconciles against
> that chain transaction by transaction, a screen renders it, and it
> follows the chain through a reorganization.** The reorg check runs
> against anvil; no reorganization has been observed on Monad testnet,
> because one cannot be requested.

## Kuru — note market

### Feasibility

Every line below was established **on chain** on 19 September, not from documentation, and `scripts/probe-kuru.sh` re-runs the whole check. It sends no transaction and creates no market, so a reader needs no funded key.

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

`deployProxy` reverts with **empty revert data** when handed addresses
that hold no code, which is indistinguishable from an access-control
rejection. Placeholder addresses `0x1`/`0x2` revert this way; real ERC20s
succeed from the same caller.

> **No feasibility blocker.** The venue is live, creation is
> permissionless, the cost is negligible, and it accepts the deployment's
> own tokens. This prices *creating a market*, not *tokenising a
> receivable*; the receivable is `NoteSeries`, below.

**Structure.** A separate `NoteSeries` funding route for one loan in an isolated deployment. **Never tokenize a receivable belonging to the baseline pooled vault** — each `LoanManager` binds to its allowed funding source. The series defines settlement asset, target principal, borrower, term, rate, maturity, recovery waterfall, and redemption method before funding. Fixed supply issued at origination; notes are a pro-rata claim on that loan's actual net collections. Early repayment pays accrued interest only — **the note does not promise unearned full-term interest.** Default can redeem below principal; pending recovery means pending redemption.

**The same loan must never appear as both a redeemable pooled-vault asset and an independently redeemable note claim.** Notes are not eligible collateral in this release.

### `NoteSeries.sol`

`contracts/src/NoteSeries.sol`, 16 tests in `NoteSeries.t.sol`. It
implements the **same funding-source interface** `LoanManager` already
calls on `CreditVault` — `lend`, `recordPrincipalRepaid`,
`recordInterestCollected`, `recordLoss`, `markReceivableUnresolved`,
`clearReceivable`, `releaseReservation` — so the manager binds to one or
the other without knowing which, and **the manager is unchanged**. The
series is a parallel funding source in an isolated deployment;
`LoanManager`, `CreditVault` and `CreditAccount` are untouched.

The double-claim rule is structural rather than checked. A `LoanManager`
holds exactly one `vault` address, so a loan funded by a series was never
in the pool and cannot be redeemed twice.

Each rule in the paragraph above has a test that fails when it is removed:

| Rule | Test |
|---|---|
| Fixed supply at origination | `test_cannotSubscribeBeyondTarget`, `test_cannotActivateUnderSubscribed` |
| One series, one loan | `test_lendOnlyToTheSeriesBorrower` |
| Pro-rata claim on **actual** collections | `test_repaidSeriesRedeemsAboveFaceValue` |
| Default can redeem below principal | `test_defaultedSeriesRedeemsBelowFaceValue` |
| Early repayment pays accrued interest only | `test_earlyRepaymentPaysOnlyAccruedInterest` |
| Pending recovery means pending redemption | `test_redemptionBlockedWhileRecoveryPends` |
| The claim is transferable | `test_transferMovesTheClaim`, `test_partialTransferSplitsTheClaimProRata` |

**Mutation checks.** Replacing the redemption arithmetic with face value
fails five of them, including an underflow on the defaulted case — a
face-value payout cannot be funded from a partial collection. The
recovery-pending test repays principal first, so it isolates the
receivables check: deleting that guard fails exactly one test.

### Listed on Kuru

`scripts/list-notes-kuru.sh` deploys a `NoteSeries`, issues its notes and
creates a Kuru orderbook market for them, on Monad testnet (listed
20 September).

The addresses live in the **`kuru` block of
[`contracts/deployments/10143.json`](../contracts/deployments/10143.json)**,
which is where every address in this project is recorded.

Cost: 0.3363 MON for all three deploys and the trades.

**Kuru's own Router recognises it**, which is the check that matters:
`verifiedMarket(<market>)` returns `true`, and the market proxy resolves
to exactly the address `orderBookImplementation()` reports. Re-check it
against the manifest:

```sh
cast call <kuru.router> 'verifiedMarket(address)(bool)' <kuru.market> \
  --rpc-url https://testnet-rpc.monad.xyz
```

The notes then moved: the whole 4,000,000,000 supply is held by the
second address and none by the issuer.

**What this does not establish.** Both participants are project
controlled. Per the paragraph below, that makes this a **controlled
market with observations lacking independent validation** — it
demonstrates that a real on-chain orderbook accepts the instrument, not
that anyone independent would price it. Nothing here feeds back into
underwriting, and `docs/12` keeps the tradable-note claim unearned on
exactly that basis.

### Market feedback stays informational

Show fundamental assessment and market-implied pricing **separately**. Do not convert note price into real-world PD without explicit models for recovery, timing, interest, and liquidity premiums.

**Why.** The note market is two project-controlled participants. That does not imply zero displayed depth — it implies the market is **controlled, with observations lacking independent validation.** A price nothing independent produced cannot move executable credit.

**What would close it is unsettled.** A depth-based gate is a *candidate* policy requiring adversarial evaluation:

```text
CANDIDATE — unproven:
  observed depth   >= a declared multiple of the influence it can exert
  participants     >= 3 independent, none project-controlled
  observation age  <= freshness bound
  influence        <= bounded fraction of limit, tighten-only, rate-limited
```

**Do not claim a depth threshold makes manipulation unprofitable.** Comparing cost-to-cross-the-book against a financing benefit does not establish an attacker's economic loss: trading notional is not money irreversibly spent, true cost depends on the executable curve and the unwind, displayed liquidity can vanish or belong to cooperating participants, three addresses are not three independent actors, and the payoff can sit outside the affected facility. **Tightening is itself exploitable** — it can force sales benefiting an attacker's other positions.

**The market feedback loop is a future capability whose economic design is outstanding.** The counterfactual panel demonstrates the *mechanism* against synthetic prices; it does not show the mechanism is safe to connect.

## Perpl — market observation

The engine's `MarketState` carries exactly two market inputs:
`realized_volatility_bps` and `liquidity_depth` (`risk/crates/risk-core/src/input.rs`).
Volatility can be observed through a generic HTTP provider. Depth without
a venue feed is **declared policy**, because no public feed reported the
depth available to this protocol at liquidation
(`gateway/market/market.go`).

Perpl's orderbook is such a feed, so depth on an observed facility is a
measurement rather than a declaration.

### Capability check

`scripts/probe-perpl.sh`, read-only and unauthenticated. Three findings
differ from what the documentation implies:

| Documentation implies | Observed |
|---|---|
| Resolution is a TradingView string (`1h`) | Integer **seconds**. `1h` returns **HTTP 500**, which reads like an outage; `3600` on the same URL returns 200 |
| `HTTP 400` means an unsupported resolution | It is a **candle-count cap** near 1000. `60` over 7 days fails; `60` over 6 hours returns 361 candles |
| Market ids are stable | **They differ per network.** ETH is 20 on mainnet, 32 on testnet; mainnet has no BTC market at all |

Both networks behave identically on the 500, so it is an endpoint
contract, not a testnet data gap.

### What is verified, and what is not

`scripts/verify-perpl.sh` builds **the same providers the scheduler
builds** and points them at app.perpl.xyz, because the unit tests run
against fixtures this repository wrote: they prove the parsing matches a
*belief* about the venue, never that the belief is right.

```text
evidence.MarketState = {
  "observed_at": 1789947180,
  "realized_volatility_bps": 4,
  "volatility_window_seconds": 21600,
  "liquidity_depth": "286232968480"
}
observation is 9s old by the local clock
```

Depth is the resting **bid** notional, rescaled to settlement units.
The bid side specifically: liquidating collateral *sells*, so it trades
into bids. Since `stress.rs` uses depth as the denominator of a
square-root impact term, counting asks would overstate depth, understate
impact, and over-lend.

**A declared fallback remains, and remains required.** When the venue is
unreachable the declared number is used and `DepthFallbackReason()`
records why — the evidence never claims a measurement it did not make.
The asymmetry is deliberate: an unobserved depth can only make the engine
lend *less*, so taking the venue down cannot be used to extract credit.

### Candle timestamps and the skew budget

A candle is stamped at its **open**, so the newest hourly candle reads up
to 3600s old even on a live feed. Against the standard 300s skew budget
every cycle would be `Stale` and the facility would authorize nothing.

Fixture timestamps are written by hand, so they never exercise a real
bar's age. Stamping the candle's *close* instead would claim an
observation from up to an hour in the future.

The check is not loosened. A resolution coarser than
`evidence.max_skew_seconds` is rejected at load
(`TestPerplResolutionMustFitTheSkewBudget`). At 60s the same observation
is 9 seconds old.

### The risk panel

Perpl publishes three relevant inputs: orderbook depth, funding rates,
and realized volatility. Depth and volatility reach the engine's three
named stress scenarios. **Funding does not, and the distinction is the
substantive design decision here.**

`pricing.rs` already has a `funding_rate_bps`. It is the **lender's cost
of capital**, deducted from gross interest over the loan term. Perpl's
funding rate is the **premium longs pay shorts** to hold a perp near its
index. They share a name and nothing else, and wiring one into the other
would silently corrupt every loan's economics while looking plausible.

So funding is observed, summarised and displayed, and never reaches the
engine. What it legitimately indicates is **crowding**: over seven days of
ETH, 78% of the non-zero samples shared the window's direction. Persistent
one-sided funding is leveraged positioning, and leveraged positioning
unwinds faster than it accumulates — a reason to read a calm volatility
number with suspicion, not a number to feed a model. Market feedback
stays informational (above); this is market feedback.

Zero samples are excluded from that measure. Roughly half of ETH's samples
were zero, and counting a flat reading as agreement would make a dead
market look maximally crowded — the exact inversion the measure exists to
avoid (`TestFundingZeroSamplesDoNotCountAsAgreement`).

#### Why the engine was not extended

`MarketState` sits inside the canonical snapshot that is **hashed** and
pinned in `schemas/v1/snapshot-vector.json`. Adding a field to it breaks
the v1 schema, invalidates every retained `input_hash`, and cascades
through four test files — verified by trying it. A display feature does
not justify that, and this integration does not need it.

#### Two snapshots, one clock

A single capacity number proves nothing. The panel evaluates the **same
retained origination snapshot twice** — once with Perpl's market, once
with the declared placeholder the fixture shipped — at the same clock,
with only the market evidence differing:

| | Declared | Perpl, observed |
|---|---|---|
| Depth | 2,000,000 | 280,381.09 |
| Volatility | 20.00% | 0.07% |
| Agent capacity | 8,117,138,556 | 7,660,993,694 (**-5.6%**) |
| Decision | ApprovedOrigination | ApprovedOrigination |

Real depth is **7× thinner** than the placeholder, so the liquidity and
combined scenarios recover less and capacity falls. The *price* shock is
identical in both columns because the declared floor binds: 0.07%
volatility scaled by the policy multiple sits far below it. That is the
floor doing its job — `docs/03` says a calm window must not make the
stress test assert nothing, and here it visibly does not.

The engine runs **in the reader's tab**, the same WebAssembly build the
gateway uses, so the scenarios displayed are its own output rather than a
second implementation that could drift.

#### Capacity only moves when the stress term binds

Capacity is a `min()` over several constraints, and **only the stress term
reads market evidence.** A snapshot bound by another constraint, such as
loan 1's `HistoryGrowthCap`, shows identical capacities beside scenarios
that differ. The capture therefore prefers a snapshot whose assessment was
bound by `StressCapacity` (`snapshots` is a dict with no meaningful order,
so without that preference the choice is arbitrary), and the page reads
`agent_binding` instead of inferring from a zero delta: when something
else binds, it names that constraint and says the market cannot move the
limit.

#### Assessment clock

Substituting a live observation into the retained fixture makes the
evidence read **8.2 days stale**, and the engine returns `Unavailable`
with `StaleRequired { age_seconds: 710300 }`. That is correct engine
behaviour; a decision on the observed market is made at the observation
time. The builder moves the assessment clock and the proposal window to
the observation, and moves the baseline's with it, so the comparison
differs by the market rather than by the clock.

`max_evidence_age_seconds` is not loosened: it is the one check stopping
stale market data from authorizing a loan.

### Scope

This is **market observation, not trading**. It feeds the three named
stress scenarios the engine already runs; it opens no position and touches
no custody rule. D14 and D33 are unaffected — the basis trade stays out of
v0, and nothing here revisits it.

The panel is a **tenth screen**, beyond `docs/09`'s eight. It answers a
different question from the assessment screens: not "what did this account
get" but "what does today's market do to it".
