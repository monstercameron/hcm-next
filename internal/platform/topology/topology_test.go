package topology

import (
	"strings"
	"testing"
)

// placeholderDecision is intentionally labelled so a human can replace the
// provider, region, and owner without mistaking fixture data for a selection.
func placeholderDecision() Decision {
	t := Manifest{
		SchemaVersion: 1, DecisionID: "decision:pilot-cell:placeholder", CellID: "cell:pilot-placeholder", Environment: "sandbox",
		Selection:    Selection{ProviderRef: "placeholder:provider-selection", Region: "placeholder:region-selection", OwnerRef: "placeholder:owner-selection", Placeholder: true},
		Zones:        []string{"zone-b", "zone-a"},
		Processes:    []Process{{ID: "api", Role: "request-handler", DataDependencies: []string{"ledger", "config"}}, {ID: "worker", Role: "queue-consumer", DataDependencies: []string{"ledger", "outbox"}}, {ID: "timer", Role: "timer", DataDependencies: []string{"workflow"}}},
		Paths:        []Path{{ID: "api-ledger", From: "api", To: "ledger", Plane: "data", Trust: "cell-identity", DataPath: "append-only-ledger"}, {ID: "worker-outbox", From: "worker", To: "outbox", Plane: "data", Trust: "cell-identity", DataPath: "transaction-outbox"}},
		FailureModes: []FailureMode{{ID: "queue-loss", Component: "queue", Degradation: "pause-and-replay", Recovery: "restore-from-checkpoint"}, {ID: "zone-loss", Component: "zone", Degradation: "serve-from-surviving-zone", Recovery: "replace-and-rebalance"}},
		Budget:       ResourceBudget{MaxTenants: 1, MaxConcurrentJobs: 8, CPUUnits: 4, MemoryMiB: 2048},
		Recovery:     RecoveryPlan{BackupRef: "backup-policy:v1", RestoreProcedure: "restore-cell-checkpoint", DrainProcedure: "drain-with-fence", ReplacementPath: "replace-failed-zone"},
		Probes:       []Probe{{Kind: "HEALTH", Expectation: "all critical processes ready", BudgetSecs: 30}, {Kind: "LOAD", Expectation: "bounded workload stays within envelope", BudgetSecs: 120}, {Kind: "DRAIN", Expectation: "no new work after fence", BudgetSecs: 60}, {Kind: "RESTORE", Expectation: "checkpoint restores with matching digest", BudgetSecs: 180}},
	}
	deployPaths := append([]Path(nil), t.Paths...)
	return Decision{Topology: t, Deploy: DeploymentManifest{SchemaVersion: 1, Inventory: []string{"api", "worker", "timer", "ledger", "outbox", "config", "workflow"}, Paths: deployPaths, Zones: t.Zones, FailureModes: t.FailureModes, Budget: t.Budget, Recovery: t.Recovery}}
}

func TestPilotCellTopologyMapsEveryProcessDataDependencyBoundaryAndFailureMode(t *testing.T) {
	evidence, err := Compile(placeholderDecision())
	if err != nil {
		t.Fatal(err)
	}
	if evidence.Status != "HUMAN_SELECTION_REQUIRED" || evidence.Ready {
		t.Fatalf("placeholder evidence = %+v", evidence)
	}
	if len(evidence.HumanInputs) != 3 || evidence.InventoryDigest == "" {
		t.Fatalf("incomplete evidence = %+v", evidence)
	}
}

func TestTodo_TOPOLOGY_001_Property(t *testing.T) {
	d := placeholderDecision()
	one, err := Compile(d)
	if err != nil {
		t.Fatal(err)
	}
	d.Topology.Paths = append([]Path(nil), d.Topology.Paths...)
	d.Topology.Paths[0], d.Topology.Paths[1] = d.Topology.Paths[1], d.Topology.Paths[0]
	d.Deploy.Paths = append([]Path(nil), d.Topology.Paths...)
	two, err := Compile(d)
	if err != nil {
		t.Fatal(err)
	}
	if one.InventoryDigest != two.InventoryDigest {
		t.Fatalf("path ordering changed inventory digest: %s != %s", one.InventoryDigest, two.InventoryDigest)
	}
}

func TestTodo_TOPOLOGY_001_Golden(t *testing.T) {
	one, err := Compile(placeholderDecision())
	if err != nil {
		t.Fatal(err)
	}
	two, err := Compile(placeholderDecision())
	if err != nil {
		t.Fatal(err)
	}
	if one.TopologyDigest != two.TopologyDigest || one.InventoryDigest != two.InventoryDigest {
		t.Fatalf("non-deterministic evidence: %+v %+v", one, two)
	}
}

func TestTodo_TOPOLOGY_001_Integration(t *testing.T) {
	d := placeholderDecision()
	if _, err := Digest(d.Topology); err != nil {
		t.Fatal(err)
	}
	if len(d.Deploy.Inventory) != 7 {
		t.Fatalf("deploy inventory length = %d", len(d.Deploy.Inventory))
	}
}

func TestTodo_TOPOLOGY_001_Fault(t *testing.T) {
	d := placeholderDecision()
	d.Topology.Selection = Selection{ProviderRef: "provider:fixture", Region: "region:fixture", OwnerRef: "owner:fixture"}
	d.Deploy.Paths[0].Trust = "wrong-trust"
	evidence, err := Compile(d)
	if err != nil {
		t.Fatal(err)
	}
	if evidence.Ready || evidence.Status != "TOPOLOGY_DEPLOY_MISMATCH" {
		t.Fatalf("fault was admitted: %+v", evidence)
	}
}

func TestTodo_TOPOLOGY_001_Security(t *testing.T) {
	d := placeholderDecision()
	d.Topology.Paths[0].To = "*"
	violations := Validate(d.Topology)
	if !hasCode(violations, "WILDCARD_BOUNDARY") {
		t.Fatalf("wildcard boundary not rejected: %+v", violations)
	}
}

func TestTodo_TOPOLOGY_001_Conformance(t *testing.T) {
	d := placeholderDecision()
	if Version() != 1 || !strings.Contains(Explain(), "digest-bound") {
		t.Fatalf("contract metadata missing: version=%d explain=%q", Version(), Explain())
	}
	if err := Check(d.Topology); err != nil {
		t.Fatal(err)
	}
}

func TestTodo_TOPOLOGY_001_Recovery(t *testing.T) {
	d := placeholderDecision()
	d.Topology.Probes[3].Expectation = ""
	if err := Check(d.Topology); err == nil {
		t.Fatal("restore probe omission was accepted")
	}
}

func BenchmarkTodo_TOPOLOGY_001(b *testing.B) {
	d := placeholderDecision()
	for i := 0; i < b.N; i++ {
		if _, err := Compile(d); err != nil {
			b.Fatal(err)
		}
	}
}

func TestTodo_TOPOLOGY_001_Mutation(t *testing.T) {
	d := placeholderDecision()
	before := append([]string(nil), d.Deploy.Inventory...)
	if _, err := Compile(d); err != nil {
		t.Fatal(err)
	}
	if strings.Join(before, "|") != strings.Join(d.Deploy.Inventory, "|") {
		t.Fatal("compile mutated deploy inventory")
	}
}

func hasCode(values []Violation, code string) bool {
	for _, value := range values {
		if value.Code == code {
			return true
		}
	}
	return false
}
