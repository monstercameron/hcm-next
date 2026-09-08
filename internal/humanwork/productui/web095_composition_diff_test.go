package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

// RED for WEB-095: semantic page diff review. Revisions pin what
// was published, but review has no semantic comparison: authors
// eyeball two compositions with no field-level account of what
// moved — floorplan swaps, region reorders, widget version
// bumps, ceiling tightenings, binding changes. The lifecycle
// needs a pure composition diff in field order — changed
// scalars, added/removed/moved list members, paired binding
// changes — with token and idempotency rotations silent (they
// re-prove, never re-mean). Textual rendering stays out.
func TestTodo_WEB_095(t *testing.T) {
	old := PageComposition{
		Purpose: "Track requests", Audience: "managers",
		Floorplan: "collection", FloorplanVersion: 1, ClassificationCeiling: "internal",
		Primitives: []string{"stack", "table"}, Regions: []string{"identity", "primary"},
		Widgets: []WidgetBinding{
			{WidgetType: "metric-display", WidgetVersion: 1, AuthorityClass: "canonical", Classification: "internal", SourceType: "projection", SourceID: "journeys"},
			{WidgetType: "external-frame", WidgetVersion: 1, AuthorityClass: "external", Classification: "public", SourceType: "embed", SourceID: "partner"},
		},
		Actions: []ActionBinding{{Capability: "journeys.create", Token: "old-token", ExpectedVersion: 1, IdempotencyKey: "k1", InputType: "CreateInput"}},
	}
	next := PageComposition{
		Purpose: "Track promotion journeys", Audience: "managers",
		Floorplan: "collection", FloorplanVersion: 2, ClassificationCeiling: "public",
		Primitives: []string{"table", "tabs"}, Regions: []string{"primary", "identity"},
		Widgets: []WidgetBinding{
			{WidgetType: "metric-display", WidgetVersion: 2, AuthorityClass: "canonical", Classification: "internal", SourceType: "projection", SourceID: "journeys"},
			{WidgetType: "proposal-form", WidgetVersion: 1, AuthorityClass: "manager", Classification: "confidential", SourceType: "workflow", SourceID: "intent-1"},
		},
		Actions: []ActionBinding{{Capability: "journeys.create", Token: "new-token", ExpectedVersion: 1, IdempotencyKey: "k2", InputType: "CreateInput"}},
	}
	diff := DiffCompositions(old, next)
	byField := map[string][]CompositionDiff{}
	for _, entry := range diff {
		byField[entry.Field] = append(byField[entry.Field], entry)
	}
	cases := []struct {
		field  string
		kind   string
		detail string
	}{
		{"purpose", "changed", `"Track requests" → "Track promotion journeys"`},
		{"floorplan_version", "changed", `1 → 2`},
		{"classification_ceiling", "changed", `"internal" → "public" (tightened)`},
		{"primitives", "removed", `"stack"`},
		{"primitives", "added", `"tabs"`},
		{"regions", "moved", `"identity" 0 → 1`},
		{"regions", "moved", `"primary" 1 → 0`},
		{"widgets", "changed", `"metric-display" 1 → 2`},
		{"widgets", "removed", `"external-frame" version 1`},
		{"widgets", "added", `"proposal-form" version 1`},
	}
	for _, want := range cases {
		found := false
		for _, entry := range byField[want.field] {
			if entry.Kind == want.kind && entry.Detail == want.detail {
				found = true
			}
		}
		if !found {
			t.Fatalf("missing %+v in %+v", want, byField[want.field])
		}
	}
	// Audience kept, token and idempotency rotations silent.
	for _, entry := range diff {
		if entry.Field == "audience" || entry.Field == "actions" {
			t.Fatalf("non-change reported: %+v", entry)
		}
		if strings.Contains(entry.Detail, "old-token") || strings.Contains(entry.Detail, "new-token") {
			t.Fatalf("token material in diff: %+v", entry)
		}
	}
	// Field order follows the composition declaration order.
	fields := []string{}
	for _, entry := range diff {
		if len(fields) == 0 || fields[len(fields)-1] != entry.Field {
			fields = append(fields, entry.Field)
		}
	}
	wantOrder := []string{"purpose", "floorplan_version", "classification_ceiling", "primitives", "regions", "widgets"}
	if !reflect.DeepEqual(fields, wantOrder) {
		t.Fatalf("diff field order = %q, want %q", fields, wantOrder)
	}

	if diff := DiffCompositions(old, old); len(diff) != 0 {
		t.Fatalf("identical compositions diff %d entries", len(diff))
	}
}

