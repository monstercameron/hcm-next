package trust

import (
	"context"
	"errors"
	"slices"
	"strings"
)

// Credential is what a transport extracted from an authenticated connection.
// It carries proof material only. It never carries an assertion about who the
// caller is: a token's contents become trusted only after a [Verifier] has
// checked the signature, issuer, audience and validity window.
type Credential struct {
	// Scheme is the authorization scheme, e.g. "Bearer". Comparison is
	// case-insensitive.
	Scheme string
	// Token is the raw credential material.
	Token string
	// PeerIdentity is the transport-authenticated peer identity (an mTLS
	// certificate subject, a workload identity). Empty when the transport did
	// not authenticate a peer.
	PeerIdentity string
	// Audience is the audience the receiving listener declares for itself. It
	// is server configuration, not a caller-supplied value.
	Audience string
}

// Verification failures. Transports project these onto the owned error model;
// they are distinguished here so that authentication telemetry can tell a
// clock-skew problem from a forged signature without parsing strings.
var (
	ErrNoCredential            = errors.New("trust: request carries no credential")
	ErrUnsupportedScheme       = errors.New("trust: credential scheme is not supported")
	ErrInvalidCredential       = errors.New("trust: credential is malformed or its signature does not verify")
	ErrExpiredCredential       = errors.New("trust: credential is expired or not yet valid")
	ErrWrongIssuer             = errors.New("trust: credential issuer is not the configured issuer")
	ErrWrongAudience           = errors.New("trust: credential audience is not this listener")
	ErrMissingAssurance        = errors.New("trust: credential carries no authentication assurance evidence")
	ErrCallerSelectedAuthority = errors.New("trust: caller attempted to select trusted context")
)

// Verifier turns a presented [Credential] into a verified [Principal]. It is
// the only way a Principal comes into existence in a running server.
//
// An implementation must fail closed: any doubt about the signature, issuer,
// audience, validity window or assurance evidence is an error, never a
// downgraded principal.
type Verifier interface {
	Verify(ctx context.Context, cred Credential) (*Principal, error)
}

// VerifierFunc adapts a function to the [Verifier] interface.
type VerifierFunc func(ctx context.Context, cred Credential) (*Principal, error)

// Verify calls f.
func (f VerifierFunc) Verify(ctx context.Context, cred Credential) (*Principal, error) {
	return f(ctx, cred)
}

// reservedMetadataKeys enumerates the lowercase transport metadata and header
// names that describe trusted context. A caller may never send one: the value
// they would carry is derived server-side from the verified credential, so a
// request that contains one is either a misconfigured client or an attempt to
// select authority, and both are rejected.
//
// The list is exhaustive over the trusted-boundary table in
// planning/specs/http-grpc-endpoint-contract.md#trusted-request-boundary.
var reservedMetadataKeys = []string{
	"x-assurance",
	"x-authority",
	"x-delegation",
	"x-evidence-id",
	"x-hcm-assurance",
	"x-hcm-authority",
	"x-hcm-organization-scope",
	"x-hcm-placement",
	"x-hcm-principal",
	"x-hcm-purpose",
	"x-hcm-roles",
	"x-hcm-session",
	"x-hcm-tenant",
	"x-legal-context",
	"x-organization-scope",
	"x-placement",
	"x-principal",
	"x-processing-placement",
	"x-purpose",
	"x-roles",
	"x-session",
	"x-source-authority",
	"x-subject",
	"x-tenant",
	"x-tenant-id",
}

// ReservedMetadataKeys returns a copy of the reserved trusted-context metadata
// and header names, sorted. Both the gRPC interceptor chain and the HTTP edge
// screen incoming metadata against this one list, which is what makes the
// rejection identical on both transports.
func ReservedMetadataKeys() []string { return slices.Clone(reservedMetadataKeys) }

// IsReservedMetadataKey reports whether name (compared case-insensitively) is
// a reserved trusted-context name.
func IsReservedMetadataKey(name string) bool {
	return slices.Contains(reservedMetadataKeys, strings.ToLower(strings.TrimSpace(name)))
}

// RejectCallerSelectedAuthority returns the sorted set of reserved
// trusted-context names present in names, or nil when there are none. A
// non-empty result means the request must be rejected before authentication
// evidence is recorded.
func RejectCallerSelectedAuthority(names []string) []string {
	var found []string
	for _, n := range names {
		if IsReservedMetadataKey(n) {
			found = append(found, strings.ToLower(strings.TrimSpace(n)))
		}
	}
	if len(found) == 0 {
		return nil
	}
	slices.Sort(found)
	return slices.Compact(found)
}
