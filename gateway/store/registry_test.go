package store_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/arcbjorn/credence/gateway/store"
)

func registry(t *testing.T) (*store.Registry, *store.Store) {
	t.Helper()
	s, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "r.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return store.NewRegistry(s), s
}

func seed(t *testing.T, r *store.Registry, bindingKey, account string) {
	t.Helper()
	ctx := context.Background()
	if err := r.RegisterAgent(ctx, store.Agent{
		AgentKey: "ag1", IdentityRegistryChainID: 10143,
		IdentityRegistry: "0xreg", AgentID: "184",
	}); err != nil {
		t.Fatalf("register agent: %v", err)
	}
	if err := r.RegisterBinding(ctx, store.Binding{
		BindingKey: bindingKey, AgentKey: "ag1",
		Operator: "0xop", CreditAccount: account, FacilityID: "1",
		ConfigVersion: 1, ConfigHash: "0xcfg", PolicyEpoch: 1, PolicyHash: "0xpol",
	}); err != nil {
		t.Fatalf("register binding: %v", err)
	}
}

func TestRegisterAndReadBinding(t *testing.T) {
	r, _ := registry(t)
	seed(t, r, "b1", "0xacct")

	got, err := r.Binding(context.Background(), "b1")
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if got.CreditAccount != "0xacct" || got.FacilityID != "1" {
		t.Fatalf("round trip lost data: %+v", got)
	}
}

func TestUnknownBindingIsNotFound(t *testing.T) {
	r, _ := registry(t)
	_, err := r.Binding(context.Background(), "absent")
	if !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

// Registration is idempotent, so replaying it is safe.
func TestRegistrationIsIdempotent(t *testing.T) {
	r, _ := registry(t)
	seed(t, r, "b1", "0xacct")
	seed(t, r, "b1", "0xupdated")

	got, _ := r.Binding(context.Background(), "b1")
	if got.CreditAccount != "0xupdated" {
		t.Fatalf("re-registration must update: %s", got.CreditAccount)
	}
}

// The property that matters: a restart must reload every active watch, or
// monitoring stops silently while permission stands on chain.
func TestActiveWatchesSurviveRestart(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "restart.db")

	s1, err := store.Open(ctx, path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	r1 := store.NewRegistry(s1)
	if err := r1.RegisterAgent(ctx, store.Agent{
		AgentKey: "ag1", IdentityRegistryChainID: 10143,
		IdentityRegistry: "0xreg", AgentID: "184",
	}); err != nil {
		t.Fatalf("agent: %v", err)
	}
	if err := r1.RegisterBinding(ctx, store.Binding{
		BindingKey: "b1", AgentKey: "ag1", Operator: "0xop",
		CreditAccount: "0xacct", FacilityID: "1",
		ConfigVersion: 1, ConfigHash: "0xc", PolicyEpoch: 1, PolicyHash: "0xp",
	}); err != nil {
		t.Fatalf("binding: %v", err)
	}
	if err := r1.OpenLoan(ctx, "b1", "42", 3, 10_000); err != nil {
		t.Fatalf("open loan: %v", err)
	}
	s1.Close()

	s2, err := store.Open(ctx, path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer s2.Close()

	watches, err := store.NewRegistry(s2).ActiveWatches(ctx)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if len(watches) != 1 {
		t.Fatalf("expected 1 watch after restart, got %d", len(watches))
	}
	w := watches[0]
	if w.BindingKey != "b1" || w.LoanID != "42" {
		t.Fatalf("watch corrupted: %+v", w)
	}
	if w.CreditAccount != "0xacct" {
		t.Fatalf("credit account not rejoined: %q", w.CreditAccount)
	}
	if w.AccountStateVersion != 3 {
		t.Fatalf("state version lost: %d", w.AccountStateVersion)
	}
}

// A closed loan must not be reloaded, or a repaid loan would be monitored
// forever.
func TestClosedLoanIsNotReloaded(t *testing.T) {
	r, _ := registry(t)
	ctx := context.Background()
	seed(t, r, "b1", "0xacct")

	_ = r.OpenLoan(ctx, "b1", "42", 3, 10_000)
	if watches, _ := r.ActiveWatches(ctx); len(watches) != 1 {
		t.Fatalf("expected 1 active watch, got %d", len(watches))
	}

	if err := r.CloseLoan(ctx, "b1"); err != nil {
		t.Fatalf("close: %v", err)
	}
	watches, err := r.ActiveWatches(ctx)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if len(watches) != 0 {
		t.Fatalf("a closed loan must not be monitored, got %d", len(watches))
	}
}

// Closing is a mark, not a delete: the history of what was watched must
// survive (`docs/04`).
func TestClosedLoanHistoryIsRetained(t *testing.T) {
	r, s := registry(t)
	ctx := context.Background()
	seed(t, r, "b1", "0xacct")
	_ = r.OpenLoan(ctx, "b1", "42", 3, 10_000)
	_ = r.CloseLoan(ctx, "b1")

	var count int
	err := s.DB().QueryRowContext(ctx,
		`SELECT count(*) FROM chain_events WHERE event_name = 'ActiveLoan'`).Scan(&count)
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Fatalf("the record must be retained, found %d rows", count)
	}
}

// Reopening after a close resumes monitoring.
func TestReopenResumesMonitoring(t *testing.T) {
	r, _ := registry(t)
	ctx := context.Background()
	seed(t, r, "b1", "0xacct")

	_ = r.OpenLoan(ctx, "b1", "42", 3, 10_000)
	_ = r.CloseLoan(ctx, "b1")
	_ = r.OpenLoan(ctx, "b1", "43", 4, 20_000)

	watches, _ := r.ActiveWatches(ctx)
	if len(watches) != 1 || watches[0].LoanID != "43" {
		t.Fatalf("reopen must resume with the new loan: %+v", watches)
	}
}

// Several bindings reload in a deterministic order, so startup is
// reproducible.
func TestMultipleWatchesReloadDeterministically(t *testing.T) {
	r, _ := registry(t)
	ctx := context.Background()

	_ = r.RegisterAgent(ctx, store.Agent{AgentKey: "ag1", IdentityRegistry: "0xreg", AgentID: "1"})
	for _, key := range []string{"b3", "b1", "b2"} {
		_ = r.RegisterBinding(ctx, store.Binding{
			BindingKey: key, AgentKey: "ag1", Operator: "0xop",
			CreditAccount: "0xacct" + key, FacilityID: "1",
			ConfigHash: "0xc", PolicyHash: "0xp",
		})
		_ = r.OpenLoan(ctx, key, "loan-"+key, 1, 10_000)
	}

	first, err := r.ActiveWatches(ctx)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if len(first) != 3 {
		t.Fatalf("expected 3 watches, got %d", len(first))
	}
	if first[0].BindingKey != "b1" {
		t.Fatalf("order is not deterministic: %s first", first[0].BindingKey)
	}
	for i := 0; i < 3; i++ {
		again, _ := r.ActiveWatches(ctx)
		for j := range first {
			if first[j].BindingKey != again[j].BindingKey {
				t.Fatal("reload order must be stable")
			}
		}
	}
}
