package intent_test

import (
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/intent"
)

func validEmissionRequest() intent.EmissionRequest {
	return intent.EmissionRequest{
		ParentID:         "intent/payroll-run-88",
		ParentRevision:   "rev9",
		ParentDefinition: "payroll.run",
		SourceNode:       "node/compute-overtime",
		SourceEvent:      "event/timesheet-approved",
		ChildDefinition:  "payroll.run.overtime",
		ChildVersion:     "v1.2.0",
		Ordinal:          0,
		Kind:             intent.RelationChild,
		ParentScope:      []string{"org:acme", "payroll:run"},
		ChildScope:       []string{"payroll:run"},
		ParentPurpose:    "payroll.run",
		ChildPurpose:     "payroll.run.overtime",
		Ancestors:        []string{"payroll.season"},
		Depth:            1,
		MaxDepth:         3,
		MaxFanout:        4,
		Cost:             10,
		MaxCost:          100,
		ManifestDecision: "manifest/decisions/44",
		Behavior:         intent.WaitForChild,
	}
}

func TestIntentEmissionIsIdempotentBoundedAndGoverned(t *testing.T) {
	emitter := intent.NewEmitter()
	first, err := emitter.Emit(validEmissionRequest())
	if err != nil {
		t.Fatalf("valid emission rejected: %v", err)
	}
	if first.Duplicate {
		t.Fatal("first emission marked duplicate")
	}
	if first.Governance.Purpose != "payroll.run.overtime" || len(first.Governance.Scope) != 1 {
		t.Fatalf("child lost independent governance: %+v", first.Governance)
	}
	second, err := emitter.Emit(validEmissionRequest())
	if err != nil {
		t.Fatalf("duplicate delivery rejected: %v", err)
	}
	if !second.Duplicate || second.Key != first.Key || second.ChildID != first.ChildID {
		t.Fatalf("duplicate did not return the original child: %+v vs %+v", first, second)
	}

	adversaries := []struct {
		name   string
		mutate func(*intent.EmissionRequest)
		cause  error
	}{
		{"recursive self-emission", func(r *intent.EmissionRequest) { r.ChildDefinition = r.ParentDefinition }, intent.ErrRecursiveEmission},
		{"ancestor recursion", func(r *intent.EmissionRequest) { r.ChildDefinition = "payroll.season" }, intent.ErrRecursiveEmission},
		{"fanout overflow", func(r *intent.EmissionRequest) { r.Ordinal = r.MaxFanout }, intent.ErrFanoutOverflow},
		{"depth overflow", func(r *intent.EmissionRequest) { r.Depth = r.MaxDepth + 1 }, intent.ErrDepthOverflow},
		{"resource overflow", func(r *intent.EmissionRequest) { r.Cost = r.MaxCost + 1 }, intent.ErrBudgetOverflow},
		{"broader scope", func(r *intent.EmissionRequest) { r.ChildScope = []string{"payroll:run", "hr:admin"} }, intent.ErrScopeExpansion},
		{"broader purpose", func(r *intent.EmissionRequest) { r.ChildPurpose = "payroll" }, intent.ErrPurposeBroadening},
		{"foreign purpose", func(r *intent.EmissionRequest) { r.ChildPurpose = "hr.leave" }, intent.ErrPurposeBroadening},
		{"missing manifest decision", func(r *intent.EmissionRequest) { r.ManifestDecision = "" }, intent.ErrMissingManifestDecision},
		{"invalid behavior", func(r *intent.EmissionRequest) { r.Behavior = "HOPE" }, intent.ErrInvalidCompletionBehavior},
		{"invalid kind", func(r *intent.EmissionRequest) { r.Kind = intent.RelationDependency }, intent.ErrInvalidEmission},
	}
	for _, tc := range adversaries {
		t.Run(tc.name, func(t *testing.T) {
			req := validEmissionRequest()
			tc.mutate(&req)
			if _, err := intent.NewEmitter().Emit(req); !errors.Is(err, tc.cause) {
				t.Fatalf("want %v, got %v", tc.cause, err)
			}
		})
	}

	t.Run("conflicting duplicate fails", func(t *testing.T) {
		emitter := intent.NewEmitter()
		if _, err := emitter.Emit(validEmissionRequest()); err != nil {
			t.Fatal(err)
		}
		req := validEmissionRequest()
		req.ChildScope = []string{"org:acme"}
		if _, err := emitter.Emit(req); !errors.Is(err, intent.ErrEmissionConflict) {
			t.Fatalf("conflicting duplicate accepted: %v", err)
		}
	})

	t.Run("parent completion requires mandatory outcomes", func(t *testing.T) {
		emitter := intent.NewEmitter()
		child, err := emitter.Emit(validEmissionRequest())
		if err != nil {
			t.Fatal(err)
		}
		if err := emitter.Complete("intent/payroll-run-88", []string{child.Key}); !errors.Is(err, intent.ErrMissingChildOutcome) {
			t.Fatalf("completion without child outcome accepted: %v", err)
		}
		if err := emitter.RecordOutcome(child.Key, "SUCCEEDED"); err != nil {
			t.Fatal(err)
		}
		if err := emitter.Complete("intent/payroll-run-88", []string{child.Key}); err != nil {
			t.Fatalf("completion with recorded outcome rejected: %v", err)
		}
	})
}
