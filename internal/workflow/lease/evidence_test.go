package lease

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

var (
	evidenceTenant = uuid.MustParse("11111111-1111-4111-8111-111111111111")
	evidenceLease  = uuid.MustParse("22222222-2222-4222-8222-222222222222")
	evidenceAt     = time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	evidenceRes    = Resource{Kind: ResourceWorkflowInstance, ID: "instance:1"}
)

func sampleEvidence() Evidence {
	return newEvidence(TransitionAcquired, evidenceTenant, evidenceLease, evidenceRes,
		"workload:w#replica:1", 3, 2, evidenceAt, "")
}

func TestEvidence_DigestIsStableAcrossIdenticalRecords(t *testing.T) {
	a, b := sampleEvidence(), sampleEvidence()
	if a.Digest() == "" {
		t.Fatalf("evidence carries no digest")
	}
	if a.Digest() != b.Digest() {
		t.Fatalf("two identical evidence records digest differently: %s vs %s", a.Digest(), b.Digest())
	}
}

// Every field the record carries has to move the digest; a field the digest
// ignores is a field an operator cannot trust the record about.
func TestEvidence_EveryFieldMovesTheDigest(t *testing.T) {
	base := sampleEvidence()
	otherLease := uuid.MustParse("33333333-3333-4333-8333-333333333333")
	otherTenant := uuid.MustParse("44444444-4444-4444-8444-444444444444")

	variants := map[string]Evidence{
		"kind": newEvidence(TransitionTakenOver, evidenceTenant, evidenceLease, evidenceRes,
			"workload:w#replica:1", 3, 2, evidenceAt, ""),
		"tenant": newEvidence(TransitionAcquired, otherTenant, evidenceLease, evidenceRes,
			"workload:w#replica:1", 3, 2, evidenceAt, ""),
		"lease": newEvidence(TransitionAcquired, evidenceTenant, otherLease, evidenceRes,
			"workload:w#replica:1", 3, 2, evidenceAt, ""),
		"resource kind": newEvidence(TransitionAcquired, evidenceTenant, evidenceLease,
			Resource{Kind: ResourceQueue, ID: "instance:1"}, "workload:w#replica:1", 3, 2, evidenceAt, ""),
		"resource id": newEvidence(TransitionAcquired, evidenceTenant, evidenceLease,
			Resource{Kind: ResourceWorkflowInstance, ID: "instance:2"}, "workload:w#replica:1", 3, 2, evidenceAt, ""),
		"holder": newEvidence(TransitionAcquired, evidenceTenant, evidenceLease, evidenceRes,
			"workload:w#replica:2", 3, 2, evidenceAt, ""),
		"token": newEvidence(TransitionAcquired, evidenceTenant, evidenceLease, evidenceRes,
			"workload:w#replica:1", 4, 2, evidenceAt, ""),
		"prior token": newEvidence(TransitionAcquired, evidenceTenant, evidenceLease, evidenceRes,
			"workload:w#replica:1", 3, 1, evidenceAt, ""),
		"instant": newEvidence(TransitionAcquired, evidenceTenant, evidenceLease, evidenceRes,
			"workload:w#replica:1", 3, 2, evidenceAt.Add(time.Second), ""),
		"reason": newEvidence(TransitionAcquired, evidenceTenant, evidenceLease, evidenceRes,
			"workload:w#replica:1", 3, 2, evidenceAt, "took over"),
	}
	for field, variant := range variants {
		if variant.Digest() == base.Digest() {
			t.Fatalf("changing %s did not move the evidence digest", field)
		}
	}
}

func TestEvidence_InstantIsNormalizedToUTC(t *testing.T) {
	zone := time.FixedZone("UTC-5", -5*3600)
	shifted := newEvidence(TransitionAcquired, evidenceTenant, evidenceLease, evidenceRes,
		"workload:w#replica:1", 3, 2, evidenceAt.In(zone), "")
	if shifted.Digest() != sampleEvidence().Digest() {
		t.Fatalf("the same instant in another zone digested differently")
	}
	if shifted.At.Location() != time.UTC {
		t.Fatalf("evidence instant was not normalized to UTC: %v", shifted.At.Location())
	}
}

func TestEvidence_EveryDeclaredTransitionIsDistinct(t *testing.T) {
	seen := map[TransitionKind]bool{}
	for _, kind := range []TransitionKind{
		TransitionAcquired, TransitionTakenOver, TransitionRenewed, TransitionReleased,
		TransitionExpired, TransitionRevoked, TransitionVerified, TransitionRefused,
	} {
		if kind == "" {
			t.Fatalf("a declared transition kind is empty")
		}
		if seen[kind] {
			t.Fatalf("transition kind %q is declared twice", kind)
		}
		seen[kind] = true
	}
	if len(seen) != 8 {
		t.Fatalf("expected eight declared transition kinds, got %d", len(seen))
	}
}
