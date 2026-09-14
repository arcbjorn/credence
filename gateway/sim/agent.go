package sim

import "fmt"

// Agent decides whether to borrow, and on what terms.
//
// `docs/07`: "The agent must be able to decline when expected benefit, on
// the correct horizon after costs, does not justify the offered terms. The
// demo must not force an approval to advance the script."
//
// That is a real constraint on this interface, not a note. A runner whose
// only outcome was "borrow" would make every evaluation a story about
// lending rather than about deciding, and an agent that always accepted
// would never exercise the path where good terms are still bad terms.
type Agent interface {
	// Name identifies the agent in the record.
	Name() string

	// Propose asks for a borrowing intent at a moment in the world. A nil
	// proposal is a decision not to ask, which is different from asking
	// and being refused.
	Propose(view AgentView) *Proposal

	// Accept decides on offered terms. Returning false is a DECLINE, and
	// the runner records it as an outcome rather than retrying.
	Accept(view AgentView, offer Offer) (bool, string)
}

// AgentView is everything an agent may see.
//
// Deliberately narrow, and deliberately not the world: an agent that could
// read the shock schedule would be trading on the generator's secrets, and
// its results would say nothing about an agent that could not.
type AgentView struct {
	Now int64

	// Price and depth as OBSERVED, the same figures the underwriter gets.
	Price          uint64
	PriceScale     uint32
	LiquidityDepth uint64
	RealizedVolBps uint64

	// Equity the agent holds, in settlement base units.
	Equity uint64

	// HasOpenLoan is true while one is outstanding. One loan at a time
	// (`docs/05`), so an agent with a live loan cannot propose another.
	HasOpenLoan bool

	// PriceHistory is what the agent has seen so far, oldest first. Its
	// own memory, not a window onto the future.
	PriceHistory []uint64
}

// Proposal is a borrowing intent, before any terms exist.
type Proposal struct {
	Principal uint64
	// TermSeconds the agent wants to borrow for.
	TermSeconds uint64
	// MaxAprBps the agent will pay. The engine may offer worse, and the
	// agent may then decline -- which is the path that must stay open.
	MaxAprBps uint32
	// ExpectedReturnBps on the position over the term, as the agent
	// estimates it. Recorded so a decline can be checked against the
	// reasoning that produced it.
	ExpectedReturnBps int64
}

// Offer is what the underwriter came back with.
type Offer struct {
	Approved          bool
	ApprovedPrincipal uint64
	AprBps            uint32
	TermSeconds       uint64
	// Refusal is the engine's stated reason when Approved is false.
	Refusal string
}

// MomentumAgent borrows when recent prices have risen and declines when
// the offered rate exceeds what it expects to earn.
//
// A deterministic policy agent, which `docs/07` permits for regression
// runs. Not a model-backed one: it makes no claim to be intelligent, and
// presenting it as a model decision would be exactly the substitution the
// document warns about. What it does provide is a decision rule that can
// say no for a stated reason, which is what the evaluation needs.
type MomentumAgent struct {
	// LookbackSteps of price history the momentum is measured over.
	LookbackSteps int
	// MinMomentumBps to borrow at all.
	MinMomentumBps int64
	// LeverageBps of equity to request, so position size follows equity
	// rather than being a constant that outgrows the account.
	LeverageBps uint64
	// CostBufferBps the expected return must clear the rate BY, so a
	// trade that merely breaks even is declined.
	CostBufferBps int64
	// TermSeconds to request.
	TermSeconds uint64
	// MaxAprBps the agent will ever pay.
	MaxAprBps uint32
}

func (a MomentumAgent) Name() string { return "momentum-v1" }

func (a MomentumAgent) Propose(v AgentView) *Proposal {
	// One loan at a time: proposing a second is a request the chain would
	// refuse, and an agent that made it would inflate the request count
	// with attempts that could never succeed.
	if v.HasOpenLoan || v.Equity == 0 {
		return nil
	}
	if len(v.PriceHistory) <= a.LookbackSteps {
		return nil
	}

	past := v.PriceHistory[len(v.PriceHistory)-1-a.LookbackSteps]
	if past == 0 {
		return nil
	}
	momentum := (int64(v.Price) - int64(past)) * 10_000 / int64(past)
	if momentum < a.MinMomentumBps {
		return nil
	}

	principal := v.Equity * a.LeverageBps / 10_000
	if principal == 0 {
		return nil
	}

	return &Proposal{
		Principal:   principal,
		TermSeconds: a.TermSeconds,
		MaxAprBps:   a.MaxAprBps,
		// The naive extrapolation: what momentum did, annualized crudely
		// onto the term. Deliberately simple and deliberately WRONG
		// sometimes -- an agent that predicted correctly would make the
		// underwriter look good for the wrong reason.
		ExpectedReturnBps: momentum,
	}
}

func (a MomentumAgent) Accept(v AgentView, offer Offer) (bool, string) {
	if !offer.Approved {
		return false, "not offered"
	}

	// The rate over the ACTUAL term, not an annual figure compared against
	// a term return. `docs/07` says the horizon must be the correct one,
	// and comparing an APR to a few days' expected move is the mistake
	// that makes every loan look cheap.
	costBps := int64(offer.AprBps) * int64(offer.TermSeconds) / (365 * 86_400)

	view := a.Propose(v)
	expected := int64(0)
	if view != nil {
		expected = view.ExpectedReturnBps
	}

	if expected-costBps < a.CostBufferBps {
		return false, fmt.Sprintf(
			"expected %dbps over the term against a cost of %dbps; "+
				"below the %dbps buffer",
			expected, costBps, a.CostBufferBps)
	}
	return true, ""
}
