// Package filing owns the versioned, encrypted delivery contract for
// workforce-exchange filings.
package filing

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/monstercameron/hcm-next/internal/governance/legal"
)

// ExchangeKind is the closed set of state workforce filings owned here.
type ExchangeKind string

const (
	StateWage   ExchangeKind = "STATE_WAGE"
	NewHire     ExchangeKind = "NEW_HIRE"
	Withholding ExchangeKind = "WITHHOLDING"

	// Long names are convenient at integration boundaries.
	ExchangeStateWage   = StateWage
	ExchangeNewHire     = NewHire
	ExchangeWithholding = Withholding
)

var (
	ErrInvalidProfile = errors.New("filing: invalid profile")
	ErrProfileMissing = errors.New("filing: no profile governs the requested coordinate")
	ErrMissingElement = errors.New("filing: required element is missing")
	ErrInvalidPayload = errors.New("filing: invalid payload")
)

// Profile pins the schema and endpoint that govern one exchange coordinate.
// EffectiveUntil is exclusive when non-zero.
type Profile struct {
	Kind             ExchangeKind
	Jurisdiction     legal.Jurisdiction
	SchemaVersion    string
	EffectiveFrom    time.Time
	EffectiveUntil   time.Time
	RequiredElements []string
	Endpoint         string
}

func (p Profile) Validate() error {
	if !validKind(p.Kind) {
		return fmt.Errorf("%w: kind %q", ErrInvalidProfile, p.Kind)
	}
	if err := p.Jurisdiction.Validate(); err != nil {
		return fmt.Errorf("%w: jurisdiction: %v", ErrInvalidProfile, err)
	}
	if !p.Jurisdiction.IsStateResolved() {
		return fmt.Errorf("%w: jurisdiction must resolve to a state", ErrInvalidProfile)
	}
	if strings.TrimSpace(p.SchemaVersion) == "" {
		return fmt.Errorf("%w: schema_version is required", ErrInvalidProfile)
	}
	if p.EffectiveFrom.IsZero() {
		return fmt.Errorf("%w: effective_from is required", ErrInvalidProfile)
	}
	if !p.EffectiveUntil.IsZero() && !p.EffectiveFrom.Before(p.EffectiveUntil) {
		return fmt.Errorf("%w: effective window is empty", ErrInvalidProfile)
	}
	if len(p.RequiredElements) == 0 {
		return fmt.Errorf("%w: required_elements are required", ErrInvalidProfile)
	}
	seen := make(map[string]struct{}, len(p.RequiredElements))
	for _, element := range p.RequiredElements {
		if strings.TrimSpace(element) == "" {
			return fmt.Errorf("%w: required element is empty", ErrInvalidProfile)
		}
		if _, ok := seen[element]; ok {
			return fmt.Errorf("%w: duplicate required element %q", ErrInvalidProfile, element)
		}
		seen[element] = struct{}{}
	}
	u, err := url.Parse(p.Endpoint)
	if err != nil || u.Scheme == "" || u.Hostname() == "" || u.User != nil {
		return fmt.Errorf("%w: endpoint is not an absolute trusted reference", ErrInvalidProfile)
	}
	return nil
}

func (p Profile) applies(j legal.Jurisdiction, at time.Time) bool {
	if p.Jurisdiction.Country != j.Country || p.Jurisdiction.State != j.State {
		return false
	}
	if p.Jurisdiction.Locality != "" && p.Jurisdiction.Locality != j.Locality {
		return false
	}
	return !at.Before(p.EffectiveFrom) &&
		(p.EffectiveUntil.IsZero() || at.Before(p.EffectiveUntil))
}

// Digest is a stable digest of the profile, including its required elements.
func (p Profile) Digest() string {
	elements := append([]string(nil), p.RequiredElements...)
	sort.Strings(elements)
	wire := struct {
		Kind, Jurisdiction, SchemaVersion, EffectiveFrom, EffectiveUntil, Endpoint string
		RequiredElements                                                           []string
	}{
		Kind: string(p.Kind), Jurisdiction: p.Jurisdiction.String(), SchemaVersion: p.SchemaVersion,
		EffectiveFrom: p.EffectiveFrom.UTC().Format(time.RFC3339Nano), EffectiveUntil: timeString(p.EffectiveUntil),
		Endpoint: p.Endpoint, RequiredElements: elements,
	}
	raw, _ := json.Marshal(wire)
	sum := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func timeString(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339Nano)
}

// Registry publishes immutable profile revisions and resolves the highest
// schema version effective at a jurisdiction and instant.
type Registry struct {
	mu       sync.RWMutex
	profiles []Profile
}

