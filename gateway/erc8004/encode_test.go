package erc8004

import (
	"encoding/hex"
	"math/big"
	"strings"
	"testing"
)

// The selectors must match the deployed registry exactly. Verified three
// ways on Monad testnet 14 September: computed here, compared against the
// values `docs/06` records, and located in the implementation behind the
// registry proxy. A drifted signature does not fail loudly -- it calls
// nothing, or something else.
func TestSelectorsMatchDeployedRegistry(t *testing.T) {
	for _, c := range []struct{ sig, want string }{
		{GiveFeedbackSig, "3c036a7e"},
		{ReadAllFeedbackSig, "d9d84224"},
		{ReadFeedbackSig, "232b0810"},
		{GetSummarySig, "81bbba58"},
		{RevokeFeedbackSig, "4ab3ca99"},
		{OwnerOfSig, "6352211e"},
	} {
		if got := hex.EncodeToString(Selector(c.sig)); got != c.want {
			t.Errorf("Selector(%s) = %s, want %s", c.sig, got, c.want)
		}
	}
}

// Encoding checked against `cast calldata`, an independent implementation.
// Hand-rolled ABI encoding verified only against itself proves nothing: a
// wrong offset decodes as a different string rather than reverting.
func TestGiveFeedbackMatchesCast(t *testing.T) {
	var h [32]byte
	raw, err := hex.DecodeString(
		"11223344556677889900aabbccddeeff00112233445566778899aabbccddeeff")
	if err != nil {
		t.Fatal(err)
	}
	copy(h[:], raw)

	got, err := EncodeGiveFeedback(Feedback{
		AgentID:       big.NewInt(184),
		Value:         big.NewInt(-2600),
		ValueDecimals: 2,
		Tag1:          "credence.loan",
		Tag2:          "defaulted",
		Endpoint:      "",
		FeedbackURI:   "ipfs://bafyfoo",
		FeedbackHash:  h,
	})
	if err != nil {
		t.Fatal(err)
	}
	const want = "0x3c036a7e00000000000000000000000000000000000000000000000000000000000000b8fffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff5d8000000000000000000000000000000000000000000000000000000000000000200000000000000000000000000000000000000000000000000000000000001000000000000000000000000000000000000000000000000000000000000000140000000000000000000000000000000000000000000000000000000000000018000000000000000000000000000000000000000000000000000000000000001a011223344556677889900aabbccddeeff00112233445566778899aabbccddeeff000000000000000000000000000000000000000000000000000000000000000d63726564656e63652e6c6f616e00000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000964656661756c74656400000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000e697066733a2f2f62616679666f6f000000000000000000000000000000000000"
	if g := "0x" + hex.EncodeToString(got); !strings.EqualFold(g, want) {
		t.Fatalf("encoding differs from cast\n got: %s\nwant: %s", g, want)
	}
}

func TestReadAllFeedbackMatchesCast(t *testing.T) {
	client, err := ParseAddress("0xCaE2590420EAb07eDF737B6069891F36e2e14A34")
	if err != nil {
		t.Fatal(err)
	}
	got, err := EncodeReadAllFeedback(
		big.NewInt(184), []Address{client}, "credence.loan", "", false)
	if err != nil {
		t.Fatal(err)
	}
	const want = "0xd9d8422400000000000000000000000000000000000000000000000000000000000000b800000000000000000000000000000000000000000000000000000000000000a000000000000000000000000000000000000000000000000000000000000000e0000000000000000000000000000000000000000000000000000000000000012000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000001000000000000000000000000cae2590420eab07edf737b6069891f36e2e14a34000000000000000000000000000000000000000000000000000000000000000d63726564656e63652e6c6f616e000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000"
	if g := "0x" + hex.EncodeToString(got); !strings.EqualFold(g, want) {
		t.Fatalf("encoding differs from cast\n got: %s\nwant: %s", g, want)
	}
}

// A negative value must sign-extend across the whole word. Encoding only
// the low 16 bytes of an int128 decodes as a large positive number, which
// would publish a loss as a gain.
func TestNegativeValueSignExtends(t *testing.T) {
	got, err := EncodeGiveFeedback(Feedback{
		AgentID: big.NewInt(1), Value: big.NewInt(-1), Tag1: "x",
	})
	if err != nil {
		t.Fatal(err)
	}
	// selector (4) then agentId (32); the value word follows.
	word := hex.EncodeToString(got[36:68])
	if word != strings.Repeat("f", 64) {
		t.Fatalf("-1 encoded as %s, want all ff", word)
	}
}

// A value the registry cannot hold must be refused rather than silently
// wrapped into a different number.
func TestValueOutsideInt128IsRefused(t *testing.T) {
	tooBig := new(big.Int).Lsh(big.NewInt(1), 127)
	if _, err := EncodeGiveFeedback(Feedback{
		AgentID: big.NewInt(1), Value: tooBig,
	}); err == nil {
		t.Fatal("expected refusal for value above int128 range")
	}
	tooSmall := new(big.Int).Neg(new(big.Int).Lsh(big.NewInt(1), 128))
	if _, err := EncodeGiveFeedback(Feedback{
		AgentID: big.NewInt(1), Value: tooSmall,
	}); err == nil {
		t.Fatal("expected refusal for value below int128 range")
	}
}
