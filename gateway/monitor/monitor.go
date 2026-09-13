// Package monitor runs the continuous underwriting loop.
//
// It closes the path `docs/03` names as a release requirement: while a loan
// is active and the agent submits no new request, a market observation
// changes, the scheduler detects it, the engine produces a monitoring
// assessment, and the updated risk state reaches the chain.
//
// Three properties shape the design:
//
//  1. **Fail-closed.** Every failure path — unreachable engine, missing
//     evidence, publish error — must leave permission blocked or expiring,
//     never extended. Nothing here can grant permission by failing.
//
//  2. **Monitoring never funds.** The loop builds `Context: Monitoring`
//     with the active loan, never a fabricated borrower request, and the
//     engine has no field in which to return terms for it.
//
//  3. **Every cycle is retained.** A blocked result is as much a record as
//     an allowed one; `docs/04` requires the evidence trail to show that a
//     decision was made, not only that one succeeded.
package monitor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"

	"github.com/arcbjorn/credence/gateway/riskclient"
	"github.com/arcbjorn/credence/gateway/scheduler"
	"github.com/arcbjorn/credence/gateway/snapshot"
)

// EvidenceSource supplies the observations a monitoring snapshot needs.
// Implementations fetch from chain, providers, or a scenario generator.
type EvidenceSource interface {
	// Collect returns a monitoring snapshot for one binding at one time.
	// A provider failure must surface as Missing evidence inside the
	// snapshot rather than as an error, so the engine still records a
	// decision and the reason it was blocked.
	Collect(ctx context.Context, task scheduler.Task, now int64) (snapshot.Input, error)
}

// Publisher delivers an accepted assessment to the chain.
type Publisher interface {
	Publish(ctx context.Context, bindingKey string, assessment json.RawMessage) (txHash string, err error)
}

// Recorder persists each cycle. Called for every outcome, including
// failures — a cycle that produced no assessment is still a cycle that
// happened.
type Recorder interface {
	RecordCycle(ctx context.Context, c Cycle) error
	// RetainDecision stores the canonical snapshot and assessment.
	// Separate from RecordCycle because a cycle log is diagnostics while
	// these bytes are the evidence replay runs against (`docs/04`).
	RetainDecision(ctx context.Context, bindingKey, inputHash string, snapshot, assessment []byte) error
}

// Cycle is one pass of the loop, whatever its outcome.
type Cycle struct {
	BindingKey string
	LoanID     string
	Trigger    scheduler.Trigger
	QueuedAt   int64
	InputHash  string
	// Empty when no assessment was produced.
	AssessmentID string
	// Permission the chain should now reflect. Blocked on every failure.
	Permission string
	// Non-empty when the assessment was published.
	TxHash string
	// Populated when the cycle failed. Retained for diagnosis.
	Failure string
}

const (
	PermissionAllowed = "Allowed"
	PermissionBlocked = "Blocked"
)

type Config struct {
	// Policy passed to the engine, pinned per deployment.
	Policy json.RawMessage
}

type Monitor struct {
	sched    *scheduler.Scheduler
	evidence EvidenceSource
	risk     *riskclient.Client
	pub      Publisher
	rec      Recorder
	cfg      Config
}

func New(
	sched *scheduler.Scheduler,
	evidence EvidenceSource,
	risk *riskclient.Client,
	pub Publisher,
	rec Recorder,
	cfg Config,
) *Monitor {
	return &Monitor{sched: sched, evidence: evidence, risk: risk, pub: pub, rec: rec, cfg: cfg}
}

// Tick runs one pass: ask the scheduler what is due, and process each task.
//
// Returns the cycles it ran. A task that fails does not abort the pass —
// one binding's unreachable provider must not stop another binding's
// permission from being revoked.
func (m *Monitor) Tick(ctx context.Context, prices map[string]*big.Int, now int64) []Cycle {
	tasks := m.sched.Due(prices)
	cycles := make([]Cycle, 0, len(tasks))
	for _, task := range tasks {
		cycles = append(cycles, m.runTask(ctx, task, prices[task.BindingKey], now))
	}
	return cycles
}

