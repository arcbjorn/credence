# Credence

A lending market for AI agents. A risk engine underwrites agent borrowers
from their demonstrated behavior; contracts hold custody and enforce the
result.

**Not undercollateralized.** The account secures the loan — origination
requires 1.20x stressed coverage. The claim is narrower and harder: the
agent posts far less *equity* than it controls, and the multiplier and the
price are set by what it has demonstrated rather than by what it posts.
[`docs/12`](docs/12-claims-roadmap.md#adversarial-qa) states this precisely.

## What it does

An agent asks to borrow. A Rust engine prices the request from evidence —
repayment history, forecast accuracy, portfolio stress — and returns terms
or a refusal. Solidity contracts hold custody, enforce the envelope the
engine authorized, and publish the outcome. Nothing about the decision is
asserted: the snapshot is hashed, the assessment replays byte for byte,
and a third party can read an agent's record from chain without asking us.

Three languages, one job each:

| | |
|---|---|
| `risk/` | Rust. Judges. Pure — no I/O, no clock, no entropy. |
| `gateway/` | Go. Gathers evidence, retains it, delivers assessments. |
| `contracts/` | Solidity. Enforces custody and authorization envelopes. |
| `cre/` | A Chainlink CRE workflow, the alternative authorization route. |
| `indexer/` | Envio HyperIndex. Reconstructs the loan lifecycle from events. |
| `tools/reader/` | Standalone readers. Import nothing from this repository. |

## Live on Monad testnet

Chain 10143. **The deployment manifest is
[`contracts/deployments/10143.json`](contracts/deployments/10143.json)**,
and it is the only place addresses are recorded.

It carries the Credence contracts, the `CREReceiver`, the canonical
ERC-8004 registries, and the registered agent id, which is what
[`docs/06`](docs/06-identity-forecasts.md) requires of a release manifest.

Agent `1830` is registered on the canonical ERC-8004 identity registry,
and Credence publishes loan outcomes to the canonical reputation
registry. **Two repaid obligations are published there against the
manifest's own manager**, read back on 19 September by a standalone
script run from outside this repository, importing only the Python
standard library and talking to nothing but a public RPC. The
`CREReceiver` is deployed; **no DON-signed report has reached it**:
deploying the workflow requires a commercial agreement with Chainlink.

## Running the scenarios

```sh
scripts/setup.sh          # fetch pinned deps, build
anvil --port 8546 &       # a local chain

LOANS=5 scripts/demo-repay.sh   # five loans; the limit rises, the rate falls
scripts/demo-default.sh         # a crash, unwound, with a real loss
scripts/demo-monitor.sh         # continuous underwriting, and replay
scripts/demo-cohort.sh          # four agents, one facility, four outcomes
scripts/demo-notes.sh           # a loan sold as a note, and redeemed
scripts/demo-erc8004.sh         # publish and read back, no Credence endpoint
```

Each script prints what it proves. `demo-repay` ends with ten checks run
against the deployment rather than against its own log — the withdrawal
refusals and the vault's ledgers among them.

Three read-only checks need no chain, no key and no funds:

```sh
scripts/verify-reputation.sh   # this agent's history, straight from ERC-8004
scripts/probe-perpl.sh         # what Perpl's public market data actually does
scripts/verify-perpl.sh        # the gateway's own providers, against the venue
```

`scripts/release-check.sh` runs every acceptance row and writes a dated
record to `releases/`. A **SKIP is a row it could not reach, never a row
that passed.**

### The interface

The interface has the eight record screens in `docs/09` plus eight more screens,
all rendering what the scenario runs retained: **Start here**, the
landing page; **Indexed**, the lifecycle rebuilt from the
event log by the Envio indexer (needs Docker, skipped without it);
**Perpl risk**, live orderbook depth, funding and volatility from the
perps venue driving the three stress scenarios; the four **observatory
rooms**, served from one component; a **field guide** to the physics;
and three that let a reader act rather than read — the **borrow desk**,
which prices a request they compose; **what a record earns**, one
controlled run where only the record moves; and **live on Monad**, the
only screen that reads the network. One command does the whole path — toolchain
check, a local chain, the scenarios, the WebAssembly engine, the data
bundle, and the dev server:

```sh
scripts/web.sh          # or: make web
```

It is idempotent and reuses evidence that already exists; `--fresh` re-runs
the scenarios, `--no-serve` builds the static site instead of serving it.

The replay screen runs the **actual engine** — the same Rust the gateway
uses, compiled to WebAssembly — so a reader can reproduce a retained
assessment in their own tab, and watch a tampered one fail. The Perpl
screen runs it too, evaluating one retained snapshot twice: once against
the market the venue reports, once against the declared placeholder, with
only the market evidence differing (`scripts/build-perpl-data.sh` captures
it; the page never fetches).

The **borrow desk** turns that around: a reader composes the request —
how much, against what record, into what market — and the same engine
prices it live, returning terms or a refusal and naming the constraint
that bound it. It edits a retained snapshot rather than inventing one,
and it is a quote, not a loan: origination consumes an attestor-signed
quote, and that key is deliberately not in the browser.

**What a record earns** is the claim this project rests on, as a single
controlled run. `scripts/demo-growth.sh` originates the same principal
eight times against one agent and holds everything else still, so the
rate it is quoted falls **12.00% → 9.20% on record alone** and the
binding constraint hands off from `HistoryGrowthCap` to `StressCapacity`
where the ceiling flattens. Nothing else moved; the script prints the
delta and fails loudly if the rate did not fall.

**The globe is a market, not a borrower.** `scripts/demo-cohort.sh`
drives four agents against ONE facility -- each with its own
`CreditAccount`, its own equity, and its own record -- so every screen
that speaks of a market has more than one agent in it. They span the
space rather than repeating a point: `sable` takes a single loan at
16.00%, `cinder` takes six and earns its way to 8.67%, and its ceiling
ends up set by what it is *forecast* to repay, a constraint no other run
reaches. Beside them the Atlas carries eight assessed profiles covering
five binding constraints and all three decisions, emitted by the engine
itself (`cargo test --test ablation -- --ignored emit_comparison`) and
pinned so they cannot drift from it.

**Live on Monad** is the one screen that reads the network. It reads the
deployment's four vault ledgers and every loan straight from chain
10143, and it will sign exactly one transaction: `repay`, the borrower's
own side, which needs no attestor signature.

`make` on its own lists the other entry points; every one of them is a
script under `scripts/`, so make is a convenience rather than a
dependency.

### Against a public chain

Export a funded key — the scripts refuse anvil's published keys anywhere
but localhost:

```sh
export RPC=https://testnet-rpc.monad.xyz
export CREDENCE_DEPLOYER_KEY=... CREDENCE_SIGNER_KEY=...
```

## Reading the docs

`docs/` is the specification, written to be checkable rather than
promotional. Start with [`docs/README.md`](docs/README.md).

Four to read first:

- [`docs/14 · Verification report`](docs/14-verification-report.md) —
  what was actually checked and what it produced, with a section naming
  what is **not** proven. Start here if you have time for one document.
- [`docs/11 · Verification`](docs/11-verification.md) — every release
  requirement, its status, and the command that reproduces it. Rows
  without evidence are named as pending rather than assumed.
- [`docs/05 · Contracts`](docs/05-contracts.md) — the invariants, and a
  plain statement of what the operator can do. The trust model is a
  prototype's, and the document says so.
- [`docs/12 · Claims`](docs/12-claims-roadmap.md) — the ten claims this
  release may make, each marked earned or not, and the seven it **must
  not** make whatever a run shows. Two of the ten are not earned, and
  the table says which and why.

## What this is not

- **Not trust-minimized.** One operator key pins routes, allowlists
  signers, and appoints keepers. It is not trustless.
- **Not audited.** External review has found and fixed real defects,
  including a fund-safety bug. That is evidence of review, not of
  correctness.
- **Not adopted.** A third party can read an agent's record
  independently. No second protocol consumes it.
