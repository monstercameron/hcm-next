// Package health owns the non-disclosing process liveness and admission
// readiness endpoints.
package health

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"google.golang.org/grpc"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"

	"github.com/monstercameron/hcm-next/internal/platform/bootstrap"
)

const ReadyService = "ready"

// Dependencies supplies bounded, role-aware readiness checks. ReadyCheck is
// expected to check the local admission prerequisites and return no details;
// details belong in authorized telemetry, never in the public response.
type Dependencies struct {
	Role          bootstrap.Role
	Live          func() bool
	ReadyCheck    func(context.Context) error
	CheckInterval time.Duration
	CheckTimeout  time.Duration
	Now           func() time.Time
}

type Server struct {
	healthpb.UnimplementedHealthServer
	deps      Dependencies
	mu        sync.Mutex
	checkedAt time.Time
	ready     bool
}

func Version() int { return 1 }

func Explain(ready bool) string {
	if ready {
		return "health v1 ready"
	}
	return "health v1 not-ready"
}

func New(deps Dependencies) *Server {
	if deps.Now == nil {
		deps.Now = func() time.Time { return time.Now().UTC() }
	}
	if deps.CheckInterval <= 0 {
		deps.CheckInterval = 5 * time.Second
	}
	if deps.CheckTimeout <= 0 {
		deps.CheckTimeout = 2 * time.Second
	}
	return &Server{deps: deps}
}

func Register(srv *grpc.Server, deps Dependencies) { healthpb.RegisterHealthServer(srv, New(deps)) }

func (s *Server) Check(ctx context.Context, req *healthpb.HealthCheckRequest) (*healthpb.HealthCheckResponse, error) {
	if req != nil && req.GetService() == ReadyService {
		if s.isReady(ctx) {
			return &healthpb.HealthCheckResponse{Status: healthpb.HealthCheckResponse_SERVING}, nil
		}
		return &healthpb.HealthCheckResponse{Status: healthpb.HealthCheckResponse_NOT_SERVING}, nil
	}
	if s.isLive() {
		return &healthpb.HealthCheckResponse{Status: healthpb.HealthCheckResponse_SERVING}, nil
	}
	return &healthpb.HealthCheckResponse{Status: healthpb.HealthCheckResponse_NOT_SERVING}, nil
}

func (s *Server) Healthz(w http.ResponseWriter, r *http.Request) { s.writeStatus(w, s.isLive()) }
func (s *Server) Readyz(w http.ResponseWriter, r *http.Request) {
	s.writeStatus(w, s.isReady(r.Context()))
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", s.Healthz)
	mux.HandleFunc("/readyz", s.Readyz)
	return mux
}

func (s *Server) isLive() bool { return s.deps.Live == nil || s.deps.Live() }

func (s *Server) isReady(parent context.Context) bool {
	now := s.deps.Now().UTC()
	s.mu.Lock()
	if !s.checkedAt.IsZero() && now.Sub(s.checkedAt) < s.deps.CheckInterval {
		ready := s.ready
		s.mu.Unlock()
		return ready
	}
	s.mu.Unlock()
	ctx, cancel := context.WithTimeout(parent, s.deps.CheckTimeout)
	defer cancel()
	results := make(chan error, 1)
	go func() {
		if s.deps.ReadyCheck == nil {
			results <- nil
			return
		}
		results <- s.deps.ReadyCheck(ctx)
	}()
	var err error
	select {
	case err = <-results:
	case <-ctx.Done():
		err = ctx.Err()
	}
	ready := err == nil && ctx.Err() == nil
	s.mu.Lock()
	s.checkedAt, s.ready = now, ready
	s.mu.Unlock()
	return ready
}

func (s *Server) writeStatus(w http.ResponseWriter, ok bool) {
	status := http.StatusOK
	if !ok {
		status = http.StatusServiceUnavailable
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(struct {
		OK bool `json:"ok"`
	}{OK: ok})
}
