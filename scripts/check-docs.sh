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

# --- every internal link resolves ---
#
# A broken link in the README is the first thing a judge hits and the
# last thing anyone notices: nothing fails, the page just 404s. Checked
# mechanically because checking it by hand is how it rots.
broken=""
while read -r src target; do
  # Strip any #anchor; the file is what must exist.
  path=${target%%#*}
  [ -n "$path" ] || continue
  case "$path" in
    http*|mailto:*) continue ;;
  esac
  dir=$(dirname "$src")
  [ -e "$dir/$path" ] || broken="$broken\n  $src -> $target"
done < <(
  grep -oHE '\]\([^)]+\)' "$ROOT/README.md" "$ROOT"/docs/*.md 2>/dev/null |
    sed -E 's/\]\(/ /; s/\)$//' | tr -d '\]'
)
if [ -n "$broken" ]; then
  # shellcheck disable=SC2059
  fail "$(printf "broken internal link(s):$broken")"
fi

# --- docs/14's test counts ---
#
# Counted from the sources rather than run: `forge test` and `pnpm test`
# take minutes, and this gate runs on every push. Counting the `function
# test_` and `it(` declarations is exact for these suites and costs
# milliseconds.
#
# Added because the table went stale within hours of being written --
# five fixes on 21 September added tests and the numbers stayed put, in
# the very document that records "a count nothing verifies is a claim".
report="$ROOT/docs/14-verification-report.md"
if [ -f "$report" ]; then
  sol=$(grep -rhoE '^\s*function test_[A-Za-z0-9_]*\(' "$ROOT"/contracts/test/*.sol |
    wc -l | tr -d ' ')
  web=$(grep -rhoE '^\s*it\(' "$ROOT"/web/src/**/*.test.ts* 2>/dev/null | wc -l | tr -d ' ')
  claimed_sol=$(grep -oE '^\| Solidity \| \*\*[0-9]+\*\*' "$report" |
    grep -oE '[0-9]+' | head -1)
  claimed_web=$(grep -oE '^\| TypeScript \(`web`\) \| \*\*[0-9]+\*\*' "$report" |
    grep -oE '[0-9]+' | head -1)

  # `forge test` reports more than the declarations, because an
  # invariant campaign counts as one test while declaring one function.
  # So the check is that the claim is not BELOW what is declared: a
  # table that undercounts is stale, and one that matches or exceeds is
  # consistent with a real run.
  if [ -n "$claimed_sol" ] && [ "$claimed_sol" -lt "$sol" ]; then
    fail "docs/14 claims $claimed_sol Solidity tests; $sol are declared"
  fi
  if [ -n "$claimed_web" ] && [ "$claimed_web" -lt "$web" ]; then
    fail "docs/14 claims $claimed_web web tests; $web are declared"
  fi
fi

# --- the eight screens docs/09 specifies, plus any beyond that list ---
screens=$(ls "$ROOT/web/src/screens"/*.tsx 2>/dev/null | wc -l | tr -d ' ')
if [ "$screens" -lt 1 ]; then
  fail "no screens found under web/src/screens"
fi

# The count is spelled as an ordinal in prose ("a ninth", "a tenth"), and
# it drifted TWICE unnoticed -- `docs/01` still said "ninth" after the
# tenth screen shipped, and the README still said "eight screens" after
# the ninth did. A reader who counts the nav and finds more than the docs
# admit has no reason to trust the next count either.
#
# So the ordinal for the HIGHEST screen must appear wherever the extras
# are described. `docs/09` is exempt: it lists every screen by route and
# names the eight record screens.
# Either phrasing is fine -- the ordinal for the highest screen ("tenth"),
# or the count of extras beyond the eight ("plus two"). What must not
# happen is a document describing the extras while naming a SMALLER
# number than exists.
ordinals=(zero first second third fourth fifth sixth seventh eighth ninth
  tenth eleventh twelfth thirteenth fourteenth fifteenth)
numbers=(zero one two three four five six seven eight nine ten)
extra=$((screens - 8))
if [ "$extra" -gt 0 ]; then
  highest=${ordinals[$screens]:-}
  count=${numbers[$extra]:-}
  # Scoped to the LINES that mention screens, not the whole file: an
  # earlier version grepped file-wide and passed on wrong text because
  # `docs/01` says "plus two" about DIRECTORIES elsewhere. A check that
  # can be satisfied by an unrelated sentence checks nothing.
  for f in "$ROOT/docs/01-architecture.md" "$ROOT/README.md"; do
    lines=$(grep -i "screens" "$f" || true)
    [ -n "$lines" ] || continue
    if ! echo "$lines" | grep -qi "$highest" &&
      ! echo "$lines" | grep -qi "plus $count"; then
      fail "$(basename "$f") describes screens but names neither \"$highest\" nor \"plus $count\"; $screens exist"
    fi
  done
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
