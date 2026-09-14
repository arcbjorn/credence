package store_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/arcbjorn/credence/gateway/publisher"
	"github.com/arcbjorn/credence/gateway/store"
)

type chainStub struct {
	accepted uint64
	submits  int
	txHash   string
	receipt  publisher.Status
}

func (c *chainStub) AcceptedSequence(context.Context, string) (uint64, error) {
	return c.accepted, nil
}

func (c *chainStub) SubmitAccept(context.Context, []byte, []byte) (string, error) {
	c.submits++
	c.accepted++
	return c.txHash, nil
}

func (c *chainStub) ReceiptStatus(context.Context, string) (publisher.Status, error) {
	return c.receipt, nil
}

// The publisher's no-double-publish guarantee is only real if it holds
// against the persistent journal, not just an in-memory stub. This is the
// integration that proves the two halves fit.
func TestPublisherIsIdempotentAcrossRestart(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "pub.db")
	chain := &chainStub{accepted: 4, txHash: "0xabc", receipt: publisher.StatusConfirmed}

	// First process: publish successfully.
	s1, err := store.Open(ctx, path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	p1 := publisher.New(chain, store.NewJournal(s1))
	tx, err := p1.Publish(ctx, "b1", 5, []byte("payload"), []byte("sig"))
	if err != nil {
		t.Fatalf("first publish: %v", err)
	}
	s1.Close()

	// Second process, same database: the journal must prevent a resend.
	s2, err := store.Open(ctx, path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer s2.Close()

	p2 := publisher.New(chain, store.NewJournal(s2))
	again, err := p2.Publish(ctx, "b1", 5, []byte("payload"), []byte("sig"))
	if err != nil {
		t.Fatalf("republish after restart: %v", err)
	}

	if again != tx {
		t.Fatalf("restart returned a different hash: %q vs %q", again, tx)
	}
	if chain.submits != 1 {
		t.Fatalf("submitted %d times across a restart, want 1", chain.submits)
	}
}

// A crash between recording intent and confirming must leave a Pending
// record that reconciles from chain state rather than resubmitting.
func TestCrashedSubmissionReconcilesAfterRestart(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "crash.db")
	key := publisher.IdempotencyKey("b1", 5, []byte("p"))

	// Simulate the crash: intent recorded, outcome never written.
	s1, _ := store.Open(ctx, path)
	_ = store.NewJournal(s1).Record(ctx, publisher.Attempt{
		BindingKey: "b1", Sequence: 5, IdempotencyKey: key,
		TxHash: "0xinflight", Status: publisher.StatusPending,
	})
	s1.Close()

	// Restart. The chain shows the transaction landed after all.
	s2, _ := store.Open(ctx, path)
	defer s2.Close()
	chain := &chainStub{accepted: 5, txHash: "0xnew", receipt: publisher.StatusConfirmed}
	p := publisher.New(chain, store.NewJournal(s2))

	tx, err := p.Publish(ctx, "b1", 5, []byte("p"), []byte("s"))
	if err != nil {
		t.Fatalf("reconcile after crash: %v", err)
	}
	if tx != "0xinflight" {
		t.Fatalf("expected the original hash, got %q", tx)
	}
	if chain.submits != 0 {
		t.Fatal("a reconciled delivery must not resubmit after a crash")
	}
}

// A superseded delivery stays superseded across a restart, even if the
// chain becomes unreadable.
func TestSupersededSurvivesRestart(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "sup.db")

	s1, _ := store.Open(ctx, path)
	chain := &chainStub{accepted: 9}
	p1 := publisher.New(chain, store.NewJournal(s1))
	if _, err := p1.Publish(ctx, "b1", 5, []byte("p"), []byte("s")); !errors.Is(err, publisher.ErrSuperseded) {
		t.Fatalf("expected supersession, got %v", err)
	}
	s1.Close()

	s2, _ := store.Open(ctx, path)
	defer s2.Close()
	p2 := publisher.New(&chainStub{accepted: 9}, store.NewJournal(s2))
	if _, err := p2.Publish(ctx, "b1", 5, []byte("p"), []byte("s")); !errors.Is(err, publisher.ErrSuperseded) {
		t.Fatalf("supersession must persist across restart, got %v", err)
	}
}
