package bitemporal_test

import (
	"context"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/data/bitemporal"
	"github.com/monstercameron/human-capital-management-suite/internal/data/ledger"
)

// timeline is the fixed scenario every DATA-005 test resolves against,
// modeled directly on the effective-date debugger example in
// specs/hris-admin-dataops.md:
//
//	effective  Jan 1        Jun 1              Sep 1
//	           $120k ------- $130k ------------ $145k
//	                             ^
//	                             | correction recorded Aug 14: Jun 1 should be $135k
//
// A and B are ordinary domain facts. C is a CORRECTION of B sharing B's
// effective_at (Jun 1) - a retroactive fix, recorded months later. D is a
// CORRECTION of C whose own effective_at (Sep 1) differs from C's - an
// explicit supersession that also introduces a new business-time boundary.
type timeline struct {
	f          *fixture
	a, b, c, d ledger.AppendReceipt
	jobTitle   ledger.AppendReceipt
}

func buildTimeline(t *testing.T) timeline {
	t.Helper()
	f := newFixture(t)

	a := f.append(t, factSpec{
		Stream: "worker:1", Schema: schemaComp,
		EffectiveAt: date("2026-01-01T00:00:00Z"), RecordedAt: date("2026-01-05T00:00:00Z"),
		Payload: "120000",
	})
	b := f.append(t, factSpec{
		Stream: "worker:1", Schema: schemaComp,
		EffectiveAt: date("2026-06-01T00:00:00Z"), RecordedAt: date("2026-06-02T00:00:00Z"),
		Payload: "130000",
	})
	c := f.append(t, factSpec{
		Stream: "worker:1", Schema: schemaComp, Class: ledger.Correction,
		EffectiveAt: date("2026-06-01T00:00:00Z"), RecordedAt: date("2026-08-14T00:00:00Z"),
		Payload: "135000", Corrects: ref(b),
	})
	d := f.append(t, factSpec{
		Stream: "worker:1", Schema: schemaComp, Class: ledger.Correction,
		EffectiveAt: date("2026-09-01T00:00:00Z"), RecordedAt: date("2026-09-01T00:00:00Z"),
		Payload: "145000", Corrects: ref(c),
	})
	job := f.append(t, factSpec{
		Stream: "worker:1", Schema: schemaJob,
		EffectiveAt: date("2026-01-01T00:00:00Z"), RecordedAt: date("2026-01-05T00:00:00Z"),
		Payload: "Engineer",
	})
	// Independent facts on other subjects, used by the authorization tests.
	f.append(t, factSpec{
		Stream: "worker:2", Schema: schemaComp,
		EffectiveAt: date("2026-01-01T00:00:00Z"), RecordedAt: date("2026-01-05T00:00:00Z"),
		Payload: "90000",
	})
	f.append(t, factSpec{
		Stream: "worker:3", Schema: schemaComp,
		EffectiveAt: date("2026-01-01T00:00:00Z"), RecordedAt: date("2026-01-05T00:00:00Z"),
		Payload: "95000",
	})

	return timeline{f: f, a: a, b: b, c: c, d: d, jobTitle: job}
}

// asOf is a small helper over bitemporal.Query for the EFFECTIVE_AS_OF mode,
// used throughout so each scenario reads as (effective, known) -> fact.
func (tl timeline) asOf(t *testing.T, effectiveAt, knownAt time.Time) bitemporal.Result {
	t.Helper()
	return tl.f.query(t, bitemporal.Request{
		Tenant: tl.f.tenant, Mode: bitemporal.ModeEffectiveAsOf,
		Subject: "worker:1", Field: schemaComp,
		EffectiveAt: effectiveAt, KnownAt: knownAt,
	}, tl.f.decision())
}

