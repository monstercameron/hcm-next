package evidenceexport

import (
	"errors"
	"testing"
	"time"
)

// TestTodo_ADMIN_007 is the primary contract test named by the planning
// registry: a valid request creates one bounded, resumable operation.
func TestTodo_ADMIN_007(t *testing.T) {
	now := time.Unix(1000, 0).UTC()
	m := NewManager()
	op, err := m.Start(validRequest(now), now)
	if err != nil {
		t.Fatal(err)
	}
	if op.ID == "" || op.Status != StatusPending || op.Total != 3 || op.Manifest.Purpose != PurposeEvidence || op.Manifest.Scope != "case:1" {
		t.Fatalf("operation = %+v", op)
	}
	if got := BuildView(op); !got.Allows(CommandResume) || got.ManifestDigest != "" {
		t.Fatalf("view = %+v", got)
	}
}

// TestTodo_ADMIN_007_Golden protects the stable manifest/progress shape used
// by operators and offline tooling.
func TestTodo_ADMIN_007_Golden(t *testing.T) {
	now := time.Unix(1000, 0).UTC()
	m := NewManager()
	op, err := m.Start(validRequest(now), now)
	if err != nil {
		t.Fatal(err)
	}
	op, err = m.Resume(op.ID, 0, now)
	if err != nil {
		t.Fatal(err)
	}
	if op.Status != StatusComplete || op.Completed != 3 || op.NextChunk != 2 || op.Manifest.ChunkCount != 2 || !op.Manifest.VerifyDigest() {
		t.Fatalf("golden operation = %+v", op)
	}
	if got := BuildView(op); got.CanResume || got.CanRetry || got.ManifestDigest != op.Manifest.ManifestDigest {
		t.Fatalf("golden view = %+v", got)
	}
}

// TestTodo_ADMIN_007_Mutation ensures every authorization/manifest binding
// remains tamper-evident in an offline package.
func TestTodo_ADMIN_007_Mutation(t *testing.T) {
	now := time.Unix(1000, 0).UTC()
	m := NewManager()
	op, err := m.Start(validRequest(now), now)
	if err != nil {
		t.Fatal(err)
	}
	op, err = m.Resume(op.ID, 0, now)
	if err != nil {
		t.Fatal(err)
	}
	p := op.Artifact(validRequest(now).Records)
	p.Manifest.PolicyProof = "forged"
	if !errors.Is(p.Verify(), ErrTampered) || VerifyOffline(p) {
		t.Fatal("forged policy proof accepted")
	}
}

// TestTodo_ADMIN_007_Security rejects requests that attempt to export fields
// outside the authorization grant and rejects expired continuation tokens.
func TestTodo_ADMIN_007_Security(t *testing.T) {
	now := time.Unix(1000, 0).UTC()
	r := validRequest(now)
	r.Records[0].Fields["ssn"] = "sensitive"
	if _, err := NewManager().Start(r, now); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("unauthorized field error = %v", err)
	}
	r = validRequest(now)
	r.Authorization.ExpiresAt = now.Add(time.Minute)
	m := NewManager()
	op, err := m.Start(r, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = m.Resume(op.ID, 0, now.Add(2*time.Minute)); !errors.Is(err, ErrExpired) {
		t.Fatalf("expired resume error = %v", err)
	}
}
