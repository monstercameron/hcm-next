package org

import (
	"context"
	"errors"
	"testing"
)

func cycleQuery(t *testing.T, node, manager string, depth int, authorize Authorizer) CycleQuery {
	t.Helper()
	return CycleQuery{
		Tenant: "tenant-a", Node: testRef(t, node), ProposedManager: testRef(t, manager),
		AsOf: testInstant(t, "2026-06-01T00:00:00Z"), MaxDepth: depth, Authorize: authorize,
	}
}

// erroringWorkerFacts fails the test if it is ever called: it proves the
// self-management short-circuit in DetectManagerCycle never reads.
type erroringWorkerFacts struct{ t *testing.T }

func (e erroringWorkerFacts) WorkerFactsAt(context.Context, WorkerFactsQuery) (WorkerFactSet, error) {
	e.t.Fatal("WorkerFactsAt should not be called for a self-management cycle")
	return WorkerFactSet{}, nil
}

func TestDetectManagerCycle_SelfManagement(t *testing.T) {
	finding, err := DetectManagerCycle(context.Background(), erroringWorkerFacts{t: t}, cycleQuery(t, "001", "001", 5, auth(true)))
	if err != nil {
		t.Fatalf("DetectManagerCycle: %v", err)
	}
	if finding.Status != CycleStatusCycle || !finding.WouldCycle() || !finding.Certain() || finding.Depth != 0 {
		t.Fatalf("finding = %+v, want a certain self-management cycle at depth 0", finding)
	}
}

// TestDetectManagerCycle_MultiHop is the genuine multi-hop proof: worker 001
// reports to 002, who reports to 003 (A -> B -> C). Proposing that 003's
// manager become 001 -- "promote 001 to manage 003" -- must be refused
// because walking up from 001 (the proposed manager) reaches 003 two hops
// up, not because 003 equals 001 (it does not) and not because of any fixed
// depth assumption.
func TestDetectManagerCycle_MultiHop(t *testing.T) {
	aReportsToB := testFact(t, "rel-a-b", "001", "002", RelationshipDirectManager)
	bReportsToC := testFact(t, "rel-b-c", "002", "003", RelationshipDirectManager)
	reader := memoryWorkerFacts{sets: map[string]WorkerFactSet{
		testRef(t, "001").String(): setFor(t, "001", aReportsToB),
		testRef(t, "002").String(): setFor(t, "002", bReportsToC),
		testRef(t, "003").String(): setFor(t, "003"),
	}}

	// Node = 003 (C), ProposedManager = 001 (A): "C now reports to A" closes
	// the loop A -> B -> C -> A.
	finding, err := DetectManagerCycle(context.Background(), reader, cycleQuery(t, "003", "001", 5, auth(true)))
	if err != nil {
		t.Fatalf("DetectManagerCycle: %v", err)
	}
	if finding.Status != CycleStatusCycle || !finding.WouldCycle() || !finding.Certain() {
		t.Fatalf("finding = %+v, want a certain cycle", finding)
	}
	if finding.Depth != 2 {
		t.Fatalf("depth = %d, want 2 (A -> B -> C is two hops above the proposed manager)", finding.Depth)
	}

	// The one-hop-only bug this todo closes: 003 is not 001, and it is not
	// 001's *direct* manager either (002 is), so a naive
	// "ProposedManager == Node" or "ProposedManager's direct manager == Node"
	// check would both miss this. Prove neither shortcut would have caught
	// it, so a regression to either shape fails loudly.
	if testRef(t, "003") == testRef(t, "001") {
		t.Fatalf("test fixture error: node must differ from the proposed manager")
	}
	directManager := reader.sets[testRef(t, "001").String()].Relationships[0].Manager
	if directManager == testRef(t, "003") {
		t.Fatalf("test fixture error: node must not be the proposed manager's direct manager, or this test would pass for the wrong reason")
	}
}

// TestDetectManagerCycle_DeepChainAdmitted proves the negative RED also
// requires: a legitimate, deep reporting chain that never reaches Node must
// be admitted, not refused merely for being deep. 001 -> 002 -> 003 -> 004
// -> 005 is four hops, comfortably inside MaxDepth, and 999 (Node) never
// appears in it.
func TestDetectManagerCycle_DeepChainAdmitted(t *testing.T) {
	hops := []ManagerRelationshipFact{
		testFact(t, "rel-1-2", "001", "002", RelationshipDirectManager),
		testFact(t, "rel-2-3", "002", "003", RelationshipDirectManager),
		testFact(t, "rel-3-4", "003", "004", RelationshipDirectManager),
		testFact(t, "rel-4-5", "004", "005", RelationshipDirectManager),
	}
	reader := memoryWorkerFacts{sets: map[string]WorkerFactSet{
		testRef(t, "001").String(): setFor(t, "001", hops[0]),
		testRef(t, "002").String(): setFor(t, "002", hops[1]),
		testRef(t, "003").String(): setFor(t, "003", hops[2]),
		testRef(t, "004").String(): setFor(t, "004", hops[3]),
		testRef(t, "005").String(): setFor(t, "005"),
	}}

	finding, err := DetectManagerCycle(context.Background(), reader, cycleQuery(t, "999", "001", 10, auth(true)))
	if err != nil {
		t.Fatalf("DetectManagerCycle: %v", err)
	}
	if finding.Status != CycleStatusSafe || finding.WouldCycle() || !finding.Certain() {
		t.Fatalf("finding = %+v, want a certain SAFE verdict for a deep non-cycling chain", finding)
	}
	if len(finding.Resolution.Chain) != 4 {
		t.Fatalf("resolution chain = %d hops, want the full 4-hop walk to have completed", len(finding.Resolution.Chain))
	}
}

