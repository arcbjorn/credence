// Package chain reads and writes contract state over JSON-RPC.
//
// ABI encoding is done by hand rather than through a binding generator.
// The call surface is four view functions and one transaction, and the
// hand-rolled encoder keeps the dependency footprint small enough to audit.
// Every selector below is derived from its signature at init, so a typo
// produces a wrong selector consistently rather than silently matching a
// different function.
package chain

import (
	"encoding/hex"
	"fmt"
	"math/big"
	"strings"

	"golang.org/x/crypto/sha3"
)

// selector returns the first four bytes of keccak256(signature).
func selector(signature string) []byte {
	h := sha3.NewLegacyKeccak256()
	h.Write([]byte(signature))
	return h.Sum(nil)[:4]
}

// pad32 left-pads bytes into a 32-byte ABI word.
func pad32(b []byte) []byte {
	if len(b) > 32 {
		b = b[len(b)-32:]
	}
	out := make([]byte, 32)
	copy(out[32-len(b):], b)
	return out
}

func encodeUint(v *big.Int) []byte {
	if v == nil {
		return make([]byte, 32)
	}
	return pad32(v.Bytes())
}

func encodeAddress(addr string) ([]byte, error) {
	b, err := hex.DecodeString(strings.TrimPrefix(addr, "0x"))
	if err != nil {
		return nil, fmt.Errorf("address %q: %w", addr, err)
	}
	if len(b) != 20 {
		return nil, fmt.Errorf("address %q: expected 20 bytes, got %d", addr, len(b))
	}
	return pad32(b), nil
}

func encodeBytes32(v string) ([]byte, error) {
	b, err := hex.DecodeString(strings.TrimPrefix(v, "0x"))
	if err != nil {
		return nil, fmt.Errorf("bytes32 %q: %w", v, err)
	}
	if len(b) != 32 {
		return nil, fmt.Errorf("bytes32 %q: expected 32 bytes, got %d", v, len(b))
	}
	return b, nil
}

// decodeUint reads one ABI word as an unsigned integer.
//
// Returns an error on a short result rather than a zero: `docs/04` requires
// that a missing value never silently becomes zero, and a truncated RPC
// response is exactly that failure.
func decodeUint(data []byte, wordIndex int) (*big.Int, error) {
	start := wordIndex * 32
	if len(data) < start+32 {
		return nil, fmt.Errorf("result too short: need %d bytes, got %d", start+32, len(data))
	}
	return new(big.Int).SetBytes(data[start : start+32]), nil
}

func decodeBool(data []byte, wordIndex int) (bool, error) {
	v, err := decodeUint(data, wordIndex)
	if err != nil {
		return false, err
	}
	return v.Sign() != 0, nil
}

func decodeHex(data []byte, wordIndex int) (string, error) {
	start := wordIndex * 32
	if len(data) < start+32 {
		return "", fmt.Errorf("result too short: need %d bytes, got %d", start+32, len(data))
	}
	return "0x" + hex.EncodeToString(data[start:start+32]), nil
}

// decodeAddress reads the low 20 bytes of a word.
func decodeAddress(data []byte, wordIndex int) (string, error) {
	start := wordIndex * 32
	if len(data) < start+32 {
		return "", fmt.Errorf("result too short: need %d bytes, got %d", start+32, len(data))
	}
	return "0x" + hex.EncodeToString(data[start+12:start+32]), nil
}

// SelectorFor exposes selector derivation for diagnostics and tests.
func SelectorFor(signature string) []byte { return selector(signature) }
