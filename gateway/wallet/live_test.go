package wallet_test

import (
	"context"
	"encoding/hex"
	"math/big"
	"net"
	"testing"
	"time"

	"github.com/arcbjorn/credence/gateway/chain"
	"github.com/arcbjorn/credence/gateway/wallet"
	"github.com/decred/dcrd/dcrec/secp256k1/v4"
)

// Signing is probabilistic: a leading zero in r or s appears in roughly 1
// of 256 signatures, so a handful of sends proves nothing. This sends
// enough that at least one such signature is near-certain, and a node that
// accepts them all is the only real evidence the encoding is canonical.
func TestLiveSendsSurviveLeadingZeroSignatures(t *testing.T) {
	const rpcURL = "http://127.0.0.1:8546"
	c, err := net.DialTimeout("tcp", "127.0.0.1:8546", 300*time.Millisecond)
	if err != nil {
		t.Skip("no local node on 8546")
	}
	_ = c.Close()

	keyBytes, _ := hex.DecodeString(
		"ac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80")
	key := secp256k1.PrivKeyFromBytes(keyBytes)

	rpc := chain.NewRPC(rpcURL, 10*time.Second)
	w := wallet.New(key, rpc, wallet.Config{
		ChainID:     big.NewInt(31337),
		GasLimitCap: 1_000_000,
		GasPriceCap: big.NewInt(500_000_000_000),
	})

	ctx := context.Background()
	// A send to a plain address with no data: cheap, always succeeds, and
	// exercises exactly the signing path that was failing.
	const to = "0x000000000000000000000000000000000000dEaD"
	for i := 0; i < 600; i++ {
		if _, err := w.Send(ctx, to, nil); err != nil {
			t.Fatalf("send %d failed: %v", i, err)
		}
	}
}
