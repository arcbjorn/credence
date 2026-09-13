package attest

// BindingWords encodes a Binding as the eleven static ABI words a contract
// expects when it takes one as a struct parameter.
//
// Shared so every caller produces byte-identical encoding. A field in the
// wrong order here is not a compile error anywhere -- it surfaces as a
// contract rejecting a binding that looks correct in the logs.
func BindingWords(b Binding) []byte {
	out := Word(b.IdentityRegistryChainID)
	out = append(out, AddressWord(b.IdentityRegistry)...)
	out = append(out, Word(b.AgentID)...)
	out = append(out, AddressWord(b.Operator)...)
	out = append(out, AddressWord(b.CreditAccount)...)
	out = append(out, Word(b.FacilityID)...)
	out = append(out, Uint64Word(b.AccountStateVersion)...)
	out = append(out, Uint64Word(b.ConfigVersion)...)
	out = append(out, b.ConfigHash[:]...)
	out = append(out, Uint64Word(b.PolicyEpoch)...)
	out = append(out, b.PolicyHash[:]...)
	return out
}
