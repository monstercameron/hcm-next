package productui

import (
	"strings"
	"testing"
)

// TestTodo_PROMOUX_001 is the PRIMARY matrix test. It proves the pure
// resolution and rendering rules the rest of PROMOUX-001 depends on:
// ResolvePromotionAvailability is exhaustive and never permissive by
// default, every non-eligible code carries a non-empty, locale-resolved
// reason, the People directory's empty-workflow fallback renders that
// reason instead of the bare "no available workflows" label the RED clause
// names, and the directory can filter itself to eligible-only workers
// (GREEN #1).
func TestTodo_PROMOUX_001(t *testing.T) {
	t.Run("resolver is exhaustive with no permissive default", func(t *testing.T) {
		cases := []struct {
			authorized, hasPath, activeConflict bool
			want                                PromotionAvailabilityCode
		}{
			// authorized=false always wins, regardless of the other two.
			{false, false, false, PromotionWithheld},
			{false, false, true, PromotionWithheld},
			{false, true, false, PromotionWithheld},
			{false, true, true, PromotionWithheld},
			// authorized=true: active conflict outranks a published path.
			{true, false, true, PromotionActiveConflict},
			{true, true, true, PromotionActiveConflict},
			// authorized=true, no conflict: path presence decides.
			{true, false, false, PromotionIneligible},
			{true, true, false, PromotionEligible},
		}
		for _, tc := range cases {
			if got := ResolvePromotionAvailability(tc.authorized, tc.hasPath, tc.activeConflict); got != tc.want {
				t.Errorf("ResolvePromotionAvailability(%v, %v, %v) = %q, want %q", tc.authorized, tc.hasPath, tc.activeConflict, got, tc.want)
			}
		}
	})

	t.Run("every non-eligible code carries a reason, eligible carries none", func(t *testing.T) {
		locale := ResolveProductLocale("")
		if reason := PromotionAvailabilityReason(locale, PromotionEligible); reason != "" {
			t.Errorf("PromotionEligible reason = %q, want empty", reason)
		}
		seen := map[string]bool{}
		for _, code := range []PromotionAvailabilityCode{PromotionIneligible, PromotionActiveConflict, PromotionWithheld, PromotionAvailabilityCode("some-future-code")} {
			reason := PromotionAvailabilityReason(locale, code)
			if reason == "" {
				t.Errorf("code %q resolved an empty reason; a non-eligible code must never render blank", code)
			}
			if strings.HasPrefix(reason, "⟦") {
				t.Errorf("code %q resolved an unlocalized key marker %q", code, reason)
			}
			seen[reason] = true
		}
		// PromotionWithheld and an unrecognized future code intentionally
		// share the exact same reason (fail-closed to the generic,
		// non-revealing text) -- so among the three distinct codes tested,
		// only two texts are unique (ineligible, active_conflict) plus the
		// one shared withheld/unknown text.
		if len(seen) != 3 {
			t.Fatalf("distinct reason texts = %d, want 3 (ineligible, active_conflict, withheld/unknown shared)", len(seen))
		}
	})

	t.Run("empty workflow menu renders the reason, not the bare fallback", func(t *testing.T) {
		view := testView(PagePeople)
		view.People = []Person{{ID: "worker-ineligible", Name: "Ineligible Worker", PromotionAvailability: PromotionIneligible}}
		view.PersonWorkflows = []PersonWorkflow{{ID: "promotion", Name: "Promotion", LaunchHref: func(id string) string { return "/workspace/app/journeys?mode=new&worker=" + id }}}
		doc, err := Render(view)
		if err != nil {
			t.Fatal(err)
		}
		want := ResolveProductLocale("").Text("workflow.no_promotion_path")
		if !strings.Contains(doc, want) {
			t.Fatalf("rendered directory does not carry the ineligible reason %q", want)
		}
		if strings.Contains(doc, ResolveProductLocale("").Text("people.no_workflows")) {
			t.Fatal("rendered directory fell back to the bare, unexplained label despite a resolved availability code")
		}
	})

	t.Run("directory filters to eligible-only workers (GREEN #1)", func(t *testing.T) {
		view := testView(PagePeople)
		view.People = []Person{
			{ID: "worker-eligible", Name: "Eligible Worker", PromotionAvailability: PromotionEligible, Team: "Product", Location: "Toronto"},
			{ID: "worker-ineligible", Name: "Ineligible Worker", PromotionAvailability: PromotionIneligible, Team: "Product", Location: "Toronto"},
			{ID: "worker-conflict", Name: "Conflicted Worker", PromotionAvailability: PromotionActiveConflict, Team: "Product", Location: "Toronto"},
		}
		view.PeopleEligibleOnly = true
		filtered := filteredPeople(view)
		if len(filtered) != 1 || filtered[0].ID != "worker-eligible" {
			t.Fatalf("eligible-only filter = %+v, want exactly the one eligible worker", filtered)
		}
		view.PeopleEligibleOnly = false
		if got := len(filteredPeople(view)); got != 3 {
			t.Fatalf("unfiltered population = %d, want all 3", got)
		}
	})
}

