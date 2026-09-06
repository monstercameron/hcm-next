package time

import "testing"

func TestNewSetup_WiresAReadyToRunPlan(t *testing.T) {
	setup, err := NewSetup(GoldenEnvironment())
	if err != nil {
		t.Fatalf("NewSetup: %v", err)
	}
	if setup.Plan == nil || setup.Options.Capabilities == nil {
		t.Fatal("setup is missing its compiled plan or capability registry")
	}
	for _, input := range []string{"worker_id", "punch_id", "device_id", "reported_clock_skew_seconds"} {
		if _, ok := setup.Inputs.Values[input]; !ok {
			t.Errorf("setup declares no %s input", input)
		}
	}
}
