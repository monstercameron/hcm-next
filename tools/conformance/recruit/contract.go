// Package recruit contains the executable semantic contract for the
// Recruit/Hire/Onboard reference workflow. It deliberately models facts and
// downstream observations only; it does not call an ATS, HRIS, payroll or IAM
// implementation.
package recruit

import "fmt"

const (
	BusinessCompleted     = "COMPLETED"
	ConsistencyConsistent = "CONSISTENT"
	ConsistencyDegraded   = "DEGRADED"
	RepairRequired        = "REPAIR_REQUIRED"
)

// Scenario is the small, deterministic boundary shared by the conformance
// fixtures. A role may have both candidate and worker relationships.
type Scenario struct {
	CandidateID             string
	PersonID                string
	WorkerID                string
	EmploymentID            string
	OfferID                 string
	OfferApproved           bool
	OfferAccepted           bool
	OfferExpired            bool
	ApprovalsBound          bool
	PositionAvailable       bool
	BudgetAvailable         bool
	WorkAuthorizationValid  bool
	FormsAccessible         bool
	DuplicatePerson         bool
	EmploymentAlreadyExists bool
	IAMReady                bool
	PayrollReady            bool
	EquipmentReady          bool
	LearningReady           bool
	BusinessState           string
	ConsistencyState        string
	RepairState             string
}

// Result describes the business terminal state and every failed invariant.
// Errors are stable, machine-readable names so callers can report exact
// conformance dimensions without depending on implementation diagnostics.
type Result struct {
	Completed          bool
	BusinessState      string
	ConsistencyState   string
	RepairState        string
	EmploymentCreated  bool
	CandidatePreserved bool
	WorkerPreserved    bool
	Errors             []string
}

func (r Result) Valid() bool { return len(r.Errors) == 0 }

// Check evaluates the Recruit/Hire/Onboard contract. It fails closed: a
// missing prerequisite is never interpreted as a successful partial hire.
func Check(s Scenario) Result {
	r := Result{BusinessState: s.BusinessState, ConsistencyState: s.ConsistencyState, RepairState: s.RepairState}
	if s.CandidateID == "" {
		r.Errors = append(r.Errors, "candidate_missing")
	}
	if s.PersonID == "" {
		r.Errors = append(r.Errors, "person_missing")
	}
	if s.WorkerID == "" {
		r.Errors = append(r.Errors, "worker_missing")
	}
	if s.EmploymentID == "" {
		r.Errors = append(r.Errors, "employment_missing")
	}
	if s.OfferID == "" {
		r.Errors = append(r.Errors, "offer_missing")
	}
	if s.DuplicatePerson {
		r.Errors = append(r.Errors, "duplicate_person")
	}
	if !s.PositionAvailable {
		r.Errors = append(r.Errors, "position_exhausted")
	}
	if !s.BudgetAvailable {
		r.Errors = append(r.Errors, "budget_exhausted")
	}
	if s.OfferExpired {
		r.Errors = append(r.Errors, "offer_expired")
	}
	if !s.OfferApproved || !s.OfferAccepted {
		r.Errors = append(r.Errors, "offer_not_bound")
	}
	if !s.ApprovalsBound {
		r.Errors = append(r.Errors, "approvals_unbound")
	}
	if !s.WorkAuthorizationValid {
		r.Errors = append(r.Errors, "work_authorization_missing")
	}
	if !s.FormsAccessible {
		r.Errors = append(r.Errors, "form_inaccessible")
	}
	if s.EmploymentAlreadyExists {
		r.Errors = append(r.Errors, "employment_duplicate")
	}
	if !s.IAMReady || !s.PayrollReady || !s.EquipmentReady || !s.LearningReady {
		if s.ConsistencyState != ConsistencyDegraded || s.RepairState != RepairRequired {
			r.Errors = append(r.Errors, "downstream_partial_without_repair")
		}
	}
	if s.BusinessState == BusinessCompleted && s.ConsistencyState != ConsistencyConsistent && s.RepairState == "" {
		r.Errors = append(r.Errors, "missing_repair_state")
	}
	if len(r.Errors) != 0 {
		return r
	}
	r.EmploymentCreated = true
	r.CandidatePreserved, r.WorkerPreserved = true, true
	r.Completed = s.BusinessState == BusinessCompleted
	if !r.Completed {
		r.Errors = append(r.Errors, "business_not_completed")
	}
	if s.ConsistencyState != ConsistencyConsistent && s.RepairState != RepairRequired {
		r.Errors = append(r.Errors, "downstream_state_untracked")
	}
	return r
}

// Validate is an alias useful to callers that treat contracts as validators.
func Validate(s Scenario) Result { return Check(s) }

// Explain returns a concise deterministic diagnostic for a failed fixture.
func (r Result) Explain() error {
	if r.Valid() {
		return nil
	}
	return fmt.Errorf("recruit conformance: %v", r.Errors)
}
