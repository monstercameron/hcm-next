package userflowgaps

import (
	"reflect"
	"sort"
	"testing"
)

func TestUserFlowGapCompilerEmitsExactOwnerKeyedTodosAndConvergesOnSecondPass(t *testing.T) {
	o := Oracle{Browser: "TestBrowserGap", API: "TestAPIGap", Security: "TestSecurityGap", Fault: "TestFaultGap", Conformance: "TestConformanceGap", Mutation: "TestMutationGap", VisibleState: "validation blocked", EnabledActions: "correct", SemanticResult: "no mutation", PersistedEffects: "none", ProhibitedDisclosure: "private evidence"}
	fs := []Finding{
		{FlowID: "UF-001", Stage: "COLLECT", Configuration: "desktop", Owner: "shared/forms", Contract: "PageDefinition/address", Kind: "surface", Description: "address form", Phase: "phase-1", Intelligence: "normal", Dependencies: []string{"D2"}, References: []string{"README#forms"}, Oracle: o},
		{FlowID: "UF-002", Stage: "COLLECT", Configuration: "mobile", Owner: "shared/forms", Contract: "PageDefinition/address", Kind: "surface", Oracle: o},
		{FlowID: "UF-003", Stage: "REPAIR", Configuration: "desktop", Owner: "payroll", Contract: "correction/invariant", Kind: "domain", Oracle: o},
	}
	first := Compile(fs)
	if len(first.Todos) != 2 || len(first.NewIdentities) != 2 {
		t.Fatalf("todos=%+v new=%v", first.Todos, first.NewIdentities)
	}
	var shared Todo
	for _, candidate := range first.Todos {
		if candidate.Key == Identity("shared/forms", "PageDefinition/address") {
			shared = candidate
		}
	}
	if len(shared.Coverage) != 2 {
		t.Fatalf("shared coverage lost: %+v", shared)
	}
	second := CompileFindings(Input{Findings: fs, Existing: first.Todos})
	if len(second.NewIdentities) != 0 || second.Digest != first.Digest {
		t.Fatalf("not fixed point: new=%v digest %s/%s", second.NewIdentities, first.Digest, second.Digest)
	}
	if len(second.Reverse[Identity("shared/forms", "PageDefinition/address")]) != 2 {
		t.Fatalf("reverse coverage=%v", second.Reverse)
	}
}

func TestIdentityCanonicalizesOwnerAndContract(t *testing.T) {
	if got := Identity(" owner ", " contract "); got != "owner::contract" {
		t.Fatal(got)
	}
}

