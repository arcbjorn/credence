/**
 * The borrow desk: a request the reader composes, priced by the engine.
 *
 * Every other screen shows a decision that was already made. This one
 * lets a reader make the request themselves and watch the same Rust
 * engine -- compiled to WebAssembly, the identical artifact the replay
 * screen uses -- answer it. `docs/09` beat 8 asks a reader to confirm a
 * retained assessment reproduces; this asks the harder question of what
 * the engine would have said to a request nobody ever made.
 *
 * **It is still not a number the interface invented.** The controls edit
 * fields of a RETAINED snapshot and hand the result to `evaluate`. The
 * page computes no capacity, no rate and no decision: it edits an input
 * and renders an output. If the engine refuses, the refusal is shown.
 *
 * **Why only four controls.** Every input here was checked against the
 * engine before it was given a slider. Liquidity depth and forecast
 * accuracy move nothing at this account's size -- depth binds only when
 * the book is large enough for the liquidity shock to be the minimum, and
 * the Brier term enters through a cap that is not binding here. Shipping
 * a control that silently does nothing would teach a reader the engine
 * ignores an input it does not ignore, so they are described in
 * `INERT_INPUTS` and rendered as text rather than as dead controls.
 */

import type { Reason } from "./reasons";

/** A field of the snapshot the desk is allowed to edit. */
export type Lever =
  | "requested_principal"
  | "weighted_repayments_ppm"
  | "resolved_defaults"
  | "realized_volatility_bps";

/** What the reader has set. Base units and ppm, as the engine takes them. */
export interface Request {
  /** Settlement base units, 6 decimals. */
  requested_principal: string;
  /** Share of obligations repaid on time, in parts per million. */
  weighted_repayments_ppm: number;
  /** Loans that resolved as defaults. */
  resolved_defaults: number;
  /** Realized volatility of the risky asset, in basis points. */
  realized_volatility_bps: number;
}

/**
 * One control, with the range the engine was actually probed over.
 *
 * `max` is not decorative. A principal slider that runs to ten times the
 * account's capacity spends most of its travel in a denial that never
 * changes, which reads as a broken control rather than as a limit.
 */
export interface LeverSpec {
  id: Lever;
  label: string;
  /** What moving it means, in the borrower's terms. */
  help: string;
  min: number;
  max: number;
  step: number;
  /** Render the raw engine value as the control's caption. */
  format: (v: number) => string;
  /** What this lever moves: the price, the ceiling, or both. */
  moves: "price" | "capacity" | "both";
}

const pct = (ppm: number) => `${(ppm / 10_000).toFixed(1)}%`;

export const LEVERS: LeverSpec[] = [
  {
    id: "requested_principal",
    label: "How much to borrow",
    help:
      "The principal asked for. Above the agent's capacity the engine " +
      "refuses outright rather than quoting a worse price.",
    // Base units are carried as strings everywhere else because a uint256
    // loses precision as a double. These are settlement amounts at six
    // decimals and at most ~1e11, far inside the safe integer range, so a
    // slider can work in numbers and convert once at the edge.
    min: 500_000_000,
    max: 12_000_000_000,
    step: 250_000_000,
    format: (v) => `${(v / 1e9).toFixed(2)}B`,
    moves: "price",
  },
  {
    id: "weighted_repayments_ppm",
    label: "Repayment record",
    help:
      "What share of its obligations this agent has repaid on time. The " +
      "one input the agent earns rather than posts.",
    min: 0,
    max: 1_000_000,
    step: 25_000,
    format: pct,
    moves: "both",
  },
  {
    id: "resolved_defaults",
    label: "Defaults on record",
    help:
      "Obligations that resolved as defaults. Priced as a surcharge, not " +
      "as a ban -- a record with a default is still a record.",
    min: 0,
    max: 6,
    step: 1,
    format: (v) => (v === 1 ? "1 default" : `${v} defaults`),
    moves: "price",
  },
  {
    id: "realized_volatility_bps",
    label: "Market volatility",
    help:
      "Realized volatility of the asset the principal buys. The engine " +
      "stresses the book against it before lending into it.",
    min: 0,
    max: 9_000,
    step: 250,
    format: (v) => `${(v / 100).toFixed(1)}%`,
    moves: "capacity",
  },
];

/**
 * Inputs a reader will look for and not find, and why.
 *
 * Named explicitly because their absence is otherwise indistinguishable
 * from an oversight, and a reader who knows the model will notice.
 */
export const INERT_INPUTS: { label: string; why: string }[] = [
  {
    label: "Loan term",
    why:
      "Priced into the interest an approved loan accrues, but it does not " +
      "move the rate or the ceiling at this size: the stress test is a " +
      "point-in-time solvency check, not a function of how long the loan runs.",
  },
  {
    label: "Liquidity depth",
    why:
      "Binds only when the liquidity shock is the minimum of the stress " +
      "scenarios. At this account's size the price shock binds first, so " +
      "depth changes nothing here. The Perpl record shows it where it does bind.",
  },
  {
    label: "Forecast accuracy",
    why:
      "Enters through a cap on capacity that is well above the binding " +
      "constraint for this account, so moving it changes no output.",
  },
];

/**
 * The engine's answer, narrowed to what the desk renders.
 *
 * Optional fields spell out `| undefined` rather than relying on `?`
 * alone: this project builds under `exactOptionalPropertyTypes`, where
 * the two mean different things and "absent" is not assignable to
 * "present but undefined". A refused quote genuinely carries no rate, so
 * the absence has to be expressible.
 */
