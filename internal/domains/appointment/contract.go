package appointment

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

const schemaVersion = 1

// Version reports this package's contract version.
func Version() int { return schemaVersion }

type PurposeKind string

const (
	PurposeInterview    PurposeKind = "INTERVIEW"
	PurposeAssessment   PurposeKind = "ASSESSMENT"
	PurposeOnboarding   PurposeKind = "ONBOARDING"
	PurposeTraining     PurposeKind = "TRAINING"
	PurposeConsultation PurposeKind = "CONSULTATION"
)

func (p PurposeKind) Valid() bool {
	switch p {
	case PurposeInterview, PurposeAssessment, PurposeOnboarding, PurposeTraining, PurposeConsultation:
		return true
	default:
		return false
	}
}

type LocationClass string

const (
	LocationVirtual  LocationClass = "VIRTUAL"
	LocationInPerson LocationClass = "IN_PERSON"
	LocationHybrid   LocationClass = "HYBRID"
)

func (l LocationClass) Valid() bool {
	return l == LocationVirtual || l == LocationInPerson || l == LocationHybrid
}

type CapacityMode string

const (
	CapacityExclusive CapacityMode = "EXCLUSIVE"
	CapacityShared    CapacityMode = "SHARED"
	CapacityUnlimited CapacityMode = "UNLIMITED"
)

func (m CapacityMode) Valid() bool {
	return m == CapacityExclusive || m == CapacityShared || m == CapacityUnlimited
}

type CancellationKind string

const (
	CancellationAllowed  CancellationKind = "ALLOWED"
	CancellationRequired CancellationKind = "REQUIRED"
	CancellationDisabled CancellationKind = "DISABLED"
)

func (k CancellationKind) Valid() bool {
	return k == CancellationAllowed || k == CancellationRequired || k == CancellationDisabled
}

type NoShowKind string

const (
	NoShowFollowUp NoShowKind = "FOLLOW_UP"
	NoShowReview   NoShowKind = "REVIEW"
	NoShowNone     NoShowKind = "NONE"
)

func (k NoShowKind) Valid() bool { return k == NoShowFollowUp || k == NoShowReview || k == NoShowNone }

type CancellationRules struct {
	Notice time.Duration
	Kind   CancellationKind
	NoShow NoShowKind
}

func (r CancellationRules) Validate() error {
	if r.Notice < 0 || !r.Kind.Valid() || !r.NoShow.Valid() {
		return fmt.Errorf("%w: cancellation notice, kind and no-show policy are invalid", ErrInvalidRequirement)
	}
	return nil
}
func (r CancellationRules) Canonical() []byte {
	if r.Validate() != nil {
		return nil
	}
	b, err := canonicalbytes.New("hcmnext.domains.appointment.CancellationRules", schemaVersion).
		Int("notice_nanos", int64(r.Notice)).String("kind", string(r.Kind)).String("no_show", string(r.NoShow)).Bytes()
	if err != nil {
		return nil
	}
	return b
}

// ParticipantRequirement binds a participant role to a tenant-scoped subject.
type ParticipantRequirement struct {
	Ref      values.EntityRef
	Role     string
	Required bool
}

func (p ParticipantRequirement) Validate(tenant values.TenantId) error {
	if err := p.Ref.Validate(); err != nil {
		return fmt.Errorf("%w: participant ref: %v", ErrInvalidRequirement, err)
	}
	if p.Ref.Tenant != tenant || strings.TrimSpace(p.Role) == "" {
		return fmt.Errorf("%w: participant ref and role must be in the requirement tenant", ErrInvalidRequirement)
	}
	return nil
}
func (p ParticipantRequirement) Canonical() []byte {
	if p.Ref.Validate() != nil || strings.TrimSpace(p.Role) == "" {
		return nil
	}
	b, err := canonicalbytes.New("hcmnext.domains.appointment.ParticipantRequirement", schemaVersion).
		Value("ref", p.Ref).String("role", p.Role).Bool("required", p.Required).Bytes()
	if err != nil {
		return nil
	}
	return b
}

// ResourceRequirement names one resource type and the quantity needed for an
// appointment. Count is a convenience for legacy callers; Quantity is the
// authoritative fixed-unit quantity when present.
type ResourceRequirement struct {
	ResourceTypeRef values.EntityRef
	ResourceTypeID  string
	Quantity        values.Quantity
	Count           int
}

