package termination

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

// TestNewSetupWithParams_OmitsTheContextSnapshot proves OmitLegalContext
// actually omits LegalContext rather than merely naming that it does.
func TestNewSetupWithParams_OmitsTheContextSnapshot(t *testing.T) {
	setup, err := NewSetupWithParams(GoldenEnvironment(), Params{OmitLegalContext: true})
	if err != nil {
		t.Fatalf("NewSetupWithParams: %v", err)
	}
	if _, ok := setup.Inputs.Context["LegalContext"]; ok {
		t.Fatal("setup carries a LegalContext snapshot; the RED case requires omitting it")
	}
}

// TestNewSetupWithParams_DefaultsRequesterAwayFromTheManager proves the
// golden Params never accidentally trip the separation-of-duties check.
func TestNewSetupWithParams_DefaultsRequesterAwayFromTheManager(t *testing.T) {
	setup, err := NewSetupWithParams(GoldenEnvironment(), Params{})
	if err != nil {
		t.Fatalf("NewSetupWithParams: %v", err)
	}
	requester, err := setup.Inputs.Values.Text("requester_id")
	if err != nil {
		t.Fatalf("requester_id: %v", err)
	}
	if requester == GoldenEnvironment().CurrentManagerID {
		t.Fatal("default requester equals the current manager; the golden scenario would trip separation of duties")
	}
}
