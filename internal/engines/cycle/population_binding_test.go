package cycle

import (
	"errors"
	"testing"
	"time"
)

func validSnapshotRef() PopulationSnapshotRef {
	return PopulationSnapshotRef{DefinitionID: "pop-1", RevisionVersion: "v1", Digest: "sha256:aaaa"}
}

// TestTodo_CYCLE_005 is the primary acceptance case: a cycle binds a
// population snapshot by reference and digest (never by copying members),
// the binding is immutable, and rebinding to a different snapshot is
// refused unless the cycle is in a declared phase that allows it.
func TestTodo_CYCLE_005(t *testing.T) {
	ref := validSnapshotRef()
	boundAt := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	binding, err := BindPopulation("sha256:cyclerev-1", ref, boundAt)
	if err != nil {
		t.Fatal(err)
	}
	if binding.Digest == "" {
		t.Fatal("binding was not digested")
	}
	if binding.Snapshot != ref {
		t.Fatal("binding did not cite the exact snapshot reference")
	}

	other := PopulationSnapshotRef{DefinitionID: "pop-1", RevisionVersion: "v2", Digest: "sha256:bbbb"}

	// A phase that does not declare the rebind operation refuses.
	closed := CompiledPhase{ID: "closed", Name: "CLOSED"}
	if _, err := binding.Rebind(closed, other, boundAt.Add(time.Hour)); !errors.Is(err, ErrRebindNotAllowed) {
		t.Fatalf("rebind outside a declared phase: got %v, want ErrRebindNotAllowed", err)
	}
	// The refusal must not have mutated the original binding.
	if binding.Snapshot != ref {
		t.Fatal("failed Rebind mutated the receiver")
	}

	// A phase that declares the rebind operation allows it.
	open := CompiledPhase{ID: "open", Name: "OPEN", AllowedOperations: []string{OperationRebindPopulation}}
	rebound, err := binding.Rebind(open, other, boundAt.Add(time.Hour))
	if err != nil {
		t.Fatalf("rebind inside a declared phase: %v", err)
	}
	if rebound.Snapshot != other {
		t.Fatal("rebind did not adopt the new snapshot reference")
	}
	if rebound.Digest == binding.Digest {
		t.Fatal("rebinding to a different snapshot must change the binding digest")
	}
	if binding.Snapshot != ref {
		t.Fatal("successful Rebind mutated the receiver")
	}
}

// TestTodo_CYCLE_005_Property verifies digest determinism: byte-identical
// inputs produce byte-identical digests, and any change to which cycle
// revision or which snapshot is bound changes the digest.
func TestTodo_CYCLE_005_Property(t *testing.T) {
	ref := validSnapshotRef()
	boundAt := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	a, err := BindPopulation("sha256:cyclerev-1", ref, boundAt)
	if err != nil {
		t.Fatal(err)
	}
	b, err := BindPopulation("sha256:cyclerev-1", ref, boundAt)
	if err != nil {
		t.Fatal(err)
	}
	if a.Digest != b.Digest {
		t.Fatalf("identical inputs produced different digests: %q vs %q", a.Digest, b.Digest)
	}

	diffRevision, err := BindPopulation("sha256:cyclerev-2", ref, boundAt)
	if err != nil {
		t.Fatal(err)
	}
	if diffRevision.Digest == a.Digest {
		t.Fatal("a different cycle revision digest did not change the binding digest")
	}

	diffSnapshot := ref
	diffSnapshot.Digest = "sha256:different"
	c, err := BindPopulation("sha256:cyclerev-1", diffSnapshot, boundAt)
	if err != nil {
		t.Fatal(err)
	}
	if c.Digest == a.Digest {
		t.Fatal("a different snapshot digest did not change the binding digest")
	}

	diffTime, err := BindPopulation("sha256:cyclerev-1", ref, boundAt.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if diffTime.Digest == a.Digest {
		t.Fatal("a different bound-at instant did not change the binding digest")
	}
}

// TestTodo_CYCLE_005_Golden pins the exact digest BindPopulation computes
// for one fixed set of inputs, so an accidental change to the canonical
// encoding is caught even when every field-level property test above still
// passes.
func TestTodo_CYCLE_005_Golden(t *testing.T) {
	ref := PopulationSnapshotRef{DefinitionID: "pop-golden", RevisionVersion: "v1", Digest: "sha256:golden-snapshot"}
	boundAt := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	binding, err := BindPopulation("sha256:golden-cyclerev", ref, boundAt)
	if err != nil {
		t.Fatal(err)
	}
	const want = "sha256:e2861aeab761995f8ddc370dfb969ccdf01ee7562404d53b9f1376469f73b382"
	if binding.Digest != want {
		t.Fatalf("BindPopulation digest = %q, want %q", binding.Digest, want)
	}
}

// TestTodo_CYCLE_005_Fault exercises the required-field and rebind-refusal
// failure paths: an incomplete snapshot reference, a missing bound-at
// instant, a missing cycle revision digest, and a rebind attempt whose
// phase declares no operations at all never silently succeed.
func TestTodo_CYCLE_005_Fault(t *testing.T) {
	boundAt := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	if _, err := BindPopulation("", validSnapshotRef(), boundAt); !errors.Is(err, ErrRevision) {
		t.Fatalf("missing cycle revision digest: got %v", err)
	}
	if _, err := BindPopulation("sha256:cyclerev-1", PopulationSnapshotRef{}, boundAt); !errors.Is(err, ErrBindingSnapshot) {
		t.Fatalf("empty snapshot reference: got %v", err)
	}
	incomplete := PopulationSnapshotRef{DefinitionID: "pop-1", RevisionVersion: "v1"}
	if _, err := BindPopulation("sha256:cyclerev-1", incomplete, boundAt); !errors.Is(err, ErrBindingSnapshot) {
		t.Fatalf("snapshot reference missing digest: got %v", err)
	}
	if _, err := BindPopulation("sha256:cyclerev-1", validSnapshotRef(), time.Time{}); !errors.Is(err, ErrBindingBoundAt) {
		t.Fatalf("missing bound-at instant: got %v", err)
	}

	binding, err := BindPopulation("sha256:cyclerev-1", validSnapshotRef(), boundAt)
	if err != nil {
		t.Fatal(err)
	}
	noOps := CompiledPhase{ID: "no-ops", Name: "NO_OPS"}
	if _, err := binding.Rebind(noOps, PopulationSnapshotRef{DefinitionID: "pop-1", RevisionVersion: "v2", Digest: "sha256:cccc"}, boundAt.Add(time.Hour)); !errors.Is(err, ErrRebindNotAllowed) {
		t.Fatalf("rebind with no declared operations: got %v", err)
	}
}
