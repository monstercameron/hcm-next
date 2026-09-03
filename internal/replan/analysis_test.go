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
	want := map[string]Status{"pto": Recompute, "hours": Recompute, "write": Invalidate, "email": Invalidate, "unrelated": Unchanged}
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
		if f.Reason != "dependency cycle" {
			t.Errorf("%s reason = %q, want dependency cycle", f.ID, f.Reason)
		}
	}
}

func TestAnalyzeDetectsDeletedInput(t *testing.T) {
	r := Analyze(Snapshot{"a": "value"}, Snapshot{}, []Node{{ID: "a", Kind: Fact}})
	if len(r.Changed) != 1 || r.Nodes[0].Status != Recompute {
		t.Fatalf("result = %#v", r)
	}
}

// TestTodo_REPLAN_001 is the primary registry test: every material node kind
// participates in the exact affected closure with the appropriate action.
func TestTodo_REPLAN_001(t *testing.T) {
	graph := []Node{
		{ID: "pto", Kind: Fact},
		{ID: "hours", Kind: Calculation, Dependencies: []string{"pto"}},
		{ID: "write", Kind: Write, Dependencies: []string{"hours"}},
		{ID: "effect", Kind: Effect, Dependencies: []string{"write"}},
		{ID: "obligation", Kind: Obligation, Dependencies: []string{"hours"}},
		{ID: "decision", Kind: Decision, Dependencies: []string{"obligation"}},
		{ID: "other", Kind: Calculation},
	}
	r := AnalyzeMaterialSubgraph(Snapshot{"pto": "8", "rule": "v1"}, Snapshot{"pto": "4", "rule": "v1"}, graph)
	want := map[string]Status{"pto": Recompute, "hours": Recompute, "write": Invalidate, "effect": Invalidate, "obligation": Invalidate, "decision": Invalidate, "other": Unchanged}
	if got := r.Changed; len(got) != 1 || got[0] != "pto" {
		t.Fatalf("changed = %#v, want [pto]", got)
	}
	for _, f := range r.Nodes {
		if f.Status != want[f.ID] {
			t.Errorf("%s = %s (%s), want %s", f.ID, f.Status, f.Reason, want[f.ID])
		}
	}
}

// TestTodo_REPLAN_001_Property verifies closure is independent of declaration
// order and unrelated components remain unchanged.
func TestTodo_REPLAN_001_Property(t *testing.T) {
	a := []Node{{ID: "a", Kind: Fact}, {ID: "b", Kind: Calculation, Dependencies: []string{"a"}}, {ID: "c", Kind: Write, Dependencies: []string{"b"}}, {ID: "z", Kind: Effect}}
	b := []Node{a[3], a[2], a[1], a[0]}
	ra, rb := Analyze(nil, Snapshot{"a": "1"}, a), Analyze(nil, Snapshot{"a": "1"}, b)
	if ra.Digest != rb.Digest {
		t.Fatalf("declaration order changed digest: %s != %s", ra.Digest, rb.Digest)
	}
	if ra.Nodes[3].Status != Unchanged { // sorted IDs: z is last
		t.Fatalf("unrelated node was affected: %#v", ra.Nodes)
	}
}

// TestTodo_REPLAN_001_Golden pins the explainable result and digest shape for
// a representative proposal graph.
func TestTodo_REPLAN_001_Golden(t *testing.T) {
	r := Analyze(Snapshot{"pto": "8"}, Snapshot{"pto": "4"}, []Node{{ID: "pto", Kind: Fact}, {ID: "calc", Kind: Calculation, Dependencies: []string{"pto"}}})
	if len(r.Nodes) != 2 || r.Nodes[0] != (Finding{ID: "calc", Kind: Calculation, Status: Recompute, Reason: "depends on changed input"}) || r.Nodes[1] != (Finding{ID: "pto", Kind: Fact, Status: Recompute, Reason: "input changed"}) {
		t.Fatalf("golden result = %#v", r.Nodes)
	}
	if len(r.Digest) != 64 {
		t.Fatalf("digest = %q, want sha256 hex", r.Digest)
	}
}

// TestTodo_REPLAN_001_Mutation verifies malformed lineage cannot become safe
// by changing an unrelated snapshot value.
func TestTodo_REPLAN_001_Mutation(t *testing.T) {
	graph := []Node{{ID: "a", Kind: Fact}, {ID: "calc", Kind: Calculation, Dependencies: []string{"missing"}}}
	for _, current := range []Snapshot{{"a": "1"}, {"a": "2", "unrelated": "x"}} {
		r := Analyze(nil, current, graph)
		for _, f := range r.Nodes {
			if f.ID == "calc" && f.Status != Unknown {
				t.Fatalf("mutation made malformed node safe: %#v", r.Nodes)
			}
		}
	}
}
