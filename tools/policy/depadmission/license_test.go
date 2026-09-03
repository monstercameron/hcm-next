package depadmission_test

import (
	"path/filepath"
	"testing"

	"github.com/monstercameron/hcm-next/tools/policy/depadmission"
	"github.com/monstercameron/hcm-next/tools/policy/depmanifest"
	"github.com/monstercameron/hcm-next/tools/policy/internal/repopath"
)

func fixtureDir(t *testing.T, elem ...string) string {
	t.Helper()
	return filepath.Join(append([]string{"testdata"}, elem...)...)
}

// TestTodo_TOOL_019 is the TOOL-019 primary test: it proves the license
// classifier against synthetic fixture directories (RED/GREEN on
// detection itself) and proves that the combined admission policy rejects
// a prohibited license, a dependency with no recorded security owner or
// upgrade path, and admits a complete, allow-listed dependency (the
// RED/GREEN clauses TOOL-019's todo entry names).
func TestTodo_TOOL_019(t *testing.T) {
	t.Run("DetectLicense classifies every recognized family", func(t *testing.T) {
		cases := []struct {
			dir  string
			want string
		}{
			{"mit", depadmission.SPDXMIT},
			{"apache2", depadmission.SPDXApache20},
			{"bsd3", depadmission.SPDXBSD3},
			{"bsd2", depadmission.SPDXBSD2},
			{"isc", depadmission.SPDXISC},
			{"mpl2", depadmission.SPDXMPL20},
			{"gpl3", depadmission.SPDXGPL3},
			{"agpl3", depadmission.SPDXAGPL3},
			{"sspl", depadmission.SPDXSSPL1},
		}
		for _, tc := range cases {
			t.Run(tc.dir, func(t *testing.T) {
				result, err := depadmission.DetectLicense(fixtureDir(t, "licenses", tc.dir))
				if err != nil {
					t.Fatalf("DetectLicense(%s): %v", tc.dir, err)
				}
				if !result.Found {
					t.Fatalf("DetectLicense(%s): Found = false, want true", tc.dir)
				}
				if result.SPDX != tc.want {
					t.Errorf("DetectLicense(%s): SPDX = %q, want %q", tc.dir, result.SPDX, tc.want)
				}
			})
		}
	})

	t.Run("RED: no license file found is reported, never guessed", func(t *testing.T) {
		result, err := depadmission.DetectLicense(fixtureDir(t, "licenses", "none"))
		if err != nil {
			t.Fatalf("DetectLicense: %v", err)
		}
		if result.Found {
			t.Fatalf("DetectLicense(none): Found = true, want false (no LICENSE/COPYING file present)")
		}
		if result.SPDX != "" {
			t.Fatalf("DetectLicense(none): SPDX = %q, want empty", result.SPDX)
		}
	})

	t.Run("RED: unrecognized license text classifies as unknown, not allowed", func(t *testing.T) {
		result, err := depadmission.DetectLicense(fixtureDir(t, "licenses", "unrecognized"))
		if err != nil {
			t.Fatalf("DetectLicense: %v", err)
		}
		if !result.Found {
			t.Fatalf("DetectLicense(unrecognized): Found = false, want true (a file exists)")
		}
		if result.SPDX != "" {
			t.Fatalf("DetectLicense(unrecognized): SPDX = %q, want empty (unrecognized text)", result.SPDX)
		}
	})

	policy := testPolicy(t)

	t.Run("RED: prohibited license fails admission", func(t *testing.T) {
		detected, err := depadmission.DetectLicense(fixtureDir(t, "licenses", "gpl3"))
		if err != nil {
			t.Fatalf("DetectLicense: %v", err)
		}
		spdx, source, disposition := depadmission.EvaluateModuleLicense(policy, "example.com/copyleft-thing", detected)
		if disposition != depadmission.DispositionDeny {
			t.Fatalf("ClassifyLicense(%s) = %s, want DENY", spdx, disposition)
		}
		eval := depadmission.ModuleEvaluation{
			Module: "example.com/copyleft-thing", Version: "v1.0.0",
			LicenseSPDX: spdx, LicenseSource: source, LicenseDisposition: disposition,
		}
		verdict, reasons := eval.Verdict()
		if verdict != depadmission.VerdictFail {
			t.Fatalf("Verdict() = %s, want FAIL; reasons=%v", verdict, reasons)
		}
		if len(reasons) == 0 {
			t.Fatal("Verdict() returned FAIL with no reasons")
		}
	})

	t.Run("RED: dependency without a governance row (security owner/upgrade path) fails admission", func(t *testing.T) {
		detected, err := depadmission.DetectLicense(fixtureDir(t, "licenses", "mit"))
		if err != nil {
			t.Fatalf("DetectLicense: %v", err)
		}
		spdx, source, disposition := depadmission.EvaluateModuleLicense(policy, "example.com/ungoverned-thing", detected)
		manifest := emptyManifest()
		found, missing, row := depadmission.EvaluateGovernance(manifest, "example.com/ungoverned-thing", true /* required by go.mod */)

		eval := depadmission.ModuleEvaluation{
			Module: "example.com/ungoverned-thing", Version: "v1.0.0",
			LicenseSPDX: spdx, LicenseSource: source, LicenseDisposition: disposition,
			GovernanceRequired: true, GovernanceFound: found, GovernanceMissing: missing,
			SecurityOwner: row.SecurityOwner, UpgradeSLA: row.UpgradeSLA, ReplacementStrategy: row.ReplacementStrategy,
		}
		verdict, reasons := eval.Verdict()
		if verdict != depadmission.VerdictFail {
			t.Fatalf("Verdict() = %s, want FAIL (no dependency-roles.yaml row); reasons=%v", verdict, reasons)
		}
		if found {
			t.Fatal("EvaluateGovernance found a row in an intentionally empty manifest")
		}
	})

	t.Run("GREEN: allow-listed license with a complete governance row is admitted, recording rationale/pin/owner/SLA/replacement", func(t *testing.T) {
		detected, err := depadmission.DetectLicense(fixtureDir(t, "licenses", "mit"))
		if err != nil {
			t.Fatalf("DetectLicense: %v", err)
		}
		spdx, source, disposition := depadmission.EvaluateModuleLicense(policy, "example.com/well-governed-thing", detected)
		if disposition != depadmission.DispositionAllow {
			t.Fatalf("ClassifyLicense(%s) = %s, want ALLOW", spdx, disposition)
		}

		manifest := manifestWithRow(t, depmanifest.ModuleRow{
			Path:                "example.com/well-governed-thing",
			Version:             "v1.0.0",
			Role:                depmanifest.RoleInfrastructureMechanic,
			SemanticOwner:       "platform-foundation",
			LicenseOwner:        "platform-foundation",
			SecurityOwner:       "platform-foundation",
			UpgradeSLA:          "security patch within 14 days",
			Exposure:            "supporting mechanics",
			ReplacementStrategy: "no blanket replacement",
		})
		found, missing, row := depadmission.EvaluateGovernance(manifest, "example.com/well-governed-thing", true)
		if !found || len(missing) != 0 {
			t.Fatalf("EvaluateGovernance: found=%v missing=%v, want found with no missing fields", found, missing)
		}

		eval := depadmission.ModuleEvaluation{
			Module: "example.com/well-governed-thing", Version: "v1.0.0",
			LicenseSPDX: spdx, LicenseSource: source, LicenseDisposition: disposition,
			GovernanceRequired: true, GovernanceFound: found, GovernanceMissing: missing,
			SecurityOwner: row.SecurityOwner, UpgradeSLA: row.UpgradeSLA, ReplacementStrategy: row.ReplacementStrategy,
		}
		verdict, reasons := eval.Verdict()
		if verdict != depadmission.VerdictPass {
			t.Fatalf("Verdict() = %s, want PASS; reasons=%v", verdict, reasons)
		}
		if eval.SecurityOwner == "" || eval.UpgradeSLA == "" || eval.ReplacementStrategy == "" {
			t.Fatalf("accepted module missing recorded owner/SLA/replacement: %+v", eval)
		}
	})

	t.Run("a policy override resolves a module with no detectable license file", func(t *testing.T) {
		none, err := depadmission.DetectLicense(fixtureDir(t, "licenses", "none"))
		if err != nil {
			t.Fatalf("DetectLicense: %v", err)
		}
		spdx, source, disposition := depadmission.EvaluateModuleLicense(policy, "gopkg.in/yaml.v3", none)
		if source != "override" {
			t.Fatalf("LicenseSource = %q, want %q", source, "override")
		}
		if spdx != depadmission.SPDXMIT {
			t.Fatalf("overridden SPDX = %q, want %q", spdx, depadmission.SPDXMIT)
		}
		if disposition != depadmission.DispositionAllow {
			t.Fatalf("overridden disposition = %q, want ALLOW", disposition)
		}
	})
}

