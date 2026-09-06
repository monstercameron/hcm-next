package telemetry

import (
	"errors"
	"strings"
	"testing"
)

type safeTestError struct{}

func (safeTestError) Error() string   { return "provider secret=TOP-SECRET" }
func (safeTestError) Code() string    { return "PROVIDER_TIMEOUT" }
func (safeTestError) Retryable() bool { return true }

func TestErrorAndPanicTelemetryClassifiesRedactsAndPreservesOwnedFailureBehavior(t *testing.T) {
	owned := safeTestError{}
	err, failure := ExecuteWithRecovery(func() error { return owned })
	if !errors.Is(err, owned) || failure.Code != "PROVIDER_TIMEOUT" || !failure.Retryable || failure.Type != FailureUnknown {
		t.Fatalf("returned failure = %v, %+v", err, failure)
	}
	if strings.Contains(failure.Code, "TOP-SECRET") || strings.Contains(failure.StackRef, "TOP-SECRET") {
		t.Fatal("provider error text leaked into classified telemetry")
	}
	panicFailure := Recover(func() { panic("password=TOP-SECRET") })
	if !panicFailure.Panic || panicFailure.Code != "PANIC_RECOVERED" || panicFailure.StackRef == "" {
		t.Fatalf("panic failure = %+v", panicFailure)
	}
	if strings.Contains(panicFailure.StackRef, "TOP-SECRET") {
		t.Fatal("panic payload leaked into stack reference")
	}
}

func TestTodo_OBS_017_Property(t *testing.T) {
	var calls int
	var guard RecoveryGuard
	fn := func() error { calls++; return nil }
	if _, _ = guard.Run(fn); calls != 1 {
		t.Fatalf("calls after first run = %d", calls)
	}
	if _, _ = guard.Run(fn); calls != 1 {
		t.Fatalf("recovery guard invoked callback twice: %d", calls)
	}
}

func TestTodo_OBS_017_Golden(t *testing.T) {
	failure := ClassifyError(errors.New("arbitrary provider text"))
	if failure.Code != "operation_failed" || failure.Type != FailureUnknown || len(failure.StackRef) != len("stackref:")+24 {
		t.Fatalf("classification = %+v", failure)
	}
}

func FuzzTodo_OBS_017(f *testing.F) {
	f.Add("secret")
	f.Add("")
	f.Fuzz(func(t *testing.T, value string) {
		failure := ClassifyPanic(value)
		if failure.Panic && strings.Contains(failure.StackRef, value) && value != "" {
			t.Fatalf("panic input leaked: %q", value)
		}
	})
}

func TestTodo_OBS_017_Fault(t *testing.T) {
	called := 0
	err, failure := ExecuteWithRecovery(func() error { called++; panic("failure") })
	if !errors.Is(err, ErrPanicRecovered) || !failure.Panic || called != 1 {
		t.Fatalf("panic disposition = %v, %+v, calls=%d", err, failure, called)
	}
}

func TestTodo_OBS_017_Security(t *testing.T) {
	secret := "authorization=Bearer-very-secret"
	failure := ClassifyPanic(secret)
	if strings.Contains(failure.Code, secret) || strings.Contains(failure.StackRef, secret) {
		t.Fatalf("secret leaked: %+v", failure)
	}
}

func TestTodo_OBS_017_Recovery(t *testing.T) {
	guard := new(RecoveryGuard)
	var calls int
	fn := func() error { calls++; return ErrPanicRecovered }
	first, _ := guard.Run(fn)
	second, _ := guard.Run(fn)
	if !errors.Is(first, ErrPanicRecovered) || !errors.Is(second, ErrPanicRecovered) || calls != 1 {
		t.Fatalf("guard recovery = %v/%v calls=%d", first, second, calls)
	}
}

func TestTodo_OBS_017_Mutation(t *testing.T) {
	failure := ClassifyPanic(errors.New("secret"))
	if failure.Type != FailurePanic || !failure.Panic {
		t.Fatal("panic classification mutant survived")
	}
}
