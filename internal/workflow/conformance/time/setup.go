package time

import (
	"fmt"

	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/simulate"
)

// Setup is a ready-to-run simulation of the time-punch/timecard/payroll-
// bridge reference workflow: the compiled plan, the declared inputs and the
// wired zero-effect environment behind every port.
type Setup struct {
	Plan    *workflow.CompiledWorkflow
	Env     *Environment
	Inputs  simulate.Inputs
	Options simulate.Options
}

// NewSetup wires env against the compiled reference workflow.
func NewSetup(env *Environment) (*Setup, error) {
	registry, err := env.Registry()
	if err != nil {
		return nil, err
	}
	plan, err := Compile(registry)
	if err != nil {
		return nil, fmt.Errorf("time: reference must compile: %w", err)
	}

	inputs := simulate.Inputs{
		Values: simulate.Bag{
			"worker_id":                   simulate.NewBranded("WorkerID", "44444444-4444-4444-8444-444444444444"),
			"punch_id":                    simulate.NewBranded("TimePunchID", "PUNCH-2026-1130-0800"),
			"device_id":                   simulate.NewBranded("DeviceID", "KIOSK-WEST-01"),
			"reported_clock_skew_seconds": simulate.NewString("2"),
		},
	}

	return &Setup{
		Plan:   plan,
		Env:    env,
		Inputs: inputs,
		Options: simulate.Options{
			Capabilities: registry,
			SubjectRef:   "principal:timekeeping-admin-3",
			Decisions:    Decisions{},
			Transforms:   Transforms{},
			Reads:        Reads{Env: env},
			Approvals:    Approvals{},
			Controls: []simulate.ControlVersion{
				{Name: "hcmnext.workflow.conformance.time.environment", Version: "v1"},
			},
		},
	}, nil
}
