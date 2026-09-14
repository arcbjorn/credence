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
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/smartcontractkit/cre-sdk-go/capabilities/blockchain/evm"
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

	// GasLimit for the write. Bounded so a pathological report cannot
	// spend without limit.
	GasLimit uint64 `json:"gasLimit"`

	// ValiditySeconds is how long a monitoring permission stands. It
	// should be comfortably longer than the schedule interval, or
	// permission lapses between runs and blocks risk increases on a
	// healthy account (`docs/05`).
	ValiditySeconds uint64 `json:"validitySeconds"`

	// Loans to reassess. A workflow that discovered them itself would
	// need to trust its own view of which obligations exist; the engine's
	// answer is scoped to a binding either way.
	Loans []LoanRef `json:"loans"`
}

// LoanRef names one open obligation to reassess.
//
// Just an id. Everything else about the loan -- which agent, which
// account, which policy -- is the assessment endpoint's to resolve and the
// receiver's to hold. A workflow that carried those fields could name a
// real loan while describing a different one.
type LoanRef struct {
	LoanID uint64 `json:"loanId"`
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
	// AccountStateVersion is the portfolio the engine actually assessed.
	// It rides in the report rather than being supplied by the receiver:
	// `riskAllowedFor` compares it against the account's CURRENT version,
	// so permission issued against a stale view is void by construction.
	AccountStateVersion uint64 `json:"account_state_version"`
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

		seq, err := nextSequence(config, runtime, loan)
		if err != nil {
			logger.Warn("could not read the next sequence",
				"loanId", loan.LoanID, "error", err.Error())
			result.Blocked++
			result.Notes = append(result.Notes,
				fmt.Sprintf("loan %d: sequence: %v", loan.LoanID, err))
			continue
		}

		if err := writeAssessment(config, runtime, loan, assessment, seq); err != nil {
			// A write that failed recorded nothing, so the previous
			// permission stands until it expires. Reporting it as
			// written would claim an update reached chain that did not.
			logger.Warn("write failed; assessment did not reach chain",
				"loanId", loan.LoanID, "error", err.Error())
			result.Blocked++
			result.Notes = append(result.Notes,
				fmt.Sprintf("loan %d: write: %v", loan.LoanID, err))
			continue
		}
		result.Written++
	}

	return result, nil
}

// writeAssessment generates a DON-signed report and submits it.
//
// The DON signs the encoded payload, the forwarder verifies those
// signatures, and only then does CREReceiver see it. That chain is what
// the CRE route buys over a development key: no single party can produce
// a report the receiver will accept.
func writeAssessment(
	config *Config,
	runtime cre.Runtime,
	loan LoanRef,
	a *assessmentResponse,
	sequence uint64,
) error {
	report, err := buildReport(loan, a, sequence, runtime.Now().Unix(),
		config.ValiditySeconds)
	if err != nil {
		return err
	}

	signed, err := runtime.GenerateReport(&cre.ReportRequest{
		EncodedPayload: encodeMonitoringReport(report),
		EncoderName:    "evm",
		SigningAlgo:    "ecdsa",
		HashingAlgo:    "keccak256",
	}).Await()
	if err != nil {
		return fmt.Errorf("generate report: %w", err)
	}

	client := &evm.Client{ChainSelector: config.ChainSelector}
	resp, err := client.WriteReport(runtime, &evm.WriteCreReportRequest{
		Receiver:  common.HexToAddress(config.ReceiverAddress).Bytes(),
		Report:    signed,
		GasConfig: &evm.GasConfig{GasLimit: config.GasLimit},
	}).Await()
	if err != nil {
		return fmt.Errorf("submit: %w", err)
	}

	// Two statuses, and both matter. A transaction can succeed while the
	// receiver reverts -- which is exactly what happens when one of the
	// five checks refuses the report -- so treating TX_STATUS_SUCCESS as
	// "recorded" would report a blocked assessment as a written one.
	if resp.TxStatus != evm.TxStatus_TX_STATUS_SUCCESS {
		msg := "unknown error"
		if resp.ErrorMessage != nil {
			msg = *resp.ErrorMessage
		}
		return fmt.Errorf("transaction failed: %s", msg)
	}
	if resp.ReceiverContractExecutionStatus != nil &&
		*resp.ReceiverContractExecutionStatus !=
			evm.ReceiverContractExecutionStatus_RECEIVER_CONTRACT_EXECUTION_STATUS_SUCCESS {
		return fmt.Errorf("receiver rejected the report")
	}
	return nil
}