export interface Quote {
  decision: string;
  approved_principal?: string | undefined;
  requested_principal?: string | undefined;
  apr_bps?: number | undefined;
  agent_capacity?: string | undefined;
  agent_binding?: string | undefined;
  score?: number | null | undefined;
  required_equity?: string | undefined;
  price_breakdown?:
    | {
        base_rate_bps: number;
        history_discount_bps: number;
        portfolio_surcharge_bps: number;
        apr_bps: number;
        clamped: boolean;
      }
    | undefined;
  stress?:
    | {
        shock: string;
        applied_price_shock_bps: number;
        recoverable_value: string;
        debt_with_reserved_interest?: string | undefined;
        passes: boolean;
      }[]
    | undefined;
  reasons: Reason[];
  /** True when the engine could not evaluate at all. */
  unavailable: boolean;
}

/** The raw engine payload, as much of it as this module reads. */
interface Payload {
  outcome?: {
    decision?: string;
    requested_principal?: string;
    approved_principal?: string;
    capacity?: {
      agent_capacity?: string;
      agent_binding?: string;
      score?: number | null;
    };
    terms?: {
      apr_bps?: number;
      required_equity?: string;
      price_breakdown?: Quote["price_breakdown"];
    };
  };
  stress_results?: Quote["stress"];
  reasons?: Reason[];
}

/**
 * The request a retained snapshot already encodes.
 *
 * The desk opens on the real request rather than on a default of
 * someone's choosing, so the reader's first move is a comparison against
 * something that actually happened.
 */
export function requestFromSnapshot(snapshot: unknown): Request {
  const s = snapshot as {
    context?: { request?: { requested_principal?: string } };
    history?: { value?: { weighted_repayments_ppm?: number; resolved_defaults?: number } };
    market?: { value?: { realized_volatility_bps?: number } };
  };
  return {
    requested_principal: s.context?.request?.requested_principal ?? "4000000000",
    weighted_repayments_ppm: s.history?.value?.weighted_repayments_ppm ?? 0,
    resolved_defaults: s.history?.value?.resolved_defaults ?? 0,
    realized_volatility_bps: s.market?.value?.realized_volatility_bps ?? 0,
  };
}

/**
 * A retained snapshot with the reader's request substituted.
 *
 * Deep-cloned rather than mutated: the caller holds the retained
 * snapshot and every other screen reads the same cached object, so
 * editing it in place would quietly corrupt the replay screen's input.
 *
 * `content_hash` on each evidence item is deliberately LEFT ALONE. The
 * engine does not verify it -- only the policy hash is enforced -- and
 * rewriting it would forge agreement between an edited value and a hash
 * that was computed over different bytes. Leaving it stale is what makes
 * an edited snapshot legible as edited.
 */
export function applyRequest(snapshot: unknown, request: Request): unknown {
  const next = structuredClone(snapshot) as Record<string, any>;
  if (next.context?.request) {
    next.context.request.requested_principal = request.requested_principal;
  }
  if (next.history?.value) {
    next.history.value.weighted_repayments_ppm = request.weighted_repayments_ppm;
    next.history.value.resolved_defaults = request.resolved_defaults;
  }
  if (next.market?.value) {
    next.market.value.realized_volatility_bps = request.realized_volatility_bps;
  }
  return next;
}

/** Narrow the engine's payload to what the desk renders. */
export function readQuote(raw: string): Quote {
  const payload = JSON.parse(raw) as Payload;
  const outcome = payload.outcome ?? {};
  const capacity = outcome.capacity ?? {};
  const terms = outcome.terms ?? {};
  const decision = outcome.decision ?? "Unavailable";
  return {
    decision,
    approved_principal: outcome.approved_principal,
    requested_principal: outcome.requested_principal,
    apr_bps: terms.apr_bps,
    agent_capacity: capacity.agent_capacity,
    agent_binding: capacity.agent_binding,
    score: capacity.score ?? null,
    required_equity: terms.required_equity,
    price_breakdown: terms.price_breakdown,
    stress: payload.stress_results,
    reasons: payload.reasons ?? [],
    // An `Unavailable` decision means the engine declined to evaluate --
    // a malformed snapshot or a policy hash that is not the content hash
    // of the policy it was handed. That is a different state from a
    // considered refusal, and the desk must not render it as one.
    unavailable: decision === "Unavailable",
  };
}

/** True when the engine approved, whatever the variant is called. */
export function approved(quote: Quote): boolean {
  return quote.decision.startsWith("Approved");
}

/**
 * The price, decomposed into the terms the engine added up.
 *
 * Returned as rows rather than rendered, so the test can assert the
 * arithmetic closes: the parts must sum to the quoted rate, or the
 * breakdown is decorative.
 */
export interface PriceRow {
  label: string;
  bps: number;
  /** Negative terms are discounts and render differently. */
  kind: "base" | "discount" | "surcharge" | "total";
}

export function priceRows(quote: Quote): PriceRow[] {
  const b = quote.price_breakdown;
  if (!b) return [];
  return [
    { label: "Base rate", bps: b.base_rate_bps, kind: "base" },
    { label: "Earned by its record", bps: -b.history_discount_bps, kind: "discount" },
    { label: "Portfolio surcharge", bps: b.portfolio_surcharge_bps, kind: "surcharge" },
    { label: "Quoted rate", bps: b.apr_bps, kind: "total" },
  ];
}

/**
 * Interest over the term, at the quoted rate.
 *
 * Simple interest on actual/365, matching `pricing.rs`. Shown so a
 * reader sees the rate as money rather than as a percentage, and marked
 * in the interface as derived HERE rather than returned by the engine --
 * the engine's own `economics` block is what the assessment record shows.
 */
export function interestOver(
  principal: string,
  aprBps: number,
  termSeconds: number,
): string {
  const p = BigInt(principal);
  const interest =
    (p * BigInt(Math.round(aprBps)) * BigInt(Math.round(termSeconds))) /
    (10_000n * 31_536_000n);
  return interest.toString();
}
