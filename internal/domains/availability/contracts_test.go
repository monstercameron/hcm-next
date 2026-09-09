package availability

import (
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
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

// TestTodo_AVAIL_001 is the primary conformance example for the shared
// effective-dated availability contract.  In particular, availability is a
// fact with its own revision and provenance; it is not an employment status.
func TestTodo_AVAIL_001(t *testing.T) {
	a := validWorkerAvailability(t, WorkerScope, Available)
	if err := a.Validate(); err != nil {
		t.Fatalf("valid worker availability rejected: %v", err)
	}
	if a.Employment.Kind != KindEmployment || a.Source.Revision == (values.RevisionToken{}) {
		t.Fatal("availability must preserve employment identity and source revision")
	}
}

func TestTodo_AVAIL_001_Property(t *testing.T) {
	for _, state := range []State{Available, Unavailable, Restricted, Unknown} {
		t.Run(string(state), func(t *testing.T) {
			if err := validWorkerAvailability(t, WorkerScope, state).Validate(); err != nil {
				t.Fatalf("state %s rejected: %v", state, err)
			}
		})
	}
}

func TestTodo_AVAIL_001_Conformance(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*WorkerAvailability)
	}{
		{"assignment_scope_requires_assignment", func(a *WorkerAvailability) { a.Scope = AssignmentScope }},
		{"worker_scope_cannot_smuggle_assignment", func(a *WorkerAvailability) {
			a.Assignment = values.EntityRef{Tenant: a.Worker.Tenant, Kind: KindAssignment, Id: "00000000-0000-4000-8000-000000000099"}
		}},
		{"identity_tenant_isolation", func(a *WorkerAvailability) { a.Employment.Tenant = "other" }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			a := validWorkerAvailability(t, WorkerScope, Available)
			tc.mutate(&a)
			if err := a.Validate(); err == nil {
				t.Fatal("invalid availability contract accepted")
			}
		})
	}
}

func TestTodo_AVAIL_001_Mutation(t *testing.T) {
	a := validWorkerAvailability(t, WorkerScope, Available)
	a.State = State("ACTIVE")
	if err := a.Validate(); err == nil {
		t.Fatal("employment-like state accepted as availability")
	}
	a = validWorkerAvailability(t, WorkerScope, Available)
	a.Source.TimezoneID = ""
	if err := a.Validate(); err == nil {
		t.Fatal("availability without timezone provenance accepted")
	}
}

func validWorkerAvailability(t *testing.T, scope Scope, state State) WorkerAvailability {
	t.Helper()
	tenant := values.TenantId("acme")
	ref := func(kind values.Kind, id string) values.EntityRef {
		return values.EntityRef{Tenant: tenant, Kind: kind, Id: id}
	}
	rev, err := values.NewSequenceRevision("availability/worker-1", 4)
	if err != nil {
		t.Fatal(err)
	}
	a := WorkerAvailability{
		AvailabilityID: ref("availability", "00000000-0000-4000-8000-000000000001"), Revision: rev,
		Worker: ref(KindWorker, "00000000-0000-4000-8000-000000000002"), Employment: ref(KindEmployment, "00000000-0000-4000-8000-000000000003"),
		Scope: scope, State: state, Reason: "worker supplied availability", Effective: mustInterval(t, "2026-04-01", "2026-04-02"),
		Source: Source{Authority: ref("source", "00000000-0000-4000-8000-000000000004"), Revision: rev, TimezoneID: "America/New_York", CalendarRef: "us-federal@2026"},
	}
	return a
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
