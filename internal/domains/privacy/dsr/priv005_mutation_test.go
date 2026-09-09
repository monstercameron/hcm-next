package dsr

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/governance/legal"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// TestTodo_PRIV_005_Mutation is the MUTATION matrix test for PRIV-005. It
// proves the boundary conditions Verify and the clock table depend on are
// exact -- the class of bug a `<` vs `<=` boundary backwards, or a
// silently-dropped field, would introduce without necessarily being caught
// by the other matrix tests.
func TestTodo_PRIV_005_Mutation(t *testing.T) {
	t.Run("assurance exactly at the floor is accepted, one level below is refused", func(t *testing.T) {
		req := fixtureRequest(t) // KindAccess: floor is AssuranceSubstantial
		floor := AssuranceFloor(KindAccess)

		atFloor, err := req.Verify(fixtureEvidence(t, floor))
		if err != nil {
			t.Fatalf("Verify at exactly the floor refused: %v", err)
		}
		if atFloor.VerificationState != VerificationVerified {
			t.Errorf("at-floor VerificationState = %s, want %s", atFloor.VerificationState, VerificationVerified)
		}

		below, ok := oneLevelBelow(floor)
		if ok {
			refused, err := req.Verify(fixtureEvidence(t, below))
			if err == nil {
				t.Fatal("Verify one level below the floor succeeded, want refusal")
			}
			if refused.VerificationState != VerificationRefused {
				t.Errorf("below-floor VerificationState = %s, want %s", refused.VerificationState, VerificationRefused)
			}
		}
	})

	t.Run("expiry is checked inclusive of the boundary instant", func(t *testing.T) {
		req := fixtureRequest(t)

		// ExpiresAt one instant after VerifiedAt: still valid.
		validEv := fixtureEvidence(t, trust.AssuranceHigh)
		validEv.ExpiresAt = mustInstant(t, fxVerifiedAt+1)
		if _, err := req.Verify(validEv); err != nil {
			t.Errorf("Verify with ExpiresAt one instant after VerifiedAt refused: %v", err)
		}

		// ExpiresAt exactly equal to VerifiedAt: expired (half-open interval
		// convention used throughout this codebase -- the expiry instant
		// itself is excluded).
		expiredEv := fixtureEvidence(t, trust.AssuranceHigh)
		expiredEv.ExpiresAt = mustInstant(t, fxVerifiedAt)
		refused, err := req.Verify(expiredEv)
		if err == nil {
			t.Fatal("Verify with ExpiresAt exactly equal to VerifiedAt succeeded, want refusal")
		}
		if refused.VerificationState != VerificationRefused {
			t.Errorf("VerificationState = %s, want %s", refused.VerificationState, VerificationRefused)
		}
	})

	t.Run("DetectDuplicate window boundary is inclusive, one instant beyond is not a duplicate", func(t *testing.T) {
		first := fixtureRequest(t)

		withinSpec := fixtureIntakeSpec(t, "dup-within", KindAccess)
		withinSpec.ReceivedAt = mustInstant(t, fxReceivedAt+int64(fxWindow.Seconds()))
		within, err := Intake(withinSpec, DefaultClockTable(), []DataSubjectRequest{first}, fxWindow)
		if err != nil {
			t.Fatalf("Intake(within): %v", err)
		}
		if within.DuplicateOf != first.ID {
			t.Errorf("DuplicateOf = %q, want %q (exactly at the window boundary must still match)", within.DuplicateOf, first.ID)
		}

		beyondSpec := fixtureIntakeSpec(t, "dup-beyond", KindAccess)
		beyondSpec.ReceivedAt = mustInstant(t, fxReceivedAt+int64(fxWindow.Seconds())+1)
		beyond, err := Intake(beyondSpec, DefaultClockTable(), []DataSubjectRequest{first}, fxWindow)
		if err != nil {
			t.Fatalf("Intake(beyond): %v", err)
		}
		if beyond.DuplicateOf != "" {
			t.Errorf("DuplicateOf = %q, want empty (one instant beyond the window must not match)", beyond.DuplicateOf)
		}
	})

	t.Run("ClockTable.Deadline prefers the most specific jurisdiction entry over the default", func(t *testing.T) {
		table, err := NewClockTable(
			ClockEntry{Kind: KindAccess, Days: 30, Basis: DayBasisCalendar},
			ClockEntry{Jurisdiction: fxJurisdiction(), Kind: KindAccess, Days: 10, Basis: DayBasisCalendar},
		)
		if err != nil {
			t.Fatalf("NewClockTable: %v", err)
		}

		specific, err := table.Deadline(fxJurisdiction(), KindAccess, mustInstant(t, fxReceivedAt))
		if err != nil {
			t.Fatalf("Deadline (specific): %v", err)
		}
		if gotSec, _ := specific.Unix(); gotSec != fxReceivedAt+10*86400 {
			t.Errorf("specific-jurisdiction deadline = %d, want %d (the 10-day row, not the 30-day default)", gotSec, fxReceivedAt+10*86400)
		}

		fallback, err := table.Deadline(legal.Jurisdiction{Country: "US", State: "NY"}, KindAccess, mustInstant(t, fxReceivedAt))
		if err != nil {
			t.Fatalf("Deadline (fallback): %v", err)
		}
		if gotSec, _ := fallback.Unix(); gotSec != fxReceivedAt+30*86400 {
			t.Errorf("fallback deadline = %d, want %d (the 30-day default row)", gotSec, fxReceivedAt+30*86400)
		}
	})
}
