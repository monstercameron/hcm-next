package productui

// PROMOUX-007: "Present promotion validation as field-linked recoverable
// guidance."
//
// TestTodo_PROMOUX_007 proves the mapper itself: a rejected proposed pay
// value associates with the Proposed base pay field rather than a
// page-level message, a below/above-band finding quotes PROMOUX-006's own
// exact guardrail bound (or falls back safely when the guardrail is not
// available), changing one field's value clears only that field's stale
// error while a second invalid field's error survives untouched, and every
// finding code the promotion domain can emit today maps to its own reviewed
// copy rather than the fail-closed default -- which a dedicated subtest
// proves is reserved for a code this file does not recognise.
//
// TestTodo_PROMOUX_007_Golden pins the exact bytes of one summary link and
// one diagnostics fragment so unintentional copy or markup drift is caught.
//
// TestTodo_PROMOUX_007_Browser renders the mapped state through the shared
// WEB-125 ValidationSummary/ValidationInput components (the same
// server-rendered path promotion_review.go, approval_disposition.go and
// compensation_guardrail.go use) and proves an entered value survives being
// re-rendered alongside its own error.
//
// TestTodo_PROMOUX_007_Accessibility proves the real association contract:
// aria-describedby and aria-invalid tie each field to its own error, and
// every summary link's fragment target is a real id rendered in the same
// document -- while an issue for a field this document does not render
// stays readable but unlinked rather than becoming a dangling href.
//
// TestTodo_PROMOUX_007_I18N proves a message that quotes a guardrail bound
// is actually formatted for its locale: en-US and de-DE swap grouping and
// decimal separators inside the same sentence, and ar renders its own
// script rather than a copy of the English or German copy.
//
// TestTodo_PROMOUX_007_Security proves both halves of GREEN's diagnostics
// clause: ordinary copy carries none of RED's named leaks (field keys,
// decimal fractions, Go error names, correlation internals) even when a
// finding's own Message field is adversarially stuffed with them, while the
// support reference stays reachable through the separate diagnostics panel.

import (
	"regexp"
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/promotion"
)

// promoux007AllCodes is every promotion.Finding code this file's mapper
// must recognise explicitly. It is deliberately a literal, independently
// maintained list rather than one derived by reflection: exhaustiveness
// here means "every code promotion.go actually defines has its own case in
// promotionFindingMessage", and a test that generated this list from the
// switch itself could never catch a case the switch forgot.
var promoux007AllCodes = []string{
	promotion.CodeCurrentAmountInvalid,
	promotion.CodeProposedAmountInvalid,
	promotion.CodeCurrencyRequired,
	promotion.CodePayBasisRequired,
	promotion.CodeCurrencyChangeNotV0,
	promotion.CodeNotARaise,
	promotion.CodeBusinessReasonRequired,
	promotion.CodeEffectiveAtRequired,
	promotion.CodeEffectiveAtTooFarPast,
	promotion.CodeIncreaseOverThreshold,
	promotion.CodeSubjectNotDisclosable,
	promotion.CodeRequiredFieldDenied,
	promotion.CodeRequiredFieldUnavailabl,
	promotion.CodeWorkerNotActive,
	promotion.CodeTargetJobRequired,
	promotion.CodeTargetGradeRequired,
	promotion.CodeSameGrade,
	promotion.CodeEffectiveBeforeHire,
	promotion.CodePayBandNotFound,
	promotion.CodeBelowBandMinimum,
	promotion.CodeAboveBandMaximum,
	promotion.CodeBudgetAuthorityMissing,
	promotion.CodeBudgetObservationOnly,
	promotion.CodeBudgetObservedShort,
	promotion.CodeTargetManagerNotFound,
	promotion.CodeManagerRelationshipCycle,
	promotion.CodeManagerChainUnresolved,
	promotion.CodeTargetPositionNotFound,
	promotion.CodeTargetPositionIncompatible,
	promotion.CodeTargetPositionNotEffective,
	promotion.CodeTargetPositionAtCapacity,
	promotion.CodeTargetPositionReservationConflict,
}