// TestTodo_PROMOUX_001_Security is the SECURITY matrix test. It proves the
// negative case GREEN #4 calls out by name: an unauthorized viewer's
// rendered reason must not distinguish an ineligible worker from a
// conflicted one from a genuinely withheld one -- otherwise the reason text
// itself would be an enumeration channel telling that viewer things exist
// (an active promotion, a real ladder gap) that their authority does not
// entitle them to learn about this specific worker. An authorized viewer,
// by contrast, must see the three real reasons differ.
func TestTodo_PROMOUX_001_Security(t *testing.T) {
	// Four role-diverse workers exercising all four codes (GREEN #4): a
	// server that had already resolved eligible/ineligible/active-conflict
	// per worker before checking viewer authority.
	underlyingCodes := []PromotionAvailabilityCode{PromotionEligible, PromotionIneligible, PromotionActiveConflict}

	t.Run("unauthorized viewer sees one indistinguishable reason for every worker", func(t *testing.T) {
		view := testView(PagePeople)
		view.EffectivePermissions = []RolePagePermission{{Page: PageJourneys, View: true, Create: false}}
		view.PersonWorkflows = []PersonWorkflow{{ID: "promotion", Name: "Promotion", LaunchHref: func(id string) string { return "/workspace/app/journeys?mode=new&worker=" + id }}}
		reasons := map[string]bool{}
		for _, underlying := range underlyingCodes {
			// The client resolves the effective code the same way
			// tools/uxqual/productclient does: authorized wins first.
			effective := ResolvePromotionAvailability(false, underlying == PromotionEligible, underlying == PromotionActiveConflict)
			view.People = []Person{{ID: "worker-under-test", Name: "Under Test", PromotionAvailability: effective}}
			rows := peopleRowProps(view, peoplePageWindow{People: view.People, Total: 1, PageCount: 1, Page: 1})
			if len(rows) != 1 {
				t.Fatalf("underlying=%q: rows = %d, want 1", underlying, len(rows))
			}
			if len(rows[0].QuickActions) != 0 {
				t.Fatalf("underlying=%q: unauthorized viewer received a launchable action", underlying)
			}
			if rows[0].WorkflowsUnavailableReason == "" {
				t.Fatalf("underlying=%q: unauthorized viewer received no reason at all", underlying)
			}
			reasons[rows[0].WorkflowsUnavailableReason] = true
		}
		if len(reasons) != 1 {
			t.Fatalf("unauthorized viewer saw %d distinct reasons across ineligible/active-conflict/eligible-but-denied workers, want exactly 1: %v", len(reasons), reasons)
		}
	})

	t.Run("authorized viewer sees the real reasons differ", func(t *testing.T) {
		view := testView(PagePeople)
		view.EffectivePermissions = []RolePagePermission{{Page: PageJourneys, View: true, Create: true}}
		view.PersonWorkflows = []PersonWorkflow{{ID: "promotion", Name: "Promotion", LaunchHref: func(id string) string { return "/workspace/app/journeys?mode=new&worker=" + id }}}
		view.People = []Person{
			{ID: "worker-eligible", Name: "Eligible", PromotionAvailability: ResolvePromotionAvailability(true, true, false)},
			{ID: "worker-ineligible", Name: "Ineligible", PromotionAvailability: ResolvePromotionAvailability(true, false, false)},
			{ID: "worker-conflict", Name: "Conflicted", PromotionAvailability: ResolvePromotionAvailability(true, true, true)},
		}
		rows := peopleRowProps(view, peoplePageWindow{People: view.People, Total: 3, PageCount: 1, Page: 1})
		if len(rows) != 3 {
			t.Fatalf("rows = %d, want 3", len(rows))
		}
		if len(rows[0].QuickActions) != 1 {
			t.Fatalf("eligible worker did not receive the launchable promotion action: %+v", rows[0])
		}
		reasons := map[string]bool{rows[1].WorkflowsUnavailableReason: true, rows[2].WorkflowsUnavailableReason: true}
		if rows[1].WorkflowsUnavailableReason == "" || rows[2].WorkflowsUnavailableReason == "" {
			t.Fatalf("authorized viewer lost a reason: ineligible=%q conflict=%q", rows[1].WorkflowsUnavailableReason, rows[2].WorkflowsUnavailableReason)
		}
		if len(reasons) != 2 {
			t.Fatalf("authorized viewer saw indistinguishable reasons for ineligible vs active-conflict: %v", reasons)
		}
	})
}

