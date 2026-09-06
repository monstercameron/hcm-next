package talent

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

// TestNewSetupWithParams_OmitsThePolicyContextSnapshot proves
// OmitPolicyContext actually omits PolicyContext rather than merely naming
// that it does.
func TestNewSetupWithParams_OmitsThePolicyContextSnapshot(t *testing.T) {
	setup, err := NewSetupWithParams(GoldenEnvironment(), Params{OmitPolicyContext: true})
	if err != nil {
		t.Fatalf("NewSetupWithParams: %v", err)
	}
	if _, ok := setup.Inputs.Context["PolicyContext"]; ok {
		t.Fatal("setup carries a PolicyContext snapshot; the RED case requires omitting it")
	}
}

// TestNewSetupWithParams_DefaultsRatingAuthorToTheRaterOfRecord proves the
// golden Params never accidentally trip the authority-mismatch check.
func TestNewSetupWithParams_DefaultsRatingAuthorToTheRaterOfRecord(t *testing.T) {
	env := GoldenEnvironment()
	setup, err := NewSetupWithParams(env, Params{})
	if err != nil {
		t.Fatalf("NewSetupWithParams: %v", err)
	}
	author, err := setup.Inputs.Values.Text("rating_author_id")
	if err != nil {
		t.Fatalf("rating_author_id: %v", err)
	}
	if author != env.RaterID {
		t.Fatalf("default rating author = %q, want the environment's own rater of record %q", author, env.RaterID)
	}
}
