package evidence_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	datalogger "github.com/monstercameron/human-capital-management-suite/internal/data/ledger"
	"github.com/monstercameron/human-capital-management-suite/internal/data/ledger/checkpoint"
	"github.com/monstercameron/human-capital-management-suite/internal/data/ledger/evidence"
)

// TestFindingRendersWhereAndWhatWithoutBeingParsed proves the contract the
// finding type states: a caller branches on the kind, and the rendering is
// for a human. Every located field it carries appears in the text, so a
// report tells an auditor where to look.
func TestFindingRendersWhereAndWhatWithoutBeingParsed(t *testing.T) {
	t.Parallel()
	f := evidence.Finding{
		Kind:        evidence.FindingBrokenChain,
		Part:        evidence.StreamChainPath(0),
		Stream:      streamOne,
		Sequence:    4,
		EpochNumber: 2,
		Detail:      "prior hash does not match",
		Expected:    "aaa",
		Actual:      "bbb",
	}
	rendered := f.String()
	for _, want := range []string{
		string(evidence.FindingBrokenChain), evidence.StreamChainPath(0),
		streamOne, "@4", "epoch 2", "prior hash does not match", "aaa", "bbb",
	} {
		if !strings.Contains(rendered, want) {
			t.Errorf("finding does not render %q: %s", want, rendered)
		}
	}
	// A finding is itself an error, so one can be returned on its own.
	var err error = f
	if err.Error() != rendered {
		t.Errorf("Error() = %q, want %q", err.Error(), rendered)
	}

	// A bare finding still renders something an auditor can read.
	bare := evidence.Finding{Kind: evidence.FindingTenantLeak}
	if bare.String() != string(evidence.FindingTenantLeak) {
		t.Errorf("a bare finding rendered %q", bare.String())
	}
}

// TestReportHasNoPartialPass proves the report's own rule: a package with no
// finding verified completely, and there is nothing in between.
func TestReportHasNoPartialPass(t *testing.T) {
	t.Parallel()
	clean := evidence.Report{}
	if !clean.OK() || clean.Err() != nil {
		t.Fatalf("an empty report is not a pass: OK=%t err=%v", clean.OK(), clean.Err())
	}
	if clean.Has(evidence.FindingTamperedPart) || clean.Of(evidence.FindingTamperedPart) != nil {
		t.Fatal("an empty report reported a finding")
	}

	failed := evidence.Report{Findings: []evidence.Finding{
		{Kind: evidence.FindingTamperedPart, Part: evidence.HeaderPath},
		{Kind: evidence.FindingBrokenChain, Stream: streamOne, Sequence: 2},
		{Kind: evidence.FindingTamperedPart, Part: evidence.EpochPath(0)},
	}}
	if failed.OK() {
		t.Fatal("a report carrying findings claimed to be a pass")
	}
	if got := len(failed.Of(evidence.FindingTamperedPart)); got != 2 {
		t.Fatalf("Of(TAMPERED_PART) returned %d findings, want 2", got)
	}
	if !failed.Has(evidence.FindingBrokenChain) {
		t.Fatal("Has missed a finding the report carries")
	}
	// Err joins every finding, so a caller that only wants pass/fail still
	// gets everything that was wrong.
	err := failed.Err()
	if err == nil {
		t.Fatal("Err() returned nil for a failing report")
	}
	for _, f := range failed.Findings {
		if !errors.Is(err, error(f)) && !strings.Contains(err.Error(), f.String()) {
			t.Errorf("the joined error dropped %s", f)
		}
	}
}

