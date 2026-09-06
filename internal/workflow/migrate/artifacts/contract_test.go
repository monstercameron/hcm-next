package artifacts

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestKinds_DeclaredOrderedAndComplete(t *testing.T) {
	t.Parallel()
	kinds := Kinds()
	if len(kinds) != 6 {
		t.Fatalf("Kinds() = %v, want six declared kinds", kinds)
	}
	if kinds[0] != KindLease {
		t.Fatalf("the lease handler must run first; Kinds()[0] = %q", kinds[0])
	}
	seen := map[Kind]bool{}
	for i, k := range kinds {
		if !k.Valid() {
			t.Fatalf("Kinds() returned undeclared kind %q", k)
		}
		if seen[k] {
			t.Fatalf("Kinds() repeats %q", k)
		}
		seen[k] = true
		if k.order() != i {
			t.Fatalf("%q.order() = %d, want %d", k, k.order(), i)
		}
	}
	if Kind("SOMETHING_ELSE").Valid() {
		t.Fatal("an undeclared kind reported itself valid")
	}
	if got := Kind("SOMETHING_ELSE").order(); got != len(kinds) {
		t.Fatalf("an undeclared kind sorts at %d, want last (%d)", got, len(kinds))
	}
}

func TestContracts_CoverEveryKindUnderOneVersion(t *testing.T) {
	t.Parallel()
	contracts := Contracts()
	if len(contracts) != len(Kinds()) {
		t.Fatalf("%d contracts for %d kinds", len(contracts), len(Kinds()))
	}
	for i, c := range contracts {
		if c.Version != ContractVersion {
			t.Fatalf("contract for %q names version %q, want %q", c.Kind, c.Version, ContractVersion)
		}
		if c.Kind != Kinds()[i] {
			t.Fatalf("contract %d is for %q, want %q -- contracts must follow handler order", i, c.Kind, Kinds()[i])
		}
		got, ok := ContractFor(c.Kind)
		if !ok || got != c {
			t.Fatalf("ContractFor(%q) = %+v, %v", c.Kind, got, ok)
		}
	}
	if _, ok := ContractFor("SOMETHING_ELSE"); ok {
		t.Fatal("ContractFor returned a contract for an undeclared kind")
	}
}

// The two kinds this package refuses to relocate must say so in the contract
// a caller can read, not only in the refusal it gets after committing to a
// migration.
func TestContracts_NameTheTwoNonRelocatableKinds(t *testing.T) {
	t.Parallel()
	want := map[Kind]bool{KindApproval: true, KindChildContinuation: true}
	for _, c := range Contracts() {
		if want[c.Kind] == c.Relocatable {
			t.Fatalf("%q reports Relocatable=%v", c.Kind, c.Relocatable)
		}
	}
}

func TestHandlers_OneOrderedHandlerPerKind(t *testing.T) {
	t.Parallel()
	handlers := Handlers()
	if len(handlers) != len(Kinds()) {
		t.Fatalf("%d handlers for %d kinds", len(handlers), len(Kinds()))
	}
	for i, h := range handlers {
		if h.Kind() != Kinds()[i] {
			t.Fatalf("handler %d is for %q, want %q", i, h.Kind(), Kinds()[i])
		}
	}
}

func TestScope_ValidateRefusesMalformedFrames(t *testing.T) {
	t.Parallel()
	base := Scope{
		TenantID: uuid.New(), InstanceID: uuid.New(),
		From:       Epoch{WorkflowVersion: 1, CompiledPlanDigest: "a", NodeID: "n1", Attempt: 1},
		To:         Epoch{WorkflowVersion: 2, CompiledPlanDigest: "b", NodeID: "n1", Attempt: 1},
		MigratedBy: "principal:operator", MigratedAt: fixedInstant,
	}
	if err := base.validate(); err != nil {
		t.Fatalf("a well-formed scope was refused: %v", err)
	}

	cases := map[string]func(s *Scope){
		"nil tenant":         func(s *Scope) { s.TenantID = uuid.Nil },
		"nil instance":       func(s *Scope) { s.InstanceID = uuid.Nil },
		"no principal":       func(s *Scope) { s.MigratedBy = "" },
		"no instant":         func(s *Scope) { s.MigratedAt = time.Time{} },
		"no source digest":   func(s *Scope) { s.From.CompiledPlanDigest = "" },
		"no source node":     func(s *Scope) { s.From.NodeID = "" },
		"source attempt 0":   func(s *Scope) { s.From.Attempt = 0 },
		"no target digest":   func(s *Scope) { s.To.CompiledPlanDigest = "" },
		"no target node":     func(s *Scope) { s.To.NodeID = "" },
		"target attempt 0":   func(s *Scope) { s.To.Attempt = 0 },
		"identical epochs":   func(s *Scope) { s.To = s.From },
		"same digest & node": func(s *Scope) { s.To.CompiledPlanDigest = s.From.CompiledPlanDigest },
	}
	for name, mutate := range cases {
		scope := base
		mutate(&scope)
		err := scope.validate()
		if err == nil {
			t.Fatalf("%s was accepted", name)
		}
		if got := CodeOf(err); got != CodeInvalidRequest {
			t.Fatalf("%s refused with %q, want %q", name, got, CodeInvalidRequest)
		}
	}
}

func TestScope_RelocatingTracksTheFrontierNode(t *testing.T) {
	t.Parallel()
	s := Scope{From: Epoch{NodeID: "a"}, To: Epoch{NodeID: "a"}}
	if s.Relocating() {
		t.Fatal("a migration that keeps the frontier node reported itself relocating")
	}
	s.To.NodeID = "b"
	if !s.Relocating() {
		t.Fatal("a migration that moves the frontier node reported itself not relocating")
	}
}
