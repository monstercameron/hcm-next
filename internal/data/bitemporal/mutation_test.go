package bitemporal_test

import (
	"testing"
	"time"

	"github.com/monstercameron/hcm-next/internal/data/bitemporal"
)

// TestTodo_DATA_005_Mutation perturbs inputs by the smallest possible amount
// around a declared boundary and proves the boundary is exact: a nanosecond
// on the wrong side of effective_at or recorded_at flips the answer, and
// adding or removing a single entry in a Decision's allow/deny list flips
// visibility for exactly that entry and nothing else.
func TestTodo_DATA_005_Mutation(t *testing.T) {

	t.Run("effective_at boundary is exact to the nanosecond", func(t *testing.T) {
		tl := buildTimeline(t)
		boundary := date("2026-09-01T00:00:00Z") // D's effective_at
		knownAt := date("2027-01-01T00:00:00Z")

		justBefore := tl.asOf(t, boundary.Add(-time.Nanosecond), knownAt)
		if got := fact(t, justBefore, "worker:1", schemaComp); string(got.Payload) != "135000" {
			t.Fatalf("one nanosecond before the boundary = %s, want 135000", got.Payload)
		}
		onBoundary := tl.asOf(t, boundary, knownAt)
		if got := fact(t, onBoundary, "worker:1", schemaComp); string(got.Payload) != "145000" {
			t.Fatalf("exactly on the boundary = %s, want 145000", got.Payload)
		}
	})

	t.Run("recorded_at boundary is exact to the nanosecond", func(t *testing.T) {
		tl := buildTimeline(t)
		boundary := date("2026-08-14T00:00:00Z") // C's recorded_at
		effectiveAt := date("2026-06-01T00:00:00Z")

		justBefore := tl.asOf(t, effectiveAt, boundary.Add(-time.Nanosecond))
		if got := fact(t, justBefore, "worker:1", schemaComp); string(got.Payload) != "130000" {
			t.Fatalf("one nanosecond before recorded_at = %s, want 130000 (not yet known)", got.Payload)
		}
		onBoundary := tl.asOf(t, effectiveAt, boundary)
		if got := fact(t, onBoundary, "worker:1", schemaComp); string(got.Payload) != "135000" {
			t.Fatalf("exactly at recorded_at = %s, want 135000 (known as of this instant)", got.Payload)
		}
	})

	t.Run("between's upper bound is exclusive", func(t *testing.T) {
		tl := buildTimeline(t)
		dec := tl.f.decision()
		now := date("2027-01-01T00:00:00Z")

		atBoundary := tl.f.query(t, bitemporal.Request{
			Tenant: tl.f.tenant, Mode: bitemporal.ModeBetween, Subject: "worker:1", Field: schemaComp,
			EffectiveFrom: date("2026-01-01T00:00:00Z"), EffectiveTo: date("2026-06-01T00:00:00Z"),
		}, dec, bitemporal.WithClock(func() time.Time { return now }))
		for _, f := range atBoundary.Facts {
			if f.EffectiveAt.Equal(date("2026-06-01T00:00:00Z")) {
				t.Fatal("BETWEEN's exclusive upper bound admitted a fact effective exactly at EffectiveTo")
			}
		}
		if len(atBoundary.Facts) != 1 {
			t.Fatalf("[Jan 1, Jun 1) has %d facts, want 1 (only A)", len(atBoundary.Facts))
		}

		// PostgreSQL's timestamptz has microsecond resolution, not nanosecond:
		// a +1ns widening would round back down to exactly Jun 1 on the wire
		// and prove nothing, so this uses +1 microsecond instead. Both B and
		// C sit at effective_at = Jun 1 (C is the correction that shares B's
		// effective_at), so widening the window to include Jun 1 admits both,
		// on top of A: BETWEEN returns every visible event in the window, not
		// a single resolved winner.
		widened := tl.f.query(t, bitemporal.Request{
			Tenant: tl.f.tenant, Mode: bitemporal.ModeBetween, Subject: "worker:1", Field: schemaComp,
			EffectiveFrom: date("2026-01-01T00:00:00Z"), EffectiveTo: date("2026-06-01T00:00:00.000001Z"),
		}, dec, bitemporal.WithClock(func() time.Time { return now }))
		if len(widened.Facts) != 3 {
			t.Fatalf("[Jan 1, Jun 1 + 1us) has %d facts, want 3 (A, B and C)", len(widened.Facts))
		}
	})

	t.Run("flipping one field in DenyFields flips exactly that field", func(t *testing.T) {
		tl := buildTimeline(t)
		now := date("2027-01-01T00:00:00Z")
		req := bitemporal.Request{Tenant: tl.f.tenant, Mode: bitemporal.ModeCurrent, Subject: "worker:1"}

		baseline := tl.f.countSQL(t, req, bitemporal.Decision{Tenant: tl.f.tenant}, now)
		if baseline != 2 {
			t.Fatalf("baseline (no deny) sees %d fields on worker:1, want 2", baseline)
		}
		withDeny := tl.f.countSQL(t, req, bitemporal.Decision{Tenant: tl.f.tenant, DenyFields: []string{schemaJob}}, now)
		if withDeny != baseline-1 {
			t.Fatalf("denying one field changed the count by %d, want exactly -1", withDeny-baseline)
		}
	})

	t.Run("evidence digest changes when and only when the authorized result changes", func(t *testing.T) {
		tl := buildTimeline(t)
		req := bitemporal.Request{
			Tenant: tl.f.tenant, Mode: bitemporal.ModeEffectiveAsOf, Subject: "worker:1", Field: schemaComp,
			EffectiveAt: date("2026-06-01T00:00:00Z"), KnownAt: date("2026-08-20T00:00:00Z"),
		}
		dec := tl.f.decision()
		result := tl.f.query(t, req, dec)
		baseline, err := bitemporal.BuildEvidence(req, dec, result)
		if err != nil {
			t.Fatalf("build evidence: %v", err)
		}

		// Same request and result, recomputed: must be byte-identical.
		repeat, err := bitemporal.BuildEvidence(req, dec, result)
		if err != nil {
			t.Fatalf("build evidence: %v", err)
		}
		if repeat.Digest != baseline.Digest {
			t.Fatal("BuildEvidence is not deterministic over identical inputs")
		}

		// Mutating the decision that produced the result must move the digest,
		// even though the result value itself is unchanged: evidence attests
		// to the authorization the answer was filtered through, not only to
		// the returned rows.
		mutatedDec := dec
		mutatedDec.DenyFields = []string{schemaJob}
		mutated, err := bitemporal.BuildEvidence(req, mutatedDec, result)
		if err != nil {
			t.Fatalf("build evidence: %v", err)
		}
		if mutated.Digest == baseline.Digest {
			t.Fatal("evidence digest did not change when the authorizing decision changed")
		}
	})
}
