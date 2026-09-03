package eligibility_test

import (
	"testing"

	"github.com/monstercameron/hcm-next/internal/engines/eligibility"
)

// TestVersionIsStable is the ARCH-GO-009 engine package contract test for
// eligibility's Version(): it reports a fixed, positive contract version
// with no dependency on any Request, CompiledPlan or Result.
func TestVersionIsStable(t *testing.T) {
	if v := eligibility.Version(); v != eligibility.Version() || v <= 0 {
		t.Fatalf("Version() = %d, want a stable positive contract version", v)
	}
}
