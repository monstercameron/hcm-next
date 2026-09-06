package legal

import (
	"context"
	"testing"
)

func TestEvidencePortMemoryStoreRejectsDuplicateAndKeepsTenantsSeparate(t *testing.T) {
	store := NewMemoryEvidenceStore()
	record := ReviewRecord{PackDigest: "pack", AuthorID: "author", ReviewerID: "reviewer", Status: ReviewStatusVendorBaseline}
	entry := ReviewRecordEntry{TenantID: "tenant-a", RowID: "row-a", EventSequence: 1, Record: record, Digest: record.ComputeDigest()}
	if _, err := store.AppendReviewRecord(context.Background(), entry); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AppendReviewRecord(context.Background(), entry); err == nil {
		t.Fatal("duplicate review was accepted")
	}
	other, err := store.ListReviewRecords(context.Background(), "tenant-b", record.PackDigest)
	if err != nil {
		t.Fatal(err)
	}
	if len(other) != 0 {
		t.Fatalf("cross-tenant review count = %d, want 0", len(other))
	}
}
