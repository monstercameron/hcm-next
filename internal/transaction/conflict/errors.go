package conflict

import "errors"

// Sentinel causes. Classify with [errors.Is]; never by matching strings.
var (
	// ErrInvalidFootprint reports a write footprint missing a required
	// dimension: canonical resource, field path, effective interval,
	// operation, expected revision or authority scope.
	ErrInvalidFootprint = errors.New("conflict: invalid write footprint")

	// ErrInvalidCandidate reports a conflict candidate missing its proposal
	// revision id, conflict state or recorded time, or carrying an invalid
	// footprint.
	ErrInvalidCandidate = errors.New("conflict: invalid conflict candidate")

	// ErrNoOverlap reports an attempt to classify a pair of candidates whose
	// footprints do not actually overlap. Classification is defined only
	// for an established overlap.
	ErrNoOverlap = errors.New("conflict: footprints do not overlap")
)
