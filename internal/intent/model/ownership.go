package model

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

var (
	ErrInvalidOwnership = errors.New("model: invalid contact/address ownership")
	ErrUnknownScope     = errors.New("model: unknown ownership scope")
)

// FactKind distinguishes communication endpoints from postal facts. The
// distinction prevents a home-address correction from being interpreted as a
// work-location correction.
type FactKind string

const (
	FactContactPoint FactKind = "CONTACT_POINT"
	FactAddress      FactKind = "ADDRESS"
)

// Scope is the business relationship that owns a fact, not a nullable foreign
// key convention.
type Scope string

const (
	ScopePersonalContact   Scope = "PERSONAL_CONTACT"
	ScopeWorkContact       Scope = "WORK_CONTACT"
	ScopeEmploymentContact Scope = "EMPLOYMENT_CONTACT"
	ScopeHomeAddress       Scope = "HOME_ADDRESS"
	ScopeWorkAddress       Scope = "WORK_ADDRESS"
	ScopeTaxResidence      Scope = "TAX_RESIDENCE"
)

func (s Scope) Valid() bool {
	switch s {
	case ScopePersonalContact, ScopeWorkContact, ScopeEmploymentContact,
		ScopeHomeAddress, ScopeWorkAddress, ScopeTaxResidence:
		return true
	default:
		return false
	}
}

func (k FactKind) Valid() bool { return k == FactContactPoint || k == FactAddress }

type ResolutionStatus string

const (
	ScopeResolved ResolutionStatus = "RESOLVED"
	UnknownScope  ResolutionStatus = "UNKNOWN_SCOPE"
)

// FactSelector is the non-sensitive identity and relationship context used to
// resolve ownership. It intentionally carries no endpoint value.
type FactSelector struct {
	Kind          FactKind
	PersonRef     values.EntityRef
	EmploymentRef values.EntityRef
	Scope         Scope
	Customer      string
	Country       string
	Alias         string
}

func (s FactSelector) Validate() error {
	if !s.Kind.Valid() || !s.Scope.Valid() || strings.TrimSpace(s.Customer) == "" || strings.TrimSpace(s.Country) == "" {
		return fmt.Errorf("%w: kind, scope, customer and country are required", ErrInvalidOwnership)
	}
	if err := s.PersonRef.Validate(); err != nil || s.PersonRef.Kind != values.Kind("person") {
		return fmt.Errorf("%w: person reference is required", ErrInvalidOwnership)
	}
	if requiresEmployment(s.Scope) {
		if err := s.EmploymentRef.Validate(); err != nil || s.EmploymentRef.Kind != values.Kind("employment") || s.EmploymentRef.Tenant != s.PersonRef.Tenant {
			return fmt.Errorf("%w: employment scope requires same-tenant employment reference", ErrInvalidOwnership)
		}
	}
	return nil
}

func requiresEmployment(s Scope) bool {
	return s == ScopeWorkContact || s == ScopeEmploymentContact || s == ScopeWorkAddress || s == ScopeTaxResidence
}

// OwnershipBinding is a reviewed, versioned policy row. '*' is an explicit
// wildcard for customer or country; an empty value is never an implicit
// wildcard.
type OwnershipBinding struct {
	Kind                 FactKind
	Scope                Scope
	Customer             string
	Country              string
	OwnerKind            values.Kind
	OwnerRef             values.EntityRef
	Role                 string
	AuthorityRef         string
	Classification       string
	Aliases              []string
	PermittedDerivations []string
	CorrectionPolicy     string
	ExportPolicy         string
	MergePolicy          string
}

func (b OwnershipBinding) Validate() error {
	if !b.Kind.Valid() || !b.Scope.Valid() || (b.Customer == "" || b.Country == "") || b.OwnerKind == "" ||
		strings.TrimSpace(b.Role) == "" || strings.TrimSpace(b.AuthorityRef) == "" || strings.TrimSpace(b.Classification) == "" ||
		strings.TrimSpace(b.CorrectionPolicy) == "" || strings.TrimSpace(b.ExportPolicy) == "" || strings.TrimSpace(b.MergePolicy) == "" {
		return fmt.Errorf("%w: binding metadata is incomplete", ErrInvalidOwnership)
	}
	if b.OwnerKind != values.Kind("person") && b.OwnerKind != values.Kind("employment") {
		return fmt.Errorf("%w: owner kind must be person or employment", ErrInvalidOwnership)
	}
	if requiresEmployment(b.Scope) && b.OwnerKind != values.Kind("employment") {
		return fmt.Errorf("%w: employment scope must be employment-owned", ErrInvalidOwnership)
	}
	if !requiresEmployment(b.Scope) && b.OwnerKind != values.Kind("person") {
		return fmt.Errorf("%w: person scope must be person-owned", ErrInvalidOwnership)
	}
	for _, alias := range b.Aliases {
		if strings.TrimSpace(alias) == "" {
			return fmt.Errorf("%w: empty alias", ErrInvalidOwnership)
		}
	}
	for i, alias := range b.Aliases {
		if contains(b.Aliases[i+1:], alias) {
			return fmt.Errorf("%w: duplicate alias %q", ErrInvalidOwnership, alias)
		}
	}
	if b.OwnerRef.Id != "" {
		if err := b.OwnerRef.Validate(); err != nil || b.OwnerRef.Kind != b.OwnerKind {
			return fmt.Errorf("%w: explicit owner reference does not match owner kind", ErrInvalidOwnership)
		}
	}
	return nil
}