func TestDetectManagerCycle_UndeterminedOnWithheldHop(t *testing.T) {
	aReportsToB := testFact(t, "rel-a-b", "001", "002", RelationshipDirectManager)
	bReportsToC := testFact(t, "rel-b-c", "002", "003", RelationshipDirectManager)
	reader := memoryWorkerFacts{sets: map[string]WorkerFactSet{
		testRef(t, "001").String(): setFor(t, "001", aReportsToB),
		testRef(t, "002").String(): setFor(t, "002", bReportsToC),
		testRef(t, "003").String(): setFor(t, "003"),
	}}
	// A restrictive authorizer withholds every hop. Node (003) is in fact two
	// hops up from the proposed manager (001), but this reader can never
	// prove it, so the answer must be UNDETERMINED, never SAFE.
	finding, err := DetectManagerCycle(context.Background(), reader, cycleQuery(t, "003", "001", 5, auth(false)))
	if err != nil {
		t.Fatalf("DetectManagerCycle: %v", err)
	}
	if finding.Status != CycleStatusUndetermined || finding.WouldCycle() || finding.Certain() {
		t.Fatalf("finding = %+v, want UNDETERMINED, never a false SAFE", finding)
	}
}

func TestDetectManagerCycle_UndeterminedOnDepthExceeded(t *testing.T) {
	hops := []ManagerRelationshipFact{
		testFact(t, "rel-1-2", "001", "002", RelationshipDirectManager),
		testFact(t, "rel-2-3", "002", "003", RelationshipDirectManager),
		testFact(t, "rel-3-4", "003", "004", RelationshipDirectManager),
	}
	reader := memoryWorkerFacts{sets: map[string]WorkerFactSet{
		testRef(t, "001").String(): setFor(t, "001", hops[0]),
		testRef(t, "002").String(): setFor(t, "002", hops[1]),
		testRef(t, "003").String(): setFor(t, "003", hops[2]),
		testRef(t, "004").String(): setFor(t, "004"),
	}}
	// The chain is 3 hops deep; a bound of 1 cannot resolve it. A shallow
	// bound must never be read as "no cycle beyond what I bothered to walk".
	finding, err := DetectManagerCycle(context.Background(), reader, cycleQuery(t, "999", "001", 1, auth(true)))
	if err != nil {
		t.Fatalf("DetectManagerCycle: %v", err)
	}
	if finding.Status != CycleStatusUndetermined || finding.Certain() {
		t.Fatalf("finding = %+v, want UNDETERMINED on an unresolved depth", finding)
	}
}

func TestDetectManagerCycle_UndeterminedOnPreexistingGraphCycle(t *testing.T) {
	cycleA := testFact(t, "a", "001", "002", RelationshipDirectManager)
	cycleB := testFact(t, "b", "002", "001", RelationshipDirectManager)
	reader := memoryWorkerFacts{sets: map[string]WorkerFactSet{
		testRef(t, "001").String(): setFor(t, "001", cycleA),
		testRef(t, "002").String(): setFor(t, "002", cycleB),
	}}
	finding, err := DetectManagerCycle(context.Background(), reader, cycleQuery(t, "999", "001", 5, auth(true)))
	if err != nil {
		t.Fatalf("DetectManagerCycle: %v", err)
	}
	if finding.Status != CycleStatusUndetermined {
		t.Fatalf("finding = %+v, want UNDETERMINED rather than a raw ErrRelationshipCycle escaping as an error", finding)
	}
}

func TestDetectManagerCycle_InvalidQuery(t *testing.T) {
	reader := memoryWorkerFacts{}
	if _, err := DetectManagerCycle(context.Background(), nil, cycleQuery(t, "001", "002", 5, auth(true))); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("nil reader: err = %v, want ErrInvalidRequest", err)
	}
	q := cycleQuery(t, "001", "002", 0, auth(true))
	if _, err := DetectManagerCycle(context.Background(), reader, q); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("zero max depth: err = %v, want ErrInvalidRequest", err)
	}
	q = cycleQuery(t, "001", "002", 5, nil)
	if _, err := DetectManagerCycle(context.Background(), reader, q); !errors.Is(err, ErrAuthorizationMissing) {
		t.Fatalf("nil authorizer: err = %v, want ErrAuthorizationMissing", err)
	}
	wrongTenant := cycleQuery(t, "001", "002", 5, auth(true))
	wrongTenant.Node.Tenant = "other-tenant"
	if _, err := DetectManagerCycle(context.Background(), reader, wrongTenant); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("cross-tenant node: err = %v, want ErrInvalidRequest", err)
	}
}