// TestTodo_TOOL_019_Security proves the supply-chain-security-relevant
// edge of TOOL-019: an unknown license (no recognized file, and no
// reviewed override) can never be silently treated as admissible, however
// the policy's allow/deny lists are configured.
func TestTodo_TOOL_019_Security(t *testing.T) {
	policy := testPolicy(t)
	if policy.License.UnknownDisposition != "FAIL" {
		t.Fatalf("policy license.unknown_license_disposition = %q, want FAIL", policy.License.UnknownDisposition)
	}

	unrecognized, err := depadmission.DetectLicense(fixtureDir(t, "licenses", "unrecognized"))
	if err != nil {
		t.Fatalf("DetectLicense: %v", err)
	}
	_, _, disposition := depadmission.EvaluateModuleLicense(policy, "example.com/no-known-license", unrecognized)
	if disposition != depadmission.DispositionUnknown {
		t.Fatalf("ClassifyLicense(unrecognized) = %s, want UNKNOWN", disposition)
	}

	eval := depadmission.ModuleEvaluation{
		Module: "example.com/no-known-license", Version: "v0.0.1",
		LicenseDisposition: disposition,
	}
	verdict, reasons := eval.Verdict()
	if verdict != depadmission.VerdictFail {
		t.Fatalf("an UNKNOWN license disposition produced verdict %s, want FAIL; reasons=%v", verdict, reasons)
	}

	// Also prove an empty policy (no allow/deny entries configured at all,
	// e.g. a future malformed or empty policy file) still fails an
	// otherwise-permissive MIT license rather than defaulting open.
	empty := &depadmission.Policy{}
	mit, err := depadmission.DetectLicense(fixtureDir(t, "licenses", "mit"))
	if err != nil {
		t.Fatalf("DetectLicense: %v", err)
	}
	_, _, disposition = depadmission.EvaluateModuleLicense(empty, "example.com/would-be-fine", mit)
	if disposition != depadmission.DispositionUnknown {
		t.Fatalf("an empty policy classified a real MIT license as %s, want UNKNOWN (fail closed)", disposition)
	}
}

// testPolicy loads the repository's real dependency-admission.yaml so
// TOOL-019's tests exercise the policy this repository actually ships,
// not a hand-rolled stand-in that could drift from it.
func testPolicy(t *testing.T) *depadmission.Policy {
	t.Helper()
	p, err := depadmission.LoadPolicy(filepath.Join(repopath.RootDir(), "definitions", "architecture", "dependency-admission.yaml"))
	if err != nil {
		t.Fatalf("loading dependency-admission.yaml: %v", err)
	}
	return p
}

func emptyManifest() *depmanifest.Manifest {
	return &depmanifest.Manifest{Version: 1, Module: "example.com/empty"}
}

func manifestWithRow(t *testing.T, row depmanifest.ModuleRow) *depmanifest.Manifest {
	t.Helper()
	return &depmanifest.Manifest{
		Version: 1,
		Module:  "example.com/empty",
		Modules: []depmanifest.ModuleRow{row},
	}
}
