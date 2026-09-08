package productui

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func validationFixture(locale string) (string, error) {
	i18n := I18nProps{Locale: ResolveProductLocale(locale)}
	issues := []ValidationIssue{
		{FieldID: "worker-prefix", Code: "required", MessageKey: "validation.required"},
		{FieldID: "worker-digits", Code: "range", MessageKey: "validation.unknown-key"},
	}
	return ui.RenderToString(html.Form(html.Props{Class: "validation-fixture"},
		ui.CreateElement(ValidationSummary, ValidationSummaryProps{I18nProps: i18n, ID: "fixture-validation-summary", Issues: issues, FieldIDs: []string{"worker-prefix", "worker-digits"}}),
		ValidationInput(ValidationInputProps{I18nProps: i18n, ID: "worker-prefix", Label: "Prefix", Help: "Up to 12 letters or digits.", Type: "text", Required: true, Issue: &issues[0]}),
		ValidationInput(ValidationInputProps{I18nProps: i18n, ID: "worker-digits", Label: "Maximum sequence digits", Help: "One to twelve digits.", Type: "number", Issue: &issues[1]}),
	))
}

func TestTodo_WEB_020(t *testing.T) {
	markup, err := validationFixture("en-US")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`id="fixture-validation-summary"`, `role="alert"`, `aria-live="assertive"`, `tabIndex="-1"`, `data-validation-focus="first-error"`,
		`href="#worker-prefix"`, `href="#worker-digits"`, `aria-invalid="true"`, `aria-errormessage="worker-prefix-error"`,
		`aria-describedby="worker-prefix-help worker-prefix-error"`, `id="worker-prefix-error"`, `Correct these items before continuing.`,
	} {
		if !strings.Contains(markup, want) {
			t.Fatalf("validation contract missing %q in %s", want, markup)
		}
	}
	if strings.Contains(markup, "validation.unknown-key") || strings.Contains(markup, "⟦") {
		t.Fatal("unresolved translation key leaked into validation presentation")
	}
	if strings.Index(markup, `id="worker-prefix"`) >= strings.Index(markup, `id="worker-prefix-help"`) {
		t.Fatal("field help must follow the control in document order")
	}
}

func TestTodo_WEB_020_Golden(t *testing.T) {
	first, err := validationFixture("en-US")
	if err != nil {
		t.Fatal(err)
	}
	second, err := validationFixture("en-US")
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("validation presentation is not deterministic")
	}
	if strings.Count(first, `id="fixture-validation-summary"`) != 1 || strings.Count(first, `id="worker-prefix-error"`) != 1 {
		t.Fatal("validation IDs are not emitted exactly once")
	}
}

func TestTodo_WEB_020_Browser(t *testing.T) {
	doc, err := Render(testView(PageWorkerIDs))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"validation-summary", "validation-field", "--hcm-color-danger", "--hcm-color-danger-surface",
		"grid-column:1 / -1;", "@media (prefers-reduced-motion:reduce)", "@media (forced-colors:active)", "CanvasText", "Mark",
	} {
		if !strings.Contains(doc, want) {
			t.Fatalf("production document missing validation browser contract %q", want)
		}
	}
}

func TestTodo_WEB_020_Conformance(t *testing.T) {
	de, err := validationFixture("de-DE")
	if err != nil {
		t.Fatal(err)
	}
	ar, err := validationFixture("ar")
	if err != nil {
		t.Fatal(err)
	}
	for name, markup := range map[string]string{"de-DE": de, "ar": ar} {
		if strings.Contains(markup, "validation.summary_title") || strings.Contains(markup, "validation.required") || strings.Contains(markup, "⟦") {
			t.Fatalf("%s validation markup leaked an unresolved message key", name)
		}
		if !strings.Contains(markup, `aria-describedby=`) || !strings.Contains(markup, `aria-errormessage=`) {
			t.Fatalf("%s validation markup dropped the field relationship", name)
		}
	}
	css := Stylesheet()
	for _, forbidden := range []string{".validation-field-error{color:#", ".validation-summary{color:#", "transition:all"} {
		if strings.Contains(css, forbidden) {
			t.Fatalf("validation styles bypassed semantic or reduced-motion tokens with %q", forbidden)
		}
	}
	if !strings.Contains(css, ".validation-summary:focus{outline:var(--hcm-focus-ring-width") {
		t.Fatal("summary focus contract missing")
	}
}

// TestValidationI18nGate keeps the reusable validation surface on reviewed
// catalogs and verifies that unknown server keys fail closed to safe copy.
func TestValidationI18nGate(t *testing.T) {
	for _, locale := range SupportedProductLocales() {
		message := validationIssueText(I18nProps{Locale: ResolveProductLocale(locale)}, ValidationIssue{MessageKey: "validation.unknown-key"})
		if message == "" || strings.Contains(message, "validation.") || strings.Contains(message, "⟦") {
			t.Fatalf("locale %s emitted unresolved validation copy %q", locale, message)
		}
	}
}

