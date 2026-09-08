package intentmanifests

import (
	"errors"
	"path/filepath"
	"testing"
)

func TestIntentGapCompilerGeneratesCompleteAtomicTDDTodos(t *testing.T) {
	catalog := realCatalog(t)
	c := completeContract(catalog[2])
	c.ResultProperties = nil
	c.RevalidationRules = nil
	c.OperationsOwner = ""
	r, e := CompileIntentGaps(GapCompilerInput{Catalog: catalog, Contracts: []IntentContract{c}})
	if e != nil {
		t.Fatal(e)
	}
	if len(r.Gaps) != 3 || len(r.NewCandidates) != 3 {
		t.Fatalf("gaps=%+v candidates=%+v", r.Gaps, r.NewCandidates)
	}
	for _, x := range r.NewCandidates {
		if x.RedOracle == "" || x.GreenOracle == "" || len(x.NegativeClasses) == 0 || len(x.FaultClasses) == 0 || len(x.SecurityClasses) == 0 {
			t.Fatalf("incomplete candidate: %+v", x)
		}
		if len(x.Dependencies) != 0 {
			t.Fatalf("fabricated dependency: %+v", x)
		}
	}
}

func TestTodo_GOV_027_Property(t *testing.T) {
	catalog := realCatalog(t)
	base := completeContract(catalog[2])
	cases := []struct {
		name   string
		mutate func(*IntentContract)
	}{
		{"input_properties", func(c *IntentContract) { c.InputProperties = nil }}, {"result_properties", func(c *IntentContract) { c.ResultProperties = nil }}, {"entity_relations", func(c *IntentContract) { c.EntityRelations = nil }}, {"authority_source", func(c *IntentContract) { c.Descriptor.Authority = nil }}, {"temporal", func(c *IntentContract) { c.Descriptor.Time = nil }}, {"lifecycle", func(c *IntentContract) { c.Descriptor.Lifecycle.Business = "" }}, {"invariants", func(c *IntentContract) { c.Invariants = nil }}, {"conflict", func(c *IntentContract) { c.ConflictRules = nil }}, {"proposal", func(c *IntentContract) { c.ProposalRules = nil }}, {"revalidation", func(c *IntentContract) { c.RevalidationRules = nil }}, {"cancellation", func(c *IntentContract) { c.CancellationRules = nil }}, {"compensation", func(c *IntentContract) { c.CompensationRules = nil }}, {"governance", func(c *IntentContract) { c.GovernanceObligations = nil }}, {"capability", func(c *IntentContract) { c.CapabilityPaths = nil }}, {"direct_path", func(c *IntentContract) { c.DirectPaths = nil }}, {"workflow", func(c *IntentContract) { c.WorkflowComposition = nil }}, {"transaction_effect", func(c *IntentContract) { c.Descriptor.Effects = nil }}, {"observation_reconciliation_repair", func(c *IntentContract) { c.ObservationReconciliationRepair = nil }}, {"evidence", func(c *IntentContract) { c.Descriptor.Evidence = nil }}, {"retention", func(c *IntentContract) { c.RetentionRules = nil }}, {"negative_dimensions", func(c *IntentContract) { c.Descriptor.NegativePolicy = nil }}, {"slo", func(c *IntentContract) { c.SLO = "" }}, {"operations_owner", func(c *IntentContract) { c.OperationsOwner = "" }}}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			c := base
			tt.mutate(&c)
			r, e := CompileIntentGaps(GapCompilerInput{Catalog: catalog, Contracts: []IntentContract{c}})
			if e != nil {
				t.Fatal(e)
			}
			if len(r.Gaps) != 1 || r.Gaps[0].Dimension != tt.name {
				t.Fatalf("got %+v", r.Gaps)
			}
		})
	}
}

func TestTodo_GOV_027_Golden(t *testing.T) {
	catalog := realCatalog(t)
	c := completeContract(catalog[2])
	c.Invariants = nil
	r, e := CompileIntentGaps(GapCompilerInput{Catalog: catalog, Contracts: []IntentContract{c}})
	if e != nil {
		t.Fatal(e)
	}
	if len(r.NewCandidates) != 1 || r.NewCandidates[0].Key != "hcmnext.people.promote_worker/v1::invariants" || r.NewCandidates[0].TestName != "TestIntentGap_hcmnext_people_promote_worker_v1_invariants" {
		t.Fatalf("golden changed: %+v", r)
	}
	if r.Digest != "sha256:da554ea646f82cc42252208da9cc3ade7feb4dbc202baeb72ed46fd6eed72933" {
		t.Fatalf("canonical candidate bytes digest=%s", r.Digest)
	}
}

