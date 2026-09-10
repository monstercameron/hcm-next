package workflowdesign

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func seedPath() string {
	return filepath.Join("testdata", "seed", "records.yaml")
}

func loadSeed(t *testing.T) []DesignRecord {
	t.Helper()
	records, err := LoadRecords(seedPath())
	if err != nil {
		t.Fatal(err)
	}
	return records
}

func completeRecord() DesignRecord {
	return DesignRecord{
		Intent:         "ChangeManager",
		Disposition:    "WORKFLOW",
		Archetype:      "A2",
		DomainProfile:  "people",
		InputBoundary:  "manager-edge-proposal",
		SnapshotPolicy: "authority-snapshot",
		Engines:        Dimension{Items: []string{"identity-resolution"}},
		HumanWork:      Dimension{Value: "manager-self-service"},
		Writes:         Dimension{Value: "manager-edge-replacement"},
		Waits:          Dimension{Value: "approval-window"},
		Invalidators:   Dimension{Value: "org-reparent"},
		Reconciliation: Dimension{Value: "relationship-access"},
		Correction:     Dimension{Value: "revoke-and-replace"},
		Completion:     "edge-observed",
	}
}

func TestWorkflowDesignRecordRejectsImplicitExecutionResponsibilities(t *testing.T) {
	records := loadSeed(t)
	if len(records) != 14 {
		t.Fatalf("seed records = %d, want 14", len(records))
	}
	report := ValidateRecords(records)
	if !report.OK() {
		t.Fatalf("seed records rejected: %+v", report.Findings)
	}
	again := ValidateRecords(loadSeed(t))
	if report.Digest != again.Digest {
		t.Fatal("record digest not deterministic across compilations")
	}
	shuffled := append([]DesignRecord(nil), records...)
	for i, j := 0, len(shuffled)-1; i < j; i, j = i+1, j-1 {
		shuffled[i], shuffled[j] = shuffled[j], shuffled[i]
	}
	if ValidateRecords(shuffled).Digest != report.Digest {
		t.Fatal("record digest depends on declaration order")
	}
}

func TestTodo_WF_DISC_005_Property(t *testing.T) {
	blank := Dimension{}
	cases := []struct {
		name   string
		mutate func(*DesignRecord)
		code   string
		field  string
	}{
		{"missing disposition", func(r *DesignRecord) { r.Disposition = "" }, "MISSING_EXECUTION_DISPOSITION", "disposition"},
		{"invalid disposition", func(r *DesignRecord) { r.Disposition = "SOMETIMES" }, "INVALID_EXECUTION_DISPOSITION", "disposition"},
		{"unknown archetype", func(r *DesignRecord) { r.Archetype = "A77" }, "UNKNOWN_ARCHETYPE", "archetype"},
		{"missing profile", func(r *DesignRecord) { r.DomainProfile = "" }, "MISSING_DOMAIN_PROFILE", "domain_profile"},
		{"missing boundary", func(r *DesignRecord) { r.InputBoundary = "" }, "MISSING_INPUT_BOUNDARY", "input_boundary"},
		{"missing snapshot", func(r *DesignRecord) { r.SnapshotPolicy = "" }, "MISSING_SNAPSHOT_POLICY", "snapshot_policy"},
		{"missing engines", func(r *DesignRecord) { r.Engines = blank }, "MISSING_ENGINES", "engines"},
		{"missing human work", func(r *DesignRecord) { r.HumanWork = blank }, "MISSING_HUMAN_WORK", "human_work"},
		{"missing writes", func(r *DesignRecord) { r.Writes = blank }, "MISSING_WRITES", "writes"},
		{"missing waits", func(r *DesignRecord) { r.Waits = blank }, "MISSING_WAITS", "waits"},
		{"missing invalidators", func(r *DesignRecord) { r.Invalidators = blank }, "MISSING_INVALIDATORS", "invalidators"},
		{"missing reconciliation", func(r *DesignRecord) { r.Reconciliation = blank }, "MISSING_RECONCILIATION", "reconciliation"},
		{"missing correction", func(r *DesignRecord) { r.Correction = blank }, "MISSING_CORRECTION", "correction"},
		{"missing completion", func(r *DesignRecord) { r.Completion = "" }, "MISSING_COMPLETION", "completion"},
		{"bare not-applicable", func(r *DesignRecord) { r.Waits = Dimension{Value: "NOT_APPLICABLE"} }, "BARE_NOT_APPLICABLE", "waits"},
		{"reasoned not-applicable", func(r *DesignRecord) {
			r.Waits = Dimension{Value: "NOT_APPLICABLE", Reason: "NO_WAIT"}
		}, "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			record := completeRecord()
			tc.mutate(&record)
			report := ValidateRecords([]DesignRecord{record})
			if tc.code == "" {
				if !report.OK() {
					t.Errorf("reasoned NOT_APPLICABLE rejected: %+v", report.Findings)
				}
				return
			}
			if !hasFinding(report.Findings, "ChangeManager", tc.code, tc.field) {
				t.Errorf("mutation %s accepted: %+v", tc.name, report.Findings)
			}
		})
	}
}

