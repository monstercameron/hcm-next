package search

import (
	"sort"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/people"
)

// EntityKind names what kind of subject one search_projection row indexes.
// It is a closed set -- migration 00037's own subject_kind CHECK constraint
// enforces the same list in the schema -- so a caller cannot silently start
// indexing a kind neither the Go code nor the schema has declared.
type EntityKind string

// KindWorker is the only declared subject kind in this phase, mirroring
// people.KindWorker: a lexical search hit names a worker, the same entity
// kind every other governed worker read already names.
const KindWorker EntityKind = EntityKind(people.KindWorker)

// entityKinds is the closed set [EntityKind.Validate] checks against.
var entityKinds = map[EntityKind]struct{}{
	KindWorker: {},
}

// Validate reports whether k is a declared subject kind.
func (k EntityKind) Validate() error {
	if _, ok := entityKinds[k]; !ok {
		return &InvalidKindError{Kind: k}
	}
	return nil
}

// String returns the kind token.
func (k EntityKind) String() string { return string(k) }

// clearedFields is the whole allowlist of people.FieldID values this package
// has declared classification-cleared for lexical search: directory-shaped
// facts a worker-directory search is expected to match on.
//
// It is deliberately narrow, and deliberately an allowlist rather than a
// denylist. people.AllFields() also defines FieldEmploymentStatus,
// FieldLifecycleStatus, FieldPayZone, FieldFTE, FieldManagerRelation,
// FieldHireDate, FieldEmploymentID and FieldAssignmentID; none of those is
// searchable text a directory box is for, and pay zone and FTE in particular
// are exactly the "restricted field" TestTodo_RETRIEVAL_001's RED clause
// ("indexed result leaks restricted field") exists to keep out of a tsvector
// no per-field authorization check ever runs against. A new people.FieldID
// added upstream is excluded by default: it has to be reviewed and added
// here explicitly before [Project] will ever let its value reach
// search_text, never indexed the day it is defined.
var clearedFields = map[people.FieldID]struct{}{
	people.FieldWorkerNumber:  {},
	people.FieldLegalName:     {},
	people.FieldPreferredName: {},
	people.FieldJobCode:       {},
	people.FieldOrgUnit:       {},
	people.FieldLocation:      {},
	people.FieldPositionID:    {},
}

// ClassificationCleared reports whether field is declared eligible to enter
// a lexical search projection.
func ClassificationCleared(field people.FieldID) bool {
	_, ok := clearedFields[field]
	return ok
}

// ClearedFields returns the whole allowlist in a fixed, sorted order. Project
// walks fields in exactly this order when it builds search_text, so the same
// input fields always produce the same text regardless of the order the
// caller's map happened to iterate in.
func ClearedFields() []people.FieldID {
	out := make([]people.FieldID, 0, len(clearedFields))
	for f := range clearedFields {
		out = append(out, f)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}
