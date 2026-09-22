import { useInView, useMotionAllowed } from "./Motion";

/**
 * The thesis, drawn.
 *
 * Three loans under one policy and one model: the limit rises, the rate
 * falls, and at some point the constraint that BOUNDS the limit stops
 * being history and becomes portfolio stress. `docs/09` calls that
 * switch the half of beat 4b that carries the argument, and it had been
 * a word in a table cell.
 *
 * The line draws left to right because that is the order the loans
 * happened in. The switch is marked where it occurs rather than
 * captioned underneath, because the point is WHEN it changed.
 *
 * Every coordinate comes from the loaded assessments. An axis drawn to a
 * round number would flatter the curve; the scale here is the data's own
 * minimum and maximum, so the shape is the shape.
 */

export interface CurvePoint {
  /** Agent capacity in base units, as the assessment recorded it. */
  capacity: string;
  /** Offered APR in basis points. */
  apr: number;
  /** Which constraint produced the capacity. */
  binding: string;
}

/** Geometry. A viewBox rather than pixels, so it scales with the card. */
const W = 560;
const H = 190;
// Generous right pad: the last point carries an APR label above it and
// a switch marker beside it, and both clipped the viewBox at 18.
const PAD = { top: 26, right: 52, bottom: 34, left: 46 };

export function CapacityCurve({ points }: { points: CurvePoint[] }) {
  const motion = useMotionAllowed();
  const { ref, seen } = useInView<HTMLDivElement>();

  if (points.length < 2) return null;

  const caps = points.map((p) => Number(BigInt(p.capacity) / 1_000_000n));
  const aprs = points.map((p) => p.apr / 100);

  const lo = Math.min(...caps);
  const hi = Math.max(...caps);
  // A flat series would divide by zero. It would also mean the limit
  // never rose, which is a story worth not drawing at all.
  const span = hi - lo || 1;

  const innerW = W - PAD.left - PAD.right;
  const innerH = H - PAD.top - PAD.bottom;

  const x = (i: number) => PAD.left + (innerW * i) / (points.length - 1);
  const y = (v: number) => PAD.top + innerH - (innerH * (v - lo)) / span;

  const line = points
    .map((_, i) => `${i === 0 ? "M" : "L"} ${x(i)} ${y(caps[i]!)}`)
    .join(" ");

  // The area under the line, closed along the baseline. Fill rather
  // than a second stroke: the quantity is what grew, not its outline.
  const area =
    `M ${x(0)} ${PAD.top + innerH} ` +
    points.map((_, i) => `L ${x(i)} ${y(caps[i]!)}`).join(" ") +
    ` L ${x(points.length - 1)} ${PAD.top + innerH} Z`;

  // Where the binding constraint changes hands, if it does.
  const switchAt = points.findIndex(
    (p, i) => i > 0 && p.binding !== points[i - 1]!.binding,
  );

  return (
    <div ref={ref} className="curve">
      <svg
        viewBox={`0 0 ${W} ${H}`}
        role="img"
        aria-label={
          `Agent capacity across ${points.length} loans, rising from ` +
          `${caps[0]!.toLocaleString()} to ${caps[caps.length - 1]!.toLocaleString()} ` +
          `settlement units while the rate falls from ${aprs[0]}% to ` +
          `${aprs[aprs.length - 1]}%.`
        }
      >
        <defs>
          <linearGradient id="capfill" x1="0" y1="0" x2="0" y2="1">
            <stop offset="0%" stopColor="var(--growth)" stopOpacity="0.22" />
            <stop offset="100%" stopColor="var(--growth)" stopOpacity="0" />
          </linearGradient>
        </defs>

        {/* Baseline, so the fill reads as height above something. */}
        <line
          x1={PAD.left}
          y1={PAD.top + innerH}
          x2={W - PAD.right}
          y2={PAD.top + innerH}
          stroke="var(--border)"
        />

        <path d={area} fill="url(#capfill)" className={seen ? "in" : ""} />
        <path
          d={line}
          fill="none"
          stroke="var(--growth)"
          strokeWidth="2"
          strokeLinecap="round"
          strokeLinejoin="round"
          className={`curve-line ${seen && motion ? "draw" : ""}`}
        />

        {points.map((p, i) => (
          <g key={i} className={seen ? "curve-point in" : "curve-point"}
             style={{ animationDelay: `${420 + i * 130}ms` }}>
            <circle
              cx={x(i)}
              cy={y(caps[i]!)}
              r={i === switchAt ? 5.5 : 4}
              fill="var(--bg)"
              stroke={i === switchAt ? "var(--machine)" : "var(--growth)"}
              strokeWidth={i === switchAt ? 2.5 : 2}
            />
            {/* The rate, above its own point. Two figures per loan is
                the whole claim: more limit AND a lower price. */}
            <text
              x={x(i)}
              y={y(caps[i]!) - 12}
              textAnchor="middle"
              className="curve-apr"
            >
              {aprs[i]!.toFixed(2)}%
            </text>
            <text
              x={x(i)}
              y={PAD.top + innerH + 20}
              textAnchor="middle"
              className="curve-axis"
            >
              loan {i + 1}
            </text>
          </g>
        ))}

        {/* The switch, marked where it happens. */}
        {switchAt > 0 && (
          <g className={seen ? "curve-switch in" : "curve-switch"}>
            <line
              x1={x(switchAt)}
              y1={PAD.top}
              x2={x(switchAt)}
              y2={PAD.top + innerH}
              stroke="var(--machine)"
              strokeWidth="1"
              strokeDasharray="3 4"
            />
            {/* Below the curve, not across it. At the top the label
                crossed the line it is annotating, which is the one
                place a caption must not be. */}
            <text
              x={x(switchAt) - 10}
              y={PAD.top + innerH - 8}
              textAnchor="end"
              className="curve-switch-label"
            >
              stress binds
            </text>
          </g>
        )}
      </svg>

      <p className="curve-caption">
        One policy, one model hash. Only the demonstrated history changes.
        {switchAt > 0 && (
          <>
            {" "}
            At loan {switchAt + 1} the binding constraint stops being{" "}
            <code>{points[switchAt - 1]!.binding}</code> and becomes{" "}
            <code>{points[switchAt]!.binding}</code> — history opens the
            facility until portfolio risk becomes the limit.
          </>
        )}
      </p>
    </div>
  );
}
