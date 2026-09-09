package secrets

import (
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/trust/custody"
)

// Resolution errors. All are matchable with errors.Is.
var (
	// ErrNotAuthorized is returned when the requesting workload, tenant,
	// region, purpose, destination or operation is not covered by a grant.
	// It is deliberately one error: a caller learns that it may not resolve
	// the reference, never which dimension it failed on.
	ErrNotAuthorized = errors.New("secrets: not authorized to resolve this reference")
	// ErrUnusableVersion is returned when the reference points at a version
	// that is not in a usable lifecycle state.
	ErrUnusableVersion = errors.New("secrets: secret version is not usable")
	// ErrInvalidPolicy is returned when an access policy cannot be built.
	ErrInvalidPolicy = errors.New("secrets: invalid secret access policy")
	// ErrInvalidResolver is returned when a resolver cannot be built.
	ErrInvalidResolver = errors.New("secrets: invalid resolver")
)

// usableStates are the lifecycle states a consumer may resolve. REQUESTED has
// no value yet; DISABLED and DESTROYED must not be reachable at use time.
// Rotation states stay usable so an overlap window does not break consumers.
func usableState(s State) bool {
	switch s {
	case Active, Rotating, ActiveNew:
		return true
	default:
		return false
	}
}

// AccessGrant is one row of the secret access policy: which workload, in
// which tenant and region, for which purpose and destination, may perform
// which custody operations against which secret.
//
// Every dimension is required. An empty dimension is not a wildcard: a grant
// that does not say what it is for grants nothing.
type AccessGrant struct {
	GrantID     string
	SecretID    string
	Workload    string
	Tenant      string
	Region      string
	Purpose     string
	Destination string
	Operations  []custody.Operation
}

func (g AccessGrant) validate() error {
	for _, item := range []struct{ name, value string }{
		{"grant_id", g.GrantID}, {"secret_id", g.SecretID}, {"workload", g.Workload},
		{"tenant", g.Tenant}, {"region", g.Region}, {"purpose", g.Purpose}, {"destination", g.Destination},
	} {
		if strings.TrimSpace(item.value) == "" || strings.TrimSpace(item.value) != item.value {
			return fmt.Errorf("%w: grant %q has an empty or padded %s", ErrInvalidPolicy, g.GrantID, item.name)
		}
	}
	if len(g.Operations) == 0 {
		return fmt.Errorf("%w: grant %q authorizes no operation", ErrInvalidPolicy, g.GrantID)
	}
	for _, op := range g.Operations {
		switch op {
		case custody.Encrypt, custody.Decrypt, custody.Sign, custody.Verify, custody.LeaseOperation:
		default:
			// Rotate and Revoke are custody-administration capabilities and
			// are deliberately not grantable through a use-time access
			// policy: create/use/rotate/revoke are separate capabilities.
			return fmt.Errorf("%w: grant %q names operation %q, which is not a use-time operation", ErrInvalidPolicy, g.GrantID, op)
		}
	}
	return nil
}

func (g AccessGrant) covers(ref SecretReference, rctx custody.RequestContext, op custody.Operation) bool {
	if g.SecretID != ref.ID || g.Tenant != ref.Tenant || g.Region != ref.Region {
		return false
	}
	if g.Workload != rctx.Workload || g.Tenant != rctx.Tenant || g.Region != rctx.Region ||
		g.Purpose != rctx.Purpose || g.Destination != rctx.Destination {
		return false
	}
	for _, allowed := range g.Operations {
		if allowed == op {
			return true
		}
	}
	return false
}

// AccessPolicy is the immutable set of grants a resolver evaluates. It is
// declarative: nothing here reaches a provider, and nothing here can widen a
// grant at use time.
type AccessPolicy struct{ grants []AccessGrant }

// NewAccessPolicy validates and freezes grants into a policy.
func NewAccessPolicy(grants ...AccessGrant) (*AccessPolicy, error) {
	seen := make(map[string]bool, len(grants))
	out := make([]AccessGrant, 0, len(grants))
	for _, g := range grants {
		if err := g.validate(); err != nil {
			return nil, err
		}
		if seen[g.GrantID] {
			return nil, fmt.Errorf("%w: duplicate grant id %q", ErrInvalidPolicy, g.GrantID)
		}
		seen[g.GrantID] = true
		copied := g
		copied.Operations = append([]custody.Operation(nil), g.Operations...)
		out = append(out, copied)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].GrantID < out[j].GrantID })
	return &AccessPolicy{grants: out}, nil
}

// Authorize returns the identifier of the grant that covers this use, or
// false when none does. It fails closed on a nil policy.
func (p *AccessPolicy) Authorize(ref SecretReference, rctx custody.RequestContext, op custody.Operation) (string, bool) {
	if p == nil {
		return "", false
	}
	for _, g := range p.grants {
		if g.covers(ref, rctx, op) {
			return g.GrantID, true
		}
	}
	return "", false
}

// Port is the provider-neutral custody surface a resolver uses. It is
// satisfied by [custody.Provider]. Keeping the resolver behind this one
// method is what keeps provider-specific vault behaviour on the far side of
// the custody interface: nothing in this package knows what a vault is, and
// there is no operation here that could return a value.
type Port interface {
	IssueLease(ctx custody.Context, object custody.Handle, operation custody.Operation, ttl time.Duration) (custody.Lease, error)
}

