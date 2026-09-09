package simcontract_test

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/promotion/simcontract"
)

// TestSectionsAreTwelveAndDistinct pins doc.go's "twelve declared sections"
// claim against the declaration itself.
func TestSectionsAreTwelveAndDistinct(t *testing.T) {
	sections := simcontract.Sections()
	if len(sections) != 12 {
		t.Fatalf("the package declares %d sections, want 12: %v", len(sections), sections)
	}
	seen := map[simcontract.SectionKind]bool{}
	for _, s := range sections {
		if seen[s] {
			t.Fatalf("section %q is declared twice", s)
		}
		seen[s] = true
		if s.String() == "" {
			t.Fatalf("section %q has no wire token", s)
		}
	}
}

// TestSectionsIsACopy proves the declaration cannot be edited through its own
// accessor.
func TestSectionsIsACopy(t *testing.T) {
	first := simcontract.Sections()
	first[0] = "tampered"
	if simcontract.Sections()[0] == "tampered" {
		t.Fatal("Sections returns the package's own slice")
	}
}

// TestEffectSimulatedNotExecutedIsTheSoleStatus pins doc.go's "every side
// effect is stamped SIMULATED/NOT_EXECUTED" claim.
func TestEffectSimulatedNotExecutedIsTheSoleStatus(t *testing.T) {
	if simcontract.EffectSimulatedNotExecuted.String() != "SIMULATED/NOT_EXECUTED" {
		t.Fatalf("EffectSimulatedNotExecuted = %q, want %q",
			simcontract.EffectSimulatedNotExecuted, "SIMULATED/NOT_EXECUTED")
	}
}

// TestResultStatusIsTwoValuedAndDistinct pins the closed result-status
// vocabulary.
func TestResultStatusIsTwoValuedAndDistinct(t *testing.T) {
	if !simcontract.ResultExecutableAsSimulated.Valid() {
		t.Fatal("ResultExecutableAsSimulated does not validate")
	}
	if !simcontract.ResultBlocked.Valid() {
		t.Fatal("ResultBlocked does not validate")
	}
	if simcontract.ResultExecutableAsSimulated == simcontract.ResultBlocked {
		t.Fatal("the two declared result statuses are not distinct")
	}
	if simcontract.ResultStatus("MAYBE").Valid() {
		t.Fatal("an undeclared result status validated")
	}
	if simcontract.ResultStatusUnspecified.Valid() {
		t.Fatal("the zero-value result status validated")
	}
}
