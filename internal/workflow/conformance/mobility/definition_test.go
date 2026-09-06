package mobility

import "testing"

func TestReferenceDefinition_CompilesUnderP1A(t *testing.T) {
	env := GoldenEnvironment()
	registry, err := env.Registry()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Compile(registry); err != nil {
		t.Fatalf("Compile: %v", err)
	}
}
