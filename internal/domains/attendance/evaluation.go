// Package attendance owns the versioned, pure attendance-evaluation contract.
// An evaluation is evidence, not a command: it never writes state or infers a
// missing punch, schedule, break, rule, or jurisdiction.
package attendance

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"
)

var (
	ErrMissingEvidence = errors.New("attendance: required evidence is missing")
	ErrInvalidEvidence = errors.New("attendance: evidence is invalid")
	// ErrRejected identifies an ATTEND-002 input refusal. It is deliberately
	// separate from invalid evidence so callers can fail closed without
	// treating a rejected evaluation as an attendance finding.
	ErrRejected = errors.New("ATTEND_002_REJECTED")
)

const contractVersion = 2

// Version is the attendance contract version. A change to the meaning or
// shape of an evaluation must advance this value.
func Version() int { return contractVersion }

type Outcome string

const (
	Compliant Outcome = "COMPLIANT"
	Unknown   Outcome = "UNKNOWN"
	Exception Outcome = "EXCEPTION"
)

// Status is retained as a semantic synonym for callers that model outcomes as
// statuses. EvaluationOutcome is likewise a convenient explicit name.
type Status = Outcome
type EvaluationOutcome = Outcome

// VersionedRef pins an immutable source revision. Version is intentionally a
// string: source systems may use sequence, hash, or published semantic versions.
type VersionedRef struct{ ID, Version string }

func (r VersionedRef) Validate(name string) error {
	if r.ID == "" || r.Version == "" {
		return fmt.Errorf("%w: %s requires id and version", ErrMissingEvidence, name)
	}
	return nil
}

type Interval struct{ Start, End time.Time }

func (i Interval) Validate() error {
	if i.Start.IsZero() || i.End.IsZero() {
		return ErrMissingEvidence
	}
	if !i.End.After(i.Start) {
		return fmt.Errorf("%w: interval must be non-empty", ErrInvalidEvidence)
	}
	return nil
}

type Shift struct {
	ID       string
	Interval Interval
}
type Schedule struct {
	Ref    VersionedRef
	Shifts []Shift
}
type Punch struct {
	ID        string
	Ref       VersionedRef
	At        time.Time
	Direction PunchDirection
}

// PunchDirection is optional for backwards compatibility. Directionless
// punches retain the established legacy envelope semantics; once a source
// supplies IN/OUT, the sides are evaluated explicitly.
type PunchDirection string

const (
	PunchUnknown PunchDirection = "UNKNOWN"
	PunchIn      PunchDirection = "IN"
	PunchOut     PunchDirection = "OUT"
)

type Break struct {
	ID       string
	Ref      VersionedRef
	Interval Interval
}
type Jurisdiction struct {
	Ref  VersionedRef
	Code string
}
type RuleSet struct {
	Ref  VersionedRef
	Name string
}
type Tolerance struct {
	Ref                VersionedRef
	Late, Early, Break time.Duration
}
type EffectiveContext struct {
	EffectiveAt, KnownAt time.Time
	Timezone, Calendar   string
}

func (c EffectiveContext) Validate() error {
	if c.EffectiveAt.IsZero() || c.KnownAt.IsZero() {
		return fmt.Errorf("%w: effective context requires effective_at and known_at", ErrMissingEvidence)
	}
	if c.Timezone == "" || c.Calendar == "" {
		return fmt.Errorf("%w: effective context requires timezone and calendar", ErrMissingEvidence)
	}
	if c.KnownAt.Before(c.EffectiveAt) {
		return fmt.Errorf("%w: known_at precedes effective_at", ErrInvalidEvidence)
	}
	return nil
}

// Request is the complete pinned input to one attendance evaluation.
type Request struct {
	WorkerID     string
	Schedule     Schedule
	Punches      []Punch
	Breaks       []Break
	Jurisdiction Jurisdiction
	Rules        RuleSet
	Tolerance    Tolerance
	Context      EffectiveContext
}

type EvaluationRequest = Request

