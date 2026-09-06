package availability

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/monstercameron/hcm-next/internal/engines/canonicalbytes"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

// ScheduleHoliday is a holiday fact scoped to the jurisdiction selected by an
// absence simulation. A holiday for another jurisdiction never suppresses a
// shift in this simulation.
type ScheduleHoliday struct {
	Date            values.LocalDate
	JurisdictionRef string
	Name            string
}

// VersionedWorkSchedule is the complete schedule input for AVAIL-002. It is a
// value, not a store handle: every revision, calendar, weekday rule, holiday,
// shift and timezone needed by the simulation is explicit.
type VersionedWorkSchedule struct {
	ScheduleID      values.EntityRef
	Assignment      values.EntityRef
	Revision        values.RevisionToken
	Calendar        values.CalendarRef
	Timezone        values.ZoneRef
	WorkingWeekdays map[time.Weekday]bool
	Shifts          []WorkShift
	Holidays        []ScheduleHoliday
}

// ExistingAbsence is an already-known absence used only for overlap reporting.
// It cannot mutate or reserve schedule time.
type ExistingAbsence struct {
	ID         string
	Worker     values.EntityRef
	Assignment values.EntityRef
	Window     values.EffectiveInterval
	Approval   ApprovalState
}

// AbsenceWindowRequest names the local-date absence window and all governed
// inputs used to simulate it.
type AbsenceWindowRequest struct {
	Worker                   values.EntityRef
	Employment               values.EntityRef
	Assignment               values.EntityRef
	Window                   values.EffectiveInterval
	JurisdictionRef          string
	Schedule                 VersionedWorkSchedule
	ExistingAbsences         []ExistingAbsence
	ExpectedScheduleRevision values.RevisionToken
}

// ScheduledAbsenceShift is one exact resolved shift consumed by the window.
type ScheduledAbsenceShift struct {
	ShiftID  string
	Interval values.EffectiveInterval
	Hours    float64
}

// ScheduledAbsenceDay groups consumed shifts by the schedule's civil date.
type ScheduledAbsenceDay struct {
	Date   values.LocalDate
	Shifts []ScheduledAbsenceShift
	Hours  float64
}

// SkippedScheduleDay records a date intentionally excluded from consumption.
type SkippedScheduleDay struct {
	Date   values.LocalDate
	Reason string
}

// AbsenceWindowConflict names an existing-absence overlap or an unresolved
// schedule fact. Unknown shift time never becomes invented scheduled hours.
type AbsenceWindowConflict struct {
	Code      string
	Reference string
	Detail    string
}

// AbsenceWindowResult is the immutable, digested impact projection. No field
// in this result grants authority to change the schedule, availability, or a
// balance.
type AbsenceWindowResult struct {
	ScheduleID       values.EntityRef
	ScheduleRevision values.RevisionToken
	Calendar         values.CalendarRef
	JurisdictionRef  string
	Timezone         values.ZoneRef
	Days             []ScheduledAbsenceDay
	Skipped          []SkippedScheduleDay
	Hours            float64
	Conflicts        []AbsenceWindowConflict
	Coverage         CoverageDemandCondition
	Digest           string
}

var (
	ErrAbsenceWindowInvalid = errors.New("availability: invalid versioned absence window")
	ErrAbsenceWindowStale   = errors.New("availability: schedule revision is stale")
)

