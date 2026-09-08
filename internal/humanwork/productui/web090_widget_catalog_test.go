package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"reflect"
	"strings"
	"testing"
)

// RED for WEB-090: the permitted widget catalog. Ceilings gate
// composed binding data (WEB-079), but the studio catalog panel
// has no per-page answer: nothing says which registered widgets
// an author may compose under a page ceiling, so authors pick
// confidential-capable widgets onto public pages and learn it
// from binding refusals. The lifecycle needs a pure permitted
// catalog — every registered widget listed with its status —
// failing closed on undeclared ceilings and unranked labels. A
// widget clears the catalog exactly when its classification
// limit sits at or below the page ceiling.
func TestTodo_WEB_090(t *testing.T) {
	catalog := PermittedWidgetCatalog("internal", RegisteredWidgets())
	if len(catalog) != 3 {
		t.Fatalf("catalog lists %d widgets, want 3", len(catalog))
	}
	byID := map[string]WidgetPermission{}
	for _, permission := range catalog {
		byID[permission.Widget.ID] = permission
	}
	if !byID["metric-display"].Permitted || byID["metric-display"].Reason != "" {
		t.Fatalf("metric-display permission = %+v", byID["metric-display"])
	}
	if !byID["external-frame"].Permitted {
		t.Fatalf("external-frame permission = %+v", byID["external-frame"])
	}
	if byID["proposal-form"].Permitted || byID["proposal-form"].Reason != `widget "proposal-form" limit "confidential" exceeds page ceiling "internal"` {
		t.Fatalf("proposal-form permission = %+v", byID["proposal-form"])
	}

	public := PermittedWidgetCatalog("public", RegisteredWidgets())
	for _, permission := range public {
		if permission.Widget.ID == "external-frame" {
			if !permission.Permitted {
				t.Fatalf("public page refuses its embed: %+v", permission)
			}
		} else if permission.Permitted {
			t.Fatalf("public page permits %q", permission.Widget.ID)
		}
	}
	for _, permission := range PermittedWidgetCatalog("restricted", RegisteredWidgets()) {
		if !permission.Permitted {
			t.Fatalf("restricted ceiling refuses %q: %+v", permission.Widget.ID, permission)
		}
	}

	for _, bad := range []struct {
		name    string
		ceiling string
		reason  string
	}{
		{"missing ceiling", "", "missing classification ceiling"},
		{"unknown ceiling", "topsecret", `unknown classification ceiling "topsecret"`},
	} {
		for _, permission := range PermittedWidgetCatalog(bad.ceiling, RegisteredWidgets()) {
			if permission.Permitted || permission.Reason != bad.reason {
				t.Fatalf("%s: %+v", bad.name, permission)
			}
		}
	}

	custom := WidgetRegistry{Widgets: []WidgetDefinition{
		{ID: "zeta", Tier: WidgetTierContent, Version: 1, ClassificationLimit: "internal"},
		{ID: "alpha", Tier: WidgetTierContent, Version: 1, ClassificationLimit: "topsecret"},
	}}
	customCatalog := PermittedWidgetCatalog("internal", custom)
	if len(customCatalog) != 2 || customCatalog[0].Widget.ID != "alpha" || customCatalog[1].Widget.ID != "zeta" {
		t.Fatalf("catalog is not ID-sorted: %+v", customCatalog)
	}
	if customCatalog[0].Permitted || customCatalog[0].Reason != `unranked classification limit "topsecret" for widget "alpha"` {
		t.Fatalf("unranked limit permission = %+v", customCatalog[0])
	}
	if !customCatalog[1].Permitted {
		t.Fatalf("zeta permission = %+v", customCatalog[1])
	}

	if empty := PermittedWidgetCatalog("internal", WidgetRegistry{}); len(empty) != 0 {
		t.Fatalf("empty registry catalogs %d widgets", len(empty))
	}
}

// Golden: catalog outcomes over ceilings and registries.
func TestTodo_WEB_090_Golden(t *testing.T) {
	registries := []WidgetRegistry{
		RegisteredWidgets(),
		{Widgets: []WidgetDefinition{
			{ID: "zeta", Tier: WidgetTierContent, Version: 1, ClassificationLimit: "internal"},
			{ID: "alpha", Tier: WidgetTierExternal, Version: 1, ClassificationLimit: "topsecret"},
		}},
		{},
	}
	ceilings := []string{"public", "internal", "confidential", "restricted", "", "topsecret"}
	var builder strings.Builder
	for _, registry := range registries {
		for _, ceiling := range ceilings {
			builder.WriteString(ceiling)
			builder.WriteString("\x00")
			for _, permission := range PermittedWidgetCatalog(ceiling, registry) {
				builder.WriteString(permission.Widget.ID)
				builder.WriteString("\x00")
				if permission.Permitted {
					builder.WriteString("permitted")
				} else {
					builder.WriteString("refused")
				}
				builder.WriteString("\x00")
				builder.WriteString(permission.Reason)
				builder.WriteString("\x00")
			}
			builder.WriteString("\n")
		}
	}
	digest := sha256.Sum256([]byte(builder.String()))
	got := hex.EncodeToString(digest[:])
	const want = "170eee8c008272d762870afc1731c318d02f87675fa4ff34ca8282f4b90e9d56"
	if got != want {
		t.Fatalf("catalog digest = %s, want %s", got, want)
	}
}

// Browser: every registered widget is permitted exactly where the
// ladder orders its limit — deterministically.
func TestTodo_WEB_090_Browser(t *testing.T) {
	ladder := []string{"public", "internal", "confidential", "restricted"}
	for _, widget := range RegisteredWidgets().Widgets {
		limitRank, ok := ClassificationRank(widget.ClassificationLimit)
		if !ok {
			t.Fatalf("registered widget %q carries an unranked limit", widget.ID)
		}
		for _, ceiling := range ladder {
			first := PermittedWidgetCatalog(ceiling, RegisteredWidgets())
			second := PermittedWidgetCatalog(ceiling, RegisteredWidgets())
			ceilingRank, _ := ClassificationRank(ceiling)
			var found *WidgetPermission
			for i, permission := range first {
				if permission.Widget.ID == widget.ID {
					found = &first[i]
				}
			}
			if found == nil {
				t.Fatalf("widget %q missing from its catalog", widget.ID)
			}
			if want := limitRank <= ceilingRank; found.Permitted != want {
				t.Fatalf("widget %q limit %q under ceiling %q permitted=%t, want %t",
					widget.ID, widget.ClassificationLimit, ceiling, found.Permitted, want)
			}
			if !reflect.DeepEqual(first, second) {
				t.Fatalf("catalog under ceiling %q is nondeterministic", ceiling)
			}
		}
	}
}

// Conformance: reasons stable, registry never mutated, permission
// entries own their widgets.
func TestTodo_WEB_090_Conformance(t *testing.T) {
	registry := RegisteredWidgets()
	before := RegisteredWidgets()
	first := PermittedWidgetCatalog("internal", registry)
	second := PermittedWidgetCatalog("internal", registry)
	if !reflect.DeepEqual(first, second) {
		t.Fatal("catalog is nondeterministic")
	}
	if !reflect.DeepEqual(registry, before) {
		t.Fatal("catalog query mutated the registry")
	}
	for _, permission := range first {
		if permission.Permitted == (permission.Reason != "") {
			t.Fatalf("permission/refusal mismatch: %+v", permission)
		}
	}
}
