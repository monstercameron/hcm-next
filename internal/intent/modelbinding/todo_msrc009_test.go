package modelbinding

import (
	"sync"
	"testing"

	"github.com/monstercameron/hcm-next/internal/intent"
	"github.com/monstercameron/hcm-next/internal/intent/definitions"
)

// TestTodo_MSRC_009 is the MSRC-009 primary test. It exercises the real
// fourteen drafted definitions against the real generated model registry
// ([BindCatalog]) and proves both RED clauses this package owns fail closed
// on synthetic input: a binding naming an entity or property the generated
// registry does not publish, and a write to a property the model marks
// immutable.
func TestTodo_MSRC_009(t *testing.T) {
	table, err := BindCatalog()
	if err != nil {
		t.Fatalf("BindCatalog: %v", err)
	}

	// GREEN: the real fourteen definitions bind cleanly against the real
	// generated registry — zero dangling model references.
	if len(table.Gaps) != 0 {
		t.Fatalf("real catalog produced gaps: %v", table.Gaps)
	}
	wantTotal := len(definitions.Bindings())
	if wantTotal != 14 {
		t.Fatalf("definitions.Bindings() = %d, want the fourteen drafted definitions", wantTotal)
	}
	if len(table.Bindings) != wantTotal {
		t.Fatalf("Bindings len = %d, want %d", len(table.Bindings), wantTotal)
	}
	if !table.FullyBound(wantTotal) {
		t.Fatalf("table not fully bound: %+v", table)
	}

	// Cross-check against the existing element-level coverage checker: the
	// same fourteen definitions must also be fully bound there. The two
	// checkers are independent (this package resolves model references;
	// intent.CheckCoverage checks binding elements are all present) and a
	// real gap in either is a real defect in the drafted catalog.
	intentReg, err := definitions.NewRegistry()
	if err != nil {
		t.Fatalf("definitions.NewRegistry: %v", err)
	}
	elementCoverage := intent.CheckCoverage(intentReg, definitions.Bindings())
	if !elementCoverage.FullyBound() {
		t.Fatalf("intent.CheckCoverage: %s", elementCoverage.Summary())
	}

	// RED: unknown entity/property.
	reg := realRegistry(t)
	unknown := Bind([]intent.Binding{{
		Definition:     intentRefFor("hcmnext.test.msrc009_red_unknown", 1),
		AggregateRoots: []string{"DoesNotExist"},
		ReadProperties: []string{"does_not.exist"},
	}}, reg)
	if len(unknown.Gaps) == 0 {
		t.Fatal("binding an unknown entity/property produced no gap")
	}

	// RED: immutable write.
	immutable := Bind([]intent.Binding{{
		Definition:      intentRefFor("hcmnext.test.msrc009_red_immutable", 1),
		AggregateRoots:  []string{"ProposalRevision"},
		ReadProperties:  []string{"proposal_revision.material_digest"},
		WriteProperties: []string{"proposal_revision.material_digest"},
	}}, reg)
	found := false
	for _, g := range immutable.Gaps {
		if g.Element == "immutable_write" {
			found = true
		}
	}
	if !found {
		t.Fatalf("writing an immutable property produced no immutable_write gap: %v", immutable.Gaps)
	}
}

// TestTodo_MSRC_009_Golden pins the real BindCatalog binding table's digest:
// a change to any of the fourteen drafted bindings, or to the generated
// model registry they resolve against, changes this digest.
func TestTodo_MSRC_009_Golden(t *testing.T) {
	table, err := BindCatalog()
	if err != nil {
		t.Fatalf("BindCatalog: %v", err)
	}
	if len(table.Gaps) != 0 {
		t.Fatalf("gaps present: %v", table.Gaps)
	}

	const goldenDigest = "sha256:1bb0cfc1c946979f5fbcaee1bed2e8f1de31c935d43dc707eccd5ba1a1e81eac"
	if got := table.Digest(); got != goldenDigest {
		t.Fatalf("binding table digest = %s, want pinned golden %s (update only after confirming the change to the fourteen definitions or the generated registry is intentional)",
			got, goldenDigest)
	}
}

// TestTodo_MSRC_009_Race runs BindCatalog concurrently: Bind holds no shared
// mutable state (each call gets its own Table), so every concurrent run
// against the same immutable inputs must agree.
func TestTodo_MSRC_009_Race(t *testing.T) {
	want, err := BindCatalog()
	if err != nil {
		t.Fatalf("baseline BindCatalog: %v", err)
	}
	wantDigest := want.Digest()

	const n = 16
	digests := make([]string, n)
	errs := make([]error, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			table, err := BindCatalog()
			if err != nil {
				errs[i] = err
				return
			}
			digests[i] = table.Digest()
		}(i)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("concurrent run %d: %v", i, err)
		}
		if digests[i] != wantDigest {
			t.Fatalf("concurrent run %d digest = %s, want %s", i, digests[i], wantDigest)
		}
	}
}
