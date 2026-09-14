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
	key := publisher.IdempotencyKey("b1", 5, []byte("p"))
	j.entries[key] = publisher.Attempt{
		BindingKey: "b1", Sequence: 5, IdempotencyKey: key,
		TxHash: "0xearlier", Status: publisher.StatusPending,
	}
	// The chain shows the sequence accepted AND our own transaction
	// confirmed. Both matter: the sequence alone says the slot is taken,
	// not that this payload took it.
	chain := &fakeChain{accepted: 5, receipt: publisher.StatusConfirmed}
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
	key := publisher.IdempotencyKey("b1", 5, []byte("p"))
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
	got := j.entries[publisher.IdempotencyKey("b1", 5, []byte("p"))]
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
	key := publisher.IdempotencyKey("b1", 5, []byte("p"))
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
	a := publisher.IdempotencyKey("b1", 5, []byte("p"))
	b := publisher.IdempotencyKey("b1", 5, []byte("p"))
	if a != b {
		t.Fatal("the same binding, sequence and payload must derive the same key")
	}
	if a == publisher.IdempotencyKey("b1", 6, []byte("p")) {
		t.Fatal("different sequences must derive different keys")
	}
	// The payload is part of the identity. Two assessments can propose
	// the same sequence -- an approval in flight and a denial prepared
	// behind it -- and treating them as one delivery reports the denial
	// as landed when only the approval did.
	if a == publisher.IdempotencyKey("b1", 5, []byte("different")) {
		t.Fatal("different payloads at one sequence must derive different keys")
	}
}

// A pending denial must not be confirmed by a DIFFERENT payload winning
// the same sequence.
//
// The oracle enforces a strictly increasing sequence, so exactly one
// assessment can ever land at a given number. When an approval and a
// denial are both prepared at that number and the approval wins,
// reconciling the denial on `accepted >= sequence` alone reports it as
// delivered -- recording a withdrawal of permission that never reached
// chain, which is the one direction that must never fail silently.
//
// Reported by review, 14 September.
func TestPendingAttemptIsNotConfirmedByAnotherPayload(t *testing.T) {
	chain := &fakeChain{
		// The approval already landed at this sequence.
		accepted: 5,
		// The denial's own transaction reverted.
		receipt: publisher.StatusFailed,
		txHash:  "0xdenial",
	}
	journal := newJournal()
	p := publisher.New(chain, journal)

	denial := []byte("denial-calldata")
	key := publisher.IdempotencyKey("0xbinding", 5, denial)
	journal.entries[key] = publisher.Attempt{
		BindingKey:     "0xbinding",
		Sequence:       5,
		IdempotencyKey: key,
		TxHash:         "0xdenial",
		Status:         publisher.StatusPending,
	}

	_, err := p.Publish(
		context.Background(), "0xbinding", 5, denial, []byte("sig"))
	if err == nil {
		t.Fatal("a denial whose transaction failed must not report success " +
			"because a different assessment won the same sequence")
	}

	got := journal.entries[key]
	if got.Status == publisher.StatusConfirmed {
		t.Fatalf("denial recorded as %s; it never reached chain", got.Status)
	}
}

// The ordinary reconciliation still works: our OWN transaction succeeded,
// and the chain reflects it.
func TestPendingAttemptConfirmsOnItsOwnReceipt(t *testing.T) {
	chain := &fakeChain{
		accepted: 5,
		receipt:  publisher.StatusConfirmed,
		txHash:   "0xmine",
	}
	journal := newJournal()
	p := publisher.New(chain, journal)

	payload := []byte("approval-calldata")
	key := publisher.IdempotencyKey("0xbinding", 5, payload)
	journal.entries[key] = publisher.Attempt{
		BindingKey:     "0xbinding",
		Sequence:       5,
		IdempotencyKey: key,
		TxHash:         "0xmine",
		Status:         publisher.StatusPending,
	}

	txHash, err := p.Publish(
		context.Background(), "0xbinding", 5, payload, []byte("sig"))
	if err != nil {
		t.Fatalf("our own confirmed transaction must reconcile: %v", err)
	}
	if txHash != "0xmine" {
		t.Fatalf("tx = %q, want 0xmine", txHash)
	}
	if chain.submits != 0 {
		t.Fatal("a confirmed attempt must not be resubmitted")
	}
}
