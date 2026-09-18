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

python3 - "$OUT" "$REPAY_DB" "$MONITOR_DB" "$DEFAULT_DB" \
  /tmp/credence-repay /tmp/credence-monitor /tmp/credence-default <<'PY'
import json, os, sqlite3, sys

out, *rest = sys.argv[1:]
dbs, works = rest[:3], rest[3:]
REPO = os.path.abspath(os.path.join(out, "..", "..", ".."))
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
    """Payload columns hold JSON text or bytes; everything else passes.

    Bytes are decoded to text FIRST, so a non-JSON blob still lands as a
    readable string rather than the Python repr `b'...'`.
    """
    if isinstance(v, (bytes, bytearray)):
        v = v.decode("utf-8", "replace")
    if isinstance(v, str) and v[:1] in "{[":
        try:
            return json.loads(v)
        except json.JSONDecodeError:
            return v
    return v

manifest = {"runs": [], "generated_from": []}

for name, db, work in zip(names, dbs, works):
    # A run can retain on-chain checks WITHOUT a database.
    #
    # `demo-default.sh` drives the contracts directly and never starts the
    # gateway, so it retains no SQLite -- and the whole run was therefore
    # dropped here, taking its realised loss with it. The lender screen
    # then read `demo-repay`, where nothing ever defaults, and its
    # "realized loss" figure was structurally always zero: the one screen
    # whose job is showing a lender loss could never show one.
    checks_path = os.path.join(work, "checks.json") if work else ""
    has_db = bool(db) and os.path.exists(db)
    has_checks = bool(checks_path) and os.path.exists(checks_path)

    if not has_db and not has_checks:
        print(f"  {name:<8} (nothing retained; skipped)")
        continue

    if not has_db:
        with open(checks_path) as f:
            run = {"name": name, "checks": json.load(f), "assessments": [],
                   "snapshots": {}, "outcomes": [], "forecasts": [],
                   "chain_events": []}
        with open(os.path.join(out, f"{name}.json"), "w") as f:
            json.dump(run, f, indent=1, sort_keys=True)
        print(f"  {name:<8} {len(run['checks'])} on-chain checks (no database)")
        manifest["runs"].append({
            "name": name, "assessments": 0, "snapshots": 0, "outcomes": 0,
        })
        manifest["generated_from"].append(checks_path)
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

    # Decode EVERY row, not just assessments and snapshots.
    #
    # `parse` was applied to two tables and `default=str` caught the rest,
    # which turned a bytes column into the Python repr `"b'{...}'"` -- a
    # string no consumer can parse, shipped as if it were data. Eight
    # fields across the two bundles carried it. Reported by review,
    # 14 September.
    def decode(rows_):
        for r in rows_:
            for k, v in r.items():
                r[k] = parse(v)
        return rows_

    for a in assessments:
        a["payload"] = parse(a.get("payload"))
    by_input = {s["input_hash"]: parse(s["payload"]) for s in snapshots}
    outcomes = decode(outcomes)
    forecasts = decode(forecasts)
    events = decode(events)

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
        json.dump(run, f, indent=1, sort_keys=True)

    # The on-chain checks `credence-verify` ran against this deployment.
    # `docs/09` beat 6 is "the single most convincing moment", and it was
    # terminal-only: the interface can now show the two DIFFERENT revert
    # selectors rather than a sentence about them.
    if has_checks:
        with open(checks_path) as f:
            run["checks"] = json.load(f)
        with open(os.path.join(out, f"{name}.json"), "w") as f:
            json.dump(run, f, indent=1, sort_keys=True)

    manifest["runs"].append({
        "name": name,
        "assessments": len(assessments),
        "snapshots": len(by_input),
        "outcomes": len(outcomes),
    })
    manifest["generated_from"].append(db)
    print(f"  {name:<8} {len(assessments)} assessments, {len(by_input)} snapshots")

