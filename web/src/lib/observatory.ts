import { useEffect, useState } from "react";
import { loadComparison, loadIndexed, loadRun, type Assessment, type Comparison, type Indexed, type Run } from "./data";

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
  loading: boolean;
  unavailable: string[];
}

/** Independent sources stay independent: a failed indexer cannot hide the book. */
export function useSurvey(): Survey {
  const [survey, setSurvey] = useState<Survey>({ run: null, comparison: null, indexed: null, loading: true, unavailable: [] });
  useEffect(() => {
    let active = true;
    Promise.allSettled([loadRun("repay"), loadComparison(), loadIndexed()]).then(([run, comparison, indexed]) => {
      if (!active) return;
      setSurvey({
        run: run.status === "fulfilled" ? run.value : null,
        comparison: comparison.status === "fulfilled" ? comparison.value : null,
        indexed: indexed.status === "fulfilled" ? indexed.value : null,
        loading: false,
        unavailable: [run.status === "rejected" ? "book" : "", comparison.status === "rejected" ? "profiles" : "", indexed.status === "rejected" ? "indexed log" : ""].filter(Boolean),
      });
    });
    return () => { active = false; };
  }, []);
  return survey;
}

export function originations(run: Run | null): Assessment[] {
  return run?.assessments.filter(a => a.payload.outcome.decision === "ApprovedOrigination") ?? [];
}

/** Coordinates arrange evidence on the object; they are not geographic claims. */
export function profileNodes(comparison: Comparison | null): WorldNode[] {
  return comparison?.profiles.map((profile, i) => ({
    id: profile.name,
    label: profile.name,
    capacity: profile.assessment.outcome.capacity?.agent_capacity ?? "0",
    score: profile.assessment.outcome.capacity?.score ?? null,
    denied: profile.assessment.outcome.decision !== "ApprovedOrigination",
    latitude: [28, -12, -42][i % 3]!,
    longitude: [-48, 38, 115][i % 3]!,
  })) ?? [];
}

export function borrowerNode(loan: Assessment | undefined): WorldNode[] {
  if (!loan) return [];
  return [{ id: "borrower", label: "Retained agent", capacity: loan.payload.outcome.capacity?.agent_capacity ?? "0", score: loan.payload.outcome.capacity?.score ?? null, denied: false, latitude: 24, longitude: -36 }];
}

export function surveyDate(run: Run | null): string | null {
  const times = run?.assessments.map(a => a.created_at).filter(Number.isFinite) ?? [];
  if (!times.length) return null;
  return new Date(Math.max(...times) * 1000).toISOString().slice(0, 16).replace("T", " ") + " UTC";
}