func promoux007Finding(code, field string, severity promotion.Severity) promotion.Finding {
	return promotion.Finding{
		Code: code, Field: field, Severity: severity,
		// Message deliberately carries the exact kind of internal detail RED
		// forbids in ordinary copy. The mapper must never read it; several
		// subtests below assert none of this text ever appears in rendered
		// output.
		Message: "internal: field=" + field + " compa-ratio=0.417293 values.Money: invalid amount corr-8f14e45fceea167a5a36dedd4bea2543",
	}
}

func TestTodo_PROMOUX_007(t *testing.T) {
	locale := ResolveProductLocale("en-US")

	t.Run("a rejected proposed pay amount associates with the Proposed base pay field, not a page-level message", func(t *testing.T) {
		result := promotion.PreflightResult{Findings: []promotion.Finding{
			promoux007Finding(promotion.CodeProposedAmountInvalid, "proposed.base", promotion.SeverityBlocking),
		}}
		state, _ := MapPromotionRefusal(locale, result, promotion.CompensationGuardrail{})
		issue := state.ForField("proposed.base")
		if issue == nil {
			t.Fatal("proposed.base carries no issue; the rejected pay value was not associated with its field")
		}
		want := locale.Text("promotion_validation.proposed_amount_invalid")
		if issue.Message != want {
			t.Fatalf("issue.Message = %q, want %q", issue.Message, want)
		}
		if issue.Severity != ValidationSeverityError {
			t.Fatalf("issue.Severity = %q, want error", issue.Severity)
		}
	})

	t.Run("a below-band-minimum finding quotes the guardrail's own exact minimum, never a recomputation", func(t *testing.T) {
		guardrail := compensationGuardrailAvailable(t, "550000.00") // band minimum 500000.00 USD
		result := promotion.PreflightResult{Findings: []promotion.Finding{
			promoux007Finding(promotion.CodeBelowBandMinimum, "proposed.base", promotion.SeverityBlocking),
		}}
		state, _ := MapPromotionRefusal(locale, result, guardrail)
		issue := state.ForField("proposed.base")
		if issue == nil {
			t.Fatal("proposed.base carries no issue")
		}
		wantAmount := money(locale, guardrail.MinimumAnnualized)
		want := locale.Text("promotion_validation.below_band_minimum", map[string]string{"amount": wantAmount})
		if issue.Message != want {
			t.Fatalf("issue.Message = %q, want %q", issue.Message, want)
		}
		if !strings.Contains(issue.Message, wantAmount) {
			t.Fatalf("issue.Message %q does not quote the guardrail minimum %q", issue.Message, wantAmount)
		}
	})

	t.Run("an above-band-maximum finding quotes the guardrail's own exact maximum", func(t *testing.T) {
		guardrail := compensationGuardrailAvailable(t, "550000.00") // band maximum 650000.00 USD
		result := promotion.PreflightResult{Findings: []promotion.Finding{
			promoux007Finding(promotion.CodeAboveBandMaximum, "proposed.base", promotion.SeverityBlocking),
		}}
		state, _ := MapPromotionRefusal(locale, result, guardrail)
		issue := state.ForField("proposed.base")
		if issue == nil {
			t.Fatal("proposed.base carries no issue")
		}
		wantAmount := money(locale, guardrail.MaximumAnnualized)
		want := locale.Text("promotion_validation.above_band_maximum", map[string]string{"amount": wantAmount})
		if issue.Message != want {
			t.Fatalf("issue.Message = %q, want %q", issue.Message, want)
		}
	})

	t.Run("when the guardrail cannot be shown, the same band findings fall back to the bound-free generic message", func(t *testing.T) {
		unavailable := compensationGuardrailUnavailable(t, promotion.GuardrailReasonNotAuthorized)
		result := promotion.PreflightResult{Findings: []promotion.Finding{
			promoux007Finding(promotion.CodeBelowBandMinimum, "proposed.base", promotion.SeverityBlocking),
			promoux007Finding(promotion.CodeAboveBandMaximum, "current.base", promotion.SeverityBlocking),
		}}
		state, _ := MapPromotionRefusal(locale, result, unavailable)
		below := state.ForField("proposed.base")
		above := state.ForField("current.base")
		if below == nil || above == nil {
			t.Fatal("expected both fields to carry an issue")
		}
		if below.Message != locale.Text("promotion_validation.below_band_minimum_generic") {
			t.Fatalf("below.Message = %q, want the generic bound-free message", below.Message)
		}
		if above.Message != locale.Text("promotion_validation.above_band_maximum_generic") {
			t.Fatalf("above.Message = %q, want the generic bound-free message", above.Message)
		}
		for _, m := range []string{below.Message, above.Message} {
			for _, digit := range "0123456789" {
				if strings.ContainsRune(m, digit) {
					t.Fatalf("bound-free fallback message %q must not contain a digit", m)
				}
			}
		}
	})

	t.Run("changing one field clears only its own stale error and leaves a second invalid field untouched", func(t *testing.T) {
		result := promotion.PreflightResult{Findings: []promotion.Finding{
			promoux007Finding(promotion.CodeProposedAmountInvalid, "proposed.base", promotion.SeverityBlocking),
			promoux007Finding(promotion.CodeEffectiveAtTooFarPast, "effective_date", promotion.SeverityBlocking),
		}}
		state, _ := MapPromotionRefusal(locale, result, promotion.CompensationGuardrail{})
		before := state.ForField("effective_date")
		if before == nil {
			t.Fatal("effective_date must start with an issue")
		}
		beforeMessage := before.Message

		after := ClearFieldIssue(state, "proposed.base")

		if after.ForField("proposed.base") != nil {
			t.Fatal("proposed.base issue should have been cleared")
		}
		second := after.ForField("effective_date")
		if second == nil {
			t.Fatal("effective_date's issue was cleared by an edit to a different field -- this is exactly the over-broad clear GREEN forbids")
		}
		if second.Message != beforeMessage || second.Code != before.Code || second.FieldID != before.FieldID || second.Severity != before.Severity {
			t.Fatalf("effective_date's issue changed after clearing an unrelated field: before=%+v after=%+v", *before, *second)
		}
		if len(after.Issues) != 1 {
			t.Fatalf("len(after.Issues) = %d, want exactly 1 (proposed.base removed, effective_date kept)", len(after.Issues))
		}
	})

	t.Run("clearing a field with no issue, or an empty field id, changes nothing", func(t *testing.T) {
		result := promotion.PreflightResult{Findings: []promotion.Finding{
			promoux007Finding(promotion.CodeProposedAmountInvalid, "proposed.base", promotion.SeverityBlocking),
		}}
		state, _ := MapPromotionRefusal(locale, result, promotion.CompensationGuardrail{})
		if got := ClearFieldIssue(state, "effective_date"); len(got.Issues) != 1 {
			t.Fatalf("clearing an untouched field must not remove an unrelated issue: got %d issues", len(got.Issues))
		}
		if got := ClearFieldIssue(state, ""); len(got.Issues) != 1 {
			t.Fatalf("clearing an empty field id must be a no-op: got %d issues", len(got.Issues))
		}
	})

	t.Run("diagnostics is available only when the result actually blocks and carries a digest", func(t *testing.T) {
		blocking := promotion.PreflightResult{
			Findings:     []promotion.Finding{promoux007Finding(promotion.CodeProposedAmountInvalid, "proposed.base", promotion.SeverityBlocking)},
			ResultDigest: "sha256:deadbeefcafef00d",
		}
		_, diagnostics := MapPromotionRefusal(locale, blocking, promotion.CompensationGuardrail{})
		if !diagnostics.Available || diagnostics.SupportReference != "sha256:deadbeefcafef00d" {
			t.Fatalf("diagnostics = %+v, want available with the result's own digest", diagnostics)
		}

		advisoryOnly := promotion.PreflightResult{
			Findings:     []promotion.Finding{promoux007Finding(promotion.CodeIncreaseOverThreshold, "proposed.base", promotion.SeverityAdvisory)},
			ResultDigest: "sha256:deadbeefcafef00d",
		}
		_, advisoryDiagnostics := MapPromotionRefusal(locale, advisoryOnly, promotion.CompensationGuardrail{})
		if advisoryDiagnostics.Available {
			t.Fatal("an advisory-only result must not surface a support reference; nothing blocked")
		}

		noDigest := promotion.PreflightResult{
			Findings: []promotion.Finding{promoux007Finding(promotion.CodeProposedAmountInvalid, "proposed.base", promotion.SeverityBlocking)},
		}
		_, noDigestDiagnostics := MapPromotionRefusal(locale, noDigest, promotion.CompensationGuardrail{})
		if noDigestDiagnostics.Available {
			t.Fatal("a result with no digest must not manufacture a placeholder support reference")
		}
	})

	t.Run("every known finding code maps to its own reviewed copy, never the unrecognized fallback", func(t *testing.T) {
		guardrail := compensationGuardrailAvailable(t, "550000.00")
		fallback := locale.Text("promotion_validation.unrecognized_reason")
		seen := make(map[string]string, len(promoux007AllCodes))
		for _, code := range promoux007AllCodes {
			result := promotion.PreflightResult{Findings: []promotion.Finding{promoux007Finding(code, "x", promotion.SeverityBlocking)}}
			state, _ := MapPromotionRefusal(locale, result, guardrail)
			if len(state.Issues) != 1 {
				t.Fatalf("code %s: expected exactly one mapped issue", code)
			}
			message := state.Issues[0].Message
			if message == "" {
				t.Fatalf("code %s mapped to an empty message", code)
			}
			if message == fallback {
				t.Fatalf("code %s fell through to the unrecognized-reason fallback; it needs its own case in promotionFindingMessage", code)
			}
			seen[code] = message
		}
		if len(seen) != len(promoux007AllCodes) {
			t.Fatalf("expected %d distinct codes exercised, saw %d", len(promoux007AllCodes), len(seen))
		}
	})

	t.Run("a finding code this file does not recognise fails closed to the generic message, never the raw code or the finding's own text", func(t *testing.T) {
		adversarial := promotion.Finding{
			Code:  "some.totally.unknown.code.9x7",
			Field: "mystery.field",
			Message: "leak-this-go-error: crypto/rsa: verification error, compa-ratio=0.417293, " +
				"corr-8f14e45fceea167a5a36dedd4bea2543",
			Severity: promotion.SeverityBlocking,
		}
		result := promotion.PreflightResult{Findings: []promotion.Finding{adversarial}}
		state, _ := MapPromotionRefusal(locale, result, promotion.CompensationGuardrail{})
		if len(state.Issues) != 1 {
			t.Fatalf("expected exactly one mapped issue, got %d", len(state.Issues))
		}
		message := state.Issues[0].Message
		want := locale.Text("promotion_validation.unrecognized_reason")
		if message != want {
			t.Fatalf("message = %q, want the generic fallback %q", message, want)
		}
		for _, leak := range []string{"some.totally.unknown.code.9x7", "leak-this-go-error", "crypto/rsa", "0.417293", "corr-8f14e45fceea167a5a36dedd4bea2543"} {
			if strings.Contains(message, leak) {
				t.Fatalf("unrecognized-code fallback leaked %q into %q", leak, message)
			}
		}
	})
}

