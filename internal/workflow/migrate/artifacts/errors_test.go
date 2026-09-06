package artifacts

import (
	"errors"
	"testing"
)

func TestError_CarriesCodeKindAndSentinel(t *testing.T) {
	t.Parallel()
	cause := errors.New("underlying")
	err := wrap(CodeStorageFailed, KindTimer, "timer-1", cause, "write the re-keyed timer for %s", "node-b")

	if !errors.Is(err, ErrArtifacts) {
		t.Fatal("a refusal does not unwrap to the package sentinel")
	}
	if !errors.Is(err, cause) {
		t.Fatal("a wrapped refusal does not unwrap to its cause")
	}
	if got := CodeOf(err); got != CodeStorageFailed {
		t.Fatalf("CodeOf = %q, want %q", got, CodeStorageFailed)
	}
	if got := KindOf(err); got != KindTimer {
		t.Fatalf("KindOf = %q, want %q", got, KindTimer)
	}
	msg := err.Error()
	for _, want := range []string{CodeStorageFailed, string(KindTimer), "timer-1", "node-b", "underlying"} {
		if !contains(msg, want) {
			t.Fatalf("message %q omits %q", msg, want)
		}
	}
}

func TestError_UnwrappedRefusalCarriesOnlyTheSentinel(t *testing.T) {
	t.Parallel()
	err := refuse(CodeStaleLease, KindLease, "instance-1", "token %d is behind", 3)
	if !errors.Is(err, ErrArtifacts) {
		t.Fatal("a refusal does not unwrap to the package sentinel")
	}
	if errors.Unwrap(err) != nil {
		t.Fatal("a refusal with no cause reported a single-error Unwrap")
	}
	if got := CodeOf(err); got != CodeStaleLease {
		t.Fatalf("CodeOf = %q, want %q", got, CodeStaleLease)
	}
}

func TestCodeOfAndKindOf_ReturnEmptyForForeignErrors(t *testing.T) {
	t.Parallel()
	foreign := errors.New("someone else's error")
	if got := CodeOf(foreign); got != "" {
		t.Fatalf("CodeOf(foreign) = %q, want empty", got)
	}
	if got := KindOf(foreign); got != "" {
		t.Fatalf("KindOf(foreign) = %q, want empty", got)
	}
	if got := CodeOf(nil); got != "" {
		t.Fatalf("CodeOf(nil) = %q, want empty", got)
	}
}

// A refusal raised before any handler ran carries no kind, and its message
// must still read cleanly rather than showing an empty bracket pair.
func TestError_MessageOmitsAbsentKindAndRef(t *testing.T) {
	t.Parallel()
	err := refuse(CodeInvalidRequest, "", "", "tenant id must not be the nil UUID")
	if got := err.Error(); got != "workflow/migrate/artifacts: INVALID_REQUEST: tenant id must not be the nil UUID" {
		t.Fatalf("message = %q", got)
	}
}

func contains(haystack, needle string) bool {
	return len(needle) == 0 || indexOf(haystack, needle) >= 0
}

func indexOf(haystack, needle string) int {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}
