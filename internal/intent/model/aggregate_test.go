package model_test

import (
	"errors"
	"sync"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/intent/model"
)

func mutateAggregate(aggs []model.AggregateDefinition, root model.EntityRef, f func(*model.AggregateDefinition)) []model.AggregateDefinition {
	out := make([]model.AggregateDefinition, len(aggs))
	copy(out, aggs)
	for i := range out {
		if out[i].Root == root {
			f(&out[i])
		}
	}
	return out
}

func compileWithAggregates(t *testing.T, aggs []model.AggregateDefinition) (*model.Registry, error) {
	t.Helper()
	reg, err := model.Catalog()
	if err != nil {
		t.Fatalf("baseline catalog: %v", err)
	}
	return model.NewRegistry(reg.Entities(), reg.Properties(), aggs, reg.Relationships(),
		reg.Authorities(), reg.RetentionClasses())
}

// TestTodo_MODEL_012 is the PRIMARY test for aggregate ownership registration.
//
// RED: tests reject unassigned roots, child mutation without owner authority,
// and cross-root atomic assumptions outside a declared consistency boundary.
//
// GREEN: every root resolves stream, lifecycle, invariants and commit
// boundary; rebuildable summaries return NO_BUSINESS_LIFECYCLE.
func TestTodo_MODEL_012(t *testing.T) {
	reg := mustRegistry(t)
	employment := model.EntityRef{Name: "Employment", Version: 1}

	t.Run("RED", func(t *testing.T) {
		t.Run("unassigned root", func(t *testing.T) {
			var kept []model.AggregateDefinition
			for _, a := range reg.Aggregates() {
				if a.Root != employment {
					kept = append(kept, a)
				}
			}
			if _, err := compileWithAggregates(t, kept); !errors.Is(err, model.ErrUnassignedRoot) {
				t.Fatalf("compiled a registry with an unassigned root: %v", err)
			}
		})

		t.Run("child mutation without owner authority", func(t *testing.T) {
			// CompensationComponent is a CHILD; claiming it a second time under
			// a different root is exactly a child asserting mutation authority
			// its actual owner never granted.
			aggs := append([]model.AggregateDefinition(nil), reg.Aggregates()...)
			aggs = mutateAggregate(aggs, model.EntityRef{Name: "Job", Version: 1}, func(a *model.AggregateDefinition) {
				a.ChildRefs = append(a.ChildRefs, model.EntityRef{Name: "CompensationComponent", Version: 1})
			})
			if _, err := compileWithAggregates(t, aggs); !errors.Is(err, model.ErrInvalidAggregate) {
				t.Fatalf("compiled a registry with a doubly-claimed child: %v", err)
			}
		})

		t.Run("child claimed with the wrong class", func(t *testing.T) {
			aggs := mutateAggregate(append([]model.AggregateDefinition(nil), reg.Aggregates()...),
				model.EntityRef{Name: "Job", Version: 1}, func(a *model.AggregateDefinition) {
					a.ChildRefs = append(a.ChildRefs, model.EntityRef{Name: "Position", Version: 1})
				})
			if _, err := compileWithAggregates(t, aggs); !errors.Is(err, model.ErrInvalidAggregate) {
				t.Fatalf("compiled a registry claiming a non-CHILD entity as a child: %v", err)
			}
		})

		t.Run("cross-root atomic assumption outside a declared boundary", func(t *testing.T) {
			person, err := reg.Aggregate(model.EntityRef{Name: "Person", Version: 1})
			if err != nil {
				t.Fatalf("resolve Person aggregate: %v", err)
			}
			worker, err := reg.Aggregate(model.EntityRef{Name: "Worker", Version: 1})
			if err != nil {
				t.Fatalf("resolve Worker aggregate: %v", err)
			}
			// Both declare LOCAL_ACID: a write spanning both is exactly the
			// unsafe cross-root assumption MODEL-012 must reject.
			if err := model.ValidateCrossRootWrite([]model.AggregateDefinition{person, worker}); !errors.Is(err, model.ErrCrossRootAtomicity) {
				t.Fatalf("accepted a cross-root write with no declared boundary: %v", err)
			}
		})

		t.Run("CROSS_AGGREGATE_TRANSACTION with no invariants", func(t *testing.T) {
			a := model.AggregateDefinition{
				Root:                model.EntityRef{Name: "Person", Version: 1},
				LifecycleAssignment: "RevisionedFactLifecycle",
				CommandBoundary:     model.BoundaryCrossAggregateTransaction,
			}
			if err := a.Validate(); !errors.Is(err, model.ErrCrossRootAtomicity) {
				t.Fatalf("validated a cross-aggregate boundary with no invariants: %v", err)
			}
		})

		t.Run("NO_BUSINESS_LIFECYCLE declaring a command boundary", func(t *testing.T) {
			a := model.AggregateDefinition{
				Root:                model.EntityRef{Name: "WorkerSummary", Version: 1},
				LifecycleAssignment: model.NoBusinessLifecycle,
				CommandBoundary:     model.BoundaryLocalACID,
			}
			if err := a.Validate(); !errors.Is(err, model.ErrInvalidAggregate) {
				t.Fatalf("validated NO_BUSINESS_LIFECYCLE with a command boundary: %v", err)
			}
		})
	})

	t.Run("GREEN", func(t *testing.T) {
		for _, a := range reg.Aggregates() {
			if a.LifecycleAssignment == "" {
				t.Fatalf("%s resolved no lifecycle", a.Root)
			}
			if !a.Rebuildable() && !a.CommandBoundary.Valid() {
				t.Fatalf("%s resolved no commit boundary", a.Root)
			}
		}
		summary, err := reg.Aggregate(model.EntityRef{Name: "WorkerSummary", Version: 1})
		if err != nil {
			t.Fatalf("resolve WorkerSummary: %v", err)
		}
		if summary.LifecycleAssignment != model.NoBusinessLifecycle {
			t.Fatalf("WorkerSummary lifecycle = %q, want NO_BUSINESS_LIFECYCLE", summary.LifecycleAssignment)
		}
		if err := summary.AuthorizeCommand(); !errors.Is(err, model.ErrNoBusinessLifecycle) {
			t.Fatalf("a NO_BUSINESS_LIFECYCLE root authorized a command: %v", err)
		}
		person, err := reg.Aggregate(model.EntityRef{Name: "Person", Version: 1})
		if err != nil {
			t.Fatalf("resolve Person: %v", err)
		}
		if err := person.AuthorizeCommand(); err != nil {
			t.Fatalf("a commandable root refused a command: %v", err)
		}
	})
}

