package main

import (
	"testing"

	"github.com/smartcontractkit/cre-sdk-go/capabilities/scheduler/cron"
	"github.com/smartcontractkit/cre-sdk-go/cre/testutils"
	"github.com/stretchr/testify/require"
)

func validConfig() *Config {
	return &Config{
		Schedule:        "0 */5 * * * *",
		RiskServiceURL:  "https://risk.credence.invalid",
		ChainSelector:   2183018362218727504,
		ReceiverAddress: "0x0000000000000000000000000000000000000001",
	}
}

func TestInitWorkflowRegistersTheCronTrigger(t *testing.T) {
	runtime := testutils.NewRuntime(t, testutils.Secrets{})

	workflow, err := InitWorkflow(validConfig(), runtime.Logger(), nil)
	require.NoError(t, err)
	require.Len(t, workflow, 1)
	require.Equal(t,
		cron.Trigger(&cron.Config{}).CapabilityID(),
		workflow[0].CapabilityID())
}

// A workflow missing the address it writes to, or the engine it asks,
// must refuse to start. Starting anyway produces a workflow that runs on
// schedule, reaches consensus on nothing, and writes nowhere -- which
// looks healthy in every log while no monitoring update reaches chain.
func TestInitWorkflowRefusesIncompleteConfig(t *testing.T) {
	runtime := testutils.NewRuntime(t, testutils.Secrets{})

	for _, c := range []struct {
		name   string
		mutate func(*Config)
	}{
		{"no schedule", func(c *Config) { c.Schedule = "" }},
		{"no risk service", func(c *Config) { c.RiskServiceURL = "" }},
		{"no receiver", func(c *Config) { c.ReceiverAddress = "" }},
		// The zero address is the config template's placeholder, not an
		// address. An empty-string check passes it, and the workflow would
		// then write reports into nothing -- succeeding, at a cost, with
		// no permission reaching the oracle. `config.production.json`
		// shipped with exactly this value.
		{"zero receiver", func(c *Config) {
			c.ReceiverAddress = "0x0000000000000000000000000000000000000000"
		}},
		// Not an address at all. `common.HexToAddress` does not error --
		// it returns the zero address for unparseable input, so garbage
		// silently becomes the same failure as above.
		{"unparseable receiver", func(c *Config) {
			c.ReceiverAddress = "not-an-address"
		}},
		{"short receiver", func(c *Config) { c.ReceiverAddress = "0xdeadbeef" }},
		// A write needs a chain to land on.
		{"no chain selector", func(c *Config) { c.ChainSelector = 0 }},
	} {
		t.Run(c.name, func(t *testing.T) {
			config := validConfig()
			c.mutate(config)
			_, err := InitWorkflow(config, runtime.Logger(), nil)
			require.Error(t, err, "an incomplete workflow must not start")
		})
	}
}

// `docs/04`: a decision and a permission are separate fields but not
// independent ones. A Denied result claiming Allowed is a contradiction,
// and the oracle rejects it -- catching it here keeps the failure
// attributable to the engine rather than surfacing far away.
func TestContradictoryResultIsRefused(t *testing.T) {
	require.Error(t, checkAssessment(&assessmentResponse{
		Decision:   "Denied",
		Permission: "Allowed",
		Class:      "PrototypeEligible",
	}))
}

// An assessment with no evidence class cannot be acted on: `docs/05`
// invariant 13 pins the executable class per deployment and compares it by
// exact match, so an empty class is not a permissive default.
func TestMissingClassIsRefused(t *testing.T) {
	require.Error(t, checkAssessment(&assessmentResponse{
		Decision:   "Approved",
		Permission: "Allowed",
		Class:      "",
	}))
}

func TestWellFormedAssessmentPasses(t *testing.T) {
	require.NoError(t, checkAssessment(&assessmentResponse{
		Decision:   "Approved",
		Permission: "Allowed",
		Class:      "PrototypeEligible",
	}))

	// A denial that says Blocked is coherent, and is a real result: it
	// withdraws permission rather than failing.
	require.NoError(t, checkAssessment(&assessmentResponse{
		Decision:   "Denied",
		Permission: "Blocked",
		Class:      "PrototypeEligible",
	}))
}
