package streaming

import (
	"errors"
	"time"
)

// OperationState is where a long-running operation stands, polled rather
// than streamed. It mirrors the shape PROTO-007's sibling todo EP-OPS-001
// names for the operation resource
// (PENDING|RUNNING|SUCCEEDED|FAILED|CANCELLATION_REQUESTED|CANCELLED); this
// package owns only the polling contract's shape, not that endpoint's own
// authority to accept, run or cancel work.
type OperationState string

// Declared operation states.
const (
	OperationPending               OperationState = "PENDING"
	OperationRunning               OperationState = "RUNNING"
	OperationSucceeded             OperationState = "SUCCEEDED"
	OperationFailed                OperationState = "FAILED"
	OperationCancellationRequested OperationState = "CANCELLATION_REQUESTED"
	OperationCancelled             OperationState = "CANCELLED"
)

// Terminal reports whether s is one a poller stops polling from: nothing
// about the operation will change again once it is in a terminal state.
func (s OperationState) Terminal() bool {
	switch s {
	case OperationSucceeded, OperationFailed, OperationCancelled:
		return true
	default:
		return false
	}
}

// valid reports whether s is one of the declared states.
func (s OperationState) valid() bool {
	switch s {
	case OperationPending, OperationRunning, OperationSucceeded, OperationFailed,
		OperationCancellationRequested, OperationCancelled:
		return true
	default:
		return false
	}
}

// Operation is one poll answer for a long-running operation: its identity,
// where it stands, an opaque cursor a caller presents on its next poll (or
// to resume a companion stream the operation feeds, when it has one), and
// how long the caller must wait before polling again.
type Operation struct {
	// ID names the operation. It is stable for the operation's whole
	// lifetime, which is what makes polling it "the same operation" rather
	// than a new request each time.
	ID string
	// State is where the operation stands right now.
	State OperationState
	// Cursor is the opaque position this poll answer leaves the caller at,
	// normally minted the same way a stream's [Producer] mints one.
	Cursor string
	// RetryAfter is how long the caller should wait before polling again.
	// It is zero exactly when State is [OperationState.Terminal], because a
	// terminal answer is not something polling again ever changes.
	RetryAfter time.Duration
}

// Sentinel validation errors [Operation.Validate] returns.
var (
	ErrOperationNoID            = errors.New("streaming: operation has no id")
	ErrOperationUnknownState    = errors.New("streaming: operation state is not one of the declared states")
	ErrOperationRetryOnTerminal = errors.New("streaming: a terminal operation must not declare a retry-after")
	ErrOperationNoRetryAfter    = errors.New("streaming: a non-terminal operation must declare a positive retry-after")
)

// Validate reports whether o is a well-formed poll answer: it names an
// operation, its state is one of the declared ones, and RetryAfter agrees
// with whether that state is terminal.
func (o Operation) Validate() error {
	if o.ID == "" {
		return ErrOperationNoID
	}
	if !o.State.valid() {
		return ErrOperationUnknownState
	}
	if o.State.Terminal() {
		if o.RetryAfter != 0 {
			return ErrOperationRetryOnTerminal
		}
		return nil
	}
	if o.RetryAfter <= 0 {
		return ErrOperationNoRetryAfter
	}
	return nil
}

// RetryAfterBound declares the floor and ceiling a poller must respect
// between polls of one operation: never faster than Min (which protects the
// operation's own store from being polled into the ground) and never told
// to wait longer than Max (which bounds how stale a caller's view of a
// cancellation request can become).
type RetryAfterBound struct {
	Min time.Duration
	Max time.Duration
}

// Default bounds a [RetryAfterBound] falls back to when either field is left
// at its zero value.
const (
	DefaultMinRetryAfter = 1 * time.Second
	DefaultMaxRetryAfter = 1 * time.Minute
)

// Clamp returns d adjusted to fall within b, applying [DefaultMinRetryAfter]
// and [DefaultMaxRetryAfter] for whichever bound b leaves unset.
func (b RetryAfterBound) Clamp(d time.Duration) time.Duration {
	min, max := b.Min, b.Max
	if min <= 0 {
		min = DefaultMinRetryAfter
	}
	if max <= 0 {
		max = DefaultMaxRetryAfter
	}
	if d < min {
		return min
	}
	if d > max {
		return max
	}
	return d
}
