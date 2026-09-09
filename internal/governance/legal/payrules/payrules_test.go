package payrules

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return b
}

func localDate(t *testing.T, text string) values.LocalDate {
	t.Helper()
	d, err := values.ParseLocalDate(text)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func TestTodo_LEGAL_TOOL_004(t *testing.T) {
	set, err := LoadPayFrequencyParameters(fixture(t, "pay-frequency.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := set.Validate(); err != nil {
		t.Fatal(err)
	}
	if set.Digest == "" || set.ComputeDigest() != set.Digest {
		t.Fatalf("digest is not self-consistent: %q", set.Digest)
	}
	if !set.AppliesOn(localDate(t, "2026-09-05")) {
		t.Fatal("fixture should apply on the pinned business date")
	}
	for _, constraint := range set.Constraints {
		if constraint.Citation.SourceFile == "" || constraint.Citation.Section == "" {
			t.Fatalf("%s is missing statute provenance", constraint.ID)
		}
	}
}

func TestTodo_LEGAL_TOOL_004_Golden(t *testing.T) {
	set, err := LoadPayFrequencyParameters(fixture(t, "pay-frequency.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		state   string
		change  PayChange
		allowed bool
	}{
		{state: "MN", change: PayChange{EffectiveDate: localDate(t, "2026-09-10"), NoticeDate: localDate(t, "2026-09-10"), Direction: Increase}, allowed: false},
		{state: "SC", change: PayChange{EffectiveDate: localDate(t, "2026-09-10"), NoticeDate: localDate(t, "2026-09-03"), Direction: Decrease}, allowed: true},
		{state: "WV", change: PayChange{EffectiveDate: localDate(t, "2026-09-10"), NoticeDate: localDate(t, "2026-08-26"), Direction: Increase, PayPeriodsBefore: 1}, allowed: true},
		{state: "AK", change: PayChange{EffectiveDate: localDate(t, "2026-09-10"), NoticeDate: localDate(t, "2026-09-10"), PriorPayday: localDate(t, "2026-08-31"), Direction: Increase}, allowed: false},
	}
	for _, tc := range cases {
		var constraint PayFrequencyConstraint
		for _, candidate := range set.Constraints {
			if candidate.StateCode == tc.state {
				constraint = candidate
				break
			}
		}
		decision := constraint.CheckNotice(tc.change)
		if decision.Allowed != tc.allowed {
			t.Errorf("%s decision = %+v, allowed %v", tc.state, decision, tc.allowed)
		}
	}
}

func TestTodo_LEGAL_TOOL_004_Conformance(t *testing.T) {
	set, err := LoadPayFrequencyParameters(fixture(t, "pay-frequency.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"MN": "Minn. Stat. § 181.032", "SC": "§ 41-10-30", "WV": "W. Va. Code § 21-5-9(4)", "AK": "AS § 23.05.160"}
	for _, row := range set.Constraints {
		if want[row.StateCode] != row.Citation.Section {
			t.Errorf("%s citation = %q", row.StateCode, row.Citation.Section)
		}
		if row.Status != Reviewed {
			t.Errorf("%s status = %s, want REVIEWED", row.StateCode, row.Status)
		}
	}
}

func TestTodo_LEGAL_TOOL_005(t *testing.T) {
	registry, err := LoadPayStatementRegistry(fixture(t, "pay-statements.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := registry.Validate(); err != nil {
		t.Fatal(err)
	}
	if len(registry.States) != 50 {
		t.Fatalf("state rows = %d, want 50", len(registry.States))
	}
	for _, state := range USStateCodes {
		row, ok := registry.ForState(state)
		if !ok || row.Citation.Section == "" {
			t.Fatalf("%s has no resolved cited mandate row", state)
		}
		if row.Mandate != Mandatory && row.Mandate != NotMandated {
			t.Fatalf("%s has unresolved mandate %q", state, row.Mandate)
		}
	}
}

func TestTodo_LEGAL_TOOL_005_Golden(t *testing.T) {
	registry, err := LoadPayStatementRegistry(fixture(t, "pay-statements.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	checks := map[string]struct {
		mandate PayStatementMandate
		fields  int
	}{
		"CA": {Mandatory, 9},
		"AK": {Mandatory, 7},
		"GA": {NotMandated, 0},
	}
	for state, want := range checks {
		row, ok := registry.ForState(state)
		if !ok || row.Mandate != want.mandate || len(row.RequiredFields) != want.fields {
			t.Errorf("%s row = %+v, want mandate=%s fields=%d", state, row, want.mandate, want.fields)
		}
	}
	if registry.ComputeDigest() != registry.Digest {
		t.Fatal("pay-statement golden digest changed")
	}
}

func TestTodo_LEGAL_TOOL_005_Conformance(t *testing.T) {
	registry, err := LoadPayStatementRegistry(fixture(t, "pay-statements.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range registry.States {
		if row.Status != Reviewed && row.Status != Unreviewed {
			t.Errorf("%s has invalid review status %q", row.StateCode, row.Status)
		}
		if row.Mandate == NotMandated && len(row.RequiredFields) != 0 {
			t.Errorf("%s has fields on NOT_MANDATED row", row.StateCode)
		}
	}
}

func TestTodo_LEGAL_TOOL_006(t *testing.T) {
	set, err := LoadLeaveParameterSet(fixture(t, "leave.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := set.Validate(); err != nil {
		t.Fatal(err)
	}
	if set.Digest == "" || set.ComputeDigest() != set.Digest {
		t.Fatal("leave digest is not self-consistent")
	}
	for _, program := range set.Programs {
		if !program.NoForfeitureOnRoleChange {
			t.Errorf("%s forfeits balance on role change", program.ID)
		}
	}
}

func TestTodo_LEGAL_TOOL_006_Golden(t *testing.T) {
	set, err := LoadLeaveParameterSet(fixture(t, "leave.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	byState := map[string]LeaveProgramParameters{}
	for _, program := range set.Programs {
		byState[program.StateCode] = program
	}
	az := byState["AZ"]
	if len(az.EmployerSizeTiers) != 2 || az.EmployerSizeTiers[0].PaidCapHours != 24 || az.EmployerSizeTiers[1].PaidCapHours != 40 {
		t.Fatalf("Arizona tiers = %+v", az.EmployerSizeTiers)
	}
	if *byState["MN"].CarryoverCapHours != 80 {
		t.Fatalf("Minnesota carryover = %v, want 80", *byState["MN"].CarryoverCapHours)
	}
	mi := byState["MI"]
	if mi.EmployerSizeTiers[0].PaidCapHours != 40 || mi.EmployerSizeTiers[0].UnpaidCapHours != 32 {
		t.Fatalf("Michigan small-employer cap pair = %+v", mi.EmployerSizeTiers[0])
	}
}

func TestTodo_LEGAL_TOOL_006_Conformance(t *testing.T) {
	set, err := LoadLeaveParameterSet(fixture(t, "leave.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !set.AppliesOn(localDate(t, "2026-09-05")) {
		t.Fatal("leave fixture should apply on the pinned date")
	}
	for i, program := range set.Programs {
		if program.Citation.SourceFile == "" || program.Citation.Section == "" {
			t.Errorf("program %d lacks a statute citation", i)
		}
		if program.AccrualHoursPerHoursWorked.Numerator != 1 || program.AccrualHoursPerHoursWorked.Denominator != 30 {
			t.Errorf("program %s has unexpected accrual ratio %+v", program.ID, program.AccrualHoursPerHoursWorked)
		}
	}
}

func TestValidationRefusalNamesField(t *testing.T) {
	_, err := LoadPayFrequencyParameters([]byte("schema_version: 1\nversion: 1\neffective_from: 2025-01-01\nknown_at: 2026-09-03T00:00:00Z\nconstraints:\n  - id: bad\n    state_code: MN\n    min_frequency: MONTHLY\n    notice_lead_time:\n      lead_unit: CALENDAR_DAYS\n      lead_count: -1\n      applies_to: BOTH\n    status: REVIEWED\n    citation: {source_file: x, section: y}\n"))
	var refusal *ValidationRefusal
	if !errors.As(err, &refusal) || refusal.Field != "constraints[0].notice_lead_time.lead_count" {
		t.Fatalf("error = %v, refusal = %+v", err, refusal)
	}
}
