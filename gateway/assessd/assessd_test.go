package assessd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/arcbjorn/credence/gateway/attest"
	"github.com/arcbjorn/credence/gateway/scheduler"
	"github.com/arcbjorn/credence/gateway/snapshot"
)

const monitoredLoan = 1

func testBinding() attest.Binding {
	return attest.Binding{
		IdentityRegistryChainID: big.NewInt(10143),
		IdentityRegistry:        attest.Address{0xAE},
		AgentID:                 big.NewInt(1830),
		Operator:                attest.Address{0xA9},
		CreditAccount:           attest.Address{0xAC},
		FacilityID:              big.NewInt(1),
		AccountStateVersion:     3,
		ConfigVersion:           1,
		ConfigHash:              attest.Hash{0x11},
		PolicyEpoch:             1,
		PolicyHash:              attest.Hash{0x22},
	}
}

type stubEvidence struct {
	input snapshot.Input
	err   error
}

func (s stubEvidence) Collect(
	_ context.Context, _ scheduler.Task, _ int64,
) (snapshot.Input, error) {
	return s.input, s.err
}

type stubEngine struct {
	out json.RawMessage
	err error
	got json.RawMessage
}

func (s *stubEngine) Assess(
	_ context.Context, input, _ json.RawMessage,
) (json.RawMessage, error) {
	s.got = input
	return s.out, s.err
}

type stubNorm struct {
	env attest.Assessment
	err error
}

func (s stubNorm) Envelope(
	_ attest.Binding, _ json.RawMessage,
) (attest.Assessment, error) {
	return s.env, s.err
}

type stubRetainer struct {
	calls int
	err   error
}

func (s *stubRetainer) RetainDecision(
	_ context.Context, _, _ string, _, _ []byte,
) error {
	s.calls++
	return s.err
}

// A monitoring snapshot the builder's own Validate accepts: real evidence
// ids, a non-empty validity window, and an assessment time inside it.
// Building a thinner one would test a snapshot the gateway would never
// send.
func monitoringInput() snapshot.Input {
	const now = 1_000_000
	ev := func(id string) snapshot.Evidence {
		e, err := snapshot.Present(
			id, "test", snapshot.OriginSynthetic, snapshot.Required,
			now, now, map[string]any{"v": 1})
		if err != nil {
			panic(err)
		}
		return e
	}
	return snapshot.Input{
		SchemaVersion:  "1",
		FacilityPolicy: "Scenario",
		RequestedClass: "PrototypeEligible",
		AssessmentTime: now,
		ScenarioID:     ptr("scenario-1"),
		Proposal: snapshot.Proposal{
			ValidAfter: now - 1,
			ValidUntil: now + 3600,
		},
		Context:   json.RawMessage(`{"kind":"Monitoring","loan":{"loan_id":"1"}}`),
		Account:   ev("account"),
		Market:    ev("market"),
		History:   ev("history"),
		Forecasts: ev("forecasts"),
		Portfolio: ev("portfolio"),
	}
}

func ptr(s string) *string { return &s }

func allowedEnvelope() attest.Assessment {
	return attest.Assessment{
		Decision:       attest.DecisionApproved,
		Permission:     attest.PermissionAllowed,
		Class:          attest.ClassPrototypeEligible,
		ModelHash:      attest.Hash{0x33},
		InputHash:      attest.Hash{0x44},
		EvidenceRoot:   attest.Hash{0x55},
		AssessmentHash: attest.Hash{0x66},
	}
}

func newServer(t *testing.T, opts ...func(*deps)) (*Server, *deps) {
	t.Helper()
	d := &deps{
		evidence: stubEvidence{input: monitoringInput()},
		engine:   &stubEngine{out: json.RawMessage(`{"assessment_id":"0x01"}`)},
		norm:     stubNorm{env: allowedEnvelope()},
		bindings: StaticBindings{monitoredLoan: testBinding()},
		retainer: &stubRetainer{},
	}
	for _, o := range opts {
		o(d)
	}
	s := New(d.evidence, d.engine, d.norm, d.bindings, d.retainer, Config{
		Policy: json.RawMessage(`{}`),
	})
	s.now = func() int64 { return 1_000_000 }
	return s, d
}

type deps struct {
	evidence EvidenceSource
	engine   Engine
	norm     Normalizer
	bindings Bindings
	retainer Retainer
}

