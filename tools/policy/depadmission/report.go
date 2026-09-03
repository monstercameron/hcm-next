package depadmission

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/monstercameron/hcm-next/tools/policy/depmanifest"
)

// Overall report verdicts.
const (
	VerdictPass    = "PASS"
	VerdictFail    = "FAIL"
	VerdictSkipped = "SKIPPED_WITH_REASON"
)

// FindingDisposition is the admission decision Evaluate reaches for one
// vulnerability finding against one module.
type FindingDisposition struct {
	VulnerabilityID string `json:"vulnerability_id"`
	Reachable       bool   `json:"reachable"`
	Disposition     string `json:"disposition"`
	ExceptionOwner  string `json:"exception_owner,omitempty"`
	ExceptionExpiry string `json:"exception_expiry,omitempty"`
	Reason          string `json:"reason,omitempty"`
}

// ClassifyFinding applies vulnerability policy to one finding: reachable
// findings block admission unless a complete, unexpired, digest-bound
// exception covers this exact module/version/vulnerability triple.
// Unreachable findings are always recorded, never dropped (TOOL-024's RED
// clause forbids silently ignoring a module-level finding).
func ClassifyFinding(policy *Policy, f Finding, now time.Time) FindingDisposition {
	d := FindingDisposition{VulnerabilityID: f.OSV, Reachable: f.Reachable()}
	if !d.Reachable {
		d.Disposition = FindingRecorded
		return d
	}

	exc := policy.FindException(f.Module(), f.ModuleVersion(), f.OSV)
	switch {
	case exc == nil:
		d.Disposition = FindingBlocked
		d.Reason = "reachable and no exception on file"
	case len(exc.Complete()) > 0:
		d.Disposition = FindingBlocked
		d.Reason = fmt.Sprintf("exception incomplete: missing %s", strings.Join(exc.Complete(), ", "))
	case exc.Expired(now):
		d.Disposition = FindingBlocked
		d.Reason = fmt.Sprintf("exception expired %s", exc.Expiry)
	default:
		d.Disposition = FindingExcepted
		d.ExceptionOwner = exc.Owner
		d.ExceptionExpiry = exc.Expiry
	}
	return d
}

// ModuleEvaluation is every input Evaluate needs for one module. Callers
// assemble one of these per module in the build (from ListModules,
// DetectLicense, the depmanifest manifest, and the findings whose
// Finding.Module() matches).
type ModuleEvaluation struct {
	Module  string
	Version string

	LicenseSPDX        string
	LicenseSource      string // "detected" | "override" | "unknown"
	LicenseDisposition string // ALLOW | DENY | UNKNOWN

	// GovernanceRequired is true when this module is one go.mod's own
	// require block lists; dependency-roles.yaml's documented scope note
	// exempts purely transitive, unlisted modules from needing their own
	// governance row.
	GovernanceRequired  bool
	GovernanceFound     bool
	GovernanceMissing   []string
	SecurityOwner       string
	UpgradeSLA          string
	ReplacementStrategy string

	Findings []FindingDisposition
}

// EvaluateModuleLicense resolves the SPDX identifier, source, and
// allow/deny/unknown disposition for one module, applying a policy
// override before falling back to the detected license file.
func EvaluateModuleLicense(policy *Policy, modulePath string, detected LicenseResult) (spdx, source, disposition string) {
	if override, ok := policy.License.Overrides[modulePath]; ok {
		spdx = override.SPDX
		source = "override"
	} else if detected.Found && detected.SPDX != "" {
		spdx = detected.SPDX
		source = "detected"
	} else {
		spdx = ""
		source = "unknown"
	}
	disposition = policy.ClassifyLicense(spdx)
	return spdx, source, disposition
}

// EvaluateGovernance reports whether modulePath carries a complete
// dependency-roles.yaml row, reusing depmanifest's own classification and
// completeness check (LIB-001) rather than duplicating owner/SLA/
// replacement bookkeeping in a second manifest.
func EvaluateGovernance(manifest *depmanifest.Manifest, modulePath string, requiredByGoMod bool) (found bool, missing []string, row depmanifest.ModuleRow) {
	c := manifest.Classify(modulePath)
	if !c.Found {
		return false, nil, depmanifest.ModuleRow{}
	}
	if !requiredByGoMod {
		// Purely transitive modules outside go.mod's own require block are
		// out of dependency-roles.yaml's documented scope; a match here is
		// a bonus, not a requirement, so completeness is not enforced.
		return true, nil, c.Row
	}
	return true, depmanifest.RowIsComplete(c.Row), c.Row
}

// Verdict computes the PASS/FAIL verdict and reasons for m.
func (m ModuleEvaluation) Verdict() (string, []string) {
	var reasons []string

	switch m.LicenseDisposition {
	case DispositionDeny:
		reasons = append(reasons, fmt.Sprintf("license %s is on the deny list", m.LicenseSPDX))
	case DispositionUnknown:
		reasons = append(reasons, "license could not be determined: no recognized license file and no policy override")
	}

	if m.GovernanceRequired {
		if !m.GovernanceFound {
			reasons = append(reasons, "no dependency-roles.yaml row: dependency has no recorded security owner or upgrade path")
		} else if len(m.GovernanceMissing) > 0 {
			reasons = append(reasons, fmt.Sprintf("dependency-roles.yaml row incomplete: missing %s", strings.Join(m.GovernanceMissing, ", ")))
		}
	}

	for _, f := range m.Findings {
		if f.Disposition == FindingBlocked {
			reasons = append(reasons, fmt.Sprintf("vulnerability %s: %s", f.VulnerabilityID, f.Reason))
		}
	}

	if len(reasons) > 0 {
		return VerdictFail, reasons
	}
	return VerdictPass, nil
}

