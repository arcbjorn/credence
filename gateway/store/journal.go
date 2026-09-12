package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/arcbjorn/credence/gateway/publisher"
)

// Journal persists publication attempts so a restart reconciles rather than
// guesses.
//
// The publisher's guarantee depends on this surviving a crash: an attempt
// recorded as Pending before submission is what lets a restarted process
// discover that a transaction may already be in flight. An in-memory
// journal would lose exactly the record that prevents a double-publish.
type Journal struct {
	store *Store
}

func NewJournal(s *Store) *Journal { return &Journal{store: s} }

// Lookup returns a prior attempt for an idempotency key.
func (j *Journal) Lookup(ctx context.Context, key string) (*publisher.Attempt, bool, error) {
	var (
		attempt  publisher.Attempt
		txHash   sql.NullString
		sequence sql.NullInt64
		detail   sql.NullString
	)
	err := j.store.db.QueryRowContext(ctx,
		`SELECT binding_key, sequence, idempotency_key, tx_hash, status, detail
		 FROM transactions WHERE idempotency_key = ?`, key,
	).Scan(&attempt.BindingKey, &sequence, &attempt.IdempotencyKey,
		&txHash, &attempt.Status, &detail)

	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("lookup attempt: %w", err)
	}

	attempt.TxHash = txHash.String
	attempt.Detail = detail.String
	if sequence.Valid {
		attempt.Sequence = uint64(sequence.Int64)
	}
	return &attempt, true, nil
}

// Record writes or updates an attempt.
//
// `attempts` increments on every write, so a binding that keeps failing is
// visible in the data rather than only in logs.
func (j *Journal) Record(ctx context.Context, a publisher.Attempt) error {
	now := time.Now().Unix()
	_, err := j.store.db.ExecContext(ctx,
		`INSERT INTO transactions
		   (idempotency_key, binding_key, kind, status, tx_hash, sequence,
		    attempts, created_at, updated_at, detail, payload)
		 VALUES (?, ?, 'accept', ?, ?, ?, 1, ?, ?, ?, x'')
		 ON CONFLICT(idempotency_key) DO UPDATE SET
		   status     = excluded.status,
		   tx_hash    = COALESCE(NULLIF(excluded.tx_hash, ''), transactions.tx_hash),
		   detail     = excluded.detail,
		   attempts   = transactions.attempts + 1,
		   updated_at = excluded.updated_at`,
		a.IdempotencyKey, a.BindingKey, string(a.Status),
		a.TxHash, int64(a.Sequence), now, now, a.Detail,
	)
	if err != nil {
		return fmt.Errorf("record attempt: %w", err)
	}
	return nil
}

// PendingAttempts lists deliveries whose outcome is unknown, so a restarted
// process can reconcile them before doing anything else.
func (j *Journal) PendingAttempts(ctx context.Context) ([]publisher.Attempt, error) {
	rows, err := j.store.db.QueryContext(ctx,
		`SELECT binding_key, sequence, idempotency_key, tx_hash, status, detail
		 FROM transactions WHERE status = ? ORDER BY updated_at`,
		string(publisher.StatusPending))
	if err != nil {
		return nil, fmt.Errorf("list pending: %w", err)
	}
	defer rows.Close()

	var out []publisher.Attempt
	for rows.Next() {
		var (
			a        publisher.Attempt
			txHash   sql.NullString
			sequence sql.NullInt64
			detail   sql.NullString
		)
		if err := rows.Scan(&a.BindingKey, &sequence, &a.IdempotencyKey,
			&txHash, &a.Status, &detail); err != nil {
			return nil, fmt.Errorf("scan pending: %w", err)
		}
		a.TxHash = txHash.String
		a.Detail = detail.String
		if sequence.Valid {
			a.Sequence = uint64(sequence.Int64)
		}
		out = append(out, a)
	}
	return out, rows.Err()
}
