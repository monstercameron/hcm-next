package prototype

import (
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork"
)

var requirementAt = time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)

func TestCompileApprovalRequirementBindsTheApproverAndTheDeadline(t *testing.T) {
	req, err := CompileApprovalRequirement("principal:promotion-approver", requirementAt.Add(48*time.Hour))
	if err != nil {
		t.Fatalf("CompileApprovalRequirement: %v", err)
	}
	if req.RequirementID != ApprovalRequirementID || req.Revision != 1 {
		t.Fatalf("identity = %s/%d, want %s/1", req.RequirementID, req.Revision, ApprovalRequirementID)
	}
	if req.Digest() == "" || req.ExpressionDigest == "" {
		t.Fatalf("a compiled requirement must carry both digests: %+v", req)
	}
	if req.Candidates.Kind != humanwork.ExprNamed || req.Candidates.PrincipalID != "principal:promotion-approver" {
		t.Fatalf("candidates = %+v, want the named approver", req.Candidates)
	}
	if req.Quorum.MinApprovals != 1 {
		t.Fatalf("quorum = %d, want 1", req.Quorum.MinApprovals)
	}
	want := requirementAt.Add(48 * time.Hour)
	if !req.Deadline.DecideBy.Time().Equal(want) || !req.Deadline.Expiry.Time().Equal(want) {
		t.Fatalf("deadline = %s/%s, want %s for both", req.Deadline.DecideBy, req.Deadline.Expiry, want)
	}
	if req.Source.GovernancePolicyRef != ApprovalGovernancePolicyRef {
		t.Fatalf("governance policy = %q, want %q", req.Source.GovernancePolicyRef, ApprovalGovernancePolicyRef)
	}
}

// The digest is what the routed assignment records and what the resume
// rebuilds, so it must be a pure function of the two inputs, and it must not
// depend on sub-second detail a database round trip would lose.
func TestCompileApprovalRequirementDigestIsStableAcrossADatabaseRoundTrip(t *testing.T) {
	deadline := requirementAt.Add(48 * time.Hour)
	routed, err := CompileApprovalRequirement("principal:a", deadline.Add(750*time.Millisecond+999*time.Nanosecond))
	if err != nil {
		t.Fatalf("CompileApprovalRequirement(routed): %v", err)
	}
	// What comes back from timestamptz: microsecond precision at best, and
	// here the whole-second value the routed side actually stored.
	rebuilt, err := CompileApprovalRequirement("principal:a", routed.Deadline.Expiry.Time())
	if err != nil {
		t.Fatalf("CompileApprovalRequirement(rebuilt): %v", err)
	}
	if routed.Digest() != rebuilt.Digest() {
		t.Fatalf("digest changed across the round trip: %s vs %s", routed.Digest(), rebuilt.Digest())
	}
	if !routed.Deadline.Expiry.Time().Equal(deadline) {
		t.Fatalf("deadline = %s, want it truncated to the second %s", routed.Deadline.Expiry, deadline)
	}
}

func TestCompileApprovalRequirementDistinguishesApproversAndDeadlines(t *testing.T) {
	base, err := CompileApprovalRequirement("principal:a", requirementAt)
	if err != nil {
		t.Fatalf("CompileApprovalRequirement: %v", err)
	}
	otherApprover, err := CompileApprovalRequirement("principal:b", requirementAt)
	if err != nil {
		t.Fatalf("CompileApprovalRequirement(other approver): %v", err)
	}
	otherDeadline, err := CompileApprovalRequirement("principal:a", requirementAt.Add(time.Second))
	if err != nil {
		t.Fatalf("CompileApprovalRequirement(other deadline): %v", err)
	}
	if base.Digest() == otherApprover.Digest() {
		t.Fatal("two approvers compiled to the same requirement digest")
	}
	if base.Digest() == otherDeadline.Digest() {
		t.Fatal("two deadlines compiled to the same requirement digest")
	}
}

func TestCompileApprovalRequirementRefusesMissingInputs(t *testing.T) {
	if _, err := CompileApprovalRequirement("", requirementAt); err == nil {
		t.Fatal("an empty approver must be refused")
	}
	if _, err := CompileApprovalRequirement("principal:a", time.Time{}); err == nil {
		t.Fatal("a zero deadline must be refused")
	}
}
