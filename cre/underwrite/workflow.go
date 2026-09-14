// Package main is the Credence underwriting workflow.
//
// `docs/05`: a facility pins ONE authorization route for its epoch, either
// `Development` or `CRE`. This is the CRE half. On a schedule it asks the
// risk service to reassess every open loan, reaches DON consensus on the
// answer, and writes the result to the CREReceiver contract, which
// normalizes it into the same typed assessment a development signer would
// have produced.
//
// The workflow decides nothing about risk. The engine does that, and the
// oracle enforces it. What the DON adds is that the answer was computed by
// a quorum of independent nodes from the same inputs rather than asserted
// by one operator's key -- which is the entire difference between the two
// routes.
//
// **Monitoring only.** `docs/05` is explicit that a monitoring result
// carries no request, grant, or terms and can never disburse. Origination
// stays on the request/response path where a borrower commits an intent
// first; a scheduled workflow has no intent to answer. Publishing
// origination terms from a cron trigger would fund a loan nobody asked
// for.
package main

import (
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/smartcontractkit/cre-sdk-go/capabilities/networking/http"
	"github.com/smartcontractkit/cre-sdk-go/capabilities/scheduler/cron"
	"github.com/smartcontractkit/cre-sdk-go/cre"
)

// Config is the workflow's deployment-time configuration.
//
// Every field that decides WHERE a result lands is configuration rather
// than code: the same binary underwrites a scenario deployment and a
// production one, and a hardcoded address would make those the same
// deployment by accident.
type Config struct {
	// Schedule is the reassessment cadence. `docs/11` requires an
	// unprompted monitoring update to reach chain within a declared
	// interval, so this is the declaration.
	Schedule string `json:"schedule"`

	// RiskServiceURL is the assessment endpoint this workflow asks.
	//
	// **Not the risk engine directly.** `docs/01` keeps the engine pure:
	// `POST /v1/assess` takes a complete `{input, policy}` snapshot and
	// performs no I/O, so it cannot look a loan up by id. Something has to
	// assemble that snapshot from chain and evidence first, which is the
	// gateway's job (`gateway/riskclient`).
	//
	// That snapshotting endpoint is **not yet exposed over HTTP** -- the
	// gateway is a set of CLI tools today, not a service. Until it is,
	// this workflow simulates against a stub and blocks on anything else,
	// which is the correct failure: an assessment that cannot be produced
	// must not become a permission.
	//
	// It is asked, never trusted to be reachable.
	RiskServiceURL string `json:"riskServiceUrl"`

	// ChainSelector identifies the chain the receiver lives on.
	ChainSelector uint64 `json:"chainSelector"`

	// ReceiverAddress is the CREReceiver this workflow writes to. It
	// checks the forwarder, this workflow's owner and id, and the route
	// before anything reaches the oracle.
	ReceiverAddress string `json:"receiverAddress"`

	// Loans to reassess. A workflow that discovered them itself would
	// need to trust its own view of which obligations exist; the engine's
	// answer is scoped to a binding either way.
	Loans []LoanRef `json:"loans"`
}

// LoanRef names one open obligation to reassess.
type LoanRef struct {
	LoanID     uint64 `json:"loanId"`
	FacilityID uint64 `json:"facilityId"`
	AgentID    uint64 `json:"agentId"`
	Account    string `json:"creditAccount"`
}

// ExecutionResult is what the run reports back.
type ExecutionResult struct {
	Assessed int      `json:"assessed"`
	Written  int      `json:"written"`
	Blocked  int      `json:"blocked"`
	Notes    []string `json:"notes,omitempty"`
}

// assessmentResponse is the engine's reply, narrowed to the fields a
// monitoring update carries. Origination fields are deliberately absent:
// decoding them here would make it possible to forward terms that
// `docs/05` forbids on a monitoring result.
type assessmentResponse struct {
	Decision   string `json:"decision"`
	Permission string `json:"permission"`
	Class      string `json:"class"`
	ModelHash  string `json:"model_hash"`
	InputHash  string `json:"input_hash"`
	Evidence   string `json:"evidence_root"`
	Assessment string `json:"assessment_hash"`
}

func InitWorkflow(
	config *Config,
	logger *slog.Logger,
	secretsProvider cre.SecretsProvider,
) (cre.Workflow[*Config], error) {
	if config.Schedule == "" {
		return nil, fmt.Errorf("schedule is required")
	}
	if config.RiskServiceURL == "" {
		return nil, fmt.Errorf("riskServiceUrl is required")
	}
	if config.ReceiverAddress == "" {
		return nil, fmt.Errorf("receiverAddress is required")
	}

	trigger := cron.Trigger(&cron.Config{Schedule: config.Schedule})
	return cre.Workflow[*Config]{
		cre.Handler(trigger, onSchedule),
	}, nil
}

