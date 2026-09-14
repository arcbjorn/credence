package erc8004

import (
	"fmt"
	"math/big"
)

// Entry is one feedback row as read back from the registry.
type Entry struct {
	Client        Address
	Index         uint64
	Value         *big.Int
	ValueDecimals uint8
	Tag1          string
	Tag2          string
	Revoked       bool
}

// DecodeReadAllFeedback decodes the registry's seven parallel arrays.
//
// The return is seven dynamic arrays, so the head is seven offsets and
// every array is length-prefixed in the tail. Decoding these by assuming a
// fixed layout is how a reader silently pairs one agent's value with
// another's tag, so each offset is followed rather than computed.
func DecodeReadAllFeedback(out []byte) ([]Entry, error) {
	if len(out) < 7*32 {
		return nil, fmt.Errorf("short response: %d bytes", len(out))
	}

	offsets := make([]uint64, 7)
	for i := range offsets {
		o, err := readOffset(out, i*32)
		if err != nil {
			return nil, err
		}
		offsets[i] = o
	}

	clients, err := decodeAddressArray(out, offsets[0])
	if err != nil {
		return nil, fmt.Errorf("clients: %w", err)
	}
	indexes, err := decodeUintArray(out, offsets[1])
	if err != nil {
		return nil, fmt.Errorf("indexes: %w", err)
	}
	values, err := decodeIntArray(out, offsets[2])
	if err != nil {
		return nil, fmt.Errorf("values: %w", err)
	}
	decimals, err := decodeUintArray(out, offsets[3])
	if err != nil {
		return nil, fmt.Errorf("decimals: %w", err)
	}
	tag1s, err := decodeStringArray(out, offsets[4])
	if err != nil {
		return nil, fmt.Errorf("tag1: %w", err)
	}
	tag2s, err := decodeStringArray(out, offsets[5])
	if err != nil {
		return nil, fmt.Errorf("tag2: %w", err)
	}
	revoked, err := decodeUintArray(out, offsets[6])
	if err != nil {
		return nil, fmt.Errorf("revoked: %w", err)
	}

	n := len(clients)
	// Every array describes the same rows. A short one is not a row with
	// a missing field, it is a decode that has gone wrong, and pairing
	// across it would attribute one entry's value to another.
	for _, l := range []int{
		len(indexes), len(values), len(decimals),
		len(tag1s), len(tag2s), len(revoked),
	} {
		if l != n {
			return nil, fmt.Errorf(
				"registry returned %d clients but %d in a parallel array", n, l)
		}
	}

	entries := make([]Entry, n)
	for i := 0; i < n; i++ {
		entries[i] = Entry{
			Client:        clients[i],
			Index:         indexes[i],
			Value:         values[i],
			ValueDecimals: uint8(decimals[i]),
			Tag1:          tag1s[i],
			Tag2:          tag2s[i],
			Revoked:       revoked[i] != 0,
		}
	}
	return entries, nil
}

// DecodeAddress reads a single address return value.
func DecodeAddress(out []byte) (Address, error) {
	var a Address
	if len(out) < 32 {
		return a, fmt.Errorf("short address response: %d bytes", len(out))
	}
	copy(a[:], out[12:32])
	return a, nil
}

func readOffset(out []byte, at int) (uint64, error) {
	if at+32 > len(out) {
		return 0, fmt.Errorf("offset past end")
	}
	v := new(big.Int).SetBytes(out[at : at+32])
	if !v.IsUint64() || v.Uint64() > uint64(len(out)) {
		return 0, fmt.Errorf("offset %s outside response", v)
	}
	return v.Uint64(), nil
}

func arrayLen(out []byte, off uint64) (int, error) {
	if off+32 > uint64(len(out)) {
		return 0, fmt.Errorf("length past end")
	}
	v := new(big.Int).SetBytes(out[off : off+32])
	// An absurd length from a malformed response would otherwise drive a
	// huge allocation before failing.
	if !v.IsUint64() || v.Uint64() > uint64(len(out))/32 {
		return 0, fmt.Errorf("implausible array length %s", v)
	}
	return int(v.Uint64()), nil
}

func decodeUintArray(out []byte, off uint64) ([]uint64, error) {
	n, err := arrayLen(out, off)
	if err != nil {
		return nil, err
	}
	vals := make([]uint64, n)
	for i := 0; i < n; i++ {
		at := off + 32 + uint64(i)*32
		if at+32 > uint64(len(out)) {
			return nil, fmt.Errorf("element %d past end", i)
		}
		v := new(big.Int).SetBytes(out[at : at+32])
		if !v.IsUint64() {
			return nil, fmt.Errorf("element %d does not fit uint64", i)
		}
		vals[i] = v.Uint64()
	}
	return vals, nil
}

// decodeIntArray reads signed values, undoing the two's-complement
// sign extension. Reading these as unsigned turns a published loss into a
// number near 2^256, which is the same bug as encoding one wrongly.
func decodeIntArray(out []byte, off uint64) ([]*big.Int, error) {
	n, err := arrayLen(out, off)
	if err != nil {
		return nil, err
	}
	vals := make([]*big.Int, n)
	for i := 0; i < n; i++ {
		at := off + 32 + uint64(i)*32
		if at+32 > uint64(len(out)) {
			return nil, fmt.Errorf("element %d past end", i)
		}
		v := new(big.Int).SetBytes(out[at : at+32])
		if v.Cmp(maxInt128) > 0 {
			v = new(big.Int).Sub(v, two256)
		}
		vals[i] = v
	}
	return vals, nil
}

func decodeAddressArray(out []byte, off uint64) ([]Address, error) {
	n, err := arrayLen(out, off)
	if err != nil {
		return nil, err
	}
	vals := make([]Address, n)
	for i := 0; i < n; i++ {
		at := off + 32 + uint64(i)*32
		if at+32 > uint64(len(out)) {
			return nil, fmt.Errorf("element %d past end", i)
		}
		copy(vals[i][:], out[at+12:at+32])
	}
	return vals, nil
}

func decodeStringArray(out []byte, off uint64) ([]string, error) {
	n, err := arrayLen(out, off)
	if err != nil {
		return nil, err
	}
	vals := make([]string, n)
	for i := 0; i < n; i++ {
		at := off + 32 + uint64(i)*32
		if at+32 > uint64(len(out)) {
			return nil, fmt.Errorf("element %d past end", i)
		}
		// Element offsets in an array of dynamic types are relative to
		// the start of the array body, not the response.
		rel := new(big.Int).SetBytes(out[at : at+32])
		if !rel.IsUint64() {
			return nil, fmt.Errorf("element %d offset does not fit", i)
		}
		s, err := decodeString(out, off+32+rel.Uint64())
		if err != nil {
			return nil, fmt.Errorf("element %d: %w", i, err)
		}
		vals[i] = s
	}
	return vals, nil
}

func decodeString(out []byte, off uint64) (string, error) {
	if off+32 > uint64(len(out)) {
		return "", fmt.Errorf("string length past end")
	}
	l := new(big.Int).SetBytes(out[off : off+32])
	if !l.IsUint64() || off+32+l.Uint64() > uint64(len(out)) {
		return "", fmt.Errorf("string body past end")
	}
	return string(out[off+32 : off+32+l.Uint64()]), nil
}
