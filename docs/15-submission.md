# 15 · Submission

The text for the Monad Metropolis entry, Onchain Finance & Trading,
due 12 October. Each section below maps to one field of the form. The
links marked **TODO** do not exist yet; the checklist at the end says
what creates them.

Every figure here comes from the release record named in *Evidence*.
When a figure changes, change it there first.

## Name

Credence

## One line

A lending market for AI agents.

## Summary

Credence lends to AI agents on Monad. A Rust engine sets each agent's
rate and borrowing limit from its repayment record, its forecast
accuracy and a stress test of what it holds. Solidity contracts hold
the borrowed funds and enforce the limits the engine set. Every
decision replays byte for byte, including in the browser.

## The problem

An AI agent has no salary, no address and no credit file, so no lender
can underwrite it. What it does have is a record of what it did. Credence
prices credit from that record, and the rate falls as the record
grows.

## How it works

1. An agent asks to borrow. The Go gateway gathers its evidence into a
   snapshot and hashes it.
2. The Rust engine returns terms or a refusal and names the constraint
   that bound the decision. The engine has no I/O, no clock and no
   randomness, so the same snapshot always yields the same bytes.
3. The agent's `CreditAccount` holds the borrowed funds. It can only
   trade inside the envelope the engine authorized, and origination
   requires 1.20x stressed coverage.
4. Each outcome is published to the canonical ERC-8004 reputation
   registry, where anyone can read an agent's record without our API.
5. An Envio indexer rebuilds every loan's lifecycle from chain events.

## Evidence

`scripts/release-check.sh` re-runs every acceptance row and writes a
dated record. The record for this entry is
[`releases/2026-09-24.md`](../releases/2026-09-24.md), tagged
`v0.2.0-rc1`: 23 rows passed and none failed. The one skip is CI, which
was still running when the check reached it and passed afterwards.

- Three sequential loans to one agent are quoted 12.00%, 11.47% and
  10.94%.
- A controlled run of eight loans, with everything but the record held
  still, takes the rate from 12.00% to 9.20% (`scripts/demo-growth.sh`).
- A retained assessment replays to the same bytes, and a tampered copy
  is rejected.
- A default is unwound on chain and its loss lands on the lender that
  made the loan.
- On Monad testnet, a transaction the prototype sends is confirmed in
  375 ms at the median, across 22 transactions on the submitted
  deployment ([`releases/latency-2026-10-05.md`](../releases/latency-2026-10-05.md)).
- Eight of the ten claims in [`docs/12`](12-claims-roadmap.md) are
  earned. The two that are not say why.

## What it is not

- Not trust-minimized. One operator key pins routes, allowlists signers
  and appoints keepers.
- Not audited.
- Not undercollateralized. The agent posts less equity than it controls,
  and the account still secures the loan.

## Links

| | |
|---|---|
| Code | https://github.com/arcbjorn/credence |
| Interface | **TODO**: the GitHub Pages URL |
| Demo video | **TODO** |
| Deployment manifest | [`contracts/deployments/10143.json`](../contracts/deployments/10143.json) |
| Verification report | [`docs/14`](14-verification-report.md) |

## Bounty entries

### Envio

The loan lifecycle screens are built from an Envio HyperIndex indexer,
which rebuilds each loan's origination, repayment and default from
`LoanManager` events. The release check forces a reorg on the local
chain and verifies that the indexed state follows it
(`scripts/verify-reorg.sh`).

### Chainlink CRE

`cre/underwrite` is a CRE workflow that reassesses open loans on a
schedule. Every node must produce byte-identical assessment bytes or no
report is made, and the workflow refuses a contradictory result before it
can become a permission. It writes the result to `CREReceiver`, which is
deployed on Monad testnet and accepts reports only from the Keystone
forwarder. The workflow builds and simulates against that live
forwarder. It is not deployed: deploy access needs a commercial
agreement with Chainlink, so no DON-signed report has reached the
receiver.

### Perpl

The Perpl risk screen feeds Perpl's public orderbook depth, funding rate
and realized volatility into the engine's three stress scenarios. It
evaluates one retained snapshot twice, once against the market Perpl
reports and once against a declared placeholder, so the market evidence
is the only difference between the two results
(`scripts/build-perpl-data.sh`, checked against the venue by
`scripts/verify-perpl.sh`).

### MetaMask

[`plugin/`](../plugin/README.md) gives a MetaMask agent four commands:
`health`, `loan`, `borrow` and `repay`. Each one becomes a transaction
the contracts judge, so the plugin cannot grant credit. A repay from a
key holding no settlement tokens reverts with `TransferFailed()`, and
every layer reports it.

### Kuru

`NoteSeries` turns one agent loan into a fixed-supply ERC20 note: a
pro-rata claim on that loan's actual net collections, with the recovery
waterfall and redemption method fixed before funding. A series is listed
on a Kuru orderbook market that Kuru's router reports as verified.
Redemption, including a default haircut, is tested by
`scripts/demo-notes.sh`. Every participant in that market so far is this
project. The entry claims the instrument and its listing. It does not
claim that the market has priced a note.

## Before submitting

1. Make the repository public.
2. Enable GitHub Pages with *GitHub Actions* as the source, run the
   `pages` workflow, and put the URL in *Links*.
3. Record the demo video from the script in [`docs/09`](09-demo.md), and
   put its URL in *Links*.
4. If an independent buyer has bought a note (`scripts/note-ask.sh`,
   `scripts/note-buy.sh`), update the Kuru entry and the claim in
   `docs/12` with the transaction.
5. Open every link above in a logged-out browser window.
6. Complete the public profile, then paste each section into the form.
