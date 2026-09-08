package productui

import (
	"fmt"
	"reflect"
	"sort"
	"strings"
)

// configurationScopeRanks is the page-configuration scope chain,
// broad to specific, from the platform catalog's configuration
// precedence: a company or legal-entity override beats an
// enterprise-group default, which beats a tenant default, which
// beats the platform default. Presentation pins this order so
// layered page configuration resolves deterministically; no second
// authority is invented here. Unlisted scopes never rank.
var configurationScopeRanks = map[string]int{
	"platform":         1,
	"tenant":           2,
	"enterprise-group": 3,
	"company":          4,
}

// ConfigurationScopeRank reports one scope's precedence rank. The
// second result is false for unlisted scopes, which resolve
// nothing.
func ConfigurationScopeRank(scope string) (int, bool) {
	rank, ok := configurationScopeRanks[scope]
	return rank, ok
}

// PageConfigurationSource is one scoped configuration layer: the
// scope that declares it, that layer's policy version (kept for
// resolution lineage), and the composition values it declares.
// Zero fields declare nothing.
type PageConfigurationSource struct {
	Scope       string
	Version     int64
	Composition PageComposition
}

// ConfigurationWinner names the winning scope per merged field —
// the lineage the catalog requires every resolution to identify.
type ConfigurationWinner struct {
	Field   string
	Scope   string
	Version int64
}

// ConfigurationResolution is the precedence answer: the merged
// composition, the consulted scopes broad to specific, the winning
// scope per decided field, overridden declarations, denied ceiling
// weakenings, and the compatibility verdict with stable reasons.
// Callers must not publish incompatible resolutions.
type ConfigurationResolution struct {
	Composition PageComposition
	Sources     []string
	Winners     []ConfigurationWinner
	Overrides   []string
	Denied      []string
	Compatible  bool
	Reasons     []string
}

