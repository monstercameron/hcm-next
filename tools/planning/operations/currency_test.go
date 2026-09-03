package operations_test

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/monstercameron/hcm-next/tools/planning/operations"
)

var ops010Now = time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)

func loadOperatingEvidence(t *testing.T) *operations.OperatingEvidenceRegistry {
	t.Helper()
	registry, err := operations.LoadOperatingEvidence(filepath.Join(repoRoot(t), filepath.FromSlash(operations.DefaultOperatingEvidencePath)))
	if err != nil {
		t.Fatalf("load operating evidence: %v", err)
	}
	return registry
}

// TestTodo_OPS_010 is the operating-evidence currency gate. It contains both
// the clean registry path and the required seeded expired-evidence defect: a
// stale proof must reject without producing any authority/effect output.
func TestTodo_OPS_010(t *testing.T) {
	t.Run("GREEN: every required proof is current at the deterministic clock", func(t *testing.T) {
		result := operations.ValidateEvidenceCurrency(loadOperatingEvidence(t), ops010Now)
		if !result.Ready() {
			t.Fatalf("OPS-010 result = %s, want READY: %v", result.Status, result.Diagnostics)
		}
		if !result.Effects.Empty() {
			t.Fatalf("currency check had effects: %+v", result.Effects)
		}
		wantKinds := map[string]bool{
			"OWNERSHIP": true, "RUNBOOK": true, "DEPENDENCY": true, "RESTORE": true,
			"DRILL": true, "VENDOR": true, "CONTROL": true,
		}
		for _, assessment := range result.Assessments {
			if assessment.State != operations.EvidenceCurrent || !assessment.GateAllowed {
				t.Errorf("assessment %+v is not a current allowed proof", assessment)
			}
			delete(wantKinds, assessment.Kind)
		}
		if len(wantKinds) != 0 {
			t.Errorf("missing required proof kinds: %v", wantKinds)
		}
	})

	t.Run("RED: expired operating evidence blocks the named gate with zero effects", func(t *testing.T) {
		registry := loadOperatingEvidence(t)
		registry.Evidence[1].VerifiedAt = "2026-09-01T00:00:00Z"
		registry.Evidence[1].DueAt = "2026-09-02T23:00:00Z"
		registry.Evidence[1].ExpiresAt = "2026-09-02T23:59:59Z"
		result := operations.ValidateEvidenceCurrency(registry, ops010Now)
		if result.Status != operations.StatusEvidenceRejected {
			t.Fatalf("expired evidence returned %s, want %s", result.Status, operations.StatusEvidenceRejected)
		}
		if !result.Effects.Empty() {
			t.Fatalf("expired evidence had effects: %+v", result.Effects)
		}
		mustFindCurrencyDiagnostic(t, result, registry.Evidence[1].ID, "OPERATING_READINESS", "expires_at", operations.EvidenceExpired, "v1")
	})

	t.Run("RED: due evidence blocks until an active signed compensating-control waiver exists", func(t *testing.T) {
		registry := loadOperatingEvidence(t)
		registry.Evidence[2].DueAt = "2026-09-03T01:00:00Z"
		result := operations.ValidateEvidenceCurrency(registry, ops010Now)
		if result.Status != operations.StatusEvidenceRejected {
			t.Fatalf("due evidence returned %s, want %s", result.Status, operations.StatusEvidenceRejected)
		}
		mustFindCurrencyDiagnostic(t, result, registry.Evidence[2].ID, "OPERATING_READINESS", "expires_at", operations.EvidenceDue, "v1")

		registry.Evidence[2].Waiver = &operations.EvidenceWaiver{
			ID: "ops010-temporary-dependency-waiver", SignedBy: "platform-foundation",
			SignedAt: "2026-09-03T11:00:00Z", SignatureRef: "signature://ops010/dependency/v1",
			Reason: "renewal review scheduled", CompensatingControl: "daily dependency-owner review",
			ExpiresAt: "2026-09-04T12:00:00Z",
		}
		result = operations.ValidateEvidenceCurrency(registry, ops010Now)
		if !result.Ready() {
			t.Fatalf("valid due-evidence waiver returned %s: %v", result.Status, result.Diagnostics)
		}
		assessment := findAssessment(t, result, registry.Evidence[2].ID)
		if assessment.State != operations.EvidenceDue || !assessment.GateAllowed || assessment.WaiverID == "" {
			t.Fatalf("due evidence assessment = %+v, want DUE, allowed, and attributable waiver", assessment)
		}
	})
}

