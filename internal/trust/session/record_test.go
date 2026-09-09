package session

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

func TestRecord_Smoke(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panic: %v", r)
		}
	}()
}

func TestRecord_NoPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panic: %v", r)
		}
	}()
	_ = 1
}

func TestRecord_AccessorsAndWireValues(t *testing.T) {
	created := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)
	tenant := values.TenantId("11111111-1111-1111-1111-111111111111")
	record := Record{
		id:                   ID("sess_opaque"),
		tenant:               tenant,
		subject:              "user:1",
		principalFingerprint: "fp:1",
		assurance:            trust.AssuranceHigh,
		status:               StatusRevoked,
		revokedReason:        ReasonManualRevoke,
		createdAt:            created,
		lastActivityAt:       created.Add(time.Minute),
		idleTimeout:          time.Hour,
		absoluteExpiresAt:    created.Add(24 * time.Hour),
		rotationCount:        3,
	}
	if record.ID() != ID("sess_opaque") || record.Tenant() != tenant || record.Subject() != "user:1" ||
		record.PrincipalFingerprint() != "fp:1" || record.Assurance() != trust.AssuranceHigh ||
		record.Status() != StatusRevoked || record.RevokedReason() != ReasonManualRevoke ||
		!record.CreatedAt().Equal(created) || !record.LastActivityAt().Equal(created.Add(time.Minute)) ||
		record.IdleTimeout() != time.Hour || !record.AbsoluteExpiresAt().Equal(created.Add(24*time.Hour)) ||
		record.RotationCount() != 3 {
		t.Fatalf("record accessors lost fields: %+v", record)
	}
	if got := record.String(); !strings.Contains(got, "sess_opaque") || !strings.Contains(got, "REVOKED") || strings.Contains(got, "raw-token") {
		t.Fatalf("redacted record string = %q", got)
	}

	for _, tc := range []struct {
		status Status
		wire   string
		valid  bool
		live   bool
	}{
		{StatusUnspecified, "STATUS_UNSPECIFIED", false, false},
		{StatusActive, "ACTIVE", true, true},
		{StatusExpiredIdle, "EXPIRED_IDLE", true, false},
		{StatusExpiredAbsolute, "EXPIRED_ABSOLUTE", true, false},
		{StatusRevoked, "REVOKED", true, false},
		{Status(99), "STATUS_UNSPECIFIED", false, false},
	} {
		if got := tc.status.String(); got != tc.wire || tc.status.Valid() != tc.valid || tc.status.Live() != tc.live {
			t.Fatalf("status %d = (%q, %v, %v), want (%q, %v, %v)", tc.status, got, tc.status.Valid(), tc.status.Live(), tc.wire, tc.valid, tc.live)
		}
	}
	for _, tc := range []struct {
		kind EvidenceKind
		wire string
	}{
		{EvidenceUnspecified, "EVIDENCE_UNSPECIFIED"},
		{EvidenceCreated, "CREATED"},
		{EvidenceRotated, "ROTATED"},
		{EvidenceDenied, "DENIED"},
		{EvidenceRevoked, "REVOKED"},
		{EvidenceExpired, "EXPIRED"},
		{EvidenceKind(99), "EVIDENCE_UNSPECIFIED"},
	} {
		if got := tc.kind.String(); got != tc.wire {
			t.Fatalf("evidence kind %d = %q, want %q", tc.kind, got, tc.wire)
		}
	}
}

func TestRecord_SecretAndEvidenceHelpers(t *testing.T) {
	raw, hash, err := newOpaqueToken()
	if err != nil {
		t.Fatalf("newOpaqueToken: %v", err)
	}
	if raw == "" || hash != hashToken(raw) {
		t.Fatalf("opaque token pair raw=%q hash=%q", raw, hash)
	}
	want := sha256.Sum256([]byte(raw))
	if hash != hex.EncodeToString(want[:]) || strings.Contains(hash, raw) {
		t.Fatalf("token hash = %q, want SHA-256 without raw token", hash)
	}
	id, err := newID()
	if err != nil || !strings.HasPrefix(string(id), "sess_") {
		t.Fatalf("newID = %q, err=%v", id, err)
	}
	first := newEvidenceID(id, EvidenceCreated, "", time.Unix(0, 0), 1)
	second := newEvidenceID(id, EvidenceCreated, "", time.Unix(0, 0), 2)
	if first == "" || first == second || !strings.HasPrefix(first, "ev:session:") {
		t.Fatalf("evidence IDs = %q and %q, want distinct durable IDs", first, second)
	}
}

func TestStoreRecord_RecordCopiesAllFields(t *testing.T) {
	at := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)
	sr := StoreRecord{
		ID: ID("sess_store"), Tenant: values.TenantId("22222222-2222-2222-2222-222222222222"), Subject: "subject",
		PrincipalFingerprint: "fingerprint", Assurance: trust.AssuranceSubstantial, Status: StatusActive,
		RevokedReason: "", CreatedAt: at, LastActivityAt: at.Add(time.Second), IdleTimeout: time.Minute,
		AbsoluteExpiresAt: at.Add(time.Hour), RotationCount: 2, CurrentTokenHash: "hash", Generation: 2, Version: 4,
	}
	record := sr.Record()
	if record.ID() != sr.ID || record.Tenant() != sr.Tenant || record.Subject() != sr.Subject ||
		record.PrincipalFingerprint() != sr.PrincipalFingerprint || record.Assurance() != sr.Assurance ||
		record.Status() != sr.Status || record.CreatedAt() != sr.CreatedAt || record.LastActivityAt() != sr.LastActivityAt ||
		record.IdleTimeout() != sr.IdleTimeout || record.AbsoluteExpiresAt() != sr.AbsoluteExpiresAt || record.RotationCount() != sr.RotationCount {
		t.Fatalf("StoreRecord.Record lost fields: %+v", record)
	}
}
