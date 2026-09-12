package release_test

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/tools/policy/release"
)

// healthyStage returns a fully present, passing StageHealth as of
// generatedAt for conformance, evaluated freshly (no staleness) relative
// to whatever `now` a caller passes into Advance/Resume alongside it.
func healthyStage(measuredAt time.Time) release.StageHealth {
	return release.StageHealth{
		SLO:            release.SLOEvidence{WithinBudget: true, MeasuredAt: measuredAt},
		Telemetry:      release.TelemetryEvidence{Healthy: true, ObservedAt: measuredAt},
		Reconciliation: release.ReconciliationEvidence{Consistent: true, CheckedAt: measuredAt},
		Conformance:    release.ConformanceEvidence{GeneratedAt: measuredAt},
	}
}

func TestTodo_CICD_005(t *testing.T) {
	rollout, err := release.NewRollout("rel-primary", []string{"canary-5", "canary-25", "full"})
	if err != nil {
		t.Fatalf("NewRollout: %v", err)
	}
	policy := release.RolloutPolicy{MaxConformanceAge: 24 * time.Hour}

	t0 := time.Date(2026, 9, 12, 9, 0, 0, 0, time.UTC)
	rec, err := rollout.Advance(healthyStage(t0), policy, t0)
	if err != nil {
		t.Fatalf("advance stage 1: %v", err)
	}
	if rec.Stage != "canary-5" || rec.Status != release.RolloutInProgress || rec.Kind != "advance" {
		t.Fatalf("advance stage 1 record = %+v, want stage canary-5, status IN_PROGRESS, kind advance", rec)
	}

	t1 := t0.Add(time.Hour)
	rec, err = rollout.Advance(healthyStage(t1), policy, t1)
	if err != nil {
		t.Fatalf("advance stage 2: %v", err)
	}
	if rec.Stage != "canary-25" || rec.Status != release.RolloutInProgress {
		t.Fatalf("advance stage 2 record = %+v, want stage canary-25, status IN_PROGRESS", rec)
	}

	t2 := t1.Add(time.Hour)
	rec, err = rollout.Advance(healthyStage(t2), policy, t2)
	if err != nil {
		t.Fatalf("advance stage 3: %v", err)
	}
	if rec.Stage != "full" || rec.Status != release.RolloutCompleted || rec.Kind != "advance" {
		t.Fatalf("final advance record = %+v, want stage full, status COMPLETED", rec)
	}
	if rollout.Status() != release.RolloutCompleted {
		t.Fatalf("rollout.Status() = %s, want COMPLETED", rollout.Status())
	}

	history := rollout.History()
	if len(history) != 3 {
		t.Fatalf("history length = %d, want 3 (one record per stage)", len(history))
	}
	wantStages := []string{"canary-5", "canary-25", "full"}
	for i, want := range wantStages {
		if history[i].Stage != want {
			t.Fatalf("history[%d].Stage = %q, want %q", i, history[i].Stage, want)
		}
		if history[i].Sequence != i+1 {
			t.Fatalf("history[%d].Sequence = %d, want %d", i, history[i].Sequence, i+1)
		}
		if history[i].Digest == "" {
			t.Fatalf("history[%d].Digest is empty, want a computed digest", i)
		}
	}

	// A completed rollout cannot be advanced further.
	if _, err := rollout.Advance(healthyStage(t2), policy, t2); !errors.Is(err, release.ErrRolloutNotActive) {
		t.Fatalf("advance after completion err = %v, want ErrRolloutNotActive", err)
	}
}

