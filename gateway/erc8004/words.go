package erc8004

import (
	"fmt"
	"math/big"

	"golang.org/x/crypto/sha3"
)

// Address is a 20-byte account.
type Address [20]byte

// maxInt128 is 2^127 - 1; minInt128 is -2^127.
var (
	maxInt128 = new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 127), big.NewInt(1))
	minInt128 = new(big.Int).Neg(new(big.Int).Lsh(big.NewInt(1), 127))
	two256    = new(big.Int).Lsh(big.NewInt(1), 256)
)

func checkInt128(v *big.Int) error {
	if v.Cmp(maxInt128) > 0 || v.Cmp(minInt128) < 0 {
		return fmt.Errorf("value %s outside int128 range", v)
	}
	return nil
}

// uintWord left-pads an unsigned value into one 32-byte word.
func uintWord(v *big.Int) []byte {
	var w [32]byte
	v.FillBytes(w[:])
	return w[:]
}

// int128Word encodes a signed value as a 32-byte two's-complement word.
//
// Solidity sign-extends a negative int128 across the full word, so -1 is
// thirty-two 0xff bytes rather than sixteen. Encoding only the low 16
// bytes would decode as a huge positive number: a loss would publish as
// a gain.
func int128Word(v *big.Int) []byte {
	if v.Sign() >= 0 {
		return uintWord(v)
	}
	return uintWord(new(big.Int).Add(two256, v))
}

func addressWord(a Address) []byte {
	var w [32]byte
	copy(w[12:], a[:])
	return w[:]
}

func boolWord(b bool) []byte {
	var w [32]byte
	if b {
		w[31] = 1
	}
	return w[:]
}

// encodeString is an ABI dynamic string: a length word, then the bytes
// right-padded to a word boundary.
func encodeString(s string) []byte {
	b := []byte(s)
	out := uintWord(big.NewInt(int64(len(b))))
	out = append(out, b...)
	if pad := len(b) % 32; pad != 0 {
		out = append(out, make([]byte, 32-pad)...)
	}
	return out
}

// Selector returns the four-byte selector for a signature.
func Selector(signature string) []byte {
	h := sha3.NewLegacyKeccak256()
	h.Write([]byte(signature))
	return h.Sum(nil)[:4]
}

// ParseAddress decodes a hex address, with or without the 0x prefix.
func ParseAddress(s string) (Address, error) {
	var a Address
	if len(s) >= 2 && (s[:2] == "0x" || s[:2] == "0X") {
		s = s[2:]
	}
	if len(s) != 40 {
		return a, fmt.Errorf("invalid address %q", s)
	}
	for i := 0; i < 20; i++ {
		var b byte
		for j := 0; j < 2; j++ {
			c := s[i*2+j]
			var v byte
			switch {
			case c >= '0' && c <= '9':
				v = c - '0'
			case c >= 'a' && c <= 'f':
				v = c - 'a' + 10
			case c >= 'A' && c <= 'F':
				v = c - 'A' + 10
			default:
				return a, fmt.Errorf("invalid address %q", s)
			}
			b = b<<4 | v
		}
		a[i] = b
	}
	return a, nil
}
