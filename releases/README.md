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

# 3. The two rows it cannot reach locally, if you want them in the tag:
#    - the independent reader needs funded keys on 10143
#    - the indexed lifecycle needs Docker
RPC=https://testnet-rpc.monad.xyz \
  CREDENCE_DEPLOYER_KEY=… CREDENCE_SIGNER_KEY=… CREDENCE_AGENT_KEY=… \
  bash scripts/demo-erc8004.sh
bash scripts/index-local.sh && bash scripts/verify-reorg.sh

# 4. Commit the record, then tag THAT commit.
git add releases/ && git commit -m "chore(release): record the baseline check"
git tag -a v0.1.0-baseline -m "Baseline, 22 September

One complete performer path and one failure path, with retained evidence
and transaction references (docs/10). Every acceptance row re-run at this
commit; see releases/<date>.md for the record and its skips."
git push origin master --follow-tags
```

**Tag the commit the record names.** The record prints the commit it ran
against and whether the tree was dirty; tagging a different commit makes
the record describe code the tag does not contain.

## What the baseline does and does not pin

It pins a **development baseline**, not the final release. `docs/11` is
explicit that a tagged development baseline may leave identified release
rows pending — and two are: a deployed CRE workflow, which needs a
commercial agreement with Chainlink, and a note traded on a market, which
is a scope decision at the 25 September gate.

Everything after the tag is an extension, developed in isolation and
re-checked before merging.
