package cba

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

func cbaAt(day int) time.Time { return time.Date(2026, time.January, day, 0, 0, 0, 0, time.UTC) }

func cbaAgreement() AgreementRevision {
	return AgreementRevision{ID: "agr-r1", AgreementID: "agr", Revision: "r1", Version: "2026.1", Title: "engineers", Representative: "union-ref", Source: "signed-doc-ref", EffectiveFrom: cbaAt(1), KnownFrom: cbaAt(1), Precedence: 10}
}

func cbaUnit() BargainingUnitRevision {
	return BargainingUnitRevision{ID: "unit-r1", UnitID: "unit", Revision: "r1", AgreementID: "agr", Name: "engineering unit", Representative: "union-ref", Source: "unit-register", EffectiveFrom: cbaAt(1), KnownFrom: cbaAt(1)}
}

func cbaMembership() MembershipRevision {
	return MembershipRevision{ID: "member-r1", MembershipID: "member", Revision: "r1", WorkerID: "worker", UnitID: "unit", Source: "membership-register", EffectiveFrom: cbaAt(1), KnownFrom: cbaAt(1)}
}

func cbaRequest() ApplicabilityRequest {
	return ApplicabilityRequest{WorkerID: "worker", AgreementID: "agr", EffectiveAt: cbaAt(5), KnownAt: cbaAt(5), Agreements: []AgreementRevision{cbaAgreement()}, Units: []BargainingUnitRevision{cbaUnit()}, Memberships: []MembershipRevision{cbaMembership()}}
}

func TestCBAApplicabilityRequiresAgreementUnitWorkerAndEffectiveRuleRevision(t *testing.T) {
	if _, err := ResolveApplicability(ApplicabilityRequest{}); err == nil {
		t.Fatal("expected incomplete request to be rejected")
	}
	got, err := ResolveApplicability(cbaRequest())
	if err != nil {
		t.Fatalf("ResolveApplicability: %v", err)
	}
	if got.Outcome != Applicable || got.AgreementRevision != "r1" || got.UnitID != "unit" || got.MembershipID != "member" {
		t.Fatalf("decision = %+v", got)
	}
	expired := cbaRequest()
	expired.EffectiveAt = cbaAt(20)
	expired.Agreements[0].EffectiveTo = cbaAt(10)
	got, err = ResolveApplicability(expired)
	if err != nil || got.Outcome != NotApplicable {
		t.Fatalf("expired agreement decision = %+v, err=%v", got, err)
	}
	overlap := cbaRequest()
	second := cbaAgreement()
	second.ID, second.Revision = "agr-r2", "r2"
	overlap.Agreements = append(overlap.Agreements, second)
	got, err = ResolveApplicability(overlap)
	if err != nil || got.Outcome != Conflict {
		t.Fatalf("overlap decision = %+v, err=%v", got, err)
	}
}

func TestTodo_CBA_001_Property(t *testing.T) {
	first, err := ResolveApplicability(cbaRequest())
	if err != nil {
		t.Fatal(err)
	}
	second, err := ResolveApplicability(cbaRequest())
	if err != nil {
		t.Fatal(err)
	}
	one, err := first.Digest()
	if err != nil {
		t.Fatal(err)
	}
	two, err := second.Digest()
	if err != nil || one != two {
		t.Fatalf("digest mismatch: %q %q; err=%v", one, two, err)
	}
}

func TestTodo_CBA_001_Golden(t *testing.T) {
	got, err := ResolveApplicability(cbaRequest())
	if err != nil {
		t.Fatal(err)
	}
	want := "AGREEMENT_PRECEDENCE_THEN_UNIT_MEMBERSHIP"
	if !strings.Contains(got.Explain(), want) {
		t.Fatalf("explanation = %q, missing %q", got.Explain(), want)
	}
}

func TestTodo_CBA_001_Race(t *testing.T) {
	q := cbaRequest()
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if got, err := ResolveApplicability(q); err != nil || got.Outcome != Applicable {
				t.Errorf("concurrent resolution = %+v, err=%v", got, err)
			}
		}()
	}
	wg.Wait()
}

type cbaFakePorts struct {
	agreementErr error
	workerErr    error
}

func (f cbaFakePorts) AgreementRevisions(context.Context, AgreementQuery) ([]AgreementRevision, error) {
	return []AgreementRevision{cbaAgreement()}, f.agreementErr
}
func (cbaFakePorts) BargainingUnitRevisions(context.Context, UnitQuery) ([]BargainingUnitRevision, error) {
	return []BargainingUnitRevision{cbaUnit()}, nil
}
func (cbaFakePorts) MembershipRevisions(context.Context, MembershipQuery) ([]MembershipRevision, error) {
	return []MembershipRevision{cbaMembership()}, nil
}
func (f cbaFakePorts) WorkerFacts(context.Context, WorkerFactsQuery) (WorkerFactSet, error) {
	return WorkerFactSet{WorkerID: "worker", JobCode: "ENG", LocationID: "NYC", Source: "worker-register"}, f.workerErr
}
func (cbaFakePorts) AgreementClauses(context.Context, ClauseQuery) ([]AgreementClauseRevision, error) {
	return []AgreementClauseRevision{{ID: "clause-1", AgreementID: "agr", AgreementRevision: "r1", Revision: "c1", Kind: "OVERTIME", Source: "clause-register", JobCodes: []string{"ENG"}, LocationIDs: []string{"NYC"}, EffectiveFrom: cbaAt(1), KnownFrom: cbaAt(1)}}, nil
}

func cbaPorts() ApplicabilityPorts {
	f := cbaFakePorts{}
	return ApplicabilityPorts{Agreements: f, Units: f, Memberships: f, Workers: f, Clauses: f}
}

func TestTodo_CBA_001_Fault(t *testing.T) {
	f := cbaFakePorts{agreementErr: errors.New("catalog unavailable")}
	ports := ApplicabilityPorts{Agreements: f, Units: f, Memberships: f, Workers: f, Clauses: f}
	_, err := ResolveApplicabilityFromPorts(context.Background(), ports, PortApplicabilityRequest{WorkerID: "worker", AgreementID: "agr", EffectiveAt: cbaAt(5), KnownAt: cbaAt(5)})
	if !errors.Is(err, ErrPortFailed) {
		t.Fatalf("err=%v, want ErrPortFailed", err)
	}
}

func TestTodo_CBA_001_Security(t *testing.T) {
	decision, err := ResolveApplicabilityFromPorts(context.Background(), cbaPorts(), PortApplicabilityRequest{WorkerID: "worker", AgreementID: "agr", EffectiveAt: cbaAt(5), KnownAt: cbaAt(5)})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(decision.Explain(), "NYC") || strings.Contains(decision.Explain(), "ENG") {
		t.Fatalf("explanation disclosed worker facts: %q", decision.Explain())
	}
}

func TestTodo_CBA_001_Conformance(t *testing.T) {
	decision, err := ResolveApplicabilityFromPorts(context.Background(), cbaPorts(), PortApplicabilityRequest{WorkerID: "worker", AgreementID: "agr", EffectiveAt: cbaAt(5), KnownAt: cbaAt(5)})
	if err != nil || decision.Applicability.Outcome != Applicable || len(decision.Clauses) != 1 {
		t.Fatalf("port result = %+v, err=%v", decision, err)
	}
}

func TestTodo_CBA_001_Mutation(t *testing.T) {
	q := cbaRequest()
	before := q.Agreements[0]
	if _, err := ResolveApplicability(q); err != nil {
		t.Fatal(err)
	}
	if q.Agreements[0] != before {
		t.Fatal("resolver mutated the caller's agreement revision")
	}
}