func (r Request) Validate() error {
	if r.WorkerID == "" {
		return fmt.Errorf("%w: worker id", ErrMissingEvidence)
	}
	if err := r.Schedule.Ref.Validate("schedule"); err != nil {
		return err
	}
	if len(r.Schedule.Shifts) == 0 {
		return fmt.Errorf("%w: schedule has no shifts", ErrMissingEvidence)
	}
	shiftIDs := make(map[string]struct{}, len(r.Schedule.Shifts))
	for _, s := range r.Schedule.Shifts {
		if s.ID == "" {
			return fmt.Errorf("%w: shift id", ErrInvalidEvidence)
		}
		if _, exists := shiftIDs[s.ID]; exists {
			return rejected("schedule.shifts.id", "duplicate:"+s.ID, r.Schedule.Ref.Version)
		}
		shiftIDs[s.ID] = struct{}{}
		if err := s.Interval.Validate(); err != nil {
			return err
		}
	}
	orderedShifts := append([]Shift(nil), r.Schedule.Shifts...)
	sort.SliceStable(orderedShifts, func(i, j int) bool {
		if orderedShifts[i].Interval.Start.Equal(orderedShifts[j].Interval.Start) {
			return orderedShifts[i].ID < orderedShifts[j].ID
		}
		return orderedShifts[i].Interval.Start.Before(orderedShifts[j].Interval.Start)
	})
	for i := 1; i < len(orderedShifts); i++ {
		if orderedShifts[i].Interval.Start.Before(orderedShifts[i-1].Interval.End) {
			state := orderedShifts[i-1].ID + ":" + orderedShifts[i].ID
			return rejected("schedule.shifts.interval", "overlap:"+state, r.Schedule.Ref.Version)
		}
	}
	if len(r.Punches) == 0 {
		return fmt.Errorf("%w: punches", ErrMissingEvidence)
	}
	punchIDs := make(map[string]struct{}, len(r.Punches))
	for _, p := range r.Punches {
		if p.ID == "" || p.At.IsZero() {
			return fmt.Errorf("%w: punch", ErrInvalidEvidence)
		}
		if err := p.Ref.Validate("punch"); err != nil {
			return err
		}
		if _, exists := punchIDs[p.ID]; exists {
			return rejected("punches.id", "duplicate:"+p.ID, p.Ref.Version)
		}
		if p.Direction != "" && p.Direction != PunchUnknown && p.Direction != PunchIn && p.Direction != PunchOut {
			return fmt.Errorf("%w: punch direction %q", ErrInvalidEvidence, p.Direction)
		}
		punchIDs[p.ID] = struct{}{}
	}
	breakIDs := make(map[string]struct{}, len(r.Breaks))
	for _, b := range r.Breaks {
		if b.ID == "" {
			return fmt.Errorf("%w: break", ErrInvalidEvidence)
		}
		if err := b.Interval.Validate(); err != nil {
			return err
		}
		if err := b.Ref.Validate("break"); err != nil {
			return err
		}
		if _, exists := breakIDs[b.ID]; exists {
			return rejected("breaks.id", "duplicate:"+b.ID, b.Ref.Version)
		}
		breakIDs[b.ID] = struct{}{}
	}
	if err := r.Jurisdiction.Ref.Validate("jurisdiction"); err != nil {
		return err
	}
	if r.Jurisdiction.Code == "" {
		return fmt.Errorf("%w: jurisdiction code", ErrMissingEvidence)
	}
	if err := r.Rules.Ref.Validate("rules"); err != nil {
		return err
	}
	if err := r.Tolerance.Ref.Validate("tolerance"); err != nil {
		return err
	}
	if r.Tolerance.Late < 0 || r.Tolerance.Early < 0 || r.Tolerance.Break < 0 {
		return fmt.Errorf("%w: negative tolerance", ErrInvalidEvidence)
	}
	return r.Context.Validate()
}

func rejected(field, state, version string) error {
	return fmt.Errorf("%w: %w: field=%s state=%s version=%s", ErrRejected, ErrInvalidEvidence, field, state, version)
}

type ShiftResult struct {
	ShiftID     string
	Outcome     Outcome
	Late, Early time.Duration
	Reason      string
	Exceptions  []Finding
}

// ExceptionKind identifies the bounded set of attendance findings produced by
// this package.  Findings are evidence only; routing or persistence belongs to
// a later workflow.
type ExceptionKind string

const (
	LateException        ExceptionKind = "LATE"
	EarlyException       ExceptionKind = "EARLY"
	MissingException     ExceptionKind = "MISSING"
	UnscheduledException ExceptionKind = "UNSCHEDULED"
)

// Exception is an exact, non-overlapping interval in which attendance differs
// from the pinned schedule. Minutes is the rounded-up whole-minute magnitude
// of the interval (zero is retained for a zero-duration finding).
type Finding struct {
	Kind     ExceptionKind
	ShiftID  string
	PunchID  string
	Interval Interval
	Minutes  int
	Reason   string
}
type Result struct {
	Outcome                                  Outcome
	Schedule, Jurisdiction, Rules, Tolerance VersionedRef
	Context                                  EffectiveContext
	Shifts                                   []ShiftResult
	Exceptions                               []Finding
	InputDigest                              string
}

type AttendanceEvaluation = Result