func TestTodo_CICD_005_Fault(t *testing.T) {
	policy := release.RolloutPolicy{MaxConformanceAge: 24 * time.Hour}
	now := time.Date(2026, 9, 12, 10, 0, 0, 0, time.UTC)

	cases := []struct {
		name    string
		mutate  func(h release.StageHealth) release.StageHealth
		wantErr error
	}{
		{
			name: "SLO breach",
			mutate: func(h release.StageHealth) release.StageHealth {
				h.SLO.WithinBudget = false
				return h
			},
			wantErr: release.ErrRolloutSLOBreach,
		},
		{
			name: "telemetry breach",
			mutate: func(h release.StageHealth) release.StageHealth {
				h.Telemetry.Healthy = false
				return h
			},
			wantErr: release.ErrRolloutTelemetryBreach,
		},
		{
			name: "reconciliation breach",
			mutate: func(h release.StageHealth) release.StageHealth {
				h.Reconciliation.Consistent = false
				return h
			},
			wantErr: release.ErrRolloutReconciliationBreach,
		},
		{
			name: "conformance breach",
			mutate: func(h release.StageHealth) release.StageHealth {
				h.Conformance.GeneratedAt = now.Add(-48 * time.Hour)
				return h
			},
			wantErr: release.ErrRolloutConformanceBreach,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rollout, err := release.NewRollout("rel-fault-"+tc.name, []string{"canary-5", "full"})
			if err != nil {
				t.Fatalf("NewRollout: %v", err)
			}
			breach := tc.mutate(healthyStage(now))
			rec, err := rollout.Advance(breach, policy, now)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("Advance err = %v, want %v", err, tc.wantErr)
			}
			if rec.Status != release.RolloutPaused || rec.Kind != "pause" {
				t.Fatalf("record = %+v, want status PAUSED, kind pause", rec)
			}
			if rollout.Status() != release.RolloutPaused {
				t.Fatalf("rollout.Status() = %s, want PAUSED", rollout.Status())
			}
			if rec.Stage != "canary-5" {
				t.Fatalf("paused record.Stage = %q, want canary-5 (the stage that was being evaluated)", rec.Stage)
			}

			// Pause is not fail (the rollout is still open to Resume) and
			// is not continue: a plain Advance call — even with perfectly
			// healthy evidence this time — must not silently advance a
			// paused rollout on a subsequent call.
			historyBefore := rollout.History()
			_, err = rollout.Advance(healthyStage(now.Add(time.Minute)), policy, now.Add(time.Minute))
			if !errors.Is(err, release.ErrRolloutPaused) {
				t.Fatalf("Advance on paused rollout err = %v, want ErrRolloutPaused", err)
			}
			if rollout.Status() != release.RolloutPaused {
				t.Fatalf("rollout.Status() after blocked Advance = %s, want still PAUSED", rollout.Status())
			}
			if len(rollout.History()) != len(historyBefore) {
				t.Fatalf("history grew from %d to %d entries on a blocked Advance call; it must not advance or record anything new", len(historyBefore), len(rollout.History()))
			}
		})
	}
}

func TestTodo_CICD_005_Conformance(t *testing.T) {
	rollout, err := release.NewRollout("rel-conformance", []string{"canary-5", "full"})
	if err != nil {
		t.Fatalf("NewRollout: %v", err)
	}
	policy := release.RolloutPolicy{MaxConformanceAge: 24 * time.Hour}

	generatedAt := time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC)
	pauseAt := generatedAt.Add(25 * time.Hour) // just outside the window

	stale := healthyStage(pauseAt)
	stale.Conformance.GeneratedAt = generatedAt
	_, err = rollout.Advance(stale, policy, pauseAt)
	if !errors.Is(err, release.ErrRolloutConformanceBreach) || !errors.Is(err, release.ErrConformanceStale) {
		t.Fatalf("initial breach err = %v, want both ErrRolloutConformanceBreach and ErrConformanceStale", err)
	}
	if rollout.Status() != release.RolloutPaused {
		t.Fatalf("rollout.Status() = %s, want PAUSED", rollout.Status())
	}

	// Reopening (resuming) the paused rollout is refused as long as the
	// conformance evidence offered is still stale, driven purely off the
	// injected evaluation time passed to Resume, never a real clock.
	stillStaleAt := pauseAt.Add(time.Hour)
	stillStale := healthyStage(stillStaleAt)
	stillStale.Conformance.GeneratedAt = generatedAt
	if _, err := rollout.Resume(stillStale, policy, stillStaleAt); !errors.Is(err, release.ErrRolloutConformanceBreach) {
		t.Fatalf("resume with still-stale conformance err = %v, want ErrRolloutConformanceBreach", err)
	}
	if rollout.Status() != release.RolloutPaused {
		t.Fatalf("rollout.Status() after failed resume = %s, want still PAUSED", rollout.Status())
	}

	// Once fresh conformance evidence is presented, Resume both clears the
	// pause and advances the stage.
	freshAt := stillStaleAt.Add(time.Hour)
	fresh := healthyStage(freshAt)
	rec, err := rollout.Resume(fresh, policy, freshAt)
	if err != nil {
		t.Fatalf("resume with fresh conformance evidence: %v", err)
	}
	if rec.Kind != "advance" || rec.Status != release.RolloutInProgress {
		t.Fatalf("resume record = %+v, want kind advance, status IN_PROGRESS", rec)
	}
	if rollout.Status() != release.RolloutInProgress {
		t.Fatalf("rollout.Status() = %s, want IN_PROGRESS", rollout.Status())
	}
}

