// Package scheduler owns reassessment timing for continuous underwriting.
//
// `docs/03` names this the owner of the unprompted-reassessment acceptance
// case: while a loan is active and the agent submits no new request, a
// market observation changes, the scheduler detects it, and an updated risk
// state reaches the chain within a declared interval.
//
// The defining property is **fail-closed**. If this component stops, its
// permission ages out and risk-increasing actions are blocked on chain.
// Nothing here can extend permission by being unavailable.
package scheduler

import (
	"fmt"
	"math/big"
	"sort"
)

// Clock is injectable so scheduling is deterministic under test. `docs/06`
// forbids wall-clock reads in the engine; here the clock is explicit for the
// same reason — a schedule that cannot be replayed cannot be debugged.
type Clock interface {
	Now() int64
}

// Trigger is why a reassessment was queued. Recorded so the demo can show
// the cause, not just the effect.
type Trigger string

const (
	// TriggerPriceMove fires when an observation moves beyond policy bounds.
	TriggerPriceMove Trigger = "price_move"
	// TriggerStaleness fires when evidence is about to age past its bound.
	TriggerStaleness Trigger = "staleness"
	// TriggerHeartbeat is the periodic floor, so permission is refreshed
	// even in a quiet market rather than silently expiring.
	TriggerHeartbeat Trigger = "heartbeat"
)

// Policy declares the thresholds for one deployment.
type Policy struct {
	// PriceMoveBps triggers reassessment once an observation moves this far
	// from the price the standing permission was computed against.
	PriceMoveBps uint32
	// MaxEvidenceAgeSeconds is the freshness bound for required evidence.
	MaxEvidenceAgeSeconds int64
	// RefreshLeadSeconds queues reassessment before evidence actually goes
	// stale, so the chain state is refreshed rather than lapsing.
	RefreshLeadSeconds int64
	// HeartbeatSeconds bounds how long permission can stand untouched.
	HeartbeatSeconds int64
}

// Watch is one active loan under continuous underwriting.
type Watch struct {
	BindingKey string
	LoanID     string
	// ReferencePrice is the price the standing permission was computed
	// against. Movement is measured from here, not from the previous tick.
	ReferencePrice *big.Int
	// PermissionIssuedAt is when the standing monitoring result was accepted.
	PermissionIssuedAt int64
	// EvidenceObservedAt is the observation time of the freshest required
	// evidence backing that permission.
	EvidenceObservedAt int64
	// AccountStateVersion the permission was bound to.
	AccountStateVersion uint64
}

// Task is a queued reassessment.
type Task struct {
	BindingKey string
	LoanID     string
	Trigger    Trigger
	QueuedAt   int64
	// Detail explains the trigger in the demo and the latency report.
	Detail string
}

// Scheduler decides which active loans need reassessment now.
//
// It holds no chain state and issues no assessments; it only decides what
// to evaluate and when. That separation keeps the decision itself in the
// pure engine.
type Scheduler struct {
	policy  Policy
	clock   Clock
	watches map[string]*Watch
}

func New(policy Policy, clock Clock) *Scheduler {
	return &Scheduler{
		policy:  policy,
		clock:   clock,
		watches: make(map[string]*Watch),
	}
}

// Track registers an active loan. Called on origination.
func (s *Scheduler) Track(w Watch) {
	copy := w
	s.watches[w.BindingKey] = &copy
}

// Untrack stops watching. Called on repayment, recovery, or default —
// continuous underwriting governs permission, and a closed loan has none.
func (s *Scheduler) Untrack(bindingKey string) {
	delete(s.watches, bindingKey)
}

func (s *Scheduler) Watching(bindingKey string) bool {
	_, ok := s.watches[bindingKey]
	return ok
}

// Due returns reassessments to run now, in deterministic order.
//
// `prices` maps binding key to the latest observed price. A binding with no
// current price is NOT skipped: absent evidence is exactly the condition
// that must trigger reassessment, since silence would otherwise let
// permission stand on stale ground.
func (s *Scheduler) Due(prices map[string]*big.Int) []Task {
	now := s.clock.Now()
	var tasks []Task

	for key, w := range s.watches {
		if t, ok := s.evaluate(now, key, w, prices[key]); ok {
			tasks = append(tasks, t)
		}
	}

	// Stable order: the same state must produce the same queue, or the
	// demo's latency numbers are not reproducible.
	sort.Slice(tasks, func(i, j int) bool {
		if tasks[i].QueuedAt != tasks[j].QueuedAt {
			return tasks[i].QueuedAt < tasks[j].QueuedAt
		}
		return tasks[i].BindingKey < tasks[j].BindingKey
	})
	return tasks
}

func (s *Scheduler) evaluate(now int64, key string, w *Watch, price *big.Int) (Task, bool) {
	mk := func(tr Trigger, detail string) (Task, bool) {
		return Task{
			BindingKey: key,
			LoanID:     w.LoanID,
			Trigger:    tr,
			QueuedAt:   now,
			Detail:     detail,
		}, true
	}

	// Staleness first: if the evidence behind the standing permission is at
	// or near its bound, reassess regardless of price. Checking price first
	// would let a quiet market hold stale permission open.
	age := now - w.EvidenceObservedAt
	if age >= s.policy.MaxEvidenceAgeSeconds-s.policy.RefreshLeadSeconds {
		return mk(TriggerStaleness, fmt.Sprintf("evidence age %ds", age))
	}

	if price == nil {
		return mk(TriggerStaleness, "no current price observation")
	}

	if moved, bps := movedBeyond(w.ReferencePrice, price, s.policy.PriceMoveBps); moved {
		return mk(TriggerPriceMove, fmt.Sprintf("moved %d bps", bps))
	}

	if now-w.PermissionIssuedAt >= s.policy.HeartbeatSeconds {
		return mk(TriggerHeartbeat, fmt.Sprintf("permission age %ds", now-w.PermissionIssuedAt))
	}

	return Task{}, false
}

// movedBeyond reports absolute basis-point movement from reference.
//
// Integer arithmetic throughout: `docs/04` forbids a floating-point path
// producing a value used in authorization, and this decides whether a
// reassessment that gates authorization happens at all.
func movedBeyond(reference, current *big.Int, thresholdBps uint32) (bool, uint64) {
	if reference == nil || current == nil || reference.Sign() <= 0 {
		return false, 0
	}
	diff := new(big.Int).Sub(current, reference)
	diff.Abs(diff)
	// bps = |current - reference| * 10_000 / reference
	bps := new(big.Int).Mul(diff, big.NewInt(10_000))
	bps.Quo(bps, reference)

	if !bps.IsUint64() {
		return true, ^uint64(0)
	}
	v := bps.Uint64()
	return v >= uint64(thresholdBps), v
}
