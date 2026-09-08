package productui

import (
	"fmt"
	"reflect"
)

// Composition diff fields in declaration order.
const (
	DiffFieldPurpose          = "purpose"
	DiffFieldAudience         = "audience"
	DiffFieldFloorplan        = "floorplan"
	DiffFieldFloorplanVersion = "floorplan_version"
	DiffFieldCeiling          = "classification_ceiling"
	DiffFieldPrimitives       = "primitives"
	DiffFieldRegions          = "regions"
	DiffFieldWidgets          = "widgets"
	DiffFieldActions          = "actions"
)

// Composition diff kinds.
const (
	DiffKindChanged = "changed"
	DiffKindAdded   = "added"
	DiffKindRemoved = "removed"
	DiffKindMoved   = "moved"
)

// CompositionDiff is one semantic change between two
// compositions: the field, the kind, and a stable detail. Empty
// diff means identical.
type CompositionDiff struct {
	Field  string
	Kind   string
	Detail string
}

// DiffCompositions diffs two compositions semantically, in field
// declaration order. Scalars report old → new; the ceiling change
// names tightening or loosening when both sides rank. List
// members report removed in old order, added in new order, then
// moved with from → to indices — occurrences pair by count, so
// duplicates match positionally. Bindings pair by widget type or
// capability: version moves arrow, token granted/withdrawn
// reports availability changes, anything else notes differing
// details. Token values and idempotency keys rotate silently:
// re-proving never re-means. Review reads the diff; the
// validators still gate.
func DiffCompositions(old, next PageComposition) []CompositionDiff {
	var diff []CompositionDiff
	changed := func(field, detail string) {
		diff = append(diff, CompositionDiff{Field: field, Kind: DiffKindChanged, Detail: detail})
	}
	if old.Purpose != next.Purpose {
		changed(DiffFieldPurpose, fmt.Sprintf("%q → %q", old.Purpose, next.Purpose))
	}
	if old.Audience != next.Audience {
		changed(DiffFieldAudience, fmt.Sprintf("%q → %q", old.Audience, next.Audience))
	}
	if old.Floorplan != next.Floorplan {
		changed(DiffFieldFloorplan, fmt.Sprintf("%q → %q", old.Floorplan, next.Floorplan))
	}
	if old.FloorplanVersion != next.FloorplanVersion {
		changed(DiffFieldFloorplanVersion, fmt.Sprintf("%d → %d", old.FloorplanVersion, next.FloorplanVersion))
	}
	if old.ClassificationCeiling != next.ClassificationCeiling {
		detail := fmt.Sprintf("%q → %q", old.ClassificationCeiling, next.ClassificationCeiling)
		if oldRank, ok := ClassificationRank(old.ClassificationCeiling); ok {
			if newRank, ok := ClassificationRank(next.ClassificationCeiling); ok {
				if newRank < oldRank {
					detail += " (tightened)"
				} else {
					detail += " (loosened)"
				}
			}
		}
		changed(DiffFieldCeiling, detail)
	}
	diff = append(diff, diffStringList(DiffFieldPrimitives, old.Primitives, next.Primitives)...)
	diff = append(diff, diffStringList(DiffFieldRegions, old.Regions, next.Regions)...)
	diff = append(diff, diffWidgetBindings(old.Widgets, next.Widgets)...)
	diff = append(diff, diffActionBindings(old.Actions, next.Actions)...)
	return diff
}

// diffStringList diffs one ordered string list: removed in old
// order, added in new order, moved in old order with from → to
// indices. Occurrences pair by count — surplus old members
// remove, surplus new members add, paired occurrences at
// different positions move.
func diffStringList(field string, old, next []string) []CompositionDiff {
	oldPositions := map[string][]int{}
	for i, value := range old {
		oldPositions[value] = append(oldPositions[value], i)
	}
	newPositions := map[string][]int{}
	for i, value := range next {
		newPositions[value] = append(newPositions[value], i)
	}
	var diff []CompositionDiff
	seen := map[string]bool{}
	for _, value := range old {
		if seen[value] {
			continue
		}
		seen[value] = true
		olds, news := oldPositions[value], newPositions[value]
		for k := len(news); k < len(olds); k++ {
			diff = append(diff, CompositionDiff{Field: field, Kind: DiffKindRemoved, Detail: fmt.Sprintf("%q", value)})
		}
	}
	seen = map[string]bool{}
	for _, value := range next {
		if seen[value] {
			continue
		}
		seen[value] = true
		olds, news := oldPositions[value], newPositions[value]
		for k := len(olds); k < len(news); k++ {
			diff = append(diff, CompositionDiff{Field: field, Kind: DiffKindAdded, Detail: fmt.Sprintf("%q", value)})
		}
	}
	seen = map[string]bool{}
	for _, value := range old {
		if seen[value] {
			continue
		}
		seen[value] = true
		olds, news := oldPositions[value], newPositions[value]
		for k := 0; k < len(olds) && k < len(news); k++ {
			if olds[k] != news[k] {
				diff = append(diff, CompositionDiff{Field: field, Kind: DiffKindMoved,
					Detail: fmt.Sprintf("%q %d → %d", value, olds[k], news[k])})
			}
		}
	}
	return diff
}

