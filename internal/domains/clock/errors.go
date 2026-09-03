package clock

import (
	"errors"
	"fmt"
)

var (
	ErrInvalidRegistration   = errors.New("clock: invalid registration")
	ErrDuplicateRegistration = errors.New("clock: duplicate registration")
)

// Rejection identifies the precise governance fact that prevented a device
// or source from being registered. Version is always the submitted contract
// version, including for malformed registrations.
type Rejection struct {
	Code    string
	Field   string
	State   string
	Version string
	Reason  string
}

func (r *Rejection) Error() string {
	return fmt.Sprintf("%s field=%s state=%s version=%s: %s", r.Code, r.Field, r.State, r.Version, r.Reason)
}

func (r *Rejection) Unwrap() error { return ErrInvalidRegistration }

func reject(field, state, version, reason string) error {
	return &Rejection{Code: "CLOCK_001_REJECTED", Field: field, State: state, Version: version, Reason: reason}
}
