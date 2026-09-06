package diagnostics

import (
	"errors"
	"sync"
	"testing"
	"time"
)

func TestDiagnosticSurfaceRequiresJITScopeRedactionBoundsAndAutomaticExpiry(t *testing.T) {
	if _, err := NewManager(DefaultManifest()).Authorize(Request{}); !errors.Is(err, ErrDisabled) {
		t.Fatalf("default manifest must deny diagnostics: %v", err)
	}
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	m := NewManagerWithClock(Manifest{Enabled: true, Surfaces: map[Surface]bool{SurfaceMetrics: true}, Budget: Budget{MaxDuration: time.Minute, MaxBytes: 64, MaxCPU: time.Second}}, func() time.Time { return now })
	if _, err := m.Authorize(Request{Workload: Workload{ID: "w", Authenticated: true}, Tenant: "t", Purpose: "incident", Surface: SurfaceDebug, Duration: time.Second, Budget: Budget{MaxBytes: 8, MaxCPU: time.Second}}); !errors.Is(err, ErrSurfaceDenied) {
		t.Fatalf("disabled surface must deny: %v", err)
	}
	s, err := m.Authorize(Request{Workload: Workload{ID: "w", Authenticated: true}, Tenant: "t", Purpose: "incident", Surface: SurfaceMetrics, Duration: time.Second, Budget: Budget{MaxBytes: 64, MaxCPU: time.Second}})
	if err != nil {
		t.Fatal(err)
	}
	a, err := s.Capture([]byte(`password=secret query=SELECT name FROM users`))
	if err != nil {
		t.Fatal(err)
	}
	if !a.Redacted || a.Classification != ClassificationRestricted || string(a.Bytes) == "" || string(a.Bytes) == string([]byte(`password=secret query=SELECT name FROM users`)) {
		t.Fatalf("capture was not protected: %+v", a)
	}
	now = now.Add(2 * time.Second)
	if _, err := s.Capture([]byte("late")); !errors.Is(err, ErrExpired) {
		t.Fatalf("expected expiry, got %v", err)
	}
	events := m.Events()
	if len(events) < 3 || events[len(events)-1].Kind != EventDrop {
		t.Fatalf("missing access/drop evidence: %+v", events)
	}
}

func enabledManager(now *time.Time) *Manager {
	return NewManagerWithClock(Manifest{Enabled: true, Surfaces: map[Surface]bool{SurfaceMetrics: true}, Budget: Budget{MaxDuration: time.Minute, MaxBytes: 16, MaxCPU: 2 * time.Second}}, func() time.Time { return *now })
}

func enabledRequest() Request {
	return Request{Workload: Workload{ID: "workload-1", Authenticated: true}, Tenant: "tenant-1", Purpose: "incident-1", Surface: SurfaceMetrics, Duration: time.Minute, Budget: Budget{MaxBytes: 16, MaxCPU: time.Second}}
}

func TestTodo_DIAG_001_Property(t *testing.T) {
	now := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)
	s, err := enabledManager(&now).Authorize(enabledRequest())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CaptureWithCPU([]byte("1234567890123456"), time.Second); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CaptureWithCPU([]byte("x"), time.Nanosecond); !errors.Is(err, ErrCaptureBounds) {
		t.Fatalf("expected byte budget fence, got %v", err)
	}
}

func TestTodo_DIAG_001_Golden(t *testing.T) {
	a, err := func() (Artifact, error) {
		now := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)
		s, openErr := enabledManager(&now).Authorize(enabledRequest())
		if openErr != nil {
			return Artifact{}, openErr
		}
		return s.Capture([]byte("password=secret"))
	}()
	if err != nil {
		t.Fatal(err)
	}
	if got := ExplainArtifact(a); got != "artifact="+a.ID+" tenant=tenant-1 surface=metrics class=RESTRICTED redacted=true bytes=19" {
		t.Fatalf("artifact explanation=%q", got)
	}
}

