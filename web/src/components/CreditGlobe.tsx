import { useEffect, useId, useRef, useState } from "react";
import { bindingPhrase } from "../lib/bindings";
import { useInView, useMotionAllowed } from "./Motion";
import {
  arcPoints,
  latitudeLine,
  meridianLine,
  onSphere,
  project,
  rotate,
  toPath,
  type Vec3,
} from "./Sphere";

/**
 * The credit body.
 *
 * A real sphere: points are placed on a unit sphere, rotated and
 * projected, so the wireframe foreshortens at the limb and anything on
 * the far side is occluded rather than drawn over the front. It is not a
 * map of the Earth -- there are no continents, because this is not a
 * place. It is one agent's credit, and every part of it is a figure:
 *
 *   capacity      radius of the body
 *   score         how bright the shell burns, moss -> teal
 *   APR           thickness of the terminator ring
 *   each loan     a point of light on the shell, with an arc to the next
 *   the limit     a ring the body grows toward and then presses against
 *   a refusal     the shell goes dark and the limit ring turns vermilion
 *
 * The one that carries the thesis is the limit ring. While the agent's
 * own record is what bounds it the ring is dotted and stands off the
 * body -- a soft edge, because more history moves it. When stress takes
 * over it goes solid and the body is pressed against it: a limit that
 * behaving well does not move. `docs/09` calls that handover the half of
 * beat 4b that carries the argument.
 *
 * Every figure is passed in from a loaded assessment. The scale is the
 * series' own maximum, so the shape is the data's, and capacity maps to
 * the body's AREA rather than its radius -- the eye reads a disc by how
 * much ink it covers, and radius-mapping would show a 31% rise as ~70%
 * more object.
 */

export interface GlobePoint {
  /** Agent capacity in base units, as the assessment recorded it. */
  capacity: string;
  /** Offered APR in basis points. */
  apr: number;
  /** Which constraint produced the capacity. */
  binding: string;
  /** The behavioural score behind it, if the assessment carried one. */
  score?: number;
}

const SIZE = 360;
const CENTER = SIZE / 2;
/** Where the limit ring sits; the body is scaled inside it. */
const LIMIT_R = 136;
const TILT = 16;

/** Latitudes and meridians of the wireframe. */
const LATS = [-60, -30, 0, 30, 60];
const MERIDIANS = [0, 30, 60, 90, 120, 150];

/** Where each loan sits on the shell. Fixed, so a loan keeps its place. */
/**
 * Sites are kept within about 45 degrees of each other so the arcs
 * between them read as spans across the near face. Antipodal sites
 * produce an arc that spends most of its length behind the body, which
 * looks like a line vanishing for no reason.
 */
const LOAN_SITES: [number, number][] = [
  [18, -34],
  [-8, 4],
  [30, 40],
  [-20, 74],
  [6, 108],
];

function bindingIsStress(binding: string): boolean {
  return binding === "StressCapacity";
}

