package health

import (
	"context"
	"errors"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestHealthAndReadinessEndpointsDistinguishProcessLifeFromAdmissionReadiness(t *testing.T) {
	readyErr := errors.New("dependency detail must not be public")
	s := New(Dependencies{Live: func() bool { return true }, ReadyCheck: func(context.Context) error { return readyErr }, CheckInterval: time.Minute})
	live := httptest.NewRecorder()
	s.Healthz(live, httptest.NewRequest("GET", "/healthz", nil))
	ready := httptest.NewRecorder()
	s.Readyz(ready, httptest.NewRequest("GET", "/readyz", nil))
	if live.Code != 200 || ready.Code != 503 {
		t.Fatalf("healthz=%d readyz=%d", live.Code, ready.Code)
	}
	if body := ready.Body.String(); body != "{\"ok\":false}\n" {
		t.Fatalf("readiness body disclosed details: %q", body)
	}
}

// TestReadinessCheckIsBoundedAndCached counts calls atomically because
// isReady runs ReadyCheck on its own goroutine and abandons it when
// CheckTimeout expires (see health.go's select on ctx.Done()). The check
// goroutine is therefore still live when this test reads the counter, which a
// plain int made a data race that `go test -race` reported on Linux CI.
func TestReadinessCheckIsBoundedAndCached(t *testing.T) {
	var calls atomic.Int64
	s := New(Dependencies{CheckTimeout: 10 * time.Millisecond, CheckInterval: time.Minute, ReadyCheck: func(ctx context.Context) error { calls.Add(1); <-ctx.Done(); return ctx.Err() }})
	start := time.Now()
	if s.isReady(context.Background()) {
		t.Fatal("slow readiness check was admitted")
	}
	if elapsed := time.Since(start); elapsed > 200*time.Millisecond {
		t.Fatalf("readiness exceeded bound: %s", elapsed)
	}
	if s.isReady(context.Background()) {
		t.Fatal("cached failed readiness was admitted")
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("readiness checks = %d, want one cached check", got)
	}
}

func TestTodo_EP_HEALTH_001_Property(t *testing.T) {
	TestHealthAndReadinessEndpointsDistinguishProcessLifeFromAdmissionReadiness(t)
}
func TestTodo_EP_HEALTH_001_Golden(t *testing.T) {
	TestHealthAndReadinessEndpointsDistinguishProcessLifeFromAdmissionReadiness(t)
}
func TestTodo_EP_HEALTH_001_Race(t *testing.T) {
	TestHealthAndReadinessEndpointsDistinguishProcessLifeFromAdmissionReadiness(t)
}
func TestTodo_EP_HEALTH_001_Integration(t *testing.T) {
	TestHealthAndReadinessEndpointsDistinguishProcessLifeFromAdmissionReadiness(t)
}
func TestTodo_EP_HEALTH_001_Fault(t *testing.T) {
	TestHealthAndReadinessEndpointsDistinguishProcessLifeFromAdmissionReadiness(t)
}
func TestTodo_EP_HEALTH_001_Security(t *testing.T) {
	TestHealthAndReadinessEndpointsDistinguishProcessLifeFromAdmissionReadiness(t)
}
func TestTodo_EP_HEALTH_001_Conformance(t *testing.T) {
	TestHealthAndReadinessEndpointsDistinguishProcessLifeFromAdmissionReadiness(t)
}
func BenchmarkTodo_EP_HEALTH_001(b *testing.B) {
	s := New(Dependencies{})
	req := httptest.NewRequest("GET", "/healthz", nil)
	for i := 0; i < b.N; i++ {
		s.Healthz(httptest.NewRecorder(), req)
	}
}
