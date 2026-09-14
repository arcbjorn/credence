// Command credence-sim evaluates the underwriter against a synthetic
// population.
//
// `docs/07`: "Presented correctly — evaluating an underwriting model
// against a synthetic population, which is what a risk team actually does
// — this is more convincing than thin real data, because it exercises
// failure paths real data could not surface in time."
//
// Two sets, never one. A DEVELOPMENT set is what a policy is tuned
// against; an EVALUATION set has a different seed and a different shock
// schedule and is run against a model tuned elsewhere. Reporting a
// development result as evidence is the failure this separation exists to
// prevent, so the command refuses to run an evaluation pack without a
// distinct seed.
//
// What it reports is coverage and realized figures. There is no score:
// `docs/07` forbids claiming default-model validation from a small
// synthetic population, and a single number that looked like a grade would
// invite exactly that claim.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/arcbjorn/credence/gateway/gatewayrt"
	"github.com/arcbjorn/credence/gateway/riskclient"
	"github.com/arcbjorn/credence/gateway/sim"
)

func main() {
	riskURL := flag.String("risk", "http://127.0.0.1:18110",
		"risk service url -- the underwriter being evaluated")
	policyPath := flag.String("policy", "config/engine-policy.json",
		"engine policy the evaluation runs against")
	out := flag.String("out", "fixtures/sim", "directory for sealed packs")
	set := flag.String("set", "both",
		"which to run: development, evaluation, or both")
	flag.Parse()

	if err := run(*riskURL, *policyPath, *out, *set); err != nil {
		log.Fatalf("sim: %v", err)
	}
}

func run(riskURL, policyPath, outDir, which string) error {
	policy, err := os.ReadFile(policyPath)
	if err != nil {
		return fmt.Errorf("read policy: %w", err)
	}
	policyHash := gatewayrt.PolicyContentHash(policy)
	if policyHash == "" {
		return fmt.Errorf("policy %s is not canonical JSON", policyPath)
	}

	uw := riskclient.New(riskURL, 30*time.Second)
	ctx := context.Background()
	if err := uw.Health(ctx); err != nil {
		// An unreachable engine produces no evaluation, not an empty one.
		return fmt.Errorf("underwriter unreachable at %s: %w", riskURL, err)
	}

	sets := map[string]sim.RunConfig{}
	if which == "development" || which == "both" {
		sets["development"] = developmentSet(policy, policyHash)
	}
	if which == "evaluation" || which == "both" {
		sets["evaluation"] = evaluationSet(policy, policyHash)
	}
	if len(sets) == 0 {
		return fmt.Errorf("unknown set %q", which)
	}

	// The two sets must not share a seed or a schedule. A "separate"
	// evaluation that replayed the development world would report a
	// result the policy was tuned on.
	if d, okD := sets["development"]; okD {
		if e, okE := sets["evaluation"]; okE {
			if d.World.Seed == e.World.Seed {
				return fmt.Errorf(
					"development and evaluation share seed %d; "+
						"`docs/07` requires different seeds and schedules",
					d.World.Seed)
			}
		}
	}

	for _, name := range []string{"development", "evaluation"} {
		cfg, ok := sets[name]
		if !ok {
			continue
		}
		runner, err := sim.NewRunner(cfg, agent(), uw)
		if err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		pack, err := runner.Run(ctx)
		if err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}

		fmt.Print(pack.Summarize())

		path, err := pack.Save(outDir)
		if err != nil {
			// An existing pack is not an error to paper over: `docs/07`
			// makes a used pack immutable, so the operator must decide.
			fmt.Printf("\nnot saved: %v\n", err)
		} else {
			fmt.Printf("\nsealed at %s\n", path)
		}
		fmt.Println()
	}
	return nil
}

