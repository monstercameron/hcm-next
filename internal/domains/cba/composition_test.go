package cba

import (
	"encoding/json"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"
)

func compositionRequest() CompositionRequest {
	known := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	return CompositionRequest{
		Applicability:            ApplicabilityResult{Outcome: Applicable, AgreementID: "agr", AgreementRevision: "r1", UnitID: "unit", UnitRevision: "r2", MembershipID: "member", MembershipRevision: "r3", Representative: "union", Source: "roster", PrecedenceBasis: "agreement precedence"},
		KnownAt:                  known,
		SeniorityServiceRevision: "service-r7",
		Policy:                   CompositionPolicy{CompareHigherMinimum, CompareLowerMaximum, CompareHigherMinimum, CompareHigherMinimum, CompareHigherMinimum},
		Constraints: []CBAConstraint{
			{ID: "wage-statute", Kind: ConstraintWage, Source: SourceStatute, Mandatory: true, Minimum: 2400, HasMinimum: true, Unit: "hourly-cents", ClauseRef: "wage", ReleaseRef: "statute-r1"},
			{ID: "wage-company", Kind: ConstraintWage, Source: SourceCompany, Minimum: 1800, HasMinimum: true, Unit: "hourly-cents", ClauseRef: "wage", ReleaseRef: "company-r1"},
			{ID: "schedule-agreement", Kind: ConstraintSchedule, Source: SourceAgreement, Mandatory: true, Maximum: 8, HasMaximum: true, Unit: "hours", ClauseRef: "schedule", ReleaseRef: "cba-r1"},
			{ID: "leave-agreement", Kind: ConstraintLeave, Source: SourceAgreement, Mandatory: true, Value: "protected", ClauseRef: "leave", ReleaseRef: "cba-r1"},
			{ID: "seniority-agreement", Kind: ConstraintSeniority, Source: SourceAgreement, Mandatory: true, Value: "service-order", KnownAt: known, ServiceResultRevision: "service-r7", ClauseRef: "seniority", ReleaseRef: "cba-r1"},
			{ID: "discipline-agreement", Kind: ConstraintDiscipline, Source: SourceAgreement, Mandatory: true, Value: "review", ClauseRef: "discipline", ReleaseRef: "cba-r1"},
		},
		Provenance: []ProvenancePin{{SourceStatute, "statute-r1", []string{"wage"}}, {SourceCompany, "company-r1", []string{"wage", "schedule"}}, {SourceAgreement, "cba-r1", []string{"schedule", "leave", "seniority", "discipline", "discipline-ban"}}},
	}
}

func TestCBACompositionPreservesMandatoryStatutoryAndAgreementConstraints(t *testing.T) {
	q := compositionRequest()
	got, err := ComposeConstraints(q)
	if err != nil || got.Outcome != CompositionAllowWithObligations || got.Calculations[0].Minimum != 2400 || got.Calculations[1].Maximum != 8 {
		t.Fatalf("got=%+v err=%v", got, err)
	}
	q.Constraints = append(q.Constraints, CBAConstraint{ID: "schedule-company", Kind: ConstraintSchedule, Source: SourceCompany, Maximum: 12, HasMaximum: true, Unit: "hours", ClauseRef: "schedule", ReleaseRef: "company-r1"})
	got, err = ComposeConstraints(q)
	if err != nil || got.Calculations[1].Maximum != 8 {
		t.Fatalf("mandatory maximum weakened: %+v err=%v", got, err)
	}
	q.Constraints = append(q.Constraints, CBAConstraint{ID: "wage-cap", Kind: ConstraintWage, Source: SourceAgreement, Mandatory: true, Maximum: 5000, HasMaximum: true, Unit: "hourly-cents", ClauseRef: "wage-cap", ReleaseRef: "cba-r1"})
	q.Provenance[2].ClauseRefs = append(q.Provenance[2].ClauseRefs, "wage-cap")
	got, err = ComposeConstraints(q)
	wage := got.Calculations[0]
	if err != nil || wage.MinimumSourceRelease != "statute-r1" || wage.MaximumSourceRelease != "cba-r1" {
		t.Fatalf("intersected bound provenance collapsed: %+v err=%v", wage, err)
	}
}

