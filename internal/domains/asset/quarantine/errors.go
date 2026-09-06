package quarantine

import "fmt"

// Stable codes, matched by callers that map a typed error to a transport
// response without reading the message text -- the same convention
// internal/data/artifacts.errors.go uses.
const (
	CodeRequestInvalid  = "QUARANTINE_REQUEST_INVALID"
	CodeContentTooLarge = "QUARANTINE_CONTENT_TOO_LARGE"
	CodeNotFound        = "QUARANTINE_NOT_FOUND"
	CodeUseRefused      = "QUARANTINE_USE_REFUSED"
)

// ErrRequestInvalid reports a missing or malformed field on a request or
// policy.
type ErrRequestInvalid struct {
	Field  string
	Reason string
}

// Code returns the stable validation identifier.
func (ErrRequestInvalid) Code() string { return CodeRequestInvalid }

func (e ErrRequestInvalid) Error() string {
	return fmt.Sprintf("%s: %s %s", CodeRequestInvalid, e.Field, e.Reason)
}

// ErrContentTooLarge reports that an upload exceeded this package's own hard
// storage backstop ([StorageBackstopBytes]) -- not the declared policy cap,
// which is refused as an ordinary [Rejected] verdict instead. This one is
// returned directly by [Upload] with nothing recorded to [Store] at all,
// because content this large cannot be persisted as quarantine evidence in
// the first place (migrations/00030_artifact_quarantine.sql's own
// artifact_quarantine_byte_size_bounded backstop would refuse the insert).
type ErrContentTooLarge struct {
	Limit int64
	Size  int64
}

// Code returns the stable validation identifier.
func (ErrContentTooLarge) Code() string { return CodeContentTooLarge }

func (e ErrContentTooLarge) Error() string {
	return fmt.Sprintf("%s: %d byte upload exceeds the %d byte hard storage backstop", CodeContentTooLarge, e.Size, e.Limit)
}

// ErrNotFound reports that [Store] has no state at all for a given tenant
// and content id -- including a syntactically well-formed but never-
// uploaded digest, i.e. a guessed reference.
type ErrNotFound struct {
	ContentID string
}

// Code returns the stable validation identifier.
func (ErrNotFound) Code() string { return CodeNotFound }

func (e ErrNotFound) Error() string {
	return fmt.Sprintf("%s: no quarantine record for content id %s", CodeNotFound, e.ContentID)
}

// ErrUseRefused reports that [Use] refused a content id because its current
// state is anything other than [Admitted]. This is the caller-facing half of
// DOC-MAL-001's central guarantee: nothing may read an artifact for use
// while it is quarantined or after it has been rejected.
type ErrUseRefused struct {
	ContentID string
	State     State
	Reason    string
}

// Code returns the stable conflict identifier.
func (ErrUseRefused) Code() string { return CodeUseRefused }

func (e ErrUseRefused) Error() string {
	if e.Reason != "" {
		return fmt.Sprintf("%s: %s is %s, not admitted for use: %s", CodeUseRefused, e.ContentID, e.State, e.Reason)
	}
	return fmt.Sprintf("%s: %s is %s, not admitted for use", CodeUseRefused, e.ContentID, e.State)
}
