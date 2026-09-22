import { useEffect, useState } from "react";
import { Link } from "react-router";
import { loadRun, units, type Check, type Run } from "../lib/data";
import { bindingPhrase } from "../lib/bindings";
import { atmosphere, core, readBook } from "../lib/book";
import { CLAIMS, EARNED, REFUSED, UNEARNED, type Claim } from "../lib/claims";
import { CreditGlobe } from "../components/CreditGlobe";
import { CutModel, type Layer } from "../components/CutModel";
import { Eclipse } from "../components/Eclipse";
import { Deployment } from "../components/Deployment";
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
  /** The behavioural score, when the assessment carried one. */
  score?: number;
}

function originations(run: Run | null): Origination[] {
  if (!run) return [];
  return run.assessments
    .filter((a) => a.payload?.outcome?.decision === "ApprovedOrigination")
    .map((a) => ({
      apr: a.payload.outcome.terms?.apr_bps ?? 0,
      capacity: a.payload.outcome.capacity?.agent_capacity ?? "0",
      binding: a.payload.outcome.capacity?.agent_binding ?? "-",
      // A null score is not a zero one: it means the assessment produced
      // no score, and the object must not colour itself as though it
      // had measured the coldest possible agent.
      ...(typeof a.payload.outcome.capacity?.score === "number"
        ? { score: a.payload.outcome.capacity.score }
        : {}),
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
        <span className="claim-status">{earned ? "live" : "building"}</span>
      </summary>
      <div className="claim-body">
        <p className="claim-detail">{claim.detail}</p>
        <p className="claim-support">
          <span className="claim-support-label">Runs on</span>
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

  // The book's physical state, from the same retained checks the lender
  // screen tabulates. `docs/brand/physics.md`: the core is idle cash and
  // the atmosphere is `1 - utilization`.
  const book = readBook(repay);

  // The section through the body. Each layer carries the figure it
  // stands for, and a layer nothing measured is drawn dashed rather
  // than given a confident width.
  const held = check(repay, "account holds withdrawable value");
  const heldUnits = held ? BigInt(held.detail.split(" ")[0]!) : null;
  const layers: Layer[] = [
    {
      key: "shell",
      label: "The agent",
      note: "identity and its account",
      amount: null,
      register: "machine",
    },
    {
      key: "history",
      label: "Its record",
      note: "what opens the facility",
      amount: null,
      register: "growth",
    },
    {
      key: "escrow",
      label: "Held as collateral",
      note: heldUnits !== null ? "locked while debt is open" : "not measured",
      // Collateral is an 18-decimal balance while the book is in 6, so
      // it is scaled here rather than compared across bases.
      amount: heldUnits !== null ? heldUnits / 1_000_000_000_000n : null,
      register: "machine",
    },
    {
      key: "core",
      label: "Lender cash",
      note: "what is free to lend",
      amount: book.idle,
      register: "growth",
    },
  ];

  return (
    <div className="split">
      {/* LEFT: the argument. Sticky on a wide screen, so it stays put
          while the ledger beside it is read. */}
      <aside className="split-left">
        <p className="kicker">Credit for AI agents</p>

        <h1 className="hero">
          A credit score is just <em>compressed</em> history.
        </h1>

        {/* THE OBJECT LEADS. It is the product in one shape -- a limit,
            what sets it, and what that costs -- and it says all three
            before a word is read. The figures and the reasoning used to
            sit above it, which meant a visitor met a paragraph and three
            stat cards before meeting the thing itself. */}
        {/* The object and the book it draws on, side by side. The
            eclipse was below the fold at 1184px, which meant nobody saw
            the one shape that states utilization at a glance. */}
        <div className="hero-objects">
          {loans.length > 0 && (
            <CreditGlobe
              atmosphere={atmosphere(book)}
              core={core(book)}
              /* No scar. The realized loss belongs to a DIFFERENT
                 agent's default, and painting it on this body said
                 this borrower defaulted when it repaid all three
                 loans. The loss is shown where it happened: on the
                 lender screen, and in the figure below. */
              points={loans.map((l) => ({
                capacity: l.capacity,
                apr: l.apr,
                binding: l.binding,
                ...(l.score !== undefined ? { score: l.score } : {}),
              }))}
            />
          )}

          {book.utilization !== null && (
            <Eclipse
              utilization={book.utilization}
              idle={units(book.idle?.toString())}
              outstanding={units(book.outstanding?.toString())}
            />
          )}
        </div>

        <p className="hero-lede">
          An agent has no salary and no credit file. It has every action it
          ever took, signed and onchain. We price that.
        </p>

        <Link to="/compare" className="start-enter">
          See who gets credit <span>&rarr;</span>
        </Link>

        {missing && (
          <p className="empty">
            The demo bundle is not built, so these figures would have
            nowhere to come from. Run <code>bash scripts/web.sh</code>.
          </p>
        )}

        {/* Everything past here is for a reader who has decided to look.
            Three figures, then the reasoning behind the shape above --
            available, not insisted on. */}
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
              cheaper by loan {loans.length || "—"}, same policy
            </span>
          </Link>

          <Link to="/enforcement" className="figure figure-alarm">
            <span className="figure-value">reverted</span>
            <span className="figure-label">
              it tried to take its collateral back
            </span>
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
            <span className="figure-label">
              lost on a loan that went bad
            </span>
          </Link>
        </div>

        <details className="hero-why">
          <summary>A section through it</summary>
          <CutModel layers={layers} />
        </details>

        {loans.length > 1 && (
          <details className="hero-why">
            <summary>Why the shape changes</summary>
            <p className="hero-note">
              Three loans, same policy, same model hash. The only input
              that changed is the history, and the limit goes{" "}
              {units(first?.capacity)} &rarr; {units(last?.capacity)}
              {switched
                ? ". Past loan 3 the ring stops being set by the agent's record and starts being set by what its book survives in a crash. Good behaviour stops buying you more."
                : `, still limited by ${bindingPhrase(last?.binding)}.`}
            </p>
          </details>
        )}

        <Deployment />
      </aside>

      {/* RIGHT: the ledger. What is claimed, what is not, and the
          evidence for each. */}
      <section className="split-right">
        <header className="ledger-head">
          <h2>What it does</h2>
          <p>
            <strong>{EARNED} of {EARNED + UNEARNED} work today.</strong>{" "}
            Open a row to see it.
          </p>
        </header>

        <div className="ledger">
          {CLAIMS.map((c) => (
            <ClaimRow key={c.title} claim={c} />
          ))}
        </div>

        <details className="refused">
          <summary>
            <h3>What this does not claim</h3>
          </summary>
          <ul>
            {REFUSED.map((r) => (
              <li key={r}>{r}</li>
            ))}
          </ul>
          <p className="refused-note">
            Determinism buys auditability, not accuracy. Every decision
            replays byte-for-byte, which proves the model did what it
            says — not that it is right.
          </p>
        </details>

        <div className="ledger-cta">
          <div>
            <h3>Don't take my word for it</h3>
            <p>
              The engine is the same Rust compiled to WebAssembly, running
              in your tab. Re-run a real decision against its retained
              inputs, then change one byte and watch it refuse to match.
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
