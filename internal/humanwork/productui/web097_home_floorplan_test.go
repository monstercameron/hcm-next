package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"reflect"
	"strings"
	"testing"
)

// RED for WEB-097: the authorization-resolved Home floorplan.
// Home renders hardcoded sections with scattered Allows checks,
// and the phase's sections (attention, summaries, quick
// actions, recent work, announcements) have no governed slots
// to attach to: ordering and visibility live in render code.
// The lifecycle needs a registered Home floorplan — ordered
// section IDs with authorization resolution — placing every
// section the phase builds. Placement splits from content:
// slots resolve here, per-viewer contents resolve in their
// section todos; the attention slot gates on the work surface
// (mirroring the work collection), the rest place
// unconditionally with authorized contents.
func TestTodo_WEB_097(t *testing.T) {
	sections := HomeFloorplanSections()
	ids := []string{}
	for _, section := range sections {
		ids = append(ids, section.ID)
	}
	if !reflect.DeepEqual(ids, []string{
		HomeSectionAttention, HomeSectionSummaries, HomeSectionQuickActions, HomeSectionRecentWork, HomeSectionAnnouncements,
	}) {
		t.Fatalf("home floorplan sections = %q", ids)
	}

	allowWork := View{EffectivePermissions: []RolePagePermission{{Page: PageWork, View: true}}}
	full := ResolveHomeFloorplan(allowWork)
	if len(full) != 5 {
		t.Fatalf("authorized floorplan resolves %d sections, want 5", len(full))
	}
	denyWork := View{EffectivePermissions: []RolePagePermission{{Page: PageWork}}}
	bare := ResolveHomeFloorplan(denyWork)
	bareIDs := []string{}
	for _, section := range bare {
		bareIDs = append(bareIDs, section.ID)
	}
	if !reflect.DeepEqual(bareIDs, []string{HomeSectionSummaries, HomeSectionQuickActions, HomeSectionRecentWork, HomeSectionAnnouncements}) {
		t.Fatalf("denied floorplan = %q", bareIDs)
	}

	// Legacy views without a permission projection keep every slot.
	legacy := ResolveHomeFloorplan(View{})
	if len(legacy) != 5 {
		t.Fatalf("legacy floorplan resolves %d sections, want 5", len(legacy))
	}
}

// Golden: floorplan resolution over a permission matrix.
func TestTodo_WEB_097_Golden(t *testing.T) {
	views := []View{
		{},
		{EffectivePermissions: []RolePagePermission{{Page: PageWork, View: true}}},
		{EffectivePermissions: []RolePagePermission{{Page: PageWork}}},
		{EffectivePermissions: []RolePagePermission{{Page: PageMyself, View: true}, {Page: PageJourneys, View: true}}},
	}
	var builder strings.Builder
	for _, view := range views {
		for _, section := range ResolveHomeFloorplan(view) {
			builder.WriteString(section.ID)
			builder.WriteString("\x00")
		}
		builder.WriteString("\n")
	}
	digest := sha256.Sum256([]byte(builder.String()))
	got := hex.EncodeToString(digest[:])
	const want = "1be2a42514d450b80ea87c013f557cbad726357eabe68f11794446ba287e46c5"
	if got != want {
		t.Fatalf("floorplan digest = %s, want %s", got, want)
	}
}

// Browser: every gate page is a registered surface, and
// resolution is deterministic across the registry.
func TestTodo_WEB_097_Browser(t *testing.T) {
	registered := map[PageID]bool{}
	for _, definition := range PageDefinitions() {
		registered[definition.ID] = true
	}
	for _, section := range HomeFloorplanSections() {
		if section.Page != "" && !registered[section.Page] {
			t.Fatalf("section %q gates on unregistered page %q", section.ID, section.Page)
		}
	}
	view := View{EffectivePermissions: []RolePagePermission{{Page: PageWork, View: true}}}
	first := ResolveHomeFloorplan(view)
	second := ResolveHomeFloorplan(view)
	if !reflect.DeepEqual(first, second) {
		t.Fatal("floorplan resolution is nondeterministic")
	}
}

// Conformance: floorplan order stable, sections unique,
// resolution never mutates the registry.
func TestTodo_WEB_097_Conformance(t *testing.T) {
	first := HomeFloorplanSections()
	second := HomeFloorplanSections()
	if !reflect.DeepEqual(first, second) {
		t.Fatal("floorplan registry is unstable")
	}
	seen := map[string]bool{}
	for _, section := range first {
		if section.ID == "" || seen[section.ID] {
			t.Fatalf("floorplan section missing or duplicated: %+v", first)
		}
		seen[section.ID] = true
	}
	before := HomeFloorplanSections()
	ResolveHomeFloorplan(View{EffectivePermissions: []RolePagePermission{{Page: PageWork, View: true}}})
	if !reflect.DeepEqual(HomeFloorplanSections(), before) {
		t.Fatal("resolution mutated the registry")
	}
	// Deny-all still places the unconditional slots in order.
	denied := ResolveHomeFloorplan(View{EffectivePermissions: []RolePagePermission{{Page: PageJourneys}}})
	ids := []string{}
	for _, section := range denied {
		ids = append(ids, section.ID)
	}
	if !reflect.DeepEqual(ids, []string{HomeSectionSummaries, HomeSectionQuickActions, HomeSectionRecentWork, HomeSectionAnnouncements}) {
		t.Fatalf("deny-all floorplan = %q", ids)
	}
}
