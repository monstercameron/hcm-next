package evolution

import (
	"strings"
	"testing"
)

func TestReport_Explain_NamesVerdictAndFields(t *testing.T) {
	r, err := CompatibilityCheck(promotionV1(), promotionV2RequiredInputAdded())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := r.Explain()
	for _, want := range []string{string(VerdictIncompatible), "compensation_committee_approval_ref", string(ChangeRequiredInputAdded)} {
		if !strings.Contains(got, want) {
			t.Fatalf("Explain() = %q, want it to contain %q", got, want)
		}
	}
}

func TestReport_Explain_NoDifferences(t *testing.T) {
	r, err := CompatibilityCheck(promotionV1(), promotionBase(2))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := r.Explain()
	if !strings.Contains(got, "no differences") {
		t.Fatalf("Explain() = %q, want it to note no differences", got)
	}
}

func TestSupersessionRecord_Explain_NamesPartiesAndPolicy(t *testing.T) {
	report := incompatibleReport(t)
	rec, err := NewSupersessionRecord(report, "adds mandatory sign-off", "author-1", "approver-1",
		mustInstant(2026, 9, 5), PolicyMigrateWithPreview)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := rec.Explain()
	for _, want := range []string{"author-1", "approver-1", string(PolicyMigrateWithPreview), rec.Digest} {
		if !strings.Contains(got, want) {
			t.Fatalf("Explain() = %q, want it to contain %q", got, want)
		}
	}
}
