/**
 * The deployment, read live from Monad testnet.
 *
 * Everything else in this interface is a static bundle, on purpose:
 * `docs/09` requires every displayed number to come from a retained
 * fixture, and a generated bundle enforces that structurally. This
 * module is the one deliberate exception, and it is honest about being
 * one -- it reads the chain directly, so what it shows is true at the
 * moment it was fetched and nowhere else.
 *
 * **Read-only by default.** Origination cannot happen from a browser and
 * this module does not pretend otherwise: `LoanManager.originate` calls
 * `oracle.consumeQuote`, which only matches a quote the ATTESTOR
 * published. That key stays with the operator (`docs/05`), so a visitor
 * holding a wallet cannot conjure terms. What a visitor CAN do is the
 * borrower's own side of an existing loan -- repaying it -- and that is
 * the only transaction this module ever builds.
 *
 * Addresses come from the deployment manifest carried in the bundle,
 * never from a literal here: `contracts/deployments/10143.json` is the
 * single source of truth, and copies in prose went stale three times.
 */

import {
  createPublicClient,
  createWalletClient,
  custom,
  http,
  type Address,
  type PublicClient,
} from "viem";
import { loadManifest, type Manifest } from "./data";

/** Monad testnet, as the manifest records it. */
export const CHAIN_ID = 10143;
export const RPC_URL = "https://testnet-rpc.monad.xyz";
export const EXPLORER = "https://testnet.monadexplorer.com";

export const monadTestnet = {
  id: CHAIN_ID,
  name: "Monad Testnet",
  nativeCurrency: { name: "MON", symbol: "MON", decimals: 18 },
  rpcUrls: { default: { http: [RPC_URL] } },
  blockExplorers: { default: { name: "Monad Explorer", url: EXPLORER } },
} as const;

/** Only the fragments this module calls. A full ABI would be noise. */
export const VAULT_ABI = [
  { type: "function", name: "idleCash", inputs: [], outputs: [{ type: "uint256" }], stateMutability: "view" },
  { type: "function", name: "outstandingPrincipal", inputs: [], outputs: [{ type: "uint256" }], stateMutability: "view" },
  { type: "function", name: "collectedInterest", inputs: [], outputs: [{ type: "uint256" }], stateMutability: "view" },
  { type: "function", name: "realizedLoss", inputs: [], outputs: [{ type: "uint256" }], stateMutability: "view" },
] as const;

export const MANAGER_ABI = [
  { type: "function", name: "status", inputs: [{ type: "uint256" }], outputs: [{ type: "uint8" }], stateMutability: "view" },
  { type: "function", name: "debtDue", inputs: [{ type: "uint256" }], outputs: [{ type: "uint256" }], stateMutability: "view" },
  { type: "function", name: "accruedInterest", inputs: [{ type: "uint256" }], outputs: [{ type: "uint256" }], stateMutability: "view" },
  { type: "function", name: "agentIdOf", inputs: [{ type: "uint256" }], outputs: [{ type: "uint256" }], stateMutability: "view" },
  { type: "function", name: "repay", inputs: [{ type: "uint256" }], outputs: [], stateMutability: "nonpayable" },
] as const;

/**
 * `LoanManager.Status`, in the contract's own order.
 *
 * Indexed rather than mapped from a name: the enum is positional on
 * chain, and reading it as anything else would mislabel every loan the
 * moment a variant is inserted.
 */
export const STATUSES = ["None", "Active", "Repaid", "Recovering", "Defaulted"] as const;
export type Status = (typeof STATUSES)[number];

export const statusOf = (raw: number): Status => STATUSES[raw] ?? "None";

/** One loan as the chain reports it right now. */
export interface LiveLoan {
  loanId: number;
  status: Status;
  /** Principal plus interest accrued to this block. Zero once resolved. */
  debtDue: bigint;
  accruedInterest: bigint;
  agentId: bigint;
}

