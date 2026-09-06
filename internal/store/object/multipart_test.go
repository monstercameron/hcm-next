package object

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func multipartGrant(tenant string, now time.Time) UploadGrant {
	return UploadGrant{TenantID: tenant, Principal: "principal:upload", Purpose: "onboarding", ExpiresAt: now.Add(time.Hour)}
}

func startMultipart(t *testing.T) (*MultipartManager, UploadGrant, MultipartUpload) {
	t.Helper()
	now := time.Unix(100, 0).UTC()
	grant := multipartGrant("tenant-a", now)
	m := NewMultipartManager(func() time.Time { return now })
	u, err := m.Start(context.Background(), MultipartRequest{TenantID: "tenant-a", ExpectedParts: 2, MaxBytes: 32, Grant: grant, IdempotencyKey: "request-1"})
	if err != nil {
		t.Fatal(err)
	}
	return m, grant, u
}

func TestTodo_ARTIFACT_002(t *testing.T) {
	m, grant, upload := startMultipart(t)
	first, err := m.PutPart(context.Background(), MultipartPartRequest{UploadID: upload.UploadID, TenantID: "tenant-a", Grant: grant, PartNumber: 1, Content: []byte("hello ")})
	if err != nil {
		t.Fatal(err)
	}
	if first.Checksum != MultipartChecksum([]byte("hello ")) {
		t.Fatalf("part checksum = %q", first.Checksum)
	}
	if _, _, err := m.Complete(context.Background(), MultipartCompleteRequest{UploadID: upload.UploadID, TenantID: "tenant-a", Grant: grant}); !errors.Is(err, ErrUploadIncomplete) {
		t.Fatalf("incomplete completion error = %v", err)
	}
	if _, err := m.PutPart(context.Background(), MultipartPartRequest{UploadID: upload.UploadID, TenantID: "tenant-a", Grant: grant, PartNumber: 2, Content: []byte("world")}); err != nil {
		t.Fatal(err)
	}
	got, content, err := m.Complete(context.Background(), MultipartCompleteRequest{UploadID: upload.UploadID, TenantID: "tenant-a", Grant: grant, ContentID: MultipartChecksum([]byte("hello world"))})
	if err != nil {
		t.Fatal(err)
	}
	if got.State != MultipartScanning || string(content) != "hello world" || got.Transitions[len(got.Transitions)-2] != MultipartReceived {
		t.Fatalf("completion = %+v content=%q", got, content)
	}
}

func TestTodo_ARTIFACT_002_Integration(t *testing.T) {
	m, grant, upload := startMultipart(t)
	content := []byte("same")
	part := MultipartPartRequest{UploadID: upload.UploadID, TenantID: "tenant-a", Grant: grant, PartNumber: 1, Content: content}
	a, err := m.PutPart(context.Background(), part)
	if err != nil {
		t.Fatal(err)
	}
	b, err := m.PutPart(context.Background(), part)
	if err != nil || a != b {
		t.Fatalf("idempotent replay = %#v, %#v, err=%v", a, b, err)
	}
	if _, err := m.PutPart(context.Background(), MultipartPartRequest{UploadID: upload.UploadID, TenantID: "tenant-a", Grant: grant, PartNumber: 1, Content: []byte("different")}); !errors.Is(err, ErrUploadConflict) {
		t.Fatalf("different replay error = %v", err)
	}
}

func TestTodo_ARTIFACT_002_Security(t *testing.T) {
	m, grant, upload := startMultipart(t)
	foreign := grant
	foreign.TenantID = "tenant-b"
	if _, err := m.PutPart(context.Background(), MultipartPartRequest{UploadID: upload.UploadID, TenantID: "tenant-b", Grant: foreign, PartNumber: 1, Content: []byte("x")}); !errors.Is(err, ErrUploadUnauthorized) {
		t.Fatalf("cross-tenant error = %v", err)
	}
	if _, err := m.PutPart(context.Background(), MultipartPartRequest{UploadID: upload.UploadID, TenantID: "tenant-a", Grant: grant, PartNumber: 1, Content: []byte("x"), Checksum: MultipartChecksum([]byte("y"))}); !errors.Is(err, ErrUploadChecksum) {
		t.Fatalf("checksum error = %v", err)
	}
	if _, err := m.PutPart(context.Background(), MultipartPartRequest{UploadID: upload.UploadID, TenantID: "tenant-a", Grant: grant, PartNumber: 1, Content: make([]byte, 33)}); !errors.Is(err, ErrUploadOversize) {
		t.Fatalf("oversize error = %v", err)
	}
}

func TestTodo_ARTIFACT_002_Recovery(t *testing.T) {
	m, grant, upload := startMultipart(t)
	if err := m.Abort(context.Background(), MultipartAbortRequest{UploadID: upload.UploadID, TenantID: "tenant-a", Grant: grant}); err != nil {
		t.Fatal(err)
	}
	if _, err := m.PutPart(context.Background(), MultipartPartRequest{UploadID: upload.UploadID, TenantID: "tenant-a", Grant: grant, PartNumber: 1, Content: []byte("late")}); !errors.Is(err, ErrUploadAborted) {
		t.Fatalf("late part error = %v", err)
	}
	if _, _, err := m.Complete(context.Background(), MultipartCompleteRequest{UploadID: upload.UploadID, TenantID: "tenant-a", Grant: grant}); !errors.Is(err, ErrUploadAborted) {
		t.Fatalf("late completion error = %v", err)
	}
	if err := m.Abort(context.Background(), MultipartAbortRequest{UploadID: upload.UploadID, TenantID: "tenant-a", Grant: grant}); err != nil {
		t.Fatalf("idempotent abort = %v", err)
	}
}

func TestTodo_ARTIFACT_002_Race(t *testing.T) {
	m, grant, upload := startMultipart(t)
	var wg sync.WaitGroup
	errs := make(chan error, 16)
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := m.PutPart(context.Background(), MultipartPartRequest{UploadID: upload.UploadID, TenantID: "tenant-a", Grant: grant, PartNumber: 1, Content: []byte("parallel")})
			if err != nil {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
}

func FuzzTodo_ARTIFACT_002(f *testing.F) {
	f.Add([]byte("part"))
	f.Fuzz(func(t *testing.T, content []byte) {
		if len(content) == 0 || len(content) > 32 {
			return
		}
		m, grant, upload := startMultipart(t)
		_, err := m.PutPart(context.Background(), MultipartPartRequest{UploadID: upload.UploadID, TenantID: "tenant-a", Grant: grant, PartNumber: 1, Content: content})
		if err != nil {
			t.Fatal(err)
		}
	})
}
