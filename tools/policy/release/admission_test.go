package release_test

import (
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/tools/policy/release"
	"github.com/monstercameron/human-capital-management-suite/tools/policy/releaseadmission"
)

// admissionFixtureBundle builds a fully self-contained, deterministic,
// custody-signed release bundle for CICD-004 admission tests. It does not
// depend on repo-root checked-in supply-chain files (unlike releaseFixture
// in release_test.go), so its manifest digest is stable across the whole
// repository's history, which the GOLDEN test requires.
func admissionFixtureBundle(t *testing.T, seedByte byte, scope string) (bundle, manifestDigest, publicKey string) {
	t.Helper()
	seed := make([]byte, ed25519.SeedSize)
	seed[0] = seedByte
	private := ed25519.NewKeyFromSeed(seed)
	provider := &releaseCustodyFake{keys: map[string]ed25519.PrivateKey{"release-key:v1": private}}
	source := custodySource(t, provider, "v1", private)
	inputs := newReleaseInputs(t)
	writeSignedReleaseStatement(t, inputs, source)
	out := filepath.Join(inputs.root, "bundle")
	manifest, err := release.BuildWithKeySource(inputs.root, inputs.options(out), source)
	if err != nil {
		t.Fatalf("BuildWithKeySource: %v", err)
	}
	digest, err := manifest.CanonicalDigest()
	if err != nil {
		t.Fatalf("CanonicalDigest: %v", err)
	}
	_ = scope
	return out, digest, source.PublicKey()
}