// Resolution is what a consumer receives at use time: an authorization to
// perform one bounded operation against one secret version, and nothing else.
// It has no field that can hold secret material, by construction.
type Resolution struct {
	// ReferenceID, Version, Provider and Kind identify what was resolved.
	// Version is the only thing evidence ever needs about the value.
	ReferenceID string
	Version     string
	Provider    string
	Kind        Kind
	// GrantID names the access-policy row that authorized this use.
	GrantID string
	// Operation, LeaseID and ExpiresAt bound the authorization.
	Operation custody.Operation
	LeaseID   string
	ExpiresAt time.Time
	// ContextDigest binds the resolution to the exact workload, tenant,
	// region, purpose and destination it was resolved for.
	ContextDigest [32]byte
	// ResolvedAt is the instant the lease was issued.
	ResolvedAt time.Time
}

// Evidence returns the durable record of the resolution. It carries the
// reference and the version, never the value, and it is safe to persist in an
// evidence package, a log line or a config snapshot.
func (r Resolution) Evidence() map[string]string {
	return map[string]string{
		"reference":      r.ReferenceID,
		"version":        r.Version,
		"kind":           string(r.Kind),
		"provider":       r.Provider,
		"grant":          r.GrantID,
		"operation":      string(r.Operation),
		"lease":          r.LeaseID,
		"expires_at":     r.ExpiresAt.UTC().Format(time.RFC3339Nano),
		"context_digest": fmt.Sprintf("%x", r.ContextDigest),
	}
}

// String returns a redacted, log-safe description.
func (r Resolution) String() string {
	return fmt.Sprintf("secret resolution ref=%s version=%s kind=%s provider=%s grant=%s operation=%s lease=%s expires=%s",
		r.ReferenceID, r.Version, r.Kind, r.Provider, r.GrantID, r.Operation, r.LeaseID, r.ExpiresAt.UTC().Format(time.RFC3339))
}

// Resolver turns a governed [SecretReference] into a bounded, short-lived
// authorization at the moment of use. It never returns, holds or logs a
// value: there is no code path here through which one could travel.
type Resolver struct {
	policy *AccessPolicy
	port   Port
	now    func() time.Time
}

// NewResolver builds a resolver over an access policy and a custody port.
func NewResolver(policy *AccessPolicy, port Port, now func() time.Time) (*Resolver, error) {
	if policy == nil {
		return nil, fmt.Errorf("%w: no access policy", ErrInvalidResolver)
	}
	if port == nil {
		return nil, fmt.Errorf("%w: no custody port", ErrInvalidResolver)
	}
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &Resolver{policy: policy, port: port, now: now}, nil
}

// Resolve authorizes one bounded use of ref and returns the resolution. The
// order of the checks is part of the contract: shape, then lifecycle state,
// then request context, then authorization, and only then does the custody
// port hear about the request at all.
func (r *Resolver) Resolve(rctx custody.RequestContext, ref SecretReference, op custody.Operation, ttl time.Duration) (Resolution, error) {
	if err := ref.Validate(); err != nil {
		return Resolution{}, err
	}
	if !usableState(ref.State) {
		return Resolution{}, fmt.Errorf("%w: %s is %s", ErrUnusableVersion, ref.ID, ref.State)
	}
	if err := rctx.Validate(); err != nil {
		return Resolution{}, err
	}
	if ttl <= 0 {
		return Resolution{}, fmt.Errorf("%w: lease duration must be positive", ErrInvalidResolver)
	}
	grantID, ok := r.policy.Authorize(ref, rctx, op)
	if !ok {
		return Resolution{}, fmt.Errorf("%w: %s", ErrNotAuthorized, ref.ID)
	}

	handle := custody.Handle{ID: ref.ID, Kind: custody.Secret, Version: ref.Version, Tenant: ref.Tenant, Region: ref.Region}
	if err := handle.Validate(); err != nil {
		return Resolution{}, err
	}
	lease, err := r.port.IssueLease(custody.Context{RequestContext: rctx}, handle, op, ttl)
	if err != nil {
		return Resolution{}, err
	}
	now := r.now().UTC()
	if err := lease.Validate(now); err != nil {
		return Resolution{}, err
	}
	if lease.Handle != handle || lease.Operation != op || lease.ContextDigest != custody.ContextDigest(rctx) {
		// A provider that widened or redirected the lease is not trusted to
		// have honoured the bound request.
		return Resolution{}, fmt.Errorf("%w: %s", ErrNotAuthorized, ref.ID)
	}
	res := Resolution{
		ReferenceID:   ref.ID,
		Version:       ref.Version,
		Provider:      ref.Provider,
		Kind:          ref.Kind,
		GrantID:       grantID,
		Operation:     op,
		LeaseID:       lease.ID,
		ExpiresAt:     lease.ExpiresAt.UTC(),
		ContextDigest: custody.ContextDigest(rctx),
		ResolvedAt:    now,
	}
	// Defence in depth: the resolution is itself run through the redaction
	// checker, so a future field that could carry material fails here first.
	if err := Check(res); err != nil {
		return Resolution{}, err
	}
	return res, nil
}

// ResolutionCarriesNoMaterial reports whether the [Resolution] type is still
// structurally incapable of carrying secret material: no byte slices, no
// arrays of bytes other than the fixed context digest, and no sensitively
// named field. It exists so the guarantee is checkable rather than a comment.
func ResolutionCarriesNoMaterial() error {
	typ := reflect.TypeOf(Resolution{})
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		if looksSensitiveField(field.Name) && !looksReferenceField(field.Name) {
			return fmt.Errorf("secrets: Resolution.%s is a sensitively named field", field.Name)
		}
		if field.Type.Kind() == reflect.Slice && field.Type.Elem().Kind() == reflect.Uint8 {
			return fmt.Errorf("secrets: Resolution.%s is a byte slice and could carry material", field.Name)
		}
		if field.Type.Kind() == reflect.Array && field.Type.Elem().Kind() == reflect.Uint8 && field.Name != "ContextDigest" {
			return fmt.Errorf("secrets: Resolution.%s is a byte array and could carry material", field.Name)
		}
	}
	return nil
}
