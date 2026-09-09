package federation

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// Claims is the normalized payload this package accepts from an enterprise
// identity provider. It mirrors the server-resolved fields of a
// [trust.PrincipalSpec]; a SAML adapter added later produces the same shape
// from an assertion's attribute statements without any change downstream of
// [Validator.Validate].
//
// Field names are the canonical JSON keys. Decoding is strict: an unknown
// key is a rejected assertion, never a silently ignored one -- a provider
// smuggling an undeclared claim past this parser is exactly the "raw claims
// leaking into AuthZ" outcome TRUST-002 exists to prevent.
//
// AuthenticationMethod is deliberately absent: every assertion this package
// accepts is a bearer credential from a federated identity provider, so
// [Validator.Validate] sets [trust.AuthenticationMethodBearerToken] itself
// rather than letting a claim assert it.
type Claims struct {
	Issuer              string   `json:"iss"`
	Audience            string   `json:"aud"`
	Subject             string   `json:"sub"`
	SubjectKind         string   `json:"sub_kind"`
	Tenant              string   `json:"tenant"`
	OrganizationScopeID string   `json:"org_scope,omitempty"`
	Roles               []string `json:"roles,omitempty"`
	AuthorityRefs       []string `json:"authority_refs,omitempty"`
	Purposes            []string `json:"purposes,omitempty"`
	Assurance           string   `json:"assurance"`
	DelegationRefs      []string `json:"delegation_refs,omitempty"`
	IssuedAtUnix        int64    `json:"iat"`
	NotBeforeUnix       int64    `json:"nbf,omitempty"`
	ExpiresAtUnix       int64    `json:"exp"`
}

// Validation errors distinct from the JOSE-layer errors in jose.go. All are
// matchable with errors.Is.
var (
	ErrAssertionIssuer    = errors.New("federation: assertion issuer is not allow-listed for any configured tenant")
	ErrTenantBinding      = errors.New("federation: assertion issuer is not allow-listed for the tenant it asserts")
	ErrAssertionAudience  = errors.New("federation: assertion audience is not this listener")
	ErrAssertionExpired   = errors.New("federation: assertion is expired or not yet valid")
	ErrAssertionClaims    = errors.New("federation: assertion is missing or misdeclares a required claim")
	ErrAssertionAssurance = errors.New("federation: assertion carries no valid assurance evidence")
)

// Config configures a [Validator].
type Config struct {
	// TenantIssuers is the per-tenant issuer allow-list: an assertion
	// federates for a tenant only when its issuer appears in that exact
	// tenant's list. An issuer allow-listed for one tenant authorizes
	// nothing for another -- this is the tenant-binding control, and it is
	// what stops a genuine, correctly signed assertion for tenant A from
	// being accepted as one for tenant B.
	TenantIssuers map[values.TenantId][]string
	// Audience is the audience every assertion this listener accepts must
	// declare.
	Audience string
	// Keys resolves the verification key for an issuer and key id.
	Keys KeySource
	// Now supplies the current time for expiry/not-before evaluation. Nil
	// means time.Now.
	Now func() time.Time
	// Leeway tolerates clock skew between this listener and the issuing
	// identity provider on the assertion's validity window. Zero means no
	// leeway, which is the correct default for a hermetic test.
	Leeway time.Duration
	// MaxAssertionBytes bounds assertion length before any allocation that
	// depends on it. Zero means 16 KiB.
	MaxAssertionBytes int
}

// Validator validates an enterprise identity assertion and produces the
// immutable [trust.Principal] it authenticates. It is the only kind of
// value in this package that can do so.
type Validator struct {
	tenantIssuers map[values.TenantId]map[string]bool
	audience      string
	keys          KeySource
	now           func() time.Time
	leeway        time.Duration
	maxBytes      int
}

// Configuration errors for [NewValidator].
var (
	ErrValidatorAudience = errors.New("federation: validator needs an audience")
	ErrValidatorKeys     = errors.New("federation: validator needs a key source")
	ErrValidatorIssuers  = errors.New("federation: validator needs at least one tenant with an allow-listed issuer")
)

