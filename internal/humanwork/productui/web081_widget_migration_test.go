package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"reflect"
	"strings"
	"testing"
)

// RED for WEB-081: widget-version migration. Compositions bind
// widget versions, the registry pins current versions, and
// validation refuses anything newer — but nothing moves a
// composition authored against an older registry forward: the only
// path today is hand-editing versions with no evidence. The
// lifecycle needs a pure migration through versioned compatibility
// rules — same-widget older-to-current moves with per-binding
// lineage — failing closed on unknown widgets and versions outside
// the registry. Replacement behavior stays out: the registry pins
// no replacements, so cross-widget moves await that contract.
func TestTodo_WEB_081(t *testing.T) {
	registry := RegisteredWidgets()
	for i := range registry.Widgets {
		registry.Widgets[i].Version = 3
	}
	composition := PageComposition{
		Floorplan: "collection", ClassificationCeiling: "internal",
		Widgets: []WidgetBinding{
			{WidgetType: "metric-display", WidgetVersion: 1, AuthorityClass: "canonical", Classification: "internal", SourceType: "projection", SourceID: "journeys"},
			{WidgetType: "proposal-form", WidgetVersion: 3, AuthorityClass: "manager", Classification: "confidential", SourceType: "workflow", SourceID: "intent-1"},
		},
	}
	migrated := MigrateCompositionWidgets(composition, registry)
	if !migrated.Compatible {
		t.Fatalf("migration refuses: %q", migrated.Reasons)
	}
	if len(migrated.Migrated) != 1 || migrated.Migrated[0] != (WidgetMigrationStep{WidgetType: "metric-display", FromVersion: 1, ToVersion: 3}) {
		t.Fatalf("migration lineage = %+v", migrated.Migrated)
	}
	if migrated.Composition.Widgets[0].WidgetVersion != 3 || migrated.Composition.Widgets[1].WidgetVersion != 3 {
		t.Fatalf("migrated versions = %d, %d", migrated.Composition.Widgets[0].WidgetVersion, migrated.Composition.Widgets[1].WidgetVersion)
	}
	if migrated.Composition.Widgets[0].Classification != "internal" || migrated.Composition.Floorplan != "collection" {
		t.Fatalf("migration rewrote non-version fields: %+v", migrated.Composition)
	}

	// Fail-closed inputs: unknown widgets, versionless bindings, and
	// bindings newer than the registry refuse with stable reasons.
	for _, bad := range []struct {
		name    string
		widgets []WidgetBinding
		reason  string
	}{
		{"unknown widget", []WidgetBinding{{WidgetType: "teleporter", WidgetVersion: 1}}, `unknown widget "teleporter"`},
		{"versionless binding", []WidgetBinding{{WidgetType: "metric-display"}}, `unsupported widget version 0 for "metric-display"`},
		{"future binding", []WidgetBinding{{WidgetType: "metric-display", WidgetVersion: 9}}, `unsupported widget version 9 for "metric-display"`},
	} {
		result := MigrateCompositionWidgets(PageComposition{Widgets: bad.widgets}, RegisteredWidgets())
		if result.Compatible {
			t.Fatalf("%s migrates", bad.name)
		}
		found := false
		for _, reason := range result.Reasons {
			if reason == bad.reason {
				found = true
			}
		}
		if !found {
			t.Fatalf("%s reasons = %q, want %q", bad.name, result.Reasons, bad.reason)
		}
	}
}

