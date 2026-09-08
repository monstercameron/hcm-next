package productui

import "fmt"

// WidgetMigrationStep is one moved binding's lineage: which widget
// moved and across which versions. Steps follow composition order.
type WidgetMigrationStep struct {
	WidgetType  string
	FromVersion int64
	ToVersion   int64
}

// WidgetMigration is the migration answer: the migrated composition
// (a copy — the input is never mutated), the per-binding lineage
// for every moved binding, and the compatibility verdict with
// stable reasons. Callers must not publish incompatible
// migrations.
type WidgetMigration struct {
	Composition PageComposition
	Migrated    []WidgetMigrationStep
	Compatible  bool
	Reasons     []string
}

// MigrateCompositionWidgets moves one composition's widget bindings
// to the registry's pinned versions through the versioned
// compatibility rule: a binding behind its widget's registered
// version advances to it; a binding already current stays
// untouched. Only the version travels — classification, source,
// authority, and every non-widget field survive identical, and the
// output never aliases the input's slices. Bindings naming unknown
// widgets or versions outside the registry fail closed with the
// registry's own unsupported-version wording, and the offending
// binding keeps its version in the returned composition.
// Cross-widget replacement awaits pinned replacement behavior,
// which the registry does not declare yet.
func MigrateCompositionWidgets(composition PageComposition, registry WidgetRegistry) WidgetMigration {
	known := make(map[string]WidgetDefinition, len(registry.Widgets))
	for _, widget := range registry.Widgets {
		known[widget.ID] = widget
	}
	migrated := PageComposition{
		Floorplan:             composition.Floorplan,
		FloorplanVersion:      composition.FloorplanVersion,
		ClassificationCeiling: composition.ClassificationCeiling,
		Primitives:            append([]string(nil), composition.Primitives...),
		Regions:               append([]string(nil), composition.Regions...),
		Widgets:               append([]WidgetBinding(nil), composition.Widgets...),
		Actions:               append([]ActionBinding(nil), composition.Actions...),
	}
	var result WidgetMigration
	for i, binding := range composition.Widgets {
		definition, ok := known[binding.WidgetType]
		if !ok {
			result.Reasons = append(result.Reasons, fmt.Sprintf("unknown widget %q", binding.WidgetType))
			continue
		}
		switch {
		case binding.WidgetVersion <= 0 || binding.WidgetVersion > definition.Version:
			result.Reasons = append(result.Reasons, fmt.Sprintf("unsupported widget version %d for %q", binding.WidgetVersion, binding.WidgetType))
		case binding.WidgetVersion < definition.Version:
			migrated.Widgets[i].WidgetVersion = definition.Version
			result.Migrated = append(result.Migrated, WidgetMigrationStep{WidgetType: binding.WidgetType, FromVersion: binding.WidgetVersion, ToVersion: definition.Version})
		}
	}
	result.Composition = migrated
	result.Compatible = len(result.Reasons) == 0
	return result
}
