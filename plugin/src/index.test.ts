import { describe, expect, it, vi, afterEach } from "vitest";
import { CredenceClient, CredenceError, commands } from "./index";

const binding = {
  identity_registry_chain_id: "10143",
  identity_registry: "0x7e5F4552091A69125d5DfCb7b8C2659029395Bdf",
  agent_id: "1830",
  operator: "0x2B5AD5c4795c026514f8317c7a215E218DcCD6cF",
  credit_account: "0x2B5AD5c4795c026514f8317c7a215E218DcCD6cF",
  facility_id: "1",
  account_state_version: 3,
  config_version: 1,
  config_hash: "0x" + "11".repeat(32),
  policy_epoch: 1,
  policy_hash: "0x" + "22".repeat(32),
};

afterEach(() => vi.unstubAllGlobals());

function stub(status: number, body: unknown) {
  const calls: Array<{ url: string; init: RequestInit }> = [];
  vi.stubGlobal("fetch", (url: string, init: RequestInit) => {
    calls.push({ url, init });
    return Promise.resolve(
      new Response(JSON.stringify(body), { status }),
    );
  });
  return calls;
}

// A revert is the contracts refusing. Reporting it as success would have
// an agent act on a debt it still owes -- the one failure this client
// exists to prevent.
it("surfaces a revert rather than swallowing it", async () => {
  stub(502, {
    error: "publish_failed",
    detail: "execution reverted: custom error 0x90b8ec18",
  });
  const client = new CredenceClient({ baseUrl: "http://x" });

  await expect(client.repay(7)).rejects.toThrow(CredenceError);
  await expect(client.repay(7)).rejects.toThrow(/0x90b8ec18/);
});

it("reads a loan status", async () => {
  stub(200, { loan_id: 1, debt_due: "4006575343", block: 8 });
  const client = new CredenceClient({ baseUrl: "http://x" });

  const status = await client.loan();
  expect(status.loan_id).toBe(1);
  expect(status.debt_due).toBe("4006575343");
});

// The debt is a STRING all the way through. A settlement balance beyond
// 2^53 read as a JSON number would be silently wrong in the direction
// that flatters the borrower.
it("keeps the debt as a string", async () => {
  stub(200, { loan_id: 1, debt_due: "18446744073709551615", block: 8 });
  const client = new CredenceClient({ baseUrl: "http://x" });

  const status = await client.loan();
  expect(status.debt_due).toBe("18446744073709551615");
  expect(typeof status.debt_due).toBe("string");
});

it("sends the bearer token when configured", async () => {
  const calls = stub(200, { loan_id: 0, debt_due: "0", block: 1 });
  const client = new CredenceClient({ baseUrl: "http://x", token: "secret" });

  await client.loan();
  const headers = calls[0]!.init.headers as Record<string, string>;
  expect(headers["Authorization"]).toBe("Bearer secret");
});

// A principal past 2^53 cannot survive JSON.stringify exactly, so it is
// refused here rather than sent as a different number than requested.
it("refuses a principal a JSON number cannot carry", async () => {
  stub(200, {});
  const client = new CredenceClient({ baseUrl: "http://x" });

  await expect(
    client.borrow(BigInt(Number.MAX_SAFE_INTEGER) + 1n, binding),
  ).rejects.toThrow(/exceeds what a JSON number carries/);
});

it("refuses a non-positive principal", async () => {
  stub(200, {});
  const client = new CredenceClient({ baseUrl: "http://x" });
  await expect(client.borrow(0, binding)).rejects.toThrow(/must be positive/);
});

// Omitting the id repays the ACTIVE loan, read from chain. A cached id
// could name a loan since repaid or moved into recovery.
it("asks the server which loan is active", async () => {
  const calls = stub(200, { tx: "0xabc", action: "repay(1)" });
  const client = new CredenceClient({ baseUrl: "http://x" });

  await client.repay();
  expect(JSON.parse(calls[0]!.init.body as string)).toEqual({ loan_id: 0 });
});

it("normalises a trailing slash in the base url", async () => {
  const calls = stub(200, { status: "ok" });
  const client = new CredenceClient({ baseUrl: "http://x///" });

  await client.health();
  expect(calls[0]!.url).toBe("http://x/health");
});

describe("the commands a MetaMask agent sees", () => {
  it("exposes exactly the four operations", () => {
    const names = commands(new CredenceClient({ baseUrl: "http://x" })).map(
      (c) => c.name,
    );
    expect(names).toEqual([
      "credence.health",
      "credence.loan",
      "credence.borrow",
      "credence.repay",
    ]);
  });
});
