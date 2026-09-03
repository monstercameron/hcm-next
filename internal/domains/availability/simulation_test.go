package availability

import (
	"errors"
	"testing"

	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

func simulationSchedule(t *testing.T, shifts ...WorkShift) WorkSchedule {
	t.Helper()
	tenant := values.TenantId("acme")
	ref := func(kind values.Kind, id string) values.EntityRef {
		return values.EntityRef{Tenant: tenant, Kind: kind, Id: id}
	}
	rev, err := values.NewSequenceRevision("schedule/assignment-1", 7)
	if err != nil {
		t.Fatal(err)
	}
	return WorkSchedule{ScheduleID: ref("schedule", "00000000-0000-4000-8000-000000000001"), Assignment: ref(KindAssignment, "00000000-0000-4000-8000-000000000002"), Revision: rev, Calendar: values.CalendarRef{Ref: "us-federal", Version: "2026"}, Timezone: values.ZoneRef{ID: "America/New_York", TzdbVersion: "2026a"}, Shifts: shifts}
}

func simulationShift(t *testing.T, id, date, start, end string) WorkShift {
	t.Helper()
	d, err := values.ParseLocalDate(date)
	if err != nil {
		t.Fatal(err)
	}
	s, err := values.ParseLocalTime(start)
	if err != nil {
		t.Fatal(err)
	}
	e, err := values.ParseLocalTime(end)
	if err != nil {
		t.Fatal(err)
	}
	return WorkShift{ID: id, Date: d, Start: s, End: e, Disambiguation: values.DisambiguationRejectGap}
}

func simulationRequest(t *testing.T, schedule WorkSchedule) AbsenceSimulationRequest {
	t.Helper()
	tenant := values.TenantId("acme")
	worker := values.EntityRef{Tenant: tenant, Kind: KindWorker, Id: "00000000-0000-4000-8000-000000000003"}
	employment := values.EntityRef{Tenant: tenant, Kind: KindEmployment, Id: "00000000-0000-4000-8000-000000000004"}
	rev, err := values.NewSequenceRevision("absence/1", 1)
	if err != nil {
		t.Fatal(err)
	}
	impact := AbsenceImpact{ImpactID: values.EntityRef{Tenant: tenant, Kind: "absence_impact", Id: "00000000-0000-4000-8000-000000000005"}, Worker: worker, Employment: employment, Assignment: schedule.Assignment, RequestedInterval: mustInterval(t, "2026-03-08", "2026-03-10"), Approval: AbsenceApproved, ApprovedInterval: mustInterval(t, "2026-03-08", "2026-03-10"), Reason: "leave", ScheduleSource: Source{Authority: schedule.ScheduleID, Revision: rev, TimezoneID: schedule.Timezone.ID, CalendarRef: schedule.Calendar.String()}}
	return AbsenceSimulationRequest{Worker: worker, Employment: employment, Absence: impact, Schedule: schedule}
}

// TestTodo_AVAIL_002 is the primary exact matrix for zero-effect schedule simulation.
func TestTodo_AVAIL_002(t *testing.T) {
	for _, tc := range []struct {
		name         string
		shift        WorkShift
		wantHours    float64
		wantConflict string
	}{
		{"ordinary", simulationShift(t, "s1", "2026-03-09", "09:00:00", "17:00:00"), 8, ""},
		{"dst-gap", simulationShift(t, "s2", "2026-03-08", "02:30:00", "04:00:00"), 0, "DST_OR_TIME_ERROR"},
		{"unscheduled-day", simulationShift(t, "s3", "2026-03-11", "09:00:00", "17:00:00"), 0, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, err := SimulateAbsence(simulationRequest(t, simulationSchedule(t, tc.shift)))
			if err != nil {
				t.Fatal(err)
			}
			if r.Hours != tc.wantHours {
				t.Fatalf("hours=%v want %v", r.Hours, tc.wantHours)
			}
			if tc.wantConflict != "" && (len(r.Conflicts) == 0 || r.Conflicts[0].Code != tc.wantConflict) {
				t.Fatalf("conflicts=%+v", r.Conflicts)
			}
			if tc.wantConflict == "" && len(r.Conflicts) != 0 {
				t.Fatalf("unexpected conflicts=%+v", r.Conflicts)
			}
		})
	}
}

func TestTodo_AVAIL_002_Conformance(t *testing.T) {
	s := simulationSchedule(t, simulationShift(t, "s1", "2026-03-09", "09:00:00", "17:00:00"))
	r := simulationRequest(t, s)
	expected := s.Revision
	r.ExpectedScheduleRevision = expected
	if _, err := SimulateAbsence(r); err != nil {
		t.Fatal(err)
	}
	other, _ := values.NewSequenceRevision("schedule/assignment-1", 8)
	r.ExpectedScheduleRevision = other
	if _, err := SimulateAbsence(r); !errors.Is(err, ErrStaleSchedule) {
		t.Fatalf("err=%v", err)
	}
}

func TestTodo_AVAIL_002_Golden(t *testing.T) {
	s := simulationSchedule(t, simulationShift(t, "s1", "2026-03-09", "09:00:00", "17:00:00"))
	r, err := SimulateAbsence(simulationRequest(t, s))
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Intervals) != 1 || r.Intervals[0].ShiftID != "s1" || r.Coverage != CoverageDemandCreated {
		t.Fatalf("result=%+v", r)
	}
}

func TestTodo_AVAIL_002_Property(t *testing.T) {
	s := simulationSchedule(t, simulationShift(t, "s1", "2026-03-09", "09:00:00", "17:00:00"))
	req := simulationRequest(t, s)
	a, _ := SimulateAbsence(req)
	b, _ := SimulateAbsence(req)
	if a.Hours != b.Hours || len(a.Intervals) != len(b.Intervals) {
		t.Fatal("simulation is not deterministic")
	}
	if len(s.Shifts) != 1 {
		t.Fatal("simulation mutated schedule")
	}
}
