// Package availability defines the shared workforce availability facts.
//
// Availability is an effective-dated, versioned fact. It is not derived from
// employment status and it is deliberately separate from authored schedules.
// AbsenceImpact describes the effect of a requested/approved absence on a
// schedule; it does not mutate either the schedule or the availability stream.
package availability

import (
	"errors"
	"fmt"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

const (
	KindWorker     values.Kind = "worker"
	KindEmployment values.Kind = "employment"
	KindAssignment values.Kind = "assignment"
)

// State is the worker's availability at an effective interval.
type State string

const (
	Available   State = "AVAILABLE"
	Unavailable State = "UNAVAILABLE"
	Restricted  State = "RESTRICTED"
	Unknown     State = "UNKNOWN"
)

func (s State) Valid() bool {
	return s == Available || s == Unavailable || s == Restricted || s == Unknown
}

// Scope identifies which assignment context the fact applies to. A worker
// scope may omit Assignment; assignment scope must carry one.
type Scope string

const (
	WorkerScope     Scope = "WORKER"
	AssignmentScope Scope = "ASSIGNMENT"
)

// Source records the authority and versioned datasets used to assert a fact.
// Schedule provenance belongs here, rather than being folded into State.
type Source struct {
	Authority   values.EntityRef
	Revision    values.RevisionToken
	TimezoneID  string
	CalendarRef string
	ConsentRef  string
}

func (s Source) Validate() error {
	if err := s.Authority.Validate(); err != nil {
		return fmt.Errorf("availability source authority: %w", err)
	}
	if err := s.Revision.Validate(); err != nil {
		return fmt.Errorf("availability source revision: %w", err)
	}
	if s.TimezoneID == "" {
		return errors.New("availability source timezone is required")
	}
	if s.CalendarRef == "" {
		return errors.New("availability source calendar is required")
	}
	return nil
}

// WorkerAvailability is one immutable revision of an availability fact.
// Employment and assignment references preserve identity; their lifecycle
// status is intentionally not copied into this contract.
type WorkerAvailability struct {
	AvailabilityID values.EntityRef
	// Revision is the append-only availability stream revision. Source.Revision
	// identifies the authority snapshot used to assert this revision.
	Revision   values.RevisionToken
	Worker     values.EntityRef
	Employment values.EntityRef
	Assignment values.EntityRef
	Scope      Scope
	State      State
	Reason     string
	Effective  values.EffectiveInterval
	Source     Source
}

func (a WorkerAvailability) Validate() error {
	if err := requireKind(a.AvailabilityID, "availability", "availability"); err != nil {
		return err
	}
	if err := a.Revision.Validate(); err != nil {
		return fmt.Errorf("availability revision: %w", err)
	}
	if err := requireKind(a.Worker, "worker", string(KindWorker)); err != nil {
		return err
	}
	if err := requireKind(a.Employment, "employment", string(KindEmployment)); err != nil {
		return err
	}
	if a.Assignment.Id != "" {
		if err := requireKind(a.Assignment, "assignment", string(KindAssignment)); err != nil {
			return err
		}
	}
	if a.Worker.Tenant != a.Employment.Tenant || (a.Assignment.Id != "" && a.Worker.Tenant != a.Assignment.Tenant) {
		return errors.New("availability identities must share a tenant")
	}
	if a.Scope != WorkerScope && a.Scope != AssignmentScope {
		return errors.New("availability scope must be WORKER or ASSIGNMENT")
	}
	if a.Scope == WorkerScope && a.Assignment.Id != "" {
		return errors.New("worker-scoped availability cannot carry an assignment")
	}
	if a.Scope == AssignmentScope && a.Assignment.Id == "" {
		return errors.New("assignment-scoped availability requires an assignment")
	}
	if !a.State.Valid() {
		return fmt.Errorf("invalid availability state %q", a.State)
	}
	if a.Reason == "" {
		return errors.New("availability reason is required")
	}
	if err := a.Effective.Validate(); err != nil {
		return fmt.Errorf("availability effective interval: %w", err)
	}
	return a.Source.Validate()
}

// ApprovalState describes the lifecycle of an absence request independently
// from the resulting availability state.
type ApprovalState string

const (
	AbsenceRequested ApprovalState = "REQUESTED"
	AbsenceApproved  ApprovalState = "APPROVED"
	AbsenceDeclined  ApprovalState = "DECLINED"
)

// CoverageEffect is a typed, descriptive effect for staffing consumers. It
// carries no authority to change a schedule or create a replacement shift.
type CoverageEffect struct {
	Kind        string
	Interval    values.EffectiveInterval
	Description string
}

func (e CoverageEffect) Validate() error {
	if e.Kind == "" {
		return errors.New("coverage effect kind is required")
	}
	if err := e.Interval.Validate(); err != nil {
		return fmt.Errorf("coverage effect interval: %w", err)
	}
	return nil
}

// AbsenceImpact keeps requested and approved intervals distinct and records
// the schedule source and resulting coverage conditions for simulation/readers.
type AbsenceImpact struct {
	ImpactID          values.EntityRef
	Worker            values.EntityRef
	Employment        values.EntityRef
	Assignment        values.EntityRef
	RequestedInterval values.EffectiveInterval
	ApprovedInterval  values.EffectiveInterval
	Approval          ApprovalState
	Reason            string
	ScheduleSource    Source
	CoverageEffects   []CoverageEffect
}

func (i AbsenceImpact) Validate() error {
	if err := requireKind(i.ImpactID, "impact", "absence_impact"); err != nil {
		return err
	}
	if err := requireKind(i.Worker, "worker", string(KindWorker)); err != nil {
		return err
	}
	if err := requireKind(i.Employment, "employment", string(KindEmployment)); err != nil {
		return err
	}
	if err := requireKind(i.Assignment, "assignment", string(KindAssignment)); err != nil {
		return err
	}
	if i.Worker.Tenant != i.Employment.Tenant || i.Worker.Tenant != i.Assignment.Tenant {
		return errors.New("absence identities must share a tenant")
	}
	if err := i.RequestedInterval.Validate(); err != nil {
		return fmt.Errorf("absence requested interval: %w", err)
	}
	if i.Approval != AbsenceRequested && i.Approval != AbsenceApproved && i.Approval != AbsenceDeclined {
		return fmt.Errorf("invalid absence approval state %q", i.Approval)
	}
	if i.Reason == "" {
		return errors.New("absence reason is required")
	}
	if i.Approval == AbsenceApproved {
		if err := i.ApprovedInterval.Validate(); err != nil {
			return fmt.Errorf("absence approved interval: %w", err)
		}
	} else if i.ApprovedInterval.Kind() != 0 {
		return errors.New("only approved absences may carry an approved interval")
	}
	if err := i.ScheduleSource.Validate(); err != nil {
		return fmt.Errorf("absence schedule source: %w", err)
	}
	for n, effect := range i.CoverageEffects {
		if err := effect.Validate(); err != nil {
			return fmt.Errorf("coverage effect %d: %w", n, err)
		}
	}
	return nil
}

func requireKind(ref values.EntityRef, label, kind string) error {
	if err := ref.Validate(); err != nil {
		return fmt.Errorf("%s reference: %w", label, err)
	}
	if string(ref.Kind) != kind {
		return fmt.Errorf("%s reference kind is %q, want %q", label, ref.Kind, kind)
	}
	return nil
}
