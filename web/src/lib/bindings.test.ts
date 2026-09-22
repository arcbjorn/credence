import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";
import { BINDINGS, bindingPhrase, isKnownBinding } from "./bindings";

describe("bindingPhrase", () => {
  it("renders every engine variant as words, not an identifier", () => {
    for (const binding of BINDINGS) {
      const phrase = bindingPhrase(binding);
      expect(phrase).not.toBe(binding);
      // No PascalCase run survives: that is the shape being removed.
      expect(phrase).not.toMatch(/[a-z][A-Z]/);
    }
  });

  it("passes an unknown variant through untranslated", () => {
    // Visibly untranslated beats silently "unknown": it shows the
    // interface is behind the engine rather than hiding it.
    expect(bindingPhrase("SomeNewCap")).toBe("SomeNewCap");
    expect(isKnownBinding("SomeNewCap")).toBe(false);
  });

  it("renders an absent binding as a dash", () => {
    expect(bindingPhrase(undefined)).toBe("—");
    expect(bindingPhrase(null)).toBe("—");
    expect(bindingPhrase("")).toBe("—");
  });
});

describe("coverage of the engine enum", () => {
  /**
   * The list here must match `BindingConstraint` in the engine. If a
   * variant is added there and not here, it renders as a raw identifier
   * on every screen -- exactly the defect this module exists to fix --
   * and nothing else would catch it.
   */
  it("names every variant the Rust enum declares", () => {
    // Resolved from the vitest root rather than `import.meta.url`: under
    // jsdom that URL has an http scheme and `readFileSync` rejects it,
    // which would fail this test for a reason unrelated to drift.
    const source = readFileSync(
      resolve(process.cwd(), "../risk/crates/risk-core/src/capacity.rs"),
      "utf8",
    );
    const block = source.match(
      /pub enum BindingConstraint \{([^}]*)\}/,
    )?.[1];
    expect(block, "BindingConstraint enum not found in capacity.rs").toBeDefined();

    const variants = [...block!.matchAll(/^\s*([A-Z]\w*),/gm)].map(
      (m) => m[1]!,
    );
    expect(variants.length).toBeGreaterThan(0);
    expect([...variants].sort()).toEqual([...BINDINGS].sort());
  });
});
