package quarantine_test

import (
	"context"
	"errors"
	"testing"

	"github.com/monstercameron/hcm-next/internal/domains/asset/quarantine"
)

func TestUseGateRefusesRejectedContent(t *testing.T) {
	ctx := context.Background()
	store := newFakeStore()
	contentID := quarantine.ComputeDigest(txtFixture)
	tenant := "tenant-a"
	mustQuarantine(t, ctx, store, tenant, contentID, txtFixture)
	if err := store.RecordVerdict(ctx, tenant, quarantine.VerdictRecord{
		ContentID:      contentID,
		State:          quarantine.Rejected,
		ScannerID:      "fake",
		ScannerVersion: "1.0",
		Reason:         "unsafe content",
		EvidenceID:     "ev:rejected",
	}); err != nil {
		t.Fatalf("record verdict: %v", err)
	}

	_, err := quarantine.Use(ctx, store, tenant, contentID)
	var refused quarantine.ErrUseRefused
	if !errors.As(err, &refused) {
		t.Fatalf("error = %v, want ErrUseRefused", err)
	}
	if refused.State != quarantine.Rejected {
		t.Fatalf("refused.State = %s, want %s", refused.State, quarantine.Rejected)
	}
	if refused.Reason != "unsafe content" {
		t.Fatalf("refused.Reason = %q, want the recorded rejection reason", refused.Reason)
	}
}

func TestUseGateAdmitsOnlyAdmittedState(t *testing.T) {
	ctx := context.Background()
	store := newFakeStore()
	contentID := quarantine.ComputeDigest(txtFixture)
	tenant := "tenant-a"
	mustQuarantine(t, ctx, store, tenant, contentID, txtFixture)
	if err := store.RecordVerdict(ctx, tenant, quarantine.VerdictRecord{
		ContentID:      contentID,
		State:          quarantine.Admitted,
		ScannerID:      "fake",
		ScannerVersion: "1.0",
		EvidenceID:     "ev:admitted",
	}); err != nil {
		t.Fatalf("record verdict: %v", err)
	}

	state, err := quarantine.Use(ctx, store, tenant, contentID)
	if err != nil {
		t.Fatalf("use: %v", err)
	}
	if state.ScannerID != "fake" || state.ScannerVersion != "1.0" {
		t.Fatalf("use gate did not echo the admitting scanner identity: %+v", state)
	}
}

func mustQuarantine(t *testing.T, ctx context.Context, store quarantine.Store, tenant, contentID string, content []byte) {
	t.Helper()
	if err := store.RecordQuarantined(ctx, tenant, quarantine.QuarantinedRecord{
		ContentID:           contentID,
		DigestAlgorithm:     quarantine.Algorithm,
		ByteSize:            int64(len(content)),
		DeclaredContentType: quarantine.ContentTXT,
		SniffedCategory:     quarantine.CategoryText,
		CreatorPrincipalRef: "user:uploader",
		EvidenceID:          "ev:quarantine",
		Content:             content,
	}); err != nil {
		t.Fatalf("record quarantined: %v", err)
	}
}
