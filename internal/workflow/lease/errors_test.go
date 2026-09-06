package lease

import (
	"errors"
	"testing"
)

// A refusal from this package has to be classifiable two ways at once: by
// sentinel (errors.Is, for a caller that only wants to know what kind of
// failure it is) and by code (CodeOf, for one that has to log or route it).
// These tests hold both open.

func TestErrors_EverySentinelAndCodeIsDeclared(t *testing.T) {
	for name, sentinel := range map[string]error{
		"ErrLease": ErrLease, "ErrInvalid": ErrInvalid, "ErrHeld": ErrHeld,
		"ErrLeaseLost": ErrLeaseLost, "ErrFenceStale": ErrFenceStale,
		"ErrLeaseLive": ErrLeaseLive, "ErrStorage": ErrStorage,
	} {
		if sentinel == nil || sentinel.Error() == "" {
			t.Fatalf("%s is nil or has an empty message", name)
		}
	}
	for name, code := range map[string]string{
		"CodeInvalid": CodeInvalid, "CodeLeaseHeld": CodeLeaseHeld, "CodeLeaseLost": CodeLeaseLost,
		"CodeFenceStale": CodeFenceStale, "CodeFenceForeign": CodeFenceForeign,
		"CodeLeaseLive": CodeLeaseLive, "CodeStorageFailed": CodeStorageFailed,
	} {
		if code == "" {
			t.Fatalf("%s is empty", name)
		}
	}
}

func TestErrors_RefusalCarriesCodeSentinelAndLocation(t *testing.T) {
	res := Resource{Kind: ResourceWorkflowInstance, ID: "instance:1"}
	err := refuse(CodeFenceStale, ErrFenceStale, res, "workload:w#replica:1", "token %d is behind %d", 3, 4)

	if !errors.Is(err, ErrFenceStale) {
		t.Fatalf("refusal does not classify as ErrFenceStale: %v", err)
	}
	if !errors.Is(err, ErrLease) {
		t.Fatalf("refusal does not classify as the package sentinel: %v", err)
	}
	if errors.Is(err, ErrLeaseLost) {
		t.Fatalf("a stale-fence refusal must not also classify as lease-lost")
	}
	if got := CodeOf(err); got != CodeFenceStale {
		t.Fatalf("CodeOf = %q, want %q", got, CodeFenceStale)
	}
	msg := err.Error()
	for _, want := range []string{CodeFenceStale, "instance:1", "workload:w#replica:1", "token 3 is behind 4"} {
		if !contains(msg, want) {
			t.Fatalf("message %q does not name %q", msg, want)
		}
	}
}

// A foreign fence is reported with its own code but classifies as stale,
// because the consequence for the caller is identical: refused, nothing
// mutated.
func TestErrors_ForeignFenceCodeStillClassifiesAsStale(t *testing.T) {
	err := refuse(CodeFenceForeign, ErrFenceStale, Resource{Kind: ResourceQueue, ID: "q"}, "h", "foreign")
	if !errors.Is(err, ErrFenceStale) {
		t.Fatalf("a foreign fence must classify as ErrFenceStale: %v", err)
	}
	if got := CodeOf(err); got != CodeFenceForeign {
		t.Fatalf("CodeOf = %q, want %q", got, CodeFenceForeign)
	}
}

func TestErrors_WrappedCauseSurvives(t *testing.T) {
	cause := errors.New("underlying driver failure")
	err := wrapStorage(Resource{Kind: ResourceQueue, ID: "q"}, "h", cause, "read the live lease")
	if !errors.Is(err, cause) {
		t.Fatalf("wrapped cause lost: %v", err)
	}
	if !errors.Is(err, ErrStorage) {
		t.Fatalf("wrapped storage failure does not classify as ErrStorage: %v", err)
	}
	if got := CodeOf(err); got != CodeStorageFailed {
		t.Fatalf("CodeOf = %q, want %q", got, CodeStorageFailed)
	}
}

func TestErrors_CodeOfIgnoresForeignErrors(t *testing.T) {
	if got := CodeOf(errors.New("not ours")); got != "" {
		t.Fatalf("CodeOf on a foreign error = %q, want empty", got)
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
