package decomposition

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func completeDecision() Decision {
	claim := func(name string) Claim {
		return Claim{Source: "artifacts/" + name + ".json", Measure: name, Value: 2, Limit: 1, Unit: "units", Outcome: "exceeded"}
	}
	d := Decision{Version: 2, ID: "ARCH-GO-019", Kind: "process", Target: "cmd/worker", Justification: "measured queue failure isolation exceeds the modular monolith limit", Evidence: Evidence{
		Scaling: claim("scaling"), FailureIsolation: claim("failure"), SecurityBoundary: claim("security"), Residency: claim("residency"), ReleaseBoundary: claim("release"), OwnershipBoundary: claim("ownership"),
		APIEventContract:   Contract{Source: "artifacts/api.json", Authority: "platform", Version: "v1", Consistency: "compatible"},
		DataAuthority:      Authority{Source: "artifacts/data.json", System: "ledger", Writer: "worker"},
		FailureRepair:      Operation{Source: "artifacts/repair.json", Failure: "crash", Detection: "health", Repair: "replay", Rollback: "pause"},
		MigrationRollback:  Operation{Source: "artifacts/migration.json", Failure: "cutover", Detection: "audit", Repair: "forward", Rollback: "expand"},
		OperationalCost:    Cost{Source: "artifacts/cost.json", MonthlyMinor: 10000, Currency: "USD", OnCallOwner: "ops", AddedAlerts: 1, AddedRunbooks: 1},
		MonolithExperiment: claim("monolith"), ExistingRoleOption: Alternative{Source: "artifacts/options.json", Option: "existing worker", Outcome: "insufficient"}, ScaleProcessOption: Alternative{Source: "artifacts/options.json", Option: "scale worker", Outcome: "insufficient"},
	}, DomainPackages: []string{"internal/connectivity"}, Processes: map[string][]string{"worker": {"internal/connectivity"}}, EvidenceDigests: map[string]string{}}
	for _, source := range evidenceSources(d) {
		d.EvidenceDigests[source] = "sha256:" + strings.Repeat("0", 64)
	}
	return d
}

func TestDecompositionDecisionRejectsTopologyDrivenSplit(t *testing.T) {
	d := completeDecision()
	d.Justification = "one service per domain because the team owns it"
	if err := Check(d); err == nil || !strings.Contains(err.Error(), TopologyOnly) {
		t.Fatalf("topology-only split accepted: %v", err)
	}
}

func TestTodo_ARCH_GO_019_Golden(t *testing.T) {
	digest, err := Digest(completeDecision())
	if err != nil {
		t.Fatal(err)
	}
	const want = "sha256:97c0f549d769c376164d177ffe567053781966c4c643ccdc08d8c21fd1b6cc12"
	if digest != want {
		t.Fatalf("digest drift: got %s want %s", digest, want)
	}
}

func TestTodo_ARCH_GO_019_Race(t *testing.T) {
	d := completeDecision()
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := Check(d); err != nil {
				t.Errorf("concurrent validation: %v", err)
			}
		}()
	}
	wg.Wait()
}

func TestTodo_ARCH_GO_019_Security(t *testing.T) {
	root, index := scanFixture(t, nil)
	outside := filepath.Join(t.TempDir(), "decision.json")
	writeJSON(t, outside, completeDecision())
	idx := BoundaryIndex{Version: 1, Baseline: []Boundary{{Kind: "module", Identity: "go.mod"}}, Reviewed: []Review{{Kind: "process", Identity: "cmd/worker", Path: outside}}}
	writeJSON(t, index, idx)
	os.MkdirAll(filepath.Join(root, "cmd", "worker"), 0o755)
	if err := ScanRootWithInventory(root, index, map[string][]string{"worker": {"internal/connectivity"}}); err == nil || !strings.Contains(err.Error(), "escapes root") {
		t.Fatalf("absolute review path accepted: %v", err)
	}
}

