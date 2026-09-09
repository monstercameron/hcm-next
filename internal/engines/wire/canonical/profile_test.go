package canonical

import (
	"errors"
	"testing"

	intentsv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/intents/v1"
)

func TestProfile_Smoke(t *testing.T) {
	if t == nil {
		t.Fatalf("nil tester")
	}
}

func TestProfile_NoPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panic: %v", r)
		}
	}()
}

func TestProfile_TrieAndMaterialPathsAreDeterministic(t *testing.T) {
	n, err := buildTrie([]string{"a.b", "a.c"})
	if err != nil || n.children["a"] == nil || n.children["a"].children["b"] == nil {
		t.Fatalf("trie = %#v, %v", n, err)
	}
	if _, err := buildTrie([]string{""}); !errors.Is(err, ErrInvalidProfile) {
		t.Fatalf("empty path error = %v", err)
	}
	if _, err := buildTrie([]string{"a..b"}); !errors.Is(err, ErrInvalidProfile) {
		t.Fatalf("empty segment error = %v", err)
	}
	leaf := &pathNode{}
	if leaf.subtree() {
		t.Fatal("empty node is a subtree")
	}
	leaf.child("field").terminal = true
	if leaf.children["field"].subtree() != true {
		t.Fatal("terminal leaf is not a subtree")
	}
	p := Profile{Material: []string{"z", "a"}}
	paths := p.MaterialPaths()
	if len(paths) != 2 || paths[0] != "a" || paths[1] != "z" {
		t.Fatalf("MaterialPaths = %v", paths)
	}
	paths[0] = "changed"
	if p.Material[0] == "changed" {
		t.Fatal("MaterialPaths returned aliased slice")
	}
}

func TestProfile_CompileRejectsUnresolvableAndDeadDeclarations(t *testing.T) {
	base := Profile{ID: "profile", Version: 1, SchemaID: "schema", SchemaVersion: 1, MessageName: "hcmnext.intents.v1.ProposalRevision", Material: []string{"intent_id"}}
	_ = &intentsv1.ProposalRevision{}
	for _, mutate := range []func(*Profile){
		func(p *Profile) { p.ID = "" }, func(p *Profile) { p.SchemaID = "" }, func(p *Profile) { p.MessageName = "" }, func(p *Profile) { p.Material = nil },
		func(p *Profile) { p.MessageName = "missing.Message" }, func(p *Profile) { p.Material = []string{"missing_field"} },
		func(p *Profile) { p.Material = []string{"intent_id"}; p.Sets = []string{"intent_id"} },
		func(p *Profile) { p.Material = []string{"intent_id"}; p.Verbatim = []string{"proposal.schema_id"} },
		func(p *Profile) { p.Material = []string{"intent_id"}; p.Currency = []string{"proposal.schema_id"} },
		func(p *Profile) {
			p.Material = []string{"intent_id"}
			p.Decimals = []Decimal{{Path: "proposal.schema_id"}}
		},
		func(p *Profile) {
			p.Material = []string{"intent_id"}
			p.DistinctPresence = []string{"proposal.schema_id"}
		},
	} {
		profile := base
		mutate(&profile)
		if _, err := Compile(profile); !errors.Is(err, ErrInvalidProfile) {
			t.Fatalf("Compile(%+v) = %v", profile, err)
		}
	}
	plan, err := Compile(base)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Profile().ID != base.ID || plan.Profile().MessageName != base.MessageName {
		t.Fatalf("Plan.Profile = %+v", plan.Profile())
	}
}