/** The vault's four ledgers, kept separate as `docs/05` keeps them. */
export interface LiveVault {
  idleCash: bigint;
  outstandingPrincipal: bigint;
  collectedInterest: bigint;
  realizedLoss: bigint;
}

export interface LiveState {
  vault: LiveVault;
  loans: LiveLoan[];
  blockNumber: bigint;
  /** When this was read. The figures are true at this moment only. */
  fetchedAt: number;
  addresses: Record<string, string>;
  agentId: number | undefined;
  /**
   * Why a loan id produced nothing.
   *
   * An empty `loans` list with entries here means the reads FAILED; an
   * empty list with none means the chain genuinely has no loans. The
   * screen must say which, because the first version of this silently
   * reported "no loans" for a deployment carrying three.
   */
  scanErrors: string[];
}

export const client = (): PublicClient =>
  createPublicClient({
    chain: monadTestnet,
    transport: http(RPC_URL),
    // The four ledgers and every loan arrive in one round trip rather
    // than a dozen. A public RPC rate-limits, and a screen that fires
    // fifteen sequential reads is the thing it rate-limits.
    batch: { multicall: true },
  }) as PublicClient;

/**
 * Read the deployment's current state.
 *
 * `maxLoans` is a scan bound, not a count: the manager numbers loans from
 * 1 and exposes no total, so the only way to enumerate is to walk ids
 * until they come back empty. Loans with status `None` are dropped.
 */
export async function readLive(maxLoans = 8): Promise<LiveState> {
  const manifest: Manifest = await loadManifest();
  const deployment = manifest.deployment ?? {};
  const addresses = deployment.addresses ?? {};
  const vaultAddress = addresses["vault"] as Address | undefined;
  const managerAddress = addresses["loan_manager"] as Address | undefined;

  if (!vaultAddress || !managerAddress) {
    throw new Error(
      "The bundle's manifest carries no vault or loan manager address, " +
        "so there is no deployment to read.",
    );
  }
  if (deployment.chain_id !== undefined && deployment.chain_id !== CHAIN_ID) {
    // Reading chain A's addresses against chain B's RPC returns whatever
    // happens to live at those addresses there -- plausible numbers for
    // a deployment that does not exist.
    throw new Error(
      `The manifest records chain ${deployment.chain_id}, but this screen ` +
        `reads chain ${CHAIN_ID}. Refusing to mix them.`,
    );
  }

  const rpc = client();
  const vaultCall = (name: (typeof VAULT_ABI)[number]["name"]) =>
    ({ address: vaultAddress, abi: VAULT_ABI, functionName: name }) as const;

  const [blockNumber, idle, outstanding, interest, loss] = await Promise.all([
    rpc.getBlockNumber(),
    rpc.readContract(vaultCall("idleCash")),
    rpc.readContract(vaultCall("outstandingPrincipal")),
    rpc.readContract(vaultCall("collectedInterest")),
    rpc.readContract(vaultCall("realizedLoss")),
  ]);

  const ids = Array.from({ length: maxLoans }, (_, i) => BigInt(i + 1));
  const loans: LiveLoan[] = [];
  const scanErrors: string[] = [];
  const read = await Promise.all(
    ids.map(async (id) => {
      try {
        const [status, debtDue, accrued, agentId] = await Promise.all([
          rpc.readContract({ address: managerAddress, abi: MANAGER_ABI, functionName: "status", args: [id] }),
          rpc.readContract({ address: managerAddress, abi: MANAGER_ABI, functionName: "debtDue", args: [id] }),
          rpc.readContract({ address: managerAddress, abi: MANAGER_ABI, functionName: "accruedInterest", args: [id] }),
          rpc.readContract({ address: managerAddress, abi: MANAGER_ABI, functionName: "agentIdOf", args: [id] }),
        ]);
        return {
          loanId: Number(id),
          status: statusOf(Number(status)),
          debtDue: debtDue as bigint,
          accruedInterest: accrued as bigint,
          agentId: agentId as bigint,
        };
      } catch (e) {
        // A loan id past the end reverts, which is the end of the scan
        // and not worth surfacing. Anything else is a real failure --
        // an RPC that rejected the batch, a wrong ABI -- and swallowing
        // it renders "no loans" for a chain that has them, which is the
        // one wrong answer this screen must not give silently.
        scanErrors.push(
          `loan ${id}: ${e instanceof Error ? e.message.split("\n")[0] : String(e)}`,
        );
        return null;
      }
    }),
  );
  for (const loan of read) {
    if (loan && loan.status !== "None") loans.push(loan);
  }

  return {
    vault: {
      idleCash: idle as bigint,
      outstandingPrincipal: outstanding as bigint,
      collectedInterest: interest as bigint,
      realizedLoss: loss as bigint,
    },
    loans,
    blockNumber,
    fetchedAt: Date.now(),
    addresses: addresses as Record<string, string>,
    agentId: deployment.erc8004?.agent_id,
    // Only meaningful when nothing was read: a scan that found loans and
    // then hit the end of the range is working exactly as intended.
    scanErrors: loans.length === 0 ? scanErrors : [],
  };
}

