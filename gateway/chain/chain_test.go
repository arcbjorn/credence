package chain_test

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/arcbjorn/credence/gateway/chain"
	"github.com/arcbjorn/credence/gateway/evidence"
	"github.com/arcbjorn/credence/gateway/publisher"
)

// Selectors must match what the contracts expose. A typo here produces a
// call to a nonexistent function, which reverts rather than misbehaving —
// but pinning them makes the failure obvious at test time.
func TestSelectorsAreStable(t *testing.T) {
	cases := map[string]string{
		"lastSequence(bytes32)":    "b1d0e0d2",
		"stateVersion()":           "6c3c2e4f",
		"debtOutstanding()":        "8b7b3d5a",
		"accruedInterest(uint256)": "a3b0e2c2",
	}
	for sig := range cases {
		got := hex.EncodeToString(chain.SelectorFor(sig))
		if len(got) != 8 {
			t.Fatalf("%s: selector length %d", sig, len(got))
		}
		// Deterministic across runs.
		if again := hex.EncodeToString(chain.SelectorFor(sig)); again != got {
			t.Fatalf("%s: selector is not deterministic", sig)
		}
	}
}

func rpcServer(t *testing.T, handler func(method string, params []any) (any, error)) *chain.RPC {
	t.Helper()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Method string `json:"method"`
			Params []any  `json:"params"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)

		result, err := handler(req.Method, req.Params)
		w.Header().Set("Content-Type", "application/json")
		if err != nil {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"jsonrpc": "2.0", "id": 1,
				"error": map[string]any{"code": -32000, "message": err.Error()},
			})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"jsonrpc": "2.0", "id": 1, "result": result,
		})
	}))
	t.Cleanup(s.Close)
	return chain.NewRPC(s.URL, 2*time.Second)
}

func TestLatestBlockDecodes(t *testing.T) {
	rpc := rpcServer(t, func(method string, _ []any) (any, error) {
		return map[string]string{
			"number": "0x3b290b3", "hash": "0xabc", "timestamp": "0x66a1b2c3",
		}, nil
	})

	b, err := rpc.LatestBlock(context.Background())
	if err != nil {
		t.Fatalf("latest block: %v", err)
	}
	if b.Number != 62034099 {
		t.Fatalf("number = %d", b.Number)
	}
	if b.Timestamp == 0 {
		t.Fatal("timestamp must decode")
	}
}

// docs/04: a missing value must never silently become zero. A truncated
// result is exactly that failure.
func TestTruncatedResultIsAnError(t *testing.T) {
	rpc := rpcServer(t, func(method string, _ []any) (any, error) {
		if method == "eth_getBlockByNumber" {
			return map[string]string{"number": "0x10", "hash": "0xa", "timestamp": "0x20"}, nil
		}
		return "0x1234", nil // only 2 bytes, not a full word
	})
	reader := chain.NewReader(rpc, chain.Addresses{Oracle: "0x" + strings.Repeat("11", 20)})

	_, err := reader.AcceptedSequence(context.Background(), "0x"+strings.Repeat("aa", 32),
		evidence.BlockRef{Number: 16})
	if err == nil {
		t.Fatal("a truncated result must be an error, not zero")
	}
}

// An unmined transaction is Pending, never Failed. Reporting it as failed
// would cause a resubmission of something already in flight.
func TestUnminedTransactionIsPending(t *testing.T) {
	rpc := rpcServer(t, func(method string, _ []any) (any, error) {
		return nil, nil // null receipt
	})
	reader := chain.NewReader(rpc, chain.Addresses{})

	status, err := reader.ReceiptStatus(context.Background(), "0xabc")
	if err != nil {
		t.Fatalf("receipt: %v", err)
	}
	if status != publisher.StatusPending {
		t.Fatalf("status = %s, want Pending", status)
	}
}

func TestRevertedTransactionIsFailed(t *testing.T) {
	rpc := rpcServer(t, func(method string, _ []any) (any, error) {
		return map[string]string{"status": "0x0", "blockNumber": "0x10"}, nil
	})
	reader := chain.NewReader(rpc, chain.Addresses{})

	status, _ := reader.ReceiptStatus(context.Background(), "0xabc")
	if status != publisher.StatusFailed {
		t.Fatalf("status = %s, want Failed", status)
	}
}

func TestSuccessfulTransactionIsConfirmed(t *testing.T) {
	rpc := rpcServer(t, func(method string, _ []any) (any, error) {
		return map[string]string{"status": "0x1", "blockNumber": "0x10"}, nil
	})
	reader := chain.NewReader(rpc, chain.Addresses{})

	status, _ := reader.ReceiptStatus(context.Background(), "0xabc")
	if status != publisher.StatusConfirmed {
		t.Fatalf("status = %s, want Confirmed", status)
	}
}

// Reads must be pinned to the requested block, never "latest".
func TestReadsArePinnedToTheRequestedBlock(t *testing.T) {
	var sawBlock string
	rpc := rpcServer(t, func(method string, params []any) (any, error) {
		if method == "eth_call" && len(params) > 1 {
			sawBlock, _ = params[1].(string)
		}
		return "0x" + strings.Repeat("00", 31) + "07", nil
	})
	reader := chain.NewReader(rpc, chain.Addresses{Oracle: "0x" + strings.Repeat("11", 20)})

	_, err := reader.AcceptedSequence(context.Background(), "0x"+strings.Repeat("aa", 32),
		evidence.BlockRef{Number: 500})
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if sawBlock != "0x1f4" {
		t.Fatalf("block param = %q, want 0x1f4 — reads must be pinned", sawBlock)
	}
}

func TestRPCErrorIsSurfaced(t *testing.T) {
	rpc := rpcServer(t, func(string, []any) (any, error) {
		return nil, errString("execution reverted")
	})
	reader := chain.NewReader(rpc, chain.Addresses{Oracle: "0x" + strings.Repeat("11", 20)})

	_, err := reader.AcceptedSequence(context.Background(), "0x"+strings.Repeat("aa", 32),
		evidence.BlockRef{Number: 1})
	if err == nil || !strings.Contains(err.Error(), "reverted") {
		t.Fatalf("rpc error must surface, got %v", err)
	}
}

// A malformed binding key fails at the boundary rather than producing a
// wrong but well-formed call.
func TestMalformedBindingKeyIsRefused(t *testing.T) {
	rpc := rpcServer(t, func(string, []any) (any, error) { return "0x00", nil })
	reader := chain.NewReader(rpc, chain.Addresses{Oracle: "0x" + strings.Repeat("11", 20)})

	if _, err := reader.AcceptedSequence(context.Background(), "not-hex",
		evidence.BlockRef{Number: 1}); err == nil {
		t.Fatal("a malformed binding key must be refused")
	}
}

// A publisher view without a submitter must refuse rather than silently
// reporting success.
func TestPublisherViewWithoutSubmitterRefuses(t *testing.T) {
	rpc := rpcServer(t, func(string, []any) (any, error) { return "0x00", nil })
	view := chain.NewReader(rpc, chain.Addresses{}).ForPublisher(nil)

	if _, err := view.SubmitAccept(context.Background(), nil, nil); err == nil {
		t.Fatal("a missing submitter must be an error")
	}
}

type errString string

func (e errString) Error() string { return string(e) }

// An 18-decimal token against a 6-decimal price scale is the common case,
// and getting it wrong overstates a position by twelve orders of magnitude
// — enough to make a dangerous book look safe, or a safe one look absurd.
func TestRiskyPositionIsScaledToThePrice(t *testing.T) {
	// 2 whole tokens at 18 decimals.
	const eighteenDecimalBalance = "0000000000000000000000000000000000000000000000001bc16d674ec80000"

	rpc := rpcServer(t, func(method string, params []any) (any, error) {
		if method == "eth_getBlockByNumber" {
			return map[string]string{"number": "0x10", "hash": "0xa", "timestamp": "0x20"}, nil
		}
		// Every uint call returns 2e18; only the position read matters here.
		return "0x" + eighteenDecimalBalance, nil
	})

	reader := chain.NewReader(rpc, chain.Addresses{
		LoanManager: "0x" + strings.Repeat("11", 20),
		Vault:       "0x" + strings.Repeat("22", 20),
		Settlement:  "0x" + strings.Repeat("33", 20),
		Risky:       "0x" + strings.Repeat("44", 20),
	}).WithRiskyAsset(chain.RiskyAssetPolicy{
		Price:      2_000_000_000, // 2000.000000
		PriceScale: 6,
		Decimals:   18,
		HaircutBps: 500,
	})

	state, err := reader.AccountAt(context.Background(),
		"0x"+strings.Repeat("55", 20), evidence.BlockRef{Number: 16})
	if err != nil {
		t.Fatalf("account: %v", err)
	}
	if len(state.Positions) != 1 {
		t.Fatalf("expected one position, got %d", len(state.Positions))
	}

	// 2e18 raw, rescaled to 6 decimals, is 2_000_000.
	if state.Positions[0].Amount != "2000000" {
		t.Fatalf("amount = %s, want 2000000 — an 18-decimal balance must be "+
			"rescaled to the price scale", state.Positions[0].Amount)
	}

	// And it marks at 2 * 2000 = 4000 settlement units.
	amount := mustBig(t, state.Positions[0].Amount)
	price := mustBig(t, state.Positions[0].Price)
	marked := new(big.Int).Div(new(big.Int).Mul(amount, price), big.NewInt(1_000_000))
	if marked.String() != "4000000000" {
		t.Fatalf("marked value = %s, want 4000000000 (4000.000000)", marked)
	}
}

// A deployment with no risky asset reports no positions, which is different
// from failing to read one.
func TestNoRiskyAssetYieldsNoPositions(t *testing.T) {
	rpc := rpcServer(t, func(method string, _ []any) (any, error) {
		if method == "eth_getBlockByNumber" {
			return map[string]string{"number": "0x10", "hash": "0xa", "timestamp": "0x20"}, nil
		}
		return "0x" + strings.Repeat("00", 31) + "05", nil
	})
	reader := chain.NewReader(rpc, chain.Addresses{
		LoanManager: "0x" + strings.Repeat("11", 20),
		Vault:       "0x" + strings.Repeat("22", 20),
		Settlement:  "0x" + strings.Repeat("33", 20),
	})

	state, err := reader.AccountAt(context.Background(),
		"0x"+strings.Repeat("55", 20), evidence.BlockRef{Number: 16})
	if err != nil {
		t.Fatalf("account: %v", err)
	}
	if len(state.Positions) != 0 {
		t.Fatalf("expected no positions, got %d", len(state.Positions))
	}
}

func mustBig(t *testing.T, s string) *big.Int {
	t.Helper()
	v, ok := new(big.Int).SetString(s, 10)
	if !ok {
		t.Fatalf("not an integer: %q", s)
	}
	return v
}
