package productslice

import "testing"

// TestProductSliceDefinitionCloneDoesNotAlias proves clone() returns a deep
// copy: mutating the clone's slices must never mutate the original's
// backing arrays. Every registry-style type in this codebase
// (floorplan.Floorplan, widgetreg.WidgetDefinition) makes the same
// guarantee at its own admission boundary, so a caller can never reach back
// into a stored definition through a value it was handed.
func TestProductSliceDefinitionCloneDoesNotAlias(t *testing.T) {
	original := ProductSliceDefinition{
		SliceID:         "promotion",
		Version:         1,
		BusinessIntents: []string{"hcmnext.people.promote_worker/v1"},
		Features:        []string{"promotion_execute"},
		Pages:           []string{"promotion.journeys.list"},
		Widgets:         []string{"widget.table.workforce@1"},
		Capabilities:    []string{"hcmnext.people.promote_worker"},
		Packages:        []string{"github.com/monstercameron/human-capital-management-suite/cmd/hcmnext"},
		Todos:           []string{"PROMO-001"},
		Jurisdictions:   []string{"US-ALL"},
		Personas:        []string{"manager"},
		ExitCriteria:    []string{"exit criterion"},
	}

	clone := original.clone()
	clone.BusinessIntents[0] = "mutated"
	clone.Features[0] = "mutated"
	clone.Pages[0] = "mutated"
	clone.Widgets[0] = "mutated"
	clone.Capabilities[0] = "mutated"
	clone.Packages[0] = "mutated"
	clone.Todos[0] = "mutated"
	clone.Jurisdictions[0] = "mutated"
	clone.Personas[0] = "mutated"
	clone.ExitCriteria[0] = "mutated"

	if original.BusinessIntents[0] != "hcmnext.people.promote_worker/v1" {
		t.Errorf("BusinessIntents aliased: got %q", original.BusinessIntents[0])
	}
	if original.Features[0] != "promotion_execute" {
		t.Errorf("Features aliased: got %q", original.Features[0])
	}
	if original.Pages[0] != "promotion.journeys.list" {
		t.Errorf("Pages aliased: got %q", original.Pages[0])
	}
	if original.Widgets[0] != "widget.table.workforce@1" {
		t.Errorf("Widgets aliased: got %q", original.Widgets[0])
	}
	if original.Capabilities[0] != "hcmnext.people.promote_worker" {
		t.Errorf("Capabilities aliased: got %q", original.Capabilities[0])
	}
	if original.Packages[0] != "github.com/monstercameron/human-capital-management-suite/cmd/hcmnext" {
		t.Errorf("Packages aliased: got %q", original.Packages[0])
	}
	if original.Todos[0] != "PROMO-001" {
		t.Errorf("Todos aliased: got %q", original.Todos[0])
	}
	if original.Jurisdictions[0] != "US-ALL" {
		t.Errorf("Jurisdictions aliased: got %q", original.Jurisdictions[0])
	}
	if original.Personas[0] != "manager" {
		t.Errorf("Personas aliased: got %q", original.Personas[0])
	}
	if original.ExitCriteria[0] != "exit criterion" {
		t.Errorf("ExitCriteria aliased: got %q", original.ExitCriteria[0])
	}
}
