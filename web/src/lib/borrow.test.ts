import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";
import init, { evaluate } from "../engine/risk_wasm.js";
import {
  applyRequest,
  approved,
  interestOver,
  LEVERS,
  priceRows,
  readQuote,
  requestFromSnapshot,
  type Request,
} from "./borrow";

/**
 * The borrow desk, checked against the engine it claims to run.
 *
 * These assert the PROPERTIES the screen's argument rests on, not a
 * transcript of one evaluation: that record lowers the price, that
 * volatility lowers the ceiling, that a request over the ceiling is
 * refused rather than repriced. A fixture of exact figures would fail on
 * every policy change and teach a reader to regenerate rather than to
 * look.
 *
 * The one thing pinned exactly is the RANGE of each control, because a
 * slider whose travel produces no change in the engine's answer is a
 * control that lies about what the model reads.
 */
const ROOT = resolve(process.cwd(), "..");
const read = (p: string) => readFileSync(`${ROOT}/${p}`, "utf8");

async function engine() {
  await init({
    module_or_path: readFileSync(`${ROOT}/web/src/engine/risk_wasm_bg.wasm`),
  });
  return evaluate;
}

const snapshot = () =>
  JSON.parse(read("fixtures/demo/repay-loan3-origination.json")) as unknown;
const policy = () => read("config/engine-policy.json");

const quoteFor = async (over: Partial<Request>) => {
  const run = await engine();
  const base = requestFromSnapshot(snapshot());
  const input = applyRequest(snapshot(), { ...base, ...over });
  return readQuote(run(JSON.stringify(input), policy()));
};

