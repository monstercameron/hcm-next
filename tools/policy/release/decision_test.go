package release_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/tools/policy/provenance"
	"github.com/monstercameron/human-capital-management-suite/tools/policy/release"
)

// decisionKeySource returns the checked-in development fixture KeySource,
// used to sign CICD-006 decision manifests in tests. It is independent of
// whatever key signed the underlying release bundle.
func decisionKeySource(t *testing.T) release.KeySource {
	t.Helper()
	fixturePath := filepath.Join("..", "..", "..", release.DefaultKeyPath)
	source, err := release.NewFixtureKeySource(fixturePath)
	if err != nil {
		t.Fatalf("NewFixtureKeySource(%s): %v", fixturePath, err)
	}
	return source
}

// fullDecisionEvidence builds a complete, independently-valid evidence set
// covering all eleven CICD-006 classes, backed by a real signed bundle
// (CICD-003), a real admitted rollback target (CICD-004) and a real
// in-progress rollout (CICD-005). conformanceAt lets callers control the
// conformance evidence class's freshness relative to whatever `now` they
// evaluate against.
func fullDecisionEvidence(t *testing.T, seedByte byte, now, conformanceAt time.Time) release.DecisionEvidence {
	t.Helper()
	out, digest, publicKey := admissionFixtureBundle(t, seedByte, "prod/pilot-cell")
	verification, err := release.VerifyBundle(out, release.VerifyOptions{TrustedPublicKeys: map[string]bool{publicKey: true}})
	if err != nil {
		t.Fatalf("VerifyBundle: %v", err)
	}

	admissionPolicy := release.AdmissionPolicy{
		TrustedPublicKeys: map[string]bool{publicKey: true},
		Target:            release.ApprovedTarget{ManifestDigest: digest, Scope: "prod/pilot-cell"},
		MaxConformanceAge: 24 * time.Hour,
	}
	rollbackCandidate := release.DeploymentCandidate{
		Bundle:        out,
		Scope:         "prod/pilot-cell",
		Vulnerability: release.VulnerabilityEvidence{Scanned: true},
		Conformance:   release.ConformanceEvidence{GeneratedAt: now.Add(-time.Hour)},
	}
	rollbackDecision, err := release.Admit(rollbackCandidate, admissionPolicy, now)
	if err != nil {
		t.Fatalf("Admit (rollback target): %v", err)
	}

	rollout, err := release.NewRollout(fmt.Sprintf("rel-decision-%d", seedByte), []string{"canary-5", "full"})
	if err != nil {
		t.Fatalf("NewRollout: %v", err)
	}
	rolloutPolicy := release.RolloutPolicy{MaxConformanceAge: 24 * time.Hour}
	if _, err := rollout.Advance(healthyStage(now), rolloutPolicy, now); err != nil {
		t.Fatalf("Advance: %v", err)
	}

	return release.DecisionEvidence{
		Source: release.SourceEvidence{Ref: provenance.SourceRef{
			Repository: provenance.RootModulePath,
			Ref:        "main",
			Commit:     "deadbeefdeadbeefdeadbeefdeadbeefdeadbeef",
		}},
		Toolchain: release.ToolchainEvidence{Config: provenance.BuildConfig{
			GoVersion: "go1.26.3", GOOS: "windows", GOARCH: "arm64", ConfigDigest: "config-digest",
		}},
		Artifact:    release.ArtifactEvidence{Verification: verification},
		Schema:      release.SchemaEvidence{BundleSchemaVersion: release.SchemaVersion, ValidatedAt: now},
		Config:      release.ConfigEvidence{Digest: "config-digest", Valid: true, ValidatedAt: now},
		Test:        release.TestEvidence{RanAt: now, Passed: 42, Failed: 0},
		Conformance: release.ConformanceEvidence{GeneratedAt: conformanceAt},
		Rollout:     release.RolloutEvidence{Snapshot: rollout.Snapshot()},
		Rollback:    release.RollbackEvidence{Decision: &rollbackDecision},
		Owner:       release.OwnerEvidence{Name: "cam", ConfirmedAt: now},
		Blocker:     release.BlockerEvidence{CheckedAt: now, Open: nil},
	}
}

