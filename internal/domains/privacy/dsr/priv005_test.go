package dsr

import (
	"testing"

	"github.com/monstercameron/hcm-next/internal/trust"
)

// TestTodo_PRIV_005 is the PRIMARY test for planning/todos.md PRIV-005:
// "Intake and verify a data-subject request."
//
// RED (todos.md PRIV-005): "unverified/expired proof, unauthorized
// representative, duplicate or cross-tenant subject creates executable
// request."
//
// GREEN (todos.md PRIV-005): "accepted ACCESS/RECTIFY/ERASE/RESTRICT/
// PORTABILITY/OBJECT/REVIEW request has immutable subject, deadline,
// jurisdiction, proof, scope, status and evidence; otherwise typed
// denial/clarification."
func TestTodo_PRIV_005(t *testing.T) {
	t.Run("GREEN: intake records an immutable claim with deadline, jurisdiction and evidence", func(t *testing.T) {
		req := fixtureRequest(t)

		if err := req.Validate(); err != nil {
			t.Fatalf("intake produced an invalid request: %v", err)
		}
		if req.Kind != KindAccess {
			t.Errorf("Kind = %s, want %s", req.Kind, KindAccess)
		}
		if req.Jurisdiction != fxJurisdiction() {
			t.Errorf("Jurisdiction = %+v, want %+v", req.Jurisdiction, fxJurisdiction())
		}
		if !req.Deadline.After(req.ReceivedAt) {
			t.Errorf("Deadline %s is not after ReceivedAt %s", req.Deadline, req.ReceivedAt)
		}
		if req.VerificationState != VerificationUnverified {
			t.Errorf("a freshly-intaken request's state = %s, want %s", req.VerificationState, VerificationUnverified)
		}
		if req.EvidenceID == "" || req.EvidenceID != requestEvidencePrefix+req.Digest() {
			t.Errorf("EvidenceID = %q, want it to match the record's own digest", req.EvidenceID)
		}
		if len(req.Trail) != 1 || req.Trail[0].Kind != EventIntake {
			t.Errorf("Trail = %+v, want exactly one INTAKE event", req.Trail)
		}
		if advance, code := req.CanAdvance(); advance {
			t.Errorf("an unverified request advanced (code=%s), want refused", code)
		}
	})

	t.Run("GREEN: verifying with assurance at the kind's floor makes the request advanceable", func(t *testing.T) {
		req := fixtureRequest(t)
		verified, err := req.Verify(fixtureEvidence(t, AssuranceFloor(KindAccess)))
		if err != nil {
			t.Fatalf("Verify: %v", err)
		}
		if verified.VerificationState != VerificationVerified {
			t.Fatalf("VerificationState = %s, want %s", verified.VerificationState, VerificationVerified)
		}
		if verified.IdentityEvidenceRef == "" {
			t.Errorf("IdentityEvidenceRef is empty after a successful Verify")
		}
		if advance, code := verified.CanAdvance(); !advance {
			t.Errorf("a verified, at-floor request could not advance: %s", code)
		}
		// The original record is untouched.
		if req.VerificationState != VerificationUnverified {
			t.Errorf("Verify mutated the receiver: original state = %s", req.VerificationState)
		}
	})

	t.Run("RED: no identity evidence at all refuses and does not advance", func(t *testing.T) {
		req := fixtureRequest(t)
		refused, err := req.Verify(IdentityEvidence{})
		if err == nil {
			t.Fatal("Verify(empty evidence) succeeded, want refusal")
		}
		if refused.VerificationState != VerificationRefused {
			t.Errorf("VerificationState = %s, want %s", refused.VerificationState, VerificationRefused)
		}
		if advance, _ := refused.CanAdvance(); advance {
			t.Error("a refused request advanced")
		}
	})

	t.Run("RED: expired proof refuses even at sufficient assurance", func(t *testing.T) {
		req := fixtureRequest(t)
		ev := fixtureEvidence(t, trust.AssuranceHigh)
		ev.ExpiresAt = mustInstant(t, fxVerifiedAt) // expires exactly at verified_at: already expired
		refused, err := req.Verify(ev)
		if err == nil {
			t.Fatal("Verify(expired proof) succeeded, want refusal")
		}
		if refused.VerificationState != VerificationRefused {
			t.Errorf("VerificationState = %s, want %s", refused.VerificationState, VerificationRefused)
		}
	})

	t.Run("RED: an unauthorized representative refuses", func(t *testing.T) {
		spec := fixtureIntakeSpec(t, "dsr-rep", KindAccess)
		spec.Claims.RepresentativeRef = "rep-legal-aid-1"
		req, err := Intake(spec, DefaultClockTable(), nil, fxWindow)
		if err != nil {
			t.Fatalf("Intake: %v", err)
		}
		ev := fixtureEvidence(t, trust.AssuranceHigh)
		ev.RepresentativeAuthorized = false
		refused, err := req.Verify(ev)
		if err == nil {
			t.Fatal("Verify(unauthorized representative) succeeded, want refusal")
		}
		if refused.VerificationState != VerificationRefused {
			t.Errorf("VerificationState = %s, want %s", refused.VerificationState, VerificationRefused)
		}

		// The same evidence, with representative authority attested, succeeds.
		ev.RepresentativeAuthorized = true
		verified, err := req.Verify(ev)
		if err != nil {
			t.Fatalf("Verify(authorized representative): %v", err)
		}
		if verified.VerificationState != VerificationVerified {
			t.Errorf("VerificationState = %s, want %s", verified.VerificationState, VerificationVerified)
		}
	})

	t.Run("RED: a cross-tenant identity evidence refuses, even at sufficient assurance", func(t *testing.T) {
		req := fixtureRequest(t)
		ev := fixtureEvidence(t, trust.AssuranceHigh)
		ev.Tenant = fxOtherTenant
		refused, err := req.Verify(ev)
		if err == nil {
			t.Fatal("Verify(cross-tenant evidence) succeeded, want refusal")
		}
		if refused.VerificationState != VerificationRefused {
			t.Errorf("VerificationState = %s, want %s", refused.VerificationState, VerificationRefused)
		}
	})

	t.Run("RED: a duplicate request within the window is linked, not independently executable", func(t *testing.T) {
		first := fixtureRequest(t)

		second, err := Intake(fixtureIntakeSpec(t, "dsr-2", KindAccess), DefaultClockTable(), []DataSubjectRequest{first}, fxWindow)
		if err != nil {
			t.Fatalf("Intake (second): %v", err)
		}
		if second.DuplicateOf != first.ID {
			t.Fatalf("DuplicateOf = %q, want %q", second.DuplicateOf, first.ID)
		}

		verified, err := second.Verify(fixtureEvidence(t, trust.AssuranceHigh))
		if err != nil {
			t.Fatalf("Verify: %v", err)
		}
		if advance, code := verified.CanAdvance(); advance {
			t.Errorf("a duplicate-linked request advanced (code=%s), want refused as %s", code, AdvanceDuplicateNotPrimary)
		} else if code != AdvanceDuplicateNotPrimary {
			t.Errorf("CanAdvance code = %s, want %s", code, AdvanceDuplicateNotPrimary)
		}
	})

	t.Run("RED: a cross-tenant subject is never linked as a duplicate", func(t *testing.T) {
		first := fixtureRequest(t)

		otherTenantSpec := fixtureIntakeSpec(t, "dsr-3", KindAccess)
		otherTenantSpec.Tenant = fxOtherTenant
		second, err := Intake(otherTenantSpec, DefaultClockTable(), []DataSubjectRequest{first}, fxWindow)
		if err != nil {
			t.Fatalf("Intake (other tenant): %v", err)
		}
		if second.DuplicateOf != "" {
			t.Errorf("DuplicateOf = %q, want empty (different tenant must never link)", second.DuplicateOf)
		}
	})

	t.Run("otherwise: an assurance level below the kind's floor is a typed denial, not a downgraded accept", func(t *testing.T) {
		spec := fixtureIntakeSpec(t, "dsr-erase", KindErasure)
		req, err := Intake(spec, DefaultClockTable(), nil, fxWindow)
		if err != nil {
			t.Fatalf("Intake: %v", err)
		}
		// Substantial is enough for ACCESS but not for ERASURE.
		refused, err := req.Verify(fixtureEvidence(t, trust.AssuranceSubstantial))
		if err == nil {
			t.Fatal("Verify(erasure, substantial assurance) succeeded, want refusal")
		}
		if refused.VerificationState != VerificationRefused {
			t.Errorf("VerificationState = %s, want %s", refused.VerificationState, VerificationRefused)
		}
		if len(refused.Trail) == 0 || refused.Trail[len(refused.Trail)-1].Detail != string(VerifyRefusedAssuranceBelowFloor) {
			t.Errorf("Trail does not record a typed %s denial: %+v", VerifyRefusedAssuranceBelowFloor, refused.Trail)
		}
	})
}
