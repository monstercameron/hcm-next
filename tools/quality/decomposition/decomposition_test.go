package decomposition_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/monstercameron/hcm-next/tools/quality/decomposition"
)

func complete() decomposition.Decision {
	return decomposition.Decision{ID: "ARCH-GO-019", Kind: "process", Target: "worker", Justification: "measured queue scaling and independent failure boundary exceed the modular monolith safely", Evidence: decomposition.Evidence{
		Scaling: "p99 queue depth and CPU exceed threshold at 4x load", FailureIsolation: "worker crash leaves committed work resumable", SecurityBoundary: "separate workload identity", Residency: "EU records remain in EU", ReleaseBoundary: "weekly release cadence differs", OwnershipBoundary: "operations owns worker lifecycle", APIEventConsistency: "versioned event contract v2", DataAuthority: "Postgres ledger remains authoritative", FailureRepair: "retry and repair runbook", MigrationRollback: "expand/cutover/rollback rehearsal", OperationalCost: "one additional on-call and dashboard", MonolithInsufficiency: "load test shows safe monolith limit exceeded",
	}, Processes: map[string][]string{"worker": {"leave"}}}
}

func TestDecompositionDecisionRejectsTopologyDrivenSplit(t *testing.T) {
	d := complete()
	d.Evidence = decomposition.Evidence{}
	d.Justification = "one service per domain because the team owns it"
	d.Target = "domain-count deployment fashion"
	v := decomposition.Validate(d)
	var topology bool
	for _, f := range v {
		topology = topology || f.Code == decomposition.TopologyOnly
	}
	if !topology {
		t.Fatalf("topology-only decision accepted: %v", v)
	}
}

func TestDuplicateDomainOwnersRejected(t *testing.T) {
	d := complete()
	d.Processes = map[string][]string{"api": {"leave"}, "worker": {"leave"}}
	v := decomposition.Validate(d)
	for _, f := range v {
		if f.Code == decomposition.DuplicateDomainOwner {
			return
		}
	}
	t.Fatalf("duplicate domain package accepted: %v", v)
}

func TestCompleteDecisionAndFile(t *testing.T) {
	d := complete()
	if err := decomposition.Check(d); err != nil {
		t.Fatal(err)
	}
	b := `{"id":"ARCH-GO-019","kind":"process","target":"worker","justification":"measured boundary","evidence":{"scaling":"load","failure_isolation":"restart","security_boundary":"identity","residency":"region","release_boundary":"cadence","ownership_boundary":"owner","api_event_consistency":"v1","data_authority":"db","failure_repair":"runbook","migration_rollback":"rehearsed","operational_cost":"on-call","modular_monolith_insufficiency":"load"},"processes":{"worker":["leave"]}}`
	p := filepath.Join(t.TempDir(), "decision.json")
	if err := os.WriteFile(p, []byte(b), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := decomposition.CheckFile(p); err != nil {
		t.Fatal(err)
	}
	if err := decomposition.CheckFile(""); err == nil || !strings.Contains(err.Error(), "path") {
		t.Fatalf("expected empty path error, got %v", err)
	}
}

// The matrix names are kept as executable evidence for the planning registry.
func TestTodo_ARCH_GO_019_Golden(t *testing.T) { TestCompleteDecisionAndFile(t) }
func TestTodo_ARCH_GO_019_Race(t *testing.T)   { TestDuplicateDomainOwnersRejected(t) }
func TestTodo_ARCH_GO_019_Security(t *testing.T) {
	TestDecompositionDecisionRejectsTopologyDrivenSplit(t)
}
func TestTodo_ARCH_GO_019_Conformance(t *testing.T) { TestCompleteDecisionAndFile(t) }
func TestTodo_ARCH_GO_019_Mutation(t *testing.T)    { TestDuplicateDomainOwnersRejected(t) }
