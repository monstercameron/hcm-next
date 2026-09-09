package crm

import (
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func prospectInstant(t *testing.T, text string) values.Instant {
	t.Helper()
	v, err := time.Parse(time.RFC3339, text)
	if err != nil {
		t.Fatal(err)
	}
	return values.NewInstant(v)
}

func validProspect(t *testing.T) ProspectRevision {
	t.Helper()
	grant := prospectInstant(t, "2026-01-02T00:00:00Z")
	return ProspectRevision{
		ProspectID: crmRef("prospect", "p-1"), Revision: crmRevision(t),
		Attribution: ProspectSourceAttribution{Source: SourceAttribution{System: "landing-page", Reference: "lead-123", RecordedBy: crmRef("principal", "principal-1")}, Campaign: "winter-2026", Referral: "employee-42", Channel: "web"},
		Consent:     ProspectConsent{Authority: crmRef("processing_authority", "pa-1"), Basis: "consent", Evidence: crmRef("evidence", "ev-1"), GrantedAt: grant},
		Effective:   crmInterval(t), Owner: crmRef("owner", "owner-1"),
	}
}

func TestTodo_CRM_002(t *testing.T) {
	p := validProspect(t)
	if err := p.Validate(); err != nil {
		t.Fatal(err)
	}
	if p.Attribution.Source.System != "landing-page" || p.Attribution.Source.Reference != "lead-123" || p.Attribution.Campaign != "winter-2026" || p.Attribution.Referral != "employee-42" || p.Attribution.Channel != "web" {
		t.Fatalf("attribution=%+v, want exact captured lineage", p.Attribution)
	}
	if p.Consent.Authority.Kind != "processing_authority" || p.Consent.Authority.Tenant != p.ProspectID.Tenant || p.Consent.Evidence.Tenant != p.ProspectID.Tenant {
		t.Fatalf("consent=%+v, want tenant-bound processing authority", p.Consent)
	}
	decision := p.Consent.OutreachAt(prospectInstant(t, "2026-01-03T00:00:00Z"))
	if !decision.Allowed || decision.Status != ProspectConsentGranted || len(decision.Obligations) != 0 {
		t.Fatalf("decision=%+v, want active outreach", decision)
	}
}

func TestTodo_CRM_002_Mutation(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*ProspectRevision)
	}{
		{name: "lineage", mutate: func(p *ProspectRevision) {
			p.Attribution.Campaign = ""
			p.Attribution.Referral = ""
		}},
		{name: "channel", mutate: func(p *ProspectRevision) { p.Attribution.Channel = "" }},
		{name: "processing authority", mutate: func(p *ProspectRevision) { p.Consent.Authority = values.EntityRef{} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := validProspect(t)
			tc.mutate(&p)
			if err := p.Validate(); !errors.Is(err, ErrInvalidProspect) && !errors.Is(err, ErrInvalidReference) {
				t.Fatalf("Validate() error=%v, want prospect/reference rejection", err)
			}
		})
	}
	p := validProspect(t)
	p.Consent.ExpiresAt = prospectInstant(t, "2026-01-03T00:00:00Z")
	decision := p.Consent.OutreachAt(prospectInstant(t, "2026-01-03T00:00:00Z"))
	if decision.Allowed || decision.Status != ProspectConsentExpired || !equalObligations(decision.Obligations, retentionObligations()) {
		t.Fatalf("expiry decision=%+v", decision)
	}
}

func TestTodo_CRM_002_Security(t *testing.T) {
	p := validProspect(t)
	withdrawn, err := p.Consent.Withdraw(prospectInstant(t, "2026-01-04T00:00:00Z"))
	if err != nil {
		t.Fatal(err)
	}
	if p.Consent.WithdrawnAt.IsSet() {
		t.Fatal("withdraw mutated original consent")
	}
	decision := withdrawn.OutreachAt(prospectInstant(t, "2026-01-05T00:00:00Z"))
	if decision.Allowed || decision.Status != ProspectConsentWithdrawn || !equalObligations(decision.Obligations, retentionObligations()) {
		t.Fatalf("withdrawal decision=%+v", decision)
	}
	crossTenant := p
	crossTenant.Consent.Authority.Tenant = "tenant-2"
	if err := crossTenant.Validate(); !errors.Is(err, ErrInvalidReference) {
		t.Fatal("cross-tenant authority accepted")
	}
	expired := p.Consent
	expired.ExpiresAt = prospectInstant(t, "2026-01-03T00:00:00Z")
	if _, err := expired.Withdraw(prospectInstant(t, "2026-01-04T00:00:00Z")); !errors.Is(err, ErrInvalidProspect) {
		t.Fatalf("withdraw expired consent error=%v, want ErrInvalidProspect", err)
	}
	expired.WithdrawnAt = prospectInstant(t, "2026-01-04T00:00:00Z")
	if err := expired.Validate(); !errors.Is(err, ErrInvalidProspect) {
		t.Fatalf("caller-forged post-expiry withdrawal error=%v, want ErrInvalidProspect", err)
	}
	invalid := p.Consent
	invalid.Authority = values.EntityRef{}
	decision = invalid.OutreachAt(prospectInstant(t, "2026-01-05T00:00:00Z"))
	if decision.Allowed || decision.Status != ProspectConsentInvalid || !equalObligations(decision.Obligations, []RetentionObligation{ObligationStopFutureOutreach}) {
		t.Fatalf("invalid consent decision=%+v, want deny without destructive retention", decision)
	}
	decision = p.Consent.OutreachAt(prospectInstant(t, "2026-01-01T00:00:00Z"))
	if decision.Allowed || decision.Status != ProspectConsentInvalid || !equalObligations(decision.Obligations, []RetentionObligation{ObligationStopFutureOutreach}) {
		t.Fatalf("pre-grant decision=%+v, want deny without destructive retention", decision)
	}
}

func retentionObligations() []RetentionObligation {
	return []RetentionObligation{ObligationStopFutureOutreach, ObligationRetainConsentEvidence, ObligationDeleteDerivedProspectData}
}

func equalObligations(got, want []RetentionObligation) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
