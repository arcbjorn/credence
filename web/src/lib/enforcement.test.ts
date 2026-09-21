import { readFileSync, existsSync } from "node:fs";
import { resolve } from "node:path";
import { fixtureMissing } from "./fixtures";
import { describe, expect, it } from "vitest";
import { heldDecimals, units, type Check } from "./data";

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

describe.skipIf(fixtureMissing(REPAY))("beat 6, as recorded on chain", () => {
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

/**
 * `docs/05` keeps the vault's ledgers separate, and `docs/09` asks the
 * lender screen to show them that way.
 *
 * The rule that matters: **uncollected interest is never withdrawable
 * cash.** A single "vault value" figure would merge a receivable with
 * settled cash and overstate what a lender could actually take, which is
 * the one direction an accounting error must never go.
 */
describe.skipIf(fixtureMissing(REPAY))("the lender ledgers", () => {
  const checks = (): Check[] =>
    JSON.parse(readFileSync(REPAY, "utf8")).checks ?? [];

  it("records the epoch ledgers from chain", () => {
    const ledger = checks().filter((c) => c.beat === "lender");
    const names = ledger.map((c) => c.name).join(" ");
    expect(names).toContain("idle cash");
    expect(names).toContain("principal outstanding");
    expect(names).toContain("realized loss");
  });

  it("reports each ledger as its own figure, never a total", () => {
    const ledger = checks().filter((c) => c.beat === "lender");
    const values = ledger.map((c) => c.detail);
    // Each carries its own base-unit reading.
    for (const v of values) {
      expect(v).toMatch(/^\d+ base units$/);
    }
    // And they are distinct entries, not one merged number.
    expect(new Set(ledger.map((c) => c.name)).size).toBe(ledger.length);
  });

  it("does not treat a zero realized loss as a failure", () => {
    // Zero is the correct value for a clean epoch. Marking it failed
    // would misreport a healthy run as a broken one.
    const loss = checks().find((c) => c.name.includes("realized loss"));
    expect(loss).toBeDefined();
    expect(loss!.passed).toBe(true);
  });
});

describe("the held figure is scaled by the asset, not by a guess", () => {
  // `credence-verify` reads the SETTLEMENT balance first and only falls
  // back to the risky token when settlement is zero. Settlement is 6
  // decimals, risky is 18, and the enforcement screen hardcoded 18 --
  // so in the common case the headline figure was understated by 10^12.
  //
  // It was masked because the committed fixture happens to hold the
  // risky token, which is the branch the hardcoded value got right.
  it("reads 6 decimals for the settlement token", () => {
    const detail = "9993569855 base units of 0xabc at 6 decimals";
    expect(heldDecimals(detail)).toBe(6);
    expect(units(detail.split(" ")[0], heldDecimals(detail))).toBe("9,993.569855");
  });

  it("reads 18 decimals for the risky asset", () => {
    const detail = "6993569855000000000 base units of 0xdef at 18 decimals";
    expect(heldDecimals(detail)).toBe(18);
    expect(units(detail.split(" ")[0], heldDecimals(detail))).toBe("6.993569855");
  });

  // An older record predates the decimals suffix. Falling back to 6
  // rather than 18 matches the verifier's own first branch, so the
  // likelier denomination wins.
  it("falls back to settlement scale on an older record", () => {
    expect(heldDecimals("123 base units of 0xabc")).toBe(6);
  });
});
