package erc8004

import (
	"encoding/hex"
	"math/big"
	"os"
	"strings"
	"testing"
)

// Decoded against a response the LIVE registry actually returned, not a
// fixture this package built. A decoder checked only against its own
// encoder agrees with itself about a layout both may have wrong.
//
// Captured 14 September from Monad testnet, agent 1, all clients:
//
//	cast call 0x8004B663056A597Dffe9eCcC1965A193B7388713 \
//	  --data $(cast calldata "readAllFeedback(uint256,address[],string,string,bool)" \
//	           1 "[]" "" "" false)
func realResponse(t *testing.T) []byte {
	t.Helper()
	raw, err := os.ReadFile("testdata/agent1.hex")
	if err != nil {
		t.Fatal(err)
	}
	b, err := hex.DecodeString(strings.TrimSpace(string(raw)))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestDecodeRealRegistryResponse(t *testing.T) {
	entries, err := DecodeReadAllFeedback(realResponse(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 9 {
		t.Fatalf("decoded %d entries, want the 9 on chain", len(entries))
	}

	// Row 0, read from the chain by cast at capture time:
	//   client 0xb6E8B2692cdc3A31280DCa8E9C8b88bb5e436f24
	//   index 1, value 90, decimals 0, tag1 "quality", tag2 "monad"
	got := entries[0]
	if want := "b6e8b2692cdc3a31280dca8e9c8b88bb5e436f24"; hex.EncodeToString(got.Client[:]) != want {
		t.Errorf("client = %s, want %s", hex.EncodeToString(got.Client[:]), want)
	}
	if got.Index != 1 {
		t.Errorf("index = %d, want 1", got.Index)
	}
	if got.Value.Cmp(big.NewInt(90)) != 0 {
		t.Errorf("value = %s, want 90", got.Value)
	}
	if got.Tag1 != "quality" || got.Tag2 != "monad" {
		t.Errorf("tags = %q/%q, want quality/monad", got.Tag1, got.Tag2)
	}
	if got.Revoked {
		t.Error("row 0 is not revoked on chain")
	}

	// The second client wrote eight entries, indexes 1..8, all tagged
	// crosscheck. Index 7 carries value 10 with tag2 "fail".
	if entries[7].Tag1 != "crosscheck" {
		t.Errorf("row 7 tag1 = %q, want crosscheck", entries[7].Tag1)
	}
	if entries[7].Value.Cmp(big.NewInt(10)) != 0 {
		t.Errorf("row 7 value = %s, want 10", entries[7].Value)
	}
	if entries[7].Tag2 != "fail" {
		t.Errorf("row 7 tag2 = %q, want fail", entries[7].Tag2)
	}

	// Indexes are per client and 1-based, so the second client restarts
	// at 1 rather than continuing from the first client's count.
	if entries[1].Index != 1 {
		t.Errorf("second client's first entry has index %d, want 1",
			entries[1].Index)
	}
}

// A published loss is negative. Reading the value word as unsigned turns
// -2,600,000,000 into a number near 2^256, which would report a default as
// an enormous gain.
func TestDecodeRecoversNegativeValues(t *testing.T) {
	var h [32]byte
	call, err := EncodeGiveFeedback(Feedback{
		AgentID:       big.NewInt(1830),
		Value:         big.NewInt(-2_600_000_000),
		ValueDecimals: 6,
		Tag1:          TagLoss,
		Tag2:          OutcomeDefaulted,
		FeedbackHash:  h,
	})
	if err != nil {
		t.Fatal(err)
	}
	// The value is the second word of the argument block.
	word := call[36:68]
	vals, err := decodeIntArray(
		append(uintWord(big.NewInt(1)), word...), 0)
	if err != nil {
		t.Fatal(err)
	}
	if vals[0].Cmp(big.NewInt(-2_600_000_000)) != 0 {
		t.Fatalf("decoded %s, want -2600000000", vals[0])
	}
}

// A malformed response must be refused rather than decoded into rows that
// pair one entry's value with another's tag.
func TestDecodeRefusesMalformed(t *testing.T) {
	full := realResponse(t)
	for _, c := range []struct {
		name string
		in   []byte
	}{
		{"empty", nil},
		{"short head", full[:100]},
		{"truncated body", full[:len(full)/2]},
	} {
		if _, err := DecodeReadAllFeedback(c.in); err == nil {
			t.Errorf("%s: decoded without error", c.name)
		}
	}
}
