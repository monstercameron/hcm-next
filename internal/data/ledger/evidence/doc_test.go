package evidence_test

import (
	"bytes"
	"go/parser"
	"go/token"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/data/ledger/evidence"
)

// TestDocThereIsNoArchiveDependency proves the claim the package doc makes
// about the layout: a package is a path-to-bytes map, and binding evidence
// to an archive format would make its digest a function of somebody's
// compression settings. The proof is the import list of the package's own
// source, because a comment cannot enforce it.
func TestDocThereIsNoArchiveDependency(t *testing.T) {
	t.Parallel()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package directory: %v", err)
	}

	forbidden := []string{"archive/", "compress/", "net/", "os/exec", "database/sql"}
	fset := token.NewFileSet()
	seen := 0
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, name, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		seen++
		for _, spec := range file.Imports {
			path, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				t.Fatalf("%s: unquote import: %v", name, err)
			}
			for _, bad := range forbidden {
				if strings.HasPrefix(path, bad) {
					t.Errorf("%s imports %s; an evidence package is bytes at paths, assembled from a Querier and verified offline", name, path)
				}
			}
		}
	}
	if seen == 0 {
		t.Fatal("no package source was parsed, so nothing was proved")
	}
}

// TestDocNothingRecordsWhenTheExportRan proves the purity claim: no export
// instant, no exporter identity and no random identifier enters any part, so
// a difference between two copies of a package is always a difference in the
// evidence.
func TestDocNothingRecordsWhenTheExportRan(t *testing.T) {
	t.Parallel()
	pkg, err := evidence.Build(goldenContent(t))
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	for _, path := range pkg.Paths() {
		raw, _ := pkg.Part(path)
		for _, forbidden := range []string{
			"exported_at", "generated_at", "export_id", "exported_by", "package_id",
		} {
			if bytes.Contains(raw, []byte(forbidden)) {
				t.Errorf("%s records %q, which would make two exports of one window differ", path, forbidden)
			}
		}
	}

	// When a checkpoint was taken is recorded where it belongs: inside the
	// signed epochs.
	epoch, _ := pkg.Part(evidence.EpochPath(0))
	if !bytes.Contains(epoch, []byte("CreatedAt")) {
		t.Error("the signed epoch does not record when it was created")
	}
}

// TestDocATenantAppearsInsideEveryPart proves the fail-closed claim: the
// tenant is named inside every part rather than only in the header, which is
// what lets an offline verifier refuse a part that belongs to somebody else
// even if the package was assembled by a path that bypassed row level
// security.
func TestDocATenantAppearsInsideEveryPart(t *testing.T) {
	t.Parallel()
	pkg, err := evidence.Build(goldenContent(t))
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	tenant := []byte(goldenTenant.String())
	for _, path := range pkg.Paths() {
		raw, _ := pkg.Part(path)
		if !bytes.Contains(raw, tenant) {
			t.Errorf("%s does not name the tenant it belongs to", path)
		}
	}
}

// TestDocTheDigestsNestFourDeep proves the doc's central claim: four
// independent bindings have to be broken at once to alter a covered event
// without the verifier naming it. Each is broken on its own here, and each
// is reported.
func TestDocTheDigestsNestFourDeep(t *testing.T) {
	t.Parallel()
	pkg, err := evidence.Build(goldenContent(t))
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	dir := goldenKeyDirectory(t)

	t.Run("the part digest in the manifest", func(t *testing.T) {
		report := evidence.Verify(mutate(t, pkg.Files(), evidence.StreamEventsPath(0), flipOneByte), dir)
		if !report.Has(evidence.FindingTamperedPart) {
			t.Fatalf("reported %v", kinds(report))
		}
	})

	t.Run("the manifest's own fold over every part row", func(t *testing.T) {
		files := mutate(t, pkg.Files(), evidence.ManifestPath, func(raw []byte) []byte {
			return rewriteJSON(t, raw, func(doc map[string]any) {
				doc["digest"] = strings.Repeat("a", 64)
			})
		})
		report := evidence.Verify(files, dir)
		if !report.Has(evidence.FindingManifestDigest) {
			t.Fatalf("reported %v", kinds(report))
		}
	})

	t.Run("the event digest the chain folds", func(t *testing.T) {
		files := mutate(t, pkg.Files(), evidence.StreamEventsPath(0), func(raw []byte) []byte {
			return rewriteJSON(t, raw, func(doc map[string]any) {
				doc["events"].([]any)[0].(map[string]any)["digest"] = strings.Repeat("b", 64)
			})
		})
		report := evidence.Verify(files, dir)
		if !report.Has(evidence.FindingTamperedEvent) {
			t.Fatalf("reported %v", kinds(report))
		}
	})

	t.Run("the chain hash a signed epoch attests to", func(t *testing.T) {
		// The chain is edited to agree with itself; what is left is the
		// signature, which this package never has the key for.
		files := mutate(t, pkg.Files(), evidence.StreamChainPath(0), func(raw []byte) []byte {
			return rewriteJSON(t, raw, func(doc map[string]any) {
				links := doc["links"].([]any)
				links[len(links)-1].(map[string]any)["chain_hash"] = strings.Repeat("c", 64)
			})
		})
		report := evidence.Verify(files, dir)
		if !report.Has(evidence.FindingBrokenChain) {
			t.Fatalf("reported %v, want a broken chain", kinds(report))
		}
	})
}

// TestDocVerificationTakesOnlyBytesAndKeys proves the offline claim in the
// only way that matters: the entry point's parameters. An auditor verifies a
// package on a laptop that has never been near the production cell.
func TestDocVerificationTakesOnlyBytesAndKeys(t *testing.T) {
	t.Parallel()
	pkg, err := evidence.Build(goldenContent(t))
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	// Round-tripping through nothing but a bytes map is the whole interface.
	transported := map[string][]byte{}
	for path, raw := range pkg.Files() {
		transported[path] = append([]byte(nil), raw...)
	}
	mustVerify(t, transported, goldenKeyDirectory(t))
}
