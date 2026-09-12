package productui

// PROMOUX-006: "Show the authorized compensation baseline and exact entry
// guardrail before submit."
//
// TestTodo_PROMOUX_006_Browser proves the guardrail card renders GREEN's six
// facts -- current pay, permitted increase, minimum, maximum, band position
// and effective-date basis -- from an already-evaluated
// promotion.CompensationGuardrail, and that the rendered numbers are the
// server's own projection rather than a client-side recomputation. This is
// asserted through ui.RenderToString on the same server-rendered path
// promotion_review.go and approval_disposition.go use, because this content
// is server-rendered rather than hydrated client-side.
//
// TestTodo_PROMOUX_006_Security proves the no-inference property for an
// unauthorized (or band-unresolved) viewer: the rendered markup carries no
// digit at all, and specifically none of the exact strings an authorized
// render of the identical underlying pay would contain.
//
// TestTodo_PROMOUX_006_Accessibility proves the card is one native,
// labeled group whose unavailable state is a real disabled control rather
// than styled text, and whose facts are label/value pairs.
//
// TestTodo_PROMOUX_006_I18N proves money and percentage formatting actually
// differs between en-US and de-DE (decimal and grouping separators swap)
// while the currency code stays correct in both, and that the ar locale
// renders right-to-left with its own script rather than a copy of the
// English or German strings.