// TestVerifyRefusesALayoutItDoesNotKnow proves the version gate: a package
// written under a manifest or layout version this verifier has never seen is
// refused outright rather than checked with assumptions that may not hold.
func TestVerifyRefusesALayoutItDoesNotKnow(t *testing.T) {
	t.Parallel()
	pkg, err := evidence.Build(goldenContent(t))
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	dir := goldenKeyDirectory(t)

	t.Run("a future manifest schema version", func(t *testing.T) {
		files := mutate(t, pkg.Files(), evidence.ManifestPath, func(raw []byte) []byte {
			return rewriteJSON(t, raw, func(doc map[string]any) {
				doc["schema_version"] = float64(evidence.SchemaVersion + 1)
			})
		})
		report := evidence.Verify(files, dir)
		if !report.Has(evidence.FindingMalformedPart) {
			t.Fatalf("reported %v, want a malformed part", kinds(report))
		}
		// It stops there rather than reporting a cascade of checks it had no
		// business running.
		if len(report.Findings) != 1 {
			t.Fatalf("an unknown layout produced %d findings, want 1: %v", len(report.Findings), kinds(report))
		}
	})

	t.Run("a header whose layout disagrees with the manifest", func(t *testing.T) {
		files := mutate(t, pkg.Files(), evidence.HeaderPath, func(raw []byte) []byte {
			return rewriteJSON(t, raw, func(doc map[string]any) {
				doc["layout_version"] = float64(evidence.LayoutVersion + 1)
			})
		})
		report := evidence.Verify(files, dir)
		if !report.Has(evidence.FindingMalformedPart) {
			t.Fatalf("reported %v, want a malformed part", kinds(report))
		}
	})

	t.Run("a part digest algorithm this verifier does not support", func(t *testing.T) {
		files := mutate(t, pkg.Files(), evidence.ManifestPath, func(raw []byte) []byte {
			return rewriteJSON(t, raw, func(doc map[string]any) {
				doc["parts"].([]any)[0].(map[string]any)["algorithm"] = "md5"
			})
		})
		report := evidence.Verify(files, dir)
		if !report.Has(evidence.FindingMalformedPart) {
			t.Fatalf("reported %v, want a malformed part", kinds(report))
		}
	})

	t.Run("a manifest digest algorithm this verifier does not support", func(t *testing.T) {
		files := mutate(t, pkg.Files(), evidence.ManifestPath, func(raw []byte) []byte {
			return rewriteJSON(t, raw, func(doc map[string]any) {
				doc["digest_algorithm"] = "md5"
			})
		})
		report := evidence.Verify(files, dir)
		if !report.Has(evidence.FindingMalformedPart) {
			t.Fatalf("reported %v, want a malformed part", kinds(report))
		}
	})
}

// TestVerifyReportsEveryFailureInOnePass proves the design decision behind
// the report: a verifier that stopped at the first problem would make an
// auditor iterate, so one pass names everything that is wrong.
func TestVerifyReportsEveryFailureInOnePass(t *testing.T) {
	t.Parallel()
	pkg, err := evidence.Build(goldenContent(t))
	if err != nil {
		t.Fatalf("build: %v", err)
	}

	files := mutate(t, pkg.Files(), evidence.StreamChainPath(0), flipOneByte)
	files = mutate(t, files, evidence.StreamEventsPath(1), flipOneByte)
	files["stray.json"] = []byte("{}\n")

	report := evidence.Verify(files, goldenKeyDirectory(t))
	tampered := report.Of(evidence.FindingTamperedPart)
	if len(tampered) != 2 {
		t.Fatalf("two altered parts reported %d tampered findings: %v", len(tampered), kinds(report))
	}
	if !report.Has(evidence.FindingUnlistedPart) {
		t.Fatalf("the stray path was not reported: %v", kinds(report))
	}
	seen := map[string]bool{}
	for _, f := range tampered {
		seen[f.Part] = true
	}
	if !seen[evidence.StreamChainPath(0)] || !seen[evidence.StreamEventsPath(1)] {
		t.Fatalf("the findings do not name both altered parts: %v", seen)
	}
}

// TestVerifyHeaderMustAgreeWithTheManifest proves that the header is not a
// second place a package may state its coverage: a header that disagrees
// with the manifest about the tenant, the window or the schema release is a
// finding rather than an alternative answer.
func TestVerifyHeaderMustAgreeWithTheManifest(t *testing.T) {
	t.Parallel()
	pkg, err := evidence.Build(goldenContent(t))
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	dir := goldenKeyDirectory(t)

	cases := []struct {
		name string
		edit func(map[string]any)
		want evidence.FindingKind
	}{
		{"another tenant", func(doc map[string]any) { doc["tenant"] = uuid.New().String() }, evidence.FindingTenantLeak},
		{"another window", func(doc map[string]any) { doc["covers_to_ns"] = float64(0) }, evidence.FindingMalformedPart},
		{"another schema release", func(doc map[string]any) { doc["schema_release_digest"] = strings.Repeat("9", 64) }, evidence.FindingMalformedPart},
		{"an unparseable tenant", func(doc map[string]any) { doc["tenant"] = "not-a-uuid" }, evidence.FindingMalformedPart},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			files := mutate(t, pkg.Files(), evidence.HeaderPath, func(raw []byte) []byte {
				return rewriteJSON(t, raw, tc.edit)
			})
			report := evidence.Verify(files, dir)
			if !report.Has(tc.want) {
				t.Fatalf("reported %v, want %s", kinds(report), tc.want)
			}
		})
	}

	t.Run("a package with no header at all", func(t *testing.T) {
		files := pkg.Files()
		delete(files, evidence.HeaderPath)
		report := evidence.Verify(files, dir)
		if !report.Has(evidence.FindingMissingPart) {
			t.Fatalf("reported %v, want a missing part", kinds(report))
		}
	})
}

