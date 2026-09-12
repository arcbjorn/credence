package publisher_test

import (
	"context"
	"errors"
	"testing"

	"github.com/arcbjorn/credence/gateway/publisher"
)

type fakeChain struct {
	accepted    uint64
	submitErr   error
	submits     int
	txHash      string
	receipt     publisher.Status
	receiptErr  error
	sequenceErr error
	// Set to simulate the chain accepting on submission.
	acceptOnSubmit bool
}

func (c *fakeChain) AcceptedSequence(context.Context, string) (uint64, error) {
	if c.sequenceErr != nil {
		return 0, c.sequenceErr
	}
	return c.accepted, nil
}

func (c *fakeChain) SubmitAccept(context.Context, []byte, []byte) (string, error) {
	c.submits++
	if c.submitErr != nil {
		return "", c.submitErr
	}
	if c.acceptOnSubmit {
		c.accepted++
	}
	return c.txHash, nil
}

func (c *fakeChain) ReceiptStatus(context.Context, string) (publisher.Status, error) {
	if c.receiptErr != nil {
		return "", c.receiptErr
	}
	return c.receipt, nil
}

type memJournal struct {
	entries   map[string]publisher.Attempt
	recordErr error
}

func newJournal() *memJournal {
	return &memJournal{entries: map[string]publisher.Attempt{}}
}

func (j *memJournal) Lookup(_ context.Context, key string) (*publisher.Attempt, bool, error) {
	a, ok := j.entries[key]
	if !ok {
		return nil, false, nil
	}
	return &a, true, nil
}

func (j *memJournal) Record(_ context.Context, a publisher.Attempt) error {
	if j.recordErr != nil {
		return j.recordErr
	}
	j.entries[a.IdempotencyKey] = a
	return nil
}

func ctx() context.Context { return context.Background() }

func TestConfirmedPublishReturnsHash(t *testing.T) {
	chain := &fakeChain{accepted: 4, txHash: "0xabc", receipt: publisher.StatusConfirmed,
		acceptOnSubmit: true}
	p := publisher.New(chain, newJournal())

	tx, err := p.Publish(ctx(), "b1", 5, []byte("payload"), []byte("sig"))
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	if tx != "0xabc" {
		t.Fatalf("tx = %q", tx)
	}
}

// The core idempotency guarantee: republishing a confirmed assessment must
// not send a second transaction.
func TestConfirmedPublishIsIdempotent(t *testing.T) {
	chain := &fakeChain{accepted: 4, txHash: "0xabc", receipt: publisher.StatusConfirmed,
		acceptOnSubmit: true}
	j := newJournal()
	p := publisher.New(chain, j)

	first, err := p.Publish(ctx(), "b1", 5, []byte("p"), []byte("s"))
	if err != nil {
		t.Fatalf("first publish: %v", err)
	}
	second, err := p.Publish(ctx(), "b1", 5, []byte("p"), []byte("s"))
	if err != nil {
		t.Fatalf("second publish: %v", err)
	}
	if first != second {
		t.Fatalf("hashes differ: %q vs %q", first, second)
	}
	if chain.submits != 1 {
		t.Fatalf("submitted %d times, want 1", chain.submits)
	}
}

// docs/04: reconcile against chain state before resubmitting. A crash
// between submission and journalling leaves the journal stale; the chain
// knows the truth.
func TestPendingReconcilesFromChainState(t *testing.T) {
	j := newJournal()
	key := publisher.IdempotencyKey("b1", 5)
	j.entries[key] = publisher.Attempt{
		BindingKey: "b1", Sequence: 5, IdempotencyKey: key,
		TxHash: "0xearlier", Status: publisher.StatusPending,
	}
	// The chain shows the sequence already accepted.
	chain := &fakeChain{accepted: 5}
	p := publisher.New(chain, j)

	tx, err := p.Publish(ctx(), "b1", 5, []byte("p"), []byte("s"))
	if err != nil {
		t.Fatalf("reconcile should succeed: %v", err)
	}
	if tx != "0xearlier" {
		t.Fatalf("expected the original hash, got %q", tx)
	}
	if chain.submits != 0 {
		t.Fatal("a reconciled delivery must not resubmit")
	}
	if j.entries[key].Status != publisher.StatusConfirmed {
		t.Fatalf("journal not updated: %s", j.entries[key].Status)
	}
}

// A pending attempt whose transaction is still unmined must not resubmit.
func TestPendingWithUnminedTxDoesNotResubmit(t *testing.T) {
	j := newJournal()
	key := publisher.IdempotencyKey("b1", 5)
	j.entries[key] = publisher.Attempt{
		BindingKey: "b1", Sequence: 5, IdempotencyKey: key,
		TxHash: "0xinflight", Status: publisher.StatusPending,
	}
	chain := &fakeChain{accepted: 4, receipt: publisher.StatusPending}
	p := publisher.New(chain, j)

	_, err := p.Publish(ctx(), "b1", 5, []byte("p"), []byte("s"))
	if !errors.Is(err, publisher.ErrPending) {
		t.Fatalf("expected ErrPending, got %v", err)
	}
	if chain.submits != 0 {
		t.Fatal("an in-flight transaction must not be resubmitted")
	}
}

