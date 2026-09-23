import { useEffect, useState } from "react";
import { Link } from "react-router";
import { bps, loadRun, units, type Run } from "../lib/data";
import { bindingPhrase } from "../lib/bindings";
import { curve, earned, plot, type Point } from "../lib/earned";
import { RecordHeader } from "../components/Record";

/**
 * What a record earns, as one run.
 *
 * `scripts/demo-growth.sh` originates the same principal eight times
 * against one agent and holds everything else still -- same policy, same
 * market, same asset, same term. The only input that moves is the
 * agent's own repayment record, so the rate it is quoted moves for
 * exactly one reason.
 *
 * This is the project's central claim, and until this run existed it was
 * spread thinly across loans that differed in several ways at once. A
 * reader could believe it or not; they could not check it.
 */

const W = 640;
const H = 180;

function Curve({
  points,
  pick,
  label,
  format,
  tone,
}: {
  points: Point[];
  pick: (p: Point) => number;
  label: string;
  format: (v: number) => string;
  tone: "growth" | "machine";
}) {
  const laid = plot(points, pick, W, H, 10);
  const stroke = tone === "growth" ? "var(--growth)" : "var(--machine)";
  return (
    <div className="card">
      <h3 style={{ marginTop: 0 }}>{label}</h3>
      <svg
        viewBox={`0 0 ${W} ${H}`}
        width="100%"
        height={H}
        role="img"
        aria-label={`${label}: ${format(laid.min)} to ${format(laid.max)} over ${points.length} loans`}
        style={{ display: "block", overflow: "visible" }}
      >
        <path d={laid.path} fill="none" stroke={stroke} strokeWidth={2} />
        {laid.points.map(({ x, y, point }) => (
          <g key={point.sequence}>
            <circle cx={x} cy={y} r={3.5} fill={stroke}>
              <title>
                {`loan ${point.sequence}: ${format(pick(point))}, bound by ${point.binding}`}
              </title>
            </circle>
          </g>
        ))}
      </svg>
      <div
        className="lede"
        style={{ display: "flex", justifyContent: "space-between", margin: 0 }}
      >
        <span>
          loan {points[0]?.sequence}: {format(pick(points[0]!))}
        </span>
        <span>
          loan {points[points.length - 1]?.sequence}:{" "}
          {format(pick(points[points.length - 1]!))}
        </span>
      </div>
    </div>
  );
}

export function Earned() {
  const [run, setRun] = useState<Run | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    loadRun("growth")
      .then(setRun)
      .catch((e: Error) => setError(e.message));
  }, []);

  if (error) {
    return (
      <>
        <RecordHeader title="What a record earns">
          This run has not been recorded.
        </RecordHeader>
        <div className="error">{error}</div>
        <details>
          <summary>How to produce it</summary>
          <p>
            Run <code>scripts/demo-growth.sh</code> against a local chain,
            then <code>pnpm data</code>.
          </p>
        </details>
      </>
    );
  }
  if (!run) {
    return (
      <RecordHeader title="What a record earns">
        Loading the retained run…
      </RecordHeader>
    );
  }

  const points = curve(run);
  const summary = earned(points);

  if (!summary) {
    return (
      <>
        <RecordHeader title="What a record earns">
          This run priced fewer than two originations, so it shows no
          trend. One loan is not a curve.
        </RecordHeader>
      </>
    );
  }

  return (
    <>
      <RecordHeader title="What a record earns">
        The same request, eight times, from one agent. Principal, term,
        market and policy are held still; only the agent's repayment
        record moves. So the price moves for one reason.
      </RecordHeader>

      <div className="grid">
        <div className="card rail rail-growth">
          <h3>Rate earned</h3>
          <p className="figure figure-growth">
            {summary.discount_bps > 0 ? "−" : ""}
            {bps(Math.abs(summary.discount_bps))}
          </p>
          <p className="lede" style={{ margin: 0 }}>
            From {bps(summary.first.apr_bps)} to {bps(summary.last.apr_bps)} on
            record alone.
          </p>
        </div>
        <div className="card rail rail-machine">
          <h3>Ceiling</h3>
          <p className="figure">{units(summary.last.capacity)}</p>
          <p className="lede" style={{ margin: 0 }}>
            Up from {units(summary.first.capacity)}, now set by{" "}
            <span title={summary.last.binding}>
              {bindingPhrase(summary.last.binding)}
            </span>
            .
          </p>
        </div>
        <div className="card">
          <h3>Held constant</h3>
          <p className="figure">{units(summary.first.principal)}</p>
          <p className="lede" style={{ margin: 0 }}>
            {summary.constant_principal ? (
              <>
                The same principal every time, so the discount is
                attributable to the record and nothing else.
              </>
            ) : (
              <>
                <strong>The principal changed across this run</strong>, so
                the rate moved for more than one reason.
              </>
            )}
          </p>
        </div>
      </div>

      <div className="grid" style={{ marginTop: 20 }}>
        <Curve
          points={points}
          pick={(p) => p.apr_bps}
          label="The rate it is quoted"
          format={(v) => bps(v)}
          tone="growth"
        />
        <Curve
          points={points}
          pick={(p) => Number(p.capacity) / 1e9}
          label="The ceiling it is granted"
          format={(v) => `${v.toFixed(2)}B`}
          tone="machine"
        />
      </div>

      <h3 style={{ marginTop: 28 }}>Every loan in the sequence</h3>
      <div className="card scroll">
        <table>
          <thead>
            <tr>
              <th>Loan</th>
              <th className="num">Record</th>
              <th className="num">APR</th>
              <th className="num">Capacity</th>
              <th>Bound by</th>
            </tr>
          </thead>
          <tbody>
            {points.map((p) => (
              <tr key={p.sequence}>
                <td>{p.sequence}</td>
                <td className="num">
                  {p.repayments_ppm === null
                    ? "—"
                    : `${(p.repayments_ppm / 10_000).toFixed(1)}%`}
                </td>
                <td className="num">{bps(p.apr_bps)}</td>
                <td className="num">{units(p.capacity)}</td>
                <td title={p.binding}>{bindingPhrase(p.binding)}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>

      {summary.bindings.length > 1 && (
        <>
          <h3 style={{ marginTop: 28 }}>The limit changes hands</h3>
          <p className="lede">
            Early loans are capped by how little the agent has proven. Once
            the record is deep enough the growth cap stops binding, and what
            limits the agent instead is what its book survives in a crash.
            The ceiling flattens there — an earned limit is bounded by
            solvency, not extrapolated forever.
          </p>
          <div className="card">
            {summary.bindings.map((b) => (
              <div key={b} style={{ marginBottom: 6 }}>
                <code className="hash">{b}</code>{" "}
                <span className="lede">the limit is set by {bindingPhrase(b)}</span>
              </div>
            ))}
          </div>
        </>
      )}

      <div className="card rail rail-machine" style={{ marginTop: 20 }}>
        <p className="lede" style={{ margin: 0 }}>
          Every figure here is an engine output retained by the run, not a
          fitted line. Ask the engine your own question at the{" "}
          <Link to="/borrow">borrow desk</Link>, or watch a record go the
          other way in the <Link to="/lender">lender's</Link> defaulted
          epoch.
        </p>
      </div>
    </>
  );
}
