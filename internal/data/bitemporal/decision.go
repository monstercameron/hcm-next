package bitemporal

import (
	"sort"
	"time"

	"github.com/google/uuid"
)

// Decision is the authorization outcome an upstream caller has already
// evaluated for one query. This package treats it as a plain input value: it
// evaluates no policy and resolves no principal. internal/trust/authz is
// being built by a different lane and is deliberately not imported here;
// whatever eventually produces a Decision (TRUST-012's query planner, or a
// hand-built fixture in this package's own tests today) is responsible for
// having already intersected tenant, organization, population, field, time
// and purpose scope.
//
// Every boundary a Decision states is compiled into the SQL WHERE clause
// before the query runs. A denied field or a withheld subject is therefore
// absent from PostgreSQL's own result set, not filtered out of an
// already-fetched Go slice.
//
// # Why "field" means schema_ref
//
// ledger_event carries a typed payload (or a governed artifact reference)
// under a registered schema_ref; it has no column per payload attribute, so
// this package cannot gate on an attribute like "compensation.base" without
// decoding protobuf bytes inside the database. schema_ref - the registered
// payload type an assertion was recorded under - is the finest unit of
// "field" this SQL adapter can enforce. A caller granting or denying a field
// is expected to name the schema_ref(s) that carry it (for example,
// "hcmnext.people.v1.CompensationBase@1"). A future field-level authority
// that reaches inside a payload belongs to a decoding layer above this one;
// this adapter's contract is that it never selects rows outside the granted
// schema_ref set in the first place.
type Decision struct {
	// Tenant is the only tenant this decision authorizes. A Request naming
	// any other tenant is refused by ErrTenantMismatch before SQL is built.
	Tenant uuid.UUID

	// AllowSubjects, when non-nil, is the exact set of ledger stream keys the
	// caller may see. A nil slice means "not restricted by subject": every
	// subject not explicitly denied is visible. A non-nil, empty slice
	// authorizes no subject at all.
	AllowSubjects []string
	// DenySubjects withholds specific subjects even when AllowSubjects would
	// otherwise admit them, and even when AllowSubjects is nil. A deny is
	// never masked by a broader allow.
	DenySubjects []string

	// AllowFields / DenyFields apply the same rule to schema_ref. See the
	// type doc for why schema_ref is what "field" means at this layer.
	AllowFields []string
	DenyFields  []string

	// MaxKnownAt bounds the knowledge horizon this decision was evaluated
	// under. A Request's KnownAt is clamped to it, never extended: a caller
	// cannot see anything recorded after the moment their own authorization
	// was established merely by asking for a later KnownAt. The zero value
	// means "no ceiling beyond whatever the request itself asks for".
	MaxKnownAt time.Time
}

// Validate reports whether the decision can be evaluated at all.
func (d Decision) Validate() error {
	if d.Tenant == uuid.Nil {
		return ErrDecisionInvalid{Reason: "tenant is required"}
	}
	return nil
}

// clampKnownAt applies MaxKnownAt to a requested known-at instant, returning
// the earlier of the two. A zero MaxKnownAt is "no ceiling".
func (d Decision) clampKnownAt(requested time.Time) time.Time {
	if d.MaxKnownAt.IsZero() {
		return requested
	}
	if requested.IsZero() || d.MaxKnownAt.Before(requested) {
		return d.MaxKnownAt
	}
	return requested
}

// sortedUnique returns a sorted copy of values with duplicates removed, for
// deterministic SQL array parameters and deterministic evidence digests. A
// nil input stays nil so the "no restriction" and "restrict to nothing"
// states in Decision survive normalization.
func sortedUnique(values []string) []string {
	if values == nil {
		return nil
	}
	out := make([]string, len(values))
	copy(out, values)
	sort.Strings(out)
	deduped := out[:0]
	for i, v := range out {
		if i == 0 || v != out[i-1] {
			deduped = append(deduped, v)
		}
	}
	if len(deduped) == 0 {
		return []string{}
	}
	return deduped
}

// normalize returns a copy of the decision with every allow/deny list sorted
// and deduplicated. Query building and digesting both call it so that
// caller-supplied ordering can never change either the SQL predicate set or
// the evidence digest.
func (d Decision) normalize() Decision {
	d.AllowSubjects = sortedUnique(d.AllowSubjects)
	d.DenySubjects = sortedUnique(d.DenySubjects)
	d.AllowFields = sortedUnique(d.AllowFields)
	d.DenyFields = sortedUnique(d.DenyFields)
	return d
}
