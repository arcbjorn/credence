import { useCallback, useEffect, useState } from "react";
import { Link } from "react-router";
import { units } from "../lib/data";
import {
  CHAIN_ID,
  addressURL,
  connect,
  injected,
  readLive,
  repay,
  txURL,
  type LiveState,
} from "../lib/live";
import { RecordHeader } from "../components/Record";

/**
 * The deployment, live.
 *
 * Every other record in this interface reads a static bundle, because
 * `docs/09` requires displayed numbers to come from retained evidence and
 * a generated bundle enforces that structurally. This screen is the one
 * deliberate exception and says so: it reads Monad testnet directly, so
 * its figures are true at the block they were fetched at and nowhere
 * else.
 *
 * **What a visitor can do here.** Read everything, and repay a loan they
 * are willing to pay for. Originating is absent, not hidden: the manager
 * consumes a quote the ATTESTOR published, and that key stays with the
 * operator (`docs/05`). A borrow button that could not work would be a
 * claim this project does not get to make.
 */

const STALE_MS = 60_000;

function Ledger({ state }: { state: LiveState }) {
  const { vault } = state;
  return (
    <div className="grid">
      <div className="card rail rail-growth">
        <h3>Idle cash</h3>
        <p className="figure figure-growth">{units(vault.idleCash.toString())}</p>
        <p className="lede" style={{ margin: 0 }}>
          Settled and available to lend or withdraw.
        </p>
      </div>
      <div className="card rail rail-machine">
        <h3>Principal outstanding</h3>
        <p className="figure">
          {units(vault.outstandingPrincipal.toString())}
        </p>
        <p className="lede" style={{ margin: 0 }}>
          Lent and not yet returned.
        </p>
      </div>
      <div className="card rail rail-growth">
        <h3>Interest collected</h3>
        <p className="figure figure-growth">
          {units(vault.collectedInterest.toString())}
        </p>
        <p className="lede" style={{ margin: 0 }}>
          Received by the vault. Accrued-but-unpaid interest is not this.
        </p>
      </div>
      <div className="card rail rail-alarm">
        <h3>Realized loss</h3>
        <p className="figure figure-alarm">
          {units(vault.realizedLoss.toString())}
        </p>
        <p className="lede" style={{ margin: 0 }}>
          Borne by the vault after borrower equity was exhausted.
        </p>
      </div>
    </div>
  );
}

