package sim

import (
	"fmt"
	"sort"
)

// Clock is the simulation's virtual time.
//
// `docs/07`: "The simulator manipulates only scenario state; it never
// advances live chain timestamps." That separation is why this type
// exists rather than a package-level variable -- a clock reachable from
// anywhere is a clock something will eventually advance by accident, and
// a scenario that advanced real chain time would make virtual elapsed
// time indistinguishable from a public-chain loan's real age.
//
// Not safe for concurrent use, deliberately. Concurrency may fetch
// independent providers, but scenario events APPLY in deterministic
// order, and a clock two goroutines could advance would destroy that.
type Clock struct {
	now int64
}

func NewClock(start int64) *Clock { return &Clock{now: start} }

// Now is the virtual time, never the wall clock.
func (c *Clock) Now() int64 { return c.now }

// Advance moves time forward. Backwards is refused: an event queue that
// could rewind would replay effects that already happened.
func (c *Clock) Advance(seconds int64) error {
	if seconds < 0 {
		return fmt.Errorf("sim: cannot advance the clock backwards")
	}
	c.now += seconds
	return nil
}

// AdvanceTo moves the clock to an absolute virtual time.
func (c *Clock) AdvanceTo(t int64) error {
	if t < c.now {
		return fmt.Errorf(
			"sim: cannot move the clock from %d back to %d", c.now, t)
	}
	c.now = t
	return nil
}

// EventKind names what happened. Deliberately a closed set: an event the
// evaluation cannot classify is an event it cannot count.
type EventKind string

const (
	EventObservation EventKind = "observation"
	EventRequest     EventKind = "request"
	EventAssessment  EventKind = "assessment"
	EventOrigination EventKind = "origination"
	EventExecution   EventKind = "execution"
	EventRepayment   EventKind = "repayment"
	EventRecovery    EventKind = "recovery"
	EventDefault     EventKind = "default"
	EventDecline     EventKind = "decline"
	EventStaleData   EventKind = "stale_data"
	EventForecast    EventKind = "forecast_resolved"
)

// Event is one thing that happened at a virtual time.
type Event struct {
	At   int64     `json:"at"`
	Kind EventKind `json:"kind"`
	// Sequence orders events that share a timestamp. Assigned on enqueue,
	// so the order two same-instant events were produced in is preserved
	// rather than left to sort stability.
	Sequence uint64 `json:"sequence"`
	// ID is stable across runs of the same world, so a diff between two
	// evaluations points at events rather than at positions.
	ID     string         `json:"id"`
	Agent  string         `json:"agent,omitempty"`
	Loan   string         `json:"loan,omitempty"`
	Detail map[string]any `json:"detail,omitempty"`
}

// Queue orders events by virtual timestamp, then sequence, then ID.
//
// `docs/07` names exactly that ordering. The third key matters: two events
// enqueued in the same instant by different producers would otherwise be
// ordered by whichever ran first, which is not reproducible.
type Queue struct {
	events []Event
	next   uint64
}

func NewQueue() *Queue { return &Queue{} }

// Push records an event. The ID must be stable across runs -- derive it
// from what the event IS, never from a counter that a different code path
// would advance differently.
func (q *Queue) Push(e Event) {
	e.Sequence = q.next
	q.next++
	if e.ID == "" {
		e.ID = fmt.Sprintf("%s:%d:%d", e.Kind, e.At, e.Sequence)
	}
	q.events = append(q.events, e)
}

// Drain returns every event in deterministic order and empties the queue.
func (q *Queue) Drain() []Event {
	out := make([]Event, len(q.events))
	copy(out, q.events)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].At != out[j].At {
			return out[i].At < out[j].At
		}
		if out[i].Sequence != out[j].Sequence {
			return out[i].Sequence < out[j].Sequence
		}
		return out[i].ID < out[j].ID
	})
	q.events = nil
	return out
}

// Len reports how many events are queued.
func (q *Queue) Len() int { return len(q.events) }

// Count tallies events by kind.
//
// `docs/07`: report what was EXERCISED -- failure-path coverage, not a
// pass rate. A run that never reached a default has not tested the loss
// path, and this is what makes that visible instead of implied.
func Count(events []Event) map[EventKind]int {
	out := map[EventKind]int{}
	for _, e := range events {
		out[e.Kind]++
	}
	return out
}
