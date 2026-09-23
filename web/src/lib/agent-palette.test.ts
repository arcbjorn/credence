import { describe, expect, it } from "vitest";
import { AGENT_COLORS, agentColor } from "./world-palette";

/**
 * The per-agent pastels, checked as contrast rather than by eye.
 *
 * Every loan used to draw in one jade, so a book of five read as a single
 * line. Colour is now what separates one agent's arcs from another's,
 * which makes "is it actually visible" a correctness question: a hue that
 * disappears into the ground it is drawn on conveys nothing, and a light
 * set reused on the dark globe does exactly that.
 *
 * The grounds are the real ones -- `--bg` on paper and the darkest shell
 * stop at night -- taken from `app.css` and `world-palette.ts`.
 */
const PAPER = "#f9f5ef";
const NIGHT = "#080e0f";

const rgb = (hex: string): [number, number, number] => {
  const h = hex.replace("#", "");
  return [0, 2, 4].map(i => parseInt(h.slice(i, i + 2), 16)) as [number, number, number];
};

/** Relative luminance, per WCAG. */
const luminance = (hex: string): number => {
  const [r, g, b] = rgb(hex).map(v => {
    const c = v / 255;
    return c <= 0.03928 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4;
  }) as [number, number, number];
  return 0.2126 * r + 0.7152 * g + 0.0722 * b;
};

const contrast = (a: string, b: string): number => {
  const [x, y] = [luminance(a), luminance(b)];
  return (Math.max(x, y) + 0.05) / (Math.min(x, y) + 0.05);
};

/** Distance in RGB space, for telling two hues apart. */
const distance = (a: string, b: string): number => {
  const [x, y] = [rgb(a), rgb(b)];
  return Math.hypot(x[0] - y[0], x[1] - y[1], x[2] - y[2]);
};

const light = Array.from({ length: AGENT_COLORS }, (_, i) => agentColor(i, false));
const dark = Array.from({ length: AGENT_COLORS }, (_, i) => agentColor(i, true));

describe("the agent pastels", () => {
  it("offers a hue for every agent the globe shows", () => {
    expect(AGENT_COLORS).toBeGreaterThanOrEqual(8);
    expect(new Set(light).size).toBe(AGENT_COLORS);
    expect(new Set(dark).size).toBe(AGENT_COLORS);
  });

  it("stays legible on warm paper", () => {
    // 3:1 is the WCAG floor for a non-text graphic. These are hairlines
    // and 2.6px dots, so anything below it is decoration a reader cannot
    // actually follow.
    for (const [i, hex] of light.entries()) {
      expect(contrast(hex, PAPER), `light ${i} (${hex}) on paper`).toBeGreaterThanOrEqual(3);
    }
  });

  it("stays legible on the night globe", () => {
    for (const [i, hex] of dark.entries()) {
      expect(contrast(hex, NIGHT), `dark ${i} (${hex}) on night`).toBeGreaterThanOrEqual(3);
    }
  });

  it("uses a different set for each theme rather than one set dimmed", () => {
    // The failure this prevents: shipping one palette for both, which
    // leaves either the paper or the night view with arcs nobody can see.
    for (let i = 0; i < AGENT_COLORS; i++) {
      expect(light[i]).not.toBe(dark[i]);
    }
  });

  it("keeps EVERY pair of agents distinguishable", () => {
    // Checked across all pairs, not just neighbours. Only adjacent
    // indices were compared at first, which let an amber at index 0 sit
    // 26 units from an ochre at index 4 -- and with five agents drawn,
    // those were exactly the two on screen together.
    //
    // 45 rather than something larger because the floor is not free:
    // 3:1 contrast on near-white paper forces every hue dark, which
    // compresses the distances between them. This is the widest
    // separation that survives that constraint across eight hues.
    for (const set of [light, dark]) {
      for (let i = 0; i < set.length; i++) {
        for (let j = i + 1; j < set.length; j++) {
          expect(
            distance(set[i]!, set[j]!),
            `${set[i]} (${i}) vs ${set[j]} (${j})`,
          ).toBeGreaterThan(45);
        }
      }
    }
  });

  it("wraps instead of running out", () => {
    expect(agentColor(AGENT_COLORS, false)).toBe(agentColor(0, false));
    expect(agentColor(AGENT_COLORS * 3 + 2, true)).toBe(agentColor(2, true));
  });

  it("survives a negative index without returning undefined", () => {
    expect(agentColor(-1, false)).toMatch(/^#[0-9a-f]{6}$/);
    expect(agentColor(-9, true)).toMatch(/^#[0-9a-f]{6}$/);
  });
});