func FuzzTodo_DIAG_001(f *testing.F) {
	f.Add([]byte("password=secret"))
	f.Add([]byte("ordinary diagnostic value"))
	f.Fuzz(func(t *testing.T, input []byte) {
		out, class := Redact(input)
		if len(input) > 0 && class == ClassificationPublic {
			t.Fatal("non-empty diagnostic output was public")
		}
		if len(out) == 0 && len(input) > 0 {
			t.Fatal("non-empty input disappeared")
		}
	})
}

func TestTodo_DIAG_001_Race(t *testing.T) {
	now := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)
	m := enabledManager(&now)
	r := enabledRequest()
	s, err := m.Authorize(r)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = s.Capture([]byte("safe"))
		}()
	}
	// The session lock protects the operation; the deterministic budget makes
	// the result independent of goroutine scheduling.
	wg.Wait()
	now = now.Add(2 * time.Minute)
	if _, err := s.Capture([]byte("late")); !errors.Is(err, ErrExpired) {
		t.Fatalf("expiry=%v", err)
	}
}

func TestTodo_DIAG_001_Integration(t *testing.T) {
	now := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)
	m := enabledManager(&now)
	manifest := m.Manifest()
	manifest.Surfaces[SurfaceDebug] = true
	if m.Manifest().Surfaces[SurfaceDebug] {
		t.Fatal("manifest leaked mutable surface map")
	}
}

func TestTodo_DIAG_001_Fault(t *testing.T) {
	now := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)
	m := enabledManager(&now)
	r := enabledRequest()
	r.Budget.MaxCPU = 2 * time.Second
	s, err := m.Authorize(r)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CaptureWithCPU([]byte("x"), 3*time.Second); !errors.Is(err, ErrCaptureBounds) {
		t.Fatalf("cpu fence=%v", err)
	}
	if got := m.Events(); len(got) == 0 || got[len(got)-1].Reason != "cpu_budget" {
		t.Fatalf("missing cpu drop evidence: %+v", got)
	}
}

func TestTodo_DIAG_001_Security(t *testing.T) {
	now := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)
	m := enabledManager(&now)
	r := enabledRequest()
	r.Workload.Authenticated = false
	if _, err := m.Authorize(r); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("unauthenticated workload=%v", err)
	}
	s, err := m.Authorize(enabledRequest())
	if err != nil {
		t.Fatal(err)
	}
	a, err := s.Capture([]byte("SELECT secret"))
	if err != nil || string(a.Bytes) != "[REDACTED]" {
		t.Fatalf("sql artifact=%q err=%v", a.Bytes, err)
	}
}

func TestTodo_DIAG_001_Conformance(t *testing.T) {
	if Version() != 1 || Explain() == "" || len(Surfaces()) != 8 {
		t.Fatalf("contract=%d %q surfaces=%v", Version(), Explain(), Surfaces())
	}
	if got := DefaultManifest(); got.Enabled || len(got.Surfaces) != 0 {
		t.Fatalf("default manifest=%+v", got)
	}
}

func TestTodo_DIAG_001_Recovery(t *testing.T) {
	now := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)
	m := enabledManager(&now)
	s, err := m.Authorize(enabledRequest())
	if err != nil {
		t.Fatal(err)
	}
	s.Close()
	s.Close()
	if _, err := s.Capture([]byte("after close")); !errors.Is(err, ErrClosed) {
		t.Fatalf("closed session=%v", err)
	}
	if got := m.Events(); len(got) != 2 || got[1].Kind != EventDrop {
		t.Fatalf("close evidence=%+v", got)
	}
}

func BenchmarkTodo_DIAG_001(b *testing.B) {
	input := []byte("password=secret trace-id=opaque")
	for i := 0; i < b.N; i++ {
		_, _ = Redact(input)
	}
}

func TestTodo_DIAG_001_Mutation(t *testing.T) {
	now := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)
	m := enabledManager(&now)
	r := enabledRequest()
	got, err := m.Authorize(r)
	if err != nil {
		t.Fatal(err)
	}
	r.Tenant = "other-tenant"
	if _, err := got.Capture([]byte("value")); err != nil {
		t.Fatal(err)
	}
	if events := m.Events(); len(events) < 2 || events[1].Tenant != "tenant-1" {
		t.Fatalf("request mutation changed session evidence=%+v", events)
	}
}
