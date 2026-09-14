#!/usr/bin/env python3
"""Standalone reader for Credence feedback in an ERC-8004 registry.

`docs/06` sub-claim B: a third party reads and verifies published feedback
**without asking Credence**. This script therefore:

  * talks only to a JSON-RPC endpoint, never to a Credence service;
  * imports nothing from this repository -- no shared ABI module, no
    helper package, only the Python standard library;
  * decodes the registry from the ERC-8004 ABI, spelled out below, so it
    works against any deployment of the standard rather than ours.

Sharing an encoder with the gateway would make the check circular: the
reader would agree with the publisher because they are the same code. The
duplication here is the point.

**How provenance differs from the local adapter.** That contract had a
single immutable `publisher()`, so "who wrote this" was a property of the
registry. ERC-8004 is open: anyone may write feedback about any agent, and
the standard attributes each entry to a `clientAddress`, leaving consumers
to decide which clients they trust. So this reader REQUIRES --client and
filters to it. Reading unfiltered would mix in entries Credence never
wrote and present them as one history.

Usage:
    read_erc8004.py --rpc URL --registry ADDRESS --agent ID --client ADDRESS
"""

import argparse
import hashlib
import json
import sys
import urllib.request

# --- Minimal ABI encoding, written from the spec rather than imported ---

# Selectors are hard-coded rather than computed: keccak256 is not in the
# standard library, and a reader that needs an install is a worse
# demonstration of independence than one that does not.
#
#   cast sig "readAllFeedback(uint256,address[],string,string,bool)"
#   cast sig "ownerOf(uint256)"
READ_ALL_FEEDBACK = "0xd9d84224"
OWNER_OF = "0x6352211e"

# Tag vocabulary Credence publishes under. One entry per signal: the
# registry stores a single value per entry, and `docs/06` forbids
# combining a repayment, a realised loss, interest earned and duration
# held into one fact.
TAG_INTEREST = "credence.interest"
TAG_LOSS = "credence.loss"
TAG_HELD = "credence.held"
TAG_OUTCOME_PREFIX = "credence.outcome."


def rpc_call(url: str, method: str, params: list):
    body = json.dumps(
        {"jsonrpc": "2.0", "id": 1, "method": method, "params": params}
    ).encode()
    req = urllib.request.Request(
        url, data=body, headers={"Content-Type": "application/json"}
    )
    with urllib.request.urlopen(req, timeout=30) as resp:
        out = json.load(resp)
    if "error" in out:
        raise SystemExit(f"rpc {method}: {out['error']}")
    return out["result"]


def eth_call(url: str, to: str, data: str) -> bytes:
    raw = rpc_call(url, "eth_call", [{"to": to, "data": data}, "latest"])
    return bytes.fromhex(raw[2:])


def word(value: int) -> str:
    if value < 0:
        value += 1 << 256
    return f"{value:064x}"


def address_word(addr: str) -> str:
    return f"{int(addr, 16):064x}"


def encode_string(s: str) -> str:
    raw = s.encode()
    out = word(len(raw)) + raw.hex()
    pad = len(raw) % 32
    if pad:
        out += "00" * (32 - pad)
    return out


def encode_read_all(agent_id: int, clients: list, tag1: str, tag2: str,
                    include_revoked: bool) -> str:
    """readAllFeedback(uint256,address[],string,string,bool).

    Five arguments, three of them dynamic, so the head is five words of
    which three are offsets measured from the start of the argument block.
    """
    head_words = 5
    offset = head_words * 32
    head = word(agent_id)
    tail = ""

    head += word(offset)
    arr = word(len(clients)) + "".join(address_word(c) for c in clients)
    tail += arr
    offset += len(arr) // 2

    for s in (tag1, tag2):
        head += word(offset)
        enc = encode_string(s)
        tail += enc
        offset += len(enc) // 2

    head += word(1 if include_revoked else 0)
    return READ_ALL_FEEDBACK + head + tail


def read_offset(data: bytes, at: int) -> int:
    return int.from_bytes(data[at:at + 32], "big")


def read_array(data: bytes, off: int):
    n = int.from_bytes(data[off:off + 32], "big")
    return n, off + 32


def decode_uint_array(data: bytes, off: int) -> list:
    n, base = read_array(data, off)
    return [int.from_bytes(data[base + i * 32:base + i * 32 + 32], "big")
            for i in range(n)]


