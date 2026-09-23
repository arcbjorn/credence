import { useEffect, useState } from "react";
import { loadRun, type Evidence, type Run } from "../lib/data";
import { evidenceMode } from "../lib/modes";
import { ModeChip } from "../components/ModeChip";
import { Hash } from "../components/Hash";
import { RecordHeader } from "../components/Record";

const ITEMS = ["account", "market", "history", "forecasts", "portfolio"] as const;

const at = (t: number) =>
  t > 0 ? new Date(t * 1000).toISOString().replace("T", " ").slice(0, 19) : "-";

/**
 * `docs/09`: where did this come from, how fresh, how complete, is it
 * synthetic?
 *
 * Each item carries its own origin, coverage, source and content hash, and
 * this screen shows them per item rather than as one summary. `docs/09`
 * forbids collapsing evidence mode into a single indicator, and an
 * aggregate "evidence: ok" would do exactly that — hiding that the account
 * was read from chain while history was derived and a scenario's history
 * was generated.
 *
 * Age is shown as a signed offset from the assessment time, because a
 * negative one would mean evidence from the future.
 */
export function EvidenceInspector() {
  const [run, setRun] = useState<Run | null>(null);
  const [which, setWhich] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    loadRun("monitor")
      .then((r) => {
        setRun(r);
        setWhich(r.assessments.at(-1)?.input_hash ?? null);
      })
      .catch((e: Error) => setError(e.message));
  }, []);

  if (error) return <div className="error">{error}</div>;
  if (!run) return <p className="lede">Loading retained evidence…</p>;

  const hash = which ?? run.assessments.at(-1)?.input_hash;
  const snapshot = hash ? run.snapshots[hash] : undefined;
  if (!snapshot) {
    return (
      <>
        <RecordHeader title="Evidence">This run retained no snapshot to inspect.</RecordHeader>
      </>
    );
  }

  const assessedAt = snapshot.assessment_time;

  return (
    <>
      <RecordHeader title="Evidence">
        Every input the engine saw for this decision, with its source and
        age. No item is collapsed into a single status.
      </RecordHeader>

      <p>
        <label>
          Snapshot{" "}
          <select
            value={hash}
            onChange={(e) => setWhich(e.target.value)}
          >
            {run.assessments.map((a, i) => (
              <option key={a.input_hash} value={a.input_hash}>
                #{i + 1} — {a.decision}
              </option>
            ))}
          </select>
        </label>
      </p>

      <div className="card scroll">
        <table>
          <thead>
            <tr>
              <th>Item</th>
              <th>Origin</th>
              <th>State</th>
              <th>Coverage</th>
              <th>Source</th>
              <th className="num">Observed</th>
              <th className="num">Age at assessment</th>
            </tr>
          </thead>
          <tbody>
            {ITEMS.map((key) => {
              const e = snapshot[key] as Evidence | undefined;
              if (!e) {
                return (
                  <tr key={key}>
                    <td>{key}</td>
                    <td colSpan={6}>
                      <em>absent — not shown to the engine</em>
                    </td>
                  </tr>
                );
              }
              const age = assessedAt - e.observed_at;
              return (
                <tr key={key}>
                  <td>{key}</td>
                  <td><ModeChip mode={evidenceMode(e.origin)} /></td>
                  <td>{e.state}</td>
                  <td>{e.coverage}</td>
                  <td>{e.source}</td>
                  <td className="num">{at(e.observed_at)}</td>
                  <td className="num">
                    {age >= 0 ? `${age}s` : `${age}s (ahead)`}
                  </td>
                </tr>
              );
            })}
          </tbody>
        </table>
      </div>

      <p className="lede" style={{ marginTop: 12 }}>
        Chain reads, provider-derived values and scenario-generated history
        are three different kinds of evidence. Each row carries its own
        label.
      </p>

      <details>
        <summary>Content hashes, one per item</summary>
        <div className="card" style={{ display: "grid", gap: 10, marginTop: 8 }}>
          {ITEMS.map((key) => {
            const e = snapshot[key] as Evidence | undefined;
            return e ? (
              <Hash key={key} label={key} value={e.content_hash} />
            ) : null;
          })}
        </div>
      </details>
    </>
  );
}
