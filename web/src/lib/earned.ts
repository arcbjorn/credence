/**
 * The curve a record earns.
 *
 * `scripts/demo-growth.sh` originates the same principal repeatedly
 * against one agent and changes nothing else: same policy, same market,
 * same asset, same term. So the rate it is quoted and the ceiling it is
 * granted move for exactly one reason, and this module reads that
 * sequence out of the retained run.
 *
 * The claim the whole project rests on is that credit can be priced from
 * demonstrated behaviour. Until this run existed the evidence for it was
 * spread across loans that differed in several ways at once, which is
 * suggestive rather than attributable.
 */

import type { Run } from "./data";

/** One origination in the sequence, as the engine priced it. */
export interface Point {
  sequence: number;
  apr_bps: number;
  capacity: string;
  binding: string;
  principal: string;
  /** The record the engine read, in ppm of obligations repaid on time. */
  repayments_ppm: number | null;
  score: number | null;
}

interface Payload {
  sequence?: number;
  outcome?: {
    decision?: string;
    approved_principal?: string;
    capacity?: {
      agent_capacity?: string;
      agent_binding?: string;
      score?: number | null;
    };
    terms?: { apr_bps?: number; approved_principal?: string };
  };
}

/**
 * The priced originations of a run, in order.
 *
 * Monitoring assessments and refusals are dropped: neither carries a
 * quoted rate, and a gap in the middle of a curve reads as a rate that
 * went to zero.
 */
export function curve(run: Run | null): Point[] {
  if (!run) return [];
  const points: Point[] = [];
  for (const a of run.assessments) {
    const payload = a.payload as Payload | undefined;
    const outcome = payload?.outcome;
    const terms = outcome?.terms;
    const capacity = outcome?.capacity;
    if (!terms?.apr_bps || !capacity?.agent_capacity) continue;
    const snapshot = run.snapshots[a.input_hash] as
      | { history?: { value?: { weighted_repayments_ppm?: number } } }
      | undefined;
    points.push({
      sequence: payload?.sequence ?? a.sequence,
      apr_bps: terms.apr_bps,
      capacity: capacity.agent_capacity,
      binding: capacity.agent_binding ?? "",
      principal:
        terms.approved_principal ?? outcome?.approved_principal ?? "0",
      repayments_ppm:
        snapshot?.history?.value?.weighted_repayments_ppm ?? null,
      score: capacity.score ?? null,
    });
  }
  return points.sort((a, b) => a.sequence - b.sequence);
}

/** What the sequence demonstrated, stated as one fact. */
export interface Earned {
  first: Point;
  last: Point;
  /** Basis points shaved off the rate. Negative if the rate rose. */
  discount_bps: number;
  /** True when the principal was held constant, making the delta clean. */
  constant_principal: boolean;
  /** Distinct constraints that bound capacity across the run. */
  bindings: string[];
}

/**
 * Summarise a curve, or return null when it cannot support a claim.
 *
 * Two points are the minimum: one origination demonstrates nothing about
 * a trend. Returning null rather than a degenerate summary keeps the
 * screen from asserting a curve through a single point.
 */
export function earned(points: Point[]): Earned | null {
  if (points.length < 2) return null;
  const first = points[0]!;
  const last = points[points.length - 1]!;
  return {
    first,
    last,
    discount_bps: first.apr_bps - last.apr_bps,
    // The delta is only attributable to the record if the request did not
    // change. When it did, the screen has to say so rather than claim
    // the record caused all of it.
    constant_principal: points.every((p) => p.principal === first.principal),
    bindings: [...new Set(points.map((p) => p.binding).filter(Boolean))],
  };
}

/**
 * Lay the curve out for an SVG polyline.
 *
 * Y is inverted here (SVG's origin is top-left) so a FALLING rate draws
 * a falling line. An earlier sketch plotted it unflipped and the rate
 * discount read as a rate increase.
 */
export interface Plot {
  points: { x: number; y: number; point: Point }[];
  path: string;
  min: number;
  max: number;
}

export function plot(
  points: Point[],
  pick: (p: Point) => number,
  width: number,
  height: number,
  pad = 4,
): Plot {
  if (points.length === 0) return { points: [], path: "", min: 0, max: 0 };
  const values = points.map(pick);
  const min = Math.min(...values);
  const max = Math.max(...values);
  // A flat series has no range to normalise against; draw it down the
  // middle rather than dividing by zero.
  const span = max - min || 1;
  const laid = points.map((point, i) => {
    const x =
      points.length === 1
        ? width / 2
        : pad + (i / (points.length - 1)) * (width - pad * 2);
    const y =
      pad + (1 - (pick(point) - min) / span) * (height - pad * 2);
    return { x, y, point };
  });
  return {
    points: laid,
    path: laid
      .map((p, i) => `${i === 0 ? "M" : "L"}${p.x.toFixed(2)} ${p.y.toFixed(2)}`)
      .join(" "),
    min,
    max,
  };
}
