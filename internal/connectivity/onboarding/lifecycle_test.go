package onboarding

import (
	"context"
	"errors"
	"testing"
	"time"
)

func stagingFixture() (*StagingStore, StagingRecord, time.Time) {
	now := time.Unix(300, 0).UTC()
	store := NewStagingStore(func() time.Time { return now })
	record, err := store.Stage(context.Background(), StagingRequest{
		TenantID: "tenant-a", ArtifactID: "artifact-1", Payload: []byte("private worker row"), Purpose: "onboarding", RetentionUntil: now.Add(time.Hour), RetentionPolicy: "onboarding-7y",
		Lineage: LineageAnchor{ManifestDigest: lineageDigest("manifest"), SourceSnapshot: "snapshot-1", SourceRow: "row-1", SourceOffset: 4, TransformRef: "transform:v1"}, CreatedAt: now,
	})
	if err != nil {
		panic(err)
	}
	return store, record, now
}

func TestTodo_ONBOARD_008(t *testing.T) {
	store, record, now := stagingFixture()
	if _, _, err := store.Export(context.Background(), record.TenantID, record.ArtifactID, record.Revision, now); !errors.Is(err, ErrLifecycleDenied) {
		t.Fatalf("uninspected export error = %v", err)
	}
	reviewed, err := store.ReviewDLP(context.Background(), record.TenantID, record.ArtifactID, record.Revision, DLPReview{Status: DLPCleared, EvidenceID: "ev:dlp:1"})
	if err != nil {
		t.Fatal(err)
	}
	exported, receipt, err := store.Export(context.Background(), record.TenantID, record.ArtifactID, reviewed.Revision, now)
	if err != nil {
		t.Fatal(err)
	}
	if exported.State != Exported || receipt.PayloadDigest != record.PayloadDigest || receipt.LineageDigest != record.LineageDigest {
		t.Fatalf("exported=%+v receipt=%+v", exported, receipt)
	}
	destroyed, err := store.Destroy(context.Background(), DestroyRequest{TenantID: record.TenantID, ArtifactID: record.ArtifactID, Revision: exported.Revision, At: now.Add(time.Hour), TenantCleanup: false})
	if err != nil {
		t.Fatal(err)
	}
	if destroyed.State != Destroyed || len(destroyed.Payload) != 0 || destroyed.PayloadDigest == "" || destroyed.LineageDigest == "" || destroyed.DestructionDigest == "" || destroyed.Lineage.SourceRow != "row-1" {
		t.Fatalf("destroyed record lost verifiable lineage: %+v", destroyed)
	}
}

func TestTodo_ONBOARD_008_Security(t *testing.T) {
	store, record, now := stagingFixture()
	if _, err := store.Destroy(context.Background(), DestroyRequest{TenantID: record.TenantID, ArtifactID: record.ArtifactID, Revision: record.Revision, At: now.Add(time.Minute)}); !errors.Is(err, ErrLifecycleRetention) {
		t.Fatalf("early destroy error = %v", err)
	}
	if _, err := store.SetHold(context.Background(), record.TenantID, record.ArtifactID, record.Revision, true); err != nil {
		t.Fatal(err)
	}
	held, err := store.Get(context.Background(), record.TenantID, record.ArtifactID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Destroy(context.Background(), DestroyRequest{TenantID: record.TenantID, ArtifactID: record.ArtifactID, Revision: held.Revision, At: now.Add(2 * time.Hour), TenantCleanup: true}); !errors.Is(err, ErrLifecycleHeld) {
		t.Fatalf("held cleanup error = %v", err)
	}
	if _, err := store.ReviewDLP(context.Background(), "tenant-b", record.ArtifactID, record.Revision, DLPReview{Status: DLPCleared, EvidenceID: "ev:foreign"}); !errors.Is(err, ErrLifecycleNotFound) {
		t.Fatalf("cross-tenant review error = %v", err)
	}
	if _, _, err := store.Export(context.Background(), record.TenantID, record.ArtifactID, record.Revision, now); !errors.Is(err, ErrLifecycleConflict) {
		t.Fatalf("stale export error = %v", err)
	}

	refusedStore, refused, _ := stagingFixture()
	refused, err = refusedStore.ReviewDLP(context.Background(), refused.TenantID, refused.ArtifactID, refused.Revision, DLPReview{Status: DLPRefused, EvidenceID: "ev:dlp:refused"})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := refusedStore.Export(context.Background(), refused.TenantID, refused.ArtifactID, refused.Revision, now); !errors.Is(err, ErrLifecycleDenied) {
		t.Fatalf("refused export error = %v", err)
	}
}
