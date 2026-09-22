import { useEffect, useState } from "react";
import {
  loadIndexed,
  units,
  type Indexed,
  type IndexedEvent,
  type IndexedLoan,
} from "../lib/data";
import { RecordHeader } from "../components/Record";

/**
 * The loan lifecycle, reconstructed from the event log by the Envio
 * indexer (`docs/08`).
 *
 * This screen exists to show the thing state cannot show. Every other
 * screen renders what the gateway retained or what a contract returns
 * NOW; the loan struct answers "what is true" and keeps running totals
 * and two timestamps. It cannot answer "how did it get here" — when
 * recovery opened, how many collections it took, or what each one
 * recovered. That sequence exists only in the log, and reconstructing it
 * is the integration's reason to exist rather than a view over work
 * already done.
 *
 * Nothing here is computed by this page. `scripts/build-indexed-data.sh`
 * reads the rows the indexer stored and writes them into the bundle, so
 * the same rule holds as everywhere else: the sync produced the file, or
 * the screen renders nothing.
 */

/** Unix seconds to a readable UTC stamp. */
const at = (t: string | null) =>
  t && t !== "0"
    ? new Date(Number(t) * 1000).toISOString().replace("T", " ").slice(0, 19)
    : "-";

/** Seconds to days and hours, the scale a loan term is read in. */
const duration = (s: string | null) => {
  if (!s || s === "0") return "-";
  const secs = Number(s);
  const days = Math.floor(secs / 86_400);
  const hours = Math.floor((secs % 86_400) / 3_600);
  return days > 0 ? `${days}d ${hours}h` : `${hours}h`;
};

/** A terminal loss earns the alarm rail; a clean repayment does not. */
const railFor = (status: string) =>
  status === "Defaulted"
    ? "rail-alarm"
    : status === "Repaid"
      ? "rail-growth"
      : "rail-machine";

function EventRow({ event }: { event: IndexedEvent }) {
  // Only the fields the event actually carried. `Recovered` and
  // `RecoveryOpened` have no amounts, and showing a zero for them would
  // invent a collection of nothing.
  const amounts: string[] = [];
  if (event.principal && event.principal !== "0") {
    amounts.push(`principal ${units(event.principal)}`);
  }
  if (event.interest && event.interest !== "0") {
    amounts.push(`interest ${units(event.interest)}`);
  }
  if (event.principalLoss && event.principalLoss !== "0") {
    amounts.push(`written off ${units(event.principalLoss)}`);
  }
  if (event.unpaidInterest && event.unpaidInterest !== "0") {
    amounts.push(`unpaid interest ${units(event.unpaidInterest)}`);
  }

  return (
    <tr>
      <td>
        <strong>{event.kind}</strong>
      </td>
      <td>{event.blockNumber}</td>
      <td>{at(event.blockTimestamp)}</td>
      <td>{amounts.length > 0 ? amounts.join(", ") : "—"}</td>
      <td>
        <code className="hash">{event.txHash.slice(0, 18)}…</code>
      </td>
    </tr>
  );
}