export function Live() {
  const [state, setState] = useState<LiveState | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);
  const [account, setAccount] = useState<string | null>(null);
  const [busy, setBusy] = useState<number | null>(null);
  const [sent, setSent] = useState<{ loanId: number; hash: string } | null>(
    null,
  );
  const [walletError, setWalletError] = useState<string | null>(null);

  const refresh = useCallback(async () => {
    setLoading(true);
    try {
      setState(await readLive());
      setError(null);
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void refresh();
  }, [refresh]);

  const onConnect = async () => {
    setWalletError(null);
    try {
      setAccount(await connect());
    } catch (e) {
      setWalletError(e instanceof Error ? e.message : String(e));
    }
  };

  const onRepay = async (loanId: number) => {
    setWalletError(null);
    setBusy(loanId);
    try {
      const hash = await repay(loanId);
      setSent({ loanId, hash });
      // The read is deliberately NOT optimistic. The ledgers below are
      // what the chain says, and showing a repayment before it is mined
      // would make this screen assert something no block confirms yet.
      await refresh();
    } catch (e) {
      setWalletError(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(null);
    }
  };

  if (error) {
    return (
      <>
        <RecordHeader title="Live on Monad">
          The chain could not be read.
        </RecordHeader>
        <div className="error">{error}</div>
        <p className="lede">
          This is the only screen that needs the network. Every other
          record reads the retained bundle and works offline.
        </p>
        <button className="action" onClick={() => void refresh()}>
          Try again
        </button>
      </>
    );
  }

  if (!state) {
    return (
      <RecordHeader title="Live on Monad">
        Reading chain {CHAIN_ID}…
      </RecordHeader>
    );
  }

  const stale = Date.now() - state.fetchedAt > STALE_MS;
  const active = state.loans.filter((l) => l.status === "Active");

  return (
    <>
      <RecordHeader title="Live on Monad">
        Read from chain {CHAIN_ID} at block {state.blockNumber.toString()}.
        Unlike every other record here, this one is not a retained bundle —
        it is true at that block and nowhere else.
      </RecordHeader>

      <div
        style={{
          display: "flex",
          gap: 12,
          alignItems: "center",
          flexWrap: "wrap",
          marginBottom: 16,
        }}
      >
        <button
          className="action action-small"
          onClick={() => void refresh()}
          disabled={loading}
        >
          {loading ? "Reading…" : "Read again"}
        </button>
        <span className="lede" style={{ margin: 0 }}>
          {stale ? "Fetched over a minute ago." : "Just fetched."}
        </span>
      </div>

      <Ledger state={state} />

      <h3 style={{ marginTop: 28 }}>Loans on this deployment</h3>
      {state.loans.length === 0 ? (
        state.scanErrors.length > 0 ? (
          <>
            <p className="lede">
              The loan reads failed, so this is <strong>not</strong> a
              chain with nothing originated — it is a chain this screen
              could not read.
            </p>
            <div className="card scroll">
              {state.scanErrors.map((line) => (
                <div className="hash" key={line}>
                  {line}
                </div>
              ))}
            </div>
          </>
        ) : (
          <p className="lede">
            The manager reports no loans. That is a chain with nothing
            originated, not a failed read.
          </p>
        )
      ) : (
        <div className="card scroll">
          <table>
            <thead>
              <tr>
                <th>Loan</th>
                <th>Status</th>
                <th className="num">Debt due now</th>
                <th className="num">Interest accrued</th>
                <th className="num">Agent</th>
              </tr>
            </thead>
            <tbody>
              {state.loans.map((loan) => (
                <tr key={loan.loanId}>
                  <td>{loan.loanId}</td>
                  <td
                    className={
                      loan.status === "Repaid"
                        ? "figure-growth"
                        : loan.status === "Defaulted"
                          ? "figure-alarm"
                          : ""
                    }
                  >
                    {loan.status}
                  </td>
                  <td className="num">{units(loan.debtDue.toString())}</td>
                  <td className="num">
                    {units(loan.accruedInterest.toString())}
                  </td>
                  <td className="num">{loan.agentId.toString()}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      <h3 style={{ marginTop: 28 }}>Repay a loan yourself</h3>
      <p className="lede">
        The one transaction this interface will build. It is the
        borrower's own side of the loan: <code>repay</code> pulls the debt
        from whoever calls it and needs no attestor signature. You need
        the settlement token and an approval for the manager — both are
        yours to arrange, and a revert is shown rather than worked around.
      </p>

      {!injected() ? (
        <div className="card">
          <p className="lede" style={{ margin: 0 }}>
            No wallet is available in this browser. Everything above still
            reads without one.
          </p>
        </div>
      ) : !account ? (
        <button className="action" onClick={() => void onConnect()}>
          Connect a wallet
        </button>
      ) : (
        <>
          <p className="lede">
            Connected as <code className="hash">{account}</code>
          </p>
          {active.length === 0 ? (
            <p className="lede">
              No loan is active, so there is nothing to repay.
            </p>
          ) : (
            <div style={{ display: "flex", gap: 12, flexWrap: "wrap" }}>
              {active.map((loan) => (
                <button
                  key={loan.loanId}
                  className="action"
                  disabled={busy !== null}
                  onClick={() => void onRepay(loan.loanId)}
                >
                  {busy === loan.loanId
                    ? "Confirm in your wallet…"
                    : `Repay loan ${loan.loanId} (${units(loan.debtDue.toString())})`}
                </button>
              ))}
            </div>
          )}
        </>
      )}

      {sent && (
        <div className="card rail rail-growth" style={{ marginTop: 16 }}>
          <p className="lede" style={{ margin: 0 }}>
            Repayment for loan {sent.loanId} was submitted.{" "}
            <a href={txURL(sent.hash)} target="_blank" rel="noreferrer">
              View the transaction
            </a>
            . The ledgers above were re-read from the chain afterwards.
          </p>
        </div>
      )}

      {walletError && (
        <div className="error" style={{ marginTop: 16 }}>
          {walletError}
        </div>
      )}

      <h3 style={{ marginTop: 28 }}>Why you cannot borrow here</h3>
      <div className="card rail rail-alarm">
        <p className="lede" style={{ marginTop: 0 }}>
          Origination calls <code>oracle.consumeQuote</code>, which matches
          only a quote the <strong>attestor</strong> signed and published.
          That key belongs to the operator (<code>docs/05</code>) and is
          deliberately not in your browser — with it, any visitor could
          authorize their own loan.
        </p>
        <p className="lede" style={{ marginBottom: 0 }}>
          So the borrow flow is absent rather than simulated. To see what
          the engine would quote you, use the{" "}
          <Link to="/borrow">borrow desk</Link>, which runs the real risk
          engine in your tab without claiming a chain agreed to it. This
          is the trust model the project documents, not a limitation of
          this page.
        </p>
      </div>

      <details style={{ marginTop: 16 }}>
        <summary>The addresses these figures were read from</summary>
        <div className="card scroll" style={{ marginTop: 8 }}>
          <table>
            <thead>
              <tr>
                <th>Contract</th>
                <th>Address</th>
              </tr>
            </thead>
            <tbody>
              {Object.entries(state.addresses).map(([name, address]) => (
                <tr key={name}>
                  <td>{name}</td>
                  <td>
                    <a
                      className="hash"
                      href={addressURL(address)}
                      target="_blank"
                      rel="noreferrer"
                    >
                      {address}
                    </a>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
          <p className="lede" style={{ marginBottom: 0 }}>
            From <code>contracts/deployments/10143.json</code>, the single
            place addresses are recorded.
            {state.agentId !== undefined && (
              <>
                {" "}
                Loans here are bound to ERC-8004 agent{" "}
                <strong>{state.agentId}</strong>.
              </>
            )}
          </p>
        </div>
      </details>
    </>
  );
}
