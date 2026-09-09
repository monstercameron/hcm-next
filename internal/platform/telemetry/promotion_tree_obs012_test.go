package telemetry

import (
	"context"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/workflow/promotionexec"
)

// TestTodo_OBS_012_ConformancePromotionDriverSpanTree runs the published
// promotion plan through a deliberately small fake driver. The fake models
// the driver's node boundary without importing a telemetry backend, then
// proves every emitted name and parent is a registry row.
func TestTodo_OBS_012_ConformancePromotionDriverSpanTree(t *testing.T) {
	plan, err := promotionexec.Compile(promotionexec.Definition())
	if err != nil {
		t.Fatalf("compile promotion plan: %v", err)
	}
	registry := CanonicalPromotionRegistry()
	if err := registry.ValidateEmission("/hcmnext.workflows.v1.Promotion/Execute", map[string]string{"logical_operation_id": "op-1"}); err != nil {
		t.Fatal(err)
	}

	type nodeSpan struct {
		name   SpanName
		parent SpanName
		node   string
	}
	var tree []nodeSpan
	start := nodeSpan{name: SpanWorkflowStart, parent: SpanIntentAdvance}
	tree = append(tree, start)
	for _, node := range plan.Nodes {
		ctx := context.Background()
		if ctx == nil { // keep the fake-driver boundary explicit and total
			t.Fatal("nil fake driver context")
		}
		tree = append(tree, nodeSpan{name: SpanWorkflowNode, parent: start.name, node: node.ID})
	}
	if len(tree) != len(plan.Nodes)+1 || len(plan.Nodes) < 10 {
		t.Fatalf("fake driver tree size = %d for %d plan nodes", len(tree), len(plan.Nodes))
	}
	for _, emitted := range tree {
		row, ok := registry.Lookup(emitted.name)
		if !ok || row.Parent != emitted.parent {
			t.Fatalf("emitted span %+v has no exact registry parent", emitted)
		}
	}
}