// NewRegistry returns an empty registry and optionally publishes profiles.
func NewRegistry(profiles ...Profile) (*Registry, error) {
	r := &Registry{}
	for _, profile := range profiles {
		if err := r.Register(profile); err != nil {
			return nil, err
		}
	}
	return r, nil
}

// Register publishes a profile. A duplicate coordinate and window is
// refused, so a previously resolved profile cannot silently change.
func (r *Registry) Register(profile Profile) error {
	if r == nil {
		return fmt.Errorf("%w: nil registry", ErrInvalidProfile)
	}
	if err := profile.Validate(); err != nil {
		return err
	}
	profile.RequiredElements = append([]string(nil), profile.RequiredElements...)
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, existing := range r.profiles {
		if existing.Kind == profile.Kind && existing.Jurisdiction.Equal(profile.Jurisdiction) &&
			existing.EffectiveFrom.Equal(profile.EffectiveFrom) && existing.EffectiveUntil.Equal(profile.EffectiveUntil) {
			return fmt.Errorf("%w: duplicate profile coordinate", ErrInvalidProfile)
		}
	}
	r.profiles = append(r.profiles, profile)
	return nil
}

// Resolve returns a defensive copy of the profile governing j at at.
func (r *Registry) Resolve(kind ExchangeKind, j legal.Jurisdiction, at time.Time) (Profile, error) {
	if r == nil || !validKind(kind) || j.Validate() != nil || at.IsZero() {
		return Profile{}, ErrProfileMissing
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	var best *Profile
	for i := range r.profiles {
		candidate := &r.profiles[i]
		if candidate.Kind != kind || !candidate.applies(j, at) {
			continue
		}
		candidateSpecific := candidate.Jurisdiction.Locality != ""
		bestSpecific := best != nil && best.Jurisdiction.Locality != ""
		if best == nil || (candidateSpecific && !bestSpecific) ||
			(candidateSpecific == bestSpecific && candidate.EffectiveFrom.After(best.EffectiveFrom)) ||
			(candidateSpecific == bestSpecific && candidate.EffectiveFrom.Equal(best.EffectiveFrom) && candidate.SchemaVersion > best.SchemaVersion) {
			best = candidate
		}
	}
	if best == nil {
		return Profile{}, fmt.Errorf("%w: kind=%s jurisdiction=%s instant=%s", ErrProfileMissing, kind, j, at.UTC().Format(time.RFC3339))
	}
	out := *best
	out.RequiredElements = append([]string(nil), best.RequiredElements...)
	return out, nil
}

// ResolveProfile is the explicit profile-resolution spelling.
func (r *Registry) ResolveProfile(kind ExchangeKind, j legal.Jurisdiction, at time.Time) (Profile, error) {
	return r.Resolve(kind, j, at)
}

// NewResearchRegistry seeds the three reviewed state profiles used by the
// filing contract's golden corpus: California, New York, and Washington.
func NewResearchRegistry() (*Registry, error) {
	states := []string{"CA", "NY", "WA"}
	var profiles []Profile
	for _, state := range states {
		j := legal.Jurisdiction{Country: "US", State: state}
		profiles = append(profiles,
			Profile{Kind: StateWage, Jurisdiction: j, SchemaVersion: state + "-UI-WAGE-2026.1", EffectiveFrom: researchStartTime, RequiredElements: []string{"employer_id", "employee_id", "reporting_period", "wages"}, Endpoint: "https://exchange.example.gov/state-wage"},
			Profile{Kind: NewHire, Jurisdiction: j, SchemaVersion: state + "-NEW-HIRE-2026.1", EffectiveFrom: researchStartTime, RequiredElements: []string{"employer_id", "employee_id", "employee_name", "employee_address", "hire_date"}, Endpoint: "https://exchange.example.gov/new-hire"},
			Profile{Kind: Withholding, Jurisdiction: j, SchemaVersion: state + "-WITHHOLDING-2026.1", EffectiveFrom: researchStartTime, RequiredElements: []string{"employer_id", "employee_id", "tax_period", "taxable_wages", "withholding_amount"}, Endpoint: "https://exchange.example.gov/withholding"},
		)
	}
	return NewRegistry(profiles...)
}

// DefaultRegistry is the reviewed three-state profile set.
func DefaultRegistry() (*Registry, error) { return NewResearchRegistry() }

const researchStart = "2026-01-01T00:00:00Z"

var researchStartTime = mustProfileTime(researchStart)

func mustProfileTime(value string) time.Time {
	t, _ := time.Parse(time.RFC3339, value)
	return t
}

func validKind(kind ExchangeKind) bool {
	switch kind {
	case StateWage, NewHire, Withholding:
		return true
	default:
		return false
	}
}
