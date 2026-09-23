import { useEffect, useMemo, useState } from "react";
import { Link } from "react-router";
import { bps, loadPolicy, loadRun, units, type Run } from "../lib/data";
import { bindingPhrase } from "../lib/bindings";
import { loadEngine, MODEL_HASH } from "../lib/engine";
import { reasonFields, reasonName } from "../lib/reasons";
import {
  applyRequest,
  approved,
  INERT_INPUTS,
  interestOver,
  LEVERS,
  priceRows,
  readQuote,
  requestFromSnapshot,
  type Quote,
  type Request,
} from "../lib/borrow";
import { Hash } from "../components/Hash";
import { RecordHeader } from "../components/Record";

/**
 * The borrow desk.
 *
 * Every other record shows a decision that was already made. Here the
 * reader composes the request and the engine answers it -- the same Rust
 * the gateway runs, compiled to WebAssembly, in their own tab.
 *
 * **The page still computes nothing.** It edits fields of a RETAINED
 * snapshot and renders what `evaluate` returns. There is no fallback
 * branch where the interface estimates a rate the engine did not quote:
 * if the engine declines to evaluate, the decline is what is shown.
 *
 * **Why this is not a loan.** Nothing here reaches a chain and no
 * counterparty is bound. A quote from the desk is what the engine WOULD
 * authorize; originating against it needs an attestor signature, which
 * `docs/05` keeps with the operator and which is not, and must not be,
 * in the browser. The record below says so where a reader will be
 * deciding what they have just seen.
 */

/** The term the retained request carried, for the interest line. */
const termOf = (snapshot: unknown): number => {
  const s = snapshot as { context?: { request?: { term_seconds?: number } } };
  return s.context?.request?.term_seconds ?? 864_000;
};

function Decision({ quote }: { quote: Quote }) {
  if (quote.unavailable) {
    return (
      <div className="card rail rail-machine">
        <h3>The engine did not evaluate</h3>
        <p className="lede" style={{ margin: 0 }}>
          Not a refusal of credit. The snapshot or the policy was rejected
          before any decision was reached, so there is no answer to read.
        </p>
      </div>
    );
  }
  const ok = approved(quote);
  return (
    <div className={`card rail ${ok ? "rail-growth" : "rail-alarm"}`}>
      <h3>{ok ? "Approved" : "Refused"}</h3>
      <p className={`figure ${ok ? "figure-growth" : "figure-alarm"}`}>
        {ok ? bps(quote.apr_bps) : "no terms"}
      </p>
      <p className="lede" style={{ margin: 0 }}>
        {ok ? (
          <>
            {units(quote.approved_principal)} settlement at{" "}
            {bps(quote.apr_bps)} APR.
          </>
        ) : (
          <>
            Above what this record supports. The engine refuses rather than
            quoting a worse price.
          </>
        )}
      </p>
    </div>
  );
}

