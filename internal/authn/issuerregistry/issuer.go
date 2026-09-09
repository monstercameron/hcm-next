package issuerregistry

import (
	"crypto/x509"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	trustfederation "github.com/monstercameron/human-capital-management-suite/internal/trust/federation"
)

// Status is the closed lifecycle state of one issuer registration. A caller
// cannot invent a fifth state: every function in this package that produces
// or reads one refuses anything outside this set.
type Status string

// The four declared lifecycle states.
const (
	StatusDraft     Status = "DRAFT"
	StatusActive    Status = "ACTIVE"
	StatusSuspended Status = "SUSPENDED"
	StatusRetired   Status = "RETIRED"
)

func (s Status) valid() bool {
	switch s {
	case StatusDraft, StatusActive, StatusSuspended, StatusRetired:
		return true
	default:
		return false
	}
}

// terminal reports whether s can never transition to another state.
// RETIRED is the only terminal state: an issuer taken out of service stays
// out of service, matching migration 00038's own doc comment.
func (s Status) terminal() bool { return s == StatusRetired }

// JWKSSourceKind is the closed set of ways an issuer's verification
// material can be pinned. There is no "fetch this URL live" kind: LIB-010
// (a JWKS-fetching client) is not yet qualified, and even once it is,
// "pinned" is a deliberate governance property this registry enforces
// independent of whether live fetch exists.
type JWKSSourceKind string

// The two declared pinning kinds.
const (
	// JWKSSourcePinnedKeys pins an explicit, closed snapshot of signing
	// keys copied from the issuer's JWKS at publish time.
	JWKSSourcePinnedKeys JWKSSourceKind = "PINNED_KEYS"
	// JWKSSourcePinnedBundle anchors the issuer's verification material to
	// one specific internal/trust/bundle.Bundle version, addressed by a
	// caller-defined reference name and that bundle's version number.
	JWKSSourcePinnedBundle JWKSSourceKind = "PINNED_BUNDLE"
)

func (k JWKSSourceKind) valid() bool {
	return k == JWKSSourcePinnedKeys || k == JWKSSourcePinnedBundle
}

// PinnedKey is one verification key copied, at publish time, from the
// issuer's JWKS. PublicKeyDER is the key's DER-encoded SubjectPublicKeyInfo
// (what x509.ParsePKIXPublicKey and most JWKS-to-DER conversions produce),
// so this package never needs to know a JOSE key's wire (JWK) encoding.
type PinnedKey struct {
	KeyID        string
	Algorithm    trustfederation.Algorithm
	PublicKeyDER []byte
	NotBefore    time.Time
	NotAfter     time.Time
}

func (k PinnedKey) valid() error {
	if strings.TrimSpace(k.KeyID) == "" {
		return fmt.Errorf("%w: pinned key has no key id", ErrInvalidIssuer)
	}
	if !k.Algorithm.Valid() {
		return fmt.Errorf("%w: pinned key %q has unsupported algorithm %q", ErrInvalidIssuer, k.KeyID, k.Algorithm)
	}
	if len(k.PublicKeyDER) == 0 {
		return fmt.Errorf("%w: pinned key %q has no public key material", ErrInvalidIssuer, k.KeyID)
	}
	if _, err := x509.ParsePKIXPublicKey(k.PublicKeyDER); err != nil {
		return fmt.Errorf("%w: pinned key %q public key does not parse: %v", ErrInvalidIssuer, k.KeyID, err)
	}
	if !k.NotBefore.IsZero() && !k.NotAfter.IsZero() && !k.NotBefore.Before(k.NotAfter) {
		return fmt.Errorf("%w: pinned key %q validity window is empty or inverted", ErrInvalidIssuer, k.KeyID)
	}
	return nil
}

// JWKSSource names the pinned verification material for one issuer
// revision. DiscoveryURL is retained only as operator-facing provenance
// (where a human would go to re-pin material by hand); it is never fetched
// and is never, by itself, sufficient to satisfy [Issuer]'s pinning
// requirement -- see [pinned].
type JWKSSource struct {
	Kind JWKSSourceKind

	// PinnedKeys is used when Kind is [JWKSSourcePinnedKeys].
	PinnedKeys []PinnedKey

	// BundleRef and BundleVersion are used when Kind is
	// [JWKSSourcePinnedBundle]: the exact bundle version this issuer's
	// verification material is anchored to.
	BundleRef     string
	BundleVersion int

	// DiscoveryURL is documentation only. See the type doc comment.
	DiscoveryURL string
}

// pinned reports whether j actually names pinned material rather than an
// empty or half-specified reference.
func (j JWKSSource) pinned() bool {
	switch j.Kind {
	case JWKSSourcePinnedKeys:
		return len(j.PinnedKeys) > 0
	case JWKSSourcePinnedBundle:
		return strings.TrimSpace(j.BundleRef) != "" && j.BundleVersion > 0
	default:
		return false
	}
}

