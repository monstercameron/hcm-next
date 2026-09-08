package productui

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	intentsv1 "github.com/monstercameron/hcm-next/gen/go/hcmnext/intents/v1"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

func canonicalStatusFixture() StatusProjection {
	return CompleteStatusProjection(&intentsv1.LifecycleDimensions{
		Request:     intentsv1.RequestState_REQUEST_STATE_APPROVED,
		Execution:   intentsv1.ExecutionState_EXECUTION_STATE_REPAIR_REQUIRED,
		Business:    intentsv1.BusinessState_BUSINESS_STATE_COMPLETED,
		Consistency: intentsv1.ConsistencyState_CONSISTENCY_STATE_DEGRADED,
		Obligation:  intentsv1.ObligationState_OBLIGATION_STATE_OVERDUE,
	})
}

func statusFixture(locale string, projection StatusProjection) (string, error) {
	return ui.RenderToString(ui.CreateElement(StatusPresentation, StatusPresentationProps{
		I18nProps: I18nProps{Locale: ResolveProductLocale(locale)}, IDSeed: "journey-42-status", Projection: projection,
	}))
}

func TestTodo_WEB_021(t *testing.T) {
	markup, err := statusFixture("en-US", canonicalStatusFixture())
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`role="group"`, `aria-label="Intent lifecycle status; Request: Approved; Execution: Repair required; Business outcome: Completed; Consistency: Degraded; Obligations: Overdue"`, `<ul class="status-dimension-list">`,
		`data-status-dimension="request"`, `data-status-dimension="execution"`,
		`data-status-dimension="business"`, `data-status-dimension="consistency"`,
		`data-status-dimension="obligation"`,
		`Request`, `Approved`, `Execution`, `Repair required`, `Business outcome`, `Completed`,
		`Consistency`, `Degraded`, `Obligations`, `Overdue`, `aria-hidden="true"`,
	} {
		if !strings.Contains(markup, want) {
			t.Fatalf("canonical status contract missing %q in %s", want, markup)
		}
	}
	if strings.Count(markup, `data-status-dimension=`) != 5 || strings.Contains(markup, "journey-42-status") {
		t.Fatal("status dimensions were flattened, omitted, or leaked the caller's identifier")
	}
}

func TestTodo_WEB_021_Golden(t *testing.T) {
	first, err := statusFixture("en-US", canonicalStatusFixture())
	if err != nil {
		t.Fatal(err)
	}
	second, err := statusFixture("en-US", canonicalStatusFixture())
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("status presentation is not deterministic")
	}
	wantID := `id="` + stableStatusID("journey-42-status") + `"`
	if strings.Count(first, wantID) != 1 || strings.Count(first, `<li class="status-dimension`) != 5 {
		t.Fatalf("stable identity or five-item list missing from %s", first)
	}
}

func TestTodo_WEB_021_Browser(t *testing.T) {
	doc, err := Render(testView(PageWork))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`data-status-dimension="request"`, `data-status-dimension="obligation"`,
		`grid-template-columns:repeat(5,minmax(5.3rem,1fr))`, `@media (max-width:1190px)`,
		`@media (max-width:760px)`, `@media (max-width:420px)`,
		`@media (prefers-reduced-motion:reduce)`, `@media (forced-colors:active)`,
		`CanvasText`, `var(--hcm-color-danger`, `.preview-head>.status-dimensions{grid-column:1 / -1;}`,
		`.status-dimension-name{color:var(--muted);`, `.preview-head .status-dimension-list{grid-template-columns:repeat(2,minmax(0,1fr));}`,
	} {
		if !strings.Contains(doc, want) {
			t.Fatalf("production document missing status browser contract %q", want)
		}
	}
}

func TestTodo_WEB_021_Conformance(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		markup, err := statusFixture(locale, canonicalStatusFixture())
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(markup, "status.") || strings.Contains(markup, "⟦") || strings.Count(markup, `data-status-dimension=`) != 5 {
			t.Fatalf("%s leaked a key or lost a canonical dimension: %s", locale, markup)
		}
	}
	css := Stylesheet()
	for _, forbidden := range []string{".status-dimension{color:#", "transition:all"} {
		if strings.Contains(css, forbidden) {
			t.Fatalf("status styles bypassed semantic or reduced-motion tokens with %q", forbidden)
		}
	}
}

func TestStatusProjectionDoesNotInferMissingOrWithheldTruth(t *testing.T) {
	missing, err := statusFixture("en-US", StatusProjection{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(missing, "Lifecycle status is not available in this view") || strings.Contains(missing, `data-status-dimension=`) {
		t.Fatalf("omitted projection was treated as lifecycle truth: %s", missing)
	}

	projection := StatusProjection{
		Available:   true,
		Request:     values.Value(intentsv1.RequestState_REQUEST_STATE_SUBMITTED),
		Execution:   values.Redacted[intentsv1.ExecutionState]("principal=secret;authority=secret"),
		Business:    values.Unknown[intentsv1.BusinessState]("protected observation"),
		Consistency: values.Unavailable[intentsv1.ConsistencyState]("provider incident 42"),
		Obligation:  values.Absent[intentsv1.ObligationState](),
	}
	markup, err := statusFixture("en-US", projection)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Submitted", "Restricted", "Unknown", "Unavailable", "Not supplied"} {
		if !strings.Contains(markup, want) {
			t.Fatalf("presence disposition %q missing: %s", want, markup)
		}
	}
	for _, secret := range []string{"principal=secret", "authority=secret", "protected observation", "provider incident 42"} {
		if strings.Contains(markup, secret) {
			t.Fatalf("presence reason leaked into markup: %q", secret)
		}
	}
}

func TestStatusProjectionPreservesContradictionsAndFlagsInvalidEnums(t *testing.T) {
	projection := CompleteStatusProjection(&intentsv1.LifecycleDimensions{
		Request:     intentsv1.RequestState_REQUEST_STATE_CANCELLED,
		Execution:   intentsv1.ExecutionState_EXECUTION_STATE_EXECUTING,
		Business:    intentsv1.BusinessState_BUSINESS_STATE_UNKNOWN,
		Consistency: intentsv1.ConsistencyState_CONSISTENCY_STATE_REPAIRING,
		Obligation:  intentsv1.ObligationState_OBLIGATION_STATE_PENDING,
	})
	projection.Request = values.Value(intentsv1.RequestState(999))
	markup, err := statusFixture("en-US", projection)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Invalid or unspecified", "Executing", "Unknown", "Repairing", "Pending", "status-tone-invalid"} {
		if !strings.Contains(markup, want) {
			t.Fatalf("independent or invalid dimension %q missing: %s", want, markup)
		}
	}
	if strings.Contains(markup, "999") {
		t.Fatal("unknown service enum escaped into markup")
	}
}

func BenchmarkStatusPresentationRendering(b *testing.B) {
	node := ui.CreateElement(StatusPresentation, StatusPresentationProps{
		I18nProps: I18nProps{Locale: ResolveProductLocale("en-US")}, IDSeed: "benchmark-status", Projection: canonicalStatusFixture(),
	})
	b.ReportAllocs()
	for b.Loop() {
		if _, err := ui.RenderToString(node); err != nil {
			b.Fatal(err)
		}
	}
}
