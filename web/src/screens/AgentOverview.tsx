import { useEffect, useState } from "react";
import { loadRun, units, bps, type Run } from "../lib/data";
import { classMode, ROUTE_MODES } from "../lib/modes";
import { ModeChip } from "../components/ModeChip";

/**
 * `docs/09` beat 4b: three sequential loans, same policy and model hash;
 * the limit rises, the APR falls, and the binding constraint switches from
 * history to stress capacity.
 *
 * The binding constraint is shown BESIDE the capacity, not in a details
 * panel. `docs/09` calls the plateau "the point, not a defect" -- a
 * capacity that stops rising is only explicable if the reader can see what
 * started limiting it.
 */
export function AgentOverview() {
  const [run, setRun] = useState<Run | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    loadRun("repay").then(setRun).catch((e: Error) => setError(e.message));
  }, []);

  if (error) return <div className="error">{error}</div>;
  if (!run) return <p className="lede">Loading retained evidence…</p>;

  const originations = run.assessments.filter(
    (a) => a.payload?.outcome?.decision === "ApprovedOrigination",
  );

  return (
    <>
      <h2>Agent overview</h2>
      <p className="lede">
        Three sequential loans under one policy and one model. The limit
        rises as history accumulates, the rate falls, and the constraint
        that bounds the facility changes.
      </p>

      <div className="modes">
        <ModeChip mode={ROUTE_MODES.Development} />
        {originations[0] && (
          <ModeChip mode={classMode(originations[0].payload.authorization_class)} />
        )}
      </div>

      <div className="card scroll">
        <table>
          <thead>
            <tr>
              <th>Loan</th>
              <th className="num">Requested</th>
              <th className="num">Approved</th>
              <th className="num">Capacity</th>
              <th>Bound by</th>
              <th className="num">APR</th>
            </tr>
          </thead>
          <tbody>
            {originations.map((a, i) => (
              <tr key={a.assessment_id}>
                <td>{i + 1}</td>
                <td className="num">{units(a.payload.outcome.requested_principal)}</td>
                <td className="num">{units(a.payload.outcome.approved_principal)}</td>
                <td className="num">{units(a.payload.outcome.capacity?.agent_capacity)}</td>
                <td>{a.payload.outcome.capacity?.agent_binding ?? "-"}</td>
                <td className="num">{bps(a.payload.outcome.terms?.apr_bps)}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>

      {originations.length === 0 && (
        <p className="lede">
          No originations in this run. Generate the bundle with{" "}
          <code>pnpm data</code> after <code>LOANS=3 scripts/demo-repay.sh</code>.
        </p>
      )}

      <details>
        <summary>Model and policy, identical across all three</summary>
        <pre>
{`model_hash   ${originations[0]?.model_hash ?? "-"}
policy_hash  ${originations[0]?.policy_hash ?? "-"}`}
        </pre>
      </details>
    </>
  );
}
