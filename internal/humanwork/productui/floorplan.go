package productui

import "fmt"

// FloorplanDefinition is one registered floorplan contract: the
// bounded set of versioned floorplans the renderer admits, grounded
// in the frontend plan's floorplan catalog. Presentation registers
// these contracts; domain truth stays in the owning packages.
type FloorplanDefinition struct {
	ID      string
	Name    string
	Purpose string
	Version int64
}

// FloorplanCatalog is the registered floorplan set.
type FloorplanCatalog struct {
	Floorplans []FloorplanDefinition
}

// registeredFloorplans is the spec catalog at its initial versions;
// widget-version migration (a later lifecycle step) moves versions.
var registeredFloorplans = []FloorplanDefinition{
	{ID: "launch", Name: "Launch", Purpose: "Orient and expose a small number of relevant starts", Version: 1},
	{ID: "collection", Name: "Collection", Purpose: "Search, filter, compare, and select authorized resources", Version: 1},
	{ID: "object", Name: "Object", Purpose: "Understand one worker, position, case, plan, or configuration", Version: 1},
	{ID: "guided-task", Name: "Guided task", Purpose: "Complete a linear or unfamiliar participant task", Version: 1},
	{ID: "intent-workspace", Name: "Intent workspace", Purpose: "Draft, simulate, confirm, submit, track, and repair a governed change", Version: 1},
	{ID: "decision", Name: "Decision", Purpose: "Review proposal-bound evidence and make an assigned decision", Version: 1},
	{ID: "monitor", Name: "Monitor", Purpose: "Track workflow, batch, connector, payroll, or reconciliation progress", Version: 1},
	{ID: "expert-workbench", Name: "Expert workbench", Purpose: "Diagnose and resolve complex exceptions with dense evidence", Version: 1},
	{ID: "analysis", Name: "Analysis", Purpose: "Inspect governed metrics, lineage, uncertainty, and proposed follow-up", Version: 1},
	{ID: "composition-studio", Name: "Composition studio", Purpose: "Author, preview, validate, review, and publish a customer page", Version: 1},
}

// registeredPrimitives is the spec semantic-primitive set page
// definitions may compose. Per-floorplan subsets are not specified,
// so any registered primitive validates on any registered floorplan.
var registeredPrimitives = []string{"stack", "section", "grid", "split", "tabs", "summary_rail", "task_panel", "table", "timeline", "comparison", "disclosure"}

// RegisteredFloorplans returns the floorplan catalog. The result is a
// fresh copy on every call so callers cannot mutate the contract.
func RegisteredFloorplans() FloorplanCatalog {
	floorplans := make([]FloorplanDefinition, len(registeredFloorplans))
	copy(floorplans, registeredFloorplans)
	return FloorplanCatalog{Floorplans: floorplans}
}

// RegisteredPrimitives returns the semantic-primitive set, freshly
// copied on every call.
func RegisteredPrimitives() []string {
	primitives := make([]string, len(registeredPrimitives))
	copy(primitives, registeredPrimitives)
	return primitives
}

// PageComposition is the draft-scoped composition document: which
// floorplan version the draft targets, which semantic primitives it
// composes, which anatomy regions it fills in resolution order,
// which typed widget bindings it carries, and which semantic action
// bindings it wires.
type PageComposition struct {
	Floorplan        string
	FloorplanVersion int64
	Primitives       []string
	Regions          []string
	Widgets          []WidgetBinding
	Actions          []ActionBinding
}

// FloorplanVerdict is the compatibility answer: compatible plus the
// stable reasons, in validation order, when not. Reasons stay nil on
// success.
type FloorplanVerdict struct {
	Compatible bool
	Reasons    []string
}

// ValidateFloorplanCompatibility checks one composition against the
// catalog: the floorplan must be registered, its version positive and
// supported, and every composed primitive registered. Violations
// accumulate in fixed order so authors see everything at once.
func ValidateFloorplanCompatibility(composition PageComposition, catalog FloorplanCatalog) FloorplanVerdict {
	known := make(map[string]int64, len(catalog.Floorplans))
	for _, floorplan := range catalog.Floorplans {
		known[floorplan.ID] = floorplan.Version
	}
	primitives := make(map[string]bool, len(registeredPrimitives))
	for _, primitive := range registeredPrimitives {
		primitives[primitive] = true
	}
	var reasons []string
	current, ok := known[composition.Floorplan]
	if !ok {
		reasons = append(reasons, fmt.Sprintf("unknown floorplan %q", composition.Floorplan))
	} else if composition.FloorplanVersion <= 0 || composition.FloorplanVersion > current {
		reasons = append(reasons, fmt.Sprintf("unsupported floorplan version %d for %q", composition.FloorplanVersion, composition.Floorplan))
	}
	seen := make(map[string]bool, len(composition.Primitives))
	for _, primitive := range composition.Primitives {
		if seen[primitive] {
			continue
		}
		seen[primitive] = true
		if !primitives[primitive] {
			reasons = append(reasons, fmt.Sprintf("unknown primitive %q", primitive))
		}
	}
	if len(reasons) > 0 {
		return FloorplanVerdict{Compatible: false, Reasons: reasons}
	}
	return FloorplanVerdict{Compatible: true}
}

// ValidateDraftFloorplan validates one draft through its composition.
func ValidateDraftFloorplan(draft PageDraft, catalog FloorplanCatalog) FloorplanVerdict {
	return ValidateFloorplanCompatibility(draft.Composition, catalog)
}