func TestTodo_CICD_006(t *testing.T) {
	now := time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)
	evidence := fullDecisionEvidence(t, 200, now, now.Add(-time.Hour))
	policy := release.DecisionPolicy{MaxConformanceAge: 24 * time.Hour}
	source := decisionKeySource(t)

	// GREEN: a complete, passing evidence set yields PROCEED and a signed
	// manifest that verifies.
	manifest, err := release.BuildDecisionManifest("rel-primary", evidence, policy, now, source)
	if err != nil {
		t.Fatalf("BuildDecisionManifest: %v", err)
	}
	if manifest.Verdict != release.DecisionProceed {
		t.Fatalf("Verdict = %s, want PROCEED: reasons %v", manifest.Verdict, manifest.Reasons)
	}
	if len(manifest.Reasons) != 0 {
		t.Fatalf("Reasons = %v, want empty on PROCEED", manifest.Reasons)
	}
	if manifest.Signature == nil {
		t.Fatal("manifest.Signature is nil, want a signed manifest")
	}
	if err := release.VerifyDecisionManifest(manifest, nil); err != nil {
		t.Fatalf("VerifyDecisionManifest: %v", err)
	}
	want := release.DecisionClassStatus{
		Source: true, Toolchain: true, Artifact: true, Schema: true, Config: true,
		Test: true, Conformance: true, Rollout: true, Rollback: true, Owner: true, Blocker: true,
	}
	if manifest.Classes != want {
		t.Fatalf("Classes = %+v, want all true: %+v", manifest.Classes, want)
	}

	// RED: each of the eleven evidence classes, removed in turn, makes
	// PROCEED unreachable - the verdict is STOP and the reason names
	// exactly which class was missing.
	zeroCases := map[string]func(ev *release.DecisionEvidence){
		"source":      func(ev *release.DecisionEvidence) { ev.Source = release.SourceEvidence{} },
		"toolchain":   func(ev *release.DecisionEvidence) { ev.Toolchain = release.ToolchainEvidence{} },
		"artifact":    func(ev *release.DecisionEvidence) { ev.Artifact = release.ArtifactEvidence{} },
		"schema":      func(ev *release.DecisionEvidence) { ev.Schema = release.SchemaEvidence{} },
		"config":      func(ev *release.DecisionEvidence) { ev.Config = release.ConfigEvidence{} },
		"test":        func(ev *release.DecisionEvidence) { ev.Test = release.TestEvidence{} },
		"conformance": func(ev *release.DecisionEvidence) { ev.Conformance = release.ConformanceEvidence{} },
		"rollout":     func(ev *release.DecisionEvidence) { ev.Rollout = release.RolloutEvidence{} },
		"rollback":    func(ev *release.DecisionEvidence) { ev.Rollback = release.RollbackEvidence{} },
		"owner":       func(ev *release.DecisionEvidence) { ev.Owner = release.OwnerEvidence{} },
		"blocker":     func(ev *release.DecisionEvidence) { ev.Blocker = release.BlockerEvidence{} },
	}
	for name, mutate := range zeroCases {
		t.Run("missing "+name, func(t *testing.T) {
			ev := evidence
			mutate(&ev)
			m, err := release.BuildDecisionManifest("rel-missing-"+name, ev, policy, now, source)
			if !errors.Is(err, release.ErrDecisionEvidenceIncomplete) {
				t.Fatalf("err = %v, want ErrDecisionEvidenceIncomplete", err)
			}
			if m.Verdict != release.DecisionStop {
				t.Fatalf("Verdict = %s, want STOP", m.Verdict)
			}
			if len(m.Reasons) == 0 {
				t.Fatal("Reasons is empty, want the missing class named")
			}
			found := false
			wantReason := "missing " + name + " evidence"
			for _, reason := range m.Reasons {
				if reason == wantReason {
					found = true
				}
			}
			if !found {
				t.Fatalf("Reasons = %v, want it to contain %q", m.Reasons, wantReason)
			}
			if m.Signature == nil {
				t.Fatal("STOP manifest.Signature is nil, want even a STOP decision to be signed")
			}
		})
	}

	// An open blocker is a STOP, not a REMEDIATE: it is a veto.
	t.Run("open blocker stops, does not remediate", func(t *testing.T) {
		ev := evidence
		ev.Blocker = release.BlockerEvidence{CheckedAt: now, Open: []string{"SEV-1 open incident"}}
		m, err := release.BuildDecisionManifest("rel-blocked", ev, policy, now, source)
		if !errors.Is(err, release.ErrDecisionBlocked) {
			t.Fatalf("err = %v, want ErrDecisionBlocked", err)
		}
		if m.Verdict != release.DecisionStop {
			t.Fatalf("Verdict = %s, want STOP", m.Verdict)
		}
	})

	// Schema mismatch quarantines: the candidate itself is not trustworthy
	// for this decision engine to reason about further.
	t.Run("schema mismatch quarantines", func(t *testing.T) {
		ev := evidence
		ev.Schema = release.SchemaEvidence{BundleSchemaVersion: release.SchemaVersion + 1, ValidatedAt: now}
		m, err := release.BuildDecisionManifest("rel-schema", ev, policy, now, source)
		if !errors.Is(err, release.ErrDecisionQuarantined) {
			t.Fatalf("err = %v, want ErrDecisionQuarantined", err)
		}
		if m.Verdict != release.DecisionQuarantine {
			t.Fatalf("Verdict = %s, want QUARANTINE", m.Verdict)
		}
	})

	// A rollback target that Admit does not admit quarantines: there is no
	// currently verified safe way back.
	t.Run("unadmitted rollback target quarantines", func(t *testing.T) {
		ev := evidence
		rejected := release.AdmissionDecision{Status: release.AdmissionRejected, Admitted: false, Scope: "prod/pilot-cell", Digest: "deadbeef", Reason: "candidate is vulnerable"}
		ev.Rollback = release.RollbackEvidence{Decision: &rejected}
		m, err := release.BuildDecisionManifest("rel-rollback", ev, policy, now, source)
		if !errors.Is(err, release.ErrDecisionQuarantined) {
			t.Fatalf("err = %v, want ErrDecisionQuarantined", err)
		}
		if m.Verdict != release.DecisionQuarantine {
			t.Fatalf("Verdict = %s, want QUARANTINE", m.Verdict)
		}
	})

	// A fenced/rolled-back rollout quarantines; a merely paused one only
	// needs remediation. These are deliberately different verdicts for the
	// same underlying evidence class.
	t.Run("fenced rollout quarantines, paused rollout remediates", func(t *testing.T) {
		fencedRollout, err := release.NewRollout("rel-fenced", []string{"canary-5"})
		if err != nil {
			t.Fatalf("NewRollout: %v", err)
		}
		out, digest, publicKey := admissionFixtureBundle(t, 201, "prod/pilot-cell")
		policyForRollback := release.AdmissionPolicy{
			TrustedPublicKeys: map[string]bool{publicKey: true},
			Target:            release.ApprovedTarget{ManifestDigest: digest, Scope: "prod/pilot-cell"},
			MaxConformanceAge: 24 * time.Hour,
		}
		rollbackCandidate := release.DeploymentCandidate{
			Bundle: out, Scope: "prod/pilot-cell",
			Vulnerability: release.VulnerabilityEvidence{Scanned: true},
			Conformance:   release.ConformanceEvidence{GeneratedAt: now.Add(-time.Hour)},
		}
		if _, err := fencedRollout.Rollback(rollbackCandidate, policyForRollback, now); err != nil {
			t.Fatalf("Rollback: %v", err)
		}
		fenced := evidence
		fenced.Rollout = release.RolloutEvidence{Snapshot: fencedRollout.Snapshot()}
		m, err := release.BuildDecisionManifest("rel-fenced", fenced, policy, now, source)
		if !errors.Is(err, release.ErrDecisionQuarantined) {
			t.Fatalf("fenced rollout err = %v, want ErrDecisionQuarantined", err)
		}
		if m.Verdict != release.DecisionQuarantine {
			t.Fatalf("fenced rollout Verdict = %s, want QUARANTINE", m.Verdict)
		}

		pausedRollout, err := release.NewRollout("rel-paused", []string{"canary-5", "full"})
		if err != nil {
			t.Fatalf("NewRollout: %v", err)
		}
		breach := healthyStage(now)
		breach.SLO.WithinBudget = false
		if _, err := pausedRollout.Advance(breach, release.RolloutPolicy{MaxConformanceAge: 24 * time.Hour}, now); !errors.Is(err, release.ErrRolloutSLOBreach) {
			t.Fatalf("Advance (expected breach): %v", err)
		}
		paused := evidence
		paused.Rollout = release.RolloutEvidence{Snapshot: pausedRollout.Snapshot()}
		m, err = release.BuildDecisionManifest("rel-paused", paused, policy, now, source)
		if !errors.Is(err, release.ErrDecisionNeedsRemediation) {
			t.Fatalf("paused rollout err = %v, want ErrDecisionNeedsRemediation", err)
		}
		if m.Verdict != release.DecisionRemediate {
			t.Fatalf("paused rollout Verdict = %s, want REMEDIATE", m.Verdict)
		}
	})

	// Invalid config and failing tests both remediate: fixable quality
	// signals, not a distrust signal.
	t.Run("invalid config remediates", func(t *testing.T) {
		ev := evidence
		ev.Config = release.ConfigEvidence{Digest: "config-digest", Valid: false, ValidatedAt: now}
		m, err := release.BuildDecisionManifest("rel-config", ev, policy, now, source)
		if !errors.Is(err, release.ErrDecisionNeedsRemediation) {
			t.Fatalf("err = %v, want ErrDecisionNeedsRemediation", err)
		}
		if m.Verdict != release.DecisionRemediate {
			t.Fatalf("Verdict = %s, want REMEDIATE", m.Verdict)
		}
	})
	t.Run("failing tests remediate", func(t *testing.T) {
		ev := evidence
		ev.Test = release.TestEvidence{RanAt: now, Passed: 10, Failed: 3}
		m, err := release.BuildDecisionManifest("rel-tests", ev, policy, now, source)
		if !errors.Is(err, release.ErrDecisionNeedsRemediation) {
			t.Fatalf("err = %v, want ErrDecisionNeedsRemediation", err)
		}
		if m.Verdict != release.DecisionRemediate {
			t.Fatalf("Verdict = %s, want REMEDIATE", m.Verdict)
		}
	})

	// A nil source (before signing) is refused with a clear error, and the
	// zero-valued Decision from a rejected build is never signed.
	t.Run("nil key source is refused", func(t *testing.T) {
		if _, err := release.BuildDecisionManifest("rel-nil-key", evidence, policy, now, nil); err == nil {
			t.Fatal("BuildDecisionManifest with nil key source unexpectedly succeeded")
		}
	})
}

