package sim

import (
	"context"
	"encoding/json"
	"fmt"
)

// Underwriter is the thing being evaluated.
//
// Narrowed to one call on purpose. The runner hands it a snapshot and
// takes back a decision; it cannot ask the underwriter what it would like
// to see, and the underwriter cannot ask the runner what happens next.
// That is the wall `docs/07` draws, expressed as an interface rather than
// as a convention somebody has to remember.
type Underwriter interface {
	Assess(ctx context.Context, input, policy json.RawMessage) (json.RawMessage, error)
}

// RunConfig declares one evaluation.
type RunConfig struct {
	Pack   string
	Kind   PackKind
	World  Config
	Policy json.RawMessage

	// PolicyHash and ModelHash identify what is being evaluated. Recorded
	// in the pack so a result can be attributed.
	PolicyHash string
	ModelHash  string

	// StartingEquity the agent holds, in settlement base units.
	StartingEquity uint64

	// NamedScenarios the model claims to stress against. A shock whose
	// label is not here is an UNNAMED one, and `docs/07` requires an
	// evaluation set to include such a shock -- it is what separates
	// expected stress protection from guaranteed recovery.
	NamedScenarios []string
}

// Runner drives a world past an agent and an underwriter.
//
// It owns the virtual clock and the event queue, and it is the only thing
// that sees both sides. Keeping that in one place is what makes the
// separation auditable: if the underwriter ever learned a future price, it
// would have to be through this file.
type Runner struct {
	cfg   RunConfig
	world *World
	clock *Clock
	queue *Queue
	agent Agent
	uw    Underwriter

	// Ledger state, kept by the runner because the sim has no chain.
	equity      uint64
	openLoan    *loanState
	history     []uint64
	outcome     Outcome
	shocksSeen  map[string]bool
	namedShocks map[string]bool
}

type loanState struct {
	Principal   uint64
	AprBps      uint32
	TermSeconds uint64
	OpenedAt    int64
	// EntryPrice the borrowed principal bought into. The position is the
	// whole principal in the risky asset, which is what the engine was
	// told via IntendedAction::Acquire.
	EntryPrice uint64
	Units      uint64
	// EquityAtRisk is the borrower's own capital deployed alongside the
	// principal. Tracked so settlement can apply it to the debt BEFORE
	// allocating any loss to lenders.
	EquityAtRisk uint64
}

func NewRunner(cfg RunConfig, agent Agent, uw Underwriter) (*Runner, error) {
	world, err := Generate(cfg.World)
	if err != nil {
		return nil, err
	}
	named := map[string]bool{}
	for _, n := range cfg.NamedScenarios {
		named[n] = true
	}
	return &Runner{
		cfg:         cfg,
		world:       world,
		clock:       NewClock(cfg.World.StartTime),
		queue:       NewQueue(),
		agent:       agent,
		uw:          uw,
		equity:      cfg.StartingEquity,
		shocksSeen:  map[string]bool{},
		namedShocks: named,
	}, nil
}

// Run executes the whole world and returns a sealed pack.
//
// Every step reads the world only through `World.At`, which cannot return
// a future observation. That is the single choke point: an agent or an
// underwriter that saw further than the clock would have to get it from
// here, and here it cannot.
func (r *Runner) Run(ctx context.Context) (Pack, error) {
	for _, obs := range r.world.Observations {
		if err := r.clock.AdvanceTo(obs.At); err != nil {
			return Pack{}, err
		}
		r.history = append(r.history, obs.Price)

		r.queue.Push(Event{
			At:   obs.At,
			Kind: EventObservation,
			Detail: map[string]any{
				"price": obs.Price,
				"depth": obs.LiquidityDepth,
			},
		})

		// A shock only counts as exercised if a loan was live for it.
		// Counting a shock that hit an idle account would overstate
		// coverage, which is the number a reader most wants to trust.
		if obs.ShockLabel != "" && r.openLoan != nil {
			if !r.shocksSeen[obs.ShockLabel] {
				r.shocksSeen[obs.ShockLabel] = true
				r.outcome.ShocksExercised = append(
					r.outcome.ShocksExercised, obs.ShockLabel)
				if !r.namedShocks[obs.ShockLabel] {
					r.outcome.UnnamedShockExercised = true
				}
			}
		}

		if r.openLoan != nil {
			if err := r.maybeResolve(obs); err != nil {
				return Pack{}, err
			}
			continue
		}
		if err := r.maybeBorrow(ctx, obs); err != nil {
			return Pack{}, err
		}
	}

	// A loan still open when the world ends is not a repayment. Resolving
	// it as one would turn every unfinished run into a clean result.
	if r.openLoan != nil {
		r.settleAtMarket(r.world.Observations[len(r.world.Observations)-1])
	}

	agentCfg, _ := json.Marshal(map[string]any{"agent": r.agent.Name()})
	pack := Pack{
		Name:             r.cfg.Pack,
		Kind:             r.cfg.Kind,
		GeneratorVersion: Version,
		World:            r.cfg.World,
		WorldFingerprint: r.world.Fingerprint(),
		ModelHash:        r.cfg.ModelHash,
		PolicyHash:       r.cfg.PolicyHash,
		AgentConfig:      agentCfg,
		Events:           r.queue.Drain(),
		Outcome:          r.finalOutcome(),
	}
	return pack.Seal()
}