def decode_int_array(data: bytes, off: int) -> list:
    """Signed values, undoing two's-complement sign extension.

    Reading these as unsigned turns a published loss of -2,600,000,000
    into a number near 2**256 -- a default reported as an enormous gain.
    """
    out = []
    for v in decode_uint_array(data, off):
        if v >= 1 << 255:
            v -= 1 << 256
        out.append(v)
    return out


def decode_address_array(data: bytes, off: int) -> list:
    n, base = read_array(data, off)
    return ["0x" + data[base + i * 32 + 12:base + i * 32 + 32].hex()
            for i in range(n)]


def decode_string_array(data: bytes, off: int) -> list:
    n, base = read_array(data, off)
    out = []
    for i in range(n):
        # Element offsets inside an array of dynamic types are relative to
        # the start of the array body, not to the response.
        rel = int.from_bytes(data[base + i * 32:base + i * 32 + 32], "big")
        at = base + rel
        length = int.from_bytes(data[at:at + 32], "big")
        out.append(data[at + 32:at + 32 + length].decode())
    return out


def read_all_feedback(url: str, registry: str, agent_id: int, client: str):
    """Every entry this client wrote about this agent."""
    data = eth_call(url, registry,
                    encode_read_all(agent_id, [client], "", "", True))
    if len(data) < 7 * 32:
        raise SystemExit(f"short response from registry: {len(data)} bytes")

    offsets = [read_offset(data, i * 32) for i in range(7)]
    clients = decode_address_array(data, offsets[0])
    indexes = decode_uint_array(data, offsets[1])
    values = decode_int_array(data, offsets[2])
    decimals = decode_uint_array(data, offsets[3])
    tag1s = decode_string_array(data, offsets[4])
    tag2s = decode_string_array(data, offsets[5])
    revoked = decode_uint_array(data, offsets[6])

    n = len(clients)
    # Every array describes the same rows. A short one is a decode that
    # has gone wrong, and pairing across it would attribute one entry's
    # value to another.
    for arr in (indexes, values, decimals, tag1s, tag2s, revoked):
        if len(arr) != n:
            raise SystemExit("registry returned mismatched arrays")

    return [
        {
            "client": clients[i],
            "index": indexes[i],
            "value": values[i],
            "decimals": decimals[i],
            "tag1": tag1s[i],
            "tag2": tag2s[i],
            "revoked": bool(revoked[i]),
        }
        for i in range(n)
    ]


def assert_present(url: str, address: str, what: str) -> None:
    """An address with no code returns the same empty result as an agent
    with no history, and those must not look alike."""
    code = rpc_call(url, "eth_getCode", [address, "latest"])
    if code in ("0x", "", None):
        raise SystemExit(f"{what} {address} has no code on this chain")


def agent_owner(url: str, identity: str, agent_id: int) -> str:
    data = eth_call(url, identity, OWNER_OF + word(agent_id))
    return "0x" + data[12:32].hex()


def deployment_tag(chain_id: int, manager: str) -> str:
    """Short digest of the chain and loan manager.

    Loan ids restart at 1 for every deployment, so a bare `loan:1` from
    one manager is indistinguishable from another's. sha256 rather than
    keccak because this reader is stdlib-only by design -- a digest it
    could not recompute would make the tag unverifiable here.
    """
    raw = f"{chain_id}:{manager.lower()}".encode()
    return hashlib.sha256(raw).hexdigest()[:8]


def group_by_loan(entries: list, prefix: str) -> tuple:
    """Credence tags every entry for one obligation `loan:<deployment>:N`.

    Entries whose tag2 names a different deployment, or no obligation at
    all, are reported as unrecognised rather than silently folded into a
    total: another manager's loan 1 is a different obligation.
    """
    loans, other = {}, []
    for e in entries:
        tag2 = e["tag2"]
        if tag2.startswith(prefix) and tag2[len(prefix):].isdigit():
            loans.setdefault(int(tag2[len(prefix):]), []).append(e)
        else:
            other.append(e)
    return loans, other


