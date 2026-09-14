package main

import (
	"encoding/hex"
	"strings"
	"testing"
)

// The encoding must match Solidity's abi.encode byte for byte, because
// CREReceiver decodes it by position. Checked against `cast abi-encode`,
// an independent implementation: an encoder verified only against its own
// decoder agrees with itself about a layout both may have wrong.
//
//	cast abi-encode "f((uint256,uint64,uint64,uint64,uint64,uint8,uint8,uint8,bytes32,bytes32,bytes32,bytes32,bytes32))" \
//	  "(7,3,11,1000000,1003600,0,1,2,0x11...,0x22...,0x33...,0x44...,0x55...)"
func TestEncodingMatchesCast(t *testing.T) {
	r := monitoringReport{
		loanID:              7,
		accountStateVersion: 3,
		sequence:            11,
		validAfter:          1_000_000,
		validUntil:          1_003_600,
		decision:            decisionApproved,
		permission:          permissionAllowed,
		class:               classPrototypeEligible,
		assessmentID:        repeat(0x11),
		assessmentHash:      repeat(0x22),
		modelHash:           repeat(0x33),
		inputHash:           repeat(0x44),
		evidenceRoot:        repeat(0x55),
	}

	const want = "0x00000000000000000000000000000000000000000000000000000000000000070000000000000000000000000000000000000000000000000000000000000003000000000000000000000000000000000000000000000000000000000000000b00000000000000000000000000000000000000000000000000000000000f424000000000000000000000000000000000000000000000000000000000000f505000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000001000000000000000000000000000000000000000000000000000000000000000211111111111111111111111111111111111111111111111111111111111111112222222222222222222222222222222222222222222222222222222222222222333333333333333333333333333333333333333333333333333333333333333344444444444444444444444444444444444444444444444444444444444444445555555555555555555555555555555555555555555555555555555555555555"
	got := "0x" + hex.EncodeToString(encodeMonitoringReport(r))
	if !strings.EqualFold(got, want) {
		t.Fatalf("encoding differs from cast\n got: %s\nwant: %s", got, want)
	}
}

// Thirteen static fields, so no offset word and no dynamic tail. Pinned
// on the Solidity side too by test_payloadIsThirteenStaticWords.
func TestEncodingIsThirteenWords(t *testing.T) {
	got := encodeMonitoringReport(monitoringReport{})
	if len(got) != 13*32 {
		t.Fatalf("encoded %d bytes, want %d", len(got), 13*32)
	}
}

// The wire format is the DECLARATION ORDER of the Solidity enums, so a
// reordering there silently changes what every published assessment
// means. These are pinned against contracts/src/Assessment.sol.
func TestEnumValuesMatchSolidity(t *testing.T) {
	for _, c := range []struct {
		got  uint8
		want uint8
		name string
	}{
		{decisionApproved, 0, "Decision.Approved"},
		{decisionDenied, 1, "Decision.Denied"},
		{decisionUnavailable, 2, "Decision.Unavailable"},
		{permissionBlocked, 0, "Permission.Blocked"},
		{permissionAllowed, 1, "Permission.Allowed"},
		{classScenarioOnly, 0, "Class.ScenarioOnly"},
		{classObservedShadow, 1, "Class.ObservedShadow"},
		{classPrototypeEligible, 2, "Class.PrototypeEligible"},
	} {
		if c.got != c.want {
			t.Errorf("%s = %d, want %d", c.name, c.got, c.want)
		}
	}
}

// An unrecognised name must be refused. Defaulting to Approved would
// authorize on a result nobody wrote; defaulting to Denied would look
// safe while discarding a permission the engine granted.
func TestUnknownEnumNamesAreRefused(t *testing.T) {
	if _, err := decodeDecision("Maybe"); err == nil {
		t.Error("unknown decision accepted")
	}
	if _, err := decodePermission("Probably"); err == nil {
		t.Error("unknown permission accepted")
	}
	if _, err := decodeClass("SomethingElse"); err == nil {
		t.Error("unknown class accepted")
	}
}

// A zeroed model hash would publish an assessment claiming to come from a
// model nobody ran, so a malformed one is refused rather than padded.
func TestShortHashIsRefused(t *testing.T) {
	for _, bad := range []string{"", "0x", "0x1234", strings.Repeat("z", 64)} {
		if _, err := parseHash32(bad); err == nil {
			t.Errorf("%q accepted as a 32-byte hash", bad)
		}
	}
	good, err := parseHash32("0x" + strings.Repeat("ab", 32))
	if err != nil {
		t.Fatal(err)
	}
	if good[0] != 0xab || good[31] != 0xab {
		t.Fatal("hash did not round trip")
	}
}

func repeat(b byte) [32]byte {
	var out [32]byte
	for i := range out {
		out[i] = b
	}
	return out
}

// The selector is written as bytes in the workflow, so it cannot drift
// from the contract silently -- a wrong one calls nothing and the read
// returns empty, which reads as "not enrolled".
//
//	cast sig "nextSequence(uint256)"  ->  0x1e663720
func TestNextSequenceSelector(t *testing.T) {
	want := [4]byte{0x1e, 0x66, 0x37, 0x20}
	got := nextSequenceSelector()
	if got != want {
		t.Fatalf("selector = %x, want %x", got, want)
	}
}
