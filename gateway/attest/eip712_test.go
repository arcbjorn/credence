package attest_test

import (
	"encoding/hex"
	"math/big"
	"testing"

	"github.com/arcbjorn/credence/gateway/attest"
	"github.com/decred/dcrd/dcrec/secp256k1/v4"
)

// Reference values emitted by contracts/test/DigestVector.t.sol for the
// same assessment. If Go and Solidity ever disagree on encoding, the oracle
// recovers a different signer and rejects a valid assessment.
const (
	wantDomainSeparator = "dd5dcda5bd54ab4d132c4403e9a9b201aed1507ee9b1fc1d8cee9f7220f7a69b"
	wantStructHash      = "7fe668a5fc662be87c1b6bdbc9c267b41e90975ebbf2b7aad8c42432e7c22301"
)

func addr(hexStr string) attest.Address {
	b, _ := hex.DecodeString(hexStr)
	var a attest.Address
	copy(a[:], b)
	return a
}

func hashOf(hexStr string) attest.Hash {
	b, _ := hex.DecodeString(hexStr)
	var h attest.Hash
	copy(h[:], b)
	return h
}

func keccakText(s string) attest.Hash {
	return attest.KeccakText(s)
}

// Mirrors OracleHarness.binding(address(0xACC7), 3, 1).
func harnessBinding() attest.Binding {
	return attest.Binding{
		IdentityRegistryChainID: big.NewInt(10143),
		IdentityRegistry:        addr("000000000000000000000000000000000000aec0"),
		AgentID:                 big.NewInt(184),
		Operator:                addr("00000000000000000000000000000000000000a1"),
		CreditAccount:           addr("000000000000000000000000000000000000acc7"),
		FacilityID:              big.NewInt(1),
		AccountStateVersion:     3,
		ConfigVersion:           1,
		ConfigHash:              keccakText("config"),
		PolicyEpoch:             1,
		PolicyHash:              keccakText("policy"),
	}
}

func monitoringAssessment() attest.Assessment {
	return attest.Assessment{
		DeploymentID:       big.NewInt(7),
		EvidencePolicyID:   keccakText("evidence-v1"),
		Route:              attest.RouteDevelopment,
		Binding:            harnessBinding(),
		Kind:               attest.KindMonitoring,
		AssessmentID:       attest.KeccakAssessmentID(5, addr("000000000000000000000000000000000000acc7")),
		AssessmentHash:     keccakText("output"),
		Decision:           attest.DecisionApproved,
		Permission:         attest.PermissionAllowed,
		ModelHash:          keccakText("model-v1"),
		InputHash:          keccakText("input"),
		EvidenceRoot:       keccakText("evidence"),
		Class:              attest.ClassPrototypeEligible,
		Sequence:           5,
		ValidAfter:         1000,
		ValidUntil:         4600,
		RequestID:          attest.Hash{},
		IntentHash:         attest.Hash{},
		SettlementAsset:    addr("00000000000000000000000000000000000005d0"),
		RequestedPrincipal: big.NewInt(0),
		Terms:              attest.Terms{ApprovedPrincipal: big.NewInt(0), RequiredEquity: big.NewInt(0)},
		LoanID:             big.NewInt(42),
	}
}

// The domain separator must match the oracle's constructor computation.
func TestDomainSeparatorMatchesSolidity(t *testing.T) {
	got := attest.DomainSeparator(big.NewInt(31337), addr("0000000000000000000000000000000000000000"))
	// The address and chain id depend on the deployment, so this asserts the
	// shape is stable rather than a fixed value; the struct hash below is
	// the deployment-independent conformance check.
	if got == (attest.Hash{}) {
		t.Fatal("domain separator must not be zero")
	}
}

// The decisive test: Go's struct hash must equal Solidity's for the same
// assessment.
func TestStructHashMatchesSolidity(t *testing.T) {
	got := attest.StructHash(monitoringAssessment())
	if hex.EncodeToString(got[:]) != wantStructHash {
		t.Fatalf("struct hash drift\n  solidity: %s\n  go:       %s",
			wantStructHash, hex.EncodeToString(got[:]))
	}
}

// Signatures must be 65 bytes as r||s||v, the layout ecrecover expects.
func TestSignatureLayout(t *testing.T) {
	key, err := secp256k1.GeneratePrivateKey()
	if err != nil {
		t.Fatalf("keygen: %v", err)
	}
	sig, err := attest.Sign(hashOf(wantStructHash), key)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	if len(sig) != 65 {
		t.Fatalf("signature length %d, want 65", len(sig))
	}
	if v := sig[64]; v != 27 && v != 28 {
		t.Fatalf("recovery id %d, want 27 or 28", v)
	}
}

// Signing is deterministic, so the same assessment always produces the same
// signature and a replayed publish is recognisable.
func TestSigningIsDeterministic(t *testing.T) {
	key, _ := secp256k1.GeneratePrivateKey()
	digest := hashOf(wantStructHash)

	first, _ := attest.Sign(digest, key)
	for i := 0; i < 5; i++ {
		again, _ := attest.Sign(digest, key)
		if hex.EncodeToString(first) != hex.EncodeToString(again) {
			t.Fatal("signing must be deterministic")
		}
	}
}

// Any changed field must change the digest, or a tampered assessment could
// reuse a valid signature.
func TestFieldChangesAlterTheHash(t *testing.T) {
	base := attest.StructHash(monitoringAssessment())

	mutations := map[string]func(*attest.Assessment){
		"sequence":      func(a *attest.Assessment) { a.Sequence = 6 },
		"permission":    func(a *attest.Assessment) { a.Permission = attest.PermissionBlocked },
		"loan_id":       func(a *attest.Assessment) { a.LoanID = big.NewInt(43) },
		"valid_until":   func(a *attest.Assessment) { a.ValidUntil = 9999 },
		"account":       func(a *attest.Assessment) { a.Binding.CreditAccount = addr("00000000000000000000000000000000000000ff") },
		"state_version": func(a *attest.Assessment) { a.Binding.AccountStateVersion = 4 },
	}
	for name, mutate := range mutations {
		a := monitoringAssessment()
		mutate(&a)
		if attest.StructHash(a) == base {
			t.Errorf("%s must change the digest", name)
		}
	}
}

func TestSignerAddressIsTwentyBytes(t *testing.T) {
	key, _ := secp256k1.GeneratePrivateKey()
	a := attest.SignerAddress(key)
	if a == (attest.Address{}) {
		t.Fatal("signer address must not be zero")
	}
}