func (r Result) Validate() error {
	if r.Outcome != Compliant && r.Outcome != Unknown && r.Outcome != Exception {
		return fmt.Errorf("%w: invalid outcome", ErrInvalidEvidence)
	}
	return nil
}

// Evaluate computes attendance from the pinned request. Punches are matched
// once to shifts in deterministic order; a missing or ambiguous match is an
// exception, never compliance.
func Evaluate(req Request) (Result, error) {
	if err := req.Validate(); err != nil {
		if errors.Is(err, ErrMissingEvidence) {
			return Result{Outcome: Unknown, Context: req.Context}, nil
		}
		return Result{Outcome: Unknown}, err
	}
	shifts := append([]Shift(nil), req.Schedule.Shifts...)
	sort.SliceStable(shifts, func(i, j int) bool {
		if shifts[i].Interval.Start.Equal(shifts[j].Interval.Start) {
			return shifts[i].ID < shifts[j].ID
		}
		return shifts[i].Interval.Start.Before(shifts[j].Interval.Start)
	})
	punches := append([]Punch(nil), req.Punches...)
	sort.SliceStable(punches, func(i, j int) bool {
		if punches[i].At.Equal(punches[j].At) {
			return punches[i].ID < punches[j].ID
		}
		return punches[i].At.Before(punches[j].At)
	})
	res := Result{Outcome: Compliant, Schedule: req.Schedule.Ref, Jurisdiction: req.Jurisdiction.Ref, Rules: req.Rules.Ref, Tolerance: req.Tolerance.Ref, Context: req.Context}
	for _, p := range punches {
		candidateIDs := make([]string, 0, 2)
		for _, s := range shifts {
			if !p.At.Before(s.Interval.Start.Add(-req.Tolerance.Late)) && !p.At.After(s.Interval.End.Add(req.Tolerance.Early)) {
				candidateIDs = append(candidateIDs, s.ID)
			}
		}
		if len(candidateIDs) > 1 {
			return Result{Outcome: Unknown}, rejected("punches.assignment", "ambiguous:"+p.ID+":"+candidateIDs[0]+":"+candidateIDs[1], p.Ref.Version)
		}
	}
	used := make(map[string]bool)
	for _, s := range shifts {
		matches := make([]*Punch, 0, 2)
		for i := range punches {
			p := &punches[i]
			if !used[p.ID] && !p.At.Before(s.Interval.Start.Add(-req.Tolerance.Late)) && !p.At.After(s.Interval.End.Add(req.Tolerance.Early)) {
				matches = append(matches, p)
			}
		}
		sort.SliceStable(matches, func(i, j int) bool {
			if matches[i].At.Equal(matches[j].At) {
				return matches[i].ID < matches[j].ID
			}
			return matches[i].At.Before(matches[j].At)
		})
		if len(matches) == 0 {
			// A valid request with no observation for a scheduled shift is a
			// detectable absence. An invalid request (including no punches) was
			// handled above as UNKNOWN due to missing evidence.
			e := exceptionFor(s.ID, MissingException, "", s.Interval, "no punch evidence for scheduled shift")
			res.Outcome = Exception
			res.Exceptions = append(res.Exceptions, e)
			res.Shifts = append(res.Shifts, ShiftResult{ShiftID: s.ID, Outcome: Exception, Reason: e.Reason, Exceptions: []Finding{e}})
			continue
		}
		explicit := make([]*Punch, 0, len(matches))
		for _, p := range matches {
			if p.Direction == PunchIn || p.Direction == PunchOut {
				explicit = append(explicit, p)
			}
		}
		if len(explicit) > 1 {
			if explicit[0].Direction == PunchOut {
				return Result{Outcome: Unknown}, rejected("punches.direction", "OUT_before_IN:"+explicit[0].ID, explicit[0].Ref.Version)
			}
			for i := 1; i < len(explicit); i++ {
				if explicit[i].Direction == explicit[i-1].Direction {
					state := "consecutive_" + string(explicit[i].Direction) + ":" + explicit[i-1].ID + ":" + explicit[i].ID
					return Result{Outcome: Unknown}, rejected("punches.direction", state, explicit[i].Ref.Version)
				}
			}
		}
		// Directioned observations form an explicit attendance envelope. A
		// directionless batch retains the legacy arrival-only/single-punch
		// behavior; an OUT-only batch never silently becomes an arrival.
		for _, p := range matches {
			used[p.ID] = true
		}
		var first, last *Punch
		directed := len(explicit) > 0
		// Recompute strictly after knowing whether this is a directioned batch;
		// unknown observations cannot fill an explicitly missing side.
		if directed {
			if explicit[0].Direction == PunchIn {
				first = explicit[0]
			}
			if explicit[len(explicit)-1].Direction == PunchOut {
				last = explicit[len(explicit)-1]
			}
		} else {
			first = matches[0]
			last = matches[len(matches)-1]
		}
		late := time.Duration(0)
		if first != nil {
			late = first.At.Sub(s.Interval.Start)
		}
		if late < 0 {
			late = 0
		}
		early := time.Duration(0)
		if last != nil && (directed || len(matches) > 1) {
			early = s.Interval.End.Sub(last.At)
			if early < 0 {
				early = 0
			}
		}
		state := Compliant
		reason := "within pinned schedule and tolerance"
		findings := make([]Finding, 0, 2)
		if directed && first == nil {
			e := exceptionFor(s.ID, MissingException, "", Interval{Start: s.Interval.Start, End: s.Interval.Start}, "missing IN arrival evidence")
			findings = append(findings, e)
			res.Exceptions = append(res.Exceptions, e)
		}
		if directed && last == nil {
			// Missing OUT is not itself an early departure: the schedule side is
			// unknown, and must not be guessed from an IN observation.
			e := exceptionFor(s.ID, MissingException, "", Interval{Start: s.Interval.End, End: s.Interval.End}, "missing OUT departure evidence")
			findings = append(findings, e)
			res.Exceptions = append(res.Exceptions, e)
		}
		if first != nil && late > req.Tolerance.Late {
			e := exceptionFor(s.ID, LateException, first.ID, Interval{Start: s.Interval.Start.Add(req.Tolerance.Late), End: first.At}, "arrival is outside late tolerance")
			findings = append(findings, e)
			res.Exceptions = append(res.Exceptions, e)
		}
		if last != nil && early > req.Tolerance.Early {
			e := exceptionFor(s.ID, EarlyException, last.ID, Interval{Start: last.At, End: s.Interval.End.Add(-req.Tolerance.Early)}, "departure is outside early tolerance")
			findings = append(findings, e)
			res.Exceptions = append(res.Exceptions, e)
		}
		if len(findings) > 0 {
			state = Exception
			reason = findings[0].Reason
			res.Outcome = Exception
		}
		res.Shifts = append(res.Shifts, ShiftResult{ShiftID: s.ID, Outcome: state, Late: late, Early: early, Reason: reason, Exceptions: findings})
	}
	for i := range punches {
		if !used[punches[i].ID] {
			s := punches[i]
			e := exceptionFor("", UnscheduledException, s.ID, Interval{Start: s.At, End: s.At}, "punch is outside every scheduled shift")
			res.Exceptions = append(res.Exceptions, e)
			res.Outcome = Exception
		}
	}
	res.InputDigest = digest(req)
	return res, nil
}

