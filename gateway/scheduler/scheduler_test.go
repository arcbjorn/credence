package scheduler_test

import (
	"math/big"
	"testing"

	"github.com/arcbjorn/credence/gateway/scheduler"
)

type fakeClock struct{ t int64 }

func (c *fakeClock) Now() int64      { return c.t }
func (c *fakeClock) advance(d int64) { c.t += d }

func policy() scheduler.Policy {
	return scheduler.Policy{
		PriceMoveBps:          500, // 5%
		MaxEvidenceAgeSeconds: 300,
		RefreshLeadSeconds:    60,
		HeartbeatSeconds:      600,
	}
}

func setup(t *testing.T) (*scheduler.Scheduler, *fakeClock) {
	t.Helper()
	clk := &fakeClock{t: 10_000}
	s := scheduler.New(policy(), clk)
	s.Track(scheduler.Watch{
		BindingKey:          "b1",
		LoanID:              "loan-1",
		ReferencePrice:      big.NewInt(2_000_000000),
		PermissionIssuedAt:  10_000,
		EvidenceObservedAt:  10_000,
		AccountStateVersion: 3,
	})
	return s, clk
}

func prices(v int64) map[string]*big.Int {
	if v == 0 {
		return map[string]*big.Int{}
	}
	return map[string]*big.Int{"b1": big.NewInt(v)}
}

// A quiet market with fresh evidence needs no reassessment.
func TestQuietMarketQueuesNothing(t *testing.T) {
	s, clk := setup(t)
	clk.advance(30)
	if tasks := s.Due(prices(2_000_000000)); len(tasks) != 0 {
		t.Fatalf("expected no tasks, got %+v", tasks)
	}
}

// The acceptance case from docs/03: the agent requests nothing, the market
// moves, and reassessment is triggered.
func TestPriceMoveTriggersReassessment(t *testing.T) {
	s, clk := setup(t)
	clk.advance(30)
	// 2000 -> 1880 is a 600 bps drop, past the 500 bps threshold.
	tasks := s.Due(prices(1_880_000000))
	if len(tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(tasks))
	}
	if tasks[0].Trigger != scheduler.TriggerPriceMove {
		t.Errorf("trigger = %s, want price_move", tasks[0].Trigger)
	}
	if tasks[0].LoanID != "loan-1" {
		t.Errorf("task must carry the active loan id")
	}
}

// Movement is measured from the price the permission was computed against,
// not from the previous tick — otherwise a slow drift never triggers.
func TestSmallMoveBelowThresholdDoesNotTrigger(t *testing.T) {
	s, clk := setup(t)
	clk.advance(30)
	// 2000 -> 1950 is 250 bps, under the threshold.
	if tasks := s.Due(prices(1_950_000000)); len(tasks) != 0 {
		t.Fatalf("expected no trigger below threshold, got %+v", tasks)
	}
}

func TestUpwardMoveAlsoTriggers(t *testing.T) {
	s, clk := setup(t)
	clk.advance(30)
	if tasks := s.Due(prices(2_200_000000)); len(tasks) != 1 {
		t.Fatal("absolute movement must trigger in both directions")
	}
}

// Fail-closed: evidence approaching its bound triggers reassessment before
// it actually goes stale, so permission is refreshed rather than lapsing.
func TestStalenessTriggersBeforeExpiry(t *testing.T) {
	s, clk := setup(t)
	// 300s bound, 60s lead -> trigger at 240s.
	clk.advance(240)
	tasks := s.Due(prices(2_000_000000))
	if len(tasks) != 1 || tasks[0].Trigger != scheduler.TriggerStaleness {
		t.Fatalf("expected staleness trigger at the lead boundary, got %+v", tasks)
	}
}

// Absent price evidence must trigger reassessment, never be skipped.
// Silence is exactly how stale permission would otherwise stand.
func TestMissingPriceTriggersReassessment(t *testing.T) {
	s, clk := setup(t)
	clk.advance(30)
	tasks := s.Due(prices(0))
	if len(tasks) != 1 || tasks[0].Trigger != scheduler.TriggerStaleness {
		t.Fatalf("missing price must trigger, got %+v", tasks)
	}
}

// Staleness is checked before price so a quiet market cannot hold stale
// permission open.
func TestStalenessWinsOverQuietMarket(t *testing.T) {
	s, clk := setup(t)
	clk.advance(280)
	tasks := s.Due(prices(2_000_000000))
	if len(tasks) != 1 || tasks[0].Trigger != scheduler.TriggerStaleness {
		t.Fatalf("stale evidence must trigger even at an unchanged price, got %+v", tasks)
	}
}

