import { describe, expect, it } from "vitest";
import { bps, units } from "./data";

describe("units", () => {
  it("renders base units without losing precision", () => {
    expect(units("4000000000")).toBe("4,000");
    expect(units("6575358")).toBe("6.575358");
  });

  it("does not round a fractional amount away", () => {
    // The failure this prevents: a display that shows 0 for a real, small
    // interest payment, which would make a demo look like nothing accrued.
    expect(units("1")).toBe("0.000001");
  });

  it("passes a non-numeric value through untouched", () => {
    expect(units("-")).toBe("-");
    expect(units(undefined)).toBe("-");
  });
});

describe("bps", () => {
  it("renders basis points as a percentage", () => {
    expect(bps(1200)).toBe("12.00%");
    expect(bps(1094)).toBe("10.94%");
  });
});
