package main

import (
	"encoding/hex"
	"fmt"
	"math/big"
	"strings"
)

// Enum values, matching contracts/src/Assessment.sol.
//
// Written out rather than derived, because the wire format is the ORDER
// these are declared in: renaming a variant is harmless, reordering one
// silently changes what every published assessment means. The test pins
// each value against the Solidity declaration.
const (
	decisionApproved    = 0
	decisionDenied      = 1
	decisionUnavailable = 2

	permissionBlocked = 0
	permissionAllowed = 1

	classScenarioOnly      = 0
	classObservedShadow    = 1
	classPrototypeEligible = 2
)

// monitoringReport mirrors the struct CREReceiver decodes.
//
// Every field is static, so the ABI encoding is 13 packed 32-byte words
// with no offset word and no dynamic tail -- which is why this can be
// hand-encoded without an ABI library. A workflow compiles to WASM and
// runs under tight size limits, so avoiding one matters.
// `test_payloadIsThirteenStaticWords` pins the layout on the Solidity
// side, and encodeMonitoringReport's test pins it here.
type monitoringReport struct {
	loanID              uint64
	accountStateVersion uint64
	sequence            uint64
	validAfter          uint64
	validUntil          uint64
	decision            uint8
	permission          uint8
	class               uint8
	assessmentID        [32]byte
	assessmentHash      [32]byte
	modelHash           [32]byte
	inputHash           [32]byte
	evidenceRoot        [32]byte
}

// encodeMonitoringReport produces the `report` argument of onReport.
//
// Field order is the struct's declaration order and must not drift from
// CREReceiver.MonitoringReport: the decoder reads by position, so swapping
// two same-width fields produces a perfectly valid decode of the wrong
// values -- a sequence read as a state version, for instance, which would
// not revert anywhere.
func encodeMonitoringReport(r monitoringReport) []byte {
	out := make([]byte, 0, 13*32)
	out = append(out, uintWord(r.loanID)...)
	out = append(out, uintWord(r.accountStateVersion)...)
	out = append(out, uintWord(r.sequence)...)
	out = append(out, uintWord(r.validAfter)...)
	out = append(out, uintWord(r.validUntil)...)
	out = append(out, uintWord(uint64(r.decision))...)
	out = append(out, uintWord(uint64(r.permission))...)
	out = append(out, uintWord(uint64(r.class))...)
	out = append(out, r.assessmentID[:]...)
	out = append(out, r.assessmentHash[:]...)
	out = append(out, r.modelHash[:]...)
	out = append(out, r.inputHash[:]...)
	out = append(out, r.evidenceRoot[:]...)
	return out
}

func uintWord(v uint64) []byte {
	var w [32]byte
	new(big.Int).SetUint64(v).FillBytes(w[:])
	return w[:]
}

// decodeEnum maps the engine's string to the on-chain value.
//
// An unrecognised name is an ERROR, never a default. Mapping an unknown
// decision to Approved would authorize on a result nobody wrote, and
// mapping it to Denied would look safe while silently discarding a
// permission the engine actually granted -- both are wrong, so neither is
// chosen.
func decodeDecision(s string) (uint8, error) {
	switch s {
	case "Approved":
		return decisionApproved, nil
	case "Denied":
		return decisionDenied, nil
	case "Unavailable":
		return decisionUnavailable, nil
	default:
		return 0, fmt.Errorf("unknown decision %q", s)
	}
}

func decodePermission(s string) (uint8, error) {
	switch s {
	case "Allowed":
		return permissionAllowed, nil
	case "Blocked":
		return permissionBlocked, nil
	default:
		return 0, fmt.Errorf("unknown permission %q", s)
	}
}

func decodeClass(s string) (uint8, error) {
	switch s {
	case "ScenarioOnly":
		return classScenarioOnly, nil
	case "ObservedShadow":
		return classObservedShadow, nil
	case "PrototypeEligible":
		return classPrototypeEligible, nil
	default:
		return 0, fmt.Errorf("unknown evidence class %q", s)
	}
}

// parseHash32 reads a 32-byte hash, with or without the 0x prefix.
//
// A short or malformed hash is refused rather than zero-padded: a zeroed
// model hash would publish an assessment that claims to come from a model
// nobody ran, and `docs/05` binds the model hash into what is signed.
func parseHash32(s string) ([32]byte, error) {
	var out [32]byte
	trimmed := strings.TrimPrefix(strings.TrimPrefix(s, "0x"), "0X")
	if len(trimmed) != 64 {
		return out, fmt.Errorf("expected a 32-byte hash, got %q", s)
	}
	b, err := hex.DecodeString(trimmed)
	if err != nil {
		return out, fmt.Errorf("invalid hash %q: %w", s, err)
	}
	copy(out[:], b)
	return out, nil
}


// nextSequenceSelector is the four-byte selector for
// `nextSequence(uint256)` on CREReceiver.
//
// Hard-coded rather than computed: keccak is not worth pulling into a
// WASM binary for one constant. The test pins it against `cast sig`, and
// a drifted selector would call nothing -- the read returns empty, which
// this workflow reads as "not enrolled" and blocks on.
func nextSequenceSelector() [4]byte {
	return [4]byte{0x1e, 0x66, 0x37, 0x20}
}
