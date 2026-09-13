// Package attest builds and signs the EIP-712 envelope the RiskOracle
// accepts.
//
// The typehash strings here must match `contracts/src/RiskOracle.sol`
// character for character. A single space or reordered field produces a
// different digest, the oracle recovers a different address, and the
// assessment is rejected as an unauthorized signer — a failure that looks
// like a key problem but is really an encoding one. The conformance test
// pins both sides against the deployed constants.
package attest

import (
	"fmt"
	"math/big"

	"github.com/decred/dcrd/dcrec/secp256k1/v4"
	"github.com/decred/dcrd/dcrec/secp256k1/v4/ecdsa"
	"golang.org/x/crypto/sha3"
)

const (
	bindingTypeString = "Binding(uint256 identityRegistryChainId,address identityRegistry," +
		"uint256 agentId,address operator,address creditAccount,uint256 facilityId," +
		"uint64 accountStateVersion,uint64 configVersion,bytes32 configHash," +
		"uint64 policyEpoch,bytes32 policyHash)"

	termsTypeString = "Terms(uint256 approvedPrincipal,uint32 aprBps,uint64 termSeconds," +
		"uint256 requiredEquity,bytes32 permittedActionHash)"

	assessmentTypeString = "Assessment(uint256 deploymentId,bytes32 evidencePolicyId,uint8 route," +
		"Binding binding,uint8 kind,bytes32 assessmentId,bytes32 assessmentHash,uint8 decision," +
		"uint8 permission,bytes32 modelHash,bytes32 inputHash,bytes32 evidenceRoot,uint8 class_," +
		"uint64 sequence,uint64 validAfter,uint64 validUntil,bytes32 requestId,bytes32 intentHash," +
		"address settlementAsset,uint256 requestedPrincipal,Terms terms,uint256 loanId)" +
		bindingTypeString + termsTypeString

	domainTypeString = "EIP712Domain(string name,string version,uint256 chainId," +
		"address verifyingContract)"
)

// Enum values, matching the ordinals in `contracts/src/Assessment.sol`.
const (
	RouteUnset uint8 = iota
	RouteDevelopment
	RouteCRE
)

const (
	KindOrigination uint8 = iota
	KindMonitoring
)

const (
	DecisionApproved uint8 = iota
	DecisionDenied
	DecisionUnavailable
)

const (
	PermissionBlocked uint8 = iota
	PermissionAllowed
)

const (
	ClassScenarioOnly uint8 = iota
	ClassObservedShadow
	ClassPrototypeEligible
)

type Address [20]byte
type Hash [32]byte

type Binding struct {
	IdentityRegistryChainID *big.Int
	IdentityRegistry        Address
	AgentID                 *big.Int
	Operator                Address
	CreditAccount           Address
	FacilityID              *big.Int
	AccountStateVersion     uint64
	ConfigVersion           uint64
	ConfigHash              Hash
	PolicyEpoch             uint64
	PolicyHash              Hash
}

type Terms struct {
	ApprovedPrincipal   *big.Int
	AprBps              uint32
	TermSeconds         uint64
	RequiredEquity      *big.Int
	PermittedActionHash Hash
}

type Assessment struct {
	DeploymentID       *big.Int
	EvidencePolicyID   Hash
	Route              uint8
	Binding            Binding
	Kind               uint8
	AssessmentID       Hash
	AssessmentHash     Hash
	Decision           uint8
	Permission         uint8
	ModelHash          Hash
	InputHash          Hash
	EvidenceRoot       Hash
	Class              uint8
	Sequence           uint64
	ValidAfter         uint64
	ValidUntil         uint64
	RequestID          Hash
	IntentHash         Hash
	SettlementAsset    Address
	RequestedPrincipal *big.Int
	Terms              Terms
	LoanID             *big.Int
}

// KeccakText hashes a string, matching Solidity's keccak256("...").
func KeccakText(s string) Hash { return keccak([]byte(s)) }

// KeccakAssessmentID mirrors the harness's
// keccak256(abi.encode("assessment", sequence, account)).
func KeccakAssessmentID(sequence uint64, account Address) Hash {
	strOffset := word(big.NewInt(0x60).Bytes())
	return keccak(
		strOffset,
		u64Word(sequence),
		addrWord(account),
		word(big.NewInt(10).Bytes()),
		rightPad([]byte("assessment")),
	)
}

func rightPad(b []byte) []byte {
	out := make([]byte, 32)
	copy(out, b)
	return out
}

func keccak(parts ...[]byte) Hash {
	h := sha3.NewLegacyKeccak256()
	for _, p := range parts {
		h.Write(p)
	}
	var out Hash
	copy(out[:], h.Sum(nil))
	return out
}

