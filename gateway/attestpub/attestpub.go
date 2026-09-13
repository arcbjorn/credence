// Package attestpub turns an engine assessment into a signed envelope and
// delivers it.
//
// This is the seam between three things that must not know about each
// other: the engine produces a decision, the attestation package encodes
// and signs it, and the publisher reconciles delivery. Putting the
// translation here keeps each of those single-purpose.
//
// The translation is where a mistake would be most dangerous, because an
// envelope that misrepresents the assessment would be correctly signed and
// correctly delivered. So every field is mapped explicitly, unknown values
// are refused rather than defaulted, and a monitoring result can never
// acquire the grant fields the oracle would reject.
package attestpub

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"strings"

	"github.com/arcbjorn/credence/gateway/attest"
	"github.com/arcbjorn/credence/gateway/publisher"
	"github.com/decred/dcrd/dcrec/secp256k1/v4"
)

// Deployment is the static context every envelope is scoped to.
type Deployment struct {
	ChainID           *big.Int
	VerifyingContract attest.Address
	DeploymentID      *big.Int
	EvidencePolicyID  attest.Hash
	// Route must match what the facility pinned, or the oracle rejects the
	// envelope regardless of who signed it.
	Route uint8
}

// Adapter signs assessments and hands them to the publisher.
type Adapter struct {
	deployment Deployment
	signer     *secp256k1.PrivateKey
	pub        *publisher.Publisher
	domain     attest.Hash
}

func New(d Deployment, signer *secp256k1.PrivateKey, pub *publisher.Publisher) *Adapter {
	return &Adapter{
		deployment: d,
		signer:     signer,
		pub:        pub,
		domain:     attest.DomainSeparator(d.ChainID, d.VerifyingContract),
	}
}

// SignerAddress is the address the oracle must allowlist for this route.
func (a *Adapter) SignerAddress() attest.Address {
	return attest.SignerAddress(a.signer)
}

// assessmentDoc is the subset of the engine's output the envelope needs.
// Decoded into a narrow struct rather than a map so a missing field is a
// zero value we check, not a silent nil.
type assessmentDoc struct {
	AssessmentID string `json:"assessment_id"`
	InputHash    string `json:"input_hash"`
	ModelHash    string `json:"model_hash"`
	Sequence     uint64 `json:"sequence"`
	ValidAfter   uint64 `json:"valid_after"`
	ValidUntil   uint64 `json:"valid_until"`
	Class        string `json:"authorization_class"`
	Outcome      struct {
		Decision string `json:"decision"`
		LoanID   string `json:"loan_id"`
	} `json:"outcome"`
}

// Binding is the on-chain identity the envelope binds to. Supplied by the
// caller because the engine's snapshot carries it in a form the contract
// does not consume directly.
type Binding = attest.Binding

// Publish signs and delivers one monitoring assessment.
//
// Returns the transaction hash on confirmation. Every refusal is an error
// carrying its reason, because a caller that cannot tell "superseded" from
// "malformed" will retry the wrong one.
// The `bindingKey` argument is the gateway's local identifier, used only
// for logging; the chain key is derived from `binding` below.
func (a *Adapter) Publish(
	ctx context.Context,
	bindingKey string,
	binding Binding,
	assessment json.RawMessage,
) (string, error) {
	_ = bindingKey
	var doc assessmentDoc
	if err := json.Unmarshal(assessment, &doc); err != nil {
		return "", fmt.Errorf("decode assessment: %w", err)
	}

	envelope, err := a.envelope(doc, binding)
	if err != nil {
		return "", err
	}

	digest := attest.Digest(a.domain, envelope)
	signature, err := attest.Sign(digest, a.signer)
	if err != nil {
		return "", fmt.Errorf("sign envelope: %w", err)
	}

	// Encode the contract call here: the publisher reconciles and delivers
	// bytes, and should not need to know the oracle's ABI.
	calldata := attest.EncodeAccept(envelope, signature)

	// Reconciliation must query the slot the oracle actually writes, which
	// is derived from the binding's identity fields — not the gateway's own
	// identifier for the same binding.
	return a.pub.Publish(ctx, attest.ChainKeyHex(binding), doc.Sequence, calldata, signature)
}

