package search_test

import (
	"errors"
	"testing"

	"github.com/monstercameron/hcm-next/internal/data/search"
)

// TestInvalidKindErrorNamesTheOffendingKind proves the error message carries
// the actual rejected token, not a generic refusal, so a caller debugging a
// stale client can see exactly what was sent.
func TestInvalidKindErrorNamesTheOffendingKind(t *testing.T) {
	t.Parallel()
	err := search.EntityKind("candidate").Validate()
	if err == nil {
		t.Fatal("Validate() = nil, want an error")
	}
	var invalid *search.InvalidKindError
	if !errors.As(err, &invalid) {
		t.Fatalf("Validate() = %v, want *search.InvalidKindError", err)
	}
	if invalid.Kind != "candidate" {
		t.Errorf("InvalidKindError.Kind = %q, want %q", invalid.Kind, "candidate")
	}
	if got := err.Error(); got == "" {
		t.Error("Error() is empty")
	}
}

// TestSentinelErrorsAreDistinct proves every declared sentinel is its own
// distinct value, so a caller's errors.Is check can never accidentally match
// the wrong refusal.
func TestSentinelErrorsAreDistinct(t *testing.T) {
	t.Parallel()
	sentinels := []error{
		search.ErrScopeDenied,
		search.ErrNoDiscloser,
		search.ErrEmptyQueryText,
		search.ErrInvalidProjectionInput,
	}
	for i, a := range sentinels {
		for j, b := range sentinels {
			if i == j {
				continue
			}
			if errors.Is(a, b) {
				t.Errorf("sentinel %d (%v) matches sentinel %d (%v)", i, a, j, b)
			}
		}
	}
}
