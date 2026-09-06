package snapshot_test

import (
	"errors"
	"testing"

	"github.com/monstercameron/hcm-next/internal/domains/people"
	"github.com/monstercameron/hcm-next/internal/domains/position"
	promosnapshot "github.com/monstercameron/hcm-next/internal/domains/promotion/snapshot"
	"github.com/monstercameron/hcm-next/internal/domains/rewards"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

// TestBuildRefusesAMalformedRequest walks every contract the request states on
// its own terms. Each case is a promise a caller could otherwise break
// silently: a subject in the wrong tenant, a target with no pay zone (which
// would resolve a band for a different job), an unpinned reference version.
func TestBuildRefusesAMalformedRequest(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		mutate func(*promosnapshot.Request)
	}{
		{"no tenant", func(r *promosnapshot.Request) { r.Tenant = "" }},
		{"a subject from another tenant", func(r *promosnapshot.Request) { r.Subject.Tenant = "other-tenant" }},
		{"a subject that is not a worker", func(r *promosnapshot.Request) { r.Subject.Kind = position.KindPosition }},
		{"a target that is not a position", func(r *promosnapshot.Request) { r.TargetPosition.Kind = people.KindWorker }},
		{"no target job code", func(r *promosnapshot.Request) { r.Target.JobCode = "" }},
		{"no target grade", func(r *promosnapshot.Request) { r.Target.Grade = "" }},
		{"no target organizational unit", func(r *promosnapshot.Request) { r.Target.OrgUnit = "" }},
		{"no target pay zone", func(r *promosnapshot.Request) { r.Target.PayZone = "" }},
		{"no desired base pay", func(r *promosnapshot.Request) { r.DesiredBasePay = values.Money{} }},
		{"no desired pay basis", func(r *promosnapshot.Request) { r.DesiredPayBasis = rewards.PayBasisUnspecified }},
		{"no effective date", func(r *promosnapshot.Request) { r.EffectiveOn = values.LocalDate{} }},
		{"no known-at", func(r *promosnapshot.Request) { r.KnownAt = values.KnownAt{} }},
		{"no calendar", func(r *promosnapshot.Request) { r.Calendar = values.CalendarRef{} }},
		{"a non-positive manager chain depth", func(r *promosnapshot.Request) { r.ManagerChainDepth = 0 }},
		{"no annualization rules", func(r *promosnapshot.Request) {
			r.Annualization = rewards.CompensationAnnualizationRule{}
		}},
		{"no budget scope", func(r *promosnapshot.Request) { r.BudgetScope = "" }},
		{"no budget period", func(r *promosnapshot.Request) { r.BudgetPeriod = "" }},
		{"no reference version", func(r *promosnapshot.Request) { r.ReferenceVersion = "" }},
		{"no source connection", func(r *promosnapshot.Request) { r.SourceConnection = "" }},
		{"no manager hop authorizer", func(r *promosnapshot.Request) { r.Authorization.ManagerHop = nil }},
		{"a withheld budget with no reason", func(r *promosnapshot.Request) {
			r.Authorization.BudgetDisclosable = false
			r.Authorization.BudgetDenialReason = ""
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			h := newHarness(t)
			req := fixtureRequest(t)
			tc.mutate(&req)
			_, err := promosnapshot.Build(t.Context(), h.readers(), req)
			if !errors.Is(err, promosnapshot.ErrRequestInvalid) {
				t.Fatalf("Build returned %v, want ErrRequestInvalid", err)
			}
		})
	}
}

// TestBuildRefusesAnUnconfiguredReader proves the reader check runs before the
// request is even parsed: a build with no ports could otherwise report a
// missing input rather than a missing wiring.
func TestBuildRefusesAnUnconfiguredReader(t *testing.T) {
	t.Parallel()
	_, err := promosnapshot.Build(t.Context(), promosnapshot.Readers{}, fixtureRequest(t))
	if !errors.Is(err, promosnapshot.ErrReaderMissing) {
		t.Fatalf("Build returned %v, want ErrReaderMissing", err)
	}
}