import (
	"context"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/ui"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/evidence"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/promotion"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/rewards"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/payband"
	"github.com/monstercameron/human-capital-management-suite/internal/experience/localize"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

var compensationGuardrailFixedTime = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

// compensationGuardrailTestEvidence builds a minimal valid Authority and
// Provenance pair for the test catalog's band record; rewards.BandRecord.
// Validate refuses a record carrying either zero value.
func compensationGuardrailTestEvidence() (evidence.SourceAuthority, evidence.Provenance) {
	recorded, err := values.NewRecordedAt(values.NewInstant(compensationGuardrailFixedTime))
	if err != nil {
		panic(err)
	}
	return evidence.SourceAuthority{Kind: evidence.AuthorityLocal, System: "productui-test", PolicyRef: "productui-test/v1"},
		evidence.Provenance{Source: "productui-test", EvidenceRef: "evidence-productui-guardrail-1", RecordedAt: recorded}
}

// compensationGuardrailTestCatalog is a minimal rewards.PayBandCatalog
// answering exactly one scope, giving these tests full control over the
// exact numbers rendered -- independent of the shared fixture corpus.
type compensationGuardrailTestCatalog struct {
	band payband.Band
	fail error
}

func (c compensationGuardrailTestCatalog) LookupBand(_ context.Context, q rewards.BandQuery) (rewards.BandRecord, error) {
	if c.fail != nil {
		return rewards.BandRecord{}, c.fail
	}
	if q.JobCode != c.band.Scope.JobCode || q.Grade != c.band.Scope.Grade || q.PayZone != c.band.Scope.PayZone || q.Currency != c.band.Currency() {
		return rewards.BandRecord{}, rewards.ErrBandNotFound
	}
	authority, provenance := compensationGuardrailTestEvidence()
	return rewards.BandRecord{Band: c.band, CatalogVersion: "productui-test-catalog/1", Authority: authority, Provenance: provenance}, nil
}

func compensationGuardrailTestMoney(t testing.TB, amount string) values.Money {
	t.Helper()
	m, err := values.NewMoney(amount, "USD", 2, values.RoundingHalfEven)
	if err != nil {
		t.Fatalf("money(%q): %v", amount, err)
	}
	return m
}

func compensationGuardrailTestBand(t testing.TB) payband.Band {
	t.Helper()
	return payband.Band{
		ID: "eng-g5-zone-a", Version: "2026.1",
		Scope:    payband.Scope{JobCode: "ENG", Grade: "G5", PayZone: "ZONE-A"},
		Minimum:  compensationGuardrailTestMoney(t, "500000.00"),
		Midpoint: compensationGuardrailTestMoney(t, "600000.00"),
		Maximum:  compensationGuardrailTestMoney(t, "650000.00"),
	}
}

func compensationGuardrailTestDate(t testing.TB) values.LocalDate {
	t.Helper()
	d, err := values.ParseLocalDate("2026-06-01")
	if err != nil {
		t.Fatalf("date: %v", err)
	}
	return d
}

// compensationGuardrailAvailable builds a real, server-evaluated guardrail
// through promotion.EvaluateCompensationGuardrail -- not a hand-built
// struct literal -- so every test below exercises the actual server
// computation this component is required to render verbatim.
func compensationGuardrailAvailable(t testing.TB, currentAmount string) promotion.CompensationGuardrail {
	t.Helper()
	req := promotion.CompensationGuardrailRequest{
		Current: rewards.CompensationSnapshot{
			Base:     values.Value(compensationGuardrailTestMoney(t, currentAmount)),
			PayBasis: rewards.PayBasisAnnualSalary, EffectiveDate: compensationGuardrailTestDate(t),
			Watermark: mustCompensationGuardrailRevision(t), Complete: true,
		},
		Target: rewards.BandQuery{
			Tenant: "productui-guardrail-tenant", JobCode: "ENG", Grade: "G5", PayZone: "ZONE-A",
			Currency: "USD", AsOf: compensationGuardrailTestDate(t),
		},
		Catalog:       compensationGuardrailTestCatalog{band: compensationGuardrailTestBand(t)},
		Annualization: rewards.DefaultAnnualization(),
	}
	got, err := promotion.EvaluateCompensationGuardrail(context.Background(), req)
	if err != nil {
		t.Fatalf("EvaluateCompensationGuardrail: %v", err)
	}
	if !got.Available() {
		t.Fatalf("guardrail unexpectedly unavailable: %s/%s", got.Status, got.Reason)
	}
	return got
}

func mustCompensationGuardrailRevision(t testing.TB) values.RevisionToken {
	t.Helper()
	r, err := values.NewSequenceRevision("productui.guardrail.test", 1)
	if err != nil {
		t.Fatalf("revision: %v", err)
	}
	return r
}

func compensationGuardrailUnavailable(t testing.TB, reason promotion.GuardrailUnavailableReason) promotion.CompensationGuardrail {
	t.Helper()
	req := promotion.CompensationGuardrailRequest{
		Current: rewards.CompensationSnapshot{
			PayBasis: rewards.PayBasisAnnualSalary, EffectiveDate: compensationGuardrailTestDate(t), Complete: true,
		},
		Target: rewards.BandQuery{
			Tenant: "productui-guardrail-tenant", JobCode: "ENG", Grade: "G5", PayZone: "ZONE-A",
			Currency: "USD", AsOf: compensationGuardrailTestDate(t),
		},
		Catalog:       compensationGuardrailTestCatalog{band: compensationGuardrailTestBand(t)},
		Annualization: rewards.DefaultAnnualization(),
	}
	switch reason {
	case promotion.GuardrailReasonNotAuthorized:
		req.Current.Base = values.Redacted[values.Money]("denied")
	case promotion.GuardrailReasonBandUnresolved:
		req.Current.Base = values.Value(compensationGuardrailTestMoney(t, "550000.00"))
		req.Target.JobCode = "NO-SUCH-JOB"
	default:
		t.Fatalf("unhandled reason %s in test helper", reason)
	}
	got, err := promotion.EvaluateCompensationGuardrail(context.Background(), req)
	if err != nil {
		t.Fatalf("EvaluateCompensationGuardrail: %v", err)
	}
	if got.Available() || got.Reason != reason {
		t.Fatalf("guardrail = %s/%s, want unavailable/%s", got.Status, got.Reason, reason)
	}
	return got
}

func TestTodo_PROMOUX_006_Browser(t *testing.T) {
	locale := ResolveProductLocale("en-US")

	t.Run("an available guardrail renders the server's own six facts, not a client recomputation", func(t *testing.T) {
		guardrail := compensationGuardrailAvailable(t, "550000.00")
		props := CompensationGuardrailPropsFrom(locale, guardrail)
		if !props.Available || len(props.Facts) != 6 {
			t.Fatalf("props = %+v, want an available card with six facts", props)
		}

		markup, err := ui.RenderToString(CompensationGuardrailCard(props))
		if err != nil {
			t.Fatalf("render CompensationGuardrailCard: %v", err)
		}
		for _, want := range []string{
			"Compensation guardrail",
			money(locale, guardrail.CurrentAnnualized),
			percentage(locale, guardrail.PermittedIncreasePercent.Fraction().String()),
			money(locale, guardrail.MinimumAnnualized),
			money(locale, guardrail.MaximumAnnualized),
			"Within the range",
		} {
			if !strings.Contains(markup, want) {
				t.Fatalf("CompensationGuardrailCard markup missing %q: %s", want, markup)
			}
		}
	})

	t.Run("the client renders the server's guardrail rather than recomputing it", func(t *testing.T) {
		// A deliberately adversarial projection: a maximum that is NOT what
		// naive percentage arithmetic (base * 1.18) would produce from the
		// rendered current pay, and a permitted percent that does not match
		// (maximum-current)/current either. If CompensationGuardrailCard or
		// CompensationGuardrailPropsFrom ever recomputed either number from
		// the other client-side, this test would render the recomputed
		// value instead of the one supplied here, and the assertions below
		// would fail.
		naiveMax, err := values.NewMoney("649000.00", "USD", 2, values.RoundingHalfEven) // NOT base*1.18
		if err != nil {
			t.Fatal(err)
		}
		adversarialMaximum, err := values.NewMoney("999999.99", "USD", 2, values.RoundingHalfEven)
		if err != nil {
			t.Fatal(err)
		}
		adversarialPercent, err := values.NewPercentage("0.415926", 6, values.RoundingHalfEven) // arbitrary, not derived from any Money field here
		if err != nil {
			t.Fatal(err)
		}
		guardrail := promotion.CompensationGuardrail{
			Status:                   promotion.GuardrailStatusAvailable,
			CurrentAnnualized:        compensationGuardrailTestMoney(t, "550000.00"),
			MinimumAnnualized:        compensationGuardrailTestMoney(t, "500000.00"),
			MaximumAnnualized:        adversarialMaximum,
			PermittedIncreasePercent: adversarialPercent,
			BandPosition:             payband.PlacementInBand,
			Currency:                 "USD",
			EffectiveDateBasis:       compensationGuardrailTestDate(t),
			BandID:                   "eng-g5-zone-a",
			BandVersion:              "2026.1",
		}
		props := CompensationGuardrailPropsFrom(locale, guardrail)
		markup, err := ui.RenderToString(CompensationGuardrailCard(props))
		if err != nil {
			t.Fatalf("render CompensationGuardrailCard: %v", err)
		}
		if !strings.Contains(markup, money(locale, adversarialMaximum)) {
			t.Fatalf("markup does not contain the supplied projection's maximum %s; the client must render the server's guardrail, not recompute it:\n%s",
				money(locale, adversarialMaximum), markup)
		}
		if !strings.Contains(markup, percentage(locale, adversarialPercent.Fraction().String())) {
			t.Fatalf("markup does not contain the supplied projection's permitted percent %s:\n%s",
				percentage(locale, adversarialPercent.Fraction().String()), markup)
		}
		if strings.Contains(markup, money(locale, naiveMax)) {
			t.Fatalf("markup contains a naively recomputed maximum instead of the supplied projection's:\n%s", markup)
		}
	})

	t.Run("an unavailable guardrail renders the typed unavailable action, not blank facts", func(t *testing.T) {
		guardrail := compensationGuardrailUnavailable(t, promotion.GuardrailReasonNotAuthorized)
		props := CompensationGuardrailPropsFrom(locale, guardrail)
		if props.Available || props.Facts != nil {
			t.Fatalf("props = %+v, want unavailable with no facts", props)
		}
		markup, err := ui.RenderToString(CompensationGuardrailCard(props))
		if err != nil {
			t.Fatalf("render CompensationGuardrailCard: %v", err)
		}
		if !strings.Contains(markup, "Enter proposed pay") {
			t.Fatalf("markup missing the typed unavailable action: %s", markup)
		}
		if !strings.Contains(markup, "not authorized to view or set compensation") {
			t.Fatalf("markup missing the not-authorized reason: %s", markup)
		}
	})
}

func TestTodo_PROMOUX_006_Accessibility(t *testing.T) {
	locale := ResolveProductLocale("en-US")

	t.Run("the available card is one labeled group with label/value fact pairs", func(t *testing.T) {
		props := CompensationGuardrailPropsFrom(locale, compensationGuardrailAvailable(t, "550000.00"))
		markup, err := ui.RenderToString(CompensationGuardrailCard(props))
		if err != nil {
			t.Fatalf("render: %v", err)
		}
		if !strings.Contains(markup, `role="group"`) {
			t.Fatalf("card must be a native group: %s", markup)
		}
		if !strings.Contains(markup, `aria-label="Compensation guardrail"`) {
			t.Fatalf("card must have an accessible name: %s", markup)
		}
		if strings.Count(markup, "<dt") != 6 || strings.Count(markup, "<dd") != 6 {
			t.Fatalf("want six label/value fact pairs (<dt>/<dd>), got dt=%d dd=%d: %s",
				strings.Count(markup, "<dt"), strings.Count(markup, "<dd"), markup)
		}
	})

	t.Run("the unavailable card exposes a real disabled control, not styled text", func(t *testing.T) {
		props := CompensationGuardrailPropsFrom(locale, compensationGuardrailUnavailable(t, promotion.GuardrailReasonNotAuthorized))
		markup, err := ui.RenderToString(CompensationGuardrailCard(props))
		if err != nil {
			t.Fatalf("render: %v", err)
		}
		if !strings.Contains(markup, `role="group"`) || !strings.Contains(markup, `aria-label="Compensation guardrail"`) {
			t.Fatalf("unavailable card must carry the same group landmark as the available one: %s", markup)
		}
		if !strings.Contains(markup, "<button") || !strings.Contains(markup, "disabled") {
			t.Fatalf("the typed unavailable action must be a real disabled <button>, not styled text: %s", markup)
		}
		if !strings.Contains(markup, `role="status"`) {
			t.Fatalf("the reason must be announced through a status region: %s", markup)
		}
	})
}

// digitPattern finds any ASCII digit, used to prove no numeric value leaks
// into an unavailable render's visible text.
var digitPattern = regexp.MustCompile(`[0-9]`)

// tagPattern strips HTML tags so a digit check runs over rendered text
// only -- an element name like "<h3>" or an id/class attribute must never
// itself be mistaken for a leaked numeric value.
var tagPattern = regexp.MustCompile(`<[^>]*>`)

func visibleText(markup string) string { return tagPattern.ReplaceAllString(markup, " ") }

func TestTodo_PROMOUX_006_Security(t *testing.T) {
	locale := ResolveProductLocale("en-US")

	// Control: an authorized render of this exact pay DOES carry digits and
	// a percent sign in its visible text, so the digit-free assertions below
	// are not vacuous.
	availableMarkup, err := ui.RenderToString(CompensationGuardrailCard(CompensationGuardrailPropsFrom(locale, compensationGuardrailAvailable(t, "550000.00"))))
	if err != nil {
		t.Fatalf("render available control: %v", err)
	}
	if !digitPattern.MatchString(visibleText(availableMarkup)) || !strings.Contains(availableMarkup, "%") {
		t.Fatalf("control render unexpectedly carries no digits or percent sign, so the unavailable assertions below would be vacuous: %s", availableMarkup)
	}

	for _, reason := range []promotion.GuardrailUnavailableReason{
		promotion.GuardrailReasonNotAuthorized, promotion.GuardrailReasonBandUnresolved,
	} {
		t.Run(string(reason), func(t *testing.T) {
			guardrail := compensationGuardrailUnavailable(t, reason)
			props := CompensationGuardrailPropsFrom(locale, guardrail)
			markup, err := ui.RenderToString(CompensationGuardrailCard(props))
			if err != nil {
				t.Fatalf("render: %v", err)
			}
			// The strongest form of "no range, no percent, no band
			// position": not one digit appears anywhere in the rendered
			// text (markup structure such as "<h3>" is excluded). A
			// permitted range at all would let a reader solve for the
			// baseline (maximum = base * 1.18 discloses base), so this
			// checks for the family of leaks, not one specific string.
			if digitPattern.MatchString(visibleText(markup)) {
				t.Fatalf("unavailable (%s) markup leaks a digit: %s", reason, markup)
			}
			if strings.Contains(markup, "%") {
				t.Fatalf("unavailable (%s) markup leaks a percent sign: %s", reason, markup)
			}
			// Specific values from an authorized render of the identical
			// underlying scenario must not appear either, belt and braces.
			for _, mustNotContain := range []string{"500,000", "550,000", "650,000", "18.18", "Within the range", "Below the minimum", "Above the maximum"} {
				if strings.Contains(markup, mustNotContain) {
					t.Fatalf("unavailable (%s) markup leaks %q: %s", reason, mustNotContain, markup)
				}
			}
		})
	}
}

func TestTodo_PROMOUX_006_I18N(t *testing.T) {
	guardrail := compensationGuardrailAvailable(t, "550000.00")

	enUS := ResolveProductLocale("en-US")
	deDE := ResolveProductLocale("de-DE")
	ar := ResolveProductLocale("ar")

	enProps := CompensationGuardrailPropsFrom(enUS, guardrail)
	deProps := CompensationGuardrailPropsFrom(deDE, guardrail)
	arProps := CompensationGuardrailPropsFrom(ar, guardrail)

	enMarkup, err := ui.RenderToString(CompensationGuardrailCard(enProps))
	if err != nil {
		t.Fatalf("render en-US: %v", err)
	}
	deMarkup, err := ui.RenderToString(CompensationGuardrailCard(deProps))
	if err != nil {
		t.Fatalf("render de-DE: %v", err)
	}
	arMarkup, err := ui.RenderToString(CompensationGuardrailCard(arProps))
	if err != nil {
		t.Fatalf("render ar: %v", err)
	}

	t.Run("money grouping and decimal separators actually swap between en-US and de-DE", func(t *testing.T) {
		if !strings.Contains(enMarkup, "550,000.00") {
			t.Fatalf("en-US markup missing comma-grouped, dot-decimal current pay: %s", enMarkup)
		}
		if !strings.Contains(deMarkup, "550.000,00") {
			t.Fatalf("de-DE markup missing dot-grouped, comma-decimal current pay: %s", deMarkup)
		}
		if strings.Contains(deMarkup, "550,000.00") {
			t.Fatal("de-DE markup uses the en-US separator convention instead of its own")
		}
	})

	t.Run("percentage formatting also swaps its decimal separator", func(t *testing.T) {
		enPercent := percentage(enUS, guardrail.PermittedIncreasePercent.Fraction().String())
		dePercent := percentage(deDE, guardrail.PermittedIncreasePercent.Fraction().String())
		if enPercent == dePercent {
			t.Fatalf("en-US and de-DE percent formatting must differ, both rendered %q", enPercent)
		}
		if !strings.Contains(enMarkup, enPercent) {
			t.Fatalf("en-US markup missing its own percent form %q: %s", enPercent, enMarkup)
		}
		if !strings.Contains(deMarkup, dePercent) {
			t.Fatalf("de-DE markup missing its own percent form %q: %s", dePercent, deMarkup)
		}
	})

	t.Run("the currency stays USD in every locale", func(t *testing.T) {
		for name, markup := range map[string]string{"en-US": enMarkup, "de-DE": deMarkup, "ar": arMarkup} {
			if !strings.Contains(markup, "USD") {
				t.Errorf("%s markup lost the currency code: %s", name, markup)
			}
		}
	})

	t.Run("ar renders its own script, right-to-left, not a copy of the English or German copy", func(t *testing.T) {
		if ar.Direction != localize.RTL {
			t.Fatalf("ar LocaleContext.Direction = %s, want rtl", ar.Direction)
		}
		if enUS.Direction != localize.LTR || deDE.Direction != localize.LTR {
			t.Fatalf("en-US/de-DE must stay ltr: en=%s de=%s", enUS.Direction, deDE.Direction)
		}
		hasArabicScript := false
		for _, r := range arMarkup {
			if r >= 0x0600 && r <= 0x06FF {
				hasArabicScript = true
				break
			}
		}
		if !hasArabicScript {
			t.Fatalf("ar markup contains no Arabic-script characters, so it is not actually localized: %s", arMarkup)
		}
		if strings.Contains(arMarkup, "Compensation guardrail") || strings.Contains(arMarkup, "Vergütungsleitplanke") {
			t.Fatalf("ar markup reuses the English or German title instead of its own: %s", arMarkup)
		}
	})
}
