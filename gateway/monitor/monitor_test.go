package monitor_test

import (
	"context"
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/arcbjorn/credence/gateway/monitor"
	"github.com/arcbjorn/credence/gateway/riskclient"
	"github.com/arcbjorn/credence/gateway/scheduler"
	"github.com/arcbjorn/credence/gateway/snapshot"
)

type fakeClock struct{ t int64 }

func (c *fakeClock) Now() int64 { return c.t }

// Minimal monitoring snapshot. Only the fields the loop inspects need to be
// realistic; the engine is stubbed in these tests.
func monitoringInput(loanID string) snapshot.Input {
	ctxRaw, _ := json.Marshal(map[string]any{
		"kind": "Monitoring",
		"loan": map[string]any{"loan_id": loanID},
	})
	ev := func(id string) snapshot.Evidence {
		e, _ := snapshot.Present(id, "test", snapshot.OriginObserved,
			snapshot.Required, 1000, 1000, map[string]any{"v": "1"})
		return e
	}
	return snapshot.Input{
		SchemaVersion:  "v1",
		FacilityPolicy: "Observed",
		RequestedClass: "PrototypeEligible",
		AssessmentTime: 1000,
		Proposal:       snapshot.Proposal{Sequence: 5, ValidAfter: 900, ValidUntil: 4600},
		Binding:        json.RawMessage(`{"facility_id":"1"}`),
		Context:        ctxRaw,
		Account:        ev("acct"),
		Market:         ev("mkt"),
		History:        ev("hist"),
		Forecasts:      ev("fc"),
		Portfolio:      ev("pf"),
		ModelPolicy:    json.RawMessage(`{"model_hash":"m","policy_hash":"p"}`),
	}
}

type stubEvidence struct {
	input snapshot.Input
	err   error
}

func (s stubEvidence) Collect(context.Context, scheduler.Task, int64) (snapshot.Input, error) {
	return s.input, s.err
}

type stubPublisher struct {
	tx    string
	err   error
	calls int
	// sawVersion records the account state version the monitor supplied,
	// so a test can assert it is the ASSESSED one rather than a fresh
	// chain read.
	sawVersion uint64
}

func (p *stubPublisher) Publish(
	_ context.Context, _ string, version uint64, _ json.RawMessage,
) (string, error) {
	p.calls++
	p.sawVersion = version
	return p.tx, p.err
}

type stubRecorder struct {
	cycles   []monitor.Cycle
	retained int
	accepted int
	// retainErr makes retention fail, so the test can check that an
	// unretainable decision blocks rather than publishing unrecorded.
	retainErr error
}

func (r *stubRecorder) RecordCycle(_ context.Context, c monitor.Cycle) error {
	r.cycles = append(r.cycles, c)
	return nil
}

func (r *stubRecorder) MarkAccepted(_ context.Context, _ string) error {
	r.accepted++
	return nil
}

func (r *stubRecorder) RetainDecision(
	_ context.Context,
	_, _ string,
	_, _ []byte,
) error {
	if r.retainErr != nil {
		return r.retainErr
	}
	r.retained++
	return nil
}

func engineReturning(t *testing.T, decision string) *riskclient.Client {
	t.Helper()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"assessment_id":"a1","input_hash":"","outcome":{"decision":"` +
			decision + `"}}`))
	}))
	t.Cleanup(s.Close)
	return riskclient.New(s.URL, 2*time.Second)
}

func setup(t *testing.T, ev monitor.EvidenceSource, risk *riskclient.Client,
	pub monitor.Publisher) (*monitor.Monitor, *stubRecorder) {
	t.Helper()
	clk := &fakeClock{t: 10_000}
	s := scheduler.New(scheduler.Policy{
		PriceMoveBps:          500,
		MaxEvidenceAgeSeconds: 300,
		RefreshLeadSeconds:    60,
		HeartbeatSeconds:      600,
	}, clk)
	s.Track(scheduler.Watch{
		BindingKey:         "b1",
		LoanID:             "loan-1",
		ReferencePrice:     big.NewInt(2_000_000000),
		PermissionIssuedAt: 10_000,
		EvidenceObservedAt: 10_000,
	})
	clk.t += 280 // force a staleness trigger

	rec := &stubRecorder{}
	return monitor.New(s, ev, risk, pub, rec, monitor.Config{
		Policy: json.RawMessage(`{}`),
	}), rec
}

