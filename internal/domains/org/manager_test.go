package org

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/hcm-next/internal/domains/evidence"
	"github.com/monstercameron/hcm-next/internal/domains/people"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

type memoryWorkerFacts struct {
	sets map[string]WorkerFactSet
}

func (m memoryWorkerFacts) WorkerFactsAt(_ context.Context, q WorkerFactsQuery) (WorkerFactSet, error) {
	set, ok := m.sets[q.Worker.String()]
	if !ok {
		return WorkerFactSet{Worker: q.Worker, PolicyVersion: "org.policy/v1"}, nil
	}
	return set, nil
}

func testRef(t *testing.T, id string) values.EntityRef {
	t.Helper()
	return values.EntityRef{Tenant: "tenant-a", Kind: people.KindWorker, Id: "00000000-0000-4000-8000-000000000" + id}
}

func testInstant(t *testing.T, text string) values.Instant {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339, text)
	if err != nil {
		t.Fatal(err)
	}
	return values.NewInstant(parsed)
}

func testFact(t *testing.T, id, worker, manager string, typ RelationshipType) ManagerRelationshipFact {
	t.Helper()
	start := testInstant(t, "2026-01-01T00:00:00Z")
	effective, err := values.NewOpenInstantInterval(start)
	if err != nil {
		t.Fatal(err)
	}
	known, err := values.NewKnownAt(testInstant(t, "2026-01-02T00:00:00Z"))
	if err != nil {
		t.Fatal(err)
	}
	recorded, err := values.NewRecordedAt(testInstant(t, "2026-01-03T00:00:00Z"))
	if err != nil {
		t.Fatal(err)
	}
	revision, err := values.NewSequenceRevision("worker-relationships", 1)
	if err != nil {
		t.Fatal(err)
	}
	return ManagerRelationshipFact{
		RelationshipID: id, Type: typ, Worker: testRef(t, worker), Manager: testRef(t, manager), AssignmentID: "assignment-" + worker,
		Effective: effective, KnownAt: known, Revision: revision,
		Authority:  evidence.SourceAuthority{Kind: evidence.AuthorityLocal, System: "org-test", PolicyRef: "org-test/v1"},
		Provenance: evidence.Provenance{Source: "org-test", EvidenceRef: "evidence-" + id, RecordedAt: recorded},
	}
}

func auth(allow bool) Authorizer {
	return func(ManagerRelationshipFact) people.AuthorizationDecision {
		decision := people.AuthorizationDecision{PolicyVersion: "authz/v1", Purpose: "manager-read", SubjectDisclosable: true, Fields: map[people.FieldID]people.FieldRuling{
			people.FieldManagerRelation: {Effect: people.EffectAllow},
		}}
		if !allow {
			decision.SubjectDisclosable = false
			decision.SubjectDenialReason = "scope.manager_relationship.absent"
		}
		return decision
	}
}

func setFor(t *testing.T, worker string, facts ...ManagerRelationshipFact) WorkerFactSet {
	t.Helper()
	revision, err := values.NewSequenceRevision("worker-relationships", 1)
	if err != nil {
		t.Fatal(err)
	}
	return WorkerFactSet{Worker: testRef(t, worker), Exists: true, Relationships: facts, Watermark: revision, PolicyVersion: "org.policy/v1"}
}

func request(t *testing.T, worker string, depth int, authorize Authorizer) ManagerResolutionRequest {
	t.Helper()
	return ManagerResolutionRequest{Tenant: "tenant-a", Worker: testRef(t, worker), AsOf: testInstant(t, "2026-06-01T00:00:00Z"), MaxDepth: depth, Authorize: authorize}
}

