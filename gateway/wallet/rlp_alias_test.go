package wallet

import (
	"bytes"
	"math/big"
	"testing"
)

// rlpString must not write into the slice it was given, nor into a buffer
// another item is still using. An aliasing bug here corrupts a transaction
// only when the allocator happens to reuse a backing array, which is why
// it presents as an intermittent "failed to decode transaction" rather
// than a reproducible failure.
func TestRLPDoesNotAliasItsInput(t *testing.T) {
	data := []byte{0xde, 0xad, 0xbe, 0xef}
	original := append([]byte(nil), data...)

	_ = rlpList(
		rlpUint(big.NewInt(1)),
		rlpString(data),
		rlpUint(big.NewInt(31337)),
	)

	if !bytes.Equal(data, original) {
		t.Fatalf("rlp mutated its input: got %x, want %x", data, original)
	}
}

// Encoding the same transaction twice must produce identical bytes. If a
// shared buffer leaks between calls, the second encoding differs.
func TestRLPEncodingIsStable(t *testing.T) {
	build := func() []byte {
		to := []byte{
			0x5f, 0xbd, 0xb2, 0x31, 0x56, 0x78, 0xaf, 0xec, 0xb3, 0x67,
			0xf0, 0x32, 0xd9, 0x3f, 0x64, 0x2f, 0x64, 0x18, 0x0a, 0xa3,
		}
		payload := bytes.Repeat([]byte{0xab}, 200)
		return rlpList(
			rlpUint(big.NewInt(7)),
			rlpUint(big.NewInt(1_000_000_000)),
			rlpUint(big.NewInt(5_000_000)),
			rlpString(to),
			rlpUint(big.NewInt(0)),
			rlpString(payload),
			rlpUint(big.NewInt(31337)),
			rlpString(nil),
			rlpString(nil),
		)
	}

	first := build()
	for i := 0; i < 200; i++ {
		if got := build(); !bytes.Equal(got, first) {
			t.Fatalf("encoding %d differs:\n got %x\nwant %x", i, got, first)
		}
	}
}

// A single byte below 0x80 encodes as itself; at or above it takes a
// length prefix. The boundary is where a shared-buffer bug shows up first.
func TestRLPSingleByteBoundary(t *testing.T) {
	if got := rlpString([]byte{0x7f}); !bytes.Equal(got, []byte{0x7f}) {
		t.Fatalf("0x7f must encode as itself, got %x", got)
	}
	if got := rlpString([]byte{0x80}); !bytes.Equal(got, []byte{0x81, 0x80}) {
		t.Fatalf("0x80 must take a prefix, got %x", got)
	}
}