# The policy each run ACTUALLY hashed, emitted per run.
#
# This used to copy `config/engine-policy.json` unconditionally and call it
# "the policy the demos ran under". It was not: `demo-monitor.sh` runs on a
# gateway template whose `base_history_cap` is 10e9, while the shipped
# policy is 6.2e9. The engine refuses a declared policy hash that is not
# the content hash of the policy it was handed, so a browser replay of a
# monitor assessment returned `Unavailable` -- a DIFFERENCE on a CLEAN
# replay, which makes the whole screen vacuous.
#
# Each run's policy is written beside its data and checked against the
# hash its own assessments declare. A mismatch fails this script rather
# than shipping a replay screen that cannot match.
# Reported by review, 14 September.
for name, db in zip(names, dbs):
    if not os.path.exists(db):
        continue
    run_path = os.path.join(out, f"{name}.json")
    if not os.path.exists(run_path):
        continue
    with open(run_path) as f:
        run = json.load(f)
    if not run["assessments"]:
        continue

    declared = {a["policy_hash"] for a in run["assessments"]}

    # The gateway config beside the database is what the run was given.
    cfg_path = os.path.join(os.path.dirname(db), "gateway.json")
    policy = None
    if os.path.exists(cfg_path):
        with open(cfg_path) as f:
            policy = json.load(f).get("engine_policy")
    if policy is None:
        # Fall back to the shipped policy, but only if the run's own
        # assessments agree with it -- checked below.
        with open(os.path.join(REPO, "config", "engine-policy.json")) as f:
            policy = json.load(f)

    out_path = os.path.join(out, f"{name}-policy.json")
    with open(out_path, "w") as f:
        json.dump(policy, f, indent=1, sort_keys=True)

    # VERIFY, do not assume. The engine hashes the policy it is handed and
    # refuses a declared hash that does not match, so a policy that merely
    # looks right produces `Unavailable` in the browser and a DIFFERENCE on
    # a clean replay.
    import subprocess
    got = subprocess.run(
        ["go", "run", "./cmd/credence-policyhash",
         os.path.relpath(out_path, os.path.join(REPO, "gateway"))],
        cwd=os.path.join(REPO, "gateway"),
        capture_output=True, text=True).stdout.strip()
    if got not in declared:
        raise SystemExit(
            f"{name}: emitted policy hashes to {got}, but its assessments "
            f"declare {sorted(declared)}. The browser would replay this "
            f"pair as a DIFFERENCE. Regenerate the run, or point this at "
            f"the policy it was given.")
    print(f"  {name:<8} policy -> {os.path.basename(out_path)}"
          f" (hash {got[:12]}... verified)")

# `docs/09` beat 1: the three-profile comparison, assessed by the real
# engine under the shipped policy. Emitted by
# `cargo test --test ablation -- --ignored emit_comparison`, and pinned by
# `comparison_fixture_matches_the_engine` so it cannot drift from what the
# engine says.
cmp_path = os.path.join(REPO, "fixtures", "demo", "comparison.json")
if os.path.exists(cmp_path):
    with open(cmp_path) as f:
        comparison = json.load(f)
    with open(os.path.join(out, "comparison.json"), "w") as f:
        json.dump(comparison, f, indent=1, sort_keys=True)
    print(f"  {'compare':<8} {len(comparison['profiles'])} profiles")

# The deployment manifest is the single source of truth for addresses.
dep = os.path.join(os.path.dirname(out), "..", "..", "contracts",
                   "deployments", "10143.json")
if os.path.exists(dep):
    with open(dep) as f:
        manifest["deployment"] = json.load(f)

with open(os.path.join(out, "manifest.json"), "w") as f:
    json.dump(manifest, f, indent=1, sort_keys=True)
PY

# The indexed lifecycle, when the indexer happens to be running.
#
# Optional on purpose, and the only optional part of this bundle. Every
# other file comes from a demo, which needs nothing but a local chain;
# this one needs Docker and a synced HyperIndex. Making it required would
# mean a developer without Docker could not build the interface at all,
# and CI would fail for a reason unrelated to the change under test.
#
# Its absence is a state the screen renders honestly -- it says the sync
# has not been run and gives the command -- so skipping here costs a
# screen, not correctness.
if bash "$ROOT/scripts/build-indexed-data.sh" 2>/dev/null; then
  :
else
  echo "  indexed  (no indexer running; skipped)"
fi

echo
echo "data written to web/public/data/"
du -sh "$OUT" | awk '{print "  total  " $1}'
