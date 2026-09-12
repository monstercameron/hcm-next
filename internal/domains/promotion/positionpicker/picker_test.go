package positionpicker_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/evidence"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/position"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/promotion/positionpicker"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

var pickerCalendar = values.CalendarRef{Ref: "positionpicker.test.calendar", Version: "1"}

func pickerDate(t testing.TB, s string) values.LocalDate {
	t.Helper()
	d, err := values.ParseLocalDate(s)
	if err != nil {
		t.Fatalf("date %q: %v", s, err)
	}
	return d
}

func pickerKnownAt(t testing.TB, s string) values.KnownAt {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatalf("known at %q: %v", s, err)
	}
	k, err := values.NewKnownAt(values.NewInstant(parsed))
	if err != nil {
		t.Fatalf("known at %q: %v", s, err)
	}
	return k
}

func pickerAsOf(t testing.TB) position.AsOf {
	t.Helper()
	return position.AsOf{EffectiveOn: pickerDate(t, "2027-02-01"), KnownAt: pickerKnownAt(t, "2027-01-15T00:00:00Z")}
}

func pickerTenant() values.TenantId { return values.TenantId("positionpicker-tenant") }

func pickerPosition(t testing.TB, id string) values.EntityRef {
	t.Helper()
	ref := values.EntityRef{Tenant: pickerTenant(), Kind: position.KindPosition, Id: id}
	if err := ref.Validate(); err != nil {
		t.Fatalf("position ref: %v", err)
	}
	return ref
}

func pickerRevision(t testing.TB, pos values.EntityRef, jobCode, orgUnit string, capacityHeads int64) position.PositionRevision {
	t.Helper()
	interval, err := values.NewLocalDateInterval(pickerDate(t, "2026-01-01"), pickerDate(t, "2028-12-31"), pickerCalendar)
	if err != nil {
		t.Fatalf("interval: %v", err)
	}
	rev, err := values.NewSequenceRevision("positionpicker.revision", 1)
	if err != nil {
		t.Fatalf("revision: %v", err)
	}
	fte, err := values.NewDecimal("1.0000", 4, values.RoundingExactRequired)
	if err != nil {
		t.Fatalf("decimal: %v", err)
	}
	recordedAt, err := time.Parse(time.RFC3339, "2026-01-02T09:00:00Z")
	if err != nil {
		t.Fatalf("recorded at: %v", err)
	}
	recorded, err := values.NewRecordedAt(values.NewInstant(recordedAt))
	if err != nil {
		t.Fatalf("recorded at: %v", err)
	}
	return position.PositionRevision{
		Position: pos, Revision: rev, Effective: interval, Lifecycle: position.LifecycleOpen,
		JobCode: jobCode, OrgUnit: orgUnit, LegalEntity: "ACME US Inc.",
		Capacity: position.CapacityPolicy{CapacityFTE: fte, CapacityHeads: capacityHeads},
		Authority: evidence.SourceAuthority{
			Kind: evidence.AuthorityLocal, System: "hcmnext.position", PolicyRef: "position.source_authority/2026.1",
		},
		Provenance: evidence.Provenance{Source: "hcmnext.position", EvidenceRef: "evd_picker", RecordedAt: recorded},
	}
}

type pickerFakeReader struct {
	byID map[string]position.PositionRevision
}

func (f pickerFakeReader) PositionRevisionAt(_ context.Context, q position.PositionQuery) (position.PositionRevision, bool, error) {
	rev, ok := f.byID[q.Position.Id]
	if !ok {
		return position.PositionRevision{}, false, nil
	}
	return rev, true, nil
}

