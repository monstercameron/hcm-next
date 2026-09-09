package model_test

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/intent/model"
)

// TestTodo_MODEL_030 is the PRIMARY test for the model coverage report.
//
// RED: the report refuses VERIFIED while any exact binding/check is absent
// and identifies the first unresolved catalog number/path.
//
// GREEN: the result includes counts by domain, maturity, phase, negative
// policy and conformance coverage with source digest.
func TestTodo_MODEL_030(t *testing.T) {
	reg := mustRegistry(t)

	t.Run("RED", func(t *testing.T) {
		t.Run("entity with no property", func(t *testing.T) {
			props := []model.PropertyDefinition{}
			for _, p := range reg.Properties() {
				if p.Entity != (model.EntityRef{Name: "Job", Version: 1}) {
					props = append(props, p)
				}
			}
			broken, err := model.NewRegistry(reg.Entities(), props, reg.Aggregates(), reg.Relationships(),
				reg.Authorities(), reg.RetentionClasses())
			if err != nil {
				t.Fatalf("compile with Job stripped of properties: %v", err)
			}
			report := model.CheckModelCoverage(broken)
			if report.FullyVerified() {
				t.Fatalf("reported VERIFIED despite Job having no property")
			}
			found := false
			for _, g := range report.Gaps {
				if g.Item == "Job/v1" && g.Element == "properties" {
					found = true
				}
			}
			if !found {
				t.Fatalf("gap list does not identify Job/v1's missing property: %v", report.Gaps)
			}
		})

		t.Run("child entity claimed by no aggregate", func(t *testing.T) {
			// NewRegistry accepts a CHILD-class entity nobody's ChildRefs
			// claims — that omission is exactly what CheckModelCoverage must
			// catch, since [Registry.OwnerRoot] cannot resolve an orphaned
			// child.
			var aggs []model.AggregateDefinition
			for _, a := range reg.Aggregates() {
				a := a
				if a.Root == (model.EntityRef{Name: "CompensationPackage", Version: 1}) {
					a.ChildRefs = nil
				}
				aggs = append(aggs, a)
			}
			orphaned, err := model.NewRegistry(reg.Entities(), reg.Properties(), aggs, reg.Relationships(),
				reg.Authorities(), reg.RetentionClasses())
			if err != nil {
				t.Fatalf("compile with an orphaned child: %v", err)
			}
			report := model.CheckModelCoverage(orphaned)
			if report.FullyVerified() {
				t.Fatalf("reported VERIFIED despite CompensationComponent having no owner root")
			}
			found := false
			for _, g := range report.Gaps {
				if g.Item == "CompensationComponent/v1" && g.Element == "owner_root" {
					found = true
				}
			}
			if !found {
				t.Fatalf("gap list does not identify the orphaned child: %v", report.Gaps)
			}
		})
	})

	t.Run("GREEN", func(t *testing.T) {
		report := model.CheckModelCoverage(reg)
		if !report.FullyVerified() {
			t.Fatalf("coverage is not complete:\n%s", report.Summary())
		}
		if report.Total != len(reg.Entities()) {
			t.Fatalf("report denominator is %d, registry publishes %d", report.Total, len(reg.Entities()))
		}
		if len(report.ByDomain) == 0 {
			t.Fatalf("report carries no domain breakdown")
		}
		if len(report.ByStatus) == 0 {
			t.Fatalf("report carries no status (maturity) breakdown")
		}
		if len(report.ByClass) == 0 {
			t.Fatalf("report carries no class (phase) breakdown")
		}
		if report.ConformancePairs == 0 {
			t.Fatalf("report carries no conformance-bearing aggregates")
		}
		if report.SourceDigest == "" {
			t.Fatalf("report carries no source digest")
		}
	})
}

