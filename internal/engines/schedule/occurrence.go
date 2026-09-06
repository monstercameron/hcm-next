package schedule

import (
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/monstercameron/hcm-next/internal/engines/canonicalbytes"
	"github.com/monstercameron/hcm-next/internal/engines/cycle"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

// Occurrence calculation errors are typed so a scheduler can refuse a bad
// frozen definition without turning it into a provider or persistence call.
var (
	ErrSCHED002Rejected   = errors.New("schedule: SCHED_002_REJECTED")
	ErrOccurrenceWindow   = errors.New("schedule: occurrence window is invalid")
	ErrOccurrenceZone     = errors.New("schedule: occurrence zone is required")
	ErrOccurrenceCalendar = errors.New("schedule: occurrence calendar is required")
	ErrOccurrenceTrigger  = errors.New("schedule: published trigger is invalid")
	ErrOccurrenceDST      = errors.New("schedule: occurrence cannot be resolved across DST")
	ErrMisfirePolicy      = errors.New("schedule: misfire policy is invalid")
	ErrMisfireResume      = errors.New("schedule: misfire resume instant is required")
	ErrMisfireBound       = errors.New("schedule: catch-up bound is invalid")
	ErrCalendarRevision   = errors.New("schedule: calendar revision does not match the trigger")
	ErrTooManyOccurrences = errors.New("schedule: occurrence limit exceeded")
)

// SCHED002RejectedCode is the stable top-level refusal code for a
// calculation that cannot safely produce authoritative occurrences.
const SCHED002RejectedCode = "SCHED_002_REJECTED"

// OccurrenceWindow is a half-open UTC interval [Start, End). A window is
// explicit and therefore does not depend on the process clock or local zone.
type OccurrenceWindow struct {
	Start values.Instant
	End   values.Instant
}

// Validate reports whether the window has a usable, non-inverted interval.
func (w OccurrenceWindow) Validate() error {
	if err := w.Start.Validate(); err != nil {
		return refuse("INVALID_OCCURRENCE_WINDOW", "window.start", ErrOccurrenceWindow, "%v", err)
	}
	if err := w.End.Validate(); err != nil {
		return refuse("INVALID_OCCURRENCE_WINDOW", "window.end", ErrOccurrenceWindow, "%v", err)
	}
	if w.Start.Compare(w.End) >= 0 {
		return refuse("INVALID_OCCURRENCE_WINDOW", "window", ErrOccurrenceWindow, "start must be before end")
	}
	return nil
}

// MisfirePolicy is the action applied to occurrences that are older than the
// declared grace period when a scheduler resumes.
type MisfirePolicy string

const (
	MisfireUnspecified MisfirePolicy = ""
	MisfireFireNow     MisfirePolicy = "FIRE_NOW"
	MisfireSkip        MisfirePolicy = "SKIP"
	MisfireCatchUp     MisfirePolicy = "CATCH_UP"
	MisfireCatchUpOnce MisfirePolicy = "CATCH_UP_ONCE"
	MisfireCatchUpAll  MisfirePolicy = "CATCH_UP_ALL"
	MisfireReview      MisfirePolicy = "REVIEW"
)

// Compatibility spellings used by callers that use the wire vocabulary.
const (
	FireNow     = MisfireFireNow
	Skip        = MisfireSkip
	CatchUp     = MisfireCatchUp
	CatchUpOnce = MisfireCatchUpOnce
	CatchUpAll  = MisfireCatchUpAll
	Review      = MisfireReview
)

func (p MisfirePolicy) valid() bool {
	switch p {
	case MisfireFireNow, MisfireSkip, MisfireCatchUp, MisfireCatchUpOnce, MisfireCatchUpAll, MisfireReview:
		return true
	default:
		return false
	}
}

// MisfireConfig bounds how old a scheduled occurrence may be before the
// policy is applied. MaxCatchUp is required by CATCH_UP and CATCH_UP_ALL.
type MisfireConfig struct {
	Policy     MisfirePolicy
	Grace      time.Duration
	MaxCatchUp uint32
}

func (c MisfireConfig) Validate() error {
	if !c.Policy.valid() {
		return refuse("INVALID_MISFIRE_POLICY", "misfire.policy", ErrMisfirePolicy, "policy must be FIRE_NOW, SKIP, CATCH_UP, CATCH_UP_ONCE, CATCH_UP_ALL, or REVIEW")
	}
	if c.Grace < 0 {
		return refuse("INVALID_MISFIRE_GRACE", "misfire.grace", ErrMisfirePolicy, "grace must not be negative")
	}
	if (c.Policy == MisfireCatchUp || c.Policy == MisfireCatchUpAll) && c.MaxCatchUp == 0 {
		return refuse("MISSING_MISFIRE_BOUND", "misfire.max_catch_up", ErrMisfireBound, "CATCH_UP requires a positive bound")
	}
	return nil
}

// OccurrenceRequest supplies the explicit tenant zone and, for a CALENDAR
// source, the exact calendar dataset used to resolve the published rule.
type OccurrenceRequest struct {
	Window   OccurrenceWindow
	Zone     values.ZoneRef
	Calendar cycle.TenantCalendar
	Misfire  MisfireConfig
	ResumeAt values.Instant
}

// Occurrence is one scheduled civil time resolved to one or two exact
// instants. Fold occurrences retain their values.ZonedDateTime disambiguation
// evidence; a key includes that evidence and can therefore never silently
// collapse the two sides of a DST fold.
type Occurrence struct {
	Trigger     TriggerRef
	Key         string
	ScheduledAt values.ZonedDateTime
	At          values.Instant
	NominalDate values.LocalDate
	Source      SourceKind
	Misfire     MisfireDecision
}

// MisfireDecision is the deterministic disposition attached to an
// occurrence. ON_TIME means the grace threshold was not crossed.
type MisfireDecision string

const (
	MisfireOnTime      MisfireDecision = "ON_TIME"
	MisfireFire        MisfireDecision = "FIRE_NOW"
	MisfireSkipped     MisfireDecision = "SKIP"
	MisfireCatch       MisfireDecision = "CATCH_UP"
	MisfireNeedsReview MisfireDecision = "REVIEW"
)

// OccurrenceResult is immutable by convention: all values are copied before
// return, and Digest is over the request, exact resolved zoned values, keys,
// and misfire decisions.
type OccurrenceResult struct {
	Trigger     TriggerRef
	Window      OccurrenceWindow
	Occurrences []Occurrence
	Digest      string
}

// CanonicalDigest returns the deterministic result digest.
func (r OccurrenceResult) CanonicalDigest() string { return r.Digest }

// Explain returns an audit-safe explanation without any provider or payload.
func (r OccurrenceResult) Explain() string {
	return fmt.Sprintf("schedule occurrences for %s: %d occurrence(s), window=%s..%s, canonical=%s",
		r.Trigger, len(r.Occurrences), r.Window.Start, r.Window.End, r.Digest)
}

// CalculateOccurrences calculates all CRON or CALENDAR occurrences of a
// published trigger in request.Window and applies its explicit misfire
// policy. EVENT sources have no time occurrences and are refused here.
func CalculateOccurrences(trigger PublishedTrigger, request OccurrenceRequest) (result OccurrenceResult, err error) {
	defer func() {
		if err != nil {
			err = wrapOccurrenceRejection(err)
		}
	}()
	if err := trigger.Verify(); err != nil {
		return OccurrenceResult{}, refuse("INVALID_TRIGGER", "trigger", ErrOccurrenceTrigger, "%v", err)
	}
	if err := request.Window.Validate(); err != nil {
		return OccurrenceResult{}, err
	}
	if request.Misfire.Policy != MisfireUnspecified || request.ResumeAt.Validate() == nil {
		if err := request.Misfire.Validate(); err != nil {
			return OccurrenceResult{}, err
		}
	}
	if request.ResumeAt.Validate() == nil && request.ResumeAt.Compare(request.Window.End) < 0 {
		return OccurrenceResult{}, refuse("INVALID_RESUME_INSTANT", "resume_at", ErrMisfireResume, "resume instant must be at or after the occurrence window end")
	}

	zone := request.Zone
	if trigger.Definition.Source.Kind == SourceCalendar {
		if request.Calendar.Zone.ID != "" {
			if zone.ID != "" && zone != request.Calendar.Zone {
				return OccurrenceResult{}, refuse("CONFLICTING_OCCURRENCE_ZONE", "zone", ErrOccurrenceZone, "request zone and calendar zone disagree")
			}
			zone = request.Calendar.Zone
		}
	}
	if err := zone.Validate(); err != nil {
		return OccurrenceResult{}, refuse("MISSING_OCCURRENCE_ZONE", "zone", ErrOccurrenceZone, "%v", err)
	}
	request.Zone = zone

	var occurrences []Occurrence
	switch trigger.Definition.Source.Kind {
	case SourceCron:
		occurrences, _ = cronOccurrences(trigger, request.Window, zone)
	case SourceCalendar:
		var err error
		occurrences, err = calendarOccurrences(trigger, request.Window, request.Calendar, zone)
		if err != nil {
			return OccurrenceResult{}, err
		}
	default:
		return OccurrenceResult{}, refuse("NON_TEMPORAL_SOURCE", "source.kind", ErrOccurrenceTrigger, "EVENT sources are resolved from events, not a time window")
	}

	if request.ResumeAt.Validate() == nil {
		var err error
		occurrences, err = applyMisfire(occurrences, request.ResumeAt, request.Misfire)
		if err != nil {
			return OccurrenceResult{}, err
		}
	}
	digest, err := occurrenceDigest(trigger, request, occurrences)
	if err != nil {
		return OccurrenceResult{}, err
	}
	return OccurrenceResult{Trigger: trigger.Ref(), Window: request.Window, Occurrences: occurrences, Digest: digest}, nil
}

// Calculate is a concise alias for CalculateOccurrences.
func Calculate(trigger PublishedTrigger, request OccurrenceRequest) (OccurrenceResult, error) {
	return CalculateOccurrences(trigger, request)
}

// ApplyMisfirePolicy applies a misfire policy to a previously calculated,
// ordered occurrence set. It is useful when resume state is known later than
// the occurrence window was calculated.
func ApplyMisfirePolicy(occurrences []Occurrence, resumeAt values.Instant, config MisfireConfig) (out []Occurrence, err error) {
	defer func() {
		if err != nil {
			err = wrapOccurrenceRejection(err)
		}
	}()
	if err := resumeAt.Validate(); err != nil {
		return nil, refuse("MISSING_RESUME_INSTANT", "resume_at", ErrMisfireResume, "%v", err)
	}
	if err := config.Validate(); err != nil {
		return nil, err
	}
	return applyMisfire(occurrences, resumeAt, config)
}

func wrapOccurrenceRejection(err error) error {
	var existing *Error
	if errors.As(err, &existing) && existing.Code == SCHED002RejectedCode {
		return err
	}
	return &Error{
		Code:   SCHED002RejectedCode,
		Field:  FieldOf(err),
		Detail: err.Error(),
		Cause:  errors.Join(ErrSCHED002Rejected, err),
	}
}

func cronOccurrences(trigger PublishedTrigger, window OccurrenceWindow, zone values.ZoneRef) ([]Occurrence, error) {
	parsed, err := ParseCron(trigger.Definition.Source.Cron.Expression)
	if err != nil {
		return nil, refuse("INVALID_CRON", "source.cron.expression", ErrCron, "%v", err)
	}
	loc, err := zone.Location()
	if err != nil {
		return nil, refuse("INVALID_OCCURRENCE_ZONE", "zone", ErrOccurrenceZone, "%v", err)
	}
	start := window.Start.Time().In(loc)
	end := window.End.Time().In(loc)
	date, err := values.NewLocalDate(start.Year(), start.Month(), start.Day())
	if err != nil {
		return nil, refuse("INVALID_OCCURRENCE_DATE", "window.start", ErrOccurrenceWindow, "%v", err)
	}
	last, err := values.NewLocalDate(end.Year(), end.Month(), end.Day())
	if err != nil {
		return nil, refuse("INVALID_OCCURRENCE_DATE", "window.end", ErrOccurrenceWindow, "%v", err)
	}
	var out []Occurrence
	for date.Compare(last) <= 0 {
		if cronDateMatches(parsed, date) {
			for minute := 0; minute < 24*60; minute++ {
				hour, min := minute/60, minute%60
				if !cronTimeMatches(parsed, hour, min) {
					continue
				}
				tod, _ := values.NewLocalTime(hour, min, 0, 0)
				resolved, err := resolveCivil(date, tod, zone)
				if err != nil {
					if errors.Is(err, values.ErrDSTGap) {
						continue // a skipped civil minute has no occurrence
					}
					return nil, refuse("DST_RESOLUTION_REQUIRED", "scheduled_at", ErrOccurrenceDST, "%v", err)
				}
				for _, zdt := range resolved {
					at := zdt.Instant()
					if at.Compare(window.Start) < 0 || at.Compare(window.End) >= 0 {
						continue
					}
					out = append(out, newOccurrence(trigger.Ref(), SourceCron, date, zdt))
				}
			}
		}
		date = date.AddDays(1)
		if !date.IsSet() {
			break
		}
	}
	sortOccurrences(out)
	return out, nil
}

func cronDateMatches(c CronExpression, date values.LocalDate) bool {
	month := int(date.Month())
	if !cronFieldMatches(c.fields[3], month) {
		return false
	}
	dom := cronFieldMatches(c.fields[2], int(date.Day()))
	weekday := int(time.Date(int(date.Year()), date.Month(), int(date.Day()), 0, 0, 0, 0, time.UTC).Weekday())
	dow := cronFieldMatches(c.fields[4], weekday) || (weekday == 0 && cronFieldMatches(c.fields[4], 7))
	// Vixie cron's OR rule applies only when both day fields are restricted.
	domStar := fieldIsStar(c.fields[2])
	dowStar := fieldIsStar(c.fields[4])
	if !domStar && !dowStar {
		return dom || dow
	}
	return dom && dow
}

func cronTimeMatches(c CronExpression, hour, minute int) bool {
	return cronFieldMatches(c.fields[0], minute) && cronFieldMatches(c.fields[1], hour)
}

func cronFieldMatches(f cronField, value int) bool {
	for _, term := range f.terms {
		if value >= term.start && value <= term.end && (value-term.start)%term.step == 0 {
			return true
		}
	}
	return false
}

func fieldIsStar(f cronField) bool {
	return len(f.terms) == 1 && f.terms[0].star && f.terms[0].start == f.min && f.terms[0].end == f.max && f.terms[0].step == 1
}

func resolveCivil(date values.LocalDate, tod values.LocalTime, zone values.ZoneRef) ([]values.ZonedDateTime, error) {
	if zdt, err := values.NewZonedDateTime(date, tod, zone, values.DisambiguationRejectGap); err == nil {
		return []values.ZonedDateTime{zdt}, nil
	} else if !errors.Is(err, values.ErrDSTAmbiguous) {
		return nil, err
	}
	earlier, err := values.NewZonedDateTime(date, tod, zone, values.DisambiguationEarlier)
	if err != nil {
		return nil, err
	}
	later, err := values.NewZonedDateTime(date, tod, zone, values.DisambiguationLater)
	if err != nil {
		return nil, err
	}
	return []values.ZonedDateTime{earlier, later}, nil
}

func calendarOccurrences(trigger PublishedTrigger, window OccurrenceWindow, calendar cycle.TenantCalendar, zone values.ZoneRef) ([]Occurrence, error) {
	if err := calendar.Validate(); err != nil {
		return nil, refuse("INVALID_CALENDAR", "calendar", ErrOccurrenceCalendar, "%v", err)
	}
	ref, rule, err := trigger.Definition.Source.Calendar.normalized()
	if err != nil {
		return nil, err
	}
	if ref != calendar.Calendar {
		return nil, refuse("CALENDAR_REVISION_MISMATCH", "calendar.version", ErrCalendarRevision, "trigger pins %s but request supplies %s", ref, calendar.Calendar)
	}
	if calendar.Zone != zone {
		return nil, refuse("CALENDAR_ZONE_MISMATCH", "zone", ErrOccurrenceZone, "calendar zone %s differs from requested zone %s", calendar.Zone, zone)
	}
	loc, err := zone.Location()
	if err != nil {
		return nil, refuse("INVALID_OCCURRENCE_ZONE", "zone", ErrOccurrenceZone, "%v", err)
	}
	start := window.Start.Time().In(loc)
	end := window.End.Time().In(loc)
	date, err := values.NewLocalDate(start.Year(), start.Month(), start.Day())
	if err != nil {
		return nil, err
	}
	last, err := values.NewLocalDate(end.Year(), end.Month(), end.Day())
	if err != nil {
		return nil, err
	}
	var out []Occurrence
	for date.Compare(last) <= 0 {
		candidate := rule
		candidate.NominalDate = date
		resolution, err := cycle.ResolveCutoff(candidate, calendar)
		if err != nil {
			return nil, refuse("CALENDAR_RESOLUTION_FAILED", "source.calendar.cutoff", ErrOccurrenceCalendar, "%v", err)
		}
		if resolution.Status != cycle.CutoffResolved {
			return nil, refuse("CALENDAR_REVIEW_REQUIRED", "source.calendar.cutoff", ErrOccurrenceDST, "%s", resolution.Rule)
		}
		at := values.NewInstant(resolution.Instant)
		if at.Compare(window.Start) >= 0 && at.Compare(window.End) < 0 {
			zdt, err := values.NewZonedDateTime(resolution.EffectiveDate, candidate.NominalTime, zone, values.DisambiguationRejectGap)
			if err != nil {
				return nil, refuse("DST_RESOLUTION_REQUIRED", "scheduled_at", ErrOccurrenceDST, "%v", err)
			}
			occ := newOccurrence(trigger.Ref(), SourceCalendar, date, zdt)
			occ.Key = calendarOccurrenceKey(trigger.Ref(), date, zdt, candidate.PhaseID)
			out = append(out, occ)
		}
		date = date.AddDays(1)
		if !date.IsSet() {
			break
		}
	}
	sortOccurrences(out)
	return out, nil
}

func newOccurrence(ref TriggerRef, source SourceKind, nominal values.LocalDate, zdt values.ZonedDateTime) Occurrence {
	return Occurrence{Trigger: ref, Key: occurrenceKey(ref, nominal, zdt), ScheduledAt: zdt, At: zdt.Instant(), NominalDate: nominal, Source: source, Misfire: MisfireOnTime}
}

func occurrenceKey(ref TriggerRef, nominal values.LocalDate, zdt values.ZonedDateTime) string {
	raw, _ := canonicalbytes.New("hcmnext.engines.schedule.OccurrenceKey", 1).
		String("tenant", ref.TenantID).String("trigger", ref.ID).String("version", ref.Version).
		Value("nominal_date", nominal).Value("scheduled_at", zdt).Bytes()
	return canonicalbytes.Digest(raw)
}

func calendarOccurrenceKey(ref TriggerRef, nominal values.LocalDate, zdt values.ZonedDateTime, phase string) string {
	raw, _ := canonicalbytes.New("hcmnext.engines.schedule.CalendarOccurrenceKey", 1).
		String("tenant", ref.TenantID).String("trigger", ref.ID).String("version", ref.Version).
		String("phase", phase).Value("nominal_date", nominal).Value("scheduled_at", zdt).Bytes()
	return canonicalbytes.Digest(raw)
}

func sortOccurrences(out []Occurrence) {
	sort.Slice(out, func(i, j int) bool {
		if cmp := out[i].At.Compare(out[j].At); cmp != 0 {
			return cmp < 0
		}
		return out[i].Key < out[j].Key
	})
}

func applyMisfire(in []Occurrence, resumeAt values.Instant, config MisfireConfig) ([]Occurrence, error) {
	if err := resumeAt.Validate(); err != nil {
		return nil, refuse("MISSING_RESUME_INSTANT", "resume_at", ErrMisfireResume, "%v", err)
	}
	out := make([]Occurrence, 0, len(in))
	missed := make([]Occurrence, 0)
	for _, occ := range in {
		graceDeadline := values.NewInstant(occ.At.Time().Add(config.Grace))
		if graceDeadline.Compare(resumeAt) < 0 {
			missed = append(missed, occ)
			continue
		}
		occ.Misfire = MisfireOnTime
		out = append(out, occ)
	}
	if len(missed) == 0 {
		return out, nil
	}
	switch config.Policy {
	case MisfireFireNow:
		chosen := missed[len(missed)-1]
		chosen.Misfire = MisfireFire
		out = append([]Occurrence{chosen}, out...)
	case MisfireSkip:
		for _, occ := range missed {
			occ.Misfire = MisfireSkipped
		}
	case MisfireCatchUp, MisfireCatchUpAll:
		if uint32(len(missed)) > config.MaxCatchUp {
			missed = missed[len(missed)-int(config.MaxCatchUp):]
		}
		for i := range missed {
			missed[i].Misfire = MisfireCatch
		}
		out = append(missed, out...)
	case MisfireCatchUpOnce:
		chosen := missed[len(missed)-1]
		chosen.Misfire = MisfireCatch
		out = append([]Occurrence{chosen}, out...)
	case MisfireReview:
		return nil, refuse("MISFIRE_REVIEW_REQUIRED", "misfire.policy", ErrMisfirePolicy, "%d occurrence(s) exceeded grace", len(missed))
	}
	sortOccurrences(out)
	return out, nil
}

func occurrenceDigest(trigger PublishedTrigger, request OccurrenceRequest, occurrences []Occurrence) (string, error) {
	w := canonicalbytes.New("hcmnext.engines.schedule.OccurrenceResult", 1).
		String("trigger_digest", trigger.Digest).
		Value("window_start", request.Window.Start).Value("window_end", request.Window.End).
		String("zone", request.Zone.String()).String("misfire_policy", string(request.Misfire.Policy)).
		Int("misfire_grace_nanos", int64(request.Misfire.Grace)).Int("misfire_max_catch_up", int64(request.Misfire.MaxCatchUp)).
		String("resume_at", request.ResumeAt.String()).String("calendar", request.Calendar.Calendar.String()).
		String("calendar_zone", request.Calendar.Zone.String())
	weekdays := make([]string, 0, len(request.Calendar.WorkingWeekdays))
	for weekday, working := range request.Calendar.WorkingWeekdays {
		weekdays = append(weekdays, fmt.Sprintf("%d=%t", weekday, working))
	}
	sort.Strings(weekdays)
	w.Count("working_weekdays", len(weekdays))
	for _, weekday := range weekdays {
		w.String("working_weekday", weekday)
	}
	holidays := append([]cycle.Holiday(nil), request.Calendar.Holidays...)
	sort.Slice(holidays, func(i, j int) bool {
		if cmp := holidays[i].Date.Compare(holidays[j].Date); cmp != 0 {
			return cmp < 0
		}
		if holidays[i].JurisdictionRef != holidays[j].JurisdictionRef {
			return holidays[i].JurisdictionRef < holidays[j].JurisdictionRef
		}
		return holidays[i].Name < holidays[j].Name
	})
	w.Count("holidays", len(holidays))
	for _, holiday := range holidays {
		w.Value("holiday_date", holiday.Date).String("holiday_jurisdiction", holiday.JurisdictionRef).String("holiday_name", holiday.Name)
	}
	w.Count("occurrences", len(occurrences))
	for _, occ := range occurrences {
		w.String("key", occ.Key).Value("scheduled_at", occ.ScheduledAt).Value("at", occ.At).
			String("source", string(occ.Source)).String("misfire", string(occ.Misfire))
	}
	return w.Digest()
}

// Explain is the package-level ARCH-GO-009 explanation for the additive
// occurrence contract.
func ExplainOccurrences() string {
	return "schedule occurrences are canonical, tenant-zoned CRON/CALENDAR values; DST folds retain both disambiguated instants, gaps are skipped, and misfires are explicit and bounded"
}
