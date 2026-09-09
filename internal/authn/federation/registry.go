package federation

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

var (
	ErrInvalidProfile  = errors.New("federation registry: invalid profile")
	ErrUnknownIssuer   = errors.New("federation registry: unknown issuer")
	ErrWrongTenant     = errors.New("federation registry: issuer is not registered for tenant")
	ErrAlgorithm       = errors.New("federation registry: algorithm is not permitted")
	ErrAudience        = errors.New("federation registry: audience is not permitted")
	ErrStaleMetadata   = errors.New("federation registry: metadata is stale or expired")
	ErrKeyRollover     = errors.New("federation registry: signing key version changed")
	ErrVersionConflict = errors.New("federation registry: profile version conflict")
)

// Protocol identifies the federation assertion profile.
type Protocol string

const (
	ProtocolOIDC Protocol = "oidc"
	ProtocolSAML Protocol = "saml"
)

// Profile is the tenant-owned, versioned contract for one issuer. Times are
// UTC instants; ExpiresAt is the hard expiry of the profile and MetadataUntil
// bounds how long discovery/JWKS metadata may be used.
type Profile struct {
	Tenant        values.TenantId
	Issuer        string
	Protocol      Protocol
	Version       uint64
	Owner         string
	Scopes        []string
	Assurance     trust.Assurance
	Audience      string
	Algorithms    []string
	IssuedAt      time.Time
	ExpiresAt     time.Time
	MetadataUntil time.Time
	KeyVersion    uint64
}

// Assertion is the signed assertion metadata an adapter presents for lookup.
// It intentionally contains no raw claims or key material.
type Assertion struct {
	Tenant         values.TenantId
	Issuer         string
	Algorithm      string
	Audience       string
	ProfileVersion uint64
	KeyVersion     uint64
	At             time.Time
}

// Registry is safe for concurrent reads and updates. An update replaces a
// complete profile, so readers never observe a partial key rollover.
type Registry struct {
	mu       sync.RWMutex
	profiles map[values.TenantId]map[string]Profile
}

// IssuerProfile is a compatibility name for Profile when the registry is
// consumed by configuration loaders.
type IssuerProfile = Profile

func New() *Registry { return &Registry{profiles: make(map[values.TenantId]map[string]Profile)} }

// NewRegistry is the descriptive constructor name used by edge callers.
func NewRegistry() *Registry { return New() }

// Register adds a profile. A nonzero version must be newer than an existing
// profile; use Replace for an explicit compare-and-swap update.
func (r *Registry) Register(p Profile) error {
	if err := validateProfile(p); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.profiles == nil {
		r.profiles = make(map[values.TenantId]map[string]Profile)
	}
	set := r.profiles[p.Tenant]
	if old, ok := set[p.Issuer]; ok && p.Version <= old.Version {
		return fmt.Errorf("%w: have %d, got %d", ErrVersionConflict, old.Version, p.Version)
	}
	if set == nil {
		set = make(map[string]Profile)
		r.profiles[p.Tenant] = set
	}
	set[p.Issuer] = clone(p)
	return nil
}

// Replace atomically replaces a profile when its current version equals want.
func (r *Registry) Replace(p Profile, want uint64) error {
	if err := validateProfile(p); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	set := r.profiles[p.Tenant]
	old, ok := set[p.Issuer]
	if !ok {
		return ErrUnknownIssuer
	}
	if old.Version != want || p.Version <= want {
		return fmt.Errorf("%w: have %d, want %d", ErrVersionConflict, old.Version, want)
	}
	set[p.Issuer] = clone(p)
	return nil
}

// Lookup returns a defensive copy of a tenant's profile.
func (r *Registry) Lookup(tenant values.TenantId, issuer string) (Profile, error) {
	r.mu.RLock()
	p, ok := r.profiles[tenant][issuer]
	r.mu.RUnlock()
	if !ok {
		if r.knownIssuer(issuer) {
			return Profile{}, ErrWrongTenant
		}
		return Profile{}, ErrUnknownIssuer
	}
	return clone(p), nil
}

// Validate checks issuer, tenant binding, algorithm, audience, profile/key
// versions and both profile freshness boundaries as one atomic read.
func (r *Registry) Validate(a Assertion) (Profile, error) {
	if err := a.Tenant.Validate(); err != nil {
		return Profile{}, fmt.Errorf("%w: tenant: %v", ErrInvalidProfile, err)
	}
	r.mu.RLock()
	p, ok := r.profiles[a.Tenant][a.Issuer]
	if ok {
		p = clone(p)
	}
	r.mu.RUnlock()
	if !ok {
		if r.knownIssuer(a.Issuer) {
			return Profile{}, ErrWrongTenant
		}
		return Profile{}, ErrUnknownIssuer
	}
	if a.Algorithm == "" || !slices.Contains(p.Algorithms, a.Algorithm) {
		return Profile{}, ErrAlgorithm
	}
	if a.Audience == "" || a.Audience != p.Audience {
		return Profile{}, ErrAudience
	}
	at := a.At
	if at.IsZero() {
		at = time.Now()
	}
	at = at.UTC()
	if p.ExpiresAt.IsZero() || !at.Before(p.ExpiresAt.UTC()) || (!p.MetadataUntil.IsZero() && !at.Before(p.MetadataUntil.UTC())) || (!p.IssuedAt.IsZero() && at.Before(p.IssuedAt.UTC())) {
		return Profile{}, ErrStaleMetadata
	}
	if a.ProfileVersion != 0 && a.ProfileVersion != p.Version {
		return Profile{}, ErrVersionConflict
	}
	if a.KeyVersion != 0 && a.KeyVersion != p.KeyVersion {
		return Profile{}, ErrKeyRollover
	}
	return p, nil
}

func (r *Registry) knownIssuer(issuer string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, set := range r.profiles {
		if _, ok := set[issuer]; ok {
			return true
		}
	}
	return false
}

func validateProfile(p Profile) error {
	if err := p.Tenant.Validate(); err != nil {
		return fmt.Errorf("%w: tenant: %v", ErrInvalidProfile, err)
	}
	if strings.TrimSpace(p.Issuer) == "" || p.Version == 0 || strings.TrimSpace(p.Owner) == "" || p.Protocol != ProtocolOIDC && p.Protocol != ProtocolSAML || p.Audience == "" || len(p.Algorithms) == 0 || p.Assurance == trust.AssuranceUnspecified || p.ExpiresAt.IsZero() || p.KeyVersion == 0 {
		return ErrInvalidProfile
	}
	seen := make(map[string]bool, len(p.Algorithms))
	for _, alg := range p.Algorithms {
		if strings.TrimSpace(alg) == "" || seen[alg] {
			return ErrInvalidProfile
		}
		seen[alg] = true
	}
	return nil
}

func clone(p Profile) Profile {
	p.Scopes = slices.Clone(p.Scopes)
	p.Algorithms = slices.Clone(p.Algorithms)
	return p
}