export function Borrow() {
  const [run, setRun] = useState<Run | null>(null);
  const [policy, setPolicy] = useState<unknown>(null);
  const [evaluate, setEvaluate] =
    useState<((input: string, policy: string) => string) | null>(null);
  const [request, setRequest] = useState<Request | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let live = true;
    (async () => {
      try {
        // The repay run's first origination: a real request, against a
        // real record, under the policy that run actually hashed. Loading
        // a different policy would make the engine refuse to evaluate --
        // it checks the declared hash against the policy it is handed.
        const [bundle, pol, engine] = await Promise.all([
          loadRun("repay"),
          loadPolicy("repay"),
          loadEngine(),
        ]);
        if (!live) return;
        setRun(bundle);
        setPolicy(pol);
        // Stored via an updater, or React calls the function as an
        // initializer and stores its return value instead.
        setEvaluate(() => engine.evaluate);
        const first = bundle.assessments[0];
        const snapshot = first ? bundle.snapshots[first.input_hash] : undefined;
        if (snapshot) setRequest(requestFromSnapshot(snapshot));
      } catch (e) {
        if (live) setError(e instanceof Error ? e.message : String(e));
      }
    })();
    return () => {
      live = false;
    };
  }, []);

  const first = run?.assessments[0];
  const snapshot = first ? run?.snapshots[first.input_hash] : undefined;
  const retained = useMemo(
    () => (snapshot ? requestFromSnapshot(snapshot) : null),
    [snapshot],
  );

  /**
   * The engine's answer to the current request.
   *
   * Recomputed synchronously on every change. The engine is a ~140KB
   * WebAssembly module answering in well under a millisecond, so there is
   * no debounce and no pending state: the number under the slider is
   * always the answer to the slider's current position.
   */
  const quote = useMemo<Quote | null>(() => {
    if (!evaluate || !snapshot || !request || !policy) return null;
    try {
      const input = applyRequest(snapshot, request);
      return readQuote(evaluate(JSON.stringify(input), JSON.stringify(policy)));
    } catch (e) {
      // A throw from the engine is a malformed input, not a refusal.
      setError(e instanceof Error ? e.message : String(e));
      return null;
    }
  }, [evaluate, snapshot, request, policy]);

  if (error) {
    return (
      <>
        <RecordHeader title="Borrow desk" />
        <div className="error">{error}</div>
      </>
    );
  }
  if (!run || !request || !retained) {
    return (
      <RecordHeader title="Borrow desk">
        Loading the engine and a retained request…
      </RecordHeader>
    );
  }

  const set = (id: keyof Request, value: number) =>
    setRequest((r) =>
      r === null
        ? r
        : {
            ...r,
            // The principal stays a string all the way to the engine: it
            // is a uint256 in base units, and the engine refuses an
            // integer there rather than silently accepting a double.
            [id]: id === "requested_principal" ? String(value) : value,
          },
    );

  const changed =
    JSON.stringify(request) !== JSON.stringify(retained);
  const term = snapshot ? termOf(snapshot) : 864_000;

  return (
    <>
      <RecordHeader title="Borrow desk">
        Compose a request and the engine prices it here, in this tab. The
        controls edit a retained snapshot; every figure below is what the
        engine returned for the edited one.
      </RecordHeader>

      <div className="grid">
        {quote && <Decision quote={quote} />}

        <div className="card rail rail-machine">
          <h3>Capacity</h3>
          <p className="figure">
            {quote?.agent_capacity ? units(quote.agent_capacity) : "-"}
          </p>
          <p className="lede" style={{ margin: 0 }}>
            The ceiling for this account, set by{" "}
            <span title={quote?.agent_binding ?? undefined}>
              {bindingPhrase(quote?.agent_binding)}
            </span>
            .
          </p>
        </div>

        <div className="card rail rail-growth">
          <h3>Interest over the term</h3>
          <p className="figure figure-growth">
            {quote && approved(quote) && quote.apr_bps !== undefined
              ? units(
                  interestOver(
                    quote.approved_principal ?? "0",
                    quote.apr_bps,
                    term,
                  ),
                )
              : "-"}
          </p>
          <p className="lede" style={{ margin: 0 }}>
            Simple actual/365 over {Math.round(term / 86_400)} days,{" "}
            <strong>computed here</strong> from the quoted rate — not a
            figure the engine returned.
          </p>
        </div>
      </div>

      <h3 style={{ marginTop: 28 }}>The request</h3>
      <div className="grid">
        {LEVERS.map((lever) => {
          const raw = request[lever.id];
          const value = typeof raw === "string" ? Number(raw) : raw;
          return (
            <div className="card" key={lever.id}>
              <label htmlFor={`lever-${lever.id}`}>
                <strong>{lever.label}</strong>
              </label>
              <p className="figure" style={{ margin: "6px 0" }}>
                {lever.format(value)}
              </p>
              <input
                id={`lever-${lever.id}`}
                type="range"
                min={lever.min}
                max={lever.max}
                step={lever.step}
                value={value}
                onChange={(e) => set(lever.id, Number(e.target.value))}
                style={{ width: "100%" }}
              />
              <p className="lede" style={{ margin: "6px 0 0" }}>
                {lever.help}
              </p>
            </div>
          );
        })}
      </div>

      <p className="lede" style={{ marginTop: 12 }}>
        {changed ? (
          <>
            This is an edited snapshot.{" "}
            <button
              className="action action-small"
              onClick={() => setRequest(retained)}
            >
              Restore the retained request
            </button>
          </>
        ) : (
          <>
            This is the request the retained loan actually made. Move a
            control to ask a different question.
          </>
        )}
      </p>

      {quote && !quote.unavailable && approved(quote) && (
        <>
          <h3 style={{ marginTop: 28 }}>How the price was reached</h3>
          <div className="card scroll">
            <table>
              <thead>
                <tr>
                  <th>Term</th>
                  <th className="num">Rate</th>
                </tr>
              </thead>
              <tbody>
                {priceRows(quote).map((row) => (
                  <tr key={row.label}>
                    <td>{row.label}</td>
                    <td
                      className={`num ${
                        row.kind === "discount" ? "figure-growth" : ""
                      }`}
                    >
                      {row.bps === 0 && row.kind !== "total"
                        ? "—"
                        : `${row.bps < 0 ? "−" : ""}${bps(Math.abs(row.bps))}`}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
            {quote.price_breakdown?.clamped && (
              <p className="lede" style={{ margin: "8px 0 0" }}>
                The rate was clamped by policy, so the terms above do not
                sum to it.
              </p>
            )}
          </div>
        </>
      )}

      {/*
        Only for an APPROVED quote. On a refusal the engine still returns
        stress rows, but `debt_with_reserved_interest` is zero -- no loan
        was priced, so there is no debt to cover and every scenario
        trivially "survives". Rendering those beside a refusal reads as
        the request having passed the stress test it was refused over.
      */}
      {quote && approved(quote) && quote.stress && quote.stress.length > 0 && (
        <>
          <h3 style={{ marginTop: 28 }}>What it survives</h3>
          <p className="lede">
            The engine stresses the book before lending into it. Every
            scenario must leave enough to cover the debt.
          </p>
          <div className="card scroll">
            <table>
              <thead>
                <tr>
                  <th>Shock</th>
                  <th className="num">Applied</th>
                  <th className="num">Recoverable</th>
                  <th className="num">Debt</th>
                  <th>Result</th>
                </tr>
              </thead>
              <tbody>
                {quote.stress.map((s) => (
                  <tr key={s.shock}>
                    <td>{s.shock}</td>
                    <td className="num">{bps(s.applied_price_shock_bps)}</td>
                    <td className="num">{units(s.recoverable_value)}</td>
                    <td className="num">
                      {units(s.debt_with_reserved_interest)}
                    </td>
                    <td className={s.passes ? "figure-growth" : "figure-alarm"}>
                      {s.passes ? "survives" : "breaks"}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </>
      )}

      {quote && quote.reasons.length > 0 && (
        <>
          <h3 style={{ marginTop: 28 }}>What the engine said</h3>
          <div className="card">
            {quote.reasons.map((reason, i) => (
              <div key={i} style={{ marginBottom: 8 }}>
                <code className="hash">{reasonName(reason)}</code>
                {reasonFields(reason).map((f) => (
                  <div className="lede" key={f.label} style={{ margin: 0 }}>
                    {f.label}: {f.value}
                  </div>
                ))}
              </div>
            ))}
          </div>
        </>
      )}

      <h3 style={{ marginTop: 28 }}>What this is, exactly</h3>
      <div className="card rail rail-alarm">
        <p className="lede" style={{ marginTop: 0 }}>
          <strong>This is a quote, not a loan.</strong> Nothing here
          touches a chain and no counterparty is bound. Originating
          against a quote needs an attestor signature, which stays with
          the operator (<code>docs/05</code>) and is deliberately not in
          your browser.
        </p>
        <p className="lede" style={{ marginBottom: 0 }}>
          The snapshot you are editing was retained from a real run, and
          its evidence hashes are left <strong>stale on purpose</strong>:
          an edited input should not carry a hash that agrees with it. To
          check an <em>unedited</em> assessment reproduces byte for byte,
          use the <Link to="/replay">replay record</Link>.
        </p>
      </div>

      <details style={{ marginTop: 16 }}>
        <summary>Inputs that are deliberately not controls</summary>
        <div className="card" style={{ marginTop: 8 }}>
          <p className="lede" style={{ marginTop: 0 }}>
            Each of these was checked against the engine and moves nothing
            for this account. A control that did nothing would teach you
            the engine ignores an input it does not ignore.
          </p>
          {INERT_INPUTS.map((item) => (
            <div key={item.label} style={{ marginBottom: 10 }}>
              <strong>{item.label}</strong>
              <div className="lede" style={{ margin: 0 }}>{item.why}</div>
            </div>
          ))}
        </div>
      </details>

      <details style={{ marginTop: 12 }}>
        <summary>What is running</summary>
        <div className="card" style={{ marginTop: 8 }}>
          <Hash label="model hash" value={MODEL_HASH} />
          <p className="lede" style={{ marginBottom: 0 }}>
            The same engine build the gateway and the CLI use. The{" "}
            <Link to="/replay">replay record</Link> shows a retained
            assessment reproducing under it, and a tampered one failing.
          </p>
        </div>
      </details>
    </>
  );
}
