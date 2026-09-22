import { useEffect, useState } from "react";
import { Link } from "react-router";
import { loadRun, units, type Check, type Run } from "../lib/data";
import { bindingPhrase } from "../lib/bindings";
import { CLAIMS, EARNED, REFUSED, UNEARNED, type Claim } from "../lib/claims";
import { CapacityCurve } from "../components/CapacityCurve";
import { Counted } from "../components/Motion";

/**
 * The way in.
 *
 * This was an essay: a hero, a lede, three cards, a curve and a list of
 * links, read top to bottom. It made the argument well and looked like a
 * write-up ABOUT a system rather than the system itself.
 *
 * The shape now is a SPLIT. The left column is fixed and holds the
 * argument -- what this is, the figures that carry it, the way in. The
 * right column scrolls and is a LEDGER: ten claims, each with the support
 * that would earn it, each expandable to the evidence. That is the shape
 * a market product uses, and it is the shape this project's own material
 * already had: `docs/12` is exactly this table, sitting in markdown
 * nobody opens while the front page made a general case in prose.
 *
 * Why the ledger rather than more prose:
 *
 *   - It is SCANNABLE. A reader arrives knowing nothing, sees the whole
 *     surface in one screen, and opens only what they doubt.
 *   - The LIMITATIONS sit at the same weight as everything else. Two of
 *     the ten are unearned, in the same list and the same type, not in a
 *     footnote. That is what makes the other eight believable.
 *   - It is auditable. Every row names its support, so the page can be
 *     checked rather than believed.
 *
 * **Every figure is loaded, never written here.** `docs/11` requires each
 * displayed number to come from a retained fixture or a
 * transaction-derived record, and a landing page is where a hardcoded
 * headline would be most tempting and least visible. If the demos have
 * not run, this screen says so rather than showing numbers from nowhere.
 */

/** One assessment's origination outcome, as the bundle stores it. */
interface Origination {
  apr: number;
  capacity: string;
  binding: string;
}

function originations(run: Run | null): Origination[] {
  if (!run) return [];
  return run.assessments
    .filter((a) => a.payload?.outcome?.decision === "ApprovedOrigination")
    .map((a) => ({
      apr: a.payload.outcome.terms?.apr_bps ?? 0,
      capacity: a.payload.outcome.capacity?.agent_capacity ?? "0",
      binding: a.payload.outcome.capacity?.agent_binding ?? "-",
    }));
}

/** The named check a beat rests on, or undefined when the run is absent. */
function check(run: Run | null, needle: string): Check | undefined {
  return run?.checks?.find((c) => c.name.includes(needle));
}

/**
 * One row of the ledger.
 *
 * Collapsed it is a claim and a status; open it is the evidence. Native
 * `<details>`, so it is keyboard operable, works without JavaScript, and
 * is found by the browser's own text search.
 */
function ClaimRow({ claim }: { claim: Claim }) {
  const earned = claim.status === "earned";
  return (
    <details className={`claim claim-${claim.status}`}>
      <summary>
        <span className="claim-title">{claim.title}</span>
        <span className="claim-status">{earned ? "earned" : "not yet"}</span>
      </summary>
      <div className="claim-body">
        <p className="claim-detail">{claim.detail}</p>
        <p className="claim-support">
          <span className="claim-support-label">Support</span>
          {claim.support}
        </p>
        {claim.to && (
          <Link to={claim.to} className="claim-link">
            Check it &rarr;
          </Link>
        )}
      </div>
    </details>
  );
}

