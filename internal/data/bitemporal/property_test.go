package bitemporal_test

import (
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/data/bitemporal"
)

// TestTodo_DATA_005_Property drives the resolution rule (greatest effective_at,
// then greatest recorded_at, then greatest sequence) across a spread of
// (asOf, knownAt) pairs against the fixed timeline, so the winner at every
// combination - not just the cases the primary test happens to narrate - is
// pinned to one deterministic answer.
func TestTodo_DATA_005_Property(t *testing.T) {
	tl := buildTimeline(t)

	cases := []struct {
		name        string
		effectiveAt string
		knownAt     string
		wantPayload string
		wantKind    bitemporal.CorrectionKind
		wantAbsent  bool
	}{
		{"before every fact", "2025-12-31T00:00:00Z", "2027-01-01T00:00:00Z", "", "", true},
		{"at the first fact, before anything else was known", "2026-01-01T00:00:00Z", "2026-01-05T00:00:00Z", "120000", bitemporal.KindOriginal, false},
		{"between first and second, fully known", "2026-03-01T00:00:00Z", "2027-01-01T00:00:00Z", "120000", bitemporal.KindOriginal, false},
		{"at the second fact, before its correction was recorded", "2026-06-01T00:00:00Z", "2026-06-05T00:00:00Z", "130000", bitemporal.KindOriginal, false},
		{"at the second fact, after its correction was recorded", "2026-06-01T00:00:00Z", "2026-08-15T00:00:00Z", "135000", bitemporal.KindCorrection, false},
		{"between second and third, after the correction", "2026-07-15T00:00:00Z", "2027-01-01T00:00:00Z", "135000", bitemporal.KindCorrection, false},
		{"exactly at the correction's own recorded_at", "2026-06-01T00:00:00Z", "2026-08-14T00:00:00Z", "135000", bitemporal.KindCorrection, false},
		{"one nanosecond before the correction was recorded", "2026-06-01T00:00:00Z", "2026-08-13T23:59:59.999999999Z", "130000", bitemporal.KindOriginal, false},
		{"at the supersession", "2026-09-01T00:00:00Z", "2027-01-01T00:00:00Z", "145000", bitemporal.KindSupersession, false},
		{"well after the supersession", "2026-12-31T00:00:00Z", "2027-01-01T00:00:00Z", "145000", bitemporal.KindSupersession, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result := tl.asOf(t, date(tc.effectiveAt), date(tc.knownAt))
			if tc.wantAbsent {
				mustNotFind(t, result, "worker:1", schemaComp)
				return
			}
			got := fact(t, result, "worker:1", schemaComp)
			if string(got.Payload) != tc.wantPayload {
				t.Fatalf("asOf(%s) known(%s) = %s, want %s", tc.effectiveAt, tc.knownAt, got.Payload, tc.wantPayload)
			}
			if got.CorrectionKind != tc.wantKind {
				t.Fatalf("asOf(%s) known(%s) kind = %s, want %s", tc.effectiveAt, tc.knownAt, got.CorrectionKind, tc.wantKind)
			}
		})
	}
}

// TestTodo_DATA_005_Property_MonotonicKnowledge proves that widening the
// KnownAt horizon can only add or replace what is visible, never remove
// evidence that was already visible: knowledge accumulates.
func TestTodo_DATA_005_Property_MonotonicKnowledge(t *testing.T) {
	tl := buildTimeline(t)

	knownAtSteps := []string{
		"2026-01-05T00:00:00Z",
		"2026-06-02T00:00:00Z",
		"2026-08-14T00:00:00Z",
		"2026-09-01T00:00:00Z",
		"2027-01-01T00:00:00Z",
	}
	req := func(knownAt time.Time) bitemporal.Request {
		return bitemporal.Request{
			Tenant: tl.f.tenant, Mode: bitemporal.ModeHistory,
			Subject: "worker:1", Field: schemaComp, KnownAt: knownAt,
		}
	}
	var previous int
	for _, step := range knownAtSteps {
		result := tl.f.query(t, req(date(step)), tl.f.decision())
		if len(result.Facts) < previous {
			t.Fatalf("known-at %s sees %d facts, fewer than the %d visible at an earlier known-at", step, len(result.Facts), previous)
		}
		previous = len(result.Facts)
	}
	if previous != 4 {
		t.Fatalf("final known-at sees %d facts, want 4 (A, B, C, D)", previous)
	}
}
