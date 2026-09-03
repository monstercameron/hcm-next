package availability

import (
	"testing"
	"time"

	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

func TestWorkerAvailabilityValidationRequiresTypedIdentityAndSource(t *testing.T) {
	iv := mustInterval(t, "2026-04-01", "2026-04-02")
	rev, err := values.NewSequenceRevision("availability/worker-1", 4)
	if err != nil {
		t.Fatal(err)
	}
	tenant := values.TenantId("acme")
	a := WorkerAvailability{
		AvailabilityID: values.EntityRef{Tenant: tenant, Kind: "availability", Id: "00000000-0000-4000-8000-000000000001"},
		Revision:       rev,
		Worker:         values.EntityRef{Tenant: tenant, Kind: KindWorker, Id: "00000000-0000-4000-8000-000000000002"},
		Employment:     values.EntityRef{Tenant: tenant, Kind: KindEmployment, Id: "00000000-0000-4000-8000-000000000003"},
		Scope:          WorkerScope,
		State:          Restricted,
		Reason:         "medical restriction",
		Effective:      iv,
		Source:         Source{Authority: values.EntityRef{Tenant: tenant, Kind: "source", Id: "00000000-0000-4000-8000-000000000004"}, Revision: rev, TimezoneID: "America/New_York", CalendarRef: "us-federal@2026"},
	}
	if err := a.Validate(); err != nil {
		t.Fatalf("valid availability rejected: %v", err)
	}
	a.State = "ACTIVE"
	if err := a.Validate(); err == nil {
		t.Fatal("unknown state accepted")
	}
}

func TestAbsenceImpactSeparatesRequestedAndApprovedIntervals(t *testing.T) {
	tenant := values.TenantId("acme")
	ref := func(kind values.Kind, id string) values.EntityRef {
		return values.EntityRef{Tenant: tenant, Kind: kind, Id: id}
	}
	requested := mustInterval(t, "2026-05-01", "2026-05-06")
	rev, err := values.NewSequenceRevision("schedule/assignment-1", 9)
	if err != nil {
		t.Fatal(err)
	}
	i := AbsenceImpact{
		ImpactID: ref("absence_impact", "00000000-0000-4000-8000-000000000010"), Worker: ref(KindWorker, "00000000-0000-4000-8000-000000000011"),
		Employment: ref(KindEmployment, "00000000-0000-4000-8000-000000000012"), Assignment: ref(KindAssignment, "00000000-0000-4000-8000-000000000013"),
		RequestedInterval: requested, Approval: AbsenceRequested, Reason: "requested leave",
		ScheduleSource: Source{Authority: ref("schedule", "00000000-0000-4000-8000-000000000014"), Revision: rev, TimezoneID: "America/New_York", CalendarRef: "us-federal@2026"},
	}
	if err := i.Validate(); err != nil {
		t.Fatalf("valid requested absence rejected: %v", err)
	}
	i.ApprovedInterval = requested
	if err := i.Validate(); err == nil {
		t.Fatal("unapproved absence accepted an approved interval")
	}
	i.Approval = AbsenceApproved
	if err := i.Validate(); err != nil {
		t.Fatalf("approved absence rejected: %v", err)
	}
}

func mustInterval(t *testing.T, start, end string) values.EffectiveInterval {
	t.Helper()
	parse := func(s string) values.LocalDate {
		d, err := time.Parse("2006-01-02", s)
		if err != nil {
			t.Fatal(err)
		}
		v, err := values.NewLocalDate(d.Year(), d.Month(), d.Day())
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	iv, err := values.NewLocalDateInterval(parse(start), parse(end), values.CalendarRef{Ref: "us-federal", Version: "2026"})
	if err != nil {
		t.Fatal(err)
	}
	return iv
}
