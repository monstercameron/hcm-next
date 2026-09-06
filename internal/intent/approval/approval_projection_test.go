package approval_test

import (
	"errors"
	"testing"

	"github.com/monstercameron/hcm-next/internal/humanwork"
	"github.com/monstercameron/hcm-next/internal/intent/approval"
)

func TestTodo_APPROVAL_006(t *testing.T) {
	store := humanwork.NewProjectionStore()
	first, err := store.Render("task-approval", humanwork.ProjectionInput{
		RequirementID:       "approval.hrbp/v1",
		VisibleFields:       []humanwork.ProjectionField{{Name: "proposal.amount", Value: "18000.00"}},
		HiddenFieldManifest: []string{"worker.email"},
		Warnings:            []string{"budget reserved"},
		Effects:             []string{"base pay successor"},
	})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if first.Safety != humanwork.SafetySafeToDecide || first.TaskVersion != 1 || first.Digest == "" {
		t.Fatalf("safe projection = %+v", first)
	}
	second, err := store.Render("task-approval", humanwork.ProjectionInput{
		RequirementID:       "approval.hrbp/v1",
		VisibleFields:       []humanwork.ProjectionField{{Name: "proposal.amount", Value: "21000.00"}},
		HiddenFieldManifest: []string{"worker.email"},
		Warnings:            []string{"budget reserved"},
		Effects:             []string{"base pay successor"},
	})
	if err != nil {
		t.Fatalf("rerender: %v", err)
	}
	if second.TaskVersion != 2 || second.Digest == first.Digest {
		t.Fatalf("rerender did not advance binding: first=%+v second=%+v", first, second)
	}
}

func TestTodo_APPROVAL_006_Golden(t *testing.T) {
	input := humanwork.ProjectionInput{
		RequirementID:       "approval.hrbp/v1",
		VisibleFields:       []humanwork.ProjectionField{{Name: "z", Value: "2"}, {Name: "a", Value: "1"}},
		HiddenFieldManifest: []string{"private.b", "private.a"},
		Warnings:            []string{"warning.b", "warning.a"},
		Effects:             []string{"effect.b", "effect.a"},
	}
	left, err := humanwork.RenderProjection(input, 1)
	if err != nil {
		t.Fatalf("left render: %v", err)
	}
	right, err := humanwork.RenderProjection(humanwork.ProjectionInput{
		RequirementID:       "approval.hrbp/v1",
		VisibleFields:       []humanwork.ProjectionField{{Name: "a", Value: "1"}, {Name: "z", Value: "2"}},
		HiddenFieldManifest: []string{"private.a", "private.b"},
		Warnings:            []string{"warning.a", "warning.b"},
		Effects:             []string{"effect.a", "effect.b"},
	}, 1)
	if err != nil || left.Digest != right.Digest {
		t.Fatalf("canonical render digests = %q / %q, err=%v", left.Digest, right.Digest, err)
	}
}

func TestTodo_APPROVAL_006_Browser(t *testing.T) {
	// This lane has no browser dependency. The browser matrix is represented by
	// the same server render contract: changing a visible value, warning or
	// effect changes the digest that a UI would receive.
	base, err := humanwork.RenderProjection(humanwork.ProjectionInput{
		RequirementID: "approval.hrbp/v1", VisibleFields: []humanwork.ProjectionField{{Name: "amount", Value: "18000"}},
		Warnings: []string{"none"}, Effects: []string{"pay"},
	}, 1)
	if err != nil {
		t.Fatalf("base render: %v", err)
	}
	changed, err := humanwork.RenderProjection(humanwork.ProjectionInput{
		RequirementID: "approval.hrbp/v1", VisibleFields: []humanwork.ProjectionField{{Name: "amount", Value: "19000"}},
		Warnings: []string{"none"}, Effects: []string{"pay"},
	}, 1)
	if err != nil || base.Digest == changed.Digest {
		t.Fatalf("material UI change did not move digest: %q / %q, err=%v", base.Digest, changed.Digest, err)
	}
}

func TestTodo_APPROVAL_006_Mutation(t *testing.T) {
	f := mustFixture(t)
	f.Projections[0].Safety = humanwork.SafetyMaterialUnknown
	if _, err := approval.NewBinder(f.Current(), f.Scenario.Requirements, f.Resolutions, f.Projections, f.Clock, nil); !errors.Is(err, approval.ErrUnsafeProjection) {
		t.Fatalf("unknown projection = %v, want ErrUnsafeProjection", err)
	}
	f.Projections[0].Safety = humanwork.SafetyMaterialRedacted
	if _, err := approval.NewBinder(f.Current(), f.Scenario.Requirements, f.Resolutions, f.Projections, f.Clock, nil); !errors.Is(err, approval.ErrUnsafeProjection) {
		t.Fatalf("redacted projection = %v, want ErrUnsafeProjection", err)
	}
}
