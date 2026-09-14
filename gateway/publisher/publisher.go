// Package publisher delivers signed assessments to the RiskOracle.
//
// `docs/04`: request IDs and contract nonces give exactly-once economic
// effects; network delivery itself is not exactly once. So this package
// treats a submission as a claim that must be *reconciled*, never as a fact.
//
// The discipline that follows:
//
//   - Before resubmitting, read the chain. The oracle's accepted sequence
//     is authoritative; if it already reflects our assessment, the previous
//     attempt succeeded and a retry would be a duplicate.
//   - A submission whose outcome is unknown is recorded as Pending, not
//     assumed failed. Assuming failure is how a system publishes twice.
//   - The publisher never decides risk. It delivers what the engine
//     produced, or reports that it could not.
package publisher

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
)

// Status of one submission attempt.
type Status string

const (
	// StatusConfirmed: the chain reflects this assessment.
	StatusConfirmed Status = "Confirmed"
	// StatusPending: submitted, outcome unknown. Must be reconciled before
	// any resubmission.
	StatusPending Status = "Pending"
	// StatusFailed: the chain rejected it. Safe to abandon.
	StatusFailed Status = "Failed"
	// StatusSuperseded: a newer sequence was accepted first, so this
	// assessment can never be accepted. Not an error — expected whenever
	// evidence moves faster than delivery.
	StatusSuperseded Status = "Superseded"
)

// Chain is the read/write surface the publisher needs. Narrow on purpose:
// anything wider would let the publisher take actions that are not its job.
type Chain interface {
	// AcceptedSequence returns the highest sequence the oracle has accepted
	// for a binding. This is the reconciliation source of truth.
	AcceptedSequence(ctx context.Context, bindingKey string) (uint64, error)
	// SubmitAccept sends one accept() call and returns its transaction hash.
	SubmitAccept(ctx context.Context, payload []byte, signature []byte) (string, error)
	// ReceiptStatus reports whether a transaction succeeded, is unmined, or
	// reverted.
	ReceiptStatus(ctx context.Context, txHash string) (Status, error)
}

// Attempt records what was tried and what is known about it.
type Attempt struct {
	BindingKey     string
	Sequence       uint64
	IdempotencyKey string
	TxHash         string
	Status         Status
	Detail         string
}

// Journal persists attempts so a restart can reconcile rather than guess.
type Journal interface {
	// Lookup returns a prior attempt for this idempotency key, if any.
	Lookup(ctx context.Context, key string) (*Attempt, bool, error)
	Record(ctx context.Context, a Attempt) error
}

type Publisher struct {
	chain   Chain
	journal Journal
}

func New(chain Chain, journal Journal) *Publisher {
	return &Publisher{chain: chain, journal: journal}
}

var (
	// ErrSuperseded means a newer assessment won the race. The caller should
	// not retry this one.
	ErrSuperseded = errors.New("publisher: superseded by a newer sequence")
	// ErrPending means the outcome is unknown. The caller must not assume
	// failure and resubmit.
	ErrPending = errors.New("publisher: submission pending, outcome unknown")
)

// IdempotencyKey identifies one assessment's delivery uniquely.
//
// Derived from the binding and sequence rather than allocated, so a retry
// after a crash computes the same key without consulting anything.
func IdempotencyKey(bindingKey string, sequence uint64, payload []byte) string {
	// The PAYLOAD is part of the identity, not just the slot it occupies.
	// Two different assessments can propose the same sequence -- an
	// approval submitted, then a denial prepared while it is still
	// pending. Keyed on binding and sequence alone, the denial looks like
	// a retry of the approval and is reported as delivered when nothing
	// of it ever reached chain.
	sum := sha256.Sum256(payload)
	return fmt.Sprintf("%s:%d:%x", bindingKey, sequence, sum[:8])
}

