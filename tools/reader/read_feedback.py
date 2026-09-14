#!/usr/bin/env python3
"""Standalone reader for Credence obligation history.

`docs/06` sub-claim B: a third party reads and verifies published feedback
**without asking Credence**. This script therefore:

  * talks only to a JSON-RPC endpoint, never to a Credence service;
  * imports nothing from this repository -- no shared ABI module, no
    helper package, only the Python standard library;
  * decodes the registry from its ABI layout, spelled out below, so it
    would work against any deployment of the same contract.

Sharing an encoder with the gateway would make the check circular: the
reader would agree with the publisher because they are the same code. The
duplication here is the point.

Usage:
    read_feedback.py --rpc URL --registry ADDRESS --agent ID
"""

import argparse
import json
import sys
import urllib.request

# --- Minimal ABI encoding, written from the spec rather than imported ---

def selector(signature: str) -> str:
    """First four bytes of keccak256(signature).

    Hard-coded per call below: computing keccak here would need a
    dependency, and a reader that needs an install is a worse
    demonstration of independence than one that does not.
    """
    return SELECTORS[signature]


SELECTORS = {
    # cast sig "agentEntries(uint256)"
    "agentEntries(uint256)": "0x4950dab9",
    # cast sig "entryAt(uint256)"
    "entryAt(uint256)": "0x9c67a385",
    # cast sig "entryCount()"
    "entryCount()": "0x0cbb0f83",
}

OUTCOMES = ["Repaid", "Recovered", "Defaulted"]


def word(n: int) -> str:
    return f"{n:064x}"


def rpc_call(url: str, to: str, data: str) -> str:
    payload = json.dumps({
        "jsonrpc": "2.0", "id": 1, "method": "eth_call",
        "params": [{"to": to, "data": data}, "latest"],
    }).encode()
    req = urllib.request.Request(
        url, data=payload, headers={"content-type": "application/json"})
    with urllib.request.urlopen(req, timeout=15) as r:
        body = json.load(r)
    if "error" in body:
        raise SystemExit(f"rpc error: {body['error']}")
    return body["result"]


def words(hexdata: str) -> list[int]:
    raw = hexdata[2:] if hexdata.startswith("0x") else hexdata
    return [int(raw[i:i + 64], 16) for i in range(0, len(raw), 64)]


def agent_entries(url: str, registry: str, agent_id: int) -> list[int]:
    data = selector("agentEntries(uint256)") + word(agent_id)
    w = words(rpc_call(url, registry, data))
    if len(w) < 2:
        return []
    # Dynamic array: offset, length, then elements.
    length = w[1]
    return w[2:2 + length]


def entry_at(url: str, registry: str, index: int) -> dict:
    data = selector("entryAt(uint256)") + word(index)
    w = words(rpc_call(url, registry, data))
    # A struct of ten static fields, returned behind one offset word.
    f = w[1:] if len(w) > 10 else w
    return {
        "agent_id": f[0],
        "credit_account": "0x" + f"{f[1]:040x}",
        "loan_id": f[2],
        "outcome": OUTCOMES[f[3]] if f[3] < len(OUTCOMES) else f"unknown({f[3]})",
        "principal": f[4],
        "interest_paid": f[5],
        "principal_loss": f[6],
        "originated_at": f[7],
        "resolved_at": f[8],
        "evidence_ref": "0x" + f"{f[9]:064x}",
    }


def assert_registry_present(url: str, registry: str) -> None:
    """Refuse to read from an address with no code.

    Every read below returns empty for a contract that is not there, so
    a wrong address looks exactly like an agent with no history. Failing
    loudly is the difference between "nothing was published" and "you are
    pointed at the wrong place".
    """
    payload = json.dumps({
        "jsonrpc": "2.0", "id": 1, "method": "eth_getCode",
        "params": [registry, "latest"],
    }).encode()
    req = urllib.request.Request(
        url, data=payload, headers={"content-type": "application/json"})
    with urllib.request.urlopen(req, timeout=15) as r:
        body = json.load(r)
    code = body.get("result", "0x")
    if code in ("0x", "", None):
        raise SystemExit(
            f"no contract at {registry} on this RPC.\n"
            f"An absent registry reads as an empty history, so this is "
            f"refused rather than reported as 'no feedback'."
        )


def main() -> int:
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("--rpc", required=True, help="JSON-RPC endpoint")
    ap.add_argument("--registry", required=True, help="feedback registry address")
    ap.add_argument("--agent", type=int, required=True, help="agent id")
    args = ap.parse_args()

    assert_registry_present(args.rpc, args.registry)

    indices = agent_entries(args.rpc, args.registry, args.agent)
    if not indices:
        print(f"agent {args.agent}: no published feedback")
        return 0

    print(f"agent {args.agent}: {len(indices)} published obligation(s)")
    print()

    # Separate signals, separately counted. `docs/06` forbids collapsing a
    # repayment, a default, and a loss into one score.
    repaid = recovered = defaulted = 0
    interest = loss = 0

    for i in indices:
        e = entry_at(args.rpc, args.registry, i)
        duration = e["resolved_at"] - e["originated_at"]
        print(f"  loan {e['loan_id']}  {e['outcome']:<10}"
              f"  principal={e['principal']}"
              f"  interest={e['interest_paid']}"
              f"  loss={e['principal_loss']}"
              f"  held={duration}s")
        print(f"    account={e['credit_account']}  evidence={e['evidence_ref']}")

        interest += e["interest_paid"]
        loss += e["principal_loss"]
        if e["outcome"] == "Repaid":
            repaid += 1
        elif e["outcome"] == "Recovered":
            recovered += 1
        elif e["outcome"] == "Defaulted":
            defaulted += 1

    print()
    print(f"  repaid={repaid}  recovered={recovered}  defaulted={defaulted}")
    print(f"  interest paid={interest}  principal loss={loss}")
    print()
    print("  Read directly from chain. No Credence endpoint was contacted.")
    return 0


if __name__ == "__main__":
    sys.exit(main())
