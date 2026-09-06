package hrcase

import "testing"

func TestReferenceDefinition_CompilesUnderP1A(t *testing.T) {
	registry, err := GoldenEnvironment().Registry()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Compile(registry); err != nil {
		t.Fatalf("Compile: %v", err)
	}
}
