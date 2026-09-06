package benefits

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

// TestNewSetupWithParams_PriorElectionDigestIsHonored proves the constructor
// actually threads the prior-election-digest override into the workflow
// input rather than merely naming that it does.
func TestNewSetupWithParams_PriorElectionDigestIsHonored(t *testing.T) {
	setup, err := NewSetupWithParams(GoldenEnvironment(), Params{PriorElectionDigest: "sha256:prior"})
	if err != nil {
		t.Fatalf("NewSetupWithParams: %v", err)
	}
	v, err := setup.Inputs.Values.Get("prior_election_digest")
	if err != nil {
		t.Fatalf("prior_election_digest: %v", err)
	}
	if v.Text != "sha256:prior" {
		t.Fatalf("prior_election_digest = %q, want %q", v.Text, "sha256:prior")
	}
}