func TestTodo_CICD_006_Golden(t *testing.T) {
	now := time.Date(2026, 9, 12, 18, 0, 0, 0, time.UTC)
	evidence := fullDecisionEvidence(t, 210, now, now.Add(-2*time.Hour))
	policy := release.DecisionPolicy{MaxConformanceAge: 24 * time.Hour}
	source := decisionKeySource(t)

	manifest, err := release.BuildDecisionManifest("rel-golden", evidence, policy, now, source)
	if err != nil {
		t.Fatalf("BuildDecisionManifest: %v", err)
	}
	if manifest.Verdict != release.DecisionProceed {
		t.Fatalf("Verdict = %s, want PROCEED: %v", manifest.Verdict, manifest.Reasons)
	}

	got, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	got = append(got, '\n')

	goldenPath := filepath.Join("testdata", "cicd006_decision_golden.json")
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
		t.Fatalf("decision manifest bytes changed:\ngot:\n%s\nwant:\n%s", got, want)
	}
}

func TestTodo_CICD_006_Race(t *testing.T) {
	now := time.Date(2026, 9, 12, 19, 0, 0, 0, time.UTC)
	evidence := fullDecisionEvidence(t, 220, now, now.Add(-time.Hour))
	policy := release.DecisionPolicy{MaxConformanceAge: 24 * time.Hour}
	source := decisionKeySource(t)

	const workers = 8
	results := make([][]byte, workers)
	errs := make([]error, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			manifest, err := release.BuildDecisionManifest("rel-race", evidence, policy, now, source)
			if err != nil {
				errs[index] = err
				return
			}
			data, marshalErr := json.Marshal(manifest)
			if marshalErr != nil {
				errs[index] = marshalErr
				return
			}
			results[index] = data
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("worker %d: BuildDecisionManifest: %v", i, err)
		}
	}
	first := string(results[0])
	if first == "" {
		t.Fatal("worker 0 produced no output")
	}
	for i := 1; i < workers; i++ {
		if got := string(results[i]); got != first {
			t.Fatalf("worker %d produced different manifest bytes under concurrency:\nworker 0: %s\nworker %d: %s", i, first, i, got)
		}
	}
}