func TestTodo_PROMOUX_007_Golden(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	guardrail := compensationGuardrailAvailable(t, "550000.00")

	t.Run("the summary link for a below-band-minimum finding is pinned byte for byte", func(t *testing.T) {
		result := promotion.PreflightResult{Findings: []promotion.Finding{
			promoux007Finding(promotion.CodeBelowBandMinimum, "proposed.base", promotion.SeverityBlocking),
		}}
		state, _ := MapPromotionRefusal(locale, result, guardrail)
		markup, err := ui.RenderToString(ValidationSummary(ValidationSummaryProps{
			I18nProps: I18nProps{Locale: locale}, ID: "promoux007-golden-summary",
			Issues: state.Issues, FieldIDs: []string{"proposed.base"},
		}))
		if err != nil {
			t.Fatalf("render ValidationSummary: %v", err)
		}
		// The amount piece is built through the same [money] helper the
		// mapper itself calls (which formats "USD" and the digits with a
		// non-breaking space between them) so this pins the mapper's exact
		// sentence wording and punctuation without hardcoding an invisible
		// Unicode character in the test source.
		want := `<li><a href="#proposed.base">Proposed base pay must be at least ` +
			money(locale, guardrail.MinimumAnnualized) + `, the minimum for this role.</a></li>`
		if !strings.Contains(markup, want) {
			t.Fatalf("markup missing exact pinned fragment %q: %s", want, markup)
		}
	})

	t.Run("the diagnostics panel is pinned byte for byte for a fixed support reference", func(t *testing.T) {
		markup, err := ui.RenderToString(PromotionValidationDiagnosticsPanel(locale, PromotionValidationDiagnostics{
			Available: true, SupportReference: "sha256:deadbeefcafef00d",
		}))
		if err != nil {
			t.Fatalf("render PromotionValidationDiagnosticsPanel: %v", err)
		}
		const want = `<summary>Diagnostics</summary><label class="promotion-validation-diagnostics-label">Support reference<input class="promotion-validation-support-reference" readOnly type="text" value="sha256:deadbeefcafef00d"></label>`
		if !strings.Contains(markup, want) {
			t.Fatalf("markup missing exact pinned fragment %q: %s", want, markup)
		}
	})

	t.Run("rendering the same inputs twice produces byte-identical output", func(t *testing.T) {
		result := promotion.PreflightResult{Findings: []promotion.Finding{
			promoux007Finding(promotion.CodeBelowBandMinimum, "proposed.base", promotion.SeverityBlocking),
		}}
		render := func() string {
			state, _ := MapPromotionRefusal(locale, result, guardrail)
			markup, err := ui.RenderToString(ValidationSummary(ValidationSummaryProps{
				I18nProps: I18nProps{Locale: locale}, ID: "promoux007-golden-summary",
				Issues: state.Issues, FieldIDs: []string{"proposed.base"},
			}))
			if err != nil {
				t.Fatalf("render: %v", err)
			}
			return markup
		}
		first, second := render(), render()
		if first != second {
			t.Fatalf("non-deterministic render:\n%s\n---\n%s", first, second)
		}
	})
}

