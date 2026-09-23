import { readFileSync, existsSync } from "node:fs";
import { resolve } from "node:path";
import { fixtureMissing } from "./fixtures";
import { describe, expect, it } from "vitest";
import type { Comparison } from "./data";

/**
 * `docs/09` beat 1 and beat 5, as the screen renders them.
 *
 * The thesis is `docs/03`'s: **a perfect record does not survive a
 * dangerous book.** Two of these profiles share a flawless repayment
 * history and reach opposite outcomes, and the only difference between
 * them is what the account currently holds.
 *
 * Pinned because the screen's whole argument collapses if the
 * concentrated profile is ever approved — it would then be showing
 * accounts that all get credit, which demonstrates nothing.
 *
 * The set grew from three to eight so the globe shows the SHAPE of the
 * decision space rather than a line through it. The count is asserted as
 * a floor rather than an equality: adding a profile that widens the
 * space is an improvement and should not fail here, while dropping back
 * to the original three would silently undo it. `emit_comparison`'s own
 * `every_comparison_profile_says_something_new` is what stops a profile
 * being added that duplicates an existing answer.
 */
const ROOT = resolve(process.cwd(), "..");
const FILE = `${ROOT}/web/public/data/comparison.json`;

describe.skipIf(fixtureMissing(FILE))("the agent comparison", () => {
  const cmp = (): Comparison => JSON.parse(readFileSync(FILE, "utf8"));

  it("carries the profiles under one policy and one model", () => {
    const c = cmp();
    expect(c.profiles.length).toBeGreaterThanOrEqual(5);
    expect(c.policy_hash).toMatch(/^[0-9a-f]{64}$/);
    expect(c.model_hash).toMatch(/^[0-9a-f]{64}$/);
    // Every profile assessed under the SAME labels, or the comparison is
    // not controlled.
    for (const p of c.profiles) {
      expect(p.assessment.policy_hash).toBe(c.policy_hash);
      expect(p.assessment.model_hash).toBe(c.model_hash);
    }
  });

  it("shows history opening the facility", () => {
    const c = cmp();
    const unproven = c.profiles.find((p) => p.name === "Unproven")!;
    const proven = c.profiles.find((p) => p.name === "Proven")!;

    const cap = (s: string | undefined) => BigInt(s ?? "0");
    expect(cap(proven.assessment.outcome.capacity?.agent_capacity)).toBeGreaterThan(
      cap(unproven.assessment.outcome.capacity?.agent_capacity),
    );
  });

  it("denies a flawless record against a dangerous book", () => {
    // The sharpest row, and the one the screen's argument rests on.
    const c = cmp();
    const proven = c.profiles.find((p) => p.name === "Proven")!;
    const concentrated = c.profiles.find((p) => p.name === "Concentrated")!;

    expect(proven.assessment.outcome.decision).toBe("ApprovedOrigination");
    expect(concentrated.assessment.outcome.decision).not.toBe(
      "ApprovedOrigination",
    );
    // And for the right reason: stress, not history.
    expect(concentrated.assessment.outcome.capacity?.agent_binding).toBe(
      "StressCapacity",
    );
  });
});
