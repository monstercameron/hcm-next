package time

import "testing"

func TestReferenceDefinition_CompilesUnderP1A(t *testing.T) {
	registry, err := GoldenEnvironment().Registry()
	if err != nil {
		t.Fatalf("Registry: %v", err)
	}
	plan, err := Compile(registry)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if plan.WorkflowID != WorkflowID || plan.Version != Version {
		t.Fatalf("compiled identity = %s/%d, want %s/%d", plan.WorkflowID, plan.Version, WorkflowID, Version)
	}
}
