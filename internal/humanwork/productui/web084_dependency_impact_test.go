package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"reflect"
	"strings"
	"testing"
)

// RED for WEB-084: page dependency impact. Registry and catalog
// versions move (WEB-081 migrates), but nothing reports which
// pages a contract change hits: a widget bump or floorplan change
// leaves the studio guessing which compositions bind the old
// version. The lifecycle needs a pure impact inventory — every
// page listed with its status — over the presentation-owned
// dependency surface (registered widgets, catalog floorplans),
// failing closed on malformed changes. Capabilities and
// classifications stay out: their authorities live in owning
// domains, and presentation must not re-own them.
func TestTodo_WEB_084(t *testing.T) {
	pages := map[PageID]PageComposition{
		"behind":  {Floorplan: "collection", FloorplanVersion: 1, Widgets: []WidgetBinding{{WidgetType: "metric-display", WidgetVersion: 1}}},
		"current": {Floorplan: "collection", FloorplanVersion: 2, Widgets: []WidgetBinding{{WidgetType: "metric-display", WidgetVersion: 2}}},
		"other":   {Floorplan: "object", FloorplanVersion: 1, Widgets: []WidgetBinding{{WidgetType: "proposal-form", WidgetVersion: 1}}},
	}
	report, err := ReportDependencyImpact(pages, DependencyChange{Kind: "widget", ID: "metric-display", ToVersion: 2})
	if err != nil {
		t.Fatalf("valid change errors: %v", err)
	}
	byPage := map[PageID]PageImpact{}
	for _, impact := range report {
		byPage[impact.Page] = impact
	}
	if len(report) != 3 || report[0].Page != "behind" || report[1].Page != "current" || report[2].Page != "other" {
		t.Fatalf("report is not a sorted full inventory: %+v", report)
	}
	if !byPage["behind"].Impacted || byPage["behind"].Reasons == nil ||
		byPage["behind"].Reasons[0] != `widget "metric-display" version 1 is behind registry version 2` {
		t.Fatalf("behind page impact = %+v", byPage["behind"])
	}
	if byPage["current"].Impacted || byPage["other"].Impacted {
		t.Fatalf("current/other pages impacted: %+v", report)
	}

	floor, err := ReportDependencyImpact(pages, DependencyChange{Kind: "floorplan", ID: "collection", ToVersion: 2})
	if err != nil {
		t.Fatal(err)
	}
	for _, impact := range floor {
		switch impact.Page {
		case "behind":
			if !impact.Impacted || impact.Reasons[0] != `floorplan "collection" version 1 is behind catalog version 2` {
				t.Fatalf("behind floorplan impact = %+v", impact)
			}
		case "current", "other":
			if impact.Impacted {
				t.Fatalf("page %q impacted by floorplan change: %+v", impact.Page, impact)
			}
		}
	}

	removed, err := ReportDependencyImpact(pages, DependencyChange{Kind: "widget", ID: "proposal-form"})
	if err != nil {
		t.Fatal(err)
	}
	for _, impact := range removed {
		if impact.Page == "other" {
			if !impact.Impacted || impact.Reasons[0] != `widget "proposal-form" was removed from the registry` {
				t.Fatalf("removed widget impact = %+v", impact)
			}
		} else if impact.Impacted {
			t.Fatalf("page %q impacted by unrelated removal: %+v", impact.Page, impact)
		}
	}

	for _, bad := range []struct {
		name   string
		change DependencyChange
		want   string
	}{
		{"unknown kind", DependencyChange{Kind: "capability", ID: "journeys.create", ToVersion: 2}, `unknown dependency change kind "capability"`},
		{"blank id", DependencyChange{Kind: "widget", ToVersion: 2}, "dependency change names no contract"},
		{"negative version", DependencyChange{Kind: "widget", ID: "metric-display", ToVersion: -1}, "dependency change version must not be negative, got -1"},
	} {
		if _, err := ReportDependencyImpact(pages, bad.change); err == nil || err.Error() != bad.want {
			t.Fatalf("%s = %v, want %q", bad.name, err, bad.want)
		}
	}

	if report, err := ReportDependencyImpact(nil, DependencyChange{Kind: "widget", ID: "metric-display", ToVersion: 2}); err != nil || len(report) != 0 {
		t.Fatalf("empty inventory = (%v, %v)", report, err)
	}
}