func TestTodo_WF_DISC_005_Golden(t *testing.T) {
	report := ValidateRecords(loadSeed(t))
	got, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile(filepath.Join("testdata", "seed", "golden.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != strings.TrimSpace(string(want)) {
		t.Fatalf("golden mismatch\nwant:\n%s\ngot:\n%s", want, got)
	}
}

func TestTodo_WF_DISC_005_Security(t *testing.T) {
	record := completeRecord()
	record.Disposition = "DIRECT"
	record.Archetype = "D1"
	record.HumanWork = Dimension{Value: "NOT_APPLICABLE", Reason: "NO_HUMAN_STEP"}
	report := ValidateRecords([]DesignRecord{record})
	if !report.OK() {
		t.Fatalf("direct disposition rejected: %+v", report.Findings)
	}
	forged := completeRecord()
	forged.Archetype = "A2; DROP TABLE"
	if !hasFinding(ValidateRecords([]DesignRecord{forged}).Findings, "ChangeManager", "UNKNOWN_ARCHETYPE", "archetype") {
		t.Fatal("injected archetype accepted")
	}
	silent := completeRecord()
	silent.Writes = Dimension{Value: "NOT_APPLICABLE", Reason: "   "}
	if !hasFinding(ValidateRecords([]DesignRecord{silent}).Findings, "ChangeManager", "BARE_NOT_APPLICABLE", "writes") {
		t.Fatal("whitespace reason accepted as explicit")
	}
}

func TestTodo_WF_DISC_005_Conformance(t *testing.T) {
	records := loadSeed(t)
	report := ValidateRecords(records)
	if !report.OK() {
		t.Fatalf("seed records rejected: %+v", report.Findings)
	}
	goRegistry, err := EmitGoRegistry(records)
	if err != nil {
		t.Fatal(err)
	}
	checkedGo, err := os.ReadFile(filepath.Join("testdata", "registry", "design_registry.go"))
	if err != nil {
		t.Fatal(err)
	}
	if goRegistry != string(checkedGo) {
		t.Fatal("checked-in Go registry is stale: regenerate it with the workflowdesign command")
	}
	protoRegistry, err := EmitProtoRegistry(records)
	if err != nil {
		t.Fatal(err)
	}
	checkedProto, err := os.ReadFile(filepath.Join("testdata", "registry", "design_registry.proto"))
	if err != nil {
		t.Fatal(err)
	}
	if protoRegistry != string(checkedProto) {
		t.Fatal("checked-in Protobuf registry is stale: regenerate it with the workflowdesign command")
	}
}

func TestTodo_WF_DISC_005_Mutation(t *testing.T) {
	before := ValidateRecords(loadSeed(t))
	mutated := loadSeed(t)
	for i := range mutated {
		if mutated[i].Intent == "ChangeManager" {
			mutated[i].Completion = ""
		}
	}
	after := ValidateRecords(mutated)
	if before.Digest == after.Digest {
		t.Fatal("dropped completion did not change the digest")
	}
	if !hasFinding(after.Findings, "ChangeManager", "MISSING_COMPLETION", "completion") {
		t.Fatalf("dropped completion accepted: %+v", after.Findings)
	}
	renamed := loadSeed(t)
	for i := range renamed {
		if renamed[i].Intent == "ChangeManager" {
			renamed[i].Intent = "ChangeManagerRenamed"
		}
	}
	if ValidateRecords(renamed).Digest == before.Digest {
		t.Fatal("renamed intent did not change the digest")
	}
}

func hasFinding(findings []Finding, intent, code, field string) bool {
	for _, finding := range findings {
		if finding.Intent == intent && finding.Code == code && (field == "" || finding.Field == field) {
			return true
		}
	}
	return false
}