func TestTodo_PROMOUX_007_Browser(t *testing.T) {
	locale := ResolveProductLocale("en-US")

	t.Run("a field with an error still renders the value the user already entered", func(t *testing.T) {
		result := promotion.PreflightResult{Findings: []promotion.Finding{
			promoux007Finding(promotion.CodeBelowBandMinimum, "proposed.base", promotion.SeverityBlocking),
		}}
		guardrail := compensationGuardrailAvailable(t, "550000.00")
		state, _ := MapPromotionRefusal(locale, result, guardrail)
		issue := state.ForField("proposed.base")

		markup, err := ui.RenderToString(ValidationInput(ValidationInputProps{
			I18nProps: I18nProps{Locale: locale}, ID: "proposed.base", Label: "Proposed base pay",
			Type: "text", Value: "480000.00", Issue: issue,
		}))
		if err != nil {
			t.Fatalf("render ValidationInput: %v", err)
		}
		if !strings.Contains(markup, `value="480000.00"`) {
			t.Fatalf("entered value was lost when the field's error rendered: %s", markup)
		}
		if !strings.Contains(markup, "Proposed base pay must be at least") {
			t.Fatalf("field error text missing: %s", markup)
		}
	})

	t.Run("a full validation surface renders the summary and every linked field from one mapped state", func(t *testing.T) {
		guardrail := compensationGuardrailAvailable(t, "550000.00")
		result := promotion.PreflightResult{Findings: []promotion.Finding{
			promoux007Finding(promotion.CodeBelowBandMinimum, "proposed.base", promotion.SeverityBlocking),
			promoux007Finding(promotion.CodeEffectiveAtTooFarPast, "effective_date", promotion.SeverityBlocking),
		}, ResultDigest: "sha256:deadbeefcafef00d"}
		state, diagnostics := MapPromotionRefusal(locale, result, guardrail)
		fieldIDs := []string{"proposed.base", "effective_date"}

		markup, err := ui.RenderToString(html.Form(html.Props{},
			ui.CreateElement(ValidationSummary, ValidationSummaryProps{
				I18nProps: I18nProps{Locale: locale}, ID: "promoux007-summary", Issues: state.Issues, FieldIDs: fieldIDs,
			}),
			ValidationInput(ValidationInputProps{I18nProps: I18nProps{Locale: locale}, ID: "proposed.base", Label: "Proposed base pay", Type: "text", Value: "480000.00", Issue: state.ForField("proposed.base")}),
			ValidationInput(ValidationInputProps{I18nProps: I18nProps{Locale: locale}, ID: "effective_date", Label: "Effective date", Type: "date", Value: "2025-01-01", Issue: state.ForField("effective_date")}),
			PromotionValidationDiagnosticsPanel(locale, diagnostics),
		))
		if err != nil {
			t.Fatalf("render: %v", err)
		}
		for _, want := range []string{
			`href="#proposed.base"`, `href="#effective_date"`,
			`id="proposed.base"`, `id="effective_date"`,
			`value="480000.00"`, `value="2025-01-01"`,
			"sha256:deadbeefcafef00d",
		} {
			if !strings.Contains(markup, want) {
				t.Fatalf("full surface missing %q: %s", want, markup)
			}
		}
	})
}