// ResolvePageConfiguration merges layered page configuration into
// one composition. Configuration resolves toward the most specific
// valid scope: each field takes the most specific non-zero
// declaration (collections take the most specific non-empty set,
// never a cross-scope merge), and broader declarations carrying a
// different value are recorded as overrides. The classification
// ceiling is security, not configuration, so it accumulates instead:
// the effective ceiling is the most restrictive ranked declaration,
// a narrower scope may tighten it but never weaken it, and each
// refused weakening is recorded as a denial naming the surviving
// restriction. Unknown scopes, duplicate scopes, and unranked
// ceiling labels fail closed: the offending input is excluded and
// the resolution is incompatible.
func ResolvePageConfiguration(sources []PageConfigurationSource) ConfigurationResolution {
	ordered := append([]PageConfigurationSource(nil), sources...)
	sort.SliceStable(ordered, func(i, j int) bool {
		first, _ := ConfigurationScopeRank(ordered[i].Scope)
		second, _ := ConfigurationScopeRank(ordered[j].Scope)
		return first < second
	})
	var resolution ConfigurationResolution
	seen := map[string]bool{}
	var layers []PageConfigurationSource
	for _, source := range ordered {
		rank, ok := ConfigurationScopeRank(source.Scope)
		if !ok || rank == 0 {
			resolution.Reasons = append(resolution.Reasons, fmt.Sprintf("unknown configuration scope %q", source.Scope))
			continue
		}
		if seen[source.Scope] {
			resolution.Reasons = append(resolution.Reasons, fmt.Sprintf("duplicate configuration scope %q", source.Scope))
			continue
		}
		seen[source.Scope] = true
		layers = append(layers, source)
		resolution.Sources = append(resolution.Sources, source.Scope)
	}
	// Most-specific-wins configuration fields, in PageComposition
	// field order. Broader declarations carrying a different value
	// are overrides; restatements stay silent.
	win := func(field string, declared func(PageComposition) bool, same func(a, b PageComposition) bool, apply func(*PageComposition, PageComposition)) {
		winner := -1
		for i, layer := range layers {
			if declared(layer.Composition) {
				winner = i
			}
		}
		if winner < 0 {
			return
		}
		apply(&resolution.Composition, layers[winner].Composition)
		resolution.Winners = append(resolution.Winners, ConfigurationWinner{Field: field, Scope: layers[winner].Scope, Version: layers[winner].Version})
		for _, layer := range layers[:winner] {
			if declared(layer.Composition) && !same(layer.Composition, layers[winner].Composition) {
				resolution.Overrides = append(resolution.Overrides, fmt.Sprintf("%s from %s overridden by %s", field, layer.Scope, layers[winner].Scope))
			}
		}
	}
	win("floorplan",
		func(composition PageComposition) bool { return composition.Floorplan != "" },
		func(a, b PageComposition) bool { return a.Floorplan == b.Floorplan },
		func(into *PageComposition, from PageComposition) { into.Floorplan = from.Floorplan })
	win("floorplan_version",
		func(composition PageComposition) bool { return composition.FloorplanVersion > 0 },
		func(a, b PageComposition) bool { return a.FloorplanVersion == b.FloorplanVersion },
		func(into *PageComposition, from PageComposition) { into.FloorplanVersion = from.FloorplanVersion })
	win("primitives",
		func(composition PageComposition) bool { return len(composition.Primitives) > 0 },
		func(a, b PageComposition) bool { return reflect.DeepEqual(a.Primitives, b.Primitives) },
		func(into *PageComposition, from PageComposition) {
			into.Primitives = append([]string(nil), from.Primitives...)
		})
	win("regions",
		func(composition PageComposition) bool { return len(composition.Regions) > 0 },
		func(a, b PageComposition) bool { return reflect.DeepEqual(a.Regions, b.Regions) },
		func(into *PageComposition, from PageComposition) {
			into.Regions = append([]string(nil), from.Regions...)
		})
	win("widgets",
		func(composition PageComposition) bool { return len(composition.Widgets) > 0 },
		func(a, b PageComposition) bool { return reflect.DeepEqual(a.Widgets, b.Widgets) },
		func(into *PageComposition, from PageComposition) {
			into.Widgets = append([]WidgetBinding(nil), from.Widgets...)
		})
	win("actions",
		func(composition PageComposition) bool { return len(composition.Actions) > 0 },
		func(a, b PageComposition) bool { return reflect.DeepEqual(a.Actions, b.Actions) },
		func(into *PageComposition, from PageComposition) {
			into.Actions = append([]ActionBinding(nil), from.Actions...)
		})

	// Accumulated security: the ceiling only ever tightens. Layers
	// run broad to specific; a declaration above the surviving
	// restriction is denied, a tighter one overrides its holder, an
	// equal one restates it silently.
	holder := -1
	holderRank := 0
	for i, layer := range layers {
		ceiling := layer.Composition.ClassificationCeiling
		if ceiling == "" {
			continue
		}
		rank, ok := ClassificationRank(ceiling)
		if !ok {
			resolution.Reasons = append(resolution.Reasons, fmt.Sprintf("unranked classification ceiling %q at scope %q", ceiling, layer.Scope))
			continue
		}
		switch {
		case holder < 0 || rank < holderRank:
			if holder >= 0 {
				resolution.Overrides = append(resolution.Overrides, fmt.Sprintf("classification_ceiling from %s overridden by %s", layers[holder].Scope, layer.Scope))
			}
			holder, holderRank = i, rank
		case rank > holderRank:
			resolution.Denied = append(resolution.Denied, fmt.Sprintf("ceiling %q from %s denied by %s %q",
				ceiling, layer.Scope, layers[holder].Scope, layers[holder].Composition.ClassificationCeiling))
		default:
			holder = i
		}
	}
	if holder >= 0 {
		resolution.Composition.ClassificationCeiling = layers[holder].Composition.ClassificationCeiling
		resolution.Winners = append(resolution.Winners, ConfigurationWinner{Field: "classification_ceiling", Scope: layers[holder].Scope, Version: layers[holder].Version})
		// The ceiling winner belongs in field order: it resolves
		// after floorplan_version and before primitives.
		sort.SliceStable(resolution.Winners, func(i, j int) bool {
			return configurationFieldOrder(resolution.Winners[i].Field) < configurationFieldOrder(resolution.Winners[j].Field)
		})
	}
	// Overrides from the ceiling layer belong in field order too.
	sort.SliceStable(resolution.Overrides, func(i, j int) bool {
		return configurationFieldOrder(overrideField(resolution.Overrides[i])) < configurationFieldOrder(overrideField(resolution.Overrides[j]))
	})
	resolution.Compatible = len(resolution.Reasons) == 0
	return resolution
}

// configurationFieldOrder is the PageComposition field order shared
// by winners and overrides.
func configurationFieldOrder(field string) int {
	for order, name := range []string{"floorplan", "floorplan_version", "classification_ceiling", "primitives", "regions", "widgets", "actions"} {
		if name == field {
			return order
		}
	}
	return len(field)
}

// overrideField recovers the field name from an override entry,
// which always starts with "<field> from ".
func overrideField(entry string) string {
	for _, name := range []string{"floorplan", "floorplan_version", "classification_ceiling", "primitives", "regions", "widgets", "actions"} {
		if strings.HasPrefix(entry, name+" from ") {
			return name
		}
	}
	return entry
}