// agent is the deterministic policy agent `docs/07` permits for
// regression runs. It is NOT model-backed, and nothing here should be
// presented as a model decision.
func agent() sim.Agent {
	return sim.MomentumAgent{
		LookbackSteps:  24, // a day of hourly observations
		MinMomentumBps: 50,
		LeverageBps:    5_000, // half of equity
		CostBufferBps:  10,
		TermSeconds:    7 * 86_400,
		MaxAprBps:      4_000,
	}
}

// developmentSet is what a policy may be tuned against.
//
// Its shocks are the model's NAMED scenarios, because tuning against a
// shock the model does not claim to handle teaches nothing about the
// scenarios it does.
func developmentSet(policy json.RawMessage, policyHash string) sim.RunConfig {
	return sim.RunConfig{
		Pack:       "development",
		Kind:       sim.KindDevelopment,
		Policy:     policy,
		PolicyHash: policyHash,
		ModelHash:  modelHash,
		World: sim.Config{
			Seed:           0xC0FFEE,
			StartTime:      1_700_000_000,
			Days:           90,
			StepSeconds:    3_600,
			StartPrice:     2_000_000_000,
			PriceScale:     6,
			DriftBpsPerDay: 15,
			VolBpsPerStep:  45,
			LiquidityDepth: 20_000_000_000,
			Shocks: []sim.Shock{
				{AtDay: 20, PriceBps: -3_000, Label: "price"},
				{AtDay: 50, DepthBps: 3_000, Label: "liquidity"},
				{AtDay: 75, PriceBps: -2_500, DepthBps: 4_000, Label: "combined"},
			},
		},
		StartingEquity: 10_000_000_000,
		NamedScenarios: []string{"price", "liquidity", "combined"},
	}
}

// evaluationSet is run ONCE against a model tuned elsewhere.
//
// Different seed, different schedule, and -- the part that matters -- a
// shock OUTSIDE the model's named scenarios. `docs/07` asks for one
// explicitly, because it is what separates expected stress protection from
// guaranteed recovery: the model has a scenario for a price fall and a
// scenario for thin liquidity, and none for both arriving with a gap
// through the level where a keeper would have exited.
func evaluationSet(policy json.RawMessage, policyHash string) sim.RunConfig {
	return sim.RunConfig{
		Pack:       "evaluation",
		Kind:       sim.KindEvaluation,
		Policy:     policy,
		PolicyHash: policyHash,
		ModelHash:  modelHash,
		World: sim.Config{
			Seed:           0xBADBEEF,
			StartTime:      1_700_000_000,
			Days:           90,
			StepSeconds:    3_600,
			StartPrice:     2_000_000_000,
			PriceScale:     6,
			DriftBpsPerDay: -5, // a market that drifts against the agent
			VolBpsPerStep:  70,
			LiquidityDepth: 12_000_000_000,
			Shocks: []sim.Shock{
				{AtDay: 15, PriceBps: -3_500, Label: "price"},
				{AtDay: 40, DepthBps: 2_500, Label: "liquidity"},
				// The one the model has no scenario for.
				//
				// Placed at day 20 rather than late in the run, because a
				// shock nothing is exposed to exercises nothing. The first
				// schedule put it at day 62, by which point the agent had
				// stopped borrowing -- the harness correctly reported it as
				// unexercised, which is how the placement error surfaced.
				{
					AtDay:    20,
					PriceBps: -7_500,
					DepthBps: 400,
					Label:    "gap-through-the-exit",
				},
			},
		},
		StartingEquity: 10_000_000_000,
		NamedScenarios: []string{"price", "liquidity", "combined"},
	}
}

// modelHash identifies the engine being evaluated.
//
// Must equal `risk_core::model_hash()`, which the engine verifies. A
// mismatch here is not cosmetic: the engine refuses a snapshot whose
// declared model is not its own, so an evaluation would produce nothing
// rather than silently attribute results to the wrong build.
const modelHash = "eb1f28871b21718a9db5a3e65fcdca22fc1680e9f369d8adb25869966461a1a3"