func TestUserFlowGapCompilerFindingKindMatrixExactOracleAndSharedReverseEdges(t *testing.T) {
	oracle := Oracle{
		Browser: "browser exact", API: "api exact", Security: "security exact",
		Fault: "fault exact", Conformance: "conformance exact", Mutation: "mutation exact",
		VisibleState: "visible exact", EnabledActions: "actions exact", SemanticResult: "result exact",
		PersistedEffects: "effects exact", ProhibitedDisclosure: "disclosure exact",
	}
	// These are the concrete finding kinds named by UXFLOW-010's red
	// contract. Keep the matrix explicit so adding/removing a kind is visible
	// in verification review.
	kinds := []string{"PageDefinition", "widget", "form", "action", "endpoint", "message", "state", "recovery", "accessibility", "analytics", "test"}
	findings := make([]Finding, 0, len(kinds)+2)
	for i, kind := range kinds {
		findings = append(findings, Finding{
			FlowID: "UF-" + string(rune('A'+i)), Stage: "STAGE", Configuration: "desktop",
			Owner: " shared/owner ", Contract: " contract/" + kind + " ", Kind: kind,
			Description: "description " + kind, Phase: "phase-1", Intelligence: "normal",
			Dependencies: []string{"dep-" + kind, "dep-" + kind}, References: []string{"ref-" + kind}, Oracle: oracle,
		})
	}
	// These two findings intentionally share an owner+contract and differ only
	// in consuming flow/stage/configuration; they must produce one todo and two
	// reverse edges.
	findings = append(findings,
		Finding{FlowID: "UF-SHARED", Stage: "COLLECT", Configuration: "mobile", Owner: " shared/owner ", Contract: " contract/shared ", Kind: "surface", Oracle: oracle},
		Finding{FlowID: "UF-SHARED", Stage: "COLLECT", Configuration: "mobile", Owner: " shared/owner ", Contract: " contract/shared ", Kind: "surface", Oracle: oracle},
	)

	first := Compile(findings)
	if len(first.Todos) != len(kinds)+1 {
		t.Fatalf("todo count=%d, want %d: %+v", len(first.Todos), len(kinds)+1, first.Todos)
	}
	if len(first.NewIdentities) != len(kinds)+1 {
		t.Fatalf("new identity count=%d: %v", len(first.NewIdentities), first.NewIdentities)
	}
	for _, kind := range kinds {
		todo := todoByKey(t, first.Todos, Identity("shared/owner", "contract/"+kind))
		if todo.Key != Identity("shared/owner", "contract/"+kind) || todo.ID != todo.Key || todo.Owner != "shared/owner" || todo.Contract != "contract/"+kind || todo.Kind != kind {
			t.Fatalf("unstable owner/contract/kind for %q: %+v", kind, todo)
		}
		if todo.Oracle != oracle {
			t.Fatalf("oracle for %q was not preserved exactly: got %+v want %+v", kind, todo.Oracle, oracle)
		}
		if len(todo.Dependencies) != 1 || len(todo.References) != 1 {
			t.Fatalf("metadata was not deduplicated for %q: deps=%v refs=%v", kind, todo.Dependencies, todo.References)
		}
		if len(first.Reverse[todo.Key]) != len(todo.Coverage) || first.Reverse[todo.Key][0] != todo.Coverage[0] {
			t.Fatalf("reverse edge mismatch for %q: reverse=%v coverage=%v", kind, first.Reverse[todo.Key], todo.Coverage)
		}
	}
	sharedKey := Identity("shared/owner", "contract/shared")
	shared := todoByKey(t, first.Todos, sharedKey)
	if len(shared.Coverage) != 1 || len(first.Reverse[sharedKey]) != 1 {
		t.Fatalf("duplicate shared edge was not deduplicated: todo=%v reverse=%v", shared.Coverage, first.Reverse[sharedKey])
	}

	// Reordering findings must not alter the canonical result digest.
	reversed := append([]Finding(nil), findings...)
	for i, j := 0, len(reversed)-1; i < j; i, j = i+1, j-1 {
		reversed[i], reversed[j] = reversed[j], reversed[i]
	}
	if reordered := Compile(reversed); reordered.Digest != first.Digest {
		t.Fatalf("digest changed with finding order: %s vs %s", first.Digest, reordered.Digest)
	}

	second := CompileFindings(Input{Findings: findings, Existing: first.Todos})
	if len(second.NewIdentities) != 0 {
		t.Fatalf("second pass emitted identities: %v", second.NewIdentities)
	}
	if second.Digest != first.Digest {
		t.Fatalf("second pass digest changed: %s vs %s", first.Digest, second.Digest)
	}
}

func todoByKey(t *testing.T, todos []Todo, key string) Todo {
	t.Helper()
	for _, todo := range todos {
		if todo.Key == key {
			return todo
		}
	}
	t.Fatalf("todo %q not found in %+v", key, todos)
	return Todo{}
}

// TestTodo_UXFLOW_010_Golden pins the externally useful shape of a generated
// candidate: canonical key, exact oracle, and sorted reverse edge.
func TestTodo_UXFLOW_010_Golden(t *testing.T) {
	f := Finding{FlowID: "UF-010", Stage: "REVIEW", Configuration: "rtl", Owner: "case", Contract: "evidence/view", Kind: "state", Oracle: Oracle{Browser: "browser", API: "api", Security: "security", Fault: "fault", Conformance: "conformance", Mutation: "mutation", VisibleState: "masked", EnabledActions: "appeal", SemanticResult: "unchanged", PersistedEffects: "receipt", ProhibitedDisclosure: "identity"}}
	r := Compile([]Finding{f})
	if len(r.Todos) != 1 || r.Todos[0].Key != "case::evidence/view" || r.Digest == "" {
		t.Fatalf("unexpected golden result: %+v", r)
	}
	if !reflect.DeepEqual(r.Todos[0].Oracle, f.Oracle) || !reflect.DeepEqual(r.Reverse[f.Owner+"::"+f.Contract], []CoverageEdge{{"UF-010", "REVIEW", "rtl"}}) {
		t.Fatalf("oracle/reverse mismatch: %+v %+v", r.Todos[0].Oracle, r.Reverse)
	}
}