func TestTodo_CICD_004(t *testing.T) {
	out, digest, publicKey := admissionFixtureBundle(t, 60, "prod/pilot-cell")
	target := release.ApprovedTarget{ManifestDigest: digest, Scope: "prod/pilot-cell"}
	trusted := map[string]bool{publicKey: true}
	now := time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC)

	baseCandidate := func() release.DeploymentCandidate {
		return release.DeploymentCandidate{
			Bundle:        out,
			Scope:         "prod/pilot-cell",
			Vulnerability: release.VulnerabilityEvidence{Scanned: true},
			Conformance:   release.ConformanceEvidence{GeneratedAt: now.Add(-1 * time.Hour)},
		}
	}
	basePolicy := func() release.AdmissionPolicy {
		return release.AdmissionPolicy{TrustedPublicKeys: trusted, Target: target, MaxConformanceAge: 24 * time.Hour}
	}

	// GREEN: an exactly-approved, signed, scanned, conformant candidate admits.
	decision, err := release.Admit(baseCandidate(), basePolicy(), now)
	if err != nil {
		t.Fatalf("admit-path Admit: %v", err)
	}
	if !decision.Admitted || decision.Status != release.AdmissionAdmitted {
		t.Fatalf("admit-path decision = %+v, want Admitted", decision)
	}
	if decision.ManifestDigest != digest {
		t.Fatalf("admit-path decision.ManifestDigest = %q, want %q", decision.ManifestDigest, digest)
	}
	if decision.Reason != "" {
		t.Fatalf("admit-path decision.Reason = %q, want empty", decision.Reason)
	}

	// RED 1: unsigned / signature-untrusted. A syntactically valid but
	// untrusted public key must reject the otherwise-valid candidate.
	untrustedPolicy := basePolicy()
	untrustedPolicy.TrustedPublicKeys = map[string]bool{strings.Repeat("f", 64): true}
	decision, err = release.Admit(baseCandidate(), untrustedPolicy, now)
	if !errors.Is(err, release.ErrCandidateUnsigned) {
		t.Fatalf("untrusted signer err = %v, want ErrCandidateUnsigned", err)
	}
	if decision.Admitted {
		t.Fatalf("untrusted signer decision.Admitted = true, want false: %+v", decision)
	}

	// RED 2: vulnerable. Everything else valid, but unresolved findings
	// remain after scanning.
	vulnerable := baseCandidate()
	vulnerable.Vulnerability.UnresolvedFindings = 2
	decision, err = release.Admit(vulnerable, basePolicy(), now)
	if !errors.Is(err, release.ErrCandidateVulnerable) {
		t.Fatalf("vulnerable candidate err = %v, want ErrCandidateVulnerable", err)
	}
	if decision.Admitted {
		t.Fatalf("vulnerable candidate decision.Admitted = true, want false: %+v", decision)
	}

	// RED 3a: incompatible scope. A validly signed, exact-digest candidate
	// offered for a scope it was not approved for must reject.
	wrongScope := baseCandidate()
	wrongScope.Scope = "staging"
	decision, err = release.Admit(wrongScope, basePolicy(), now)
	if !errors.Is(err, release.ErrCandidateIncompatible) {
		t.Fatalf("wrong-scope candidate err = %v, want ErrCandidateIncompatible", err)
	}
	if decision.Admitted {
		t.Fatalf("wrong-scope candidate decision.Admitted = true, want false: %+v", decision)
	}

	// RED 3b: incompatible digest. A different, but validly and trustedly
	// signed, build offered for the approved scope must also reject: only
	// an exact digest match admits, not "any trusted signature verifies".
	otherOut, otherDigest, otherPublicKey := admissionFixtureBundle(t, 61, "prod/pilot-cell")
	if otherDigest == digest {
		t.Fatal("test setup: expected two distinct bundle digests")
	}
	otherCandidate := release.DeploymentCandidate{
		Bundle:        otherOut,
		Scope:         "prod/pilot-cell",
		Vulnerability: release.VulnerabilityEvidence{Scanned: true},
		Conformance:   release.ConformanceEvidence{GeneratedAt: now.Add(-1 * time.Hour)},
	}
	otherPolicy := basePolicy()
	otherPolicy.TrustedPublicKeys = map[string]bool{publicKey: true, otherPublicKey: true}
	decision, err = release.Admit(otherCandidate, otherPolicy, now)
	if !errors.Is(err, release.ErrCandidateIncompatible) {
		t.Fatalf("different-but-validly-signed candidate err = %v, want ErrCandidateIncompatible", err)
	}
	if decision.Admitted {
		t.Fatalf("different-but-validly-signed candidate decision.Admitted = true, want false: %+v", decision)
	}
	if decision.ManifestDigest != otherDigest {
		t.Fatalf("decision.ManifestDigest = %q, want the candidate's own verified digest %q", decision.ManifestDigest, otherDigest)
	}

	// RED 4: stale conformance.
	stale := baseCandidate()
	stale.Conformance.GeneratedAt = now.Add(-48 * time.Hour)
	decision, err = release.Admit(stale, basePolicy(), now)
	if !errors.Is(err, release.ErrConformanceStale) {
		t.Fatalf("stale conformance err = %v, want ErrConformanceStale", err)
	}
	if decision.Admitted {
		t.Fatalf("stale conformance decision.Admitted = true, want false: %+v", decision)
	}

	// RED 5: incomplete evidence. Every zero-valued / absent input must
	// independently reject rather than being treated as "permitted".
	incompleteCases := map[string]struct {
		candidate release.DeploymentCandidate
		policy    release.AdmissionPolicy
	}{
		"empty trusted-key set": {baseCandidate(), func() release.AdmissionPolicy {
			p := basePolicy()
			p.TrustedPublicKeys = nil
			return p
		}()},
		"zero-valued approved target": {baseCandidate(), func() release.AdmissionPolicy {
			p := basePolicy()
			p.Target = release.ApprovedTarget{}
			return p
		}()},
		"zero conformance staleness window": {baseCandidate(), func() release.AdmissionPolicy {
			p := basePolicy()
			p.MaxConformanceAge = 0
			return p
		}()},
		"absent vulnerability scan evidence": {func() release.DeploymentCandidate {
			c := baseCandidate()
			c.Vulnerability = release.VulnerabilityEvidence{}
			return c
		}(), basePolicy()},
		"conformance record with no timestamp": {func() release.DeploymentCandidate {
			c := baseCandidate()
			c.Conformance = release.ConformanceEvidence{}
			return c
		}(), basePolicy()},
	}
	for name, tc := range incompleteCases {
		t.Run(name, func(t *testing.T) {
			decision, err := release.Admit(tc.candidate, tc.policy, now)
			if !errors.Is(err, release.ErrEvidenceIncomplete) {
				t.Fatalf("%s: err = %v, want ErrEvidenceIncomplete", name, err)
			}
			if decision.Admitted {
				t.Fatalf("%s: decision.Admitted = true, want false: %+v", name, decision)
			}
		})
	}
}

