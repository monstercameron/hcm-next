package version_test

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	version "github.com/monstercameron/human-capital-management-suite/internal/workflow/version"
)

// TestTodo_WF_COMP_006_Mutation kills one mutant per WF-COMP-006 governance
// rule: a single flipped field in otherwise-valid activation evidence, or a
// single-point tamper of a published record, must be rejected with the code
// that rule owns. Mirrors internal/workflow's own runMutations discipline
// (see internal/workflow/fixtures_test.go).
func TestTodo_WF_COMP_006_Mutation(t *testing.T) {
	def := workflow.PromotionReferenceDefinition()

	// Every mutation case below starts from a version already published and
	// evidence that would otherwise activate it cleanly, then flips exactly
	// one field.
	type mutationCase struct {
		name   string
		mutate func(ev *version.ActivationEvidence)
		code   string
	}
	cases := []mutationCase{
		{"authorized_flipped_false", func(ev *version.ActivationEvidence) {
			ev.Authorized = false
		}, version.CodeUnauthorizedActivation},
		{"approver_blanked", func(ev *version.ActivationEvidence) {
			ev.ApprovedBy = ""
		}, version.CodeUnauthorizedActivation},
		{"reviewed_digest_stale", func(ev *version.ActivationEvidence) {
			ev.ReviewedPlanDigest = "sha256:a-different-review"
		}, version.CodeChangedAfterReview},
		{"reviewed_digest_blanked", func(ev *version.ActivationEvidence) {
			ev.ReviewedPlanDigest = ""
		}, version.CodeChangedAfterReview},
		{"tests_passed_flipped_false", func(ev *version.ActivationEvidence) {
			ev.TestsPassed = false
		}, version.CodeFailedTest},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := version.NewRegistry()
			v := publishPromotion(t, store)
			ev := validEvidence(v)
			tc.mutate(&ev)
			_, err := version.Activate(store, v.CompiledPlanDigest, ev)
			requireCode(t, err, tc.code)
		})
	}

	t.Run("dependency_digest_mutated_after_declaration", func(t *testing.T) {
		store := version.NewRegistry()
		plan := mustCompilePromotion(t)
		meta := validMeta()
		meta.DependsOn = []version.VersionRef{{WorkflowID: "hcmnext.workflows.upstream", CompiledPlanDigest: "sha256:v1"}}
		v, err := version.Publish(store, def, plan, promotionOptions(t), meta)
		if err != nil {
			t.Fatalf("publish with dependency: %v", err)
		}
		// Publish and activate the dependency at a digest that does not match
		// what v declared: the declared dependency is a specific pinned
		// version, not "whatever is currently active".
		upstreamDef := def
		upstreamDef.WorkflowID = "hcmnext.workflows.upstream"
		upstreamPlan, err := workflow.Compile(upstreamDef, promotionOptions(t))
		if err != nil {
			t.Fatalf("compile upstream: %v", err)
		}
		upstream, err := version.Publish(store, upstreamDef, upstreamPlan, promotionOptions(t), validMeta())
		if err != nil {
			t.Fatalf("publish upstream: %v", err)
		}
		if _, err := version.Activate(store, upstream.CompiledPlanDigest, validEvidence(upstream)); err != nil {
			t.Fatalf("activate upstream: %v", err)
		}
		// upstream's real digest never equals the literal "sha256:v1" this
		// mutation declared, so the dependency stays unresolved even though
		// *a* version of the same workflow id is active.
		_, err = version.Activate(store, v.CompiledPlanDigest, validEvidence(v))
		requireCode(t, err, version.CodeUnresolvedDependency)
	})

	t.Run("plan_recompiled_with_different_compiler_version_is_not_the_published_plan", func(t *testing.T) {
		store := version.NewRegistry()
		plan, err := workflow.Compile(def, workflow.Options{
			Phase:           workflow.PhaseP1A,
			Capabilities:    promotionRegistry(t),
			CompilerVersion: "hcmnext.workflow.compiler/mutant",
		})
		if err != nil {
			t.Fatalf("compile mutant: %v", err)
		}
		_, err = version.Publish(store, def, plan, promotionOptions(t), validMeta())
		requireCode(t, err, version.CodePlanDigestMismatch)
	})

	t.Run("record_content_mutated_after_minting_fails_verify", func(t *testing.T) {
		store := version.NewRegistry()
		v := publishPromotion(t, store)
		v.SemanticVersion = "9.9.9"
		if err := v.Verify(); err == nil {
			t.Fatal("a version whose content was altered after minting must stop verifying")
		} else if version.CodeOf(err) != version.CodeRecordMutated {
			t.Fatalf("expected %s, got %v", version.CodeRecordMutated, err)
		}
	})
}