// Publish delivers one assessment, reconciling before it sends.
//
// Returns the transaction hash on confirmation. Every other outcome is an
// error carrying enough detail for the caller to decide, because a
// publisher that silently swallows an ambiguous result is how duplicates
// happen.
func (p *Publisher) Publish(
	ctx context.Context,
	bindingKey string,
	sequence uint64,
	payload []byte,
	signature []byte,
) (string, error) {
	key := IdempotencyKey(bindingKey, sequence, payload)

	// 1. Has this exact delivery been attempted before?
	if prior, found, err := p.journal.Lookup(ctx, key); err != nil {
		return "", fmt.Errorf("journal lookup: %w", err)
	} else if found {
		switch prior.Status {
		case StatusConfirmed:
			// Already delivered. Returning the original hash keeps the
			// operation idempotent rather than sending a duplicate.
			return prior.TxHash, nil
		case StatusPending:
			return p.reconcile(ctx, bindingKey, sequence, *prior)
		case StatusSuperseded:
			return "", ErrSuperseded
		}
		// A failed attempt may be retried; fall through.
	}

	// 2. Reconcile against the chain before sending. The journal can be
	//    stale — a crash between submission and recording leaves no trace,
	//    and only the chain knows the truth.
	accepted, err := p.chain.AcceptedSequence(ctx, bindingKey)
	if err != nil {
		return "", fmt.Errorf("read accepted sequence: %w", err)
	}
	if accepted >= sequence {
		// The oracle requires strictly increasing sequences, so this can
		// never be accepted now -- whether the occupant is this exact
		// assessment or a different one that won the slot. Either way
		// THIS payload will not reach chain, and saying so is the point:
		// reporting it as delivered would claim a decision landed that
		// never did.
		status := StatusSuperseded
		detail := fmt.Sprintf("chain accepted %d, this is %d", accepted, sequence)
		_ = p.journal.Record(ctx, Attempt{
			BindingKey: bindingKey, Sequence: sequence, IdempotencyKey: key,
			Status: status, Detail: detail,
		})
		return "", fmt.Errorf("%w: %s", ErrSuperseded, detail)
	}

	// 3. Record the intent BEFORE submitting, so a crash mid-flight leaves
	//    a Pending record to reconcile rather than silence.
	if err := p.journal.Record(ctx, Attempt{
		BindingKey: bindingKey, Sequence: sequence, IdempotencyKey: key,
		Status: StatusPending, Detail: "submitting",
	}); err != nil {
		return "", fmt.Errorf("journal record: %w", err)
	}

	txHash, err := p.chain.SubmitAccept(ctx, payload, signature)
	if err != nil {
		// Submission may still have reached the node. Record Pending, not
		// Failed — treating an ambiguous send as failed is what produces a
		// double-publish on the next attempt.
		_ = p.journal.Record(ctx, Attempt{
			BindingKey: bindingKey, Sequence: sequence, IdempotencyKey: key,
			Status: StatusPending, Detail: fmt.Sprintf("submit error: %v", err),
		})
		return "", fmt.Errorf("submit: %w", err)
	}

	status, err := p.chain.ReceiptStatus(ctx, txHash)
	if err != nil {
		_ = p.journal.Record(ctx, Attempt{
			BindingKey: bindingKey, Sequence: sequence, IdempotencyKey: key,
			TxHash: txHash, Status: StatusPending,
			Detail: fmt.Sprintf("receipt unavailable: %v", err),
		})
		return "", fmt.Errorf("%w: tx %s", ErrPending, txHash)
	}

	_ = p.journal.Record(ctx, Attempt{
		BindingKey: bindingKey, Sequence: sequence, IdempotencyKey: key,
		TxHash: txHash, Status: status,
	})

	switch status {
	case StatusConfirmed:
		return txHash, nil
	case StatusPending:
		return "", fmt.Errorf("%w: tx %s", ErrPending, txHash)
	default:
		return "", fmt.Errorf("publish failed: tx %s reverted", txHash)
	}
}

// reconcile resolves a prior Pending attempt without resubmitting blindly.
func (p *Publisher) reconcile(
	ctx context.Context,
	bindingKey string,
	sequence uint64,
	prior Attempt,
) (string, error) {
	// Ask about OUR transaction first, before reading anything from the
	// sequence.
	//
	// The oracle enforces a strictly increasing sequence, so exactly one
	// assessment ever lands at a given number. Two can be prepared for it
	// -- an approval submitted, a denial built while it is still pending
	// -- and concluding from `accepted >= sequence` alone would report the
	// LOSER as delivered. For a denial that means recording a withdrawal
	// of permission that never reached chain, which is the one direction
	// that must never fail silently. Reported by review, 14 September.
	if prior.TxHash != "" {
		status, err := p.chain.ReceiptStatus(ctx, prior.TxHash)
		if err != nil {
			return "", fmt.Errorf("%w: tx %s", ErrPending, prior.TxHash)
		}
		switch status {
		case StatusPending:
			return "", fmt.Errorf("%w: tx %s still unmined", ErrPending, prior.TxHash)
		case StatusConfirmed:
			confirmed := prior
			confirmed.Status = StatusConfirmed
			confirmed.Detail = "reconciled from its own receipt"
			_ = p.journal.Record(ctx, confirmed)
			return prior.TxHash, nil
		}
		// Mined and reverted. Whatever the sequence now says, this
		// payload is not what produced it.
		failed := prior
		failed.Status = StatusFailed
		failed.Detail = "transaction reverted"
		_ = p.journal.Record(ctx, failed)
		return "", fmt.Errorf(
			"publish reverted for sequence %d; a different assessment may hold it",
			sequence)
	}

	// No hash to ask about -- the send never returned one. The sequence is
	// then the only evidence available, and it is weaker: it says the slot
	// is taken, not that WE took it. Treat it as superseded rather than
	// confirmed, so the caller reassesses instead of assuming delivery.
	accepted, err := p.chain.AcceptedSequence(ctx, bindingKey)
	if err != nil {
		return "", fmt.Errorf("reconcile: read sequence: %w", err)
	}
	if accepted >= sequence {
		superseded := prior
		superseded.Status = StatusFailed
		superseded.Detail = "sequence taken by another assessment"
		_ = p.journal.Record(ctx, superseded)
		return "", fmt.Errorf(
			"sequence %d already accepted; this attempt has no transaction to claim it",
			sequence)
	}

	// Known not to have landed: safe to retry.
	failed := prior
	failed.Status = StatusFailed
	failed.Detail = "did not land; eligible for retry"
	_ = p.journal.Record(ctx, failed)
	return "", fmt.Errorf("publish did not land for sequence %d", sequence)
}

// EncodeHex is a small helper for logging payload digests without dumping
// the whole calldata.
func EncodeHex(b []byte) string {
	if len(b) > 8 {
		b = b[:8]
	}
	return hex.EncodeToString(b)
}