// TestWithEventDigesterIsOptInAndCatchesAForgedPayload proves why the
// payload check is a caller's choice: the wrong canonicalization profile
// would accuse a sound package of tampering, so a verifier that does not
// know the cell's profile checks one binding fewer rather than guessing.
func TestWithEventDigesterIsOptInAndCatchesAForgedPayload(t *testing.T) {
	f := newFixture(t)
	f.seedCoveredWindow(t)
	pkg := f.mustExport(t, f.request())
	dir := f.liveKeyDirectory()

	forged := mutate(t, pkg.Files(), evidence.StreamEventsPath(0), func(raw []byte) []byte {
		return rewriteJSON(t, raw, func(doc map[string]any) {
			// "Zm9yZ2Vk" is "forged"; the recorded digest is left alone, so
			// only a recomputation can notice.
			doc["events"].([]any)[0].(map[string]any)["payload"] = "Zm9yZ2Vk"
		})
	})

	t.Run("without a digester the payload is not recomputed", func(t *testing.T) {
		report := evidence.Verify(forged, dir)
		for _, finding := range report.Of(evidence.FindingTamperedEvent) {
			t.Fatalf("a payload was accused without a digester: %s", finding)
		}
	})

	t.Run("with the cell's digester the forgery is named", func(t *testing.T) {
		report := evidence.Verify(forged, dir, evidence.WithEventDigester(datalogger.SHA256Digester{}))
		findings := report.Of(evidence.FindingTamperedEvent)
		if len(findings) == 0 {
			t.Fatalf("reported %v, want a tampered event", kinds(report))
		}
		if findings[0].Stream != streamOne || findings[0].Sequence != 1 {
			t.Fatalf("the finding names %s@%d, want %s@1", findings[0].Stream, findings[0].Sequence, streamOne)
		}
	})

	t.Run("a digester that cannot compute is reported, not ignored", func(t *testing.T) {
		report := evidence.Verify(pkg.Files(), dir, evidence.WithEventDigester(refusingDigester{}))
		if !report.Has(evidence.FindingMalformedPart) {
			t.Fatalf("reported %v, want a malformed part", kinds(report))
		}
	})
}

// TestVerifyNeedsNoKeyDirectoryToRefuse proves the fail-closed default: a
// verifier handed no keys at all cannot confirm any signature, so no epoch
// verifies and the window is reported as unattested rather than accepted.
func TestVerifyNeedsNoKeyDirectoryToRefuse(t *testing.T) {
	t.Parallel()
	pkg, err := evidence.Build(goldenContent(t))
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	report := evidence.Verify(pkg.Files(), checkpoint.NewStaticKeyDirectory())
	if report.OK() {
		t.Fatal("a package verified against an empty key directory")
	}
	if !report.Has(evidence.FindingSignatureMismatch) || !report.Has(evidence.FindingMissingEpochCoverage) {
		t.Fatalf("reported %v, want a signature mismatch and missing coverage", kinds(report))
	}
	if !report.Has(evidence.FindingUnattestedHead) {
		t.Fatalf("reported %v, want the covered streams reported as unattested", kinds(report))
	}
}

// refusingDigester stands in for a canonicalization profile that cannot
// handle an event it is asked about.
type refusingDigester struct{}

func (refusingDigester) Digest([]byte, string) (string, string, int, error) {
	return "", "", 0, errors.New("no profile for this schema")
}
