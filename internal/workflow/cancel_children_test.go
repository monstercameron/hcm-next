package workflow_test

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/steps/subworkflow"
)

func propagationRequest() workflow.ParentCancellationRequest {
	return workflow.ParentCancellationRequest{
		RunID:    "run/recruit-3",
		Revision: "rev2",
		Children: []workflow.PropagatingChild{
			{Ref: subworkflow.ChildRef{Workflow: "notify.send", Version: "v2.0.0"},
				State: subworkflow.StateRunning, Cancellable: true, Mandatory: true},
			{Ref: subworkflow.ChildRef{Workflow: "schedule.interview", Version: "v1.0.0"},
				State: subworkflow.StateSucceeded, Mandatory: true},
		},
	}
}

// TestTodo_WF_RUN_011 proves parent cancellation reaches every child:
// each child reports, mandatory outcomes reconcile, detached children keep
// accountable owners, and a parent never closes cancelled over an
// unresolved child.
func TestTodo_WF_RUN_011(t *testing.T) {
	outcome, err := workflow.PropagateCancellationToChildren(propagationRequest())
	if err != nil {
		t.Fatalf("propagating run rejected: %v", err)
	}
	if outcome.Decision != workflow.Cancelled {
		t.Fatalf("decision = %s, want CANCELLED", outcome.Decision)
	}
	if len(outcome.Completion.Children) != 2 {
		t.Fatalf("completion dropped child truth: %+v", outcome.Completion)
	}
	for i, truth := range outcome.Completion.Children {
		if truth.Mandatory != (i < 2) {
			t.Fatalf("mandatory flag lost: %+v", truth)
		}
	}

	t.Run("detached child keeps its owner", func(t *testing.T) {
		req := propagationRequest()
		expanded, err := subworkflow.Expand(subworkflow.Expansion{
			ParentRevision:      "run/recruit-3#rev2",
			Child:               subworkflow.ChildRef{Workflow: "background.check", Version: "v1.0.0"},
			ParentApprovedScope: []string{"hr:read"}, CallerScope: []string{"hr:read"},
			ChildPolicyScope: []string{"hr:read"},
			Depth:            1, MaxDepth: 2, SiblingOrdinal: 2, MaxFanout: 3,
			Certificate: "cert/9", WaitMode: subworkflow.WaitModeDetachWithObligation,
		})
		if err != nil {
			t.Fatal(err)
		}
		obligation, err := subworkflow.Detach(expanded, "corr/recruit-3/bg")
		if err != nil {
			t.Fatal(err)
		}
		req.Children = append(req.Children, workflow.PropagatingChild{
			Ref: expanded.Child, State: subworkflow.StateRunning, Cancellable: true,
			Detached: true, Obligation: obligation,
		})
		outcome, err := workflow.PropagateCancellationToChildren(req)
		if err != nil {
			t.Fatal(err)
		}
		if len(outcome.Obligations) != 1 || outcome.Obligations[0].Correlation != "corr/recruit-3/bg" {
			t.Fatalf("detached owner lost: %+v", outcome)
		}
	})

	t.Run("ownerless detached child refused", func(t *testing.T) {
		req := propagationRequest()
		req.Children = append(req.Children, workflow.PropagatingChild{
			Ref:   subworkflow.ChildRef{Workflow: "background.check", Version: "v1.0.0"},
			State: subworkflow.StateRunning, Cancellable: true, Detached: true,
		})
		if _, err := workflow.PropagateCancellationToChildren(req); err == nil {
			t.Fatal("ownerless detached child accepted")
		}
	})

	t.Run("unresolved child never closes cancelled", func(t *testing.T) {
		req := propagationRequest()
		req.Children[0].State = "VANISHED"
		req.Children[0].Mandatory = false
		outcome, err := workflow.PropagateCancellationToChildren(req)
		if err != nil {
			t.Fatal(err)
		}
		if outcome.Decision != workflow.RepairRequired {
			t.Fatalf("decision = %s, want REPAIR_REQUIRED", outcome.Decision)
		}
	})

	t.Run("missing mandatory outcome refused", func(t *testing.T) {
		req := propagationRequest()
		req.Children[0].State = "VANISHED"
		if _, err := workflow.PropagateCancellationToChildren(req); err == nil {
			t.Fatal("unresolved mandatory child accepted")
		}
	})
}