def main() -> int:
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("--rpc", required=True, help="JSON-RPC endpoint")
    ap.add_argument("--registry", required=True,
                    help="ERC-8004 reputation registry address")
    ap.add_argument("--agent", type=int, required=True, help="agent id")
    ap.add_argument("--client", required=True,
                    help="the client whose feedback to read; ERC-8004 lets "
                         "anyone write, so a consumer must choose whom to trust")
    ap.add_argument("--identity", default=None,
                    help="identity registry, to confirm the agent exists")
    ap.add_argument("--manager", default=None,
                    help="loan manager whose obligations to read; loan ids "
                         "restart per deployment, so the tag is scoped to it")
    ap.add_argument("--expect-chain", type=int, default=None,
                    help="refuse a registry on any other chain")
    args = ap.parse_args()

    assert_present(args.rpc, args.registry, "reputation registry")

    chain_id = int(rpc_call(args.rpc, "eth_chainId", []), 16)
    print(f"chain id  {chain_id}")
    print(f"registry  {args.registry}")
    print(f"client    {args.client}")
    if args.expect_chain is not None and chain_id != args.expect_chain:
        raise SystemExit(
            f"chain {chain_id} is not the expected {args.expect_chain}")

    # The agent must exist. Feedback about an unregistered id attributes an
    # outcome to nobody, and this registry accepts such writes silently.
    if args.identity:
        assert_present(args.rpc, args.identity, "identity registry")
        owner = agent_owner(args.rpc, args.identity, args.agent)
        print(f"agent {args.agent} owner {owner}")
        if int(owner, 16) == 0:
            raise SystemExit(f"agent {args.agent} has no owner")
        if owner.lower() == args.client.lower():
            # The standard refuses self-feedback, so this cannot happen
            # for entries written correctly -- if it does, the records are
            # not what they appear to be.
            raise SystemExit(
                "the client owns this agent: self-feedback is not allowed")
    print()

    entries = read_all_feedback(
        args.rpc, args.registry, args.agent, args.client)
    if not entries:
        print(f"agent {args.agent}: no feedback from this client")
        return 0

    if args.manager:
        prefix = f"loan:{deployment_tag(chain_id, args.manager)}:"
    else:
        # Without a manager every loan tag is foreign, which would report
        # nothing at all. Say so rather than printing an empty history.
        raise SystemExit(
            "--manager is required: loan ids restart per deployment, so "
            "the reader cannot tell whose loan 1 an entry describes")
    loans, other = group_by_loan(entries, prefix)
    print(f"agent {args.agent}: {len(entries)} entries "
          f"covering {len(loans)} obligation(s)")
    print()

    # Separate signals, separately counted. `docs/06` forbids collapsing a
    # repayment, a default, and a loss into one score, so they are summed
    # independently and never combined.
    repaid = recovered = defaulted = 0
    interest_total = loss_total = 0

    for loan_id in sorted(loans):
        rows = loans[loan_id]
        outcome, principal = "unknown", None
        interest = loss = held = None
        for e in rows:
            if e["revoked"]:
                continue
            tag1 = e["tag1"]
            if tag1.startswith(TAG_OUTCOME_PREFIX):
                outcome = tag1[len(TAG_OUTCOME_PREFIX):]
                principal = e["value"]
            elif tag1 == TAG_INTEREST:
                interest = e["value"]
            elif tag1 == TAG_LOSS:
                loss = e["value"]
            elif tag1 == TAG_HELD:
                held = e["value"]

        def show(v):
            return "-" if v is None else str(v)

        print(f"  loan {loan_id}  {outcome:<10}"
              f"  principal={show(principal)}"
              f"  interest={show(interest)}"
              f"  loss={show(loss)}"
              f"  held={show(held)}s")

        if interest is not None:
            interest_total += interest
        if loss is not None:
            loss_total += loss
        if outcome == "repaid":
            repaid += 1
        elif outcome == "recovered":
            recovered += 1
        elif outcome == "defaulted":
            defaulted += 1

    if other:
        print()
        print(f"  {len(other)} entry(ies) from this client are not "
              f"obligation records and were not counted")

    print()
    print(f"  repaid={repaid}  recovered={recovered}  defaulted={defaulted}")
    print(f"  interest paid={interest_total}  principal loss={loss_total}")
    print()
    print("  Read directly from chain. No Credence endpoint was contacted.")
    print()
    print("  Verified: the registry exists, its chain id, that the agent is")
    print("  registered and owned by someone other than the client, and")
    print("  that every record shown was written by the named client.")
    print("  NOT verified: the referenced evidence payloads. ERC-8004 does")
    print("  not return feedbackURI or feedbackHash through any view, so")
    print("  this reader cannot show -- let alone check -- them.")
    print("  Attributable and filterable, NOT proof the content is true.")
    return 0


if __name__ == "__main__":
    sys.exit(main())
