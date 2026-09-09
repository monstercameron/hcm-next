package depadmission_test

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/tools/policy/depadmission"
)

// moduleB and moduleA build the two fixture modules TestTodo_TOOL_019_Golden
// and TestTodo_TOOL_024_Golden both check different facets of. moduleA
// deliberately sorts before moduleB, and its findings are given here in
// descending vulnerability-ID order, so a passing test proves BuildReport
// performs the sort itself rather than the fixture happening to already be
// in order.
func moduleA() depadmission.ModuleEvaluation {
	return depadmission.ModuleEvaluation{
		Module: "example.com/aaa-module", Version: "v1.0.0",
		LicenseSPDX: depadmission.SPDXMIT, LicenseSource: "detected", LicenseDisposition: depadmission.DispositionAllow,
		GovernanceRequired: true, GovernanceFound: true,
		SecurityOwner: "platform-foundation", UpgradeSLA: "14 days", ReplacementStrategy: "none needed",
		Findings: []depadmission.FindingDisposition{
			{VulnerabilityID: "GO-2026-0002", Reachable: true, Disposition: depadmission.FindingBlocked, Reason: "reachable and no exception on file"},
			{VulnerabilityID: "GO-2026-0001", Reachable: false, Disposition: depadmission.FindingRecorded},
		},
	}
}

func moduleB() depadmission.ModuleEvaluation {
	return depadmission.ModuleEvaluation{
		Module: "example.com/bbb-module", Version: "v2.0.0",
		LicenseSPDX: depadmission.SPDXApache20, LicenseSource: "override", LicenseDisposition: depadmission.DispositionAllow,
		GovernanceRequired: true, GovernanceFound: true,
		SecurityOwner: "platform-foundation", UpgradeSLA: "30 days", ReplacementStrategy: "none needed",
	}
}

// TestTodo_TOOL_019_Golden pins the module-level admission fields TOOL-019
// requires an accepted dependency to record (license disposition, source,
// and the dependency-roles.yaml owner/SLA/replacement triple), and that
// BuildReport orders modules by path regardless of input order.
func TestTodo_TOOL_019_Golden(t *testing.T) {
	evidence := depadmission.VulnEvidence{Status: depadmission.VulnEvidenceSkipped, Reason: "no evidence in this fixture"}

	// moduleB first, moduleA second: BuildReport must still emit aaa before bbb.
	report := depadmission.BuildReport([]depadmission.ModuleEvaluation{moduleB(), moduleA()}, evidence)

	if len(report.Modules) != 2 {
		t.Fatalf("len(Modules) = %d, want 2", len(report.Modules))
	}
	if report.Modules[0].Module != "example.com/aaa-module" || report.Modules[1].Module != "example.com/bbb-module" {
		t.Fatalf("modules not sorted by path: got [%s, %s]", report.Modules[0].Module, report.Modules[1].Module)
	}

	bbb := report.Modules[1]
	if bbb.LicenseSPDX != depadmission.SPDXApache20 || bbb.LicenseSource != "override" || bbb.LicenseDisposition != depadmission.DispositionAllow {
		t.Fatalf("moduleB license fields drifted: %+v", bbb.ModuleEvaluation)
	}
	if bbb.SecurityOwner != "platform-foundation" || bbb.UpgradeSLA != "30 days" || bbb.ReplacementStrategy != "none needed" {
		t.Fatalf("moduleB did not record owner/SLA/replacement: %+v", bbb.ModuleEvaluation)
	}
	if bbb.Verdict != depadmission.VerdictPass {
		t.Fatalf("moduleB (clean allow-listed, fully governed, no findings) verdict = %s, want PASS", bbb.Verdict)
	}

	data, err := report.MarshalDeterministic()
	if err != nil {
		t.Fatalf("MarshalDeterministic: %v", err)
	}
	for _, key := range []string{
		`"license_spdx"`, `"license_source"`, `"license_disposition"`,
		`"security_owner"`, `"upgrade_sla"`, `"replacement_strategy"`,
		`"governance_required"`, `"governance_found"`, `"verdict"`,
	} {
		if !bytes.Contains(data, []byte(key)) {
			t.Errorf("report JSON is missing pinned field %s:\n%s", key, data)
		}
	}

	// A module carrying a reachable, unexcepted vulnerability fails
	// overall admission (TOOL-019's RED clause: a prohibited condition on
	// any accepted dependency must not silently pass).
	if report.Verdict != depadmission.VerdictFail {
		t.Fatalf("overall Verdict = %s, want FAIL (moduleA carries a blocked finding)", report.Verdict)
	}
}

