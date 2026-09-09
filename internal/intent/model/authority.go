package model

import "github.com/monstercameron/human-capital-management-suite/internal/kernel/values"

// AuthorityKind classifies who asserts a fact. Values match the
// authority_assignment_kind_allowed CHECK constraint in
// migrations/00005_ledger.sql exactly, so a compiled assignment and the
// physical row it becomes never disagree on vocabulary.
type AuthorityKind string

// Authority kinds.
const (
	AuthorityInternal       AuthorityKind = "INTERNAL"
	AuthorityExternalSystem AuthorityKind = "EXTERNAL_SYSTEM"
	AuthorityHuman          AuthorityKind = "HUMAN"
	AuthorityImport         AuthorityKind = "IMPORT"
	AuthorityAgent          AuthorityKind = "AGENT"
)

// Valid reports whether k is one of the five declared authority kinds.
func (k AuthorityKind) Valid() bool {
	switch k {
	case AuthorityInternal, AuthorityExternalSystem, AuthorityHuman, AuthorityImport, AuthorityAgent:
		return true
	default:
		return false
	}
}

// MergePolicy declares how two authorities' values for one scope are
// reconciled when they disagree.
type MergePolicy string

// Merge policies.
const (
	MergeLastWriteWins        MergePolicy = "LAST_WRITE_WINS"
	MergeAuthorityPrecedence  MergePolicy = "AUTHORITY_PRECEDENCE"
	MergeManualReconciliation MergePolicy = "MANUAL_RECONCILIATION"
)

// Valid reports whether m is one of the three declared merge policies.
func (m MergePolicy) Valid() bool {
	switch m {
	case MergeLastWriteWins, MergeAuthorityPrecedence, MergeManualReconciliation:
		return true
	default:
		return false
	}
}

// SourceAuthorityAssignment declares which system or actor is authoritative
// for a scope over an effective interval (MODEL-021).
type SourceAuthorityAssignment struct {
	AssignmentRef string
	Kind          AuthorityKind

	// DomainScope names the entity or property scope this assignment governs,
	// e.g. "Employment" or "employment.status".
	DomainScope string

	Effective values.EffectiveInterval

	// Exclusive marks a scope only one authority may govern at a time.
	Exclusive bool

	// FreshnessSeconds is the maximum staleness this authority's observations
	// may carry before [ResolveAuthority] reports them stale.
	FreshnessSeconds uint32

	Merge MergePolicy

	EvidenceRef string
}

// Validate rejects an authority assignment that cannot be published.
func (a SourceAuthorityAssignment) Validate() error {
	if a.AssignmentRef == "" {
		return newError("SourceAuthorityAssignment.Validate", "assignment_ref", ErrInvalidAuthorityAssignment,
			"assignment carries no reference")
	}
	if !a.Kind.Valid() {
		return newError("SourceAuthorityAssignment.Validate", "kind", ErrInvalidAuthorityAssignment,
			"%s has kind %q, outside the five declared kinds", a.AssignmentRef, a.Kind)
	}
	if a.DomainScope == "" {
		return newError("SourceAuthorityAssignment.Validate", "domain_scope", ErrInvalidAuthorityAssignment,
			"%s names no domain scope", a.AssignmentRef)
	}
	if err := a.Effective.Validate(); err != nil {
		return newError("SourceAuthorityAssignment.Validate", "effective", ErrInvalidAuthorityAssignment,
			"%s has no valid effective interval: %v", a.AssignmentRef, err)
	}
	if !a.Merge.Valid() {
		return newError("SourceAuthorityAssignment.Validate", "merge", ErrInvalidAuthorityAssignment,
			"%s has merge policy %q, outside the three declared policies", a.AssignmentRef, a.Merge)
	}
	if a.EvidenceRef == "" {
		return newError("SourceAuthorityAssignment.Validate", "evidence_ref", ErrInvalidAuthorityAssignment,
			"%s names no evidence", a.AssignmentRef)
	}
	return nil
}

// AuthorityDecision is the result of resolving which authority governs a
// scope at a point in time.
type AuthorityDecision struct {
	AssignmentRef    string
	Kind             AuthorityKind
	Scope            string
	Effective        values.EffectiveInterval
	FreshnessSeconds uint32
	Merge            MergePolicy
	EvidenceRef      string
}

// ResolveAuthority picks the authority governing scope at asOf from a set of
// compiled assignments.
//
// It blocks: a scope with no assignment effective at asOf
// ([ErrNoAuthority]); two exclusive assignments for the same scope with
// overlapping effective intervals, which is ambiguous ownership rather than a
// resolvable answer ([ErrInvalidAuthorityAssignment]); and, when
// lastObservedAt is set, a source whose last observation is older than its
// declared freshness relative to asOf ([ErrAuthorityOutOfScope]).
func ResolveAuthority(assignments []SourceAuthorityAssignment, scope string, asOf values.Instant, lastObservedAt *values.Instant) (AuthorityDecision, error) {
	var candidates []SourceAuthorityAssignment
	for _, a := range assignments {
		if a.DomainScope != scope {
			continue
		}
		ok, err := a.Effective.ContainsInstant(asOf)
		if err != nil || !ok {
			continue
		}
		candidates = append(candidates, a)
	}
	if len(candidates) == 0 {
		return AuthorityDecision{}, newError("ResolveAuthority", "domain_scope", ErrNoAuthority,
			"no authority assignment governs %q at %s", scope, asOf)
	}
	exclusiveCount := 0
	for _, c := range candidates {
		if c.Exclusive {
			exclusiveCount++
		}
	}
	if exclusiveCount > 1 {
		return AuthorityDecision{}, newError("ResolveAuthority", "domain_scope", ErrInvalidAuthorityAssignment,
			"%d overlapping exclusive authorities govern %q at %s", exclusiveCount, scope, asOf)
	}
	winner := candidates[0]
	if lastObservedAt != nil && winner.FreshnessSeconds > 0 {
		age := asOf.Time().Sub(lastObservedAt.Time())
		if age.Seconds() > float64(winner.FreshnessSeconds) {
			return AuthorityDecision{}, newError("ResolveAuthority", "freshness", ErrAuthorityOutOfScope,
				"%s is stale for %q: last observed %s, max freshness %ds",
				winner.AssignmentRef, scope, lastObservedAt, winner.FreshnessSeconds)
		}
	}
	return AuthorityDecision{
		AssignmentRef:    winner.AssignmentRef,
		Kind:             winner.Kind,
		Scope:            winner.DomainScope,
		Effective:        winner.Effective,
		FreshnessSeconds: winner.FreshnessSeconds,
		Merge:            winner.Merge,
		EvidenceRef:      winner.EvidenceRef,
	}, nil
}

// CheckWriterScope rejects a writer acting outside its authority assignment's
// effective interval.
func CheckWriterScope(a SourceAuthorityAssignment, at values.Instant) error {
	ok, err := a.Effective.ContainsInstant(at)
	if err != nil {
		return newError("CheckWriterScope", "effective", ErrAuthorityOutOfScope,
			"%s effective interval cannot evaluate %s: %v", a.AssignmentRef, at, err)
	}
	if !ok {
		return newError("CheckWriterScope", "effective", ErrAuthorityOutOfScope,
			"%s is not effective at %s", a.AssignmentRef, at)
	}
	return nil
}