func TestTodo_PROMOUX_007_Accessibility(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	guardrail := compensationGuardrailAvailable(t, "550000.00")

	result := promotion.PreflightResult{Findings: []promotion.Finding{
		promoux007Finding(promotion.CodeBelowBandMinimum, "proposed.base", promotion.SeverityBlocking),
		promoux007Finding(promotion.CodeEffectiveAtTooFarPast, "effective_date", promotion.SeverityBlocking),
		// worker.lifecycle_status is a real code path (CodeWorkerNotActive) but is
		// never rendered as an input control on this surface, so it must stay a
		// readable, UNLINKED summary entry rather than a dangling fragment link.
		promoux007Finding(promotion.CodeWorkerNotActive, "worker.lifecycle_status", promotion.SeverityBlocking),
	}}
	state, _ := MapPromotionRefusal(locale, result, guardrail)
	renderedFieldIDs := []string{"proposed.base", "effective_date"}

	markup, err := ui.RenderToString(html.Form(html.Props{},
		ui.CreateElement(ValidationSummary, ValidationSummaryProps{
			I18nProps: I18nProps{Locale: locale}, ID: "promoux007-a11y-summary", Issues: state.Issues, FieldIDs: renderedFieldIDs,
		}),
		ValidationInput(ValidationInputProps{I18nProps: I18nProps{Locale: locale}, ID: "proposed.base", Label: "Proposed base pay", Type: "text", Issue: state.ForField("proposed.base")}),
		ValidationInput(ValidationInputProps{I18nProps: I18nProps{Locale: locale}, ID: "effective_date", Label: "Effective date", Type: "date", Issue: state.ForField("effective_date")}),
	))
	if err != nil {
		t.Fatalf("render: %v", err)
	}

	t.Run("each invalid input is really associated with its own error", func(t *testing.T) {
		for _, id := range renderedFieldIDs {
			if !strings.Contains(markup, `id="`+id+`"`) {
				t.Fatalf("no rendered control with id %q", id)
			}
			if !strings.Contains(markup, `aria-describedby="`+id+`-error"`) {
				t.Fatalf("control %q is not described by its own error node", id)
			}
			if !strings.Contains(markup, `id="`+id+`-error"`) {
				t.Fatalf("described-by target %q-error does not exist in the document", id)
			}
		}
		if strings.Count(markup, `aria-invalid="true"`) != len(renderedFieldIDs) {
			t.Fatalf("expected exactly %d aria-invalid controls, markup: %s", len(renderedFieldIDs), markup)
		}
	})

	t.Run("every linked summary entry targets a real element id present in this same document", func(t *testing.T) {
		for _, id := range renderedFieldIDs {
			href := `href="#` + id + `"`
			if !strings.Contains(markup, href) {
				t.Fatalf("summary is missing a link to %q", id)
			}
			if !strings.Contains(markup, `id="`+id+`"`) {
				t.Fatalf("summary links to %q but no element with that id is rendered in this document -- a dangling link", id)
			}
		}
	})

	t.Run("an issue for a field this document never renders stays readable but is not a dangling link", func(t *testing.T) {
		if strings.Contains(markup, `href="#worker.lifecycle_status"`) {
			t.Fatal("worker.lifecycle_status has no rendered control, so the summary must not link to it")
		}
		// ui.RenderToString HTML-escapes text nodes (an apostrophe becomes
		// &#39;), so this checks a substring either side of the one
		// apostrophe in the reviewed copy rather than the exact Go string.
		if !strings.Contains(markup, "employment status does not allow a promotion right now") {
			t.Fatalf("the unlinked finding's own text must still be readable in the summary: %s", markup)
		}
	})
}

