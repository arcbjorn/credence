import { useEffect, useState } from "react";
import { loadRun, units, bps, type Run } from "../lib/data";
import { bindingPhrase } from "../lib/bindings";
import { classMode, ROUTE_MODES } from "../lib/modes";
import { CapacityCurve } from "../components/CapacityCurve";
import { ModeChip } from "../components/ModeChip";
import { RecordHeader } from "../components/Record";

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
/**
 * Which register a binding constraint belongs to.
 *
 * `docs/09` requires the binding constraint beside the capacity, because
 * a number without it cannot be interpreted. The colour carries the same
 * distinction the word does: history GROWING the facility is a different
 * kind of fact from a structural cap taking over, and the plateau in
 * beat 4b is exactly that handover.
 */
function bindingRegister(binding: string | undefined): string {
  if (binding === "HistoryGrowthCap") return "growth";
  if (binding === undefined) return "";
  return "machine";
}

export function AgentOverview() {
  const [run, setRun] = useState<Run | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    loadRun("repay").then(setRun).catch((e: Error) => setError(e.message));
  }, []);

  if (error) return <><RecordHeader title="Agent overview" /><div className="error">{error}</div></>;
  if (!run) return <RecordHeader title="Agent overview">Loading retained evidence…</RecordHeader>;

  const originations = run.assessments.filter(
    (a) => a.payload?.outcome?.decision === "ApprovedOrigination",
  );

  return (
    <>
      <RecordHeader title="Agent overview">
        {originations.length > 0 ? (
          <>
            {originations.length} sequential{" "}
            {originations.length === 1 ? "loan" : "loans"} under one policy
            and one model. The limit rises as history accumulates, the rate
            falls, and the binding constraint changes.
          </>
        ) : (
          <>
            This run kept no originations.
          </>
        )}
      </RecordHeader>

      <div className="modes">
        <ModeChip mode={ROUTE_MODES.Development} />
        {originations[0] && (
          <ModeChip mode={classMode(originations[0].payload.authorization_class)} />
        )}
      </div>

      {/* The trajectory, before the figures that make it up. The landing
          page's object shows ONE state well; a series is what this
          screen is for, so the curve lives here. */}
      {originations.length > 1 && (
        <CapacityCurve
          points={originations.map((a) => ({
            capacity: a.payload.outcome.capacity?.agent_capacity ?? "0",
            apr: a.payload.outcome.terms?.apr_bps ?? 0,
            binding: a.payload.outcome.capacity?.agent_binding ?? "-",
          }))}
        />
      )}

      <div className="card scroll">
        <table>
          <thead>
            <tr>
              <th>Loan</th>
              <th className="num">Requested</th>
              <th className="num">Approved</th>
              <th className="num">Agent capacity</th>
              <th>Bound by</th>
              <th className="num">Drawable</th>
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
                <td
                  className={`bound-${bindingRegister(a.payload.outcome.capacity?.agent_binding)}`}
                  title={a.payload.outcome.capacity?.agent_binding ?? undefined}
                >
                  {bindingPhrase(a.payload.outcome.capacity?.agent_binding)}
                </td>
                <td className="num">{units(a.payload.outcome.capacity?.drawable_capacity)}</td>
                <td title={a.payload.outcome.capacity?.drawable_binding ?? undefined}>{bindingPhrase(a.payload.outcome.capacity?.drawable_binding)}</td>
                <td className="num">{bps(a.payload.outcome.terms?.apr_bps)}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>

      {originations.length === 0 && (
        <details>
          <summary>How to produce them</summary>
          <p>
            Generate the bundle with <code>pnpm data</code> after{" "}
            <code>LOANS=3 scripts/demo-repay.sh</code>.
          </p>
        </details>
      )}

      {originations.length > 0 && <details>
        <summary>
          Model and policy, identical across all {originations.length}
        </summary>
        <pre>
{`model_hash   ${originations[0]?.model_hash ?? "-"}
policy_hash  ${originations[0]?.policy_hash ?? "-"}`}
        </pre>
      </details>}
    </>
  );
}
