import { describe, expect, it } from "vitest";
import {
  CHAIN_ID,
  MANAGER_ABI,
  STATUSES,
  VAULT_ABI,
  addressURL,
  monadTestnet,
  statusOf,
  txURL,
} from "./live";

/**
 * The live screen's contract with the chain.
 *
 * Nothing here touches the network: a test that needs a public RPC to be
 * up turns an unrelated outage into a red build, which is exactly the
 * mistake `perpl.test.ts` documents. What IS pinned is the shape -- the
 * status enum's order, the chain id, and the fact that this module
 * builds no transaction beyond `repay`.
 */
describe("the live deployment reader", () => {
  it("reads the chain the manifest records", () => {
    expect(CHAIN_ID).toBe(10143);
    expect(monadTestnet.id).toBe(CHAIN_ID);
  });

  it("keeps the status enum in the contract's own order", () => {
    // `LoanManager.Status` is positional on chain. Reading it by any
    // other order mislabels every loan.
    expect(STATUSES).toEqual([
      "None",
      "Active",
      "Repaid",
      "Recovering",
      "Defaulted",
    ]);
    expect(statusOf(1)).toBe("Active");
    expect(statusOf(2)).toBe("Repaid");
    expect(statusOf(4)).toBe("Defaulted");
  });

  it("treats an unknown status as None rather than guessing", () => {
    expect(statusOf(99)).toBe("None");
  });

  it("offers exactly one state-changing call, and it is repay", () => {
    // The guarantee this screen rests on: a visitor's wallet is never
    // asked to sign anything but their own repayment. Origination needs
    // the attestor key and must stay absent rather than be simulated.
    const writes = MANAGER_ABI.filter(
      (entry) => entry.stateMutability !== "view",
    ).map((entry) => entry.name);
    expect(writes).toEqual(["repay"]);
  });

  it("reads the vault's four ledgers separately", () => {
    // `docs/05` keeps them separate: merging them would report a
    // receivable as withdrawable cash.
    expect(VAULT_ABI.map((e) => e.name).sort()).toEqual([
      "collectedInterest",
      "idleCash",
      "outstandingPrincipal",
      "realizedLoss",
    ]);
  });

  it("builds explorer links for the chain it reads", () => {
    expect(txURL("0xabc")).toContain("testnet.monadexplorer.com");
    expect(addressURL("0xdef")).toContain("testnet.monadexplorer.com");
  });
});
