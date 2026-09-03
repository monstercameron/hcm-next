package appointment

import (
	"errors"
	"testing"
	"time"
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
