# 10 · Release process

A release is an annotated tag on a commit whose acceptance rows were all
re-run, with the record of that run committed beside it. This document
states the rules; [`releases/README.md`](../releases/README.md) has the
commands.

## Rules

**Pinned releases.** Each release tag names a commit and a dated record
in `releases/`. The first, `v0.1.0-baseline`, pinned the complete core
path: one request explained, enforced, repaid or recovered, and replayed
from retained evidence. Every later change must preserve that behaviour.

**Re-run before merge.** `scripts/release-check.sh` runs every acceptance
row and writes the record. A row it cannot reach is recorded as `SKIP`,
never as a pass, and rows without evidence stay pending
([11 · Verification](11-verification.md)).

**Freeze before release.** A release candidate freezes contracts,
schemas, the model and fixtures. After the freeze, changes are fixes
only.

**Isolated extensions.** Integrations are built apart from the pinned
release and re-checked before they merge. A failing integration must
leave the core path unchanged.

> **Scope reductions preserve three outcomes: accountable decisions, enforced capital rules, visible recovery and losses. Secondary integrations are removed before any of these is weakened.**

**Transport gate.** JSON is the reference transport. The gRPC transport
is accepted only if it passes the same reference vectors and the
applicable acceptance matrix, byte for byte, against frozen artifacts
(`scripts/verify-grpc.sh`). Otherwise JSON is retained.

**Named test assets.** Every token the prototype deploys is named so it
cannot be mistaken for a real asset: the settlement token is
`Credence Test Settlement (NOT USDC)` (`tCRED`), and the risky asset is
`Credence Test Risky Asset (NOT ETH)` (`tRISK`). Anyone may mint either.

**Public interface.** Every release tag runs the scenarios on a fresh
chain, indexes them, builds the interface from what they retained, and
publishes the static site (`.github/workflows/pages.yml`). The one
checked-in input is the Atlas comparison, which the engine emits and
`comparison_fixture_matches_the_engine` pins.

## Releases

| Tag | Commit | Record |
|---|---|---|
| `v0.1.0-baseline` | `e8815d8` | [`releases/2026-09-21.md`](../releases/2026-09-21.md) |
| `v0.2.0-rc1` | `4ac5e35` | [`releases/2026-09-24.md`](../releases/2026-09-24.md) |
