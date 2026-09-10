package workflow_test

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/steps/subworkflow"
)

func cancellationRequest() workflow.CancellationRequest {
	return workflow.CancellationRequest{
		RunID:    "run/equipment-shipment-7",
		Revision: "rev3",
		Phase:    "EXECUTING",
		Children: []workflow.CancellableNode{
			{Ref: subworkflow.ChildRef{Workflow: "notify.send", Version: "v2.0.0"},
				State: subworkflow.StateRunning, Cancellable: true},
			{Ref: subworkflow.ChildRef{Workflow: "reserve.stock", Version: "v1.1.0"},
				State: subworkflow.StateSucceeded},
		},
		Effects: []workflow.EffectRecord{
			{ID: "email-sent", Reversible: true},
		},
	}
}

// TestTodo_WF_RUN_010 proves governed cancellation: running work cancels
// cleanly, irreversible effects route to compensation or refuse, unknown
// states repair instead of completing, and history is never deleted.
func TestTodo_WF_RUN_010(t *testing.T) {
	outcome, err := workflow.DecideCancellation(cancellationRequest())
	if err != nil {
		t.Fatalf("cancellable run rejected: %v", err)
	}
	if outcome.Decision != workflow.Cancelled {
		t.Fatalf("decision = %s, want CANCELLED", outcome.Decision)
	}
	if len(outcome.ChildReports) != 2 || outcome.ChildReports[0].Report != subworkflow.ReportCancelled {
		t.Fatalf("child reports wrong: %+v", outcome.ChildReports)
	}
	if len(outcome.History) != 2 {
		t.Fatalf("history deleted: %+v", outcome)
	}

	t.Run("irreversible effect needs compensation", func(t *testing.T) {
		req := cancellationRequest()
		req.Effects = append(req.Effects, workflow.EffectRecord{ID: "stock-reserved", Compensation: "release-stock"})
		outcome, err := workflow.DecideCancellation(req)
		if err != nil {
			t.Fatal(err)
		}
		if outcome.Decision != workflow.CompensationRequired {
			t.Fatalf("decision = %s, want COMPENSATION_REQUIRED", outcome.Decision)
		}
	})

	t.Run("uncompensatable effect cannot cancel", func(t *testing.T) {
		req := cancellationRequest()
		req.Children = req.Children[:1]
		req.Effects = []workflow.EffectRecord{{ID: "payment-captured"}}
		outcome, err := workflow.DecideCancellation(req)
		if err != nil {
			t.Fatal(err)
		}
		if outcome.Decision != workflow.CannotCancel {
			t.Fatalf("decision = %s, want CANNOT_CANCEL", outcome.Decision)
		}
	})

	t.Run("uncancellable child cannot cancel", func(t *testing.T) {
		req := cancellationRequest()
		req.Children[0].Cancellable = false
		outcome, err := workflow.DecideCancellation(req)
		if err != nil {
			t.Fatal(err)
		}
		if outcome.Decision != workflow.CannotCancel {
			t.Fatalf("decision = %s, want CANNOT_CANCEL", outcome.Decision)
		}
	})

	t.Run("unknown state repairs", func(t *testing.T) {
		req := cancellationRequest()
		req.Children[0].State = "VANISHED"
		outcome, err := workflow.DecideCancellation(req)
		if err != nil {
			t.Fatal(err)
		}
		if outcome.Decision != workflow.RepairRequired {
			t.Fatalf("decision = %s, want REPAIR_REQUIRED", outcome.Decision)
		}
	})

	t.Run("missing run identity refused", func(t *testing.T) {
		req := cancellationRequest()
		req.RunID = ""
		if _, err := workflow.DecideCancellation(req); err == nil {
			t.Fatal("identity-free cancellation accepted")
		}
	})
}
