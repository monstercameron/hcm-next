package application

import (
	"errors"
	"testing"
)

type recordingSink struct{ events []Event }

func (s *recordingSink) Append(e Event) error { s.events = append(s.events, e); return nil }

func completeApplication() PartnerApplication {
	return PartnerApplication{
		ID: "payroll", Publisher: Publisher{ID: "acme", Name: "Acme", Contact: "ops@acme.test"}, Software: SoftwareIdentity{Name: "Payroll", Identifier: "com.acme.payroll"},
		RequestedCapabilities: []string{"payroll.read"}, RequestedData: []string{"worker.payroll"}, Callbacks: []Callback{{Name: "events", URL: "https://acme.test/callback", Method: "POST"}}, Regions: []string{"US"},
		SBOM: SBOM{Format: "SPDX", Digest: "sha256:sbom"}, Provenance: Provenance{Builder: "buildkite", Source: "git:abc", Digest: "sha256:source", Signature: "sig"}, Support: SupportPlan{Owner: "Acme", Contact: "support@acme.test", SLA: "24h"}, Exit: ExitPlan{Export: "json", Revocation: "revoke", Destruction: "delete"}, Compatibility: Compatibility{API: "v1", Schema: "v1", Runtime: "go", Policy: "backward"},
	}
}
func completeVersion() ApplicationVersion {
	return ApplicationVersion{ApplicationID: "payroll", Version: "1.0.0", State: StateReviewed, Reviewed: true}
}

func TestTodo_APP_001(t *testing.T) {
	sink := &recordingSink{}
	r := NewRegistry(sink)
	if err := r.Register(completeApplication()); err != nil {
		t.Fatal(err)
	}
	if err := r.Publish(completeVersion()); err != nil {
		t.Fatal(err)
	}
	if len(sink.events) != 1 {
		t.Fatalf("events=%d", len(sink.events))
	}
	got, ok := r.Lookup("payroll", "1.0.0")
	if !ok || got.State != StatePublished {
		t.Fatalf("lookup=%+v ok=%v", got, ok)
	}
}

func TestTodo_APP_001_Security(t *testing.T) {
	r := NewRegistry()
	if err := r.Register(completeApplication()); err != nil {
		t.Fatal(err)
	}
	v := completeVersion()
	v.Mutable = true
	err := r.Publish(v)
	var rej *Rejection
	if !errors.As(err, &rej) || rej.Code != RejectionCode || rej.Field != "mutable" || rej.Version != "1.0.0" {
		t.Fatalf("rejection=%v", err)
	}
	if _, ok := r.Lookup("payroll", "1.0.0"); ok {
		t.Fatal("rejected version persisted")
	}
}

func TestTodo_APP_001_Conformance(t *testing.T) {
	fields := []struct {
		name string
		edit func(*PartnerApplication)
	}{{"publisher", func(a *PartnerApplication) { a.Publisher = Publisher{} }}, {"software", func(a *PartnerApplication) { a.Software = SoftwareIdentity{} }}, {"capabilities", func(a *PartnerApplication) { a.RequestedCapabilities = nil }}, {"data", func(a *PartnerApplication) { a.RequestedData = nil }}, {"callbacks", func(a *PartnerApplication) { a.Callbacks = nil }}, {"regions", func(a *PartnerApplication) { a.Regions = nil }}, {"sbom", func(a *PartnerApplication) { a.SBOM = SBOM{} }}, {"provenance", func(a *PartnerApplication) { a.Provenance = Provenance{} }}, {"support", func(a *PartnerApplication) { a.Support = SupportPlan{} }}, {"exit", func(a *PartnerApplication) { a.Exit = ExitPlan{} }}, {"compatibility", func(a *PartnerApplication) { a.Compatibility = Compatibility{} }}}
	for _, tc := range fields {
		t.Run(tc.name, func(t *testing.T) {
			a := completeApplication()
			tc.edit(&a)
			r := NewRegistry()
			if tc.name == "publisher" || tc.name == "software" {
				if err := r.Register(a); err == nil {
					t.Fatal("incomplete application accepted")
				}
				return
			}
			if err := r.Register(a); err != nil {
				t.Fatal(err)
			}
			err := r.Publish(completeVersion())
			var rej *Rejection
			if !errors.As(err, &rej) || rej.Code != RejectionCode {
				t.Fatalf("err=%v", err)
			}
			if len(r.Versions(a.ID)) != 0 {
				t.Fatal("rejected publication persisted")
			}
		})
	}
}

func TestTodo_APP_001_Mutation(t *testing.T) {
	r := NewRegistry()
	a := completeApplication()
	if err := r.Register(a); err != nil {
		t.Fatal(err)
	}
	v := completeVersion()
	if err := r.Publish(v); err != nil {
		t.Fatal(err)
	}
	v.RequestedCapabilities = []string{"admin.write"}
	got, _ := r.Lookup("payroll", "1.0.0")
	if got.RequestedCapabilities[0] != "payroll.read" {
		t.Fatal("registry aliases caller data")
	}
	if err := r.Publish(completeVersion()); err == nil {
		t.Fatal("duplicate version accepted")
	}
}