func TestTodo_ARCH_GO_019_Conformance(t *testing.T) {
	manifest := "apiVersion: v1\nkind: List\nitems:\n  - apiVersion: apps/v1\n    kind: Deployment\n    metadata: {name: api}\n  - apiVersion: v1\n    kind: Service\n    metadata: {name: api}\n"
	boundaries, err := yamlServiceBoundaries("stack.yaml", []byte(manifest))
	if err != nil || len(boundaries) != 2 || boundaries[0].Identity != "stack.yaml#deployment/default/api" || boundaries[1].Identity != "stack.yaml#service/default/api" {
		t.Fatalf("Kubernetes List resources not enumerated: %#v, %v", boundaries, err)
	}
	compose := "services:\n  api: {image: api}\n  worker: {image: worker}\n"
	boundaries, err = yamlServiceBoundaries("compose.yaml", []byte(compose))
	if err != nil || len(boundaries) != 2 || boundaries[0].Identity != "compose.yaml#compose/api" || boundaries[1].Identity != "compose.yaml#compose/worker" {
		t.Fatalf("Compose resources not enumerated: %#v, %v", boundaries, err)
	}
}

func TestTodo_ARCH_GO_019_Mutation(t *testing.T) {
	d := completeDecision()
	d.Evidence.MonolithExperiment.Value = d.Evidence.MonolithExperiment.Limit
	if err := Check(d); err == nil || !strings.Contains(err.Error(), UnmeasuredEvidence) {
		t.Fatalf("non-exceeding experiment accepted: %v", err)
	}
}

func TestScannerRejectsSecondResourceAliasesMalformedAndOpaqueDockerChange(t *testing.T) {
	root, index := scanFixture(t, nil)
	boundaries, err := yamlServiceBoundaries("stack.yaml", []byte("kind: Deployment\nmetadata: {name: api}\n---\nkind: Service\nmetadata: {name: api}\n"))
	if err != nil || len(boundaries) != 2 || !strings.Contains(boundaries[1].Identity, "#service/default/api") {
		t.Fatalf("second YAML resource escaped: %#v, %v", boundaries, err)
	}
	if _, err := yamlServiceBoundaries("stack.yaml", []byte("base: &base\n  kind: Service\n  metadata: {name: api}\ncopy: *base\n")); err == nil || !strings.Contains(err.Error(), "aliases") {
		t.Fatalf("alias topology accepted: %v", err)
	}
	if _, err := yamlServiceBoundaries("stack.yaml", []byte("kind: [")); err == nil {
		t.Fatalf("malformed YAML accepted: %v", err)
	}
	os.WriteFile(filepath.Join(root, "Dockerfile"), []byte("FROM scratch\n"), 0o600)
	err1 := ScanRoot(root, index)
	os.WriteFile(filepath.Join(root, "Dockerfile"), []byte("FROM busybox\n"), 0o600)
	err2 := ScanRoot(root, index)
	if err1 == nil || err2 == nil || err1.Error() == err2.Error() {
		t.Fatalf("Docker content was not represented by opaque changing hashes: %v / %v", err1, err2)
	}
}

func TestScannerFindsNestedModuleCommandsAndRejectsSymlink(t *testing.T) {
	root, index := scanFixture(t, nil)
	os.MkdirAll(filepath.Join(root, "nested", "cmd", "worker"), 0o755)
	os.WriteFile(filepath.Join(root, "nested", "go.mod"), []byte("module example/nested\n\ngo 1.26\n"), 0o600)
	boundaries, err := discoverBoundaries(root)
	if err != nil || !containsBoundary(boundaries, Boundary{Kind: "process", Identity: "nested/cmd/worker"}) {
		t.Fatalf("nested module command escaped: %#v, %v", boundaries, err)
	}
	target := filepath.Join(t.TempDir(), "outside.yaml")
	os.WriteFile(target, []byte("kind: Service\nmetadata: {name: outside}\n"), 0o600)
	link := filepath.Join(root, "linked.yaml")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if err := ScanRoot(root, index); err == nil || !strings.Contains(err.Error(), "symbolic link") {
		t.Fatalf("symlink accepted: %v", err)
	}
}

func TestFrozenBaselineRejectsAppendBypass(t *testing.T) {
	root, index := scanFixture(t, nil)
	os.MkdirAll(filepath.Join(root, "cmd", "new"), 0o755)
	writeJSON(t, index, BoundaryIndex{Version: 1, Baseline: []Boundary{{Kind: "module", Identity: "go.mod"}, {Kind: "process", Identity: "cmd/new"}}})
	if err := ScanRoot(root, index); err == nil || !strings.Contains(err.Error(), "frozen initial baseline") {
		t.Fatalf("new boundary smuggled into baseline: %v", err)
	}
}

func containsBoundary(boundaries []Boundary, want Boundary) bool {
	for _, boundary := range boundaries {
		if boundary == want {
			return true
		}
	}
	return false
}

