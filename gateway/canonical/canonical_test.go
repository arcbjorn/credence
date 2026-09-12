package canonical_test

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"

	"github.com/arcbjorn/credence/gateway/canonical"
)

type vector struct {
	Name    string          `json:"name"`
	Value   json.RawMessage `json:"value"`
	Encoded string          `json:"encoded"`
	Sha256  string          `json:"sha256"`
}

func load(t *testing.T) []vector {
	t.Helper()
	raw, err := os.ReadFile("../../schemas/v1/canonical-vectors.json")
	if err != nil {
		t.Fatalf("read vectors: %v", err)
	}
	var doc struct {
		Vectors []vector `json:"vectors"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("parse vectors: %v", err)
	}
	return doc.Vectors
}

// Reads the committed file rather than regenerating, so an accidental change
// to the encoder fails here instead of silently rewriting the contract Rust
// is tested against.
func TestVectorsRoundTrip(t *testing.T) {
	for _, v := range load(t) {
		val, err := canonical.DecodeStrict(v.Value)
		if err != nil {
			t.Fatalf("%s: decode: %v", v.Name, err)
		}
		enc, err := canonical.Encode(val)
		if err != nil {
			t.Fatalf("%s: encode: %v", v.Name, err)
		}
		if string(enc) != v.Encoded {
			t.Errorf("%s: encoding drift\n  want %s\n  got  %s", v.Name, v.Encoded, enc)
		}
		h, err := canonical.HashHex(val)
		if err != nil {
			t.Fatalf("%s: hash: %v", v.Name, err)
		}
		if h != v.Sha256 {
			t.Errorf("%s: hash drift\n  want %s\n  got  %s", v.Name, v.Sha256, h)
		}
	}
}

func TestKeyOrderDoesNotAffectHash(t *testing.T) {
	a, _ := canonical.DecodeStrict([]byte(`{"b":1,"a":2,"c":3}`))
	b, _ := canonical.DecodeStrict([]byte(`{"c":3,"a":2,"b":1}`))
	ha, _ := canonical.HashHex(a)
	hb, _ := canonical.HashHex(b)
	if ha != hb {
		t.Fatalf("key order changed hash: %s vs %s", ha, hb)
	}
}

func TestArrayOrderChangesHash(t *testing.T) {
	a, _ := canonical.DecodeStrict([]byte(`{"e":[1,2,3]}`))
	b, _ := canonical.DecodeStrict([]byte(`{"e":[3,2,1]}`))
	ha, _ := canonical.HashHex(a)
	hb, _ := canonical.HashHex(b)
	if ha == hb {
		t.Fatal("array order must change the hash")
	}
}

// docs/04: no floating-point path produces an amount used in authorization.
func TestFloatsRejected(t *testing.T) {
	v, err := canonical.DecodeStrict([]byte(`{"x":1.5}`))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if _, err := canonical.Encode(v); err == nil {
		t.Fatal("expected float to be rejected")
	}
}

// A u128-scale amount must survive; rounding through float64 would corrupt it.
func TestLargeIntegerSurvives(t *testing.T) {
	const big = "340282366920938463463374607431768211455"
	v, err := canonical.DecodeStrict([]byte(`{"amount":` + big + `}`))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	enc, err := canonical.Encode(v)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if !bytes.Contains(enc, []byte(big)) {
		t.Fatalf("large integer corrupted: %s", enc)
	}
}
