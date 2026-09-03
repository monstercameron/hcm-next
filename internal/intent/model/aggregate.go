package model

// ConsistencyBoundary declares how a write that touches more than one
// aggregate root is committed.
type ConsistencyBoundary string

// Consistency boundaries.
const (
	// BoundaryLocalACID is the single-root, single-database-transaction
	// boundary. It never spans two roots.
	BoundaryLocalACID ConsistencyBoundary = "LOCAL_ACID"

	// BoundaryCrossAggregateTransaction is the declared BusinessTransaction +
	// TransactionParticipant boundary: the only way two roots may be written
	// together.
	BoundaryCrossAggregateTransaction ConsistencyBoundary = "CROSS_AGGREGATE_TRANSACTION"

	// BoundaryExternalObservation is not a transaction at all: an external
	// system is a durable effect with observation/reconciliation/repair,
	// never fake ACID.
	BoundaryExternalObservation ConsistencyBoundary = "EXTERNAL_OBSERVATION"
)

// Valid reports whether b is one of the three declared boundaries.
func (b ConsistencyBoundary) Valid() bool {
	switch b {
	case BoundaryLocalACID, BoundaryCrossAggregateTransaction, BoundaryExternalObservation:
		return true
	default:
		return false
	}
}

// AggregateDefinition registers one aggregate root's ownership, consistency
// boundary, lifecycle and invariants (MODEL-012).
type AggregateDefinition struct {
	Root EntityRef

	// ChildRefs are the entities this root owns. A child not listed under any
	// root's ChildRefs has no independent mutation authority and cannot be
	// commanded (planning/data/models/registry-and-coverage-contracts.md,
	// "Child entities not listed as roots inherit no independent mutation
	// authority").
	ChildRefs []EntityRef

	// LifecycleAssignment must match the root EntityDefinition's own
	// LifecycleAssignment; it is repeated here so an AggregateDefinition is
	// self-describing without a registry lookup.
	LifecycleAssignment string

	CommandBoundary ConsistencyBoundary

	// StreamKind names the physical ledger stream kind this root's authoritative
	// facts append to, if one exists in the migrated schema. Empty means no
	// physical stream is provisioned yet; DB-002 reports that as a disposition
	// mismatch rather than inventing one.
	StreamKind string

	InvariantRefs []string
}

// Validate rejects an aggregate definition that cannot be published: an
// unassigned root, an undeclared consistency boundary, or a
// CROSS_AGGREGATE_TRANSACTION boundary with no invariants governing it.
func (a AggregateDefinition) Validate() error {
	if err := a.Root.Validate(); err != nil {
		return err
	}
	for _, c := range a.ChildRefs {
		if err := c.Validate(); err != nil {
			return err
		}
		if c == a.Root {
			return newError("AggregateDefinition.Validate", "child_refs", ErrInvalidAggregate,
				"%s lists itself as its own child", a.Root)
		}
	}
	if a.LifecycleAssignment == "" {
		return newError("AggregateDefinition.Validate", "lifecycle_assignment", ErrUnassignedRoot,
			"%s declares no lifecycle assignment", a.Root)
	}
	if a.LifecycleAssignment == NoBusinessLifecycle {
		if a.CommandBoundary != "" {
			return newError("AggregateDefinition.Validate", "command_boundary", ErrInvalidAggregate,
				"%s is NO_BUSINESS_LIFECYCLE and may declare no commit boundary", a.Root)
		}
		return nil
	}
	if !a.CommandBoundary.Valid() {
		return newError("AggregateDefinition.Validate", "command_boundary", ErrInvalidAggregate,
			"%s has command boundary %q, outside the three declared boundaries", a.Root, a.CommandBoundary)
	}
	if a.CommandBoundary == BoundaryCrossAggregateTransaction && len(a.InvariantRefs) == 0 {
		return newError("AggregateDefinition.Validate", "invariant_refs", ErrCrossRootAtomicity,
			"%s declares CROSS_AGGREGATE_TRANSACTION with no invariants governing it", a.Root)
	}
	return nil
}

// Rebuildable reports whether the root is a read model with no commands.
func (a AggregateDefinition) Rebuildable() bool {
	return a.LifecycleAssignment == NoBusinessLifecycle
}

// AuthorizeCommand rejects a command against a root registered
// NO_BUSINESS_LIFECYCLE: such a root is a rebuildable summary only.
func (a AggregateDefinition) AuthorizeCommand() error {
	if a.Rebuildable() {
		return newError("AuthorizeCommand", "lifecycle_assignment", ErrNoBusinessLifecycle,
			"%s is NO_BUSINESS_LIFECYCLE; it accepts no commands", a.Root)
	}
	return nil
}

// ValidateCrossRootWrite rejects a write that touches more than one aggregate
// root without an explicit CROSS_AGGREGATE_TRANSACTION (or
// EXTERNAL_OBSERVATION) boundary declared by every participating root.
func ValidateCrossRootWrite(participants []AggregateDefinition) error {
	if len(participants) <= 1 {
		return nil
	}
	for _, p := range participants {
		if p.CommandBoundary != BoundaryCrossAggregateTransaction &&
			p.CommandBoundary != BoundaryExternalObservation {
			return newError("ValidateCrossRootWrite", "command_boundary", ErrCrossRootAtomicity,
				"%s declares %s; a write spanning %d roots requires a declared cross-aggregate boundary on every participant",
				p.Root, p.CommandBoundary, len(participants))
		}
	}
	return nil
}