// TestTodo_OPS_010_Recovery proves a stale proof cannot be carried over by an
// expired waiver, and that publishing a fresh verification restores readiness
// without a side effect or a hidden dependency on process time.
func TestTodo_OPS_010_Recovery(t *testing.T) {
	registry := loadOperatingEvidence(t)
	entry := &registry.Evidence[3]
	entry.VerifiedAt = "2026-09-01T00:00:00Z"
	entry.DueAt = "2026-09-02T23:00:00Z"
	entry.ExpiresAt = "2026-09-02T23:59:59Z"
	entry.Waiver = &operations.EvidenceWaiver{
		ID: "ops010-expired-restore-waiver", SignedBy: "platform-foundation",
		SignedAt: "2026-09-02T00:00:00Z", SignatureRef: "signature://ops010/restore/v1",
		Reason: "restore renewal in progress", CompensatingControl: "restore owner reviews daily",
		ExpiresAt: "2026-09-03T11:59:59Z",
	}

	blocked := operations.ValidateEvidenceCurrency(registry, ops010Now)
	if blocked.Status != operations.StatusEvidenceRejected {
		t.Fatalf("expired waiver returned %s, want %s", blocked.Status, operations.StatusEvidenceRejected)
	}
	mustFindCurrencyDiagnostic(t, blocked, entry.ID, "OPERATING_READINESS", "waiver.expires_at", operations.EvidenceExpired, "v1")
	if !blocked.Effects.Empty() {
		t.Fatalf("expired waiver had effects: %+v", blocked.Effects)
	}

	entry.Waiver = nil
	entry.VerifiedAt = "2026-09-03T12:00:00Z"
	entry.DueAt = "2026-12-01T00:00:00Z"
	entry.ExpiresAt = "2026-12-31T23:59:59Z"
	recovered := operations.ValidateEvidenceCurrency(registry, ops010Now)
	if !recovered.Ready() {
		t.Fatalf("fresh restored evidence returned %s: %v", recovered.Status, recovered.Diagnostics)
	}
	assessment := findAssessment(t, recovered, entry.ID)
	if assessment.State != operations.EvidenceCurrent || !assessment.GateAllowed || assessment.WaiverID != "" {
		t.Fatalf("recovered assessment = %+v, want current, allowed, no waiver", assessment)
	}
	if !recovered.Effects.Empty() {
		t.Fatalf("recovery had effects: %+v", recovered.Effects)
	}
}

func mustFindCurrencyDiagnostic(t *testing.T, result operations.CurrencyResult, evidenceID, gate, field, state, version string) {
	t.Helper()
	for _, diagnostic := range result.Diagnostics {
		if diagnostic.EvidenceID == evidenceID && diagnostic.Gate == gate && diagnostic.Field == field && diagnostic.State == state && diagnostic.Version == version {
			return
		}
	}
	t.Fatalf("no diagnostic evidence=%s gate=%s field=%s state=%s version=%s in %v", evidenceID, gate, field, state, version, result.Diagnostics)
}

func findAssessment(t *testing.T, result operations.CurrencyResult, evidenceID string) operations.EvidenceAssessment {
	t.Helper()
	for _, assessment := range result.Assessments {
		if assessment.EvidenceID == evidenceID {
			return assessment
		}
	}
	t.Fatalf("no assessment for %s in %v", evidenceID, result.Assessments)
	return operations.EvidenceAssessment{}
}
