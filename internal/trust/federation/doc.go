// Package federation validates an enterprise identity assertion (an OIDC ID
// token or an equivalent signed JWT-shaped credential from a third-party
// identity provider) and turns it into an immutable [trust.Principal] via
// the frozen [trust.NewPrincipal] constructor.
//
// Semantic owner: governance-and-trust. Phase: P1A. Todo: TRUST-002.
// Depends on: TRUST-001 (internal/trust), LIB-010, LIB-011.
//
// # Scope
//
// Human Capital Management Suite does not pin an OAuth/OIDC client library or a JOSE library
// (LIB-010 and LIB-011 are not yet qualified, and this package may not add
// one: go.mod is frozen for this work). What this package implements is the
// part of enterprise federation that is pure verification of an
// already-issued assertion, using only stdlib crypto (RSA, ECDSA, Ed25519)
// for the three signature algorithms an enterprise identity provider
// realistically issues (RS256, ES256, EdDSA): issuer allow-listing per
// tenant, audience, expiry/not-before with injected-clock skew tolerance,
// signature verification through the [KeySource] port, required-claim
// presence and tenant binding.
//
// The authorization-code/PKCE redirect dance, state/nonce handling and live
// JWKS retrieval belong to the LIB-010 adapter qualification (go-oidc plus
// x/oauth2) and to a later todo once that dependency is approved; they are
// out of this package's reach without adding a dependency this todo does
// not authorize. [KeySource] is the seam: a production adapter resolves
// live JWKS behind it and everything in this package is unchanged.
//
// # Invariant
//
// Every field [Validator.Validate] hands to [trust.NewPrincipal] is
// server-derived from a signature-verified, claim-validated assertion.
// [Validator] never trusts the unsigned JWS header or payload for anything
// before the signature over it has been checked against a key the
// [KeySource] resolved for the exact issuer the assertion claims — an
// issuer allow-listed for one tenant authorizes nothing for another, which
// is the tenant-binding control that stops an issuer mix-up.
package federation
