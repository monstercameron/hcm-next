// Package appointment owns the semantic contract for appointments. It is a
// conformance boundary: validation and publication do not write authority,
// emit events, enqueue work, or call providers.
package appointment

import (
	"errors"
	"fmt"
)

var (
	ErrInvalidRequirement  = errors.New("appointment: invalid requirement")
	ErrPublicationRejected = errors.New("appointment: publication rejected")
)

// Rejection identifies the exact contract member that prevented publication.
// State is the lifecycle state observed by the validator and Version is the
// immutable definition version under which it was evaluated.
type Rejection struct {
	Code    string
	Field   string
	State   string
	Version string
	Reason  string
}

func (e *Rejection) Error() string {
	return fmt.Sprintf("%s field=%s state=%s version=%s: %s", e.Code, e.Field, e.State, e.Version, e.Reason)
}

func (e *Rejection) Unwrap() error { return ErrInvalidRequirement }

func reject(field, state, version, reason string) error {
	return &Rejection{Code: "APPT_001_REJECTED", Field: field, State: state, Version: version, Reason: reason}
}
