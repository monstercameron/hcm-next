package evolution

import (
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func incompatibleReport(t *testing.T) Report {
	t.Helper()
	r, err := CompatibilityCheck(promotionV1(), promotionV2RequiredInputAdded())
	if err != nil {
		t.Fatalf("unexpected error building fixture report: %v", err)
	}
	if r.OK() {
		t.Fatal("fixture report must be INCOMPATIBLE")
	}
	return r
}

func TestNewSupersessionRecord_Success(t *testing.T) {
	report := incompatibleReport(t)
	effective := mustInstant(2026, 9, 5)
	rec, err := NewSupersessionRecord(report, "adds mandatory compensation-committee sign-off",
		"author-1", "approver-1", effective, PolicyMigrateWithPreview)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rec.Previous != report.Previous || rec.Current != report.Current {
		t.Fatalf("record does not name the report's pair: %+v", rec)
	}
	if rec.CompatibilityVerdict != VerdictIncompatible {
		t.Fatalf("want recorded verdict INCOMPATIBLE, got %s", rec.CompatibilityVerdict)
	}
	if rec.Digest == "" {
		t.Fatal("digest was not minted")
	}
	if err := rec.Verify(); err != nil {
		t.Fatalf("freshly minted record failed to verify: %v", err)
	}
}

func TestNewSupersessionRecord_RequiresIncompatibleReport(t *testing.T) {
	compatible, err := CompatibilityCheck(promotionV1(), promotionV2CompatibleOptionalAdded())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_, err = NewSupersessionRecord(compatible, "reason", "author-1", "approver-1",
		mustInstant(2026, 9, 5), PolicyContinueOnOld)
	if !errors.Is(err, ErrSupersessionRequiresIncompatibility) {
		t.Fatalf("want ErrSupersessionRequiresIncompatibility, got %v", err)
	}
}

func TestNewSupersessionRecord_ApproverIsAuthor_IsRefused(t *testing.T) {
	report := incompatibleReport(t)
	_, err := NewSupersessionRecord(report, "reason", "same-principal", "same-principal",
		mustInstant(2026, 9, 5), PolicyDrain)
	if !errors.Is(err, ErrApproverIsAuthor) {
		t.Fatalf("want ErrApproverIsAuthor, got %v", err)
	}
}

func TestNewSupersessionRecord_MissingFields_AreRefused(t *testing.T) {
	report := incompatibleReport(t)
	effective := mustInstant(2026, 9, 5)
	cases := []struct {
		name     string
		reason   string
		author   string
		approver string
		instant  values.Instant
		policy   LiveInstancePolicy
	}{
		{"empty reason", "", "author-1", "approver-1", effective, PolicyDrain},
		{"empty author", "reason", "", "approver-1", effective, PolicyDrain},
		{"empty approver", "reason", "author-1", "", effective, PolicyDrain},
		{"unset instant", "reason", "author-1", "approver-1", values.Instant{}, PolicyDrain},
		{"unrecognized policy", "reason", "author-1", "approver-1", effective, LiveInstancePolicy("SOMETHING_ELSE")},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := NewSupersessionRecord(report, c.reason, c.author, c.approver, c.instant, c.policy)
			if !errors.Is(err, ErrInvalidSupersession) {
				t.Fatalf("want ErrInvalidSupersession, got %v", err)
			}
		})
	}
}

func TestSupersessionRecord_Verify_DetectsTampering(t *testing.T) {
	report := incompatibleReport(t)
	rec, err := NewSupersessionRecord(report, "reason", "author-1", "approver-1",
		mustInstant(2026, 9, 5), PolicyDrain)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	tampered := rec
	tampered.LiveInstancePolicy = PolicyContinueOnOld
	if err := tampered.Verify(); !errors.Is(err, ErrSupersessionTampered) {
		t.Fatalf("want ErrSupersessionTampered, got %v", err)
	}
	// The original, untouched value must still verify.
	if err := rec.Verify(); err != nil {
		t.Fatalf("original record must still verify: %v", err)
	}
}

func TestLiveInstancePolicy_Valid(t *testing.T) {
	for _, p := range []LiveInstancePolicy{PolicyContinueOnOld, PolicyMigrateWithPreview, PolicyDrain} {
		if !p.Valid() {
			t.Fatalf("%s should be valid", p)
		}
	}
	if LiveInstancePolicy("UNKNOWN").Valid() {
		t.Fatal("unknown policy should not be valid")
	}
}
