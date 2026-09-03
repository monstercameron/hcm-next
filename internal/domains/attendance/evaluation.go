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
)

const contractVersion = 1

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
	ID  string
	Ref VersionedRef
	At  time.Time
}
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
	for _, s := range r.Schedule.Shifts {
		if s.ID == "" {
			return fmt.Errorf("%w: shift id", ErrInvalidEvidence)
		}
		if err := s.Interval.Validate(); err != nil {
			return err
		}
	}
	if len(r.Punches) == 0 {
		return fmt.Errorf("%w: punches", ErrMissingEvidence)
	}
	for _, p := range r.Punches {
		if p.ID == "" || p.At.IsZero() {
			return fmt.Errorf("%w: punch", ErrInvalidEvidence)
		}
		if err := p.Ref.Validate("punch"); err != nil {
			return err
		}
	}
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

type ShiftResult struct {
	ShiftID     string
	Outcome     Outcome
	Late, Early time.Duration
	Reason      string
}
type Result struct {
	Outcome                                  Outcome
	Schedule, Jurisdiction, Rules, Tolerance VersionedRef
	Context                                  EffectiveContext
	Shifts                                   []ShiftResult
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
	sort.SliceStable(shifts, func(i, j int) bool { return shifts[i].Interval.Start.Before(shifts[j].Interval.Start) })
	res := Result{Outcome: Compliant, Schedule: req.Schedule.Ref, Jurisdiction: req.Jurisdiction.Ref, Rules: req.Rules.Ref, Tolerance: req.Tolerance.Ref, Context: req.Context}
	used := make(map[string]bool)
	for _, s := range shifts {
		var match *Punch
		for i := range req.Punches {
			p := &req.Punches[i]
			if !used[p.ID] && !p.At.Before(s.Interval.Start.Add(-req.Tolerance.Late)) && !p.At.After(s.Interval.End.Add(req.Tolerance.Early)) {
				if match != nil {
					res.Outcome = Exception
					res.Shifts = append(res.Shifts, ShiftResult{ShiftID: s.ID, Outcome: Exception, Reason: "ambiguous punch evidence"})
					match = nil
					break
				}
				match = p
			}
		}
		if match == nil {
			if res.Outcome != Exception {
				res.Outcome = Unknown
			}
			res.Shifts = append(res.Shifts, ShiftResult{ShiftID: s.ID, Outcome: res.Outcome, Reason: "no unambiguous punch evidence"})
			continue
		}
		used[match.ID] = true
		late := match.At.Sub(s.Interval.Start)
		early := s.Interval.Start.Sub(match.At)
		if late < 0 {
			late = 0
		}
		if early < 0 {
			early = 0
		}
		state := Compliant
		reason := "within pinned schedule and tolerance"
		if late > req.Tolerance.Late || early > req.Tolerance.Early {
			state = Exception
			reason = "punch outside tolerance"
			res.Outcome = Exception
		}
		res.Shifts = append(res.Shifts, ShiftResult{ShiftID: s.ID, Outcome: state, Late: late, Early: early, Reason: reason})
	}
	res.InputDigest = digest(req)
	return res, nil
}

// EvaluateAttendance is the domain-named spelling of Evaluate.
func EvaluateAttendance(req EvaluationRequest) (AttendanceEvaluation, error) {
	return Evaluate(req)
}

func digest(req Request) string {
	b, _ := json.Marshal(req)
	h := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(h[:])
}
