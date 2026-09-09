package appointment

import (
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func validRequirement() Requirement {
	start := time.Date(2026, 9, 10, 9, 0, 0, 0, time.UTC)
	return Requirement{
		ID: "interview", Version: "v1", Purpose: "candidate interview",
		Participants: []ParticipantRole{{Role: "candidate", Required: true, Min: 1, Max: 1}, {Role: "interviewer", Required: true, Min: 1, Max: 2}},
		Duration:     30 * time.Minute, Window: TimeWindow{Start: start, End: start.Add(time.Hour), TimeZone: "America/New_York"},
		Location: Location{Mode: "VIRTUAL", Channel: "video"}, Qualification: "interviewers:engineering", PrivacyClass: "candidate-confidential",
		Resources:        []ResourceType{{ID: "room-video", Version: "v1", Name: "video room", Kind: ResourceCapability, Qualification: "video", PrivacyClass: "candidate-confidential", Capacity: 1}},
		Cancellation:     CancellationPolicy{Notice: time.Hour, FeePolicy: "none", NoShowPolicy: "follow-up"},
		ExternalCalendar: ExternalCalendarPolicy{Mode: "DISABLED", DetailDisclosure: "free_busy"},
	}
}

func TestTodo_APPT_001(t *testing.T) {
	if _, err := Publish(validRequirement()); err != nil {
		t.Fatalf("valid requirement rejected: %v", err)
	}
	cases := []struct {
		name, field string
		mutate      func(*Requirement)
	}{
		{"time", "duration", func(r *Requirement) { r.Duration = 0 }},
		{"window", "window.start", func(r *Requirement) { r.Window.Start = time.Time{} }},
		{"resource", "resources", func(r *Requirement) { r.Resources = nil }},
		{"purpose", "purpose", func(r *Requirement) { r.Purpose = "" }},
		{"privacy", "privacy_class", func(r *Requirement) { r.PrivacyClass = "" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := validRequirement()
			tc.mutate(&r)
			_, err := Publish(r)
			if err == nil {
				t.Fatal("accepted invalid contract")
			}
			var rej *Rejection
			if !errors.As(err, &rej) || rej.Code != "APPT_001_REJECTED" || rej.Field != tc.field || rej.Version != "v1" {
				t.Fatalf("rejection = %v, want code/field/version %s", err, tc.field)
			}
		})
	}
}

func TestTodo_APPT_001_Security(t *testing.T) {
	r := validRequirement()
	r.ExternalCalendar = ExternalCalendarPolicy{Mode: "ENABLED", DetailDisclosure: "free_busy"}
	if err := r.Validate(); err == nil {
		t.Fatal("enabled calendar without provider accepted")
	}
}
func TestTodo_APPT_001_Conformance(t *testing.T) {
	p, err := Publish(validRequirement())
	if err != nil {
		t.Fatal(err)
	}
	p.Requirement.Participants[0].Role = "mutated"
	if validRequirement().Participants[0].Role == "mutated" {
		t.Fatal("fixture alias")
	}
}
func TestTodo_APPT_001_Mutation(t *testing.T) {
	r := validRequirement()
	r.Location.Mode = "IN_PERSON"
	r.Location.Address = ""
	if err := r.Validate(); err == nil {
		t.Fatal("in-person location without address accepted")
	}
}

func typedAppointmentRef(kind values.Kind, suffix string) values.EntityRef {
	return values.EntityRef{Tenant: "tenant-a", Kind: kind, Id: "00000000-0000-4000-8000-000000000" + suffix}
}

func typedAppointmentWindow(t *testing.T) values.EffectiveInterval {
	t.Helper()
	start := values.NewInstant(time.Date(2026, 9, 10, 9, 0, 0, 0, time.UTC))
	end := values.NewInstant(time.Date(2026, 9, 10, 10, 0, 0, 0, time.UTC))
	window, err := values.NewInstantInterval(start, end)
	if err != nil {
		t.Fatal(err)
	}
	return window
}

func typedAppointmentRequirement(t *testing.T) Requirement {
	t.Helper()
	quantity, err := values.NewQuantity("2", "EACH", 0, values.RoundingHalfEven)
	if err != nil {
		t.Fatal(err)
	}
	return Requirement{
		ID: "typed-appointment", Version: "v1", Revision: 1, PurposeKind: PurposeInterview,
		ParticipantRefs: []values.EntityRef{typedAppointmentRef("candidate", "001"), typedAppointmentRef("interviewer", "002")},
		Duration:        30 * time.Minute, WindowInterval: typedAppointmentWindow(t), LocationClass: LocationVirtual,
		RequiredResources: []ResourceRequirement{{ResourceTypeRef: typedAppointmentRef("resource_type", "003"), Quantity: quantity}},
		QualificationRefs: []values.EntityRef{typedAppointmentRef("qualification", "004")}, PrivacyClass: "candidate-confidential",
		CancellationRules: CancellationRules{Kind: CancellationAllowed, NoShow: NoShowReview}, LeadTime: time.Hour,
	}
}

func TestTodo_APPT_001_TypedContract(t *testing.T) {
	requirement, err := NewAppointmentRequirement(typedAppointmentRequirement(t))
	if err != nil {
		t.Fatal(err)
	}
	if requirement.CanonicalDigest == "" || requirement.Canonical() == nil {
		t.Fatal("typed requirement was not canonically digested")
	}
	if _, err := Explain(requirement); err != nil {
		t.Fatal(err)
	}
	resource, err := NewResourceType(ResourceType{ID: "video-room", Version: "v1", Name: "video room", Kind: ResourceCapability, Qualification: "video", PrivacyClass: "candidate-confidential", CapacityMode: CapacityExclusive})
	if err != nil || resource.CanonicalDigest == "" {
		t.Fatalf("resource = %+v, err = %v", resource, err)
	}
}

func TestTodo_APPT_001_Feasibility(t *testing.T) {
	requirement, err := NewAppointmentRequirement(typedAppointmentRequirement(t))
	if err != nil {
		t.Fatal(err)
	}
	availability := []ResourceAvailability{
		{ResourceRef: typedAppointmentRef("resource", "005"), ResourceTypeRef: typedAppointmentRef("resource_type", "003"), Window: typedAppointmentWindow(t), Capacity: 1, CapacityMode: CapacityExclusive},
		{ResourceRef: typedAppointmentRef("resource", "006"), ResourceTypeRef: typedAppointmentRef("resource_type", "003"), Window: typedAppointmentWindow(t), Capacity: 1, CapacityMode: CapacityExclusive},
	}
	result, err := CheckFeasibility(requirement, availability)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Feasible || len(result.Shortfalls) != 0 || result.CanonicalDigest == "" {
		t.Fatalf("feasibility = %+v", result)
	}
	result, err = CheckFeasibility(requirement, availability[:1])
	if err != nil {
		t.Fatal(err)
	}
	if result.Feasible || len(result.Shortfalls) != 1 || result.Shortfalls[0].Reason != ShortfallCapacity {
		t.Fatalf("shortfall = %+v", result.Shortfalls)
	}
}
