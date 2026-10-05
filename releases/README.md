# Release records

One file per release check, produced by `scripts/release-check.sh`.

A tag whose evidence lives only in a terminal somebody closed is a tag
nobody can check. These records exist so the claim "every acceptance row
was re-run at this commit" is verifiable afterwards rather than
remembered.

Each record carries the commit, whether the tree was clean, the Foundry
version, the chain, and a row-by-row result. **A `SKIP` is a row the run
could not reach, never a row that passed** — `docs/11` names rows without
evidence as pending rather than assumed, and the same rule applies here.

## Cutting the 22 September baseline

`docs/10` commits to "a pinned baseline release tagged on 22 September",
with extensions developed in isolation from it. There are no tags before
it; this is the first.

```sh
# 1. A clean chain, so the run indexes nothing left over.
pkill -f 'anvil --port 8546'; anvil --port 8546 --silent &

# 2. Every row, recorded. Fails non-zero if any row fails.
bash scripts/release-check.sh

#    For every row including the indexed lifecycle, which needs
#    Docker and costs an extra demo run plus a sync:
RESYNC_INDEXER=1 bash scripts/release-check.sh

#    The behavioural rows redeploy the manager at a fresh address and
#    `demo-default.sh` leaves loan 1 Defaulted, so by the time the
#    indexed row is reached the indexer is always watching a superseded
#    contract. Without the flag the check reports that as SKIP rather
#    than FAIL -- a stale indexer is the harness's doing, not the
#    code's. With it, the check restores the precondition itself.

# 3. Or close the indexed row by hand, in this order:
LOANS=2 bash scripts/demo-repay.sh   # loan 1 must be Repaid
bash scripts/index-local.sh          # sync to the CURRENT manager
bash scripts/verify-reorg.sh

# 4. Push, and wait for CI to go green on the commit you will tag.
#
#    The check's "CI on this commit" row reports this. It is a SKIP
#    while the run is queued or in progress, so a record produced
#    immediately after pushing says nothing about the build -- re-run
#    it, or read `gh run list`, before tagging.
#
#    On 21 September a baseline tag landed on a RED build: `web` had
#    been failing for two commits because a test required a fixture
#    that depends on a third-party venue. No local run could have
#    shown it, because locally the file is always present.
git push origin master
gh run watch

# 5. Commit the record, then tag THAT commit.
git add releases/ && git commit -m "chore(release): record the baseline check"
git tag -a v0.1.0-baseline -m "Baseline, 22 September

One complete performer path and one failure path, with retained evidence
and transaction references (docs/10). Every acceptance row re-run at this
commit; see releases/<date>.md for the record and its skips."
git push origin master --follow-tags
```

**Tag the commit the record names — or the commit that adds it, having
verified the two differ only by the record file.** The record prints the
commit it ran against and whether the tree was dirty; tagging a different
commit makes the record describe code the tag does not contain.

There is an unavoidable circularity here: committing the record changes
HEAD, so a record can never name the commit that carries it. Resolve it
by checking that the record commit touches nothing else —

```sh
git diff --name-only <record-commit-parent> <record-commit> | grep -v '^releases/'
```

— and tagging the record commit when that prints nothing. The tag then
contains both the code the record describes and the evidence for it.

## What the baseline does and does not pin

It pins a **development baseline**, not the final release. `docs/11` is
explicit that a tagged development baseline may leave identified release
rows pending — and one is: a deployed CRE workflow, which needs a
commercial agreement with Chainlink.

The note-on-a-market row closed on 20 September (`NoteSeries` listed on
Kuru), and the independent reputation reader closed on 21 September. That
last one is now checked on **every** run rather than announced as
unreachable: reading the registry needs no key and no funds, which is
precisely the property the row asserts, so `scripts/verify-reputation.sh`
runs read-only against the canonical deployment.

Everything after the tag is an extension, developed in isolation and
re-checked before merging.

## Latency records

`scripts/measure-latency.sh` writes `latency-<date>.md` here, with the
raw observations beside it in `latency-<date>.jsonl`. It is a different
kind of record from the release checks above: it runs loans on the
deployment the manifest names and reports how long each step took, so
it measures rather than passes or fails. Every transaction it sent is
listed by hash, so each figure can be checked on chain.