func tick(m *monitor.Monitor) []monitor.Cycle {
	return m.Tick(context.Background(), map[string]*big.Int{}, 10_280)
}

// The acceptance case: no agent request, evidence changes, permission is
// refreshed and published.
func TestHealthyCycleAllowsAndPublishes(t *testing.T) {
	pub := &stubPublisher{tx: "0xdead"}
	m, rec := setup(t, stubEvidence{input: monitoringInput("loan-1")},
		engineReturning(t, "MonitoringAllowed"), pub)

	cycles := tick(m)
	if len(cycles) != 1 {
		t.Fatalf("expected 1 cycle, got %d", len(cycles))
	}
	c := cycles[0]
	if c.Permission != monitor.PermissionAllowed {
		t.Errorf("permission = %s, want Allowed (failure: %s)", c.Permission, c.Failure)
	}
	if c.TxHash != "0xdead" {
		t.Errorf("expected publication, got tx %q", c.TxHash)
	}
	if c.Trigger != scheduler.TriggerStaleness {
		t.Errorf("trigger = %s", c.Trigger)
	}
	if len(rec.cycles) != 1 {
		t.Error("every cycle must be recorded")
	}
}

// A blocked engine result must revoke permission, not merely log.
func TestBlockedResultRevokesPermission(t *testing.T) {
	pub := &stubPublisher{tx: "0xbeef"}
	m, _ := setup(t, stubEvidence{input: monitoringInput("loan-1")},
		engineReturning(t, "MonitoringBlocked"), pub)

	c := tick(m)[0]
	if c.Permission != monitor.PermissionBlocked {
		t.Fatalf("permission = %s, want Blocked", c.Permission)
	}
	// A revocation still has to reach the chain.
	if pub.calls != 1 {
		t.Fatal("a blocked result must still be published")
	}
}

// Fail-closed: an unreachable engine must not leave permission standing.
func TestUnreachableEngineBlocks(t *testing.T) {
	pub := &stubPublisher{tx: "0x1"}
	m, rec := setup(t, stubEvidence{input: monitoringInput("loan-1")},
		riskclient.New("http://127.0.0.1:1", 200*time.Millisecond), pub)

	c := tick(m)[0]
	if c.Permission != monitor.PermissionBlocked {
		t.Fatalf("permission = %s, want Blocked", c.Permission)
	}
	if c.Failure == "" {
		t.Error("the failure must be retained for diagnosis")
	}
	if pub.calls != 0 {
		t.Error("nothing should be published when no assessment exists")
	}
	if len(rec.cycles) != 1 {
		t.Error("a failed cycle is still a cycle and must be recorded")
	}
}

// Fail-closed: an evidence source that errors blocks rather than proceeding.
func TestEvidenceFailureBlocks(t *testing.T) {
	m, rec := setup(t, stubEvidence{err: errors.New("provider down")},
		engineReturning(t, "MonitoringAllowed"), &stubPublisher{})

	c := tick(m)[0]
	if c.Permission != monitor.PermissionBlocked {
		t.Fatalf("permission = %s, want Blocked", c.Permission)
	}
	if c.Failure == "" || len(rec.cycles) != 1 {
		t.Error("the failure must be recorded")
	}
}

