package issuerregistry

import (
	"context"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rsa"
	"crypto/x509"
	"fmt"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/bundle"
	trustfederation "github.com/monstercameron/human-capital-management-suite/internal/trust/federation"
)

// BundleSource resolves a pinned internal/trust/bundle.Bundle version by
// reference, for an [Issuer] whose [JWKSSource.Kind] is
// [JWKSSourcePinnedBundle]. A [Resolver] with a nil source refuses any
// issuer that names one rather than guessing.
type BundleSource interface {
	LookupBundle(ref string, version int) (*bundle.Bundle, bool)
}

// Resolver adapts this package's governed issuer registry into
// internal/trust/federation.IssuerResolver: the seam that hands the
// federation port an active issuer's verification material. Wire it to a
// [trustfederation.Validator] through [trustfederation.NewRegistryKeySource].
type Resolver struct {
	store   Store
	tenant  values.TenantId // zero value ("") means "resolve across every tenant store knows about"
	bundles BundleSource
}

var _ trustfederation.IssuerResolver = (*Resolver)(nil)

// NewResolver returns a [Resolver] that looks an issuer up across every
// tenant store knows about, matching the tenant-agnostic shape of
// [trustfederation.KeySource.ResolveKey] (issuer alone, no tenant is
// passed). It requires store to additionally provide the cross-tenant
// index [MemoryStore] implements; a store that cannot answer "which
// tenants know this issuer" -- [PGStore], by design -- cannot back a
// multi-tenant [Resolver] at all, so this reports that immediately rather
// than failing confusingly on first use. A single-tenant deployment (the
// realistic PGStore-backed production shape, where the caller already
// knows which tenant a request is for) uses [NewTenantResolver] instead.
func NewResolver(store Store, bundles BundleSource) (*Resolver, error) {
	if store == nil {
		return nil, fmt.Errorf("%w: resolver needs a store", ErrInvalidIssuer)
	}
	if _, ok := store.(crossTenantIndex); !ok {
		return nil, fmt.Errorf("issuerregistry: store does not support multi-tenant issuer resolution; use NewTenantResolver")
	}
	return &Resolver{store: store, bundles: bundles}, nil
}

// NewTenantResolver returns a [Resolver] scoped to exactly one tenant. It
// works with any [Store], including [PGStore], because it never needs to
// ask which tenant an issuer belongs to -- the caller already knows.
func NewTenantResolver(store Store, tenant values.TenantId, bundles BundleSource) *Resolver {
	return &Resolver{store: store, tenant: tenant, bundles: bundles}
}

// ResolveIssuerKeys implements [trustfederation.IssuerResolver]. It looks
// up the currently ACTIVE issuer for issuerURL through [Lookup] -- so
// every refusal [Lookup] can produce (unknown issuer, wrong tenant,
// suspended, retired, not yet activated) propagates here unchanged -- and
// turns its pinned material into [trustfederation.SigningKey] values.
func (r *Resolver) ResolveIssuerKeys(_ context.Context, issuerURL string) ([]trustfederation.SigningKey, error) {
	if r == nil || r.store == nil {
		return nil, fmt.Errorf("issuerregistry: resolver has no store")
	}
	if r.tenant != "" {
		issuer, err := Lookup(r.store, r.tenant, issuerURL)
		if err != nil {
			return nil, err
		}
		return issuerSigningKeys(issuer, r.bundles)
	}

	xt, ok := r.store.(crossTenantIndex)
	if !ok {
		return nil, fmt.Errorf("issuerregistry: store does not support multi-tenant issuer resolution")
	}
	tenants := xt.tenantsForIssuer(issuerURL)
	if len(tenants) == 0 {
		return nil, ErrUnknownIssuer
	}
	var lastErr error
	for _, t := range tenants {
		issuer, err := Lookup(r.store, t, issuerURL)
		if err != nil {
			lastErr = err
			continue
		}
		return issuerSigningKeys(issuer, r.bundles)
	}
	if lastErr == nil {
		lastErr = ErrIssuerNotActive
	}
	return nil, lastErr
}

