package execute

import (
	"errors"
	"fmt"
)

var (
	// ErrInvalidConfiguration reports a driver missing a required port or a
	// request missing execution identity.
	ErrInvalidConfiguration = errors.New("workflow execute: invalid configuration")
	// ErrUnsupportedContinuation reports a timer or signal continuation. The
	// prototype has no durable store for either and must never fake one.
	ErrUnsupportedContinuation = errors.New("workflow execute: unsupported continuation")
	// ErrNoProgress reports a non-terminal run with neither READY work nor a
	// durable WorkItem on which it can honestly park.
	ErrNoProgress = errors.New("workflow execute: no progress")
)

func invalid(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalidConfiguration, fmt.Sprintf(format, args...))
}

func unsupported(kind, nodeID string) error {
	return fmt.Errorf("%w: %s for node %s has no durable prototype store", ErrUnsupportedContinuation, kind, nodeID)
}