// SimulateAbsenceWindow resolves a local-date absence against one explicit
// schedule revision. It is pure and deterministic: it reads no clock,
// timezone default, calendar registry, schedule store, availability stream or
// balance account.
func SimulateAbsenceWindow(req AbsenceWindowRequest) (AbsenceWindowResult, error) {
	if err := validateAbsenceWindowRequest(req); err != nil {
		return AbsenceWindowResult{}, err
	}
	if req.ExpectedScheduleRevision.IsSpecified() && !req.ExpectedScheduleRevision.Equal(req.Schedule.Revision) {
		return AbsenceWindowResult{}, ErrAbsenceWindowStale
	}
	start, _ := req.Window.StartDate()
	end, _ := req.Window.EndDate()
	result := AbsenceWindowResult{
		ScheduleID:       req.Schedule.ScheduleID,
		ScheduleRevision: req.Schedule.Revision,
		Calendar:         req.Schedule.Calendar,
		JurisdictionRef:  req.JurisdictionRef,
		Timezone:         req.Schedule.Timezone,
		Coverage:         CoverageNotAffected,
	}

	for _, existing := range req.ExistingAbsences {
		if existing.Approval == AbsenceDeclined || existing.Worker != req.Worker || existing.Assignment != req.Assignment {
			continue
		}
		overlaps, err := req.Window.Overlaps(existing.Window)
		if err != nil {
			return AbsenceWindowResult{}, fmt.Errorf("%w: existing absence %q: %v", ErrAbsenceWindowInvalid, existing.ID, err)
		}
		if overlaps {
			result.Conflicts = append(result.Conflicts, AbsenceWindowConflict{Code: "EXISTING_ABSENCE_OVERLAP", Reference: existing.ID, Detail: "requested absence overlaps an existing absence"})
		}
	}

	for date := start; date.Compare(end) < 0; date = date.AddDays(1) {
		if working, reason := req.Schedule.isWorkingDate(date, req.JurisdictionRef); !working {
			result.Skipped = append(result.Skipped, SkippedScheduleDay{Date: date, Reason: reason})
			continue
		}
		day := ScheduledAbsenceDay{Date: date}
		for _, shift := range req.Schedule.Shifts {
			if shift.Date != date {
				continue
			}
			z1, err1 := values.NewZonedDateTime(shift.Date, shift.Start, req.Schedule.Timezone, shift.Disambiguation)
			z2, err2 := values.NewZonedDateTime(shift.Date, shift.End, req.Schedule.Timezone, shift.Disambiguation)
			if err1 != nil || err2 != nil {
				result.Conflicts = append(result.Conflicts, AbsenceWindowConflict{Code: "DST_OR_TIME_ERROR", Reference: shift.ID, Detail: "shift cannot be resolved without inventing hours"})
				continue
			}
			if z1.Instant().Compare(z2.Instant()) >= 0 {
				result.Conflicts = append(result.Conflicts, AbsenceWindowConflict{Code: "UNKNOWN_SHIFT", Reference: shift.ID, Detail: "shift end must be after shift start on its schedule date"})
				continue
			}
			interval, err := values.NewInstantInterval(z1.Instant(), z2.Instant())
			if err != nil {
				result.Conflicts = append(result.Conflicts, AbsenceWindowConflict{Code: "INVALID_INTERVAL", Reference: shift.ID, Detail: err.Error()})
				continue
			}
			hours := z2.Instant().Time().Sub(z1.Instant().Time()).Hours()
			day.Shifts = append(day.Shifts, ScheduledAbsenceShift{ShiftID: shift.ID, Interval: interval, Hours: hours})
			day.Hours += hours
		}
		sort.Slice(day.Shifts, func(i, j int) bool {
			a, _ := day.Shifts[i].Interval.StartInstant()
			b, _ := day.Shifts[j].Interval.StartInstant()
			return a.Compare(b) < 0
		})
		for i := 1; i < len(day.Shifts); i++ {
			priorEnd, _ := day.Shifts[i-1].Interval.EndInstant()
			currentStart, _ := day.Shifts[i].Interval.StartInstant()
			if priorEnd.Compare(currentStart) > 0 {
				result.Conflicts = append(result.Conflicts, AbsenceWindowConflict{Code: "OVERLAPPING_SHIFT", Reference: day.Shifts[i].ShiftID, Detail: "overlapping assignment shifts are ambiguous"})
			}
		}
		if len(day.Shifts) == 0 {
			result.Skipped = append(result.Skipped, SkippedScheduleDay{Date: date, Reason: "NO_SHIFT_AUTHORED"})
			continue
		}
		result.Hours += day.Hours
		result.Days = append(result.Days, day)
	}
	if result.Hours > 0 {
		result.Coverage = CoverageDemandCreated
	}
	if len(result.Conflicts) > 0 {
		result.Coverage = CoverageUnknown
	}
	var err error
	result.Digest, err = absenceWindowDigest(result)
	if err != nil {
		return AbsenceWindowResult{}, fmt.Errorf("%w: digest: %v", ErrAbsenceWindowInvalid, err)
	}
	return result, nil
}