func (j JWKSSource) validate() error {
	if !j.Kind.valid() {
		return fmt.Errorf("%w: jwks source kind %q is not in the closed vocabulary", ErrInvalidIssuer, j.Kind)
	}
	if !j.pinned() {
		return fmt.Errorf("%w: jwks source is not pinned", ErrJWKSNotPinned)
	}
	if j.Kind == JWKSSourcePinnedKeys {
		seen := make(map[string]bool, len(j.PinnedKeys))
		for _, k := range j.PinnedKeys {
			if err := k.valid(); err != nil {
				return err
			}
			if seen[k.KeyID] {
				return fmt.Errorf("%w: duplicate pinned key id %q", ErrInvalidIssuer, k.KeyID)
			}
			seen[k.KeyID] = true
		}
	}
	return nil
}

// PrincipalField is the closed set of internal/trust.PrincipalSpec fields a
// [ClaimMapping] may target -- the fields internal/trust/federation's
// [trustfederation.Claims] shape already sources from a fixed wire key.
type PrincipalField string

// The declared principal fields a claim mapping may target.
const (
	PrincipalFieldSubject             PrincipalField = "sub"
	PrincipalFieldSubjectKind         PrincipalField = "sub_kind"
	PrincipalFieldOrganizationScopeID PrincipalField = "org_scope"
	PrincipalFieldRoles               PrincipalField = "roles"
	PrincipalFieldAuthorityRefs       PrincipalField = "authority_refs"
	PrincipalFieldPurposes            PrincipalField = "purposes"
	PrincipalFieldAssurance           PrincipalField = "assurance"
	PrincipalFieldDelegationRefs      PrincipalField = "delegation_refs"
)

func (f PrincipalField) valid() bool {
	switch f {
	case PrincipalFieldSubject, PrincipalFieldSubjectKind, PrincipalFieldOrganizationScopeID,
		PrincipalFieldRoles, PrincipalFieldAuthorityRefs, PrincipalFieldPurposes,
		PrincipalFieldAssurance, PrincipalFieldDelegationRefs:
		return true
	default:
		return false
	}
}

// ClaimMapping declares which source claim in the issuer's assertion feeds
// one platform principal field. It is governance metadata describing the
// tenant's issuer configuration, not a runtime transform: internal/trust/federation's
// frozen [trustfederation.Validator] decodes a closed Claims shape directly
// from the signature-verified payload today, so a runtime claim-renaming
// pipeline reading this record is later work; this is what that pipeline
// will read once it exists.
type ClaimMapping struct {
	SourceClaim string
	Target      PrincipalField
}

func validateClaimMappings(mappings []ClaimMapping) error {
	seenSource := make(map[string]bool, len(mappings))
	seenTarget := make(map[PrincipalField]bool, len(mappings))
	for _, m := range mappings {
		if strings.TrimSpace(m.SourceClaim) == "" {
			return fmt.Errorf("%w: claim mapping has no source claim", ErrInvalidIssuer)
		}
		if !m.Target.valid() {
			return fmt.Errorf("%w: claim mapping targets %q, not a platform principal field", ErrInvalidIssuer, m.Target)
		}
		if seenSource[m.SourceClaim] {
			return fmt.Errorf("%w: source claim %q is mapped more than once", ErrInvalidIssuer, m.SourceClaim)
		}
		if seenTarget[m.Target] {
			return fmt.Errorf("%w: principal field %q is targeted by more than one claim mapping", ErrInvalidIssuer, m.Target)
		}
		seenSource[m.SourceClaim] = true
		seenTarget[m.Target] = true
	}
	return nil
}

// Issuer is one immutable, published revision of a tenant's federation
// issuer configuration. A new revision is always a new value [Publish]
// mints; nothing in this package edits one in place.
type Issuer struct {
	Tenant    values.TenantId
	IssuerURL string
	Audience  string

	JWKS       JWKSSource
	Algorithms []trustfederation.Algorithm

	ClaimMappings []ClaimMapping

	// ClockSkew bounds the clock skew tolerance a verifier consuming this
	// issuer's material may apply (mirrors internal/trust/federation.Config.Leeway).
	ClockSkew time.Duration
	// MetadataStaleness bounds how long this revision's pinned material may
	// be relied on before it must be re-pinned by a new revision.
	MetadataStaleness time.Duration

	// GovernmentTenant marks a revision whose tenant-specific assurance floor
	// must be declared and resolved against the versioned step-up table.
	GovernmentTenant  bool
	AssuranceContract AssuranceContract

	Revision           uint32
	PublisherPrincipal string
	PublishedAt        time.Time
}

// Ref names one exact published revision: the identity [Store.GetIssuer]
// looks a revision up by.
type Ref struct {
	Tenant    values.TenantId
	IssuerURL string
	Revision  uint32
}

