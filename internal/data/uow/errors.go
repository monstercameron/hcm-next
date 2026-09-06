package uow

import (
	"errors"
	"fmt"

	"github.com/google/uuid"
)

// ErrUnitOfWork is the sentinel every refusal from this package unwraps to.
// Classify with errors.Is; read [ConflictError] (via errors.As) for detail.
var ErrUnitOfWork = errors.New("uow: refused")

// ErrClosed is returned by [Register], [Stage], [UnitOfWork.Commit] or
// [UnitOfWork.Rollback] once the unit of work has already committed or rolled
// back. A unit of work finishes exactly once; there is no way to reopen or
// nest a second one inside it.
var ErrClosed = fmt.Errorf("%w: unit of work already finished", ErrUnitOfWork)

// ErrStaleVersion is what an [AggregateRepository.Save] implementation
// returns when the expected version no longer matches the aggregate's
// current one. Actual is the version the repository found instead, used to
// build the fuller [ConflictError] the unit of work reports to its own
// caller. A conforming Save must return this exact type (not merely
// something matching [ErrUnitOfWork]) so [Stage]'s wrapping can read Actual.
type ErrStaleVersion struct {
	// Actual is the aggregate's current version, as the repository observed
	// it at the moment the compare-and-swap statement ran. It may already be
	// stale again by the time the caller reads it -- optimistic concurrency
	// never promises otherwise -- but it is exact as of that moment, not an
	// estimate.
	Actual Version
}

func (e *ErrStaleVersion) Error() string {
	return fmt.Sprintf("uow: stale version: actual %d", uint64(e.Actual))
}

// ConflictError is the typed refusal [UnitOfWork.Commit] returns when a
// staged aggregate's expected version no longer matches what is stored: it
// names the aggregate kind and entity that lost the race, plus the version
// the caller expected and the version the repository actually found.
type ConflictError struct {
	Kind            string
	EntityID        uuid.UUID
	ExpectedVersion Version
	ActualVersion   Version
}

func (e *ConflictError) Error() string {
	return fmt.Sprintf("uow: %s %s: optimistic version conflict: expected version %d, actual version %d",
		e.Kind, e.EntityID, uint64(e.ExpectedVersion), uint64(e.ActualVersion))
}

// Unwrap exposes the package sentinel so errors.Is(err, ErrUnitOfWork) sees
// through a ConflictError without every caller needing errors.As.
func (e *ConflictError) Unwrap() error { return ErrUnitOfWork }

// IsConflict reports whether err is (or wraps) a [*ConflictError], and
// returns it for callers that want the expected/actual versions without
// their own errors.As boilerplate.
func IsConflict(err error) (*ConflictError, bool) {
	var c *ConflictError
	if errors.As(err, &c) {
		return c, true
	}
	return nil, false
}
