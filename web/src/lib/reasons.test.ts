import { readFileSync, existsSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";
import { reasonFields, reasonText } from "./reasons";

/**
 * The engine's `reasons` are a Rust enum: unit variants serialize as
 * strings, variants with data as `{ Variant: { field: value } }`.
 *
 * The interface typed them as `string[]` and rendered `.join(", ")`, so a
 * denial carrying data displayed as the literal `[object Object]` — on
 * the comparison card whose denial is the whole point of the screen.
 *
 * Reported by review, 15 September.
 */
const ROOT = resolve(process.cwd(), "..");
const FILE = `${ROOT}/web/public/data/comparison.json`;

describe("reason rendering", () => {
  it("renders a unit reason as its own name", () => {
    expect(reasonText("Eligible")).toBe("Eligible");
  });

  it("renders a data-carrying reason with its fields", () => {
    const r = { CapacityExceeded: { capacity: "0", limiting: "StressCapacity" } };
    const out = reasonText(r);
    expect(out).toContain("CapacityExceeded");
    // The constraint still reaches the reader, now in words.
    expect(out).toContain("what it survives a crash");
    // The failure this exists to prevent.
    expect(out).not.toContain("[object Object]");
  });

  it("never returns [object Object] for anything in the shipped fixture", () => {
    if (!existsSync(FILE)) return;
    const cmp = JSON.parse(readFileSync(FILE, "utf8"));
    for (const p of cmp.profiles) {
      for (const r of p.assessment.reasons) {
        expect(reasonText(r), `${p.name} reason`).not.toContain("[object Object]");
      }
    }
  });

  it("shows the denial reason for the concentrated profile", () => {
    if (!existsSync(FILE)) return;
    const cmp = JSON.parse(readFileSync(FILE, "utf8"));
    const c = cmp.profiles.find((p: { name: string }) => p.name === "Concentrated");
    const text = c.assessment.reasons.map(reasonText).join(", ");
    // The screen's entire argument rests on WHY this was refused.
    expect(text).toContain("what it survives a crash");
  });
});

describe("binding constraints inside structured reasons", () => {
  /**
   * `CapacityExceeded` carries the constraint that produced the zero, and
   * it is the denial the comparison screen rests its argument on. The
   * field was rendered raw, so the most important sentence on that screen
   * ended in `StressCapacity`.
   */
  it("renders a limiting constraint as words", () => {
    const r = { CapacityExceeded: { capacity: "0", limiting: "StressCapacity" } };
    const fields = reasonFields(r);
    const limiting = fields.find((f) => f.label === "limiting");
    expect(limiting?.value).toBe("what it survives a crash");
  });

  it("leaves other fields untouched", () => {
    const r = { CapacityExceeded: { capacity: "0", limiting: "StressCapacity" } };
    const capacity = reasonFields(r).find((f) => f.label === "capacity");
    expect(capacity?.value).toBe("0");
  });
});
