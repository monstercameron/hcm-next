package model

import (
	"errors"
	"fmt"
)

// Sentinel causes. Classify with [errors.Is]; never by matching strings.
var (
	// ErrInvalidReference reports a malformed entity, property or relationship
	// reference.
	ErrInvalidReference = errors.New("model: invalid reference")

	// ErrInvalidEntity reports an EntityDefinition that cannot be published: an
	// unknown class, an unassigned lifecycle, or a missing owner domain.
	ErrInvalidEntity = errors.New("model: invalid entity definition")

	// ErrInvalidProperty reports a PropertyDefinition missing a concrete type,
	// presence, authority, temporal, classification, correction or retention
	// semantics (MODEL-011 RED).
	ErrInvalidProperty = errors.New("model: invalid property definition")

	// ErrUnknownEntity reports a reference naming an entity the registry does
	// not publish.
	ErrUnknownEntity = errors.New("model: unknown entity")

	// ErrUnknownProperty reports a reference naming a property the registry
	// does not publish.
	ErrUnknownProperty = errors.New("model: unknown property")

	// ErrDuplicateEntity reports material reuse of an entity reference.
	ErrDuplicateEntity = errors.New("model: duplicate entity version")

	// ErrDuplicateProperty reports material reuse of a property reference.
	ErrDuplicateProperty = errors.New("model: duplicate property")

	// ErrInvalidAggregate reports an AggregateDefinition that cannot be
	// published: an unassigned root, a child claimed by two roots, or an
	// undeclared consistency boundary (MODEL-012 RED).
	ErrInvalidAggregate = errors.New("model: invalid aggregate definition")

	// ErrUnassignedRoot reports a root entity with no aggregate registration.
	ErrUnassignedRoot = errors.New("model: aggregate root has no registration")

	// ErrNoBusinessLifecycle reports an attempt to command a root whose
	// registration is NO_BUSINESS_LIFECYCLE: it may be read as a rebuildable
	// summary only.
	ErrNoBusinessLifecycle = errors.New("model: NO_BUSINESS_LIFECYCLE")

	// ErrCrossRootAtomicity reports a write that assumes atomicity across two
	// aggregate roots outside a declared consistency boundary.
	ErrCrossRootAtomicity = errors.New("model: cross-root write outside a declared consistency boundary")

	// ErrInvalidRelationship reports a RelationshipDefinition missing an
	// endpoint kind, or a cardinality/exclusivity/cycle rule that cannot be
	// evaluated (MODEL-013 RED).
	ErrInvalidRelationship = errors.New("model: invalid relationship definition")

	// ErrRelationshipCycle reports a relationship fact that would create a
	// cycle the definition prohibits.
	ErrRelationshipCycle = errors.New("model: relationship cycle prohibited")

	// ErrExclusiveOverlap reports two exclusive relationship facts whose
	// effective intervals overlap for the same source.
	ErrExclusiveOverlap = errors.New("model: overlapping exclusive relationship")

	// ErrCrossTenantEdge reports a relationship fact whose endpoints do not
	// share one tenant.
	ErrCrossTenantEdge = errors.New("model: cross-tenant relationship endpoint")

	// ErrInvalidSchemaRelease reports a SchemaRelease that cannot transition:
	// an unresolved consumer, an incompatible change, a missing migration, or
	// publication without approval (MODEL-017 RED).
	ErrInvalidSchemaRelease = errors.New("model: invalid schema release")

	// ErrUnknownReleaseTransition reports a release-state transition the
	// lifecycle profile does not declare.
	ErrUnknownReleaseTransition = errors.New("model: undeclared schema release transition")

	// ErrMissingLineage reports a material value or result without a complete
	// provenance edge: source, transformation, authority, recorded/effective
	// time and digest (MODEL-020 RED).
	ErrMissingLineage = errors.New("model: material value requires lineage")

	// ErrUnauthorizedLineageNode reports an attempt to disclose a provenance
	// node the caller is not authorized to see.
	ErrUnauthorizedLineageNode = errors.New("model: unauthorized lineage node")

	// ErrInvalidAuthorityAssignment reports a SourceAuthorityAssignment that
	// cannot be published: no owner, an overlapping exclusive owner, a stale
	// source, or a writer outside its effective scope (MODEL-021 RED).
	ErrInvalidAuthorityAssignment = errors.New("model: invalid source-authority assignment")

	// ErrNoAuthority reports a scope with no authority assignment.
	ErrNoAuthority = errors.New("model: no authority assignment for scope")

	// ErrAuthorityOutOfScope reports a writer outside its authority's
	// effective interval or domain scope.
	ErrAuthorityOutOfScope = errors.New("model: writer outside authority scope")

	// ErrInvalidClassification reports a derived value, artifact, log or
	// outbound payload with no classification label, or a label the registry
	// does not know how to propagate (MODEL-023 RED).
	ErrInvalidClassification = errors.New("model: invalid data classification")

	// ErrMissingClassification reports a material artifact created or
	// delivered with no classification label.
	ErrMissingClassification = errors.New("model: missing classification label")

	// ErrInvalidRetention reports a RecordsDeclaration missing a record
	// class, retention schedule, authority or disposition owner (MODEL-026
	// RED).
	ErrInvalidRetention = errors.New("model: invalid records declaration")

	// ErrNoJurisdictionOverride reports a retention lookup for a jurisdiction
	// the class declares no override or default for.
	ErrNoJurisdictionOverride = errors.New("model: no retention period for jurisdiction")

	// ErrCoverageIncomplete reports a MODEL-030 report that is not VERIFIED:
	// at least one entity, property, aggregate or binding check is absent.
	ErrCoverageIncomplete = errors.New("model: model coverage is incomplete")
)

// Error is the typed error this package returns. Op names the operation,
// Field names the offending field path when there is one, and Cause is the
// sentinel to classify against.
type Error struct {
	Op     string
	Field  string
	Cause  error
	Detail string
}

func (e *Error) Error() string {
	msg := e.Cause.Error()
	if e.Field != "" {
		msg += " [" + e.Field + "]"
	}
	if e.Detail != "" {
		msg += ": " + e.Detail
	}
	if e.Op != "" {
		msg = e.Op + ": " + msg
	}
	return msg
}

// Unwrap exposes the sentinel cause to [errors.Is].
func (e *Error) Unwrap() error { return e.Cause }

func newError(op, field string, cause error, format string, args ...any) *Error {
	return &Error{Op: op, Field: field, Cause: cause, Detail: fmt.Sprintf(format, args...)}
}