// TestTodo_UXFLOW_010_Property proves all input permutations converge to one
// canonical result and that duplicate edges remain idempotent.
func TestTodo_UXFLOW_010_Property(t *testing.T) {
	base := []Finding{{FlowID: "UF-1", Stage: "A", Configuration: "desktop", Owner: "o", Contract: "c"}, {FlowID: "UF-2", Stage: "B", Configuration: "mobile", Owner: "o", Contract: "c"}, {FlowID: "UF-1", Stage: "A", Configuration: "desktop", Owner: "o", Contract: "c"}}
	want := Compile(base)
	for i := 0; i < len(base); i++ {
		p := append([]Finding(nil), base...)
		sort.Slice(p, func(a, b int) bool { return p[a].FlowID < p[b].FlowID })
		if i%2 == 1 {
			for a, b := 0, len(p)-1; a < b; a, b = a+1, b-1 {
				p[a], p[b] = p[b], p[a]
			}
		}
		got := Compile(p)
		if got.Digest != want.Digest || len(got.Todos[0].Coverage) != 2 {
			t.Fatalf("permutation %d diverged: %s/%s", i, want.Digest, got.Digest)
		}
	}
}

// TestTodo_UXFLOW_010_Security ensures semantic ownership is part of identity;
// a domain invariant cannot be absorbed into a shared screen todo.
func TestTodo_UXFLOW_010_Security(t *testing.T) {
	r := Compile([]Finding{{Owner: "shared", Contract: "form/address", FlowID: "UF-1"}, {Owner: "payroll", Contract: "form/address", FlowID: "UF-1"}})
	if len(r.Todos) != 2 || r.Todos[0].Key == r.Todos[1].Key {
		t.Fatalf("cross-owner findings collapsed: %+v", r.Todos)
	}
	if _, ok := r.Reverse["payroll::form/address"]; !ok {
		t.Fatal("domain reverse edge missing")
	}
}

// TestTodo_UXFLOW_010_Conformance checks the fixed-point contract and reverse
// coverage consistency for every generated candidate.
func TestTodo_UXFLOW_010_Conformance(t *testing.T) {
	fs := []Finding{{Owner: "o", Contract: "a", FlowID: "UF-1", Stage: "S"}, {Owner: "o", Contract: "b", FlowID: "UF-2", Stage: "T"}}
	one := Compile(fs)
	two := CompileFindings(Input{Findings: fs, Existing: one.Todos})
	if len(two.NewIdentities) != 0 || one.Digest != two.Digest {
		t.Fatalf("not converged: %+v %+v", one, two)
	}
	for _, todo := range two.Todos {
		if !reflect.DeepEqual(todo.Coverage, two.Reverse[todo.Key]) {
			t.Errorf("reverse mismatch for %s", todo.Key)
		}
	}
}

// TestTodo_UXFLOW_010_Mutation catches the common identity mutation where a
// flow or description is accidentally included in the owner+contract key.
func TestTodo_UXFLOW_010_Mutation(t *testing.T) {
	a := Compile([]Finding{{Owner: "o", Contract: "c", FlowID: "UF-1", Description: "one"}})
	b := Compile([]Finding{{Owner: "o", Contract: "c", FlowID: "UF-2", Description: "two"}})
	if a.Todos[0].Key != b.Todos[0].Key || a.Todos[0].Key != "o::c" {
		t.Fatalf("identity mutated by flow data: %q/%q", a.Todos[0].Key, b.Todos[0].Key)
	}
}