/** An injected EIP-1193 provider, when the visitor has one. */
interface Eip1193 {
  request(args: { method: string; params?: unknown[] }): Promise<unknown>;
}

export const injected = (): Eip1193 | null => {
  const w = globalThis as { ethereum?: Eip1193 };
  return w.ethereum ?? null;
};

/**
 * Connect a wallet and make sure it is pointed at the right chain.
 *
 * Signing against the wrong chain would send a repayment to whatever
 * lives at these addresses elsewhere, so the chain is switched -- and
 * added if the wallet does not know it -- before any transaction is
 * built. A wallet that refuses both is reported rather than worked
 * around.
 */
export async function connect(): Promise<Address> {
  const provider = injected();
  if (!provider) {
    throw new Error(
      "No wallet found in this browser. This screen reads the chain " +
        "without one; connecting is only needed to repay a loan.",
    );
  }
  const accounts = (await provider.request({
    method: "eth_requestAccounts",
  })) as Address[];
  const account = accounts[0];
  if (!account) throw new Error("The wallet returned no account.");

  const hexId = `0x${CHAIN_ID.toString(16)}`;
  try {
    await provider.request({
      method: "wallet_switchEthereumChain",
      params: [{ chainId: hexId }],
    });
  } catch {
    await provider.request({
      method: "wallet_addEthereumChain",
      params: [
        {
          chainId: hexId,
          chainName: monadTestnet.name,
          nativeCurrency: monadTestnet.nativeCurrency,
          rpcUrls: [RPC_URL],
          blockExplorerUrls: [EXPLORER],
        },
      ],
    });
  }
  return account;
}

/**
 * Repay a loan, signed by the visitor.
 *
 * The ONE state-changing call this interface offers, and it is offered
 * because it is the one a borrower genuinely owns: `repay` pulls the debt
 * from the caller and needs no attestor signature. Origination does, and
 * is therefore absent rather than faked.
 *
 * The caller must hold the settlement token and have approved the
 * manager for the debt. Both are the visitor's own business; this
 * surfaces the revert rather than silently sending approvals on their
 * behalf.
 */
export async function repay(loanId: number): Promise<`0x${string}`> {
  const provider = injected();
  if (!provider) throw new Error("No wallet is connected.");
  const account = await connect();
  const manifest = await loadManifest();
  const manager = manifest.deployment?.addresses?.["loan_manager"] as
    | Address
    | undefined;
  if (!manager) throw new Error("The manifest carries no loan manager address.");

  const wallet = createWalletClient({
    account,
    chain: monadTestnet,
    transport: custom(provider),
  });
  return wallet.writeContract({
    address: manager,
    abi: MANAGER_ABI,
    functionName: "repay",
    args: [BigInt(loanId)],
  });
}

export const txURL = (hash: string) => `${EXPLORER}/tx/${hash}`;
export const addressURL = (address: string) => `${EXPLORER}/address/${address}`;