// TestTodo_DATA_005 proves the bitemporal query surface end to end: AsOf and
// KnownAt each resolve their own axis, corrections and supersessions are
// distinguished, effective boundaries are half-open, authorization is
// enforced as a SQL predicate rather than a Go-side filter, ordering is
// deterministic, pagination is keyset-based, and a fixed (asOf, knownAt)
// answer never moves after later appends.
func TestTodo_DATA_005(t *testing.T) {

	t.Run("as-of resolves business time independent of knowledge time", func(t *testing.T) {
		tl := buildTimeline(t)

		result := tl.asOf(t, date("2026-03-01T00:00:00Z"), date("2027-01-01T00:00:00Z"))
		got := fact(t, result, "worker:1", schemaComp)
		if string(got.Payload) != "120000" {
			t.Fatalf("asOf(Mar 1) = %s, want 120000 (the Jan 1 fact)", got.Payload)
		}
		if got.CorrectionKind != bitemporal.KindOriginal {
			t.Fatalf("asOf(Mar 1) kind = %s, want ORIGINAL", got.CorrectionKind)
		}
		if !got.EffectiveAt.Equal(date("2026-01-01T00:00:00Z")) {
			t.Fatalf("asOf(Mar 1) effective_at = %s, want Jan 1", got.EffectiveAt)
		}
	})

	t.Run("known-at hides a correction not yet recorded", func(t *testing.T) {
		tl := buildTimeline(t)

		before := tl.asOf(t, date("2026-06-01T00:00:00Z"), date("2026-06-10T00:00:00Z"))
		gotBefore := fact(t, before, "worker:1", schemaComp)
		if string(gotBefore.Payload) != "130000" || gotBefore.CorrectionKind != bitemporal.KindOriginal {
			t.Fatalf("known before correction = %s/%s, want 130000/ORIGINAL", gotBefore.Payload, gotBefore.CorrectionKind)
		}

		after := tl.asOf(t, date("2026-06-01T00:00:00Z"), date("2026-08-20T00:00:00Z"))
		gotAfter := fact(t, after, "worker:1", schemaComp)
		if string(gotAfter.Payload) != "135000" {
			t.Fatalf("known after correction = %s, want 135000", gotAfter.Payload)
		}
		if gotAfter.CorrectionKind != bitemporal.KindCorrection {
			t.Fatalf("correction sharing its target's effective_at classified %s, want CORRECTION", gotAfter.CorrectionKind)
		}
		if gotAfter.Corrects == nil || gotAfter.Corrects.Sequence != tl.b.Sequence {
			t.Fatalf("correction Corrects = %+v, want sequence %d", gotAfter.Corrects, tl.b.Sequence)
		}
	})

	t.Run("a supersession carries a new effective boundary forward", func(t *testing.T) {
		tl := buildTimeline(t)

		justBefore := tl.asOf(t, date("2026-08-31T23:59:59Z"), date("2027-01-01T00:00:00Z"))
		if got := fact(t, justBefore, "worker:1", schemaComp); string(got.Payload) != "135000" {
			t.Fatalf("just before Sep 1 = %s, want 135000 (still the correction)", got.Payload)
		}

		onBoundary := tl.asOf(t, date("2026-09-01T00:00:00Z"), date("2027-01-01T00:00:00Z"))
		got := fact(t, onBoundary, "worker:1", schemaComp)
		if string(got.Payload) != "145000" {
			t.Fatalf("on Sep 1 = %s, want 145000", got.Payload)
		}
		if got.CorrectionKind != bitemporal.KindSupersession {
			t.Fatalf("correction with a different effective_at than its target classified %s, want SUPERSESSION", got.CorrectionKind)
		}
	})

	t.Run("effective boundaries are half-open", func(t *testing.T) {
		tl := buildTimeline(t)

		before := tl.asOf(t, date("2026-01-01T00:00:00Z").Add(-time.Nanosecond), date("2027-01-01T00:00:00Z"))
		mustNotFind(t, before, "worker:1", schemaComp)

		onStart := tl.asOf(t, date("2026-01-01T00:00:00Z"), date("2027-01-01T00:00:00Z"))
		if got := fact(t, onStart, "worker:1", schemaComp); string(got.Payload) != "120000" {
			t.Fatalf("exactly at the first fact's effective_at = %s, want 120000", got.Payload)
		}
	})

	t.Run("KnownAt resolves the same fact as an equivalent EFFECTIVE_AS_OF query", func(t *testing.T) {
		tl := buildTimeline(t)

		viaMode := tl.asOf(t, date("2026-06-01T00:00:00Z"), date("2026-08-20T00:00:00Z"))
		viaHelper, err := bitemporal.KnownAt(context.Background(), tl.f.db.Conn, tl.f.tenant, "worker:1", schemaComp, date("2026-08-20T00:00:00Z"), tl.f.decision(),
			bitemporal.WithClock(func() time.Time { return date("2026-06-01T00:00:00Z") }))
		if err != nil {
			t.Fatalf("KnownAt: %v", err)
		}
		gotMode := fact(t, viaMode, "worker:1", schemaComp)
		gotHelper := fact(t, viaHelper, "worker:1", schemaComp)
		if gotMode.Digest != gotHelper.Digest {
			t.Fatalf("Query(EFFECTIVE_AS_OF) and KnownAt() disagree: %s vs %s", gotMode.Digest, gotHelper.Digest)
		}
	})

	t.Run("AsOf helper matches the equivalent Query call", func(t *testing.T) {
		tl := buildTimeline(t)

		viaMode := tl.asOf(t, date("2026-03-01T00:00:00Z"), date("2027-01-01T00:00:00Z"))
		viaHelper, err := bitemporal.AsOf(context.Background(), tl.f.db.Conn, tl.f.tenant, "worker:1", schemaComp, date("2026-03-01T00:00:00Z"), tl.f.decision())
		if err != nil {
			t.Fatalf("AsOf: %v", err)
		}
		if fact(t, viaMode, "worker:1", schemaComp).Digest != fact(t, viaHelper, "worker:1", schemaComp).Digest {
			t.Fatal("AsOf() and Query(EFFECTIVE_AS_OF) disagree")
		}
	})

	t.Run("fields authorized by SQL predicate: a denied field never reaches the driver", func(t *testing.T) {
		tl := buildTimeline(t)
		dec := bitemporal.Decision{Tenant: tl.f.tenant, DenyFields: []string{schemaJob}}
		req := bitemporal.Request{Tenant: tl.f.tenant, Mode: bitemporal.ModeCurrent, Subject: "worker:1"}
		now := date("2027-01-01T00:00:00Z")

		slots := tl.f.rawSlots(t, req, dec, now)
		if slots[[2]string{"worker:1", schemaJob}] {
			t.Fatal("PostgreSQL's own result set contains the denied field; the WHERE clause did not exclude it")
		}
		if !slots[[2]string{"worker:1", schemaComp}] {
			t.Fatal("the authorized field is missing from PostgreSQL's result set")
		}

		result := tl.f.query(t, req, dec, bitemporal.WithClock(func() time.Time { return now }))
		mustNotFind(t, result, "worker:1", schemaJob)
		if len(result.Facts) != tl.f.countSQL(t, req, dec, now) {
			t.Fatalf("Query returned %d facts but the SQL it ran returned a different count; Go-side filtering is happening", len(result.Facts))
		}
	})

	t.Run("subjects authorized by SQL predicate: a withheld subject is not counted", func(t *testing.T) {
		tl := buildTimeline(t)
		dec := bitemporal.Decision{Tenant: tl.f.tenant, DenySubjects: []string{"worker:2"}}
		req := bitemporal.Request{Tenant: tl.f.tenant, Mode: bitemporal.ModeCurrent, Field: schemaComp}
		now := date("2027-01-01T00:00:00Z")

		slots := tl.f.rawSlots(t, req, dec, now)
		if slots[[2]string{"worker:2", schemaComp}] {
			t.Fatal("PostgreSQL's own result set contains the withheld subject")
		}
		for _, s := range []string{"worker:1", "worker:3"} {
			if !slots[[2]string{s, schemaComp}] {
				t.Fatalf("authorized subject %s is missing from PostgreSQL's result set", s)
			}
		}

		result := tl.f.query(t, req, dec, bitemporal.WithClock(func() time.Time { return now }))
		subjects := map[string]bool{}
		for _, f := range result.Facts {
			subjects[f.StreamKey] = true
		}
		if len(subjects) != 2 {
			t.Fatalf("result names %d distinct subjects, want 2 (withheld subject must not be counted)", len(subjects))
		}
		if subjects["worker:2"] {
			t.Fatal("withheld subject appears in the query result")
		}
	})

	t.Run("keyset pagination reproduces the unlimited answer", func(t *testing.T) {
		tl := buildTimeline(t)
		dec := tl.f.decision()
		now := date("2027-01-01T00:00:00Z")

		full := tl.f.query(t, bitemporal.Request{
			Tenant: tl.f.tenant, Mode: bitemporal.ModeHistory, Subject: "worker:1", Field: schemaComp,
		}, dec, bitemporal.WithClock(func() time.Time { return now }))
		if len(full.Facts) != 4 {
			t.Fatalf("unlimited history has %d facts, want 4 (A, B, C, D)", len(full.Facts))
		}

		var paged []bitemporal.Fact
		cursor := ""
		for page := 0; ; page++ {
			if page > 10 {
				t.Fatal("pagination did not terminate")
			}
			result := tl.f.query(t, bitemporal.Request{
				Tenant: tl.f.tenant, Mode: bitemporal.ModeHistory, Subject: "worker:1", Field: schemaComp,
				Limit: 2, Cursor: cursor,
			}, dec, bitemporal.WithClock(func() time.Time { return now }))
			paged = append(paged, result.Facts...)
			if result.NextCursor == "" {
				break
			}
			cursor = result.NextCursor
		}

		if len(paged) != len(full.Facts) {
			t.Fatalf("paged result has %d facts, want %d", len(paged), len(full.Facts))
		}
		for i := range full.Facts {
			if paged[i].Digest != full.Facts[i].Digest || paged[i].EventID != full.Facts[i].EventID {
				t.Fatalf("page %d disagrees with the unlimited order at position %d", i, i)
			}
		}
	})

	t.Run("a fixed (asOf, knownAt) answer is stable after later appends", func(t *testing.T) {
		tl := buildTimeline(t)
		frozenKnownAt := date("2026-08-20T00:00:00Z") // after C, before D
		req := bitemporal.Request{
			Tenant: tl.f.tenant, Mode: bitemporal.ModeEffectiveAsOf,
			Subject: "worker:1", Field: schemaComp,
			EffectiveAt: date("2026-06-01T00:00:00Z"), KnownAt: frozenKnownAt,
		}
		dec := tl.f.decision()

		before := tl.f.query(t, req, dec)
		evidenceBefore, err := bitemporal.BuildEvidence(req, dec, before)
		if err != nil {
			t.Fatalf("build evidence: %v", err)
		}

		// New appends land well after the frozen KnownAt: a same-effective
		// correction, and an unrelated later fact on a different subject.
		tl.f.append(t, factSpec{
			Stream: "worker:1", Schema: schemaComp, Class: ledger.Correction,
			EffectiveAt: date("2026-06-01T00:00:00Z"), RecordedAt: date("2026-12-01T00:00:00Z"),
			Payload: "999999", Corrects: ref(tl.c),
		})
		tl.f.append(t, factSpec{
			Stream: "worker:3", Schema: schemaComp,
			EffectiveAt: date("2026-12-01T00:00:00Z"), RecordedAt: date("2026-12-01T00:00:00Z"),
			Payload: "1",
		})

		after := tl.f.query(t, req, dec)
		evidenceAfter, err := bitemporal.BuildEvidence(req, dec, after)
		if err != nil {
			t.Fatalf("build evidence: %v", err)
		}

		if len(before.Facts) != 1 || len(after.Facts) != 1 {
			t.Fatalf("expected exactly one resolved fact before and after, got %d and %d", len(before.Facts), len(after.Facts))
		}
		if before.Facts[0].Digest != after.Facts[0].Digest {
			t.Fatalf("the resolved fact changed after a later append: %s -> %s", before.Facts[0].Digest, after.Facts[0].Digest)
		}
		if string(after.Facts[0].Payload) != "135000" {
			t.Fatalf("frozen answer is %s, want 135000 (the append after KnownAt must not leak in)", after.Facts[0].Payload)
		}
		if evidenceBefore.Digest != evidenceAfter.Digest {
			t.Fatalf("evidence digest moved after a later append: %s -> %s", evidenceBefore.Digest, evidenceAfter.Digest)
		}
	})

	t.Run("a tenant mismatch between request and decision is refused before any SQL runs", func(t *testing.T) {
		tl := buildTimeline(t)
		_, err := bitemporal.Query(context.Background(), tl.f.db.Conn, bitemporal.Request{
			Tenant: tl.f.tenant, Mode: bitemporal.ModeCurrent,
		}, bitemporal.Decision{Tenant: uuidOtherThan(tl.f.tenant)})
		if err == nil {
			t.Fatal("a request for one tenant under another tenant's decision succeeded")
		}
	})
}
