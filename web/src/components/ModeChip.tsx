import type { Mode } from "../lib/modes";

/**
 * A mode label, always rendered as text.
 *
 * `docs/09`: these must never collapse into an ambiguous green indicator.
 * The label is the content -- there is no icon-only variant and no prop
 * that hides it, because the moment one exists a layout gets tight and
 * someone uses it.
 */
export function ModeChip({ mode }: { mode: Mode }) {
  return (
    <span className={`mode mode-${mode.token}`} title={mode.meaning}>
      {mode.label}
    </span>
  );
}