// NewValidator validates cfg and returns the validator. Every tenant/issuer
// pair in cfg.TenantIssuers is copied defensively, so mutating the map or
// slices cfg was built from after construction has no effect on the
// validator.
func NewValidator(cfg Config) (*Validator, error) {
	if cfg.Audience == "" {
		return nil, ErrValidatorAudience
	}
	if cfg.Keys == nil {
		return nil, ErrValidatorKeys
	}
	if len(cfg.TenantIssuers) == 0 {
		return nil, ErrValidatorIssuers
	}
	tenantIssuers := make(map[values.TenantId]map[string]bool, len(cfg.TenantIssuers))
	for tenant, issuers := range cfg.TenantIssuers {
		if err := tenant.Validate(); err != nil {
			return nil, fmt.Errorf("%w: tenant %q: %v", ErrValidatorIssuers, tenant, err)
		}
		if len(issuers) == 0 {
			return nil, fmt.Errorf("%w: tenant %q has no allow-listed issuer", ErrValidatorIssuers, tenant)
		}
		set := make(map[string]bool, len(issuers))
		for _, iss := range issuers {
			if iss == "" {
				return nil, fmt.Errorf("%w: tenant %q has an empty issuer", ErrValidatorIssuers, tenant)
			}
			set[iss] = true
		}
		tenantIssuers[tenant] = set
	}
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	maxBytes := cfg.MaxAssertionBytes
	if maxBytes <= 0 {
		maxBytes = maxAssertionBytesDefault
	}
	return &Validator{
		tenantIssuers: tenantIssuers,
		audience:      cfg.Audience,
		keys:          cfg.Keys,
		now:           now,
		leeway:        cfg.Leeway,
		maxBytes:      maxBytes,
	}, nil
}

// Validate verifies raw as a compact JWS enterprise identity assertion and
// returns the [trust.Principal] it authenticates.
//
// Validation order matters for the security posture, not just for error
// messages: the header and payload are decoded (still untrusted) only far
// enough to learn the issuer and key id, the issuer is checked against the
// tenant it claims before any key lookup happens, the signature is verified
// against a key [KeySource] resolved for that exact issuer, and only then
// are the remaining claims (audience, validity window, assurance) trusted.
func (v *Validator) Validate(ctx context.Context, raw string) (*trust.Principal, error) {
	h, signingInput, payload, sig, err := splitJWS(raw, v.maxBytes)
	if err != nil {
		return nil, err
	}
	if h.Type != "" && h.Type != "JWT" {
		return nil, fmt.Errorf("%w: unexpected typ %q", ErrMalformedAssertion, h.Type)
	}
	alg := Algorithm(h.Algorithm)
	if !alg.valid() {
		return nil, fmt.Errorf("%w: %q", ErrUnsupportedAlgorithm, h.Algorithm)
	}
	if h.KeyID == "" {
		return nil, fmt.Errorf("%w: no key id", ErrMalformedAssertion)
	}

	var claims Claims
	dec := json.NewDecoder(bytes.NewReader(payload))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&claims); err != nil {
		return nil, fmt.Errorf("%w: claims: %v", ErrMalformedAssertion, err)
	}
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("%w: trailing content after claims", ErrMalformedAssertion)
	}

	if claims.Issuer == "" {
		return nil, fmt.Errorf("%w: iss", ErrAssertionClaims)
	}
	tenant := values.TenantId(claims.Tenant)
	if err := tenant.Validate(); err != nil {
		return nil, fmt.Errorf("%w: tenant: %v", ErrAssertionClaims, err)
	}
	allowed, tenantKnown := v.tenantIssuers[tenant]
	if !tenantKnown || !allowed[claims.Issuer] {
		if v.issuerKnownElsewhere(claims.Issuer, tenant) {
			return nil, fmt.Errorf("%w: issuer %q is not allow-listed for tenant %q", ErrTenantBinding, claims.Issuer, tenant)
		}
		return nil, fmt.Errorf("%w: %q", ErrAssertionIssuer, claims.Issuer)
	}

	key, err := v.keys.ResolveKey(ctx, claims.Issuer, KeyID(h.KeyID))
	if err != nil {
		return nil, err
	}
	if key.Algorithm != alg {
		return nil, fmt.Errorf("%w: header declares %q, resolved key is %q", ErrUnsupportedAlgorithm, alg, key.Algorithm)
	}
	now := v.now().UTC()
	if !key.validAt(now) {
		return nil, fmt.Errorf("%w: key %q for issuer %q", ErrKeyExpired, h.KeyID, claims.Issuer)
	}
	if err := verifySignature(alg, key.Public, signingInput, sig); err != nil {
		return nil, err
	}

	// Everything below this line trusts the claims: the signature over them
	// has verified against a key bound to the exact issuer they claim.
	if claims.Subject == "" {
		return nil, fmt.Errorf("%w: sub", ErrAssertionClaims)
	}
	if claims.Audience != v.audience {
		return nil, fmt.Errorf("%w: %q", ErrAssertionAudience, claims.Audience)
	}
	if claims.IssuedAtUnix == 0 || claims.ExpiresAtUnix == 0 {
		return nil, fmt.Errorf("%w: validity window is not stated", ErrAssertionExpired)
	}
	issuedAt := time.Unix(claims.IssuedAtUnix, 0).UTC()
	expiresAt := time.Unix(claims.ExpiresAtUnix, 0).UTC()
	if !expiresAt.After(issuedAt) {
		return nil, fmt.Errorf("%w: exp does not follow iat", ErrAssertionExpired)
	}
	if !now.Before(expiresAt.Add(v.leeway)) {
		return nil, fmt.Errorf("%w: expired at %s", ErrAssertionExpired, expiresAt.Format(time.RFC3339))
	}
	notBefore := issuedAt
	if claims.NotBeforeUnix != 0 {
		notBefore = time.Unix(claims.NotBeforeUnix, 0).UTC()
	}
	if now.Before(notBefore.Add(-v.leeway)) {
		return nil, fmt.Errorf("%w: not valid until %s", ErrAssertionExpired, notBefore.Format(time.RFC3339))
	}

	assurance, ok := parseAssurance(claims.Assurance)
	if !ok {
		return nil, fmt.Errorf("%w: assurance %q", ErrAssertionAssurance, claims.Assurance)
	}
	kind, ok := parseSubjectKind(claims.SubjectKind)
	if !ok {
		return nil, fmt.Errorf("%w: sub_kind %q", ErrAssertionClaims, claims.SubjectKind)
	}

	digest := sha256.Sum256([]byte(raw))
	credentialDigest := "cred:sha256:" + hex.EncodeToString(digest[:])
	// Federation authenticates the identity; it does not itself run a
	// server-side session. Until TRUST-003 attaches a real session to this
	// principal, the session reference is a stable, credential-derived
	// placeholder rather than a caller-suppliable value, so the printable,
	// non-empty invariant [trust.NewPrincipal] enforces is never satisfied
	// by anything but server-derived material.
	sessionSeed := sha256.Sum256([]byte("federation-pending-session:" + raw))
	sessionRef := "federation:" + hex.EncodeToString(sessionSeed[:16])

	principal, err := trust.NewPrincipal(trust.PrincipalSpec{
		Tenant:               tenant,
		Subject:              claims.Subject,
		SubjectKind:          kind,
		OrganizationScopeID:  claims.OrganizationScopeID,
		Roles:                claims.Roles,
		AuthorityRefs:        claims.AuthorityRefs,
		Purposes:             claims.Purposes,
		AuthenticationMethod: trust.AuthenticationMethodBearerToken,
		Assurance:            assurance,
		SessionRef:           sessionRef,
		DelegationRefs:       claims.DelegationRefs,
		IssuedAt:             issuedAt,
		ExpiresAt:            expiresAt,
		CredentialDigest:     credentialDigest,
	})
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrMalformedAssertion, err)
	}
	return principal, nil
}

