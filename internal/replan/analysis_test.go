package replan

import "testing"

func TestAnalyzeMaterialClosure(t *testing.T) {
	graph := []Node{
		{ID: "pto", Kind: Fact},
		{ID: "hours", Kind: Calculation, Dependencies: []string{"pto"}},
		{ID: "write", Kind: Write, Dependencies: []string{"hours"}},
		{ID: "email", Kind: Effect, Dependencies: []string{"write"}},
		{ID: "unrelated", Kind: Calculation},
	}
	r := Analyze(Snapshot{"pto": "8"}, Snapshot{"pto": "4"}, graph)
	if len(r.Changed) != 1 || r.Changed[0] != "pto" {
		t.Fatalf("changed = %#v", r.Changed)
	}
	want := map[string]Status{"pto": Recompute, "hours": Invalidate, "write": Invalidate, "email": Invalidate, "unrelated": Unchanged}
	for _, f := range r.Nodes {
		if f.Status != want[f.ID] {
			t.Errorf("%s = %s, want %s", f.ID, f.Status, want[f.ID])
		}
	}
	if r.Digest == "" {
		t.Fatal("missing digest")
	}
	if r.Digest != Analyze(Snapshot{"pto": "8"}, Snapshot{"pto": "4"}, graph).Digest {
		t.Fatal("digest is not deterministic")
	}
}

func TestAnalyzeFailsClosedOnUnknownLineage(t *testing.T) {
	r := Analyze(Snapshot{"x": "old"}, Snapshot{"x": "new"}, []Node{{ID: "calc", Kind: Calculation, Dependencies: []string{"missing"}}})
	if len(r.Nodes) != 2 {
		t.Fatalf("nodes = %#v", r.Nodes)
	}
	for _, f := range r.Nodes {
		if f.Status != Unknown {
			t.Errorf("%s = %s, want UNKNOWN", f.ID, f.Status)
		}
	}
}

func TestAnalyzeDetectsCycle(t *testing.T) {
	r := Analyze(nil, Snapshot{"a": "1"}, []Node{{ID: "a", Kind: Fact, Dependencies: []string{"b"}}, {ID: "b", Kind: Calculation, Dependencies: []string{"a"}}})
	for _, f := range r.Nodes {
		if f.Status != Unknown {
			t.Errorf("%s = %s, want UNKNOWN", f.ID, f.Status)
		}
	}
}

func TestAnalyzeDetectsDeletedInput(t *testing.T) {
	r := Analyze(Snapshot{"a": "value"}, Snapshot{}, []Node{{ID: "a", Kind: Fact}})
	if len(r.Changed) != 1 || r.Nodes[0].Status != Recompute {
		t.Fatalf("result = %#v", r)
	}
}
