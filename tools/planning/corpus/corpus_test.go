package corpus

import (
	"bytes"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func fixtureCorpus() Corpus {
	return Corpus{
		Obligations:   []Obligation{{ID: "REQ-1", Document: "planning/specs/a.md", Line: 4, TodoIDs: []string{"GOV-1"}}},
		Specs:         []SpecDocument{{Path: "planning/specs/a.md", Content: "Worker and Position are governed."}},
		Workflows:     []Workflow{{ID: "WF-1", Name: "Promote", Intents: []string{"hcmnext.people.promote_worker/v1"}, TodoIDs: []string{"GOV-1"}, SpecRefs: []string{"planning/specs/a.md"}, ModelText: []string{"Worker"}}},
		ModelEntities: []ModelEntity{{Ref: "Worker/v1", Key: "worker"}},
		Todos:         []Todo{{ID: "GOV-1", Test: "TestProof", Refs: "[spec](specs/a.md)"}},
	}
}

func TestPlanningCorpusGraphRejectsOrphanContractOrFalseCoverage(t *testing.T) {
	c := fixtureCorpus()
	good := Reconcile(c, Options{TestNames: map[string]bool{"TestProof": true}, CheckOracles: true})
	if len(good.NewOrphans) != 0 {
		t.Fatalf("complete fixture has orphan paths: %+v", good.NewOrphans)
	}
	c.Obligations[0].TodoIDs = nil
	bad := Reconcile(c)
	if len(bad.NewOrphans) != 1 || bad.NewOrphans[0].Kind != "spec_obligation" {
		t.Fatalf("missing requirement edge was not rejected: %+v", bad.NewOrphans)
	}
	c = fixtureCorpus()
	c.Todos[0].Refs = "[named document](specs/missing.md)"
	bad = Reconcile(c)
	if len(bad.NewOrphans) != 1 || bad.NewOrphans[0].Kind != "todo_authority" {
		t.Fatalf("unresolved authority reference was not rejected: %+v", bad.NewOrphans)
	}
	c = fixtureCorpus()
	c.Workflows[0].TodoIDs = nil
	bad = Reconcile(c)
	if len(bad.NewOrphans) != 1 || bad.NewOrphans[0].Kind != "workflow_todos" {
		t.Fatalf("missing workflow proof edge was not rejected: %+v", bad.NewOrphans)
	}
}

func TestTodo_GOV_024_Property(t *testing.T) {
	a := Reconcile(fixtureCorpus())
	b := Reconcile(fixtureCorpus())
	if a.MatrixDigest() != b.MatrixDigest() || a.JSONString() != b.JSONString() {
		t.Fatal("reconciliation is not deterministic")
	}
}

func TestTodo_GOV_024_Golden(t *testing.T) {
	report := Reconcile(fixtureCorpus())
	want, err := os.ReadFile(filepath.Join("testdata", "matrix-digest.golden"))
	if err != nil {
		t.Fatal(err)
	}
	if string(bytes.TrimSpace(want)) != report.MatrixDigest() {
		t.Fatalf("matrix digest = %s, want %s", report.MatrixDigest(), bytes.TrimSpace(want))
	}
}

func TestTodo_GOV_024_Race(t *testing.T) {
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if len(Reconcile(fixtureCorpus()).Matrix.Pairs) != 6 {
				t.Error("incomplete matrix")
			}
		}()
	}
	wg.Wait()
}

func TestTodo_GOV_024_Conformance(t *testing.T) {
	report := Reconcile(fixtureCorpus())
	if len(report.Matrix.Pairs) != 6 {
		t.Fatalf("pair count = %d, want six source pairs", len(report.Matrix.Pairs))
	}
	for _, pair := range report.Matrix.Pairs {
		if pair.Count < 0 {
			t.Fatalf("negative pair count: %+v", pair)
		}
	}
	if report.MatrixDigest() == "" {
		t.Fatal("matrix digest is empty")
	}
}

func TestTodo_GOV_024_Mutation(t *testing.T) {
	c := fixtureCorpus()
	c.ModelEntities = append(c.ModelEntities, ModelEntity{Ref: "Unclaimed/v1", Key: "unclaimed"})
	report := Reconcile(c)
	if len(report.NewOrphans) != 1 || report.NewOrphans[0].Kind != "model_entity" || report.NewOrphans[0].ID != "Unclaimed/v1" {
		t.Fatalf("mutation did not produce the complete model orphan path: %+v", report.NewOrphans)
	}
	accepted := Reconcile(c, Options{Allowlist: []AllowlistEntry{{Kind: "model_entity", ID: "Unclaimed/v1", Owner: "model-owner", Reason: "deferred in current slice", ReviewDate: "2026-09-05"}}})
	if len(accepted.NewOrphans) != 0 || len(accepted.Allowlisted) != 1 {
		t.Fatalf("owner-backed allowlist did not classify exact orphan: %+v", accepted)
	}
}

func (r Report) JSONString() string { b, _ := r.JSON(); return string(b) }

func TestLoadAllowlistMissingIsEmpty(t *testing.T) {
	entries, err := LoadAllowlist(filepath.Join(t.TempDir(), "missing.json"))
	if err != nil || len(entries) != 0 {
		t.Fatalf("missing allowlist = %v, %v", entries, err)
	}
}

func TestBuildAllowlistUsesExactOwnerBackedKeys(t *testing.T) {
	entries := BuildAllowlist([]Orphan{{Kind: "todo_oracle", ID: "T-1"}, {Kind: "todo_oracle", ID: "T-1"}}, "2026-09-05")
	if len(entries) != 1 || entries[0].Owner != "test-governance" || entries[0].ID != "T-1" {
		t.Fatalf("baseline allowlist = %+v", entries)
	}
	if err := WriteAllowlist(filepath.Join(t.TempDir(), "nested", "allowlist.json"), entries); err != nil {
		t.Fatal(err)
	}
}
