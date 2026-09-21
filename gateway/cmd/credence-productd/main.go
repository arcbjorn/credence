// credence-productd serves the borrower-facing product API.
//
// Separate from `credence-assessd` on purpose. That service answers the
// CRE route's "reassess loan N" and deliberately does not publish:
// `docs/05` pins ONE authority per epoch, and a service that both
// answered reassessment and sent transactions would be a second one.
// This one publishes and never assesses.
//
// It holds a key, so it refuses to start unauthenticated on anything but
// loopback -- an open borrow endpoint on a public interface is a button
// anyone can press.
package main

import (
	"encoding/hex"
	"flag"
	"fmt"
	"log"
	"math/big"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/decred/dcrd/dcrec/secp256k1/v4"

	"github.com/arcbjorn/credence/gateway/chain"
	"github.com/arcbjorn/credence/gateway/productapi"
	"github.com/arcbjorn/credence/gateway/wallet"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:18120", "listen address")
	rpcURL := flag.String("rpc", "http://127.0.0.1:8546", "chain rpc")
	manager := flag.String("manager", "", "loan manager address")
	account := flag.String("account", "", "credit account address")
	chainID := flag.Uint64("chain-id", 31337, "chain id")
	gasLimit := flag.Uint64("gas-limit", 5_000_000, "per-transaction gas cap")
	gasPrice := flag.Uint64("gas-price-cap", 500_000_000_000, "gas price cap, wei")
	timeout := flag.Duration("timeout", 30*time.Second, "per-request chain timeout")
	flag.Parse()

	if *manager == "" || *account == "" {
		fail("-manager and -account are required")
	}

	// The signing key never reaches a flag: a flag lands in shell history
	// and in `ps`.
	raw := strings.TrimPrefix(os.Getenv("CREDENCE_AGENT_KEY"), "0x")
	keyBytes, err := hex.DecodeString(raw)
	if err != nil || len(keyBytes) != 32 {
		fail("CREDENCE_AGENT_KEY must be 32 hex bytes")
	}
	key := secp256k1.PrivKeyFromBytes(keyBytes)
	token := os.Getenv("CREDENCE_API_TOKEN")

	// An open endpoint that can spend is only acceptable where nothing
	// else can reach it. Loopback is checked rather than assumed,
	// matching what `credence-assessd` does for the same reason.
	if token == "" && !isLoopback(*addr) {
		fail("CREDENCE_API_TOKEN is required when not listening on loopback; "+
			"%s would expose a borrow endpoint to the network", *addr)
	}

	rpc := chain.NewRPC(*rpcURL, 30*time.Second)
	w := wallet.New(key, rpc, wallet.Config{
		ChainID:     new(big.Int).SetUint64(*chainID),
		GasLimitCap: *gasLimit,
		GasPriceCap: new(big.Int).SetUint64(*gasPrice),
	})

	server := productapi.New(productapi.Config{
		Manager:   *manager,
		Account:   *account,
		AuthToken: token,
		Timeout:   *timeout,
	}, rpc, w)

	log.Printf("product api on %s", *addr)
	log.Printf("  manager %s", *manager)
	log.Printf("  account %s", *account)
	if token == "" {
		log.Printf("  UNAUTHENTICATED, loopback only")
	}

	srv := &http.Server{
		Addr:              *addr,
		Handler:           server.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	if err := srv.ListenAndServe(); err != nil {
		fail("listen: %v", err)
	}
}

// isLoopback reports whether the listener is unreachable from elsewhere.
func isLoopback(addr string) bool {
	host := addr
	if i := strings.LastIndex(addr, ":"); i >= 0 {
		host = addr[:i]
	}
	host = strings.Trim(host, "[]")
	return host == "127.0.0.1" || host == "localhost" || host == "::1"
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "FAIL  "+format+"\n", args...)
	os.Exit(1)
}
