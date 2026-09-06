package search

import (
	"errors"
	"fmt"
)

// Sentinel refusals. All are matchable with errors.Is.
var (
	// ErrScopeDenied is returned by [Query] when the caller's [Authorization]
	// does not grant the search scope. Query runs no statement in this case:
	// a caller with no search scope learns nothing, not even that the query
	// text matched zero or many subjects.
	ErrScopeDenied = errors.New("search: DENIED - caller authorization does not grant search scope")
	// ErrNoDiscloser is returned by [Query] when no [Discloser] is supplied.
	// A query with no way to test per-subject disclosure would have to
	// return every match unfiltered, which is precisely the existence leak
	// RETRIEVAL-001's RED clause forbids; failing closed here is cheaper than
	// trusting every future caller to remember to pass one.
	ErrNoDiscloser = errors.New("search: query has no discloser to filter results through")
	// ErrEmptyQueryText is returned for a blank query string.
	ErrEmptyQueryText = errors.New("search: query text is empty")
	// ErrInvalidProjectionInput is returned when [Project]'s input is not
	// complete enough to project: a missing tenant, an invalid subject
	// reference, or an unspecified source revision. search_projection_event
	// is append-only evidence, so an incomplete input is refused before
	// anything is written, the same discipline
	// internal/data/workforce.ErrInvalidRow enforces for journey_worker.
	ErrInvalidProjectionInput = errors.New("search: projection input is incomplete")
)

// InvalidKindError is returned by [EntityKind.Validate] for a kind outside
// the closed, declared set. It names the offending kind so a caller does not
// have to re-derive it from a generic message.
type InvalidKindError struct {
	Kind EntityKind
}

func (e *InvalidKindError) Error() string {
	return fmt.Sprintf("search: %q is not a declared subject kind", string(e.Kind))
}