function LoanCard({ loan, events }: { loan: IndexedLoan; events: IndexedEvent[] }) {
  const mine = events.filter((e) => e.loan_id === loan.id);

  // The conservation check, stated on screen rather than asserted in a
  // comment: what came back plus what was written off is the principal.
  // `docs/05` requires collections and losses to reconcile, and this is
  // the arithmetic a reader can do by eye.
  //
  // Only for a RESOLVED loan. An open one has nothing recovered and
  // nothing written off because it has not ended, and holding it to the
  // terminal arithmetic reported a healthy active loan as a
  // reconciliation failure -- an alarm on the one loan that is simply
  // still running.
  const resolved = ["Repaid", "Recovered", "Defaulted"].includes(loan.status);
  const repaid = BigInt(loan.principalRepaid);
  const lost = BigInt(loan.principalLoss);
  const principal = BigInt(loan.principal);
  const reconciles = repaid + lost === principal;

  return (
    <div className={`card rail ${railFor(loan.status)}`} style={{ marginBottom: 18 }}>
      <h3>
        Loan {loan.loanId} — {loan.status}
      </h3>

      <div className="grid" style={{ marginBottom: 12 }}>
        <div>
          <p className="lede" style={{ margin: 0 }}>Principal</p>
          <p className="figure" style={{ margin: 0 }}>{units(loan.principal)}</p>
        </div>
        <div>
          <p className="lede" style={{ margin: 0 }}>Recovered</p>
          {/* Green only when something actually came back. A zero styled
              as growth reads as a good outcome on a loan that has simply
              not ended. */}
          <p
            className={repaid > 0n ? "figure figure-growth" : "figure"}
            style={{ margin: 0 }}
          >
            {units(loan.principalRepaid)}
          </p>
        </div>
        <div>
          <p className="lede" style={{ margin: 0 }}>Written off</p>
          <p
            className={lost > 0n ? "figure figure-alarm" : "figure"}
            style={{ margin: 0 }}
          >
            {units(loan.principalLoss)}
          </p>
        </div>
        <div>
          <p className="lede" style={{ margin: 0 }}>Held</p>
          <p className="figure" style={{ margin: 0 }}>
            {duration(loan.heldSeconds)}
          </p>
        </div>
      </div>

      {resolved ? (
        <p className={reconciles ? "match" : "difference"}>
          {reconciles
            ? `${units(loan.principalRepaid)} recovered + ${units(loan.principalLoss)} written off = ${units(loan.principal)} principal`
            : "These figures do not reconcile against the principal."}
        </p>
      ) : (
        <p className="lede">
          Still open — nothing has been recovered or written off yet, so
          there is no final accounting to check.
        </p>
      )}

      <h4>What the log says happened</h4>
      <div className="scroll">
        <table>
          <thead>
            <tr>
              <th>Event</th>
              <th>Block</th>
              <th>Time (UTC)</th>
              <th>Carried</th>
              <th>Transaction</th>
            </tr>
          </thead>
          <tbody>
            {mine.map((e) => (
              <EventRow key={e.id} event={e} />
            ))}
          </tbody>
        </table>
      </div>

      <p className="lede" style={{ marginBottom: 0 }}>
        Opened {at(loan.originatedAt)}
        {loan.recoveryOpenedAt && loan.recoveryOpenedAt !== "0"
          ? `, recovery opened ${at(loan.recoveryOpenedAt)}`
          : ""}
        {loan.resolvedAt && loan.resolvedAt !== "0"
          ? `, resolved ${at(loan.resolvedAt)}`
          : ""}
        . Every row above is a transaction on chain; the hashes are real
        and resolve against the same deployment.
      </p>
    </div>
  );
}

export function IndexedLifecycle() {
  const [data, setData] = useState<Indexed | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    loadIndexed()
      .then(setData)
      .catch((e: Error) => setError(e.message));
  }, []);

  // A missing bundle means the indexer has not been run, which is a
  // different thing from a chain with no loans. Say which, and give the
  // command, rather than rendering an empty lifecycle that reads like a
  // loan with no events.
  if (error) {
    return (
      <>
        <RecordHeader title="Indexed lifecycle">
          The indexer has not been run against this chain.
        </RecordHeader>
        <div className="card rail rail-machine">
          <p style={{ marginTop: 0 }}>
            Sync it, then regenerate the bundle:
          </p>
          <code className="hash">scripts/index-local.sh</code>
          <br />
          <code className="hash">scripts/build-indexed-data.sh</code>
          <p className="lede" style={{ marginBottom: 0 }}>
            The first needs Docker — HyperIndex runs Postgres in it.
          </p>
        </div>
      </>
    );
  }

  if (!data) return <p className="lede">Loading indexed evidence…</p>;

  return (
    <>
      <RecordHeader title="Indexed lifecycle">
        Rebuilt from the contract's event log by the Envio indexer, with no
        state read at all. A loan struct records running totals and two
        timestamps; it cannot say when recovery opened or what each
        collection recovered. That order is only in the log.
      </RecordHeader>

      <div className="modes" style={{ marginBottom: 18 }}>
        <span className="mode mode-observed">
          observed — {data.events.length} event(s) from chain{" "}
          {data.loans[0]?.chainId ?? "-"}
        </span>
      </div>

      {data.loans.length === 0 ? (
        <div className="card rail rail-machine">
          <p style={{ margin: 0 }}>
            The indexer synced and found no loans on this deployment. That
            is an empty chain, not a failed sync.
          </p>
        </div>
      ) : (
        data.loans.map((loan) => (
          <LoanCard key={loan.id} loan={loan} events={data.events} />
        ))
      )}
    </>
  );
}
