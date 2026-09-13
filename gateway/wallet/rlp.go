// Package wallet signs and submits transactions.
//
// This is the only component that spends money, so it is the one place
// where an optimistic assumption costs something irreversible. Two rules
// follow:
//
//  1. **Nonces come from the chain, not from memory.** A cached nonce that
//     drifts produces either a stuck queue or a replaced transaction. The
//     wallet reads the pending nonce before each send and lets the node be
//     authoritative.
//
//  2. **A send is never assumed to have failed.** An ambiguous result is
//     reported as such, so the publisher's reconciliation decides — it is
//     the component that knows whether the effect already landed.
package wallet

import "math/big"

// rlpEncode implements the subset of RLP that transaction encoding needs.
// Hand-rolled because the alternative is a full Ethereum client dependency
// for one encoding, and this surface is small enough to verify by eye.

func rlpString(b []byte) []byte {
	if len(b) == 1 && b[0] < 0x80 {
		return b
	}
	return append(rlpLength(len(b), 0x80), b...)
}

func rlpList(items ...[]byte) []byte {
	var body []byte
	for _, item := range items {
		body = append(body, item...)
	}
	return append(rlpLength(len(body), 0xc0), body...)
}

func rlpLength(length int, offset byte) []byte {
	if length < 56 {
		return []byte{offset + byte(length)}
	}
	encoded := bigEndian(uint64(length))
	return append([]byte{offset + 55 + byte(len(encoded))}, encoded...)
}

// rlpUint encodes an integer with no leading zeros, as RLP requires.
// A zero value encodes as an empty string, not as 0x00.
func rlpUint(v *big.Int) []byte {
	if v == nil || v.Sign() == 0 {
		return rlpString(nil)
	}
	return rlpString(v.Bytes())
}

func bigEndian(v uint64) []byte {
	var out []byte
	for v > 0 {
		out = append([]byte{byte(v & 0xff)}, out...)
		v >>= 8
	}
	if len(out) == 0 {
		out = []byte{0}
	}
	return out
}