func TestTodo_CICD_005_Golden(t *testing.T) {
	out, digest, publicKey := admissionFixtureBundle(t, 100, "prod/pilot-cell")
	admissionPolicy := release.AdmissionPolicy{
		TrustedPublicKeys: map[string]bool{publicKey: true},
		Target:            release.ApprovedTarget{ManifestDigest: digest, Scope: "prod/pilot-cell"},
		MaxConformanceAge: 24 * time.Hour,
	}
	rollbackCandidate := release.DeploymentCandidate{
		Bundle:        out,
		Scope:         "prod/pilot-cell",
		Vulnerability: release.VulnerabilityEvidence{Scanned: true},
		Conformance:   release.ConformanceEvidence{GeneratedAt: time.Date(2026, 9, 12, 11, 0, 0, 0, time.UTC)},
	}

	rollout, err := release.NewRollout("rel-golden", []string{"canary-5", "canary-25", "full"})
	if err != nil {
		t.Fatalf("NewRollout: %v", err)
	}
	policy := release.RolloutPolicy{MaxConformanceAge: 24 * time.Hour}

	t0 := time.Date(2026, 9, 12, 9, 0, 0, 0, time.UTC)
	if _, err := rollout.Advance(healthyStage(t0), policy, t0); err != nil {
		t.Fatalf("advance canary-5: %v", err)
	}

	t1 := t0.Add(time.Hour)
	breach := healthyStage(t1)
	breach.SLO.WithinBudget = false
	if _, err := rollout.Advance(breach, policy, t1); !errors.Is(err, release.ErrRolloutSLOBreach) {
		t.Fatalf("advance canary-25 (expected breach): err = %v, want ErrRolloutSLOBreach", err)
	}

	t2 := t1.Add(time.Hour)
	if _, err := rollout.Resume(healthyStage(t2), policy, t2); err != nil {
		t.Fatalf("resume canary-25: %v", err)
	}

	t3 := t2.Add(time.Hour)
	if _, err := rollout.Advance(healthyStage(t3), policy, t3); err != nil {
		t.Fatalf("advance full: %v", err)
	}
	if rollout.Status() != release.RolloutCompleted {
		t.Fatalf("rollout.Status() = %s, want COMPLETED before rollback", rollout.Status())
	}

	t4 := t3.Add(time.Hour)
	if _, err := rollout.Rollback(rollbackCandidate, admissionPolicy, t4); err != nil {
		t.Fatalf("rollback: %v", err)
	}
	if rollout.Status() != release.RolloutFenced {
		t.Fatalf("rollout.Status() = %s, want FENCED after rollback", rollout.Status())
	}

	t5 := t4.Add(time.Hour)
	health := release.HealthEvidence{Healthy: true, ObservedAt: t5}
	reconciliation := release.ReconciliationEvidence{Consistent: true, CheckedAt: t5}
	if _, err := rollout.Reopen(health, reconciliation, t5); err != nil {
		t.Fatalf("reopen: %v", err)
	}
	if rollout.Status() != release.RolloutRolledBack {
		t.Fatalf("rollout.Status() = %s, want ROLLED_BACK after reopen", rollout.Status())
	}

	snapshot := rollout.Snapshot()
	if len(snapshot.History) != 6 {
		t.Fatalf("snapshot history length = %d, want 6 (advance, pause, advance, advance, rollback, reopen)", len(snapshot.History))
	}

	got, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		t.Fatalf("marshal snapshot: %v", err)
	}
	got = append(got, '\n')

	goldenPath := filepath.Join("testdata", "cicd005_rollout_golden.json")
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
		t.Fatalf("rollout/rollback snapshot bytes changed:\ngot:\n%s\nwant:\n%s", got, want)
	}
}