func TestTodo_CICD_004_Golden(t *testing.T) {
	out, digest, publicKey := admissionFixtureBundle(t, 70, "prod/pilot-cell")
	policy := release.AdmissionPolicy{
		TrustedPublicKeys: map[string]bool{publicKey: true},
		Target:            release.ApprovedTarget{ManifestDigest: digest, Scope: "prod/pilot-cell"},
		MaxConformanceAge: 24 * time.Hour,
	}
	candidate := release.DeploymentCandidate{
		Bundle:        out,
		Scope:         "prod/pilot-cell",
		Vulnerability: release.VulnerabilityEvidence{Scanned: true},
		Conformance:   release.ConformanceEvidence{GeneratedAt: time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)},
	}
	now := time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC)

	decision, err := release.Admit(candidate, policy, now)
	if err != nil {
		t.Fatalf("Admit: %v", err)
	}
	if !decision.Admitted {
		t.Fatalf("decision not admitted: %+v", decision)
	}

	got, err := json.MarshalIndent(decision, "", "  ")
	if err != nil {
		t.Fatalf("marshal decision: %v", err)
	}
	got = append(got, '\n')

	goldenPath := filepath.Join("testdata", "cicd004_admission_decision_golden.json")
	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if err := os.MkdirAll(filepath.Dir(goldenPath), 0o755); err != nil {
			t.Fatalf("mkdir testdata: %v", err)
		}
		if err := os.WriteFile(goldenPath, got, 0o644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
	}
	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden %s: %v", goldenPath, err)
	}
	if string(got) != string(want) {
		t.Fatalf("admission decision bytes changed:\ngot:\n%s\nwant:\n%s", got, want)
	}
}

func TestTodo_CICD_004_Conformance(t *testing.T) {
	out, digest, publicKey := admissionFixtureBundle(t, 80, "prod/pilot-cell")
	policy := release.AdmissionPolicy{
		TrustedPublicKeys: map[string]bool{publicKey: true},
		Target:            release.ApprovedTarget{ManifestDigest: digest, Scope: "prod/pilot-cell"},
		MaxConformanceAge: 24 * time.Hour,
	}
	generatedAt := time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC)
	candidate := release.DeploymentCandidate{
		Bundle:        out,
		Scope:         "prod/pilot-cell",
		Vulnerability: release.VulnerabilityEvidence{Scanned: true},
		Conformance:   release.ConformanceEvidence{GeneratedAt: generatedAt},
	}

	// Driven off the injected evaluation time, not the wall clock: the same
	// candidate/GeneratedAt pair admits when evaluated just inside the
	// window and rejects as stale when evaluated just outside it.
	within := generatedAt.Add(23 * time.Hour)
	decision, err := release.Admit(candidate, policy, within)
	if err != nil {
		t.Fatalf("within window: Admit = %v, decision = %+v, want admit", err, decision)
	}
	if !decision.Admitted {
		t.Fatalf("within window: decision.Admitted = false, want true: %+v", decision)
	}

	outside := generatedAt.Add(25 * time.Hour)
	decision, err = release.Admit(candidate, policy, outside)
	if !errors.Is(err, release.ErrConformanceStale) {
		t.Fatalf("outside window: err = %v, want ErrConformanceStale", err)
	}
	if decision.Admitted {
		t.Fatalf("outside window: decision.Admitted = true, want false: %+v", decision)
	}
}

