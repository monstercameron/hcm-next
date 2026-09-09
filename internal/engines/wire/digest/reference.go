package digest

import (
	"encoding/hex"
	"errors"
	"fmt"

	intentsv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/intents/v1"
)

// Sentinel causes. Classify with [errors.Is]; never by matching strings.
var (
	// ErrUnknownAlgorithm reports a digest algorithm the registry does not
	// publish. Algorithm eligibility is registry-owned, not caller-owned.
	ErrUnknownAlgorithm = errors.New("digest: unknown algorithm")

	// ErrUnknownProfile reports a profile id or profile version the registry
	// does not publish.
	ErrUnknownProfile = errors.New("digest: unknown canonicalization profile")

	// ErrOmittedMaterialPath reports a profile registration that fails to bind
	// a path the platform requires that profile id to bind.
	ErrOmittedMaterialPath = errors.New("digest: profile omits a required material path")

	// ErrNonMaterialPath reports a profile registration that binds a path the
	// platform declares revalidated context rather than material.
	ErrNonMaterialPath = errors.New("digest: profile binds a non-material path")

	// ErrEmptyDigest reports a reference whose digest is absent or all zero.
	// A nullable digest never verifies.
	ErrEmptyDigest = errors.New("digest: reference carries no digest")

	// ErrProfileSubstitution reports a reference presented for verification
	// under a profile other than the one it was minted with.
	ErrProfileSubstitution = errors.New("digest: profile substitution")

	// ErrDigestMismatch reports a recomputation that does not reproduce the
	// reference.
	ErrDigestMismatch = errors.New("digest: recomputed digest does not match")

	// ErrScopeMismatch reports a reference whose scope binding does not match
	// the tenant, intent and proposal identity of the source object.
	ErrScopeMismatch = errors.New("digest: scope binding does not match source")

	// ErrInvalidReference reports a structurally invalid reference or proto.
	ErrInvalidReference = errors.New("digest: invalid reference")

	// ErrAlreadyRegistered reports a duplicate profile version or algorithm id.
	// Published profile versions are immutable.
	ErrAlreadyRegistered = errors.New("digest: already registered")
)

// Error is the single error type this package returns.
type Error struct {
	Op     string
	Cause  error
	Detail string
}

func (e *Error) Error() string {
	msg := e.Cause.Error()
	if e.Detail != "" {
		msg = msg + ": " + e.Detail
	}
	if e.Op != "" {
		msg = e.Op + ": " + msg
	}
	return msg
}

// Unwrap exposes the sentinel cause to [errors.Is].
func (e *Error) Unwrap() error { return e.Cause }

func newError(op string, cause error, format string, args ...any) *Error {
	return &Error{Op: op, Cause: cause, Detail: fmt.Sprintf(format, args...)}
}

// Reference is the CanonicalDigest envelope. It mirrors
// hcmnext.intents.v1.CanonicalDigestReference field for field and converts to
// and from it without loss, including the distinction between an absent
// optional field and one explicitly set to the empty string.
//
// An approval records this whole envelope, not a naked hash: without profile,
// profile version, schema, schema version and algorithm, a hash cannot be
// re-derived and therefore cannot be audited.
type Reference struct {
	ProfileID       string
	ProfileVersion  uint32
	SchemaID        string
	SchemaVersion   uint32
	AlgorithmID     string
	CanonicalLength uint64

	// Digest is lowercase hex of the algorithm output over the canonical bytes.
	Digest string

	// ScopeBindingDigest binds the tenant, intent and proposal identity the
	// canonical bytes were computed for, so a digest cannot be replayed against
	// a different subject.
	ScopeBindingDigest string

	// CanonicalBytesArtifactRef points at durably stored canonical bytes, for
	// high-value evidence only. Canonical bytes may carry sensitive source
	// data; store them only when evidence value requires it.
	CanonicalBytesArtifactRef *string

	IntentID           *string
	ProposalRevisionID *string

	// MaterialProfileRef names the published profile record in the
	// Canonicalization Registry.
	MaterialProfileRef *string
}

// ToProto converts to the transport representation.
func (r Reference) ToProto() *intentsv1.CanonicalDigestReference {
	return &intentsv1.CanonicalDigestReference{
		ProfileId:                 r.ProfileID,
		ProfileVersion:            r.ProfileVersion,
		SchemaId:                  r.SchemaID,
		SchemaVersion:             r.SchemaVersion,
		AlgorithmId:               r.AlgorithmID,
		CanonicalLength:           r.CanonicalLength,
		Digest:                    r.Digest,
		CanonicalBytesArtifactRef: cloneOptional(r.CanonicalBytesArtifactRef),
		ScopeBindingDigest:        r.ScopeBindingDigest,
		IntentId:                  cloneOptional(r.IntentID),
		ProposalRevisionId:        cloneOptional(r.ProposalRevisionID),
		MaterialProfileRef:        cloneOptional(r.MaterialProfileRef),
	}
}

// FromProto converts from the transport representation. A nil message is an
// error rather than a zero reference: an absent digest never verifies, and
// silently producing one would hide that.
func FromProto(p *intentsv1.CanonicalDigestReference) (Reference, error) {
	if p == nil {
		return Reference{}, newError("FromProto", ErrInvalidReference, "nil CanonicalDigestReference")
	}
	return Reference{
		ProfileID:                 p.GetProfileId(),
		ProfileVersion:            p.GetProfileVersion(),
		SchemaID:                  p.GetSchemaId(),
		SchemaVersion:             p.GetSchemaVersion(),
		AlgorithmID:               p.GetAlgorithmId(),
		CanonicalLength:           p.GetCanonicalLength(),
		Digest:                    p.GetDigest(),
		CanonicalBytesArtifactRef: cloneOptional(p.CanonicalBytesArtifactRef),
		ScopeBindingDigest:        p.GetScopeBindingDigest(),
		IntentID:                  cloneOptional(p.IntentId),
		ProposalRevisionID:        cloneOptional(p.ProposalRevisionId),
		MaterialProfileRef:        cloneOptional(p.MaterialProfileRef),
	}, nil
}

func cloneOptional(s *string) *string {
	if s == nil {
		return nil
	}
	v := *s
	return &v
}

// Ptr returns a pointer to v, for populating the optional fields of a
// [Reference].
func Ptr(v string) *string { return &v }

// Key identifies a published profile version.
type Key struct {
	ProfileID string
	Version   uint32
}

func (k Key) String() string { return fmt.Sprintf("%s@v%d", k.ProfileID, k.Version) }

// Key returns the profile version this reference was minted under.
func (r Reference) Key() Key { return Key{ProfileID: r.ProfileID, Version: r.ProfileVersion} }

// validate rejects references that cannot be verified on their face, before any
// canonicalization work is attempted.
func (r Reference) validate() error {
	if r.ProfileID == "" {
		return newError("verify", ErrInvalidReference, "reference names no profile")
	}
	if r.AlgorithmID == "" {
		return newError("verify", ErrUnknownAlgorithm, "reference names no algorithm")
	}
	if r.Digest == "" {
		return newError("verify", ErrEmptyDigest, "digest is empty")
	}
	raw, err := hex.DecodeString(r.Digest)
	if err != nil {
		return newError("verify", ErrInvalidReference, "digest is not lowercase hex: %v", err)
	}
	if len(raw) == 0 {
		return newError("verify", ErrEmptyDigest, "digest decodes to zero bytes")
	}
	for _, b := range raw {
		if b != 0 {
			return nil
		}
	}
	return newError("verify", ErrEmptyDigest, "digest is all zero bytes")
}