func TestTodo_CICD_006_Conformance(t *testing.T) {
	generatedAt := time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC)
	policy := release.DecisionPolicy{MaxConformanceAge: 24 * time.Hour}
	source := decisionKeySource(t)

	// Driven off the injected evaluation time, not the wall clock: the same
	// candidate/GeneratedAt pair proceeds when evaluated just inside the
	// window and requires remediation, naming conformance staleness, when
	// evaluated just outside it.
	within := generatedAt.Add(23 * time.Hour)
	evidenceWithin := fullDecisionEvidence(t, 230, within, generatedAt)
	manifest, err := release.BuildDecisionManifest("rel-conformance-within", evidenceWithin, policy, within, source)
	if err != nil {
		t.Fatalf("within window: BuildDecisionManifest: %v, manifest = %+v", err, manifest)
	}
	if manifest.Verdict != release.DecisionProceed {
		t.Fatalf("within window: Verdict = %s, want PROCEED", manifest.Verdict)
	}

	outside := generatedAt.Add(25 * time.Hour)
	evidenceOutside := fullDecisionEvidence(t, 231, outside, generatedAt)
	manifest, err = release.BuildDecisionManifest("rel-conformance-outside", evidenceOutside, policy, outside, source)
	if !errors.Is(err, release.ErrConformanceStale) {
		t.Fatalf("outside window: err = %v, want it to wrap ErrConformanceStale", err)
	}
	if !errors.Is(err, release.ErrDecisionNeedsRemediation) {
		t.Fatalf("outside window: err = %v, want it to wrap ErrDecisionNeedsRemediation", err)
	}
	if manifest.Verdict != release.DecisionRemediate {
		t.Fatalf("outside window: Verdict = %s, want REMEDIATE (stale conformance is fixable by a fresh run, not a distrust signal)", manifest.Verdict)
	}
	if manifest.Classes.Conformance {
		t.Fatal("outside window: Classes.Conformance = true, want false")
	}
}