type OwnershipResolution struct {
	Status               ResolutionStatus
	WriteAllowed         bool
	OwnerRef             values.EntityRef
	Scope                Scope
	Role                 string
	AuthorityRef         string
	Classification       string
	PermittedDerivations []string
	CorrectionPolicy     string
	ExportPolicy         string
	MergePolicy          string
}

// Policy is an immutable-in-use reviewed policy revision. Callers compile a
// new Policy when a decision changes; no method mutates a policy in place.
type Policy struct {
	Revision string
	Bindings []OwnershipBinding
}

func (p Policy) Validate() error {
	if strings.TrimSpace(p.Revision) == "" || len(p.Bindings) == 0 {
		return fmt.Errorf("%w: policy revision and bindings are required", ErrInvalidOwnership)
	}
	seen := map[string]bool{}
	for _, b := range p.Bindings {
		if err := b.Validate(); err != nil {
			return err
		}
		key := fmt.Sprintf("%s|%s|%s|%s|%s", b.Kind, b.Scope, b.Customer, b.Country, b.OwnerKind)
		if seen[key] {
			return fmt.Errorf("%w: duplicate binding %s", ErrInvalidOwnership, key)
		}
		seen[key] = true
	}
	return nil
}

// Resolve returns UNKNOWN_SCOPE plus ErrUnknownScope when no unique binding
// can answer. The zero-value OwnerRef and WriteAllowed=false are deliberate:
// ambiguity is a safe boundary, not a best-effort write target.
func (p Policy) Resolve(selector FactSelector) (OwnershipResolution, error) {
	unknown := OwnershipResolution{Status: UnknownScope, Scope: selector.Scope, WriteAllowed: false}
	if err := p.Validate(); err != nil {
		return unknown, err
	}
	if err := selector.Validate(); err != nil {
		return unknown, err
	}
	type candidate struct {
		binding OwnershipBinding
		score   int
	}
	var candidates []candidate
	for _, b := range p.Bindings {
		if b.Kind != selector.Kind || b.Scope != selector.Scope || !matches(b.Customer, selector.Customer) || !matches(b.Country, selector.Country) || (selector.Alias != "" && !contains(b.Aliases, selector.Alias)) {
			continue
		}
		score := 0
		if b.Customer == selector.Customer {
			score++
		}
		if b.Country == selector.Country {
			score++
		}
		candidates = append(candidates, candidate{b, score})
	}
	if len(candidates) == 0 {
		return unknown, ErrUnknownScope
	}
	max := candidates[0].score
	for _, c := range candidates[1:] {
		if c.score > max {
			max = c.score
		}
	}
	var best []OwnershipBinding
	for _, c := range candidates {
		if c.score == max {
			best = append(best, c.binding)
		}
	}
	if len(best) != 1 {
		return unknown, ErrUnknownScope
	}
	b := best[0]
	owner := b.OwnerRef
	if owner.Id == "" {
		if b.OwnerKind == values.Kind("person") {
			owner = selector.PersonRef
		} else {
			owner = selector.EmploymentRef
		}
	}
	if err := owner.Validate(); err != nil || owner.Tenant != selector.PersonRef.Tenant || owner.Kind != b.OwnerKind {
		return unknown, fmt.Errorf("%w: binding owner is not a valid same-tenant target", ErrUnknownScope)
	}
	return OwnershipResolution{Status: ScopeResolved, WriteAllowed: true, OwnerRef: owner, Scope: b.Scope, Role: b.Role, AuthorityRef: b.AuthorityRef, Classification: b.Classification, PermittedDerivations: append([]string(nil), b.PermittedDerivations...), CorrectionPolicy: b.CorrectionPolicy, ExportPolicy: b.ExportPolicy, MergePolicy: b.MergePolicy}, nil
}

func matches(pattern, value string) bool { return pattern == value || pattern == "*" }

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

// ContactPointRevision and AddressRevision bind reusable values to the
// resolved relationship. ValueRef is a normalized/provider reference, never a
// raw endpoint copied into an audit explanation.
type ContactPointRevision struct {
	ContactPointRef values.EntityRef
	PersonRef       values.EntityRef
	EmploymentRef   values.EntityRef
	Scope           Scope
	Channel         string
	ValueRef        string
	PurposeTags     []string
	AuthorityRef    string
	Classification  string
	Effective       values.EffectiveInterval
	KnownAt         values.Instant
	Revision        uint64
	CorrectionOf    string
}

