# Credence

Autonomous credit markets for AI agents. A risk engine underwrites agent
borrowers from their demonstrated behavior; contracts hold custody and
enforce the result.

**Not undercollateralized.** The account secures the loan — origination
requires 1.20x stressed coverage. The claim is narrower and harder: the
agent posts far less *equity* than it controls, and the multiplier and the
price are set by what it has demonstrated rather than by what it posts.
[`docs/12`](docs/12-claims-roadmap.md#adversarial-qa) states this precisely.

Built for the Monad Metropolis hackathon, Onchain Finance & Trading.

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
and it is the only place addresses are recorded — copies in prose go stale
the first time anything is redeployed, and three of them did.

It carries the Credence contracts, the `CREReceiver`, the canonical
ERC-8004 registries, and the registered agent id, which is what
[`docs/06`](docs/06-identity-forecasts.md) requires of a release manifest.

Agent `1830` is registered on the canonical ERC-8004 identity registry,
and Credence publishes loan outcomes to the canonical reputation
registry. **Two repaid obligations are published there against the
manifest's own manager**, read back on 19 September by a standalone
script run from outside this repository, importing only the Python
standard library and talking to nothing but a public RPC. The
`CREReceiver` is deployed; **no DON-signed report has reached it**, and
none will in this release — deploy access turned out to need a commercial
agreement with Chainlink rather than an approval.

## Running it

```sh
scripts/setup.sh          # fetch pinned deps, build
anvil --port 8546 &       # a local chain

LOANS=3 scripts/demo-repay.sh   # three loans; the limit rises, the rate falls
scripts/demo-default.sh         # a crash, unwound, with a real loss
scripts/demo-monitor.sh         # continuous underwriting, and replay
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

`docs/09`'s eight screens, rendering what the demos retained, plus five
more: **Start here**, which is the nine-beat script `docs/09` specifies
rather than another table; **Indexed**, the lifecycle rebuilt from the
event log by the Envio indexer (needs Docker, skipped without it);
**Perpl risk**, live orderbook depth, funding and volatility from the
perps venue driving the three stress scenarios; the four **observatory
rooms**, served from one component; and a **field guide** to the physics. One command does the whole path — toolchain
check, a local chain, the demos, the WebAssembly engine, the data bundle,
and the dev server:

```sh
scripts/web.sh          # or: make web
```

It is idempotent and reuses evidence that already exists; `--fresh` re-runs
the demos, `--no-serve` builds the static site instead of serving it.

The replay screen runs the **actual engine** — the same Rust the gateway
uses, compiled to WebAssembly — so a reader can reproduce a retained
assessment in their own tab, and watch a tampered one fail. The Perpl
screen runs it too, evaluating one retained snapshot twice: once against
the market the venue reports, once against the declared placeholder, with
only the market evidence differing (`scripts/build-perpl-data.sh` captures
it; the page never fetches).

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

Four worth reading before judging anything:

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
  not** make whatever the demo shows. Two of the ten are not earned, and
  the table says which and why.

## What this is not

Stated here because the rest of the repository is careful about it:

- **Not trust-minimized.** One operator key pins routes, allowlists
  signers, and appoints keepers. Honest for a prototype, fatal if anyone
  calls it trustless.
- **Not audited.** External review has found and fixed real defects,
  including a fund-safety bug. That is evidence of review, not of
  correctness.
- **Not adopted.** A third party can read an agent's record
  independently. No second protocol consumes it.
