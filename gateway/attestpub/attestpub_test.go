package attestpub_test

import (
	"context"
	"encoding/json"
	"math/big"
	"strings"
	"testing"

	"github.com/arcbjorn/credence/gateway/attest"
	"github.com/arcbjorn/credence/gateway/attestpub"
	"github.com/arcbjorn/credence/gateway/publisher"
	"github.com/decred/dcrd/dcrec/secp256k1/v4"
)

type chainStub struct {
	accepted    uint64
	submits     int
	lastSig     []byte
	lastPayload []byte
}

func (c *chainStub) AcceptedSequence(context.Context, string) (uint64, error) {
	return c.accepted, nil
}

func (c *chainStub) SubmitAccept(_ context.Context, payload, sig []byte) (string, error) {
	c.submits++
	c.lastPayload = payload
	c.lastSig = sig
	c.accepted++
	return "0xtx", nil
}

func (c *chainStub) ReceiptStatus(context.Context, string) (publisher.Status, error) {
	return publisher.StatusConfirmed, nil
}

type memJournal struct{ entries map[string]publisher.Attempt }

func (j *memJournal) Lookup(_ context.Context, k string) (*publisher.Attempt, bool, error) {
	a, ok := j.entries[k]
	if !ok {
		return nil, false, nil
	}
	return &a, true, nil
}

func (j *memJournal) Record(_ context.Context, a publisher.Attempt) error {
	j.entries[a.IdempotencyKey] = a
	return nil
}

func deployment() attestpub.Deployment {
	return attestpub.Deployment{
		ChainID:           big.NewInt(10143),
		VerifyingContract: attest.Address{0x01},
		DeploymentID:      big.NewInt(7),
		EvidencePolicyID:  attest.KeccakText("evidence-v1"),
		Route:             attest.RouteDevelopment,
	}
}

func binding() attest.Binding {
	return attest.Binding{
		IdentityRegistryChainID: big.NewInt(10143),
		AgentID:                 big.NewInt(184),
		FacilityID:              big.NewInt(1),
		AccountStateVersion:     3,
		ConfigVersion:           1,
		ConfigHash:              attest.KeccakText("config"),
		PolicyEpoch:             1,
		PolicyHash:              attest.KeccakText("policy"),
	}
}

func adapter(t *testing.T, chain *chainStub) *attestpub.Adapter {
	t.Helper()
	key, err := secp256k1.GeneratePrivateKey()
	if err != nil {
		t.Fatalf("keygen: %v", err)
	}
	pub := publisher.New(chain, &memJournal{entries: map[string]publisher.Attempt{}})
	return attestpub.New(deployment(), key, pub)
}

const validHash = "2c3744f9aecea30351bf13f04f46cadfdb5a8a04e9533c7e35c392992564a882"

func assessment(decision string) json.RawMessage {
	doc := map[string]any{
		"assessment_id":       validHash,
		"input_hash":          validHash,
		"model_hash":          "model-v1",
		"sequence":            5,
		"valid_after":         1000,
		"valid_until":         4600,
		"authorization_class": "PrototypeEligible",
		"outcome":             map[string]any{"decision": decision, "loan_id": "loan-42"},
	}
	raw, _ := json.Marshal(doc)
	return raw
}

func TestAllowedMonitoringIsSignedAndPublished(t *testing.T) {
	chain := &chainStub{accepted: 4}
	a := adapter(t, chain)

	tx, err := a.Publish(context.Background(), "b1", binding(), assessment("MonitoringAllowed"))
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	if tx != "0xtx" {
		t.Fatalf("tx = %q", tx)
	}
	if len(chain.lastSig) != 65 {
		t.Fatalf("signature length %d, want 65", len(chain.lastSig))
	}
}

// A revocation must reach the chain exactly like an approval; a blocked
// result that silently stayed home would leave stale permission standing.
func TestBlockedMonitoringIsAlsoPublished(t *testing.T) {
	chain := &chainStub{accepted: 4}
	a := adapter(t, chain)

	if _, err := a.Publish(context.Background(), "b1", binding(),
		assessment("MonitoringBlocked")); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if chain.submits != 1 {
		t.Fatal("a revocation must be delivered")
	}
}

func TestUnavailableIsPublishedAsBlocked(t *testing.T) {
	chain := &chainStub{accepted: 4}
	a := adapter(t, chain)

	if _, err := a.Publish(context.Background(), "b1", binding(),
		assessment("Unavailable")); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if chain.submits != 1 {
		t.Fatal("an unavailable result must still revoke permission")
	}
}

