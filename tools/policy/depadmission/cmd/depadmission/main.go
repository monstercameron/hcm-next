// Command depadmission is the TOOL-019/TOOL-024 dependency admission gate.
// It classifies every module in the build's license and, when given
// `govulncheck -json` evidence, its reachable vulnerabilities, against
// definitions/architecture/dependency-admission.yaml and
// definitions/architecture/dependency-roles.yaml, and writes a
// deterministic report.
//
// Usage:
//
//	go run ./tools/policy/depadmission \
//	  -root . \
//	  -govulncheck-json govulncheck.json \
//	  -require-vuln-evidence \
//	  -out dependency-admission-report.json
//
// -govulncheck-json and -require-vuln-evidence are both optional. Without
// -govulncheck-json the report's vuln_evidence carries
// SKIPPED_WITH_REASON, never PASS, matching golang.org/x/vuln's own
// network requirement: the offline development machine cannot reach the
// vulnerability database, so CI (which can) is the only place that flag is
// set. Without -require-vuln-evidence a SKIPPED_WITH_REASON report still
// exits 0, so local runs of this tool (and `go test`) never depend on
// network access; CI passes -require-vuln-evidence so missing evidence
// there is a hard failure.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/monstercameron/human-capital-management-suite/tools/policy/depadmission"
	"github.com/monstercameron/human-capital-management-suite/tools/policy/depmanifest"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr *os.File) int {
	fs := flag.NewFlagSet("depadmission", flag.ContinueOnError)
	fs.SetOutput(stderr)
	root := fs.String("root", ".", "repository root (containing go.mod)")
	policyPath := fs.String("policy", "", "path to dependency-admission.yaml (default: <root>/definitions/architecture/dependency-admission.yaml)")
	manifestPath := fs.String("manifest", "", "path to dependency-roles.yaml (default: <root>/definitions/architecture/dependency-roles.yaml)")
	govulncheckJSON := fs.String("govulncheck-json", "", "path to `govulncheck -json` output; omitted means SKIPPED_WITH_REASON")
	requireVulnEvidence := fs.Bool("require-vuln-evidence", false, "treat missing/incomplete govulncheck evidence as a failure (set in CI, never locally)")
	out := fs.String("out", "", "report output path (default: stdout)")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	if *policyPath == "" {
		*policyPath = filepath.Join(*root, "definitions", "architecture", "dependency-admission.yaml")
	}
	if *manifestPath == "" {
		*manifestPath = filepath.Join(*root, "definitions", "architecture", "dependency-roles.yaml")
	}

	report, err := Run(*root, *policyPath, *manifestPath, *govulncheckJSON, time.Now())
	if err != nil {
		fmt.Fprintf(stderr, "depadmission: %v\n", err)
		return 2
	}

	data, err := report.MarshalDeterministic()
	if err != nil {
		fmt.Fprintf(stderr, "depadmission: %v\n", err)
		return 2
	}

	if *out == "" {
		if _, werr := stdout.Write(data); werr != nil {
			fmt.Fprintf(stderr, "depadmission: writing report: %v\n", werr)
			return 2
		}
	} else if werr := os.WriteFile(*out, data, 0o644); werr != nil {
		fmt.Fprintf(stderr, "depadmission: writing report to %s: %v\n", *out, werr)
		return 2
	}

	switch report.Verdict {
	case depadmission.VerdictPass:
		return 0
	case depadmission.VerdictSkipped:
		if *requireVulnEvidence {
			fmt.Fprintln(stderr, "depadmission: vulnerability evidence required (-require-vuln-evidence) but report is SKIPPED_WITH_REASON")
			return 1
		}
		return 0
	default:
		return 1
	}
}

