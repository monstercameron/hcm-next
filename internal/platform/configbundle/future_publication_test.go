package configbundle

import (
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/cycle"
)

func futureEvidenceFixture(t *testing.T) (FuturePublicationEvidence, cycle.PublicationVerification, *InMemoryKeyring) {
	t.Helper()
	public, private, _ := ed25519.GenerateKey(rand.Reader)
	signer, err := NewEd25519ReceiptSigner("publication-key", "v1", private)
	if err != nil {
		t.Fatal(err)
	}
	scope := Scope{TenantID: "cycle-tenant", CellID: "cell-a"}
	obj := IncludedObject{Ref: ObjectRef{Scope: scope, Kind: KindRule, ID: "cycle", Revision: 1}, Digest: cp005Digest("object")}
	bundle := Bundle{BundleID: "approved", ManifestVersion: manifestVersion, TargetScope: scope, Scope: scope, MinimumRuntimeVersion: "go1.26.3", Roots: []ObjectRef{obj.Ref}, Objects: []IncludedObject{obj}}
	bundle.Digest, err = BundleDigest(bundle)
	if err != nil {
		t.Fatal(err)
	}
	signed, err := SignBundle(bundle, "publication-key", "v1", private)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	activation := ActivationReceipt{BundleID: bundle.BundleID, BundleDigest: bundle.Digest, Scope: scope, Environment: "prod", TrustProfile: "release", Epoch: 7, IssuedAt: now.Add(-5 * time.Hour), ExpiresAt: now.Add(time.Hour), ActivatedAt: now.Add(-4 * time.Hour), Signer: signed.Signature}
	activation.Digest, err = activation.DigestValue()
	if err != nil {
		t.Fatal(err)
	}
	activation.Signature, err = signer.SignDigest(activation.Digest)
	if err != nil {
		t.Fatal(err)
	}
	impact, err := SignImpact(ImpactAnalysis{Scope: scope, RevisionDigest: cp005Digest("revision"), BundleDigest: bundle.Digest, Epoch: 7, AnalyzedAt: now.Add(-3 * time.Hour)}, signer)
	if err != nil {
		t.Fatal(err)
	}
	plan := CanaryPlan{Scope: scope, BundleDigest: bundle.Digest, Epoch: 7, Cohort: []string{"org-a"}, MaxCohort: 2, CohortID: "pilot", WindowFrom: now.Add(-2 * time.Hour), WindowTo: now.Add(-time.Hour), Health: []CanaryObservation{{ID: "health", Status: CanaryHealthy, ObservedAt: now.Add(-time.Hour), WindowFrom: now.Add(-2 * time.Hour), WindowTo: now.Add(-time.Hour), Good: 1, Total: 1, Complete: true, EvidenceDigest: cp005Digest("health")}}, Reconciliation: CanaryEvidence{Available: true, Passed: true, EvidenceDigest: cp005Digest("reconcile")}, Conformance: CanaryEvidence{Available: true, Passed: true, EvidenceDigest: cp005Digest("conformance")}}
	canary, err := EvaluateCanary(plan, signer, now.Add(-30*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	keyring := NewKeyring()
	if err := keyring.Put(BundleKey{Handle: "publication-key", Version: "v1", PublicKey: public, TrustProfile: "release"}); err != nil {
		t.Fatal(err)
	}
	evidence := FuturePublicationEvidence{ApprovedBundle: signed, Activation: activation, Impact: impact, Canary: canary, ApprovedRevisionDigest: impact.RevisionDigest}
	verification := cycle.PublicationVerification{CurrentRevisionDigest: cp005Digest("current"), CurrentState: cycle.StateUnopened, CandidateDigest: impact.RevisionDigest, Evidence: cycle.PublicationEvidence{RevisionDigest: impact.RevisionDigest, BundleDigest: bundle.Digest, TenantID: scope.TenantID, ActivationEpoch: 7, ApprovedAt: activation.ActivatedAt, ImpactAnalyzedAt: impact.AnalyzedAt, CanaryWindowEnd: canary.WindowTo, CanaryDecidedAt: canary.DecidedAt}}
	return evidence, verification, keyring
}

func TestTodo_CYCLE_007_IntegrationSignedEvidence(t *testing.T) {
	evidence, verification, keys := futureEvidenceFixture(t)
	verifier := NewFuturePublicationVerifier(evidence, keys)
	if NewFutureCycleVerifier(evidence, keys) == nil {
		t.Fatal("compatibility verifier constructor returned nil")
	}
	if err := verifier.VerifyFuturePublication(verification); err != nil {
		t.Fatalf("signed promoted evidence rejected: %v", err)
	}
	if err := evidence.Impact.Verify(make(ed25519.PublicKey, ed25519.PublicKeySize)); !errors.Is(err, ErrImpactInvalid) {
		t.Fatalf("wrong impact verification key error=%v", err)
	}
	if _, err := (ImpactAnalysis{Scope: Scope{TenantID: "t"}, Epoch: 1, AnalyzedAt: time.Now(), RevisionDigest: cp005Digest("r")}).DigestValue(); !errors.Is(err, ErrInvalidSignature) {
		t.Fatalf("missing impact bundle digest error=%v", err)
	}
	unsigned := evidence.Impact
	unsigned.Signature = BundleSignature{}
	if err := unsigned.Verify(keysKey(t, keysForFreshFixture(t))); err == nil {
		t.Fatal("unsigned impact was accepted")
	}
	for name, mutate := range map[string]func(*FuturePublicationEvidence){
		"paused":           func(e *FuturePublicationEvidence) { e.Canary.Outcome = CanaryPaused },
		"replay":           func(e *FuturePublicationEvidence) { e.Canary.Epoch = e.Canary.Epoch - 1 },
		"different-tenant": func(e *FuturePublicationEvidence) { e.Canary.Scope.TenantID = "other" },
		"different-cycle":  func(e *FuturePublicationEvidence) { e.Impact.RevisionDigest = cp005Digest("other-revision") },
	} {
		t.Run(name, func(t *testing.T) {
			candidate := evidence
			mutate(&candidate)
			if err := NewFuturePublicationVerifier(candidate, keys).VerifyFuturePublication(verification); !errors.Is(err, ErrPublicationEvidenceMismatch) && !errors.Is(err, ErrPublicationCanaryNotPromoted) {
				t.Fatalf("mutation accepted: %v", err)
			}
		})
	}
}

func TestTodo_CYCLE_007_Property(t *testing.T) {
	evidence, verification, keys := futureEvidenceFixture(t)
	verification.Evidence.CanaryDecidedAt = verification.Evidence.CanaryDecidedAt.Add(time.Second)
	if err := NewFuturePublicationVerifier(evidence, keys).VerifyFuturePublication(verification); !errors.Is(err, ErrPublicationEvidenceMismatch) {
		t.Fatalf("timing mismatch=%v", err)
	}
	if err := NewFuturePublicationVerifier(evidence, nil).VerifyFuturePublication(verification); !errors.Is(err, ErrPublicationEvidenceUnavailable) {
		t.Fatalf("missing configured keys=%v", err)
	}
}

func TestTodo_CYCLE_007_ImpactAndTrustFailures(t *testing.T) {
	evidence, verification, keys := futureEvidenceFixture(t)
	bad := evidence.Impact
	bad.BundleDigest = "sha256:not-a-digest"
	if _, err := bad.DigestValue(); !errors.Is(err, ErrInvalidSignature) {
		t.Fatalf("invalid impact digest error=%v", err)
	}
	bad = evidence.Impact
	bad.Digest = "sha256:" + "0" + bad.Digest[8:]
	if err := bad.Verify(keysKey(t, keys)); !errors.Is(err, ErrImpactInvalid) {
		t.Fatalf("mutated impact error=%v", err)
	}
	if _, err := SignImpact(evidence.Impact, nil); !errors.Is(err, ErrImpactSigner) {
		t.Fatalf("nil impact signer error=%v", err)
	}
	unknown := NewKeyring()
	if err := NewFuturePublicationVerifier(evidence, unknown).VerifyFuturePublication(verification); !errors.Is(err, ErrPublicationEvidenceUnauthenticated) {
		t.Fatalf("unknown configured key error=%v", err)
	}
	if err := keys.Revoke("publication-key", "v1"); err != nil {
		t.Fatal(err)
	}
	if err := NewFuturePublicationVerifier(evidence, keys).VerifyFuturePublication(verification); !errors.Is(err, ErrPublicationEvidenceUnauthenticated) {
		t.Fatalf("revoked configured key error=%v", err)
	}
	invalid := ImpactAnalysis{Epoch: 1, AnalyzedAt: time.Now()}
	if _, err := invalid.DigestValue(); !errors.Is(err, ErrImpactInvalid) {
		t.Fatalf("invalid impact identity error=%v", err)
	}
	if _, err := SignImpact(invalid, &Ed25519ReceiptSigner{}); !errors.Is(err, ErrImpactInvalid) {
		t.Fatalf("invalid impact signing error=%v", err)
	}
	if err := (*FuturePublicationVerifier)(nil).VerifyFuturePublication(verification); !errors.Is(err, ErrPublicationEvidenceUnavailable) {
		t.Fatalf("nil verifier error=%v", err)
	}
	for _, mutate := range []func(*FuturePublicationEvidence){
		func(e *FuturePublicationEvidence) { e.ApprovedBundle.Signature.Value = strings.Repeat("0", 128) },
		func(e *FuturePublicationEvidence) { e.Activation.Signature.KeyVersion = "missing" },
		func(e *FuturePublicationEvidence) { e.Impact.Signature.Value = strings.Repeat("0", 128) },
		func(e *FuturePublicationEvidence) { e.Canary.Signature.Value = strings.Repeat("0", 128) },
	} {
		candidate := evidence
		mutate(&candidate)
		if err := NewFuturePublicationVerifier(candidate, keysForFreshFixture(t)).VerifyFuturePublication(verification); !errors.Is(err, ErrPublicationEvidenceUnauthenticated) {
			t.Fatalf("forged signed evidence error=%v", err)
		}
	}
	for _, mutate := range []func(*cycle.PublicationVerification){
		func(p *cycle.PublicationVerification) { p.CandidateDigest = cp005Digest("other") },
		func(p *cycle.PublicationVerification) { p.Evidence.TenantID = "other" },
		func(p *cycle.PublicationVerification) { p.Evidence.ApprovedAt = time.Time{} },
	} {
		candidate := verification
		mutate(&candidate)
		if err := NewFuturePublicationVerifier(evidence, keysForFreshFixture(t)).VerifyFuturePublication(candidate); !errors.Is(err, ErrPublicationEvidenceMismatch) {
			t.Fatalf("forged request projection error=%v", err)
		}
	}
}

func keysKey(t *testing.T, keys *InMemoryKeyring) ed25519.PublicKey {
	t.Helper()
	key, ok, err := keys.ResolveBundleKey("publication-key", "v1")
	if err != nil || !ok {
		t.Fatalf("resolve fixture key: %v", err)
	}
	return key.PublicKey
}

func keysForFreshFixture(t *testing.T) *InMemoryKeyring {
	_, _, keys := futureEvidenceFixture(t)
	return keys
}
