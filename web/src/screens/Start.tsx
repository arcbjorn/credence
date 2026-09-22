import { useEffect, useState } from "react";
import { Link } from "react-router";
import { bps, loadRun, units, type Check, type Run } from "../lib/data";
import { bindingPhrase } from "../lib/bindings";
import { CapacityCurve } from "../components/CapacityCurve";
import { Counted } from "../components/Motion";

/**
 * The way in.
 *
 * `docs/09` specifies eight screens and a nine-beat script, and the
 * interface had the screens without the script: ten equal nav items,
 * every one of them opening on a table. A visitor who already knew what
 * Credence claimed could find the evidence; a visitor who did not was
 * handed a filing cabinet and left to assemble the argument themselves.
 *
 * So this screen is the argument, in the order `docs/09` tells it:
 *
 *   1. credit COMPOUNDS -- the limit rises and the rate falls (beat 4b)
 *   2. the refusal is REAL -- a reverting transaction (beat 6, which
 *      `docs/09` calls "the single most convincing moment")
 *   3. the loss is REAL -- 2.6e9 written off on chain (beat 7)
 *
 * **Every figure is loaded, never written here.** `docs/11` requires
 * each displayed number to come from a retained fixture or a
 * transaction-derived record, and a landing page is exactly where a
 * hardcoded headline would be most tempting and least visible. If the
 * demos have not run, this screen says so rather than showing numbers
 * from nowhere.
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

  // Did the binding constraint actually change hands? `docs/09` calls
  // this the half of beat 4b that carries the thesis, and it is only
  // true once a loan is bound by stress rather than by history.
  const switched =
    loans.length > 1 && first?.binding !== last?.binding;

  const refusal = check(repay, "owner withdrawal reverts");
  const loss = check(fault, "vault realized loss");

  return (
    <section className="start">
      <p className="kicker">Autonomous credit markets for AI agents</p>

      <h1 className="hero">
        An agent borrows against what it has <em>demonstrated</em>.
      </h1>

      <p className="hero-lede">
        Software agents are starting to trade, hedge and pay for their own
        compute — and none of them can get credit, because nobody can price
        a borrower with no salary, no address and no credit file. Credence
        prices one from the only thing it has: a record of what it actually
        did. Borrow, repay, borrow more. The limit grows because the
        behaviour earned it.
      </p>

      {missing && (
        <p className="empty">
          The demo bundle is not built, so the figures below would have
          nowhere to come from. Run <code>bash scripts/web.sh</code>.
        </p>
      )}

      <div className="proofs">
        {/* 1. Credit compounds. */}
        <Link to="/agent" className="proof proof-growth">
          <span className="proof-label">Credit compounds</span>
          <span className="proof-figure">
            {rateFall !== null ? (
              <Counted value={rateFall / 100} decimals={2} prefix="−" suffix="%" />
            ) : (
              "—"
            )}
          </span>
          <span className="proof-note">
            {loans.length > 1 ? (
              <>
                across {loans.length} sequential loans, one policy and one
                model. The limit rises from {units(first?.capacity)} to{" "}
                {units(last?.capacity)}
                {/* The constraint switch is the sharper half of the
                    thesis, and it only happens once stress capacity
                    becomes the binding term. Claimed from the DATA
                    rather than from the story: a two-loan bundle is
                    still bound by history, and saying otherwise would
                    describe a run that did not happen. */}
                {switched
                  ? ", and the constraint that bounds it switches from history to portfolio stress."
                  : `, still limited by ${bindingPhrase(last?.binding)}.`}
              </>
            ) : (
              <>Run the demos to see the limit rise and the rate fall.</>
            )}
          </span>
        </Link>

        {/* 2. The refusal is real. */}
        <Link to="/enforcement" className="proof proof-alarm">
          <span className="proof-label">The refusal is real</span>
          <span className="proof-figure">reverted</span>
          <span className="proof-note">
            {refusal ? (
              <>
                The borrower tried to take its collateral back while it still
                owed money. It could not. Not a policy, not a promise — the
                money is locked until the debt is settled, and the attempt is
                on the public record.
              </>
            ) : (
              <>A prohibited withdrawal, rejected by the contract itself.</>
            )}
          </span>
        </Link>

        {/* 3. The loss is real. */}
        <Link to="/lender" className="proof proof-machine">
          <span className="proof-label">The loss is real</span>
          <span className="proof-figure">
            {loss ? (
              <Counted value={Number(BigInt(loss.detail.split(" ")[0]!)) / 1e6} />
            ) : (
              "—"
            )}
          </span>
          <span className="proof-note">
            One agent bet wrong and the market moved against it. The system
            sold what was left, handed the lender the proceeds, and wrote off
            the shortfall in public. Most demos only ever show you the loans
            that worked.
          </span>
        </Link>
      </div>

      {/* The way in. On the landing page the only chrome IS the
          argument, so the entry point is part of it rather than a nav
          item competing with it. */}
      <Link to="/compare" className="start-enter">
        See who gets credit <span>&rarr;</span>
      </Link>

      {loans.length > 1 && (
        <CapacityCurve
          points={loans.map((l) => ({
            capacity: l.capacity,
            apr: l.apr,
            binding: l.binding,
          }))}
        />
      )}

      <div className="start-paths">
        <div>
          <h3>See how it works</h3>
          <ol className="path">
            <li>
              <Link to="/compare">Who gets credit</Link> — three agents, same
              rules, different track records
            </li>
            <li>
              <Link to="/agent">How credit grows</Link> — borrow, repay, borrow
              more, at a better rate
            </li>
            <li>
              <Link to="/enforcement">What a borrower cannot do</Link> — take back
              collateral it still owes against
            </li>
            <li>
              <Link to="/lender">When it goes wrong</Link> — a real loss, shown
              rather than hidden
            </li>
            <li>
              <Link to="/replay">Check the maths yourself</Link> — re-run a real
              decision in your browser
            </li>
          </ol>
        </div>

        <div>
          <h3>Audit it yourself</h3>
          <ul className="path">
            <li>
              <Link to="/evidence">Where the data came from</Link> — every input,
              how fresh, how complete
            </li>
            <li>
              <Link to="/assessment">Why this rate</Link> — the one limit that
              decided it
            </li>
            <li>
              <Link to="/lifecycle">One loan, end to end</Link> — predictions made
              up front, scored afterwards
            </li>
            <li>
              <Link to="/indexed">Rebuilt from the chain</Link> — the same loan,
              reconstructed from public records
            </li>
            <li>
              <Link to="/perpl">Live market risk</Link> — today’s real market,
              stress-testing the book
            </li>
          </ul>
        </div>
      </div>

      <p className="note start-honest">
        <strong>Being straight with you.</strong> The loan is still secured —
        this is not lending against nothing. What changes is how much an agent
        can put to work and what it pays: both come from its track record
        rather than from the size of its deposit. Two of the ten things this
        project could claim are marked <em>not yet earned</em>, and the
        write-up says which and why.
      </p>
    </section>
  );
}
