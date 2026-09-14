// Package erc8004 encodes calls to the ERC-8004 registries.
//
// This is the standard's ABI, not a Credence one. The local adapter in
// contracts/src/FeedbackRegistry.sol shares no selectors with it and uses
// a different value model, so `docs/06` records the move as a rewrite
// rather than a configuration change.
//
// Every signature here was verified three ways against Monad testnet on
// 14 September: computed from the text below, compared against the
// selector `docs/06` records, and located in the deployed implementation
// behind the registry's EIP-1967 proxy slot. A signature that drifts from
// the deployed contract produces a call that reverts or, worse, silently
// hits a different function, so the test pins the selectors.
package erc8004

import (
	"fmt"
	"math/big"
)

// Registry function signatures. Written out in full because the selector
// is derived from the exact text: abbreviating a parameter type here
// changes the selector and the call lands nowhere.
const (
	GiveFeedbackSig = "giveFeedback(uint256,int128,uint8," +
		"string,string,string,string,bytes32)"
	ReadAllFeedbackSig = "readAllFeedback(uint256,address[],string,string,bool)"
	ReadFeedbackSig    = "readFeedback(uint256,address,uint64)"
	GetSummarySig      = "getSummary(uint256,address[],string,string)"
	RevokeFeedbackSig  = "revokeFeedback(uint256,uint64)"

	// IdentityRegistry is an ERC-721. Agent ids are token ids, so an
	// unregistered id reverts ERC721NonexistentToken rather than
	// returning an empty record.
	OwnerOfSig = "ownerOf(uint256)"
)

// Feedback is one entry as the registry stores it.
//
// The registry holds ONE signed value per entry plus two short tags.
// `docs/06` forbids combining a repayment, a profit, a forecast score and
// an absence of default into a single fact, so Credence publishes one
// entry per signal and distinguishes them by tag rather than packing
// several numbers into Value.
type Feedback struct {
	AgentID *big.Int
	// Value carries a single measurement, scaled by ValueDecimals.
	// Signed because some signals are losses.
	Value *big.Int
	// ValueDecimals scales Value: 2 means Value is hundredths.
	ValueDecimals uint8
	// Tag1 names the signal; Tag2 qualifies it. Both are filterable by
	// readAllFeedback, which is how a reader selects one signal without
	// interpreting the others.
	Tag1 string
	Tag2 string
	// Endpoint is where the agent was reached, empty when not applicable.
	Endpoint string
	// FeedbackURI points at the retained record this entry summarises,
	// and FeedbackHash commits to that record's bytes. Together they are
	// what makes an entry checkable rather than merely asserted.
	FeedbackURI  string
	FeedbackHash [32]byte
}

// EncodeGiveFeedback builds calldata for giveFeedback.
//
// Four of the eight parameters are dynamic strings, so the argument block
// is a head of eight words followed by a tail holding each string's
// length and padded bytes. An offset measured from the wrong base is the
// classic failure here: it does not revert, it decodes as a different
// string, so the offsets are computed from the head size rather than
// written as literals.
func EncodeGiveFeedback(f Feedback) ([]byte, error) {
	if f.AgentID == nil {
		return nil, fmt.Errorf("agent id required")
	}
	if f.AgentID.Sign() < 0 {
		return nil, fmt.Errorf("agent id must not be negative")
	}
	value := f.Value
	if value == nil {
		value = big.NewInt(0)
	}
	// int128 range. A value outside it would silently wrap into a
	// different number on chain, publishing a figure nobody computed.
	if err := checkInt128(value); err != nil {
		return nil, err
	}

	const argWords = 8
	head := make([]byte, 0, argWords*32)
	var tail []byte

	// Offsets are relative to the start of the argument block, and every
	// string sits after the whole head.
	offset := argWords * 32
	appendString := func(s string) {
		head = append(head, uintWord(big.NewInt(int64(offset)))...)
		encoded := encodeString(s)
		tail = append(tail, encoded...)
		offset += len(encoded)
	}

	head = append(head, uintWord(f.AgentID)...)
	head = append(head, int128Word(value)...)
	head = append(head, uintWord(big.NewInt(int64(f.ValueDecimals)))...)
	appendString(f.Tag1)
	appendString(f.Tag2)
	appendString(f.Endpoint)
	appendString(f.FeedbackURI)
	head = append(head, f.FeedbackHash[:]...)

	out := append([]byte{}, Selector(GiveFeedbackSig)...)
	out = append(out, head...)
	return append(out, tail...), nil
}

// EncodeReadAllFeedback builds calldata for readAllFeedback.
//
// An empty clientAddresses list means "every client". Credence reads with
// its own address to prove provenance: the standard attributes feedback
// to a client and expects consumers to choose which clients they trust,
// so reading unfiltered would mix in entries Credence never wrote.
func EncodeReadAllFeedback(
	agentID *big.Int,
	clients []Address,
	tag1, tag2 string,
	includeRevoked bool,
) ([]byte, error) {
	if agentID == nil {
		return nil, fmt.Errorf("agent id required")
	}

	const argWords = 5
	head := make([]byte, 0, argWords*32)
	var tail []byte
	offset := argWords * 32

	head = append(head, uintWord(agentID)...)

	// address[] is dynamic: offset in the head, length plus elements in
	// the tail.
	head = append(head, uintWord(big.NewInt(int64(offset)))...)
	arr := uintWord(big.NewInt(int64(len(clients))))
	for _, c := range clients {
		arr = append(arr, addressWord(c)...)
	}
	tail = append(tail, arr...)
	offset += len(arr)

	for _, s := range []string{tag1, tag2} {
		head = append(head, uintWord(big.NewInt(int64(offset)))...)
		encoded := encodeString(s)
		tail = append(tail, encoded...)
		offset += len(encoded)
	}

	head = append(head, boolWord(includeRevoked)...)

	out := append([]byte{}, Selector(ReadAllFeedbackSig)...)
	out = append(out, head...)
	return append(out, tail...), nil
}

// EncodeOwnerOf builds calldata for the identity registry's ownerOf.
func EncodeOwnerOf(agentID *big.Int) ([]byte, error) {
	if agentID == nil {
		return nil, fmt.Errorf("agent id required")
	}
	return append(Selector(OwnerOfSig), uintWord(agentID)...), nil
}