func TestRollout_RollbackRequiresVerifiedApprovedDigest(t *testing.T) {
	out, digest, publicKey := admissionFixtureBundle(t, 101, "prod/pilot-cell")
	otherOut, otherDigest, otherPublicKey := admissionFixtureBundle(t, 102, "prod/pilot-cell")
	if otherDigest == digest {
		t.Fatal("test setup: expected two distinct bundle digests")
	}
	now := time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)
	approvedTarget := release.ApprovedTarget{ManifestDigest: digest, Scope: "prod/pilot-cell"}

	baseCandidate := release.DeploymentCandidate{
		Bundle:        out,
		Scope:         "prod/pilot-cell",
		Vulnerability: release.VulnerabilityEvidence{Scanned: true},
		Conformance:   release.ConformanceEvidence{GeneratedAt: now.Add(-time.Hour)},
	}

	newRollout := func(t *testing.T, name string) *release.Rollout {
		t.Helper()
		r, err := release.NewRollout(name, []string{"canary-5"})
		if err != nil {
			t.Fatalf("NewRollout: %v", err)
		}
		return r
	}

	t.Run("untrusted signer refused", func(t *testing.T) {
		rollout := newRollout(t, "rel-rb-untrusted")
		policy := release.AdmissionPolicy{
			TrustedPublicKeys: map[string]bool{strings.Repeat("f", 64): true},
			Target:            approvedTarget,
			MaxConformanceAge: 24 * time.Hour,
		}
		rec, err := rollout.Rollback(baseCandidate, policy, now)
		if !errors.Is(err, release.ErrCandidateUnsigned) {
			t.Fatalf("Rollback err = %v, want ErrCandidateUnsigned", err)
		}
		if rec.Kind != "rollback_refused" {
			t.Fatalf("record.Kind = %q, want rollback_refused", rec.Kind)
		}
		if rollout.Status() != release.RolloutInProgress {
			t.Fatalf("rollout.Status() = %s, want unchanged IN_PROGRESS after refusal", rollout.Status())
		}
	})

	t.Run("verified but unapproved digest refused", func(t *testing.T) {
		rollout := newRollout(t, "rel-rb-unapproved")
		policy := release.AdmissionPolicy{
			TrustedPublicKeys: map[string]bool{publicKey: true, otherPublicKey: true},
			Target:            approvedTarget, // approves `digest`, not `otherDigest`
			MaxConformanceAge: 24 * time.Hour,
		}
		wrongCandidate := release.DeploymentCandidate{
			Bundle:        otherOut,
			Scope:         "prod/pilot-cell",
			Vulnerability: release.VulnerabilityEvidence{Scanned: true},
			Conformance:   release.ConformanceEvidence{GeneratedAt: now.Add(-time.Hour)},
		}
		rec, err := rollout.Rollback(wrongCandidate, policy, now)
		if !errors.Is(err, release.ErrCandidateIncompatible) {
			t.Fatalf("Rollback err = %v, want ErrCandidateIncompatible", err)
		}
		if rec.ManifestDigest != otherDigest {
			t.Fatalf("refused record.ManifestDigest = %q, want the candidate's own verified digest %q", rec.ManifestDigest, otherDigest)
		}
		if rollout.Status() != release.RolloutInProgress {
			t.Fatalf("rollout.Status() = %s, want unchanged IN_PROGRESS after refusal", rollout.Status())
		}
	})

	t.Run("exact verified and approved digest accepted and fences writers", func(t *testing.T) {
		rollout := newRollout(t, "rel-rb-accepted")
		policy := release.AdmissionPolicy{
			TrustedPublicKeys: map[string]bool{publicKey: true},
			Target:            approvedTarget,
			MaxConformanceAge: 24 * time.Hour,
		}
		rec, err := rollout.Rollback(baseCandidate, policy, now)
		if err != nil {
			t.Fatalf("Rollback: %v", err)
		}
		if rec.Kind != "rollback" || rec.Status != release.RolloutFenced {
			t.Fatalf("record = %+v, want kind rollback, status FENCED", rec)
		}
		if rec.ManifestDigest != digest {
			t.Fatalf("record.ManifestDigest = %q, want %q", rec.ManifestDigest, digest)
		}
		if rollout.Status() != release.RolloutFenced {
			t.Fatalf("rollout.Status() = %s, want FENCED", rollout.Status())
		}

		// A second rollback attempt against an already-fenced rollout is
		// refused outright.
		if _, err := rollout.Rollback(baseCandidate, policy, now); !errors.Is(err, release.ErrRolloutAlreadyFenced) {
			t.Fatalf("second Rollback err = %v, want ErrRolloutAlreadyFenced", err)
		}
	})
}

