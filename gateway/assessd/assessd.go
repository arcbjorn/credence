// Package assessd serves monitoring assessments over HTTP.
//
// The CRE workflow needs to ask "reassess loan N" and get an answer it can
// put on chain. It cannot ask the engine directly: `docs/01` keeps the
// engine pure, so `POST /v1/assess` takes a complete `{input, policy}`
// snapshot and performs no I/O of its own. Something has to read chain and
// evidence and build that snapshot, which is the gateway's job.
//
// This package is that boundary and nothing more. It does not decide
// anything: it collects evidence, calls the engine, normalizes the answer
// into the same typed fields a development signer would produce, and
// returns them. The judgment stays in the engine and the enforcement stays
// on chain.
//
// **Why it returns rather than publishes.** On the development route the
// gateway signs and sends. On the CRE route it must not: the DON signs the
// report and the KeystoneForwarder verifies it, and a gateway that also
// published would be a second authority for the same facility -- exactly
// what `docs/05` forbids by pinning ONE route per epoch.
package assessd

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/arcbjorn/credence/gateway/attest"
	"github.com/arcbjorn/credence/gateway/scheduler"
	"github.com/arcbjorn/credence/gateway/snapshot"
)

// MaxBodyBytes bounds a request. A service that can be made to buffer an
// unbounded body is a service that can be made to fall over.
const MaxBodyBytes = 1 << 20

// EvidenceSource supplies the observations a monitoring snapshot needs.
// The same interface the monitor loop uses, so a deployment configures one
// source and both paths see identical evidence.
type EvidenceSource interface {
	Collect(ctx context.Context, task scheduler.Task, now int64) (snapshot.Input, error)
}

// Engine evaluates a snapshot. Narrowed to the one call this needs.
type Engine interface {
	Assess(ctx context.Context, input, policy json.RawMessage) (json.RawMessage, error)
}

// Normalizer turns an engine assessment into the oracle's typed fields.
type Normalizer interface {
	Envelope(binding attest.Binding, assessment json.RawMessage) (attest.Assessment, error)
}

// Retainer stores the snapshot and assessment a decision was made from.
// `docs/04` requires a decision be replayable, and that applies whether or
// not the answer ever reaches chain.
type Retainer interface {
	RetainDecision(ctx context.Context, bindingKey, inputHash string, snapshot, assessment []byte) error
}

// Bindings resolves a loan id to the binding it was originated under.
//
// The caller names a loan; this says what that loan IS. A service that
// accepted a caller-supplied binding would let anyone have an assessment
// produced for an account they do not own.
type Bindings interface {
	Lookup(loanID uint64) (attest.Binding, bool)
}

type Config struct {
	// Policy passed to the engine, pinned per deployment.
	Policy json.RawMessage
	// ValiditySeconds bounds how long an assessment stands. Zero leaves
	// the engine's own window in place.
	ValiditySeconds uint64
}

type Server struct {
	evidence EvidenceSource
	engine   Engine
	norm     Normalizer
	bindings Bindings
	retainer Retainer
	cfg      Config
	now      func() int64
}

func New(
	evidence EvidenceSource,
	engine Engine,
	norm Normalizer,
	bindings Bindings,
	retainer Retainer,
	cfg Config,
) *Server {
	return &Server{
		evidence: evidence,
		engine:   engine,
		norm:     norm,
		bindings: bindings,
		retainer: retainer,
		cfg:      cfg,
		now:      func() int64 { return time.Now().Unix() },
	}
}

// Request is what a caller asks for.
//
// Deliberately just a loan id. Everything else -- the binding, the policy,
// the evidence -- comes from this deployment's own configuration, so a
// caller cannot steer the assessment by supplying different inputs.
type Request struct {
	LoanID uint64 `json:"loan_id"`
}

// Response is the normalized assessment, in the shape the CRE workflow
// encodes for `CREReceiver`.
//
// Field names match what `cre/underwrite` decodes. Decision and permission
// are separate because `docs/04` treats them as separate facts: a Denied
// result that claimed Allowed would be a contradiction, not a permissive
// edge case.
type Response struct {
	Decision            string `json:"decision"`
	Permission          string `json:"permission"`
	Class               string `json:"class"`
	ModelHash           string `json:"model_hash"`
	InputHash           string `json:"input_hash"`
	EvidenceRoot        string `json:"evidence_root"`
	AssessmentHash      string `json:"assessment_hash"`
	AccountStateVersion uint64 `json:"account_state_version"`
	LoanID              uint64 `json:"loan_id"`
}

