package productui

import "fmt"

// Governed outline edit kinds: the semantic outline editor adds
// and removes primitives and regions. Moves stay out — reordering
// is the keyboard-reorder step — and every kind outside this set
// refuses.
const (
	OutlineAddPrimitive    = "add-primitive"
	OutlineRemovePrimitive = "remove-primitive"
	OutlineAddRegion       = "add-region"
	OutlineRemoveRegion    = "remove-region"
)

// OutlineEdit is one governed outline edit: the kind and the
// primitive or region it names.
type OutlineEdit struct {
	Kind  string
	Value string
}

// OutlineVerdict is the editor answer: compatible plus the
// resulting composition — always freshly copied, never aliasing
// the input — and the stable reasons, in edit order, when not. A
// refused batch returns the input outline unchanged.
type OutlineVerdict struct {
	Compatible  bool
	Reasons     []string
	Composition PageComposition
}

// ApplyOutlineEdits applies one batch of outline edits to a
// composition's primitives and regions. Adds check the registered
// vocabulary (platform-owned regions refuse with the validator's
// wording); removes are pure subtraction, so convergent edits —
// adding present, removing absent — succeed silently and batches
// replay idempotently. The batch is atomic: any vocabulary or
// kind violation voids every edit. Ordering is not the editor's
// concern: adds append, and region order validates downstream.
// Every other composition field copies through untouched.
func ApplyOutlineEdits(composition PageComposition, edits []OutlineEdit) OutlineVerdict {
	primitives := make(map[string]bool, len(registeredPrimitives))
	for _, primitive := range registeredPrimitives {
		primitives[primitive] = true
	}
	ranks := make(map[string]int, len(registeredRegions))
	for _, region := range registeredRegions {
		ranks[region.ID] = region.Rank
	}
	working := PageComposition{
		Purpose:               composition.Purpose,
		Audience:              composition.Audience,
		Floorplan:             composition.Floorplan,
		FloorplanVersion:      composition.FloorplanVersion,
		ClassificationCeiling: composition.ClassificationCeiling,
		Primitives:            append([]string(nil), composition.Primitives...),
		Regions:               append([]string(nil), composition.Regions...),
		Widgets:               append([]WidgetBinding(nil), composition.Widgets...),
		Actions:               append([]ActionBinding(nil), composition.Actions...),
	}
	var reasons []string
	for _, edit := range edits {
		switch edit.Kind {
		case OutlineAddPrimitive:
			if !primitives[edit.Value] {
				reasons = append(reasons, fmt.Sprintf("unknown primitive %q", edit.Value))
			} else if !containsString(working.Primitives, edit.Value) {
				working.Primitives = append(working.Primitives, edit.Value)
			}
		case OutlineRemovePrimitive:
			working.Primitives = removeString(working.Primitives, edit.Value)
		case OutlineAddRegion:
			if _, ok := ranks[edit.Value]; !ok {
				if platformOwnedRegions[edit.Value] {
					reasons = append(reasons, fmt.Sprintf("platform-owned region %q", edit.Value))
				} else {
					reasons = append(reasons, fmt.Sprintf("unknown region %q", edit.Value))
				}
			} else if !containsString(working.Regions, edit.Value) {
				working.Regions = append(working.Regions, edit.Value)
			}
		case OutlineRemoveRegion:
			working.Regions = removeString(working.Regions, edit.Value)
		default:
			reasons = append(reasons, fmt.Sprintf("unknown outline edit kind %q", edit.Kind))
		}
	}
	if len(reasons) > 0 {
		unaltered := PageComposition{
			Purpose:               composition.Purpose,
			Audience:              composition.Audience,
			Floorplan:             composition.Floorplan,
			FloorplanVersion:      composition.FloorplanVersion,
			ClassificationCeiling: composition.ClassificationCeiling,
			Primitives:            append([]string(nil), composition.Primitives...),
			Regions:               append([]string(nil), composition.Regions...),
			Widgets:               append([]WidgetBinding(nil), composition.Widgets...),
			Actions:               append([]ActionBinding(nil), composition.Actions...),
		}
		return OutlineVerdict{Compatible: false, Reasons: reasons, Composition: unaltered}
	}
	return OutlineVerdict{Compatible: true, Composition: working}
}

// containsString reports membership.
func containsString(set []string, value string) bool {
	for _, item := range set {
		if item == value {
			return true
		}
	}
	return false
}

// removeString drops every occurrence, preserving order. Empty
// input stays nil; output never aliases the input.
func removeString(set []string, value string) []string {
	var kept []string
	for _, item := range set {
		if item != value {
			kept = append(kept, item)
		}
	}
	return kept
}
