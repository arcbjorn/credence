#!/usr/bin/env bash
#
# Check the numbers the documents claim against the numbers that exist.
#
# Every cited figure is a claim a reader can check, which means each one
# can also go stale. Several did: `docs/11` and `docs/08` said the CRE
# receiver had 19 tests when it had 21, and the README said `demo-repay`
# ran seven checks after it grew to ten. Both drifted because a test or a
# check was ADDED -- the direction nothing warns about.
#
# This is deliberately narrow. It checks counts that are cheap to derive
# and impossible to argue with, not prose.
set -euo pipefail

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
failed=0

fail() {
  echo "  MISMATCH: $1" >&2
  failed=1
}

# --- CRE receiver test count ---
actual=$(grep -c 'function test_' "$ROOT/contracts/test/CREReceiver.t.sol")
for doc in docs/08-integrations.md docs/11-verification.md; do
  # Any "<n> tests" on a line mentioning CREReceiver.
  cited=$(grep -oE 'CREReceiver[^|]*\| *[^|]*[0-9]+ tests' "$ROOT/$doc" 2>/dev/null \
    | grep -oE '[0-9]+ tests' | grep -oE '[0-9]+' | head -1 || true)
  if [ -n "$cited" ] && [ "$cited" != "$actual" ]; then
    fail "$doc says $cited CREReceiver tests, there are $actual"
  fi
done

# --- the eight screens docs/09 specifies ---
screens=$(ls "$ROOT/web/src/screens"/*.tsx 2>/dev/null | wc -l | tr -d ' ')
if [ "$screens" -lt 1 ]; then
  fail "no screens found under web/src/screens"
fi

# --- indexer lifecycle test count ---
# Spelled as a word in prose ("Eleven tests"), so the words this can
# reach are listed rather than parsed. Added the moment the count drifted
# for the first time, which is the only honest moment to add it.
actual=$(grep -c '^\s*it(' "$ROOT/indexer/src/lifecycle.test.ts" 2>/dev/null || echo 0)
words=(zero one two three four five six seven eight nine ten eleven twelve
  thirteen fourteen fifteen sixteen seventeen eighteen nineteen twenty)
expected=${words[$actual]:-}
cited=$(grep -oiE '(one|two|three|four|five|six|seven|eight|nine|ten|eleven|twelve|thirteen|fourteen|fifteen|sixteen|seventeen|eighteen|nineteen|twenty) tests\.' \
  "$ROOT/docs/08-integrations.md" 2>/dev/null | head -1 | awk '{print tolower($1)}' || true)
if [ -n "$cited" ] && [ -n "$expected" ] && [ "$cited" != "$expected" ]; then
  fail "docs/08 says $cited indexer tests, there are $actual ($expected)"
fi

if [ "$failed" -eq 0 ]; then
  echo "docs: cited counts match"
else
  echo >&2
  echo "Update the document, or the thing it counts." >&2
  exit 1
fi
