// CYCLE-003: resolve a cycle's declared nominal cutoffs against a tenant
// calendar -- working weekdays, holidays scoped by jurisdiction, and a
// timezone/tzdb version -- into exact cutoff instants. ResolveCutoff is
// pure and deterministic: every input it needs (the rule, the calendar) is
// a parameter, it never reads a wall clock, and two calls with
// byte-identical inputs always produce a byte-identical resolution. When
// the calendar cannot resolve one exact instant -- a local time inside a
// DST gap or repeat, or a calendar with no reachable working day within its
// adjustment horizon -- ResolveCutoff reports REVIEW_REQUIRED rather than
// guessing at an answer.
package cycle

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

// maxCutoffAdjustmentDays bounds how many calendar days ResolveCutoff will
// walk looking for a working day before giving up and reporting
// REVIEW_REQUIRED. A calendar that has no working day within a year is not
// one this resolver will loop over forever.
const maxCutoffAdjustmentDays = 366

var (
	ErrCalendarZone           = errors.New("cycle: tenant calendar requires a valid zone")
	ErrCalendarRef            = errors.New("cycle: tenant calendar requires a valid calendar reference")
	ErrCalendarWorkingDays    = errors.New("cycle: tenant calendar declares no working weekday")
	ErrCutoffRulePhase        = errors.New("cycle: cutoff rule requires a phase id")
	ErrCutoffRuleDate         = errors.New("cycle: cutoff rule requires a valid nominal local date and time")
	ErrCutoffRuleJurisdiction = errors.New("cycle: cutoff rule requires a jurisdiction reference")
	ErrCutoffRuleAdjustment   = errors.New("cycle: cutoff rule requires a declared adjustment policy")
)

// Holiday is one non-working calendar date recognized under a specific
// jurisdiction. A tenant calendar can carry holidays for several
// jurisdictions at once; ResolveCutoff only ever consults the jurisdiction a
// CutoffRule names explicitly, never every jurisdiction on file.
type Holiday struct {
	Date            values.LocalDate
	JurisdictionRef string
	Name            string
}

// AdjustmentPolicy declares how a nominal cutoff date that lands on a
// non-working day is moved onto a working one. There is no default policy:
// an unrecognized or unset policy is invalid, never "no adjustment by
// convention".
type AdjustmentPolicy string

const (
	AdjustmentNone             AdjustmentPolicy = "NO_ADJUSTMENT"
	AdjustmentNextBusinessDay  AdjustmentPolicy = "ROLL_FORWARD_NEXT_BUSINESS_DAY"
	AdjustmentPriorBusinessDay AdjustmentPolicy = "ROLL_BACKWARD_PRIOR_BUSINESS_DAY"
)

func (a AdjustmentPolicy) valid() bool {
	switch a {
	case AdjustmentNone, AdjustmentNextBusinessDay, AdjustmentPriorBusinessDay:
		return true
	default:
		return false
	}
}

// TenantCalendar is the explicit, versioned calendar a cutoff resolves
// against: the timezone/tzdb version, the business-calendar reference and
// version, which weekdays are working by default, and the holiday list. A
// republished calendar (a new Calendar.Version) is a different
// TenantCalendar even when its holiday list is byte-identical, so a
// resolution always names the exact revision that produced it.
type TenantCalendar struct {
	Zone            values.ZoneRef
	Calendar        values.CalendarRef
	WorkingWeekdays map[time.Weekday]bool
	Holidays        []Holiday
}

// Validate reports whether the calendar is complete enough to resolve a
// cutoff against.
func (c TenantCalendar) Validate() error {
	if err := c.Zone.Validate(); err != nil {
		return fmt.Errorf("%w: %v", ErrCalendarZone, err)
	}
	if err := c.Calendar.Validate(); err != nil {
		return fmt.Errorf("%w: %v", ErrCalendarRef, err)
	}
	any := false
	for _, working := range c.WorkingWeekdays {
		if working {
			any = true
			break
		}
	}
	if !any {
		return ErrCalendarWorkingDays
	}
	return nil
}

// civilWeekday returns the day of week for a LocalDate, treated as a plain
// civil calendar date (never converted through any zone).
func civilWeekday(d values.LocalDate) time.Weekday {
	return time.Date(int(d.Year()), d.Month(), int(d.Day()), 0, 0, 0, 0, time.UTC).Weekday()
}

func (c TenantCalendar) holidayOn(d values.LocalDate, jurisdiction string) (Holiday, bool) {
	for _, h := range c.Holidays {
		if h.JurisdictionRef == jurisdiction && h.Date == d {
			return h, true
		}
	}
	return Holiday{}, false
}

// isWorkingDay reports whether d is a working day under c for jurisdiction,
// and, when it is not, the reason it is not.
func (c TenantCalendar) isWorkingDay(d values.LocalDate, jurisdiction string) (bool, string) {
	wd := civilWeekday(d)
	if !c.WorkingWeekdays[wd] {
		return false, fmt.Sprintf("%s is a non-working weekday (%s) under calendar %s", d, wd, c.Calendar)
	}
	if h, ok := c.holidayOn(d, jurisdiction); ok {
		return false, fmt.Sprintf("%s is holiday %q for jurisdiction %q under calendar %s", d, h.Name, jurisdiction, c.Calendar)
	}
	return true, ""
}

