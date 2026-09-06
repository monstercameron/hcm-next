package auditpack_test

import (
	"bytes"
	"testing"

	"github.com/monstercameron/hcm-next/internal/domains/payroll/auditpack"
)

// TestDocBuildRefusesAnUnexplainedVariance proves the doc's central release
// gate: Build never turns a run with an unexplained variance into a package,
// because a package exists to attest that a run reconciled.
func TestDocBuildRefusesAnUnexplainedVariance(t *testing.T) {
	t.Parallel()
	content := goldenContent(t)
	totals, err := auditpack.ResolveFromContent(content, goldenTenant, goldenRunID)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	// Corrupt the resolved filing acknowledgment total in place, standing in
	// for a caller that (incorrectly) hands Build totals it did not actually
	// derive from content.
	totals.Totals[auditpack.KindFilingAcknowledgment] = decimal(t, "1.00")

	decision, err := auditpack.Reconcile(totals)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if decision.OK() {
		t.Fatal("a corrupted total did not produce a variance")
	}
	if _, err := auditpack.Build(content, totals, decision); err == nil {
		t.Fatal("Build produced a package over an unexplained variance")
	}
}

// TestDocPackageIsAPureFunctionOfItsInputs proves the purity claim: no
// clock, exporter identity or random identifier enters a built package, so
// building the same content twice produces byte-identical bytes.
func TestDocPackageIsAPureFunctionOfItsInputs(t *testing.T) {
	t.Parallel()
	content := goldenContent(t)
	totals, err := auditpack.ResolveFromContent(content, goldenTenant, goldenRunID)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	decision, err := auditpack.Reconcile(totals)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	first, err := auditpack.Build(content, totals, decision)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	second, err := auditpack.Build(content, totals, decision)
	if err != nil {
		t.Fatalf("build: %v", err)
	}

	firstFiles, secondFiles := first.Files(), second.Files()
	if len(firstFiles) != len(secondFiles) {
		t.Fatalf("two builds produced %d and %d paths", len(firstFiles), len(secondFiles))
	}
	for path, raw := range firstFiles {
		other, ok := secondFiles[path]
		if !ok || !bytes.Equal(other, raw) {
			t.Fatalf("two builds of the same content differ at %s", path)
		}
	}
}

// TestDocNothingRecordsWhenTheBuildRan proves that, like the evidence
// package it wraps, no build instant or random identifier is recorded
// anywhere in the package.
func TestDocNothingRecordsWhenTheBuildRan(t *testing.T) {
	t.Parallel()
	pkg := goldenPackage(t)
	for _, path := range pkg.Paths() {
		raw, _ := pkg.Part(path)
		for _, forbidden := range []string{"built_at", "generated_at", "export_id", "exported_by", "package_id"} {
			if bytes.Contains(raw, []byte(forbidden)) {
				t.Errorf("%s records %q, which would make two builds of one run differ", path, forbidden)
			}
		}
	}
}
