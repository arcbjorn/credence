#!/usr/bin/env python3
"""Time what the prototype sends: transactions to the chain, snapshots to the engine.

A plain forwarder in front of a JSON-RPC endpoint and the risk service, so
`credence-scenario` and `forge script` run unmodified and every number is a
request they made. Three intervals, each measured on this machine's clock:

  chain   eth_sendRawTransaction arrives here -> its receipt is first returned
  index   that receipt is first returned      -> the loan's row shows the event
  engine  POST /v1/assess arrives here        -> the assessment is returned

The receipt is polled over a kept-alive connection, so "first returned" is
late by at most one round trip to the RPC. That round trip is measured at
startup and reported beside the results rather than subtracted from them.

Writes one JSON object per observation to --out. Standard library only.
"""

import argparse
import http.client
import json
import queue
import threading
import time
import urllib.parse
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

LOCK = threading.Lock()
SENT = queue.Queue()


def record(out, obj):
    with LOCK, open(out, "a") as f:
        f.write(json.dumps(obj) + "\n")


class Upstream:
    """One kept-alive connection per thread, so no request pays a handshake."""

    def __init__(self, url):
        u = urllib.parse.urlsplit(url)
        self.https = u.scheme == "https"
        self.host = u.hostname or "localhost"
        self.port = u.port or (443 if self.https else 80)
        self.path = u.path or "/"
        self.local = threading.local()

    def _conn(self):
        c = getattr(self.local, "conn", None)
        if c is None:
            cls = http.client.HTTPSConnection if self.https else http.client.HTTPConnection
            c = cls(self.host, self.port, timeout=60)
            self.local.conn = c
        return c

    def send(self, method, path, body=None, headers=None):
        for attempt in (0, 1):
            c = self._conn()
            try:
                c.request(method, path, body=body, headers=headers or {})
                r = c.getresponse()
                return r.status, r.read(), r.getheader("Content-Type") or "application/json"
            except (http.client.HTTPException, OSError):
                c.close()
                self.local.conn = None
                if attempt:
                    raise
        raise AssertionError("unreachable")

    def rpc(self, method, params):
        body = json.dumps({"jsonrpc": "2.0", "id": 1, "method": method, "params": params})
        _, data, _ = self.send("POST", self.path, body, {"Content-Type": "application/json"})
        return json.loads(data).get("result")


def handler(upstream, on_exchange):
    class Forward(BaseHTTPRequestHandler):
        protocol_version = "HTTP/1.1"

        def _forward(self, method):
            body = self.rfile.read(int(self.headers.get("Content-Length") or 0))
            arrived = time.time()
            path = upstream.path if upstream.path != "/" and self.path == "/" else self.path
            status, data, ctype = upstream.send(
                method, path, body or None, {"Content-Type": "application/json"})
            on_exchange(self.path, body, data, arrived, time.time())
            self.send_response(status)
            self.send_header("Content-Type", ctype)
            self.send_header("Content-Length", str(len(data)))
            self.end_headers()
            self.wfile.write(data)

        def do_POST(self):
            self._forward("POST")

        def do_GET(self):
            self._forward("GET")

        def log_message(self, format, *args):
            pass

    return Forward


