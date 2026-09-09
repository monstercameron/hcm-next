package dsr

import (
	"errors"
	"fmt"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// ErrIdentityEvidenceInvalid is returned by [IdentityEvidence.Validate] when
// the evidence record itself is malformed, independent of whether it would
// satisfy any particular request's assurance floor.
var ErrIdentityEvidenceInvalid = errors.New("dsr: identity evidence is invalid")

// VerifyRefusalCode is the stable reason token
// [DataSubjectRequest.Verify] returns (alongside a non-nil error) when it
// refuses to move a request to [VerificationVerified].
type VerifyRefusalCode string

// Verify refusal codes.
const (
	VerifyRefusedNoEvidence          VerifyRefusalCode = "IDENTITY_EVIDENCE_MISSING"
	VerifyRefusedEvidenceExpired     VerifyRefusalCode = "IDENTITY_EVIDENCE_EXPIRED"
	VerifyRefusedCrossTenant         VerifyRefusalCode = "CROSS_TENANT_EVIDENCE"
	VerifyRefusedRepresentative      VerifyRefusalCode = "REPRESENTATIVE_NOT_AUTHORIZED"
	VerifyRefusedAssuranceBelowFloor VerifyRefusalCode = "ASSURANCE_BELOW_FLOOR"
)

// IdentityEvidence is the identity-assurance evidence
// [DataSubjectRequest.Verify] binds to a request. It is produced elsewhere
// (a federation/step-up verifier, e.g.
// internal/trust and internal/trust/stepup) and handed in as an opaque,
// already-evaluated reference -- this package never re-derives assurance
// itself, it only checks the evidence it is given against the request it
// is being bound to.
type IdentityEvidence struct {
	// Ref is an opaque, durable reference to the underlying evidence record
	// (e.g. a step-up proof digest or verifier evidence id). Never the raw
	// credential.
	Ref string
	// Tenant is the tenant this evidence was verified under. Verify refuses
	// to bind evidence from a different tenant than the request's own.
	Tenant values.TenantId
	// Assurance is the assurance level this evidence actually reached.
	Assurance trust.Assurance
	// VerifiedAt is when this evidence was evaluated (not necessarily when
	// Verify is called).
	VerifiedAt values.Instant
	// ExpiresAt is this evidence's own expiry, if any. The zero (unset)
	// value means no expiry is asserted. Verify treats VerifiedAt at or
	// after a set ExpiresAt as expired proof.
	ExpiresAt values.Instant
	// RepresentativeAuthorized must be true for Verify to accept evidence
	// bound to a request whose [SubjectClaims.RepresentativeRef] is set. It
	// is ignored when the request names no representative.
	RepresentativeAuthorized bool
}

// Validate reports whether e is well-formed, independent of any particular
// request it might be bound to.
func (e IdentityEvidence) Validate() error {
	if e.Ref == "" {
		return fmt.Errorf("%w: no reference", ErrIdentityEvidenceInvalid)
	}
	if err := e.Tenant.Validate(); err != nil {
		return fmt.Errorf("%w: %v", ErrIdentityEvidenceInvalid, err)
	}
	if e.Assurance == trust.AssuranceUnspecified {
		return fmt.Errorf("%w: unspecified assurance", ErrIdentityEvidenceInvalid)
	}
	if !e.VerifiedAt.IsSet() {
		return fmt.Errorf("%w: no verified_at", ErrIdentityEvidenceInvalid)
	}
	if e.ExpiresAt.IsSet() && !e.VerifiedAt.Before(e.ExpiresAt) {
		return fmt.Errorf("%w: verified_at is not before its own expires_at", ErrIdentityEvidenceInvalid)
	}
	return nil
}

// refused returns a copy of r moved to VerificationRefused, with a
// VERIFICATION_REFUSED evidence entry recorded, and the matching error.
func (r DataSubjectRequest) refused(at values.Instant, code VerifyRefusalCode) (DataSubjectRequest, error) {
	r.VerificationState = VerificationRefused
	r.IdentityEvidenceRef = ""
	r.IdentityAssurance = trust.AssuranceUnspecified
	r.VerifiedAt = values.Instant{}
	r = r.withEvidenceID()
	r = r.appendEvidence(EventVerificationRefused, at, string(code))
	return r, fmt.Errorf("dsr: request %q verification refused: %s", r.ID, code)
}

// Verify attempts to bind ev to r and move it to [VerificationVerified]. It
// refuses -- returning r moved to [VerificationRefused] plus a non-nil
// error naming a [VerifyRefusalCode] -- unless every one of the following
// holds:
//
//   - ev is itself well-formed ([IdentityEvidence.Validate]);
//   - ev.Tenant equals r.Tenant (never bind evidence verified under a
//     different tenant, however strong its assurance);
//   - ev.VerifiedAt is before ev.ExpiresAt, when ExpiresAt is set (expired
//     proof is refused even if it once met the floor);
//   - when r.Claims.RepresentativeRef is set, ev.RepresentativeAuthorized
//     is true (an evidence record that does not attest representative
//     authority cannot verify a request filed on someone else's behalf);
//   - ev.Assurance meets [AssuranceFloor] for r.Kind.
//
// Verify never mutates r; it returns a new record either way, and a
// refusal is itself recorded as [Evidence], not silently dropped.
func (r DataSubjectRequest) Verify(ev IdentityEvidence) (DataSubjectRequest, error) {
	if err := r.Validate(); err != nil {
		return r, fmt.Errorf("dsr: cannot verify an invalid request: %w", err)
	}
	// Classify an expired proof explicitly before validating the evidence as a
	// standalone record. IdentityEvidence.Validate must reject the same
	// malformed interval, but Verify has a more useful refusal code for this
	// request-time security decision.
	if ev.ExpiresAt.IsSet() && !ev.VerifiedAt.Before(ev.ExpiresAt) {
		return r.refused(ev.VerifiedAt, VerifyRefusedEvidenceExpired)
	}
	if err := ev.Validate(); err != nil {
		return r.refused(ev.VerifiedAt, VerifyRefusedNoEvidence)
	}
	if ev.Tenant != r.Tenant {
		return r.refused(ev.VerifiedAt, VerifyRefusedCrossTenant)
	}
	if ev.ExpiresAt.IsSet() && !ev.VerifiedAt.Before(ev.ExpiresAt) {
		return r.refused(ev.VerifiedAt, VerifyRefusedEvidenceExpired)
	}
	if r.Claims.RepresentativeRef != "" && !ev.RepresentativeAuthorized {
		return r.refused(ev.VerifiedAt, VerifyRefusedRepresentative)
	}
	floor := AssuranceFloor(r.Kind)
	if !ev.Assurance.AtLeast(floor) {
		return r.refused(ev.VerifiedAt, VerifyRefusedAssuranceBelowFloor)
	}

	r.VerificationState = VerificationVerified
	r.IdentityEvidenceRef = ev.Ref
	r.IdentityAssurance = ev.Assurance
	r.VerifiedAt = ev.VerifiedAt
	r = r.withEvidenceID()
	r = r.appendEvidence(EventVerified, ev.VerifiedAt, ev.Ref)

	if err := r.Validate(); err != nil {
		return r, err
	}
	return r, nil
}