// TestResolveCandidatesFiltersToAuthorizedCompatibleVacantPositions proves
// the picker's whole contract in one pass: an authorized, compatible,
// vacant position is disclosed with every GREEN field; an unauthorized, a
// nonexistent, an incompatible and an at-capacity candidate are all
// silently absent rather than erroring the whole call.
func TestResolveCandidatesFiltersToAuthorizedCompatibleVacantPositions(t *testing.T) {
	ctx := context.Background()
	authorizedID := "11111111-1111-4111-8111-111111111111"
	unauthorizedID := "22222222-2222-4222-8222-222222222222"
	nonexistentID := "33333333-3333-4333-8333-333333333333"
	incompatibleID := "44444444-4444-4444-8444-444444444444"
	atCapacityID := "55555555-5555-4555-8555-555555555555"

	authorizedPos := pickerPosition(t, authorizedID)
	unauthorizedPos := pickerPosition(t, unauthorizedID)
	nonexistentPos := pickerPosition(t, nonexistentID)
	incompatiblePos := pickerPosition(t, incompatibleID)
	atCapacityPos := pickerPosition(t, atCapacityID)

	reader := pickerFakeReader{byID: map[string]position.PositionRevision{
		authorizedID:   pickerRevision(t, authorizedPos, "ENG-MGR", "ENGINEERING", 1),
		unauthorizedID: pickerRevision(t, unauthorizedPos, "ENG-MGR", "ENGINEERING", 1),
		incompatibleID: pickerRevision(t, incompatiblePos, "SALES-REP", "SALES", 1),
		atCapacityID:   pickerRevision(t, atCapacityPos, "ENG-MGR", "ENGINEERING", 0),
	}}

	req := positionpicker.Request{
		Tenant: pickerTenant(), AsOf: pickerAsOf(t),
		DesiredJobCode: "ENG-MGR", DesiredOrgUnit: "ENGINEERING",
		Authorize: func(rev position.PositionRevision) bool { return rev.Position.Id != unauthorizedID },
		Directory: []positionpicker.DirectoryEntry{
			{Position: authorizedPos, Title: "Engineering Manager", Organization: "Engineering", Manager: "Jane Smith", Location: "Remote"},
			{Position: unauthorizedPos, Title: "Should Never Appear", Manager: "Nobody"},
			{Position: nonexistentPos, Title: "Also Should Never Appear"},
			{Position: incompatiblePos, Title: "Wrong Job/Org"},
			{Position: atCapacityPos, Title: "No Vacancy Left"},
		},
	}
	candidates, err := positionpicker.ResolveCandidates(ctx, reader, req)
	if err != nil {
		t.Fatalf("ResolveCandidates: %v", err)
	}
	if len(candidates) != 1 {
		t.Fatalf("candidates = %+v, want exactly one", candidates)
	}
	got := candidates[0]
	if got.Position != authorizedPos {
		t.Fatalf("candidate position = %v, want %v", got.Position, authorizedPos)
	}
	if got.Title != "Engineering Manager" || got.Manager != "Jane Smith" || got.Location != "Remote" || got.Organization != "Engineering" {
		t.Fatalf("candidate display fields = %+v, want the directory entry's own labels", got)
	}
	if got.ReservationState != positionpicker.ReservationAvailable {
		t.Fatalf("ReservationState = %q, want %q", got.ReservationState, positionpicker.ReservationAvailable)
	}
	if got.Reference == "" {
		t.Fatal("candidate must carry a non-empty revision reference")
	}
	decodedPos, _, err := got.Reference.Decode()
	if err != nil {
		t.Fatalf("decode candidate reference: %v", err)
	}
	if decodedPos != authorizedPos {
		t.Fatalf("decoded reference position = %v, want %v", decodedPos, authorizedPos)
	}
}

// TestResolveCandidatesZeroValueFailsClosed proves an invalid request (no
// tenant, no bitemporal coordinate) is refused rather than silently
// returning an empty or unfiltered candidate list.
func TestResolveCandidatesZeroValueFailsClosed(t *testing.T) {
	if _, err := positionpicker.ResolveCandidates(context.Background(), pickerFakeReader{}, positionpicker.Request{}); !errors.Is(err, positionpicker.ErrInvalidRequest) {
		t.Fatalf("ResolveCandidates(zero value) = %v, want ErrInvalidRequest", err)
	}
	if _, err := positionpicker.ResolveCandidates(context.Background(), nil, positionpicker.Request{Tenant: pickerTenant(), AsOf: pickerAsOf(t)}); !errors.Is(err, positionpicker.ErrInvalidRequest) {
		t.Fatalf("ResolveCandidates(nil reader) = %v, want ErrInvalidRequest", err)
	}
}

