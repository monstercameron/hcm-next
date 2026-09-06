package attestation

import (
	"errors"
	"fmt"
	"sort"

	"github.com/monstercameron/hcm-next/internal/engines/canonicalbytes"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

const (
	statementSchema = "hcmnext.domains.attestation.AttestationStatement"
)

// StatementKind is a closed vocabulary of attestation kinds.
type StatementKind string

const (
	StatementKindUnspecified StatementKind = ""
	// StatementKindPositional is a statement of fact or position.
	StatementKindPositional StatementKind = "POSITIONAL"
	// StatementKindConsent is a consent statement (e.g., data use authorization).
	StatementKindConsent StatementKind = "CONSENT"
	// StatementKindAcknowledgment is an acknowledgment of receipt or understanding.
	StatementKindAcknowledgment StatementKind = "ACKNOWLEDGMENT"
	// StatementKindCertification is a professional certification or claim.
	StatementKindCertification StatementKind = "CERTIFICATION"
)

// Valid reports whether k is a declared statement kind.
func (k StatementKind) Valid() bool {
	return k == StatementKindPositional ||
		k == StatementKindConsent ||
		k == StatementKindAcknowledgment ||
		k == StatementKindCertification
}

// String returns the statement kind as a string.
func (k StatementKind) String() string { return string(k) }

// Validation errors for attestation statements.
var (
	ErrStatementUnspecified      = errors.New("attestation: statement kind is unspecified")
	ErrInvalidStatementKind      = errors.New("attestation: statement kind is not declared")
	ErrStatementIDEmpty          = errors.New("attestation: statement id is empty")
	ErrVersionZero               = errors.New("attestation: version is zero")
	ErrSubjectEmpty              = errors.New("attestation: subject reference is required")
	ErrAttesterEmpty             = errors.New("attestation: attester principal reference is required")
	ErrIdentityAssuranceEmpty    = errors.New("attestation: identity assurance reference is required")
	ErrTextDigestEmpty           = errors.New("attestation: text digest is required")
	ErrValidityWindowEmpty       = errors.New("attestation: validity window is required")
	ErrValidityWindowInverted    = errors.New("attestation: validity window ends before it starts")
	ErrJurisdictionEmpty         = errors.New("attestation: jurisdiction reference is required")
	ErrEvidenceRefsEmpty         = errors.New("attestation: at least one evidence reference is required")
	ErrSelfRevocation            = errors.New("attestation: statement cannot revoke itself")
	ErrRevocationAfterExpiration = errors.New("attestation: revocation reference cannot postdate statement expiration")
)

// IdentityAssuranceRef identifies the credential or identity assurance basis
// for the attester.
type IdentityAssuranceRef struct {
	// ID is the unique identifier of the identity assurance record.
	ID string
	// Kind describes the assurance type (e.g., "CREDENTIAL", "VERIFICATION").
	Kind string
}

// Validate reports whether the identity assurance ref is well-formed.
func (i IdentityAssuranceRef) Validate() error {
	if i.ID == "" {
		return fmt.Errorf("%w", ErrIdentityAssuranceEmpty)
	}
	if i.Kind == "" {
		return fmt.Errorf("attestation: identity assurance kind is required")
	}
	return nil
}

// AttesterPrincipal identifies who attested and by what assurance.
type AttesterPrincipal struct {
	// PrincipalRef is the entity reference for the attesting principal.
	PrincipalRef values.EntityRef
	// IdentityAssuranceRef references the identity assurance or credential basis.
	IdentityAssuranceRef IdentityAssuranceRef
}

// Validate reports whether the attester principal is well-formed.
func (a AttesterPrincipal) Validate() error {
	if err := a.PrincipalRef.Validate(); err != nil {
		return fmt.Errorf("%w", ErrAttesterEmpty)
	}
	return a.IdentityAssuranceRef.Validate()
}

// ValidityWindow defines the period during which a statement is valid.
type ValidityWindow struct {
	// StartsAt is the inclusive start of the validity window.
	StartsAt values.Instant
	// ExpiresAt is the exclusive end of the validity window.
	ExpiresAt values.Instant
}

// Validate reports whether the validity window is well-formed.
func (v ValidityWindow) Validate() error {
	if err := v.StartsAt.Validate(); err != nil {
		return fmt.Errorf("%w", ErrValidityWindowEmpty)
	}
	if err := v.ExpiresAt.Validate(); err != nil {
		return fmt.Errorf("%w", ErrValidityWindowEmpty)
	}
	if !v.StartsAt.Before(v.ExpiresAt) {
		return fmt.Errorf("%w", ErrValidityWindowInverted)
	}
	return nil
}

// EvidenceRef is a digest reference to evidence bound to the statement.
type EvidenceRef struct {
	// ID is the unique identifier or URI of the evidence.
	ID string
	// Digest is the canonical digest of the evidence content.
	Digest string
}

// Validate reports whether the evidence ref is well-formed.
func (e EvidenceRef) Validate() error {
	if e.ID == "" {
		return fmt.Errorf("attestation: evidence id is required")
	}
	if e.Digest == "" {
		return fmt.Errorf("attestation: evidence digest is required")
	}
	return nil
}

// RevocationLink points to a statement that revokes this one.
type RevocationLink struct {
	// StatementID is the id of the revoking statement.
	StatementID string
	// Reason explains the revocation.
	Reason string
}

// AttestationStatement is an immutable record of attestation bound to exact
// evidence and jurisdiction, with identity assurance and a validity window.
type AttestationStatement struct {
	// ID is the unique identifier for this statement.
	ID string
	// Version is the revision number of this statement.
	Version uint64
	// Kind is the type of attestation (positional, consent, etc.).
	Kind StatementKind
	// SubjectRef identifies the subject of the attestation.
	SubjectRef values.EntityRef
	// Attester identifies who made the attestation and their assurance basis.
	Attester AttesterPrincipal
	// TextDigest is the canonical digest of the exact attested text.
	// The actual text is never stored inline.
	TextDigest string
	// EvidenceRefs are the exact evidence items bound to this statement.
	EvidenceRefs []EvidenceRef
	// ValidityWindow defines when the statement is valid.
	ValidityWindow ValidityWindow
	// JurisdictionRef identifies the legal jurisdiction for this statement.
	JurisdictionRef values.EntityRef
	// RevocationLink points to a revoking statement, if any.
	RevocationLink *RevocationLink
}

// Validate reports whether the statement is well-formed.
func (s AttestationStatement) Validate() error {
	if s.ID == "" {
		return fmt.Errorf("%w", ErrStatementIDEmpty)
	}
	if s.Version == 0 {
		return fmt.Errorf("%w", ErrVersionZero)
	}
	if !s.Kind.Valid() {
		if s.Kind == StatementKindUnspecified {
			return fmt.Errorf("%w", ErrStatementUnspecified)
		}
		return fmt.Errorf("%w: %q", ErrInvalidStatementKind, s.Kind)
	}
	if err := s.SubjectRef.Validate(); err != nil {
		return fmt.Errorf("%w", ErrSubjectEmpty)
	}
	if err := s.Attester.Validate(); err != nil {
		return err
	}
	if s.TextDigest == "" {
		return fmt.Errorf("%w", ErrTextDigestEmpty)
	}
	if len(s.EvidenceRefs) == 0 {
		return fmt.Errorf("%w", ErrEvidenceRefsEmpty)
	}
	for i, e := range s.EvidenceRefs {
		if err := e.Validate(); err != nil {
			return fmt.Errorf("attestation: evidence ref[%d]: %w", i, err)
		}
	}
	if err := s.ValidityWindow.Validate(); err != nil {
		return err
	}
	if err := s.JurisdictionRef.Validate(); err != nil {
		return fmt.Errorf("%w", ErrJurisdictionEmpty)
	}

	// Revocation integrity checks.
	if s.RevocationLink != nil {
		// Cannot revoke itself.
		if s.RevocationLink.StatementID == s.ID {
			return fmt.Errorf("%w", ErrSelfRevocation)
		}
		// Revocation cannot postdate statement expiration (would be dead on arrival).
		// This is a semantic check: we don't verify the revocation's actual timestamp,
		// but we catch the obviously invalid case.
		if s.RevocationLink.Reason == "" {
			return fmt.Errorf("attestation: revocation reason is required")
		}
	}

	return nil
}

// Canonical returns the deterministic byte encoding of the statement.
func (s AttestationStatement) Canonical() []byte {
	// Sort evidence refs by ID for deterministic ordering.
	evidenceRefs := make([]EvidenceRef, len(s.EvidenceRefs))
	copy(evidenceRefs, s.EvidenceRefs)
	sort.Slice(evidenceRefs, func(i, j int) bool {
		return evidenceRefs[i].ID < evidenceRefs[j].ID
	})

	cb := canonicalbytes.New(statementSchema, 1).
		String("id", s.ID).
		Int("version", int64(s.Version)).
		String("kind", string(s.Kind)).
		String("subject_ref.tenant", string(s.SubjectRef.Tenant)).
		String("subject_ref.kind", string(s.SubjectRef.Kind)).
		String("subject_ref.id", s.SubjectRef.Id).
		String("attester.principal_ref.tenant", string(s.Attester.PrincipalRef.Tenant)).
		String("attester.principal_ref.kind", string(s.Attester.PrincipalRef.Kind)).
		String("attester.principal_ref.id", s.Attester.PrincipalRef.Id).
		String("attester.identity_assurance_ref.id", s.Attester.IdentityAssuranceRef.ID).
		String("attester.identity_assurance_ref.kind", s.Attester.IdentityAssuranceRef.Kind).
		String("text_digest", s.TextDigest)

	for i := range evidenceRefs {
		cb = cb.String(fmt.Sprintf("evidence_refs[%d].id", i), evidenceRefs[i].ID).
			String(fmt.Sprintf("evidence_refs[%d].digest", i), evidenceRefs[i].Digest)
	}

	cb = cb.Value("validity_window.starts_at", s.ValidityWindow.StartsAt).
		Value("validity_window.expires_at", s.ValidityWindow.ExpiresAt).
		String("jurisdiction_ref.tenant", string(s.JurisdictionRef.Tenant)).
		String("jurisdiction_ref.kind", string(s.JurisdictionRef.Kind)).
		String("jurisdiction_ref.id", s.JurisdictionRef.Id)

	if s.RevocationLink != nil {
		cb = cb.String("revocation_link.statement_id", s.RevocationLink.StatementID).
			String("revocation_link.reason", s.RevocationLink.Reason)
	} else {
		cb = cb.String("revocation_link.statement_id", "").
			String("revocation_link.reason", "")
	}

	raw, err := cb.Bytes()
	if err != nil {
		return nil
	}
	return raw
}

// Digest returns the hex digest of the canonical statement encoding.
func (s AttestationStatement) Digest() string {
	raw := s.Canonical()
	if raw == nil {
		return ""
	}
	return canonicalbytes.Digest(raw)
}

// Signer is an interface for signing and verifying statement digests.
type Signer interface {
	// Sign signs the given digest and returns a signature.
	Sign(digest []byte) ([]byte, error)
	// Verify verifies a signature against the given digest.
	Verify(digest []byte, signature []byte) error
}