func TestAssessReturnsTheNormalizedAssessment(t *testing.T) {
	s, _ := newServer(t)

	resp, status, err := s.Assess(context.Background(), Request{LoanID: monitoredLoan})
	if err != nil {
		t.Fatalf("status %d: %v", status, err)
	}
	if resp.Decision != "Approved" || resp.Permission != "Allowed" {
		t.Fatalf("got %s/%s, want Approved/Allowed", resp.Decision, resp.Permission)
	}
	if resp.Class != "PrototypeEligible" {
		t.Fatalf("class = %s", resp.Class)
	}
	// The workflow parses these as 32-byte hashes; a bare or short
	// spelling would be refused there.
	for name, h := range map[string]string{
		"model":      resp.ModelHash,
		"input":      resp.InputHash,
		"evidence":   resp.EvidenceRoot,
		"assessment": resp.AssessmentHash,
	} {
		if len(h) != 66 || !strings.HasPrefix(h, "0x") {
			t.Errorf("%s hash %q is not an 0x-prefixed 32-byte hash", name, h)
		}
	}
}

// The state version comes from the BINDING this deployment holds, not from
// the caller. `riskAllowedFor` compares it against the account's current
// version, so a caller who could set it would grant permission against a
// portfolio nobody assessed.
func TestStateVersionComesFromTheBinding(t *testing.T) {
	s, _ := newServer(t)
	resp, _, err := s.Assess(context.Background(), Request{LoanID: monitoredLoan})
	if err != nil {
		t.Fatal(err)
	}
	if resp.AccountStateVersion != testBinding().AccountStateVersion {
		t.Fatalf("state version = %d, want %d",
			resp.AccountStateVersion, testBinding().AccountStateVersion)
	}
}

// A caller naming a loan this deployment does not monitor gets a refusal,
// not an assessment. Otherwise anyone could have one produced for an
// account they do not own.
func TestUnknownLoanIsRefused(t *testing.T) {
	s, _ := newServer(t)
	_, status, err := s.Assess(context.Background(), Request{LoanID: 99})
	if err == nil {
		t.Fatal("expected a refusal")
	}
	if status != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", status)
	}
}

// Every failure returns an error rather than a Blocked assessment. The
// caller's correct response is to publish nothing, which leaves permission
// to expire; returning Blocked would assert a judgment the engine never
// made.
func TestFailuresReturnErrorsNotBlockedAssessments(t *testing.T) {
	for _, c := range []struct {
		name string
		opt  func(*deps)
		want int
	}{
		{
			"evidence unavailable",
			func(d *deps) {
				d.evidence = stubEvidence{err: errors.New("provider down")}
			},
			http.StatusServiceUnavailable,
		},
		{
			"engine unreachable",
			func(d *deps) {
				d.engine = &stubEngine{err: errors.New("connection refused")}
			},
			http.StatusServiceUnavailable,
		},
		{
			"normalization failed",
			func(d *deps) {
				d.norm = stubNorm{err: errors.New("policy divergence")}
			},
			http.StatusInternalServerError,
		},
		{
			"retention failed",
			func(d *deps) {
				d.retainer = &stubRetainer{err: errors.New("disk full")}
			},
			http.StatusInternalServerError,
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			s, _ := newServer(t, c.opt)
			resp, status, err := s.Assess(
				context.Background(), Request{LoanID: monitoredLoan})
			if err == nil {
				t.Fatal("expected an error, got an assessment")
			}
			if resp != nil {
				t.Fatal("a failed assessment must return no body")
			}
			if status != c.want {
				t.Fatalf("status = %d, want %d", status, c.want)
			}
		})
	}
}

// `docs/04` requires a decision be replayable whether or not it reached
// chain, so the snapshot is retained BEFORE the answer is returned.
func TestDecisionIsRetainedBeforeReturning(t *testing.T) {
	s, d := newServer(t)
	if _, _, err := s.Assess(
		context.Background(), Request{LoanID: monitoredLoan}); err != nil {
		t.Fatal(err)
	}
	if got := d.retainer.(*stubRetainer).calls; got != 1 {
		t.Fatalf("retained %d times, want 1", got)
	}
}

// Monitoring evaluates an existing loan and can never fund one. A builder
// bug that produced an origination context here would turn a health check
// into a draw, so the snapshot is checked before it reaches the engine.
func TestOriginationContextIsRefused(t *testing.T) {
	in := snapshot.Input{}
	in.Context = json.RawMessage(`{"kind":"Origination","request":{"request_id":"r1"}}`)

	s, d := newServer(t, func(d *deps) {
		d.evidence = stubEvidence{input: in}
	})
	_, status, err := s.Assess(context.Background(), Request{LoanID: monitoredLoan})
	if err == nil {
		t.Fatal("an origination context must not be assessed here")
	}
	if status != http.StatusInternalServerError {
		t.Fatalf("status = %d", status)
	}
	if e, ok := d.engine.(*stubEngine); ok && e.got != nil {
		t.Fatal("the engine was called with an origination snapshot")
	}
}