func (r ResourceRequirement) Validate(tenant values.TenantId) error {
	hasRef := r.ResourceTypeRef != (values.EntityRef{})
	if hasRef {
		if err := r.ResourceTypeRef.Validate(); err != nil || r.ResourceTypeRef.Tenant != tenant {
			return fmt.Errorf("%w: resource type ref is invalid or cross-tenant", ErrInvalidRequirement)
		}
	}
	if !hasRef && strings.TrimSpace(r.ResourceTypeID) == "" {
		return fmt.Errorf("%w: resource type ref or id is required", ErrInvalidRequirement)
	}
	if r.Quantity.Validate() != nil {
		if r.Count <= 0 {
			return fmt.Errorf("%w: resource quantity must be positive", ErrInvalidRequirement)
		}
		return nil
	}
	if r.Quantity.Value().Sign() <= 0 {
		return fmt.Errorf("%w: resource quantity must be positive", ErrInvalidRequirement)
	}
	return nil
}

func (r ResourceRequirement) quantity() (values.Quantity, error) {
	if r.Quantity.Validate() == nil {
		return r.Quantity, nil
	}
	return values.NewQuantity(fmt.Sprintf("%d", r.Count), "EACH", 0, values.RoundingHalfEven)
}
func (r ResourceRequirement) key() string {
	if r.ResourceTypeRef != (values.EntityRef{}) {
		return r.ResourceTypeRef.String()
	}
	return r.ResourceTypeID
}
func (r ResourceRequirement) Canonical() []byte {
	if r.ResourceTypeRef.Validate() != nil && strings.TrimSpace(r.ResourceTypeID) == "" {
		return nil
	}
	quantity, err := r.quantity()
	if err != nil {
		return nil
	}
	w := canonicalbytes.New("hcmnext.domains.appointment.ResourceRequirement", schemaVersion).
		Optional("resource_type_ref", r.ResourceTypeRef != (values.EntityRef{}), r.ResourceTypeRef).
		String("resource_type_id", r.ResourceTypeID).Value("quantity", quantity)
	b, err := w.Bytes()
	if err != nil {
		return nil
	}
	return b
}

func (r Requirement) modernWindow() (values.EffectiveInterval, error) {
	if r.WindowInterval.Validate() == nil {
		return r.WindowInterval, nil
	}
	if r.Window.Start.IsZero() || r.Window.End.IsZero() {
		return values.EffectiveInterval{}, fmt.Errorf("%w: appointment window is required", ErrInvalidRequirement)
	}
	return values.NewInstantInterval(values.NewInstant(r.Window.Start), values.NewInstant(r.Window.End))
}

func (r Requirement) modernResources() []ResourceRequirement {
	if len(r.RequiredResources) > 0 {
		return append([]ResourceRequirement(nil), r.RequiredResources...)
	}
	out := make([]ResourceRequirement, 0, len(r.Resources))
	for _, resource := range r.Resources {
		out = append(out, ResourceRequirement{ResourceTypeID: resource.ID, Count: 1})
	}
	return out
}

func (r Requirement) canonicalBody() []byte {
	window, err := r.modernWindow()
	if err != nil {
		return nil
	}
	resources := r.modernResources()
	sort.Slice(resources, func(i, j int) bool { return resources[i].key() < resources[j].key() })
	refs := append([]values.EntityRef(nil), r.ParticipantRefs...)
	sort.Slice(refs, func(i, j int) bool { return refs[i].String() < refs[j].String() })
	qualificationRefs := append([]values.EntityRef(nil), r.QualificationRefs...)
	sort.Slice(qualificationRefs, func(i, j int) bool { return qualificationRefs[i].String() < qualificationRefs[j].String() })
	participants := append([]ParticipantRole(nil), r.Participants...)
	sort.Slice(participants, func(i, j int) bool { return participants[i].Role < participants[j].Role })
	mode := r.Location.Mode
	if r.LocationClass != "" {
		mode = string(r.LocationClass)
	}
	w := canonicalbytes.New("hcmnext.domains.appointment.AppointmentRequirement", schemaVersion).
		String("id", r.ID).String("version", r.Version).Int("revision", int64(r.Revision)).
		String("purpose", r.Purpose).String("purpose_kind", string(r.PurposeKind)).String("location_class", mode).
		Value("window", window).Int("duration_nanos", int64(r.Duration)).Int("lead_time_nanos", int64(r.LeadTime)).
		String("privacy_class", r.PrivacyClass).String("qualification", r.Qualification).
		String("external_calendar.mode", r.ExternalCalendar.Mode).String("external_calendar.provider", r.ExternalCalendar.Provider).
		String("external_calendar.detail_disclosure", r.ExternalCalendar.DetailDisclosure).
		Count("participant_refs", len(refs))
	for _, ref := range refs {
		w.Value("participant_ref", ref)
	}
	w.Count("qualification_refs", len(qualificationRefs))
	for _, ref := range qualificationRefs {
		w.Value("qualification_ref", ref)
	}
	w.Count("participant_roles", len(participants))
	for _, participant := range participants {
		w.String("participant_role", participant.Role).Bool("participant_required", participant.Required).
			Int("participant_min", int64(participant.Min)).Int("participant_max", int64(participant.Max))
	}
	w.Count("resources", len(resources))
	for _, resource := range resources {
		w.Value("resource", resource)
	}
	if r.CancellationRules.ValidForCanonical() {
		w.Value("cancellation_rules", r.CancellationRules)
	} else {
		w.String("cancellation_fee_policy", r.Cancellation.FeePolicy).
			String("cancellation_no_show_policy", r.Cancellation.NoShowPolicy).Int("cancellation_notice_nanos", int64(r.Cancellation.Notice))
	}
	b, err := w.Bytes()
	if err != nil {
		return nil
	}
	return b
}

