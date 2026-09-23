import { useEffect, useState } from "react";
import { loadComparison, units, bps, type Comparison } from "../lib/data";
import { bindingPhrase } from "../lib/bindings";
import { classMode } from "../lib/modes";
import { reasonFields, reasonName } from "../lib/reasons";
import { CreditGlobe } from "../components/CreditGlobe";
import { ModeChip } from "../components/ModeChip";
import { Hash } from "../components/Hash";
import { RecordHeader } from "../components/Record";

/**
 * `docs/09` beat 1: three accounts and the financing problem, plus beat 5,
 * where a good record is denied by the current portfolio.
 *
 * Every figure is the real engine's output under the shipped policy.
 * `emit_comparison` produces the fixture and
 * `comparison_fixture_matches_the_engine` refuses to let it drift, so a
 * number here is one the engine still returns rather than one it returned
 * once.
 *
 * **The three profiles differ by ONE thing at a time**, from a single
 * baseline. That is the whole discipline (`docs/03`): three accounts that
 * differed in several ways at once would show a difference without
 * attributing it to anything.
 */
export function Comparison_() {
  const [cmp, setCmp] = useState<Comparison | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    loadComparison().then(setCmp).catch((e: Error) => setError(e.message));
  }, []);

  if (error) return <div className="error">{error}</div>;
  if (!cmp) return <p className="lede">Loading the comparison…</p>;

  // One scale for all three objects. Normalising each to its own
  // capacity would draw the denied agent and the strongest one the same
  // size, which is the opposite of the comparison being made.
  const peak = Math.max(
    ...cmp.profiles.map((p) =>
      Number(BigInt(p.assessment.outcome.capacity?.agent_capacity ?? "0") / 1_000_000n),
    ),
  );

  return (
    <>
      <RecordHeader title="Three agents">
        One policy and one model against three accounts, each differing in
        a single respect: what they have repaid, and what they hold now.
      </RecordHeader>

      <div className="modes">
        {cmp.profiles[0] && (
          <ModeChip mode={classMode(cmp.profiles[0].assessment.authorization_class)} />
        )}
      </div>

      <div className="grid">
        {cmp.profiles.map((p) => {
          const o = p.assessment.outcome;
          const approved = o.decision === "ApprovedOrigination";
          return (
            <div
              className={`card rail ${approved ? "rail-growth" : "rail-alarm"}`}
              key={p.name}
            >
              <h3>{p.name}</h3>

              {/* Each agent as its own object, all three drawn to ONE
                  scale so they are comparable: the denied profile is a
                  ring with nothing inside it, which is the refusal
                  stated as geometry rather than as a red word. */}
              <CreditGlobe
                still
                peakOverride={peak}
                points={[
                  {
                    capacity: o.capacity?.agent_capacity ?? "0",
                    apr: o.terms?.apr_bps ?? 0,
                    binding: o.capacity?.agent_binding ?? "-",
                    ...(typeof o.capacity?.score === "number"
                      ? { score: o.capacity.score }
                      : {}),
                  },
                ]}
              />

              <p className="lede" style={{ minHeight: "4.5em" }}>{p.blurb}</p>
              <table>
                <tbody>
                  <tr>
                    <th>Decision</th>
                    <td>{approved ? "Approved" : "Denied"}</td>
                  </tr>
                  <tr>
                    <th>Capacity</th>
                    <td className="num">{units(o.capacity?.agent_capacity)}</td>
                  </tr>
                  <tr>
                    <th>Limited by</th>
                    <td title={o.capacity?.agent_binding ?? undefined}>
                      {bindingPhrase(o.capacity?.agent_binding)}
                    </td>
                  </tr>
                  <tr>
                    <th>APR</th>
                    <td className="num">
                      {o.terms ? bps(o.terms.apr_bps) : "-"}
                    </td>
                  </tr>
                </tbody>
              </table>
              {!approved && (
                <div className="difference" style={{ marginTop: 10 }}>
                  {p.assessment.reasons.length === 0 && "Denied"}
                  {p.assessment.reasons.map((r, i) => (
                    <div key={i}>
                      <div>{reasonName(r)}</div>
                      {reasonFields(r).map((f) => (
                        <div key={f.label} className="reason-field">
                          {f.label} <strong>{f.value}</strong>
                        </div>
                      ))}
                    </div>
                  ))}
                </div>
              )}
            </div>
          );
        })}
      </div>

      <h3 style={{ marginTop: 24 }}>What the comparison shows</h3>
      <div className="card">
        <p>
          History opens the facility: a demonstrated record moves capacity
          from the history cap to the account's hard cap. It does not
          override the book.
        </p>
        <p style={{ marginBottom: 0 }}>
          The third account has the <strong>same flawless record</strong> as
          the second and is refused anyway. Stress capacity binds before the
          history cap is ever reached.
        </p>
      </div>

      <details>
        <summary>Policy and model</summary>
        <div className="card" style={{ display: "grid", gap: 10, marginTop: 8 }}>
          <Hash label="policy_hash" value={cmp.policy_hash} />
          <Hash label="model_hash" value={cmp.model_hash} />
        </div>
      </details>
    </>
  );
}