// TestTodo_TOOL_024_Golden pins the vulnerability-evidence section of the
// report (scanner identity, module graph digest, and each finding's
// reachable/disposition/exception fields), the finding sort order within a
// module, and the three-way overall verdict rollup
// (PASS/FAIL/SKIPPED_WITH_REASON) driven by vuln evidence status.
func TestTodo_TOOL_024_Golden(t *testing.T) {
	provided := depadmission.VulnEvidence{
		Status: depadmission.VulnEvidenceProvided, ScannerName: "govulncheck", ScannerVersion: "v1.1.3",
		DB: "https://vuln.go.dev", ModuleGraphDigest: "sha256:deadbeef",
	}
	report := depadmission.BuildReport([]depadmission.ModuleEvaluation{moduleA()}, provided)

	if len(report.Modules) != 1 || len(report.Modules[0].Findings) != 2 {
		t.Fatalf("unexpected report shape: %+v", report)
	}
	findings := report.Modules[0].Findings
	if findings[0].VulnerabilityID != "GO-2026-0001" || findings[1].VulnerabilityID != "GO-2026-0002" {
		t.Fatalf("findings not sorted by vulnerability ID: got [%s, %s]", findings[0].VulnerabilityID, findings[1].VulnerabilityID)
	}
	if findings[0].Disposition != depadmission.FindingRecorded {
		t.Errorf("GO-2026-0001 disposition = %s, want RECORD (unreachable, never dropped)", findings[0].Disposition)
	}
	if findings[1].Disposition != depadmission.FindingBlocked {
		t.Errorf("GO-2026-0002 disposition = %s, want BLOCK (reachable, unexcepted)", findings[1].Disposition)
	}

	data, err := report.MarshalDeterministic()
	if err != nil {
		t.Fatalf("MarshalDeterministic: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("report is not valid JSON: %v", err)
	}
	vulnEvidence, ok := decoded["vuln_evidence"].(map[string]any)
	if !ok {
		t.Fatalf("report JSON has no vuln_evidence object: %s", data)
	}
	if vulnEvidence["module_graph_digest"] != "sha256:deadbeef" {
		t.Errorf("vuln_evidence.module_graph_digest = %v, want sha256:deadbeef", vulnEvidence["module_graph_digest"])
	}

	t.Run("PASS: clean module, provided evidence", func(t *testing.T) {
		r := depadmission.BuildReport([]depadmission.ModuleEvaluation{moduleB()}, provided)
		if r.Verdict != depadmission.VerdictPass {
			t.Fatalf("Verdict = %s, want PASS", r.Verdict)
		}
	})

	t.Run("SKIPPED_WITH_REASON: clean modules, no evidence given (the local/offline default, never PASS-by-omission)", func(t *testing.T) {
		r := depadmission.BuildReport([]depadmission.ModuleEvaluation{moduleB()}, depadmission.VulnEvidence{Status: depadmission.VulnEvidenceSkipped, Reason: "offline"})
		if r.Verdict != depadmission.VerdictSkipped {
			t.Fatalf("Verdict = %s, want SKIPPED_WITH_REASON", r.Verdict)
		}
		if !strings.Contains(string(mustMarshal(t, r)), `"SKIPPED_WITH_REASON"`) {
			t.Fatal("report does not literally spell out SKIPPED_WITH_REASON")
		}
	})

	t.Run("RED: INCOMPLETE evidence (present but missing scanner/database digests) fails even with otherwise-clean modules", func(t *testing.T) {
		r := depadmission.BuildReport([]depadmission.ModuleEvaluation{moduleB()}, depadmission.VulnEvidence{Status: depadmission.VulnEvidenceIncomplete, Reason: "missing scanner_version"})
		if r.Verdict != depadmission.VerdictFail {
			t.Fatalf("Verdict = %s, want FAIL (incomplete scan evidence must not be treated as a pass)", r.Verdict)
		}
	})

	t.Run("a reachable, blocked finding fails overall admission even when vuln evidence status is otherwise PROVIDED", func(t *testing.T) {
		if report.Verdict != depadmission.VerdictFail {
			t.Fatalf("Verdict = %s, want FAIL", report.Verdict)
		}
	})
}

func mustMarshal(t *testing.T, r depadmission.Report) []byte {
	t.Helper()
	data, err := r.MarshalDeterministic()
	if err != nil {
		t.Fatalf("MarshalDeterministic: %v", err)
	}
	return data
}
