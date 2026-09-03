package recruit

import (
	"reflect"
	"testing"
)

func golden() Scenario {
	return Scenario{CandidateID: "cand-1", PersonID: "person-1", WorkerID: "worker-1", EmploymentID: "employment-1", OfferID: "offer-1", OfferApproved: true, OfferAccepted: true, ApprovalsBound: true, PositionAvailable: true, BudgetAvailable: true, WorkAuthorizationValid: true, FormsAccessible: true, IAMReady: true, PayrollReady: true, EquipmentReady: true, LearningReady: true, BusinessState: BusinessCompleted, ConsistencyState: ConsistencyConsistent}
}

// TestTodo_CONF_002 is the PRIMARY contract test: the accepted offer creates
// one employment while retaining both candidate and worker relationships.
func TestTodo_CONF_002(t *testing.T) {
	r := Check(golden())
	if !r.Valid() || !r.Completed || !r.EmploymentCreated || !r.CandidatePreserved || !r.WorkerPreserved {
		t.Fatalf("golden result = %+v", r)
	}
}

func TestTodo_CONF_002_Security(t *testing.T) {
	cases := []struct {
		name, field, want string
		mutate            func(*Scenario)
	}{
		{"duplicate person", "duplicate_person", "duplicate_person", func(s *Scenario) { s.DuplicatePerson = true }},
		{"position", "position_exhausted", "position_exhausted", func(s *Scenario) { s.PositionAvailable = false }},
		{"budget", "budget_exhausted", "budget_exhausted", func(s *Scenario) { s.BudgetAvailable = false }},
		{"expired offer", "offer_expired", "offer_expired", func(s *Scenario) { s.OfferExpired = true }},
		{"authorization", "work authorization", "work_authorization_missing", func(s *Scenario) { s.WorkAuthorizationValid = false }},
		{"form", "form", "form_inaccessible", func(s *Scenario) { s.FormsAccessible = false }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := golden()
			tc.mutate(&s)
			r := Check(s)
			if r.Completed || r.EmploymentCreated {
				t.Fatalf("unsafe completion: %+v", r)
			}
			if !contains(r.Errors, tc.want) {
				t.Fatalf("errors %v missing %s", r.Errors, tc.want)
			}
		})
	}
}

func TestTodo_CONF_002_Conformance(t *testing.T) {
	s := golden()
	a, b := Check(s), Check(s)
	if !reflect.DeepEqual(a, b) {
		t.Fatalf("non-deterministic results: %+v != %+v", a, b)
	}
	if a.BusinessState != BusinessCompleted || a.ConsistencyState != ConsistencyConsistent {
		t.Fatalf("terminal state = %+v", a)
	}
	// Candidate and worker are overlapping roles, never an exclusive identity.
	if s.CandidateID == s.WorkerID {
		t.Fatal("fixture must retain distinct role relationships")
	}
}

func TestTodo_CONF_002_Mutation(t *testing.T) {
	fields := []func(*Scenario){func(s *Scenario) { s.OfferApproved = false }, func(s *Scenario) { s.OfferAccepted = false }, func(s *Scenario) { s.ApprovalsBound = false }, func(s *Scenario) { s.EmploymentAlreadyExists = true }, func(s *Scenario) { s.BusinessState = "IN_PROGRESS" }}
	for i, mutate := range fields {
		t.Run(string(rune('a'+i)), func(t *testing.T) {
			s := golden()
			mutate(&s)
			r := Check(s)
			if r.Completed {
				t.Fatalf("mutation falsely completed: %+v", r)
			}
		})
	}
	// A degraded downstream integration is a completed business outcome only
	// when the repair route is explicit.
	s := golden()
	s.ConsistencyState, s.RepairState = ConsistencyDegraded, RepairRequired
	if r := Check(s); !r.Valid() || !r.Completed {
		t.Fatalf("explicit repair route rejected: %+v", r)
	}
	s.RepairState = ""
	if r := Check(s); r.Completed || r.Valid() {
		t.Fatalf("implicit repair route accepted: %+v", r)
	}
}

func contains(xs []string, want string) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}
