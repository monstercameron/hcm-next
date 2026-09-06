package dsr

import (
	"testing"

	"github.com/monstercameron/hcm-next/internal/trust"
)

func TestDataSubjectRequest_ValidateRejectsInconsistentVerificationState(t *testing.T) {
	valid := fixtureRequest(t)

	claimsVerifiedWithoutEvidence := valid
	claimsVerifiedWithoutEvidence.VerificationState = VerificationVerified
	if err := claimsVerifiedWithoutEvidence.Validate(); err == nil {
		t.Error("a request claiming VERIFIED with no evidence ref validated")
	}

	unverifiedWithEvidence := valid
	unverifiedWithEvidence.IdentityEvidenceRef = "ev:something"
	if err := unverifiedWithEvidence.Validate(); err == nil {
		t.Error("an UNVERIFIED request carrying an evidence ref validated")
	}

	unverifiedWithAssurance := valid
	unverifiedWithAssurance.IdentityAssurance = trust.AssuranceHigh
	if err := unverifiedWithAssurance.Validate(); err == nil {
		t.Error("an UNVERIFIED request carrying a specified assurance validated")
	}
}

func TestDataSubjectRequest_ValidateRejectsTamperedEvidenceID(t *testing.T) {
	req := fixtureRequest(t)
	req.EvidenceID = "ev:privacy:dsr:0000000000000000000000000000000000000000000000000000000000000000"
	if err := req.Validate(); err == nil {
		t.Fatal("a request with an EvidenceID that does not match its own digest validated")
	}
}

func TestDataSubjectRequest_ValidateRejectsDeadlineBeforeReceivedAt(t *testing.T) {
	req := fixtureRequest(t)
	req.Deadline = mustInstant(t, fxReceivedAt-1)
	req = req.withEvidenceID()
	if err := req.Validate(); err == nil {
		t.Fatal("a request whose deadline precedes received_at validated")
	}
}

func TestDataSubjectRequest_CanAdvanceRefusesAnInvalidRecord(t *testing.T) {
	req := fixtureRequest(t)
	req.Kind = "" // now fails Validate
	if advance, code := req.CanAdvance(); advance || code != AdvanceRecordInvalid {
		t.Fatalf("CanAdvance() on an invalid record = (%v, %s), want (false, %s)", advance, code, AdvanceRecordInvalid)
	}
}