// word left-pads a value into a 32-byte ABI slot.
func word(b []byte) []byte {
	out := make([]byte, 32)
	copy(out[32-len(b):], b)
	return out
}

func uintWord(v *big.Int) []byte {
	if v == nil {
		return make([]byte, 32)
	}
	return word(v.Bytes())
}

func u64Word(v uint64) []byte   { return uintWord(new(big.Int).SetUint64(v)) }
func u32Word(v uint32) []byte   { return uintWord(new(big.Int).SetUint64(uint64(v))) }
func u8Word(v uint8) []byte     { return uintWord(new(big.Int).SetUint64(uint64(v))) }
func addrWord(a Address) []byte { return word(a[:]) }

// DomainSeparator mirrors the oracle's constructor computation.
func DomainSeparator(chainID *big.Int, verifyingContract Address) Hash {
	nameHash := keccak([]byte("Credence"))
	versionHash := keccak([]byte("1"))
	typeHash := keccak([]byte(domainTypeString))
	return keccak(
		typeHash[:], nameHash[:], versionHash[:],
		uintWord(chainID), addrWord(verifyingContract),
	)
}

func bindingHash(b Binding) Hash {
	th := keccak([]byte(bindingTypeString))
	return keccak(
		th[:],
		uintWord(b.IdentityRegistryChainID),
		addrWord(b.IdentityRegistry),
		uintWord(b.AgentID),
		addrWord(b.Operator),
		addrWord(b.CreditAccount),
		uintWord(b.FacilityID),
		u64Word(b.AccountStateVersion),
		u64Word(b.ConfigVersion),
		b.ConfigHash[:],
		u64Word(b.PolicyEpoch),
		b.PolicyHash[:],
	)
}

func termsHash(t Terms) Hash {
	th := keccak([]byte(termsTypeString))
	return keccak(
		th[:],
		uintWord(t.ApprovedPrincipal),
		u32Word(t.AprBps),
		u64Word(t.TermSeconds),
		uintWord(t.RequiredEquity),
		t.PermittedActionHash[:],
	)
}

// StructHash mirrors the oracle's split head/tail hashing, which exists
// there to stay under the stack limit. The split is part of the contract's
// encoding, so it must be reproduced exactly rather than simplified.
func StructHash(a Assessment) Hash {
	typeHash := keccak([]byte(assessmentTypeString))
	bh := bindingHash(a.Binding)

	head := keccak(
		typeHash[:],
		uintWord(a.DeploymentID),
		a.EvidencePolicyID[:],
		u8Word(a.Route),
		bh[:],
		u8Word(a.Kind),
		a.AssessmentID[:],
		a.AssessmentHash[:],
		u8Word(a.Decision),
		u8Word(a.Permission),
	)

	th := termsHash(a.Terms)
	tail := keccak(
		a.ModelHash[:],
		a.InputHash[:],
		a.EvidenceRoot[:],
		u8Word(a.Class),
		u64Word(a.Sequence),
		u64Word(a.ValidAfter),
		u64Word(a.ValidUntil),
		a.RequestID[:],
		a.IntentHash[:],
		addrWord(a.SettlementAsset),
		uintWord(a.RequestedPrincipal),
		th[:],
		uintWord(a.LoanID),
	)

	return keccak(head[:], tail[:])
}

// Digest is the EIP-712 message the oracle recovers a signer from.
func Digest(domain Hash, a Assessment) Hash {
	sh := StructHash(a)
	return keccak([]byte{0x19, 0x01}, domain[:], sh[:])
}

func (h Hash) Bytes() []byte { return h[:] }

// Sign produces the 65-byte r||s||v signature ecrecover expects.
func Sign(digest Hash, privKey *secp256k1.PrivateKey) ([]byte, error) {
	sig := ecdsa.SignCompact(privKey, digest[:], false)
	if len(sig) != 65 {
		return nil, fmt.Errorf("unexpected signature length %d", len(sig))
	}

	// dcrd returns v||r||s with v in {27,28} for uncompressed keys;
	// Solidity's ecrecover wants r||s||v.
	out := make([]byte, 65)
	copy(out[0:32], sig[1:33])
	copy(out[32:64], sig[33:65])
	out[64] = sig[0]
	return out, nil
}

// SignerAddress derives the Ethereum address for a key, so a caller can
// register the right signer with the oracle.
func SignerAddress(privKey *secp256k1.PrivateKey) Address {
	pub := privKey.PubKey().SerializeUncompressed()
	h := keccak(pub[1:]) // drop the 0x04 prefix
	var addr Address
	copy(addr[:], h[12:])
	return addr
}

// KeccakBytes hashes raw bytes, for callers computing contract-side keys.
func KeccakBytes(b []byte) Hash { return keccak(b) }
