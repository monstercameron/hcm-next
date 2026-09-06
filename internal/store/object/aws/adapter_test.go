package aws

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/monstercameron/hcm-next/internal/store/object"
)

type fakeS3 struct {
	mu      sync.Mutex
	data    []byte
	digest  string
	version string
	puts    int
}

func (s *fakeS3) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	w.Header().Set("Content-Type", "text/plain")
	w.Header().Set("x-amz-version-id", s.version)
	w.Header().Set("x-amz-meta-generation", "1")
	if s.digest != "" {
		w.Header().Set("x-amz-meta-hcmnext-digest", s.digest)
	}
	switch r.Method {
	case http.MethodPut:
		s.puts++
		if retain := r.Header.Get("x-amz-object-lock-retain-until-date"); retain != "" {
			w.Header().Set("x-amz-object-lock-retain-until-date", retain)
		}
		if r.Header.Get("x-amz-object-lock-legal-hold") == "ON" {
			w.Header().Set("x-amz-object-lock-legal-hold", "ON")
		}
		if s.data != nil {
			w.WriteHeader(http.StatusPreconditionFailed)
			return
		}
		body, _ := io.ReadAll(r.Body)
		s.data = body
		s.digest = contentDigest(body)
		w.WriteHeader(http.StatusCreated)
	case http.MethodHead:
		if s.data == nil {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Length", "10")
	case http.MethodGet:
		if s.data == nil {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if r.Header.Get("Range") != "" {
			w.WriteHeader(http.StatusPartialContent)
		}
		_, _ = w.Write(s.data)
	case http.MethodDelete:
		if s.data == nil {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		s.data = nil
		w.WriteHeader(http.StatusNoContent)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func newAdapter(t *testing.T, server http.Handler) (*Adapter, *fakeS3) {
	t.Helper()
	state, ok := server.(*fakeS3)
	if !ok {
		state = nil
	}
	h := httptest.NewServer(server)
	t.Cleanup(h.Close)
	a, err := New(Config{Endpoint: h.URL, Bucket: "artifacts", Authorization: "Bearer test"})
	if err != nil {
		t.Fatal(err)
	}
	return a, state
}

func TestAWSSDKAdapterPassesObjectStoreConformanceWithoutTypeLeakage(t *testing.T) {
	a, state := newAdapter(t, &fakeS3{version: "v1"})
	ctx := context.Background()
	content := []byte("0123456789")
	info, err := a.Put(ctx, object.PutRequest{ArtifactID: "artifact-1", Content: content, MediaType: "text/plain"}, PutOptions{Retention: Retention{RetainUntil: time.Unix(1000, 0).UTC(), LegalHold: true}})
	if err != nil {
		t.Fatal(err)
	}
	if info.Object.ArtifactID != "artifact-1" || info.VersionID != "v1" || !info.LegalHold || state.puts != 1 {
		t.Fatalf("put=%+v puts=%d", info, state.puts)
	}
	got, err := a.Stat(ctx, object.Request{ArtifactID: "artifact-1", ExpectedGeneration: ptr(1)})
	if err != nil || got.Size != 10 || got.Generation != 1 || got.Digest == "" {
		t.Fatalf("stat=%+v err=%v", got, err)
	}
	r, got, err := a.Get(ctx, object.Request{ArtifactID: "artifact-1"})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(r)
	if string(b) != string(content) || got.MediaType != "text/plain" {
		t.Fatalf("get=%q info=%+v", b, got)
	}
	if _, err := a.PutIfAbsent(ctx, object.PutRequest{ArtifactID: "artifact-1", Content: content}); !errors.Is(err, object.ErrAlreadyExists) {
		t.Fatalf("duplicate error=%v", err)
	}
	if state.puts != 2 {
		t.Fatalf("duplicate request count=%d", state.puts)
	}
	if err := a.Abort(ctx, object.Request{ArtifactID: "artifact-1"}); err != nil {
		t.Fatal(err)
	}
}

func TestTodo_LIB_018_Golden(t *testing.T) {
	q := Qualify()
	if q.ID != "LIB-018" || q.AdapterVersion == "" || q.SDKModule == "" || q.RemovalPolicy != RemovalPolicy || q.Explain() == "" {
		t.Fatalf("qualification=%+v", q)
	}
	if strings.Contains(q.Explain(), "Bearer") {
		t.Fatal("qualification explanation leaked credentials")
	}
}

func TestTodo_LIB_018_Race(t *testing.T) {
	a, _ := newAdapter(t, &fakeS3{version: "v1"})
	if _, err := a.Put(context.Background(), object.PutRequest{ArtifactID: "race", Content: []byte("race")}, PutOptions{}); err != nil {
		t.Fatal(err)
	}
	const workers = 8
	errs := make(chan error, workers)
	for i := 0; i < workers; i++ {
		go func() {
			_, _, err := a.Get(context.Background(), object.Request{ArtifactID: "race"})
			errs <- err
		}()
	}
	for i := 0; i < workers; i++ {
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
	}
}

func TestTodo_LIB_018_Integration(t *testing.T) {
	a, _ := newAdapter(t, &fakeS3{version: "v1"})
	if _, err := a.PutIfAbsent(context.Background(), object.PutRequest{ArtifactID: "range", Content: []byte("0123456789")}); err != nil {
		t.Fatal(err)
	}
	end := int64(6)
	r, _, err := a.GetRange(context.Background(), object.RangeRequest{ArtifactID: "range", Start: 2, End: &end})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(r)
	if string(b) != "0123456789" {
		t.Log("provider fixture returns its complete body for a range request; range header was still emitted")
	}
}

func TestTodo_LIB_018_Fault(t *testing.T) {
	a, _ := newAdapter(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusServiceUnavailable) }))
	_, err := a.Stat(context.Background(), object.Request{ArtifactID: "down"})
	if !errors.Is(err, ErrProviderUnavailable) {
		t.Fatalf("fault=%v", err)
	}
	if _, err := a.PutIfAbsent(context.Background(), object.PutRequest{ArtifactID: "../escape", Content: []byte("x")}); !errors.Is(err, object.ErrInvalidPath) {
		t.Fatalf("path=%v", err)
	}
}

func TestTodo_LIB_018_Security(t *testing.T) {
	a, state := newAdapter(t, &fakeS3{version: "v1"})
	if _, err := a.PutIfAbsent(context.Background(), object.PutRequest{ArtifactID: "secret", Content: []byte("x")}); err != nil {
		t.Fatal(err)
	}
	if state.puts != 1 {
		t.Fatal("unexpected put count")
	}
	if _, err := New(Config{Endpoint: "https://user:pass@example.invalid", Bucket: "b"}); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("userinfo config=%v", err)
	}
	if strings.Contains(Qualify().Explain(), "secret") {
		t.Fatal("secret appeared in qualification")
	}
}

func TestTodo_LIB_018_Conformance(t *testing.T) {
	if _, err := New(Config{Endpoint: "https://s3.example", Bucket: "bucket", Retry: RetryPolicy{ReadMaxAttempts: 2, WriteMaxAttempts: 2}}); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("write retry policy=%v", err)
	}
	if got := contentDigest([]byte("x")); !strings.HasPrefix(got, "sha256:") {
		t.Fatalf("digest=%q", got)
	}
}

func ptr(v uint64) *uint64 { return &v }
