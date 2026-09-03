package manifest

import (
	"testing"
)

func completeManifest() Manifest {
	deps := []string{"GOV-002", "GOV-001"}
	return Manifest{
		ID:                 "GOV-999",
		Owner:              "TERRA",
		Estimate:           "3d",
		Gate:               "GATE_A",
		Dependencies:       &deps,
		AcceptanceEvidence: "TestSomething PASS",
		OperationsOwner:    "platform-ops",
		DisplacedScope:     "none",
	}
}

// TestDeliveryManifestRejectsMissingRequiredFields is the primary red/green
// test for GOV-001.
func TestDeliveryManifestRejectsMissingRequiredFields(t *testing.T) {
	t.Run("missing owner", func(t *testing.T) {
		m := completeManifest()
		m.Owner = ""
		assertHasViolation(t, Validate(m), "owner")
	})
	t.Run("missing estimate", func(t *testing.T) {
		m := completeManifest()
		m.Estimate = ""
		assertHasViolation(t, Validate(m), "estimate")
	})
	t.Run("missing gate", func(t *testing.T) {
		m := completeManifest()
		m.Gate = ""
		assertHasViolation(t, Validate(m), "gate")
	})
	t.Run("missing dependencies", func(t *testing.T) {
		m := completeManifest()
		m.Dependencies = nil
		assertHasViolation(t, Validate(m), "dependencies")
	})
	t.Run("explicit no dependencies is not a violation", func(t *testing.T) {
		m := completeManifest()
		none := []string{}
		m.Dependencies = &none
		if violations := Validate(m); len(violations) != 0 {
			t.Errorf("expected zero violations for explicit empty dependencies, got %v", violations)
		}
	})
	t.Run("missing acceptance evidence", func(t *testing.T) {
		m := completeManifest()
		m.AcceptanceEvidence = ""
		assertHasViolation(t, Validate(m), "acceptance_evidence")
	})
	t.Run("missing operations owner", func(t *testing.T) {
		m := completeManifest()
		m.OperationsOwner = ""
		assertHasViolation(t, Validate(m), "operations_owner")
	})
	t.Run("missing displaced scope", func(t *testing.T) {
		m := completeManifest()
		m.DisplacedScope = ""
		assertHasViolation(t, Validate(m), "displaced_scope")
	})
	t.Run("all fields missing reports all seven violations", func(t *testing.T) {
		violations := Validate(Manifest{})
		if len(violations) != 7 {
			t.Fatalf("expected 7 violations for an empty manifest, got %d: %v", len(violations), violations)
		}
	})
	t.Run("complete fixture validates with zero violations and a stable digest", func(t *testing.T) {
		m := completeManifest()
		if violations := Validate(m); len(violations) != 0 {
			t.Fatalf("expected zero violations, got %v", violations)
		}
		d1, err := CanonicalDigest(m)
		if err != nil {
			t.Fatalf("CanonicalDigest: %v", err)
		}
		d2, err := CanonicalDigest(m)
		if err != nil {
			t.Fatalf("CanonicalDigest: %v", err)
		}
		if d1 != d2 {
			t.Errorf("digest is not stable across calls: %s != %s", d1, d2)
		}

		// Reordering Dependencies must not change the digest.
		reordered := completeManifest()
		swapped := []string{"GOV-001", "GOV-002"}
		reordered.Dependencies = &swapped
		d3, err := CanonicalDigest(reordered)
		if err != nil {
			t.Fatalf("CanonicalDigest: %v", err)
		}
		if d1 != d3 {
			t.Errorf("digest changed when only dependency order changed: %s != %s", d1, d3)
		}
	})
}

func assertHasViolation(t *testing.T, violations []Violation, field string) {
	t.Helper()
	for _, v := range violations {
		if v.Field == field {
			return
		}
	}
	t.Errorf("expected a violation for field %q, got %v", field, violations)
}

// TestTodo_GOV_001_Golden pins the canonical digest of a fixed manifest
// fixture. If this ever fails, the digest algorithm changed observable
// behavior and every consumer that stores a digest must be re-evaluated
// deliberately - it must never drift silently under refactor.
func TestTodo_GOV_001_Golden(t *testing.T) {
	deps := []string{"GOV-001"}
	m := Manifest{
		ID:                 "GOV-GOLDEN",
		Owner:              "LUNA",
		Estimate:           "1d",
		Gate:               "P0",
		Dependencies:       &deps,
		AcceptanceEvidence: "TestGoldenFixture PASS",
		OperationsOwner:    "governance",
		DisplacedScope:     "none",
	}

	const wantDigest = "b01c2feb19f04cfe59f328459365e38d865afd4d43ba73da88d1d4f91a59898d"

	got, err := CanonicalDigest(m)
	if err != nil {
		t.Fatalf("CanonicalDigest: %v", err)
	}
	if got != wantDigest {
		t.Errorf("canonical digest changed:\n got:  %s\n want: %s\nUpdate wantDigest deliberately if this change is intended.", got, wantDigest)
	}
}
