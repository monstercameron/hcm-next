package dsr

import "testing"

func TestNewEvidence_Deterministic(t *testing.T) {
	at := mustInstant(t, fxReceivedAt)
	a := newEvidence(EventIntake, "digest-1", at, "detail-1")
	b := newEvidence(EventIntake, "digest-1", at, "detail-1")
	if a.ID != b.ID {
		t.Fatalf("identical inputs produced different evidence ids: %s vs %s", a.ID, b.ID)
	}
	if a.RequestDigest != "digest-1" || a.Kind != EventIntake || a.Detail != "detail-1" {
		t.Fatalf("newEvidence did not preserve its inputs: %+v", a)
	}
}

func TestNewEvidence_DiffersPerField(t *testing.T) {
	at := mustInstant(t, fxReceivedAt)
	base := newEvidence(EventIntake, "digest-1", at, "detail-1")

	if newEvidence(EventVerified, "digest-1", at, "detail-1").ID == base.ID {
		t.Error("changing Kind did not change the evidence id")
	}
	if newEvidence(EventIntake, "digest-2", at, "detail-1").ID == base.ID {
		t.Error("changing RequestDigest did not change the evidence id")
	}
	if newEvidence(EventIntake, "digest-1", mustInstant(t, fxReceivedAt+1), "detail-1").ID == base.ID {
		t.Error("changing At did not change the evidence id")
	}
	if newEvidence(EventIntake, "digest-1", at, "detail-2").ID == base.ID {
		t.Error("changing Detail did not change the evidence id")
	}
}
