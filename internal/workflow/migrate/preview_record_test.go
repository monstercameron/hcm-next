package migrate

import (
	"testing"

	"github.com/monstercameron/hcm-next/internal/capability"
	"github.com/monstercameron/hcm-next/internal/workflow"
	"github.com/monstercameron/hcm-next/internal/workflow/migrationpreview"
)

// registryForTest and the two compiled plans below reuse the repository's one
// real compiled definition (the Promotion reference), the same way
// internal/workflow/migrationpreview's own test file does: a hand-built
// minimal [workflow.Definition] would have to reconstruct every compiler
// invariant (start node, governance, end spec) from scratch for no benefit,
// where bumping the reference definition's version and recompiling gives two
// genuinely distinct, genuinely valid compiled plans for free.
func registryForTest(t *testing.T) *capability.Registry {
	t.Helper()
	reg, err := capability.NewBootstrapRegistry()
	if err != nil {
		t.Fatalf("bootstrap capability registry: %v", err)
	}
	return reg
}

func sourcePlanForTest(t *testing.T, reg *capability.Registry) *workflow.CompiledWorkflow {
	t.Helper()
	plan, err := workflow.CompilePromotionReference(reg)
	if err != nil {
		t.Fatalf("compile Promotion fixture: %v", err)
	}
	return plan
}

func targetPlanForTest(t *testing.T, reg *capability.Registry) *workflow.CompiledWorkflow {
	t.Helper()
	def := workflow.PromotionReferenceDefinition()
	def.Version++
	plan, err := workflow.Compile(def, workflow.Options{Phase: workflow.PhaseP1A, Capabilities: reg})
	if err != nil {
		t.Fatalf("compile bumped Promotion fixture: %v", err)
	}
	return plan
}

func TestCapturePreview_RequiresBothPlans(t *testing.T) {
	reg := registryForTest(t)
	plan := sourcePlanForTest(t, reg)
	if _, err := CapturePreview(nil, plan, migrationpreview.Result{}); CodeOf(err) != CodeInvalidRequest {
		t.Fatalf("nil source: code = %q, want %q", CodeOf(err), CodeInvalidRequest)
	}
	if _, err := CapturePreview(plan, nil, migrationpreview.Result{}); CodeOf(err) != CodeInvalidRequest {
		t.Fatalf("nil target: code = %q, want %q", CodeOf(err), CodeInvalidRequest)
	}
}

func TestCapturePreview_RefusesAMismatchedResult(t *testing.T) {
	reg := registryForTest(t)
	source := sourcePlanForTest(t, reg)
	target := targetPlanForTest(t, reg)

	t.Run("workflow id mismatch", func(t *testing.T) {
		result := migrationpreview.Result{
			SourceWorkflowID: "not-" + source.WorkflowID, SourceVersion: source.Version,
			TargetWorkflowID: target.WorkflowID, TargetVersion: target.Version,
		}
		if _, err := CapturePreview(source, target, result); CodeOf(err) != CodePlanMismatch {
			t.Fatalf("code = %q, want %q", CodeOf(err), CodePlanMismatch)
		}
	})

	t.Run("version mismatch", func(t *testing.T) {
		result := migrationpreview.Result{
			SourceWorkflowID: source.WorkflowID, SourceVersion: source.Version + 5,
			TargetWorkflowID: target.WorkflowID, TargetVersion: target.Version,
		}
		if _, err := CapturePreview(source, target, result); CodeOf(err) != CodePlanMismatch {
			t.Fatalf("code = %q, want %q", CodeOf(err), CodePlanMismatch)
		}
	})
}

func TestCapturePreview_SealsDigestsAndIsDeterministic(t *testing.T) {
	reg := registryForTest(t)
	source := sourcePlanForTest(t, reg)
	target := targetPlanForTest(t, reg)
	result := migrationpreview.Result{
		SourceWorkflowID: source.WorkflowID, SourceVersion: source.Version,
		TargetWorkflowID: target.WorkflowID, TargetVersion: target.Version,
		Assessments: []migrationpreview.Assessment{
			{
				InstanceID: "instance-1",
				Source:     migrationpreview.LiveInstanceState{InstanceID: "instance-1", StepID: workflow.PromotionNodeSnapshotWorker, Stage: "READY"},
				Outcome:    migrationpreview.OutcomeSafe,
			},
		},
	}

	rec, err := CapturePreview(source, target, result)
	if err != nil {
		t.Fatalf("CapturePreview: %v", err)
	}
	if rec.SourceDigest != source.Digest() || rec.TargetDigest != target.Digest() {
		t.Fatalf("captured digests = %s/%s, want %s/%s", rec.SourceDigest, rec.TargetDigest, source.Digest(), target.Digest())
	}
	if rec.Digest() == "" {
		t.Fatal("CapturePreview minted no digest")
	}

	again, err := CapturePreview(source, target, result)
	if err != nil {
		t.Fatalf("second CapturePreview: %v", err)
	}
	if again.Digest() != rec.Digest() {
		t.Fatalf("identical inputs produced different digests: %s vs %s", again.Digest(), rec.Digest())
	}

	assessment, found := rec.Assessment("instance-1")
	if !found || assessment.Outcome != migrationpreview.OutcomeSafe {
		t.Fatalf("Assessment(instance-1) = %+v, found=%v", assessment, found)
	}
	if _, found := rec.Assessment("instance-missing"); found {
		t.Fatal("Assessment reported a hit for an instance the preview never classified")
	}
}

func TestCapturePreview_ChangedTargetChangesTheDigest(t *testing.T) {
	reg := registryForTest(t)
	source := sourcePlanForTest(t, reg)
	target := targetPlanForTest(t, reg)

	def := workflow.PromotionReferenceDefinition()
	def.Version += 2
	otherTarget, err := workflow.Compile(def, workflow.Options{Phase: workflow.PhaseP1A, Capabilities: reg})
	if err != nil {
		t.Fatalf("compile a third Promotion version: %v", err)
	}

	rec, err := CapturePreview(source, target, migrationpreview.Result{
		SourceWorkflowID: source.WorkflowID, SourceVersion: source.Version,
		TargetWorkflowID: target.WorkflowID, TargetVersion: target.Version,
	})
	if err != nil {
		t.Fatalf("CapturePreview: %v", err)
	}
	other, err := CapturePreview(source, otherTarget, migrationpreview.Result{
		SourceWorkflowID: source.WorkflowID, SourceVersion: source.Version,
		TargetWorkflowID: otherTarget.WorkflowID, TargetVersion: otherTarget.Version,
	})
	if err != nil {
		t.Fatalf("CapturePreview with a different target: %v", err)
	}
	if other.Digest() == rec.Digest() {
		t.Fatal("two previews against different target plans digested identically")
	}
}
