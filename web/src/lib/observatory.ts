import { useEffect, useState } from "react";
import { loadCohort, loadComparison, loadIndexed, loadRun, type Assessment, type Cohort, type Comparison, type Indexed, type Run } from "./data";

export type Projection = "globe" | "atlas" | "core" | "layers" | "moons" | "eclipse";

export interface WorldNode {
  id: string;
  label: string;
  capacity: string;
  score: number | null;
  denied: boolean;
  latitude: number;
  longitude: number;
}

export interface Survey {
  run: Run | null;
  comparison: Comparison | null;
  indexed: Indexed | null;
  /** Several agents on one facility. Absent until `demo-cohort.sh` runs. */
  cohort: Cohort | null;
  loading: boolean;
  unavailable: string[];
}

/** Independent sources stay independent: a failed indexer cannot hide the book. */
export function useSurvey(): Survey {
  const [survey, setSurvey] = useState<Survey>({ run: null, comparison: null, indexed: null, cohort: null, loading: true, unavailable: [] });
  useEffect(() => {
    let active = true;
    Promise.allSettled([loadRun("repay"), loadComparison(), loadIndexed(), loadCohort()]).then(([run, comparison, indexed, cohort]) => {
      if (!active) return;
      setSurvey({
        run: run.status === "fulfilled" ? run.value : null,
        comparison: comparison.status === "fulfilled" ? comparison.value : null,
        indexed: indexed.status === "fulfilled" ? indexed.value : null,
        cohort: cohort.status === "fulfilled" ? cohort.value : null,
        loading: false,
        unavailable: [run.status === "rejected" ? "book" : "", comparison.status === "rejected" ? "profiles" : "", indexed.status === "rejected" ? "indexed log" : "", cohort.status === "rejected" ? "cohort" : ""].filter(Boolean),
      });
    });
    return () => { active = false; };
  }, []);
  return survey;
}

export function originations(run: Run | null): Assessment[] {
  return run?.assessments.filter(a => a.payload.outcome.decision === "ApprovedOrigination") ?? [];
}

/**
 * Spread `count` nodes over the sphere without clumping.
 *
 * The coordinates were three hardcoded pairs indexed `i % 3`, so a fourth
 * agent landed exactly on the first and an eighth stacked three deep --
 * a globe that silently showed three points for eight agents.
 *
 * A Fibonacci lattice places any number of points at near-uniform
 * spacing, so the arrangement stays legible as the set grows. These are
 * positions on an object, not geographic claims: the latitude an agent
 * gets means nothing about the agent.
 */
function lattice(count: number, i: number): { latitude: number; longitude: number } {
  if (count <= 0) return { latitude: 0, longitude: 0 };
  // Offset by a half step so the first point is not exactly at a pole,
  // where every longitude renders on top of itself.
  const y = count === 1 ? 0 : 1 - (2 * i + 1) / count;
  const latitude = (Math.asin(Math.max(-1, Math.min(1, y))) * 180) / Math.PI;
  // The golden angle, which is what keeps successive points from lining
  // up into visible spokes.
  const longitude = (((i * 137.507_764_05) % 360) + 540) % 360 - 180;
  return {
    latitude: Number(latitude.toFixed(2)),
    longitude: Number(longitude.toFixed(2)),
  };
}

/** Coordinates arrange evidence on the object; they are not geographic claims. */
export function profileNodes(comparison: Comparison | null): WorldNode[] {
  const profiles = comparison?.profiles ?? [];
  return profiles.map((profile, i) => {
    const outcome = profile.assessment.outcome;
    return {
      id: profile.name,
      label: profile.name,
      // An `Unavailable` assessment carries no capacity at all: the
      // engine declined to evaluate rather than sizing the agent at
      // zero. Rendered as a zero-radius node, which is honest -- there
      // is nothing to size it by -- and `denied` keeps it out of the
      // approved filter.
      capacity: outcome.capacity?.agent_capacity ?? "0",
      score: outcome.capacity?.score ?? null,
      denied: outcome.decision !== "ApprovedOrigination",
      ...lattice(profiles.length, i),
    };
  });
}

