package privacy

import (
	"testing"

	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

// --- shared test fixtures --------------------------------------------------
//
// These build one consistent PRIV-002 evidence chain (an "ai_assist" notice,
// presented and consented to by worker "worker-1048") that every matrix test
// in this package starts from, then perturbs one thing at a time. Timestamps
// use plain Unix seconds for readability; only relative order matters to any
// assertion in this package.

func mustInstant(t *testing.T, unixSec int64) values.Instant {
	t.Helper()
	i, err := values.NewInstantFromUnix(unixSec, 0)
	if err != nil {
		t.Fatalf("NewInstantFromUnix(%d): %v", unixSec, err)
	}
	return i
}

const (
	fxNoticeEffectiveFrom = 1_700_000_000
	fxNoticeEffectiveTo   = 1_900_000_000
	fxPresentedAt         = 1_700_100_000
	fxAcknowledgedAt      = 1_700_100_060
	fxGrantedAt           = 1_700_100_120
	fxEvaluateAt          = 1_700_200_000 // "now" for most tests: inside every fixture interval
)

func fixtureNotice(t *testing.T) Notice {
	t.Helper()
	return Notice{
		ID:            "notice-ai-assist",
		Version:       "v1",
		Purpose:       "ai_assist",
		DataClasses:   []string{"work_email", "job_title", "manager_name"},
		Jurisdiction:  "US-CA",
		Locale:        "en-US",
		EffectiveFrom: mustInstant(t, fxNoticeEffectiveFrom),
		EffectiveTo:   mustInstant(t, fxNoticeEffectiveTo),
	}
}

func fixtureMandatoryNotice(t *testing.T) Notice {
	t.Helper()
	n := fixtureNotice(t)
	n.ID = "notice-tax-reporting"
	n.Purpose = "tax_reporting"
	n.Mandatory = true
	return n
}

const fixturePrincipal = "worker-1048"

func fixturePresentation(t *testing.T, notice Notice) Presentation {
	t.Helper()
	p, err := NewPresentation("presentation-1", notice, fixturePrincipal, "en-US", true,
		mustInstant(t, fxPresentedAt), mustInstant(t, fxAcknowledgedAt))
	if err != nil {
		t.Fatalf("NewPresentation: %v", err)
	}
	return p
}

func fixtureConsent(t *testing.T, presentationID string) OptionalProcessing {
	t.Helper()
	c, err := NewOptionalProcessing("consent-1", fixturePrincipal, []string{"ai_assist", "analytics"},
		presentationID, mustInstant(t, fxGrantedAt), values.Instant{})
	if err != nil {
		t.Fatalf("NewOptionalProcessing: %v", err)
	}
	return c
}

var fixtureSupportedLocales = []string{"en-US", "es-MX"}

// fixtureAuthorityInput builds the full GREEN authority input: a valid
// notice, a matching accessible presentation, and a matching in-scope
// consent, evaluated at fxEvaluateAt (inside every fixture interval).
func fixtureAuthorityInput(t *testing.T) AuthorityInput {
	t.Helper()
	notice := fixtureNotice(t)
	presentation := fixturePresentation(t, notice)
	consent := fixtureConsent(t, presentation.ID)
	return AuthorityInput{
		Purpose:          "ai_assist",
		Principal:        fixturePrincipal,
		EffectiveAt:      mustInstant(t, fxEvaluateAt),
		Notice:           notice,
		Presentation:     &presentation,
		Consent:          &consent,
		SupportedLocales: fixtureSupportedLocales,
	}
}