func (r *Runner) view(obs Observation) AgentView {
	return AgentView{
		Now:            r.clock.Now(),
		Price:          obs.Price,
		PriceScale:     obs.PriceScale,
		LiquidityDepth: obs.LiquidityDepth,
		RealizedVolBps: obs.RealizedVolBps,
		Equity:         r.equity,
		HasOpenLoan:    r.openLoan != nil,
		PriceHistory:   r.history,
	}
}

func (r *Runner) maybeBorrow(ctx context.Context, obs Observation) error {
	view := r.view(obs)
	proposal := r.agent.Propose(view)
	if proposal == nil {
		return nil
	}

	r.queue.Push(Event{
		At: obs.At, Kind: EventRequest,
		Agent:  r.agent.Name(),
		Detail: map[string]any{"principal": proposal.Principal},
	})

	offer, err := r.assess(ctx, obs, *proposal)
	if err != nil {
		return err
	}

	r.queue.Push(Event{
		At: obs.At, Kind: EventAssessment,
		Agent: r.agent.Name(),
		Detail: map[string]any{
			"approved": offer.Approved,
			"apr_bps":  offer.AprBps,
			"refusal":  offer.Refusal,
		},
	})

	accept, reason := r.agent.Accept(view, offer)
	if !accept {
		// A decline is an OUTCOME, not a retry. `docs/07`: the demo must
		// not force an approval to advance the script.
		r.outcome.Declined++
		r.queue.Push(Event{
			At: obs.At, Kind: EventDecline,
			Agent:  r.agent.Name(),
			Detail: map[string]any{"reason": reason},
		})
		return nil
	}

	// The WHOLE post-draw balance goes into the risky asset -- equity as
	// well as principal -- because that is what the snapshot declared via
	// `deploy_bps: 10000`.
	//
	// Until 14 September this bought units with the borrowed principal
	// alone and left equity sitting in settlement. The evaluated book was
	// then not the assessed book: equity absorbed nothing, and a fall
	// could book a lender loss while the borrower still held cash the
	// recovery never touched. `docs/09` makes agent equity the first loss
	// layer, so it has to be at risk. Reported by review, 14 September.
	deployed := r.equity + offer.ApprovedPrincipal
	units := deployed * pow10(obs.PriceScale) / obs.Price
	r.openLoan = &loanState{
		Principal:   offer.ApprovedPrincipal,
		AprBps:      offer.AprBps,
		TermSeconds: offer.TermSeconds,
		OpenedAt:    obs.At,
		EntryPrice:  obs.Price,
		Units:       units,
		// The equity that went in with it, so settlement can return what
		// is left after the debt is served.
		EquityAtRisk: r.equity,
	}
	// It is deployed, not idle. Settlement returns the residual.
	r.equity = 0
	r.outcome.Originated++
	r.outcome.PrincipalLent += offer.ApprovedPrincipal

	r.queue.Push(Event{
		At: obs.At, Kind: EventOrigination,
		Agent: r.agent.Name(),
		Detail: map[string]any{
			"principal": offer.ApprovedPrincipal,
			"apr_bps":   offer.AprBps,
		},
	})
	r.queue.Push(Event{
		At: obs.At, Kind: EventExecution,
		Agent:  r.agent.Name(),
		Detail: map[string]any{"units": units, "price": obs.Price},
	})
	return nil
}

// maybeResolve closes a loan at maturity, marking the position at the
// price the world actually reached.
func (r *Runner) maybeResolve(obs Observation) error {
	loan := r.openLoan
	if obs.At < loan.OpenedAt+int64(loan.TermSeconds) {
		return nil
	}
	r.settleAtMarket(obs)
	return nil
}

