package evidenceexport

import (
	"errors"
	"testing"
	"time"
)

func validRequest(now time.Time) Request {
	return Request{Query: "incident-evidence", From: now.Add(-time.Hour), To: now, ChunkSize: 2, Authorization: Authorization{TenantID: "t1", SubjectID: "operator", Scope: "case:1", Purpose: PurposeEvidence, IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour), AllowedFields: []string{"id", "kind"}, RedactionProfile: "minimum", SourceProof: "source-v1", ConfigProof: "config-v1", PolicyProof: "policy-v1"}, Records: []Record{{ID: "a", Digest: "da", Fields: map[string]string{"id": "a"}}, {ID: "b", Digest: "db", Fields: map[string]string{"kind": "event"}}, {ID: "c", Digest: "dc"}}}
}

func TestADMIN007RegistryExact(t *testing.T) {
	r := Registry()
	if r.ID != "ADMIN-007" || r.Version != ContractVersion || r.Purpose != PurposeEvidence || r.MaxTTL != MaxTTL || !r.SupportsResume || !r.OfflineVerification || !r.RedactionRequired {
		t.Fatalf("registry=%+v", r)
	}
}
func TestADMIN007ResumableAndOfflineTamperDetection(t *testing.T) {
	now := time.Unix(1000, 0).UTC()
	m := NewManager()
	op, err := m.Start(validRequest(now), now)
	if err != nil {
		t.Fatal(err)
	}
	if op.Status != StatusPending || op.NextChunk != 0 || BuildView(op).ManifestDigest != "" {
		t.Fatalf("initial=%+v", op)
	}
	if _, err = m.Resume(op.ID, 1, now); !errors.Is(err, ErrConflict) {
		t.Fatalf("checkpoint=%v", err)
	}
	op, err = m.Resume(op.ID, 0, now)
	if err != nil {
		t.Fatal(err)
	}
	if op.Status != StatusComplete || op.Completed != 3 || op.NextChunk != 2 || !op.Manifest.VerifyDigest() {
		t.Fatalf("complete=%+v", op)
	}
	p := op.Artifact(validRequest(now).Records)
	if err := p.Verify(); err != nil {
		t.Fatal(err)
	}
	p.Manifest.Scope = "case:other"
	if !errors.Is(p.Verify(), ErrTampered) {
		t.Fatal("manifest tampering accepted")
	}
}
func TestADMIN007RejectsUnauthorizedFieldAndExpiredResume(t *testing.T) {
	now := time.Unix(1000, 0).UTC()
	r := validRequest(now)
	r.Records[0].Fields["email"] = "secret"
	if _, err := NewManager().Start(r, now); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("field=%v", err)
	}
	r = validRequest(now)
	r.Authorization.ExpiresAt = now.Add(time.Minute)
	m := NewManager()
	op, err := m.Start(r, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = m.Resume(op.ID, 0, now.Add(2*time.Minute)); !errors.Is(err, ErrExpired) {
		t.Fatalf("expiry=%v", err)
	}
}
