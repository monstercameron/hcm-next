package dsr

import (
	"errors"
	"fmt"
	"slices"

	"github.com/monstercameron/human-capital-management-suite/internal/governance/legal"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// ErrRequestInvalid is returned by [DataSubjectRequest.Validate] when a
// record is missing a required field or its state is internally
// inconsistent (e.g. claiming [VerificationVerified] without evidence).
var ErrRequestInvalid = errors.New("dsr: data subject request fails validation")

const requestEvidencePrefix = "ev:privacy:dsr:"

// DataSubjectRequest is one immutable data-subject request record: what was
// claimed, when, through which channel, under which jurisdiction's
// statutory clock, and its current identity-verification state.
//
// A DataSubjectRequest is never mutated in place. [Intake] produces the
// initial record; [DataSubjectRequest.Verify] returns a new record (with a
// freshly-appended [Evidence] entry on [DataSubjectRequest.Trail]) rather
// than changing the receiver.
type DataSubjectRequest struct {
	ID     string
	Tenant values.TenantId
	Kind   Kind
	Claims SubjectClaims
	// Jurisdiction is the fact this request's statutory clock and any
	// downstream fulfillment obligations are resolved against.
	Jurisdiction legal.Jurisdiction
	ReceivedAt   values.Instant
	Channel      Channel

	// Deadline is the statutory response deadline [Intake] computed from a
	// [ClockTable] at intake time. It never changes afterward, even if the
	// request is later re-verified.
	Deadline values.Instant

	VerificationState VerificationState
	// IdentityEvidenceRef, IdentityAssurance and VerifiedAt are populated
	// only once [DataSubjectRequest.Verify] has succeeded; they are the
	// empty/zero value while VerificationState is
	// [VerificationUnverified] or [VerificationRefused].
	IdentityEvidenceRef string
	IdentityAssurance   trust.Assurance
	VerifiedAt          values.Instant

	// DuplicateOf is the ID of the earliest request [DetectDuplicate]
	// matched this one against, within the same tenant, subject key, kind
	// and window. Empty means this request is its window's primary
	// (executable) request.
	DuplicateOf string

	// EvidenceID is derived from this record's own [DataSubjectRequest.Digest].
	EvidenceID string
	// Trail is the append-only sequence of events recorded against this
	// request. It is not part of Digest: Digest is a content digest of the
	// request's current state, while Trail is the audit history of how it
	// got there. Order is insertion order and is preserved by every
	// state-transition method in this package.
	Trail []Evidence
}

// canonicalBytes appends r's deterministic, length-prefixed encoding of
// every field that defines its current state (excluding EvidenceID itself
// and Trail, per the [DataSubjectRequest.Trail] doc comment).
func (r DataSubjectRequest) canonicalBytes() []byte {
	dst := appendFields(nil,
		"id", r.ID,
		"tenant", r.Tenant.String(),
		"kind", string(r.Kind),
		"jurisdiction", r.Jurisdiction.String(),
		"jurisdiction_country", r.Jurisdiction.Country,
		"jurisdiction_state", r.Jurisdiction.State,
		"jurisdiction_locality", r.Jurisdiction.Locality,
		"received_at", r.ReceivedAt.String(),
		"channel", string(r.Channel),
		"deadline", r.Deadline.String(),
		"verification_state", string(r.VerificationState),
		"identity_evidence_ref", r.IdentityEvidenceRef,
		"identity_assurance", r.IdentityAssurance.String(),
		"verified_at", r.VerifiedAt.String(),
		"duplicate_of", r.DuplicateOf,
	)
	dst = r.Claims.canonicalBytes(dst)
	return dst
}

// Digest is the canonical content digest of this exact request state. Two
// requests with identical field values -- built independently -- always
// digest identically; any difference in any field, including a claims
// field or the verification state, changes it.
func (r DataSubjectRequest) Digest() string {
	return digestHex(r.canonicalBytes())
}

// Validate reports whether r carries every required field, a well-formed
// jurisdiction, a declared kind/channel/verification-state, and an
// internally consistent verification state: [VerificationVerified] requires
// a non-empty evidence ref, a specified assurance level and a set
// VerifiedAt; anything less than that must not claim to be verified. It
// also checks that EvidenceID still matches r's own canonical digest, the
// same tamper-evidence discipline
// internal/governance/privacy.OptionalProcessing.Validate uses.
func (r DataSubjectRequest) Validate() error {
	if r.ID == "" {
		return fmt.Errorf("%w: no id", ErrRequestInvalid)
	}
	if err := r.Tenant.Validate(); err != nil {
		return fmt.Errorf("%w: request %q: %v", ErrRequestInvalid, r.ID, err)
	}
	if err := r.Kind.Validate(); err != nil {
		return fmt.Errorf("%w: request %q: %v", ErrRequestInvalid, r.ID, err)
	}
	if err := r.Claims.Validate(); err != nil {
		return fmt.Errorf("%w: request %q: %v", ErrRequestInvalid, r.ID, err)
	}
	if err := r.Jurisdiction.Validate(); err != nil {
		return fmt.Errorf("%w: request %q: jurisdiction %v", ErrRequestInvalid, r.ID, err)
	}
	if !r.ReceivedAt.IsSet() {
		return fmt.Errorf("%w: request %q has no received_at", ErrRequestInvalid, r.ID)
	}
	if err := r.Channel.Validate(); err != nil {
		return fmt.Errorf("%w: request %q: %v", ErrRequestInvalid, r.ID, err)
	}
	if !r.Deadline.IsSet() {
		return fmt.Errorf("%w: request %q has no statutory deadline", ErrRequestInvalid, r.ID)
	}
	if r.Deadline.Before(r.ReceivedAt) {
		return fmt.Errorf("%w: request %q deadline precedes received_at", ErrRequestInvalid, r.ID)
	}
	if err := r.VerificationState.Validate(); err != nil {
		return fmt.Errorf("%w: request %q: %v", ErrRequestInvalid, r.ID, err)
	}

	switch r.VerificationState {
	case VerificationVerified:
		if r.IdentityEvidenceRef == "" {
			return fmt.Errorf("%w: request %q is VERIFIED with no identity evidence ref", ErrRequestInvalid, r.ID)
		}
		if r.IdentityAssurance == trust.AssuranceUnspecified {
			return fmt.Errorf("%w: request %q is VERIFIED with unspecified assurance", ErrRequestInvalid, r.ID)
		}
		if !r.VerifiedAt.IsSet() {
			return fmt.Errorf("%w: request %q is VERIFIED with no verified_at", ErrRequestInvalid, r.ID)
		}
	default:
		if r.IdentityEvidenceRef != "" || r.IdentityAssurance != trust.AssuranceUnspecified || r.VerifiedAt.IsSet() {
			return fmt.Errorf("%w: request %q carries verification evidence while %s", ErrRequestInvalid, r.ID, r.VerificationState)
		}
	}

	if r.EvidenceID == "" {
		return fmt.Errorf("%w: request %q has no evidence id", ErrRequestInvalid, r.ID)
	}
	if r.EvidenceID != requestEvidencePrefix+r.Digest() {
		return fmt.Errorf("%w: request %q evidence id does not match its own canonical digest", ErrRequestInvalid, r.ID)
	}
	return nil
}

// withEvidenceID returns a copy of r with EvidenceID recomputed from its
// current canonical digest.
func (r DataSubjectRequest) withEvidenceID() DataSubjectRequest {
	r.EvidenceID = requestEvidencePrefix + r.Digest()
	return r
}

// appendEvidence returns a copy of r with a new [Evidence] entry appended to
// Trail (a cloned slice; r's own Trail is left untouched), recorded against
// r's current digest.
func (r DataSubjectRequest) appendEvidence(kind EventKind, at values.Instant, detail string) DataSubjectRequest {
	r.Trail = append(slices.Clone(r.Trail), newEvidence(kind, r.Digest(), at, detail))
	return r
}

// AdvanceCode is the stable reason token [DataSubjectRequest.CanAdvance]
// returns alongside its allow/deny answer.
type AdvanceCode string

// Advance decision codes.
const (
	AdvanceAllowed             AdvanceCode = "ADVANCEABLE"
	AdvanceRecordInvalid       AdvanceCode = "RECORD_INVALID"
	AdvanceDuplicateNotPrimary AdvanceCode = "DUPLICATE_LINKED_NOT_PRIMARY"
	AdvanceUnverified          AdvanceCode = "UNVERIFIED"
	AdvanceAssuranceBelowFloor AdvanceCode = "ASSURANCE_BELOW_FLOOR"
)

// CanAdvance reports whether r may be handed to a downstream fulfillment
// capability (PRIV-006). It is deny-by-default and re-derives its answer
// from r's own stored fields every time -- it never trusts a cached
// boolean:
//
//   - an invalid record (failing [DataSubjectRequest.Validate]) never
//     advances;
//   - a request linked as a duplicate ([DataSubjectRequest.DuplicateOf] set)
//     never advances -- only the primary request in its window does;
//   - a request whose [DataSubjectRequest.VerificationState] is not
//     [VerificationVerified] never advances;
//   - a verified request whose [DataSubjectRequest.IdentityAssurance] does
//     not meet [AssuranceFloor] for its [Kind] never advances, even if
//     VerificationState somehow reads VERIFIED -- this is what makes a
//     forged or mis-migrated "VERIFIED" record with insufficient assurance
//     just as unable to advance as one that was never verified at all.
func (r DataSubjectRequest) CanAdvance() (bool, AdvanceCode) {
	if err := r.Validate(); err != nil {
		return false, AdvanceRecordInvalid
	}
	if r.DuplicateOf != "" {
		return false, AdvanceDuplicateNotPrimary
	}
	if r.VerificationState != VerificationVerified {
		return false, AdvanceUnverified
	}
	if !r.IdentityAssurance.AtLeast(AssuranceFloor(r.Kind)) {
		return false, AdvanceAssuranceBelowFloor
	}
	return true, AdvanceAllowed
}
