/**
 * Naming the constraint that bound a number, in words.
 *
 * `BindingConstraint` is a Rust enum serialized by serde, so it arrives as
 * the identifier itself: `StressCapacity`, `HistoryGrowthCap`. Every screen
 * rendered that identifier raw, which reads as a variable name leaking
 * through the interface -- a visitor sees `AccountHardCap` and learns
 * nothing except that somebody wrote it in PascalCase.
 *
 * The phrase and the identifier are BOTH kept. `docs/09` requires the
 * binding constraint beside the capacity because a capacity without its
 * reason hides why it is what it is, and an auditor checking a screen
 * against a payload needs the exact variant to match on. So the phrase
 * carries the meaning and the identifier stays available as the precise
 * term -- rendered as a title attribute, or in a detail row beside it.
 *
 * Phrasing rule: each one completes the sentence "the limit is set by
 * ___". They are noun phrases, not sentences, because they appear in
 * table cells and beside figures.
 */

/** Every variant of `risk_core::capacity::BindingConstraint`. */
export const BINDINGS = [
  "StressCapacity",
  "HistoryGrowthCap",
  "AccountHardCap",
  "ForecastCap",
  "DebtHeadroom",
  "StrategyBudget",
  "VaultBudget",
  "AvailableCash",
] as const;

export type Binding = (typeof BINDINGS)[number];

/**
 * The plain-language phrase for a binding constraint.
 *
 * Borrower-side constraints describe the BORROWER's own position; lender
 * side ones describe the facility it is borrowing from. A reader should
 * be able to tell which side bound them without knowing the model.
 */
const PHRASES: Record<Binding, string> = {
  // Borrower side.
  StressCapacity: "what it survives a crash",
  HistoryGrowthCap: "how much it has proven",
  AccountHardCap: "its ceiling for this account",
  ForecastCap: "what it is forecast to repay",
  // Lender side.
  DebtHeadroom: "how much it already owes",
  StrategyBudget: "the budget for this strategy",
  VaultBudget: "the budget for this vault",
  AvailableCash: "the cash the lender has free",
};

/**
 * Render a binding constraint as words.
 *
 * An unrecognised value is returned unchanged rather than replaced with a
 * placeholder: a new engine variant should show up as its own identifier,
 * visibly untranslated, instead of silently becoming "unknown" and hiding
 * that the interface is behind the engine.
 */
export function bindingPhrase(binding: string | undefined | null): string {
  if (binding === undefined || binding === null || binding === "") return "—";
  if (binding in PHRASES) return PHRASES[binding as Binding];
  return binding;
}

/** True when the value is a variant this module knows how to phrase. */
export function isKnownBinding(binding: string): binding is Binding {
  return binding in PHRASES;
}
