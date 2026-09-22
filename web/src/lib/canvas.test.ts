import { describe, expect, it } from "vitest";
import { canvasResolution } from "./canvas";

describe("the globe's bounded drawing surface", () => {
  it("keeps a retina drawing surface sharp without following unbounded device density", () => {
    expect(canvasResolution(800, 1)).toBe(800);
    expect(canvasResolution(800, 2)).toBe(1600);
    expect(canvasResolution(800, 4)).toBe(1600);
  });

  it("cannot grow without bound if an unstyled layout reports the bitmap's intrinsic width", () => {
    let width = 800;
    for (let i = 0; i < 100; i++) {
      width = canvasResolution(width, 2);
      expect(width).toBeLessThanOrEqual(2048);
      expect(Number.isInteger(width)).toBe(true);
    }
    expect(canvasResolution(width, 2)).toBe(width);
  });

  it.each([0, -1, NaN, Infinity])("uses a finite drawing surface for an unavailable width of %s", width => {
    expect(canvasResolution(width, 2)).toBe(1600);
  });

  it("does not allocate an empty or oversized bitmap for extreme layout values", () => {
    expect(canvasResolution(0.01, 1)).toBe(1);
    expect(canvasResolution(1_000_000, 2)).toBe(2048);
    expect(canvasResolution(800, Infinity)).toBe(800);
  });
});
