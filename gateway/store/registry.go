package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// Registry owns agent and binding records.
//
// Its most important job is **rehydration**. The scheduler holds watches in
// memory, so a restart that does not reload them stops monitoring every
// active loan — silently, with permission still standing on chain until it
// expires. That is the opposite of fail-closed, and it is invisible unless
// someone is watching the logs at exactly the wrong moment.
type Registry struct {
	store *Store
}

func NewRegistry(s *Store) *Registry { return &Registry{store: s} }

// Agent is a persistent identity.
type Agent struct {
	AgentKey                string
	IdentityRegistryChainID uint64
	IdentityRegistry        string
	AgentID                 string
}

// Binding is the tuple financing attaches to.
type Binding struct {
	BindingKey    string
	AgentKey      string
	Operator      string
	CreditAccount string
	FacilityID    string
	ConfigVersion uint64
	ConfigHash    string
	PolicyEpoch   uint64
	PolicyHash    string
}

// ActiveWatch is a binding with an open loan, as reloaded at startup.
type ActiveWatch struct {
	BindingKey          string
	LoanID              string
	CreditAccount       string
	AccountStateVersion uint64
	PermissionIssuedAt  int64
	EvidenceObservedAt  int64
}

var ErrNotFound = errors.New("registry: not found")

// RegisterAgent records an identity. Idempotent, so replaying a
// registration is safe.
func (r *Registry) RegisterAgent(ctx context.Context, a Agent) error {
	_, err := r.store.db.ExecContext(ctx,
		`INSERT INTO agents
		   (agent_key, identity_registry_chain_id, identity_registry, agent_id, created_at)
		 VALUES (?, ?, ?, ?, ?)
		 ON CONFLICT(agent_key) DO UPDATE SET
		   identity_registry_chain_id = excluded.identity_registry_chain_id,
		   identity_registry          = excluded.identity_registry,
		   agent_id                   = excluded.agent_id`,
		a.AgentKey, a.IdentityRegistryChainID, a.IdentityRegistry, a.AgentID,
		time.Now().Unix())
	if err != nil {
		return fmt.Errorf("register agent: %w", err)
	}
	return nil
}

// RegisterBinding records a binding.
//
// Config and policy identifiers are updated on conflict because a binding's
// configuration can legitimately change; `docs/06` requires that change to
// trigger reassessment, which the mismatch between a stored version and an
// on-chain one will surface.
func (r *Registry) RegisterBinding(ctx context.Context, b Binding) error {
	_, err := r.store.db.ExecContext(ctx,
		`INSERT INTO bindings
		   (binding_key, agent_key, operator, credit_account, facility_id,
		    config_version, config_hash, policy_epoch, policy_hash, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(binding_key) DO UPDATE SET
		   operator       = excluded.operator,
		   credit_account = excluded.credit_account,
		   facility_id    = excluded.facility_id,
		   config_version = excluded.config_version,
		   config_hash    = excluded.config_hash,
		   policy_epoch   = excluded.policy_epoch,
		   policy_hash    = excluded.policy_hash`,
		b.BindingKey, b.AgentKey, b.Operator, b.CreditAccount, b.FacilityID,
		b.ConfigVersion, b.ConfigHash, b.PolicyEpoch, b.PolicyHash,
		time.Now().Unix())
	if err != nil {
		return fmt.Errorf("register binding: %w", err)
	}
	return nil
}

// Binding reads one binding.
func (r *Registry) Binding(ctx context.Context, bindingKey string) (Binding, error) {
	var b Binding
	err := r.store.db.QueryRowContext(ctx,
		`SELECT binding_key, agent_key, operator, credit_account, facility_id,
		        config_version, config_hash, policy_epoch, policy_hash
		 FROM bindings WHERE binding_key = ?`, bindingKey,
	).Scan(&b.BindingKey, &b.AgentKey, &b.Operator, &b.CreditAccount, &b.FacilityID,
		&b.ConfigVersion, &b.ConfigHash, &b.PolicyEpoch, &b.PolicyHash)

	if errors.Is(err, sql.ErrNoRows) {
		return Binding{}, fmt.Errorf("%w: binding %q", ErrNotFound, bindingKey)
	}
	if err != nil {
		return Binding{}, fmt.Errorf("read binding: %w", err)
	}
	return b, nil
}

// OpenLoan records that a binding has an active loan to monitor.
func (r *Registry) OpenLoan(
	ctx context.Context,
	bindingKey, loanID string,
	stateVersion uint64,
	observedAt int64,
) error {
	_, err := r.store.db.ExecContext(ctx,
		`INSERT INTO chain_events
		   (chain_id, tx_hash, log_index, block_number, block_hash,
		    contract, event_name, payload)
		 VALUES (0, ?, 0, 0, '', 'LoanManager', 'ActiveLoan', ?)
		 ON CONFLICT(chain_id, tx_hash, log_index) DO UPDATE SET
		   payload        = excluded.payload,
		   invalidated_at = NULL`,
		loanWatchKey(bindingKey),
		watchPayload(bindingKey, loanID, stateVersion, observedAt))
	if err != nil {
		return fmt.Errorf("open loan: %w", err)
	}
	return nil
}

// CloseLoan stops monitoring. Marked rather than deleted: `docs/04`
// requires preserving the fact that a decision was made, and a removed row
// would erase the history of what was watched and when.
func (r *Registry) CloseLoan(ctx context.Context, bindingKey string) error {
	_, err := r.store.db.ExecContext(ctx,
		`UPDATE chain_events SET invalidated_at = ?
		 WHERE tx_hash = ? AND event_name = 'ActiveLoan'`,
		time.Now().Unix(), loanWatchKey(bindingKey))
	if err != nil {
		return fmt.Errorf("close loan: %w", err)
	}
	return nil
}

// ActiveWatches reloads everything the scheduler must track.
//
// Called at startup. Without it, a restart leaves active loans unmonitored
// while their on-chain permission continues to stand.
func (r *Registry) ActiveWatches(ctx context.Context) ([]ActiveWatch, error) {
	rows, err := r.store.db.QueryContext(ctx,
		`SELECT e.payload, b.credit_account
		 FROM chain_events e
		 JOIN bindings b ON b.binding_key = json_extract(e.payload, '$.binding_key')
		 WHERE e.event_name = 'ActiveLoan' AND e.invalidated_at IS NULL
		 ORDER BY json_extract(e.payload, '$.binding_key')`)
	if err != nil {
		return nil, fmt.Errorf("list active watches: %w", err)
	}
	defer rows.Close()

	var out []ActiveWatch
	for rows.Next() {
		var payload []byte
		var account string
		if err := rows.Scan(&payload, &account); err != nil {
			return nil, fmt.Errorf("scan watch: %w", err)
		}
		w, err := parseWatchPayload(payload)
		if err != nil {
			return nil, err
		}
		w.CreditAccount = account
		out = append(out, w)
	}
	return out, rows.Err()
}
