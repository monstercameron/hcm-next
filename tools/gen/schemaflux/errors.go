package schemaflux

import "fmt"

// ReferenceKind classifies which symbolic reference an
// UnresolvedReferenceError names. TOOL-005 requires exactly four cases:
// schema, capability, owner and family.
type ReferenceKind string

// The four reference kinds TOOL-005 requires the qualified generator to
// reject when unresolved.
const (
	ReferenceKindSchema     ReferenceKind = "schema"
	ReferenceKindCapability ReferenceKind = "capability"
	ReferenceKindOwner      ReferenceKind = "owner"
	ReferenceKindFamily     ReferenceKind = "family"
)

// UnresolvedReferenceError is the qualified generator's typed compile error
// for TOOL-005. It always names the exact source file, the field within that
// source definition, and the value that failed to resolve, so a diagnostic
// points at one line rather than reporting a generic "compile failed."
//
// Warnings cannot publish a P1A or P1B contract (TOOL-005 GREEN): there is no
// "resolved with warnings" state. A definition either compiles with zero
// unresolved references, or [Compile] reports this error and the definition
// does not enter the catalog.
type UnresolvedReferenceError struct {
	Kind         ReferenceKind
	SourceFile   string
	SourceLine   int
	IntentTypeID string
	Field        string
	Value        string
	Reason       string
}

func (e *UnresolvedReferenceError) Error() string {
	return fmt.Sprintf("%s:%d: %s: unresolved %s reference %q in field %q: %s",
		e.SourceFile, e.SourceLine, e.IntentTypeID, e.Kind, e.Value, e.Field, e.Reason)
}