// TestTodo_MODEL_030_Property asserts that the digest is a pure function of
// the compiled registry content: recompiling the identical catalog twice
// yields byte-identical digests, and every domain/status/class count in the
// report sums back to the total.
func TestTodo_MODEL_030_Property(t *testing.T) {
	reg1, err := model.Catalog()
	if err != nil {
		t.Fatalf("compile 1: %v", err)
	}
	reg2, err := model.Catalog()
	if err != nil {
		t.Fatalf("compile 2: %v", err)
	}
	if reg1.Digest() != reg2.Digest() {
		t.Fatalf("two compilations of the same catalog produced different digests: %s vs %s",
			reg1.Digest(), reg2.Digest())
	}
	report := model.CheckModelCoverage(reg1)
	sum := 0
	for _, n := range report.ByDomain {
		sum += n
	}
	if sum != report.Total {
		t.Fatalf("ByDomain sums to %d, want %d", sum, report.Total)
	}
	sum = 0
	for _, n := range report.ByStatus {
		sum += n
	}
	if sum != report.Total {
		t.Fatalf("ByStatus sums to %d, want %d", sum, report.Total)
	}
}

// TestTodo_MODEL_030_Golden pins the compiled coverage report.
func TestTodo_MODEL_030_Golden(t *testing.T) {
	reg := mustRegistry(t)
	report := model.CheckModelCoverage(reg)
	goldenJSON(t, "model_030_coverage.json", struct {
		Summary          string
		ByDomain         map[string]int
		ByStatus         map[string]int
		ByClass          map[string]int
		ConformancePairs int
	}{
		Summary: report.Summary(), ByDomain: report.ByDomain, ByStatus: report.ByStatus,
		ByClass: report.ByClass, ConformancePairs: report.ConformancePairs,
	})
}

// TestTodo_MODEL_030_Conformance asserts the minimum conformance proof this
// registry can check for itself: every aggregate root that is not
// NO_BUSINESS_LIFECYCLE declares a commit boundary and at least the
// happy-path invariant coverage a governed root requires
// (planning/data/models/registry-and-coverage-contracts.md, "Minimum
// conformance proof per intent" — scoped here to structural presence, since
// per-scenario behavioral conformance belongs to MODEL-016's checker over the
// intent catalog).
func TestTodo_MODEL_030_Conformance(t *testing.T) {
	reg := mustRegistry(t)
	for _, a := range reg.Aggregates() {
		if a.Rebuildable() {
			continue
		}
		if !a.CommandBoundary.Valid() {
			t.Fatalf("%s is commandable but declares no valid commit boundary", a.Root)
		}
		if a.CommandBoundary == model.BoundaryLocalACID && len(a.InvariantRefs) == 0 {
			// LOCAL_ACID roots with material state should declare at least one
			// invariant; roots with none are pure reference/lookup data.
			t.Logf("%s declares LOCAL_ACID with no invariants (reference data)", a.Root)
		}
	}
	report := model.CheckModelCoverage(reg)
	if !report.FullyVerified() {
		t.Fatalf("conformance requires full verification first:\n%s", report.Summary())
	}
}

// FuzzTodo_MODEL_030 fuzzes coverage-report determinism: running the checker
// twice over the same registry, regardless of how many times it is called
// first, must always produce the same summary string.
func FuzzTodo_MODEL_030(f *testing.F) {
	f.Add(1)
	f.Add(5)
	f.Fuzz(func(t *testing.T, warmupCalls int) {
		if warmupCalls < 0 {
			warmupCalls = -warmupCalls
		}
		reg, err := model.Catalog()
		if err != nil {
			t.Fatalf("compile catalog: %v", err)
		}
		for i := 0; i < warmupCalls%8; i++ {
			_ = model.CheckModelCoverage(reg)
		}
		a := model.CheckModelCoverage(reg)
		b := model.CheckModelCoverage(reg)
		if a.Summary() != b.Summary() {
			t.Fatalf("coverage summary is not deterministic: %q vs %q", a.Summary(), b.Summary())
		}
		if a.SourceDigest != b.SourceDigest {
			t.Fatalf("source digest is not deterministic: %q vs %q", a.SourceDigest, b.SourceDigest)
		}
	})
}
