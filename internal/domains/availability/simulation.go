package availability

import (
	"errors"
	"fmt"
	"sort"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// WorkShift is an authored shift in a particular schedule revision.  Times
// are civil times and are resolved only with the schedule's explicit zone and
// tzdb version.
type WorkShift struct {
	ID             string
	Date           values.LocalDate
	Start          values.LocalTime
	End            values.LocalTime
	Disambiguation values.Disambiguation
}

// WorkSchedule is an immutable, versioned assignment schedule. Holidays and
// unscheduled dates are intentionally represented by no shift.
type WorkSchedule struct {
	ScheduleID values.EntityRef
	Assignment values.EntityRef
	Revision   values.RevisionToken
	Calendar   values.CalendarRef
	Timezone   values.ZoneRef
	Shifts     []WorkShift
	Holidays   []values.LocalDate
}

func (s WorkSchedule) Validate() error {
	if err := requireKind(s.ScheduleID, "schedule", "schedule"); err != nil {
		return err
	}
	if err := requireKind(s.Assignment, "assignment", string(KindAssignment)); err != nil {
		return err
	}
	if s.Assignment.Tenant != s.ScheduleID.Tenant {
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
	seen := map[string]bool{}
	for _, h := range s.Holidays {
		if err := h.Validate(); err != nil {
			return fmt.Errorf("holiday: %w", err)
		}
	}
	for _, sh := range s.Shifts {
		if sh.ID == "" || seen[sh.ID] {
			return errors.New("schedule shift ids must be non-empty and unique")
		}
		seen[sh.ID] = true
		if err := sh.Date.Validate(); err != nil {
			return fmt.Errorf("shift %s date: %w", sh.ID, err)
		}
		if err := sh.Start.Validate(); err != nil {
			return fmt.Errorf("shift %s start: %w", sh.ID, err)
		}
		if err := sh.End.Validate(); err != nil {
			return fmt.Errorf("shift %s end: %w", sh.ID, err)
		}
		if !sh.Disambiguation.Valid() {
			return fmt.Errorf("shift %s requires explicit DST disambiguation", sh.ID)
		}
	}
	return nil
}

type AbsenceSimulationRequest struct {
	Worker, Employment       values.EntityRef
	Absence                  AbsenceImpact
	Schedule                 WorkSchedule
	ExpectedScheduleRevision values.RevisionToken
}

type ScheduledInterval struct {
	ShiftID  string
	Interval values.EffectiveInterval
	Hours    float64
}

type CoverageDemandCondition string

const (
	CoverageNotAffected   CoverageDemandCondition = "NOT_AFFECTED"
	CoverageDemandCreated CoverageDemandCondition = "COVERAGE_DEMAND"
	CoverageUnknown       CoverageDemandCondition = "COVERAGE_UNKNOWN"
)

type SimulationConflict struct{ ShiftID, Code, Detail string }

type AbsenceSimulationResult struct {
	Intervals        []ScheduledInterval
	Hours            float64
	ScheduleRevision values.RevisionToken
	Calendar         values.CalendarRef
	Timezone         values.ZoneRef
	Conflicts        []SimulationConflict
	Coverage         CoverageDemandCondition
}

var (
	ErrSimulationInvalid = errors.New("availability: absence simulation input is invalid")
	ErrStaleSchedule     = errors.New("availability: schedule revision is stale")
)

// SimulateAbsence computes a zero-effect impact projection. It never changes
// the schedule, availability, or any other state.
func SimulateAbsence(req AbsenceSimulationRequest) (AbsenceSimulationResult, error) {
	if err := req.Schedule.Validate(); err != nil {
		return AbsenceSimulationResult{}, fmt.Errorf("%w: %v", ErrSimulationInvalid, err)
	}
	if err := requireKind(req.Worker, "worker", string(KindWorker)); err != nil {
		return AbsenceSimulationResult{}, fmt.Errorf("%w: %v", ErrSimulationInvalid, err)
	}
	if err := requireKind(req.Employment, "employment", string(KindEmployment)); err != nil {
		return AbsenceSimulationResult{}, fmt.Errorf("%w: %v", ErrSimulationInvalid, err)
	}
	if req.Worker.Tenant != req.Schedule.Assignment.Tenant || req.Employment.Tenant != req.Worker.Tenant {
		return AbsenceSimulationResult{}, fmt.Errorf("%w: identities must share a tenant", ErrSimulationInvalid)
	}
	if req.Absence.Assignment != req.Schedule.Assignment {
		return AbsenceSimulationResult{}, fmt.Errorf("%w: absence assignment does not match schedule", ErrSimulationInvalid)
	}
	if req.ExpectedScheduleRevision.IsSpecified() && !req.ExpectedScheduleRevision.Equal(req.Schedule.Revision) {
		return AbsenceSimulationResult{}, ErrStaleSchedule
	}
	if err := req.Absence.RequestedInterval.Validate(); err != nil {
		return AbsenceSimulationResult{}, fmt.Errorf("%w: absence interval: %v", ErrSimulationInvalid, err)
	}
	target := req.Absence.RequestedInterval
	if req.Absence.Approval == AbsenceApproved {
		target = req.Absence.ApprovedInterval
	}
	if req.Absence.Approval == AbsenceDeclined {
		return AbsenceSimulationResult{ScheduleRevision: req.Schedule.Revision, Calendar: req.Schedule.Calendar, Timezone: req.Schedule.Timezone, Coverage: CoverageNotAffected}, nil
	}
	start, ok := target.StartDate()
	if !ok {
		return AbsenceSimulationResult{}, fmt.Errorf("%w: absence must use local dates", ErrSimulationInvalid)
	}
	end, ok := target.EndDate()
	if !ok {
		return AbsenceSimulationResult{}, fmt.Errorf("%w: open-ended absence is not simulatable", ErrSimulationInvalid)
	}
	result := AbsenceSimulationResult{ScheduleRevision: req.Schedule.Revision, Calendar: req.Schedule.Calendar, Timezone: req.Schedule.Timezone, Coverage: CoverageNotAffected}
	for _, sh := range req.Schedule.Shifts {
		if sh.Date.Compare(start) < 0 || sh.Date.Compare(end) >= 0 || isHoliday(req.Schedule.Holidays, sh.Date) {
			continue
		}
		z1, err1 := values.NewZonedDateTime(sh.Date, sh.Start, req.Schedule.Timezone, sh.Disambiguation)
		z2, err2 := values.NewZonedDateTime(sh.Date, sh.End, req.Schedule.Timezone, sh.Disambiguation)
		if err1 != nil || err2 != nil || z1.Instant().Compare(z2.Instant()) >= 0 {
			code := "UNKNOWN_SHIFT"
			if err1 != nil || err2 != nil {
				code = "DST_OR_TIME_ERROR"
			}
			result.Conflicts = append(result.Conflicts, SimulationConflict{sh.ID, code, "shift cannot be resolved without inventing hours"})
			continue
		}
		iv, err := values.NewInstantInterval(z1.Instant(), z2.Instant())
		if err != nil {
			result.Conflicts = append(result.Conflicts, SimulationConflict{sh.ID, "INVALID_INTERVAL", err.Error()})
			continue
		}
		hours := z2.Instant().Time().Sub(z1.Instant().Time()).Hours()
		result.Intervals = append(result.Intervals, ScheduledInterval{sh.ID, iv, hours})
		result.Hours += hours
	}
	// Overlapping authored shifts are a conflict, never additive invented time.
	sort.Slice(result.Intervals, func(i, j int) bool {
		a, _ := result.Intervals[i].Interval.StartInstant()
		b, _ := result.Intervals[j].Interval.StartInstant()
		return a.Compare(b) < 0
	})
	for i := 1; i < len(result.Intervals); i++ {
		a, _ := result.Intervals[i-1].Interval.EndInstant()
		b, _ := result.Intervals[i].Interval.StartInstant()
		if a.Compare(b) > 0 {
			result.Conflicts = append(result.Conflicts, SimulationConflict{result.Intervals[i].ShiftID, "OVERLAPPING_SHIFT", "overlapping assignment shifts are ambiguous"})
		}
	}
	if len(result.Intervals) > 0 {
		result.Coverage = CoverageDemandCreated
	}
	if len(result.Conflicts) > 0 {
		result.Coverage = CoverageUnknown
	}
	return result, nil
}

func isHoliday(days []values.LocalDate, d values.LocalDate) bool {
	for _, h := range days {
		if h.Compare(d) == 0 {
			return true
		}
	}
	return false
}
