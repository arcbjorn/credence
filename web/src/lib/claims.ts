/**
 * What this system claims, and what it refuses to claim.
 *
 * `docs/12` is the most product-shaped document in the repository: ten
 * claims, each with the support that would earn it and an honest status.
 * It sat in markdown nobody opens, while the landing page made a general
 * case in prose. This module puts the ledger itself on the front page.
 *
 * The structure follows `docs/12` exactly and adds nothing. A claim is
 * `earned` only when the support beside it exists and was re-run; the two
 * that are not earned say why in their own words, because the REASON is
 * the interesting part -- one is closed by a commercial agreement outside
 * this project's control, the other by the fact that a market of
 * project-controlled participants has priced nothing.
 *
 * Keeping the unearned rows at the same visual weight as the earned ones
 * is the point. A page that lists only what works reads as marketing; the
 * limitations are what make the rest credible.
 */

/** Whether a claim's support exists and was re-run. */
export type ClaimStatus = "earned" | "unearned";

export interface Claim {
  /** The claim itself, phrased as the thing a reader would want true. */
  title: string;
  /** What it would take to earn it. */
  support: string;
  status: ClaimStatus;
  /** The evidence, or the reason it is missing. Shown on expand. */
  detail: string;
  /** Where a reader can go and check, when a screen shows it. */
  to?: string;
}

/**
 * The ten claims of `docs/12`, in its order.
 *
 * Ordering is not by status: interleaving would hide that eight are
 * earned, and sorting the two unearned to the bottom would bury them.
 * `docs/12`'s own order puts them last because they are the newest, and
 * that is honest without being evasive.
 */
export const CLAIMS: Claim[] = [
  {
    title: "Decisions are reproducible",
    support: "Retained canonical inputs plus matching replay",
    status: "earned",
    detail:
      "The retained SQLite is reopened and replayed to MATCH, with a " +
      "tampered control that must DIFFER. Replay it yourself in the browser.",
    to: "/replay",
  },
  {
    title: "Credit grows with demonstrated behaviour",
    support: "Three sequential loans under one policy, plus ablation",
    status: "earned",
    detail:
      "1200 → 1147 → 1094 bps under one policy and one model hash, with " +
      "the binding constraint switching to stress capacity at loan 3.",
    to: "/agent",
  },
  {
    title: "Current risk overrides reputation",
    support: "A concentrated book with binding portfolio constraints",
    status: "earned",
    detail:
      "The Concentrated profile is denied on a flawless repayment record, " +
      "because what it is holding now is what would not survive a crash.",
    to: "/compare",
  },
  {
    title: "Borrowed capital is constrained",
    support: "Actual contract rejection and adapter boundaries",
    status: "earned",
    detail:
      "The withdrawal reverts on chain with two different selectors: one " +
      "for open debt, another for a stranger. Not a policy — the contract.",
    to: "/enforcement",
  },
  {
    title: "Losses are accounted for",
    support: "Realized collection and final settlement reconcile",
    status: "earned",
    detail:
      "4,000 lent = 1,400 recovered + 2,600 written off, reconciled on " +
      "chain. Most demos never show you a loan that went wrong.",
    to: "/lender",
  },
  {
    title: "Underwriting is continuous",
    support: "Unprompted reassessment reaching chain within an interval",
    status: "earned",
    detail:
      "A price move produced a blocked result on chain about a second " +
      "after the observation moved, against a 600s heartbeat.",
    to: "/lifecycle",
  },
  {
    title: "Forecasts are evaluated",
    support: "Prior commitment, outcome protocol, retained resolutions",
    status: "earned",
    detail:
      "The commitment precedes funding, and three resolved Brier scores " +
      "are retained. Three observations permit an average, not a " +
      "reliability claim — and this page will not call it one.",
    to: "/lifecycle",
  },
  {
    title: "Reputation is independently verifiable",
    support: "A standalone reader using no Credence endpoint",
    status: "earned",
    detail:
      "Two repaid obligations read back from the canonical registry with " +
      "no Credence endpoint involved. Attributable and filterable — not " +
      "proof the referenced evidence is true, and not third-party adoption.",
    to: "/indexed",
  },
  {
    title: "CRE checks an assessment",
    support: "Exact deployed workflow checks and report evidence",
    status: "unearned",
    detail:
      "Closed rather than pending. The receiver is deployed, 21 tests " +
      "pass and the workflow simulates against the live forwarder — but " +
      "deploy access requires a commercial agreement, not an approval. No " +
      "DON-signed report will reach the live receiver in this release.",
  },
  {
    title: "A note is tradable debt exposure",
    support: "Defined holder rights, market transfer, redemption",
    status: "unearned",
    detail:
      "The instrument is built, tested and listed on a real orderbook " +
      "that the router confirms is a verified market. But every " +
      "participant so far is project-controlled, and a controlled market " +
      "has valued nothing. A price nothing independent produced cannot " +
      "move executable credit.",
  },
];

/**
 * What the release must not claim, from `docs/12`.
 *
 * Shown verbatim and without softening. A reader who has just been told
 * eight things work is owed the list of things that do not, in the
 * project's own words rather than in a summary of them.
 */
export const REFUSED: string[] = [
  "Calibrated default probabilities",
  "Guaranteed liquidation recoveries",
  "Organic note liquidity",
  "Complete Sybil resistance",
  "Proof of offchain model execution",
  "Third-party adoption of published reputation",
];

/** Counts for the summary line. Derived, never written down twice. */
export const EARNED = CLAIMS.filter((c) => c.status === "earned").length;
export const UNEARNED = CLAIMS.filter((c) => c.status === "unearned").length;
