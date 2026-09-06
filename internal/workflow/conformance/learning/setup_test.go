package learning

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

// TestNewSetupWithParams_OmitsThePolicyContextSnapshot proves the
// constructor actually omits PolicyContext rather than merely naming that
// it does.
func TestNewSetupWithParams_OmitsThePolicyContextSnapshot(t *testing.T) {
	setup, err := NewSetupWithParams(GoldenEnvironment(), Params{OmitPolicyContext: true})
	if err != nil {
		t.Fatalf("NewSetupWithParams: %v", err)
	}
	if _, ok := setup.Inputs.Context["PolicyContext"]; ok {
		t.Fatal("setup carries a PolicyContext snapshot; the RED case requires omitting it")
	}
}

// TestNewSetup_SuppliesThePolicyContextSnapshotByDefault proves the golden
// Params never accidentally omit the context the waiver read node expects.
func TestNewSetup_SuppliesThePolicyContextSnapshotByDefault(t *testing.T) {
	setup, err := NewSetup(GoldenEnvironment())
	if err != nil {
		t.Fatalf("NewSetup: %v", err)
	}
	if _, ok := setup.Inputs.Context["PolicyContext"]; !ok {
		t.Fatal("setup carries no PolicyContext snapshot; the golden scenario must supply it")
	}
}
