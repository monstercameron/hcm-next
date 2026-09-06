package dsr

import (
	"testing"

	"github.com/monstercameron/hcm-next/internal/trust"
)

// TestTodo_PRIV_005_Security is the SECURITY matrix test for PRIV-005: an
// unverified request must never be able to advance, and a request whose
// stored assurance is below its kind's declared floor must never be able
// to advance -- even when it was not reached through
// [DataSubjectRequest.Verify] at all, but assembled directly (a forged
// record, a bad migration, a bug in some future caller that sets fields by
// hand). [DataSubjectRequest.CanAdvance] is the security boundary this test
// exercises: it must re-derive its answer from the record's own fields
// every time, never trust a state label alone.
func TestTodo_PRIV_005_Security(t *testing.T) {
	t.Run("an UNVERIFIED request never advances, regardless of Kind", func(t *testing.T) {
		for _, kind := range AllKinds() {
			req, err := Intake(fixtureIntakeSpec(t, "sec-unverified-"+string(kind), kind), DefaultClockTable(), nil, fxWindow)
			if err != nil {
				t.Fatalf("Intake(%s): %v", kind, err)
			}
			if advance, code := req.CanAdvance(); advance || code != AdvanceUnverified {
				t.Errorf("kind %s: CanAdvance() = (%v, %s), want (false, %s)", kind, advance, code, AdvanceUnverified)
			}
		}
	})

	t.Run("a REFUSED request never advances", func(t *testing.T) {
		req := fixtureRequest(t)
		refused, err := req.Verify(IdentityEvidence{})
		if err == nil {
			t.Fatal("Verify(empty evidence) unexpectedly succeeded")
		}
		if advance, code := refused.CanAdvance(); advance || code != AdvanceUnverified {
			t.Errorf("CanAdvance() = (%v, %s), want (false, %s)", advance, code, AdvanceUnverified)
		}
	})

	t.Run("a directly-assembled record claiming VERIFIED with assurance below its kind's floor never advances", func(t *testing.T) {
		// This bypasses Verify entirely: it is the "forged or mis-migrated
		// record" scenario CanAdvance's doc comment names. Verify itself
		// would refuse to produce this record (proven by the PRIMARY test's
		// "assurance below the kind's floor" case); CanAdvance must refuse it
		// independently of how it came to exist.
		req := fixtureRequest(t) // KindAccess: floor is AssuranceSubstantial
		req.VerificationState = VerificationVerified
		req.IdentityEvidenceRef = "ev:forged"
		req.IdentityAssurance = trust.AssuranceLow // below AssuranceFloor(KindAccess)
		req.VerifiedAt = mustInstant(t, fxVerifiedAt)
		req = req.withEvidenceID() // keep the record internally self-consistent (Validate would otherwise catch the mismatch first)

		if err := req.Validate(); err != nil {
			t.Fatalf("test setup: forged record should still pass structural Validate, got %v", err)
		}
		if advance, code := req.CanAdvance(); advance || code != AdvanceAssuranceBelowFloor {
			t.Fatalf("CanAdvance() on a forged low-assurance VERIFIED record = (%v, %s), want (false, %s)", advance, code, AdvanceAssuranceBelowFloor)
		}
	})

	t.Run("every declared kind's floor is enforced: assurance one level below floor always refuses", func(t *testing.T) {
		for _, kind := range AllKinds() {
			floor := AssuranceFloor(kind)
			if floor == trust.AssuranceUnspecified {
				t.Fatalf("kind %s has no declared assurance floor", kind)
			}
			below, ok := oneLevelBelow(floor)
			if !ok {
				continue // floor is already the lowest defined level; nothing below it to test
			}
			req, err := Intake(fixtureIntakeSpec(t, "sec-floor-"+string(kind), kind), DefaultClockTable(), nil, fxWindow)
			if err != nil {
				t.Fatalf("Intake(%s): %v", kind, err)
			}
			refused, err := req.Verify(fixtureEvidence(t, below))
			if err == nil {
				t.Errorf("kind %s: Verify at assurance %s (one below floor %s) unexpectedly succeeded", kind, below, floor)
			}
			if refused.VerificationState != VerificationRefused {
				t.Errorf("kind %s: VerificationState = %s, want %s", kind, refused.VerificationState, VerificationRefused)
			}
			if advance, _ := refused.CanAdvance(); advance {
				t.Errorf("kind %s: refused request advanced", kind)
			}
		}
	})
}

// oneLevelBelow returns the assurance level immediately below a, and false
// when a is already the lowest declared level ([trust.AssuranceLow]).
func oneLevelBelow(a trust.Assurance) (trust.Assurance, bool) {
	switch a {
	case trust.AssuranceHigh:
		return trust.AssuranceSubstantial, true
	case trust.AssuranceSubstantial:
		return trust.AssuranceLow, true
	default:
		return trust.AssuranceUnspecified, false
	}
}
