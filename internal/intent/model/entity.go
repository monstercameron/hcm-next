package model

// EntityClass classifies one EntityDefinition, matching
// planning/data/models/registry-and-coverage-contracts.md's
// "entity class = AGGREGATE_ROOT | CHILD | VALUE | EVENT | EVIDENCE |
// READ_MODEL".
type EntityClass string

// Entity classes.
const (
	ClassAggregateRoot EntityClass = "AGGREGATE_ROOT"
	ClassChild         EntityClass = "CHILD"
	ClassValue         EntityClass = "VALUE"
	ClassEvent         EntityClass = "EVENT"
	ClassEvidence      EntityClass = "EVIDENCE"
	ClassReadModel     EntityClass = "READ_MODEL"
)

// Valid reports whether c is one of the six declared entity classes.
func (c EntityClass) Valid() bool {
	switch c {
	case ClassAggregateRoot, ClassChild, ClassValue, ClassEvent, ClassEvidence, ClassReadModel:
		return true
	default:
		return false
	}
}

// DefinitionStatus is the publication status shared by EntityDefinition and
// PropertyDefinition. It is independent of the whole-schema
// [ReleaseState] lifecycle: a definition's own status tracks whether this
// one entity or property is still being drafted, is live, or has been
// withdrawn.
type DefinitionStatus string

// Definition statuses.
const (
	StatusDraft      DefinitionStatus = "DRAFT"
	StatusActive     DefinitionStatus = "ACTIVE"
	StatusDeprecated DefinitionStatus = "DEPRECATED"
	StatusRetired    DefinitionStatus = "RETIRED"
)

// Valid reports whether s is one of the four declared statuses.
func (s DefinitionStatus) Valid() bool {
	switch s {
	case StatusDraft, StatusActive, StatusDeprecated, StatusRetired:
		return true
	default:
		return false
	}
}

// EntityDefinition publishes one entity's identity, ownership, classification
// and lifecycle assignment (MODEL-011, MODEL-012).
//
// NoBusinessLifecycle is the literal LifecycleAssignment value a READ_MODEL
// entity carries: an affirmative policy, not an absence of one. See
// planning/data/models/registry-and-coverage-contracts.md's "Aggregate
// lifecycle assignment registry".
const NoBusinessLifecycle = "NO_BUSINESS_LIFECYCLE"

// EntityDefinition is the canonical entity registry entry.
type EntityDefinition struct {
	Ref EntityRef

	// Key is the snake_case identifier this entity uses as the leading
	// segment of every [PropertyRef] it owns, for example "employment" for
	// Employment.
	Key string

	Aliases []string

	// OwnerDomain names the owning business domain, e.g. "PEOPLE", "REWARDS",
	// "WORK", "OPERATIONS", "GOVERNANCE".
	OwnerDomain string

	Class EntityClass

	// LifecycleAssignment names the lifecycle profile this root or child
	// follows, e.g. "RevisionedFactLifecycle", "EmploymentLifecycle", or the
	// affirmative [NoBusinessLifecycle] for a rebuildable read model with no
	// commands.
	LifecycleAssignment string

	TenantScoped bool

	Status DefinitionStatus
}

// Validate rejects an entity definition that cannot be published.
func (e EntityDefinition) Validate() error {
	if err := e.Ref.Validate(); err != nil {
		return err
	}
	if err := validEntityKey(e.Key); err != nil {
		return err
	}
	if e.OwnerDomain == "" {
		return newError("EntityDefinition.Validate", "owner_domain", ErrInvalidEntity,
			"%s names no owner domain", e.Ref)
	}
	if !e.Class.Valid() {
		return newError("EntityDefinition.Validate", "class", ErrInvalidEntity,
			"%s has class %q, outside the six declared classes", e.Ref, e.Class)
	}
	if e.LifecycleAssignment == "" {
		return newError("EntityDefinition.Validate", "lifecycle_assignment", ErrUnassignedRoot,
			"%s declares no lifecycle assignment", e.Ref)
	}
	if e.LifecycleAssignment == NoBusinessLifecycle && e.Class != ClassReadModel {
		return newError("EntityDefinition.Validate", "lifecycle_assignment", ErrInvalidEntity,
			"%s carries NO_BUSINESS_LIFECYCLE but is class %s, not READ_MODEL", e.Ref, e.Class)
	}
	if !e.Status.Valid() {
		return newError("EntityDefinition.Validate", "status", ErrInvalidEntity,
			"%s has status %q, outside the four declared statuses", e.Ref, e.Status)
	}
	return nil
}
