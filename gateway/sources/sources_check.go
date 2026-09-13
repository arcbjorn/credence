package sources

import "github.com/arcbjorn/credence/gateway/evidence"

// Compile-time proof that the composite satisfies the collector's
// interface, so a signature drift fails the build rather than at wiring.
var _ evidence.HistoryReader = (*History)(nil)