// TestTodo_MODEL_012_Property asserts that every CHILD-class entity resolves
// to exactly one owner root, and that every root's own lifecycle assignment
// matches its EntityDefinition (they are never allowed to drift).
func TestTodo_MODEL_012_Property(t *testing.T) {
	reg := mustRegistry(t)
	for _, e := range reg.Entities() {
		if e.Class != model.ClassChild {
			continue
		}
		root, err := reg.OwnerRoot(e.Ref)
		if err != nil {
			t.Fatalf("child %s resolves to no owner root: %v", e.Ref, err)
		}
		if root == e.Ref {
			t.Fatalf("child %s resolved itself as its own root", e.Ref)
		}
	}
	for _, a := range reg.Aggregates() {
		root, err := reg.Entity(a.Root)
		if err != nil {
			t.Fatalf("resolve root entity %s: %v", a.Root, err)
		}
		if root.LifecycleAssignment != a.LifecycleAssignment {
			t.Fatalf("%s: entity lifecycle %q disagrees with aggregate lifecycle %q",
				a.Root, root.LifecycleAssignment, a.LifecycleAssignment)
		}
	}
}

// TestTodo_MODEL_012_Golden pins the compiled aggregate registry.
func TestTodo_MODEL_012_Golden(t *testing.T) {
	reg := mustRegistry(t)
	type row struct {
		Root, Lifecycle, Boundary, StreamKind string
		Children, Invariants                  []string
	}
	var rows []row
	for _, a := range reg.Aggregates() {
		r := row{Root: a.Root.String(), Lifecycle: a.LifecycleAssignment,
			Boundary: string(a.CommandBoundary), StreamKind: a.StreamKind, Invariants: a.InvariantRefs}
		for _, c := range a.ChildRefs {
			r.Children = append(r.Children, c.String())
		}
		rows = append(rows, r)
	}
	goldenJSON(t, "model_012_aggregates.json", rows)
}

