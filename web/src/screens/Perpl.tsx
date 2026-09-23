import { useEffect, useState } from "react";
import { bps, loadPerpl, loadPolicy, units, type Perpl } from "../lib/data";
import { bindingPhrase } from "../lib/bindings";
import { loadEngine, MODEL_HASH } from "../lib/engine";
import { Hash } from "../components/Hash";
import { RecordHeader } from "../components/Record";

/**
 * Live Perpl market data, driving the three mandatory stress scenarios.
 *
 * The engine runs HERE, in the reader's tab -- the same Rust compiled to
 * WebAssembly that the gateway and CLI use. The scenarios below are its
 * own output over observed market data, not a second implementation that
 * could quietly disagree with the one that authorizes loans.
 *
 * **Two snapshots, one clock.** The panel evaluates the same retained
 * account twice: once with the market Perpl reports, once with the
 * declared placeholder the fixture shipped. Only the market evidence
 * differs, so the difference in capacity is attributable to it and
 * nothing else. A single capacity number proves nothing without the
 * counterfactual it is being compared against.
 *
 * **Funding is displayed and never fed to the engine.** Perpl's funding
 * rate is the premium longs pay shorts to hold a perp near its index. The
 * engine's `funding_rate_bps` is the LENDER'S cost of capital, deducted
 * from gross interest in `pricing.rs`. They share a name and nothing
 * else, and wiring one into the other would silently corrupt loan
 * pricing. `docs/08` keeps market feedback informational; this is market
 * feedback.
 *
 * Nothing here is fetched by the page. `credence-perplpanel` captured it,
 * so the same rule holds as everywhere else: the capture produced the
 * file, or the screen renders nothing.
 */

interface StressRow {
  shock: string;
  applied_price_shock_bps: number;
  recoverable_value: string;
  passes: boolean;
}

interface Assessment {
  outcome?: {
    decision?: string;
    capacity?: {
      agent_capacity?: string;
      // Which constraint produced that number. Capacity is a min() over
      // several, so market evidence moves it ONLY when the stress term
      // is the one binding -- reading a zero delta as "the market is
      // deep enough" is wrong whenever something else binds first.
      agent_binding?: string;
      score?: number;
    };
  };
  stress_results?: StressRow[];
}

/** Unix seconds to a readable UTC stamp. */
const at = (t: number | undefined) =>
  t === undefined
    ? "-"
    : new Date(t * 1000).toISOString().replace("T", " ").slice(0, 19) + "Z";

/** A venue-scaled integer to a readable decimal. */
const scaled = (v: number, decimals: number) =>
  (v / 10 ** decimals).toLocaleString(undefined, {
    minimumFractionDigits: Math.min(decimals, 2),
    maximumFractionDigits: decimals,
  });

/** A signed bps reading, with its sign kept. */
const signedBps = (v: number) => `${v > 0 ? "+" : ""}${(v / 100).toFixed(2)}%`;