func TestTodo_GOV_027_Conformance(t *testing.T) {
	catalog := realCatalog(t)
	// Exercise the package's production validator as well as the YAML loader:
	// the compiler's catalog authority is the same contract validated at intake.
	if err := ValidateIntentManifest(catalog); err != nil {
		t.Fatalf("production catalog invalid: %v", err)
	}
	if firstDigest, err := ComputeIntentDigest(catalog); err != nil || firstDigest == "" {
		t.Fatalf("production catalog digest=%q error=%v", firstDigest, err)
	}
	live, err := CompileCatalogGaps(catalog, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(live.Gaps) != 238 || len(live.NewCandidates) != 238 {
		t.Fatalf("live descriptor gaps=%d candidates=%d, want 238", len(live.Gaps), len(live.NewCandidates))
	}
	c := completeContract(catalog[2])
	c.Invariants = nil
	first, e := CompileIntentGaps(GapCompilerInput{Catalog: catalog, Contracts: []IntentContract{c}})
	if e != nil {
		t.Fatal(e)
	}
	second, e := CompileIntentGaps(GapCompilerInput{Catalog: catalog, Contracts: []IntentContract{c}, ExistingTodos: first.Candidates})
	if e != nil {
		t.Fatal(e)
	}
	if len(second.NewCandidates) != 0 || first.Digest != second.Digest {
		t.Fatalf("not fixed point: %+v", second)
	}
	c.Descriptor.IntentTypeID = "hcmnext.people.unknown"
	r, e := CompileIntentGaps(GapCompilerInput{Catalog: catalog, Contracts: []IntentContract{c}})
	if e != nil || len(r.Gaps) != 0 {
		t.Fatalf("unknown accepted: %+v %v", r, e)
	}
}

func TestTodo_GOV_027_Mutation(t *testing.T) {
	catalog := realCatalog(t)
	c := completeContract(catalog[2])
	c.Invariants = nil
	first, _ := CompileIntentGaps(GapCompilerInput{Catalog: catalog, Contracts: []IntentContract{c}})
	changed := first.Candidates[0]
	changed.GreenOracle = "false completion"
	_, e := CompileIntentGaps(GapCompilerInput{Catalog: catalog, Contracts: []IntentContract{c}, ExistingTodos: []TodoCandidate{changed}})
	if !errors.Is(e, ErrDuplicateTodo) {
		t.Fatalf("error=%v", e)
	}
	_, e = CompileIntentGaps(GapCompilerInput{Catalog: catalog, Contracts: []IntentContract{c, c}})
	if !errors.Is(e, ErrDuplicateContract) {
		t.Fatalf("duplicate error=%v", e)
	}
}

func FuzzTodo_GOV_027(f *testing.F) {
	f.Add("hcmnext.people.unknown")
	f.Fuzz(func(t *testing.T, name string) {
		catalog := realCatalog(t)
		c := completeContract(catalog[2])
		c.Descriptor.IntentTypeID = name
		r, e := CompileIntentGaps(GapCompilerInput{Catalog: catalog, Contracts: []IntentContract{c}})
		known := false
		for _, d := range catalog {
			known = known || intentRef(d) == intentRef(c.Descriptor)
		}
		if !known && (e != nil || len(r.Gaps) != 0 || len(r.NewCandidates) != 0) {
			t.Fatalf("unknown emitted work: %+v %v", r, e)
		}
	})
}

func realCatalog(t testing.TB) []IntentDescriptor {
	t.Helper()
	d, e := LoadIntentManifestYAML(filepath.Join("..", "..", "..", "definitions", "governance", "intent-conformance-descriptors.yaml"))
	if e != nil {
		t.Fatal(e)
	}
	return d
}
func completeContract(d IntentDescriptor) IntentContract {
	return IntentContract{Descriptor: d, OwnerDomain: "people", Intelligence: "SOL_HIGH", InputProperties: []string{"input"}, ResultProperties: []string{"result"}, EntityRelations: []string{"relation"}, Invariants: []string{"invariant"}, ConflictRules: []string{"conflict"}, ProposalRules: []string{"proposal"}, RevalidationRules: []string{"revalidate"}, CancellationRules: []string{"cancel"}, CompensationRules: []string{"compensate"}, GovernanceObligations: []string{"govern"}, CapabilityPaths: []string{"capability"}, DirectPaths: []string{"direct"}, WorkflowComposition: []string{"workflow"}, ObservationReconciliationRepair: []string{"repair"}, RetentionRules: []string{"retain"}, SLO: "slo", OperationsOwner: "ops"}
}
