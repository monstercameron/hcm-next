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
	failure, err := ExecuteWithRecovery(func() error { return owned })
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
	f.Add("password=TOP-SECRET-payload-value")
	f.Fuzz(func(t *testing.T, value string) {
		failure := ClassifyPanic(value)
		// StackRef is "stackref:" plus exactly 24 hex digest characters
		// over the call PCs and the value's TYPE (never the value). A
		// value that could appear in a well-formed StackRef by chance —
		// inside the literal prefix, inside the hex digest, or straddling
		// their boundary — proves nothing either way and is skipped. Any
		// other value present in StackRef is a genuine leak.
		if failure.Panic && value != "" && !isCoincidencePlausible(value) && strings.Contains(failure.StackRef, value) {
			t.Fatalf("panic input leaked: %q", value)
		}
	})
}

const stackRefPrefix = "stackref:"

// isCoincidencePlausible reports whether value could occur inside a
// well-formed StackRef ("stackref:" + 24 hex chars) without a leak: fully
// inside the literal prefix, fully inside the hex digest, or straddling
// their boundary. Genuine secrets (long, non-hex) never qualify.
func isCoincidencePlausible(value string) bool {
	if value == "" || strings.Contains(stackRefPrefix, value) {
		return true
	}
	for i := 1; i < len(value); i++ {
		if strings.HasSuffix(stackRefPrefix, value[:i]) && isHexBounded(value[i:], 24) {
			return true
		}
	}
	return isHexBounded(value, 24)
}

// isHexBounded reports whether value is all hex characters within maxLen.
func isHexBounded(value string, maxLen int) bool {
	if value == "" || len(value) > maxLen {
		return false
	}
	for _, r := range value {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')) {
			return false
		}
	}
	return true
}

func TestTodo_OBS_017_CoincidenceGuard(t *testing.T) {
	plausible := []string{"", "0", "s", "ref", "stackref:", "ab12", "ref:ab12", "stackref:abcdef0123456789abcdef"}
	for _, v := range plausible {
		if !isCoincidencePlausible(v) {
			t.Errorf("isCoincidencePlausible(%q) = false, want true", v)
		}
	}
	distinctive := []string{"secret", "password=TOP-SECRET", "Bearer abc", "a-very-long-hex-lookalike-string-0123456789abcdef", strings.Repeat("a", 25)}
	for _, v := range distinctive {
		if isCoincidencePlausible(v) {
			t.Errorf("isCoincidencePlausible(%q) = true, want false", v)
		}
	}
}

func TestTodo_OBS_017_Fault(t *testing.T) {
	called := 0
	failure, err := ExecuteWithRecovery(func() error { called++; panic("failure") })
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
	_, first := guard.Run(fn)
	_, second := guard.Run(fn)
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