// issuerKnownElsewhere reports whether issuer is allow-listed for some
// tenant other than exclude. It exists only to distinguish, in error text,
// an issuer mix-up from an issuer no configured tenant recognizes at all;
// both outcomes deny identically.
func (v *Validator) issuerKnownElsewhere(issuer string, exclude values.TenantId) bool {
	for tenant, set := range v.tenantIssuers {
		if tenant == exclude {
			continue
		}
		if set[issuer] {
			return true
		}
	}
	return false
}

// parseAssurance maps the wire spelling of an assurance level. It mirrors
// the unexported mapping in package trust; duplicating it here (rather than
// exporting it from the frozen package) keeps the wire vocabulary the
// federation adapter accepts identical to the dev verifier's without adding
// a new export surface to a package this work may only import.
func parseAssurance(s string) (trust.Assurance, bool) {
	switch s {
	case "low":
		return trust.AssuranceLow, true
	case "substantial":
		return trust.AssuranceSubstantial, true
	case "high":
		return trust.AssuranceHigh, true
	default:
		return trust.AssuranceUnspecified, false
	}
}

// parseSubjectKind maps the wire spelling of a subject kind.
func parseSubjectKind(s string) (trust.SubjectKind, bool) {
	switch s {
	case "human":
		return trust.SubjectKindHuman, true
	case "service":
		return trust.SubjectKindService, true
	case "agent":
		return trust.SubjectKindAgent, true
	case "integration":
		return trust.SubjectKindIntegration, true
	default:
		return trust.SubjectKindUnspecified, false
	}
}