// TestReleasePackage_ExportedHelpersCoverage is not a CICD-004 matrix test;
// CICD-004's work surfaced that several exported entry points in this
// package that CICD-003/SECARCH-005 already ship (Version, Verify,
// Verification.Explain, NewFixtureKeySource, VerifyBundleWithScannerEvidence)
// had no test anywhere in the package, leaving overall package coverage
// below the 70% floor this file root must clear. This test exercises them
// directly against a real, validly signed, fully-scanned bundle rather than
// adding assertion-free calls, so it can fail: on a broken fixture-key
// lookup, a broken short-form Verify, or a broken scanner-evidence bundle
// check.
func TestReleasePackage_ExportedHelpersCoverage(t *testing.T) {
	if release.Version() != release.SchemaVersion {
		t.Fatalf("Version() = %d, want %d", release.Version(), release.SchemaVersion)
	}

	seed := make([]byte, ed25519.SeedSize)
	seed[0] = 90
	private := ed25519.NewKeyFromSeed(seed)
	provider := &releaseCustodyFake{keys: map[string]ed25519.PrivateKey{"release-key:v1": private}}
	source := custodySource(t, provider, "v1", private)
	inputs := newReleaseInputs(t)
	for _, kind := range releaseadmission.RequiredScannerKinds() {
		evidence, err := releaseadmission.NewScannerEvidence(kind, "govulncheck", "1.0.0", "config-digest-"+string(kind), nil)
		if err != nil {
			t.Fatalf("NewScannerEvidence(%s): %v", kind, err)
		}
		data, err := json.Marshal(evidence)
		if err != nil {
			t.Fatalf("marshal scanner evidence %s: %v", kind, err)
		}
		name := releaseadmission.ScannerPolicyReportName(kind)
		path := filepath.Join(inputs.root, name+".json")
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatalf("write scanner evidence %s: %v", name, err)
		}
		inputs.pol[name] = path
	}
	writeSignedReleaseStatement(t, inputs, source)
	out := filepath.Join(inputs.root, "bundle")
	if _, err := release.BuildWithKeySource(inputs.root, inputs.options(out), source); err != nil {
		t.Fatalf("BuildWithKeySource: %v", err)
	}

	verifyOpts := release.VerifyOptions{TrustedPublicKeys: map[string]bool{source.PublicKey(): true}}
	verification, err := release.VerifyBundleWithScannerEvidence(out, verifyOpts)
	if err != nil {
		t.Fatalf("VerifyBundleWithScannerEvidence: %v", err)
	}
	if verification.ManifestDigest == "" {
		t.Fatal("VerifyBundleWithScannerEvidence: ManifestDigest is empty")
	}
	if explained := verification.Explain(); !strings.Contains(explained, verification.ManifestDigest) {
		t.Fatalf("Verification.Explain() = %q, want it to contain manifest digest %q", explained, verification.ManifestDigest)
	}

	untrustedOpts := release.VerifyOptions{TrustedPublicKeys: map[string]bool{strings.Repeat("a", 64): true}}
	if _, err := release.VerifyBundleWithScannerEvidence(out, untrustedOpts); err == nil {
		t.Fatal("VerifyBundleWithScannerEvidence with an untrusted key set unexpectedly succeeded")
	}

	fixturePath := filepath.Join("..", "..", "..", release.DefaultKeyPath)
	fixtureSource, err := release.NewFixtureKeySource(fixturePath)
	if err != nil {
		t.Fatalf("NewFixtureKeySource(%s): %v", fixturePath, err)
	}
	if fixtureSource.PublicKey() != release.DevFixturePublicKey {
		t.Fatalf("fixture key source public key = %s, want %s", fixtureSource.PublicKey(), release.DevFixturePublicKey)
	}

	fixtureOut := filepath.Join(inputs.root, "bundle-fixture-key")
	fixtureInputs := newReleaseInputs(t)
	fixtureSourceStatement := fixtureSource
	writeSignedReleaseStatement(t, fixtureInputs, fixtureSourceStatement)
	if _, err := release.BuildWithKeySource(fixtureInputs.root, fixtureInputs.options(fixtureOut), fixtureSource); err != nil {
		t.Fatalf("BuildWithKeySource with fixture key source: %v", err)
	}
	if err := release.Verify(fixtureOut); err != nil {
		t.Fatalf("Verify (pinned dev fixture key, short form): %v", err)
	}
}
