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
/**
 * Where the limit ring sits. Set above the body's own maximum so that
 * even a fully drawn facility leaves daylight between shell and limit:
 * at equal radii the limit, the body's edge and the rim light stacked
 * into one bright hoop, which read as a heavy outline rather than as a
 * body approaching its bound.
 */
const LIMIT_R = 150;
/** The largest the body itself gets, inside that ring. */
const BODY_MAX_R = 128;
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
  atmosphere = null,
  core = null,
  scar = null,
}: {
  points: GlobePoint[];
  /** Scale against a figure outside this series, so several bodies drawn
   *  side by side stay comparable. */
  peakOverride?: number;
  /** A single state: no stepper, no walk-through. */
  still?: boolean;
  /** `1 - utilization` in [0,1]. Thick haze is a lightly used book.
   *  `null` draws the default rim rather than inventing health. */
  atmosphere?: number | null;
  /** Idle cash against the whole book, in [0,1]: the core's brightness. */
  core?: number | null;
  /** Realized loss against the book, in [0,1]: a crater that stays. */
  scar?: number | null;
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
  const bodyR = peak > 0 ? Math.sqrt(cap / peak) * BODY_MAX_R : 0;

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

  // Atmosphere. A thick haze starts further in and burns brighter; a
  // thin one is a hairline at the limb. Clamped so even a fully drawn
  // book keeps an edge -- an object with NO rim reads as a hole.
  const haze = atmosphere;
  const rimStart = haze === null ? 88 : 94 - haze * 8;
  const rimStrength =
    haze === null
      ? denied
        ? 0.22
        : 0.42
      : Number((0.16 + haze * 0.34).toFixed(3));

  // The instrument readout, from loaded figures only.
  const readout: { label: string; value: string }[] = [];
  if (current.score !== undefined) {
    readout.push({ label: "score", value: String(current.score) });
  }
  if (atmosphere !== null) {
    readout.push({
      label: "util",
      value: `${Math.round((1 - atmosphere) * 100)}%`,
    });
  }
  if (scar !== null) {
    readout.push({ label: "scar", value: `${(scar * 100).toFixed(1)}%` });
  }

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
              drawn.

              Its THICKNESS is `1 - utilization`. A lightly used book
              looks padded; a heavily used one looks vacuum-thin, and
              you can read the book's health from across the room. With
              no figure the default rim is drawn rather than a
              reassuring one. */}
          <radialGradient id={`${gradientId}-rim`} cx="50%" cy="50%" r="50%">
            <stop offset={`${rimStart}%`} stopColor={hue} stopOpacity="0" />
            <stop offset="97%" stopColor={hue} stopOpacity={rimStrength} />
            <stop offset="100%" stopColor={hue} stopOpacity="0" />
          </radialGradient>

          {/* The core: the lender's idle cash, felt rather than
              tabulated. It dims as the vault lends its cash out. */}
          {core !== null && (
            <radialGradient id={`${gradientId}-core`} cx="50%" cy="50%" r="50%">
              <stop
                offset="0%"
                stopColor={hue}
                stopOpacity={(0.1 + core * 0.4).toFixed(3)}
              />
              <stop offset="45%" stopColor={hue} stopOpacity="0.04" />
              <stop offset="100%" stopColor={hue} stopOpacity="0" />
            </radialGradient>
          )}

          {/* The dark side. Light comes from upper left, so the
              opposite quarter falls away -- this is what turns a
              filled circle into a lit sphere. */}
          <radialGradient id={`${gradientId}-dark`} cx="72%" cy="76%" r="76%">
            <stop offset="0%" stopColor="#000" stopOpacity="0.3" />
            <stop offset="55%" stopColor="#000" stopOpacity="0.1" />
            <stop offset="100%" stopColor="#000" stopOpacity="0" />
          </radialGradient>

          {/* A specular bloom where the light strikes. Small and
              off-centre; a highlight in the middle reads as a button. */}
          <radialGradient id={`${gradientId}-spec`} cx="50%" cy="50%" r="50%">
            <stop offset="0%" stopColor="#fff" stopOpacity="0.1" />
            <stop offset="70%" stopColor="#fff" stopOpacity="0.02" />
            <stop offset="100%" stopColor="#fff" stopOpacity="0" />
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
          strokeWidth={denied || stress ? 1.2 : 1}
          strokeDasharray={denied || stress ? "none" : "2 7"}
          /* Held back so the bound frames the body rather than
             outshining it: at full strength the ring was the brightest
             thing on the page and the object read as its outline. */
          strokeOpacity={denied ? 0.72 : stress ? 0.55 : 0.85}
          className="globe-limit"
        />

        {/* Headroom: the room between what this agent has drawn and
            what it is allowed. A wide band is a facility with slack; as
            the body grows toward its bound the band closes, and at the
            switch it is nearly gone. Drawn as faint radial ticks so the
            gap reads as MEASURED space rather than as background. */}
        {bodyR > 0 && LIMIT_R - bodyR > 6 && (
          <g className="globe-headroom">
            {Array.from({ length: 72 }, (_, n) => {
              const a = ((n / 72) * 360 + spin * 0.4) * (Math.PI / 180);
              const x1 = CENTER + Math.cos(a) * (bodyR + 4);
              const y1 = CENTER + Math.sin(a) * (bodyR + 4);
              const x2 = CENTER + Math.cos(a) * (LIMIT_R - 3);
              const y2 = CENTER + Math.sin(a) * (LIMIT_R - 3);
              return (
                <line
                  key={n}
                  x1={x1}
                  y1={y1}
                  x2={x2}
                  y2={y2}
                  stroke={stress ? "var(--machine)" : "var(--rule)"}
                  strokeWidth="0.5"
                  strokeOpacity={n % 6 === 0 ? 0.16 : 0.05}
                />
              );
            })}
          </g>
        )}

        {/* The body. */}
        <circle
          cx={CENTER}
          cy={CENTER}
          r={bodyR}
          fill={`url(#${gradientId})`}
          className="globe-shell"
        />

        {/* The core. Drawn inside the body, so it reads as something
            the shell contains rather than a highlight on it. */}
        {core !== null && bodyR > 0 && (
          <circle
            cx={CENTER}
            cy={CENTER}
            r={bodyR * 0.62}
            fill={`url(#${gradientId}-core)`}
            className="globe-core"
          />
        )}

        {/* Far side of the wireframe, seen through the body. */}
        <g stroke={hue} fill="none" strokeWidth="0.4" opacity="0.16">
          {lats.map((l, n) => (
            <path key={`latf${n}`} d={l.far} />
          ))}
          {mers.map((m, n) => (
            <path key={`merf${n}`} d={m.far} />
          ))}
        </g>

        {/* Near side. */}
        <g stroke={hue} fill="none" strokeWidth="0.55" opacity="0.4">
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

        {/* Shading, over the wireframe so the lines fall into shadow
            with the surface they sit on. */}
        {bodyR > 0 && (
          <>
            <circle
              cx={CENTER}
              cy={CENTER}
              r={bodyR}
              fill={`url(#${gradientId}-dark)`}
              className="globe-dark"
            />
            <ellipse
              cx={CENTER - bodyR * 0.3}
              cy={CENTER - bodyR * 0.36}
              rx={bodyR * 0.42}
              ry={bodyR * 0.3}
              fill={`url(#${gradientId}-spec)`}
              className="globe-spec"
            />
          </>
        )}

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
          const d = toPath(pts);
          return (
            <g key={`arc${n}`}>
              <path
                d={d}
                fill="none"
                stroke={hue}
                strokeWidth="1.2"
                strokeOpacity={behind ? 0.16 : 0.7}
                className="globe-arc"
              />
              {/* A charge travelling the arc, lender to borrower. Only
                  on the near face and only when motion is allowed: a
                  pulse nobody can see is work the browser does for
                  nothing. The DIRECTION is the point -- capital moves
                  one way and comes back as repayment. */}
              {!behind && motion && (
                <circle r="1.8" fill={hue} className="globe-pulse">
                  <animateMotion
                    dur={`${2.4 + n * 0.5}s`}
                    repeatCount="indefinite"
                    path={d}
                  />
                </circle>
              )}
            </g>
          );
        })}

        {/* The scar. A default is a retained mark on the body, placed
            on the shell so it turns with it and passes out of sight
            rather than hovering. `brand/physics.md`: absent a default
            timestamp, nothing here infers that it has healed. */}
        {scar !== null && bodyR > 0 && (() => {
          const v = rotate(onSphere(-28, 58), spin, TILT);
          const at = project(v, CENTER, CENTER, bodyR);
          // Hidden well before the limb. At exactly z=0 a crater is
          // edge-on, and drawing it there produced a sliver hanging off
          // the silhouette that read as a rendering fault.
          if (v.z < 0.12) return null;

          const rr = Math.max(3.5, Math.sqrt(scar) * bodyR * 0.55);
          // Foreshortened along the surface normal: a crater seen at an
          // angle is an ellipse squashed TOWARD the centre of the disc,
          // not one squashed horizontally regardless of where it sits.
          const angle = (Math.atan2(at.y - CENTER, at.x - CENTER) * 180) / Math.PI;
          return (
            <g
              transform={`rotate(${angle.toFixed(2)} ${at.x.toFixed(2)} ${at.y.toFixed(2)})`}
              className="globe-scar"
            >
              <ellipse
                cx={at.x}
                cy={at.y}
                rx={rr * v.z}
                ry={rr}
                fill="var(--alarm)"
                fillOpacity={0.16 + v.z * 0.24}
                stroke="var(--alarm)"
                strokeOpacity={0.3 + v.z * 0.4}
                strokeWidth="0.7"
              />
              {/* A darker floor, so it reads as a pit rather than a
                  sticker: the crater has depth. */}
              <ellipse
                cx={at.x}
                cy={at.y}
                rx={rr * v.z * 0.5}
                ry={rr * 0.5}
                fill="#000"
                fillOpacity={0.3 * v.z}
              />
            </g>
          );
        })()}

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
        {points.length > 1 && <text x={CENTER} y={CENTER + 18} textAnchor="middle" className="globe-sub">
          loan {i + 1}
        </text>}
      </svg>

      {/* The instrument. Only figures that were loaded: a utilization
          the book reported and a score the assessment carried. No
          coordinate is shown, because the sites on the shell are a
          layout choice and printing them as latitude would dress a
          decoration up as a measurement. */}
      {(readout.length > 0) && (
        <p className="globe-readout">
          {readout.map((r) => (
            <span key={r.label}>
              <span className="globe-readout-label">{r.label}</span>
              {r.value}
            </span>
          ))}
        </p>
      )}

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
