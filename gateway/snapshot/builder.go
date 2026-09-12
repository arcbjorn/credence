// Package snapshot assembles a RiskInput from stored evidence.
//
// The builder's job is assembly and honesty, not judgment. It decides
// nothing about risk: it gathers observations, records what is missing, and
// hands the engine a complete picture of what is and is not known.
//
// Two rules from `docs/04` shape it:
//
//  1. **An acquisition failure changes availability, not origin.** A
//     provider that fails produces a `Missing` observation, never a
//     substituted value. There is deliberately no code path that fills a
//     gap with a synthetic default.
//
//  2. **The engine allocates no identity.** Sequence and validity are
//     proposals supplied here and validated there, so the gateway owns
//     timing and the engine stays pure.
package snapshot

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/arcbjorn/credence/gateway/canonical"
)

// Origin of an observation. Fixed when it is recorded.
type Origin string

const (
	OriginObserved         Origin = "Observed"
	OriginSynthetic        Origin = "Synthetic"
	OriginPolicyAssumption Origin = "PolicyAssumption"
)

// State is availability, independent of origin.
type State string

const (
	StatePresent     State = "Present"
	StateMissing     State = "Missing"
	StateStale       State = "Stale"
	StateConflicting State = "Conflicting"
	StateUnsupported State = "Unsupported"
)

type Coverage string

const (
	CoverageComplete Coverage = "Complete"
	CoveragePartial  Coverage = "Partial"
	CoverageUnknown  Coverage = "Unknown"
)

type Requirement string

const (
	Required Requirement = "Required"
	Optional Requirement = "Optional"
)

// Evidence mirrors risk_core::evidence::Evidence. Field names and tags match
// the Rust representation exactly; a mismatch would change the snapshot hash
// and break replay.
type Evidence struct {
	Value             any         `json:"value"`
	State             State       `json:"state"`
	Origin            Origin      `json:"origin"`
	Requirement       Requirement `json:"requirement"`
	Source            string      `json:"source"`
	ObservedAt        *uint64     `json:"observed_at"`
	RetrievedAt       uint64      `json:"retrieved_at"`
	CoverageValue     Coverage    `json:"coverage"`
	EvidenceID        string      `json:"evidence_id"`
	ContentHash       string      `json:"content_hash"`
	ParentEvidenceIDs []string    `json:"parent_evidence_ids"`
	ScenarioID        *string     `json:"scenario_id"`
}

// Present records a successfully acquired observation.
func Present(
	id, source string,
	origin Origin,
	requirement Requirement,
	observedAt, retrievedAt uint64,
	value any,
) (Evidence, error) {
	hash, err := canonical.HashHex(value)
	if err != nil {
		return Evidence{}, fmt.Errorf("hash %s: %w", id, err)
	}
	return Evidence{
		Value:             value,
		State:             StatePresent,
		Origin:            origin,
		Requirement:       requirement,
		Source:            source,
		ObservedAt:        &observedAt,
		RetrievedAt:       retrievedAt,
		CoverageValue:     CoverageComplete,
		EvidenceID:        id,
		ContentHash:       hash,
		ParentEvidenceIDs: []string{},
	}, nil
}

// Missing records an acquisition failure.
//
// The origin of what *would* have been fetched is preserved, and no value is
// substituted. This is the only way the builder represents a failed fetch —
// there is no variant that quietly supplies a default.
func Missing(id, source string, origin Origin, requirement Requirement, retrievedAt uint64) Evidence {
	return Evidence{
		Value:             nil,
		State:             StateMissing,
		Origin:            origin,
		Requirement:       requirement,
		Source:            source,
		RetrievedAt:       retrievedAt,
		CoverageValue:     CoverageUnknown,
		EvidenceID:        id,
		ContentHash:       "",
		ParentEvidenceIDs: []string{},
	}
}

// Partial records an observation that arrived but does not cover its scope.
// A response can be current and still incomplete (`docs/04`).
func Partial(
	id, source string,
	origin Origin,
	requirement Requirement,
	observedAt, retrievedAt uint64,
	value any,
) (Evidence, error) {
	ev, err := Present(id, source, origin, requirement, observedAt, retrievedAt, value)
	if err != nil {
		return Evidence{}, err
	}
	ev.CoverageValue = CoveragePartial
	return ev, nil
}

// Proposal is the sequence and validity window the gateway proposes.
type Proposal struct {
	Sequence   uint64 `json:"sequence"`
	ValidAfter uint64 `json:"valid_after"`
	ValidUntil uint64 `json:"valid_until"`
}