// Golden: migration outcomes over a binding matrix against a
// bumped registry.
func TestTodo_WEB_081_Golden(t *testing.T) {
	registry := RegisteredWidgets()
	for i := range registry.Widgets {
		registry.Widgets[i].Version = 2
	}
	bindings := []WidgetBinding{
		{WidgetType: "metric-display", WidgetVersion: 1, AuthorityClass: "canonical", Classification: "internal", SourceType: "projection", SourceID: "journeys"},
		{WidgetType: "proposal-form", WidgetVersion: 2, AuthorityClass: "manager", Classification: "confidential", SourceType: "workflow", SourceID: "intent-1"},
		{WidgetType: "external-frame", WidgetVersion: 5, AuthorityClass: "external", Classification: "public", SourceType: "embed", SourceID: "partner"},
		{WidgetType: "teleporter", WidgetVersion: 1},
		{WidgetType: "metric-display"},
	}
	var builder strings.Builder
	for _, binding := range bindings {
		result := MigrateCompositionWidgets(PageComposition{ClassificationCeiling: "internal", Widgets: []WidgetBinding{binding}}, registry)
		builder.WriteString(binding.WidgetType)
		builder.WriteString("\x00")
		if result.Compatible {
			builder.WriteString("compatible")
		} else {
			builder.WriteString("incompatible")
		}
		builder.WriteString("\x00")
		for _, step := range result.Migrated {
			builder.WriteString(step.WidgetType)
			builder.WriteString("\x00")
		}
		builder.WriteString(strings.Join(result.Reasons, ";"))
		builder.WriteString("\n")
	}
	digest := sha256.Sum256([]byte(builder.String()))
	got := hex.EncodeToString(digest[:])
	const want = "b3061771b46e9e3d0162e546750365bdf90f386ee8a9d822fb471bf38eb1df41"
	if got != want {
		t.Fatalf("migration digest = %s, want %s", got, want)
	}
}

// Browser: every registered widget migrates a back-version binding
// to its bumped registry version — deterministically — and an
// already-current composition migrates to itself.
func TestTodo_WEB_081_Browser(t *testing.T) {
	for _, widget := range RegisteredWidgets().Widgets {
		registry := RegisteredWidgets()
		for i := range registry.Widgets {
			if registry.Widgets[i].ID == widget.ID {
				registry.Widgets[i].Version = widget.Version + 1
			}
		}
		composition := PageComposition{Widgets: []WidgetBinding{{WidgetType: widget.ID, WidgetVersion: widget.Version,
			AuthorityClass: "canonical", Classification: widget.ClassificationLimit, SourceType: "projection", SourceID: "catalog"}}}
		first := MigrateCompositionWidgets(composition, registry)
		second := MigrateCompositionWidgets(composition, registry)
		if !first.Compatible || len(first.Migrated) != 1 || first.Migrated[0].ToVersion != widget.Version+1 {
			t.Fatalf("widget %q does not migrate: %+v", widget.ID, first)
		}
		if !reflect.DeepEqual(first, second) {
			t.Fatalf("widget %q migration is nondeterministic", widget.ID)
		}
		current := MigrateCompositionWidgets(first.Composition, registry)
		if !current.Compatible || len(current.Migrated) != 0 || !reflect.DeepEqual(current.Composition, first.Composition) {
			t.Fatalf("widget %q current composition does not migrate to itself: %+v", widget.ID, current)
		}
	}
}

// Conformance: migration preserves the validate chain — migrated
// bindings validate, the ceiling still gates, and non-widget
// fields survive byte-identical.
func TestTodo_WEB_081_Conformance(t *testing.T) {
	registry := RegisteredWidgets()
	for i := range registry.Widgets {
		registry.Widgets[i].Version = 2
	}
	composition := PageComposition{Floorplan: "collection", FloorplanVersion: 1, ClassificationCeiling: "internal",
		Primitives: []string{"stack"}, Regions: []string{"main"},
		Widgets: []WidgetBinding{{WidgetType: "metric-display", WidgetVersion: 1, AuthorityClass: "canonical",
			Classification: "internal", SourceType: "projection", SourceID: "journeys"}},
		Actions: []ActionBinding{{Capability: "journeys.create", Token: "t", ExpectedVersion: 1, IdempotencyKey: "k", InputType: "CreateInput"}}}
	migrated := MigrateCompositionWidgets(composition, registry)
	if !migrated.Compatible {
		t.Fatalf("migration refuses: %q", migrated.Reasons)
	}
	draft := PageDraft{Page: "studio", Composition: migrated.Composition}
	if verdict := ValidateDraftWidgets(draft, registry); !verdict.Compatible {
		t.Fatalf("migrated bindings fail validation: %q", verdict.Reasons)
	}
	if verdict := ValidateDraftCeiling(draft); !verdict.Compatible {
		t.Fatalf("migration broke the ceiling: %q", verdict.Reasons)
	}
	want := composition
	want.Widgets = []WidgetBinding{{WidgetType: "metric-display", WidgetVersion: 2, AuthorityClass: "canonical",
		Classification: "internal", SourceType: "projection", SourceID: "journeys"}}
	if !reflect.DeepEqual(migrated.Composition, want) {
		t.Fatalf("migration touched non-version fields:\n%+v\n%+v", migrated.Composition, want)
	}
}