// TestTodo_MODEL_012_Recovery proves a NO_BUSINESS_LIFECYCLE summary can
// always be read back after a failed command attempt: rejecting the command
// never corrupts or removes the read path.
func TestTodo_MODEL_012_Recovery(t *testing.T) {
	reg := mustRegistry(t)
	ref := model.EntityRef{Name: "WorkerSummary", Version: 1}
	summary, err := reg.Aggregate(ref)
	if err != nil {
		t.Fatalf("resolve WorkerSummary: %v", err)
	}
	for i := 0; i < 3; i++ {
		if err := summary.AuthorizeCommand(); !errors.Is(err, model.ErrNoBusinessLifecycle) {
			t.Fatalf("attempt %d: expected ErrNoBusinessLifecycle, got %v", i, err)
		}
		// The read path recovers unaffected on every attempt.
		again, err := reg.Aggregate(ref)
		if err != nil {
			t.Fatalf("attempt %d: WorkerSummary no longer resolves: %v", i, err)
		}
		if again.LifecycleAssignment != model.NoBusinessLifecycle {
			t.Fatalf("attempt %d: lifecycle drifted to %q", i, again.LifecycleAssignment)
		}
	}
}

// TestTodo_MODEL_012_Race resolves aggregates concurrently against the
// immutable registry.
func TestTodo_MODEL_012_Race(t *testing.T) {
	reg := mustRegistry(t)
	roots := make([]model.EntityRef, 0, len(reg.Aggregates()))
	for _, a := range reg.Aggregates() {
		roots = append(roots, a.Root)
	}
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			root := roots[i%len(roots)]
			a, err := reg.Aggregate(root)
			if err != nil {
				t.Errorf("concurrent resolve %s: %v", root, err)
				return
			}
			a.ChildRefs = append(a.ChildRefs, model.EntityRef{Name: "Clobbered", Version: 1})
			_, _ = reg.OwnerRoot(root)
		}(i)
	}
	wg.Wait()
	for _, root := range roots {
		a, err := reg.Aggregate(root)
		if err != nil {
			t.Fatalf("registry no longer resolves %s after concurrent access: %v", root, err)
		}
		for _, c := range a.ChildRefs {
			if c.Name == "Clobbered" {
				t.Fatalf("%s child list was mutated by a concurrent reader", root)
			}
		}
	}
}

// FuzzTodo_MODEL_012 fuzzes [model.ValidateCrossRootWrite] over random
// combinations of consistency boundaries: a single participant is always
// legal, and two or more are legal only when every participant declares
// CROSS_AGGREGATE_TRANSACTION or EXTERNAL_OBSERVATION.
func FuzzTodo_MODEL_012(f *testing.F) {
	f.Add(uint8(0), uint8(0))
	f.Add(uint8(1), uint8(2))
	f.Add(uint8(2), uint8(2))
	f.Add(uint8(3), uint8(1))
	boundaries := []model.ConsistencyBoundary{
		model.BoundaryLocalACID, model.BoundaryCrossAggregateTransaction, model.BoundaryExternalObservation,
	}
	f.Fuzz(func(t *testing.T, bi, bj uint8) {
		b1 := boundaries[int(bi)%len(boundaries)]
		b2 := boundaries[int(bj)%len(boundaries)]
		p1 := model.AggregateDefinition{
			Root: model.EntityRef{Name: "Person", Version: 1}, LifecycleAssignment: "L",
			CommandBoundary: b1, InvariantRefs: []string{"invariant.x/v1"},
		}
		p2 := model.AggregateDefinition{
			Root: model.EntityRef{Name: "Worker", Version: 1}, LifecycleAssignment: "L",
			CommandBoundary: b2, InvariantRefs: []string{"invariant.y/v1"},
		}
		err := model.ValidateCrossRootWrite([]model.AggregateDefinition{p1, p2})
		wantOK := (b1 == model.BoundaryCrossAggregateTransaction || b1 == model.BoundaryExternalObservation) &&
			(b2 == model.BoundaryCrossAggregateTransaction || b2 == model.BoundaryExternalObservation)
		if wantOK && err != nil {
			t.Fatalf("rejected a legal cross-root write (%s, %s): %v", b1, b2, err)
		}
		if !wantOK && err == nil {
			t.Fatalf("accepted an illegal cross-root write (%s, %s)", b1, b2)
		}
		if err := model.ValidateCrossRootWrite([]model.AggregateDefinition{p1}); err != nil {
			t.Fatalf("a single participant must never require a cross-root boundary: %v", err)
		}
	})
}