func TestRollout_ReopenRequiresHealthAndReconciliationEvidence(t *testing.T) {
	out, digest, publicKey := admissionFixtureBundle(t, 103, "prod/pilot-cell")
	now := time.Date(2026, 9, 12, 13, 0, 0, 0, time.UTC)
	policy := release.AdmissionPolicy{
		TrustedPublicKeys: map[string]bool{publicKey: true},
		Target:            release.ApprovedTarget{ManifestDigest: digest, Scope: "prod/pilot-cell"},
		MaxConformanceAge: 24 * time.Hour,
	}
	candidate := release.DeploymentCandidate{
		Bundle:        out,
		Scope:         "prod/pilot-cell",
		Vulnerability: release.VulnerabilityEvidence{Scanned: true},
		Conformance:   release.ConformanceEvidence{GeneratedAt: now.Add(-time.Hour)},
	}

	setup := func(t *testing.T) *release.Rollout {
		t.Helper()
		rollout, err := release.NewRollout("rel-reopen", []string{"canary-5"})
		if err != nil {
			t.Fatalf("NewRollout: %v", err)
		}
		if _, err := rollout.Rollback(candidate, policy, now); err != nil {
			t.Fatalf("Rollback: %v", err)
		}
		return rollout
	}

	t.Run("reopen refused before any rollback", func(t *testing.T) {
		rollout, err := release.NewRollout("rel-reopen-none", []string{"canary-5"})
		if err != nil {
			t.Fatalf("NewRollout: %v", err)
		}
		health := release.HealthEvidence{Healthy: true, ObservedAt: now}
		reconciliation := release.ReconciliationEvidence{Consistent: true, CheckedAt: now}
		if _, err := rollout.Reopen(health, reconciliation, now); !errors.Is(err, release.ErrRolloutNotFenced) {
			t.Fatalf("Reopen err = %v, want ErrRolloutNotFenced", err)
		}
	})

	t.Run("zero-value evidence keeps it closed", func(t *testing.T) {
		rollout := setup(t)
		// The Go zero value for both evidence types: no observation was
		// ever made. It must never be read as healthy.
		if _, err := rollout.Reopen(release.HealthEvidence{}, release.ReconciliationEvidence{}, now); !errors.Is(err, release.ErrReopenEvidenceIncomplete) {
			t.Fatalf("Reopen with zero-value evidence err = %v, want ErrReopenEvidenceIncomplete", err)
		}
		if rollout.Status() != release.RolloutFenced {
			t.Fatalf("rollout.Status() = %s, want still FENCED", rollout.Status())
		}
	})

	t.Run("a true boolean without an observation timestamp is still absent", func(t *testing.T) {
		rollout := setup(t)
		// Healthy/Consistent are both true, but ObservedAt/CheckedAt are
		// the Go zero value: no observation actually happened. This is
		// exactly the defect pattern the todo calls out — a zero value
		// must never be read as "healthy" — applied to the boolean's
		// companion timestamp rather than the boolean itself.
		health := release.HealthEvidence{Healthy: true}
		reconciliation := release.ReconciliationEvidence{Consistent: true}
		if _, err := rollout.Reopen(health, reconciliation, now); !errors.Is(err, release.ErrReopenEvidenceIncomplete) {
			t.Fatalf("Reopen with timestamp-less evidence err = %v, want ErrReopenEvidenceIncomplete", err)
		}
		if rollout.Status() != release.RolloutFenced {
			t.Fatalf("rollout.Status() = %s, want still FENCED", rollout.Status())
		}
	})

	t.Run("present but unhealthy evidence is refused for a different reason", func(t *testing.T) {
		rollout := setup(t)
		health := release.HealthEvidence{Healthy: false, ObservedAt: now}
		reconciliation := release.ReconciliationEvidence{Consistent: true, CheckedAt: now}
		if _, err := rollout.Reopen(health, reconciliation, now); !errors.Is(err, release.ErrReopenNotHealthy) {
			t.Fatalf("Reopen with unhealthy evidence err = %v, want ErrReopenNotHealthy", err)
		}
		if rollout.Status() != release.RolloutFenced {
			t.Fatalf("rollout.Status() = %s, want still FENCED", rollout.Status())
		}
	})

	t.Run("present and healthy evidence reopens", func(t *testing.T) {
		rollout := setup(t)
		health := release.HealthEvidence{Healthy: true, ObservedAt: now}
		reconciliation := release.ReconciliationEvidence{Consistent: true, CheckedAt: now}
		rec, err := rollout.Reopen(health, reconciliation, now)
		if err != nil {
			t.Fatalf("Reopen: %v", err)
		}
		if rec.Kind != "reopen" || rec.Status != release.RolloutRolledBack {
			t.Fatalf("record = %+v, want kind reopen, status ROLLED_BACK", rec)
		}
		if rollout.Status() != release.RolloutRolledBack {
			t.Fatalf("rollout.Status() = %s, want ROLLED_BACK", rollout.Status())
		}
	})
}