// Input mirrors risk_core::input::RiskInput.
type Input struct {
	SchemaVersion  string          `json:"schema_version"`
	FacilityPolicy string          `json:"facility_policy"`
	RequestedClass string          `json:"requested_class"`
	AssessmentTime uint64          `json:"assessment_time"`
	ScenarioID     *string         `json:"scenario_id"`
	Proposal       Proposal        `json:"proposal"`
	Binding        json.RawMessage `json:"binding"`
	Context        json.RawMessage `json:"context"`
	Account        Evidence        `json:"account"`
	Market         Evidence        `json:"market"`
	History        Evidence        `json:"history"`
	Forecasts      Evidence        `json:"forecasts"`
	Portfolio      Evidence        `json:"portfolio"`
	ModelPolicy    json.RawMessage `json:"model_policy"`
}

var (
	ErrDuplicateEvidenceID = errors.New("snapshot: duplicate evidence id")
	ErrSyntheticInObserved = errors.New("snapshot: synthetic evidence in an observed facility")
	ErrScenarioIDMissing   = errors.New("snapshot: scenario facility requires a scenario id")
)

// Validate catches what the gateway can catch before spending a round trip.
//
// This does not duplicate the engine's checks — the engine still validates
// everything independently, because a gateway bug must not be able to
// produce an unvalidated decision. This only fails fast on problems the
// gateway itself introduced.
func (in *Input) Validate() error {
	seen := map[string]bool{}
	for _, ev := range in.evidence() {
		if ev.EvidenceID == "" {
			return fmt.Errorf("snapshot: evidence id required")
		}
		if seen[ev.EvidenceID] {
			return fmt.Errorf("%w: %s", ErrDuplicateEvidenceID, ev.EvidenceID)
		}
		seen[ev.EvidenceID] = true

		// Catching this here means a misconfigured provider fails loudly at
		// assembly rather than producing a snapshot the engine must reject.
		if in.FacilityPolicy == "Observed" && ev.Origin == OriginSynthetic {
			return fmt.Errorf("%w: %s", ErrSyntheticInObserved, ev.EvidenceID)
		}
	}

	if in.FacilityPolicy == "Scenario" && (in.ScenarioID == nil || *in.ScenarioID == "") {
		return ErrScenarioIDMissing
	}
	if in.Proposal.ValidUntil <= in.Proposal.ValidAfter {
		return fmt.Errorf("snapshot: validity window is empty")
	}
	if in.AssessmentTime < in.Proposal.ValidAfter || in.AssessmentTime >= in.Proposal.ValidUntil {
		return fmt.Errorf("snapshot: assessment time outside its validity window")
	}
	return nil
}

func (in *Input) evidence() []Evidence {
	return []Evidence{in.Account, in.Market, in.History, in.Forecasts, in.Portfolio}
}

// SyntheticUsed reports whether any input would cap the result at
// scenario-only. Surfaced so the caller can label the result before the
// engine returns.
func (in *Input) SyntheticUsed() bool {
	for _, ev := range in.evidence() {
		if ev.Origin == OriginSynthetic {
			return true
		}
	}
	return false
}

// BlockingEvidence lists required observations that are not usable.
// Reported so the gateway can log why an assessment will be unavailable
// without inferring the decision itself.
func (in *Input) BlockingEvidence() []string {
	var blocking []string
	for _, ev := range in.evidence() {
		if ev.Requirement != Required {
			continue
		}
		if ev.State != StatePresent || ev.Value == nil || ev.CoverageValue != CoverageComplete {
			blocking = append(blocking, ev.EvidenceID)
		}
	}
	return blocking
}

// Canonical returns the exact bytes the engine will hash.
//
// `docs/04` requires persisting these bytes, not a row that could later be
// re-serialized differently.
func (in *Input) Canonical() ([]byte, error) {
	raw, err := json.Marshal(in)
	if err != nil {
		return nil, fmt.Errorf("marshal input: %w", err)
	}
	value, err := canonical.DecodeStrict(raw)
	if err != nil {
		return nil, fmt.Errorf("decode input: %w", err)
	}
	return canonical.Encode(value)
}

// InputHash is the SHA-256 of the canonical encoding, matching the engine's
// own `input_hash`.
func (in *Input) InputHash() (string, error) {
	raw, err := json.Marshal(in)
	if err != nil {
		return "", err
	}
	value, err := canonical.DecodeStrict(raw)
	if err != nil {
		return "", err
	}
	return canonical.HashHex(value)
}
