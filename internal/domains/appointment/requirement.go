package appointment

import (
	"fmt"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
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
	ID                string
	Version           string
	Revision          uint64
	Purpose           string
	PurposeKind       PurposeKind
	Participants      []ParticipantRole
	ParticipantRefs   []values.EntityRef
	Duration          time.Duration
	Window            TimeWindow
	WindowInterval    values.EffectiveInterval
	Location          Location
	LocationClass     LocationClass
	Resources         []ResourceType
	RequiredResources []ResourceRequirement
	Qualification     string
	QualificationRefs []values.EntityRef
	PrivacyClass      string
	Cancellation      CancellationPolicy
	CancellationRules CancellationRules
	LeadTime          time.Duration
	ExternalCalendar  ExternalCalendarPolicy
	State             string
	CanonicalDigest   string
}

// AppointmentRequirement is retained as the descriptive API name.
type AppointmentRequirement = Requirement

func (r Requirement) Validate() error {
	state := r.State
	if state == "" {
		state = "DRAFT"
	}
	typed := r.Revision > 0
	required := []struct{ field, value string }{{"id", r.ID}, {"version", r.Version}, {"privacy_class", r.PrivacyClass}}
	for _, v := range required {
		if strings.TrimSpace(v.value) == "" {
			return reject(v.field, state, r.Version, "is required")
		}
	}
	if strings.TrimSpace(r.Qualification) == "" && len(r.QualificationRefs) == 0 {
		return reject("qualification", state, r.Version, "a qualification string or reference is required")
	}
	if !typed && strings.TrimSpace(r.Purpose) == "" {
		return reject("purpose", state, r.Version, "is required")
	}
	if typed && !r.PurposeKind.Valid() {
		return reject("purpose_kind", state, r.Version, "is required for a typed requirement")
	}
	if !typed && (strings.TrimSpace(r.Location.Mode) == "" || strings.TrimSpace(r.Location.Channel) == "") {
		return reject("location", state, r.Version, "mode and channel are required")
	}
	if typed && !r.LocationClass.Valid() {
		return reject("location_class", state, r.Version, "is required for a typed requirement")
	}
	if !typed && (strings.TrimSpace(r.Cancellation.FeePolicy) == "" || strings.TrimSpace(r.Cancellation.NoShowPolicy) == "") {
		return reject("cancellation", state, r.Version, "fee and no-show policies are required")
	}
	if typed && r.CancellationRules.Kind == "" {
		return reject("cancellation_rules", state, r.Version, "are required for a typed requirement")
	}
	if !typed && (strings.TrimSpace(r.ExternalCalendar.Mode) == "" || strings.TrimSpace(r.ExternalCalendar.DetailDisclosure) == "") {
		return reject("external_calendar", state, r.Version, "mode and disclosure are required")
	}
	if len(r.Participants) == 0 && len(r.ParticipantRefs) == 0 {
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
	if r.WindowInterval.Validate() != nil && r.Window.Start.IsZero() {
		return reject("window.start", state, r.Version, "is required")
	}
	if r.WindowInterval.Validate() != nil && r.Window.End.IsZero() {
		return reject("window.end", state, r.Version, "is required")
	}
	if r.WindowInterval.Validate() != nil && !r.Window.End.After(r.Window.Start) {
		return reject("window", state, r.Version, "end must be after start")
	}
	if r.WindowInterval.Validate() != nil && strings.TrimSpace(r.Window.TimeZone) == "" {
		return reject("window.time_zone", state, r.Version, "is required")
	}
	if !typed && r.Location.Mode == "IN_PERSON" && strings.TrimSpace(r.Location.Address) == "" {
		return reject("location.address", state, r.Version, "is required for IN_PERSON")
	}
	if r.Cancellation.Notice < 0 || r.LeadTime < 0 {
		return reject("cancellation.notice", state, r.Version, "must not be negative")
	}
	if !typed && strings.TrimSpace(r.ExternalCalendar.Mode) != "DISABLED" && strings.TrimSpace(r.ExternalCalendar.Provider) == "" {
		return reject("external_calendar.provider", state, r.Version, "is required when calendar integration is enabled")
	}
	if len(r.Resources) == 0 && len(r.RequiredResources) == 0 {
		return reject("resources", state, r.Version, "at least one resource type is required")
	}
	if len(r.Resources) > 0 {
		if err := ResourceTypes(r.Resources); err != nil {
			if x, ok := err.(*Rejection); ok {
				x.State = state
				x.Version = r.Version
			}
			return err
		}
	}
	if r.PurposeKind != "" && !r.PurposeKind.Valid() {
		return reject("purpose_kind", state, r.Version, "is not declared")
	}
	if len(r.ParticipantRefs) > 0 {
		seenRefs := make(map[string]struct{}, len(r.ParticipantRefs))
		for i, ref := range r.ParticipantRefs {
			if err := ref.Validate(); err != nil {
				return reject(fmt.Sprintf("participant_refs[%d]", i), state, r.Version, "is invalid")
			}
			if _, ok := seenRefs[ref.String()]; ok {
				return reject(fmt.Sprintf("participant_refs[%d]", i), state, r.Version, "duplicates another participant")
			}
			seenRefs[ref.String()] = struct{}{}
		}
	}
	seenQualifications := make(map[string]struct{}, len(r.QualificationRefs))
	for i, ref := range r.QualificationRefs {
		if err := ref.Validate(); err != nil {
			return reject(fmt.Sprintf("qualification_refs[%d]", i), state, r.Version, "is invalid")
		}
		if tenant := r.RequestTenant(); tenant != "tenant-placeholder" && ref.Tenant != tenant {
			return reject(fmt.Sprintf("qualification_refs[%d]", i), state, r.Version, "is cross-tenant")
		}
		if _, ok := seenQualifications[ref.String()]; ok {
			return reject(fmt.Sprintf("qualification_refs[%d]", i), state, r.Version, "duplicates another qualification")
		}
		seenQualifications[ref.String()] = struct{}{}
	}
	if r.LocationClass != "" && !r.LocationClass.Valid() {
		return reject("location_class", state, r.Version, "is not declared")
	}
	if r.WindowInterval.Validate() == nil && r.WindowInterval.Kind() != values.IntervalKindInstant {
		return reject("window_interval", state, r.Version, "must use INSTANT boundaries")
	}
	if r.WindowInterval.Validate() != nil && r.Revision > 0 {
		return reject("window_interval", state, r.Version, "is required for a versioned typed requirement")
	}
	if r.LeadTime < 0 {
		return reject("lead_time", state, r.Version, "must not be negative")
	}
	if r.CancellationRules.Kind != "" {
		if err := r.CancellationRules.Validate(); err != nil {
			return reject("cancellation_rules", state, r.Version, "is invalid")
		}
	}
	if len(r.RequiredResources) > 0 {
		seenResources := make(map[string]struct{}, len(r.RequiredResources))
		for i, resource := range r.RequiredResources {
			if err := resource.Validate(r.RequestTenant()); err != nil {
				return reject(fmt.Sprintf("required_resources[%d]", i), state, r.Version, "is invalid")
			}
			if _, ok := seenResources[resource.key()]; ok {
				return reject(fmt.Sprintf("required_resources[%d]", i), state, r.Version, "duplicates another resource type")
			}
			seenResources[resource.key()] = struct{}{}
		}
	}
	if r.Revision > 0 {
		if len(r.ParticipantRefs) == 0 {
			return reject("participant_refs", state, r.Version, "at least one participant reference is required")
		}
		if len(r.RequiredResources) == 0 {
			return reject("required_resources", state, r.Version, "at least one typed resource requirement is required")
		}
	}
	if r.CanonicalDigest != "" && r.CanonicalDigest != r.computedDigest() {
		return reject("canonical_digest", state, r.Version, "does not match the canonical requirement bytes")
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
	c.ParticipantRefs = append([]values.EntityRef(nil), r.ParticipantRefs...)
	c.Resources = append([]ResourceType(nil), r.Resources...)
	c.RequiredResources = append([]ResourceRequirement(nil), r.RequiredResources...)
	c.QualificationRefs = append([]values.EntityRef(nil), r.QualificationRefs...)
	c.CanonicalDigest = c.computedDigest()
	return Publication{Requirement: c}, nil
}
