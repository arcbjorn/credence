import { describe, expect, it } from "vitest";
import { eclipseCovers, eclipseDistance, eclipseLayout } from "./eclipse";

describe("utilization as an eclipse", () => {
  it.each([0, .01, .08, .4, .61, .91, 1])("covers the measured fraction of the light at %s", utilization => {
    const distance = eclipseDistance(utilization);
    // Integrate the intersection independently, as horizontal slices of the light.
    const samples = 20000;
    let area = 0;
    for (let i = 0; i < samples; i++) {
      const y = -1 + (i + .5) * 2 / samples;
      const halfChord = Math.sqrt(1 - y * y);
      area += Math.max(0, 2 * halfChord - distance) * 2 / samples;
    }
    expect(area / Math.PI).toBeCloseTo(utilization, 5);
  });

  it("keeps both bodies inside the frame at every utilization", () => {
    for (let u = 0; u <= 1; u += .01) {
      const { radius, lightX, shadowX } = eclipseLayout(u);
      expect(lightX - radius).toBeGreaterThanOrEqual(-320 - 1e-9);
      expect(shadowX + radius).toBeLessThanOrEqual(320 + 1e-9);
      expect(lightX + shadowX).toBe(0);
    }
  });

  it("clamps boundary values instead of drawing outside the book", () => {
    expect(eclipseDistance(-.1)).toBe(2);
    expect(eclipseDistance(1.1)).toBe(0);
  });

  it("occludes agents covered by the shadow, preserving exposed agents", () => {
    const { radius, shadowX } = eclipseLayout(.61);
    expect(eclipseCovers(0, 0, shadowX, 0, radius)).toBe(true);
    expect(eclipseCovers(-320, 0, shadowX, 0, radius)).toBe(false);
    expect(eclipseCovers(shadowX, radius + 1, shadowX, 0, radius)).toBe(false);
  });
});
