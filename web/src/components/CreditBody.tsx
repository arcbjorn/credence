import { useEffect, useId, useState } from "react";
import { bindingPhrase } from "../lib/bindings";
import { useInView, useMotionAllowed } from "./Motion";

/**
 * The object.
 *
 * The rule this follows, and the reason it exists: every product concept
 * maps to a physical property, and anything that does not change the
 * object does not belong on it. A decorative shape beside a number is a
 * picture of a system; a shape whose geometry IS the number is the
 * system. This is the second thing.
 *
 * It is deliberately NOT a globe. A sphere implies a population -- you
 * rotate it because there is more than fits on one face -- and this
 * project has three agents and three loans. Rotating would reveal
 * nothing, and a "longitude = risk" control mapped onto an empty axis is
 * the exact failure the reference warns about: looks empty if the book
 * is small. So the object is the thing this data actually has, which is
 * one agent's capacity being clamped by one constraint at a time.
 *
 * The grammar:
 *
 *   capacity            radius of the filled disc
 *   binding constraint  WHICH ring is the hard edge, drawn solid
 *   score               warmth of the fill, moss (low) toward teal (high)
 *   APR                 thickness of the rate ring
 *   the switch          the outer ring closing IN, not the disc growing
 *
 * That last one carries the thesis. `docs/09` calls the constraint
 * handover the half of beat 4b that matters: at loan 3 the limit stops
 * being set by the agent's record and starts being set by what its book
 * survives in a crash. On a chart that is a change of label. Here it is
 * the ring that touches -- the limit arrives from outside rather than
 * being reached from inside.
 *
 * Every figure is passed in from a loaded assessment. Nothing is
 * invented here, including the scale: the disc is sized against the
 * largest capacity in the series, so the shape is the data's own.
 */

export interface BodyPoint {
  /** Agent capacity in base units, as the assessment recorded it. */
  capacity: string;
  /** Offered APR in basis points. */
  apr: number;
  /** Which constraint produced the capacity. */
  binding: string;
  /** The behavioural score behind it, if the assessment carried one. */
  score?: number;
}

/** Geometry. A square viewBox so the object scales with its column. */
const SIZE = 320;
const CENTER = SIZE / 2;
/** The outer ring sits here; the disc is scaled inside it. */
const LIMIT_R = 122;

/**
 * Which register the binding belongs to.
 *
 * `docs/09` forbids collapsing evidence modes into one indicator, so the
 * constraint keeps its own hue rather than everything becoming one
 * accent. A stress-bound limit is MACHINE: it is the portfolio model
 * speaking, not the agent's record.
 */
function bindingIsStress(binding: string): boolean {
  return binding === "StressCapacity";
}

