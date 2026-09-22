import { useId } from "react";
import { useInView, useMotionAllowed } from "./Motion";

/**
 * The eclipse.
 *
 * Two bodies: the lender's capital casts the light, the borrowed
 * position casts the shadow. How far the shadow has travelled across the
 * light IS utilization -- at a lightly used book the two barely touch, at
 * a heavily used one the light is nearly covered.
 *
 * The reason this earns its place next to the numbers: a fund manager
 * reads an overlap in about a second, and reads "utilization 61%" in
 * about five. Same figure, and the geometry is derived from it rather
 * than drawn to look dramatic -- the offset is a direct function of
 * utilization, so the picture cannot flatter the book.
 *
 * With no utilization figure it renders nothing. An eclipse drawn at a
 * guessed offset would be the most confident lie on the page.
 */

const W = 300;
const H = 180;
const R = 58;

export function Eclipse({
  utilization,
  idle,
  outstanding,
}: {
  /** Outstanding over the whole book, in [0,1]. */
  utilization: number | null;
  /** For the readout, in display units. */
  idle?: string;
  outstanding?: string;
}) {
  const motion = useMotionAllowed();
  const { ref, seen } = useInView<SVGSVGElement>();
  const id = useId();

  if (utilization === null) return null;

  const u = Math.max(0, Math.min(1, utilization));

  // Full separation at 0, concentric at 1. The shadow travels the whole
  // diameter, so the overlap area is the thing that moves.
  const offset = (1 - u) * (R * 2);

  const lightX = W / 2 - offset / 2;
  const shadowX = W / 2 + offset / 2;

  return (
    <div className="eclipse">
      <svg
        ref={ref}
        viewBox={`0 0 ${W} ${H}`}
        role="img"
        aria-label={
          `The lender's capital and the borrowed position, overlapping by ` +
          `${Math.round(u * 100)} percent: the book's utilization.`
        }
        className={seen && motion ? "in" : ""}
      >
        <defs>
          <radialGradient id={`${id}-light`} cx="40%" cy="34%" r="72%">
            <stop offset="0%" stopColor="var(--growth)" stopOpacity="0.42" />
            <stop offset="100%" stopColor="var(--growth)" stopOpacity="0.08" />
          </radialGradient>
          {/* The shadow body is a hole, not a colour: it is capital that
              is NOT available, so it takes light away. */}
          <radialGradient id={`${id}-shadow`} cx="60%" cy="40%" r="72%">
            <stop offset="0%" stopColor="#000" stopOpacity="0.88" />
            <stop offset="100%" stopColor="#000" stopOpacity="0.72" />
          </radialGradient>
        </defs>

        {/* Light: the lender's capital. */}
        <circle
          cx={lightX}
          cy={H / 2}
          r={R}
          fill={`url(#${id}-light)`}
          stroke="var(--growth)"
          strokeWidth="0.75"
          strokeOpacity="0.5"
          className="eclipse-light"
        />

        {/* Shadow: what is out on loan. */}
        <circle
          cx={shadowX}
          cy={H / 2}
          r={R}
          fill={`url(#${id}-shadow)`}
          stroke="var(--rule)"
          strokeWidth="0.75"
          strokeOpacity="0.4"
          className="eclipse-shadow"
        />

        <text x={W / 2} y={H - 14} textAnchor="middle" className="eclipse-figure">
          {Math.round(u * 100)}% drawn
        </text>
      </svg>

      <p className="eclipse-legend">
        <span className="eclipse-key eclipse-key-light" />
        {idle ?? "idle"} free
        <span className="eclipse-key eclipse-key-shadow" />
        {outstanding ?? "outstanding"} lent
      </p>
    </div>
  );
}
