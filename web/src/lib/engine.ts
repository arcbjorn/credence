/**
 * The risk engine, running in this tab.
 *
 * `docs/09` beat 8 asks a reader to confirm that a retained assessment
 * reproduces. This module loads the SAME Rust engine the gateway and CLI
 * use, compiled to WebAssembly, so the check in the browser is the same
 * computation rather than a second implementation that could drift.
 *
 * Loaded lazily. The artifact is ~137KB gzipped and only the replay screen
 * needs it, so the first paint never waits on it.
 */
import build from "../engine/build.json";

export interface Difference {
  field: string;
  retained: string;
  replayed: string;
}

export interface ReplayReport {
  matched: boolean;
  assessment_id: string;
  input_hash: string;
  differences: Difference[];
}

type EngineModule = {
  evaluate(inputJson: string, policyJson: string): string;
  replay(assessmentJson: string, inputJson: string, policyJson: string): ReplayReport;
  model_hash(): string;
};

let pending: Promise<EngineModule> | null = null;

/**
 * Instantiate the engine once and reuse it.
 *
 * The promise itself is cached rather than the module, so two screens
 * mounting at the same time share one instantiation instead of racing.
 */
export function loadEngine(): Promise<EngineModule> {
  if (pending) return pending;
  pending = (async () => {
    const mod = await import("../engine/risk_wasm.js");
    await mod.default();
    return mod as unknown as EngineModule;
  })();
  return pending;
}

/**
 * The model hash this build carries, available without instantiating.
 *
 * A MATCH means nothing if the reader cannot tell which engine produced
 * it, and the replay screen shows this beside every result.
 */
export const MODEL_HASH: string = build.model_hash;

/** Size of the engine artifact, for the "what am I running" panel. */
export const WASM_BYTES: number = build.wasm_bytes;
