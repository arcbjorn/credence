import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";
import { CLAIMS, EARNED, REFUSED, UNEARNED } from "./claims";

/**
 * The landing page states how many claims are earned. `docs/12` is the
 * source of truth for that, and a page asserting "8 of 10" while the
 * document says otherwise would be the exact failure this project spends
 * its credibility avoiding.
 */
const DOC = resolve(process.cwd(), "../docs/12-claims-roadmap.md");

describe("claims ledger", () => {
  it("carries every claim row in docs/12", () => {
    const md = readFileSync(DOC, "utf8");
    // The claims table: rows between the header and the next heading.
    const table = md.slice(
      md.indexOf("| Claim | Required support | Status |"),
      md.indexOf("## Claims the release must not make"),
    );
    const rows = table
      .split("\n")
      .filter((l) => l.startsWith("|"))
      .slice(2); // header and separator
    expect(rows.length).toBe(CLAIMS.length);
  });

  it("counts earned and unearned the way docs/12 does", () => {
    // Whitespace-normalised: the sentence is wrapped across lines in the
    // source, and this test is about the count, not the line breaks.
    const md = readFileSync(DOC, "utf8").replace(/\s+/g, " ");
    expect(md).toContain("Eight of ten are earned");
    expect(EARNED).toBe(8);
    expect(UNEARNED).toBe(2);
    expect(EARNED + UNEARNED).toBe(CLAIMS.length);
  });

  it("marks the two unearned claims as unearned", () => {
    const unearned = CLAIMS.filter((c) => c.status === "unearned");
    expect(unearned.map((c) => c.title)).toEqual([
      "CRE checks an assessment",
      "A note is tradable debt exposure",
    ]);
  });

  it("gives every claim a support line and a detail", () => {
    for (const c of CLAIMS) {
      expect(c.support.length, `${c.title} support`).toBeGreaterThan(0);
      expect(c.detail.length, `${c.title} detail`).toBeGreaterThan(0);
    }
  });

  it("never lists a refused claim as an earned one", () => {
    // A refusal and a claim sharing a title would mean the page both
    // makes and disclaims the same promise.
    const titles = new Set(CLAIMS.map((c) => c.title.toLowerCase()));
    for (const r of REFUSED) {
      expect(titles.has(r.toLowerCase()), `${r} appears as a claim`).toBe(
        false,
      );
    }
  });
});