// Ref returns i's own identity.
func (i Issuer) Ref() Ref { return Ref{Tenant: i.Tenant, IssuerURL: i.IssuerURL, Revision: i.Revision} }

// clone returns a deep copy of i so a caller mutating a value this package
// returns can never reach into a [Store]'s own storage.
func (i Issuer) clone() Issuer {
	c := i
	c.JWKS.PinnedKeys = slices.Clone(i.JWKS.PinnedKeys)
	for idx := range c.JWKS.PinnedKeys {
		c.JWKS.PinnedKeys[idx].PublicKeyDER = slices.Clone(i.JWKS.PinnedKeys[idx].PublicKeyDER)
	}
	c.Algorithms = slices.Clone(i.Algorithms)
	c.ClaimMappings = slices.Clone(i.ClaimMappings)
	return c
}

// Validation and lifecycle errors this package returns. All are matchable
// with errors.Is.
var (
	ErrInvalidIssuer       = errors.New("issuerregistry: invalid issuer record")
	ErrJWKSNotPinned       = errors.New("issuerregistry: jwks source is not pinned")
	ErrNoAlgorithms        = errors.New("issuerregistry: issuer declares no signing algorithm")
	ErrAlgorithmNotAllowed = errors.New("issuerregistry: issuer declares an algorithm outside the closed signing-algorithm set")
	ErrUnknownIssuer       = errors.New("issuerregistry: unknown issuer")
	ErrWrongTenant         = errors.New("issuerregistry: issuer is registered for a different tenant")
	ErrIssuerSuspended     = errors.New("issuerregistry: issuer is suspended")
	ErrIssuerRetired       = errors.New("issuerregistry: issuer is retired")
	ErrIssuerNotActive     = errors.New("issuerregistry: issuer has not been activated")
	ErrRevisionConflict    = errors.New("issuerregistry: revision is not newer than the current revision")
	ErrUnknownRevision     = errors.New("issuerregistry: no published revision at that number")
	ErrInvalidTransition   = errors.New("issuerregistry: lifecycle transition is not permitted from the current state")
	ErrSameApprover        = errors.New("issuerregistry: activation must be performed by a principal distinct from the publisher")
	ErrMissingEvidence     = errors.New("issuerregistry: state change evidence is incomplete")
)

// validate checks every field [Publish] requires before a revision is ever
// persisted, including the refusals AUTHN-001 names explicitly: an
// unpinned JWKS source, and an algorithm set that is empty or that names
// anything outside internal/trust/federation's closed algorithm vocabulary.
func (i Issuer) validate() error {
	if err := i.Tenant.Validate(); err != nil {
		return fmt.Errorf("%w: tenant: %v", ErrInvalidIssuer, err)
	}
	if strings.TrimSpace(i.IssuerURL) == "" {
		return fmt.Errorf("%w: issuer url is required", ErrInvalidIssuer)
	}
	if _, err := url.ParseRequestURI(i.IssuerURL); err != nil {
		return fmt.Errorf("%w: issuer url %q does not parse: %v", ErrInvalidIssuer, i.IssuerURL, err)
	}
	if strings.TrimSpace(i.Audience) == "" {
		return fmt.Errorf("%w: audience is required", ErrInvalidIssuer)
	}
	if err := i.JWKS.validate(); err != nil {
		return err
	}
	if len(i.Algorithms) == 0 {
		return ErrNoAlgorithms
	}
	seen := make(map[trustfederation.Algorithm]bool, len(i.Algorithms))
	for _, alg := range i.Algorithms {
		if !alg.Valid() {
			return fmt.Errorf("%w: %q", ErrAlgorithmNotAllowed, alg)
		}
		if seen[alg] {
			return fmt.Errorf("%w: algorithm %q declared more than once", ErrInvalidIssuer, alg)
		}
		seen[alg] = true
	}
	if err := validateClaimMappings(i.ClaimMappings); err != nil {
		return err
	}
	if i.GovernmentTenant && !i.AssuranceContract.Configured() {
		return fmt.Errorf("%w: assurance contract field is required for a government tenant", ErrInvalidIssuer)
	}
	if err := i.AssuranceContract.validate(); err != nil {
		return err
	}
	if i.ClockSkew < 0 {
		return fmt.Errorf("%w: clock skew must not be negative", ErrInvalidIssuer)
	}
	if i.MetadataStaleness <= 0 {
		return fmt.Errorf("%w: metadata staleness must be positive", ErrInvalidIssuer)
	}
	if i.Revision == 0 {
		return fmt.Errorf("%w: revision must be >= 1", ErrInvalidIssuer)
	}
	if strings.TrimSpace(i.PublisherPrincipal) == "" {
		return fmt.Errorf("%w: publisher principal is required", ErrInvalidIssuer)
	}
	if i.PublishedAt.IsZero() {
		return fmt.Errorf("%w: published-at is required", ErrInvalidIssuer)
	}
	return nil
}
