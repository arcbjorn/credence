package wallet

import (
	"context"
	"encoding/hex"
	"fmt"
	"math/big"
	"strings"

	"github.com/decred/dcrd/dcrec/secp256k1/v4"
	"github.com/decred/dcrd/dcrec/secp256k1/v4/ecdsa"
	"golang.org/x/crypto/sha3"
)

// Node is the chain access a wallet needs. Narrow on purpose: a wallet that
// could read arbitrary state would invite decisions that belong elsewhere.
type Node interface {
	PendingNonce(ctx context.Context, address string) (uint64, error)
	GasPrice(ctx context.Context) (*big.Int, error)
	EstimateGas(ctx context.Context, from, to string, data []byte) (uint64, error)
	SendRawTransaction(ctx context.Context, signed []byte) (string, error)
}

// Wallet signs and submits transactions from one key.
type Wallet struct {
	key     *secp256k1.PrivateKey
	address string
	chainID *big.Int
	node    Node

	// gasLimitCap bounds what a single transaction may consume. An
	// estimator that returns an absurd value must not be able to drain the
	// operator's balance in one send.
	gasLimitCap uint64
	// gasPriceCap bounds the price paid. Without it, a gas spike turns a
	// routine monitoring update into an expensive one.
	gasPriceCap *big.Int
}

type Config struct {
	ChainID     *big.Int
	GasLimitCap uint64
	GasPriceCap *big.Int
}

func New(key *secp256k1.PrivateKey, node Node, cfg Config) *Wallet {
	return &Wallet{
		key:         key,
		address:     addressOf(key),
		chainID:     cfg.ChainID,
		node:        node,
		gasLimitCap: cfg.GasLimitCap,
		gasPriceCap: cfg.GasPriceCap,
	}
}

func (w *Wallet) Address() string { return w.address }

// Send signs and submits one call.
//
// Returns the transaction hash on successful submission. A failure to
// submit is returned as-is: the caller must not treat it as proof the
// transaction did not land, because a dropped response looks identical to
// a rejected send.
func (w *Wallet) Send(ctx context.Context, to string, data []byte) (string, error) {
	// Read the nonce fresh. A cached one drifts the moment anything else
	// sends from this key, and the failure is a stuck queue rather than an
	// obvious error.
	nonce, err := w.node.PendingNonce(ctx, w.address)
	if err != nil {
		return "", fmt.Errorf("wallet: read nonce: %w", err)
	}

	gasPrice, err := w.node.GasPrice(ctx)
	if err != nil {
		return "", fmt.Errorf("wallet: read gas price: %w", err)
	}
	if w.gasPriceCap != nil && gasPrice.Cmp(w.gasPriceCap) > 0 {
		// Refusing is correct: a monitoring update is not urgent enough to
		// pay an arbitrary price, and permission fails closed anyway.
		return "", fmt.Errorf("wallet: gas price %s exceeds cap %s",
			gasPrice, w.gasPriceCap)
	}

	gasLimit, err := w.node.EstimateGas(ctx, w.address, to, data)
	if err != nil {
		return "", fmt.Errorf("wallet: estimate gas: %w", err)
	}
	// Estimates can be tight; a modest buffer avoids an out-of-gas revert
	// on a call whose cost varies with storage state.
	gasLimit = gasLimit + gasLimit/5
	if w.gasLimitCap > 0 && gasLimit > w.gasLimitCap {
		return "", fmt.Errorf("wallet: gas limit %d exceeds cap %d", gasLimit, w.gasLimitCap)
	}

	signed, err := w.sign(nonce, to, data, gasPrice, gasLimit)
	if err != nil {
		return "", err
	}

	txHash, err := w.node.SendRawTransaction(ctx, signed)
	if err != nil {
		return "", fmt.Errorf("wallet: submit: %w", err)
	}
	return txHash, nil
}

// sign produces an EIP-155 legacy transaction.
//
// Legacy rather than EIP-1559 because it is the format with the fewest
// moving parts, and a monitoring update has no need for priority fees.
func (w *Wallet) sign(
	nonce uint64,
	to string,
	data []byte,
	gasPrice *big.Int,
	gasLimit uint64,
) ([]byte, error) {
	toBytes, err := hex.DecodeString(strings.TrimPrefix(to, "0x"))
	if err != nil || len(toBytes) != 20 {
		return nil, fmt.Errorf("wallet: invalid recipient %q", to)
	}

	// The signing payload includes the chain id, so a signature cannot be
	// replayed onto another chain (EIP-155).
	unsigned := rlpList(
		rlpUint(new(big.Int).SetUint64(nonce)),
		rlpUint(gasPrice),
		rlpUint(new(big.Int).SetUint64(gasLimit)),
		rlpString(toBytes),
		rlpUint(big.NewInt(0)), // value: monitoring updates never transfer
		rlpString(data),
		rlpUint(w.chainID),
		rlpString(nil),
		rlpString(nil),
	)

	digest := keccak(unsigned)
	sig := ecdsa.SignCompact(w.key, digest, false)
	if len(sig) != 65 {
		return nil, fmt.Errorf("wallet: unexpected signature length %d", len(sig))
	}

	// dcrd returns v||r||s with v in {27,28}; EIP-155 wants
	// v = recovery + chainID*2 + 35.
	recovery := new(big.Int).SetUint64(uint64(sig[0] - 27))
	v := new(big.Int).Add(recovery, new(big.Int).Add(
		new(big.Int).Mul(w.chainID, big.NewInt(2)), big.NewInt(35)))

	return rlpList(
		rlpUint(new(big.Int).SetUint64(nonce)),
		rlpUint(gasPrice),
		rlpUint(new(big.Int).SetUint64(gasLimit)),
		rlpString(toBytes),
		rlpUint(big.NewInt(0)),
		rlpString(data),
		rlpUint(v),
		// r and s are RLP integers, not fixed-width byte strings: a
		// leading zero must be stripped or the node rejects the whole
		// transaction as undecodable. Roughly one signature in 256 has
		// one, which is why encoding them raw fails intermittently.
		rlpUint(new(big.Int).SetBytes(sig[1:33])),
		rlpUint(new(big.Int).SetBytes(sig[33:65])),
	), nil
}

func keccak(b []byte) []byte {
	h := sha3.NewLegacyKeccak256()
	h.Write(b)
	return h.Sum(nil)
}

func addressOf(key *secp256k1.PrivateKey) string {
	pub := key.PubKey().SerializeUncompressed()
	return "0x" + hex.EncodeToString(keccak(pub[1:])[12:])
}
