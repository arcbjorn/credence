package attest_test

import (
	"encoding/hex"
	"testing"

	"github.com/arcbjorn/credence/gateway/attest"
)

// Emitted by contracts/test/DigestVector.t.sol for the same binding. If Go
// and Solidity disagree here, an assessment lands under one key while
// reconciliation queries another — the publisher then retries a sequence
// the chain has already accepted, forever.
const wantChainKey = "8b2bbb1c8189a67c3a98b32a81ca531869f086038f940ef00d0b125fda8c2a9f"

func TestChainKeyMatchesSolidity(t *testing.T) {
	got := attest.ChainKey(harnessBinding())
	if hex.EncodeToString(got[:]) != wantChainKey {
		t.Fatalf("chain key drift\n  solidity: %s\n  go:       %s",
			wantChainKey, hex.EncodeToString(got[:]))
	}
}

func TestChainKeyHexIsPrefixed(t *testing.T) {
	got := attest.ChainKeyHex(harnessBinding())
	if got != "0x"+wantChainKey {
		t.Fatalf("hex form = %s", got)
	}
}

// The oracle keys on identity, account, and facility. A change to any of
// them must produce a different key, or two distinct bindings would share
// on-chain state.
func TestIdentityFieldsChangeTheKey(t *testing.T) {
	base := attest.ChainKey(harnessBinding())

	mutations := map[string]func(*attest.Binding){
		"registry chain": func(b *attest.Binding) {
			b.IdentityRegistryChainID = bigOf(1)
		},
		"registry": func(b *attest.Binding) {
			b.IdentityRegistry = addr("00000000000000000000000000000000000000ff")
		},
		"agent": func(b *attest.Binding) { b.AgentID = bigOf(999) },
		"account": func(b *attest.Binding) {
			b.CreditAccount = addr("00000000000000000000000000000000000000ee")
		},
		"facility": func(b *attest.Binding) { b.FacilityID = bigOf(2) },
	}
	for name, mutate := range mutations {
		b := harnessBinding()
		mutate(&b)
		if attest.ChainKey(b) == base {
			t.Errorf("%s must change the chain key", name)
		}
	}
}

// Fields outside the oracle's key must NOT change it. Policy and config
// versions move as a binding is reassessed; if they altered the key, every
// reassessment would appear to be a new binding with sequence zero.
func TestNonKeyFieldsDoNotChangeTheKey(t *testing.T) {
	base := attest.ChainKey(harnessBinding())

	for name, mutate := range map[string]func(*attest.Binding){
		"state version":  func(b *attest.Binding) { b.AccountStateVersion = 99 },
		"config version": func(b *attest.Binding) { b.ConfigVersion = 99 },
		"config hash":    func(b *attest.Binding) { b.ConfigHash = attest.KeccakText("other") },
		"policy epoch":   func(b *attest.Binding) { b.PolicyEpoch = 99 },
		"policy hash":    func(b *attest.Binding) { b.PolicyHash = attest.KeccakText("other") },
		"operator":       func(b *attest.Binding) { b.Operator = addr("00000000000000000000000000000000000000dd") },
	} {
		b := harnessBinding()
		mutate(&b)
		if attest.ChainKey(b) != base {
			t.Errorf("%s must not change the chain key", name)
		}
	}
}
