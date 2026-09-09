package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/tools/quality/decomposition"
)

func TestMainSmoke(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panic: %v", r)
		}
	}()
}

func TestMainNoPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panic: %v", r)
		}
	}()
}

func TestDecompositionCommandValidatesSubmittedDecision(t *testing.T) {
	path := writeDecisionFixture(t, validDecisionJSON())
	var out, errOut bytes.Buffer
	if code := run([]string{"decomposition", "-decision", path}, &out, &errOut); code != 0 {
		t.Fatalf("valid decision exit=%d stderr=%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "PASS") {
		t.Fatalf("valid decision output=%q", out.String())
	}
}

func TestDecompositionCommandRejectsInventedOwnershipSubset(t *testing.T) {
	path := writeDecisionFixture(t, strings.Replace(validDecisionJSON(), `"processes":{"worker":["internal/connectivity"]}`, `"processes":{"hcmnext":["internal/connectivity"]}`, 1))
	var out, errOut bytes.Buffer
	if code := run([]string{"decomposition", "-decision", path}, &out, &errOut); code == 0 || !strings.Contains(errOut.String(), "INVENTORY_MISMATCH") {
		t.Fatalf("invented ownership accepted: exit=%d stdout=%s stderr=%s", code, out.String(), errOut.String())
	}
}

func TestDecompositionCommandRequiresDecisionFile(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := run([]string{"decomposition"}, &out, &errOut); code != 2 || !strings.Contains(errOut.String(), "usage") {
		t.Fatalf("missing decision was not rejected: exit=%d stderr=%s", code, errOut.String())
	}
}

func TestDecompositionRootScanRejectsUnreviewedAndAcceptsIndexedBoundary(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example\n\ngo 1.26\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "decision.json"), []byte(validDecisionJSON()), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "definitions", "architecture"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "definitions", "architecture", "process-roles.yaml"), []byte("processes:\n  - command: new\n    semantic_packages: [internal/connectivity]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	index := filepath.Join(root, "index.json")
	writeIndex(t, index, decomposition.BoundaryIndex{Version: 1, Baseline: []decomposition.Boundary{{Kind: "module", Identity: "go.mod"}}})
	var out, errOut bytes.Buffer
	if code := run([]string{"decomposition", "-root", root, "-index", index}, &out, &errOut); code != 0 {
		t.Fatalf("baseline scan failed: %d %s", code, errOut.String())
	}
	if err := os.MkdirAll(filepath.Join(root, "cmd", "new"), 0o755); err != nil {
		t.Fatal(err)
	}
	errOut.Reset()
	out.Reset()
	if code := run([]string{"decomposition", "-root", root, "-index", index}, &out, &errOut); code == 0 || !strings.Contains(errOut.String(), "unreviewed new boundary") {
		t.Fatalf("unreviewed process accepted: %d %s", code, errOut.String())
	}
	decisionBytes := strings.Replace(validDecisionJSON(), `"target":"worker"`, `"target":"cmd/new"`, 1)
	decisionBytes = strings.Replace(decisionBytes, `"processes":{"worker":["internal/connectivity"]}`, `"processes":{"new":["internal/connectivity"]}`, 1)
	decisionBytes = materializeDecisionEvidence(t, root, decisionBytes)
	if err := os.WriteFile(filepath.Join(root, "decision.json"), []byte(decisionBytes), 0o600); err != nil {
		t.Fatal(err)
	}
	writeIndex(t, index, decomposition.BoundaryIndex{Version: 1, Baseline: []decomposition.Boundary{{Kind: "module", Identity: "go.mod"}}, Reviewed: []decomposition.Review{{Kind: "process", Identity: "cmd/new", Path: "decision.json"}}})
	errOut.Reset()
	out.Reset()
	if code := run([]string{"decomposition", "-root", root, "-index", index}, &out, &errOut); code != 0 {
		t.Fatalf("reviewed process failed: %d %s", code, errOut.String())
	}
}