func (r ContactPointRevision) Validate() error {
	if r.ContactPointRef.Kind != values.Kind("contact_point") {
		return fmt.Errorf("%w: contact point reference has wrong kind", ErrInvalidOwnership)
	}
	if err := validateRevisionIdentity(r.ContactPointRef, r.PersonRef, r.EmploymentRef, r.Scope, r.Channel, r.ValueRef, r.AuthorityRef, r.Classification, r.Effective, r.KnownAt, r.Revision); err != nil {
		return err
	}
	if len(r.PurposeTags) == 0 {
		return fmt.Errorf("%w: contact point purpose tags are required", ErrInvalidOwnership)
	}
	return nil
}

type AddressRevision struct {
	AddressRef           values.EntityRef
	PersonRef            values.EntityRef
	EmploymentRef        values.EntityRef
	Scope                Scope
	AddressType          string
	NormalizedAddressRef string
	AuthorityRef         string
	Classification       string
	Effective            values.EffectiveInterval
	KnownAt              values.Instant
	Revision             uint64
	CorrectionOf         string
}

func (r AddressRevision) Validate() error {
	if r.AddressRef.Kind != values.Kind("address") {
		return fmt.Errorf("%w: address reference has wrong kind", ErrInvalidOwnership)
	}
	if err := validateRevisionIdentity(r.AddressRef, r.PersonRef, r.EmploymentRef, r.Scope, r.AddressType, r.NormalizedAddressRef, r.AuthorityRef, r.Classification, r.Effective, r.KnownAt, r.Revision); err != nil {
		return err
	}
	return nil
}

func validateRevisionIdentity(ref, person, employment values.EntityRef, scope Scope, value, valueRef, authority, classification string, effective values.EffectiveInterval, knownAt values.Instant, revision uint64) error {
	if err := ref.Validate(); err != nil {
		return fmt.Errorf("%w: invalid fact reference: %v", ErrInvalidOwnership, err)
	}
	if err := person.Validate(); err != nil {
		return fmt.Errorf("%w: invalid person reference: %v", ErrInvalidOwnership, err)
	}
	if ref.Tenant != person.Tenant || !scope.Valid() || strings.TrimSpace(value) == "" || strings.TrimSpace(valueRef) == "" || strings.TrimSpace(authority) == "" || strings.TrimSpace(classification) == "" || revision == 0 {
		return fmt.Errorf("%w: revision identity, scope, value, authority and revision are required", ErrInvalidOwnership)
	}
	if requiresEmployment(scope) && (employment.Validate() != nil || employment.Kind != values.Kind("employment") || employment.Tenant != person.Tenant) {
		return fmt.Errorf("%w: employment relationship is required", ErrInvalidOwnership)
	}
	if err := effective.Validate(); err != nil || !knownAt.IsSet() {
		return fmt.Errorf("%w: effective and known-at coordinates are required", ErrInvalidOwnership)
	}
	return nil
}

// LegacyDisposition tells migration callers whether a legacy row is safe to
// link automatically or must be reviewed before any authoritative write.
type LegacyDisposition string

const (
	LegacyLink   LegacyDisposition = "LINK"
	LegacyReview LegacyDisposition = "REVIEW"
)

type LegacyFact struct {
	ID       string
	Selector FactSelector
}
type MigrationOutcome struct {
	ID          string
	Disposition LegacyDisposition
	Resolution  OwnershipResolution
}

func (p Policy) MigrateLegacy(f LegacyFact) (MigrationOutcome, error) {
	if strings.TrimSpace(f.ID) == "" {
		return MigrationOutcome{}, fmt.Errorf("%w: legacy id is required", ErrInvalidOwnership)
	}
	resolution, err := p.Resolve(f.Selector)
	if errors.Is(err, ErrUnknownScope) {
		return MigrationOutcome{ID: f.ID, Disposition: LegacyReview, Resolution: resolution}, nil
	}
	if err != nil {
		return MigrationOutcome{}, err
	}
	return MigrationOutcome{ID: f.ID, Disposition: LegacyLink, Resolution: resolution}, nil
}

// Digest provides a deterministic policy identity for revision and migration
// evidence. It sorts only set-like metadata and leaves no endpoint payload in
// the explanation surface.
func (p Policy) Digest() string {
	if p.Validate() != nil {
		return ""
	}
	rows := make([]string, 0, len(p.Bindings))
	for _, b := range p.Bindings {
		aliases := append([]string(nil), b.Aliases...)
		sort.Strings(aliases)
		derivations := append([]string(nil), b.PermittedDerivations...)
		sort.Strings(derivations)
		rows = append(rows, strings.Join([]string{string(b.Kind), string(b.Scope), b.Customer, b.Country, string(b.OwnerKind), b.OwnerRef.String(), b.Role, b.AuthorityRef, b.Classification, strings.Join(aliases, ","), strings.Join(derivations, ","), b.CorrectionPolicy, b.ExportPolicy, b.MergePolicy}, "|"))
	}
	sort.Strings(rows)
	sum := sha256.Sum256([]byte(p.Revision + "\n" + strings.Join(rows, "\n")))
	return hex.EncodeToString(sum[:])
}

func (r OwnershipResolution) Explain() string {
	return fmt.Sprintf("ownership status=%s scope=%s owner=%s write=%t authority=%s", r.Status, r.Scope, r.OwnerRef, r.WriteAllowed, r.AuthorityRef)
}
