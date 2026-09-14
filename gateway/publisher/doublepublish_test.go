package publisher_test

import (
	"errors"
	"testing"

	"github.com/arcbjorn/credence/gateway/publisher"
)

// The guarantee worth proving exhaustively: across every combination of
// crash points and chain conditions, a confirmed assessment is never
// submitted twice. Enumerating this is more convincing than the handful of
// scenarios I happened to imagine.
func TestNoDoublePublishAcrossFailureModes(t *testing.T) {
	type scenario struct {
		name        string
		priorStatus publisher.Status
		priorTx     string
		accepted    uint64 // chain state before the retry
		receipt     publisher.Status
		wantSubmits int
	}

	const seq = 5
	scenarios := []scenario{
		{"confirmed in journal", publisher.StatusConfirmed, "0xold", 5, publisher.StatusConfirmed, 0},
		{"pending, chain accepted", publisher.StatusPending, "0xold", 5, publisher.StatusConfirmed, 0},
		{"pending, tx unmined", publisher.StatusPending, "0xold", 4, publisher.StatusPending, 0},
		{"pending, tx reverted", publisher.StatusPending, "0xold", 4, publisher.StatusFailed, 0},
		{"pending, no tx hash", publisher.StatusPending, "", 4, publisher.StatusFailed, 0},
		{"superseded", publisher.StatusSuperseded, "", 9, publisher.StatusConfirmed, 0},
		{"failed, chain moved on", publisher.StatusFailed, "0xold", 9, publisher.StatusConfirmed, 0},
	}

	for _, s := range scenarios {
		t.Run(s.name, func(t *testing.T) {
			j := newJournal()
			key := publisher.IdempotencyKey("b1", seq, []byte("p"))
			j.entries[key] = publisher.Attempt{
				BindingKey: "b1", Sequence: seq, IdempotencyKey: key,
				TxHash: s.priorTx, Status: s.priorStatus,
			}
			chain := &fakeChain{
				accepted: s.accepted,
				txHash:   "0xnew",
				receipt:  s.receipt,
			}
			p := publisher.New(chain, j)

			_, _ = p.Publish(ctx(), "b1", seq, []byte("p"), []byte("s"))

			if chain.submits != s.wantSubmits {
				t.Fatalf("submits = %d, want %d — a duplicate publish is possible here",
					chain.submits, s.wantSubmits)
			}
		})
	}
}

// Only one situation permits a resubmission: a prior attempt known not to
// have landed, with the chain still behind. Anything less certain must not
// resubmit.
func TestResubmissionRequiresCertaintyOfFailure(t *testing.T) {
	j := newJournal()
	key := publisher.IdempotencyKey("b1", 5, []byte("p"))
	j.entries[key] = publisher.Attempt{
		BindingKey: "b1", Sequence: 5, IdempotencyKey: key,
		Status: publisher.StatusFailed,
	}
	chain := &fakeChain{accepted: 4, txHash: "0xnew", receipt: publisher.StatusConfirmed}
	p := publisher.New(chain, j)

	if _, err := p.Publish(ctx(), "b1", 5, []byte("p"), []byte("s")); err != nil {
		t.Fatalf("a known failure with the chain behind must be retryable: %v", err)
	}
	if chain.submits != 1 {
		t.Fatalf("expected exactly one resubmission, got %d", chain.submits)
	}
}

// A journal that loses its record must still not double-publish: the chain
// read catches it.
func TestLostJournalStillReconcilesFromChain(t *testing.T) {
	chain := &fakeChain{accepted: 5} // already accepted, journal empty
	p := publisher.New(chain, newJournal())

	_, err := p.Publish(ctx(), "b1", 5, []byte("p"), []byte("s"))
	if !errors.Is(err, publisher.ErrSuperseded) {
		t.Fatalf("chain state must catch a lost journal, got %v", err)
	}
	if chain.submits != 0 {
		t.Fatal("must not submit when the chain already accepted this sequence")
	}
}

// A different assessment at the same sequence is not a retry.
//
// An approval is submitted and lands. Meanwhile a denial is prepared at
// the same proposed sequence. Reconciling on binding and sequence alone
// reports the DENIAL as delivered — returning the approval's transaction
// hash — when nothing of the denial ever reached chain. The permission it
// was meant to withdraw would keep standing.
func TestDifferentDecisionAtOneSequenceIsNotDelivered(t *testing.T) {
	j := newJournal()

	approval := []byte(`{"decision":"MonitoringAllowed"}`)
	denial := []byte(`{"decision":"MonitoringBlocked"}`)

	// The approval was delivered at sequence 5.
	approvalKey := publisher.IdempotencyKey("b1", 5, approval)
	j.entries[approvalKey] = publisher.Attempt{
		BindingKey: "b1", Sequence: 5, IdempotencyKey: approvalKey,
		TxHash: "0xapproval", Status: publisher.StatusConfirmed,
	}

	// The chain has accepted sequence 5 — by the approval.
	chain := &fakeChain{accepted: 5, txHash: "0xdenial", receipt: publisher.StatusConfirmed}
	p := publisher.New(chain, j)

	tx, err := p.Publish(ctx(), "b1", 5, denial, []byte("s"))
	if err == nil {
		t.Fatalf("a denial that never reached chain must not report success (tx %s)", tx)
	}
	if tx == "0xapproval" {
		t.Fatal("the approval's transaction must not be reported as the denial's")
	}
	if !errors.Is(err, publisher.ErrSuperseded) {
		t.Fatalf("an occupied sequence must read as superseded, got %v", err)
	}
	if chain.submits != 0 {
		t.Fatal("and must not submit into an occupied sequence")
	}
}
