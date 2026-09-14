/**
 * A hash, shown in full by default.
 *
 * Truncation is opt-in rather than automatic: `docs/09` puts hashes in
 * accessible details, but a hash a reader cannot copy in full is not
 * evidence they can check.
 */
export function Hash({ value, label }: { value: string; label?: string }) {
  return (
    <div>
      {label && <div className="hash" style={{ marginBottom: 2 }}>{label}</div>}
      <code className="hash">{value}</code>
    </div>
  );
}
