import { readFileSync, existsSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";
import type { Check } from "./data";

/**
 * `docs/09` beat 6 turns on the two refusals being DIFFERENT.
 *
 * The owner is refused because debt is open; a stranger because they are
 * not the owner. If those produced the same error, the first would only
 * be demonstrating access control — and the claim being made is lender
 * protection, which is a strictly stronger thing.
 *
 * Pinned here because it is easy to lose: a refactor that unified the two
 * reverts would leave every screen looking identical and the demo's most
 * convincing beat quietly hollow.
 */
const ROOT = resolve(process.cwd(), "..");
const REPAY = `${ROOT}/web/public/data/repay.json`;

describe.skipIf(!existsSync(REPAY))("beat 6, as recorded on chain", () => {
  const checks = (): Check[] =>
    JSON.parse(readFileSync(REPAY, "utf8")).checks ?? [];

  it("recorded the beat 6 checks", () => {
    const beat6 = checks().filter((c) => c.beat === "6");
    expect(beat6.length).toBeGreaterThanOrEqual(3);
  });

  it("passed every check against the deployment", () => {
    for (const c of checks()) {
      expect(c.passed, `${c.beat} ${c.name}`).toBe(true);
    }
  });

  it("refuses the owner and a stranger for DIFFERENT reasons", () => {
    const beat6 = checks().filter((c) => c.beat === "6");
    const owner = beat6.find((c) => c.name.includes("owner withdrawal"));
    const stranger = beat6.find((c) => c.name.includes("stranger"));

    expect(owner, "no owner-withdrawal check").toBeDefined();
    expect(stranger, "no stranger-withdrawal check").toBeDefined();

    const selector = (d: string) => d.match(/0x[0-9a-f]{8}/)?.[0];
    const a = selector(owner!.detail);
    const b = selector(stranger!.detail);

    expect(a, "owner refusal carries no selector").toBeDefined();
    expect(b, "stranger refusal carries no selector").toBeDefined();
    expect(a).not.toBe(b);
  });
});
