package replan

import "testing"

func TestVersionIsTheEngineContractVersion(t *testing.T) {
	if got := Version(); got != 1 {
		t.Fatalf("Version() = %d, want 1", got)
	}
}
