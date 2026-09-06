package pgstore

import (
	"errors"
	"strings"
	"testing"

	"github.com/monstercameron/hcm-next/internal/kernel/values"
	"github.com/monstercameron/hcm-next/internal/trust"
	"github.com/monstercameron/hcm-next/internal/trust/session"
)

func TestParseWireValues_ClosedVocabularies(t *testing.T) {
	for _, tc := range []struct {
		name string
		text string
		want trust.Assurance
	}{
		{"low", "low", trust.AssuranceLow},
		{"substantial", "substantial", trust.AssuranceSubstantial},
		{"high", "high", trust.AssuranceHigh},
	} {
		t.Run("assurance/"+tc.name, func(t *testing.T) {
			got, err := parseAssurance(tc.text)
			if err != nil || got != tc.want {
				t.Fatalf("parseAssurance(%q) = %v, %v", tc.text, got, err)
			}
		})
	}
	if got, err := parseAssurance("HIGH"); err == nil || got != trust.AssuranceUnspecified || !strings.Contains(err.Error(), "unknown assurance") {
		t.Fatalf("invalid assurance = %v, %v", got, err)
	}

	for _, tc := range []struct {
		text string
		want session.Status
	}{
		{"ACTIVE", session.StatusActive},
		{"EXPIRED_IDLE", session.StatusExpiredIdle},
		{"EXPIRED_ABSOLUTE", session.StatusExpiredAbsolute},
		{"REVOKED", session.StatusRevoked},
	} {
		got, err := parseStatus(tc.text)
		if err != nil || got != tc.want {
			t.Fatalf("parseStatus(%q) = %v, %v", tc.text, got, err)
		}
	}
	if got, err := parseStatus("ACTIVE "); err == nil || got != session.StatusUnspecified {
		t.Fatalf("invalid status = %v, %v", got, err)
	}

	for _, tc := range []struct {
		text string
		want session.EvidenceKind
	}{
		{"CREATED", session.EvidenceCreated},
		{"ROTATED", session.EvidenceRotated},
		{"DENIED", session.EvidenceDenied},
		{"REVOKED", session.EvidenceRevoked},
		{"EXPIRED", session.EvidenceExpired},
	} {
		got, err := parseEvidenceKind(tc.text)
		if err != nil || got != tc.want {
			t.Fatalf("parseEvidenceKind(%q) = %v, %v", tc.text, got, err)
		}
	}
	if got, err := parseEvidenceKind("UNKNOWN"); err == nil || got != session.EvidenceUnspecified || !strings.Contains(err.Error(), "unknown evidence kind") {
		t.Fatalf("invalid evidence kind = %v, %v", got, err)
	}
}

func TestValidateTenant_RequiresCanonicalUUID(t *testing.T) {
	valid := values.TenantId("11111111-1111-1111-1111-111111111111")
	if err := validateTenant(valid); err != nil {
		t.Fatalf("canonical tenant rejected: %v", err)
	}
	for _, tenant := range []values.TenantId{"", "tenant-acme", "111111111111111111111111111111111111", "11111111-1111-1111-1111-11111111111A"} {
		if err := validateTenant(tenant); err == nil {
			t.Errorf("validateTenant(%q) succeeded, want refusal", tenant)
		} else if tenant == "" && !errors.Is(err, values.ErrTenantRequired) {
			t.Errorf("validateTenant(empty) = %v, want ErrTenantRequired", err)
		}
	}
}

func TestNew_ReturnsStoreWithDatabaseCapability(t *testing.T) {
	store := New(nil)
	if store == nil || store.db != nil {
		t.Fatalf("New(nil) = %#v, want a store retaining the nil capability", store)
	}
}
