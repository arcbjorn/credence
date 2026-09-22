/**
 * Rendering the engine's reasons.
 *
 * `reasons` is a Rust enum serialized by serde: a unit variant becomes a
 * plain string (`"Eligible"`), and a variant carrying data becomes a
 * single-key object (`{ CapacityExceeded: { capacity, limiting } }`).
 *
 * The interface typed the whole array as `string[]` and rendered it with
 * `.join(", ")`, which turned every data-carrying reason into the literal
 * `[object Object]` — including the one on the comparison screen's denied
 * profile, where the reason IS the argument the screen is making.
 *
 * Reported by review, 15 September.
 */

import { bindingPhrase } from "./bindings";

/**
 * Fields naming a `BindingConstraint`, which render as words rather than
 * as the serialized Rust identifier. `CapacityExceeded.limiting` is the
 * field the comparison screen's denial rests on, and it read
 * `StressCapacity` there.
 */
const BINDING_FIELDS = new Set(["limiting", "binding"]);

/** A reason as the engine emits it. */
export type Reason = string | Record<string, unknown>;

/** One field of a structured reason, for rendering as a detail row. */
export interface ReasonField {
  label: string;
  value: string;
}

/** The variant name alone: `CapacityExceeded`, `Eligible`. */
export function reasonName(reason: Reason): string {
  if (typeof reason === "string") return reason;
  const keys = Object.keys(reason);
  return keys[0] ?? "Unknown";
}

/**
 * The fields a structured reason carries, flattened one level.
 *
 * Values are rendered with `String`, never `JSON.stringify`, so a number
 * or a base-unit string reads as itself. A nested object is rendered as
 * JSON rather than silently becoming `[object Object]` again.
 */
export function reasonFields(reason: Reason): ReasonField[] {
  if (typeof reason === "string") return [];
  const key = Object.keys(reason)[0];
  if (key === undefined) return [];
  const body = reason[key];
  if (body === null || typeof body !== "object") {
    return body === undefined ? [] : [{ label: key, value: String(body) }];
  }
  return Object.entries(body as Record<string, unknown>).map(([k, v]) => ({
    label: k,
    value:
      v !== null && typeof v === "object"
        ? JSON.stringify(v)
        : BINDING_FIELDS.has(k)
          ? bindingPhrase(String(v))
          : String(v),
  }));
}

/**
 * A reason as one line of text.
 *
 * Used where a list is wanted rather than a detail table. Never produces
 * `[object Object]`.
 */
export function reasonText(reason: Reason): string {
  const name = reasonName(reason);
  const fields = reasonFields(reason);
  if (fields.length === 0) return name;
  return `${name} (${fields.map((f) => `${f.label} ${f.value}`).join(", ")})`;
}
