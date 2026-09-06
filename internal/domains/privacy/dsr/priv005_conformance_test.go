package dsr

import (
	"testing"

	"github.com/monstercameron/hcm-next/internal/governance/legal"
	"github.com/monstercameron/hcm-next/internal/trust"
)

// TestTodo_PRIV_005_Conformance is the CONFORMANCE matrix test for
// PRIV-005: every declared [Kind] must have both a declared assurance floor
// ([AssuranceFloor]) and a declared statutory clock entry
// ([DefaultClockTable]'s fallback row), and erasure's floor must be at
// least as high as every other kind's -- "erasure needs the highest" is a
// checkable invariant here, not just a comment.
func TestTodo_PRIV_005_Conformance(t *testing.T) {
	kinds := AllKinds()
	if len(kinds) == 0 {
		t.Fatal("AllKinds() is empty")
	}

	clock := DefaultClockTable()
	erasureFloor := AssuranceFloor(KindErasure)
	if erasureFloor == trust.AssuranceUnspecified {
		t.Fatal("KindErasure has no declared assurance floor")
	}

	for _, kind := range kinds {
		t.Run(string(kind), func(t *testing.T) {
			floor := AssuranceFloor(kind)
			if floor == trust.AssuranceUnspecified {
				t.Errorf("kind %s has no declared assurance floor", kind)
			}
			if floor > erasureFloor {
				t.Errorf("kind %s declares floor %s, higher than erasure's own floor %s -- erasure must be the highest", kind, floor, erasureFloor)
			}

			// Every kind must resolve a deadline against the zero-value
			// (default) jurisdiction: the fallback entry every kind needs so
			// an as-yet-unmodeled jurisdiction still gets a clock.
			if _, err := clock.Deadline(legal.Jurisdiction{}, kind, mustInstant(t, fxReceivedAt)); err != nil {
				t.Errorf("kind %s has no declared default clock entry: %v", kind, err)
			}
		})
	}

	if AssuranceFloor(KindErasure) != erasureFloor {
		t.Fatal("AssuranceFloor is not stable across calls")
	}

	t.Run("an undeclared kind has no floor and no clock entry", func(t *testing.T) {
		bogus := Kind("SOMETHING_ELSE")
		if floor := AssuranceFloor(bogus); floor != trust.AssuranceUnspecified {
			t.Errorf("AssuranceFloor(undeclared kind) = %s, want %s", floor, trust.AssuranceUnspecified)
		}
		if _, err := clock.Deadline(legal.Jurisdiction{}, bogus, mustInstant(t, fxReceivedAt)); err == nil {
			t.Error("Deadline(undeclared kind) succeeded, want an error")
		}
	})
}
