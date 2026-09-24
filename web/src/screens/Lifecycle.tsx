import { useEffect, useState } from "react";
import { loadRun, units, bps, type Run } from "../lib/data";
import { ModeChip } from "../components/ModeChip";
import { ROUTE_MODES } from "../lib/modes";
import { RecordHeader } from "../components/Record";

/** Millionths to a percentage, the scale forecasts are committed in. */
const ppm = (v: number) => `${(v / 10_000).toFixed(1)}%`;

/** Unix seconds to a readable UTC stamp. */
const at = (t: number) =>
  t > 0 ? new Date(t * 1000).toISOString().replace("T", " ").slice(0, 19) : "-";

/**
 * `docs/09` beats 3, 4 and 7: where the money went, what is owed, and what
 * happened at resolution.
 *
 * The forecast column is the part that matters and the part easiest to
 * fake. `docs/06` requires the probability to be **committed before
 * funding** and scored against a precisely defined event, so both the
 * commitment time and the resolution are shown — and an unresolved
 * forecast is shown as unresolved rather than dropped. A screen that
 * listed only resolved forecasts would let a borrower's record improve by
 * abandoning the ones going badly.
 */
export function Lifecycle() {
  const [run, setRun] = useState<Run | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    loadRun("repay").then(setRun).catch((e: Error) => setError(e.message));
  }, []);

  if (error) return <><RecordHeader title="Loan lifecycle" /><div className="error">{error}</div></>;
  if (!run) return <RecordHeader title="Loan lifecycle">Loading retained evidence…</RecordHeader>;

  const byForecast = new Map(run.outcomes.map((o) => [o.forecast_id, o]));
  const originations = run.assessments.filter(
    (a) => a.payload?.outcome?.decision === "ApprovedOrigination",
  );

  return (
    <>
      <RecordHeader title="Loan lifecycle">
        Each loan commits a forecast before it is funded. The forecast is
        scored when the loan resolves.
      </RecordHeader>

      <div className="modes">
        <ModeChip mode={ROUTE_MODES.Development} />
      </div>

      <h3>What was borrowed</h3>
      <div className="card scroll">
        <table>
          <thead>
            <tr>
              <th>Loan</th>
              <th className="num">Principal</th>
              <th className="num">APR</th>
              <th className="num">Term</th>
              <th className="num">Equity required</th>
            </tr>
          </thead>
          <tbody>
            {originations.map((a, i) => (
              <tr key={a.assessment_id}>
                <td>{i + 1}</td>
                <td className="num">{units(a.payload.outcome.approved_principal)}</td>
                <td className="num">{bps(a.payload.outcome.terms?.apr_bps)}</td>
                <td className="num">
                  {a.payload.outcome.terms
                    ? `${a.payload.outcome.terms.term_seconds / 86_400}d`
                    : "-"}
                </td>
                <td className="num">{units(a.payload.outcome.terms?.required_equity)}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>

      <h3 style={{ marginTop: 24 }}>Forecasts, committed before funding</h3>
      <div className="card scroll">
        <table>
          <thead>
            <tr>
              <th>Event</th>
              <th className="num">Committed</th>
              <th className="num">Probability</th>
              <th className="num">Horizon</th>
              <th>Resolution</th>
              <th className="num">Brier</th>
            </tr>
          </thead>
          <tbody>
            {run.forecasts.map((f) => {
              const o = byForecast.get(f.forecast_id);
              return (
                <tr key={f.forecast_id}>
                  <td>{f.event_definition}</td>
                  <td className="num">{at(f.committed_at)}</td>
                  <td className="num">{ppm(f.probability_ppm)}</td>
                  <td className="num">{f.horizon_seconds}s</td>
                  <td>
                    {o ? (
                      o.resolution
                    ) : (
                      <em>unresolved</em>
                    )}
                  </td>
                  <td className="num">{o ? ppm(o.brier_ppm) : "-"}</td>
                </tr>
              );
            })}
          </tbody>
        </table>
      </div>

      <p className="lede" style={{ marginTop: 12 }}>
        Unresolved forecasts stay on the list with no score. Dropping them
        would let a record improve by abandoning the forecasts going badly.
      </p>

      <details>
        <summary>The scored event, exactly as defined</summary>
        <pre>{JSON.stringify(run.forecasts[0]?.payload ?? {}, null, 1)}</pre>
      </details>
    </>
  );
}
