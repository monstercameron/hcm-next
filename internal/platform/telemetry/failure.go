package telemetry

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"runtime"
	"sync"
)

// SafeError is the only error interface whose code and retryability may be
// used by telemetry. Arbitrary Error() text is never copied to a signal.
type SafeError interface {
	Code() string
	Retryable() bool
}

type FailureType string

const (
	FailureUnknown  FailureType = "unknown"
	FailureCanceled FailureType = "canceled"
	FailureTimeout  FailureType = "timeout"
	FailurePanic    FailureType = "panic"
)

var ErrPanicRecovered = errors.New("telemetry: panic recovered")

// ClassifiedFailure is the safe projection of an error or panic. StackRef is
// a protected opaque reference, not a stack string and not a payload field.
type ClassifiedFailure struct {
	Code      string
	Type      FailureType
	Retryable bool
	Panic     bool
	StackRef  string
}

// ClassifyError emits bounded, non-sensitive failure metadata. It deliberately
// avoids errors.As/errors.Is over arbitrary wrapping graphs: a hostile cyclic
// provider error must not hang a telemetry helper.
func ClassifyError(err error) ClassifiedFailure {
	if err == nil {
		return ClassifiedFailure{}
	}
	failure := ClassifiedFailure{Code: "operation_failed", Type: FailureUnknown, StackRef: stackReference()}
	if err == context.Canceled {
		failure.Code, failure.Type = "CANCELED", FailureCanceled
	} else if err == context.DeadlineExceeded {
		failure.Code, failure.Type, failure.Retryable = "DEADLINE_EXCEEDED", FailureTimeout, true
	}
	if safe, ok := err.(SafeError); ok {
		if code := safe.Code(); code != "" && len(code) <= 64 {
			failure.Code = code
		}
		failure.Retryable = safe.Retryable()
	}
	return failure
}

// ClassifyPanic classifies only the dynamic type of the panic value. Neither
// the panic value nor its formatted representation is retained.
func ClassifyPanic(value any) ClassifiedFailure {
	return ClassifiedFailure{Code: "PANIC_RECOVERED", Type: FailurePanic, Panic: true, StackRef: stackReferenceForType(value)}
}

// Recover executes fn and returns one safe failure if it panics. A normal
// return has no failure. This helper never re-panics and never calls a fatal
// logging primitive.
func Recover(fn func()) (failure ClassifiedFailure) {
	defer func() {
		if value := recover(); value != nil {
			failure = ClassifyPanic(value)
		}
	}()
	fn()
	return ClassifiedFailure{}
}

// ExecuteWithRecovery preserves an owned returned error and converts a panic
// into ErrPanicRecovered. The callback is invoked once and the recovery path
// can be guarded by callers that may receive duplicate worker delivery.
func ExecuteWithRecovery(fn func() error) (err error, failure ClassifiedFailure) {
	defer func() {
		if value := recover(); value != nil {
			failure = ClassifyPanic(value)
			err = ErrPanicRecovered
		}
	}()
	err = fn()
	if err != nil {
		failure = ClassifyError(err)
	}
	return err, failure
}

// RecoveryGuard makes the recovery disposition idempotent for a boundary
// that may accidentally invoke its completion hook twice.
type RecoveryGuard struct {
	once sync.Once
	err  error
	info ClassifiedFailure
}

// Run calls fn at most once and returns the first disposition on every call.
func (g *RecoveryGuard) Run(fn func() error) (error, ClassifiedFailure) {
	if g == nil {
		return ExecuteWithRecovery(fn)
	}
	g.once.Do(func() { g.err, g.info = ExecuteWithRecovery(fn) })
	return g.err, g.info
}

func stackReference() string {
	return stackReferenceForType(nil)
}

func stackReferenceForType(value any) string {
	var pcs [16]uintptr
	n := runtime.Callers(3, pcs[:])
	h := sha256.New()
	for _, pc := range pcs[:n] {
		var b [8]byte
		for i := range b {
			b[i] = byte(pc >> (8 * i))
		}
		_, _ = h.Write(b[:])
	}
	if value != nil {
		_, _ = h.Write([]byte(fmt.Sprintf("%T", value)))
	}
	return "stackref:" + hex.EncodeToString(h.Sum(nil))[:24]
}