func TestTodo_CBA_002_Property(t *testing.T) {
	q := compositionRequest()
	want, err := ComposeConstraints(q)
	if err != nil {
		t.Fatal(err)
	}
	for n := 1; n < len(q.Constraints); n++ {
		x := compositionRequest()
		x.Constraints = append(x.Constraints[n:], x.Constraints[:n]...)
		got, err := ComposeConstraints(x)
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Fatalf("rotation %d changed result: %+v err=%v", n, got, err)
		}
	}
}

func TestTodo_CBA_002_Golden(t *testing.T) {
	got, err := ComposeConstraints(compositionRequest())
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	const golden = `{"Outcome":"ALLOW_WITH_OBLIGATIONS","Obligations":[{"ID":"discipline-agreement","Kind":"DISCIPLINE","ClauseRef":"discipline","ReleaseRef":"cba-r1","Source":"AGREEMENT","Mandatory":true},{"ID":"leave-agreement","Kind":"LEAVE","ClauseRef":"leave","ReleaseRef":"cba-r1","Source":"AGREEMENT","Mandatory":true},{"ID":"schedule-agreement","Kind":"SCHEDULE","ClauseRef":"schedule","ReleaseRef":"cba-r1","Source":"AGREEMENT","Mandatory":true},{"ID":"seniority-agreement","Kind":"SENIORITY","ClauseRef":"seniority","ReleaseRef":"cba-r1","Source":"AGREEMENT","Mandatory":true},{"ID":"wage-company","Kind":"WAGE","ClauseRef":"wage","ReleaseRef":"company-r1","Source":"COMPANY","Mandatory":false},{"ID":"wage-statute","Kind":"WAGE","ClauseRef":"wage","ReleaseRef":"statute-r1","Source":"STATUTE","Mandatory":true}],"Prohibitions":null,"Calculations":[{"Kind":"WAGE","Unit":"hourly-cents","Minimum":2400,"Maximum":0,"HasMinimum":true,"HasMaximum":false,"MinimumSourceClause":"wage","MinimumSourceRelease":"statute-r1","MaximumSourceClause":"","MaximumSourceRelease":""},{"Kind":"SCHEDULE","Unit":"hours","Minimum":0,"Maximum":8,"HasMinimum":false,"HasMaximum":true,"MinimumSourceClause":"","MinimumSourceRelease":"","MaximumSourceClause":"schedule","MaximumSourceRelease":"cba-r1"}],"Reason":""}`
	if string(b) != golden {
		t.Fatalf("golden mismatch\nwant %s\n got %s", golden, b)
	}
}

func TestTodo_CBA_002_Race(t *testing.T) {
	q := compositionRequest()
	var wg sync.WaitGroup
	for range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got, err := ComposeConstraints(q)
			if err != nil || got.Outcome != CompositionAllowWithObligations {
				t.Errorf("got=%+v err=%v", got, err)
			}
		}()
	}
	wg.Wait()
}

func TestTodo_CBA_002_Fault(t *testing.T) {
	q := compositionRequest()
	q.Constraints[0].ClauseRef = ""
	if _, err := ComposeConstraints(q); !errors.Is(err, ErrInvalidConstraint) {
		t.Fatalf("err=%v", err)
	}
	q = compositionRequest()
	q.Policy.Wage = ""
	if _, err := ComposeConstraints(q); !errors.Is(err, ErrInvalidPolicy) {
		t.Fatalf("err=%v", err)
	}
	for name, mutate := range map[string]func(*CompositionRequest){
		"negative":        func(q *CompositionRequest) { q.Constraints[0].Minimum = -1 },
		"unit mismatch":   func(q *CompositionRequest) { q.Constraints[1].Unit = "dollars" },
		"duplicate id":    func(q *CompositionRequest) { q.Constraints[1].ID = q.Constraints[0].ID },
		"ambiguous bound": func(q *CompositionRequest) { q.Constraints[0].HasMinimum = false },
	} {
		t.Run(name, func(t *testing.T) {
			q := compositionRequest()
			mutate(&q)
			if _, err := ComposeConstraints(q); !errors.Is(err, ErrInvalidConstraint) {
				t.Fatalf("err=%v", err)
			}
		})
	}
	q = compositionRequest()
	q.Constraints[2].Maximum = 0
	got, err := ComposeConstraints(q)
	if err != nil || !got.Calculations[1].HasMaximum || got.Calculations[1].Maximum != 0 {
		t.Fatalf("explicit zero maximum lost: %+v err=%v", got.Calculations, err)
	}
}

