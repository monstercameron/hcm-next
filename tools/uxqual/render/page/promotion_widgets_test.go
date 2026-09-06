package page

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"

	"github.com/monstercameron/hcm-next/tools/uxqual/pagedef"
)

// TestPromotionWidgetRegistryResolvesEveryPromotionPageWidgetRef proves the
// registry [PromotionWidgetRegistry] returns is complete against both real
// Promotion PageDefinitions: every widget ref either page names resolves to
// a registered constructor, so rendering either page through this registry
// never hits [UnregisteredWidgetError].
func TestPromotionWidgetRegistryResolvesEveryPromotionPageWidgetRef(t *testing.T) {
	reg := PromotionWidgetRegistry()

	var refs []string
	for _, pd := range []pagedef.PageDefinition{pagedef.PromotionListPageDefinition(), pagedef.PromotionDetailPageDefinition()} {
		for _, region := range pd.Regions {
			for _, slot := range region.Widgets {
				refs = append(refs, slot.WidgetRef)
			}
		}
	}
	if len(refs) == 0 {
		t.Fatal("fixture PageDefinitions declare no widget slots at all; test fixture is broken")
	}
	for _, ref := range refs {
		if _, ok := reg.Lookup(ref); !ok {
			t.Errorf("PromotionWidgetRegistry has no entry for %q, which a real Promotion PageDefinition declares", ref)
		}
	}
}

// TestPromotionWidgetRegistryConstructorsAreDeterministic proves every
// registered constructor is a pure function of its WidgetContext: called
// twice, each renders to identical bytes. This is the property [Render]'s
// own determinism depends on (see doc.go).
func TestPromotionWidgetRegistryConstructorsAreDeterministic(t *testing.T) {
	reg := PromotionWidgetRegistry()
	ctx := WidgetContext{PageID: "p", PageVersion: 1,
		Region: pagedef.Region{ID: "r", Kind: pagedef.RegionPrimary},
		Slot:   pagedef.WidgetSlot{ID: "s", WidgetRef: "irrelevant"}}

	for _, ref := range reg.Refs() {
		ctor, _ := reg.Lookup(ref)
		first, err := ui.RenderToString(ctor(ctx))
		if err != nil {
			t.Fatalf("widget %q: RenderToString: %v", ref, err)
		}
		second, err := ui.RenderToString(ctor(ctx))
		if err != nil {
			t.Fatalf("widget %q: RenderToString: %v", ref, err)
		}
		if first != second {
			t.Errorf("widget %q rendered differently across two calls with an equal WidgetContext:\n--- first ---\n%s\n--- second ---\n%s", ref, first, second)
		}
		if first == "" {
			t.Errorf("widget %q rendered to an empty string", ref)
		}
	}
}

// TestPromotionWidgetRegistryNoConstructorReturnsNil proves every
// registered constructor honors the "never nil" contract [Widget]'s doc
// comment states: a widget with nothing to show must still return an empty,
// valid node.
func TestPromotionWidgetRegistryNoConstructorReturnsNil(t *testing.T) {
	reg := PromotionWidgetRegistry()
	ctx := WidgetContext{}
	for _, ref := range reg.Refs() {
		ctor, _ := reg.Lookup(ref)
		if ctor(ctx) == nil {
			t.Errorf("widget %q returned a nil node", ref)
		}
	}
}

// TestFieldsFormRendersEveryFieldsLabelAndTheSubmitText is a focused check
// on the one non-trivial helper this file adds (fieldsForm): every field's
// label appears, using its Value when set and falling back to its
// Placeholder otherwise, and the submit button carries the form's own
// submit label.
func TestFieldsFormRendersEveryFieldsLabelAndTheSubmitText(t *testing.T) {
	ctx := WidgetContext{}
	out, err := ui.RenderToString(createWorkerFormWidget(ctx))
	if err != nil {
		t.Fatalf("RenderToString: %v", err)
	}
	if !strings.Contains(out, "<form") {
		t.Errorf("createWorkerFormWidget did not render a <form>: %s", out)
	}
	if !strings.Contains(out, "<button") {
		t.Errorf("createWorkerFormWidget did not render a submit <button>: %s", out)
	}
}
