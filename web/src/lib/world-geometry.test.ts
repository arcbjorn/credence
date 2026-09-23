import { describe, expect, it } from "vitest";
import { loanArc, rotatePoint, sphereOccludes, spherePoint, traceVisiblePath } from "./world-geometry";

describe("loan paths on a rotating body", () => {
  const lender = spherePoint(-22, 60), borrower = spherePoint(24, -36);

  it("anchors both ends to their coordinates throughout a full revolution", () => {
    const arc = loanArc(lender, borrower, .4);
    for (let angle = 0; angle <= Math.PI * 2; angle += Math.PI / 12) {
      expect(rotatePoint(arc[0]!, angle)).toEqual(rotatePoint(lender, angle));
      expect(rotatePoint(arc.at(-1)!, angle)).toEqual(rotatePoint(borrower, angle));
    }
  });

  it("raises the path outside the shell, with higher paths for longer terms", () => {
    const short = loanArc(lender, borrower, .2), long = loanArc(lender, borrower, .6);
    short.forEach(p => expect(Math.hypot(p.x, p.y, p.z)).toBeGreaterThanOrEqual(1 - 1e-10));
    const apex = (path: typeof short) => Math.max(...path.map(p => Math.hypot(p.x, p.y, p.z)));
    expect(apex(short)).toBeCloseTo(1.2);
    expect(apex(long)).toBeCloseTo(1.6);
  });

  it("hides the far-side arc behind the shell instead of drawing through it", () => {
    const arc = loanArc(spherePoint(0, -15), spherePoint(0, 15), .2);
    expect(arc.every(p => !sphereOccludes(rotatePoint(p, 0, 0)))).toBe(true);
    expect(arc.every(p => sphereOccludes(rotatePoint(p, Math.PI, 0)))).toBe(true);
  });

  it("preserves a high arc beyond the silhouette even when it is on the far side", () => {
    expect(sphereOccludes({ x: 1.1, y: 0, z: -.3 })).toBe(false);
    expect(sphereOccludes({ x: .3, y: 0, z: -.3 })).toBe(true);
    expect(sphereOccludes({ x: .3, y: 0, z: 1.1 })).toBe(false);
  });

  it.each([spherePoint(-22, 60), spherePoint(22, -120)])("handles coincident or antipodal endpoints without invalid coordinates", endpoint => {
    for (const p of loanArc(lender, endpoint, .3)) {
      expect([p.x, p.y, p.z].every(Number.isFinite)).toBe(true);
    }
  });

  it("starts a new stroke after a hidden stretch instead of joining across the body", () => {
    const commands: string[] = [];
    const path = { moveTo: (x: number) => commands.push(`move ${x}`), lineTo: (x: number) => commands.push(`line ${x}`) };
    traceVisiblePath(path, [false, true, true, false, true, true, false].map((visible, x) => ({ x, y: 0, visible })));
    expect(commands).toEqual(["move 1", "line 2", "move 4", "line 5"]);
  });
});