// Fail-closed: a publish failure must not report permission as granted.
func TestPublishFailureBlocks(t *testing.T) {
	pub := &stubPublisher{err: errors.New("rpc down")}
	m, _ := setup(t, stubEvidence{input: monitoringInput("loan-1")},
		engineReturning(t, "MonitoringAllowed"), pub)

	c := tick(m)[0]
	if c.Permission != monitor.PermissionBlocked {
		t.Fatalf("undelivered permission must not read as Allowed, got %s", c.Permission)
	}
	if c.AssessmentID == "" {
		t.Error("the assessment still happened and must be retained")
	}
}

// The loop's defining constraint: monitoring can never fund a loan, so an
// origination context here is a bug that must be refused.
func TestOriginationContextIsRefused(t *testing.T) {
	in := monitoringInput("loan-1")
	in.Context = json.RawMessage(`{"kind":"Origination","request":{"request_id":"r1"}}`)

	m, _ := setup(t, stubEvidence{input: in}, engineReturning(t, "MonitoringAllowed"),
		&stubPublisher{tx: "0x1"})

	c := tick(m)[0]
	if c.Permission != monitor.PermissionBlocked {
		t.Fatal("an origination context must not be monitored")
	}
	if c.Failure == "" {
		t.Fatal("the refusal must be recorded")
	}
}

// A monitoring context that smuggles a borrower request is equally refused.
func TestMonitoringWithRequestIsRefused(t *testing.T) {
	in := monitoringInput("loan-1")
	in.Context = json.RawMessage(
		`{"kind":"Monitoring","loan":{"loan_id":"l1"},"request":{"request_id":"r1"}}`)

	m, _ := setup(t, stubEvidence{input: in}, engineReturning(t, "MonitoringAllowed"),
		&stubPublisher{tx: "0x1"})

	if c := tick(m)[0]; c.Permission != monitor.PermissionBlocked {
		t.Fatal("a monitoring context carrying a request must be refused")
	}
}

// A malformed snapshot must not reach the engine.
func TestInvalidSnapshotIsRefused(t *testing.T) {
	in := monitoringInput("loan-1")
	in.Market.EvidenceID = in.Account.EvidenceID // duplicate ids

	m, _ := setup(t, stubEvidence{input: in}, engineReturning(t, "MonitoringAllowed"),
		&stubPublisher{tx: "0x1"})

	c := tick(m)[0]
	if c.Permission != monitor.PermissionBlocked || c.Failure == "" {
		t.Fatal("an invalid snapshot must be refused before assessment")
	}
}

// A hash divergence means the gateway reshaped what it sent. It must be
// visible, not silently accepted.
func TestInputHashDivergenceIsRefused(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(
			`{"assessment_id":"a1","input_hash":"deadbeef","outcome":{"decision":"MonitoringAllowed"}}`))
	}))
	defer s.Close()

	m, _ := setup(t, stubEvidence{input: monitoringInput("loan-1")},
		riskclient.New(s.URL, 2*time.Second), &stubPublisher{tx: "0x1"})

	c := tick(m)[0]
	if c.Permission != monitor.PermissionBlocked {
		t.Fatal("a hash divergence must block")
	}
	if c.Failure == "" {
		t.Fatal("the divergence must be reported")
	}
}

// An unexpected decision string grants nothing. Only an explicit
// MonitoringAllowed permits risk.
func TestUnknownDecisionDoesNotGrant(t *testing.T) {
	m, _ := setup(t, stubEvidence{input: monitoringInput("loan-1")},
		engineReturning(t, "SomethingElse"), &stubPublisher{tx: "0x1"})

	if c := tick(m)[0]; c.Permission != monitor.PermissionBlocked {
		t.Fatal("only an explicit approval may grant permission")
	}
}

