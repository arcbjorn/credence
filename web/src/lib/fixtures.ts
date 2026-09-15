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
 * stays a skip, so `pnpm test` works in a fresh clone.
 *
 * Reported by review, 15 September.
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