// SimulateVersionedAbsence is an explicit alias for callers that want the
// schedule-version boundary visible at the call site.
func SimulateVersionedAbsence(req AbsenceWindowRequest) (AbsenceWindowResult, error) {
	return SimulateAbsenceWindow(req)
}

func (s VersionedWorkSchedule) Validate() error {
	if err := requireKind(s.ScheduleID, "schedule", "schedule"); err != nil {
		return err
	}
	if err := requireKind(s.Assignment, "assignment", string(KindAssignment)); err != nil {
		return err
	}
	if s.ScheduleID.Tenant != s.Assignment.Tenant {
		return errors.New("schedule identities must share a tenant")
	}
	if err := s.Revision.Validate(); err != nil {
		return fmt.Errorf("schedule revision: %w", err)
	}
	if err := s.Calendar.Validate(); err != nil {
		return fmt.Errorf("schedule calendar: %w", err)
	}
	if err := s.Timezone.Validate(); err != nil {
		return fmt.Errorf("schedule timezone: %w", err)
	}
	working := false
	for _, enabled := range s.WorkingWeekdays {
		working = working || enabled
	}
	if !working {
		return errors.New("schedule must declare at least one working weekday")
	}
	seen := make(map[string]struct{}, len(s.Shifts))
	for _, shift := range s.Shifts {
		if shift.ID == "" {
			return errors.New("schedule shift id is required")
		}
		if _, exists := seen[shift.ID]; exists {
			return fmt.Errorf("schedule shift %q is duplicated", shift.ID)
		}
		seen[shift.ID] = struct{}{}
		if err := shift.Date.Validate(); err != nil {
			return fmt.Errorf("shift %s date: %w", shift.ID, err)
		}
		if err := shift.Start.Validate(); err != nil {
			return fmt.Errorf("shift %s start: %w", shift.ID, err)
		}
		if err := shift.End.Validate(); err != nil {
			return fmt.Errorf("shift %s end: %w", shift.ID, err)
		}
		if !shift.Disambiguation.Valid() {
			return fmt.Errorf("shift %s requires explicit DST disambiguation", shift.ID)
		}
	}
	for _, holiday := range s.Holidays {
		if err := holiday.Date.Validate(); err != nil {
			return fmt.Errorf("holiday date: %w", err)
		}
		if strings.TrimSpace(holiday.JurisdictionRef) == "" {
			return errors.New("holiday jurisdiction is required")
		}
		if strings.TrimSpace(holiday.Name) == "" {
			return errors.New("holiday name is required")
		}
	}
	return nil
}

func (s VersionedWorkSchedule) isWorkingDate(date values.LocalDate, jurisdiction string) (bool, string) {
	weekday := time.Date(int(date.Year()), date.Month(), int(date.Day()), 0, 0, 0, 0, time.UTC).Weekday()
	if !s.WorkingWeekdays[weekday] {
		return false, fmt.Sprintf("%s is a non-working weekday", date)
	}
	for _, holiday := range s.Holidays {
		if holiday.Date == date && holiday.JurisdictionRef == jurisdiction {
			return false, fmt.Sprintf("%s is holiday %q for jurisdiction %q", date, holiday.Name, jurisdiction)
		}
	}
	return true, ""
}