func TestReviewedProcessRequiresExactTargetAndLiveInventory(t *testing.T) {
	root, index := scanFixture(t, []Boundary{{Kind: "module", Identity: "go.mod"}})
	os.MkdirAll(filepath.Join(root, "cmd", "worker"), 0o755)
	d := completeDecision()
	d.Target = "cmd/other"
	writeJSON(t, filepath.Join(root, "decision.json"), d)
	writeJSON(t, index, BoundaryIndex{Version: 1, Baseline: []Boundary{{Kind: "module", Identity: "go.mod"}}, Reviewed: []Review{{Kind: "process", Identity: "cmd/worker", Path: "decision.json"}}})
	inv := map[string][]string{"worker": {"internal/connectivity"}}
	if err := ScanRootWithInventory(root, index, inv); err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("wrong reviewed target accepted: %v", err)
	}
	d.Target = "cmd/worker"
	d.Processes["worker"] = append(d.Processes["worker"], "internal/hidden")
	materializeEvidence(t, root, &d)
	writeJSON(t, filepath.Join(root, "decision.json"), d)
	if err := ScanRootWithInventory(root, index, inv); err == nil || !strings.Contains(err.Error(), InventoryMismatch) {
		t.Fatalf("partial live inventory accepted: %v", err)
	}
}

func materializeEvidence(t *testing.T, root string, d *Decision) {
	t.Helper()
	for _, source := range evidenceSources(*d) {
		path := filepath.Join(root, filepath.FromSlash(source))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		data := []byte("measured evidence for " + source + "\n")
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(data)
		d.EvidenceDigests[source] = fmt.Sprintf("sha256:%x", sum[:])
	}
}

func TestInventoryRejectsPackagesHiddenOutsideDeclaredScope(t *testing.T) {
	d := completeDecision()
	d.Processes["worker"] = append(d.Processes["worker"], "internal/hidden")
	if err := CheckInventory(d, map[string][]string{"worker": {"internal/connectivity", "internal/hidden"}}); err == nil || !strings.Contains(err.Error(), InventoryMismatch) {
		t.Fatalf("submitted ownership outside domain_packages accepted: %v", err)
	}
}

func TestReviewedEvidenceRequiresPinnedRepositoryBytesAndIntegerMoney(t *testing.T) {
	root := t.TempDir()
	d := completeDecision()
	materializeEvidence(t, root, &d)
	if err := VerifyEvidence(root, d); err != nil {
		t.Fatalf("pinned evidence rejected: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(d.Evidence.Scaling.Source)), []byte("changed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := VerifyEvidence(root, d); err == nil || !strings.Contains(err.Error(), "digest") {
		t.Fatalf("changed evidence accepted: %v", err)
	}
	d = completeDecision()
	d.Evidence.OperationalCost.MonthlyMinor = 0
	if err := Check(d); err == nil || !strings.Contains(err.Error(), UnmeasuredEvidence) {
		t.Fatalf("zero minor-unit cost accepted: %v", err)
	}
}

func TestCheckFileRejectsMissingVersionAndFloatMoney(t *testing.T) {
	d := completeDecision()
	d.Version = 0
	path := filepath.Join(t.TempDir(), "missing-version.json")
	writeJSON(t, path, d)
	if err := CheckFile(path); err == nil || !strings.Contains(err.Error(), "version") {
		t.Fatalf("missing decision version accepted: %v", err)
	}
	b, err := json.Marshal(completeDecision())
	if err != nil {
		t.Fatal(err)
	}
	raw := strings.Replace(string(b), `"monthly_minor":10000`, `"monthly_usd":0.01`, 1)
	path = filepath.Join(t.TempDir(), "float-money.json")
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := CheckFile(path); err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("floating-point money accepted: %v", err)
	}
}

func scanFixture(t *testing.T, baseline []Boundary) (string, string) {
	t.Helper()
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example\n\ngo 1.26\n"), 0o600)
	index := filepath.Join(root, "index.json")
	if baseline == nil {
		baseline = []Boundary{{Kind: "module", Identity: "go.mod"}}
	}
	writeJSON(t, index, BoundaryIndex{Version: 1, Baseline: baseline})
	return root, index
}

func writeJSON(t *testing.T, path string, value any) {
	t.Helper()
	b, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatal(err)
	}
}
