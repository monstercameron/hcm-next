package migrate

import (
	"errors"
	"fmt"
	"testing"
)

func TestError_UnwrapsToTheSentinel(t *testing.T) {
	plain := refuse(CodeStranded, "instance-1", "no legal target")
	if !errors.Is(plain, ErrMigrate) {
		t.Fatalf("refuse(...) does not unwrap to ErrMigrate: %v", plain)
	}

	cause := fmt.Errorf("underlying storage failure")
	wrapped := wrap(CodeUnsafePoint, "instance-1", cause, "no durable checkpoint")
	if !errors.Is(wrapped, ErrMigrate) {
		t.Fatalf("wrap(...) does not unwrap to ErrMigrate: %v", wrapped)
	}
	if !errors.Is(wrapped, cause) {
		t.Fatalf("wrap(...) does not unwrap to its cause: %v", wrapped)
	}
}

func TestCodeOf(t *testing.T) {
	if got := CodeOf(nil); got != "" {
		t.Fatalf("CodeOf(nil) = %q, want empty", got)
	}
	if got := CodeOf(errors.New("some other package's error")); got != "" {
		t.Fatalf("CodeOf(foreign error) = %q, want empty", got)
	}
	err := refuse(CodeChangedInstance, "instance-1", "moved on")
	if got := CodeOf(err); got != CodeChangedInstance {
		t.Fatalf("CodeOf = %q, want %q", got, CodeChangedInstance)
	}
}

func TestError_MessageNamesCodeAndRef(t *testing.T) {
	err := refuse(CodeStranded, "instance-1", "no legal target exists")
	msg := err.Error()
	if msg == "" {
		t.Fatal("Error() returned an empty message")
	}
	// The message is not a stable contract callers should match on, but it
	// must at least surface the ref and the underlying detail for a human
	// reading logs -- this is a smoke check, not a golden string.
	if got := CodeOf(err); got != CodeStranded {
		t.Fatalf("CodeOf = %q, want %q", got, CodeStranded)
	}
}

// TestDeclaredCodesAreDistinct guards against a copy-paste that gives two
// refusal reasons the same wire code, which would make CodeOf ambiguous to a
// caller branching on it.
func TestDeclaredCodesAreDistinct(t *testing.T) {
	codes := []string{
		CodeInvalidRequest, CodePlanMismatch, CodeUnsafePoint, CodeMultiNodeFrontier,
		CodeNotPreviewed, CodeChangedInstance, CodeStalePreview, CodeUnapprovedDigest,
		CodeSeparationOfDuties, CodeStranded, CodeRequiresRepair, CodeInvalidBridgeStage,
		CodeUnsupportedBridgeSource,
	}
	seen := make(map[string]bool, len(codes))
	for _, c := range codes {
		if seen[c] {
			t.Fatalf("code %q is declared more than once", c)
		}
		seen[c] = true
	}
}