// ModuleReport is one module's evaluation plus its computed verdict, as it
// appears in Report.Modules.
type ModuleReport struct {
	ModuleEvaluation
	Verdict string   `json:"verdict"`
	Reasons []string `json:"reasons,omitempty"`
}

// MarshalJSON flattens ModuleEvaluation's fields alongside Verdict/Reasons
// so the report reads as one object per module rather than a nested
// "ModuleEvaluation" key; field order matches ModuleEvaluation's own
// declaration order, then Verdict, then Reasons, which is what keeps
// output byte-identical across runs.
func (m ModuleReport) MarshalJSON() ([]byte, error) {
	type alias struct {
		Module              string               `json:"module"`
		Version             string               `json:"version"`
		LicenseSPDX         string               `json:"license_spdx"`
		LicenseSource       string               `json:"license_source"`
		LicenseDisposition  string               `json:"license_disposition"`
		GovernanceRequired  bool                 `json:"governance_required"`
		GovernanceFound     bool                 `json:"governance_found"`
		GovernanceMissing   []string             `json:"governance_missing,omitempty"`
		SecurityOwner       string               `json:"security_owner,omitempty"`
		UpgradeSLA          string               `json:"upgrade_sla,omitempty"`
		ReplacementStrategy string               `json:"replacement_strategy,omitempty"`
		Findings            []FindingDisposition `json:"findings,omitempty"`
		Verdict             string               `json:"verdict"`
		Reasons             []string             `json:"reasons,omitempty"`
	}
	return json.Marshal(alias{
		Module:              m.Module,
		Version:             m.Version,
		LicenseSPDX:         m.LicenseSPDX,
		LicenseSource:       m.LicenseSource,
		LicenseDisposition:  m.LicenseDisposition,
		GovernanceRequired:  m.GovernanceRequired,
		GovernanceFound:     m.GovernanceFound,
		GovernanceMissing:   m.GovernanceMissing,
		SecurityOwner:       m.SecurityOwner,
		UpgradeSLA:          m.UpgradeSLA,
		ReplacementStrategy: m.ReplacementStrategy,
		Findings:            m.Findings,
		Verdict:             m.Verdict,
		Reasons:             m.Reasons,
	})
}

// VulnEvidence describes whether, and how, vulnerability scan evidence
// backed this report.
type VulnEvidence struct {
	// Status is PROVIDED, SKIPPED_WITH_REASON (no scan evidence given: the
	// local checker's honest default, never reported as PASS) or
	// INCOMPLETE (evidence given but missing the scanner/database identity
	// TOOL-024 requires).
	Status            string `json:"status"`
	Reason            string `json:"reason,omitempty"`
	ScannerName       string `json:"scanner_name,omitempty"`
	ScannerVersion    string `json:"scanner_version,omitempty"`
	DB                string `json:"db,omitempty"`
	ModuleGraphDigest string `json:"module_graph_digest,omitempty"`
}

const (
	VulnEvidenceProvided   = "PROVIDED"
	VulnEvidenceSkipped    = "SKIPPED_WITH_REASON"
	VulnEvidenceIncomplete = "INCOMPLETE"
)

// Report is the deterministic, serializable output of one admission run.
type Report struct {
	Modules      []ModuleReport `json:"modules"`
	VulnEvidence VulnEvidence   `json:"vuln_evidence"`
	Verdict      string         `json:"verdict"`
}

// BuildReport sorts modules by path and each module's findings by
// vulnerability ID, computes every module's verdict, and rolls them up
// into one overall verdict. The sort is what makes two runs over the same
// (unordered) inputs produce byte-identical JSON.
func BuildReport(modules []ModuleEvaluation, vulnEvidence VulnEvidence) Report {
	sorted := make([]ModuleEvaluation, len(modules))
	copy(sorted, modules)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Module < sorted[j].Module })

	reports := make([]ModuleReport, 0, len(sorted))
	anyModuleFailed := false
	for _, m := range sorted {
		findings := make([]FindingDisposition, len(m.Findings))
		copy(findings, m.Findings)
		sort.Slice(findings, func(i, j int) bool { return findings[i].VulnerabilityID < findings[j].VulnerabilityID })
		m.Findings = findings

		verdict, reasons := m.Verdict()
		if verdict == VerdictFail {
			anyModuleFailed = true
		}
		reports = append(reports, ModuleReport{ModuleEvaluation: m, Verdict: verdict, Reasons: reasons})
	}

	overall := VerdictPass
	switch {
	case anyModuleFailed:
		overall = VerdictFail
	case vulnEvidence.Status == VulnEvidenceIncomplete:
		overall = VerdictFail
	case vulnEvidence.Status == VulnEvidenceSkipped:
		overall = VerdictSkipped
	}

	return Report{Modules: reports, VulnEvidence: vulnEvidence, Verdict: overall}
}

// MarshalDeterministic serializes r with two-space indentation and a
// trailing newline, suitable for writing to a report file that a
// reviewer, or a future run's diff, can compare byte-for-byte.
func (r Report) MarshalDeterministic() ([]byte, error) {
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("depadmission: marshaling report: %w", err)
	}
	return append(data, '\n'), nil
}
