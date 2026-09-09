package quarantine_test

import (
	"context"
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/asset/quarantine"
)

// compile-time proof that fakeStore satisfies quarantine.Store.
var _ quarantine.Store = (*fakeStore)(nil)

// TestFakeStoreCurrentStateIsMostRecentVerdict documents and proves the port
// contract every [quarantine.Store] implementation (including the
// PostgreSQL-backed one in internal/data/artifacts) must uphold: recording a
// verdict after the initial quarantined fact makes that verdict, not the
// original quarantined fact, the current state.
func TestFakeStoreCurrentStateIsMostRecentVerdict(t *testing.T) {
	ctx := context.Background()
	s := newFakeStore()
	const tenant, contentID = "tenant-a", "abc123"

	if err := s.RecordQuarantined(ctx, tenant, quarantine.QuarantinedRecord{
		ContentID:           contentID,
		DigestAlgorithm:     quarantine.Algorithm,
		ByteSize:            3,
		DeclaredContentType: quarantine.ContentTXT,
		SniffedCategory:     quarantine.CategoryText,
		CreatorPrincipalRef: "user:1",
		EvidenceID:          "ev:test:1",
		Content:             []byte("abc"),
	}); err != nil {
		t.Fatalf("record quarantined: %v", err)
	}

	state, err := s.CurrentState(ctx, tenant, contentID)
	if err != nil {
		t.Fatalf("current state after quarantine: %v", err)
	}
	if state.State != quarantine.Quarantined {
		t.Fatalf("state = %s, want %s", state.State, quarantine.Quarantined)
	}

	if err := s.RecordVerdict(ctx, tenant, quarantine.VerdictRecord{
		ContentID:      contentID,
		State:          quarantine.Admitted,
		ScannerID:      "fake",
		ScannerVersion: "1.0",
		EvidenceID:     "ev:test:2",
	}); err != nil {
		t.Fatalf("record verdict: %v", err)
	}

	state, err = s.CurrentState(ctx, tenant, contentID)
	if err != nil {
		t.Fatalf("current state after verdict: %v", err)
	}
	if state.State != quarantine.Admitted {
		t.Fatalf("state = %s, want %s", state.State, quarantine.Admitted)
	}
	if state.EvidenceID != "ev:test:2" {
		t.Fatalf("evidence id = %s, want the verdict's own evidence id", state.EvidenceID)
	}
}

func TestFakeStoreCurrentStateOfUnknownContentIDIsNotFound(t *testing.T) {
	s := newFakeStore()
	_, err := s.CurrentState(context.Background(), "tenant-a", "never-uploaded")
	var notFound quarantine.ErrNotFound
	if !errors.As(err, &notFound) {
		t.Fatalf("error = %v, want ErrNotFound", err)
	}
}

func TestFakeStoreRecordVerdictWithoutQuarantineIsRefused(t *testing.T) {
	s := newFakeStore()
	err := s.RecordVerdict(context.Background(), "tenant-a", quarantine.VerdictRecord{
		ContentID: "never-quarantined",
		State:     quarantine.Admitted,
	})
	var notFound quarantine.ErrNotFound
	if !errors.As(err, &notFound) {
		t.Fatalf("error = %v, want ErrNotFound: a verdict cannot be recorded for content that was never quarantined", err)
	}
}

func TestFakeStoreRecordQuarantinedIsIdempotent(t *testing.T) {
	ctx := context.Background()
	s := newFakeStore()
	const tenant, contentID = "tenant-a", "abc123"
	rec := quarantine.QuarantinedRecord{
		ContentID:           contentID,
		ByteSize:            3,
		DeclaredContentType: quarantine.ContentTXT,
		EvidenceID:          "ev:1",
		Content:             []byte("abc"),
	}
	if err := s.RecordQuarantined(ctx, tenant, rec); err != nil {
		t.Fatalf("first record: %v", err)
	}
	if err := s.RecordQuarantined(ctx, tenant, rec); err != nil {
		t.Fatalf("replayed record: %v", err)
	}
	if got := s.verdictCount(tenant, contentID); got != 1 {
		t.Fatalf("verdict log has %d rows after an idempotent replay, want exactly 1", got)
	}
}