def main():
    p = argparse.ArgumentParser()
    p.add_argument("--rpc", required=True, help="upstream JSON-RPC url")
    p.add_argument("--rpc-port", type=int, required=True)
    p.add_argument("--risk", required=True, help="upstream risk service url")
    p.add_argument("--risk-port", type=int, required=True)
    p.add_argument("--graphql", default="", help="indexer GraphQL url; empty skips the index interval")
    p.add_argument("--manager", required=True)
    p.add_argument("--chain-id", required=True)
    p.add_argument("--originated-topic", required=True)
    p.add_argument("--repaid-topic", required=True)
    p.add_argument("--out", required=True)
    a = p.parse_args()

    rpc_up, risk_up = Upstream(a.rpc), Upstream(a.risk)
    manager = a.manager.lower()
    # Originated creates the row; Repaid moves it to "Repaid". Waiting for
    # "Active" instead would miss a loan repaid before the index caught up.
    topics = {a.originated_topic.lower(): None, a.repaid_topic.lower(): "Repaid"}

    # The round trip every chain figure below includes once.
    probe = Upstream(a.rpc)
    rtts = []
    for _ in range(20):
        t = time.time()
        probe.rpc("eth_blockNumber", [])
        rtts.append(round((time.time() - t) * 1000, 1))
    record(a.out, {"kind": "rtt", "ms": rtts})

    def on_rpc(_path, body, data, arrived, _done):
        try:
            req, resp = json.loads(body), json.loads(data)
        except ValueError:
            return
        reqs = req if isinstance(req, list) else [req]
        resps = {r.get("id"): r for r in (resp if isinstance(resp, list) else [resp])
                 if isinstance(r, dict)}
        for r in reqs:
            if isinstance(r, dict) and r.get("method") == "eth_sendRawTransaction":
                got = resps.get(r.get("id"), {})
                if got.get("result"):
                    SENT.put((got["result"], arrived))
                else:
                    record(a.out, {"kind": "send_error", "error": got.get("error")})

    def on_risk(path, body, _data, arrived, done):
        if path.startswith("/v1/assess"):
            record(a.out, {"kind": "engine", "bytes_in": len(body),
                           "ms": round((done - arrived) * 1000, 1)})

    def watch_index(loan, expect, tx, confirmed):
        index = Upstream(a.graphql)
        query = json.dumps({"query": '{ Loan(where: {id: {_eq: "%s-%d"}}) { status } }'
                            % (a.chain_id, loan)})
        deadline = confirmed + 600
        while time.time() < deadline:
            try:
                _, data, _ = index.send("POST", index.path, query,
                                        {"Content-Type": "application/json"})
                rows = json.loads(data).get("data", {}).get("Loan") or []
            except (OSError, ValueError, http.client.HTTPException):
                rows = []
            if rows and (expect is None or rows[0]["status"] == expect):
                now = time.time()
                record(a.out, {"kind": "index", "loan": loan,
                               "event": "Originated" if expect is None else "Repaid",
                               "tx": tx, "ms": round((now - confirmed) * 1000, 1)})
                return
            time.sleep(0.05)
        record(a.out, {"kind": "index_timeout", "loan": loan, "tx": tx})

    def watch_receipts():
        chain = Upstream(a.rpc)
        pending = {}
        while True:
            try:
                tx, arrived = SENT.get(timeout=0.05) if not pending else SENT.get_nowait()
                pending[tx] = arrived
                continue
            except queue.Empty:
                pass
            # 10 ms between passes: a local node answers in about 1 ms and
            # would otherwise be polled flat out.
            time.sleep(0.01)
            for tx in list(pending):
                receipt = chain.rpc("eth_getTransactionReceipt", [tx])
                if not receipt:
                    continue
                confirmed = time.time()
                arrived = pending.pop(tx)
                sent = chain.rpc("eth_getTransactionByHash", [tx]) or {}
                block = chain.rpc("eth_getBlockByNumber", [receipt["blockNumber"], False]) or {}
                record(a.out, {
                    "kind": "tx", "tx": tx,
                    "to": receipt.get("to"),
                    "selector": (sent.get("input") or "0x")[:10],
                    "status": int(receipt["status"], 16),
                    "block": int(receipt["blockNumber"], 16),
                    "block_time": int(block.get("timestamp", "0x0"), 16),
                    "gas": int(receipt["gasUsed"], 16),
                    "ms": round((confirmed - arrived) * 1000, 1),
                })
                if not a.graphql:
                    continue
                for log in receipt.get("logs", []):
                    t = (log.get("topics") or [""])[0].lower()
                    if log["address"].lower() == manager and t in topics:
                        loan = int(log["topics"][1], 16)
                        record(a.out, {"kind": "index_wait", "loan": loan, "tx": tx})
                        threading.Thread(target=watch_index, daemon=True,
                                         args=(loan, topics[t], tx, confirmed)).start()

    threading.Thread(target=watch_receipts, daemon=True).start()
    for port, up, cb in ((a.rpc_port, rpc_up, on_rpc), (a.risk_port, risk_up, on_risk)):
        server = ThreadingHTTPServer(("127.0.0.1", port), handler(up, cb))
        threading.Thread(target=server.serve_forever, daemon=True).start()
    print("latency proxy ready", flush=True)
    threading.Event().wait()


if __name__ == "__main__":
    main()