// TestValidationAccessibilityGate checks the relationships consumed by
// screen readers and the focus-safe summary/field keyboard order.
func TestValidationAccessibilityGate(t *testing.T) {
	markup, err := validationFixture("en-US")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`<label class="validation-field has-error" for="worker-prefix">`, `aria-invalid="true"`,
		`aria-describedby="worker-prefix-help worker-prefix-error"`, `aria-errormessage="worker-prefix-error"`,
		`role="alert"`, `aria-live="assertive"`, `aria-atomic="true"`, `tabIndex="-1"`,
	} {
		if !strings.Contains(markup, want) {
			t.Fatalf("accessible validation relationship missing %q", want)
		}
	}
	if strings.Count(markup, `aria-live="assertive"`) != 1 || strings.Count(markup, `role="alert"`) != 1 {
		t.Fatal("validation must expose exactly one assertive announcement at the summary")
	}
}

func TestValidationFiltersWarningsAndUnknownFieldTargets(t *testing.T) {
	i18n := I18nProps{Locale: ResolveProductLocale("en-US")}
	issues := []ValidationIssue{
		{FieldID: "worker-prefix", Message: "Prefix warning", Severity: ValidationSeverityWarning},
		{FieldID: "service-only-field", Message: "Service error", Severity: ValidationSeverityError},
	}
	markup, err := ui.RenderToString(ui.CreateElement(ValidationSummary, ValidationSummaryProps{
		I18nProps: i18n, ID: "summary", Issues: issues, FieldIDs: []string{"worker-prefix"},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(markup, "Prefix warning") {
		t.Fatal("warning leaked into the blocking error summary")
	}
	if !strings.Contains(markup, "Service error") || strings.Contains(markup, `href="#service-only-field"`) {
		t.Fatal("unknown service field must remain readable without a dangling focus target")
	}
	state := ValidationState{Issues: issues}
	if state.ForField("worker-prefix") != nil || len(state.Errors()) != 1 {
		t.Fatal("warning/error filtering is inconsistent")
	}
}

func TestValidationPrefersLocalizedKeyAndEscapesServiceFallback(t *testing.T) {
	i18n := I18nProps{Locale: ResolveProductLocale("de-DE")}
	localized := validationIssueText(i18n, ValidationIssue{MessageKey: "validation.required", Message: "English service fallback"})
	if localized != "Dieses Feld ist erforderlich." {
		t.Fatalf("localized key did not take precedence: %q", localized)
	}
	if got := validationIssueText(i18n, ValidationIssue{Message: "service.validation.required"}); got != i18n.Text("validation.generic_error") {
		t.Fatalf("raw service key leaked as copy: %q", got)
	}
	issue := ValidationIssue{Message: `<script>alert("x")</script>`}
	markup, err := ui.RenderToString(ValidationInput(ValidationInputProps{I18nProps: i18n, ID: "safe-field", Label: "Safe field", Type: "text", Issue: &issue}))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(markup, "<script>") || !strings.Contains(markup, "&lt;script&gt;") {
		t.Fatalf("service fallback was not escaped at the text boundary: %s", markup)
	}
}

func TestValidationRequiresStableComponentIDs(t *testing.T) {
	assertPanics := func(name string, render func()) {
		t.Helper()
		defer func() {
			if recover() == nil {
				t.Errorf("%s accepted a missing stable ID", name)
			}
		}()
		render()
	}
	assertPanics("input", func() { ValidationInput(ValidationInputProps{}) })
	assertPanics("summary", func() { ValidationSummary(ValidationSummaryProps{}) })
}

func TestWorkerIDProductionPageProjectsAndFocusesValidation(t *testing.T) {
	view := testView(PageWorkerIDs)
	view.WorkerIDValidation = ValidationState{SubmissionAttempted: true, Issues: []ValidationIssue{
		{FieldID: "worker-prefix", MessageKey: "validation.required"},
	}}
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`id="worker-id-validation-summary"`, `href="#worker-prefix"`, `aria-required="true"`, `required`,
	} {
		if !strings.Contains(doc, want) {
			t.Errorf("production worker-ID page missing %q", want)
		}
	}
}

func BenchmarkValidationRendering(b *testing.B) {
	i18n := I18nProps{Locale: ResolveProductLocale("en-US")}
	issue := ValidationIssue{FieldID: "worker-prefix", MessageKey: "validation.required"}
	node := html.Div(html.Props{Class: "validation-benchmark"},
		ui.CreateElement(ValidationSummary, ValidationSummaryProps{I18nProps: i18n, ID: "benchmark-validation-summary", Issues: []ValidationIssue{issue}, FieldIDs: []string{"worker-prefix"}}),
		ValidationInput(ValidationInputProps{I18nProps: i18n, ID: "worker-prefix", Label: "Prefix", Help: "Up to 12 letters or digits.", Type: "text", Issue: &issue}),
	)
	b.ReportAllocs()
	for b.Loop() {
		if _, err := ui.RenderToString(node); err != nil {
			b.Fatal(err)
		}
	}
}