type errorBody struct {
	Error  string `json:"error"`
	Detail string `json:"detail,omitempty"`
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", s.health)
	mux.HandleFunc("/v1/assess", s.assess)
	return mux
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// assess reassesses one loan.
//
// Every failure path returns an error rather than a permissive default.
// The caller's correct response to an error is to publish nothing, which
// leaves the standing permission to expire -- and `docs/05` makes expired
// permission block risk increases on its own. A service that returned a
// Blocked assessment on an internal failure would instead be asserting a
// judgment the engine never made.
func (s *Server) assess(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "")
		return
	}

	var req Request
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, MaxBodyBytes))
	// Unknown fields are a version skew, not a harmless extra: a caller
	// asking for something this build does not implement should be told.
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", err.Error())
		return
	}
	if req.LoanID == 0 {
		writeError(w, http.StatusBadRequest, "invalid_body", "loan_id is required")
		return
	}

	resp, status, err := s.Assess(r.Context(), req)
	if err != nil {
		writeError(w, status, errorCode(status), err.Error())
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

// Assess is the handler's body, exported so it is testable without HTTP.
func (s *Server) Assess(ctx context.Context, req Request) (*Response, int, error) {
	binding, ok := s.bindings.Lookup(req.LoanID)
	if !ok {
		// Not found, not an internal error: the caller named a loan this
		// deployment does not monitor.
		return nil, http.StatusNotFound,
			fmt.Errorf("loan %d is not monitored by this deployment", req.LoanID)
	}

	now := s.now()
	task := scheduler.Task{
		BindingKey: attest.ChainKeyHex(binding),
		LoanID:     strconv.FormatUint(req.LoanID, 10),
		// A caller-driven reassessment is a heartbeat: no market event
		// prompted it, the schedule did. Labelling it a price move would
		// put a cause in the record that nothing observed.
		Trigger:    scheduler.TriggerHeartbeat,
		QueuedAt:   now,
	}

	input, err := s.evidence.Collect(ctx, task, now)
	if err != nil {
		return nil, http.StatusServiceUnavailable,
			fmt.Errorf("collect evidence: %w", err)
	}
	// A snapshot the gateway itself malformed must not reach the engine.
	if err := input.Validate(); err != nil {
		return nil, http.StatusInternalServerError,
			fmt.Errorf("invalid snapshot: %w", err)
	}
	// The loop's defining constraint: monitoring evaluates an existing
	// loan and can never fund one. A builder bug that produced an
	// origination context here would turn a health check into a draw.
	if err := requireMonitoringContext(input.Context); err != nil {
		return nil, http.StatusInternalServerError, err
	}

	inputJSON, err := json.Marshal(input)
	if err != nil {
		return nil, http.StatusInternalServerError,
			fmt.Errorf("marshal snapshot: %w", err)
	}

	raw, err := s.engine.Assess(ctx, inputJSON, s.cfg.Policy)
	if err != nil {
		return nil, http.StatusServiceUnavailable, fmt.Errorf("assess: %w", err)
	}

	// Retained BEFORE the answer is returned: a decision the caller never
	// managed to publish was still made, and `docs/04` requires it be
	// replayable. Retaining only what reached chain would lose exactly the
	// cases worth investigating.
	if s.retainer != nil {
		inputHash, _ := input.InputHash()
		if err := s.retainer.RetainDecision(
			ctx, task.BindingKey, inputHash, inputJSON, raw); err != nil {
			return nil, http.StatusInternalServerError, fmt.Errorf("retain: %w", err)
		}
	}

	// Normalized through the SAME path the development route signs, so the
	// two cannot drift. `docs/05` requires the CRE receiver to produce the
	// same typed fields a development signer would have.
	envelope, err := s.norm.Envelope(binding, raw)
	if err != nil {
		return nil, http.StatusInternalServerError, fmt.Errorf("normalize: %w", err)
	}

	decision, permission, err := names(envelope.Decision, envelope.Permission)
	if err != nil {
		return nil, http.StatusInternalServerError, err
	}
	class, err := className(envelope.Class)
	if err != nil {
		return nil, http.StatusInternalServerError, err
	}

	return &Response{
		Decision:            decision,
		Permission:          permission,
		Class:               class,
		ModelHash:           hashHex(envelope.ModelHash),
		InputHash:           hashHex(envelope.InputHash),
		EvidenceRoot:        hashHex(envelope.EvidenceRoot),
		AssessmentHash:      hashHex(envelope.AssessmentHash),
		AccountStateVersion: binding.AccountStateVersion,
		LoanID:              req.LoanID,
	}, http.StatusOK, nil
}

// requireMonitoringContext refuses a snapshot that is not about an open
// loan.
//
// The same shape the monitor loop enforces, deliberately: this path
// reaches the same engine with the same consequences, and a second
// spelling of the rule would be a second place for it to drift.
//
// Three separate things are checked, and the third is the one that
// matters most: a context that carried a borrower REQUEST would be an
// origination wearing a monitoring label, which is how a health check
// becomes a draw.
func requireMonitoringContext(raw json.RawMessage) error {
	if len(raw) == 0 {
		return errors.New("snapshot carries no context")
	}
	var probe struct {
		Kind    string          `json:"kind"`
		Loan    json.RawMessage `json:"loan"`
		Request json.RawMessage `json:"request"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil {
		return errors.New("undecodable context")
	}
	if probe.Kind != "Monitoring" {
		return fmt.Errorf("context kind %q is not Monitoring", probe.Kind)
	}
	if len(probe.Loan) == 0 {
		return errors.New("monitoring context names no active loan")
	}
	if len(probe.Request) != 0 {
		return errors.New("monitoring context carries a borrower request")
	}
	return nil
}

// names renders the on-chain enums back as the strings the workflow parses.
//
// Round-tripped through the envelope rather than read off the engine's
// output, so the vocabulary the workflow sees is the one the chain uses --
// the engine says "MonitoringAllowed", the chain says Approved plus
// Allowed, and only one of those is what the receiver encodes.
func names(decision, permission uint8) (string, string, error) {
	var d, p string
	switch decision {
	case attest.DecisionApproved:
		d = "Approved"
	case attest.DecisionDenied:
		d = "Denied"
	case attest.DecisionUnavailable:
		d = "Unavailable"
	default:
		return "", "", fmt.Errorf("unknown decision %d", decision)
	}
	switch permission {
	case attest.PermissionAllowed:
		p = "Allowed"
	case attest.PermissionBlocked:
		p = "Blocked"
	default:
		return "", "", fmt.Errorf("unknown permission %d", permission)
	}
	return d, p, nil
}

func className(class uint8) (string, error) {
	switch class {
	case attest.ClassScenarioOnly:
		return "ScenarioOnly", nil
	case attest.ClassObservedShadow:
		return "ObservedShadow", nil
	case attest.ClassPrototypeEligible:
		return "PrototypeEligible", nil
	default:
		return "", fmt.Errorf("unknown authorization class %d", class)
	}
}

func hashHex(h attest.Hash) string { return "0x" + hex.EncodeToString(h[:]) }

func errorCode(status int) string {
	switch status {
	case http.StatusNotFound:
		return "not_found"
	case http.StatusServiceUnavailable:
		return "unavailable"
	case http.StatusBadRequest:
		return "invalid_body"
	default:
		return "internal"
	}
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, status int, code, detail string) {
	writeJSON(w, status, errorBody{Error: code, Detail: detail})
}

// StaticBindings resolves loans from a fixed table.
//
// The operator says which loans are monitored and what they are, exactly
// as `CREReceiver.enrol` does on chain. A deployment that discovered
// bindings from caller input would let anyone have an assessment produced
// for an account they do not own.
type StaticBindings map[uint64]attest.Binding

func (b StaticBindings) Lookup(loanID uint64) (attest.Binding, bool) {
	binding, ok := b[loanID]
	return binding, ok
}
