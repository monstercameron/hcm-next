package canonical

import (
	"errors"
	"fmt"
)

// Sentinel causes. Every failure returned by this package wraps exactly one of
// them, so callers classify with [errors.Is] rather than by matching strings.
var (
	// ErrInvalidUTF8 reports a string field whose bytes are not valid UTF-8.
	// Invalid UTF-8 is rejected, never replaced with U+FFFD.
	ErrInvalidUTF8 = errors.New("canonical: invalid UTF-8")

	// ErrDuplicateSetMember reports two members of a declared set that share
	// canonical bytes.
	ErrDuplicateSetMember = errors.New("canonical: duplicate set member")

	// ErrUnknownField reports unknown Protobuf wire data retained on a message
	// reached through a material path of a profile that rejects unknown fields.
	ErrUnknownField = errors.New("canonical: unknown protobuf field")

	// ErrUnrepresentable reports a value the canonical model cannot express
	// without choosing an arbitrary representation: NaN, infinities, an instant
	// outside the normalized range, or a malformed fixed decimal.
	ErrUnrepresentable = errors.New("canonical: unrepresentable value")

	// ErrInvalidProfile reports a profile that does not describe its declared
	// canonical model: an unresolvable material path, a set declaration on a
	// non-repeated field, or a missing identity component.
	ErrInvalidProfile = errors.New("canonical: invalid profile")

	// ErrSchemaMismatch reports a message whose descriptor is not the canonical
	// model the profile declares.
	ErrSchemaMismatch = errors.New("canonical: schema mismatch")
)

// Error is the single error type this package returns. Op names the operation,
// Path names the material path under encoding (empty at the profile level), and
// Cause is one of the package sentinels.
type Error struct {
	Op     string
	Path   string
	Cause  error
	Detail string
}

func (e *Error) Error() string {
	msg := e.Cause.Error()
	if e.Path != "" {
		msg = fmt.Sprintf("%s at %q", msg, e.Path)
	}
	if e.Detail != "" {
		msg = msg + ": " + e.Detail
	}
	if e.Op != "" {
		msg = e.Op + ": " + msg
	}
	return msg
}

// Unwrap exposes the sentinel cause to [errors.Is].
func (e *Error) Unwrap() error { return e.Cause }

func newError(op, path string, cause error, format string, args ...any) *Error {
	return &Error{Op: op, Path: path, Cause: cause, Detail: fmt.Sprintf(format, args...)}
}
