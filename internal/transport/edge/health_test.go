package edge_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/monstercameron/hcm-next/internal/transport/edge"
	transporthealth "github.com/monstercameron/hcm-next/internal/transport/health"
	"github.com/monstercameron/hcm-next/internal/transport/transporttest"
)

func TestHealthRoutesRemainDistinctThroughEdge(t *testing.T) {
	verifier, err := transporttest.NewVerifier(func() time.Time { return time.Now().UTC() })
	if err != nil {
		t.Fatalf("NewVerifier: %v", err)
	}
	server, err := edge.NewHandler(edge.Options{
		Config: transporttest.Config(verifier, time.Now, "health-edge", nil),
		Health: transporthealth.New(transporthealth.Dependencies{
			Live:       func() bool { return true },
			ReadyCheck: func(context.Context) error { return errors.New("private dependency reason") },
		}),
	})
	if err != nil {
		t.Fatalf("NewHandler: %v", err)
	}

	for _, test := range []struct {
		path string
		code int
	}{
		{path: "/healthz", code: http.StatusOK},
		{path: "/readyz", code: http.StatusServiceUnavailable},
	} {
		t.Run(test.path, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			server.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, test.path, nil))
			if recorder.Code != test.code {
				t.Fatalf("%s status = %d, want %d", test.path, recorder.Code, test.code)
			}
		})
	}
}