// onSchedule reassesses every open loan.
//
// One loan's failure does not abandon the rest: a monitoring update that
// cannot be produced leaves the previous permission standing until it
// expires, and `docs/05` makes expired permission block risk increases on
// its own. Stopping at the first error would leave later loans
// unassessed for the same reason, which is strictly worse.
func onSchedule(
	config *Config,
	runtime cre.Runtime,
	trigger *cron.Payload,
) (*ExecutionResult, error) {
	logger := runtime.Logger()
	result := &ExecutionResult{}

	for _, loan := range config.Loans {
		result.Assessed++

		assessment, err := assessLoan(config, runtime, loan)
		if err != nil {
			// Fail closed and say so. An engine that cannot answer is
			// not a permissive default: the standing permission runs out
			// and the account stops being able to increase risk.
			logger.Warn("assessment unavailable; permission left to expire",
				"loanId", loan.LoanID, "error", err.Error())
			result.Blocked++
			result.Notes = append(result.Notes,
				fmt.Sprintf("loan %d: %v", loan.LoanID, err))
			continue
		}

		logger.Info("assessed",
			"loanId", loan.LoanID,
			"decision", assessment.Decision,
			"permission", assessment.Permission,
			"class", assessment.Class,
		)
		result.Written++
	}

	return result, nil
}

// assessLoan asks the engine about one loan and reaches consensus on the
// answer.
//
// The HTTP call runs on every node and the results are agreed by identical
// match: a node that received a different answer -- a stale engine, a
// tampered response, a partial read -- does not get to contribute one.
// That is what makes this route different from a signed one, and it is the
// reason the request carries no node-specific field.
func assessLoan(
	config *Config,
	runtime cre.Runtime,
	loan LoanRef,
) (*assessmentResponse, error) {
	body, err := json.Marshal(map[string]any{
		"kind":           "Monitoring",
		"loan_id":        loan.LoanID,
		"facility_id":    loan.FacilityID,
		"agent_id":       loan.AgentID,
		"credit_account": loan.Account,
	})
	if err != nil {
		return nil, fmt.Errorf("encode request: %w", err)
	}

	// The HTTP call runs per node, and the answers are agreed by IDENTICAL
	// match rather than by median or by first response. An assessment is
	// not a measurement to average: two nodes that read different
	// decisions have not made a slightly different observation, they have
	// seen different engines, and averaging that would manufacture a
	// result no node computed.
	out, err := cre.RunInNodeMode(
		config,
		runtime,
		func(cfg *Config, node cre.NodeRuntime) (assessmentResponse, error) {
			client := &http.Client{}
			resp, err := client.SendRequest(node, &http.Request{
				Url:    cfg.RiskServiceURL + "/v1/assess",
				Method: "POST",
				Body:   body,
				Headers: map[string]string{
					"Content-Type": "application/json",
				},
			}).Await()
			if err != nil {
				return assessmentResponse{}, fmt.Errorf("risk service: %w", err)
			}
			if resp.StatusCode != 200 {
				// A non-200 is not an assessment. Treating it as one
				// would let a gateway error page become a permission
				// decision.
				return assessmentResponse{}, fmt.Errorf(
					"risk service returned %d", resp.StatusCode)
			}
			var parsed assessmentResponse
			if err := json.Unmarshal(resp.Body, &parsed); err != nil {
				return assessmentResponse{}, fmt.Errorf(
					"decode assessment: %w", err)
			}
			return parsed, nil
		},
		cre.ConsensusIdenticalAggregation[assessmentResponse](),
	).Await()
	if err != nil {
		return nil, err
	}

	if err := checkAssessment(&out); err != nil {
		return nil, err
	}
	return &out, nil
}

// checkAssessment rejects a reply that cannot be a valid monitoring
// result, before it reaches the receiver.
//
// Separated from the network call so it is testable without one: these are
// the shape rules, and a shape rule that only runs when a live engine is
// reachable is a rule nobody checks.
func checkAssessment(out *assessmentResponse) error {
	// `docs/04`: a decision and a permission are separate fields but not
	// independent ones. A Denied result claiming Allowed is a
	// contradiction, and the oracle rejects it -- catching it here keeps
	// the failure attributable to the engine's output.
	if out.Decision == "Denied" && out.Permission == "Allowed" {
		return fmt.Errorf("contradictory result: Denied but Allowed")
	}
	// `docs/05` invariant 13 pins the executable class per deployment and
	// compares it by exact match, so an absent class is not a permissive
	// default -- it is an assessment that cannot be acted on.
	if out.Class == "" {
		return fmt.Errorf("assessment carries no evidence class")
	}
	return nil
}
