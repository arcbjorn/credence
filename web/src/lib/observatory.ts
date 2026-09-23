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
  return unique.map((node, i) =>
    node.id === "borrower"
      ? node
      : { ...node, ...lattice(unique.length, i) },
  );
}

export function surveyDate(run: Run | null): string | null {
  const times = run?.assessments.map(a => a.created_at).filter(Number.isFinite) ?? [];
  if (!times.length) return null;
  return new Date(Math.max(...times) * 1000).toISOString().slice(0, 16).replace("T", " ") + " UTC";
}
