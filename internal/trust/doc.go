// Package trust owns the server-derived identity primitives that every HCM
// Next transport, capability and domain consumes instead of raw
// identity-provider claims.
//
// Semantic owner: governance-and-trust. Phase: P1A. Todo: TRUST-001.
//
// The single invariant of this package is that a [Principal] is created only
// from a credential that a [Verifier] has actually verified. There is no code
// path anywhere in HCM Next that turns a caller-supplied header, metadata key,
// query parameter or request-message field into a Principal: the reserved
// names that describe trusted context are enumerated in [ReservedMetadataKeys]
// and transports reject a request that carries any of them.
//
// A Principal is immutable once constructed. Accessors copy every slice, and
// there is no exported mutator. [Principal.Fingerprint] is a canonical digest
// over the trusted fields, so two transports that authenticated the same
// credential can be proven to have derived byte-identical trusted context.
//
// [NewHMACVerifier] is the deterministic development and test verifier
// required by the P1A scope: an HMAC-SHA256 signed bearer token with an
// explicit issuer, audience and expiry. Enterprise federation (OAuth/OIDC/
// SAML, workload identity, mTLS peer identity) arrives with TRUST-002 and
// TRUST-011 and plugs in behind the same [Verifier] port; nothing downstream
// of this package changes when it does.
package trust
