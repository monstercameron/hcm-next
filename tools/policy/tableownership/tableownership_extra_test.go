package tableownership

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/tools/policy/tableinventory"
)

func ownershipRepoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Dir(filepath.Dir(filepath.Dir(filepath.Dir(file))))
}

func TestTableOwnership_MetadataAndReportMethods(t *testing.T) {
	if Version() != 1 || ModulePath != "github.com/monstercameron/human-capital-management-suite" {
		t.Fatalf("metadata mismatch: version=%d module=%q", Version(), ModulePath)
	}
	finding := Finding{Table: "worker", Code: "OWNERLESS", Detail: "missing"}
	if got := finding.Error(); !strings.Contains(got, "tableownership: worker: OWNERLESS: missing") {
		t.Fatalf("Finding.Error() = %q", got)
	}
	clean := Report{Consumers: map[string][]string{"worker": {"pkg"}}}
	if !clean.OK() || !strings.Contains(clean.Explain(), "1 consumer mapping") {
		t.Fatalf("clean report methods disagree: ok=%v explain=%q", clean.OK(), clean.Explain())
	}
	dirty := Report{Findings: []Finding{{Table: "worker"}}}
	if dirty.OK() {
		t.Fatal("report with findings was reported OK")
	}
}

func TestTableOwnership_ValidateBranchesAndSorting(t *testing.T) {
	inventory := tableinventory.Inventory{Tables: []tableinventory.Table{
		{Table: "zeta"},
		{Table: "alpha", OwnerPackage: "internal/a"},
		{Table: " ", OwnerPackage: "internal/ignored"},
	}}
	findings := Validate(inventory, map[string][]string{"alpha": {"pkg"}})
	if len(findings) != 2 || findings[0].Table != "zeta" || findings[0].Code != "CONSUMERLESS" || findings[1].Table != "zeta" || findings[1].Code != "OWNERLESS" {
		t.Fatalf("Validate findings = %+v", findings)
	}
	lower := tableinventory.Inventory{Tables: []tableinventory.Table{{Table: "Worker", OwnerPackage: "pkg"}}}
	if got := Validate(lower, map[string][]string{"worker": {"pkg"}}); len(got) != 0 {
		t.Fatalf("case-insensitive consumer lookup was rejected: %+v", got)
	}
}

func TestTableOwnership_DiscoverConsumersBoundaries(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "pkg"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "vendor"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "pkg", "consumer.go"), []byte("package pkg\nconst Query = `SELECT * FROM worker`\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "pkg", "duplicate.go"), []byte("package pkg\nvar Again = \"worker\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "pkg", "false.go"), []byte("package pkg\nvar Other = \"workerhouse\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "vendor", "ignored.go"), []byte("package ignored\nvar Worker = \"worker\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".git", "ignored.go"), []byte("package ignored\nvar Worker = \"worker\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	consumers, err := DiscoverConsumers(root, []tableinventory.Table{{Table: "worker"}, {Table: "other"}})
	if err != nil {
		t.Fatal(err)
	}
	wantPackage := ModulePath + "/pkg"
	if len(consumers["worker"]) != 1 || consumers["worker"][0] != wantPackage || len(consumers["other"]) != 0 {
		t.Fatalf("DiscoverConsumers = %+v", consumers)
	}
	if !containsWord("worker worker", "worker") || containsWord("workerhouse", "worker") || containsWord("", "worker") {
		t.Fatal("containsWord boundary behavior is incorrect")
	}
	if got := appendUnique([]string{"a"}, "a"); len(got) != 1 {
		t.Fatalf("appendUnique duplicated an existing value: %v", got)
	}
	if got := appendUnique([]string{"a"}, "b"); len(got) != 2 || got[1] != "b" {
		t.Fatalf("appendUnique failed to append a new value: %v", got)
	}
	if isIdentifier('-') || !isIdentifier('_') || !isIdentifier('9') {
		t.Fatal("isIdentifier classification is incorrect")
	}
}

func TestTableOwnership_EvaluateAndCheckErrors(t *testing.T) {
	if _, err := Evaluate(t.TempDir()); err == nil {
		t.Fatal("Evaluate accepted a root without the table registry")
	}
	if err := Check(ownershipRepoRoot(t)); err == nil || !strings.Contains(err.Error(), "tableownership:") {
		t.Fatalf("Check error = %v, want a reported repository finding", err)
	}
}
