package wallet_test

import (
	"context"
	"encoding/hex"
	"errors"
	"math/big"
	"strings"
	"testing"

	"github.com/arcbjorn/credence/gateway/wallet"
	"github.com/decred/dcrd/dcrec/secp256k1/v4"
)

type fakeNode struct {
	nonce       uint64
	gasPrice    *big.Int
	gasEstimate uint64
	sent        [][]byte
	sendErr     error
	nonceErr    error
}

func (n *fakeNode) PendingNonce(context.Context, string) (uint64, error) {
	if n.nonceErr != nil {
		return 0, n.nonceErr
	}
	return n.nonce, nil
}

func (n *fakeNode) GasPrice(context.Context) (*big.Int, error) { return n.gasPrice, nil }

func (n *fakeNode) EstimateGas(context.Context, string, string, []byte) (uint64, error) {
	return n.gasEstimate, nil
}

func (n *fakeNode) SendRawTransaction(_ context.Context, signed []byte) (string, error) {
	n.sent = append(n.sent, signed)
	if n.sendErr != nil {
		return "", n.sendErr
	}
	return "0xtx", nil
}

func testKey(t *testing.T) *secp256k1.PrivateKey {
	t.Helper()
	b, _ := hex.DecodeString(
		"ac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80")
	return secp256k1.PrivKeyFromBytes(b)
}

func newWallet(t *testing.T, node *fakeNode) *wallet.Wallet {
	t.Helper()
	return wallet.New(testKey(t), node, wallet.Config{
		ChainID:     big.NewInt(31337),
		GasLimitCap: 5_000_000,
		GasPriceCap: big.NewInt(500_000_000_000),
	})
}

// The address must derive from the key, matching the funded account anvil
// reports for this well-known test key.
func TestAddressDerivation(t *testing.T) {
	w := newWallet(t, &fakeNode{})
	const want = "0xf39fd6e51aad88f6f4ce6ab8827279cfffb92266"
	if w.Address() != want {
		t.Fatalf("address = %s, want %s", w.Address(), want)
	}
}

func TestSendProducesSignedTransaction(t *testing.T) {
	node := &fakeNode{nonce: 3, gasPrice: big.NewInt(1_000_000_000), gasEstimate: 100_000}
	w := newWallet(t, node)

	tx, err := w.Send(context.Background(), "0x1111111111111111111111111111111111111111",
		[]byte{0xde, 0xad})
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if tx != "0xtx" || len(node.sent) != 1 {
		t.Fatalf("expected one submission, got %d", len(node.sent))
	}
	// An RLP list with a signature is not a trivial payload.
	if len(node.sent[0]) < 70 {
		t.Fatalf("signed transaction is implausibly short: %d bytes", len(node.sent[0]))
	}
}

// A gas spike must not turn a routine monitoring update into an expensive
// one. Permission fails closed anyway, so refusing is the cheaper error.
func TestGasPriceCapIsEnforced(t *testing.T) {
	node := &fakeNode{
		nonce: 0, gasPrice: big.NewInt(900_000_000_000), gasEstimate: 100_000,
	}
	w := newWallet(t, node)

	_, err := w.Send(context.Background(), "0x1111111111111111111111111111111111111111", nil)
	if err == nil || !strings.Contains(err.Error(), "exceeds cap") {
		t.Fatalf("expected a gas price refusal, got %v", err)
	}
	if len(node.sent) != 0 {
		t.Fatal("nothing must be submitted when the price exceeds the cap")
	}
}

// An estimator returning an absurd value must not drain the operator.
func TestGasLimitCapIsEnforced(t *testing.T) {
	node := &fakeNode{nonce: 0, gasPrice: big.NewInt(1), gasEstimate: 10_000_000}
	w := newWallet(t, node)

	_, err := w.Send(context.Background(), "0x1111111111111111111111111111111111111111", nil)
	if err == nil || !strings.Contains(err.Error(), "gas limit") {
		t.Fatalf("expected a gas limit refusal, got %v", err)
	}
	if len(node.sent) != 0 {
		t.Fatal("nothing must be submitted above the gas cap")
	}
}

// The nonce comes from the chain each time: a cached one drifts the moment
// anything else sends from this key.
func TestNonceIsReadPerSend(t *testing.T) {
	node := &fakeNode{nonce: 7, gasPrice: big.NewInt(1), gasEstimate: 21_000}
	w := newWallet(t, node)

	_, _ = w.Send(context.Background(), "0x1111111111111111111111111111111111111111", nil)
	node.nonce = 8
	_, _ = w.Send(context.Background(), "0x1111111111111111111111111111111111111111", nil)

	if len(node.sent) != 2 {
		t.Fatalf("expected two submissions, got %d", len(node.sent))
	}
	// Different nonces produce different signed payloads.
	if string(node.sent[0]) == string(node.sent[1]) {
		t.Fatal("a changed nonce must change the signed transaction")
	}
}

func TestUnreadableNonceAbortsBeforeSending(t *testing.T) {
	node := &fakeNode{nonceErr: errors.New("rpc down"), gasPrice: big.NewInt(1)}
	w := newWallet(t, node)

	if _, err := w.Send(context.Background(),
		"0x1111111111111111111111111111111111111111", nil); err == nil {
		t.Fatal("expected an error")
	}
	if len(node.sent) != 0 {
		t.Fatal("must not sign without a nonce")
	}
}

func TestInvalidRecipientIsRefused(t *testing.T) {
	node := &fakeNode{nonce: 0, gasPrice: big.NewInt(1), gasEstimate: 21_000}
	w := newWallet(t, node)

	if _, err := w.Send(context.Background(), "0xabc", nil); err == nil {
		t.Fatal("a short recipient must be refused")
	}
}

// Signing is deterministic, so a retry of the same transaction produces
// identical bytes and the node recognises it as a duplicate.
func TestSigningIsDeterministic(t *testing.T) {
	first := &fakeNode{nonce: 5, gasPrice: big.NewInt(1_000_000_000), gasEstimate: 50_000}
	second := &fakeNode{nonce: 5, gasPrice: big.NewInt(1_000_000_000), gasEstimate: 50_000}

	_, _ = newWallet(t, first).Send(context.Background(),
		"0x1111111111111111111111111111111111111111", []byte{0x01})
	_, _ = newWallet(t, second).Send(context.Background(),
		"0x1111111111111111111111111111111111111111", []byte{0x01})

	if string(first.sent[0]) != string(second.sent[0]) {
		t.Fatal("identical inputs must produce identical signed bytes")
	}
}
