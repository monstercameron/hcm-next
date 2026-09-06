package obligations_test

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/monstercameron/hcm-next/tools/planning/obligations"
)

func TestPlanningRequirementRegistryRejectsUntrackedNormativeObligations(t *testing.T) {
	items := obligations.ScanDocument("planning/specs/example.md", "The service MUST preserve tenant scope.\n")
	if len(items) != 1 {
		t.Fatalf("found %d obligations, want 1", len(items))
	}
	if items[0].ID == "" || items[0].TextDigest == "" {
		t.Fatal("obligation lacks stable identity or digest")
	}
	if items[0].Line != 1 {
		t.Fatalf("line = %d, want 1", items[0].Line)
	}
}

func TestTodo_GOV_022_Property(t *testing.T) {
	first := obligations.ScanDocument("planning/specs/example.md", "A rule MUST be deterministic.\n")
	second := obligations.ScanDocument("planning/specs/example.md", "A rule MUST be deterministic.\n")
	if !bytes.Equal(mustJSON(t, first), mustJSON(t, second)) {
		t.Fatal("obligation scan is not deterministic")
	}
}

func TestTodo_GOV_022_Golden(t *testing.T) {
	registry, err := obligations.Scan(repoRoot(t))
	if err != nil {
		t.Fatalf("scan repository: %v", err)
	}
	want, err := osRead(filepath.Join("testdata", "requirements.json"))
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	got, err := registry.JSON()
	if err != nil {
		t.Fatalf("marshal registry: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatal("requirements registry drifted; regenerate testdata/requirements.json")
	}
}

func FuzzTodo_GOV_022(f *testing.F) {
	f.Add("The boundary MUST remain deterministic.")
	f.Fuzz(func(t *testing.T, text string) {
		items := obligations.ScanDocument("planning/specs/fuzz.md", text)
		for _, item := range items {
			if item.ID == "" || item.TextDigest == "" || item.Line < 1 {
				t.Fatalf("invalid obligation: %+v", item)
			}
		}
	})
}

func TestTodo_GOV_022_Conformance(t *testing.T) {
	content := "The policy MUST hold.\n"
	previous := obligations.Registry{Version: 1, Obligations: obligations.ScanDocument("planning/specs/example.md", content)}
	current := obligations.Registry{Version: 1, Obligations: obligations.ScanDocument("planning/specs/example.md", "The policy MUST evolve.\n")}
	if findings := obligations.ValidateLineage(current, previous); len(findings) != 1 {
		t.Fatalf("ValidateLineage findings = %v, want one stale-lineage finding", findings)
	}
	current = obligations.AttachLineage(current, previous)
	if findings := obligations.ValidateLineage(current, previous); len(findings) != 0 {
		t.Fatalf("attached lineage still invalid: %v", findings)
	}
}

func TestTodo_GOV_022_Mutation(t *testing.T) {
	old := obligations.ScanDocument("planning/specs/example.md", "A control MUST hold.\n")[0]
	changed := obligations.ScanDocument("planning/specs/example.md", "A control MUST change.\n")[0]
	if old.ID == changed.ID || old.TextDigest == changed.TextDigest {
		t.Fatal("changed normative text retained the old identity or digest")
	}
}

func mustJSON(t *testing.T, items []obligations.Obligation) []byte {
	t.Helper()
	data, err := (obligations.Registry{Version: 1, Obligations: items}).JSON()
	if err != nil {
		t.Fatalf("marshal fixture: %v", err)
	}
	return data
}

func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve test source path")
	}
	return filepath.Dir(filepath.Dir(filepath.Dir(filepath.Dir(file))))
}

func osRead(path string) ([]byte, error) {
	return os.ReadFile(path)
}
