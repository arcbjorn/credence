import { useEffect, useState } from "react";
import { loadRun, units, type Check, type Run } from "../lib/data";

/**
 * `docs/09`: what cash is available, what principal is exposed, and what
 * was realized.
 *
 * The lender's side, read from the vault rather than derived here. Every
 * figure is a `credence-verify` ledger read against the live deployment.
 *
 * The four ledgers are shown SEPARATELY because `docs/05` keeps them
 * separate: idle cash, outstanding principal, collected interest and
 * realized loss are different facts, and **uncollected interest is never
 * withdrawable cash**. A single "vault value" number would merge a
 * receivable with settled cash and overstate what a lender could take.
 */
export function Lender() {
  const [run, setRun] = useState<Run | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    loadRun("repay").then(setRun).catch((e: Error) => setError(e.message));
  }, []);

  if (error) return <div className="error">{error}</div>;
  if (!run) return <p className="lede">Loading retained evidence…</p>;

  const checks: Check[] = run.checks ?? [];
  const ledger = checks.filter((c) => c.beat === "lender");
  const interest = checks.find((c) => c.name.includes("interest was collected"));

  const amount = (c: Check | undefined) =>
    c ? units(c.detail.split(" ")[0]) : "-";

  const find = (needle: string) =>
    ledger.find((c) => c.name.includes(needle));

  if (ledger.length === 0) {
    return (
      <>
        <h2>Lender</h2>
        <p className="lede">
          This run recorded no vault ledgers. Re-run{" "}
          <code>LOANS=3 scripts/demo-repay.sh</code>, then{" "}
          <code>pnpm data</code>.
        </p>
      </>
    );
  }

  return (
    <>
      <h2>Lender</h2>
      <p className="lede">
        The epoch's ledgers, read from the vault on chain. Kept apart
        because they are different facts — uncollected interest is not
        cash a lender can withdraw.
      </p>

      <div className="grid">
        <div className="card">
          <h3>Idle cash</h3>
          <p className="num" style={{ fontSize: 22, margin: "4px 0" }}>
            {amount(find("idle cash"))}
          </p>
          <p className="lede" style={{ margin: 0 }}>
            Available to lend, and the only figure that is settled cash.
          </p>
        </div>

        <div className="card">
          <h3>Principal outstanding</h3>
          <p className="num" style={{ fontSize: 22, margin: "4px 0" }}>
            {amount(find("principal outstanding"))}
          </p>
          <p className="lede" style={{ margin: 0 }}>
            Lent and not yet returned. Exposed to the borrower's book.
          </p>
        </div>

        <div className="card">
          <h3>Interest collected</h3>
          <p className="num" style={{ fontSize: 22, margin: "4px 0" }}>
            {amount(interest)}
          </p>
          <p className="lede" style={{ margin: 0 }}>
            Actually received. Accrued-but-unpaid interest is a receivable,
            not this number.
          </p>
        </div>

        <div className="card">
          <h3>Realized loss</h3>
          <p className="num" style={{ fontSize: 22, margin: "4px 0" }}>
            {amount(find("realized loss"))}
          </p>
          <p className="lede" style={{ margin: 0 }}>
            Borne by the vault after a borrower's equity was exhausted.
            Zero is the correct value for a clean epoch.
          </p>
        </div>
      </div>

      <details>
        <summary>Every ledger read, verbatim</summary>
        <div className="card scroll" style={{ marginTop: 8 }}>
          <table>
            <thead>
              <tr><th>Ledger</th><th className="num">Value</th></tr>
            </thead>
            <tbody>
              {ledger.map((c) => (
                <tr key={c.name}>
                  <td>{c.name}</td>
                  <td className="num">{c.detail}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </details>
    </>
  );
}
