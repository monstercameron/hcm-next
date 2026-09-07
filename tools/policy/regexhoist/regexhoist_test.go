package regexhoist

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func repositoryRoot(t *testing.T) string {
	t.Helper()
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	root := filepath.Dir(source)
	for range 3 {
		root = filepath.Dir(root)
	}
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("repository root %s: %v", root, err)
	}
	return root
}

func TestTodo_PERFOPT_003(t *testing.T) {
	root := repositoryRoot(t)
	dirs := []string{
		"internal/governance/legal/extract",
		"internal/domains/dataops/importing",
		"internal/intent/model",
		"internal/customobject",
		"internal/trust/dlp",
		"internal/data/partition",
		"internal/domains/paymethod",
	}
	findings, err := Scan(root, dirs)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(findings) != 0 {
		t.Fatalf("regexp compilation inside function bodies: %v", findings)
	}
}

func TestScan_FixtureCatchesIndentedCompile(t *testing.T) {
	root := repositoryRoot(t)
	findings, err := Scan(root, []string{"tools/policy/regexhoist/testdata/fixture"})
	if err != nil {
		t.Fatalf("Scan fixture: %v", err)
	}
	want := []string{
		"tools/policy/regexhoist/testdata/fixture/fixture.go:8 (function-body)",
		"tools/policy/regexhoist/testdata/fixture/fixture.go:16 (alias)",
	}
	if len(findings) != len(want) {
		t.Fatalf("findings = %v, want %v (the marked dynamic call and init must not be reported)", findings, want)
	}
	for i := range want {
		if got := findings[i].String(); got != want[i] {
			t.Fatalf("finding[%d] = %q, want %q", i, got, want[i])
		}
	}
}

func TestScan_MarkerExemptsOnlyItsOwnLine(t *testing.T) {
	dir := t.TempDir()
	src := "package p\n\nimport \"regexp\"\n\nfunc a(s string) *regexp.Regexp {\n\treturn regexp.MustCompile(s) // " + DynamicMarker + "\n}\n\nfunc b(s string) *regexp.Regexp {\n\t// " + DynamicMarker + " on the previous line does not count\n\treturn regexp.MustCompile(s)\n}\n"
	if err := os.WriteFile(filepath.Join(dir, "p.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	findings, err := Scan(dir, []string{dir})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(findings) != 1 || findings[0].Line != 11 || findings[0].Kind != "function-body" {
		t.Fatalf("findings = %v, want only p.go:11 (function-body)", findings)
	}
}