func TestEvaluateDecision_ZeroConformanceWindowIsIncompleteEvidence(t *testing.T) {
	now := time.Date(2026, 9, 12, 20, 0, 0, 0, time.UTC)
	evidence := fullDecisionEvidence(t, 240, now, now.Add(-time.Hour))
	if _, err := release.EvaluateDecision("rel-zero-window", evidence, release.DecisionPolicy{}, now); !errors.Is(err, release.ErrDecisionEvidenceIncomplete) {
		t.Fatalf("err = %v, want ErrDecisionEvidenceIncomplete", err)
	}
}

func TestDecisionManifest_Explain(t *testing.T) {
	proceeded := release.DecisionManifest{Verdict: release.DecisionProceed, ID: "rel-x"}
	if explained := proceeded.Explain(); !containsSubstring(explained, "PROCEED") || !containsSubstring(explained, "rel-x") {
		t.Fatalf("Explain() = %q, want it to contain verdict and id", explained)
	}
	stopped := release.DecisionManifest{Verdict: release.DecisionStop, ID: "rel-y", Reasons: []string{"missing owner evidence"}}
	explained := stopped.Explain()
	for _, want := range []string{"STOP", "rel-y", "missing owner evidence"} {
		if !containsSubstring(explained, want) {
			t.Fatalf("Explain() = %q, want it to contain %q", explained, want)
		}
	}
}

func TestVerifyDecisionManifest_RejectsUntrustedSigner(t *testing.T) {
	now := time.Date(2026, 9, 12, 21, 0, 0, 0, time.UTC)
	evidence := fullDecisionEvidence(t, 250, now, now.Add(-time.Hour))
	policy := release.DecisionPolicy{MaxConformanceAge: 24 * time.Hour}
	source := decisionKeySource(t)

	manifest, err := release.BuildDecisionManifest("rel-untrusted", evidence, policy, now, source)
	if err != nil {
		t.Fatalf("BuildDecisionManifest: %v", err)
	}
	if err := release.VerifyDecisionManifest(manifest, map[string]bool{"0000000000000000000000000000000000000000000000000000000000000000": true}); err == nil {
		t.Fatal("VerifyDecisionManifest with an untrusted key set unexpectedly succeeded")
	}

	tampered := manifest
	tampered.ID = "rel-tampered"
	if err := release.VerifyDecisionManifest(tampered, nil); err == nil {
		t.Fatal("VerifyDecisionManifest on a tampered manifest unexpectedly succeeded")
	}
}