export function CreditGlobe({
  points,
  peakOverride,
  still = false,
}: {
  points: GlobePoint[];
  /** Scale against a figure outside this series, so several bodies drawn
   *  side by side stay comparable. */
  peakOverride?: number;
  /** A single state: no stepper, no walk-through. */
  still?: boolean;
}) {
  const motion = useMotionAllowed();
  const { ref, seen } = useInView<HTMLDivElement>();
  const [step, setStep] = useState(0);
  const [spin, setSpin] = useState(0);
  const gradientId = `globe-${useId()}`;
  const frame = useRef(0);

  const last = points.length - 1;

  // The body turns. Slow -- a full revolution takes about 40 seconds --
  // because this is a thing being examined, not an animation playing.
  useEffect(() => {
    if (!seen || !motion) return;
    let started = performance.now();
    const tick = (now: number) => {
      const dt = now - started;
      started = now;
      setSpin((s) => (s + dt * 0.009) % 360);
      frame.current = requestAnimationFrame(tick);
    };
    frame.current = requestAnimationFrame(tick);
    return () => cancelAnimationFrame(frame.current);
  }, [seen, motion]);

  // Walk the loans once, then settle: the final state is the claim.
  useEffect(() => {
    if (!seen || still) return;
    if (!motion) {
      setStep(last);
      return;
    }
    let i = 0;
    setStep(0);
    const timer = setInterval(() => {
      i += 1;
      setStep(i);
      if (i >= last) clearInterval(timer);
    }, 1800);
    return () => clearInterval(timer);
  }, [seen, motion, last, still]);

  if (points.length === 0) return null;

  const caps = points.map((p) => Number(BigInt(p.capacity) / 1_000_000n));
  const peak = peakOverride ?? Math.max(...caps);

  const i = Math.min(step, last);
  const current = points[i]!;
  const cap = caps[i]!;
  const stress = bindingIsStress(current.binding);
  const denied = cap === 0;

  // Area-proportional, so the ink matches the figure.
  const bodyR = peak > 0 ? Math.sqrt(cap / peak) * LIMIT_R : 0;

  const score = current.score ?? 0;
  const warmth = Math.max(0, Math.min(1, (score - 560) / 200));
  const hue = denied
    ? "var(--alarm)"
    : warmth > 0.5
      ? "var(--machine)"
      : "var(--growth)";

  // The rate, as the weight of the body's own edge. Kept to a hairline
  // range: a 6px stroke at full saturation read as a cyan hoop stuck
  // around a sphere -- clip art, not an object. A premium body gets its
  // form from LIGHT, so the edge is barely there and the rim light
  // below does the work.
  const rateWeight = denied ? 1 : Math.max(0.6, (current.apr / 1200) * 1.5);

  // The wireframe, at the body's current size.
  const lats = LATS.map((l) => latitudeLine(l, spin, TILT, CENTER, CENTER, bodyR));
  const mers = MERIDIANS.map((m) =>
    meridianLine(m, spin, TILT, CENTER, CENTER, bodyR),
  );

  // One node per loan so far, and an arc from each to the next: the
  // history this agent has accumulated, on the shell that it bought.
  const shown = points.slice(0, i + 1);
  const sites: Vec3[] = shown.map((_, n) => {
    const [lat, lon] = LOAN_SITES[n % LOAN_SITES.length]!;
    return onSphere(lat, lon);
  });
  const nodes = sites.map((v) =>
    project(rotate(v, spin, TILT), CENTER, CENTER, bodyR),
  );

  return (
    <div ref={ref} className="globe">
      <svg
        viewBox={`0 0 ${SIZE} ${SIZE}`}
        role="img"
        aria-label={
          `Loan ${i + 1} of ${points.length}: capacity ` +
          `${cap.toLocaleString()} settlement units at ` +
          `${(current.apr / 100).toFixed(2)} percent, limited by ` +
          `${bindingPhrase(current.binding)}.`
        }
      >
        <defs>
          {/* The body's own surface, lit from upper left. The light
              falls off toward the terminator rather than ending at a
              stroke, which is what makes it read as a sphere. */}
          <radialGradient id={gradientId} cx="34%" cy="28%" r="78%">
            <stop offset="0%" stopColor={hue} stopOpacity={denied ? 0.05 : 0.26} />
            <stop offset="55%" stopColor={hue} stopOpacity={denied ? 0.02 : 0.1} />
            <stop offset="100%" stopColor={hue} stopOpacity={denied ? 0 : 0.02} />
          </radialGradient>

          {/* Atmosphere: brightest exactly at the limb and gone a few
              pixels in. This is the whole trick -- an object with a
              rim light looks lit, an object with an outline looks
              drawn. */}
          <radialGradient id={`${gradientId}-rim`} cx="50%" cy="50%" r="50%">
            <stop offset="88%" stopColor={hue} stopOpacity="0" />
            <stop offset="97%" stopColor={hue} stopOpacity={denied ? 0.22 : 0.42} />
            <stop offset="100%" stopColor={hue} stopOpacity="0" />
          </radialGradient>

          {/* A soft shadow under the body, so it sits in space rather
              than floating on the page. */}
          <radialGradient id={`${gradientId}-shadow`} cx="50%" cy="50%" r="50%">
            <stop offset="0%" stopColor="#000" stopOpacity="0.5" />
            <stop offset="100%" stopColor="#000" stopOpacity="0" />
          </radialGradient>
        </defs>

        {/* The body sits in space rather than on the page. */}
        {bodyR > 0 && (
          <ellipse
            cx={CENTER}
            cy={CENTER + bodyR * 0.96}
            rx={bodyR * 0.78}
            ry={bodyR * 0.14}
            fill={`url(#${gradientId}-shadow)`}
          />
        )}

        {/* The limit. Dotted while history bounds it; solid once stress
            does, because that edge does not move by behaving well. */}
        <circle
          cx={CENTER}
          cy={CENTER}
          r={LIMIT_R}
          fill="none"
          stroke={
            denied ? "var(--alarm)" : stress ? "var(--machine)" : "var(--border)"
          }
          strokeWidth={denied || stress ? 1.5 : 1}
          strokeDasharray={denied || stress ? "none" : "2 7"}
          className="globe-limit"
        />

        {/* The body. */}
        <circle
          cx={CENTER}
          cy={CENTER}
          r={bodyR}
          fill={`url(#${gradientId})`}
          className="globe-shell"
        />

        {/* Far side of the wireframe, seen through the body. */}
        <g stroke={hue} fill="none" strokeWidth="0.4" opacity="0.12">
          {lats.map((l, n) => (
            <path key={`latf${n}`} d={l.far} />
          ))}
          {mers.map((m, n) => (
            <path key={`merf${n}`} d={m.far} />
          ))}
        </g>

        {/* Near side. */}
        <g stroke={hue} fill="none" strokeWidth="0.5" opacity="0.26">
          {lats.map((l, n) => (
            <path key={`latn${n}`} d={l.near} />
          ))}
          {mers.map((m, n) => (
            <path key={`mern${n}`} d={m.near} />
          ))}
        </g>

        {/* The terminator: the body's own edge, weighted by the rate. */}
        <circle
          cx={CENTER}
          cy={CENTER}
          r={bodyR}
          fill="none"
          stroke={hue}
          strokeWidth={rateWeight}
          strokeOpacity={denied ? 0.4 : 0.55}
          className="globe-edge"
        />

        {/* The atmosphere, over the shell and under the nodes. */}
        {bodyR > 0 && (
          <circle
            cx={CENTER}
            cy={CENTER}
            r={bodyR}
            fill={`url(#${gradientId}-rim)`}
            className="globe-rim"
          />
        )}

        {/* Loans, as arcs between the points they landed on. An arc on
            the far side is drawn faintly rather than over the body. */}
        {sites.slice(0, -1).map((from, n) => {
          const to = sites[n + 1]!;
          const pts = arcPoints(
            from,
            to,
            spin,
            TILT,
            CENTER,
            CENTER,
            bodyR,
          );
          const behind = pts.every((p) => p.z < 0);
          return (
            <path
              key={`arc${n}`}
              d={toPath(pts)}
              fill="none"
              stroke={hue}
              strokeWidth="1.2"
              strokeOpacity={behind ? 0.16 : 0.7}
              className="globe-arc"
            />
          );
        })}

        {/* Each loan, a point of light. Dimmed when it swings behind. */}
        {nodes.map((p, n) => (
          <circle
            key={`node${n}`}
            cx={p.x}
            cy={p.y}
            r={n === i ? 3.4 : 2.2}
            fill={hue}
            fillOpacity={p.z < 0 ? 0.22 : 1}
            className="globe-node"
          />
        ))}

        {/* The figure, over the body. */}
        <text x={CENTER} y={CENTER - 2} textAnchor="middle" className="globe-figure">
          {denied ? "denied" : `${(current.apr / 100).toFixed(2)}%`}
        </text>
        <text x={CENTER} y={CENTER + 18} textAnchor="middle" className="globe-sub">
          loan {i + 1}
        </text>
      </svg>

      <p
        className={
          `globe-caption ${denied ? "globe-caption-denied" : ""} ` +
          `${!denied && stress ? "globe-caption-stress" : ""}`
        }
      >
        <span className="globe-caption-label">Limited by</span>
        <span title={current.binding}>{bindingPhrase(current.binding)}</span>
      </p>

      {points.length > 1 && (
        <div className="globe-steps" role="group" aria-label="Choose a loan">
          {points.map((p, n) => (
            <button
              key={n}
              type="button"
              className={`globe-step ${n === i ? "on" : ""} ${
                bindingIsStress(p.binding) ? "globe-step-stress" : ""
              }`}
              aria-label={`Loan ${n + 1}`}
              aria-pressed={n === i}
              onClick={() => setStep(n)}
            />
          ))}
        </div>
      )}
    </div>
  );
}
