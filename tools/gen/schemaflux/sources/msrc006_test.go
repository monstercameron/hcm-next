package sources_test

import (
	"path/filepath"
	"sort"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/tools/gen/schemaflux/sources"
)

func loadDeferredBundle(t *testing.T) sources.Bundle {
	t.Helper()
	root := findRepoRoot(t)
	mm, err := sources.LoadMetamodel(filepath.Join(root, "schema", "schemaflux", "metamodel", "v1", "metamodel.yaml"))
	if err != nil {
		t.Fatalf("LoadMetamodel: %v", err)
	}
	auths, rets, err := sources.LoadRegistries(filepath.Join(root, "schema", "schemaflux", "registries", "v1", "registries.yaml"))
	if err != nil {
		t.Fatalf("LoadRegistries: %v", err)
	}
	ents, rels, err := sources.LoadEntityFamilies(filepath.Join(root, "schema", "schemaflux", "deferred", "v1"))
	if err != nil {
		t.Fatalf("LoadEntityFamilies: %v", err)
	}
	return sources.Bundle{Metamodel: mm, Authorities: auths, Retentions: rets, Entities: ents, Relationships: rels}
}

// TestTodo_MSRC_006 proves every deferred domain has a compilable, explicit
// conformance model while remaining outside the publishable entity tree.
func TestTodo_MSRC_006(t *testing.T) {
	bundle := loadDeferredBundle(t)
	manifest, errs := sources.Compile(bundle)
	if len(errs) != 0 {
		t.Fatalf("deferred sources failed to compile: %v", errs)
	}
	if len(manifest.Entities) != 12 {
		t.Fatalf("deferred entity count = %d, want 12", len(manifest.Entities))
	}
	wantDomains := []string{"ANALYTICS", "BENEFITS", "CASES", "LEARNING", "LEAVE", "MOBILITY", "PAYROLL", "RECRUITING", "REGULATORY", "SAFETY", "TALENT", "TIME"}
	var gotDomains []string
	for _, e := range manifest.Entities {
		gotDomains = append(gotDomains, e.OwnerDomain)
		if e.Status != "DRAFT" || e.Covered {
			t.Errorf("%s: status=%q covered=%v, want DRAFT/false", e.Ref(), e.Status, e.Covered)
		}
		if e.CommandBoundary != "EXTERNAL_OBSERVATION" {
			t.Errorf("%s: command boundary=%q, want EXTERNAL_OBSERVATION", e.Ref(), e.CommandBoundary)
		}
		if e.LifecycleAssignment != "NO_BUSINESS_LIFECYCLE" {
			t.Errorf("%s: lifecycle=%q, want NO_BUSINESS_LIFECYCLE", e.Ref(), e.LifecycleAssignment)
		}
		for _, p := range e.Properties {
			if p.Status == "ACTIVE" {
				t.Errorf("%s.%s unexpectedly publishes ACTIVE property", e.Key, p.Name)
			}
		}
	}
	sort.Strings(gotDomains)
	if len(gotDomains) != len(wantDomains) {
		t.Fatalf("domains = %v, want %v", gotDomains, wantDomains)
	}
	for i := range wantDomains {
		if gotDomains[i] != wantDomains[i] {
			t.Errorf("domain[%d] = %q, want %q", i, gotDomains[i], wantDomains[i])
		}
	}
}

func TestTodo_MSRC_006_Golden(t *testing.T) {
	bundle := loadDeferredBundle(t)
	manifest, errs := sources.Compile(bundle)
	if len(errs) != 0 {
		t.Fatalf("Compile: %v", errs)
	}
	var refs []string
	for _, e := range manifest.Entities {
		refs = append(refs, e.Ref())
	}
	sort.Strings(refs)
	want := []string{"AnalyticsSource/v1", "BenefitsSource/v1", "CaseSource/v1", "LearningSource/v1", "LeaveSource/v1", "MobilitySource/v1", "PayrollSource/v1", "RecruitingSource/v1", "RegulatorySource/v1", "SafetySource/v1", "TalentSource/v1", "TimeSource/v1"}
	for i := range want {
		if refs[i] != want[i] {
			t.Errorf("ref[%d] = %q, want %q", i, refs[i], want[i])
		}
	}
}

func TestTodo_MSRC_006_Conformance(t *testing.T) {
	bundle := loadDeferredBundle(t)
	manifest, errs := sources.Compile(bundle)
	if len(errs) != 0 {
		t.Fatalf("Compile: %v", errs)
	}
	for _, e := range manifest.Entities {
		if e.Class != "READ_MODEL" {
			t.Errorf("%s class=%q, want READ_MODEL", e.Ref(), e.Class)
		}
		for _, p := range e.Properties {
			if p.AuthorityRef == "" || p.RetentionClassRef == "" {
				t.Errorf("%s.%s lacks authority/retention gate", e.Ref(), p.Name)
			}
		}
	}
}

func TestTodo_MSRC_006_Mutation(t *testing.T) {
	bundle := loadDeferredBundle(t)
	bundle.Entities[0].Status = "ACTIVE"
	_, errs := sources.Compile(bundle)
	if len(errs) == 0 {
		t.Fatal("mutated uncovered deferred source compiled as ACTIVE")
	}
}
