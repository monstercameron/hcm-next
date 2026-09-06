package schemasnapshot_test

import (
	"errors"
	"testing"

	"github.com/monstercameron/hcm-next/internal/connectivity/schemasnapshot"
)

// TestErrorFormatting proves schemasnapshot.Error renders "op: cause: detail"
// when every field is set, degrades gracefully when Op or Detail is empty,
// and Unwrap exposes the sentinel cause to errors.Is -- the contract every
// other test in this package relies on when it asserts on a sentinel rather
// than a message string.
func TestErrorFormatting(t *testing.T) {
	full := &schemasnapshot.Error{Op: "pkg.Func", Cause: schemasnapshot.ErrIncomplete, Detail: "missing field x"}
	if got, want := full.Error(), "pkg.Func: schemasnapshot: structurally incomplete: missing field x"; got != want {
		t.Fatalf("Error() = %q, want %q", got, want)
	}
	if !errors.Is(full, schemasnapshot.ErrIncomplete) {
		t.Fatal("errors.Is did not see the wrapped sentinel cause")
	}
	if !errors.Is(full, full) {
		t.Fatal("errors.Is failed on identity")
	}

	noOp := &schemasnapshot.Error{Cause: schemasnapshot.ErrInvalid, Detail: "bad value"}
	if got, want := noOp.Error(), "schemasnapshot: invalid input: bad value"; got != want {
		t.Fatalf("Error() with no Op = %q, want %q", got, want)
	}

	noDetail := &schemasnapshot.Error{Op: "pkg.Func", Cause: schemasnapshot.ErrNotFound}
	if got, want := noDetail.Error(), "pkg.Func: schemasnapshot: snapshot not found"; got != want {
		t.Fatalf("Error() with no Detail = %q, want %q", got, want)
	}

	bare := &schemasnapshot.Error{Cause: schemasnapshot.ErrStore}
	if got, want := bare.Error(), "schemasnapshot: store failed"; got != want {
		t.Fatalf("Error() with only Cause = %q, want %q", got, want)
	}
}

// TestSentinelErrorsAreDistinct proves the six sentinels this package
// classifies by are pairwise distinct, so a caller matching one with
// errors.Is can never accidentally match another.
func TestSentinelErrorsAreDistinct(t *testing.T) {
	sentinels := []error{
		schemasnapshot.ErrIncomplete,
		schemasnapshot.ErrInvalid,
		schemasnapshot.ErrImmutable,
		schemasnapshot.ErrNotFound,
		schemasnapshot.ErrNotAdmitted,
		schemasnapshot.ErrStore,
	}
	for i, a := range sentinels {
		for j, b := range sentinels {
			if i == j {
				continue
			}
			if errors.Is(a, b) {
				t.Fatalf("sentinel %d (%v) unexpectedly matches sentinel %d (%v)", i, a, j, b)
			}
		}
	}
}
