package main

import (
	"bytes"
	"testing"
	"time"

	"github.com/monstercameron/hcm-next/tools/policy/depadmission"
	"github.com/monstercameron/hcm-next/tools/policy/internal/repopath"
)

// TestRunAgainstRealRepository exercises the full CLI pipeline (go.mod's
// require block -> license detection from the real module cache ->
// dependency-roles.yaml governance -> report) against this repository's
// own go.mod, definitions/architecture/dependency-admission.yaml and
// definitions/architecture/dependency-roles.yaml, with no govulncheck
// evidence (the offline default). It requires the module cache to already
// hold every module go.mod requires, true after `go build ./...`, and
// makes no network request itself.
func TestRunAgainstRealRepository(t *testing.T) {
	root := repopath.RootDir()
	policyPath := root + "/definitions/architecture/dependency-admission.yaml"
	manifestPath := root + "/definitions/architecture/dependency-roles.yaml"
	now := time.Now()

	report, err := Run(root, policyPath, manifestPath, "", now)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if len(report.Modules) == 0 {
		t.Fatal("Run reported zero modules; expected go.mod's require block to produce at least one")
	}
	if report.VulnEvidence.Status != depadmission.VulnEvidenceSkipped {
		t.Fatalf("VulnEvidence.Status = %s, want SKIPPED_WITH_REASON (no -govulncheck-json given)", report.VulnEvidence.Status)
	}
	if report.VulnEvidence.ModuleGraphDigest == "" {
		t.Fatal("VulnEvidence.ModuleGraphDigest is empty; ModuleGraphDigest(root) should have succeeded against the real go.sum")
	}

	for _, m := range report.Modules {
		if m.LicenseDisposition == "" {
			t.Errorf("module %s has no license disposition", m.Module)
		}
		if m.Verdict == "" {
			t.Errorf("module %s has no verdict", m.Module)
		}
	}

	first, err := report.MarshalDeterministic()
	if err != nil {
		t.Fatalf("MarshalDeterministic: %v", err)
	}
	reportAgain, err := Run(root, policyPath, manifestPath, "", now)
	if err != nil {
		t.Fatalf("Run (second call): %v", err)
	}
	second, err := reportAgain.MarshalDeterministic()
	if err != nil {
		t.Fatalf("MarshalDeterministic (second call): %v", err)
	}
	if !bytes.Equal(first, second) {
		t.Fatal("two Run() calls over the same repository state produced different report bytes")
	}
}