func (a *Adapter) envelope(doc assessmentDoc, binding Binding) (attest.Assessment, error) {
	decision, permission, err := mapDecision(doc.Outcome.Decision)
	if err != nil {
		return attest.Assessment{}, err
	}
	class, err := mapClass(doc.Class)
	if err != nil {
		return attest.Assessment{}, err
	}

	assessmentID, err := parseHash(doc.AssessmentID)
	if err != nil {
		return attest.Assessment{}, fmt.Errorf("assessment_id: %w", err)
	}
	inputHash, err := parseHash(doc.InputHash)
	if err != nil {
		return attest.Assessment{}, fmt.Errorf("input_hash: %w", err)
	}

	loanID, err := parseLoanID(doc.Outcome.LoanID)
	if err != nil {
		return attest.Assessment{}, err
	}

	// Monitoring only. An origination envelope carries a grant, which this
	// adapter deliberately cannot construct — the oracle rejects grant
	// fields on a monitoring kind, and building them here would mean the
	// bug surfaced on chain instead of at the boundary.
	return attest.Assessment{
		DeploymentID:       a.deployment.DeploymentID,
		EvidencePolicyID:   a.deployment.EvidencePolicyID,
		Route:              a.deployment.Route,
		Binding:            binding,
		Kind:               attest.KindMonitoring,
		AssessmentID:       assessmentID,
		AssessmentHash:     hashText(doc.AssessmentID),
		Decision:           decision,
		Permission:         permission,
		ModelHash:          hashText(doc.ModelHash),
		InputHash:          inputHash,
		EvidenceRoot:       attest.Hash{},
		Class:              class,
		Sequence:           doc.Sequence,
		ValidAfter:         doc.ValidAfter,
		ValidUntil:         doc.ValidUntil,
		RequestID:          attest.Hash{},
		IntentHash:         attest.Hash{},
		SettlementAsset:    attest.Address{},
		RequestedPrincipal: big.NewInt(0),
		Terms: attest.Terms{
			ApprovedPrincipal: big.NewInt(0),
			RequiredEquity:    big.NewInt(0),
		},
		LoanID: loanID,
	}, nil
}

// mapDecision refuses anything it does not recognise. Defaulting an unknown
// decision to Blocked would be safe on chain but would hide a version skew
// between engine and gateway.
func mapDecision(decision string) (uint8, uint8, error) {
	switch decision {
	case "MonitoringAllowed":
		return attest.DecisionApproved, attest.PermissionAllowed, nil
	case "MonitoringBlocked":
		return attest.DecisionDenied, attest.PermissionBlocked, nil
	case "Unavailable":
		return attest.DecisionUnavailable, attest.PermissionBlocked, nil
	case "ApprovedOrigination", "Denied":
		return 0, 0, fmt.Errorf("origination outcome %q cannot be published as monitoring", decision)
	default:
		return 0, 0, fmt.Errorf("unknown decision %q", decision)
	}
}

func mapClass(class string) (uint8, error) {
	switch class {
	case "ScenarioOnly":
		return attest.ClassScenarioOnly, nil
	case "ObservedShadow":
		return attest.ClassObservedShadow, nil
	case "PrototypeEligible":
		return attest.ClassPrototypeEligible, nil
	default:
		return 0, fmt.Errorf("unknown authorization class %q", class)
	}
}

func parseHash(s string) (attest.Hash, error) {
	var h attest.Hash
	b, err := hex.DecodeString(strings.TrimPrefix(s, "0x"))
	if err != nil {
		return h, fmt.Errorf("not hex: %w", err)
	}
	if len(b) != 32 {
		return h, fmt.Errorf("expected 32 bytes, got %d", len(b))
	}
	copy(h[:], b)
	return h, nil
}

// hashText keccaks a string so a non-hex identifier still binds into the
// envelope rather than being dropped.
func hashText(s string) attest.Hash {
	return attest.KeccakText(s)
}

// parseLoanID accepts a decimal id. Loan ids are contract-assigned integers;
// anything else is a mismatch worth failing on.
func parseLoanID(s string) (*big.Int, error) {
	if s == "" {
		return big.NewInt(0), nil
	}
	v, ok := new(big.Int).SetString(strings.TrimPrefix(s, "loan-"), 10)
	if !ok {
		return nil, fmt.Errorf("loan id %q is not numeric", s)
	}
	return v, nil
}
