package federation

import (
	"context"
	"fmt"
)

// Valid reports whether a is one of this package's closed algorithm set
// (RS256, ES256, EdDSA). It exports [Algorithm.valid] for a governance
// record outside this package -- AUTHN-001's tenant federation issuer
// registry (internal/authn/issuerregistry) is the first caller -- that
// needs to validate a declared algorithm set against the identical closed
// vocabulary this package enforces at verification time, without
// duplicating the switch statement (and risking it drifting out of sync
// with [verifySignature]'s own cases).
func (a Algorithm) Valid() bool { return a.valid() }

// IssuerResolver looks up the currently active, tenant-governed
// verification material for one issuer. It is the seam between a tenant
// federation issuer registry -- AUTHN-001's internal/authn/issuerregistry
// implements it -- and this package's [KeySource] port: this package never
// needs to know how (or whether) issuers are governed, published, activated
// or persisted, and the registry never needs to know about JOSE key
// resolution.
//
// An implementation returns every currently-valid signing key for issuer
// (there may be more than one during a key rotation overlap window); it is
// [RegistryKeySource] that then picks the one matching a specific key id.
type IssuerResolver interface {
	ResolveIssuerKeys(ctx context.Context, issuer string) ([]SigningKey, error)
}

// RegistryKeySource adapts an [IssuerResolver] into a [KeySource]. It is the
// production seam a composition root wires a [Validator] to: everything
// this package does with the resulting keys (algorithm-confusion rejection,
// rotation-window validity, signature verification) is exactly what it does
// with [StaticKeySource]'s development keys, unchanged.
type RegistryKeySource struct {
	resolver IssuerResolver
}

// NewRegistryKeySource returns a [KeySource] backed by resolver. A nil
// resolver is accepted and always fails resolution -- the same
// fail-closed shape [NewValidator] enforces by refusing a nil [KeySource]
// outright, kept here as a defensive floor for a caller that constructs
// [RegistryKeySource] directly rather than through [NewValidator].
func NewRegistryKeySource(resolver IssuerResolver) *RegistryKeySource {
	return &RegistryKeySource{resolver: resolver}
}

// ResolveKey implements [KeySource]. It asks resolver for every currently
// valid key for issuer and returns the one matching keyID; a rotation
// overlap window is exactly why more than one key can come back for the
// same issuer, and only the one the assertion's own header names is ever
// used.
func (s *RegistryKeySource) ResolveKey(ctx context.Context, issuer string, keyID KeyID) (SigningKey, error) {
	if s == nil || s.resolver == nil {
		return SigningKey{}, fmt.Errorf("%w: no issuer resolver configured", ErrKeyNotFound)
	}
	keys, err := s.resolver.ResolveIssuerKeys(ctx, issuer)
	if err != nil {
		return SigningKey{}, err
	}
	for _, k := range keys {
		if k.ID == keyID {
			return k, nil
		}
	}
	return SigningKey{}, fmt.Errorf("%w: issuer %q key %q", ErrKeyNotFound, issuer, keyID)
}
