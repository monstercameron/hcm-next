package diagnostics

import (
	"errors"
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