func materializeDecisionEvidence(t *testing.T, root, raw string) string {
	t.Helper()
	var d decomposition.Decision
	if err := json.Unmarshal([]byte(raw), &d); err != nil {
		t.Fatal(err)
	}
	for source := range d.EvidenceDigests {
		path := filepath.Join(root, filepath.FromSlash(source))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		data := []byte("evidence: " + source + "\n")
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(data)
		d.EvidenceDigests[source] = "sha256:" + fmt.Sprintf("%x", sum[:])
	}
	b, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestDecompositionRootScanRejectsTraversalReviewPath(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example\n\ngo 1.26\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "definitions", "architecture"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "definitions", "architecture", "process-roles.yaml"), []byte("processes: []\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	index := filepath.Join(root, "index.json")
	writeIndex(t, index, decomposition.BoundaryIndex{Version: 1, Baseline: []decomposition.Boundary{{Kind: "module", Identity: "go.mod"}}, Reviewed: []decomposition.Review{{Kind: "module", Identity: "x", Path: "../decision.json"}}})
	var out, errOut bytes.Buffer
	if code := run([]string{"decomposition", "-root", root, "-index", index}, &out, &errOut); code == 0 || !strings.Contains(errOut.String(), "escapes root") {
		t.Fatalf("traversal path accepted: %d %s", code, errOut.String())
	}
}

func writeIndex(t *testing.T, path string, index decomposition.BoundaryIndex) {
	t.Helper()
	b, err := json.Marshal(index)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestDecompositionCommandRejectsMalformedDecision(t *testing.T) {
	path := writeDecisionFixture(t, `{"id":`)
	var out, errOut bytes.Buffer
	if code := run([]string{"decomposition", "-decision", path}, &out, &errOut); code == 0 || !strings.Contains(errOut.String(), "parse decision") {
		t.Fatalf("malformed decision accepted: exit=%d stdout=%s stderr=%s", code, out.String(), errOut.String())
	}
}

func TestLoadProcessInventoryRejectsMalformedOrOwnerlessRows(t *testing.T) {
	path := filepath.Join(t.TempDir(), "process-roles.yaml")
	if err := os.WriteFile(path, []byte("processes: ["), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadProcessInventory(path); err == nil || !strings.Contains(err.Error(), "parse") {
		t.Fatalf("malformed inventory accepted: %v", err)
	}
	if err := os.WriteFile(path, []byte("processes:\n  - semantic_packages: [internal/example]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadProcessInventory(path); err == nil || !strings.Contains(err.Error(), "command") {
		t.Fatalf("ownerless inventory accepted: %v", err)
	}
	if err := os.WriteFile(path, []byte("processes:\n  - command: worker\n    semantic_packages: [internal/example]\n  - command: worker\n    semantic_packages: [internal/other]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadProcessInventory(path); err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("duplicate live process accepted: %v", err)
	}
}

func TestDecompositionCommandRejectsMixedScanAndDecisionModes(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := run([]string{"decomposition", "-root", ".", "-index", "index.json", "-decision", "decision.json"}, &out, &errOut); code != 2 || !strings.Contains(errOut.String(), "usage") {
		t.Fatalf("mixed command modes accepted: exit=%d stderr=%s", code, errOut.String())
	}
}

func writeDecisionFixture(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "decision.json")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func validDecisionJSON() string {
	var d decomposition.Decision
	if err := json.Unmarshal([]byte(legacyDecisionJSON()), &d); err != nil {
		panic(err)
	}
	d.Version = 2
	d.Evidence.OperationalCost.MonthlyMinor = 10000
	d.Evidence.OperationalCost.Currency = "USD"
	d.Evidence.Scaling.Source = "./go.mod"
	d.Evidence.FailureIsolation.Source = "./go.mod"
	d.Evidence.SecurityBoundary.Source = "./go.mod"
	d.Evidence.Residency.Source = "./go.mod"
	d.Evidence.ReleaseBoundary.Source = "./go.mod"
	d.Evidence.OwnershipBoundary.Source = "./go.mod"
	d.Evidence.APIEventContract.Source = "./go.mod"
	d.Evidence.DataAuthority.Source = "./go.mod"
	d.Evidence.FailureRepair.Source = "./go.mod"
	d.Evidence.MigrationRollback.Source = "./go.mod"
	d.Evidence.OperationalCost.Source = "./go.mod"
	d.Evidence.MonolithExperiment.Source = "./go.mod"
	d.Evidence.ExistingRoleOption.Source = "./go.mod"
	d.Evidence.ScaleProcessOption.Source = "./go.mod"
	root, err := findRepoRoot()
	if err != nil {
		panic(err)
	}
	contents, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		panic(err)
	}
	sum := sha256.Sum256(contents)
	d.EvidenceDigests = map[string]string{"./go.mod": "sha256:" + fmt.Sprintf("%x", sum[:])}
	b, err := json.Marshal(d)
	if err != nil {
		panic(err)
	}
	return string(b)
}

func legacyDecisionJSON() string {
	return `{"id":"ARCH-GO-019","kind":"process","target":"worker","justification":"measured queue failure isolation exceeds modular monolith limit","evidence":{"scaling":{"source":"artifacts/load.json","measure":"queue depth","value":20,"limit":10,"unit":"items","outcome":"exceeds"},"failure_isolation":{"source":"artifacts/failure.json","measure":"restart recovery","value":2,"limit":1,"unit":"minutes","outcome":"isolated"},"security_boundary":{"source":"artifacts/security.json","measure":"identity","value":2,"limit":1,"unit":"boundary","outcome":"separate"},"residency":{"source":"artifacts/residency.json","measure":"region","value":2,"limit":1,"unit":"regions","outcome":"contained"},"release_boundary":{"source":"artifacts/release.json","measure":"cadence","value":2,"limit":1,"unit":"releases","outcome":"independent"},"ownership_boundary":{"source":"artifacts/ownership.json","measure":"on-call","value":2,"limit":1,"unit":"owners","outcome":"assigned"},"api_event_contract":{"source":"artifacts/api.json","authority":"platform","version":"v1","consistency":"compatible"},"data_authority":{"source":"artifacts/data.json","system":"ledger","writer":"worker"},"failure_repair":{"source":"artifacts/repair.json","failure":"crash","detection":"health","repair":"replay","rollback":"pause"},"migration_rollback":{"source":"artifacts/migration.json","failure":"cutover","detection":"audit","repair":"forward","rollback":"expand"},"operational_cost":{"source":"artifacts/cost.json","monthly_usd":100,"on_call_owner":"ops","added_alerts":1,"added_runbooks":1},"monolith_experiment":{"source":"artifacts/mono.json","measure":"safe limit","value":20,"limit":10,"unit":"qps","outcome":"exceeded"},"existing_role_option":{"source":"artifacts/options.json","option":"existing worker","outcome":"insufficient"},"scale_process_option":{"source":"artifacts/options.json","option":"scale worker","outcome":"insufficient"}},"domain_packages":["internal/connectivity"],"processes":{"worker":["internal/connectivity"]}}`
}
