package scopeexchange

import (
	"testing"

	"github.com/monstercameron/hcm-next/tools/planning/manifest"
)

func fundedManifest() manifest.Manifest {
	deps := []string{"GOV-001"}
	return manifest.Manifest{
		ID:                 "GATE-ADD-001",
		Owner:              "TERRA",
		Estimate:           "2w",
		Gate:               "GATE_A",
		Dependencies:       &deps,
		AcceptanceEvidence: "TestSomething PASS",
		OperationsOwner:    "platform-ops",
		DisplacedScope:     "removed LEGACY-042 (equivalent effort)",
	}
}

func fundedExchange() Exchange {
	return Exchange{
		Requirement:         fundedManifest(),
		ChangedCriticalPath: "critical path unchanged: LEGACY-042 was off the critical path",
		ApprovalDigest:      "sha256:deadbeef",
	}
}

// TestScopeExchangeRejectsUnfundedAddition is the primary red/green test
// for GOV-006.
func TestScopeExchangeRejectsUnfundedAddition(t *testing.T) {
	t.Run("Gate A addition lacking owner is rejected", func(t *testing.T) {
		ex := fundedExchange()
		ex.Requirement.Owner = ""
		assertHasIssue(t, Validate(ex), "missing gate owner")
	})
	t.Run("Gate B addition lacking schedule impact is rejected", func(t *testing.T) {
		ex := fundedExchange()
		ex.Requirement.Gate = "GATE_B"
		ex.Requirement.Estimate = ""
		assertHasIssue(t, Validate(ex), "missing staff/schedule impact")
	})
	t.Run("addition lacking acceptance evidence is rejected", func(t *testing.T) {
		ex := fundedExchange()
		ex.Requirement.AcceptanceEvidence = ""
		assertHasIssue(t, Validate(ex), "missing acceptance evidence")
	})
	t.Run("addition lacking equivalent displaced scope is rejected", func(t *testing.T) {
		ex := fundedExchange()
		ex.Requirement.DisplacedScope = ""
		assertHasIssue(t, Validate(ex), "missing equivalent scope removed or deferred")
	})
	t.Run("addition lacking changed critical path record is rejected", func(t *testing.T) {
		ex := fundedExchange()
		ex.ChangedCriticalPath = ""
		assertHasIssue(t, Validate(ex), "approved exchange must record the changed critical path")
	})
	t.Run("addition lacking approval digest is rejected", func(t *testing.T) {
		ex := fundedExchange()
		ex.ApprovalDigest = ""
		assertHasIssue(t, Validate(ex), "approved exchange must record an approval digest")
	})
	t.Run("Gate P0 additions are outside the scope-exchange rule", func(t *testing.T) {
		ex := Exchange{Requirement: manifest.Manifest{ID: "P0-001", Gate: "P0"}}
		if v := Validate(ex); len(v) != 0 {
			t.Errorf("expected zero violations for a non-Gate A/B requirement, got %v", v)
		}
	})
	t.Run("a DEFINED long-term contract alone does not fund an addition", func(t *testing.T) {
		// A manifest that only claims maturity ("DEFINED") without owner,
		// schedule, evidence or displaced scope must still be rejected -
		// maturity never substitutes for a funded exchange.
		ex := Exchange{
			Requirement: manifest.Manifest{ID: "UNFUNDED-001", Gate: "GATE_A"},
		}
		violations := Validate(ex)
		if len(violations) == 0 {
			t.Fatal("expected an unfunded Gate A addition to be rejected")
		}
	})
	t.Run("fully funded and approved exchange has zero violations", func(t *testing.T) {
		if v := Validate(fundedExchange()); len(v) != 0 {
			t.Errorf("expected zero violations for a fully funded exchange, got %v", v)
		}
	})
}

func assertHasIssue(t *testing.T, violations []Violation, issue string) {
	t.Helper()
	for _, v := range violations {
		if v.Issue == issue {
			return
		}
	}
	t.Errorf("expected a violation with issue %q, got %v", issue, violations)
}

// TestTodo_GOV_006_Golden pins the exact violation format for a fixed
// unfunded fixture.
func TestTodo_GOV_006_Golden(t *testing.T) {
	ex := Exchange{Requirement: manifest.Manifest{ID: "GOLDEN-006", Gate: "GATE_B"}}
	violations := Validate(ex)

	want := []string{
		"GOLDEN-006: owner: missing gate owner",
		"GOLDEN-006: estimate: missing staff/schedule impact",
		"GOLDEN-006: dependencies: missing dependency",
		"GOLDEN-006: acceptance_evidence: missing acceptance evidence",
		"GOLDEN-006: operations_owner: missing operations owner",
		"GOLDEN-006: displaced_scope: missing equivalent scope removed or deferred",
		"GOLDEN-006: changed_critical_path: approved exchange must record the changed critical path",
		"GOLDEN-006: approval_digest: approved exchange must record an approval digest",
	}
	if len(violations) != len(want) {
		t.Fatalf("got %d violations, want %d:\n%v", len(violations), len(want), violations)
	}
	for i, w := range want {
		if got := violations[i].String(); got != w {
			t.Errorf("violation %d changed:\n got:  %s\n want: %s", i, got, w)
		}
	}
}
