package partnerapp

import (
	"errors"
	"testing"
)

func partnerAgreement() PartnerAgreement {
	return PartnerAgreement{Ref: "agreement:acme:2026", Capabilities: []string{"people.read", "payroll.read"}, DataClasses: []string{"IDENTITY", "PAYROLL"}}
}

func partnerApplication() PartnerApplication {
	return PartnerApplication{
		PartnerRef: "partner:acme", ApplicationID: "app-payroll", AgreementRef: "agreement:acme:2026",
		DeclaredCapabilities: []string{"payroll.read"}, DataClasses: []string{"PAYROLL"},
		RedirectEndpoints: []EndpointRef{{Ref: "endpoint:redirect", Digest: "sha256:redirect"}},
		CallbackEndpoints: []EndpointRef{{Ref: "endpoint:callback", Digest: "sha256:callback"}},
		ContactRef:        "contact:acme-ops", LegalRef: "legal:acme-contract",
	}
}

func partnerVersionRequest() VersionRequest {
	return VersionRequest{Version: "1.0.0", Requester: "requester-1"}
}

func TestTodo_APP_001(t *testing.T) {
	agreement := partnerAgreement()
	app := partnerApplication()
	draft, err := NewDraft(app, partnerVersionRequest(), agreement)
	if err != nil {
		t.Fatal(err)
	}
	reviewed, err := draft.RecordReview("reviewer-1", true, "coverage checked")
	if err != nil {
		t.Fatal(err)
	}
	active, err := reviewed.Activate()
	if err != nil {
		t.Fatal(err)
	}
	registry, err := NewRegistry(agreement)
	if err != nil {
		t.Fatal(err)
	}
	if err := registry.RegisterApplication(app); err != nil {
		t.Fatal(err)
	}
	for _, v := range []ApplicationVersion{draft, reviewed, active} {
		if err := registry.RegisterVersion(v); err != nil {
			t.Fatalf("register %s@%d: %v", v.State, v.Revision, err)
		}
	}
	history := registry.Revisions(app.ApplicationID, "1.0.0")
	if len(history) != 3 || history[2].State != StateActive || history[2].Digest == "" {
		t.Fatalf("history=%+v", history)
	}
	if app.Explain().CapabilityCount != 1 || Explain(app).AgreementRef != agreement.Ref {
		t.Fatalf("explanation=%+v", app.Explain())
	}
}

func TestTodo_APP_001_Security(t *testing.T) {
	agreement := partnerAgreement()
	app := partnerApplication()
	draft, err := NewDraft(app, partnerVersionRequest(), agreement)
	if err != nil {
		t.Fatal(err)
	}
	withoutReview := draft
	withoutReview.State = StateActive
	withoutReview.Digest = computeDigest(withoutReview)
	if err := withoutReview.Validate(app, agreement); !errors.Is(err, ErrReviewRequired) {
		t.Fatalf("unreviewed active error=%v", err)
	}
	outsideAgreement := partnerVersionRequest()
	outsideAgreement.DeclaredCapabilities = []string{"admin.write"}
	if _, err := NewDraft(app, outsideAgreement, agreement); !errors.Is(err, ErrAgreementMismatch) {
		t.Fatalf("capability coverage error=%v", err)
	}
	outsideData := partnerVersionRequest()
	outsideData.DataClasses = []string{"HEALTH"}
	if _, err := NewDraft(app, outsideData, agreement); !errors.Is(err, ErrAgreementMismatch) {
		t.Fatalf("data coverage error=%v", err)
	}
}

func TestTodo_APP_001_Conformance(t *testing.T) {
	agreement := partnerAgreement()
	app := partnerApplication()
	draft, err := NewDraft(app, partnerVersionRequest(), agreement)
	if err != nil {
		t.Fatal(err)
	}
	reviewed, err := draft.RecordReview("reviewer-1", true, "approved")
	if err != nil {
		t.Fatal(err)
	}
	active, err := reviewed.Activate()
	if err != nil {
		t.Fatal(err)
	}
	deprecated, err := active.Deprecate("operator-1", "2.0.0")
	if err != nil || deprecated.SuccessorVersion != "2.0.0" || deprecated.State != StateDeprecated {
		t.Fatalf("deprecated=%+v err=%v", deprecated, err)
	}
	revoked, event, err := active.Revoke("operator-2", []string{"sub-z", "sub-a"}, []string{"lease-z", "lease-a"})
	if err != nil {
		t.Fatal(err)
	}
	if revoked.State != StateRevoked || event.Digest == "" || event.SubscriptionRefs[0] != "sub-a" || event.LeaseRefs[0] != "lease-a" {
		t.Fatalf("revoked=%+v event=%+v", revoked, event)
	}
	if event.Digest != computeRevocationDigest(event) {
		t.Fatal("revocation digest is not stable")
	}
}

func TestTodo_APP_001_Mutation(t *testing.T) {
	agreement := partnerAgreement()
	app := partnerApplication()
	registry, err := NewRegistry(agreement)
	if err != nil {
		t.Fatal(err)
	}
	if err := registry.RegisterApplication(app); err != nil {
		t.Fatal(err)
	}
	app.DeclaredCapabilities[0] = "admin.write"
	stored, ok := registry.applications["app-payroll"]
	if !ok || stored.DeclaredCapabilities[0] != "payroll.read" {
		t.Fatalf("registry retained caller alias: %+v", stored)
	}
	draft, err := NewDraft(partnerApplication(), partnerVersionRequest(), agreement)
	if err != nil {
		t.Fatal(err)
	}
	if err := registry.RegisterVersion(draft); err != nil {
		t.Fatal(err)
	}
	draft.DeclaredCapabilities[0] = "admin.write"
	if got := registry.Revisions("app-payroll", "1.0.0")[0].DeclaredCapabilities[0]; got != "payroll.read" {
		t.Fatalf("version retained caller alias: %q", got)
	}
	if err := registry.RegisterVersion(draft); err == nil {
		t.Fatal("duplicate immutable revision accepted")
	}
}
