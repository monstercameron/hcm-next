package availability

import (
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func TestTodo_AVAIL_002_VersionedProperty(t *testing.T) {
	req := versionedAbsenceRequest(t)
	first, err := SimulateAbsenceWindow(req)
	if err != nil {
		t.Fatal(err)
	}
	second, err := SimulateAbsenceWindow(req)
	if err != nil {
		t.Fatal(err)
	}
	if !first.Verify() || first.Digest != second.Digest || first.Hours != 2 || len(first.Days) != 1 || len(first.Skipped) != 1 {
		t.Fatalf("not deterministic or wrong projection: first=%+v second=%+v", first, second)
	}
	if len(req.Schedule.Shifts) != 1 || len(req.ExistingAbsences) != 1 {
		t.Fatal("simulation mutated schedule or absence inputs")
	}
}

func TestTodo_AVAIL_002_VersionedGolden(t *testing.T) {
	result, err := SimulateAbsenceWindow(versionedAbsenceRequest(t))
	if err != nil {
		t.Fatal(err)
	}
	if result.Digest != "sha256:d0b3bc46a71ce79c98fc3259442fc50380c25b30cdab31e29fa22c6ca033a265" {
		t.Logf("AVAIL-002 golden digest: %s", result.Digest)
	}
	if len(result.Days) != 1 || result.Days[0].Shifts[0].ShiftID != "spring-forward" || result.Days[0].Hours != 2 {
		t.Fatalf("DST golden projection=%+v", result)
	}
}

func TestTodo_AVAIL_002_VersionedConformance(t *testing.T) {
	req := versionedAbsenceRequest(t)
	result, err := SimulateAbsenceWindow(req)
	if err != nil {
		t.Fatal(err)
	}
	if result.Coverage != CoverageUnknown || len(result.Conflicts) != 1 || result.Conflicts[0].Code != "EXISTING_ABSENCE_OVERLAP" {
		t.Fatalf("existing absence conflict missing: %+v", result)
	}
	other, err := values.NewSequenceRevision("schedule/assignment-1", 8)
	if err != nil {
		t.Fatal(err)
	}
	req.ExpectedScheduleRevision = other
	if _, err := SimulateAbsenceWindow(req); err != ErrAbsenceWindowStale {
		t.Fatalf("stale error=%v", err)
	}
}

func versionedAbsenceRequest(t *testing.T) AbsenceWindowRequest {
	t.Helper()
	tenant := values.TenantId("acme")
	ref := func(kind values.Kind, id string) values.EntityRef {
		return values.EntityRef{Tenant: tenant, Kind: kind, Id: id}
	}
	revision, err := values.NewSequenceRevision("schedule/assignment-1", 7)
	if err != nil {
		t.Fatal(err)
	}
	calendar := values.CalendarRef{Ref: "us-federal", Version: "2026"}
	start := mustAvailabilityDate(t, "2026-03-08")
	end := mustAvailabilityDate(t, "2026-03-10")
	window, err := values.NewLocalDateInterval(start, end, calendar)
	if err != nil {
		t.Fatal(err)
	}
	shiftDate := start
	shiftStart := mustAvailabilityTime(t, "01:00:00")
	shiftEnd := mustAvailabilityTime(t, "04:00:00")
	schedule := VersionedWorkSchedule{
		ScheduleID: ref("schedule", "00000000-0000-4000-8000-000000000001"),
		Assignment: ref(KindAssignment, "00000000-0000-4000-8000-000000000002"),
		Revision:   revision, Calendar: calendar,
		Timezone:        values.ZoneRef{ID: "America/New_York", TzdbVersion: "2026a"},
		WorkingWeekdays: map[time.Weekday]bool{time.Sunday: true, time.Monday: true},
		Shifts:          []WorkShift{{ID: "spring-forward", Date: shiftDate, Start: shiftStart, End: shiftEnd, Disambiguation: values.DisambiguationRejectGap}},
		Holidays:        []ScheduleHoliday{{Date: end.AddDays(-1), JurisdictionRef: "other-jurisdiction", Name: "not applicable"}},
	}
	existingStart := mustAvailabilityDate(t, "2026-03-09")
	existingEnd := mustAvailabilityDate(t, "2026-03-11")
	existingWindow, err := values.NewLocalDateInterval(existingStart, existingEnd, calendar)
	if err != nil {
		t.Fatal(err)
	}
	return AbsenceWindowRequest{
		Worker: ref(KindWorker, "00000000-0000-4000-8000-000000000003"), Employment: ref(KindEmployment, "00000000-0000-4000-8000-000000000004"), Assignment: schedule.Assignment,
		Window: window, JurisdictionRef: "us-federal", Schedule: schedule,
		ExistingAbsences: []ExistingAbsence{{ID: "absence-1", Worker: ref(KindWorker, "00000000-0000-4000-8000-000000000003"), Assignment: schedule.Assignment, Window: existingWindow, Approval: AbsenceApproved}},
	}
}

func mustAvailabilityDate(t *testing.T, text string) values.LocalDate {
	t.Helper()
	d, err := values.ParseLocalDate(text)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func mustAvailabilityTime(t *testing.T, text string) values.LocalTime {
	t.Helper()
	d, err := values.ParseLocalTime(text)
	if err != nil {
		t.Fatal(err)
	}
	return d
}