export function Start() {
  const [repay, setRepay] = useState<Run | null>(null);
  const [fault, setFault] = useState<Run | null>(null);
  const [missing, setMissing] = useState(false);

  useEffect(() => {
    let live = true;
    Promise.all([loadRun("repay"), loadRun("default")])
      .then(([r, d]) => {
        if (!live) return;
        setRepay(r);
        setFault(d);
      })
      .catch(() => live && setMissing(true));
    return () => {
      live = false;
    };
  }, []);

  const loans = originations(repay);
  const first = loans[0];
  const last = loans[loans.length - 1];

  // The rate fall is the thesis in one number, so it is computed from
  // the loaded assessments rather than stated.
  const rateFall =
    first && last && loans.length > 1 ? first.apr - last.apr : null;

  // Did the binding constraint actually change hands? Claimed from the
  // DATA: a two-loan bundle is still bound by history, and saying
  // otherwise would describe a run that did not happen.
  const switched = loans.length > 1 && first?.binding !== last?.binding;
  const loss = check(fault, "vault realized loss");

  return (
    <div className="split">
      {/* LEFT: the argument. Sticky on a wide screen, so it stays put
          while the ledger beside it is read. */}
      <aside className="split-left">
        <p className="kicker">Credit for AI agents</p>

        <h1 className="hero">
          An agent borrows against what it has <em>demonstrated</em>.
        </h1>

        <p className="hero-lede">
          Software agents are starting to trade, hedge and pay for their own
          compute. None of them can get credit, because nobody can price a
          borrower with no salary, no address and no credit file. Credence
          prices one from the only thing it has: a record of what it
          actually did.
        </p>

        {missing && (
          <p className="empty">
            The demo bundle is not built, so these figures would have
            nowhere to come from. Run <code>bash scripts/web.sh</code>.
          </p>
        )}

        {/* The three figures that carry the argument, in a row: a reader
            should take them in at once rather than scroll past them. */}
        <div className="figures">
          <Link to="/agent" className="figure figure-growth">
            <span className="figure-value">
              {rateFall !== null ? (
                <Counted
                  value={rateFall / 100}
                  decimals={2}
                  prefix="−"
                  suffix="%"
                />
              ) : (
                "—"
              )}
            </span>
            <span className="figure-label">
              rate, across {loans.length || "—"} loans
            </span>
          </Link>

          <Link to="/enforcement" className="figure figure-alarm">
            <span className="figure-value">reverted</span>
            <span className="figure-label">a withdrawal it could not make</span>
          </Link>

          <Link to="/lender" className="figure figure-machine">
            <span className="figure-value">
              {loss ? (
                <Counted
                  value={Number(BigInt(loss.detail.split(" ")[0]!)) / 1e6}
                />
              ) : (
                "—"
              )}
            </span>
            <span className="figure-label">written off, in public</span>
          </Link>
        </div>

        {loans.length > 1 && (
          <CapacityCurve
            points={loans.map((l) => ({
              capacity: l.capacity,
              apr: l.apr,
              binding: l.binding,
            }))}
          />
        )}

        {loans.length > 1 && (
          <p className="hero-note">
            The limit rises from {units(first?.capacity)} to{" "}
            {units(last?.capacity)}
            {switched
              ? ", and what bounds it switches from its own record to what it survives in a crash."
              : `, still limited by ${bindingPhrase(last?.binding)}.`}{" "}
            One policy, one model. Only the history changes.
          </p>
        )}

        <Link to="/compare" className="start-enter">
          See who gets credit <span>&rarr;</span>
        </Link>
      </aside>

      {/* RIGHT: the ledger. What is claimed, what is not, and the
          evidence for each. */}
      <section className="split-right">
        <header className="ledger-head">
          <h2>What this claims</h2>
          <p>
            Ten claims, each with the support that would earn it.{" "}
            <strong>{EARNED} are earned.</strong> {UNEARNED} are not, and
            they are in this list rather than in a footnote. Open a row for
            the evidence.
          </p>
        </header>

        <div className="ledger">
          {CLAIMS.map((c) => (
            <ClaimRow key={c.title} claim={c} />
          ))}
        </div>

        <div className="refused">
          <h3>What it refuses to claim</h3>
          <ul>
            {REFUSED.map((r) => (
              <li key={r}>{r}</li>
            ))}
          </ul>
          <p className="refused-note">
            High APR does not establish profitability. Synthetic tests do
            not establish demand. Determinism establishes auditability, not
            accuracy.
          </p>
        </div>

        <div className="ledger-cta">
          <div>
            <h3>Check it yourself</h3>
            <p>
              Re-run a real decision in your browser against the retained
              inputs, and watch a tampered copy fail.
            </p>
          </div>
          <Link to="/replay" className="cta-button">
            Replay a decision <span>&rarr;</span>
          </Link>
        </div>
      </section>
    </div>
  );
}