func TestMissingContextIsRefused(t *testing.T) {
	s, _ := newServer(t, func(d *deps) {
		d.evidence = stubEvidence{input: snapshot.Input{}}
	})
	if _, _, err := s.Assess(
		context.Background(), Request{LoanID: monitoredLoan}); err == nil {
		t.Fatal("a snapshot with no context must be refused")
	}
}

// --- HTTP surface ---

func TestHTTPRejectsMalformedRequests(t *testing.T) {
	s, _ := newServer(t)
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()

	for _, c := range []struct {
		name, body string
		want       int
	}{
		{"empty", "", http.StatusBadRequest},
		{"not json", "{", http.StatusBadRequest},
		{"no loan id", `{}`, http.StatusBadRequest},
		{"zero loan id", `{"loan_id":0}`, http.StatusBadRequest},
		// A caller asking for something this build does not implement
		// should be told, not silently given a different answer.
		{"unknown field", `{"loan_id":1,"binding":"mine"}`, http.StatusBadRequest},
		{"unknown loan", `{"loan_id":42}`, http.StatusNotFound},
	} {
		t.Run(c.name, func(t *testing.T) {
			resp, err := http.Post(
				srv.URL+"/v1/assess", "application/json", strings.NewReader(c.body))
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != c.want {
				t.Fatalf("status = %d, want %d", resp.StatusCode, c.want)
			}
		})
	}
}

// A caller cannot supply its own binding: the request has one field, and
// anything else is refused. This is the property that stops a caller
// steering an assessment onto an account it does not own.
func TestCallerCannotSupplyABinding(t *testing.T) {
	s, _ := newServer(t)
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()

	body := `{"loan_id":1,"account_state_version":999}`
	resp, err := http.Post(
		srv.URL+"/v1/assess", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 for an unknown field", resp.StatusCode)
	}
}

func TestHTTPHappyPath(t *testing.T) {
	s, _ := newServer(t)
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/v1/assess", "application/json",
		strings.NewReader(fmt.Sprintf(`{"loan_id":%d}`, monitoredLoan)))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}

	var out Response
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if out.Decision != "Approved" {
		t.Fatalf("decision = %q", out.Decision)
	}
	if out.LoanID != monitoredLoan {
		t.Fatalf("loan id = %d", out.LoanID)
	}
}

func TestHealth(t *testing.T) {
	s, _ := newServer(t)
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/health")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
}

func TestGetOnAssessIsRefused(t *testing.T) {
	s, _ := newServer(t)
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/v1/assess")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", resp.StatusCode)
	}
}

// The engine's vocabulary is not the chain's: it says MonitoringAllowed,
// the chain says Approved plus Allowed. Rendering the ENVELOPE rather than
// the engine's own string is what keeps the workflow reading the values
// the receiver encodes.
func TestUnknownEnvelopeValuesAreRefused(t *testing.T) {
	s, _ := newServer(t, func(d *deps) {
		env := allowedEnvelope()
		env.Decision = 99
		d.norm = stubNorm{env: env}
	})
	if _, _, err := s.Assess(
		context.Background(), Request{LoanID: monitoredLoan}); err == nil {
		t.Fatal("an unrecognised decision must be refused")
	}

	s2, _ := newServer(t, func(d *deps) {
		env := allowedEnvelope()
		env.Class = 99
		d.norm = stubNorm{env: env}
	})
	if _, _, err := s2.Assess(
		context.Background(), Request{LoanID: monitoredLoan}); err == nil {
		t.Fatal("an unrecognised class must be refused")
	}
}

// A context labelled Monitoring that still carries a borrower request is
// an origination wearing the wrong label -- the case where a health check
// would become a draw.
func TestMonitoringContextCarryingARequestIsRefused(t *testing.T) {
	in := monitoringInput()
	in.Context = json.RawMessage(
		`{"kind":"Monitoring","loan":{"loan_id":"1"},"request":{"principal":"1"}}`)

	s, d := newServer(t, func(d *deps) {
		d.evidence = stubEvidence{input: in}
	})
	if _, _, err := s.Assess(
		context.Background(), Request{LoanID: monitoredLoan}); err == nil {
		t.Fatal("a monitoring context carrying a request must be refused")
	}
	if e, ok := d.engine.(*stubEngine); ok && e.got != nil {
		t.Fatal("the engine was called with a request-bearing context")
	}
}
