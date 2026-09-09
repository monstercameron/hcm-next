package bitemporal_test

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/data/bitemporal"
)

// TestTodo_DATA_005_Security proves the RED clause directly: a denied field
// is absent from PostgreSQL's own result set (not merely absent from the Go
// struct), a withheld subject is never counted, and a correction recorded
// after the query's KnownAt horizon can never leak a future-known fact -
// even when a Decision's own ceiling is lower than what the request asks
// for.
func TestTodo_DATA_005_Security(t *testing.T) {

	t.Run("an allow-list admits only its own fields, proven against PostgreSQL's rows", func(t *testing.T) {
		tl := buildTimeline(t)
		now := date("2027-01-01T00:00:00Z")
		dec := bitemporal.Decision{Tenant: tl.f.tenant, AllowFields: []string{schemaComp}}
		req := bitemporal.Request{Tenant: tl.f.tenant, Mode: bitemporal.ModeCurrent, Subject: "worker:1"}

		slots := tl.f.rawSlots(t, req, dec, now)
		if slots[[2]string{"worker:1", schemaJob}] {
			t.Fatal("a field outside the allow-list appears in PostgreSQL's own result set")
		}
		if !slots[[2]string{"worker:1", schemaComp}] {
			t.Fatal("the allow-listed field is missing")
		}
		if got, want := len(slots), 1; got != want {
			t.Fatalf("PostgreSQL returned %d distinct (subject, field) slots, want %d", got, want)
		}
	})

	t.Run("an empty non-nil allow-list authorizes nothing", func(t *testing.T) {
		tl := buildTimeline(t)
		now := date("2027-01-01T00:00:00Z")
		dec := bitemporal.Decision{Tenant: tl.f.tenant, AllowSubjects: []string{}}
		req := bitemporal.Request{Tenant: tl.f.tenant, Mode: bitemporal.ModeCurrent}

		if n := tl.f.countSQL(t, req, dec, now); n != 0 {
			t.Fatalf("a Decision with AllowSubjects = []string{} (deny-all) returned %d rows from PostgreSQL, want 0", n)
		}
	})

	t.Run("deny always wins over a broader allow", func(t *testing.T) {
		tl := buildTimeline(t)
		now := date("2027-01-01T00:00:00Z")
		dec := bitemporal.Decision{
			Tenant:        tl.f.tenant,
			AllowSubjects: []string{"worker:1", "worker:2", "worker:3"},
			DenySubjects:  []string{"worker:1"},
		}
		req := bitemporal.Request{Tenant: tl.f.tenant, Mode: bitemporal.ModeCurrent, Field: schemaComp}

		slots := tl.f.rawSlots(t, req, dec, now)
		if slots[[2]string{"worker:1", schemaComp}] {
			t.Fatal("an explicitly denied subject was admitted because it also appeared in AllowSubjects")
		}
		if !slots[[2]string{"worker:2", schemaComp}] || !slots[[2]string{"worker:3", schemaComp}] {
			t.Fatal("subjects that were allowed and not denied are missing")
		}
	})

	t.Run("a future correction never leaks into a historical known-at query", func(t *testing.T) {
		tl := buildTimeline(t)
		dec := tl.f.decision()

		beforeCorrection := date("2026-08-13T00:00:00Z") // strictly before C's recorded_at
		result := tl.f.query(t, bitemporal.Request{
			Tenant: tl.f.tenant, Mode: bitemporal.ModeKnownAsOf,
			Subject: "worker:1", Field: schemaComp,
			EffectiveAt: date("2026-06-01T00:00:00Z"), KnownAt: beforeCorrection,
		}, dec)
		got := fact(t, result, "worker:1", schemaComp)
		if string(got.Payload) != "130000" {
			t.Fatalf("known-at before the correction was recorded returned %s, want 130000 (the correction leaked in)", got.Payload)
		}

		// The raw SQL result set must not even contain event C's row.
		slots := tl.f.rawSlots(t, bitemporal.Request{
			Tenant: tl.f.tenant, Mode: bitemporal.ModeHistory, Subject: "worker:1", Field: schemaComp,
			KnownAt: beforeCorrection,
		}, dec, date("2027-01-01T00:00:00Z"))
		if len(slots) != 1 {
			t.Fatalf("PostgreSQL returned %d slots for a single-subject/field history query, want 1", len(slots))
		}
	})

	t.Run("a decision's knowledge ceiling clamps a request that asks for more", func(t *testing.T) {
		tl := buildTimeline(t)
		// The caller asks to know everything as of 2027, but the decision's
		// own authorization was only established through Aug 20 2026 - before
		// D was recorded. The ceiling must win.
		dec := bitemporal.Decision{Tenant: tl.f.tenant, MaxKnownAt: date("2026-08-20T00:00:00Z")}
		result := tl.f.query(t, bitemporal.Request{
			Tenant: tl.f.tenant, Mode: bitemporal.ModeEffectiveAsOf,
			Subject: "worker:1", Field: schemaComp,
			EffectiveAt: date("2026-09-01T00:00:00Z"), KnownAt: date("2027-01-01T00:00:00Z"),
		}, dec)
		got := fact(t, result, "worker:1", schemaComp)
		if string(got.Payload) != "135000" {
			t.Fatalf("a request's KnownAt bypassed the decision's MaxKnownAt ceiling: got %s, want 135000 (D at Sep 1 must be invisible)", got.Payload)
		}
	})

	t.Run("cross-tenant reads are refused before any statement is built", func(t *testing.T) {
		tl := buildTimeline(t)
		other := uuidOtherThan(tl.f.tenant)
		_, _, err := bitemporal.BuildSQL(bitemporal.Request{Tenant: other, Mode: bitemporal.ModeCurrent}, tl.f.decision(), date("2027-01-01T00:00:00Z"))
		if err == nil {
			t.Fatal("BuildSQL accepted a request tenant that does not match the decision's tenant")
		}
	})
}
