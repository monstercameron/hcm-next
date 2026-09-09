package archdoc_test

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/tools/gen/archdoc"
)

func TestArchitectureDocumentationMatchesImportGraph(t *testing.T) {
	want, err := os.ReadFile(filepath.Join("testdata", "architecture.md"))
	if err != nil {
		t.Fatalf("read architecture golden: %v", err)
	}
	got, err := archdoc.Generate(repoRoot(t))
	if err != nil {
		t.Fatalf("generate architecture document: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("architecture document drifted; regenerate testdata/architecture.md from the current manifests")
	}
}

func TestTodo_ARCH_GO_028_Property(t *testing.T) {
	first, err := archdoc.Generate(repoRoot(t))
	if err != nil {
		t.Fatalf("first generation: %v", err)
	}
	second, err := archdoc.Generate(repoRoot(t))
	if err != nil {
		t.Fatalf("second generation: %v", err)
	}
	if !bytes.Equal(first, second) {
		t.Fatal("architecture generation is not deterministic")
	}
}

func TestTodo_ARCH_GO_028_Golden(t *testing.T) {
	document, err := archdoc.Generate(repoRoot(t))
	if err != nil {
		t.Fatalf("generate architecture document: %v", err)
	}
	for _, section := range []string{
		"## Declared layers and roots",
		"## Allowed dependency edges",
		"## Library firewall roots",
		"## Package inventory by declared root",
	} {
		if !strings.Contains(string(document), section) {
			t.Errorf("generated document lacks section %q", section)
		}
	}
}

func TestTodo_ARCH_GO_028_Integration(t *testing.T) {
	document, err := archdoc.Generate(repoRoot(t))
	if err != nil {
		t.Fatalf("generate from repository tree: %v", err)
	}
	if !strings.Contains(string(document), "`github.com/monstercameron/human-capital-management-suite/tools/gen/archdoc`") {
		t.Fatal("package inventory does not include the generated architecture package")
	}
}

func TestTodo_ARCH_GO_028_Conformance(t *testing.T) {
	document, err := archdoc.Generate(repoRoot(t))
	if err != nil {
		t.Fatalf("generate architecture document: %v", err)
	}
	text := string(document)
	for _, source := range []string{
		"definitions/architecture/repository-layout.yaml",
		"definitions/architecture/package-dependency-policy.yaml",
		"definitions/architecture/dependency-roles.yaml",
	} {
		if !strings.Contains(text, source) {
			t.Errorf("generated document omits source manifest %q", source)
		}
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve test source path")
	}
	return filepath.Dir(filepath.Dir(filepath.Dir(filepath.Dir(file))))
}
