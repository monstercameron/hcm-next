// Package oidc implements AUTHN-002: the OpenID Connect authorization-code
// flow with PKCE (S256 only), as pure functions plus a small state machine,
// entirely on the Go standard library.
//
// # Why no go-oidc or x/oauth2
//
// LIB-010 qualified coreos/go-oidc and golang.org/x/oauth2 for this exact
// job and then deferred both: the decision record (see
// tools/policy/libqualification's TestOIDCBackendQualification) requires
// that neither module be imported anywhere in this tree, and that the
// authorization-code/PKCE flow be built behind the internal/trust/federation
// adapter port instead. This package is that build: every HTTP call this
// flow makes goes through the injected [TokenExchanger] port (an in-memory
// fake identity provider in tests; [HTTPTokenExchanger] over net/http in
// production), and every cryptographic operation uses crypto/rand,
// crypto/rsa, crypto/ecdsa or crypto/ed25519 directly.
//
// # Relationship to internal/trust/federation
//
// internal/trust/federation.Validator already verifies a *bearer assertion*
// JWS against a governed per-tenant issuer allow-list (TRUST-002). This
// package is a different, earlier stage of the same identity story: it runs
// the actual browser redirect dance with an OIDC identity provider and
// produces the ID token that a validator like that one would examine. It
// reuses internal/trust/federation's exported vocabulary where the two
// stages agree (the same closed [trustfederation.Algorithm] set, the same
// [trustfederation.SigningKey] shape, the same [trustfederation.IssuerResolver]
// port that AUTHN-001's issuer registry already implements as
// issuerregistry.Resolver) but implements its own compact-JWS decode and
// signature verification for ID tokens: [trustfederation]'s own JWS parser
// and verifier (splitJWS, verifySignature) are unexported, so a second
// package validating a differently-shaped token (nonce, at_hash, single
// audience-membership semantics instead of Claims' strict single audience
// string) cannot reuse them without widening that package's export surface.
// Duplicating the three-algorithm verification switch here, rather than
// exporting it, keeps internal/trust/federation's JOSE surface exactly as
// narrow as TRUST-002 designed it; internal/trust/federation.assertion.go's
// own parseAssurance already sets this precedent for this codebase.
//
// # Issuer records only pin verification material, not endpoints
//
// AUTHN-001's issuerregistry.Issuer pins an issuer's signing keys,
// algorithm set, claim mappings, clock skew and metadata staleness bound --
// exactly what a token/assertion *validator* needs. It does not carry an
// authorization endpoint, a token endpoint or a redirect URI: those are
// OIDC-discovery-document facts, and no discovery client is qualified
// (LIB-010 defers go-oidc, which is what would normally fetch
// /.well-known/openid-configuration). Consistent with how the issuer
// registry pins JWKS material rather than fetching it live, this package
// requires those additional facts to be pinned too, via [ClientRegistration]:
// a per-(tenant, issuer) record supplying the client_id, optional
// client_secret, redirect_uri, and the authorization/token endpoint URLs,
// looked up through the injected [ClientSource] port. [ClientRegistration.RedirectURI]
// is the redirect_uri this package's authorization request always carries;
// nothing this package does ever takes a redirect_uri from an incoming
// request, which is the control this package's callback handling relies on
// to resist redirect-URI confusion.
//
// # Code verifier custody
//
// [Flow] never persists a PKCE code_verifier in cleartext. Instead, one
// authorization attempt's verifier is derived deterministically as
// HMAC-SHA256(flow secret, state) -- see [deriveVerifier] -- and only its
// S256 digest (the code_challenge RFC 7636 already defines as a digest of
// the verifier) is written to the pending-authorization record a
// [StateStore] holds, alongside that record's TTL. A caller who can read
// every row a [StateStore] implementation ever persists -- an operator with
// database access, a backup, a logging pipeline -- learns nothing that lets
// them complete someone else's token exchange, because the verifier itself
// is reconstructible only by a party that also holds the flow's secret. A
// production deployment that runs more than one process must configure
// [FlowConfig.Secret] to the same shared value across every replica, since
// [Flow.BeginAuthorization] and [Flow.HandleCallback] for one authorization
// attempt may land on different instances.
//
// # What this package does not do
//
// It does not establish a server-side session (AUTHN-004), does not decide
// step-up or outage posture (internal/trust/stepup, internal/trust/outage),
// and is not wired into internal/humanwork/workspace's dev browser login.
// See the AUTHN-002 delivery report for the concrete integration point.
package oidc
