package position_test

import (
	"testing"
	"time"

	"github.com/monstercameron/hcm-next/internal/domains/evidence"
	"github.com/monstercameron/hcm-next/internal/domains/position"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

var testCalendar = values.CalendarRef{Ref: "position.test.calendar", Version: "1"}

func mustDate(t testing.TB, s string) values.LocalDate {
	t.Helper()
	d, err := values.ParseLocalDate(s)
	if err != nil {
		t.Fatalf("date %q: %v", s, err)
	}
	return d
}

func mustKnownAt(t testing.TB, s string) values.KnownAt {
	t.Helper()
	at, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatalf("known at %q: %v", s, err)
	}
	k, err := values.NewKnownAt(values.NewInstant(at))
	if err != nil {
		t.Fatalf("known at %q: %v", s, err)
	}
	return k
}

func mustRecordedAt(t testing.TB, s string) values.RecordedAt {
	t.Helper()
	at, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatalf("recorded at %q: %v", s, err)
	}
	r, err := values.NewRecordedAt(values.NewInstant(at))
	if err != nil {
		t.Fatalf("recorded at %q: %v", s, err)
	}
	return r
}

// testPositionIDs maps the short, readable position keys the tests use onto
// canonical UUIDs: values.EntityId requires a UUID or ULID body, and a
// hand-typed slug like "pos-1" is neither.
var testPositionIDs = map[string]string{
	"pos-1":       "11111111-1111-4111-8111-111111111111",
	"pos-other":   "22222222-2222-4222-8222-222222222222",
	"pos-missing": "33333333-3333-4333-8333-333333333333",
}

func positionRef(t testing.TB, tenant values.TenantId, key string) values.EntityRef {
	t.Helper()
	id, ok := testPositionIDs[key]
	if !ok {
		id = key
	}
	ref := values.EntityRef{Tenant: tenant, Kind: position.KindPosition, Id: id}
	if err := ref.Validate(); err != nil {
		t.Fatalf("position ref: %v", err)
	}
	return ref
}

func testAsOf(t testing.TB) position.AsOf {
	t.Helper()
	return position.AsOf{EffectiveOn: mustDate(t, "2026-06-01"), KnownAt: mustKnownAt(t, "2026-05-15T00:00:00Z")}
}

// validRevision builds a well-formed OPEN position revision covering the
// testAsOf coordinate.
func validRevision(t testing.TB, tenant values.TenantId, id string) position.PositionRevision {
	t.Helper()
	interval, err := values.NewLocalDateInterval(mustDate(t, "2026-01-01"), mustDate(t, "2026-12-31"), testCalendar)
	if err != nil {
		t.Fatalf("interval: %v", err)
	}
	revision, err := values.NewSequenceRevision("position.revision.pos-1", 10)
	if err != nil {
		t.Fatalf("revision: %v", err)
	}
	capacityFTE, err := values.NewDecimal("2.0000", 4, values.RoundingExactRequired)
	if err != nil {
		t.Fatalf("capacity fte: %v", err)
	}
	return position.PositionRevision{
		Position:    positionRef(t, tenant, id),
		Revision:    revision,
		Effective:   interval,
		Lifecycle:   position.LifecycleOpen,
		JobCode:     "ENG-SWE3",
		OrgUnit:     "eng-platform",
		LegalEntity: "HarborCare US Inc.",
		Capacity: position.CapacityPolicy{
			CapacityFTE:     capacityFTE,
			CapacityHeads:   2,
			OverfillAllowed: false,
		},
		Authority: evidence.SourceAuthority{
			Kind: evidence.AuthorityLocal, System: "hcmnext.position", PolicyRef: "position.source_authority/2026.1",
		},
		Provenance: evidence.Provenance{
			Source: "hcmnext.position", EvidenceRef: "evd_pos_1_r10", RecordedAt: mustRecordedAt(t, "2026-01-02T09:00:00Z"),
		},
	}
}

func TestContracts_Smoke(t *testing.T) {
	if testing.Short() {
		t.Log("short")
	}
}

func TestContracts_NoPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panic: %v", r)
		}
	}()
}

func TestPositionRevisionValidateAndCanonical(t *testing.T) {
	rev := validRevision(t, "tenant-a", "pos-1")
	if err := rev.Validate(); err != nil {
		t.Fatalf("valid revision rejected: %v", err)
	}
	if got := rev.Canonical(); len(got) == 0 {
		t.Fatal("valid revision has no canonical encoding")
	}

	broken := rev
	broken.Lifecycle = "NOT_A_STATE"
	if err := broken.Validate(); err == nil {
		t.Fatal("an undefined lifecycle must be rejected")
	}
	if got := broken.Canonical(); got != nil {
		t.Fatal("an invalid revision must not encode")
	}
}

func TestPositionQueryValidateRejectsCrossTenant(t *testing.T) {
	q := position.PositionQuery{
		Tenant:   "tenant-a",
		Position: positionRef(t, "tenant-b", "pos-1"),
		AsOf:     testAsOf(t),
	}
	if err := q.Validate(); err == nil {
		t.Fatal("a query naming a position outside its tenant must be rejected")
	}
}

func TestLifecycleAcceptsPlacement(t *testing.T) {
	accepts := map[position.Lifecycle]bool{
		position.LifecycleDraft:           false,
		position.LifecycleOpen:            true,
		position.LifecycleReserved:        true,
		position.LifecyclePartiallyFilled: true,
		position.LifecycleFilled:          false,
		position.LifecycleVacant:          true,
		position.LifecycleFrozen:          false,
		position.LifecycleClosed:          false,
	}
	for lifecycle, want := range accepts {
		if got := lifecycle.AcceptsPlacement(); got != want {
			t.Errorf("%s.AcceptsPlacement() = %v, want %v", lifecycle, got, want)
		}
	}
}
