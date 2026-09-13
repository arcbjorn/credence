package wallet

import (
	"bytes"
	"math/big"
	"testing"
)

// r and s are RLP integers, so a leading zero byte must be stripped.
// Encoding the raw 32 bytes produces a non-canonical integer that nodes
// reject outright -- "failed to decode transaction" -- and because a
// leading zero occurs in roughly 1 of 256 signatures, the failure is
// intermittent rather than reproducible.
func TestSignatureComponentsDropLeadingZeros(t *testing.T) {
	var r [32]byte
	r[0] = 0x00
	r[1] = 0x00
	r[2] = 0xff
	for i := 3; i < 32; i++ {
		r[i] = byte(i)
	}

	got := rlpUint(new(big.Int).SetBytes(r[:]))
	want := rlpString(new(big.Int).SetBytes(r[:]).Bytes())
	if !bytes.Equal(got, want) {
		t.Fatalf("got %x, want %x", got, want)
	}

	// The encoded value must be 30 bytes of payload, not 32.
	if got[0] != 0x80+30 {
		t.Fatalf("expected a 30-byte payload prefix, got %#x", got[0])
	}

	// rlpString on the raw bytes is what the bug looked like: it keeps the
	// zeros and declares 32 bytes.
	buggy := rlpString(r[:])
	if buggy[0] != 0x80+32 {
		t.Fatalf("expected the raw form to declare 32 bytes, got %#x", buggy[0])
	}
	if bytes.Equal(buggy, got) {
		t.Fatal("raw and canonical encodings must differ for a leading zero")
	}
}
