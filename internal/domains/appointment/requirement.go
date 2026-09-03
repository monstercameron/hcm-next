package appointment

import (
	"fmt"
	"strings"
	"time"
)

type ParticipantRole struct {
	Role     string
	Required bool
	Min      int
	Max      int
}
type TimeWindow struct {
	Start, End time.Time
	TimeZone   string
}
type Location struct {
	Mode    string
	Address string
	Channel string
}
type CancellationPolicy struct {
	Notice       time.Duration
	FeePolicy    string
	NoShowPolicy string
}
type ExternalCalendarPolicy struct {
	Mode             string
	Provider         string
	DetailDisclosure string
}

// Requirement is the complete appointment contract. All fields are explicit;
// an omitted policy is not interpreted as an implicit permissive default.
type Requirement struct {
	ID               string
	Version          string
	Purpose          string
	Participants     []ParticipantRole
	Duration         time.Duration
	Window           TimeWindow
	Location         Location
	Resources        []ResourceType
	Qualification    string
	PrivacyClass     string
	Cancellation     CancellationPolicy
	ExternalCalendar ExternalCalendarPolicy
	State            string
}

// AppointmentRequirement is retained as the descriptive API name.
type AppointmentRequirement = Requirement

func (r Requirement) Validate() error {
	state := r.State
	if state == "" {
		state = "DRAFT"
	}
	required := []struct{ field, value string }{{"id", r.ID}, {"version", r.Version}, {"purpose", r.Purpose}, {"qualification", r.Qualification}, {"privacy_class", r.PrivacyClass}, {"location.mode", r.Location.Mode}, {"location.channel", r.Location.Channel}, {"cancellation.fee_policy", r.Cancellation.FeePolicy}, {"cancellation.no_show_policy", r.Cancellation.NoShowPolicy}, {"external_calendar.mode", r.ExternalCalendar.Mode}, {"external_calendar.detail_disclosure", r.ExternalCalendar.DetailDisclosure}}
	for _, v := range required {
		if strings.TrimSpace(v.value) == "" {
			return reject(v.field, state, r.Version, "is required")
		}
	}
	if len(r.Participants) == 0 {
		return reject("participants", state, r.Version, "at least one participant role is required")
	}
	seen := map[string]bool{}
	for i, p := range r.Participants {
		if strings.TrimSpace(p.Role) == "" {
			return reject(fmt.Sprintf("participants[%d].role", i), state, r.Version, "is required")
		}
		k := strings.ToLower(strings.TrimSpace(p.Role))
		if seen[k] {
			return reject(fmt.Sprintf("participants[%d].role", i), state, r.Version, "duplicates another role")
		}
		seen[k] = true
		if p.Min < 0 || p.Max < 0 || (p.Max > 0 && p.Min > p.Max) {
			return reject(fmt.Sprintf("participants[%d]", i), state, r.Version, "has invalid cardinality")
		}
	}
	if r.Duration <= 0 {
		return reject("duration", state, r.Version, "must be positive")
	}
	if r.Window.Start.IsZero() {
		return reject("window.start", state, r.Version, "is required")
	}
	if r.Window.End.IsZero() {
		return reject("window.end", state, r.Version, "is required")
	}
	if !r.Window.End.After(r.Window.Start) {
		return reject("window", state, r.Version, "end must be after start")
	}
	if strings.TrimSpace(r.Window.TimeZone) == "" {
		return reject("window.time_zone", state, r.Version, "is required")
	}
	if r.Location.Mode == "IN_PERSON" && strings.TrimSpace(r.Location.Address) == "" {
		return reject("location.address", state, r.Version, "is required for IN_PERSON")
	}
	if r.Cancellation.Notice < 0 {
		return reject("cancellation.notice", state, r.Version, "must not be negative")
	}
	if strings.TrimSpace(r.ExternalCalendar.Mode) != "DISABLED" && strings.TrimSpace(r.ExternalCalendar.Provider) == "" {
		return reject("external_calendar.provider", state, r.Version, "is required when calendar integration is enabled")
	}
	if err := ResourceTypes(r.Resources); err != nil {
		if x, ok := err.(*Rejection); ok {
			x.State = state
			x.Version = r.Version
		}
		return err
	}
	return nil
}

type Publication struct{ Requirement Requirement }

func Publish(r Requirement) (Publication, error) {
	if err := r.Validate(); err != nil {
		return Publication{}, fmt.Errorf("%w: %w", ErrPublicationRejected, err)
	}
	c := r
	c.Participants = append([]ParticipantRole(nil), r.Participants...)
	c.Resources = append([]ResourceType(nil), r.Resources...)
	return Publication{Requirement: c}, nil
}
