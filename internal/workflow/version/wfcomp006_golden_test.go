package version_test

import (
	"encoding/json"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	version "github.com/monstercameron/human-capital-management-suite/internal/workflow/version"
)

// TestTodo_WF_COMP_006_Golden pins the published compiled promotion reference
// version: its digests, its lifecycle status and the exact bytes a runtime
// would replay to pin an instance against it (WF-RUN-023's dependency). It
// publishes from the checked-in document
// internal/workflow/testdata/promotion_reference.json — the same artifact
// internal/workflow's own TestTodo_WF_COMP_001_Golden pins — not just the Go
// constructor, so this golden is provably about that file's exact bytes.
func TestTodo_WF_COMP_006_Golden(t *testing.T) {
	loaded, err := workflow.LoadFile(filepath.Join("..", "testdata", "promotion_reference.json"))
	if err != nil {
		t.Fatalf("load internal/workflow/testdata/promotion_reference.json: %v", err)
	}
	if !reflect.DeepEqual(loaded, workflow.PromotionReferenceDefinition()) {
		t.Fatal("the checked-in reference document no longer matches workflow.PromotionReferenceDefinition")
	}

	store := version.NewRegistry()
	def := loaded
	plan, err := workflow.Compile(def, promotionOptions(t))
	if err != nil {
		t.Fatalf("compile the loaded reference definition: %v", err)
	}
	published, err := version.Publish(store, def, plan, promotionOptions(t), validMeta())
	if err != nil {
		t.Fatalf("publish the loaded reference definition: %v", err)
	}

	if err := published.Verify(); err != nil {
		t.Fatalf("published golden must verify: %v", err)
	}

	goldenJSON(t, "promotion_version.json", struct {
		RecordDigest       string                  `json:"record_digest"`
		CompiledPlanDigest string                  `json:"compiled_plan_digest"`
		DefinitionDigest   string                  `json:"definition_digest"`
		Version            version.CompiledVersion `json:"version"`
	}{
		RecordDigest:       published.Digest(),
		CompiledPlanDigest: published.CompiledPlanDigest,
		DefinitionDigest:   published.DefinitionDigest,
		Version:            published,
	})

	activated, err := version.Activate(store, published.CompiledPlanDigest, validEvidence(published))
	if err != nil {
		t.Fatalf("activate: %v", err)
	}
	goldenJSON(t, "promotion_version_activated.json", struct {
		RecordDigest string                  `json:"record_digest"`
		Version      version.CompiledVersion `json:"version"`
	}{
		RecordDigest: activated.Digest(),
		Version:      activated,
	})

	// Reproducibility: publishing the identical definition and plan into an
	// entirely independent store produces byte-identical canonical plan
	// bytes and the same record digest. A golden that only ever compares
	// against itself is not proof of determinism.
	again := publishPromotion(t, version.NewRegistry())
	if again.Digest() != published.Digest() {
		t.Fatalf("republishing the same content in a fresh store produced a different record digest: %s vs %s",
			again.Digest(), published.Digest())
	}
	if string(again.CanonicalPlanBytes) != string(published.CanonicalPlanBytes) {
		t.Fatal("republishing the same content produced different canonical plan bytes")
	}

	// The golden plan bytes must themselves be exactly what
	// internal/workflow's own promotion_plan.json golden already pins,
	// modulo the enclosing publication record: the plan a version carries is
	// the same plan the compiler golden already promises to keep stable.
	rebuiltPlan := mustCompilePromotion(t)
	rawPlan, err := json.MarshalIndent(rebuiltPlan, "", "  ")
	if err != nil {
		t.Fatalf("marshal plan: %v", err)
	}
	if string(rawPlan)+"\n" != string(published.CanonicalPlanBytes) {
		t.Fatal("the version's canonical plan bytes are not the compiler's own canonical rendering of the plan")
	}
}
