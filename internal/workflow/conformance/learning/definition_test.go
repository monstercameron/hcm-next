package learning

import "testing"

// TestReferenceDefinition_CompilesUnderP1A is a smoke check that the
// definition this file assembles compiles at all. The full walk, terminal
// lattice and RED/GREEN semantics are conformance_test.go's job.
func TestReferenceDefinition_CompilesUnderP1A(t *testing.T) {
	env := GoldenEnvironment()
	registry, err := env.Registry()
	if err != nil {
		t.Fatalf("Registry: %v", err)
	}
	if _, err := Compile(registry); err != nil {
		t.Fatalf("Compile: %v", err)
	}
}