// TestBuildPropagatesAReadFailureAsAFailureNotAnAbsence is the distinction the
// whole three-valued design rests on: a port that broke is not a record that
// says nothing, and turning the first into the second is how a promotion gets
// proposed against facts nobody actually read.
func TestBuildPropagatesAReadFailureAsAFailureNotAnAbsence(t *testing.T) {
	t.Parallel()
	broken := errors.New("the read port is unavailable")
	for _, tc := range []struct {
		name    string
		arrange func(*harness)
	}{
		{"organization", func(h *harness) { h.Org.failure = broken }},
		{"position", func(h *harness) { h.Position.failure = broken }},
		{"compensation", func(h *harness) { h.Compensation.failure = broken }},
		{"budget", func(h *harness) { h.Budget.failure = broken }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			h := newHarness(t)
			tc.arrange(h)
			_, err := promosnapshot.Build(t.Context(), h.readers(), fixtureRequest(t))
			if !errors.Is(err, promosnapshot.ErrReadFailed) {
				t.Fatalf("Build returned %v, want ErrReadFailed", err)
			}
			if errors.Is(err, promosnapshot.ErrInputUnavailable) {
				t.Fatal("a broken read port was reported as an unavailable input")
			}
		})
	}
}

// TestBuildWithholdsAPositionTheCallerMayNotSee proves the position
// authorizer's refusal reaches the snapshot as a WITHHELD input rather than as
// an error the caller could not tell from an outage.
func TestBuildWithholdsAPositionTheCallerMayNotSee(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	req := fixtureRequest(t)
	req.Authorization.Position = func(position.PositionRevision) bool { return false }

	snap, err := promosnapshot.Build(t.Context(), h.readers(), req)
	if err == nil {
		t.Fatal("a build for a caller who may not see the target position was accepted")
	}
	for _, name := range []string{
		promosnapshot.InputTargetPositionCapacity,
		promosnapshot.InputTargetPositionVacancy,
	} {
		in, ok := snap.Lookup(name)
		if !ok {
			t.Fatalf("input %s was dropped rather than withheld", name)
		}
		if in.Availability != promosnapshot.AvailabilityWithheld {
			t.Fatalf("input %s is %s, want WITHHELD", name, in.Availability)
		}
	}
	if got := promosnapshot.InputNameOf(err); got != promosnapshot.InputTargetPositionCapacity {
		t.Fatalf("the refusal names %q, want the capacity input (%v)", got, err)
	}
}

// TestBuildBindsAVacancyDateWhenTheRecordHasOne proves the vacancy input is a
// real read rather than a placeholder: an occupant whose placement ends leaves
// a vacancy date, and the snapshot binds it.
func TestBuildBindsAVacancyDateWhenTheRecordHasOne(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	req := fixtureRequest(t)

	interval, err := values.NewLocalDateInterval(
		mustLocalDate(t, "2026-01-01"), mustLocalDate(t, "2026-09-01"), mustCalendar(t))
	if err != nil {
		t.Fatalf("NewLocalDateInterval: %v", err)
	}
	req.Occupants = []position.Occupant{{
		Worker:    mustWorkerRef(t),
		FTE:       mustDecimal(t, "1.0000", 4),
		Effective: interval,
		Exclusive: true,
	}}

	snap, buildErr := promosnapshot.Build(t.Context(), h.readers(), req)
	if buildErr != nil {
		t.Fatalf("Build: %v", buildErr)
	}
	vacancy, ok := snap.Disclosed(promosnapshot.InputTargetPositionVacancy)
	if !ok {
		t.Fatalf("the vacancy input is not disclosed:\n%s", snap.Explain())
	}
	if vacancy != "2026-09-01" {
		t.Fatalf("the vacancy date is %q, want 2026-09-01", vacancy)
	}
	capacity, _ := snap.Disclosed(promosnapshot.InputTargetPositionCapacity)
	if capacity != promosnapshot.CapacityExhausted {
		t.Fatalf("a fully occupied position reports %q, want %q", capacity, promosnapshot.CapacityExhausted)
	}
}

// TestBuildAbsentsBothBandInputsWhenTheCatalogPublishesNoBand proves a missing
// band is an absence of two inputs rather than a silent in-band verdict.
func TestBuildAbsentsBothBandInputsWhenTheCatalogPublishesNoBand(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	req := fixtureRequest(t)
	req.Target.Grade = "P9"

	snap, err := promosnapshot.Build(t.Context(), h.readers(), req)
	if err == nil {
		t.Fatal("a promotion into a grade the catalog does not band was accepted")
	}
	for _, name := range []string{
		promosnapshot.InputPayBandPositionCurrent,
		promosnapshot.InputPayBandPositionDesired,
	} {
		in, ok := snap.Lookup(name)
		if !ok || in.Availability != promosnapshot.AvailabilityAbsent {
			t.Fatalf("input %s is %s, want ABSENT", name, in.Availability)
		}
		if in.CanonicalText != "" {
			t.Fatalf("an ABSENT band input carries the value %q", in.CanonicalText)
		}
	}
}
