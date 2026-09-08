package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"reflect"
	"strings"
	"testing"
)

// RED for WEB-085: the page-definition inventory. Revisions,
// compositions, rollouts, retirements, and impact reports exist as
// separate pieces, but the studio has no single page inventory:
// no one view lists every known page with its published version,
// live scopes, retirement state, and impacted contracts. The
// lifecycle needs a pure inventory build over those inputs —
// unioned, sorted, deterministic — failing closed on malformed
// contract changes. Validation stays in the validate chain;
// serving reads stay mechanical on validated inputs.
func TestTodo_WEB_085(t *testing.T) {
	var log PageRevisionLog
	snap := PageDefinitionSnapshot{Page: "studio", Route: "/workspace/app/studio"}
	v1, err := log.Record(snap.Page, snap, 1)
	if err != nil {
		t.Fatal(err)
	}
	v2, err := log.Record(snap.Page, snap, 2)
	if err != nil {
		t.Fatal(err)
	}
	compositions := map[PageID]PageComposition{
		"studio": {Floorplan: "collection", FloorplanVersion: 1,
			Widgets: []WidgetBinding{{WidgetType: "metric-display", WidgetVersion: 1}}},
		"draft-only": {Floorplan: "object",
			Widgets: []WidgetBinding{{WidgetType: "proposal-form", WidgetVersion: 1}}},
	}
	changes := []DependencyChange{{Kind: "widget", ID: "metric-display", ToVersion: 2}}
	rollouts := []PageRollout{
		{Page: "studio", Version: 1, Digest: v1.Digest, Scopes: []RolloutScope{{Scope: "org-old"}}},
		{Page: "studio", Version: 2, Digest: v2.Digest, Scopes: []RolloutScope{{Scope: "org-a"}, {Scope: "org-b", EffectiveFrom: 1700000000}}},
	}
	inventory, err := BuildPageInventory(&log, compositions, changes, rollouts, nil, 1700000001)
	if err != nil {
		t.Fatalf("inventory build errors: %v", err)
	}
	if len(inventory) != 2 || inventory[0].Page != "draft-only" || inventory[1].Page != "studio" {
		t.Fatalf("inventory is not a sorted union: %+v", inventory)
	}
	studio := inventory[1]
	if !studio.Published || studio.Version != 2 || studio.Digest != v2.Digest {
		t.Fatalf("studio entry pins no latest revision: %+v", studio)
	}
	if !reflect.DeepEqual(studio.LiveScopes, []string{"org-a", "org-b"}) {
		t.Fatalf("studio live scopes = %q, want latest-version rollout scopes only", studio.LiveScopes)
	}
	if studio.Retired || !reflect.DeepEqual(studio.ImpactedContracts, []string{"metric-display"}) {
		t.Fatalf("studio status wrong: %+v", studio)
	}
	draft := inventory[0]
	if draft.Published || draft.Version != 0 || draft.Digest != "" {
		t.Fatalf("unpublished page looks published: %+v", draft)
	}
	if len(draft.LiveScopes) != 0 || draft.Retired || len(draft.ImpactedContracts) != 0 {
		t.Fatalf("draft-only entry carries status: %+v", draft)
	}

	retired, err := BuildPageInventory(&log, compositions, changes, rollouts,
		[]PageRetirement{{Page: "studio", EffectiveFrom: 1800000000, Reason: "ended"}}, 1800000001)
	if err != nil {
		t.Fatal(err)
	}
	if !retired[1].Retired {
		t.Fatalf("active retirement not reflected: %+v", retired[1])
	}
	if retired[1].LiveScopes == nil || len(retired[1].LiveScopes) != 2 {
		t.Fatalf("retirement rewrote rollout facts: %+v", retired[1])
	}

	if _, err := BuildPageInventory(&log, compositions,
		[]DependencyChange{{Kind: "capability", ID: "x"}}, rollouts, nil, 0); err == nil {
		t.Fatal("malformed change builds")
	}
	if inventory, err := BuildPageInventory(&PageRevisionLog{}, nil, nil, nil, nil, 0); err != nil || len(inventory) != 0 {
		t.Fatalf("empty build = (%v, %v)", inventory, err)
	}
}

