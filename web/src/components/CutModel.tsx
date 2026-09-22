import { useId } from "react";
import { useInView, useMotionAllowed } from "./Motion";

/**
 * The body in section.
 *
 * `brand/physics.md` gives the body a core and a shell. This opens
 * it: an architectural cut through the same object, showing what sits
 * between the outside and the middle. It is the "how it works" diagram
 * drawn in the language of the thing rather than as boxes and arrows.
 *
 * Four layers, outermost first, each one a real stage a loan passes
 * through:
 *
 *   SHELL       the agent and its identity
 *   REPUTATION  the demonstrated history that opens the facility
 *   ESCROW      capital held in the credit account, not yet released
 *   CORE        the lender's cash
 *
 * Every ring's THICKNESS is the figure it stands for, scaled against the
 * book. A diagram whose proportions are decorative would be the thing
 * `docs/11` forbids: a picture asserting quantities nothing measured.
 * Where a figure is missing the ring is drawn hollow, labelled, and
 * carries no width claim.
 */

export interface Layer {
  key: string;
  label: string;
  /** What the reader should take from it. */
  note: string;
  /** Base units this layer holds, or null when nothing measured it. */
  amount: bigint | null;
  /** Which register it belongs to. */
  register: "machine" | "growth" | "alarm" | "quiet";
}

const W = 460;
const H = 300;
const CX = W / 2;
const CY = H / 2 + 6;
/** The outermost radius of the cut. */
const R = 122;
/** The core never shrinks below this, or it stops reading as a centre. */
const CORE_MIN = 26;

const HUES: Record<Layer["register"], string> = {
  machine: "var(--machine)",
  growth: "var(--growth)",
  alarm: "var(--alarm)",
  quiet: "var(--rule)",
};

export function CutModel({ layers }: { layers: Layer[] }) {
  const motion = useMotionAllowed();
  const { ref, seen } = useInView<HTMLDivElement>();
  const id = useId();

  if (layers.length === 0) return null;

  // Rings are sized by their share of the total, so the section is a
  // measurement rather than an illustration. Layers with no figure take
  // a fixed thin band and are marked as unmeasured.
  const measured = layers.filter((l) => l.amount !== null);
  const total = measured.reduce((a, l) => a + (l.amount ?? 0n), 0n);

  const span = R - CORE_MIN;
  const unmeasuredBand = 14;
  const unmeasuredCount = layers.length - measured.length;
  const measuredSpan = Math.max(20, span - unmeasuredCount * unmeasuredBand);

  // Walk from the outside in, accumulating radii.
  let r = R;
  const rings = layers.map((l) => {
    const outer = r;
    const width =
      l.amount === null || total === 0n
        ? unmeasuredBand
        : Math.max(
            7,
            (Number((l.amount * 10_000n) / total) / 10_000) * measuredSpan,
          );
    r = Math.max(CORE_MIN, outer - width);
    return { ...l, outer, inner: r };
  });

  return (
    <div ref={ref} className={`cut ${seen ? "in" : ""}`}>
      <svg viewBox={`0 0 ${W} ${H}`} role="img"
        aria-label={
          "A section through the credit body: " +
          rings.map((l) => l.label).join(", then ") + ", at the centre."
        }
      >
        <defs>
          {rings.map((l, n) => (
            <radialGradient
              key={l.key}
              id={`${id}-${n}`}
              cx="38%"
              cy="30%"
              r="80%"
            >
              <stop offset="0%" stopColor={HUES[l.register]} stopOpacity="0.3" />
              <stop offset="100%" stopColor={HUES[l.register]} stopOpacity="0.08" />
            </radialGradient>
          ))}
        </defs>

        {rings.map((l, n) => (
          <g
            key={l.key}
            className="cut-ring"
            style={{ animationDelay: motion ? `${n * 140}ms` : "0ms" }}
          >
            {/* The band. Drawn as a thick stroke on the mid-radius, so
                one element carries both edges. */}
            <circle
              cx={CX}
              cy={CY}
              r={(l.outer + l.inner) / 2}
              fill="none"
              stroke={`url(#${id}-${n})`}
              strokeWidth={Math.max(1, l.outer - l.inner)}
            />
            <circle
              cx={CX}
              cy={CY}
              r={l.outer}
              fill="none"
              stroke={HUES[l.register]}
              strokeWidth="0.75"
              strokeOpacity={l.amount === null ? 0.3 : 0.55}
              strokeDasharray={l.amount === null ? "2 5" : "none"}
            />

            {/* A leader out to the label: the cut stays clean and the
                naming happens in the margin. */}
            <line
              x1={CX + (l.outer + l.inner) / 2}
              y1={CY}
              x2={CX + R + 26}
              y2={CY - 78 + n * 38}
              stroke="var(--border)"
              strokeWidth="0.75"
            />
            <text
              x={CX + R + 32}
              y={CY - 78 + n * 38}
              className="cut-label"
              dominantBaseline="middle"
            >
              {l.label}
            </text>
            <text
              x={CX + R + 32}
              y={CY - 78 + n * 38 + 13}
              className="cut-note"
              dominantBaseline="middle"
            >
              {l.note}
            </text>
          </g>
        ))}

        {/* The core. */}
        <circle
          cx={CX}
          cy={CY}
          r={CORE_MIN}
          fill={HUES.machine}
          fillOpacity="0.16"
          stroke={HUES.machine}
          strokeWidth="0.75"
          strokeOpacity="0.5"
          className="cut-core"
        />
      </svg>
    </div>
  );
}