// diffWidgetBindings pairs bindings by widget type. Deep-equal
// pairs stay silent; type pairs change with a version arrow or a
// differing-details note. Emission order is fixed: removed in
// old order, added in new order, changed in old order.
func diffWidgetBindings(old, next []WidgetBinding) []CompositionDiff {
	used := make([]bool, len(next))
	var removed []WidgetBinding
	for _, oldBinding := range old {
		matched := -1
		for j, newBinding := range next {
			if !used[j] && reflect.DeepEqual(oldBinding, newBinding) {
				matched = j
				break
			}
		}
		if matched < 0 {
			removed = append(removed, oldBinding)
			continue
		}
		used[matched] = true
	}
	var added []WidgetBinding
	for j, newBinding := range next {
		if !used[j] {
			added = append(added, newBinding)
		}
	}
	leftover := append([]WidgetBinding(nil), added...)
	var changed []CompositionDiff
	var gone []WidgetBinding
	for _, oldBinding := range removed {
		paired := -1
		for j, newBinding := range leftover {
			if newBinding.WidgetType == oldBinding.WidgetType {
				paired = j
				break
			}
		}
		if paired < 0 {
			gone = append(gone, oldBinding)
			continue
		}
		newBinding := leftover[paired]
		leftover = append(leftover[:paired], leftover[paired+1:]...)
		detail := fmt.Sprintf("%q details differ", oldBinding.WidgetType)
		if oldBinding.WidgetVersion != newBinding.WidgetVersion {
			detail = fmt.Sprintf("%q %d → %d", oldBinding.WidgetType, oldBinding.WidgetVersion, newBinding.WidgetVersion)
		}
		changed = append(changed, CompositionDiff{Field: DiffFieldWidgets, Kind: DiffKindChanged, Detail: detail})
	}
	var diff []CompositionDiff
	for _, oldBinding := range gone {
		diff = append(diff, CompositionDiff{Field: DiffFieldWidgets, Kind: DiffKindRemoved,
			Detail: fmt.Sprintf("%q version %d", oldBinding.WidgetType, oldBinding.WidgetVersion)})
	}
	for _, newBinding := range leftover {
		diff = append(diff, CompositionDiff{Field: DiffFieldWidgets, Kind: DiffKindAdded,
			Detail: fmt.Sprintf("%q version %d", newBinding.WidgetType, newBinding.WidgetVersion)})
	}
	return append(diff, changed...)
}

// actionBindingSame reports rotation-insensitive equality: every
// field but token values and idempotency keys, with token
// presence significant (a granted or withdrawn token re-means).
func actionBindingSame(a, b ActionBinding) bool {
	if (a.Token == "") != (b.Token == "") {
		return false
	}
	a.Token, b.Token = "", ""
	a.IdempotencyKey, b.IdempotencyKey = "", ""
	return reflect.DeepEqual(a, b)
}

// diffActionBindings pairs bindings by capability with rotation
// silence. Changed details prioritize version moves, then input
// moves, then token granted/withdrawn, then differing details.
// Emission order matches widgets: removed, added, changed.
func diffActionBindings(old, next []ActionBinding) []CompositionDiff {
	used := make([]bool, len(next))
	var removed []ActionBinding
	for _, oldBinding := range old {
		matched := -1
		for j, newBinding := range next {
			if !used[j] && actionBindingSame(oldBinding, newBinding) {
				matched = j
				break
			}
		}
		if matched < 0 {
			removed = append(removed, oldBinding)
			continue
		}
		used[matched] = true
	}
	var added []ActionBinding
	for j, newBinding := range next {
		if !used[j] {
			added = append(added, newBinding)
		}
	}
	leftover := append([]ActionBinding(nil), added...)
	var changed []CompositionDiff
	var gone []ActionBinding
	for _, oldBinding := range removed {
		paired := -1
		for j, newBinding := range leftover {
			if newBinding.Capability == oldBinding.Capability {
				paired = j
				break
			}
		}
		if paired < 0 {
			gone = append(gone, oldBinding)
			continue
		}
		newBinding := leftover[paired]
		leftover = append(leftover[:paired], leftover[paired+1:]...)
		detail := fmt.Sprintf("%q details differ", oldBinding.Capability)
		switch {
		case oldBinding.ExpectedVersion != newBinding.ExpectedVersion:
			detail = fmt.Sprintf("%q %d → %d", oldBinding.Capability, oldBinding.ExpectedVersion, newBinding.ExpectedVersion)
		case oldBinding.InputType != newBinding.InputType:
			detail = fmt.Sprintf("%q input %q → %q", oldBinding.Capability, oldBinding.InputType, newBinding.InputType)
		case oldBinding.Token == "" && newBinding.Token != "":
			detail = fmt.Sprintf("action %q token granted", oldBinding.Capability)
		case oldBinding.Token != "" && newBinding.Token == "":
			detail = fmt.Sprintf("action %q token withdrawn", oldBinding.Capability)
		}
		changed = append(changed, CompositionDiff{Field: DiffFieldActions, Kind: DiffKindChanged, Detail: detail})
	}
	var diff []CompositionDiff
	for _, oldBinding := range gone {
		diff = append(diff, CompositionDiff{Field: DiffFieldActions, Kind: DiffKindRemoved,
			Detail: fmt.Sprintf("%q version %d", oldBinding.Capability, oldBinding.ExpectedVersion)})
	}
	for _, newBinding := range leftover {
		diff = append(diff, CompositionDiff{Field: DiffFieldActions, Kind: DiffKindAdded,
			Detail: fmt.Sprintf("%q version %d", newBinding.Capability, newBinding.ExpectedVersion)})
	}
	return append(diff, changed...)
}