// settleAtMarket sells the position at the observed price and applies the
// proceeds to the debt.
//
// The three outcomes are the ones the ledger can actually produce:
// proceeds cover principal and interest (repaid), cover part (recovered),
// or the position is worth so little that principal is written off
// (defaulted). `docs/07` asks for several recovery outcomes INCLUDING
// near-zero recovery, which a deep enough shock produces here naturally
// rather than by being scripted.
func (r *Runner) settleAtMarket(obs Observation) {
	loan := r.openLoan
	r.openLoan = nil

	elapsed := obs.At - loan.OpenedAt
	if elapsed < 0 {
		elapsed = 0
	}
	interest := loan.Principal * uint64(loan.AprBps) * uint64(elapsed) /
		(10_000 * 365 * 86_400)

	// Mark the position, then charge the exit its liquidity impact: a
	// position larger than the book does not sell at the screen price.
	gross := loan.Units * obs.Price / pow10(obs.PriceScale)
	proceeds := applyImpact(gross, obs.LiquidityDepth)

	// `proceeds` is the WHOLE custody position -- principal and equity
	// both -- so the debt is served from all of it before any loss
	// reaches lenders. That is what makes equity the first loss layer
	// rather than a number quoted beside one.
	owed := loan.Principal + interest
	switch {
	case proceeds >= owed:
		r.outcome.Repaid++
		r.outcome.PrincipalRepaid += loan.Principal
		r.outcome.InterestCollected += interest
		r.equity += proceeds - owed
		r.queue.Push(Event{
			At: obs.At, Kind: EventRepayment,
			Agent:  r.agent.Name(),
			Detail: map[string]any{"proceeds": proceeds, "interest": interest},
		})

	case proceeds >= loan.Principal:
		// Principal returns, interest only partly. Recovery, not default:
		// lenders got their capital back.
		r.outcome.Recovered++
		r.outcome.PrincipalRecovered += loan.Principal
		r.outcome.InterestCollected += proceeds - loan.Principal
		r.queue.Push(Event{
			At: obs.At, Kind: EventRecovery,
			Agent:  r.agent.Name(),
			Detail: map[string]any{"proceeds": proceeds, "owed": owed},
		})

	default:
		// A real loss. The whole point of the exercise.
		lost := loan.Principal - proceeds
		r.outcome.Defaulted++
		r.outcome.PrincipalRecovered += proceeds
		r.outcome.PrincipalLost += lost
		r.queue.Push(Event{
			At: obs.At, Kind: EventDefault,
			Agent: r.agent.Name(),
			Detail: map[string]any{
				"proceeds": proceeds, "principal_lost": lost,
			},
		})
	}
}

// applyImpact reduces proceeds by the square-root market impact of exiting
// into the available depth -- the same shape `risk-core::stress` uses, so
// the world charges what the model priced.
func applyImpact(gross, depth uint64) uint64 {
	if depth == 0 || gross == 0 {
		return 0
	}
	ratioBps := gross * 10_000 / depth
	if ratioBps > 10_000 {
		ratioBps = 10_000
	}
	impactBps := isqrt(ratioBps*10_000) * 500 / 10_000
	if impactBps > 10_000 {
		impactBps = 10_000
	}
	return gross * (10_000 - impactBps) / 10_000
}

func isqrt(n uint64) uint64 {
	if n == 0 {
		return 0
	}
	x := n
	y := (x + 1) / 2
	for y < x {
		x = y
		y = (x + n/x) / 2
	}
	return x
}

func pow10(scale uint32) uint64 {
	out := uint64(1)
	for i := uint32(0); i < scale; i++ {
		out *= 10
	}
	return out
}

// assess builds a snapshot and asks the underwriter.
//
// The snapshot carries only what an observer at this moment could know.
// Building it here rather than letting the underwriter reach into the
// world is what keeps the wall standing.
func (r *Runner) assess(
	ctx context.Context,
	obs Observation,
	p Proposal,
) (Offer, error) {
	input, err := r.snapshot(obs, p)
	if err != nil {
		return Offer{}, err
	}

	raw, err := r.uw.Assess(ctx, input, r.cfg.Policy)
	if err != nil {
		// An unreachable engine is not a refusal with a reason; it is a
		// run that cannot continue honestly.
		return Offer{}, fmt.Errorf("assess at %d: %w", obs.At, err)
	}

	var decoded struct {
		Outcome struct {
			Decision string `json:"decision"`
			Terms    *struct {
				ApprovedPrincipal string `json:"approved_principal"`
				AprBps            uint32 `json:"apr_bps"`
				TermSeconds       uint64 `json:"term_seconds"`
			} `json:"terms"`
		} `json:"outcome"`
		// Reasons are a mixed vocabulary: some variants serialize as a
		// bare string, others as an object. Decoded loosely because the
		// runner only reports the first one -- interpreting them is the
		// engine's job, not the harness's.
		Reasons []json.RawMessage `json:"reasons"`
	}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return Offer{}, fmt.Errorf("decode assessment: %w", err)
	}

	if decoded.Outcome.Decision != "ApprovedOrigination" || decoded.Outcome.Terms == nil {
		refusal := decoded.Outcome.Decision
		if len(decoded.Reasons) > 0 {
			refusal = fmt.Sprintf("%s: %s", refusal, decoded.Reasons[0])
		}
		return Offer{Approved: false, Refusal: refusal}, nil
	}

	principal := parseAmount(decoded.Outcome.Terms.ApprovedPrincipal)
	return Offer{
		Approved:          true,
		ApprovedPrincipal: principal,
		AprBps:            decoded.Outcome.Terms.AprBps,
		TermSeconds:       decoded.Outcome.Terms.TermSeconds,
	}, nil
}

func parseAmount(s string) uint64 {
	var v uint64
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return 0
		}
		v = v*10 + uint64(s[i]-'0')
	}
	return v
}

// finalOutcome stamps the borrower's remaining capital onto the result.
//
// Reported so the first-loss rule is checkable: principal lost while
// equity remains is an accounting error, not a risk result.
func (r *Runner) finalOutcome() Outcome {
	out := r.outcome
	out.ResidualEquity = r.equity
	if r.openLoan != nil {
		// A loan still open at the end holds its equity in the position.
		out.ResidualEquity += r.openLoan.EquityAtRisk
	}
	return out
}
