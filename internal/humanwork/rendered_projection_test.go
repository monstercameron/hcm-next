package humanwork

import "testing"

func TestRenderedProjectionStore(t *testing.T) {
	store := NewProjectionStore()
	first, err := store.Render("task-1", ProjectionInput{
		RequirementID:       "req-1",
		VisibleFields:       []ProjectionField{{Name: "amount", Value: "18000.00"}},
		HiddenFieldManifest: []string{"worker.email"},
		Warnings:            []string{"budget is reserved"},
		Effects:             []string{"base pay changes"},
	})
	if err != nil {
		t.Fatalf("first render: %v", err)
	}
	if first.TaskVersion != 1 || first.Safety != SafetySafeToDecide || first.Digest == "" {
		t.Fatalf("first projection = %+v", first)
	}
	second, err := store.Render("task-1", ProjectionInput{
		RequirementID: "req-1", VisibleFields: []ProjectionField{{Name: "amount", Value: "19000.00"}},
	})
	if err != nil {
		t.Fatalf("second render: %v", err)
	}
	if second.TaskVersion != 2 || second.Digest == first.Digest {
		t.Fatalf("re-render did not advance binding: first=%+v second=%+v", first, second)
	}
	unsafe, err := store.Render("task-1", ProjectionInput{
		RequirementID: "req-1", UnknownFields: []string{"budget.available"},
	})
	if err != nil {
		t.Fatalf("unsafe render: %v", err)
	}
	if unsafe.TaskVersion != 3 || unsafe.Safety != SafetyMaterialUnknown {
		t.Fatalf("unsafe projection = %+v", unsafe)
	}
	got, ok := store.Current("task-1")
	if !ok || got.TaskVersion != unsafe.TaskVersion || got.Digest != unsafe.Digest {
		t.Fatalf("current = %+v, ok=%t", got, ok)
	}
	got.VisibleFields = append(got.VisibleFields, ProjectionField{Name: "mutated"})
	again, _ := store.Current("task-1")
	if len(again.VisibleFields) != len(got.VisibleFields)-1 {
		t.Fatal("Current did not return defensive copies")
	}
}
