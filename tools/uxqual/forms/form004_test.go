package forms

import (
	"os"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/monstercameron/hcm-next/tools/uxqual/qual"
	"github.com/monstercameron/hcm-next/tools/uxqual/render/gwc"
	"github.com/monstercameron/hcm-next/tools/uxqual/render/ssr"
	"github.com/monstercameron/hcm-next/tools/uxqual/testdata"
)

// TestTodo_FORM_004 is the PRIMARY test for planning/todos.md FORM-004:
// "Prove form accessibility and equivalent human route."
//
// RED (todos.md FORM-004): "keyboard/focus/label/error/AT/zoom/reflow/locale
// fixture blocks task completion or no equivalent governed route exists."
//
// GREEN (todos.md FORM-004): "WCAG evidence and assisted/manual route
// preserve same validation, authority and audit semantics."
func TestTodo_FORM_004(t *testing.T) {
	fixture := FixtureWithValidationError()

	ssrDoc, err := ssr.Render(fixture)
	if err != nil {
		t.Fatalf("ssr.Render: %v", err)
	}
	gwcDoc, err := gwc.Document(fixture)
	if err != nil {
		t.Fatalf("gwc.Document: %v", err)
	}

	maskedNeedles := testdata.MaskedNeedles()

	t.Run("SSR: the accessible route passes every reused and new criterion", func(t *testing.T) {
		results := []qual.CriterionResult{
			qual.CheckKeyboard(ssrDoc),
			qual.CheckScreenReaderSemantics(ssrDoc),
			qual.CheckContrastAA(),
			qual.CheckReflow(qual.ExtractInlineCSS(ssrDoc)),
			qual.CheckMasking(ssrDoc, maskedNeedles),
			CheckRequiredFieldSemantics(ssrDoc, RequiredFieldIDs),
			CheckErrorAssociation(ssrDoc, ErroredFieldIDs),
		}
		for _, r := range results {
			t.Logf("[ssr] %s: pass=%v (%s)", r.Name, r.Pass, r.Detail)
			if !r.Pass {
				t.Errorf("SSR failed %q, FORM-004's accessible route must always pass: %s", r.Name, r.Detail)
			}
		}
	})

	t.Run("GWC: documented gap in required-field semantics and error association", func(t *testing.T) {
		required := CheckRequiredFieldSemantics(gwcDoc, RequiredFieldIDs)
		errAssoc := CheckErrorAssociation(gwcDoc, ErroredFieldIDs)
		t.Logf("[gwc] %s: pass=%v (%s)", required.Name, required.Pass, required.Detail)
		t.Logf("[gwc] %s: pass=%v (%s)", errAssoc.Name, errAssoc.Pass, errAssoc.Detail)

		rec := readAccessibilityDecision(t)
		if rec.AccessibleRoute != "ssr" {
			t.Errorf("decision record accessible_route = %q, want %q", rec.AccessibleRoute, "ssr")
		}
		if rec.GWCKnownGaps.RequiredFieldSemantics != !required.Pass {
			t.Errorf("decision record gwc_known_gaps.required_field_semantics = %v, but GWC's observed pass=%v (record is stale -- re-run and update definitions/ux/forms/promotion-form-accessibility-decision.yaml)",
				rec.GWCKnownGaps.RequiredFieldSemantics, required.Pass)
		}
		if rec.GWCKnownGaps.ErrorAssociation != !errAssoc.Pass {
			t.Errorf("decision record gwc_known_gaps.error_association = %v, but GWC's observed pass=%v (record is stale -- re-run and update definitions/ux/forms/promotion-form-accessibility-decision.yaml)",
				rec.GWCKnownGaps.ErrorAssociation, errAssoc.Pass)
		}

		// GWC still must never leak the masked field/action, and must still
		// keep keyboard order/landmarks intact -- the gap is narrowly
		// required-field semantics and error association, not everything.
		for _, r := range []qual.CriterionResult{
			qual.CheckKeyboard(gwcDoc),
			qual.CheckScreenReaderSemantics(gwcDoc),
			qual.CheckMasking(gwcDoc, maskedNeedles),
		} {
			if !r.Pass {
				t.Errorf("GWC unexpectedly also failed %q (widening beyond the documented gap): %s", r.Name, r.Detail)
			}
		}
	})

	t.Run("RED: a broken document actually fails the two new criteria (not a rubber stamp)", func(t *testing.T) {
		broken := `<!doctype html><body><input id="proposedCompensation" name="proposedCompensation"></body></html>`
		if r := CheckRequiredFieldSemantics(broken, RequiredFieldIDs); r.Pass {
			t.Errorf("expected a document with no required/aria-required attributes to fail Required-field semantics")
		}
		if r := CheckErrorAssociation(broken, ErroredFieldIDs); r.Pass {
			t.Errorf("expected a document with no aria-invalid/aria-describedby to fail Error association")
		}
		// And a clean control with no error should not be flagged for a
		// field the caller did not ask it to check.
		clean := `<!doctype html><body><input id="other" name="other"></body></html>`
		if r := CheckErrorAssociation(clean, nil); !r.Pass {
			t.Errorf("Error association with an empty field list should trivially pass, got: %s", r.Detail)
		}
	})

	t.Run("RED: no equivalent governed route exists is checked structurally", func(t *testing.T) {
		route := PromotionEquivalentRoute()
		if route.Kind != "CAPABILITY_CALL" {
			t.Errorf("EquivalentRoute.Kind = %q, want CAPABILITY_CALL", route.Kind)
		}
		if route.CapabilityID == "" || route.CapabilityVersion == "" {
			t.Errorf("EquivalentRoute is missing a capability id/version: %+v", route)
		}
		if route.Description == "" {
			t.Errorf("EquivalentRoute has no human-readable description")
		}
	})
}

type accessibilityDecision struct {
	Todo            string `yaml:"todo"`
	AccessibleRoute string `yaml:"accessible_route"`
	ReviewBy        string `yaml:"review_by"`
	GWCKnownGaps    struct {
		RequiredFieldSemantics bool `yaml:"required_field_semantics"`
		ErrorAssociation       bool `yaml:"error_association"`
	} `yaml:"gwc_known_gaps"`
}

func readAccessibilityDecision(t *testing.T) accessibilityDecision {
	t.Helper()
	const path = "../../../definitions/ux/forms/promotion-form-accessibility-decision.yaml"
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read decision record: %v", err)
	}
	var rec accessibilityDecision
	if err := yaml.Unmarshal(raw, &rec); err != nil {
		t.Fatalf("parse decision record: %v", err)
	}
	if rec.Todo != "FORM-004" {
		t.Errorf("decision record todo = %q, want FORM-004", rec.Todo)
	}
	if rec.ReviewBy == "" {
		t.Errorf("decision record review_by must be set")
	}
	return rec
}