func TestTodo_PROMOUX_007_I18N(t *testing.T) {
	guardrail := compensationGuardrailAvailable(t, "550000.00") // band minimum 500000.00 USD

	enUS := ResolveProductLocale("en-US")
	deDE := ResolveProductLocale("de-DE")
	ar := ResolveProductLocale("ar")

	result := promotion.PreflightResult{Findings: []promotion.Finding{
		promoux007Finding(promotion.CodeBelowBandMinimum, "proposed.base", promotion.SeverityBlocking),
	}}

	enState, _ := MapPromotionRefusal(enUS, result, guardrail)
	deState, _ := MapPromotionRefusal(deDE, result, guardrail)
	arState, _ := MapPromotionRefusal(ar, result, guardrail)

	enMessage := enState.ForField("proposed.base").Message
	deMessage := deState.ForField("proposed.base").Message
	arMessage := arState.ForField("proposed.base").Message

	t.Run("the quoted bound's grouping and decimal separators actually swap between en-US and de-DE", func(t *testing.T) {
		if !strings.Contains(enMessage, "500,000.00") {
			t.Fatalf("en-US message missing comma-grouped, dot-decimal bound: %s", enMessage)
		}
		if !strings.Contains(deMessage, "500.000,00") {
			t.Fatalf("de-DE message missing dot-grouped, comma-decimal bound: %s", deMessage)
		}
		if strings.Contains(deMessage, "500,000.00") {
			t.Fatal("de-DE message used the en-US separator convention instead of its own")
		}
	})

	t.Run("the surrounding sentence itself is translated, not just the number", func(t *testing.T) {
		if enMessage == deMessage {
			t.Fatal("en-US and de-DE messages must differ beyond the quoted amount")
		}
		if strings.Contains(enMessage, "must be at least") && !strings.Contains(deMessage, "mindestens") {
			t.Fatalf("de-DE message does not read as a translation: %s", deMessage)
		}
	})

	t.Run("ar renders its own script and differs from the English and German copy", func(t *testing.T) {
		if ar.Direction != enUS.Direction && ar.Direction == "" {
			t.Fatal("ar LocaleContext.Direction must be resolved")
		}
		hasArabicScript := false
		for _, r := range arMessage {
			if r >= 0x0600 && r <= 0x06FF {
				hasArabicScript = true
				break
			}
		}
		if !hasArabicScript {
			t.Fatalf("ar message contains no Arabic-script characters: %s", arMessage)
		}
		if arMessage == enMessage || arMessage == deMessage {
			t.Fatal("ar message must not equal the English or German copy")
		}
	})
}