// The guard that matters most: an origination outcome must never be
// published through the monitoring path, where it would carry no grant and
// silently misrepresent the decision.
func TestOriginationOutcomeIsRefused(t *testing.T) {
	chain := &chainStub{accepted: 4}
	a := adapter(t, chain)

	for _, decision := range []string{"ApprovedOrigination", "Denied"} {
		_, err := a.Publish(context.Background(), "b1", binding(), assessment(decision))
		if err == nil {
			t.Fatalf("%s must not publish as monitoring", decision)
		}
		if !strings.Contains(err.Error(), "cannot be published as monitoring") {
			t.Fatalf("unexpected error for %s: %v", decision, err)
		}
	}
	if chain.submits != 0 {
		t.Fatal("nothing should reach the chain")
	}
}

// An unknown decision is a version skew between engine and gateway. Failing
// loudly beats defaulting to Blocked, which would hide the mismatch.
func TestUnknownDecisionIsRefused(t *testing.T) {
	chain := &chainStub{accepted: 4}
	a := adapter(t, chain)

	_, err := a.Publish(context.Background(), "b1", binding(), assessment("SomethingNew"))
	if err == nil || !strings.Contains(err.Error(), "unknown decision") {
		t.Fatalf("expected an unknown-decision refusal, got %v", err)
	}
	if chain.submits != 0 {
		t.Fatal("an unrecognised decision must not be published")
	}
}

func TestUnknownClassIsRefused(t *testing.T) {
	chain := &chainStub{accepted: 4}
	a := adapter(t, chain)

	var doc map[string]any
	_ = json.Unmarshal(assessment("MonitoringAllowed"), &doc)
	doc["authorization_class"] = "Invented"
	raw, _ := json.Marshal(doc)

	if _, err := a.Publish(context.Background(), "b1", binding(), raw); err == nil {
		t.Fatal("an unknown class must be refused")
	}
}

// A malformed hash must fail at the boundary, not produce a zeroed field
// that signs cleanly and misrepresents the assessment.
func TestMalformedHashIsRefused(t *testing.T) {
	chain := &chainStub{accepted: 4}
	a := adapter(t, chain)

	var doc map[string]any
	_ = json.Unmarshal(assessment("MonitoringAllowed"), &doc)
	doc["input_hash"] = "not-hex"
	raw, _ := json.Marshal(doc)

	_, err := a.Publish(context.Background(), "b1", binding(), raw)
	if err == nil || !strings.Contains(err.Error(), "input_hash") {
		t.Fatalf("expected an input_hash refusal, got %v", err)
	}
}

func TestShortHashIsRefused(t *testing.T) {
	chain := &chainStub{accepted: 4}
	a := adapter(t, chain)

	var doc map[string]any
	_ = json.Unmarshal(assessment("MonitoringAllowed"), &doc)
	doc["assessment_id"] = "abcd"
	raw, _ := json.Marshal(doc)

	if _, err := a.Publish(context.Background(), "b1", binding(), raw); err == nil {
		t.Fatal("a short hash must be refused")
	}
}

// The sequence comes from the assessment, so the publisher's reconciliation
// reasons about the same number the oracle enforces.
func TestSequenceComesFromTheAssessment(t *testing.T) {
	chain := &chainStub{accepted: 9} // ahead of the assessment's sequence 5
	a := adapter(t, chain)

	_, err := a.Publish(context.Background(), "b1", binding(), assessment("MonitoringAllowed"))
	if err == nil {
		t.Fatal("a superseded sequence must be refused")
	}
	if chain.submits != 0 {
		t.Fatal("nothing should be submitted for a superseded sequence")
	}
}

// Signing is deterministic, so a retry produces the identical envelope.
func TestSigningIsStableAcrossCalls(t *testing.T) {
	key, _ := secp256k1.GeneratePrivateKey()

	first := &chainStub{accepted: 4}
	a1 := attestpub.New(deployment(), key,
		publisher.New(first, &memJournal{entries: map[string]publisher.Attempt{}}))
	_, _ = a1.Publish(context.Background(), "b1", binding(), assessment("MonitoringAllowed"))

	second := &chainStub{accepted: 4}
	a2 := attestpub.New(deployment(), key,
		publisher.New(second, &memJournal{entries: map[string]publisher.Attempt{}}))
	_, _ = a2.Publish(context.Background(), "b1", binding(), assessment("MonitoringAllowed"))

	if string(first.lastSig) != string(second.lastSig) {
		t.Fatal("the same assessment must produce the same signature")
	}
}

func TestSignerAddressIsDerivable(t *testing.T) {
	a := adapter(t, &chainStub{})
	if a.SignerAddress() == (attest.Address{}) {
		t.Fatal("the signer address must be derivable for allowlisting")
	}
}

