#!/usr/bin/env bash
# Copy the demos' retained evidence somewhere the OS will not delete it.
#
# The demos keep their evidence in /tmp: gateway databases, retained
# snapshots, logs. macOS deletes /tmp files that have not been accessed
# for three days, with no reboot needed. On 5 October every directory the
# 23 September runs left was gone, and `scripts/web.sh` quietly re-ran
# every demo instead of rebuilding from what had been retained. `docs/11`
# asks for a consistent backup before recording the demonstration; this
# is that backup, and the way back from it.
#
#   scripts/backup-evidence.sh                  # to ~/.credence/evidence/<stamp>
#   scripts/backup-evidence.sh --restore <dir>  # put a backup back in /tmp
#
# Databases are copied with SQLite's online backup, so a gateway still
# writing cannot leave a torn copy. A SHA256SUMS file is written beside
# the copy and checked on restore, so a restore that would bring back
# something other than what was saved stops instead.
set -euo pipefail

DEST_ROOT=${CREDENCE_EVIDENCE:-$HOME/.credence/evidence}
DIRS="credence-repay credence-monitor credence-default credence-growth credence-cohort"

if [ "${1:-}" = "--restore" ]; then
  SRC=${2:?usage: backup-evidence.sh --restore <backup dir>}
  [ -f "$SRC/SHA256SUMS" ] || { echo "$SRC has no SHA256SUMS: not a backup" >&2; exit 1; }
  (cd "$SRC" && shasum -a 256 -c --quiet SHA256SUMS) ||
    { echo "$SRC does not match its checksums; nothing restored" >&2; exit 1; }
  for d in $DIRS; do
    [ -d "$SRC/$d" ] || continue
    rm -rf "/tmp/$d"
    cp -Rp "$SRC/$d" "/tmp/$d"
    echo "  restored /tmp/$d"
  done
  exit 0
fi

command -v sqlite3 >/dev/null || { echo "sqlite3 is required for a consistent copy" >&2; exit 1; }
DEST="$DEST_ROOT/$(date -u +%Y-%m-%dT%H%M%SZ)"
found=0
for d in $DIRS; do
  [ -d "/tmp/$d" ] || continue
  found=$((found + 1))
  (cd /tmp && find "$d" -type f) | while read -r f; do
    mkdir -p "$DEST/$(dirname "$f")"
    case "$f" in
      *.db) sqlite3 "/tmp/$f" ".backup '$DEST/$f'" ;;
      *-wal | *-shm | *-journal) ;; # folded into the .db by the online backup
      *) cp -p "/tmp/$f" "$DEST/$f" ;;
    esac
  done
done
if [ "$found" -eq 0 ]; then
  echo "no demo evidence in /tmp: run the demos (scripts/web.sh --fresh) first" >&2
  exit 1
fi
(cd "$DEST" && find . -type f ! -name SHA256SUMS | sort | xargs shasum -a 256 > SHA256SUMS)
echo "backed up $found demo directories, $(($(wc -l < "$DEST/SHA256SUMS"))) files, $(du -sh "$DEST" | cut -f1)"
echo "  $DEST"
echo "restore with: scripts/backup-evidence.sh --restore $DEST"