/**
 * Where the money comes from, as a point on the object.
 *
 * Every arc used to start at a hardcoded `point(-22, 60)` that no node
 * occupied, so the origin dot sat at a coordinate with no referent and
 * the picture never said what the lines came OUT of.
 *
 * The pool is a real destination in the interface already -- `/moons`
 * opens it, and `facility` is the selection id both the moon satellite
 * and the room instruments use -- so it gets a node like anything else a
 * reader can click.
 *
 * Its coordinates are FIXED rather than laid out with the agents: it is
 * the one point every arc shares, and a pool that wandered as the cohort
 * grew would drag the whole picture with it.
 */
export const POOL_ID = "facility";
export const POOL_POSITION = { latitude: -22, longitude: 60 } as const;

export function poolNode(book: { idle: bigint | null; outstanding: bigint | null }): WorldNode[] {
  // Nothing measured means no pool to draw. A zero-sized point would
  // claim a facility with no cash rather than one nobody has read.
  if (book.idle === null && book.outstanding === null) return [];
  const total = (book.idle ?? 0n) + (book.outstanding ?? 0n);
  return [{
    id: POOL_ID,
    label: "Lending pool",
    capacity: total.toString(),
    score: null,
    denied: false,
    ...POOL_POSITION,
  }];
}

export function borrowerNode(loan: Assessment | undefined): WorldNode[] {
  if (!loan) return [];
  return [{ id: "borrower", label: "Retained agent", capacity: loan.payload.outcome.capacity?.agent_capacity ?? "0", score: loan.payload.outcome.capacity?.score ?? null, denied: false, latitude: 24, longitude: -36 }];
}

/**
 * The agents that actually borrowed, each on its own credit account.
 *
 * These are the only multi-agent nodes with real chain history behind
 * them: `demo-cohort.sh` gave each one an account, funded it, and had the
 * engine price every loan it took. The comparison profiles are assessed
 * scenarios and never touched a chain, which is why the two are built by
 * different functions and labelled differently rather than concatenated
 * into one undifferentiated crowd.
 *
 * Sized by the LAST assessment, because an agent's capacity is what its
 * record has earned by the end of the run, not what it started with.
 */
export function cohortNodes(cohort: Cohort | null): WorldNode[] {
  const agents = cohort?.agents ?? [];
  return agents.map((agent, i) => {
    const last = agent.assessments[agent.assessments.length - 1];
    const outcome = last?.payload.outcome;
    return {
      id: agent.name,
      label: agent.name,
      capacity: outcome?.capacity?.agent_capacity ?? "0",
      score: outcome?.capacity?.score ?? null,
      denied: outcome ? outcome.decision !== "ApprovedOrigination" : true,
      ...lattice(agents.length, i),
    };
  });
}

/**
 * Every agent the bundle knows, for the landing globe.
 *
 * The landing copy says "One point per agent, sized by what we'd lend it
 * today" while the globe rendered a single point, because it was handed
 * `borrowerNode` alone. A reader counting points against that sentence
 * found one agent and a claim about a market.
 *
 * Ordered by how much each node is worth as evidence. The retained
 * borrower keeps its own fixed position and stays FIRST; the cohort
 * follows, because those agents also borrowed on chain; the assessed
 * profiles come last. A lattice knows nothing about provenance, so
 * without this the one agent with real chain history would be shuffled in
 * among scenarios that never touched a chain.
 *
 * Duplicate ids are dropped rather than rendered twice: two nodes with
 * the same id would fight over the same focusable button.
 */