// promoux007StripTags removes markup so a "visible copy" assertion cannot be
// satisfied by text hiding inside an attribute value (an id or href, which
// legitimately carries the domain's raw field identity so the summary can
// link to it -- see promotion_validation.go's FieldID doc comment). This
// mirrors the technique TestTodo_PROMOUX_006_Security already established.
var promoux007TagPattern = regexp.MustCompile(`<[^>]*>`)

func promoux007StripTags(markup string) string {
	return promoux007TagPattern.ReplaceAllString(markup, " ")
}

func TestTodo_PROMOUX_007_Security(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	guardrail := compensationGuardrailAvailable(t, "550000.00")

	t.Run("ordinary copy carries none of RED's named leaks even when the finding's own Message is adversarial", func(t *testing.T) {
		result := promotion.PreflightResult{Findings: []promotion.Finding{
			promoux007Finding(promotion.CodeProposedAmountInvalid, "proposed.base", promotion.SeverityBlocking),
			promoux007Finding(promotion.CodeBelowBandMinimum, "current.base", promotion.SeverityBlocking),
		}, ResultDigest: "sha256:deadbeefcafef00d"}
		state, _ := MapPromotionRefusal(locale, result, guardrail)

		markup, err := ui.RenderToString(html.Form(html.Props{},
			ui.CreateElement(ValidationSummary, ValidationSummaryProps{
				I18nProps: I18nProps{Locale: locale}, ID: "promoux007-sec-summary", Issues: state.Issues,
				FieldIDs: []string{"proposed.base", "current.base"},
			}),
			ValidationInput(ValidationInputProps{I18nProps: I18nProps{Locale: locale}, ID: "proposed.base", Label: "Proposed base pay", Type: "text", Issue: state.ForField("proposed.base")}),
			ValidationInput(ValidationInputProps{I18nProps: I18nProps{Locale: locale}, ID: "current.base", Label: "Current base pay", Type: "text", Issue: state.ForField("current.base")}),
		))
		if err != nil {
			t.Fatalf("render: %v", err)
		}
		visible := promoux007StripTags(markup)
		for _, leak := range []string{
			"compa-ratio=0.417293",                  // decimal fraction
			"values.Money: invalid amount",          // Go error name
			"corr-8f14e45fceea167a5a36dedd4bea2543", // correlation internal
			"internal: field=",                      // raw internal message prefix
		} {
			if strings.Contains(visible, leak) {
				t.Fatalf("visible copy leaked %q: %s", leak, visible)
			}
		}
		// The domain's raw field key is a legitimate structural identifier for
		// the href/id attributes a field-linked summary requires, but must never
		// appear as READABLE prose. Checking the whole markup for the field key
		// would also fail because of `id="proposed.base"`, so this checks the
		// tag-stripped visible text only.
		if strings.Contains(visible, "proposed.base") || strings.Contains(visible, "current.base") {
			t.Fatalf("a raw field key leaked into visible copy: %s", visible)
		}
	})

	t.Run("the support reference is reachable through diagnostics but never mixed into ordinary copy", func(t *testing.T) {
		result := promotion.PreflightResult{Findings: []promotion.Finding{
			promoux007Finding(promotion.CodeProposedAmountInvalid, "proposed.base", promotion.SeverityBlocking),
		}, ResultDigest: "sha256:deadbeefcafef00d"}
		state, diagnostics := MapPromotionRefusal(locale, result, guardrail)
		if !diagnostics.Available {
			t.Fatal("diagnostics must be available: the result blocks and carries a digest")
		}

		ordinaryMarkup, err := ui.RenderToString(ValidationSummary(ValidationSummaryProps{
			I18nProps: I18nProps{Locale: locale}, ID: "promoux007-sec-ordinary", Issues: state.Issues,
			FieldIDs: []string{"proposed.base"},
		}))
		if err != nil {
			t.Fatalf("render ordinary summary: %v", err)
		}
		if strings.Contains(ordinaryMarkup, diagnostics.SupportReference) {
			t.Fatalf("ordinary copy leaked the support reference: %s", ordinaryMarkup)
		}

		diagnosticsMarkup, err := ui.RenderToString(PromotionValidationDiagnosticsPanel(locale, diagnostics))
		if err != nil {
			t.Fatalf("render diagnostics panel: %v", err)
		}
		if !strings.Contains(diagnosticsMarkup, diagnostics.SupportReference) {
			t.Fatalf("diagnostics panel does not carry the support reference at all: %s", diagnosticsMarkup)
		}
	})

	t.Run("an unavailable diagnostics result renders nothing a viewer could mistake for a real reference", func(t *testing.T) {
		markup, err := ui.RenderToString(PromotionValidationDiagnosticsPanel(locale, PromotionValidationDiagnostics{}))
		if err != nil {
			t.Fatalf("render: %v", err)
		}
		if strings.Contains(markup, "<details") || strings.Contains(markup, "<input") {
			t.Fatalf("an unavailable diagnostics result must render no disclosure at all: %s", markup)
		}
	})
}