// Heartbeat bounds how long permission stands untouched.
func TestHeartbeatRefreshesStandingPermission(t *testing.T) {
	clk := &fakeClock{t: 10_000}
	p := policy()
	p.MaxEvidenceAgeSeconds = 100_000 // keep staleness out of the way
	p.RefreshLeadSeconds = 60
	s := scheduler.New(p, clk)
	s.Track(scheduler.Watch{
		BindingKey:         "b1",
		LoanID:             "loan-1",
		ReferencePrice:     big.NewInt(2_000_000000),
		PermissionIssuedAt: 10_000,
		EvidenceObservedAt: 10_000,
	})
	clk.advance(600)
	tasks := s.Due(prices(2_000_000000))
	if len(tasks) != 1 || tasks[0].Trigger != scheduler.TriggerHeartbeat {
		t.Fatalf("expected heartbeat, got %+v", tasks)
	}
}

// A closed loan has no permission to govern.
func TestUntrackedLoanIsNotReassessed(t *testing.T) {
	s, clk := setup(t)
	s.Untrack("b1")
	clk.advance(1_000)
	if tasks := s.Due(prices(1_000_000000)); len(tasks) != 0 {
		t.Fatal("a repaid loan must not be reassessed")
	}
	if s.Watching("b1") {
		t.Fatal("untracked binding must not remain watched")
	}
}

// Deterministic order: the same state must produce the same queue, or the
// latency report is not reproducible.
func TestQueueOrderIsDeterministic(t *testing.T) {
	clk := &fakeClock{t: 10_000}
	s := scheduler.New(policy(), clk)
	for _, k := range []string{"b3", "b1", "b2"} {
		s.Track(scheduler.Watch{
			BindingKey:         k,
			LoanID:             "loan-" + k,
			ReferencePrice:     big.NewInt(2_000_000000),
			PermissionIssuedAt: 10_000,
			EvidenceObservedAt: 10_000,
		})
	}
	clk.advance(280) // all stale together

	var first []string
	for _, task := range s.Due(map[string]*big.Int{}) {
		first = append(first, task.BindingKey)
	}
	for i := 0; i < 5; i++ {
		var again []string
		for _, task := range s.Due(map[string]*big.Int{}) {
			again = append(again, task.BindingKey)
		}
		for j := range first {
			if first[j] != again[j] {
				t.Fatalf("queue order not deterministic: %v vs %v", first, again)
			}
		}
	}
	if len(first) != 3 || first[0] != "b1" {
		t.Fatalf("expected binding-key order, got %v", first)
	}
}

// A zero or absent reference price must not panic or divide by zero.
func TestZeroReferencePriceIsSafe(t *testing.T) {
	clk := &fakeClock{t: 10_000}
	s := scheduler.New(policy(), clk)
	s.Track(scheduler.Watch{
		BindingKey:         "b1",
		LoanID:             "loan-1",
		ReferencePrice:     big.NewInt(0),
		PermissionIssuedAt: 10_000,
		EvidenceObservedAt: 10_000,
	})
	clk.advance(30)
	_ = s.Due(prices(2_000_000000)) // must not panic
}

// After a reassessment lands, movement is measured from the new price.
// Without this the same move would retrigger forever, and the demo's
// "one move, one reassessment" claim would be false.
func TestObservedRebasesTheReference(t *testing.T) {
	s, clk := setup(t)
	clk.advance(30)

	moved := prices(1_880_000000)
	if tasks := s.Due(moved); len(tasks) != 1 {
		t.Fatalf("expected the move to trigger, got %+v", tasks)
	}

	// The cycle reached chain at the moved price.
	s.Observed("b1", big.NewInt(1_880_000000), clk.t, clk.t)

	clk.advance(30)
	if tasks := s.Due(moved); len(tasks) != 0 {
		t.Fatalf("same price must not retrigger, got %+v", tasks)
	}

	// A further move from the NEW reference triggers again. 1880 -> 1760
	// is 638 bps, past the threshold; measured from the original 2000 it
	// would be 1200 bps, so this also proves the reference actually moved.
	clk.advance(30)
	if tasks := s.Due(prices(1_760_000000)); len(tasks) != 1 {
		t.Fatalf("expected a move from the new reference, got %+v", tasks)
	}
}

// A failed publish must not advance the reference: permission still rests
// on the old price, so the move has to keep retriggering until it lands.
func TestUnpublishedCycleKeepsRetriggering(t *testing.T) {
	s, clk := setup(t)
	clk.advance(30)

	moved := prices(1_880_000000)
	if tasks := s.Due(moved); len(tasks) != 1 {
		t.Fatalf("expected the move to trigger, got %+v", tasks)
	}

	// No Observed call: the cycle failed before reaching chain.
	clk.advance(30)
	if tasks := s.Due(moved); len(tasks) != 1 {
		t.Fatalf("unpublished move must retrigger, got %+v", tasks)
	}
}

// Observed on an untracked binding is a no-op, not a panic or a
// resurrection: a loan that closed mid-cycle must stay untracked.
func TestObservedOnUntrackedBindingIsIgnored(t *testing.T) {
	s, _ := setup(t)
	s.Observed("gone", big.NewInt(1), 1, 1)
	if s.Watching("gone") {
		t.Fatal("Observed must not create a watch")
	}
}