// Run performs one full admission evaluation: it lists the build's
// modules, detects each one's license, joins dependency-roles.yaml
// governance, optionally parses govulncheck evidence, and returns the
// resulting deterministic report. It is exported (lowercase-package
// internal to main, but unit-testable from this package's own tests) so
// tests can exercise the full pipeline without shelling out to `go run`.
func Run(root, policyPath, manifestPath, govulncheckJSONPath string, now time.Time) (depadmission.Report, error) {
	policy, err := depadmission.LoadPolicy(policyPath)
	if err != nil {
		return depadmission.Report{}, err
	}
	manifest, err := depmanifest.Load(manifestPath)
	if err != nil {
		return depadmission.Report{}, err
	}
	requires, err := depmanifest.ParseGoModRequires(root)
	if err != nil {
		return depadmission.Report{}, err
	}
	required := make(map[string]bool, len(requires))
	modulePaths := make([]string, 0, len(requires))
	for _, r := range requires {
		required[r.Path] = true
		modulePaths = append(modulePaths, r.Path)
	}

	// go.mod's own require block, not `go list -m -json all`: see
	// ListModules's doc comment for why the "all" pattern is unsafe here.
	modules, err := depadmission.ListModules(root, modulePaths)
	if err != nil {
		return depadmission.Report{}, err
	}

	vulnEvidence, findingsByModule, err := loadVulnEvidence(root, govulncheckJSONPath, now)
	if err != nil {
		return depadmission.Report{}, err
	}

	evaluations := make([]depadmission.ModuleEvaluation, 0, len(modules))
	for _, mod := range modules {
		if mod.Main {
			continue
		}

		var detected depadmission.LicenseResult
		if mod.Dir != "" {
			detected, err = depadmission.DetectLicense(mod.Dir)
			if err != nil {
				return depadmission.Report{}, err
			}
		}
		spdx, source, disposition := depadmission.EvaluateModuleLicense(policy, mod.Path, detected)

		found, missing, row := depadmission.EvaluateGovernance(manifest, mod.Path, required[mod.Path])

		var findings []depadmission.FindingDisposition
		for _, f := range findingsByModule[mod.Path] {
			findings = append(findings, depadmission.ClassifyFinding(policy, f, now))
		}

		evaluations = append(evaluations, depadmission.ModuleEvaluation{
			Module:              mod.Path,
			Version:             mod.Version,
			LicenseSPDX:         spdx,
			LicenseSource:       source,
			LicenseDisposition:  disposition,
			GovernanceRequired:  required[mod.Path],
			GovernanceFound:     found,
			GovernanceMissing:   missing,
			SecurityOwner:       row.SecurityOwner,
			UpgradeSLA:          row.UpgradeSLA,
			ReplacementStrategy: row.ReplacementStrategy,
			Findings:            findings,
		})
	}

	return depadmission.BuildReport(evaluations, vulnEvidence), nil
}

func loadVulnEvidence(root, govulncheckJSONPath string, now time.Time) (depadmission.VulnEvidence, map[string][]depadmission.Finding, error) {
	digest, digestErr := depadmission.ModuleGraphDigest(root)

	if govulncheckJSONPath == "" {
		evidence := depadmission.VulnEvidence{
			Status: depadmission.VulnEvidenceSkipped,
			Reason: "no -govulncheck-json evidence provided; govulncheck requires network access to golang.org/x/vuln's database and runs in CI only",
		}
		if digestErr == nil {
			evidence.ModuleGraphDigest = digest
		}
		return evidence, nil, nil
	}

	scan, err := depadmission.ParseGovulncheckFile(govulncheckJSONPath)
	if err != nil {
		return depadmission.VulnEvidence{}, nil, err
	}

	byModule := make(map[string][]depadmission.Finding, len(scan.Findings))
	for _, f := range scan.Findings {
		byModule[f.Module()] = append(byModule[f.Module()], f)
	}

	if missing := depadmission.ConfigMissingFields(scan.Config); len(missing) > 0 {
		evidence := depadmission.VulnEvidence{
			Status: depadmission.VulnEvidenceIncomplete,
			Reason: fmt.Sprintf("govulncheck evidence missing scanner/database identity: %v", missing),
		}
		if digestErr == nil {
			evidence.ModuleGraphDigest = digest
		}
		return evidence, byModule, nil
	}

	evidence := depadmission.VulnEvidence{
		Status:         depadmission.VulnEvidenceProvided,
		ScannerName:    scan.Config.ScannerName,
		ScannerVersion: scan.Config.ScannerVersion,
		DB:             scan.Config.DB,
	}
	if digestErr == nil {
		evidence.ModuleGraphDigest = digest
	}
	return evidence, byModule, nil
}