func (m *Monitor) runTask(
	ctx context.Context,
	task scheduler.Task,
	price *big.Int,
	now int64,
) Cycle {
	// Blocked is the default, so every early return below fails closed.
	cycle := Cycle{
		BindingKey: task.BindingKey,
		LoanID:     task.LoanID,
		Trigger:    task.Trigger,
		QueuedAt:   task.QueuedAt,
		Permission: PermissionBlocked,
	}

	input, err := m.evidence.Collect(ctx, task, now)
	if err != nil {
		return m.finish(ctx, cycle, fmt.Errorf("collect evidence: %w", err))
	}

	// A snapshot the gateway itself malformed must not reach the engine.
	if err := input.Validate(); err != nil {
		return m.finish(ctx, cycle, fmt.Errorf("invalid snapshot: %w", err))
	}

	// Guard the loop's defining constraint: monitoring evaluates an
	// existing loan and can never fund one. A builder bug that produced an
	// origination context here would turn a health check into a draw.
	if err := requireMonitoringContext(input.Context); err != nil {
		return m.finish(ctx, cycle, err)
	}

	if hash, err := input.InputHash(); err == nil {
		cycle.InputHash = hash
	}

	inputJSON, err := json.Marshal(input)
	if err != nil {
		return m.finish(ctx, cycle, fmt.Errorf("marshal snapshot: %w", err))
	}

	raw, err := m.risk.Assess(ctx, inputJSON, m.cfg.Policy)
	if err != nil {
		// An unreachable engine leaves permission blocked. Evidence then
		// ages out on chain, which is the fail-closed behaviour `docs/03`
		// requires when the scheduler or engine is down.
		return m.finish(ctx, cycle, fmt.Errorf("assess: %w", err))
	}

	var result struct {
		AssessmentID string `json:"assessment_id"`
		InputHash    string `json:"input_hash"`
		Outcome      struct {
			Decision string `json:"decision"`
		} `json:"outcome"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return m.finish(ctx, cycle, fmt.Errorf("decode assessment: %w", err))
	}

	// Retained before publishing: a decision that fails to reach chain was
	// still made, and `docs/04` requires it be replayable. Retaining only
	// on success would lose exactly the cases worth investigating.
	if m.rec != nil {
		if err := m.rec.RetainDecision(
			ctx, task.BindingKey, cycle.InputHash, inputJSON, raw); err != nil {
			return m.finish(ctx, cycle, fmt.Errorf("retain: %w", err))
		}
	}

	cycle.AssessmentID = result.AssessmentID
	if result.InputHash != "" {
		// The engine's hash is authoritative; a divergence means the
		// gateway reshaped what it sent and must be visible.
		if cycle.InputHash != "" && cycle.InputHash != result.InputHash {
			return m.finish(ctx, cycle, fmt.Errorf(
				"input hash divergence: gateway %s, engine %s", cycle.InputHash, result.InputHash))
		}
		cycle.InputHash = result.InputHash
	}

	// Only an explicit monitoring approval grants permission. Anything
	// else — a denial, an unavailable, or an unexpected decision — leaves
	// it blocked.
	if result.Outcome.Decision == "MonitoringAllowed" {
		cycle.Permission = PermissionAllowed
	}

	txHash, err := m.pub.Publish(ctx, task.BindingKey, raw)
	if err != nil {
		// The assessment stands and is recorded; only delivery failed.
		// Permission on chain keeps whatever it had until it expires,
		// which is why expiry must be short enough to matter.
		cycle.Permission = PermissionBlocked
		return m.finish(ctx, cycle, fmt.Errorf("publish: %w", err))
	}
	cycle.TxHash = txHash

	// The reassessment landed, so the standing permission is now bound to
	// this price and this evidence. Advancing the reference here -- and
	// only here -- means a move that failed to publish keeps retriggering
	// instead of going quiet while permission rests on stale ground.
	observedAt := uint64(now)
	if at, ok := input.OldestRequiredObservation(); ok {
		observedAt = at
	}
	m.sched.Observed(task.BindingKey, price, now, int64(observedAt))

	return m.finish(ctx, cycle, nil)
}

func (m *Monitor) finish(ctx context.Context, cycle Cycle, cause error) Cycle {
	if cause != nil {
		cycle.Failure = cause.Error()
		cycle.Permission = PermissionBlocked
	}
	if m.rec != nil {
		// A recording failure must not mask the cycle's own outcome, but it
		// must be visible: the evidence trail is the product.
		if err := m.rec.RecordCycle(ctx, cycle); err != nil && cycle.Failure == "" {
			cycle.Failure = fmt.Errorf("record cycle: %w", err).Error()
		}
	}
	return cycle
}

var errNotMonitoring = errors.New("snapshot is not a monitoring context")

func requireMonitoringContext(raw json.RawMessage) error {
	var probe struct {
		Kind   string          `json:"kind"`
		Loan   json.RawMessage `json:"loan"`
		Reqest json.RawMessage `json:"request"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil {
		return fmt.Errorf("%w: undecodable context", errNotMonitoring)
	}
	if probe.Kind != "Monitoring" {
		return fmt.Errorf("%w: kind %q", errNotMonitoring, probe.Kind)
	}
	if len(probe.Loan) == 0 {
		return fmt.Errorf("%w: no active loan", errNotMonitoring)
	}
	if len(probe.Reqest) != 0 {
		return fmt.Errorf("%w: carries a borrower request", errNotMonitoring)
	}
	return nil
}