// buildReport turns the engine's answer into the payload the receiver
// decodes.
//
// Separated from the write so it is testable without a chain: these are
// the field mappings, and a mapping that only runs against a live DON is
// one nobody checks.
func buildReport(
	loan LoanRef,
	a *assessmentResponse,
	sequence uint64,
	now int64,
	validitySeconds uint64,
) (monitoringReport, error) {
	var r monitoringReport

	decision, err := decodeDecision(a.Decision)
	if err != nil {
		return r, err
	}
	permission, err := decodePermission(a.Permission)
	if err != nil {
		return r, err
	}
	class, err := decodeClass(a.Class)
	if err != nil {
		return r, err
	}
	assessmentID, err := parseHash32(a.Assessment)
	if err != nil {
		return r, fmt.Errorf("assessment hash: %w", err)
	}
	modelHash, err := parseHash32(a.ModelHash)
	if err != nil {
		return r, fmt.Errorf("model hash: %w", err)
	}
	inputHash, err := parseHash32(a.InputHash)
	if err != nil {
		return r, fmt.Errorf("input hash: %w", err)
	}
	evidenceRoot, err := parseHash32(a.Evidence)
	if err != nil {
		return r, fmt.Errorf("evidence root: %w", err)
	}

	if validitySeconds == 0 {
		return r, fmt.Errorf("validitySeconds must be set")
	}

	return monitoringReport{
		loanID:              loan.LoanID,
		accountStateVersion: a.AccountStateVersion,
		sequence:            sequence,
		// Chain time, not wall clock: the oracle compares these against
		// block.timestamp, and a validity window anchored to the node's
		// own clock would expire early or late by whatever the two
		// disagree by.
		validAfter:     uint64(now),
		validUntil:     uint64(now) + validitySeconds,
		decision:       decision,
		permission:     permission,
		class:          class,
		assessmentID:   assessmentID,
		assessmentHash: assessmentID,
		modelHash:      modelHash,
		inputHash:      inputHash,
		evidenceRoot:   evidenceRoot,
	}, nil
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
	// Only the loan id. The binding, the policy and the evidence all come
	// from the deployment's own configuration, so this workflow cannot
	// steer an assessment onto an account it does not underwrite -- and
	// the endpoint refuses any other field rather than ignoring it.
	body, err := json.Marshal(map[string]any{
		"loan_id": loan.LoanID,
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


// nextSequence asks the receiver what sequence this loan's next report
// must carry.
//
// `docs/05` requires a single strictly increasing sequence per binding
// across BOTH assessment kinds, so a workflow cannot count its own runs:
// an origination on the signed path advances the same counter. Reading it
// means asking the authority rather than guessing, and a guess that
// collided would have its update rejected and the permission left to
// expire.
func nextSequence(
	config *Config,
	runtime cre.Runtime,
	loan LoanRef,
) (uint64, error) {
	sel := nextSequenceSelector()
	data := append(sel[:], uintWord(loan.LoanID)...)

	client := &evm.Client{ChainSelector: config.ChainSelector}
	resp, err := client.CallContract(runtime, &evm.CallContractRequest{
		Call: &evm.CallMsg{
			To:   common.HexToAddress(config.ReceiverAddress).Bytes(),
			Data: data,
		},
	}).Await()
	if err != nil {
		return 0, fmt.Errorf("read sequence: %w", err)
	}
	if len(resp.Data) < 32 {
		return 0, fmt.Errorf("short sequence response: %d bytes", len(resp.Data))
	}

	seq := new(big.Int).SetBytes(resp.Data[:32])
	if !seq.IsUint64() {
		return 0, fmt.Errorf("sequence does not fit uint64")
	}
	// Zero means the receiver does not have this loan enrolled, so a
	// report would be refused anyway. Saying so here attributes the
	// failure to enrolment rather than to a rejected write.
	if seq.Uint64() == 0 {
		return 0, fmt.Errorf("loan %d is not enrolled on the receiver", loan.LoanID)
	}
	return seq.Uint64(), nil
}