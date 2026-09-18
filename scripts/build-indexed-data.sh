#!/usr/bin/env bash
# Export the indexer's rows into the web bundle.
#
# `docs/09` requires every displayed number to come from a retained
# fixture or a transaction-derived record, and `scripts/build-web-data.sh`
# enforces that structurally: the demo produced the file, or the screen
# renders nothing. The indexed lifecycle is held to the same rule rather
# than queried live from the page -- a screen that talks to a local
# GraphQL endpoint shows nothing in the submitted static folder, and
# "works on the machine that has Postgres running" is not evidence.
#
# So this reads what the indexer actually stored and writes it beside the
# other bundle files. Run it after `scripts/index-local.sh` has synced.
#
# Absent indexer, absent file: the screen then says the sync has not been
# run, which is true, rather than rendering an empty lifecycle that reads
# like a loan with no events.
set -euo pipefail

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
OUT="$ROOT/web/public/data"
CONTAINER=${ENVIO_PG_CONTAINER:-envio-postgres}
DB=${ENVIO_PG_DATABASE:-envio-dev}
USER=${ENVIO_PG_USER:-postgres}

if ! docker exec "$CONTAINER" psql -U "$USER" -d "$DB" -tAc 'select 1' \
  >/dev/null 2>&1; then
  echo "no indexer database ($CONTAINER/$DB)." >&2
  echo "Run scripts/index-local.sh first; it must reach 'indexer is ready'." >&2
  exit 1
fi

mkdir -p "$OUT"

# One query, emitting JSON from Postgres itself rather than parsing psql's
# table output: the amounts are uint256 and a text round-trip through awk
# is exactly where precision goes missing.
#
# Amounts stay STRINGS all the way to the page. JSON numbers are doubles,
# and 4000000000 survives that while a realistic principal does not.
docker exec "$CONTAINER" psql -U "$USER" -d "$DB" -tAc "$(
  cat <<'SQL'
-- Order by the NUMERIC columns, and cast to text only in the projection.
--
-- `order by` on the cast alias sorts lexicographically: block 6 lands
-- after block 14, and the lifecycle renders with origination last. The
-- sort keys are therefore carried through the subquery unconverted and
-- dropped from the emitted row with `- 'sort_block'`.
select json_build_object(
  'loans', coalesce((
    select json_agg((to_jsonb(l) - 'sort_loan') order by l.sort_loan)
    from (
      select
        "loanId"                   as sort_loan,
        id, "chainId", "loanId"::text as "loanId", account,
        principal::text            as principal,
        status,
        "principalRepaid"::text    as "principalRepaid",
        "interestCollected"::text  as "interestCollected",
        "principalLoss"::text      as "principalLoss",
        "unpaidInterest"::text     as "unpaidInterest",
        "originatedAt"::text       as "originatedAt",
        "recoveryOpenedAt"::text   as "recoveryOpenedAt",
        "resolvedAt"::text         as "resolvedAt",
        "heldSeconds"::text        as "heldSeconds"
      from "Loan"
    ) l
  ), '[]'::json),
  'events', coalesce((
    select json_agg(
      (to_jsonb(e) - 'sort_block') order by e.sort_block, e."logIndex"
    )
    from (
      select
        "blockNumber"          as sort_block,
        id, loan_id, "chainId", kind,
        "blockNumber"::text    as "blockNumber",
        "blockTimestamp"::text as "blockTimestamp",
        "logIndex", "txHash",
        principal::text        as principal,
        interest::text         as interest,
        "principalLoss"::text  as "principalLoss",
        "unpaidInterest"::text as "unpaidInterest"
      from "LoanEvent"
    ) e
  ), '[]'::json)
)
SQL
)" > "$OUT/indexed.json"

python3 - "$OUT/indexed.json" <<'PY'
import json
import sys

path = sys.argv[1]
with open(path) as f:
    data = json.load(f)

loans, events = data["loans"], data["events"]

# Refuse to write a bundle that would render as a lifecycle with no
# lifecycle. An empty index means the sync did not reach this chain --
# a different thing from a chain with no loans, and the screen should
# not have to guess which.
if not loans and not events:
    raise SystemExit(
        "the indexer database is empty: nothing was synced.\n"
        "Check that scripts/index-local.sh reported 'The indexer is ready'."
    )

# Re-write it formatted, so a reviewer can read the file the page reads.
with open(path, "w") as f:
    json.dump(data, f, indent=1, sort_keys=True)

print(f"indexed  {len(loans)} loan(s), {len(events)} event(s) -> indexed.json")
for loan in loans:
    print(
        f"  loan {loan['loanId']:>3}  {loan['status']:<15}"
        f" repaid={loan['principalRepaid']}"
        f" loss={loan['principalLoss']}"
    )
PY