func TestTodo_CBA_002_Security(t *testing.T) {
	q := compositionRequest()
	q.Constraints[0].ReleaseRef = "forged"
	got, err := ComposeConstraints(q)
	if err != nil || got.Outcome != CompositionUnknown {
		t.Fatalf("forged release: %+v err=%v", got, err)
	}
	q = compositionRequest()
	q.Applicability.MembershipRevision = ""
	got, err = ComposeConstraints(q)
	if err != nil || got.Outcome != CompositionUnknown {
		t.Fatalf("forged applicability: %+v err=%v", got, err)
	}
}

func TestTodo_CBA_002_Conformance(t *testing.T) {
	q := compositionRequest()
	q.Constraints = append(q.Constraints, CBAConstraint{ID: "discipline-ban", Kind: ConstraintDiscipline, Source: SourceAgreement, Mandatory: true, Prohibition: true, ClauseRef: "discipline-ban", ReleaseRef: "cba-r1"})
	got, err := ComposeConstraints(q)
	if err != nil || got.Outcome != CompositionBlock {
		t.Fatalf("got=%+v err=%v", got, err)
	}
	q = compositionRequest()
	q.Constraints[4].KnownAt = q.KnownAt.Add(-time.Second)
	got, err = ComposeConstraints(q)
	if err != nil || got.Outcome != CompositionUnknown {
		t.Fatalf("stale seniority: %+v err=%v", got, err)
	}
	q = compositionRequest()
	q.Constraints[4].KnownAt = q.KnownAt.Add(time.Second)
	got, err = ComposeConstraints(q)
	if err != nil || got.Outcome != CompositionUnknown {
		t.Fatalf("future seniority: %+v err=%v", got, err)
	}
	q = compositionRequest()
	q.Constraints[4].ServiceResultRevision = "service-r6"
	got, err = ComposeConstraints(q)
	if err != nil || got.Outcome != CompositionUnknown {
		t.Fatalf("stale service revision: %+v err=%v", got, err)
	}
	q = compositionRequest()
	q.Constraints = append(q.Constraints, CBAConstraint{ID: "schedule-floor", Kind: ConstraintSchedule, Source: SourceStatute, Mandatory: true, Minimum: 10, HasMinimum: true, Unit: "hours", ClauseRef: "schedule-floor", ReleaseRef: "statute-r1"})
	q.Provenance[0].ClauseRefs = append(q.Provenance[0].ClauseRefs, "schedule-floor")
	got, err = ComposeConstraints(q)
	if err != nil || got.Outcome != CompositionBlock {
		t.Fatalf("conflicting mandatory ranges: %+v err=%v", got, err)
	}
	q = compositionRequest()
	q.Applicability.Outcome = Conflict
	got, err = ComposeConstraints(q)
	if err != nil || got.Outcome != CompositionUnknown {
		t.Fatalf("applicability conflict: %+v err=%v", got, err)
	}
}

func TestTodo_CBA_002_Mutation(t *testing.T) {
	q := compositionRequest()
	before := append([]CBAConstraint(nil), q.Constraints...)
	if _, err := ComposeConstraints(q); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(q.Constraints, before) {
		t.Fatal("mutated input")
	}
}
