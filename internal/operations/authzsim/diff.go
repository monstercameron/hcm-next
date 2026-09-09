package authzsim

import (
	"fmt"
	"sort"

	"github.com/monstercameron/human-capital-management-suite/internal/operations/internal/viewdigest"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/authz"
)

// FieldDelta is one field's ruling before and after a proposed change.
// Changed is redundant with Before != After but is kept explicit so a
// caller never has to compare authz.Effect values itself.
type FieldDelta struct {
	Field   authz.FieldID
	Before  authz.Effect
	After   authz.Effect
	Changed bool
}

// Diff is the field-by-field comparison [DiffDecisions] produces. When
// either input's subject is not disclosable, FieldDeltas is nil and
// ScopeRelationshipBefore/After are empty: see [DiffDecisions] for why.
type Diff struct {
	SubjectDisclosableBefore  bool
	SubjectDisclosableAfter   bool
	SubjectDisclosableChanged bool

	// ScopeRelationshipBefore/After name the matched relationship
	// (authz.RelationshipKind.String()) on each side, empty whenever that
	// side's subject was not disclosable.
	ScopeRelationshipBefore string
	ScopeRelationshipAfter  string

	FieldDeltas []FieldDelta

	Explanation string
	Digest      string
}

// DiffDecisions compares before and after - typically the same request
// simulated once under the current role/relationship set and once under a
// proposed change - and reports exactly what changed.
//
// When either decision's subject is not disclosable, DiffDecisions collapses
// the whole comparison to the one coarse fact (disclosable: before -> after)
// and reports no field name, rule ID or relationship, matching
// authz.Decision.Explain's own rule that a non-disclosable subject's
// explanation never leaks which fields, domains or relationships were
// involved: a diff that named fields on one side of a withheld comparison
// would let a caller infer what a denied decision would otherwise have
// granted, which is exactly what Explain already refuses to do.
func DiffDecisions(before, after authz.Decision) Diff {
	d := Diff{
		SubjectDisclosableBefore:  before.SubjectDisclosable,
		SubjectDisclosableAfter:   after.SubjectDisclosable,
		SubjectDisclosableChanged: before.SubjectDisclosable != after.SubjectDisclosable,
	}

	if !before.SubjectDisclosable || !after.SubjectDisclosable {
		d.Explanation = fmt.Sprintf("subject_disclosable: %v -> %v", before.SubjectDisclosable, after.SubjectDisclosable)
		d.Digest = digestDiff(d)
		return d
	}

	d.ScopeRelationshipBefore = before.Scope.Relationship.String()
	d.ScopeRelationshipAfter = after.Scope.Relationship.String()

	fields := make(map[authz.FieldID]bool, len(before.Fields)+len(after.Fields))
	for f := range before.Fields {
		fields[f] = true
	}
	for f := range after.Fields {
		fields[f] = true
	}
	names := make([]authz.FieldID, 0, len(fields))
	for f := range fields {
		names = append(names, f)
	}
	sort.Slice(names, func(i, j int) bool { return names[i] < names[j] })

	changed := 0
	for _, f := range names {
		b := before.Fields[f].Effect
		a := after.Fields[f].Effect
		delta := FieldDelta{Field: f, Before: b, After: a, Changed: a != b}
		if delta.Changed {
			changed++
		}
		d.FieldDeltas = append(d.FieldDeltas, delta)
	}

	d.Explanation = fmt.Sprintf(
		"scope %s -> %s; %d/%d fields changed",
		d.ScopeRelationshipBefore, d.ScopeRelationshipAfter, changed, len(names),
	)
	d.Digest = digestDiff(d)
	return d
}

func digestDiff(d Diff) string {
	b := viewdigest.New().
		Bool("disclosable.before", d.SubjectDisclosableBefore).
		Bool("disclosable.after", d.SubjectDisclosableAfter).
		Bool("disclosable.changed", d.SubjectDisclosableChanged).
		String("scope.before", d.ScopeRelationshipBefore).
		String("scope.after", d.ScopeRelationshipAfter).
		String("explanation", d.Explanation).
		Int("field_deltas", int64(len(d.FieldDeltas)))
	for _, fd := range d.FieldDeltas {
		b.String("field", string(fd.Field)).
			String("before", fd.Before.String()).
			String("after", fd.After.String()).
			Bool("changed", fd.Changed)
	}
	return b.Digest()
}
