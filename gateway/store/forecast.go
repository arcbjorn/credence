package store

import (
	"context"
	"encoding/json"
	"fmt"
)

// Forecast is a commitment made BEFORE funding.
//
// `docs/06`: commit before disbursement, storing the retrievable payload
// *and* its hash. A forecast recorded after the outcome is known is not a
// forecast, so the commitment time is what makes the score meaningful.
type Forecast struct {
	ForecastID      string
	BindingKey      string
	IntentHash      string
	EventDefinition string
	// ProbabilityPPM is the forecast probability in parts per million, so
	// the whole scoring path stays in integers.
	ProbabilityPPM uint64
	HorizonSeconds uint64
	CommittedAt    int64
	Payload        []byte
}

// Resolution of a committed forecast.
type Resolution string

const (
	Occurred      Resolution = "Occurred"
	DidNotOccur   Resolution = "DidNotOccur"
	NotObservable Resolution = "NotObservable"
)

// CommitForecast records a forecast before the loan it concerns is funded.
//
// Idempotent on forecast_id: re-committing the same forecast is safe, and
// must not overwrite what was originally committed. The first commitment
// is the evidence.
func (r *Registry) CommitForecast(ctx context.Context, f Forecast) error {
	if f.ForecastID == "" || len(f.Payload) == 0 {
		return fmt.Errorf("commit forecast: id and payload are required")
	}
	if f.ProbabilityPPM > 1_000_000 {
		return fmt.Errorf("commit forecast: probability %d exceeds 1e6", f.ProbabilityPPM)
	}

	var intent any
	if f.IntentHash != "" {
		intent = f.IntentHash
	}

	_, err := r.store.db.ExecContext(ctx,
		`INSERT INTO forecasts
		   (forecast_id, binding_key, intent_hash, event_definition,
		    probability_ppm, horizon_seconds, committed_at, payload)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(forecast_id) DO NOTHING`,
		f.ForecastID, f.BindingKey, intent, f.EventDefinition,
		f.ProbabilityPPM, f.HorizonSeconds, f.CommittedAt, f.Payload)
	if err != nil {
		return fmt.Errorf("commit forecast: %w", err)
	}
	return nil
}

// ResolveForecast records what actually happened and scores it.
//
// Brier loss is `(p - y)^2` with `y` in {0,1}, computed in ppm² and scaled
// back to ppm so the whole path is integer arithmetic (`docs/04` forbids a
// float producing a value used in authorization, and this feeds the
// forecast penalty that caps capacity).
//
// A NotObservable outcome carries no score. `docs/06`: unobservable
// outcomes receive no favourable treatment, and scoring one as correct
// would reward an agent for an event nobody could check.
func (r *Registry) ResolveForecast(
	ctx context.Context,
	outcomeID, forecastID string,
	resolution Resolution,
	resolvedAt int64,
	payload []byte,
) error {
	if outcomeID == "" || forecastID == "" {
		return fmt.Errorf("resolve forecast: ids are required")
	}

	var probabilityPPM uint64
	err := r.store.db.QueryRowContext(ctx,
		`SELECT probability_ppm FROM forecasts WHERE forecast_id = ?`,
		forecastID).Scan(&probabilityPPM)
	if err != nil {
		return fmt.Errorf("resolve forecast: unknown forecast %q: %w", forecastID, err)
	}

	var brier any
	switch resolution {
	case Occurred:
		brier = brierPPM(probabilityPPM, 1_000_000)
	case DidNotOccur:
		brier = brierPPM(probabilityPPM, 0)
	case NotObservable:
		brier = nil
	default:
		return fmt.Errorf("resolve forecast: unknown resolution %q", resolution)
	}

	if len(payload) == 0 {
		payload, _ = json.Marshal(map[string]any{
			"resolution": string(resolution), "resolved_at": resolvedAt,
		})
	}

	_, err = r.store.db.ExecContext(ctx,
		`INSERT INTO outcomes
		   (outcome_id, forecast_id, resolution, resolved_at, brier_ppm, payload)
		 VALUES (?, ?, ?, ?, ?, ?)
		 ON CONFLICT(outcome_id) DO NOTHING`,
		outcomeID, forecastID, string(resolution), resolvedAt, brier, payload)
	if err != nil {
		return fmt.Errorf("resolve forecast: %w", err)
	}
	return nil
}

// brierPPM computes `(p - y)^2` for probabilities in ppm, returning ppm.
//
// The squared difference is ppm², so it is divided back by 1e6. A perfect
// forecast scores 0; the worst possible scores 1e6 (a loss of 1.0).
func brierPPM(probabilityPPM, outcomePPM uint64) uint64 {
	var diff uint64
	if probabilityPPM > outcomePPM {
		diff = probabilityPPM - outcomePPM
	} else {
		diff = outcomePPM - probabilityPPM
	}
	return diff * diff / 1_000_000
}
