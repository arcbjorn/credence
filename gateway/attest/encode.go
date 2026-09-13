package attest

import (
	"math/big"
)

// EncodeAccept builds the calldata for RiskOracle.accept(Assessment, bytes).
//
// The Assessment struct contains only static types plus two nested static
// structs, so it encodes inline as a head — no offsets. The trailing
// `bytes signature` is dynamic and therefore carries one.
//
// Hand-rolled to avoid an ABI dependency for one call, and verified against
// the deployed contract rather than trusted: an encoding mistake here fails
// as a revert with no message, which is the hardest kind to diagnose.
func EncodeAccept(a Assessment, signature []byte) []byte {
	selector := keccak([]byte(
		"accept((uint256,bytes32,uint8,(uint256,address,uint256,address,address," +
			"uint256,uint64,uint64,bytes32,uint64,bytes32),uint8,bytes32,bytes32," +
			"uint8,uint8,bytes32,bytes32,bytes32,uint8,uint64,uint64,uint64," +
			"bytes32,bytes32,address,uint256,(uint256,uint32,uint64,uint256,bytes32)," +
			"uint256),bytes)"))

	var head []byte
	appendWord := func(b []byte) { head = append(head, b...) }

	appendWord(uintWord(a.DeploymentID))
	appendWord(a.EvidencePolicyID[:])
	appendWord(u8Word(a.Route))

	// Binding, inlined: every member is static.
	appendWord(uintWord(a.Binding.IdentityRegistryChainID))
	appendWord(addrWord(a.Binding.IdentityRegistry))
	appendWord(uintWord(a.Binding.AgentID))
	appendWord(addrWord(a.Binding.Operator))
	appendWord(addrWord(a.Binding.CreditAccount))
	appendWord(uintWord(a.Binding.FacilityID))
	appendWord(u64Word(a.Binding.AccountStateVersion))
	appendWord(u64Word(a.Binding.ConfigVersion))
	appendWord(a.Binding.ConfigHash[:])
	appendWord(u64Word(a.Binding.PolicyEpoch))
	appendWord(a.Binding.PolicyHash[:])

	appendWord(u8Word(a.Kind))
	appendWord(a.AssessmentID[:])
	appendWord(a.AssessmentHash[:])
	appendWord(u8Word(a.Decision))
	appendWord(u8Word(a.Permission))
	appendWord(a.ModelHash[:])
	appendWord(a.InputHash[:])
	appendWord(a.EvidenceRoot[:])
	appendWord(u8Word(a.Class))
	appendWord(u64Word(a.Sequence))
	appendWord(u64Word(a.ValidAfter))
	appendWord(u64Word(a.ValidUntil))
	appendWord(a.RequestID[:])
	appendWord(a.IntentHash[:])
	appendWord(addrWord(a.SettlementAsset))
	appendWord(uintWord(a.RequestedPrincipal))

	// Terms, inlined.
	appendWord(uintWord(a.Terms.ApprovedPrincipal))
	appendWord(u32Word(a.Terms.AprBps))
	appendWord(u64Word(a.Terms.TermSeconds))
	appendWord(uintWord(a.Terms.RequiredEquity))
	appendWord(a.Terms.PermittedActionHash[:])

	appendWord(uintWord(a.LoanID))

	// The signature offset is measured from the start of the argument
	// block: the struct head, plus this offset word itself.
	offset := len(head) + 32
	out := append([]byte{}, selector[:4]...)
	out = append(out, head...)
	out = append(out, uintWord(big.NewInt(int64(offset)))...)

	// Dynamic bytes: length, then data padded to a word boundary.
	out = append(out, uintWord(big.NewInt(int64(len(signature))))...)
	out = append(out, signature...)
	if pad := len(signature) % 32; pad != 0 {
		out = append(out, make([]byte, 32-pad)...)
	}
	return out
}

// Word, AddressWord, and Uint64Word expose ABI word encoding so callers can
// build calldata for other contracts without duplicating the padding rules
// — a second copy is a second place for a padding bug to hide.
func Word(v *big.Int) []byte { return uintWord(v) }

func AddressWord(a Address) []byte { return addrWord(a) }

func Uint64Word(v uint64) []byte { return u64Word(v) }