// CutoffRule declares one nominal local cutoff: the calendar date and time
// of day it names, the jurisdiction whose holiday list governs it, and the
// adjustment policy applied when the nominal date is not a working day.
type CutoffRule struct {
	PhaseID         string
	NominalDate     values.LocalDate
	NominalTime     values.LocalTime
	JurisdictionRef string
	Adjustment      AdjustmentPolicy
}

// Validate reports whether the rule is complete enough to resolve.
func (r CutoffRule) Validate() error {
	if strings.TrimSpace(r.PhaseID) == "" {
		return ErrCutoffRulePhase
	}
	if err := r.NominalDate.Validate(); err != nil {
		return fmt.Errorf("%w: %v", ErrCutoffRuleDate, err)
	}
	if err := r.NominalTime.Validate(); err != nil {
		return fmt.Errorf("%w: %v", ErrCutoffRuleDate, err)
	}
	if strings.TrimSpace(r.JurisdictionRef) == "" {
		return ErrCutoffRuleJurisdiction
	}
	if !r.Adjustment.valid() {
		return ErrCutoffRuleAdjustment
	}
	return nil
}

// CutoffStatus is the exhaustive outcome of resolving one CutoffRule.
type CutoffStatus string

const (
	// CutoffResolved reports that ResolveCutoff produced an exact instant.
	CutoffResolved CutoffStatus = "RESOLVED"
	// CutoffReviewRequired reports that the calendar could not resolve an
	// exact instant for the rule; a human decision is required instead of a
	// guess.
	CutoffReviewRequired CutoffStatus = "REVIEW_REQUIRED"
)

// CutoffResolution is the deterministic, explained outcome of resolving one
// CutoffRule against one TenantCalendar.
type CutoffResolution struct {
	PhaseID       string
	Status        CutoffStatus
	EffectiveDate values.LocalDate
	// Instant is set only when Status is CutoffResolved.
	Instant time.Time
	// Rule names the deciding rule: which calendar facts were consulted and
	// why the resolution landed where it did (ARCH-GO-009's required
	// Explain-shaped output).
	Rule string
}

// ResolveCutoff turns rule's nominal local cutoff into an exact instant
// under calendar, or reports REVIEW_REQUIRED when the calendar cannot
// resolve one. No ambient-clock calculation: ResolveCutoff never reads a
// wall clock and never touches package-level state, so two calls with
// byte-identical rule and calendar values always produce byte-identical
// resolutions.
func ResolveCutoff(rule CutoffRule, calendar TenantCalendar) (CutoffResolution, error) {
	if err := rule.Validate(); err != nil {
		return CutoffResolution{}, err
	}
	if err := calendar.Validate(); err != nil {
		return CutoffResolution{}, err
	}

	date := rule.NominalDate
	base := fmt.Sprintf("nominal cutoff %s %s in jurisdiction %q under calendar %s",
		rule.NominalDate, rule.NominalTime, rule.JurisdictionRef, calendar.Calendar)

	if working, why := calendar.isWorkingDay(date, rule.JurisdictionRef); !working && rule.Adjustment != AdjustmentNone {
		step := 1
		if rule.Adjustment == AdjustmentPriorBusinessDay {
			step = -1
		}
		moved := 0
		for {
			if moved >= maxCutoffAdjustmentDays {
				return CutoffResolution{
					PhaseID:       rule.PhaseID,
					Status:        CutoffReviewRequired,
					EffectiveDate: date,
					Rule: fmt.Sprintf("%s; %s; no working day found within %d days under calendar %s: REVIEW_REQUIRED",
						base, why, maxCutoffAdjustmentDays, calendar.Calendar),
				}, nil
			}
			date = date.AddDays(step)
			moved++
			if ok, _ := calendar.isWorkingDay(date, rule.JurisdictionRef); ok {
				break
			}
		}
		base = fmt.Sprintf("%s; %s; adjusted (%s) %d day(s) to %s", base, why, rule.Adjustment, moved, date)
	}

	zdt, err := values.NewZonedDateTime(date, rule.NominalTime, calendar.Zone, values.DisambiguationRejectGap)
	if err != nil {
		if errors.Is(err, values.ErrDSTGap) || errors.Is(err, values.ErrDSTAmbiguous) {
			return CutoffResolution{
				PhaseID:       rule.PhaseID,
				Status:        CutoffReviewRequired,
				EffectiveDate: date,
				Rule: fmt.Sprintf("%s; local time is ambiguous or does not exist under zone %s tzdb %s: %v: REVIEW_REQUIRED",
					base, calendar.Zone.ID, calendar.Zone.TzdbVersion, err),
			}, nil
		}
		return CutoffResolution{}, err
	}

	return CutoffResolution{
		PhaseID:       rule.PhaseID,
		Status:        CutoffResolved,
		EffectiveDate: date,
		Instant:       zdt.Instant().Time(),
		Rule: fmt.Sprintf("%s; resolved at UTC offset %+ds under zone %s tzdb %s",
			base, zdt.OffsetSeconds(), calendar.Zone.ID, calendar.Zone.TzdbVersion),
	}, nil
}

// ResolveCycleCutoffs resolves one CutoffResolution per rule, in the same
// order given, against one shared tenant calendar.
func ResolveCycleCutoffs(rules []CutoffRule, calendar TenantCalendar) ([]CutoffResolution, error) {
	out := make([]CutoffResolution, 0, len(rules))
	for _, rule := range rules {
		res, err := ResolveCutoff(rule, calendar)
		if err != nil {
			return nil, fmt.Errorf("cycle: resolving cutoff for phase %q: %w", rule.PhaseID, err)
		}
		out = append(out, res)
	}
	return out, nil
}