func (r CancellationRules) ValidForCanonical() bool {
	return r.Kind.Valid() && r.NoShow.Valid() && r.Notice >= 0
}

func (r Requirement) computedDigest() string {
	b := r.canonicalBody()
	if b == nil {
		return ""
	}
	return canonicalbytes.Digest(b)
}

// NewAppointmentRequirement copies, validates, and digests a typed immutable
// appointment requirement. Revision zero is normalized to the first revision.
func NewAppointmentRequirement(r Requirement) (Requirement, error) {
	if r.Revision == 0 {
		r.Revision = 1
	}
	r.ParticipantRefs = append([]values.EntityRef(nil), r.ParticipantRefs...)
	r.Participants = append([]ParticipantRole(nil), r.Participants...)
	r.Resources = append([]ResourceType(nil), r.Resources...)
	r.RequiredResources = append([]ResourceRequirement(nil), r.RequiredResources...)
	r.QualificationRefs = append([]values.EntityRef(nil), r.QualificationRefs...)
	for i := range r.Resources {
		if r.Resources[i].CanonicalDigest == "" {
			resource, err := NewResourceType(r.Resources[i])
			if err != nil {
				return Requirement{}, err
			}
			r.Resources[i] = resource
		}
	}
	r.CanonicalDigest = r.computedDigest()
	if err := r.Validate(); err != nil {
		return Requirement{}, err
	}
	return r, nil
}

func (r Requirement) Canonical() []byte {
	if r.Validate() != nil {
		return nil
	}
	return r.canonicalBody()
}
func (r Requirement) Digest() (string, error) {
	if err := r.Validate(); err != nil {
		return "", err
	}
	return r.computedDigest(), nil
}

// ResourceAvailability is a declaration of a resource type's available
// capacity over a window. The evaluator only reads these values.
type ResourceAvailability struct {
	ResourceRef     values.EntityRef
	ResourceTypeRef values.EntityRef
	ResourceTypeID  string
	Window          values.EffectiveInterval
	Capacity        int64
	CapacityMode    CapacityMode
	CapacityLimit   int64
}

func (a ResourceAvailability) Validate(tenant values.TenantId) error {
	if err := a.ResourceRef.Validate(); err != nil || tenant != "tenant-placeholder" && a.ResourceRef.Tenant != tenant {
		return fmt.Errorf("%w: resource ref is invalid or cross-tenant", ErrInvalidRequirement)
	}
	if a.ResourceTypeRef != (values.EntityRef{}) {
		if err := a.ResourceTypeRef.Validate(); err != nil || tenant != "tenant-placeholder" && a.ResourceTypeRef.Tenant != tenant {
			return fmt.Errorf("%w: resource type ref is invalid or cross-tenant", ErrInvalidRequirement)
		}
	}
	if a.ResourceTypeRef == (values.EntityRef{}) && strings.TrimSpace(a.ResourceTypeID) == "" {
		return fmt.Errorf("%w: resource type ref or id is required", ErrInvalidRequirement)
	}
	if err := a.Window.Validate(); err != nil {
		return fmt.Errorf("%w: resource availability window: %v", ErrInvalidRequirement, err)
	}
	if a.Window.Kind() != values.IntervalKindInstant || a.Capacity <= 0 {
		return fmt.Errorf("%w: resource availability needs INSTANT window and positive capacity", ErrInvalidRequirement)
	}
	if a.CapacityMode != "" && !a.CapacityMode.Valid() {
		return fmt.Errorf("%w: resource availability capacity mode is not declared", ErrInvalidRequirement)
	}
	if a.CapacityMode == CapacityShared && a.CapacityLimit <= 0 {
		return fmt.Errorf("%w: shared resource availability needs a positive capacity limit", ErrInvalidRequirement)
	}
	return nil
}