// TestResolveCandidatesRejectsCrossTenantDirectoryEntries proves a directory
// entry naming a position outside the requested tenant is refused as a
// caller error rather than silently checked against the wrong tenant.
func TestResolveCandidatesRejectsCrossTenantDirectoryEntries(t *testing.T) {
	otherTenantPos := values.EntityRef{Tenant: values.TenantId("some-other-tenant"), Kind: position.KindPosition, Id: "11111111-1111-4111-8111-111111111111"}
	_, err := positionpicker.ResolveCandidates(context.Background(), pickerFakeReader{}, positionpicker.Request{
		Tenant: pickerTenant(), AsOf: pickerAsOf(t),
		Directory: []positionpicker.DirectoryEntry{{Position: otherTenantPos}},
	})
	if !errors.Is(err, positionpicker.ErrInvalidRequest) {
		t.Fatalf("ResolveCandidates(cross-tenant entry) = %v, want ErrInvalidRequest", err)
	}
}

// TestResolveCandidatesDisclosesVacancyEnd proves a position whose vacancy
// is known to end (a successor is not yet lined up) still discloses that
// window rather than reporting it as unconditionally open-ended.
func TestResolveCandidatesDisclosesVacancyEnd(t *testing.T) {
	ctx := context.Background()
	posID := "66666666-6666-4666-8666-666666666666"
	pos := pickerPosition(t, posID)
	rev := pickerRevision(t, pos, "ENG-MGR", "ENGINEERING", 1)
	reader := pickerFakeReader{byID: map[string]position.PositionRevision{posID: rev}}

	incumbent := values.EntityRef{Tenant: pickerTenant(), Kind: position.KindWorker, Id: "77777777-7777-4777-8777-777777777777"}
	// A partial (non-exclusive) occupant leaves some FTE and every head
	// free, so the position still passes the vacancy ground while still
	// disclosing a known vacancy end -- proving the two are independent
	// facts, not a single "vacant or not" bit.
	fte, err := values.NewDecimal("0.5000", 4, values.RoundingExactRequired)
	if err != nil {
		t.Fatalf("decimal: %v", err)
	}
	endDate := pickerDate(t, "2027-03-01")
	interval, err := values.NewLocalDateInterval(pickerDate(t, "2020-01-01"), endDate, pickerCalendar)
	if err != nil {
		t.Fatalf("interval: %v", err)
	}

	candidates, err := positionpicker.ResolveCandidates(ctx, reader, positionpicker.Request{
		Tenant: pickerTenant(), AsOf: position.AsOf{EffectiveOn: pickerDate(t, "2027-02-01"), KnownAt: pickerKnownAt(t, "2027-01-15T00:00:00Z")},
		DesiredJobCode: "ENG-MGR", DesiredOrgUnit: "ENGINEERING",
		Occupants: []position.Occupant{{Worker: incumbent, FTE: fte, Exclusive: false, Effective: interval}},
		Directory: []positionpicker.DirectoryEntry{{Position: pos, Title: "Engineering Manager"}},
	})
	if err != nil {
		t.Fatalf("ResolveCandidates: %v", err)
	}
	if len(candidates) != 1 {
		t.Fatalf("candidates = %+v, want exactly one", candidates)
	}
	if !candidates[0].HasVacancyEnd {
		t.Fatal("HasVacancyEnd = false, want true: the incumbent's coverage ends with no successor lined up")
	}
	if candidates[0].VacancyEnd.Compare(endDate) != 0 {
		t.Fatalf("VacancyEnd = %s, want %s", candidates[0].VacancyEnd, endDate)
	}
}