// TestTodo_ORG_002 is the primary ORG-002 contract test.
func TestTodo_ORG_002(t *testing.T) {
	worker := testFact(t, "direct-a", "001", "002", RelationshipDirectManager)
	second := testFact(t, "direct-b", "002", "003", RelationshipDirectManager)
	dotted := testFact(t, "dotted-a", "001", "004", RelationshipDottedLine)
	reader := memoryWorkerFacts{sets: map[string]WorkerFactSet{
		testRef(t, "001").String(): setFor(t, "001", worker, dotted),
		testRef(t, "002").String(): setFor(t, "002", second),
		testRef(t, "003").String(): setFor(t, "003"),
	}}
	result, err := ResolveManagerRelationships(context.Background(), reader, request(t, "001", 3, auth(true)))
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != StatusResolved || result.Direct == nil || len(result.Chain) != 2 || len(result.DottedLines) != 1 {
		t.Fatalf("result = %+v", result)
	}
	if result.Direct.Manager.Value.Id != testRef(t, "002").Id || result.Chain[1].Manager.Value.Id != testRef(t, "003").Id {
		t.Fatalf("chain manager values = %+v", result.Chain)
	}
	if result.Chain[0].AssignmentID != worker.AssignmentID || result.Watermark.String() == "" || result.PolicyVersion == "" {
		t.Fatalf("missing assignment/evidence = %+v", result)
	}
	explanation := Explain(result)
	if explanation.ChainHops != 2 || explanation.DottedLineHops != 1 || len(explanation.Inputs) == 0 {
		t.Fatalf("explanation = %+v", explanation)
	}
}

func TestTodo_ORG_002_Property(t *testing.T) {
	direct := testFact(t, "direct", "001", "002", RelationshipDirectManager)
	dotted := testFact(t, "dotted", "001", "003", RelationshipDottedLine)
	reader := memoryWorkerFacts{sets: map[string]WorkerFactSet{testRef(t, "001").String(): setFor(t, "001", dotted, direct)}}
	result, err := ResolveManagerRelationships(context.Background(), reader, request(t, "001", 1, auth(true)))
	if err != nil {
		t.Fatal(err)
	}
	if result.Direct == nil || result.Direct.Type != RelationshipDirectManager || result.Chain[0].RelationshipID != "direct" {
		t.Fatalf("dotted line became primary: %+v", result)
	}
}

func TestTodo_ORG_002_Fault(t *testing.T) {
	cycleA := testFact(t, "a", "001", "002", RelationshipDirectManager)
	cycleB := testFact(t, "b", "002", "001", RelationshipDirectManager)
	reader := memoryWorkerFacts{sets: map[string]WorkerFactSet{
		testRef(t, "001").String(): setFor(t, "001", cycleA),
		testRef(t, "002").String(): setFor(t, "002", cycleB),
	}}
	_, err := ResolveManagerRelationships(context.Background(), reader, request(t, "001", 4, auth(true)))
	if !errors.Is(err, ErrRelationshipCycle) {
		t.Fatalf("cycle error = %v", err)
	}
	long := testFact(t, "long", "002", "003", RelationshipDirectManager)
	reader.sets[testRef(t, "002").String()] = setFor(t, "002", long)
	_, err = ResolveManagerRelationships(context.Background(), reader, request(t, "001", 1, auth(true)))
	if !errors.Is(err, ErrDepthExceeded) {
		t.Fatalf("depth error = %v", err)
	}
}

func TestTodo_ORG_002_Mutation(t *testing.T) {
	fact := testFact(t, "direct", "001", "002", RelationshipDirectManager)
	reader := memoryWorkerFacts{sets: map[string]WorkerFactSet{testRef(t, "001").String(): setFor(t, "001", fact)}}
	result, err := ResolveManagerRelationships(context.Background(), reader, request(t, "001", 1, auth(false)))
	if err != nil {
		t.Fatal(err)
	}
	if result.Direct == nil || result.Direct.Disclosure != people.DisclosureWithheld || result.Direct.Manager.Value.Id != "" || result.Direct.RelationshipID != "" {
		t.Fatalf("withheld hop leaked relationship: %+v", result.Direct)
	}
	if result.Explain().WithheldHops != 1 {
		t.Fatalf("explanation = %+v", result.Explain())
	}
}
