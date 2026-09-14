# Credence

Autonomous credit markets for AI agents. Undercollateralized lending where
a risk engine underwrites agent borrowers and contracts enforce the result.

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
| `tools/reader/` | Standalone readers. Import nothing from this repository. |

## Live on Monad testnet

Chain 10143. Every address below was read back from chain rather than
trusted from a deploy log.

| | |
|---|---|
| `RiskOracle` | `0x4deF904856f9146CCe1de202A736bB18D071ab7E` |
| `LoanManager` | `0x07989623835ced1C57F2dBA61D99f157515e3192` |
| `CreditVault` | `0xCdF849Bb9529202bb824e94cb900D9432398c32E` |
| `CREReceiver` | `0xEc7A11Aa4da6482E4f7461ae535B9b3bB96CeF2c` |

Agent `1830` is registered on the canonical ERC-8004 identity registry,
and Credence publishes its loan outcomes to the canonical reputation
registry.

## Running it

```sh
scripts/setup.sh          # fetch pinned deps, build
anvil --port 8546 &       # a local chain

LOANS=3 scripts/demo-repay.sh   # three loans; the limit rises, the rate falls
scripts/demo-default.sh         # a crash, unwound, with a real loss
scripts/demo-monitor.sh         # continuous underwriting, and replay
scripts/demo-erc8004.sh         # publish and read back, no Credence endpoint
```

Each script prints what it proves. `demo-repay` ends with seven checks run
against the deployment rather than against its own log.

Against a public chain, export a funded key — the scripts refuse anvil's
published keys anywhere but localhost:

```sh
export RPC=https://testnet-rpc.monad.xyz
export CREDENCE_DEPLOYER_KEY=... CREDENCE_SIGNER_KEY=...
```

## Reading the docs

`docs/` is the specification, written to be checkable rather than
promotional. Start with [`docs/README.md`](docs/README.md).

Two worth reading before judging anything:

- [`docs/11 · Verification`](docs/11-verification.md) — every release
  requirement, its status, and the command that reproduces it. Rows
  without evidence are named as pending rather than assumed.
- [`docs/05 · Contracts`](docs/05-contracts.md) — the invariants, and a
  plain statement of what the operator can do. The trust model is a
  prototype's, and the document says so.

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