func TestRollout_HistoryPreservedAndRecordsAreCopies(t *testing.T) {
	rollout, err := release.NewRollout("rel-history", []string{"canary-5", "canary-25", "full"})
	if err != nil {
		t.Fatalf("NewRollout: %v", err)
	}
	policy := release.RolloutPolicy{MaxConformanceAge: 24 * time.Hour}
	now := time.Date(2026, 9, 12, 14, 0, 0, 0, time.UTC)

	if _, err := rollout.Advance(healthyStage(now), policy, now); err != nil {
		t.Fatalf("advance 1: %v", err)
	}
	breach := healthyStage(now)
	breach.Telemetry.Healthy = false
	if _, err := rollout.Advance(breach, policy, now); !errors.Is(err, release.ErrRolloutTelemetryBreach) {
		t.Fatalf("advance 2 (expected breach): %v", err)
	}

	before := rollout.History()
	if len(before) != 2 {
		t.Fatalf("history length = %d, want 2", len(before))
	}

	// Mutating a caller's copy of the history must never affect what the
	// rollout has stored.
	mutated := rollout.History()
	mutated[0].Stage = "tampered"
	mutated[0].Status = release.RolloutCompleted
	// Appending is part of the attempt, not decoration: a returned slice with
	// spare capacity would write the new record straight into the rollout's
	// own backing array. Asserting on the grown slice keeps that attempt
	// honest -- the caller's copy must grow while the store does not.
	grown := append(mutated, release.StageRecord{Stage: "injected"})
	if len(grown) != 3 {
		t.Fatalf("appending to a returned copy produced %d records in the caller's own slice, want 3", len(grown))
	}

	after := rollout.History()
	if len(after) != 2 {
		t.Fatalf("history length after external mutation attempt = %d, want still 2", len(after))
	}
	if after[0].Stage != before[0].Stage || after[0].Status != before[0].Status {
		t.Fatalf("stored history[0] changed to %+v after mutating a returned copy; history must be immutable", after[0])
	}

	// Resuming past the pause must append a new record, never rewrite the
	// pause record that is already there.
	resumed, err := rollout.Resume(healthyStage(now.Add(time.Hour)), policy, now.Add(time.Hour))
	if err != nil {
		t.Fatalf("resume: %v", err)
	}
	final := rollout.History()
	if len(final) != 3 {
		t.Fatalf("history length after resume = %d, want 3 (prior two entries kept, one appended)", len(final))
	}
	if final[0] != before[0] || final[1] != before[1] {
		t.Fatalf("resume rewrote a prior history entry: final[0:2] = %+v, want unchanged %+v", final[0:2], before)
	}
	if final[2] != resumed {
		t.Fatalf("final[2] = %+v, want the resume record %+v", final[2], resumed)
	}
}

