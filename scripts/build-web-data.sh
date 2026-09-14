#!/usr/bin/env bash
#
# Emit the web bundle's data from what the demos retained.
#
# `docs/09`: "Every displayed number comes from a retained fixture or an
# actual transaction-derived record." A generated bundle enforces that
# structurally rather than by discipline -- the demo produced the file, or
# the screen has nothing to render. There is no path where the interface
# computes a figure the engine never returned.
#
# It also means the submission is a static folder. Nothing to run during
# judging, nothing to fail.
set -euo pipefail

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
OUT="$ROOT/web/public/data"

# Each demo leaves its retained evidence behind. Read them where they are
# rather than re-running: a bundle built from a fresh run of a script that
# happened to behave differently is not the run that was recorded.
#
# The filename differs by demo -- `demo-repay` writes scenario.db and
# `demo-monitor` writes credence.db -- so each is DISCOVERED rather than
# assumed. An earlier version hardcoded one name and silently skipped the
# other two, producing a bundle that looked complete.
find_db() {
  local dir=$1
  for candidate in "$dir"/credence.db "$dir"/scenario.db; do
    [ -f "$candidate" ] && { echo "$candidate"; return; }
  done
  # Any other .db in the directory. A demo that retained nothing yields
  # an empty string, which the generator reports as skipped -- `|| true`
  # because an absent directory must not abort the whole bundle.
  find "$dir" -maxdepth 1 -name '*.db' -type f 2>/dev/null | head -1 || true
}

REPAY_DB=${REPAY_DB:-$(find_db /tmp/credence-repay || true)}
MONITOR_DB=${MONITOR_DB:-$(find_db /tmp/credence-monitor || true)}
DEFAULT_DB=${DEFAULT_DB:-$(find_db /tmp/credence-default || true)}

mkdir -p "$OUT"

python3 - "$OUT" "$REPAY_DB" "$MONITOR_DB" "$DEFAULT_DB" <<'PY'
import json, os, sqlite3, sys

out, *dbs = sys.argv[1:]
names = ["repay", "monitor", "default"]

def rows(db, sql):
    if not os.path.exists(db):
        return []
    c = sqlite3.connect(f"file:{db}?mode=ro", uri=True)
    c.row_factory = sqlite3.Row
    try:
        return [dict(r) for r in c.execute(sql)]
    except sqlite3.Error:
        return []
    finally:
        c.close()

def parse(v):
    """Payload columns hold JSON text or bytes; everything else passes."""
    if isinstance(v, (bytes, bytearray)):
        v = v.decode("utf-8", "replace")
    if isinstance(v, str) and v[:1] in "{[":
        try:
            return json.loads(v)
        except json.JSONDecodeError:
            return v
    return v

manifest = {"runs": [], "generated_from": []}

for name, db in zip(names, dbs):
    if not os.path.exists(db):
        print(f"  {name:<8} (no database at {db}; skipped)")
        continue

    assessments = rows(db, """
        SELECT assessment_id, binding_key, input_hash, model_hash,
               policy_hash, assessment_kind, decision, risk_permission,
               sequence, assessment_hash, created_at, payload
        FROM assessments ORDER BY sequence
    """)
    snapshots = rows(db, "SELECT input_hash, payload FROM snapshots")
    outcomes  = rows(db, "SELECT * FROM outcomes")
    forecasts = rows(db, "SELECT * FROM forecasts")
    events    = rows(db, "SELECT * FROM chain_events")

    for a in assessments:
        a["payload"] = parse(a.get("payload"))
    by_input = {s["input_hash"]: parse(s["payload"]) for s in snapshots}

    run = {
        "name": name,
        "assessments": assessments,
        # Keyed by input_hash so the replay screen can pair an assessment
        # with the exact snapshot it was computed from.
        "snapshots": by_input,
        "outcomes": outcomes,
        "forecasts": forecasts,
        "chain_events": events,
    }
    with open(os.path.join(out, f"{name}.json"), "w") as f:
        json.dump(run, f, indent=1, sort_keys=True, default=str)

    manifest["runs"].append({
        "name": name,
        "assessments": len(assessments),
        "snapshots": len(by_input),
        "outcomes": len(outcomes),
    })
    manifest["generated_from"].append(db)
    print(f"  {name:<8} {len(assessments)} assessments, {len(by_input)} snapshots")

# The engine policy the demos ran under, so a replay in the browser uses
# the same one rather than a copy that can drift.
with open(os.path.join(os.path.dirname(out), "..", "..", "config",
                       "engine-policy.json")) as f:
    policy = json.load(f)
with open(os.path.join(out, "policy.json"), "w") as f:
    json.dump(policy, f, indent=1, sort_keys=True)

# The deployment manifest is the single source of truth for addresses.
dep = os.path.join(os.path.dirname(out), "..", "..", "contracts",
                   "deployments", "10143.json")
if os.path.exists(dep):
    with open(dep) as f:
        manifest["deployment"] = json.load(f)

with open(os.path.join(out, "manifest.json"), "w") as f:
    json.dump(manifest, f, indent=1, sort_keys=True)
PY

echo
echo "data written to web/public/data/"
du -sh "$OUT" | awk '{print "  total  " $1}'