type ShortfallReason string

const (
	ShortfallMissingType ShortfallReason = "MISSING_RESOURCE_TYPE"
	ShortfallWindowGap   ShortfallReason = "WINDOW_UNAVAILABLE"
	ShortfallCapacity    ShortfallReason = "INSUFFICIENT_CAPACITY"
)

func (r ShortfallReason) Valid() bool {
	return r == ShortfallMissingType || r == ShortfallWindowGap || r == ShortfallCapacity
}

type ResourceShortfall struct {
	ResourceTypeRef values.EntityRef
	ResourceTypeID  string
	Required        values.Quantity
	Available       values.Quantity
	Shortfall       values.Quantity
	Reason          ShortfallReason
}

func (s ResourceShortfall) Validate() error {
	if s.ResourceTypeRef.Validate() != nil && strings.TrimSpace(s.ResourceTypeID) == "" || s.Required.Validate() != nil || s.Available.Validate() != nil || s.Shortfall.Validate() != nil || !s.Reason.Valid() {
		return fmt.Errorf("%w: malformed resource shortfall", ErrInvalidRequirement)
	}
	return nil
}

type FeasibilityResult struct {
	RequirementID     string
	RequirementDigest string
	Feasible          bool
	Shortfalls        []ResourceShortfall
	CanonicalDigest   string
}

func (f FeasibilityResult) body() []byte {
	w := canonicalbytes.New("hcmnext.domains.appointment.FeasibilityResult", schemaVersion).
		String("requirement_id", f.RequirementID).String("requirement_digest", f.RequirementDigest).
		Bool("feasible", f.Feasible).Count("shortfalls", len(f.Shortfalls))
	for _, s := range f.Shortfalls {
		w.Optional("resource_type_ref", s.ResourceTypeRef != (values.EntityRef{}), s.ResourceTypeRef).
			String("resource_type_id", s.ResourceTypeID).Value("required", s.Required).Value("available", s.Available).
			Value("shortfall", s.Shortfall).String("reason", string(s.Reason))
	}
	b, err := w.Bytes()
	if err != nil {
		return nil
	}
	return b
}
func (f FeasibilityResult) computedDigest() string {
	b := f.body()
	if b == nil {
		return ""
	}
	return canonicalbytes.Digest(b)
}
func (f FeasibilityResult) Validate() error {
	if strings.TrimSpace(f.RequirementID) == "" || strings.TrimSpace(f.RequirementDigest) == "" {
		return fmt.Errorf("%w: feasibility requirement binding is required", ErrInvalidRequirement)
	}
	for _, s := range f.Shortfalls {
		if err := s.Validate(); err != nil {
			return err
		}
	}
	if f.Feasible && len(f.Shortfalls) != 0 || !f.Feasible && len(f.Shortfalls) == 0 {
		return fmt.Errorf("%w: feasible result and shortfalls disagree", ErrInvalidRequirement)
	}
	return nil
}
func (f FeasibilityResult) Canonical() []byte {
	if err := f.Validate(); err != nil {
		return nil
	}
	return f.body()
}

func quantityWithUnit(value int64, unit string) (values.Quantity, error) {
	return values.NewQuantity(fmt.Sprintf("%d", value), unit, 0, values.RoundingHalfEven)
}