// Golden: diffs over old/new composition pairs.
func TestTodo_WEB_095_Golden(t *testing.T) {
	empty := PageComposition{}
	full := PageComposition{
		Purpose: "Track promotion journeys", Audience: "managers",
		Floorplan: "object", FloorplanVersion: 2, ClassificationCeiling: "confidential",
		Primitives: []string{"tabs", "table"}, Regions: []string{"primary", "supporting"},
		Widgets: []WidgetBinding{
			{WidgetType: "metric-display", WidgetVersion: 2, AuthorityClass: "canonical", Classification: "internal", SourceType: "projection", SourceID: "journeys"},
			{WidgetType: "proposal-form", WidgetVersion: 1, AuthorityClass: "manager", Classification: "confidential", SourceType: "workflow", SourceID: "intent-1"},
		},
		Actions: []ActionBinding{
			{Capability: "journeys.create", Token: "t", ExpectedVersion: 1, IdempotencyKey: "k", InputType: "CreateInput"},
			{Capability: "journeys.approve", Token: "u", ExpectedVersion: 2, IdempotencyKey: "j", InputType: "ApproveInput"},
		},
	}
	loosened := full
	loosened.ClassificationCeiling = "topsecret"
	reordered := full
	reordered.Regions = []string{"supporting", "primary"}
	duplicates := full
	duplicates.Primitives = []string{"tabs", "tabs", "table"}
	pairs := [][2]PageComposition{
		{empty, empty},
		{empty, full},
		{full, empty},
		{full, loosened},
		{full, reordered},
		{full, duplicates},
	}
	var builder strings.Builder
	for _, pair := range pairs {
		for _, entry := range DiffCompositions(pair[0], pair[1]) {
			builder.WriteString(entry.Field)
			builder.WriteString("\x00")
			builder.WriteString(entry.Kind)
			builder.WriteString("\x00")
			builder.WriteString(entry.Detail)
			builder.WriteString("\n")
		}
		builder.WriteString("---\n")
	}
	digest := sha256.Sum256([]byte(builder.String()))
	got := hex.EncodeToString(digest[:])
	const want = "b9b395c1415f41eff0dd4de1eb3722f752bedc180adbabbcc549640a296f4952"
	if got != want {
		t.Fatalf("diff digest = %s, want %s", got, want)
	}
}

// Browser: every registered widget's version bump diffs to
// exactly one changed entry with the version arrow —
// deterministically.
func TestTodo_WEB_095_Browser(t *testing.T) {
	for _, widget := range RegisteredWidgets().Widgets {
		binding := WidgetBinding{WidgetType: widget.ID, WidgetVersion: widget.Version, AuthorityClass: "canonical",
			Classification: widget.ClassificationLimit, SourceType: "projection", SourceID: "catalog"}
		bumped := binding
		bumped.WidgetVersion = widget.Version + 1
		old := PageComposition{Widgets: []WidgetBinding{binding}}
		next := PageComposition{Widgets: []WidgetBinding{bumped}}
		first := DiffCompositions(old, next)
		second := DiffCompositions(old, next)
		if len(first) != 1 || first[0].Field != "widgets" || first[0].Kind != "changed" {
			t.Fatalf("widget %q bump diffs %+v", widget.ID, first)
		}
		want := `"` + widget.ID + `" ` + strconv.FormatInt(widget.Version, 10) + " → " + strconv.FormatInt(widget.Version+1, 10)
		if first[0].Detail != want {
			t.Fatalf("widget %q bump detail = %q, want %q", widget.ID, first[0].Detail, want)
		}
		if !reflect.DeepEqual(first, second) {
			t.Fatalf("widget %q diff is nondeterministic", widget.ID)
		}
	}
}

// Conformance: token and idempotency rotations silent,
// token granted/withdrawn reported, dupes matched by
// occurrence.
func TestTodo_WEB_095_Conformance(t *testing.T) {
	base := WidgetBinding{WidgetType: "metric-display", WidgetVersion: 1, AuthorityClass: "canonical",
		Classification: "internal", SourceType: "projection", SourceID: "journeys", Value: "v"}
	rotated := base
	rotated.Value = "v2"
	if diff := DiffCompositions(PageComposition{Widgets: []WidgetBinding{base}}, PageComposition{Widgets: []WidgetBinding{rotated}}); len(diff) != 1 {
		t.Fatalf("reconfigured binding diffs %d entries: %+v", len(diff), diff)
	} else if diff[0].Detail != `"metric-display" details differ` {
		t.Fatalf("reconfigured detail = %q", diff[0].Detail)
	}
	oldRegions := PageComposition{Regions: []string{"supporting", "primary", "supporting"}}
	newRegions := PageComposition{Regions: []string{"supporting", "supporting", "primary"}}
	diff := DiffCompositions(oldRegions, newRegions)
	if len(diff) != 2 {
		t.Fatalf("dupe reorder diffs %+v", diff)
	}
	again := DiffCompositions(oldRegions, newRegions)
	if !reflect.DeepEqual(diff, again) {
		t.Fatal("diff is nondeterministic")
	}
	actionOld := PageComposition{Actions: []ActionBinding{{Capability: "journeys.create", ExpectedVersion: 1, InputType: "A"}}}
	actionNew := PageComposition{Actions: []ActionBinding{{Capability: "journeys.create", Token: "t", ExpectedVersion: 1, InputType: "A"}}}
	granted := DiffCompositions(actionOld, actionNew)
	if len(granted) != 1 || granted[0].Detail != `action "journeys.create" token granted` {
		t.Fatalf("granted token diffs %+v", granted)
	}
	withdrawn := DiffCompositions(actionNew, actionOld)
	if len(withdrawn) != 1 || withdrawn[0].Detail != `action "journeys.create" token withdrawn` {
		t.Fatalf("withdrawn token diffs %+v", withdrawn)
	}
}
