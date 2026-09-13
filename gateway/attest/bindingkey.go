package attest

// ChainKey derives the key the RiskOracle stores state under.
//
// Mirrors `RiskOracle.bindingKey`:
//
//	keccak256(abi.encode(identityRegistryChainId, identityRegistry,
//	                     agentId, creditAccount, facilityId))
//
// This is deliberately *not* the gateway's own binding key. The gateway
// uses a local string identifier for its database; the oracle keys on
// identity fields. Conflating them means a submission lands under one key
// while reconciliation queries another — the assessment looks unpublished
// forever, and the publisher retries a sequence the chain has already
// accepted.
//
// Keeping the derivation here, beside the EIP-712 encoding it must agree
// with, means both are verified against the same contract by the same test.
func ChainKey(b Binding) Hash {
	return keccak(
		uintWord(b.IdentityRegistryChainID),
		addrWord(b.IdentityRegistry),
		uintWord(b.AgentID),
		addrWord(b.CreditAccount),
		uintWord(b.FacilityID),
	)
}

// ChainKeyHex renders the key for an eth_call argument.
func ChainKeyHex(b Binding) string {
	k := ChainKey(b)
	return "0x" + hexOf(k[:])
}

func hexOf(b []byte) string {
	const digits = "0123456789abcdef"
	out := make([]byte, len(b)*2)
	for i, c := range b {
		out[i*2] = digits[c>>4]
		out[i*2+1] = digits[c&0x0f]
	}
	return string(out)
}
