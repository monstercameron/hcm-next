package hrcase

import "testing"

func TestNewSetup_WiresAReadyToRunPlan(t *testing.T) {
	setup, err := NewSetup(GoldenEnvironment())
	if err != nil {
		t.Fatal(err)
	}
	if setup.Plan == nil || setup.Options.Capabilities == nil {
		t.Fatal("setup is not wired")
	}
	if _, ok := setup.Inputs.Values["case_id"]; !ok {
		t.Fatal("setup omitted case_id")
	}
}

func TestNewSetupWithParams_OmitsLegalContext(t *testing.T) {
	setup, err := NewSetupWithParams(GoldenEnvironment(), Params{OmitLegalContext: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := setup.Inputs.Context["LegalContext"]; ok {
		t.Fatal("legal context was not omitted")
	}
}
