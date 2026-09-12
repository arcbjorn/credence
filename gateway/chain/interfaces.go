package chain

import (
	"github.com/arcbjorn/credence/gateway/evidence"
	"github.com/arcbjorn/credence/gateway/publisher"
)

// Compile-time proof that Reader satisfies the collector's interface. A
// signature drift then fails the build rather than at wiring time.
var _ evidence.ChainReader = (*Reader)(nil)

// And that PublisherView satisfies the publisher's.
var _ publisher.Chain = (*PublisherView)(nil)