func validateAbsenceWindowRequest(req AbsenceWindowRequest) error {
	if err := requireKind(req.Worker, "worker", string(KindWorker)); err != nil {
		return fmt.Errorf("%w: %v", ErrAbsenceWindowInvalid, err)
	}
	if err := requireKind(req.Employment, "employment", string(KindEmployment)); err != nil {
		return fmt.Errorf("%w: %v", ErrAbsenceWindowInvalid, err)
	}
	if err := requireKind(req.Assignment, "assignment", string(KindAssignment)); err != nil {
		return fmt.Errorf("%w: %v", ErrAbsenceWindowInvalid, err)
	}
	if req.Worker.Tenant != req.Employment.Tenant || req.Worker.Tenant != req.Assignment.Tenant {
		return fmt.Errorf("%w: identities must share a tenant", ErrAbsenceWindowInvalid)
	}
	if req.Assignment != req.Schedule.Assignment {
		return fmt.Errorf("%w: assignment does not match schedule", ErrAbsenceWindowInvalid)
	}
	if strings.TrimSpace(req.JurisdictionRef) == "" {
		return fmt.Errorf("%w: jurisdiction is required", ErrAbsenceWindowInvalid)
	}
	if err := req.Schedule.Validate(); err != nil {
		return fmt.Errorf("%w: schedule: %v", ErrAbsenceWindowInvalid, err)
	}
	if err := req.Window.Validate(); err != nil {
		return fmt.Errorf("%w: window: %v", ErrAbsenceWindowInvalid, err)
	}
	if req.Window.Kind() != values.IntervalKindLocalDate || req.Window.IsOpenEnded() {
		return fmt.Errorf("%w: window must be a closed local-date interval", ErrAbsenceWindowInvalid)
	}
	if req.Window.Calendar() != req.Schedule.Calendar {
		return fmt.Errorf("%w: window calendar does not match schedule calendar", ErrAbsenceWindowInvalid)
	}
	for _, existing := range req.ExistingAbsences {
		if strings.TrimSpace(existing.ID) == "" {
			return fmt.Errorf("%w: existing absence id is required", ErrAbsenceWindowInvalid)
		}
		if existing.Worker != req.Worker || existing.Assignment != req.Assignment {
			continue
		}
		if existing.Approval != AbsenceRequested && existing.Approval != AbsenceApproved && existing.Approval != AbsenceDeclined {
			return fmt.Errorf("%w: existing absence %q has invalid approval", ErrAbsenceWindowInvalid, existing.ID)
		}
		if err := existing.Window.Validate(); err != nil || existing.Window.Kind() != values.IntervalKindLocalDate || existing.Window.IsOpenEnded() {
			return fmt.Errorf("%w: existing absence %q window is not a closed local-date interval", ErrAbsenceWindowInvalid, existing.ID)
		}
	}
	return nil
}

func absenceWindowDigest(result AbsenceWindowResult) (string, error) {
	w := canonicalbytes.New("hcmnext.domains.availability.AbsenceWindowResult", 1).
		Value("schedule_id", result.ScheduleID).
		Value("schedule_revision", result.ScheduleRevision).
		String("calendar", result.Calendar.String()).
		String("jurisdiction", result.JurisdictionRef).
		String("timezone", result.Timezone.String()).
		String("hours", strconv.FormatFloat(result.Hours, 'f', 9, 64)).
		String("coverage", string(result.Coverage)).
		Count("days", len(result.Days))
	for _, day := range result.Days {
		w.String("day.date", day.Date.String()).String("day.hours", strconv.FormatFloat(day.Hours, 'f', 9, 64)).Count("day.shifts", len(day.Shifts))
		for _, shift := range day.Shifts {
			w.String("shift.id", shift.ShiftID).Value("shift.interval", shift.Interval).String("shift.hours", strconv.FormatFloat(shift.Hours, 'f', 9, 64))
		}
	}
	w.Count("skipped", len(result.Skipped))
	for _, skipped := range result.Skipped {
		w.String("skipped.date", skipped.Date.String()).String("skipped.reason", skipped.Reason)
	}
	w.Count("conflicts", len(result.Conflicts))
	for _, conflict := range result.Conflicts {
		w.String("conflict.code", conflict.Code).String("conflict.reference", conflict.Reference).String("conflict.detail", conflict.Detail)
	}
	return w.Digest()
}

// Verify checks the result digest without consulting any external state.
func (r AbsenceWindowResult) Verify() bool {
	if r.Digest == "" {
		return false
	}
	digest, err := absenceWindowDigest(r)
	return err == nil && digest == r.Digest
}
