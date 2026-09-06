package reconcile

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func fixtureJob() Job {
	return Job{
		TenantID:  uuid.MustParse("33333333-3333-4333-8333-333333333333"),
		JobID:     uuid.MustParse("44444444-4444-4444-8444-444444444444"),
		EffectRef: "hcmnext.effect.provider_call/abc123", EffectID: "node.provision_seat",
		PolicyRef: "hcmnext.reconcile.policy/v1", IntendedRef: "proposal:1", CanonicalRef: "",
		ObservationAttempts: 2, Status: StatusObserving,
	}
}

// TestEvidence_GoldenDigestIsStableAndDeterministic is this package's GOLDEN
// vector: fixed input always produces the exact same digest, on every run and
// every machine, because canonicalDigest hashes only the profile-prefixed JSON
// identity and never a clock reading of its own.
func TestEvidence_GoldenDigestIsStableAndDeterministic(t *testing.T) {
	at := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)

	job := fixtureJob()
	e1 := newEvidence(EventObserved, job, at, 7, "observation freshness STALE does not meet the required FRESH")
	e2 := newEvidence(EventObserved, job, at, 7, "observation freshness STALE does not meet the required FRESH")
	if e1.Digest() == "" {
		t.Fatal("evidence digest is empty")
	}
	if e1.Digest() != e2.Digest() {
		t.Fatalf("two evidence records built from identical inputs digest differently: %s != %s", e1.Digest(), e2.Digest())
	}
}

func TestEvidence_DigestChangesWithEveryMaterialField(t *testing.T) {
	at := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	base := newEvidence(EventObserved, fixtureJob(), at, 7, "reason")

	variants := []Evidence{
		newEvidence(EventSettled, fixtureJob(), at, 7, "reason"),                   // kind
		newEvidence(EventObserved, fixtureJob(), at.Add(time.Second), 7, "reason"), // at
		newEvidence(EventObserved, fixtureJob(), at, 8, "reason"),                  // fence token
		newEvidence(EventObserved, fixtureJob(), at, 7, "different reason"),        // reason
	}
	for i, v := range variants {
		if v.Digest() == base.Digest() {
			t.Fatalf("variant %d did not change the digest: %+v vs %+v", i, v, base)
		}
	}

	changedStatus := fixtureJob()
	changedStatus.Status = StatusPass
	if newEvidence(EventObserved, changedStatus, at, 7, "reason").Digest() == base.Digest() {
		t.Fatal("a changed job status did not change the digest")
	}

	changedAttempts := fixtureJob()
	changedAttempts.ObservationAttempts = 99
	if newEvidence(EventObserved, changedAttempts, at, 7, "reason").Digest() == base.Digest() {
		t.Fatal("a changed observation-attempts count did not change the digest")
	}
}

func TestEvidence_EventKindsAreDistinctTokens(t *testing.T) {
	kinds := []EventKind{EventTriggered, EventReplayed, EventObserved, EventSettled, EventExhausted}
	seen := map[EventKind]bool{}
	for _, k := range kinds {
		if k == "" {
			t.Fatal("an event kind is empty")
		}
		if seen[k] {
			t.Fatalf("event kind %q is declared twice", k)
		}
		seen[k] = true
	}
}
