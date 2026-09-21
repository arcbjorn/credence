package loans_test

import (
	"encoding/hex"
	"math/big"
	"strings"
	"testing"

	"github.com/arcbjorn/credence/gateway/attest"
	"github.com/arcbjorn/credence/gateway/loans"
)

// The encoders came out of `cmd/credence-scenario`, which the acceptance
// matrix exercises against real contracts on every run. An extraction
// that changed a single byte would still compile and still look right,
// and the loan it originated would be for a different principal or bound
// to a different account.
//
// So the expectations here are pinned literals, derived from the ABI
// rather than from the implementation: a test that called the same
// function it checks would agree with itself about an encoding both got
// wrong.

func TestRepayCalldataIsTheAbiEncoding(t *testing.T) {
	got := hex.EncodeToString(loans.EncodeRepay(7))

	// repay(uint256) selector, then loan id 7 left-padded to 32 bytes.
	selector := hex.EncodeToString(attest.Selector("repay(uint256)"))
	want := selector + strings.Repeat("0", 63) + "7"

	if got != want {
		t.Fatalf("repay calldata\n got %s\nwant %s", got, want)
	}
	if len(got) != 8+64 {
		t.Fatalf("expected 4 selector bytes + one word, got %d hex chars", len(got))
	}
}

// Every field of the binding must land in its own word, in declaration
// order. A transposition here would bind the loan to the wrong account
// or the wrong facility while encoding perfectly well.
func TestOriginateCalldataPlacesEveryBindingField(t *testing.T) {
	var registry, operator, account attest.Address
	registry[19] = 0x11
	operator[19] = 0x22
	account[19] = 0x33

	var configHash, policyHash attest.Hash
	configHash[31] = 0xaa
	policyHash[31] = 0xbb

	b := attest.Binding{
		IdentityRegistryChainID: big.NewInt(10143),
		IdentityRegistry:        registry,
		AgentID:                 big.NewInt(1830),
		Operator:                operator,
		CreditAccount:           account,
		FacilityID:              big.NewInt(1),
		AccountStateVersion:     3,
		ConfigVersion:           1,
		ConfigHash:              configHash,
		PolicyEpoch:             1,
		PolicyHash:              policyHash,
	}

	data := loans.EncodeOriginate(b, 4_000_000_000)

	// 4 selector bytes, eleven binding words, one principal word.
	const words = 12
	if len(data) != 4+words*32 {
		t.Fatalf("expected %d bytes, got %d", 4+words*32, len(data))
	}

	word := func(i int) string {
		return hex.EncodeToString(data[4+i*32 : 4+(i+1)*32])
	}

	// Spot-check the fields whose transposition would be most damaging
	// and least visible: the account the loan binds to, the state
	// version that makes a stale assessment unusable, and the principal.
	if !strings.HasSuffix(word(4), "33") {
		t.Fatalf("credit account is not in word 4: %s", word(4))
	}
	if new(big.Int).SetBytes(data[4+6*32:4+7*32]).Uint64() != 3 {
		t.Fatalf("account state version is not in word 6: %s", word(6))
	}
	if new(big.Int).SetBytes(data[4+11*32:]).Uint64() != 4_000_000_000 {
		t.Fatalf("principal is not in the last word: %s", word(11))
	}
	// The registry chain id leads, because the binding starts there.
	if new(big.Int).SetBytes(data[4:36]).Uint64() != 10143 {
		t.Fatalf("chain id is not in word 0: %s", word(0))
	}
}

// A short address would left-pad into a different, valid-looking
// address, and calldata built from it would read the wrong contract
// rather than fail.
func TestShortAddressIsRefused(t *testing.T) {
	for _, bad := range []string{
		"0x1234",
		"0x",
		"",
		"0xZZ7e5F4552091A69125d5DfCb7b8C2659029395Bdf",
	} {
		if _, err := loans.ParseAddress(bad); err == nil {
			t.Fatalf("address %q should be refused", bad)
		}
	}
}

func TestWellFormedAddressParses(t *testing.T) {
	addr, err := loans.ParseAddress("0x7e5F4552091A69125d5DfCb7b8C2659029395Bdf")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if addr[0] != 0x7e || addr[19] != 0xdf {
		t.Fatalf("parsed to the wrong bytes: %x", addr)
	}
}
