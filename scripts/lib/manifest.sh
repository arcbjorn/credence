# Preserve the chain-level facts a redeploy would otherwise destroy.
#
# `Deploy.s.sol` builds the manifest from scratch and overwrites the file,
# because Solidity cannot merge JSON. That is fine for the addresses it
# just deployed and wrong for everything else in there: the ERC-8004
# registries and the CRE forwarders describe the CHAIN, not this
# deployment. They are still at the same addresses afterwards, and
# `docs/06` requires the registered agent id in a release manifest.
#
# Observed 19 September: a redeploy dropped the `erc8004` block, and
# `demo-repay.sh` then refused to bind loans -- correctly, because the
# agent id it needed had just been deleted. The refusal was the guard
# working; the deletion was the bug.
#
# So the blocks are snapshotted before the deploy and merged back after.
# Merged, not restored wholesale: `addresses` comes from the new
# deployment, and only keys the deploy does not write are carried over.

# credence_manifest_snapshot <manifest-path>
#
# Echoes the preserved blocks as JSON, or `{}` when there is no manifest
# yet. Capture it BEFORE running the deploy.
credence_manifest_snapshot() {
  local path=$1
  [ -f "$path" ] || { echo '{}'; return; }
  python3 - "$path" <<'PY'
import json
import sys

try:
    with open(sys.argv[1]) as f:
        manifest = json.load(f)
except (OSError, json.JSONDecodeError):
    print("{}")
    raise SystemExit

keep = {k: manifest[k] for k in ("erc8004", "cre") if k in manifest}

# The CRE receiver is deployed separately and `Deploy.s.sol` does not
# write it, so it belongs with the chain-level facts rather than the
# addresses this deploy produced.
receiver = (manifest.get("addresses") or {}).get("cre_receiver")
if receiver:
    keep["_cre_receiver"] = receiver

print(json.dumps(keep))
PY
}

# credence_manifest_restore <manifest-path> <snapshot-json>
#
# Merges the snapshot back into a freshly written manifest. Never
# overwrites a key the deploy wrote.
credence_manifest_restore() {
  local path=$1 snapshot=$2
  [ -f "$path" ] || return 0
  [ -n "$snapshot" ] && [ "$snapshot" != "{}" ] || return 0

  python3 - "$path" "$snapshot" <<'PY'
import json
import sys

path, raw = sys.argv[1], sys.argv[2]
with open(path) as f:
    manifest = json.load(f)
keep = json.loads(raw)

restored = []
receiver = keep.pop("_cre_receiver", None)
for key, value in keep.items():
    if key not in manifest:
        manifest[key] = value
        restored.append(key)

if receiver and "cre_receiver" not in manifest.setdefault("addresses", {}):
    manifest["addresses"]["cre_receiver"] = receiver
    restored.append("addresses.cre_receiver")

if restored:
    with open(path, "w") as f:
        json.dump(manifest, f, indent=2)
        f.write("\n")
    print("  manifest: carried over " + ", ".join(restored))
PY
}
