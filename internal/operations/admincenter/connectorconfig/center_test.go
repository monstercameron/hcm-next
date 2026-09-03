package connectorconfig

import (
	"errors"
	"testing"
)

func TestActionRegistryExact(t *testing.T) {
	want := []Action{ActionInspect, ActionTest, ActionRedrive, ActionReconcile, ActionDiff, ActionSimulate, ActionPromote, ActionRollback}
	got := Actions()
	if len(got) != len(want) {
		t.Fatalf("registry length=%d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("registry[%d]=%q, want %q", i, got[i], want[i])
		}
	}
	if len(Registry()) != len(want) {
		t.Fatal("registry map is incomplete")
	}
}

func TestPlanActionSafetyBoundary(t *testing.T) {
	base := ActionRequest{Action: ActionTest, Authorized: true, EvidenceRefs: []EvidenceRef{{ID: "ev-1", Kind: "test"}}}
	if _, err := PlanAction(base); err != nil {
		t.Fatalf("valid plan: %v", err)
	}
	for _, tc := range []struct {
		name   string
		mutate func(*ActionRequest)
		want   error
	}{
		{"production", func(r *ActionRequest) { r.Production = true }, ErrProductionTest},
		{"production environment", func(r *ActionRequest) { r.Environment = "PRODUCTION" }, ErrProductionTest},
		{"parent rerun", func(r *ActionRequest) { r.Action = ActionRedrive; r.ParentWorkflowID = "wf-1" }, ErrParentRerun},
		{"stale version", func(r *ActionRequest) { r.ExpectedStateVersion = 2; r.CurrentStateVersion = 1 }, ErrStaleConfig},
		{"stale digest", func(r *ActionRequest) { r.ExpectedConfigDigest = "new"; r.CurrentConfigDigest = "old" }, ErrStaleConfig},
		{"no auth", func(r *ActionRequest) { r.Authorized = false }, ErrUnauthorized},
		{"no evidence", func(r *ActionRequest) { r.EvidenceRefs = nil }, ErrEvidenceRequired},
	} {
		r := base
		tc.mutate(&r)
		if _, err := PlanAction(r); !errors.Is(err, tc.want) {
			t.Errorf("%s: error=%v, want %v", tc.name, err, tc.want)
		}
	}
	p, err := PlanAction(ActionRequest{Action: ActionPromote, Authorized: true, EvidenceRefs: []EvidenceRef{{ID: "ev", Kind: "approval"}}})
	if err != nil || !p.RequiresApproval || !p.Mutates {
		t.Fatalf("promote plan=%+v err=%v", p, err)
	}
}
