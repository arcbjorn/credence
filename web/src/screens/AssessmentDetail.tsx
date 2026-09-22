import { useEffect, useState } from "react";
import { loadRun, units, bps, type Assessment, type Run } from "../lib/data";
import { bindingPhrase } from "../lib/bindings";
import { classMode } from "../lib/modes";
import { reasonText } from "../lib/reasons";
import { ModeChip } from "../components/ModeChip";
import { Hash } from "../components/Hash";
import { RecordHeader } from "../components/Record";

/**
 * `docs/09`: "Which constraints bound this decision, and what evidence
 * supports them?"
 *
 * Leads with amounts, terms and reasons; hashes go in details. The price
 * breakdown is shown as its components rather than a single rate, because
 * a rate the reader cannot decompose is a number they have to trust.
 */
export function AssessmentDetail() {
  const [run, setRun] = useState<Run | null>(null);
  const [selected, setSelected] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    loadRun("repay")
      .then((r) => {
        setRun(r);
        setSelected(r.assessments.at(-1)?.assessment_id ?? null);
      })
      .catch((e: Error) => setError(e.message));
  }, []);

  if (error) return <div className="error">{error}</div>;
  if (!run) return <p className="lede">Loading retained evidence…</p>;

  const a: Assessment | undefined =
    run.assessments.find((x) => x.assessment_id === selected) ?? run.assessments[0];
  if (!a) return <p className="lede">This run retained no assessments.</p>;

  const { outcome } = a.payload;
  const pb = outcome.terms?.price_breakdown;

  return (
    <>
      <RecordHeader title="Assessment detail">
        What bounded this decision, and the evidence behind it.
      </RecordHeader>

      <div className="modes">
        <ModeChip mode={classMode(a.payload.authorization_class)} />
      </div>

      <p>
        <label>
          Assessment{" "}
          <select
            value={a.assessment_id}
            onChange={(e) => setSelected(e.target.value)}
          >
            {run.assessments.map((x, i) => (
              <option key={x.assessment_id} value={x.assessment_id}>
                #{i + 1} — {x.payload.outcome.decision}
              </option>
            ))}
          </select>
        </label>
      </p>

      <div className="grid">
        <div className="card">
          <h3>Decision</h3>
          <table>
            <tbody>
              <tr><th>Outcome</th><td>{outcome.decision}</td></tr>
              <tr><th>Requested</th><td className="num">{units(outcome.requested_principal)}</td></tr>
              <tr><th>Approved</th><td className="num">{units(outcome.approved_principal)}</td></tr>
              <tr><th>Reasons</th><td>{a.payload.reasons.map(reasonText).join(", ") || "-"}</td></tr>
            </tbody>
          </table>
        </div>

        <div className="card">
          <h3>What bounded it</h3>
          <table>
            <tbody>
              <tr><th>Agent capacity</th><td className="num">{units(outcome.capacity?.agent_capacity)}</td></tr>
              <tr><th>Limited by</th><td title={outcome.capacity?.agent_binding ?? undefined}>{bindingPhrase(outcome.capacity?.agent_binding)}</td></tr>
              <tr><th>Drawable</th><td className="num">{units(outcome.capacity?.drawable_capacity)}</td></tr>
              <tr><th>Limited by</th><td title={outcome.capacity?.drawable_binding ?? undefined}>{bindingPhrase(outcome.capacity?.drawable_binding)}</td></tr>
            </tbody>
          </table>
        </div>

        {pb && (
          <div className="card">
            <h3>How the rate was built</h3>
            <table>
              <tbody>
                <tr><th>Base rate</th><td className="num">{bps(pb.base_rate_bps)}</td></tr>
                <tr><th>History discount</th><td className="num">-{bps(pb.history_discount_bps)}</td></tr>
                <tr><th>Portfolio surcharge</th><td className="num">+{bps(pb.portfolio_surcharge_bps)}</td></tr>
                <tr><th>Offered APR</th><td className="num"><strong>{bps(pb.apr_bps)}</strong></td></tr>
                {pb.clamped && (
                  <tr><th>Clamped</th><td>yes — the schedule's bound applied</td></tr>
                )}
              </tbody>
            </table>
          </div>
        )}
      </div>

      <details>
        <summary>Identifiers</summary>
        <div className="card" style={{ display: "grid", gap: 10, marginTop: 8 }}>
          <Hash label="assessment_id" value={a.assessment_id} />
          <Hash label="input_hash" value={a.input_hash} />
          <Hash label="model_hash" value={a.model_hash} />
          <Hash label="policy_hash" value={a.policy_hash} />
        </div>
      </details>
    </>
  );
}
