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
 *
 * The detail strings are written to survive a skeptical reader. Each one
 * says what was actually run, then bounds it -- what the evidence does
 * NOT establish. That second half is the part a demo normally omits, and
 * omitting it is how a project ends up claiming calibration from three
 * data points.
 */

/** Whether a claim's support exists and was re-run. */
export type ClaimStatus = "earned" | "unearned";

/**
 * Which part of the story a claim belongs to.
 *
 * Ten flat rows read as a table of contents: the reader has no way in
 * except to start at the top. Three groups give the list a shape --
 * what it does, what it refuses to pretend, and what it is still
 * building -- and each group can carry one sentence explaining why it
 * exists, rather than one header standing for all ten.
 */
export type ClaimGroup = "proof" | "limits" | "building";

export interface Claim {
  group: ClaimGroup;
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
    group: "proof",
    title: "Decisions are reproducible",
    support: "The saved inputs, re-run and matched",
    status: "earned",
    detail:
      "Reopen the retained SQLite, feed the same bytes back through the " +
      "same engine, and you get the same assessment. There is a control " +
      "that tampers with one input and must NOT match — a replay test " +
      "that only checks the happy path proves nothing.",
    to: "/replay",
  },
  {
    group: "proof",
    title: "Credit grows with demonstrated behaviour",
    support: "Three loans in a row under one unchanged rulebook",
    status: "earned",
    detail:
      "1200 → 1147 → 1094 bps across three loans, same policy and same " +
      "model hash throughout. The ablation isolates which evidence moved " +
      "the terms. Note this shows behaviour CHANGES the price; it does " +
      "not show the change is correctly sized.",
    to: "/agent",
  },
  {
    group: "limits",
    title: "Current risk overrides reputation",
    support: "A borrower holding too much of one risky asset",
    status: "earned",
    detail:
      "An agent with a spotless repayment record gets denied, because " +
      "what it is holding right now would not survive the stress " +
      "scenarios. Reputation is an input, not a trump card.",
    to: "/compare",
  },
  {
    group: "limits",
    title: "Borrowed capital is constrained",
    support: "A real transaction, refused on chain",
    status: "earned",
    detail:
      "The agent tries to withdraw its collateral while it still owes " +
      "money, and the transaction reverts. Two different revert selectors: " +
      "one for open debt, one for a caller who is not the owner. This is " +
      "the contract refusing, not a service declining.",
    to: "/enforcement",
  },
  {
    group: "limits",
    title: "Losses are accounted for",
    support: "What was lent, recovered and lost, adding up",
    status: "earned",
    detail:
      "4,000 lent, 1,400 recovered, 2,600 written off, and the three " +
      "reconcile onchain. Every lending demo shows you the loans that got " +
      "repaid; this one shows you the default and where the money went.",
    to: "/lender",
  },
  {
    group: "proof",
    title: "Underwriting is continuous",
    support: "A re-check nobody asked for, landing on chain",
    status: "earned",
    detail:
      "Nobody asked it to re-underwrite. A price moved, and a blocked " +
      "result landed onchain about a second later, against a 600-second " +
      "heartbeat — so the reassessment came from the observation, not " +
      "from the timer.",
    to: "/arc",
  },
  {
    group: "proof",
    title: "Forecasts are evaluated",
    support: "A prediction made before the money moved, then scored",
    status: "earned",
    detail:
      "The forecast is committed onchain BEFORE the money moves, then " +
      "scored against a rule fixed in advance. Three resolved Brier " +
      "scores are retained. Three data points give you an average and " +
      "nothing more — calling that calibration would be a lie, so this " +
      "page does not.",
    to: "/arc",
  },
  {
    group: "proof",
    title: "Reputation is independently verifiable",
    support: "A reader that works without touching our servers",
    status: "earned",
    detail:
      "A standalone reader pulls two repaid obligations off the canonical " +
      "registry without touching any Credence endpoint, so the reputation " +
      "survives us going away. What this does NOT show: that the " +
      "referenced evidence is true, or that anyone else is using it.",
    to: "/indexed",
  },
  {
    group: "building",
    title: "CRE checks an assessment",
    support: "The live workflow running its checks for real",
    status: "unearned",
    detail:
      "Closed, not pending. The receiver is deployed, 21 tests pass, and " +
      "the workflow simulates against the live forwarder. What is missing " +
      "is deploy access, which turns out to need a commercial agreement " +
      "rather than an approval — not something a build can fix. So no " +
      "DON-signed report reaches the live receiver in this release, and " +
      "the claim stays unearned.",
  },
  {
    group: "building",
    title: "A note is tradable debt exposure",
    support: "Someone independent actually buying or selling one",
    status: "unearned",
    detail:
      "The instrument exists, its redemption arithmetic is tested, and " +
      "it is listed on a real orderbook the router confirms is verified. " +
      "The problem is who is on the other side: every participant so far " +
      "is us. A market where you are both counterparties has priced " +
      "nothing, and a price nothing independent produced must not be " +
      "allowed to move real credit.",
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
  "That our default odds are accurate",
  "That collateral always sells for what we assumed",
  "That anyone outside this project trades the debt notes",
  "That nobody can game it by spinning up many identities",
  "That the scoring ran untampered outside the chain",
  "That anyone else uses the reputation we publish",
];

/** Counts for the summary line. Derived, never written down twice. */
/**
 * The heading and the one sentence that explains each group.
 *
 * The explanation is the reference's middle column: a reader should
 * know why a group exists before reading its items, and one shared
 * header cannot say that for ten unrelated rows.
 */
export const GROUPS: { key: ClaimGroup; title: string; blurb: string }[] = [
  {
    key: "proof",
    title: "What it proves",
    blurb:
      "Each of these was produced by a real run and kept. Open one to see the evidence and where it came from.",
  },
  {
    key: "limits",
    title: "What it refuses",
    blurb:
      "A lending demo that only shows loans being repaid proves nothing. These are the refusals and the losses.",
  },
  {
    key: "building",
    title: "Still building",
    blurb:
      "Not finished, and not hidden in a footnote. Each says plainly what is missing and why.",
  },
];

export const EARNED = CLAIMS.filter((c) => c.status === "earned").length;
export const UNEARNED = CLAIMS.filter((c) => c.status === "unearned").length;
