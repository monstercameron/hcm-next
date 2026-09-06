package snapshot

import (
	"errors"
	"fmt"
	"strings"
)

// ErrRequestIncomplete is returned when an InputRequest is malformed.
// Matchable with errors.Is.
var ErrRequestIncomplete = errors.New("snapshot: input request is malformed")

// InputRequest names one business input Resolve must include in a
// ReadSnapshot, and optionally pins the authority class the caller requires
// that input's resolved entry to carry.
type InputRequest struct {
	// Name is the semantic input identifier Resolve must answer, matching
	// InputEntry.Name.
	Name string
	// RequiredAuthority, when set to a declared AuthorityClass, is the only
	// class the resolved entry for Name may carry: Resolve refuses with
	// ErrAuthorityMismatch rather than accept a different one. Left at
	// AuthorityUnspecified, any declared class is accepted -- a caller that
	// must prevent an external observation from being presented as native
	// state pins this explicitly rather than inferring it from shape.
	RequiredAuthority AuthorityClass
}

// Validate reports whether the request is well formed.
func (r InputRequest) Validate() error {
	if strings.TrimSpace(r.Name) == "" {
		return fmt.Errorf("%w: request has no input name", ErrRequestIncomplete)
	}
	if r.RequiredAuthority != AuthorityUnspecified && !r.RequiredAuthority.Valid() {
		return fmt.Errorf("%w: request %q names an unknown authority class %q",
			ErrRequestIncomplete, r.Name, string(r.RequiredAuthority))
	}
	return nil
}
