// Package career owns career preference, target-role and development-objective
// meaning. Revisions are immutable values; consumers must append corrections
// rather than changing an existing revision.
package career

import "errors"

var (
	ErrInvalidRevision  = errors.New("career: invalid revision")
	ErrInvalidReference = errors.New("career: invalid reference")
)
