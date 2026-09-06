package payroll

import "testing"

// TestNewSetup_WiresAReadyToRunPlan is a smoke check on the setup wiring
// itself; conformance_test.go exercises what the walk actually does.
func TestNewSetup_WiresAReadyToRunPlan(t *testing.T) {
	setup, err := NewSetup(GoldenEnvironment())
	if err != nil {
		t.Fatalf("NewSetup: %v", err)
	}
	if setup.Plan == nil {
		t.Fatal("setup carries no compiled plan")
	}
	if _, ok := setup.Inputs.Values["run_id"]; !ok {
		t.Fatal("setup declares no run_id input")
	}
	if setup.Options.Capabilities == nil {
		t.Fatal("setup wires no capability registry")
	}
}

// TestNewSetupWithParams_ReversalRequestedIsHonored proves the constructor
// actually threads the reversal_requested override into the workflow input
// rather than merely naming that it does.
func TestNewSetupWithParams_ReversalRequestedIsHonored(t *testing.T) {
	setup, err := NewSetupWithParams(GoldenEnvironment(), Params{ReversalRequested: true})
	if err != nil {
		t.Fatalf("NewSetupWithParams: %v", err)
	}
	v, err := setup.Inputs.Values.Get("reversal_requested")
	if err != nil {
		t.Fatalf("reversal_requested: %v", err)
	}
	got, err := v.Bool()
	if err != nil {
		t.Fatalf("reversal_requested.Bool: %v", err)
	}
	if !got {
		t.Fatal("reversal_requested = false, want true")
	}
}
