package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
)

// Derived computes the history and forecast evidence the engine consumes.
//
// These are the gateway's own records, not an external provider's, so they
// are always available — but "available" is not "complete". `docs/04`
// distinguishes a *verified absence* of history from an *unknown* one:
//
//	Verified absence of repayment history -> new account, base cap, no uplift
//	Missing required evidence             -> Unavailable, risk blocked
//
// The difference is whether the gateway can prove it looked. A binding it
// has never seen is unknown; a binding it has seen with zero resolved loans
// has a verified empty history. Conflating them would either deny credit to
// every new agent or grant a clean record to one the gateway knows nothing
// about.
type Derived struct {
	store *Store
}

func NewDerived(s *Store) *Derived { return &Derived{store: s} }

// HistorySummary mirrors the engine's input shape.
type HistorySummary struct {
	WeightedRepaymentsPPM uint64 `json:"weighted_repayments_ppm"`
	ResolvedDefaults      uint32 `json:"resolved_defaults"`
	PolicyBreaches        uint32 `json:"policy_breaches"`
	// CoverageComplete is false when the gateway cannot prove it enumerated
	// everything. An incomplete history is not a clean one.
	CoverageComplete bool `json:"coverage_complete"`
}

// ForecastSummary mirrors the engine's input shape.
type ForecastSummary struct {
	ComparableResolved uint32 `json:"comparable_resolved"`
	MeanBrierPPM       uint64 `json:"mean_brier_ppm"`
	Unresolved         uint32 `json:"unresolved"`
	Unobservable       uint32 `json:"unobservable"`
}

// History computes repayment history for a binding.
//
// Returns an error when the binding is unknown: the caller turns that into
// Missing evidence, which blocks authorization. A known binding with no
// loans returns a complete, empty history instead — a verified absence.
func (d *Derived) History(ctx context.Context, bindingKey string) (json.RawMessage, error) {
	var exists int
	err := d.store.db.QueryRowContext(ctx,
		`SELECT count(*) FROM bindings WHERE binding_key = ?`, bindingKey).Scan(&exists)
	if err != nil {
		return nil, fmt.Errorf("history: check binding: %w", err)
	}
	if exists == 0 {
		// Unknown, not empty. The gateway cannot prove anything about a
		// binding it has never registered.
		return nil, fmt.Errorf("history: unknown binding %q", bindingKey)
	}

	summary := HistorySummary{CoverageComplete: true}

	// Outcomes are recorded as chain events, so a binding with no recorded
	// outcomes has a verified empty history.
	rows, err := d.store.db.QueryContext(ctx,
		`SELECT event_name, payload FROM chain_events
		 WHERE event_name IN ('Repaid','Defaulted','PolicyBreach')
		   AND invalidated_at IS NULL
		   AND json_extract(payload, '$.binding_key') = ?`, bindingKey)
	if err != nil {
		return nil, fmt.Errorf("history: read outcomes: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var name string
		var payload []byte
		if err := rows.Scan(&name, &payload); err != nil {
			return nil, fmt.Errorf("history: scan outcome: %w", err)
		}
		switch name {
		case "Repaid":
			weight, err := repaymentWeight(payload)
			if err != nil {
				// A malformed record means coverage cannot be proven
				// complete. Better to under-credit than to silently drop an
				// outcome and report a cleaner history than the data shows.
				summary.CoverageComplete = false
				continue
			}
			summary.WeightedRepaymentsPPM += weight
		case "Defaulted":
			summary.ResolvedDefaults++
		case "PolicyBreach":
			summary.PolicyBreaches++
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("history: iterate outcomes: %w", err)
	}

	return json.Marshal(summary)
}

// repaymentWeight applies the exposure-by-duration weighting from `docs/06`.
//
//	weight = (principal / reference) * min(eligible_seconds / 30d, 1)
//
// Same-block borrow and repay earns nothing, so ten instant micro-loans
// cannot approach the weight of real capital held for real duration.
func repaymentWeight(payload []byte) (uint64, error) {
	var doc struct {
		Principal       uint64 `json:"principal"`
		ReferenceSize   uint64 `json:"reference_size"`
		EligibleSeconds uint64 `json:"eligible_seconds"`
	}
	if err := json.Unmarshal(payload, &doc); err != nil {
		return 0, err
	}
	if doc.ReferenceSize == 0 {
		return 0, fmt.Errorf("reference size is zero")
	}

	const thirtyDays = 30 * 86_400
	duration := doc.EligibleSeconds
	if duration > thirtyDays {
		duration = thirtyDays
	}

	// Integer arithmetic throughout: this feeds a capacity decision, and
	// `docs/04` forbids a floating-point path producing such a value.
	// ppm = principal * 1_000_000 * duration / (reference * 30d)
	numerator := doc.Principal
	if numerator > 0 {
		numerator = numerator * 1_000_000
	}
	weight := numerator / doc.ReferenceSize
	return weight * duration / thirtyDays, nil
}

// Forecasts computes forecast quality for a binding.
//
// Unresolved and unobservable commitments are counted separately and never
// folded into the resolved total: `docs/03` requires that insufficient
// eligible outcomes take the maximum penalty, so inflating the count would
// award credit the evidence does not support.
func (d *Derived) Forecasts(ctx context.Context, bindingKey string) (json.RawMessage, error) {
	var exists int
	if err := d.store.db.QueryRowContext(ctx,
		`SELECT count(*) FROM bindings WHERE binding_key = ?`, bindingKey,
	).Scan(&exists); err != nil {
		return nil, fmt.Errorf("forecasts: check binding: %w", err)
	}
	if exists == 0 {
		return nil, fmt.Errorf("forecasts: unknown binding %q", bindingKey)
	}

	summary := ForecastSummary{}

	var (
		resolved   sql.NullInt64
		brierTotal sql.NullInt64
	)
	err := d.store.db.QueryRowContext(ctx,
		`SELECT count(o.outcome_id), coalesce(sum(o.brier_ppm), 0)
		 FROM outcomes o
		 JOIN forecasts f ON f.forecast_id = o.forecast_id
		 WHERE f.binding_key = ? AND o.resolution IN ('Occurred','DidNotOccur')
		   AND o.brier_ppm IS NOT NULL`, bindingKey,
	).Scan(&resolved, &brierTotal)
	if err != nil {
		return nil, fmt.Errorf("forecasts: read resolved: %w", err)
	}
	if resolved.Valid && resolved.Int64 > 0 {
		summary.ComparableResolved = uint32(resolved.Int64)
		summary.MeanBrierPPM = uint64(brierTotal.Int64) / uint64(resolved.Int64)
	}

	var unresolved, unobservable sql.NullInt64
	err = d.store.db.QueryRowContext(ctx,
		`SELECT
		   (SELECT count(*) FROM forecasts f
		     WHERE f.binding_key = ?
		       AND NOT EXISTS (SELECT 1 FROM outcomes o WHERE o.forecast_id = f.forecast_id)),
		   (SELECT count(*) FROM outcomes o
		     JOIN forecasts f ON f.forecast_id = o.forecast_id
		     WHERE f.binding_key = ? AND o.resolution = 'NotObservable')`,
		bindingKey, bindingKey,
	).Scan(&unresolved, &unobservable)
	if err != nil {
		return nil, fmt.Errorf("forecasts: read pending: %w", err)
	}
	summary.Unresolved = uint32(unresolved.Int64)
	summary.Unobservable = uint32(unobservable.Int64)

	return json.Marshal(summary)
}