export function PerplPanel() {
  const [data, setData] = useState<Perpl | null>(null);
  const [observed, setObserved] = useState<Assessment | null>(null);
  const [baseline, setBaseline] = useState<Assessment | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let live = true;
    (async () => {
      try {
        const panel = await loadPerpl();
        if (!live) return;
        setData(panel);

        // The same engine and the same policy the run hashed. Loading a
        // different policy would make the comparison meaningless: the
        // engine refuses a declared policy hash that is not the content
        // hash of the policy it was handed.
        const [engine, policy] = await Promise.all([
          loadEngine(),
          loadPolicy("repay"),
        ]);
        if (!live) return;

        const policyJSON = JSON.stringify(policy);
        const run = (snapshot: unknown): Assessment =>
          JSON.parse(
            engine.evaluate(JSON.stringify(snapshot), policyJSON),
          ) as Assessment;

        setObserved(run(panel.snapshot));
        setBaseline(run(panel.baseline));
      } catch (e) {
        if (live) setError(e instanceof Error ? e.message : String(e));
      }
    })();
    return () => {
      live = false;
    };
  }, []);

  if (error) {
    return (
      <section>
        <RecordHeader title="Perpl market risk">No capture to read.</RecordHeader>
        <div className="error">{error}</div>
        <details><summary>How to capture it</summary><p>
          Refresh the capture with{" "}
          <code>bash scripts/build-perpl-data.sh</code>, which reads the live
          venue and writes <code>web/public/data/perpl.json</code>.
        </p></details>
      </section>
    );
  }

  if (!data) {
    return (
      <section>
        <RecordHeader title="Perpl market risk">Reading the capture…</RecordHeader>
      </section>
    );
  }

  const obsCap = observed?.outcome?.capacity?.agent_capacity;
  const baseCap = baseline?.outcome?.capacity?.agent_capacity;
  const binding = observed?.outcome?.capacity?.agent_binding;
  // Capacity is a min() over several constraints. Only StressCapacity
  // reads the market, so anything else binding means the market cannot
  // move this number no matter how thin the book is.
  const stressBinds = binding === "StressCapacity";
  const delta =
    obsCap && baseCap && BigInt(baseCap) !== 0n
      ? Number(((BigInt(obsCap) - BigInt(baseCap)) * 10000n) / BigInt(baseCap)) /
        100
      : null;

  return (
    <section>
      <RecordHeader title="Perpl market risk">
        Observed market depth and volatility for{" "}
        <strong>
          {data.venue.symbol} (market {data.venue.market_id})
        </strong>{" "}
        on Perpl, driving the three mandatory stress scenarios. The engine
        itself runs in this tab, compiled to WebAssembly.
      </RecordHeader>

      <div className="modes">
        <span className="mode mode-observed">market: observed</span>
        <span className="mode mode-declared">funding: displayed only</span>
        <span className="mode mode-unknown">
          captured {at(data.captured_at)}
        </span>
      </div>

      <div className="grid">
        <div className="card">
          <table>
            <tbody>
              <tr>
                <th>Realized volatility</th>
                <td className="num">{bps(data.market.realized_volatility_bps)}</td>
              </tr>
              <tr>
                <th>Window</th>
                <td className="num">
                  {data.market.volatility_window_seconds / 3600}h at{" "}
                  {data.venue.resolution_seconds}s candles
                </td>
              </tr>
              <tr>
                <th>Observed at</th>
                <td className="num">{at(data.market.observed_at)}</td>
              </tr>
            </tbody>
          </table>
        </div>

        <div className="card">
          <table>
            <tbody>
              <tr>
                <th>Liquidation depth</th>
                <td className="num">{units(data.market.liquidity_depth)}</td>
              </tr>
              <tr>
                <th>Summed from</th>
                <td className="num">{data.book.bid_levels} bid levels</td>
              </tr>
            </tbody>
          </table>
        </div>

        <div className="card">
          <table>
            <tbody>
              <tr>
                <th>Funding, one-sided</th>
                <td className="num">{data.funding.one_sided_pct}%</td>
              </tr>
              <tr>
                <th>Mean / latest</th>
                <td className="num">
                  {signedBps(data.funding.mean_bps)} /{" "}
                  {signedBps(data.funding.latest_bps)}
                </td>
              </tr>
              <tr>
                <th>Samples</th>
                <td className="num">
                  {data.funding.samples} over{" "}
                  {Math.round(data.funding.window_seconds / 86400)}d
                </td>
              </tr>
            </tbody>
          </table>
        </div>
      </div>

      <p className="note">
        <strong>Funding is displayed, not consumed.</strong> Perpl&rsquo;s
        funding rate is the premium longs pay shorts. The engine&rsquo;s{" "}
        <code>funding_rate_bps</code> is the lender&rsquo;s cost of capital,
        deducted from gross interest. Same name, unrelated quantities; wiring
        one into the other would corrupt loan pricing. Persistent one-sided
        funding means crowded positioning, which unwinds faster than it
        builds up. Calm volatility under crowded funding is not calm.
      </p>

      <h3>The book that depth was summed from</h3>
      <div className="scroll">
        <table>
          <thead>
            <tr>
              <th>Bid price</th>
              <th>Bid size</th>
              <th>Ask price</th>
              <th>Ask size</th>
            </tr>
          </thead>
          <tbody>
            {Array.from({
              length: Math.max(data.book.bids.length, data.book.asks.length),
            }).map((_, i) => {
              const bid = data.book.bids[i];
              const ask = data.book.asks[i];
              return (
                <tr key={i}>
                  <td className="num">
                    {bid ? scaled(bid.price, data.book.price_decimals) : ""}
                  </td>
                  <td className="num">
                    {bid ? scaled(bid.size, data.book.size_decimals) : ""}
                  </td>
                  <td className="num">
                    {ask ? scaled(ask.price, data.book.price_decimals) : ""}
                  </td>
                  <td className="num">
                    {ask ? scaled(ask.size, data.book.size_decimals) : ""}
                  </td>
                </tr>
              );
            })}
          </tbody>
        </table>
      </div>
      <p className="note">
        Depth sums the <strong>bid</strong> side only. Liquidating collateral
        sells into bids; counting asks would report depth that cannot absorb
        the trade. The stress model divides by depth in a square-root impact
        term. Overstate depth and the model understates impact, which
        over-lends.
      </p>

      <h3>The same account, two markets</h3>
      {!observed || !baseline ? (
        <p className="empty">Evaluating&hellip;</p>
      ) : (
        <>
          <p className="note">
            The <code>{data.snapshot_source}</code> account, policy and clock
            stay fixed. Only market evidence changes.
          </p>
          <div className="scroll">
            <table>
              <thead>
                <tr>
                  <th>Scenario</th>
                  <th>Declared market</th>
                  <th>Perpl, observed</th>
                </tr>
              </thead>
              <tbody>
                <tr>
                  <th>Depth used</th>
                  <td className="num">
                    {units(data.baseline_market.liquidity_depth)}
                  </td>
                  <td className="num">{units(data.market.liquidity_depth)}</td>
                </tr>
                <tr>
                  <th>Volatility used</th>
                  <td className="num">
                    {bps(data.baseline_market.realized_volatility_bps)}
                  </td>
                  <td className="num">
                    {bps(data.market.realized_volatility_bps)}
                  </td>
                </tr>
                {(baseline.stress_results ?? []).map((row, i) => {
                  const obs = observed.stress_results?.[i];
                  return (
                    <tr key={row.shock}>
                      <th>
                        {row.shock} &middot; shock{" "}
                        {bps(row.applied_price_shock_bps)}
                      </th>
                      <td className="num">
                        {units(row.recoverable_value)}{" "}
                        {row.passes ? "" : "(fails)"}
                      </td>
                      <td className="num">
                        {units(obs?.recoverable_value)}{" "}
                        {obs?.passes ? "" : "(fails)"}
                      </td>
                    </tr>
                  );
                })}
                <tr>
                  <th>
                    Agent capacity
                    {binding && (
                      <span className="muted" title={binding}>
                        {" "}&middot; limited by {bindingPhrase(binding)}
                      </span>
                    )}
                  </th>
                  <td className="num">{units(baseCap)}</td>
                  <td className="num">
                    {units(obsCap)}
                    {delta !== null && (
                      <>
                        {" "}
                        ({delta > 0 ? "+" : ""}
                        {delta.toFixed(1)}%)
                      </>
                    )}
                  </td>
                </tr>
                <tr>
                  <th>Decision</th>
                  <td className="num">{baseline.outcome?.decision ?? "-"}</td>
                  <td className="num">{observed.outcome?.decision ?? "-"}</td>
                </tr>
              </tbody>
            </table>
          </div>

          <p className="note">
            {delta !== null && delta < 0 ? (
              <>
                Observed depth reduces stress recovery and capacity falls{" "}
                {Math.abs(delta).toFixed(1)}%.
              </>
            ) : !stressBinds ? (
              <>
                <strong>
                  Capacity is unchanged because the limit is set by{" "}
                  <span title={binding}>{bindingPhrase(binding)}</span>, not by
                  the market.
                </strong>{" "}
                Only stress capacity uses market evidence. An unchanged limit
                does not establish adequate market depth.
              </>
            ) : (
              <>
                The observed market is at least as deep as the declared
                placeholder, so capacity does not fall.
              </>
            )}{" "}
            Both columns use the policy's minimum price shock because scaled
            volatility falls below that floor.
          </p>
          <details><summary>Model identifier</summary><Hash value={MODEL_HASH} label="engine" /></details>
        </>
      )}
    </section>
  );
}
