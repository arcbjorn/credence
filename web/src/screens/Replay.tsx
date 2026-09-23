import { useEffect, useState } from "react";
import { loadRun, loadPolicy, type Run } from "../lib/data";
import { loadEngine, MODEL_HASH, WASM_BYTES, type ReplayReport } from "../lib/engine";
import { Hash } from "../components/Hash";
import { RecordHeader } from "../components/Record";

/**
 * `docs/09` beat 8: replay a retained assessment.
 *
 * The engine runs HERE, in the reader's tab -- the same Rust compiled to
 * WebAssembly that the gateway and CLI use, not a reimplementation. A
 * viewer that agreed with a retained assessment for a reason the CLI did
 * not share would be worse than no viewer at all.
 *
 * **The tamper control is not decoration.** `docs/11`: "A MATCH is only
 * evidence if the tampered run produces a DIFFERENCE." Both buttons are
 * offered together for that reason -- a reader who only ever sees MATCH
 * has been shown nothing.
 */
export function Replay() {
  const [run, setRun] = useState<Run | null>(null);
  const [policy, setPolicy] = useState<unknown>(null);
  const [report, setReport] = useState<ReplayReport | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [ran, setRan] = useState<"clean" | "tampered" | null>(null);

  useEffect(() => {
    Promise.all([loadRun("monitor"), loadPolicy("monitor")])
      .then(([r, p]) => {
        setRun(r);
        setPolicy(p);
      })
      .catch((e: Error) => setError(e.message));
  }, []);

  const latest = run?.assessments.at(-1);
  const snapshot = latest ? run?.snapshots[latest.input_hash] : undefined;

  async function run_(tamper: boolean) {
    if (!latest || !snapshot || !policy) return;
    setBusy(true);
    setError(null);
    try {
      const engine = await loadEngine();

      let input: unknown = snapshot;
      if (tamper) {
        // One digit of the position's mark, exactly as the demo's control
        // does. Structured cloning keeps the retained snapshot pristine --
        // a tamper that mutated it would poison every later run.
        const copy = structuredClone(snapshot);
        const account = copy.account?.value as
          | { positions?: { price: string }[] }
          | undefined;
        const pos = account?.positions?.[0];
        if (!pos) throw new Error("this snapshot holds no position to alter");
        pos.price = String(BigInt(pos.price) - 100000000n);
        input = copy;
      }

      const result = engine.replay(
        JSON.stringify(latest.payload),
        JSON.stringify(input),
        JSON.stringify(policy),
      );
      setReport(result);
      setRan(tamper ? "tampered" : "clean");
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
      setReport(null);
    } finally {
      setBusy(false);
    }
  }

  if (error && !run) return <div className="error">{error}</div>;
  if (!run || !policy) return <p className="lede">Loading retained evidence…</p>;

  return (
    <>
      <RecordHeader title="Replay">
        Does the retained input reproduce the assessment under the pinned
        model? The engine runs in this tab: the same Rust the gateway uses,
        compiled to WebAssembly.
      </RecordHeader>

      <div className="card" style={{ marginBottom: 16 }}>
        <table>
          <tbody>
            <tr>
              <th>Engine</th>
              <td>{(WASM_BYTES / 1024).toFixed(0)} KB WebAssembly, running locally</td>
            </tr>
            <tr>
              <th>Model</th>
              <td><code className="hash">{MODEL_HASH}</code></td>
            </tr>
          </tbody>
        </table>
      </div>

      <p style={{ display: "flex", gap: 10, flexWrap: "wrap" }}>
        <button onClick={() => void run_(false)} disabled={busy}>
          Replay the retained assessment
        </button>
        <button onClick={() => void run_(true)} disabled={busy}>
          Replay with one digit altered
        </button>
      </p>

      {error && <div className="error">{error}</div>}

      {report && (
        <div style={{ display: "grid", gap: 12 }}>
          <div className={report.matched ? "match" : "difference"}>
            {report.matched ? "Reproduced" : "Did not reproduce"}
            {ran === "tampered" && report.matched && (
              <> — altered evidence produced the same assessment. This
              replay checks nothing.</>
            )}
            {ran === "tampered" && !report.matched && (
              <> — expected. The altered input is a different decision.</>
            )}
          </div>

          {report.differences.length > 0 && (
            <div className="card scroll">
              <table>
                <thead>
                  <tr><th>Field</th><th>Retained</th><th>Replayed</th></tr>
                </thead>
                <tbody>
                  {report.differences.map((d) => (
                    <tr key={d.field}>
                      <td>{d.field}</td>
                      <td className="hash">{d.retained}</td>
                      <td className="hash">{d.replayed}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}

        </div>
      )}
      {report && (
        <details>
          <summary>Identifiers for this replay</summary>
          <div className="card" style={{ display: "grid", gap: 10, marginTop: 8 }}>
            <Hash label="model_hash" value={MODEL_HASH} />
            <Hash label="assessment_id" value={report.assessment_id} />
            <Hash label="input_hash" value={report.input_hash} />
          </div>
        </details>
      )}
    </>
  );
}
