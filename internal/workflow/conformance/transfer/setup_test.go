package transfer

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
	if _, ok := setup.Inputs.Values["worker_id"]; !ok {
		t.Fatal("setup declares no worker_id input")
	}
	if setup.Options.Capabilities == nil {
		t.Fatal("setup wires no capability registry")
	}
}

// TestNewSetupMissingLegalContext_OmitsTheContextSnapshot proves the
// constructor actually omits LegalContext rather than merely naming that it
// does.
func TestNewSetupMissingLegalContext_OmitsTheContextSnapshot(t *testing.T) {
	setup, err := NewSetupMissingLegalContext(GoldenEnvironment())
	if err != nil {
		t.Fatalf("NewSetupMissingLegalContext: %v", err)
	}
	if _, ok := setup.Inputs.Context["LegalContext"]; ok {
		t.Fatal("setup carries a LegalContext snapshot; the RED case requires omitting it")
	}
}