// issuerSigningKeys turns issuer's pinned material into the
// [trustfederation.SigningKey] values [Resolver.ResolveIssuerKeys] returns.
func issuerSigningKeys(issuer Issuer, bundles BundleSource) ([]trustfederation.SigningKey, error) {
	switch issuer.JWKS.Kind {
	case JWKSSourcePinnedKeys:
		out := make([]trustfederation.SigningKey, 0, len(issuer.JWKS.PinnedKeys))
		for _, k := range issuer.JWKS.PinnedKeys {
			pub, err := x509.ParsePKIXPublicKey(k.PublicKeyDER)
			if err != nil {
				return nil, fmt.Errorf("issuerregistry: pinned key %q: %w", k.KeyID, err)
			}
			out = append(out, trustfederation.SigningKey{
				ID:        trustfederation.KeyID(k.KeyID),
				Algorithm: k.Algorithm,
				Public:    pub,
				NotBefore: k.NotBefore,
				NotAfter:  k.NotAfter,
			})
		}
		return out, nil
	case JWKSSourcePinnedBundle:
		if bundles == nil {
			return nil, fmt.Errorf("issuerregistry: issuer %q pins bundle %q but no bundle source is configured", issuer.IssuerURL, issuer.JWKS.BundleRef)
		}
		b, ok := bundles.LookupBundle(issuer.JWKS.BundleRef, issuer.JWKS.BundleVersion)
		if !ok {
			return nil, fmt.Errorf("issuerregistry: pinned bundle %q version %d not found", issuer.JWKS.BundleRef, issuer.JWKS.BundleVersion)
		}
		return bundleSigningKeys(b)
	default:
		return nil, fmt.Errorf("issuerregistry: issuer %q has unsupported jwks source kind %q", issuer.IssuerURL, issuer.JWKS.Kind)
	}
}

// bundleSigningKeys extracts one [trustfederation.SigningKey] per pinned
// certificate in b whose public key algorithm this package's federation
// port can verify (RSA, ECDSA P-256, or Ed25519 -- the identical closed set
// [trustfederation.Algorithm.Valid] enforces). A certificate's own
// [bundle.PinnedCert.Digest] -- not its subject or serial number -- is its
// [trustfederation.KeyID], keeping the same "pin by digest, never by name"
// discipline internal/trust/bundle's own doc comment describes.
func bundleSigningKeys(b *bundle.Bundle) ([]trustfederation.SigningKey, error) {
	if b == nil {
		return nil, fmt.Errorf("issuerregistry: pinned bundle reference resolved to no bundle")
	}
	var out []trustfederation.SigningKey
	for _, pc := range append(append([]bundle.PinnedCert(nil), b.Roots...), b.Intermediates...) {
		if pc.Cert == nil {
			continue
		}
		alg, ok := algorithmForPublicKey(pc.Cert.PublicKey)
		if !ok {
			continue
		}
		out = append(out, trustfederation.SigningKey{
			ID:        trustfederation.KeyID(pc.Digest),
			Algorithm: alg,
			Public:    pc.Cert.PublicKey,
			NotBefore: b.ActivatesAt,
			NotAfter:  b.ExpiresAt,
		})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("issuerregistry: pinned bundle version %d has no key this package can verify", b.Version)
	}
	return out, nil
}

// algorithmForPublicKey maps a parsed public key to the closed
// [trustfederation.Algorithm] it corresponds to, mirroring
// [trustfederation]'s own verifySignature switch (RSA -> RS256, P-256 ECDSA
// -> ES256, Ed25519 -> EdDSA). Any other key type or curve is not a key
// this package's federation port can ever verify against, so it is skipped
// rather than surfaced as an error.
func algorithmForPublicKey(pub any) (trustfederation.Algorithm, bool) {
	switch k := pub.(type) {
	case *rsa.PublicKey:
		return trustfederation.AlgRS256, true
	case *ecdsa.PublicKey:
		if k.Curve == elliptic.P256() {
			return trustfederation.AlgES256, true
		}
		return "", false
	case ed25519.PublicKey:
		return trustfederation.AlgEdDSA, true
	default:
		return "", false
	}
}