// The signed envelope must commit to the assessment's CONTENT.
//
// `docs/04` claims a retained assessment can be verified against what
// reached chain. Hashing only the assessment ID breaks that: two
// assessments with different reasons, stress results, or terms produce
// byte-identical calldata, so the signature attests to an identifier
// rather than to a decision.
func TestChangedOutputChangesTheSignedEnvelope(t *testing.T) {
	// One adapter, so the key is identical and only the CONTENT differs.
	// Two adapters would each generate a fresh key and the payloads would
	// differ no matter what, which proves nothing.
	chain := &chainStub{}
	a := adapter(t, chain)
	doc := map[string]any{
		"assessment_id":       validHash,
		"input_hash":          validHash,
		"model_hash":          "model-v1",
		"policy_hash":         "policy",
		"sequence":            5,
		"valid_after":         1000,
		"valid_until":         4600,
		"authorization_class": "PrototypeEligible",
		"outcome":             map[string]any{"decision": "MonitoringAllowed", "loan_id": "loan-42"},
		"reasons":             []any{"Eligible"},
	}
	rawA, _ := json.Marshal(doc)
	if _, err := a.Publish(context.Background(), "b1", binding(), rawA); err != nil {
		t.Fatalf("publish: %v", err)
	}
	firstPayload := append([]byte(nil), chain.lastPayload...)

	// Same identity, same decision, different explanation. The sequence
	// must advance or the publisher refuses it as superseded.
	doc["reasons"] = []any{"MonitoringCoverageBreach"}
	doc["sequence"] = 6
	rawB, _ := json.Marshal(doc)
	if _, err := a.Publish(context.Background(), "b1", binding(), rawB); err != nil {
		t.Fatalf("publish: %v", err)
	}

	// Compare the assessmentHash word specifically. The whole payload
	// differs because the sequence advanced, which would make a
	// full-payload comparison pass no matter what was committed.
	if assessmentHashWord(firstPayload) == assessmentHashWord(chain.lastPayload) {
		t.Fatal("a changed assessment must change the hash it commits to")
	}
}

// The engine's reported policy must match the binding being signed.
//
// Substituting the caller's policy hash silently signs an assessment as
// though it were produced under a policy the engine never used.
func TestDivergentPolicyHashIsRefused(t *testing.T) {
	doc := map[string]any{
		"assessment_id":       validHash,
		"input_hash":          validHash,
		"model_hash":          "model-v1",
		"policy_hash":         "a-different-policy",
		"sequence":            5,
		"valid_after":         1000,
		"valid_until":         4600,
		"authorization_class": "PrototypeEligible",
		"outcome":             map[string]any{"decision": "MonitoringAllowed", "loan_id": "loan-42"},
	}
	raw, _ := json.Marshal(doc)

	_, err := adapter(t, &chainStub{}).Publish(context.Background(), "b1", binding(), raw)
	if err == nil || !strings.Contains(err.Error(), "policy") {
		t.Fatalf("a divergent policy hash must be refused, got %v", err)
	}
}

// An evidence root of zero asserts "no evidence" for every assessment.
// It must commit to the evidence the decision actually rested on.
func TestEvidenceRootIsNotZero(t *testing.T) {
	chain := &chainStub{}
	doc := map[string]any{
		"assessment_id":       validHash,
		"input_hash":          validHash,
		"model_hash":          "model-v1",
		"policy_hash":         "policy",
		"sequence":            5,
		"valid_after":         1000,
		"valid_until":         4600,
		"authorization_class": "PrototypeEligible",
		"outcome":             map[string]any{"decision": "MonitoringAllowed", "loan_id": "loan-42"},
	}
	raw, _ := json.Marshal(doc)
	if _, err := adapter(t, chain).Publish(context.Background(), "b1", binding(), raw); err != nil {
		t.Fatalf("publish: %v", err)
	}

	var zero [32]byte
	if wordAt(chain.lastPayload, wordEvidenceRoot) == zero {
		t.Fatal("evidence root must commit to the evidence, not be zero")
	}
}

// Word offsets into accept() calldata, after the 4-byte selector. The
// envelope is a flat head of static words (see attest.EncodeAccept), so
// these positions are fixed.
const (
	wordAssessmentHash = 16
	wordEvidenceRoot   = 21
)

func wordAt(payload []byte, index int) [32]byte {
	var w [32]byte
	start := 4 + index*32
	if start+32 <= len(payload) {
		copy(w[:], payload[start:start+32])
	}
	return w
}

func assessmentHashWord(payload []byte) [32]byte { return wordAt(payload, wordAssessmentHash) }
