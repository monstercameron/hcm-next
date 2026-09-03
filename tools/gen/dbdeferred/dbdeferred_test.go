package dbdeferred

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/monstercameron/hcm-next/tools/gen/schemaflux/sources"
)

func bundle(t *testing.T) sources.Bundle {
	t.Helper()
	root := filepath.Clean(filepath.Join("..", "..", ".."))
	mm, err := sources.LoadMetamodel(filepath.Join(root, "schema", "schemaflux", "metamodel", "v1", "metamodel.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	a, r, err := sources.LoadRegistries(filepath.Join(root, "schema", "schemaflux", "registries", "v1", "registries.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	e, rel, err := sources.LoadEntityFamilies(filepath.Join(root, "schema", "schemaflux", "deferred", "v1"))
	if err != nil {
		t.Fatal(err)
	}
	return sources.Bundle{Metamodel: mm, Authorities: a, Retentions: r, Entities: e, Relationships: rel}
}

func TestGenerateDeferredPlan(t *testing.T) {
	p, err := Generate(bundle(t))
	if err != nil {
		t.Fatal(err)
	}
	if err := Check(p); err != nil {
		t.Fatal(err)
	}
	if len(p.Tables) != 12 {
		t.Fatalf("tables=%d", len(p.Tables))
	}
}
func TestLoadAndGenerate(t *testing.T) {
	root := filepath.Clean(filepath.Join("..", "..", ".."))
	p, err := LoadAndGenerate(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := Check(p); err != nil {
		t.Fatal(err)
	}
}
func TestDeterministic(t *testing.T) {
	a, _ := Generate(bundle(t))
	b, _ := Generate(bundle(t))
	if a.Digest() != b.Digest() || a.SQL != b.SQL {
		t.Fatal("nondeterministic output")
	}
}
func TestAllDraft(t *testing.T) {
	p, _ := Generate(bundle(t))
	for _, x := range p.Tables {
		if !strings.Contains(x.SQL, "DRAFT") {
			t.Errorf("%s", x.Name)
		}
	}
}
func TestNoWriteTokens(t *testing.T) {
	p, _ := Generate(bundle(t))
	for _, x := range []string{"INSERT ", "UPDATE ", "DELETE ", "ALTER ", "GRANT ", "CREATE ROLE"} {
		if strings.Contains(strings.ToUpper(p.SQL), x) {
			t.Errorf("token %s", x)
		}
	}
}
func TestRejectWrongFamily(t *testing.T) {
	p, _ := Generate(bundle(t))
	p.Family = "production"
	if Check(p) == nil {
		t.Fatal("accepted wrong family")
	}
}
func TestRejectMissingMarker(t *testing.T) {
	p, _ := Generate(bundle(t))
	p.SQL = "CREATE TABLE x"
	if Check(p) == nil {
		t.Fatal("accepted missing marker")
	}
}
func TestRejectDigest(t *testing.T) {
	p, _ := Generate(bundle(t))
	p.PlanDigest = "sha256:bad"
	if Check(p) == nil {
		t.Fatal("accepted bad digest")
	}
}
func TestRejectEmpty(t *testing.T) {
	if Check(Plan{}) == nil {
		t.Fatal("accepted empty plan")
	}
}
func TestRejectMutation(t *testing.T) {
	b := bundle(t)
	b.Entities[0].Status = "ACTIVE"
	if _, err := Generate(b); err == nil {
		t.Fatal("accepted active deferred source")
	}
}
func TestRejectCovered(t *testing.T) {
	b := bundle(t)
	b.Entities[0].Covered = true
	if _, err := Generate(b); err == nil {
		t.Fatal("accepted covered deferred source")
	}
}
func TestRejectWriteSQL(t *testing.T) {
	p, _ := Generate(bundle(t))
	p.SQL += "\nINSERT INTO x;"
	if Check(p) == nil {
		t.Fatal("accepted write SQL")
	}
}
