package migrate

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func wellFormedApproval() Approval {
	return Approval{
		PreviewDigest: "sha256:preview-fixture",
		Approver:      "principal:release-manager",
		Reason:        "REVIEWED_MIGRATION_PLAN",
		ApprovedAt:    time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC),
	}
}

func TestApproval_ValidateRefusesEachMissingField(t *testing.T) {
	cases := []struct {
		name string
		with func(*Approval)
	}{
		{"no preview digest", func(a *Approval) { a.PreviewDigest = "" }},
		{"no approver", func(a *Approval) { a.Approver = "" }},
		{"no reason", func(a *Approval) { a.Reason = "" }},
		{"no instant", func(a *Approval) { a.ApprovedAt = time.Time{} }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := wellFormedApproval()
			tc.with(&a)
			if err := a.validate(); CodeOf(err) != CodeInvalidRequest {
				t.Fatalf("code = %q, want %q (%v)", CodeOf(err), CodeInvalidRequest, err)
			}
		})
	}
}

func TestApproval_ValidateAcceptsAWellFormedApproval(t *testing.T) {
	if err := wellFormedApproval().validate(); err != nil {
		t.Fatalf("validate: %v", err)
	}
}

// TestRequest_ValidateDelegatesToApproval proves Request.validate() reaches
// Approval's own field checks -- a Request otherwise well formed still
// refuses when its embedded Approval is not.
func TestRequest_ValidateDelegatesToApproval(t *testing.T) {
	reg := registryForTest(t)
	plan := sourcePlanForTest(t, reg)
	req := Request{
		TenantID:   uuid.New(),
		InstanceID: uuid.New(),
		SourcePlan: plan, TargetPlan: plan,
		MigratedBy: "principal:migration-operator",
		MigratedAt: time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC),
		Approval:   Approval{},
	}
	if err := req.validate(); CodeOf(err) != CodeInvalidRequest {
		t.Fatalf("code = %q, want %q (%v)", CodeOf(err), CodeInvalidRequest, err)
	}
}