export function populationNodes(
  loan: Assessment | undefined,
  comparison: Comparison | null,
  cohort: Cohort | null = null,
  book: { idle: bigint | null; outstanding: bigint | null } | null = null,
): WorldNode[] {
  const all = [
    ...borrowerNode(loan),
    ...cohortNodes(cohort),
    ...profileNodes(comparison),
  ];
  const seen = new Set<string>();
  const unique = all.filter(node => {
    if (seen.has(node.id)) return false;
    seen.add(node.id);
    return true;
  });

  // Re-lay the WHOLE population as one set.
  //
  // `cohortNodes` and `profileNodes` each spread their own group over the
  // full sphere, so concatenating them puts two lattices on top of each
  // other and agents land on profiles. The positions only mean anything
  // relative to the other points being drawn, so they are assigned once,
  // here, over everything that is actually on screen.
  //
  // The borrower keeps its fixed coordinates: it is the one node another
  // screen links to, and having it move as the cohort grows would make it
  // unfindable.
  const laid = unique.map((node, i) =>
    node.id === "borrower"
      ? node
      : { ...node, ...lattice(unique.length, i) },
  );

  // The pool goes on LAST and keeps its fixed coordinates: it is the one
  // point every passage shares, so laying it out with the agents would
  // move the origin of every arc as the cohort grew.
  return book ? [...laid, ...poolNode(book)] : laid;
}

/**
 * One agent's borrowing, as a single passage from the pool.
 *
 * This was one arc PER LOAN, which drew seventeen curves for five
 * relationships: `cinder` alone got a six-deep fan while `sable` got one
 * line, so the picture's density was the loan count rather than anything
 * a reader could act on. It also meant the fan had to be invented --
 * successive loans share both endpoints, so each was lifted higher than
 * the last to stop them overlapping, which is geometry standing in for a
 * quantity that is not spatial.
 *
 * One arc per agent instead. The line says "this agent borrows from this
 * pool"; how MUCH and how OFTEN are carried by its weight and by the
 * figures in the dock, where a number belongs.
 */
export interface LoanArc {
  /** The node id this passage reaches, matching `WorldNode.id`. */
  agent: string;
  /** Every approved origination's principal, summed. Base units. */
  borrowed: string;
  /** How many approved originations the agent has taken. */
  loans: number;
  /** The longest term it borrowed for, which sets the arc's height. */
  term_seconds: number;
}

/** Fold one agent's approved originations into a single passage. */
function passage(agent: string, assessments: Assessment[]): LoanArc | null {
  let borrowed = 0n;
  let loans = 0;
  let term = 0;
  for (const a of assessments) {
    const outcome = a.payload.outcome;
    if (outcome.decision !== "ApprovedOrigination") continue;
    loans += 1;
    // Base units are uint256. Summed as BigInt because a realistic book
    // loses precision as a double, and this figure sets a line width a
    // reader compares against other lines.
    borrowed += BigInt(outcome.approved_principal ?? "0");
    term = Math.max(term, outcome.terms?.term_seconds ?? 0);
  }
  // An agent that borrowed nothing has no passage. Drawing one would
  // show a transfer that never happened.
  return loans === 0
    ? null
    : { agent, borrowed: borrowed.toString(), loans, term_seconds: term };
}

/**
 * One passage per agent that actually borrowed.
 *
 * The retained run's loans belong to the `borrower` node; each cohort
 * agent's belong to its own. Refusals are excluded: a denial moved no
 * money.
 */
export function loanArcs(
  run: Run | null,
  cohort: Cohort | null = null,
): LoanArc[] {
  const arcs: LoanArc[] = [];
  const retained = passage("borrower", run?.assessments ?? []);
  if (retained) arcs.push(retained);
  for (const agent of cohort?.agents ?? []) {
    const drawn = passage(agent.name, agent.assessments);
    if (drawn) arcs.push(drawn);
  }
  return arcs;
}

export function surveyDate(run: Run | null): string | null {
  const times = run?.assessments.map(a => a.created_at).filter(Number.isFinite) ?? [];
  if (!times.length) return null;
  return new Date(Math.max(...times) * 1000).toISOString().slice(0, 16).replace("T", " ") + " UTC";
}
