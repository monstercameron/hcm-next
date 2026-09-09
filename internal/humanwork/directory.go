package humanwork

import (
	"sort"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// PrincipalFacts is everything resolution needs to know about one principal.
// It is a projection handed to this package, never fetched by it: the port is
// read-only and the caller owns freshness.
type PrincipalFacts struct {
	PrincipalID string
	// Active reports whether the principal still exists as an actor: employed,
	// not suspended, not departed.
	Active bool
	// Available reports whether the principal can act right now: not on leave,
	// not out of office. An inactive principal is never available.
	Available bool
	// Roles are the roles the principal holds at the effective time.
	Roles []string
	// OrganizationScopeID is the principal's own organizational scope.
	OrganizationScopeID string
	// IdentityAssuranceRef is the assurance level behind the principal's
	// identity, carried into decision evidence.
	IdentityAssuranceRef string
}

// HoldsAll reports whether the principal holds every role in floor.
func (f PrincipalFacts) HoldsAll(floor []string) bool {
	held := make(map[string]bool, len(f.Roles))
	for _, r := range f.Roles {
		held[r] = true
	}
	for _, r := range floor {
		if !held[r] {
			return false
		}
	}
	return true
}

// Missing returns the roles in floor the principal does not hold, sorted.
func (f PrincipalFacts) Missing(floor []string) []string {
	held := make(map[string]bool, len(f.Roles))
	for _, r := range f.Roles {
		held[r] = true
	}
	var out []string
	for _, r := range floor {
		if !held[r] {
			out = append(out, r)
		}
	}
	sort.Strings(out)
	return out
}

// Delegation is one bounded transfer of decision authority. Scope is stated
// three ways - which requirements, which authorities and which organizational
// frame - and all three are upper bounds. A delegation may narrow the
// delegator's authority; it can never widen it, which is why Covers checks the
// requirement's authority floor against AllowedRoles rather than against the
// delegate's own roles.
type Delegation struct {
	DelegationID string
	// FromPrincipalID is the delegator whose authority is borrowed.
	FromPrincipalID string
	// ToPrincipalID is the delegate who may act.
	ToPrincipalID string

	// AllowedRequirementIDs are the exact requirements the delegate may decide.
	AllowedRequirementIDs []string
	// AllowedRoles are the authorities the delegation carries.
	AllowedRoles []string
	// Scope is the organizational frame the delegation covers.
	Scope Scope

	NotBefore values.Instant
	Expiry    values.Instant

	PolicyRef string
}

// Validate rejects a delegation that is unidentified, unbounded or eternal.
func (d Delegation) Validate() error {
	for _, f := range []struct{ field, value string }{
		{"delegation.delegation_id", d.DelegationID},
		{"delegation.from_principal_id", d.FromPrincipalID},
		{"delegation.to_principal_id", d.ToPrincipalID},
		{"delegation.policy_ref", d.PolicyRef},
	} {
		if f.value == "" {
			return newError("Validate", f.field, ErrInvalidDelegation, "%s is empty", f.field)
		}
	}
	if d.FromPrincipalID == d.ToPrincipalID {
		return newError("Validate", "delegation.to_principal_id", ErrInvalidDelegation,
			"principal %q cannot delegate to themselves", d.FromPrincipalID)
	}
	if len(d.AllowedRequirementIDs) == 0 {
		return newError("Validate", "delegation.allowed_requirement_ids", ErrInvalidDelegation,
			"delegation %q names no requirement it covers", d.DelegationID)
	}
	if len(d.AllowedRoles) == 0 {
		return newError("Validate", "delegation.allowed_roles", ErrInvalidDelegation,
			"delegation %q carries no authority", d.DelegationID)
	}
	if err := d.Scope.Validate(); err != nil {
		return err
	}
	if !d.NotBefore.IsSet() || !d.Expiry.IsSet() {
		return newError("Validate", "delegation.expiry", ErrInvalidDelegation,
			"delegation %q has no bounded validity window", d.DelegationID)
	}
	if !d.Expiry.After(d.NotBefore) {
		return newError("Validate", "delegation.expiry", ErrInvalidDelegation,
			"delegation %q expires at or before it starts", d.DelegationID)
	}
	return nil
}

// ActiveAt reports whether the delegation is within its validity window at at.
func (d Delegation) ActiveAt(at values.Instant) bool {
	return !at.Before(d.NotBefore) && at.Before(d.Expiry)
}

// Covers reports whether the delegation authorizes a decision on requirementID
// with the given authority floor and term scope. All three are subset checks
// against the delegation's own upper bounds, and the scope must match exactly:
// a delegation for a wider frame would be an expansion of the delegator's grant
// rather than a narrowing of it.
func (d Delegation) Covers(requirementID string, floor []string, scope Scope) bool {
	found := false
	for _, id := range d.AllowedRequirementIDs {
		if id == requirementID {
			found = true
			break
		}
	}
	if !found {
		return false
	}
	allowed := make(map[string]bool, len(d.AllowedRoles))
	for _, r := range d.AllowedRoles {
		allowed[r] = true
	}
	for _, r := range floor {
		if !allowed[r] {
			return false
		}
	}
	return d.Scope.Equal(scope)
}

// Directory is the org/authority port resolution reads. Every method answers
// from a supplied projection at an explicit effective time; nothing here
// consults a clock, a network or a database of its own.
type Directory interface {
	// Holders returns the principal ids satisfying one atomic term at at. An
	// unknown role, relationship or group is an empty result, never an error:
	// "nobody holds this" is a business fact. An error means the port itself
	// could not answer.
	//
	// A NAMED term never reaches this method: the expression already names the
	// principal, and resolution checks their facts instead of searching for
	// them.
	Holders(t Term, at values.Instant) ([]string, error)

	// Facts returns the principal's facts at at. The boolean reports whether
	// the principal is known at all.
	Facts(principalID string, at values.Instant) (PrincipalFacts, bool, error)

	// ManagerChain returns the ordered manager chain above principalID,
	// nearest manager first.
	ManagerChain(principalID string, at values.Instant) ([]string, error)

	// DelegationsFrom returns the delegations in which principalID is the
	// delegator, including expired ones: resolution reports an expired
	// delegation as an exclusion with a rule id rather than silently not
	// seeing it.
	DelegationsFrom(principalID string, at values.Instant) ([]Delegation, error)

	// Version identifies the projection the answers came from, so a resolution
	// can be replayed against the same directory state.
	Version() string
}

// MemoryDirectory is an in-memory Directory for fixtures, conformance
// scenarios and any lane that needs an org/authority port without a database.
// The zero value is not usable; build one with [NewMemoryDirectory].
type MemoryDirectory struct {
	version     string
	holders     map[string][]string
	facts       map[string]PrincipalFacts
	managers    map[string]string
	delegations map[string][]Delegation
}

// NewMemoryDirectory returns an empty in-memory directory identified by
// version.
func NewMemoryDirectory(version string) *MemoryDirectory {
	return &MemoryDirectory{
		version:     version,
		holders:     map[string][]string{},
		facts:       map[string]PrincipalFacts{},
		managers:    map[string]string{},
		delegations: map[string][]Delegation{},
	}
}

// WithHolders records the principals satisfying a term. It replaces any
// previous answer for that exact term.
func (d *MemoryDirectory) WithHolders(t Term, principalIDs ...string) *MemoryDirectory {
	ids := append([]string(nil), principalIDs...)
	sort.Strings(ids)
	d.holders[t.Ref()] = ids
	return d
}

// WithPrincipal records one principal's facts.
func (d *MemoryDirectory) WithPrincipal(f PrincipalFacts) *MemoryDirectory {
	roles := append([]string(nil), f.Roles...)
	sort.Strings(roles)
	f.Roles = roles
	d.facts[f.PrincipalID] = f
	return d
}

// WithManager records that manager manages principalID.
func (d *MemoryDirectory) WithManager(principalID, manager string) *MemoryDirectory {
	d.managers[principalID] = manager
	return d
}

// WithDelegation records one delegation, keyed by its delegator.
func (d *MemoryDirectory) WithDelegation(del Delegation) *MemoryDirectory {
	d.delegations[del.FromPrincipalID] = append(d.delegations[del.FromPrincipalID], del)
	return d
}

// SetAvailable flips one principal's availability, for scenarios where the
// primary approver is out and the fallback path must be exercised.
func (d *MemoryDirectory) SetAvailable(principalID string, available bool) *MemoryDirectory {
	f, ok := d.facts[principalID]
	if !ok {
		return d
	}
	f.Available = available
	d.facts[principalID] = f
	return d
}

// SetActive flips one principal's active flag.
func (d *MemoryDirectory) SetActive(principalID string, active bool) *MemoryDirectory {
	f, ok := d.facts[principalID]
	if !ok {
		return d
	}
	f.Active = active
	d.facts[principalID] = f
	return d
}

// Holders implements [Directory].
func (d *MemoryDirectory) Holders(t Term, _ values.Instant) ([]string, error) {
	return append([]string(nil), d.holders[t.Ref()]...), nil
}

// Facts implements [Directory].
func (d *MemoryDirectory) Facts(principalID string, _ values.Instant) (PrincipalFacts, bool, error) {
	f, ok := d.facts[principalID]
	if !ok {
		return PrincipalFacts{}, false, nil
	}
	f.Roles = append([]string(nil), f.Roles...)
	return f, true, nil
}

// ManagerChain implements [Directory]. It stops at the first repeat so a
// mis-seeded cycle produces a bounded answer rather than a hang.
func (d *MemoryDirectory) ManagerChain(principalID string, _ values.Instant) ([]string, error) {
	seen := map[string]bool{principalID: true}
	var chain []string
	for cur := principalID; ; {
		next, ok := d.managers[cur]
		if !ok || next == "" || seen[next] {
			return chain, nil
		}
		chain = append(chain, next)
		seen[next] = true
		cur = next
	}
}

// DelegationsFrom implements [Directory].
func (d *MemoryDirectory) DelegationsFrom(principalID string, _ values.Instant) ([]Delegation, error) {
	out := append([]Delegation(nil), d.delegations[principalID]...)
	sort.Slice(out, func(i, j int) bool { return out[i].DelegationID < out[j].DelegationID })
	return out, nil
}

// Version implements [Directory].
func (d *MemoryDirectory) Version() string { return d.version }
