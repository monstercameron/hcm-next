package model

import (
	"fmt"

	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

// RetentionClass declares how long evidence of one record class is kept,
// including per-jurisdiction overrides (MODEL-026). Jurisdiction is modeled
// as a typed override table rather than a single number because
// planning/research/state-employment-law will supply per-jurisdiction
// retention periods later; a jurisdiction with no override falls back to
// DefaultPeriodDays.
type RetentionClass struct {
	ClassRef string

	// DefaultPeriodDays is the retention floor applied when no jurisdiction
	// override exists. Zero is invalid: every class must declare either a
	// default or be unresolvable, and an unresolvable class fails
	// publication rather than silently retaining forever.
	DefaultPeriodDays uint32

	// JurisdictionOverrides maps an ISO-3166-2-style jurisdiction code (e.g.
	// "US-CA") to its retention period in days.
	JurisdictionOverrides map[string]uint32

	// TriggerEvent names the event the retention clock starts from, e.g.
	// "EMPLOYMENT_END" or "RECORD_CREATED".
	TriggerEvent string

	DispositionOwner string

	AuthorityRef string
}

// Validate rejects a retention class missing a trigger, disposition owner,
// authority or usable period.
func (c RetentionClass) Validate() error {
	if c.ClassRef == "" {
		return newError("RetentionClass.Validate", "class_ref", ErrInvalidRetention,
			"retention class carries no reference")
	}
	if c.DefaultPeriodDays == 0 && len(c.JurisdictionOverrides) == 0 {
		return newError("RetentionClass.Validate", "default_period_days", ErrInvalidRetention,
			"%s declares no default period and no jurisdiction override", c.ClassRef)
	}
	if c.TriggerEvent == "" {
		return newError("RetentionClass.Validate", "trigger_event", ErrInvalidRetention,
			"%s names no retention trigger event", c.ClassRef)
	}
	if c.DispositionOwner == "" {
		return newError("RetentionClass.Validate", "disposition_owner", ErrInvalidRetention,
			"%s names no disposition owner", c.ClassRef)
	}
	if c.AuthorityRef == "" {
		return newError("RetentionClass.Validate", "authority_ref", ErrInvalidRetention,
			"%s names no authority", c.ClassRef)
	}
	return nil
}

// RetentionSchedule is the explainable, resolved retention outcome for one
// jurisdiction.
type RetentionSchedule struct {
	ClassRef     string
	Jurisdiction string
	PeriodDays   uint32
	TriggerAt    values.Instant
	NextActionAt values.Instant
	Explanation  string
}

// EffectiveRetention resolves the retention period for jurisdiction, falling
// back to the class default when no override exists, and computes the next
// action date from triggerAt. It rejects a jurisdiction with neither an
// override nor a usable class default.
func (c RetentionClass) EffectiveRetention(jurisdiction string, triggerAt values.Instant) (RetentionSchedule, error) {
	if err := c.Validate(); err != nil {
		return RetentionSchedule{}, err
	}
	if !triggerAt.IsSet() {
		return RetentionSchedule{}, newError("EffectiveRetention", "trigger_at", ErrInvalidRetention,
			"%s resolution requires a trigger time", c.ClassRef)
	}
	period, explanation, ok := c.resolvePeriod(jurisdiction)
	if !ok {
		return RetentionSchedule{}, newError("EffectiveRetention", "jurisdiction", ErrNoJurisdictionOverride,
			"%s has no retention period for jurisdiction %q", c.ClassRef, jurisdiction)
	}
	next := values.NewInstant(triggerAt.Time().AddDate(0, 0, int(period)))
	return RetentionSchedule{
		ClassRef:     c.ClassRef,
		Jurisdiction: jurisdiction,
		PeriodDays:   period,
		TriggerAt:    triggerAt,
		NextActionAt: next,
		Explanation:  explanation,
	}, nil
}

func (c RetentionClass) resolvePeriod(jurisdiction string) (uint32, string, bool) {
	if period, ok := c.JurisdictionOverrides[jurisdiction]; ok {
		return period, fmt.Sprintf("jurisdiction override for %s", jurisdiction), true
	}
	if c.DefaultPeriodDays > 0 {
		return c.DefaultPeriodDays, "class default", true
	}
	return 0, "", false
}

// RecordsDeclaration is one material record's declared retention binding
// (MODEL-026).
type RecordsDeclaration struct {
	RecordRef        string
	RecordClassRef   string
	AuthorityRef     string
	DispositionOwner string
	Jurisdiction     string
	CreatedAt        values.Instant
}

// Validate rejects a records declaration missing a record class, authority or
// disposition owner.
func (d RecordsDeclaration) Validate() error {
	if d.RecordRef == "" {
		return newError("RecordsDeclaration.Validate", "record_ref", ErrInvalidRetention,
			"declaration carries no record reference")
	}
	if d.RecordClassRef == "" {
		return newError("RecordsDeclaration.Validate", "record_class_ref", ErrInvalidRetention,
			"%s declares no record class", d.RecordRef)
	}
	if d.AuthorityRef == "" {
		return newError("RecordsDeclaration.Validate", "authority_ref", ErrInvalidRetention,
			"%s declares no authority", d.RecordRef)
	}
	if d.DispositionOwner == "" {
		return newError("RecordsDeclaration.Validate", "disposition_owner", ErrInvalidRetention,
			"%s declares no disposition owner", d.RecordRef)
	}
	if d.Jurisdiction == "" {
		return newError("RecordsDeclaration.Validate", "jurisdiction", ErrInvalidRetention,
			"%s declares no jurisdiction", d.RecordRef)
	}
	if !d.CreatedAt.IsSet() {
		return newError("RecordsDeclaration.Validate", "created_at", ErrInvalidRetention,
			"%s declares no creation time", d.RecordRef)
	}
	return nil
}

// Declare resolves a records declaration against the compiled retention-class
// table, producing an explainable schedule and next action date. It rejects
// material evidence with no record class, retention schedule, authority or
// disposition owner (MODEL-026 RED).
func Declare(classes map[string]RetentionClass, d RecordsDeclaration) (RetentionSchedule, error) {
	if err := d.Validate(); err != nil {
		return RetentionSchedule{}, err
	}
	class, ok := classes[d.RecordClassRef]
	if !ok {
		return RetentionSchedule{}, newError("Declare", "record_class_ref", ErrInvalidRetention,
			"%s references unknown retention class %q", d.RecordRef, d.RecordClassRef)
	}
	return class.EffectiveRetention(d.Jurisdiction, d.CreatedAt)
}
