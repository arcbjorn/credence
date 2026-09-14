import { useEffect, useState } from "react";
import { loadRun, units, type Check, type Run } from "../lib/data";

/**
 * `docs/09` beat 6: a prohibited withdrawal.
 *
 * > **Beat 6 is the single most convincing moment** — the difference
 * > between claiming lender protection and demonstrating it, in one
 * > reverted transaction.
 *
 * Every row here was produced by `credence-verify` running against the
 * deployment, not by this page. The screen renders what the chain did.
 *
 * The two refusals are deliberately shown side by side with their raw
 * selectors. They are DIFFERENT errors, and that is the whole point: the
 * owner is refused because debt is open (`0xe9d4b4e2`), a stranger because
 * they are not the owner (`0x72cb8533`). Collapsing them into "reverted"
 * would lose the distinction that makes this lender protection rather
 * than access control.
 */
export function Enforcement() {
  const [run, setRun] = useState<Run | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    loadRun("repay").then(setRun).catch((e: Error) => setError(e.message));
  }, []);

  if (error) return <div className="error">{error}</div>;
  if (!run) return <p className="lede">Loading retained evidence…</p>;

  const checks: Check[] = run.checks ?? [];
  const beat6 = checks.filter((c) => c.beat === "6");
  const others = checks.filter((c) => c.beat !== "6");

  if (checks.length === 0) {
    return (
      <>
        <h2>Enforcement</h2>
        <p className="lede">
          This run recorded no checks. Re-run{" "}
          <code>LOANS=3 scripts/demo-repay.sh</code> and then{" "}
          <code>pnpm data</code>.
        </p>
      </>
    );
  }

  const held = beat6.find((c) => c.name.includes("withdrawable value"));
  const ownerRefused = beat6.find((c) => c.name.includes("owner withdrawal"));
  const strangerRefused = beat6.find((c) => c.name.includes("stranger"));

  return (
    <>
      <h2>Enforcement</h2>
      <p className="lede">
        The account holds real value, and the contract refuses to release
        it while the loan is open. Both refusals below are transactions
        that reverted on chain.
      </p>

      {held && (
        <div className="card rail rail-growth" style={{ marginBottom: 18 }}>
          <h3>What the account holds</h3>
          <p className="figure figure-growth">
            {units(held.detail.split(" ")[0], 18)}
          </p>
          <p className="lede" style={{ margin: 0 }}>
            Withdrawable in principle — the balance is really there. The
            refusals below are not an empty account.
          </p>
        </div>
      )}

      <div className="grid">
        {ownerRefused && (
          <div className="card rail rail-alarm">
            <h3>The owner is refused</h3>
            <p>Debt is open, so the account will not release collateral.</p>
            <code className="hash">{ownerRefused.detail}</code>
            <p className="lede" style={{ marginTop: 8, marginBottom: 0 }}>
              This is the lender protection. The owner has every right of
              ownership and still cannot withdraw.
            </p>
          </div>
        )}
        {strangerRefused && (
          <div className="card rail rail-machine">
            <h3>A stranger is refused</h3>
            <p>Not the owner, so the call never reaches the debt check.</p>
            <code className="hash">{strangerRefused.detail}</code>
            <p className="lede" style={{ marginTop: 8, marginBottom: 0 }}>
              A <strong>different</strong> error. If these two matched, the
              first refusal would only be proving access control.
            </p>
          </div>
        )}
      </div>

      <h3 style={{ marginTop: 24 }}>Every check, against the deployment</h3>
      <div className="card scroll">
        <table>
          <thead>
            <tr>
              <th>Beat</th>
              <th>Check</th>
              <th>Result</th>
              <th>Detail</th>
            </tr>
          </thead>
          <tbody>
            {[...beat6, ...others].map((c) => (
              <tr key={`${c.beat}-${c.name}`}>
                <td>{c.beat}</td>
                <td>{c.name}</td>
                <td>{c.passed ? "pass" : "FAIL"}</td>
                <td className="hash">{c.detail}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </>
  );
}