func TestRollout_ZeroValueStageHealthIsNeverHealthy(t *testing.T) {
	rollout, err := release.NewRollout("rel-zero-health", []string{"canary-5"})
	if err != nil {
		t.Fatalf("NewRollout: %v", err)
	}
	policy := release.RolloutPolicy{MaxConformanceAge: 24 * time.Hour}
	now := time.Date(2026, 9, 12, 15, 0, 0, 0, time.UTC)

	if _, err := rollout.Advance(release.StageHealth{}, policy, now); !errors.Is(err, release.ErrEvidenceIncomplete) {
		t.Fatalf("Advance with zero-value StageHealth err = %v, want ErrEvidenceIncomplete", err)
	}
	if rollout.Status() != release.RolloutPaused {
		t.Fatalf("rollout.Status() = %s, want PAUSED (absence of evidence is not passing evidence)", rollout.Status())
	}

	rollout2, err := release.NewRollout("rel-zero-slo-timestamp", []string{"canary-5"})
	if err != nil {
		t.Fatalf("NewRollout: %v", err)
	}
	// WithinBudget is true, but MeasuredAt is the Go zero value: no
	// observation actually happened, so it must still be treated as
	// absent, not as a passing SLO check.
	half := healthyStage(now)
	half.SLO = release.SLOEvidence{WithinBudget: true}
	if _, err := rollout2.Advance(half, policy, now); !errors.Is(err, release.ErrEvidenceIncomplete) {
		t.Fatalf("Advance with timestamp-less SLO evidence err = %v, want ErrEvidenceIncomplete", err)
	}
}

func TestNewRollout_RequiresIDAndStages(t *testing.T) {
	if _, err := release.NewRollout("", []string{"canary-5"}); !errors.Is(err, release.ErrEvidenceIncomplete) {
		t.Fatalf("NewRollout with empty id err = %v, want ErrEvidenceIncomplete", err)
	}
	if _, err := release.NewRollout("rel", nil); !errors.Is(err, release.ErrEvidenceIncomplete) {
		t.Fatalf("NewRollout with no stages err = %v, want ErrEvidenceIncomplete", err)
	}
}

func TestRollout_ZeroConformanceWindowIsIncompleteEvidence(t *testing.T) {
	rollout, err := release.NewRollout("rel-zero-window", []string{"canary-5"})
	if err != nil {
		t.Fatalf("NewRollout: %v", err)
	}
	now := time.Date(2026, 9, 12, 16, 0, 0, 0, time.UTC)
	if _, err := rollout.Advance(healthyStage(now), release.RolloutPolicy{}, now); !errors.Is(err, release.ErrEvidenceIncomplete) {
		t.Fatalf("Advance with zero MaxConformanceAge err = %v, want ErrEvidenceIncomplete", err)
	}
}

func TestStageRecord_Explain(t *testing.T) {
	withReason := release.StageRecord{Kind: "pause", Stage: "canary-5", Status: release.RolloutPaused, Digest: "abc123", Reason: "SLO breach"}
	explained := withReason.Explain()
	if explained == "" {
		t.Fatal("Explain() returned empty string")
	}
	for _, want := range []string{"pause", "canary-5", string(release.RolloutPaused), "abc123", "SLO breach"} {
		if !containsSubstring(explained, want) {
			t.Fatalf("Explain() = %q, want it to contain %q", explained, want)
		}
	}

	withoutReason := release.StageRecord{Kind: "advance", Stage: "full", Status: release.RolloutCompleted, Digest: "def456"}
	explained = withoutReason.Explain()
	if containsSubstring(explained, "reason") {
		t.Fatalf("Explain() with no reason = %q, want it to omit the reason clause", explained)
	}
}

func containsSubstring(haystack, needle string) bool {
	return len(haystack) >= len(needle) && (needle == "" || indexOf(haystack, needle) >= 0)
}

func indexOf(haystack, needle string) int {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}

func TestRollout_SnapshotIsACopy(t *testing.T) {
	rollout, err := release.NewRollout("rel-snapshot", []string{"canary-5"})
	if err != nil {
		t.Fatalf("NewRollout: %v", err)
	}
	now := time.Date(2026, 9, 12, 17, 0, 0, 0, time.UTC)
	policy := release.RolloutPolicy{MaxConformanceAge: 24 * time.Hour}
	if _, err := rollout.Advance(healthyStage(now), policy, now); err != nil {
		t.Fatalf("advance: %v", err)
	}
	if rollout.ID() != "rel-snapshot" {
		t.Fatalf("ID() = %q, want rel-snapshot", rollout.ID())
	}
	snap := rollout.Snapshot()
	snap.History[0].Stage = "tampered"
	snap.Status = release.RolloutFenced
	again := rollout.Snapshot()
	if again.Status != release.RolloutCompleted {
		t.Fatalf("Snapshot().Status = %s after mutating a prior snapshot copy, want unaffected COMPLETED", again.Status)
	}
	if again.History[0].Stage == "tampered" {
		t.Fatal("Snapshot().History[0].Stage reflects a mutation made to a previously returned copy")
	}
}
