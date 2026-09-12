// Package canonical implements the one encoding used for every hashed
// artifact in Credence.
//
// `docs/04` requires: UTF-8, deterministic key ordering, fixed decimal
// strings, no non-finite values, deterministic ordering of semantically
// unordered collections, and rejection of duplicate evidence IDs.
//
// Go and Rust must produce identical bytes for the same logical value. The
// reference vectors in schemas/v1 prove it; if either side drifts, those
// tests fail on both sides rather than one silently accepting the other's
// output.
package canonical

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
)

var (
	// ErrNonFinite guards floats that have no canonical decimal form.
	ErrNonFinite = errors.New("canonical: non-finite number")
	// ErrFloat rejects floats outright in hashed payloads: amounts are
	// integer base units carried as decimal strings (`docs/04`).
	ErrFloat = errors.New("canonical: float in hashed payload")
)

// Encode renders v as canonical JSON bytes.
//
// Object keys are sorted by their UTF-8 byte sequence, which is stable
// across languages — unlike locale-aware or rune-wise collation.
func Encode(v any) ([]byte, error) {
	var buf bytes.Buffer
	if err := encodeValue(&buf, v); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// Hash returns the SHA-256 of the canonical encoding.
func Hash(v any) ([32]byte, error) {
	b, err := Encode(v)
	if err != nil {
		return [32]byte{}, err
	}
	return sha256.Sum256(b), nil
}

// HashHex is Hash as lowercase hex, the form stored in SQLite and compared
// against Rust's output.
func HashHex(v any) (string, error) {
	h, err := Hash(v)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", h), nil
}

func encodeValue(buf *bytes.Buffer, v any) error {
	switch t := v.(type) {
	case nil:
		buf.WriteString("null")
	case bool:
		if t {
			buf.WriteString("true")
		} else {
			buf.WriteString("false")
		}
	case string:
		return encodeString(buf, t)
	case json.Number:
		// With UseNumber the literal text survives, so a u128-scale integer
		// round-trips exactly. A value that is actually a float is rejected
		// here rather than rounded — matching the Rust side, which sees the
		// same text through serde_json's arbitrary_precision.
		text := t.String()
		if strings.ContainsAny(text, ".eE") {
			return ErrFloat
		}
		buf.WriteString(text)
	case int:
		buf.WriteString(strconv.FormatInt(int64(t), 10))
	case int64:
		buf.WriteString(strconv.FormatInt(t, 10))
	case uint64:
		buf.WriteString(strconv.FormatUint(t, 10))
	case float64:
		// Floats never carry authorization amounts. Reject rather than
		// silently round, which would diverge from Rust.
		if math.IsNaN(t) || math.IsInf(t, 0) {
			return ErrNonFinite
		}
		return ErrFloat
	case map[string]any:
		return encodeObject(buf, t)
	case []any:
		buf.WriteByte('[')
		for i, item := range t {
			if i > 0 {
				buf.WriteByte(',')
			}
			if err := encodeValue(buf, item); err != nil {
				return err
			}
		}
		buf.WriteByte(']')
	default:
		return fmt.Errorf("canonical: unsupported type %T", v)
	}
	return nil
}

func encodeObject(buf *bytes.Buffer, m map[string]any) error {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	// Byte-wise sort: identical in Go and Rust for UTF-8 input.
	sort.Strings(keys)

	buf.WriteByte('{')
	for i, k := range keys {
		if i > 0 {
			buf.WriteByte(',')
		}
		if err := encodeString(buf, k); err != nil {
			return err
		}
		buf.WriteByte(':')
		if err := encodeValue(buf, m[k]); err != nil {
			return err
		}
	}
	buf.WriteByte('}')
	return nil
}

// encodeString emits the shortest escaping both languages agree on:
// mandatory escapes only, \u00XX for other control characters, and no
// escaping of forward slashes or non-ASCII.
func encodeString(buf *bytes.Buffer, s string) error {
	buf.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			buf.WriteString(`\"`)
		case '\\':
			buf.WriteString(`\\`)
		case '\n':
			buf.WriteString(`\n`)
		case '\r':
			buf.WriteString(`\r`)
		case '\t':
			buf.WriteString(`\t`)
		case '\b':
			buf.WriteString(`\b`)
		case '\f':
			buf.WriteString(`\f`)
		default:
			if r < 0x20 {
				fmt.Fprintf(buf, `\u%04x`, r)
			} else {
				buf.WriteRune(r)
			}
		}
	}
	buf.WriteByte('"')
	return nil
}

// DecodeStrict parses JSON into canonical-safe Go values, refusing floats
// and duplicate keys. Numbers arrive as json.Number so integer precision
// beyond float64 survives.
func DecodeStrict(data []byte) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, err
	}
	if dec.More() {
		return nil, errors.New("canonical: trailing content")
	}
	return v, nil
}