// A quiet market with fresh evidence produces no work.
func TestNothingDueProducesNoCycles(t *testing.T) {
	clk := &fakeClock{t: 10_000}
	s := scheduler.New(scheduler.Policy{
		PriceMoveBps: 500, MaxEvidenceAgeSeconds: 300,
		RefreshLeadSeconds: 60, HeartbeatSeconds: 600,
	}, clk)
	s.Track(scheduler.Watch{
		BindingKey: "b1", LoanID: "loan-1",
		ReferencePrice:     big.NewInt(2_000_000000),
		PermissionIssuedAt: 10_000, EvidenceObservedAt: 10_000,
	})
	clk.t += 30

	m := monitor.New(s, stubEvidence{input: monitoringInput("loan-1")},
		engineReturning(t, "MonitoringAllowed"), &stubPublisher{}, &stubRecorder{},
		monitor.Config{Policy: json.RawMessage(`{}`)})

	cycles := m.Tick(context.Background(),
		map[string]*big.Int{"b1": big.NewInt(2_000_000000)}, 10_030)
	if len(cycles) != 0 {
		t.Fatalf("expected no work, got %d cycles", len(cycles))
	}
}

// Every decision that reaches the engine must be retained, because
// `docs/04` requires replay from retained evidence and a published
// assessment with no stored snapshot cannot be replayed at all.
func TestDecisionIsRetained(t *testing.T) {
	m, rec := setup(t, stubEvidence{input: monitoringInput("loan-1")},
		engineReturning(t, "MonitoringAllowed"), &stubPublisher{tx: "0x1"})

	tick(m)
	if rec.retained != 1 {
		t.Fatalf("expected the decision to be retained, got %d", rec.retained)
	}
}

// A decision that cannot be retained must not be published. Publishing it
// would put an assessment on chain that no one can ever replay, which is
// precisely the claim the retained evidence exists to support.
func TestUnretainableDecisionIsNotPublished(t *testing.T) {
	pub := &stubPublisher{tx: "0x1"}
	clk := &fakeClock{t: 10_000}
	s := scheduler.New(scheduler.Policy{
		PriceMoveBps:          500,
		MaxEvidenceAgeSeconds: 300,
		RefreshLeadSeconds:    60,
		HeartbeatSeconds:      600,
	}, clk)
	s.Track(scheduler.Watch{
		BindingKey:         "b1",
		LoanID:             "loan-1",
		ReferencePrice:     big.NewInt(2_000_000000),
		PermissionIssuedAt: 10_000,
		EvidenceObservedAt: 10_000,
	})
	clk.t += 280

	rec := &stubRecorder{retainErr: errors.New("disk full")}
	m := monitor.New(s, stubEvidence{input: monitoringInput("loan-1")},
		engineReturning(t, "MonitoringAllowed"), pub, rec,
		monitor.Config{Policy: json.RawMessage(`{}`)})

	c := tick(m)[0]
	if pub.calls != 0 {
		t.Fatalf("an unretainable decision must not be published, got %d calls", pub.calls)
	}
	if c.Permission != monitor.PermissionBlocked {
		t.Fatalf("permission must fail closed, got %s", c.Permission)
	}
}

// The publisher must be given the version the ASSESSMENT was computed
// against, not a fresh read.
//
// `publisherFunc` used to pin a block and re-read the account at publish
// time. If the account executed between collect and publish, the signed
// envelope bound permission to a portfolio the snapshot never hashed --
// and `riskAllowedFor`, which compares the signed version against the
// account's current one, would then allow an action the engine never
// assessed. A later execution must INVALIDATE the permission, not
// silently relabel it.
//
// Reported by review, 14 September.
func TestPublisherGetsTheAssessedStateVersion(t *testing.T) {
	in := monitoringInput("loan-1")
	// The version the engine hashes into input_hash.
	in.Binding = json.RawMessage(`{"facility_id":"1","account_state_version":7}`)

	pub := &stubPublisher{tx: "0xdead"}
	m, _ := setup(t, stubEvidence{input: in},
		engineReturning(t, "MonitoringAllowed"), pub)

	tick(m)

	if pub.calls == 0 {
		t.Fatal("nothing was published, so nothing was tested")
	}
	if pub.sawVersion != 7 {
		t.Fatalf(
			"publisher was given version %d, want the assessed 7",
			pub.sawVersion)
	}
}
