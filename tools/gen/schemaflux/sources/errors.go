package sources

import "fmt"

// ReferenceKind classifies which symbolic reference an
// UnresolvedReferenceError names.
type ReferenceKind string

// The reference kinds this package's [Compile] rejects when unresolved.
const (
	ReferenceKindEntity     ReferenceKind = "entity"
	ReferenceKindAuthority  ReferenceKind = "authority"
	ReferenceKindRetention  ReferenceKind = "retention"
	ReferenceKindFamily     ReferenceKind = "family"
	ReferenceKindVocabulary ReferenceKind = "vocabulary"
	ReferenceKindChild      ReferenceKind = "child"
	ReferenceKindKey        ReferenceKind = "key"
)

// UnresolvedReferenceError is this package's typed compile error: it always
// names the exact source file, the field within that source entry, and the
// value that failed to resolve, mirroring
// tools/gen/schemaflux.UnresolvedReferenceError's diagnostic shape for the
// business-intent source pipeline. A definition either compiles with zero
// unresolved references, or [Compile] reports this error and the entry does
// not enter the [Manifest].
type UnresolvedReferenceError struct {
	Kind       ReferenceKind
	SourceFile string
	SourceLine int
	EntityName string
	Field      string
	Value      string
	Reason     string
}

func (e *UnresolvedReferenceError) Error() string {
	return fmt.Sprintf("%s:%d: %s: unresolved %s reference %q in field %q: %s",
		e.SourceFile, e.SourceLine, e.EntityName, e.Kind, e.Value, e.Field, e.Reason)
}

// MismatchError describes one field where a covered:true source entry
// disagrees with (or has no counterpart in) internal/intent/model.Catalog().
// [CrossCheckModel] returns a slice of these; it never mutates the Go
// registry it compares against.
type MismatchError struct {
	SourceFile string
	EntityName string
	Field      string
	Reason     string
}

func (e *MismatchError) Error() string {
	return fmt.Sprintf("%s: %s: %s: %s", e.SourceFile, e.EntityName, e.Field, e.Reason)
}
