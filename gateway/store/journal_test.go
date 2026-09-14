package store_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/arcbjorn/credence/gateway/publisher"
	"github.com/arcbjorn/credence/gateway/store"
)

func journal(t *testing.T) (*store.Journal, *store.Store) {
	t.Helper()
	s, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "j.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return store.NewJournal(s), s
}

func TestRecordAndLookup(t *testing.T) {
	j, _ := journal(t)
	ctx := context.Background()

	a := publisher.Attempt{
		BindingKey: "b1", Sequence: 5,
		IdempotencyKey: publisher.IdempotencyKey("b1", 5, []byte("p")),
		TxHash:         "0xabc", Status: publisher.StatusConfirmed,
	}
	if err := j.Record(ctx, a); err != nil {
		t.Fatalf("record: %v", err)
	}

	got, found, err := j.Lookup(ctx, a.IdempotencyKey)
	if err != nil || !found {
		t.Fatalf("lookup: found=%v err=%v", found, err)
	}
	if got.Status != publisher.StatusConfirmed || got.TxHash != "0xabc" || got.Sequence != 5 {
		t.Fatalf("round trip lost data: %+v", got)
	}
}

func TestLookupMissingKeyIsNotAnError(t *testing.T) {
	j, _ := journal(t)
	_, found, err := j.Lookup(context.Background(), "absent")
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	if found {
		t.Fatal("an absent key must not report found")
	}
}

// The publisher records Pending before submitting, then updates. The update
// must not lose the transaction hash discovered in between.
func TestUpdatePreservesTxHash(t *testing.T) {
	j, _ := journal(t)
	ctx := context.Background()
	key := publisher.IdempotencyKey("b1", 5, []byte("p"))

	_ = j.Record(ctx, publisher.Attempt{
		BindingKey: "b1", Sequence: 5, IdempotencyKey: key,
		TxHash: "0xfirst", Status: publisher.StatusPending,
	})
	// A later write with no hash — for example a reconciliation — must not
	// erase what was already known.
	_ = j.Record(ctx, publisher.Attempt{
		BindingKey: "b1", Sequence: 5, IdempotencyKey: key,
		Status: publisher.StatusConfirmed, Detail: "reconciled",
	})

	got, _, _ := j.Lookup(ctx, key)
	if got.TxHash != "0xfirst" {
		t.Fatalf("tx hash lost on update: %q", got.TxHash)
	}
	if got.Status != publisher.StatusConfirmed {
		t.Fatalf("status not updated: %s", got.Status)
	}
}

// Superseded must be storable; the publisher relies on remembering it.
func TestSupersededStatusIsAccepted(t *testing.T) {
	j, _ := journal(t)
	err := j.Record(context.Background(), publisher.Attempt{
		BindingKey: "b1", Sequence: 5,
		IdempotencyKey: publisher.IdempotencyKey("b1", 5, []byte("p")),
		Status:         publisher.StatusSuperseded,
	})
	if err != nil {
		t.Fatalf("superseded must be a valid status: %v", err)
	}
}

// A binding not yet registered must not block publication. Refusing here
// would make the journal a gate on delivery rather than a record of it.
func TestUnregisteredBindingStillRecords(t *testing.T) {
	j, _ := journal(t)
	err := j.Record(context.Background(), publisher.Attempt{
		BindingKey: "never-registered", Sequence: 1,
		IdempotencyKey: "never-registered:1",
		Status:         publisher.StatusPending,
	})
	if err != nil {
		t.Fatalf("an unregistered binding must still record: %v", err)
	}
}

// The guarantee that matters: a Pending record survives a restart, so a
// restarted process reconciles instead of resubmitting blindly.
func TestPendingSurvivesReopen(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "restart.db")

	s1, err := store.Open(ctx, path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	key := publisher.IdempotencyKey("b1", 7, []byte("p"))
	_ = store.NewJournal(s1).Record(ctx, publisher.Attempt{
		BindingKey: "b1", Sequence: 7, IdempotencyKey: key,
		TxHash: "0xinflight", Status: publisher.StatusPending,
	})
	s1.Close()

	s2, err := store.Open(ctx, path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer s2.Close()

	got, found, err := store.NewJournal(s2).Lookup(ctx, key)
	if err != nil || !found {
		t.Fatalf("pending record lost across restart: found=%v err=%v", found, err)
	}
	if got.Status != publisher.StatusPending || got.TxHash != "0xinflight" {
		t.Fatalf("record corrupted across restart: %+v", got)
	}
}

// A restarted process must be able to find everything needing reconciliation.
func TestPendingAttemptsListsOnlyPending(t *testing.T) {
	j, _ := journal(t)
	ctx := context.Background()

	for _, a := range []publisher.Attempt{
		{BindingKey: "b1", Sequence: 1, IdempotencyKey: "b1:1", Status: publisher.StatusPending},
		{BindingKey: "b2", Sequence: 1, IdempotencyKey: "b2:1", Status: publisher.StatusConfirmed},
		{BindingKey: "b3", Sequence: 1, IdempotencyKey: "b3:1", Status: publisher.StatusPending},
		{BindingKey: "b4", Sequence: 1, IdempotencyKey: "b4:1", Status: publisher.StatusSuperseded},
	} {
		if err := j.Record(ctx, a); err != nil {
			t.Fatalf("record: %v", err)
		}
	}

	pending, err := j.PendingAttempts(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(pending) != 2 {
		t.Fatalf("expected 2 pending, got %d", len(pending))
	}
}

// Attempt counts accumulate, so repeated failures are visible in the data.
func TestAttemptsAccumulate(t *testing.T) {
	j, s := journal(t)
	ctx := context.Background()
	key := publisher.IdempotencyKey("b1", 5, []byte("p"))

	for i := 0; i < 3; i++ {
		_ = j.Record(ctx, publisher.Attempt{
			BindingKey: "b1", Sequence: 5, IdempotencyKey: key,
			Status: publisher.StatusPending,
		})
	}

	var attempts int
	err := s.DB().QueryRowContext(ctx,
		`SELECT attempts FROM transactions WHERE idempotency_key = ?`, key).Scan(&attempts)
	if err != nil {
		t.Fatalf("read attempts: %v", err)
	}
	if attempts != 3 {
		t.Fatalf("attempts = %d, want 3", attempts)
	}
}
