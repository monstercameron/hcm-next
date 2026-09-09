package artifacts

import (
	"context"
	"errors"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/intent/model"
)

func downloadFixture(t *testing.T) (*DownloadCatalog, DownloadGrant, string, time.Time) {
	t.Helper()
	now := time.Unix(200, 0).UTC()
	content := []byte("downloadable artifact")
	id := MultipartDigest(content)
	catalog := NewDownloadCatalog()
	if err := catalog.Register(DownloadRecord{TenantID: "tenant-a", ContentID: id, Revision: 1, Bytes: content, Classification: model.ClassInternal, State: DownloadAvailable, AllowedPurposes: []string{"onboarding"}}); err != nil {
		t.Fatal(err)
	}
	grant := DownloadGrant{TenantID: "tenant-a", Principal: "principal:reader", Purpose: "onboarding", ContentID: id, Revision: 1, AllowedClassifications: []model.ClassificationLabel{model.ClassInternal}, ExpiresAt: now.Add(time.Hour)}
	return catalog, grant, id, now
}

func TestTodo_ARTIFACT_003(t *testing.T) {
	catalog, grant, _, now := downloadFixture(t)
	reader, receipt, err := catalog.Open(context.Background(), DownloadRequest{Grant: grant}, now)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	got, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "downloadable artifact" || receipt.ByteCount != int64(len(got)) || receipt.RangeDigest != MultipartDigest(got) || receipt.Digest == "" {
		t.Fatalf("bytes=%q receipt=%+v", got, receipt)
	}
}

func TestTodo_ARTIFACT_003_Golden(t *testing.T) {
	catalog, grant, _, now := downloadFixture(t)
	end := int64(12)
	reader, receipt, err := catalog.Open(context.Background(), DownloadRequest{Grant: grant, Start: 0, End: &end}, now)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	got, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "downloadable" || receipt.RangeStart != 0 || receipt.RangeEnd != 12 || receipt.RangeDigest != MultipartDigest(got) {
		t.Fatalf("range=%q receipt=%+v", got, receipt)
	}
}

func TestTodo_ARTIFACT_003_Integration(t *testing.T) {
	catalog, grant, id, now := downloadFixture(t)
	if err := catalog.SetState("tenant-a", id, 1, DownloadQuarantined, false); err != nil {
		t.Fatal(err)
	}
	grant.Revision = 1
	if _, _, err := catalog.Open(context.Background(), DownloadRequest{Grant: grant}, now); !errors.Is(err, ErrDownloadDenied) {
		t.Fatalf("quarantined error = %v", err)
	}
	if err := catalog.SetState("tenant-a", id, 2, DownloadAvailable, false); err != nil {
		t.Fatal(err)
	}
	grant.Revision = 3 // SetState advances the revision fence.
	reader, _, err := catalog.Open(context.Background(), DownloadRequest{Grant: grant}, now)
	if err != nil {
		t.Fatal(err)
	}
	reader.Close()
}

func TestTodo_ARTIFACT_003_Security(t *testing.T) {
	catalog, grant, id, now := downloadFixture(t)
	stale := grant
	stale.ExpiresAt = now
	if _, _, err := catalog.Open(context.Background(), DownloadRequest{Grant: stale}, now); !errors.Is(err, ErrDownloadDenied) {
		t.Fatalf("stale grant error = %v", err)
	}
	foreign := grant
	foreign.TenantID = "tenant-b"
	if _, _, err := catalog.Open(context.Background(), DownloadRequest{Grant: foreign}, now); !errors.Is(err, ErrDownloadNotFound) {
		t.Fatalf("cross-tenant error = %v", err)
	}
	wrongPurpose := grant
	wrongPurpose.Purpose = "payroll"
	if _, _, err := catalog.Open(context.Background(), DownloadRequest{Grant: wrongPurpose}, now); !errors.Is(err, ErrDownloadDenied) {
		t.Fatalf("wrong-purpose error = %v", err)
	}
	if err := catalog.SetState("tenant-a", id, 1, DownloadAvailable, true); err != nil {
		t.Fatal(err)
	}
	if _, _, err := catalog.Open(context.Background(), DownloadRequest{Grant: grant}, now); !errors.Is(err, ErrDownloadDenied) {
		t.Fatalf("held error = %v", err)
	}
}

func TestTodo_ARTIFACT_003_Race(t *testing.T) {
	catalog, grant, _, now := downloadFixture(t)
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			reader, _, err := catalog.Open(context.Background(), DownloadRequest{Grant: grant}, now)
			if err != nil {
				t.Errorf("open: %v", err)
				return
			}
			_, _ = io.Copy(io.Discard, reader)
			_ = reader.Close()
		}()
	}
	wg.Wait()
}

func FuzzTodo_ARTIFACT_003(f *testing.F) {
	f.Add(int64(0), int64(2))
	f.Fuzz(func(t *testing.T, start, end int64) {
		catalog, grant, _, now := downloadFixture(t)
		if start < 0 || end < start || end > 21 {
			return
		}
		reader, receipt, err := catalog.Open(context.Background(), DownloadRequest{Grant: grant, Start: start, End: &end}, now)
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(reader)
		reader.Close()
		if err != nil || int64(len(data)) != end-start || receipt.RangeDigest != MultipartDigest(data) {
			t.Fatalf("data=%q receipt=%+v err=%v", data, receipt, err)
		}
	})
}
