package lineage

import (
	"fmt"
	"time"

	"github.com/google/uuid"

	datalogger "github.com/monstercameron/human-capital-management-suite/internal/data/ledger"
)

// EventRef names one recorded assertion by its exact stream and sequence.
// It is an alias for internal/data/ledger.EventRef, the same type
// AppendRequest.Corrects already uses, so a lineage walk starts from and
// returns the identical value a caller already has in hand from an
// AppendReceipt or a prior lineage query.
type EventRef = datalogger.EventRef

// Node is one event as seen by a lineage walk: its identity, its assertion
// class, when it was recorded, and - when it is itself a correction or
// supersession - the exact event it replaces.
type Node struct {
	Ref            EventRef
	EventID        uuid.UUID
	AssertionClass datalogger.AssertionClass
	RecordedAt     time.Time
	// Corrects is the event this node replaces, or nil when this node is an
	// origin: an assertion no later assertion has (yet) corrected or
	// superseded anything to reach.
	Corrects *EventRef
}

// maxLineageDepth bounds every walk in this package. A well-formed ledger
// can never need it - see the package doc's "why lineage cannot be forged by
// mutation" - but a walk that finds a (stream, sequence) it has already
// visited, or one that would otherwise never terminate against a
// corrupted or adversarially constructed graph, fails closed with
// [ErrLineageCycle] rather than looping forever.
const maxLineageDepth = 10_000

// ErrCorrectionTargetNotFound reports that a correction or supersession
// names a target that does not exist for this tenant - including a target
// that exists, but only for a different tenant. Because every lookup in
// this package is scoped by an explicit tenant argument, a cross-tenant
// reference simply never resolves; it is indistinguishable from - and
// refused exactly like - a target that was never recorded at all.
type ErrCorrectionTargetNotFound struct {
	Tenant    uuid.UUID
	StreamKey string
	Sequence  int64
}

func (ErrCorrectionTargetNotFound) Code() string { return "LEDGER_LINEAGE_TARGET_NOT_FOUND" }

func (e ErrCorrectionTargetNotFound) Error() string {
	return fmt.Sprintf("%s: tenant %s has no event at %s@%d", e.Code(), e.Tenant, e.StreamKey, e.Sequence)
}

// ErrLineageCycle reports that a lineage walk revisited a (stream, sequence)
// it had already seen, or exceeded [maxLineageDepth]. A correctly formed
// ledger can never produce this - a correction can only ever name a target
// that was already durably recorded, so no later event can be an ancestor of
// an earlier one - so this error proves the walker fails closed rather than
// looping forever against a graph a direct, out-of-band write corrupted.
type ErrLineageCycle struct {
	Path []EventRef
}

func (ErrLineageCycle) Code() string { return "LEDGER_LINEAGE_CYCLE" }

func (e ErrLineageCycle) Error() string {
	last := e.Path[len(e.Path)-1]
	return fmt.Sprintf("%s: lineage walk revisited %s@%d after %d steps", e.Code(), last.StreamKey, last.Sequence, len(e.Path))
}

// ErrNotACorrection reports that [ValidateCorrectionTarget] or [Append] was
// asked to validate a request that does not carry a correction target at
// all.
type ErrNotACorrection struct {
	AssertionClass datalogger.AssertionClass
}

func (ErrNotACorrection) Code() string { return "LEDGER_LINEAGE_NOT_A_CORRECTION" }

func (e ErrNotACorrection) Error() string {
	return fmt.Sprintf("%s: assertion class %s carries no correction target", e.Code(), e.AssertionClass)
}
