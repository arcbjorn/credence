/**
 * Credence as a MetaMask agent plugin.
 *
 * The superpower is **credit**: an agent that can borrow against its own
 * demonstrated behaviour, which no other plugin offers. Four commands —
 * `health`, `loan`, `borrow`, `repay` — over the product API.
 *
 * # What this plugin cannot do
 *
 * It is a client. Every command issues an HTTP request that becomes a
 * transaction the contracts judge:
 *
 *   - `borrow` sends `originate(binding, principal)`. `LoanManager`
 *     reads the oracle's quote for that exact binding and reverts if
 *     there is none, if the facility caps are exceeded, or if the
 *     account already has a loan open.
 *   - `repay` sends `repay(loanId)`. It reverts on a loan that is not
 *     the caller's, already settled, or in recovery.
 *
 * Neither the plugin nor the API it calls can grant credit. That is the
 * whole point: the claim this project makes is that authorization lives
 * on chain, and a convenience layer that could bypass it would refute
 * the claim rather than package it.
 *
 * A failure is reported as a failure. The API returns the revert reason
 * verbatim, and these commands surface it rather than retrying or
 * softening it — an agent told "repaid" when nothing reached the chain
 * would act on a debt it still owes.
 */

/** One account's credit status, read at a pinned block. */
export interface LoanStatus {
  /** Zero when the account has no active loan. */
  loan_id: number;
  /** Principal plus accrued interest, in settlement base units. */
  debt_due: string;
  /** The height every figure was read at. */
  block: number;
}

/** A published transaction. The caller verifies the outcome on chain. */
export interface TxResult {
  tx: string;
  action: string;
}

/**
 * The binding an assessment was accepted under.
 *
 * Supplied by the caller and validated BY THE ORACLE, not by this
 * plugin: `LoanManager.originate` calls `quoteActionHash(binding)`, so a
 * forged or stale binding finds no quote and the transaction reverts.
 *
 * Numbers are strings because a uint256 does not survive a JSON number,
 * and a facility id truncated by a float64 would bind a loan to a
 * different facility while looking correct.
 */
export interface Binding {
  identity_registry_chain_id: string;
  identity_registry: string;
  agent_id: string;
  operator: string;
  credit_account: string;
  facility_id: string;
  account_state_version: number;
  config_version: number;
  config_hash: string;
  policy_epoch: number;
  policy_hash: string;
}

export interface ClientOptions {
  /** Base URL of `credence-productd`. */
  baseUrl: string;
  /** Bearer token, when the API is configured with one. */
  token?: string;
  /** Per-request timeout in milliseconds. */
  timeoutMs?: number;
}

/** An error carrying what the API, or the chain, actually said. */
export class CredenceError extends Error {
  constructor(
    readonly code: string,
    readonly detail: string,
    readonly status: number,
  ) {
    super(`${code}: ${detail}`);
    this.name = "CredenceError";
  }
}

export class CredenceClient {
  readonly #baseUrl: string;
  readonly #token: string | undefined;
  readonly #timeoutMs: number;

  constructor(options: ClientOptions) {
    // Trailing slashes would produce `//v1/loan`, which some proxies
    // rewrite and others 404.
    this.#baseUrl = options.baseUrl.replace(/\/+$/, "");
    this.#token = options.token;
    this.#timeoutMs = options.timeoutMs ?? 30_000;
  }

  /** Is the API reachable? */
  async health(): Promise<{ status: string }> {
    return this.#request("GET", "/health");
  }

  /** What does the account owe right now? */
  async loan(): Promise<LoanStatus> {
    return this.#request("GET", "/v1/loan");
  }

  /**
   * Draw against an accepted assessment.
   *
   * The contracts decide. A binding the oracle never accepted produces a
   * reverted transaction, surfaced here as a `CredenceError` carrying
   * the revert reason.
   */
  async borrow(principal: bigint | number, binding: Binding): Promise<TxResult> {
    // Sent as a number because the API reads it into a uint64; a bigint
    // beyond that range is refused here rather than silently truncated
    // by JSON.stringify.
    const value = BigInt(principal);
    if (value <= 0n) {
      throw new CredenceError("invalid_request", "principal must be positive", 400);
    }
    if (value > BigInt(Number.MAX_SAFE_INTEGER)) {
      throw new CredenceError(
        "invalid_request",
        `principal ${value} exceeds what a JSON number carries exactly`,
        400,
      );
    }
    return this.#request("POST", "/v1/borrow", {
      principal: Number(value),
      binding,
    });
  }

  /**
   * Settle a loan.
   *
   * Omitting the id repays the account's active loan, read from chain
   * rather than remembered: an id this client cached could name a loan
   * that has since been repaid or moved into recovery.
   */
  async repay(loanId?: number): Promise<TxResult> {
    return this.#request("POST", "/v1/repay", { loan_id: loanId ?? 0 });
  }

  async #request<T>(method: string, path: string, body?: unknown): Promise<T> {
    const controller = new AbortController();
    const timer = setTimeout(() => controller.abort(), this.#timeoutMs);

    try {
      const headers: Record<string, string> = {};
      if (body !== undefined) headers["Content-Type"] = "application/json";
      if (this.#token) headers["Authorization"] = `Bearer ${this.#token}`;

      // `body` is omitted rather than set to undefined: with
      // `exactOptionalPropertyTypes` those are different types, and the
      // distinction is real -- a GET with an explicit undefined body is
      // not the same request as one with no body.
      const init: RequestInit = { method, headers, signal: controller.signal };
      if (body !== undefined) init.body = JSON.stringify(body);

      const response = await fetch(`${this.#baseUrl}${path}`, init);

      const text = await response.text();
      let parsed: unknown;
      try {
        parsed = JSON.parse(text);
      } catch {
        throw new CredenceError(
          "unreadable_response",
          text.slice(0, 200),
          response.status,
        );
      }

      if (!response.ok) {
        const err = parsed as { error?: string; detail?: string };
        // The revert reason travels verbatim. An agent told "repaid"
        // when nothing reached the chain would act on a debt it owes.
        throw new CredenceError(
          err.error ?? "request_failed",
          err.detail ?? text.slice(0, 200),
          response.status,
        );
      }
      return parsed as T;
    } finally {
      clearTimeout(timer);
    }
  }
}

/**
 * The commands a MetaMask agent sees.
 *
 * Kept as plain data so the host decides how to register them: the
 * agent SDK's surface is its own concern, and a plugin that hard-coded
 * one version of it would break on the next.
 */
export function commands(client: CredenceClient) {
  return [
    {
      name: "credence.health",
      description: "Is the credit service reachable?",
      run: () => client.health(),
    },
    {
      name: "credence.loan",
      description: "What does this account owe right now?",
      run: () => client.loan(),
    },
    {
      name: "credence.borrow",
      description:
        "Draw against an accepted assessment. The contracts decide; " +
        "an unauthorized draw reverts.",
      run: (args: { principal: number; binding: Binding }) =>
        client.borrow(args.principal, args.binding),
    },
    {
      name: "credence.repay",
      description: "Settle the active loan, or a named one.",
      run: (args: { loanId?: number }) => client.repay(args.loanId),
    },
  ];
}
