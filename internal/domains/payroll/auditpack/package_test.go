package auditpack_test

import (
	"testing"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/ledger/evidence"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/payroll/auditpack"
)

func TestBuildRefusesATenantMismatchBetweenContentAndTotals(t *testing.T) {
	t.Parallel()
	content := goldenContent(t)
	totals, err := auditpack.ResolveFromContent(content, goldenTenant, goldenRunID)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	totals.Tenant = uuid.New()
	decision, err := auditpack.Reconcile(totals)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if _, err := auditpack.Build(content, totals, decision); err == nil {
		t.Fatal("Build accepted totals resolved for a different tenant than content")
	}
}

func TestBuildRefusesAnEmptyRunID(t *testing.T) {
	t.Parallel()
	content := goldenContent(t)
	totals, err := auditpack.ResolveFromContent(content, goldenTenant, goldenRunID)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	totals.RunID = ""
	decision, err := auditpack.Reconcile(totals)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	// Reconcile itself does not require a run id; Build still must.
	if _, err := auditpack.Build(content, totals, decision); err == nil {
		t.Fatal("Build accepted an empty run id")
	}
}

func TestPackageFilesPathsAndPartRoundTrip(t *testing.T) {
	t.Parallel()
	pkg := goldenPackage(t)

	files := pkg.Files()
	paths := pkg.Paths()
	if len(files) != len(paths) {
		t.Fatalf("Files() holds %d paths, Paths() lists %d", len(files), len(paths))
	}
	for _, path := range paths {
		raw, ok := pkg.Part(path)
		if !ok {
			t.Fatalf("Part(%s) reports absent, but Paths() lists it", path)
		}
		if fileRaw, ok := files[path]; !ok || string(fileRaw) != string(raw) {
			t.Fatalf("Files()[%s] disagrees with Part(%s)", path, path)
		}
	}
	if _, ok := pkg.Part("does/not/exist.json"); ok {
		t.Fatal("Part reported an unknown path as present")
	}
	if pkg.TotalBytes() == 0 {
		t.Fatal("TotalBytes reports zero for a real package")
	}

	// Open is Files' inverse.
	reopened, err := auditpack.Open(files)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if reopened.Manifest.Digest != pkg.Manifest.Digest {
		t.Fatalf("reopened manifest digest = %s, want %s", reopened.Manifest.Digest, pkg.Manifest.Digest)
	}
}

func TestVerifyRefusesAPackageWithNoManifest(t *testing.T) {
	t.Parallel()
	pkg := goldenPackage(t)
	files := pkg.Files()
	delete(files, auditpack.ManifestPath)
	report := auditpack.Verify(files, goldenKeyDirectory(t), goldenRunID)
	if report.OK() {
		t.Fatal("a package with no manifest verified")
	}
	if !report.Has(auditpack.FindingMalformedPart) {
		t.Fatalf("reported %v, want a malformed part", auditKinds(report))
	}
}

func TestVerifyRefusesAPackageWithNoSummary(t *testing.T) {
	t.Parallel()
	pkg := goldenPackage(t)
	files := pkg.Files()
	delete(files, auditpack.SummaryPath)
	report := auditpack.Verify(files, goldenKeyDirectory(t), goldenRunID)
	if report.OK() {
		t.Fatal("a package with no summary verified")
	}
	if !report.Has(auditpack.FindingMissingPart) {
		t.Fatalf("reported %v, want a missing part", auditKinds(report))
	}
}

func TestVerifyDetectsAnAlteredManifestDigest(t *testing.T) {
	t.Parallel()
	pkg := goldenPackage(t)
	files := mutate(t, pkg.Files(), auditpack.ManifestPath, func(raw []byte) []byte {
		return rewriteJSON(t, raw, func(doc map[string]any) {
			doc["digest"] = "0000000000000000000000000000000000000000000000000000000000000000"[:64]
		})
	})
	report := auditpack.Verify(files, goldenKeyDirectory(t), goldenRunID)
	if !report.Has(auditpack.FindingManifestDigest) {
		t.Fatalf("reported %v, want a manifest digest mismatch", auditKinds(report))
	}
}

func TestVerifyRequiresTheRequestedRunID(t *testing.T) {
	t.Parallel()
	pkg := goldenPackage(t)
	report := auditpack.Verify(pkg.Files(), goldenKeyDirectory(t), "a-different-run")
	if report.OK() {
		t.Fatal("verifying under the wrong run id succeeded")
	}
	if !report.Has(auditpack.FindingKeyMismatch) {
		t.Fatalf("reported %v, want an idempotency key mismatch", auditKinds(report))
	}
}

func TestVerifyNeedsNoDatabaseAtAll(t *testing.T) {
	t.Parallel()
	pkg := goldenPackage(t)
	// The signature of the offline entry point is the proof: bytes, a key
	// directory and a run id, nothing that could reach PostgreSQL.
	var verify func(map[string][]byte, auditpack.KeyDirectory, string) auditpack.Report = auditpack.Verify
	if report := verify(pkg.Files(), goldenKeyDirectory(t), goldenRunID); !report.OK() {
		t.Fatalf("offline verification reported %v", auditKinds(report))
	}
}

func TestVerifyRevokedKeyFailsTheWrappedEvidenceLayer(t *testing.T) {
	t.Parallel()
	pkg := goldenPackage(t)
	revoked := checkpointRevokedDirectory(t)
	report := auditpack.Verify(pkg.Files(), revoked, goldenRunID)
	if report.OK() {
		t.Fatal("a package signed by a since-revoked key verified")
	}
	if !report.Evidence.Has(evidence.FindingSignatureMismatch) {
		t.Fatalf("wrapped evidence reported %v, want a signature mismatch", kinds(report.Evidence))
	}
}
