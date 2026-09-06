package anomaly002

import (
	"errors"
	"testing"
)

func hardeningDefinition() Definition {
	return Definition{ID: "privileged-activity", Version: "7", Owner: "security", MaxVolume: 100, MaxScope: 5, AllowedHours: [2]int{6, 20}, AllowedLocations: []string{"office", "vpn"}}
}

func hardeningActivity() Activity {
	return Activity{ID: "a-1", Actor: "operator-1", Tenant: "tenant-a", Resource: "sensitive-record", Location: "vpn", Hour: 10, Volume: 2, Scope: 1, Sequence: 1, Sensitive: true, Version: "source-3"}
}

func TestAnomaly002DefinitionAndActivityValidationBranches(t *testing.T) {
	validDefinition := hardeningDefinition()
	for _, tc := range []struct {
		name   string
		mutate func(*Definition)
	}{
		{"id", func(d *Definition) { d.ID = "" }}, {"version", func(d *Definition) { d.Version = "" }}, {"owner", func(d *Definition) { d.Owner = "" }},
		{"volume", func(d *Definition) { d.MaxVolume = 0 }}, {"scope", func(d *Definition) { d.MaxScope = 0 }}, {"hour low", func(d *Definition) { d.AllowedHours[0] = -1 }}, {"hour high", func(d *Definition) { d.AllowedHours[1] = 25 }}, {"hour order", func(d *Definition) { d.AllowedHours = [2]int{20, 6} }}, {"locations", func(d *Definition) { d.AllowedLocations = nil }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := validDefinition
			tc.mutate(&d)
			if !errors.Is(d.Validate(), ErrDefinition) {
				t.Fatalf("error=%v", d.Validate())
			}
		})
	}
	validActivity := hardeningActivity()
	for _, tc := range []struct {
		name   string
		mutate func(*Activity)
	}{
		{"id", func(a *Activity) { a.ID = "" }}, {"actor", func(a *Activity) { a.Actor = "" }}, {"tenant", func(a *Activity) { a.Tenant = "" }}, {"resource", func(a *Activity) { a.Resource = "" }}, {"version", func(a *Activity) { a.Version = "" }},
		{"hour low", func(a *Activity) { a.Hour = -1 }}, {"hour high", func(a *Activity) { a.Hour = 24 }}, {"volume", func(a *Activity) { a.Volume = -1 }}, {"scope", func(a *Activity) { a.Scope = -1 }}, {"sequence", func(a *Activity) { a.Sequence = -1 }}, {"classification", func(a *Activity) { a.Privileged = false; a.Sensitive = false }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := validActivity
			tc.mutate(&a)
			if !errors.Is(a.Validate(), ErrActivity) {
				t.Fatalf("error=%v", a.Validate())
			}
		})
	}
	if !contains([]string{"a", "b"}, "b") || contains([]string{"a"}, "b") {
		t.Fatal("contains branches failed")
	}
}

func TestAnomaly002DetectOrderExemptionsAndFindingState(t *testing.T) {
	d := hardeningDefinition()
	first := hardeningActivity()
	first.ID = "z"
	first.Hour = 2
	second := hardeningActivity()
	second.ID = "a"
	second.Location = "unknown"
	result, err := Detect(d, []Activity{first, second})
	if err != nil || result.Code != RejectedCode || result.Finding == nil || result.Finding.ActivityID != "a" || result.Finding.OffendingField != "location" || result.Finding.State != "REVIEW_REQUIRED" || result.Finding.Version != d.Version {
		t.Fatalf("ordered result=%+v err=%v", result, err)
	}
	for _, tc := range []struct {
		name   string
		mutate func(*Activity)
		field  string
	}{
		{"time", func(a *Activity) { a.Hour = 2 }, "time"}, {"location", func(a *Activity) { a.Location = "unknown" }, "location"}, {"volume", func(a *Activity) { a.Volume = d.MaxVolume + 1 }, "volume"}, {"scope", func(a *Activity) { a.Scope = d.MaxScope + 1 }, "scope"}, {"sequence", func(a *Activity) { a.Sequence = 2 }, "sequence"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := hardeningActivity()
			tc.mutate(&a)
			got, err := Detect(d, []Activity{a})
			if err != nil || got.Code != RejectedCode || got.Finding == nil || got.Finding.OffendingField != tc.field {
				t.Fatalf("result=%+v err=%v", got, err)
			}
		})
	}
	approved := hardeningActivity()
	approved.Hour = 2
	approved.Volume = 1000
	approved.ApprovedWork = true
	incident := hardeningActivity()
	incident.Location = "unknown"
	incident.IncidentWork = true
	accepted, err := Detect(d, []Activity{approved, incident})
	if err != nil || accepted.Code != AcceptedCode || accepted.Finding != nil {
		t.Fatalf("exempt result=%+v err=%v", accepted, err)
	}
	invalid := hardeningActivity()
	invalid.ID = ""
	if _, err := Detect(d, []Activity{invalid}); !errors.Is(err, ErrActivity) {
		t.Fatalf("invalid activity error=%v", err)
	}
	badDefinition := d
	badDefinition.AllowedLocations = nil
	if _, err := Detect(badDefinition, []Activity{hardeningActivity()}); !errors.Is(err, ErrDefinition) {
		t.Fatalf("invalid definition error=%v", err)
	}
	if result.Finding.Code != RejectedCode {
		t.Fatalf("finding code=%q", result.Finding.Code)
	}
}

func TestAnomaly002ExplainValidationAndDeclaredInputs(t *testing.T) {
	a, d := hardeningActivity(), hardeningDefinition()
	valid := Explain(a, d)
	if !valid.Applicable || valid.Reason != "DECLARED_ACTIVITY" || valid.DetectorID != d.ID || valid.Version != d.Version || valid.ActivityID != a.ID || len(valid.Inputs) != 7 {
		t.Fatalf("valid explanation=%+v", valid)
	}
	badDefinition := d
	badDefinition.ID = ""
	if got := Explain(a, badDefinition); got.Applicable || got.Reason != "DEFINITION_INVALID" || len(got.Inputs) != 7 {
		t.Fatalf("bad definition explanation=%+v", got)
	}
	badActivity := a
	badActivity.Privileged = false
	badActivity.Sensitive = false
	if got := Explain(badActivity, d); got.Applicable || got.Reason != "ACTIVITY_INVALID" {
		t.Fatalf("bad activity explanation=%+v", got)
	}
}