func exceptionFor(shiftID string, kind ExceptionKind, punchID string, interval Interval, reason string) Finding {
	d := interval.End.Sub(interval.Start)
	minutes := 0
	if d > 0 {
		minutes = int((d + time.Minute - 1) / time.Minute)
	}
	return Finding{Kind: kind, ShiftID: shiftID, PunchID: punchID, Interval: interval, Minutes: minutes, Reason: reason}
}

// EvaluateAttendance is the domain-named spelling of Evaluate.
func EvaluateAttendance(req EvaluationRequest) (AttendanceEvaluation, error) {
	return Evaluate(req)
}

func digest(req Request) string {
	canonical := req
	canonical.Schedule.Shifts = append([]Shift(nil), req.Schedule.Shifts...)
	sort.SliceStable(canonical.Schedule.Shifts, func(i, j int) bool {
		if canonical.Schedule.Shifts[i].Interval.Start.Equal(canonical.Schedule.Shifts[j].Interval.Start) {
			return canonical.Schedule.Shifts[i].ID < canonical.Schedule.Shifts[j].ID
		}
		return canonical.Schedule.Shifts[i].Interval.Start.Before(canonical.Schedule.Shifts[j].Interval.Start)
	})
	canonical.Punches = append([]Punch(nil), req.Punches...)
	sort.SliceStable(canonical.Punches, func(i, j int) bool {
		if canonical.Punches[i].At.Equal(canonical.Punches[j].At) {
			return canonical.Punches[i].ID < canonical.Punches[j].ID
		}
		return canonical.Punches[i].At.Before(canonical.Punches[j].At)
	})
	canonical.Breaks = append([]Break(nil), req.Breaks...)
	sort.SliceStable(canonical.Breaks, func(i, j int) bool { return canonical.Breaks[i].ID < canonical.Breaks[j].ID })
	b, _ := json.Marshal(canonical)
	h := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(h[:])
}
