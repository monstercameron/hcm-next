package widgetreg

import (
	"errors"
	"strings"
	"testing"

	"github.com/monstercameron/hcm-next/tools/uxqual/pagedef"
)

// TestTodo_WEB_005 proves the governed registry resolves every slot in both
// Promotion pages only to published, region-compatible definitions.
func TestTodo_WEB_005(t *testing.T) {
	registry := PromotionRegistry()
	pages := []pagedef.PageDefinition{pagedef.PromotionListPageDefinition(), pagedef.PromotionDetailPageDefinition()}
	for _, page := range pages {
		resolution, err := registry.Resolve(page)
		if err != nil {
			t.Fatalf("Resolve(%s): %v", page.PageID, err)
		}
		if len(resolution.Slots) == 0 {
			t.Fatalf("Resolve(%s) returned no slot bindings", page.PageID)
		}
		for _, slot := range resolution.Slots {
			if slot.Widget.Lifecycle != LifecyclePublished {
				t.Errorf("slot %s resolved to %s, want PUBLISHED", slot.SlotID, slot.Widget.Lifecycle)
			}
		}
	}

	page := pagedef.PromotionListPageDefinition()
	page.Regions[2].Widgets[0].WidgetRef = "widget.not-registered.v1"
	if _, err := registry.Resolve(page); !errors.Is(err, ErrUnknownWidget) {
		t.Fatalf("unknown slot error = %v, want ErrUnknownWidget", err)
	}
}

// TestTodo_WEB_005_Golden pins the exact Promotion widget set and registry
// digest, including the RPC, region, accessibility, owner, and lifecycle
// contracts carried by each definition.
func TestTodo_WEB_005_Golden(t *testing.T) {
	registry := PromotionRegistry()
	wantRefs := []string{
		"widget.factlist@1", "widget.form.create-worker@1", "widget.form.propose-journey@1",
		"widget.gauge.budget@1", "widget.gauge.pay-band@1", "widget.list.journeys@1",
		"widget.stepper.journey-stage@1", "widget.table.comparison@1", "widget.table.work-items@1",
		"widget.table.workforce@1", "widget.timeline@1",
	}
	gotRefs := registry.Refs()
	if strings.Join(gotRefs, "\n") != strings.Join(wantRefs, "\n") {
		t.Fatalf("Promotion registry refs = %v, want %v", gotRefs, wantRefs)
	}
	const wantDigest = "sha256:e0c36e1fd516e35abb219939c449e972f5c98a6bfbc78e9b1b3b56051c0eb4fc"
	if got := registry.Digest(); got != wantDigest {
		t.Fatalf("Promotion registry digest = %q, want pinned golden %q", got, wantDigest)
	}
}

// TestTodo_WEB_005_Browser verifies the browser-facing contract is semantic
// data: every Promotion widget declares keyboard, focus, state, error,
// reflow, reduced-motion, and high-contrast obligations.
func TestTodo_WEB_005_Browser(t *testing.T) {
	for _, ref := range PromotionRegistry().Refs() {
		widget, ok := PromotionRegistry().Lookup(ref)
		if !ok {
			t.Fatalf("Lookup(%s) failed", ref)
		}
		if !widget.Accessibility.KeyboardAccessible || !widget.Accessibility.VisibleFocus ||
			!widget.Accessibility.SemanticStates || !widget.Accessibility.ErrorAssociation ||
			!widget.Accessibility.ResponsiveReflow || !widget.Accessibility.ReducedMotion ||
			!widget.Accessibility.HighContrast {
			t.Errorf("%s has incomplete accessibility contract: %+v", ref, widget.Accessibility)
		}
	}
}

// TestTodo_WEB_005_Conformance covers lifecycle refusal, successor evidence,
// retirement usage safety, and the no-renderer/no-code boundary.
func TestTodo_WEB_005_Conformance(t *testing.T) {
	old := validWidget("widget.example", LifecycleDraft)
	successor := validWidget("widget.example", LifecyclePublished)
	successor.Version = 2
	registry, err := NewRegistry(old, successor)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	if err := registry.Transition("widget.example@1", LifecyclePublished, ""); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if err := registry.Transition("widget.example@1", LifecycleDeprecated, "widget.example@2"); err != nil {
		t.Fatalf("deprecate: %v", err)
	}
	page := pagedef.PageDefinition{
		PageID: "example", Version: 1, FloorplanRef: "floorplan.example.v1",
		Regions:       []pagedef.Region{{ID: "main", Kind: pagedef.RegionPrimary, Widgets: []pagedef.WidgetSlot{{ID: "slot", WidgetRef: "widget.example.v1"}}}},
		Accessibility: pagedef.Accessibility{Landmarks: []string{"main"}, LiveRegion: pagedef.LiveRegionOff},
		BrandTokens:   []string{"brand.color.primary"},
	}
	if err := registry.Retire("widget.example@1", page); !errors.Is(err, ErrWidgetInUse) {
		t.Fatalf("retire in-use widget = %v, want ErrWidgetInUse", err)
	}
	if err := registry.Retire("widget.example@1"); err != nil {
		t.Fatalf("retire after page removal: %v", err)
	}
	if len(registry.History()) != 3 {
		t.Fatalf("history length = %d, want 3", len(registry.History()))
	}
	if registry.History()[1].Digest == "" {
		t.Fatal("deprecation transition has no digest")
	}

	if err := validWidget("widget.invalid", LifecycleDeprecated).Validate(); !errors.Is(err, ErrSuccessorRequired) {
		t.Fatalf("deprecated widget without successor = %v, want ErrSuccessorRequired", err)
	}
}

func validWidget(id string, state LifecycleState) WidgetDefinition {
	widget := WidgetDefinition{
		ID: id, Version: 1, RegionKinds: []pagedef.RegionKind{pagedef.RegionPrimary},
		Accessibility: AccessibilityContract{
			Name: "Example", RequiredAttributes: []string{"aria-label"},
			KeyboardAccessible: true, VisibleFocus: true, SemanticStates: true,
			ErrorAssociation: true, ResponsiveReflow: true, ReducedMotion: true, HighContrast: true,
		},
		BrandTokenRefs: []string{"brand.color.primary"}, Owner: "test", Lifecycle: state,
	}
	return widget
}