// Golden: inventory entries over a fixed multi-source setup.
func TestTodo_WEB_085_Golden(t *testing.T) {
	var log PageRevisionLog
	for _, page := range []PageID{"studio", "people"} {
		snap := PageDefinitionSnapshot{Page: page}
		if _, err := log.Record(snap.Page, snap, 1); err != nil {
			t.Fatal(err)
		}
	}
	compositions := map[PageID]PageComposition{
		"studio": {Floorplan: "collection", FloorplanVersion: 1,
			Widgets: []WidgetBinding{{WidgetType: "metric-display", WidgetVersion: 1}}},
		"people": {Floorplan: "object",
			Widgets: []WidgetBinding{{WidgetType: "proposal-form", WidgetVersion: 1}}},
		"ghost": {},
	}
	changes := []DependencyChange{
		{Kind: "widget", ID: "metric-display", ToVersion: 2},
		{Kind: "floorplan", ID: "object"},
	}
	latest, _ := log.Latest("studio")
	rollouts := []PageRollout{
		{Page: "studio", Version: 1, Digest: latest.Digest, Scopes: []RolloutScope{{Scope: "org-a"}, {Scope: "org-b", EffectiveFrom: 1700000000}}},
		{Page: "people", Version: 1, Digest: "unpinned", Scopes: []RolloutScope{{Scope: "org-a"}}},
	}
	retirements := []PageRetirement{{Page: "people", EffectiveFrom: 100, Reason: "merged"}}
	var builder strings.Builder
	for _, now := range []int64{50, 1700000001} {
		inventory, err := BuildPageInventory(&log, compositions, changes, rollouts, retirements, now)
		if err != nil {
			t.Fatal(err)
		}
		for _, entry := range inventory {
			builder.WriteString(string(entry.Page))
			builder.WriteString("\x00")
			if entry.Published {
				builder.WriteString("published")
			} else {
				builder.WriteString("unpublished")
			}
			builder.WriteString("\x00")
			builder.WriteString(strings.Join(entry.LiveScopes, ","))
			builder.WriteString("\x00")
			if entry.Retired {
				builder.WriteString("retired")
			} else {
				builder.WriteString("active")
			}
			builder.WriteString("\x00")
			builder.WriteString(strings.Join(entry.ImpactedContracts, ","))
			builder.WriteString("\n")
		}
	}
	digest := sha256.Sum256([]byte(builder.String()))
	got := hex.EncodeToString(digest[:])
	const want = "e3a7a1c5ee8d52cfbaf8aa2891dc6fd9d5c7f20379994df937eb6db9d780e5e3"
	if got != want {
		t.Fatalf("inventory digest = %s, want %s", got, want)
	}
}

// Browser: every registered page inventories with its revision,
// composition, and rollout — deterministically.
func TestTodo_WEB_085_Browser(t *testing.T) {
	var log PageRevisionLog
	compositions := map[PageID]PageComposition{}
	var rollouts []PageRollout
	for _, definition := range PageDefinitions() {
		snap := SnapshotPageDefinition(definition)
		recorded, err := log.Record(snap.Page, snap, 1)
		if err != nil {
			t.Fatal(err)
		}
		compositions[definition.ID] = PageComposition{Floorplan: "collection"}
		rollouts = append(rollouts, PageRollout{Page: definition.ID, Version: 1, Digest: recorded.Digest,
			Scopes: []RolloutScope{{Scope: "org-a"}}})
	}
	first, err := BuildPageInventory(&log, compositions, nil, rollouts, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	second, err := BuildPageInventory(&log, compositions, nil, rollouts, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != len(PageDefinitions()) {
		t.Fatalf("inventory covers %d pages, want %d", len(first), len(PageDefinitions()))
	}
	for _, entry := range first {
		if !entry.Published || entry.Version != 1 || entry.Digest == "" {
			t.Fatalf("page %q entry pins no revision: %+v", entry.Page, entry)
		}
		if !reflect.DeepEqual(entry.LiveScopes, []string{"org-a"}) {
			t.Fatalf("page %q live scopes = %q", entry.Page, entry.LiveScopes)
		}
		if entry.Retired || len(entry.ImpactedContracts) != 0 {
			t.Fatalf("page %q carries false status: %+v", entry.Page, entry)
		}
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatal("inventory is nondeterministic")
	}
}

// Conformance: rollout-only pages list unpublished, superseded
// versions stay out of live scopes, empty compositions carry no
// impact.
func TestTodo_WEB_085_Conformance(t *testing.T) {
	var log PageRevisionLog
	snap := PageDefinitionSnapshot{Page: "studio"}
	v1, err := log.Record(snap.Page, snap, 1)
	if err != nil {
		t.Fatal(err)
	}
	v2, err := log.Record(snap.Page, snap, 2)
	if err != nil {
		t.Fatal(err)
	}
	rollouts := []PageRollout{
		{Page: "studio", Version: 2, Digest: v2.Digest, Scopes: []RolloutScope{{Scope: "org-new"}}},
		{Page: "studio", Version: 1, Digest: v1.Digest, Scopes: []RolloutScope{{Scope: "org-stale"}}},
		{Page: "phantom", Version: 1, Digest: "x", Scopes: []RolloutScope{{Scope: "org-a"}}},
	}
	inventory, err := BuildPageInventory(&log, map[PageID]PageComposition{"studio": {}}, nil, rollouts, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(inventory) != 2 {
		t.Fatalf("inventory = %+v, want studio plus phantom", inventory)
	}
	for _, entry := range inventory {
		switch entry.Page {
		case "studio":
			if !reflect.DeepEqual(entry.LiveScopes, []string{"org-new"}) {
				t.Fatalf("superseded rollout leaks into live scopes: %q", entry.LiveScopes)
			}
		case "phantom":
			if entry.Published || !reflect.DeepEqual(entry.LiveScopes, []string{"org-a"}) {
				t.Fatalf("rollout-only page entry wrong: %+v", entry)
			}
		}
	}
}
