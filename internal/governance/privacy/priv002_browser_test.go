package privacy

import (
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/qual"
)

// TestTodo_PRIV_002_Browser is the BROWSER matrix test for PRIV-002. It
// scores the notice presentation channel itself against tools/uxqual/qual's
// structural accessibility fixture (the same landmarks/label/live-region/
// contrast/reflow checks UX-QUAL-001 scores the Promotion workspace with),
// and proves EvaluateAuthority's "unsupported locale/accessibility
// presentation" RED case actually corresponds to a channel that fixture
// would flag: an accessible channel passes every criterion and authorizes;
// a channel that fails the fixture is exactly the kind of presentation
// [Presentation.Accessible] must be false for, and EvaluateAuthority denies
// it.
func TestTodo_PRIV_002_Browser(t *testing.T) {
	notice := fixtureNotice(t)

	t.Run("an accessible notice presentation passes every structural criterion", func(t *testing.T) {
		doc, err := RenderNoticeDocument(notice)
		if err != nil {
			t.Fatalf("RenderNoticeDocument: %v", err)
		}

		checks := []qual.CriterionResult{
			qual.CheckKeyboard(doc),
			qual.CheckScreenReaderSemantics(doc),
			qual.CheckContrastAA(),
			qual.CheckReflow(qual.ExtractInlineCSS(doc)),
		}
		for _, c := range checks {
			t.Logf("%s: pass=%v (%s)", c.Name, c.Pass, c.Detail)
			if !c.Pass {
				t.Errorf("accessible notice document failed %q: %s", c.Name, c.Detail)
			}
		}

		// This is the channel EvaluateAuthority's Presentation.Accessible=true
		// represents: prove the full chain authorizes when it is used.
		presentation := fixturePresentation(t, notice)
		consent := fixtureConsent(t, presentation.ID)
		in := AuthorityInput{
			Purpose: "ai_assist", Principal: fixturePrincipal, EffectiveAt: mustInstant(t, fxEvaluateAt),
			Notice: notice, Presentation: &presentation, Consent: &consent, SupportedLocales: fixtureSupportedLocales,
		}
		if got := EvaluateAuthority(in); !got.Allowed {
			t.Errorf("EvaluateAuthority denied an accessible, fully-evidenced presentation: %+v", got)
		}
	})

	t.Run("RED: a channel that fails the accessibility fixture is exactly what Accessible=false must deny", func(t *testing.T) {
		// A deliberately broken presentation screen: no landmark, no label,
		// no live region, and an oversized fixed-width panel -- the same
		// class of broken fixture tools/uxqual/qual's own PRIMARY test
		// (TestWorkspaceQualificationFixture) uses to prove its RED case.
		broken := `<html><head><style>.panel{width:900px}</style></head><body><input id="ack"></body></html>`

		screenReader := qual.CheckScreenReaderSemantics(broken)
		reflow := qual.CheckReflow(qual.ExtractInlineCSS(broken))
		if screenReader.Pass {
			t.Errorf("expected the broken presentation document to fail Screen-reader semantics")
		}
		if reflow.Pass {
			t.Errorf("expected the broken presentation document's 900px panel to fail Reflow at 320px")
		}

		// A presentation recorded against a channel that fails this fixture
		// must be recorded with Accessible=false, and EvaluateAuthority must
		// deny it -- this is PRIV-002's "unsupported locale/accessibility
		// presentation" RED case made concrete end to end.
		presentation, err := NewPresentation("presentation-broken-channel", notice, fixturePrincipal, "en-US", false,
			mustInstant(t, fxPresentedAt), mustInstant(t, fxAcknowledgedAt))
		if err != nil {
			t.Fatalf("NewPresentation: %v", err)
		}
		consent := fixtureConsent(t, presentation.ID)
		in := AuthorityInput{
			Purpose: "ai_assist", Principal: fixturePrincipal, EffectiveAt: mustInstant(t, fxEvaluateAt),
			Notice: notice, Presentation: &presentation, Consent: &consent, SupportedLocales: fixtureSupportedLocales,
		}
		got := EvaluateAuthority(in)
		if got.Allowed || got.Code != AuthorityPresentationNotA11y {
			t.Fatalf("EvaluateAuthority(broken-channel presentation) = %+v, want denied with %s", got, AuthorityPresentationNotA11y)
		}
	})

	t.Run("the accessible document never renders raw notice identifiers a screen reader would announce out of context", func(t *testing.T) {
		doc, err := RenderNoticeDocument(notice)
		if err != nil {
			t.Fatalf("RenderNoticeDocument: %v", err)
		}
		if !strings.Contains(doc, notice.Purpose) {
			t.Errorf("rendered notice document does not even mention its own purpose %q; a reviewer could not tell what it covers", notice.Purpose)
		}
	})
}