// A newer sequence winning the race is expected, not an error condition to
// retry forever.
func TestSupersededSequenceIsNotRetried(t *testing.T) {
	chain := &fakeChain{accepted: 9}
	p := publisher.New(chain, newJournal())

	_, err := p.Publish(ctx(), "b1", 5, []byte("p"), []byte("s"))
	if !errors.Is(err, publisher.ErrSuperseded) {
		t.Fatalf("expected ErrSuperseded, got %v", err)
	}
	if chain.submits != 0 {
		t.Fatal("a superseded assessment must not be submitted")
	}
}

func TestSupersededIsRememberedAcrossCalls(t *testing.T) {
	chain := &fakeChain{accepted: 9}
	j := newJournal()
	p := publisher.New(chain, j)

	_, _ = p.Publish(ctx(), "b1", 5, []byte("p"), []byte("s"))
	chain.sequenceErr = errors.New("rpc down") // chain now unreachable

	_, err := p.Publish(ctx(), "b1", 5, []byte("p"), []byte("s"))
	if !errors.Is(err, publisher.ErrSuperseded) {
		t.Fatalf("journal must remember supersession, got %v", err)
	}
}

// An ambiguous send is Pending, never Failed. Recording it as failed is how
// a system publishes twice.
func TestSubmitErrorRecordsPendingNotFailed(t *testing.T) {
	chain := &fakeChain{accepted: 4, submitErr: errors.New("connection reset")}
	j := newJournal()
	p := publisher.New(chain, j)

	_, err := p.Publish(ctx(), "b1", 5, []byte("p"), []byte("s"))
	if err == nil {
		t.Fatal("expected an error")
	}
	got := j.entries[publisher.IdempotencyKey("b1", 5)]
	if got.Status != publisher.StatusPending {
		t.Fatalf("status = %s, want Pending — an ambiguous send is not a failure", got.Status)
	}
}

// The intent must be journalled before submission, so a crash mid-flight
// leaves a record to reconcile rather than silence.
func TestIntentIsRecordedBeforeSubmission(t *testing.T) {
	j := newJournal()
	chain := &fakeChain{accepted: 4, txHash: "0xabc", receipt: publisher.StatusConfirmed}
	p := publisher.New(chain, j)

	// A journal that refuses to record must abort before any submission.
	j.recordErr = errors.New("disk full")
	_, err := p.Publish(ctx(), "b1", 5, []byte("p"), []byte("s"))
	if err == nil {
		t.Fatal("expected the publish to abort")
	}
	if chain.submits != 0 {
		t.Fatal("must not submit when the intent cannot be recorded")
	}
}

// An unavailable receipt is ambiguous, so the result is Pending.
func TestUnknownReceiptIsPending(t *testing.T) {
	chain := &fakeChain{accepted: 4, txHash: "0xabc",
		receiptErr: errors.New("node timeout")}
	p := publisher.New(chain, newJournal())

	_, err := p.Publish(ctx(), "b1", 5, []byte("p"), []byte("s"))
	if !errors.Is(err, publisher.ErrPending) {
		t.Fatalf("expected ErrPending, got %v", err)
	}
}

func TestRevertedTransactionFails(t *testing.T) {
	chain := &fakeChain{accepted: 4, txHash: "0xbad", receipt: publisher.StatusFailed}
	p := publisher.New(chain, newJournal())

	if _, err := p.Publish(ctx(), "b1", 5, []byte("p"), []byte("s")); err == nil {
		t.Fatal("a reverted transaction must be reported as a failure")
	}
}

// A failed attempt is safe to retry, unlike a pending one.
func TestFailedAttemptMayBeRetried(t *testing.T) {
	j := newJournal()
	key := publisher.IdempotencyKey("b1", 5)
	j.entries[key] = publisher.Attempt{
		BindingKey: "b1", Sequence: 5, IdempotencyKey: key,
		Status: publisher.StatusFailed,
	}
	chain := &fakeChain{accepted: 4, txHash: "0xnew", receipt: publisher.StatusConfirmed}
	p := publisher.New(chain, j)

	tx, err := p.Publish(ctx(), "b1", 5, []byte("p"), []byte("s"))
	if err != nil {
		t.Fatalf("retry should succeed: %v", err)
	}
	if tx != "0xnew" || chain.submits != 1 {
		t.Fatalf("expected one resubmission, got tx=%q submits=%d", tx, chain.submits)
	}
}

// An unreadable chain must not lead to a blind submission.
func TestUnreadableChainAbortsBeforeSubmitting(t *testing.T) {
	chain := &fakeChain{sequenceErr: errors.New("rpc down")}
	p := publisher.New(chain, newJournal())

	if _, err := p.Publish(ctx(), "b1", 5, []byte("p"), []byte("s")); err == nil {
		t.Fatal("expected an error")
	}
	if chain.submits != 0 {
		t.Fatal("must not submit without reconciling first")
	}
}

func TestIdempotencyKeyIsDerivedNotAllocated(t *testing.T) {
	a := publisher.IdempotencyKey("b1", 5)
	b := publisher.IdempotencyKey("b1", 5)
	if a != b {
		t.Fatal("the same binding and sequence must derive the same key")
	}
	if a == publisher.IdempotencyKey("b1", 6) {
		t.Fatal("different sequences must derive different keys")
	}
}