// CheckFeasibility compares a requirement's resource quantities to the
// declared availability windows. It returns typed shortfalls and never
// reserves or mutates a resource.
func CheckFeasibility(r Requirement, availability []ResourceAvailability) (FeasibilityResult, error) {
	if err := r.Validate(); err != nil {
		return FeasibilityResult{}, err
	}
	window, err := r.modernWindow()
	if err != nil {
		return FeasibilityResult{}, err
	}
	resources := r.modernResources()
	result := FeasibilityResult{RequirementID: r.ID, RequirementDigest: r.computedDigest(), Feasible: true}
	for i, item := range availability {
		if err := item.Validate(r.RequestTenant()); err != nil {
			return FeasibilityResult{}, fmt.Errorf("availability %d: %w", i, err)
		}
	}
	for _, required := range resources {
		quantity, err := required.quantity()
		if err != nil {
			return FeasibilityResult{}, err
		}
		available, err := quantityWithUnit(0, quantity.Unit())
		if err != nil {
			return FeasibilityResult{}, err
		}
		matchedType, matchedWindow := false, false
		seenResources := make(map[string]struct{})
		for _, item := range availability {
			if !sameResourceType(required, item) {
				continue
			}
			matchedType = true
			if !coversInstantWindow(item.Window, window) {
				continue
			}
			matchedWindow = true
			resourceKey := item.ResourceRef.String()
			if _, seen := seenResources[resourceKey]; seen {
				continue
			}
			seenResources[resourceKey] = struct{}{}
			capacity := item.Capacity
			switch item.CapacityMode {
			case CapacityExclusive:
				capacity = 1
			case CapacityShared:
				if item.CapacityLimit < capacity {
					capacity = item.CapacityLimit
				}
			case CapacityUnlimited:
				available = quantity
			}
			if item.CapacityMode == CapacityUnlimited {
				break
			}
			if capacity < 1 {
				continue
			}
			added, err := quantityWithUnit(capacity, quantity.Unit())
			if err != nil {
				return FeasibilityResult{}, err
			}
			available, err = available.Add(added)
			if err != nil {
				return FeasibilityResult{}, err
			}
		}
		cmp := available.Value().Cmp(quantity.Value())
		if cmp >= 0 {
			continue
		}
		shortfall, err := quantity.Sub(available)
		if err != nil {
			return FeasibilityResult{}, err
		}
		reason := ShortfallCapacity
		if !matchedType {
			reason = ShortfallMissingType
		} else if !matchedWindow {
			reason = ShortfallWindowGap
		}
		result.Feasible = false
		result.Shortfalls = append(result.Shortfalls, ResourceShortfall{ResourceTypeRef: required.ResourceTypeRef, ResourceTypeID: required.ResourceTypeID, Required: quantity, Available: available, Shortfall: shortfall, Reason: reason})
	}
	result.CanonicalDigest = result.computedDigest()
	return result, nil
}

func sameResourceType(required ResourceRequirement, available ResourceAvailability) bool {
	if required.ResourceTypeRef != (values.EntityRef{}) && available.ResourceTypeRef != (values.EntityRef{}) {
		return required.ResourceTypeRef == available.ResourceTypeRef
	}
	return required.ResourceTypeID != "" && required.ResourceTypeID == available.ResourceTypeID
}

func coversInstantWindow(container, target values.EffectiveInterval) bool {
	if container.Validate() != nil || target.Validate() != nil || container.Kind() != values.IntervalKindInstant || target.Kind() != values.IntervalKindInstant {
		return false
	}
	start, _ := container.StartInstant()
	targetStart, _ := target.StartInstant()
	if start.Compare(targetStart) > 0 {
		return false
	}
	end, hasEnd := container.EndInstant()
	targetEnd, targetHasEnd := target.EndInstant()
	return !targetHasEnd || hasEnd && end.Compare(targetEnd) >= 0
}

// RequestTenant is the tenant that owns tenant-scoped participant and
// resource references. Legacy requirements have no typed ref and use the
// empty tenant, so their availability is validated structurally only.
func (r Requirement) RequestTenant() values.TenantId {
	for _, ref := range r.ParticipantRefs {
		return ref.Tenant
	}
	for _, resource := range r.RequiredResources {
		if resource.ResourceTypeRef != (values.EntityRef{}) {
			return resource.ResourceTypeRef.Tenant
		}
	}
	return "tenant-placeholder"
}

type AppointmentExplanation struct {
	RequirementID string
	Revision      uint64
	Digest        string
	Purpose       PurposeKind
	LocationClass LocationClass
	ResourceCount int
	Authority     string
}

func (r Requirement) Explain() (AppointmentExplanation, error) {
	if err := r.Validate(); err != nil {
		return AppointmentExplanation{}, err
	}
	return AppointmentExplanation{RequirementID: r.ID, Revision: r.Revision, Digest: r.computedDigest(), Purpose: r.PurposeKind, LocationClass: r.LocationClass, ResourceCount: len(r.modernResources()), Authority: "descriptive requirement only; no reservation authority"}, nil
}

func Explain(r Requirement) (AppointmentExplanation, error) { return r.Explain() }
