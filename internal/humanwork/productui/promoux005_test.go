package productui

// PROMOUX-005: "Include reporting-line and organization impact in
// management promotions."
//
// TestTodo_PROMOUX_005_Browser proves the review card renders GREEN's four
// facts -- manager, organization, position and the affected direct-report
// scope -- plus the cycle-safety verdict, from an already-resolved
// promotion.ManagementImpact. Like PROMOUX-003's approval disposition card,
// this is asserted through ui.RenderToString on the same server-rendered
// path the page uses, because promotion review content is server-rendered
// rather than hydrated client-side.

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/people"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/promotion"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func promoux005ReviewTarget() promotion.TargetPlacement {
	return promotion.TargetPlacement{JobCode: "ENG-MGR", Grade: "M2", OrgUnit: "engineering", PositionID: "POS-ENG-MGR-1"}
}

func promoux005ReviewWorker(t testing.TB, id string) values.EntityRef {
	t.Helper()
	return values.EntityRef{Tenant: "promoux005-review-tenant", Kind: people.KindWorker, Id: id}
}

func TestTodo_PROMOUX_005_Browser(t *testing.T) {
	locale := ResolveProductLocale("en-US")

	t.Run("a certified cycle-safe management change shows manager, organization, position and affected scope", func(t *testing.T) {
		impact := promotion.ManagementImpact{
			TargetManager: promoux005ReviewWorker(t, "10000000-0000-4000-8000-000000000001"),
			AffectedDirectReports: []values.EntityRef{
				promoux005ReviewWorker(t, "20000000-0000-4000-8000-000000000002"),
				promoux005ReviewWorker(t, "30000000-0000-4000-8000-000000000003"),
			},
			CycleSafe: true,
		}
		props := PromotionReviewPropsFrom(locale, promoux005ReviewTarget(), impact)
		if !props.HasTargetManager || props.AffectedDirectReportsCount != 2 || !props.CycleSafe {
			t.Fatalf("props = %+v, want a target manager, two affected reports and cycle-safe", props)
		}

		markup, err := ui.RenderToString(PromotionReview(locale, props))
		if err != nil {
			t.Fatalf("render PromotionReview: %v", err)
		}
		for _, want := range []string{
			"Reports to " + impact.TargetManager.Id,
			"Organization: engineering",
			"Position: POS-ENG-MGR-1",
			"2 direct reports will move",
			"No reporting-cycle conflicts found",
		} {
			if !strings.Contains(markup, want) {
				t.Fatalf("PromotionReview markup missing %q: %s", want, markup)
			}
		}
		if strings.Contains(markup, "could not be certified") {
			t.Fatalf("a certified-safe review must not also render the unsafe verdict: %s", markup)
		}
	})

	t.Run("an uncertified or cyclical change renders the unsafe verdict, not silence", func(t *testing.T) {
		impact := promotion.ManagementImpact{TargetManager: promoux005ReviewWorker(t, "10000000-0000-4000-8000-000000000001"), CycleSafe: false}
		props := PromotionReviewPropsFrom(locale, promoux005ReviewTarget(), impact)
		markup, err := ui.RenderToString(PromotionReview(locale, props))
		if err != nil {
			t.Fatalf("render PromotionReview: %v", err)
		}
		if !strings.Contains(markup, "This change could not be certified free of a reporting cycle") {
			t.Fatalf("PromotionReview markup does not surface the unsafe verdict: %s", markup)
		}
		if strings.Contains(markup, "No reporting-cycle conflicts found") {
			t.Fatal("an uncertified change must not also render the safe verdict")
		}
		if !strings.Contains(markup, "No direct reports are affected") {
			t.Fatalf("a promotion with no declared affected reports must say so explicitly, not render silence: %s", markup)
		}
	})

	t.Run("no target-manager selection renders a complete, non-misleading review rather than blank fields", func(t *testing.T) {
		// This is exactly the shape a refused evaluation
		// (evaluateTargetManagerSelection) leaves behind: the zero value.
		// The review must never imply a manager or scope was resolved when
		// none was, and must never leak a value from a refused selection.
		props := PromotionReviewPropsFrom(locale, promoux005ReviewTarget(), promotion.ManagementImpact{})
		if props.HasTargetManager || props.AffectedDirectReportsCount != 0 {
			t.Fatalf("props = %+v, want no target manager and zero affected reports for an unevaluated impact", props)
		}
		markup, err := ui.RenderToString(PromotionReview(locale, props))
		if err != nil {
			t.Fatalf("render PromotionReview: %v", err)
		}
		for _, want := range []string{"No target manager selected", "No direct reports are affected"} {
			if !strings.Contains(markup, want) {
				t.Fatalf("PromotionReview markup missing %q: %s", want, markup)
			}
		}
		for _, mustNotContain := range []string{"Reports to", "reporting-cycle", "certified"} {
			if strings.Contains(markup, mustNotContain) {
				t.Fatalf("an unevaluated review must not render a cycle-safety verdict for a manager it never checked: %s", markup)
			}
		}
	})
}