// TestTodo_PROMOUX_001_Regression proves the pre-existing authorized-and-
// eligible path this todo must not disturb -- an explicitly eligible worker
// still gets a launchable promotion quick action with its original href and
// accessible label -- and, in the same test, that the zero value fails
// closed.
//
// The zero value matters because PromotionAvailabilityCode is a string: a
// Person built by a future page, provider or partial projection that never
// sets the field would otherwise render a launchable promotion for a worker
// the server never evaluated, and GREEN requires every displayed
// availability state to carry a server-provided reason. An unevaluated
// worker has no such verdict, so it must render as unavailable with the
// same generic reason a withheld worker gets -- never as available.
func TestTodo_PROMOUX_001_Regression(t *testing.T) {
	view := testView(PagePeople)
	view.People = []Person{
		{ID: "worker-avery", Name: "Avery Patel", PromotionAvailability: PromotionEligible},
		{ID: "worker-unset", Name: "No Server Verdict"},
	}
	rows := peopleRowProps(view, peoplePageWindow{People: view.People, Total: 2, PageCount: 1, Page: 1})
	if len(rows) != 2 {
		t.Fatalf("rows = %d, want 2", len(rows))
	}

	eligible, unset := rows[0], rows[1]
	if eligible.WorkflowsUnavailableReason != "" {
		t.Errorf("explicitly eligible worker carried an unavailable reason: %q", eligible.WorkflowsUnavailableReason)
	}
	found := false
	for _, action := range eligible.QuickActions {
		if action.Label == "Promotion" && action.Href == "/workspace/app/journeys?mode=new&worker="+eligible.ID {
			found = true
		}
	}
	if !found {
		t.Errorf("eligible worker lost its promotion quick action href shape: %+v", eligible.QuickActions)
	}

	for _, action := range unset.QuickActions {
		if action.Label == "Promotion" {
			t.Fatalf("a worker with no server verdict was offered a launchable promotion (%q); the zero value must fail closed", action.Href)
		}
	}
	if unset.WorkflowsUnavailableReason == "" {
		t.Fatal("a worker with no server verdict rendered no reason at all; every availability state must carry one")
	}
	if unset.WorkflowsUnavailableReason != PromotionAvailabilityReason(view.Locale, PromotionWithheld) {
		t.Errorf("unset worker's reason = %q, want the same generic reason a withheld worker gets so the zero value reveals nothing",
			unset.WorkflowsUnavailableReason)
	}
}

// TestTodo_PROMOUX_001_Browser is the BROWSER matrix entry. The section
// preamble requires direct browser evidence against the real server-backed
// UI, and that evidence is recorded in this todo's Evidence line: the People
// directory was driven in a real browser at
// http://127.0.0.1:8080/workspace/app/people, showing the promotion-eligible
// filter, workflow menus on eligible workers and a server-provided reason on
// the rest, with ?eligible=1 narrowing the directory to workers that all
// offer a workflow. tools/uxqual/browser/promoux001_people_eligibility.spec.mjs
// automates that run.
//
// What this Go test adds is the part a spec cannot give the traceability
// gate: it pins the browser-observable contract in the rendered markup, so
// the spec cannot silently start asserting against a page that no longer
// emits these controls. It fails if the filter loses its form field name,
// if the eligible control stops being a checkbox the browser will submit, or
// if an unavailable worker renders without a reason for the browser to show.
func TestTodo_PROMOUX_001_Browser(t *testing.T) {
	view := testView(PagePeople)
	view.People = []Person{
		{ID: "worker-eligible", Name: "Eligible", PromotionAvailability: PromotionEligible},
		{ID: "worker-ineligible", Name: "Ineligible", PromotionAvailability: PromotionIneligible},
	}
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}

	// The filter control the browser submits. name= is what puts eligible=1
	// on the query string; without it the filter cannot round-trip.
	for _, want := range []string{
		`name="eligible"`,
		`id="people-eligible-filter"`,
		`type="checkbox"`,
	} {
		if !strings.Contains(doc, want) {
			t.Errorf("rendered People page is missing the browser-submittable eligible filter fragment %q", want)
		}
	}

	// An unavailable worker must carry text a browser can show. The People
	// directory's Actions column is hydrated client-side, so the reason is
	// asserted on the row props the client renders from rather than on the
	// SSR document -- that is genuinely where this contract lives, and
	// claiming otherwise would make this test pass for the wrong reason.
	rows := peopleRowProps(view, peoplePageWindow{People: view.People, Total: 2, PageCount: 1, Page: 1})
	if len(rows) != 2 {
		t.Fatalf("rows = %d, want 2", len(rows))
	}
	reason := view.Locale.Text("workflow.no_promotion_path")
	if reason == "" {
		t.Fatal("workflow.no_promotion_path resolved to empty text; the browser would show nothing")
	}
	if rows[1].WorkflowsUnavailableReason != reason {
		t.Errorf("ineligible worker's browser-visible reason = %q, want %q", rows[1].WorkflowsUnavailableReason, reason)
	}
	if bare := view.Locale.Text("people.no_workflows"); rows[1].WorkflowsUnavailableReason == bare {
		t.Errorf("ineligible worker still renders the bare %q fallback RED names", bare)
	}
	if len(rows[0].QuickActions) == 0 {
		t.Error("eligible worker rendered no launchable action for the browser to click")
	}
}