// Golden: impact inventories over a change matrix.
func TestTodo_WEB_084_Golden(t *testing.T) {
	pages := map[PageID]PageComposition{
		"alpha": {Floorplan: "collection", FloorplanVersion: 1, Widgets: []WidgetBinding{
			{WidgetType: "metric-display", WidgetVersion: 1},
			{WidgetType: "metric-display", WidgetVersion: 1},
			{WidgetType: "external-frame", WidgetVersion: 1},
		}},
		"beta":  {Floorplan: "object", Widgets: []WidgetBinding{{WidgetType: "proposal-form", WidgetVersion: 3}}},
		"gamma": {},
	}
	changes := []DependencyChange{
		{Kind: "widget", ID: "metric-display", ToVersion: 2},
		{Kind: "widget", ID: "proposal-form"},
		{Kind: "floorplan", ID: "collection", ToVersion: 2},
		{Kind: "floorplan", ID: "object"},
		{Kind: "widget", ID: "teleporter", ToVersion: 2},
	}
	var builder strings.Builder
	for _, change := range changes {
		report, err := ReportDependencyImpact(pages, change)
		if err != nil {
			t.Fatal(err)
		}
		builder.WriteString(change.Kind + "\x00" + change.ID)
		builder.WriteString("\x00")
		for _, impact := range report {
			builder.WriteString(string(impact.Page))
			builder.WriteString("\x00")
			if impact.Impacted {
				builder.WriteString("impacted")
			} else {
				builder.WriteString("clear")
			}
			builder.WriteString("\x00")
			builder.WriteString(strings.Join(impact.Reasons, ";"))
			builder.WriteString("\x00")
		}
		builder.WriteString("\n")
	}
	digest := sha256.Sum256([]byte(builder.String()))
	got := hex.EncodeToString(digest[:])
	const want = "3cbd34c04e2ad3eafc787b29f7a894fac3ff500f63fa70bb123ed4deb722add1"
	if got != want {
		t.Fatalf("impact digest = %s, want %s", got, want)
	}
}

// Browser: every registered widget bump and every registered
// floorplan bump reports exactly the pages binding the old
// version — deterministically.
func TestTodo_WEB_084_Browser(t *testing.T) {
	pages := map[PageID]PageComposition{}
	for _, widget := range RegisteredWidgets().Widgets {
		pages["page-"+PageID(widget.ID)] = PageComposition{Widgets: []WidgetBinding{{WidgetType: widget.ID, WidgetVersion: widget.Version}}}
	}
	for _, floorplan := range RegisteredFloorplans().Floorplans {
		pages["floor-"+PageID(floorplan.ID)] = PageComposition{Floorplan: floorplan.ID, FloorplanVersion: floorplan.Version}
	}
	for _, widget := range RegisteredWidgets().Widgets {
		first, err := ReportDependencyImpact(pages, DependencyChange{Kind: "widget", ID: widget.ID, ToVersion: widget.Version + 1})
		if err != nil {
			t.Fatal(err)
		}
		second, err := ReportDependencyImpact(pages, DependencyChange{Kind: "widget", ID: widget.ID, ToVersion: widget.Version + 1})
		if err != nil {
			t.Fatal(err)
		}
		hits := 0
		for _, impact := range first {
			if impact.Page == "page-"+PageID(widget.ID) {
				hits++
				if !impact.Impacted {
					t.Fatalf("widget %q bump misses its page", widget.ID)
				}
			} else if impact.Impacted {
				t.Fatalf("widget %q bump hits unrelated page %q", widget.ID, impact.Page)
			}
		}
		if hits != 1 {
			t.Fatalf("widget %q bump hits %d pages", widget.ID, hits)
		}
		if !reflect.DeepEqual(first, second) {
			t.Fatalf("widget %q impact is nondeterministic", widget.ID)
		}
	}
	for _, floorplan := range RegisteredFloorplans().Floorplans {
		report, err := ReportDependencyImpact(pages, DependencyChange{Kind: "floorplan", ID: floorplan.ID, ToVersion: floorplan.Version + 1})
		if err != nil {
			t.Fatal(err)
		}
		hits := 0
		for _, impact := range report {
			if impact.Page == "floor-"+PageID(floorplan.ID) {
				hits++
				if !impact.Impacted {
					t.Fatalf("floorplan %q bump misses its page", floorplan.ID)
				}
			} else if impact.Impacted {
				t.Fatalf("floorplan %q bump hits unrelated page %q", floorplan.ID, impact.Page)
			}
		}
		if hits != 1 {
			t.Fatalf("floorplan %q bump hits %d pages", floorplan.ID, hits)
		}
	}
}

// Conformance: reasons deduplicate, ahead versions stay clear,
// undeclared fields never hit.
func TestTodo_WEB_084_Conformance(t *testing.T) {
	pages := map[PageID]PageComposition{
		"ahead":  {Widgets: []WidgetBinding{{WidgetType: "metric-display", WidgetVersion: 5}}},
		"bare":   {},
		"double": {Widgets: []WidgetBinding{{WidgetType: "metric-display", WidgetVersion: 1}, {WidgetType: "metric-display", WidgetVersion: 1}}},
	}
	report, err := ReportDependencyImpact(pages, DependencyChange{Kind: "widget", ID: "metric-display", ToVersion: 2})
	if err != nil {
		t.Fatal(err)
	}
	for _, impact := range report {
		switch impact.Page {
		case "ahead", "bare":
			if impact.Impacted {
				t.Fatalf("page %q impacted: %+v", impact.Page, impact)
			}
		case "double":
			if !impact.Impacted || len(impact.Reasons) != 1 {
				t.Fatalf("duplicate bindings report %d reasons: %+v", len(impact.Reasons), impact)
			}
		}
	}
	floor, err := ReportDependencyImpact(map[PageID]PageComposition{"bare": {}}, DependencyChange{Kind: "floorplan", ID: "collection"})
	if err != nil {
		t.Fatal(err)
	}
	if floor[0].Impacted {
		t.Fatalf("floorplan removal hits a floorplan-less page: %+v", floor[0])
	}
}
