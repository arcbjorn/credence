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
- Amount, rate, and term arithmetic reconciles across Go, Rust, and Solidity
  (the web interface renders retained figures; it computes none of them)
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
| **Independent reputation reader** | evidence | `scripts/demo-erc8004.sh` against the canonical ERC-8004 registries on Monad testnet; see [Independent reader](#independent-reader--erc-8004-14-september) |
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
> **15 September**. The reader is written against an ABI the real registry
> does **not** share: `agentEntries` exists only on the local adapter and
> reverts against ERC-8004, whose read path is
> `readAllFeedback(uint256,address[],string,string,bool)` — `0xd9d84224`,
> confirmed in the deployed implementation. It is a rewrite.

## Public-chain deployment — 14 September

Deployed to **Monad testnet (chain 10143)** and verified by reading the
chain back, not by trusting the deploy script's exit code:

**Addresses live in
[`contracts/deployments/10143.json`](../contracts/deployments/10143.json)**,
which is the only place they are recorded.

This table used to repeat them and went stale the first time anything was
redeployed — it named a `RiskOracle` three deployments old while the
manifest named the live one, so a reader comparing the two documents found
a contradiction rather than an address. That is the same failure the
README already fixed by pointing here, and this document had not.
Reported by review, 14 September.

Three loans completed the full lifecycle on the public chain, and all
seven deployment checks passed against it — including the one that
separates the two refusal reasons:

```
[PASS] beat 6  owner withdrawal reverts while debt is open   custom error 0xe9d4b4e2
[PASS] beat 6  stranger withdrawal reverts                   custom error 0x72cb8533
[PASS] beat 6  owner blocked by debt, not access control     owner=0xe9d4b4e2 stranger=0x72cb8533
```

Reproduce with `scripts/deploy-testnet.sh`, then
`RPC=https://testnet-rpc.monad.xyz TERM_SECONDS=600 HOLD_SECONDS=45 LOANS=3 scripts/demo-repay.sh`.

### What a public chain surfaced that a local one hid

Three defects, none of which a local run could have found:

- **Revert data was discarded at the transport layer.** `rpcError` kept
  only `code` and `message`. Anvil writes the selector into the message,
  so every custom error was distinguishable locally; Monad returns it in
  a separate `data` field and *also* decodes the bytes into the message
  as text, where `0x72cb8533` renders as `r˅3`. Every custom error was
  therefore identical to every other on a public chain, and the beat-6
  check that distinguishes debt from access control reported `unknown`
  against contracts that had behaved correctly.
- **The hold had no implementation for a chain that cannot warp.** The
  scenario probed for `evm_increaseTime` and printed the right advice,
  but called it unconditionally anyway, so the run aborted after
  funding. A real hold now waits on **chain time**, confirmed by a block
  carrying the later timestamp — wall-clock sleep proves nothing, since
  `block.timestamp` is what the contract charges interest against.
- **Forge's broadcast cache stalls a redeploy.** A restarted chain resets
  nonces while forge replays cached ones, and it retries `nonce too low`
  with no output until it gives up. Observed twice as a silent
  ten-minute stall.

### The sequential-loans row does not reproduce on testnet

Stated plainly, because the testnet run looks like a weaker result:

```
loan 1: apr=1200bps capacity=6200000000 binding=HistoryGrowthCap
loan 2: apr=1200bps capacity=6200170000 binding=HistoryGrowthCap
loan 3: apr=1200bps capacity=6200340000 binding=HistoryGrowthCap
```

The constraint never switches and capacity barely moves. **This is the
anti-farming rule working, not a regression.** `docs/06` credits
`min(eligible_seconds, 30d) / 30d`, so a 45-second hold earns 1/9,600th
of the history credit a five-day hold earns. The observed capacity growth
is ~5,600x smaller, which is that rule and the binding `min()` acting
together.

A public chain cannot warp time, and holding three loans for five days
each is not a demo. **The row's evidence therefore remains the local
run**, where the elapsed term is real within the chain's own clock. The
testnet run demonstrates portability and enforcement, not repricing.

## Independent reader — ERC-8004, 14 September

Sub-claims A and B of `docs/06` are met against the **canonical ERC-8004
registries on Monad testnet**, not a local adapter:

| Registry | Address |
|---|---|
| Identity | `0x8004A818BFB912233c491871b3d84c89A494BD9e` |
| Reputation | `0x8004B663056A597Dffe9eCcC1965A193B7388713` |
| Agent | `1830`, owned by `0x2a442c45BbB5B2E97981B8fC8855bb5437F52754` |
| Credence client | `0xCaE2590420EAb07eDF737B6069891F36e2e14A34` |

Reproduce with `scripts/demo-erc8004.sh`. With no registry set it deploys
a local double whose selectors match the deployed contract exactly, so the
path runs without a funded key; pointed at the canonical addresses it uses
the real thing.

Observed on the live registry:

```
loan 1: credence.outcome.repaid value=4000000000
loan 1: credence.interest       value=746
loan 1: credence.loss           value=0
loan 1: credence.held           value=49
republish -> already published, published=0

agent 1830: repaid   principal=4000000000 interest=746 loss=0 held=49s
```

`tools/reader/read_erc8004.py` produced that last line from the chain
alone, run from a directory outside the repository, importing only the
Python standard library.

### What the standard forced

- **One entry per signal.** ERC-8004 stores a single `int128` per entry,
  and `docs/06` forbids merging a repayment, a realised loss, interest and
  duration into one fact. A composite score would fit the field and break
  the rule, so four tagged entries carry one fact each. The loss is
  published **negative**: the value is signed for that purpose, and an
  unsigned loss reads as a gain of the same size.
- **Credence is a client, never the agent.** Self-feedback reverts
  `"Self-feedback not allowed"`, so the borrowing agent must be a
  separately owned registered identity.
- **Idempotency is the client's job.** `feedbackIndex` auto-increments;
  the registry accepts a duplicate without complaint. The publisher reads
  back what it already wrote and skips those loans.
- **The loan id lives in `tag2`.** `feedbackURI` and `feedbackHash` are
  write-only in practice -- no view returns them, and they survive only in
  the `NewFeedback` event, which a public RPC will not scan cheaply over
  any useful range.
- **Tags are scoped to the deployment** (`loan:<sha256(chain:manager)>:N`).
  Loan ids restart at 1 per manager, and because idempotency keys on this
  tag, an unscoped collision would make a second deployment's loan 1
  permanently unpublishable -- the same defect `docs/06` records against
  the old `keccak("loan-N")` reference. The reader recomputes the digest
  with `hashlib`, so sha256 rather than keccak keeps it stdlib-only.

### What is still NOT verified

The reader checks the registry exists, its chain id, that the agent is
registered and owned by someone other than the client, and that every
record shown was written by the named client. It does **not** check the
referenced evidence: ERC-8004 returns neither `feedbackURI` nor
`feedbackHash` through any view, so the reader cannot show them, let alone
resolve them.

**Attributable and filterable, not proof the content is true.** The
standard attributes feedback to a client address and expects consumers to
choose which clients they trust. Claim "independently verifiable", never
"adopted" -- sub-claim C still requires a second team.

## CRE route — 14 September

The CRE authorization route is built and tested; **deployment is the only
part still gated.** `cre whoami` reports deploy access not enabled while
the request is reviewed, and per Chainlink's documentation that gates
`cre workflow deploy` alone -- `simulate` and `build` need nothing.

| Piece | State |
|---|---|
| `CREReceiver.sol` | 19 tests, `forge test --match-contract CREReceiver` |
| `cre/underwrite` workflow | compiles to WASM, runs in the simulator |
| Deploy to the workflow registry | **blocked on access** |

### Verified against the live chain, not documentation

The Monad testnet forwarders are real and were probed:

```
monad-testnet   selector 2183018362218727504
  forwarder       0xF8344CFd5c43616a4366C34E3EEE75af79a74482   8591 bytes
  mock forwarder  0xB9F79d863261869B234c481D1f9A7af84AeAd192   4579 bytes
```

`onReport(bytes,bytes)` = `0x805f2132`, found in the deployed forwarder
along with ERC-165, which the forwarder checks before it will deliver.

**The metadata layout was established by observation.** A throwaway probe
was deployed to Monad testnet and a report routed through the live mock
forwarder into it. What arrived:

```
msg.sender      0xB9F79d86...      (the forwarder, so the gate works)
metadata length 64                 (NOT the 62 bytes the packing implies)
  [ 0:32] workflowId
  [32:42] workflowName
  [42:62] workflowOwner
  [62:64] reportId
```

The 62/64 difference is the trap: the identity packs to 62 bytes and is
delivered word-aligned as 64. A receiver that assumed 62 reads the owner
two bytes early. `test_metadataLayoutMatchesTheForwarder` pins the
observed layout, and shifting `_decodeMetadata` by one byte fails five
tests.

### What the receiver checks

Each check exists because skipping it makes the route meaningless, and
each fails closed -- a revert records no assessment, so no new risk is
authorized:

| # | Check | Why |
|---|---|---|
| 1 | `msg.sender` is the forwarder | Otherwise anyone calls `onReport` and the DON is decorative |
| 2 | Workflow owner matches | One forwarder routes for every workflow on the chain |
| 3 | Workflow id matches | An owner may run several workflows; one underwrites |
| 4 | The loan is enrolled | The workflow names a loan; the OPERATOR says what that loan is |
| 5 | Report not already seen | The forwarder retries a reverted delivery (`docs/05` invariant 2) |

**The route is not a check.** An earlier version of this table listed
"Route is `CRE`" as check 4. It is not read from the report at all:
`_normalize` hardcodes `Route.CRE`, because a `MonitoringReport` has no
route field to carry. That is stronger than a check — the route is
structurally unforgeable — but it is not what the table said. Corrected
after review, 14 September.

The receiver **authenticates, never permits**. `RiskOracle.accept` was
split so signature and caller authentication share one recording path:
`test_receiverDoesNotBypassOracleExpiry` and
`test_receiverDoesNotBypassSequenceOrdering` show the oracle still applies
its own rules to a report that passed every receiver check. (An earlier
version of this line cited a test that does not exist; it was renamed when
the payload was narrowed.)

### A monitoring report cannot carry a grant

`docs/05`: a monitoring result carries no request, grant, or terms and can
never disburse. The workflow does not send an `Assessment`; it sends a
`MonitoringReport`, which **has no field** for terms, a requested
principal, or a request id. The receiver expands it, filling those with
zero.

That makes the rule structural rather than remembered. The oracle's
`GrantOnMonitoring` check still runs and is still correct -- but a
workflow could not violate it even if that check were removed.

The same reasoning puts the **binding** in receiver storage rather than in
the report. The workflow names a loan; the operator says what that loan
IS. A compromised workflow can therefore withdraw permission or refuse it,
but cannot invent an obligation or move one to an account nobody assessed.

`accountStateVersion` is the exception and rides in the report on purpose:
it is the portfolio the engine actually assessed, and `riskAllowedFor`
compares it against the account's CURRENT version. Substituting a stored
value would quietly defeat that check.

### Cross-language encoding

The workflow hand-encodes the payload, so it is checked against
`cast abi-encode` -- an independent implementation -- rather than against
its own decoder:

```
TestEncodingMatchesCast       Go bytes == cast bytes
test_payloadIsThirteenStaticWords   13 words, no offset, no tail
TestEnumValuesMatchSolidity   Decision/Permission/Class pinned to the enum order
```

Swapping two same-width fields is the dangerous failure here: it decodes
cleanly into the wrong values, so nothing reverts. Reordering
`sequence` and `accountStateVersion` fails `TestEncodingMatchesCast`.

### Simulator results

Run with `cre workflow simulate`, against the receiver deployed at
`0xEc7A11Aa4da6482E4f7461ae535B9b3bB96CeF2c` on Monad testnet:

```
engine reachable, loan enrolled   -> {"Assessed":1,"Written":1,"Blocked":0}
engine unreachable                -> {"Assessed":1,"Written":0,"Blocked":1,
                                      "Notes":["loan 1: ... no such host"]}
loan not enrolled                 -> Blocked, "not enrolled on the receiver"
```

The run reads `nextSequence(uint256)` from the live receiver, assesses,
generates a DON-signed report and submits the write.

**`Written: 1` means the write path executed and was accepted in
simulation, not that a transaction landed.** The simulator performs a
"chain write simulation" with its own key; `nextSequence` on the deployed
receiver still reads 1 afterwards, which is how that was confirmed rather
than assumed.

### What is NOT built

Stated plainly, because the workflow reads as complete otherwise:

- ~~The assessment endpoint does not exist.~~ **Built 14 September**:
  `credence-assessd` serves `POST /v1/assess {loan_id}` and returns a
  normalized monitoring assessment. See below.
- **No report has travelled the real path.** The receiver is tested
  against a stub forwarder, and the metadata layout against the live mock
  one, but a DON-signed report has never reached it. That needs deploy
  access.
- **The continuous-underwriting row still rests on the signed route.**
  `scripts/demo-monitor.sh` is the evidence for it; the CRE route is an
  alternative authorization path, not a second demonstration of that row.

## The assessment endpoint — 14 September

`docs/01` keeps the engine pure: `POST /v1/assess` on the risk service
takes a whole `{input, policy}` snapshot and performs no I/O, so it cannot
look a loan up by id. Something must read chain and evidence and build
that snapshot. `credence-assessd` is that boundary.

```
POST /v1/assess  {"loan_id": 1}
->  {"decision":"Denied","permission":"Blocked","class":"PrototypeEligible",
     "model_hash":"0x23ec20bd…","input_hash":"0x589f3acc…",
     "evidence_root":"0xb23dedb2…","assessment_hash":"0x14855922…",
     "account_state_version":0,"loan_id":1}
```

Observed against the state `scripts/demo-monitor.sh` leaves behind, where
the price has fallen 70% -- so `Denied`/`Blocked` is the engine's real
answer, not a placeholder.

### A separate binary, on purpose

`credence-gateway` assesses **and publishes**, signing with a development
key. `credence-assessd` only assesses.

They are separate processes because `docs/05` pins ONE authorization route
per facility epoch. A single process that could both sign an assessment
and hand one to a DON would be two authorities for the same facility --
the arrangement the pinned route exists to prevent. A CRE deployment runs
`credence-assessd` and holds no attestation key at all: the normalizer is
constructed with a nil signer, so there is nothing there to misuse.

Construction they share lives in `gateway/gatewayrt`, not duplicated:
two spellings of the binding query would answer differently after a schema
change, and two policy hashes would let one process attest under a policy
the other never ran.

### What a caller cannot do

The request has **one field**. Unknown fields are refused rather than
ignored, so a caller cannot supply a binding, a state version, or a
policy:

```
{"loan_id":1,"account_state_version":999}   -> 400 invalid_body
{"loan_id":42}                              -> 404, loan not monitored
```

The binding comes from the store, keyed by the loan the operator enrolled
with `-loans loanID=bindingKey`. That mirrors `CREReceiver.enrol` on
chain: the caller names a loan, the operator says what that loan IS.

### Failing closed

Every failure returns an **error**, never a Blocked assessment:

| Failure | Status |
|---|---|
| evidence unavailable | 503 |
| engine unreachable | 503 |
| normalization failed (e.g. policy divergence) | 500 |
| retention failed | 500 |

The caller's correct response to an error is to publish nothing, which
leaves the standing permission to expire -- and `docs/05` makes expired
permission block risk increases on its own. Returning `Blocked` instead
would assert a judgment the engine never made.
`TestFailuresReturnErrorsNotBlockedAssessments` pins this; replacing the
engine-error branch with a Blocked response fails it.

### Monitoring can never fund

The snapshot is checked before it reaches the engine, with the same guard
the monitor loop uses. Three things must hold: the context kind is
`Monitoring`, it names an active loan, and it carries **no borrower
request** -- the third being how an origination would otherwise wear a
monitoring label and turn a health check into a draw.

### End to end, no stubs

```
cre workflow simulate underwrite
->  {"Assessed":1,"Written":1,"Blocked":0}
```

That run reads `nextSequence` from the receiver deployed on Monad testnet,
calls `credence-assessd`, which collects real evidence and calls the real
engine, normalizes the answer through the same path the development route
signs, generates a DON-signed report, and submits the write. The only
thing still simulated is the chain write itself, which needs deploy
access.

## External review — 14 September

Six findings against baseline `726324c`, all confirmed and fixed. Each
was reproduced with a failing test before the fix, and each test was
checked to fail again when the fix is reverted.

| # | Finding | Fix |
|---|---|---|
| 1 | Repayment could clear a NEWER loan's custody lock | `_releaseAccount` closes debt only when no loan is active |
| 2 | The signed binding dropped the account state version | `LoadBinding` carries the watched version |
| 3 | A pending publish confirmed on sequence alone | Reconcile on the transaction's own receipt first |
| 4 | Core evidence could be labelled Optional | Validation requires account and market |
| 5 | The price timestamp was discarded | Account evidence ages its price against the block |
| 6 | Splitting a holding inflated recovery value | Impact is computed from aggregate exposure per asset |

### 1 — the sharpest one

`repay` clears `activeLoan` before pulling settlement tokens, so a
callback during that transfer sees a loan-free account. With a standing
quote it can draw a second loan, and the outer repayment's unconditional
`closeDebt()` then released the NEW loan's lock -- leaving the vault owed
principal while the owner could withdraw.

The existing reentrancy tests covered re-entering `repay` and `collect`;
neither tried to ORIGINATE. `test_repaymentCannotCloseANewerLoansDebt`
now does.

The fix checks `activeLoan` rather than adding a reentrancy guard, which
keeps the rule where the rule lives: the account is locked exactly while
it owes, whatever order the calls arrived in.

### 2 and 5 — the same shape, twice

Both are a value that exists being replaced by a default on the way
through. The signed binding left `AccountStateVersion` at zero while the
assessment carried the real one, so permission landed on chain and could
never match. The price observation carried a source timestamp that
`positionsAt` dropped, so a stale price entered evidence stamped with the
current block.

Both had a comment nearby explaining exactly why the value mattered.
Neither had a test.

### 6 — worth stating precisely

Market impact grows with the square root of size, so computing it per
position made two half-sized rows cheaper to exit than one whole one. The
reverted-fix run shows the gap: `1712838600` whole against `1762120800`
split, a 2.9% inflation of recoverable value for the same book.

Impact now comes from aggregate exposure per asset. A second test pins
the other half of the rule: **distinct** assets still get their own
impact, since they trade in their own books.

### What the review did not establish

Its own framing, kept here because it is right: passing those diagnostics
was evidence of defects, not correctness. The review performed no
public-network deployment and established no CRE or ERC-8004
interoperability -- those rest on the separate evidence recorded above.

## Operational measurement

Engine runtime, input size, provider lag, event reconciliation lag, transaction status. One scripted command runs the offline scenario suite; another starts the demo environment. Document required credentials and optional integrations explicitly.

Persist SQLite on durable storage and take a consistent backup **before recording the demonstration.**
