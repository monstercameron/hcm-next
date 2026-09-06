package dsr

import (
	"testing"

	"github.com/monstercameron/hcm-next/internal/governance/legal"
)

// TestTodo_PRIV_005_Golden is the GOLDEN matrix test for PRIV-005: fixed
// inputs produce fixed, recorded outputs -- the statutory deadline
// arithmetic, the jurisdiction-specificity fallback order, and the request's
// own canonical digest all pin to an exact recorded value, not merely "some"
// value.
func TestTodo_PRIV_005_Golden(t *testing.T) {
	t.Run("US-CA ACCESS deadline is received_at plus the declared 45 calendar days", func(t *testing.T) {
		deadline, err := DefaultClockTable().Deadline(legal.Jurisdiction{Country: "US", State: "CA"}, KindAccess, mustInstant(t, fxReceivedAt))
		if err != nil {
			t.Fatalf("Deadline: %v", err)
		}
		const wantSec = fxReceivedAt + 45*86400
		gotSec, _ := deadline.Unix()
		if gotSec != wantSec {
			t.Errorf("deadline unix seconds = %d, want %d", gotSec, wantSec)
		}
	})

	t.Run("an undeclared jurisdiction falls back to the zero-value default entry", func(t *testing.T) {
		// US-NY has no jurisdiction-specific RECTIFICATION entry in
		// DefaultClockTable; it must fall through country-only (also
		// undeclared) to the zero-value default, which resolves to the same
		// 45-day figure as the US-CA entries.
		deadline, err := DefaultClockTable().Deadline(legal.Jurisdiction{Country: "US", State: "NY"}, KindRectification, mustInstant(t, fxReceivedAt))
		if err != nil {
			t.Fatalf("Deadline: %v", err)
		}
		const wantSec = fxReceivedAt + 45*86400
		gotSec, _ := deadline.Unix()
		if gotSec != wantSec {
			t.Errorf("fallback deadline unix seconds = %d, want %d", gotSec, wantSec)
		}
	})

	t.Run("a fixed intake fixture's evidence id is a stable, recorded digest", func(t *testing.T) {
		req := fixtureRequest(t)
		const wantEvidenceID = "ev:privacy:dsr:" + goldenIntakeDigest
		if req.EvidenceID != wantEvidenceID {
			t.Errorf("EvidenceID = %q, want %q (recorded golden digest; update goldenIntakeDigest deliberately if canonicalBytes intentionally changed)", req.EvidenceID, wantEvidenceID)
		}
	})
}

// goldenIntakeDigest is fixtureRequest's recorded [DataSubjectRequest.Digest]
// hex value. It was computed by running this package's tests once against
// the implementation in request.go's canonicalBytes; any deliberate change
// to that encoding (a field added, removed, or reordered) needs this
// constant updated in the same change, which is exactly the tamper-evidence
// property this golden test exists to enforce.
const goldenIntakeDigest = "e1474a86448422903814d5c51ed4851e764279bec1c5829a53670f301d940787"
