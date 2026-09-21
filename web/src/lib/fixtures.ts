/**
 * Whether a test bundle is required, or may be absent.
 *
 * The data-dependent suites used `describe.skipIf(!exists)` uniformly,
 * which is right for a developer who has not run the demos and wrong for
 * CI: a clean run reported 19 passed and 19 SKIPPED, green, while testing
 * half the interface. Nothing distinguished "not generated here" from
 * "generation is broken".
 *
 * `CREDENCE_REQUIRE_FIXTURES=1` — set in CI, after the bundle is
 * generated — turns a missing bundle into a failure instead. Locally it
 * stays a skip, so a missing BUNDLE does not stop `pnpm test`.
 *
 * A missing ENGINE still does, and cannot be skipped the same way:
 * `src/lib/engine.ts` imports `src/engine/` statically, so the failure is
 * at compile time. `scripts/require-engine.mjs` runs before the suite and
 * says which command builds it, rather than leaving a reader with three
 * module-resolution errors.
 *
 * Reported by review, 15 September.
 *
 * **Only for fixtures a demo produces.** `fixtureMissing` makes a missing
 * file a CI failure, which is right for anything a local chain and the Go
 * toolchain can always generate. It is wrong for a capture that depends
 * on a THIRD PARTY being reachable: `perpl.json` used this helper and
 * turned a green build into a hostage of app.perpl.xyz, reddening CI on
 * 21 September over something no change here could cause or fix. Such a
 * capture skips with a plain `existsSync`, and its liveness is checked by
 * `scripts/release-check.sh` against the real venue instead.
 */
import { existsSync } from "node:fs";

export const FIXTURES_REQUIRED =
  process.env.CREDENCE_REQUIRE_FIXTURES === "1";

/**
 * Report whether a fixture is usable, failing loudly when CI expected it.
 */
export function fixtureMissing(path: string): boolean {
  const missing = !existsSync(path);
  if (missing && FIXTURES_REQUIRED) {
    throw new Error(
      `${path} is missing, and CREDENCE_REQUIRE_FIXTURES=1. ` +
        `Generate it with: bash scripts/build-web-data.sh`,
    );
  }
  return missing;
}