describe("the borrow desk", () => {
  it("opens on the request the snapshot actually carried", () => {
    const request = requestFromSnapshot(snapshot());
    // The retained loan asked for 4,000 settlement against a real record.
    expect(request.requested_principal).toBe("4000000000");
    expect(request.weighted_repayments_ppm).toBeGreaterThan(0);
  });

  it("leaves the retained snapshot untouched when a request is applied", () => {
    // Every screen reads the same cached object. Mutating it here would
    // corrupt the replay screen's input, and the corruption would look
    // like engine drift.
    const original = snapshot();
    const before = JSON.stringify(original);
    applyRequest(original, {
      requested_principal: "9000000000",
      weighted_repayments_ppm: 999_999,
      resolved_defaults: 5,
      realized_volatility_bps: 8_000,
    });
    expect(JSON.stringify(original)).toBe(before);
  });

  it("does not rewrite the content hash of evidence it edits", () => {
    // The engine does not verify these, so rewriting one would forge
    // agreement between an edited value and a hash computed over
    // different bytes. Staleness is what makes an edit legible.
    const original = snapshot() as any;
    const edited = applyRequest(original, {
      ...requestFromSnapshot(original),
      weighted_repayments_ppm: 900_000,
    }) as any;
    expect(edited.history.value.weighted_repayments_ppm).toBe(900_000);
    expect(edited.history.content_hash).toBe(original.history.content_hash);
  });

  it("prices a better record lower", async () => {
    const poor = await quoteFor({ weighted_repayments_ppm: 0 });
    const strong = await quoteFor({ weighted_repayments_ppm: 1_000_000 });
    expect(approved(poor)).toBe(true);
    expect(approved(strong)).toBe(true);
    expect(strong.apr_bps!).toBeLessThan(poor.apr_bps!);
  });

  it("raises the ceiling for a better record", async () => {
    const poor = await quoteFor({ weighted_repayments_ppm: 0 });
    const strong = await quoteFor({ weighted_repayments_ppm: 1_000_000 });
    expect(BigInt(strong.agent_capacity!)).toBeGreaterThan(
      BigInt(poor.agent_capacity!),
    );
  });

  it("surcharges a record carrying defaults", async () => {
    // Held at a record where the growth cap is not the binding
    // constraint, because the surcharge is invisible underneath it.
    const clean = await quoteFor({
      weighted_repayments_ppm: 750_000,
      resolved_defaults: 0,
    });
    const scarred = await quoteFor({
      weighted_repayments_ppm: 750_000,
      resolved_defaults: 3,
    });
    expect(scarred.apr_bps!).toBeGreaterThan(clean.apr_bps!);
    // Priced, not banned. A record with a default is still a record.
    expect(approved(scarred)).toBe(true);
  });

  it("lowers the ceiling as volatility rises", async () => {
    const calm = await quoteFor({ realized_volatility_bps: 0 });
    const wild = await quoteFor({ realized_volatility_bps: 9_000 });
    expect(BigInt(wild.agent_capacity!)).toBeLessThan(
      BigInt(calm.agent_capacity!),
    );
    expect(wild.agent_binding).toBe("StressCapacity");
  });

  it("refuses a request above the ceiling rather than repricing it", async () => {
    const quote = await quoteFor({ requested_principal: "60000000000" });
    expect(approved(quote)).toBe(false);
    expect(quote.unavailable).toBe(false);
    expect(quote.apr_bps).toBeUndefined();
    // The refusal still reports the ceiling, so a reader learns what to
    // ask for instead of only that they were told no.
    expect(BigInt(quote.agent_capacity!)).toBeGreaterThan(0n);
  });

  it("returns stress rows carrying no debt when it refuses", async () => {
    // The engine still emits three scenarios on a refusal, but nothing
    // was priced, so `debt_with_reserved_interest` is zero and every row
    // trivially passes. The screen must not render these beside a
    // refusal: "survives" against no debt reads as the request having
    // passed the test it was actually refused over.
    const quote = await quoteFor({ requested_principal: "60000000000" });
    expect(approved(quote)).toBe(false);
    for (const row of quote.stress ?? []) {
      expect(row.debt_with_reserved_interest).toBe("0");
      expect(row.passes).toBe(true);
    }
  });

  it("names the constraint that bound every answer", async () => {
    for (const vol of [0, 5_000, 9_000]) {
      const quote = await quoteFor({ realized_volatility_bps: vol });
      expect(quote.agent_binding).toBeTruthy();
    }
  });

  it("distinguishes a refusal from an engine that could not evaluate", async () => {
    const run = await engine();
    // A policy the snapshot does not declare: the engine refuses to
    // evaluate at all. The desk must not render this as a credit refusal.
    // A field the policy REALLY carries. An invented key is ignored by
    // the deserializer and the content hash still matches, so the engine
    // approves and the test proves nothing -- which is what an earlier
    // version of it did.
    const bogus = JSON.parse(policy());
    bogus.account_hard_cap = String(BigInt(bogus.account_hard_cap) + 1n);
    const input = applyRequest(snapshot(), requestFromSnapshot(snapshot()));
    const quote = readQuote(
      run(JSON.stringify(input), JSON.stringify(bogus)),
    );
    expect(quote.unavailable).toBe(true);
  });

  it("breaks the price into terms that sum to the quoted rate", async () => {
    const quote = await quoteFor({ weighted_repayments_ppm: 750_000 });
    const rows = priceRows(quote);
    expect(rows.length).toBeGreaterThan(0);
    const parts = rows.filter((r) => r.kind !== "total");
    const total = rows.find((r) => r.kind === "total")!;
    const summed = parts.reduce((acc, r) => acc + r.bps, 0);
    // The breakdown is only worth showing if it closes. A clamped rate is
    // the documented exception: the engine bounds the result, so the
    // parts legitimately do not sum to it.
    if (!quote.price_breakdown!.clamped) {
      expect(summed).toBe(total.bps);
    }
  });

  it("gives every control a range the engine actually responds to", async () => {
    // A slider whose travel changes no output is a control that lies
    // about what the model reads. Each one is swept end to end and must
    // move something.
    // The principal is the one lever carried as a string. The engine
    // refuses an integer there outright -- `invalid type: integer,
    // expected a string` -- which is the precision rule enforced at the
    // boundary rather than trusted, so the sweep has to honour it too.
    const at = (lever: (typeof LEVERS)[number], v: number) =>
      ({
        [lever.id]: lever.id === "requested_principal" ? String(v) : v,
      }) as Partial<Request>;

    for (const lever of LEVERS) {
      const low = await quoteFor(at(lever, lever.min));
      const high = await quoteFor(at(lever, lever.max));
      const moved =
        low.apr_bps !== high.apr_bps ||
        low.agent_capacity !== high.agent_capacity ||
        low.decision !== high.decision;
      expect(moved, `${lever.id} moved nothing between its bounds`).toBe(true);
    }
  });

  it("passes the principal as a string, never as a double", async () => {
    // uint256 base units lose precision as JSON numbers. The slider works
    // in numbers; the value that reaches the engine must not.
    const quote = await quoteFor({ requested_principal: "4000000000" });
    expect(typeof quote.requested_principal).toBe("string");
  });

  it("computes interest as simple actual/365 on the principal", () => {
    // 1,000 settlement at 12% for a full year.
    expect(interestOver("1000000000", 1200, 31_536_000)).toBe("120000000");
    // Half a year is half the interest.
    expect(interestOver("1000000000", 1200, 15_768_000)).toBe("60000000");
  });
});