export function CreditBody({
  points,
  /** Scale the object against a figure outside its own series, so
   *  several bodies drawn side by side are comparable. Without this each
   *  one normalises to its own maximum and a small agent looks as large
   *  as a big one. */
  peakOverride,
  /** A single-state object: no stepper, no autoplay. */
  still = false,
}: {
  points: BodyPoint[];
  peakOverride?: number;
  still?: boolean;
}) {
  const motion = useMotionAllowed();
  const { ref, seen } = useInView<HTMLDivElement>();
  const [step, setStep] = useState(0);
  // Gradients are document-scoped, so several bodies on one page would
  // otherwise all paint with whichever one mounted last. Declared with
  // the other hooks: below the early return it would run conditionally
  // and desync React's hook order.
  const gradientId = `bodyfill-${useId()}`;

  const last = points.length - 1;

  // Walk the loans once the object is on screen. This is the series
  // playing forward, not a loop: it settles on the final state and
  // stays, because the final state is the claim.
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
    }, 1400);
    return () => clearInterval(timer);
  }, [seen, motion, last, still]);

  if (points.length === 0) return null;

  const caps = points.map((p) => Number(BigInt(p.capacity) / 1_000_000n));
  const peak = peakOverride ?? Math.max(...caps);

  const current = points[Math.min(step, last)]!;
  const cap = caps[Math.min(step, last)]!;
  const stress = bindingIsStress(current.binding);
  // Zero capacity is a REFUSAL, not a small loan. It takes the ALARM
  // register and renders as a ring with nothing inside it -- the limit
  // exists and the agent reaches none of it.
  const denied = cap === 0;

  // The disc, scaled against the largest capacity in the series by AREA,
  // not radius. Mapping a quantity straight to the radius exaggerates
  // it: the eye reads a disc by how much ink it covers, and area grows
  // as the square, so a 31% rise in capacity would look like ~70% more
  // object. `sqrt` makes the covered area proportional to the figure,
  // which is the only version of this that is not flattering itself.
  //
  // A disc drawn to a round number would flatter it too, so the scale
  // is the series' own maximum.
  const discR = peak > 0 ? Math.sqrt(cap / peak) * LIMIT_R : 0;

  // The rate ring. A lower APR is a thinner ring: the price is the
  // weight the facility carries, so less of it is literally lighter.
  const rateWeight = Math.max(1.5, (current.apr / 1200) * 7);

  // Score drives the fill's warmth. Low scores sit in GROWTH (this is
  // history compounding); a high score pulls toward MACHINE, which is
  // the register the stress model speaks in.
  const score = current.score ?? 0;
  const warmth = Math.max(0, Math.min(1, (score - 560) / 200));

  // One hue per state, chosen from the three registers `docs/09`
  // requires stay distinct: a refusal is ALARM, a high score reaching
  // the stress model is MACHINE, and history compounding is GROWTH.
  const hue = denied
    ? "var(--alarm)"
    : warmth > 0.5
      ? "var(--machine)"
      : "var(--growth)";



  return (
    <div ref={ref} className="body">
      <svg
        viewBox={`0 0 ${SIZE} ${SIZE}`}
        role="img"
        aria-label={
          `Loan ${Math.min(step, last) + 1} of ${points.length}: capacity ` +
          `${cap.toLocaleString()} settlement units at ` +
          `${(current.apr / 100).toFixed(2)} percent, limited by ` +
          `${bindingPhrase(current.binding)}.`
        }
      >
        <defs>
          <radialGradient id={gradientId} cx="50%" cy="50%" r="50%">
            <stop offset="0%" stopColor={hue} stopOpacity="0.34" />
            <stop offset="100%" stopColor={hue} stopOpacity="0.05" />
          </radialGradient>
        </defs>

        {/* The limit ring. Dashed while the agent's own record is what
            bounds it -- a soft edge, because more history moves it. It
            goes SOLID when stress takes over: that limit does not move
            by behaving well. */}
        <circle
          cx={CENTER}
          cy={CENTER}
          r={LIMIT_R}
          fill="none"
          stroke={
            denied
              ? "var(--alarm)"
              : stress
                ? "var(--machine)"
                : "var(--border)"
          }
          strokeWidth={denied || stress ? 2 : 1}
          strokeDasharray={denied || stress ? "none" : "2 6"}
          className="body-limit"
        />

        {/* The capacity itself. */}
        <circle
          cx={CENTER}
          cy={CENTER}
          r={discR}
          fill={`url(#${gradientId})`}
          stroke={hue}
          strokeWidth="1.5"
          className="body-disc"
        />

        {/* The rate, as a ring riding the capacity edge. */}
        <circle
          cx={CENTER}
          cy={CENTER}
          r={discR}
          fill="none"
          stroke={hue}
          strokeWidth={rateWeight}
          strokeOpacity="0.18"
          className="body-rate"
        />

        {/* The figure at the core. The object is the argument; the
            number is what a reader writes down. */}
        <text
          x={CENTER}
          y={CENTER - 2}
          textAnchor="middle"
          className="body-figure"
        >
          {denied ? "denied" : `${(current.apr / 100).toFixed(2)}%`}
        </text>
        <text
          x={CENTER}
          y={CENTER + 17}
          textAnchor="middle"
          className="body-sub"
        >
          loan {Math.min(step, last) + 1}
        </text>
      </svg>

      {/* The leader line, naming the edge that is actually binding. */}
      <p
        className={
          `body-caption ${denied ? "body-caption-denied" : ""} ` +
          `${!denied && stress ? "body-caption-stress" : ""}`
        }
      >
        <span className="body-caption-label">Limited by</span>
        <span title={current.binding}>{bindingPhrase(current.binding)}</span>
      </p>

      {/* Stepping is manual too. An object that only plays cannot be
          examined, and the switch is the frame worth sitting on. */}
      {points.length > 1 && (
      <div className="body-steps" role="group" aria-label="Choose a loan">
        {points.map((p, i) => (
          <button
            key={i}
            type="button"
            className={`body-step ${i === Math.min(step, last) ? "on" : ""} ${
              bindingIsStress(p.binding) ? "body-step-stress" : ""
            }`}
            aria-label={`Loan ${i + 1}`}
            aria-pressed={i === Math.min(step, last)}
            onClick={() => setStep(i)}
          />
        ))}
      </div>
      )}
    </div>
  );
}
