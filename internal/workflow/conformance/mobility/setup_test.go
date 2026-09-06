package mobility

import "testing"

func TestNewSetup_WiresAReadyToRunPlan(t *testing.T) {
	setup, err := NewSetup(GoldenEnvironment())
	if err != nil {
		t.Fatal(err)
	}
	if setup.Plan == nil || setup.Options.Capabilities == nil {
		t.Fatal("setup is not wired")
	}
	if _, ok := setup.Inputs.Values["worker_id"]; !ok {
		t.Fatal("setup omitted worker_id")
	}
}

func TestNewSetupWithParams_OmitsRequestedContexts(t *testing.T) {
	setup, err := NewSetupWithParams(GoldenEnvironment(), Params{OmitLegalContext: true, OmitPrivacyContext: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := setup.Inputs.Context["LegalContext"]; ok {
		t.Fatal("legal context was not omitted")
	}
	if _, ok := setup.Inputs.Context["PrivacyContext"]; ok {
		t.Fatal("privacy context was not omitted")
	}
}
